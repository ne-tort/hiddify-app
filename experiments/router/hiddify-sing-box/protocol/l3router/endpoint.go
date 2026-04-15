package l3routerendpoint

import (
	"context"
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
	ctx     context.Context
	cancel  context.CancelFunc
	logger  logger.ContextLogger
	engine  *rt.MemEngine
	started atomic.Bool

	overlayDest M.Socksaddr

	routeOwners map[rt.RouteID]string

	refMu   sync.Mutex
	userRef map[rt.SessionKey]int

	sessMu   sync.RWMutex
	sessions map[rt.SessionKey]N.PacketConn

	egressMu     sync.RWMutex
	egressQueues map[rt.SessionKey]chan *buf.Buffer
	egressWg     sync.WaitGroup

	ingressPackets  atomic.Uint64
	forwardPackets  atomic.Uint64
	dropPackets     atomic.Uint64
	egressWriteFail atomic.Uint64
	writeTimeout    atomic.Uint64
	queueOverflow   atomic.Uint64
	dropNoSession   atomic.Uint64
	dropACLSource   atomic.Uint64
	dropACLDest     atomic.Uint64
	fragmentDrops   atomic.Uint64
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
	WriteTimeout    uint64
	QueueOverflow   uint64
	DropNoSession   uint64
	DropACLSource   uint64
	DropACLDest     uint64
	FragmentDrops   uint64
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
		Adapter:      outbound.NewAdapter(C.TypeL3Router, tag, []string{N.NetworkTCP, N.NetworkUDP}, nil),
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		engine:       rt.NewMemEngine(),
		overlayDest:  overlayDest,
		routeOwners:  make(map[rt.RouteID]string),
		userRef:      make(map[rt.SessionKey]int),
		sessions:     make(map[rt.SessionKey]N.PacketConn),
		egressQueues: make(map[rt.SessionKey]chan *buf.Buffer),
	}

	for _, ro := range options.Routes {
		r, err := ParseRouteOptions(ro)
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

// Engine exposes the router data plane for protocol integration (ingress path).
func (e *Endpoint) Engine() *rt.MemEngine { return e.engine }

// SnapshotMetrics returns current dataplane counters.
func (e *Endpoint) SnapshotMetrics() Metrics {
	return Metrics{
		IngressPackets:  e.ingressPackets.Load(),
		ForwardPackets:  e.forwardPackets.Load(),
		DropPackets:     e.dropPackets.Load(),
		EgressWriteFail: e.egressWriteFail.Load(),
		WriteTimeout:    e.writeTimeout.Load(),
		QueueOverflow:   e.queueOverflow.Load(),
		DropNoSession:   e.dropNoSession.Load(),
		DropACLSource:   e.dropACLSource.Load(),
		DropACLDest:     e.dropACLDest.Load(),
		FragmentDrops:   e.fragmentDrops.Load(),
		ControlUpsertOK: e.controlUpsertOK.Load(),
		ControlRemoveOK: e.controlRemoveOK.Load(),
		ControlErrors:   e.controlErrors.Load(),
	}
}

func (e *Endpoint) Start(stage adapter.StartStage) error {
	if stage == adapter.StartStatePostStart {
		e.started.Store(true)
		e.logger.InfoContext(context.Background(), "[l3router] MemEngine ready; overlay=", e.overlayDest.String())
	}
	return nil
}

func (e *Endpoint) IsReady() bool {
	return e.started.Load()
}

func (e *Endpoint) DisplayType() string {
	display := C.ProxyDisplayName(e.Type())
	if !e.IsReady() {
		display += " (starting)"
	}
	return display
}

func (e *Endpoint) Close() error {
	e.cancel()
	e.egressMu.Lock()
	for sk, q := range e.egressQueues {
		close(q)
		delete(e.egressQueues, sk)
	}
	e.egressMu.Unlock()
	e.egressWg.Wait()
	e.sessMu.Lock()
	for sk, c := range e.sessions {
		c.Close()
		delete(e.sessions, sk)
	}
	e.sessMu.Unlock()
	return nil
}
