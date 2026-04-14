package l3routerendpoint

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	rt "github.com/sagernet/sing-box/experimental/l3router"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var (
	_ adapter.ConnectionHandlerEx       = (*Endpoint)(nil)
	_ adapter.PacketConnectionHandlerEx = (*Endpoint)(nil)
)

func RegisterEndpoint(registry *endpoint.Registry) {
	endpoint.Register[option.L3RouterEndpointOptions](registry, C.TypeL3Router, NewEndpoint)
}

// Endpoint hosts a [rt.MemEngine] data plane; multi-user inbounds route UDP here and payload bytes are raw IP datagrams.
type Endpoint struct {
	outbound.Adapter
	ctx    context.Context
	cancel context.CancelFunc
	logger logger.ContextLogger
	engine *rt.MemEngine

	overlayDest M.Socksaddr

	routeOwners map[rt.RouteID]string

	refMu   sync.Mutex
	userRef map[rt.SessionKey]int

	sessMu   sync.Mutex
	sessions map[rt.SessionKey]N.PacketConn

	ingressPackets  atomic.Uint64
	forwardPackets  atomic.Uint64
	dropPackets     atomic.Uint64
	egressWriteFail atomic.Uint64
	controlUpsertOK atomic.Uint64
	controlRemoveOK atomic.Uint64
	controlErrors   atomic.Uint64
}

// Metrics is a snapshot of l3router dataplane counters.
type Metrics struct {
	IngressPackets  uint64
	ForwardPackets  uint64
	DropPackets     uint64
	EgressWriteFail uint64
	ControlUpsertOK uint64
	ControlRemoveOK uint64
	ControlErrors   uint64
}

func NewEndpoint(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options option.L3RouterEndpointOptions) (adapter.Endpoint, error) {
	ctx, cancel := context.WithCancel(ctx)
	overlay := options.OverlayDestination
	if overlay == "" {
		overlay = "198.18.0.1:33333"
	}
	overlayDest := M.ParseSocksaddr(overlay)
	if !overlayDest.IsValid() {
		cancel()
		return nil, E.New("invalid l3router overlay_destination: ", overlay)
	}

	e := &Endpoint{
		Adapter:   outbound.NewAdapter(C.TypeL3Router, tag, []string{N.NetworkTCP, N.NetworkUDP}, nil),
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		engine:    rt.NewMemEngine(),
		overlayDest: overlayDest,
		routeOwners: make(map[rt.RouteID]string),
		userRef:     make(map[rt.SessionKey]int),
		sessions:    make(map[rt.SessionKey]N.PacketConn),
	}

	for _, ro := range options.Routes {
		r, err := routeFromOptions(ro)
		if err != nil {
			cancel()
			return nil, E.Cause(err, "l3router route ", ro.ID)
		}
		if err := e.UpsertRoute(r); err != nil {
			cancel()
			return nil, E.Cause(err, "l3router route ", ro.ID)
		}
	}
	return e, nil
}

func routeFromOptions(ro option.L3RouterRouteOptions) (rt.Route, error) {
	var r rt.Route
	r.ID = rt.RouteID(ro.ID)
	r.Owner = ro.Owner
	var err error
	r.AllowedSrc, err = parsePrefixes(ro.AllowedSrc)
	if err != nil {
		return rt.Route{}, err
	}
	r.AllowedDst, err = parsePrefixes(ro.AllowedDst)
	if err != nil {
		return rt.Route{}, err
	}
	r.ExportedPrefixes, err = parsePrefixes(ro.ExportedPrefixes)
	if err != nil {
		return rt.Route{}, err
	}
	return r, nil
}

func parsePrefixes(ss []string) ([]netip.Prefix, error) {
	if len(ss) == 0 {
		return nil, nil
	}
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Engine exposes the router data plane for protocol integration (ingress path).
func (e *Endpoint) Engine() *rt.MemEngine { return e.engine }

// UpsertRoute updates/creates one route in runtime control-plane and immediately binds
// currently active owner session (if present) as ingress+egress session.
func (e *Endpoint) UpsertRoute(r rt.Route) error {
	if err := validateRoute(r); err != nil {
		e.controlErrors.Add(1)
		return err
	}
	e.sessMu.Lock()
	prevOwner := e.routeOwners[r.ID]
	e.sessMu.Unlock()

	e.engine.UpsertRoute(r)
	e.controlUpsertOK.Add(1)
	if prevOwner != "" && prevOwner != r.Owner {
		// Drop stale bindings when route ownership changes to avoid old-session ingress loops.
		e.engine.ClearIngressSession(rt.SessionKey(prevOwner))
		e.engine.ClearEgressSession(r.ID)
	}
	e.sessMu.Lock()
	if r.Owner == "" {
		delete(e.routeOwners, r.ID)
	} else {
		e.routeOwners[r.ID] = r.Owner
	}
	e.sessMu.Unlock()
	if r.Owner == "" {
		return nil
	}
	e.refMu.Lock()
	defer e.refMu.Unlock()
	for sk, refs := range e.userRef {
		if refs <= 0 {
			continue
		}
		if string(sk) == r.Owner {
			e.engine.SetIngressSession(r.ID, sk)
			e.engine.SetEgressSession(r.ID, sk)
			return nil
		}
	}
	return nil
}

// RemoveRoute deletes one route in runtime control-plane.
func (e *Endpoint) RemoveRoute(id rt.RouteID) {
	if id == 0 {
		e.controlErrors.Add(1)
		return
	}
	e.engine.RemoveRoute(id)
	e.controlRemoveOK.Add(1)
	e.sessMu.Lock()
	delete(e.routeOwners, id)
	e.sessMu.Unlock()
}

// SnapshotMetrics returns current dataplane counters.
func (e *Endpoint) SnapshotMetrics() Metrics {
	return Metrics{
		IngressPackets:  e.ingressPackets.Load(),
		ForwardPackets:  e.forwardPackets.Load(),
		DropPackets:     e.dropPackets.Load(),
		EgressWriteFail: e.egressWriteFail.Load(),
		ControlUpsertOK: e.controlUpsertOK.Load(),
		ControlRemoveOK: e.controlRemoveOK.Load(),
		ControlErrors:   e.controlErrors.Load(),
	}
}

func validateRoute(r rt.Route) error {
	if r.ID == 0 {
		return E.New("route id must be non-zero")
	}
	if len(r.ExportedPrefixes) == 0 {
		return E.New("route exported_prefixes must not be empty")
	}
	return nil
}

func (e *Endpoint) Start(stage adapter.StartStage) error {
	if stage == adapter.StartStatePostStart {
		e.logger.InfoContext(context.Background(), "[l3router] MemEngine ready; overlay=", e.overlayDest.String())
	}
	return nil
}

func (e *Endpoint) Close() error {
	e.cancel()
	e.sessMu.Lock()
	for sk, c := range e.sessions {
		c.Close()
		delete(e.sessions, sk)
	}
	e.sessMu.Unlock()
	return nil
}

func (e *Endpoint) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	e.logger.WarnContext(ctx, "[l3router] TCP not supported; use UDP raw IP overlay")
	N.CloseOnHandshakeFailure(conn, onClose, os.ErrInvalid)
}

func (e *Endpoint) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if metadata.User == "" {
		e.logger.WarnContext(ctx, "[l3router] missing inbound user/session; drop")
		conn.Close()
		if onClose != nil {
			onClose(os.ErrInvalid)
		}
		return
	}
	sk := rt.SessionKey(metadata.User)
	e.enterSession(sk)
	e.registerSession(sk, conn)
	go func() {
		defer e.leaveSession(sk)
		defer e.unregisterSession(sk, conn)
		e.runOverlay(ctx, conn, sk, onClose)
	}()
}

func (e *Endpoint) enterSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	e.userRef[sk]++
	if e.userRef[sk] == 1 {
		e.bindUserSessions(sk)
	}
}

func (e *Endpoint) leaveSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	e.userRef[sk]--
	if e.userRef[sk] == 0 {
		delete(e.userRef, sk)
		e.unbindUserSessions(sk)
	}
}

func (e *Endpoint) bindUserSessions(sk rt.SessionKey) {
	user := string(sk)
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	for rid, owner := range e.routeOwners {
		if owner == user {
			e.engine.SetIngressSession(rid, sk)
			e.engine.SetEgressSession(rid, sk)
		}
	}
}

func (e *Endpoint) unbindUserSessions(sk rt.SessionKey) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	e.engine.ClearIngressSession(sk)
	for rid, owner := range e.routeOwners {
		if owner == string(sk) {
			e.engine.ClearEgressSession(rid)
		}
	}
}

func (e *Endpoint) registerSession(sk rt.SessionKey, conn N.PacketConn) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	if old := e.sessions[sk]; old != nil {
		old.Close()
	}
	e.sessions[sk] = conn
}

func (e *Endpoint) unregisterSession(sk rt.SessionKey, conn N.PacketConn) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	if c, ok := e.sessions[sk]; ok && c == conn {
		delete(e.sessions, sk)
	}
}

func (e *Endpoint) sessionConn(sk rt.SessionKey) N.PacketConn {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	return e.sessions[sk]
}

func (e *Endpoint) runOverlay(ctx context.Context, conn N.PacketConn, ingress rt.SessionKey, onClose N.CloseHandlerFunc) {
	for {
		buffer := buf.NewPacket()
		_, err := conn.ReadPacket(buffer)
		if err != nil {
			buffer.Release()
			if onClose != nil {
				onClose(err)
			}
			return
		}
		pkt := buffer.Bytes()
		if len(pkt) == 0 {
			buffer.Release()
			continue
		}
		e.ingressPackets.Add(1)
		payload := append([]byte(nil), pkt...)
		buffer.Release()

		dec := e.engine.HandleIngress(payload, ingress)
		if dec.Action != rt.ActionForward {
			e.dropPackets.Add(1)
			continue
		}
		e.forwardPackets.Add(1)
		out := e.sessionConn(dec.EgressSession)
		if out == nil {
			e.egressWriteFail.Add(1)
			e.logger.WarnContext(ctx, "[l3router] no session for egress ", dec.EgressSession)
			continue
		}
		outBuf := buf.NewPacket()
		_, werr := outBuf.Write(payload)
		if werr != nil {
			outBuf.Release()
			e.logger.WarnContext(ctx, "[l3router] buffer: ", werr)
			continue
		}
		werr = out.WritePacket(outBuf, e.overlayDest)
		if werr != nil {
			e.egressWriteFail.Add(1)
			outBuf.Release()
			e.logger.WarnContext(ctx, "[l3router] write egress: ", werr)
		}
	}
}

func (e *Endpoint) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	e.logger.InfoContext(ctx, "[l3router] drop TCP dial (stub)")
	return nil, os.ErrInvalid
}

func (e *Endpoint) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	e.logger.InfoContext(ctx, "[l3router] drop UDP listen (stub)")
	return nil, os.ErrInvalid
}
