package l3router

import (
	"net/netip"
	"sync"
)

var _ Engine = (*MemEngine)(nil)
var _ RouteStore = (*MemEngine)(nil)
var _ SessionBinding = (*MemEngine)(nil)

// MemEngine is a concrete Engine + RouteStore + SessionBinding for tests and in-process sing-box.
// It is safe for concurrent use.
type MemEngine struct {
	mu sync.RWMutex

	routes map[RouteID]Route

	// ingress: which logical Route an inbound session represents (anti-spoof / policy).
	sessionIngress map[SessionKey]RouteID
	// egress: active delivery session per Route (peer-like destination).
	routeEgress map[RouteID]SessionKey
}

// NewMemEngine returns an empty router engine.
func NewMemEngine() *MemEngine {
	return &MemEngine{
		routes:         make(map[RouteID]Route),
		sessionIngress: make(map[SessionKey]RouteID),
		routeEgress:    make(map[RouteID]SessionKey),
	}
}

func (e *MemEngine) UpsertRoute(r Route) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.routes[r.ID] = r
}

func (e *MemEngine) RemoveRoute(id RouteID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.routes, id)
	delete(e.routeEgress, id)
	for sk, rid := range e.sessionIngress {
		if rid == id {
			delete(e.sessionIngress, sk)
		}
	}
}

func (e *MemEngine) SetIngressSession(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionIngress[session] = routeID
}

func (e *MemEngine) ClearIngressSession(session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sessionIngress, session)
}

func (e *MemEngine) ClearIngressSessionRoute(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	routeSet, exists := e.sessionIngress[session]
	if !exists {
		return
	}
	if routeSet == routeID {
		delete(e.sessionIngress, session)
	}
}

func (e *MemEngine) SetEgressSession(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.routeEgress[routeID] = session
}

func (e *MemEngine) ClearEgressSession(routeID RouteID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.routeEgress, routeID)
}

// HandleIngress implements [Engine].
func (e *MemEngine) HandleIngress(packet []byte, ingress SessionKey) Decision {
	src, dst, ok := packetSrcDst(packet)
	if !ok {
		return Decision{Action: ActionDrop, DropReason: DropMalformedPacket}
	}

	e.mu.RLock()
	ingressRouteID, ok := e.sessionIngress[ingress]
	if !ok {
		e.mu.RUnlock()
		return Decision{Action: ActionDrop, DropReason: DropNoIngressRoute}
	}
	ingressRoute, ok := e.routes[ingressRouteID]
	if !ok {
		e.mu.RUnlock()
		return Decision{Action: ActionDrop, DropReason: DropNoIngressRoute}
	}
	if !prefixListContains(ingressRoute.AllowedSrc, src) {
		e.mu.RUnlock()
		return Decision{Action: ActionDrop, DropReason: DropACLSource}
	}
	if len(ingressRoute.AllowedDst) > 0 && !prefixListContains(ingressRoute.AllowedDst, dst) {
		e.mu.RUnlock()
		return Decision{Action: ActionDrop, DropReason: DropACLDestination}
	}
	egressSession, ok := e.fibLookupForwardSessionLocked(dst, ingress)
	if !ok {
		e.mu.RUnlock()
		return Decision{Action: ActionDrop, DropReason: DropNoEgressRoute}
	}
	e.mu.RUnlock()

	return Decision{
		Action:        ActionForward,
		EgressSession: egressSession,
	}
}

func (e *MemEngine) fibLookupForwardSessionLocked(addr netip.Addr, ingress SessionKey) (SessionKey, bool) {
	var bestRoute RouteID
	var bestSession SessionKey
	var bestBits int = -1
	for id, r := range e.routes {
		for _, p := range r.ExportedPrefixes {
			if !p.Contains(addr) {
				continue
			}
			egressSession, ok := e.routeEgress[id]
			if !ok || egressSession == ingress {
				continue
			}
			b := p.Bits()
			// Stable tie-breaker by RouteID keeps forwarding deterministic when
			// multiple routes export prefixes with identical mask length.
			if b > bestBits || (b == bestBits && id < bestRoute) {
				bestBits = b
				bestRoute = id
				bestSession = egressSession
			}
		}
	}
	if bestBits < 0 {
		return "", false
	}
	return bestSession, true
}

func prefixListContains(list []netip.Prefix, addr netip.Addr) bool {
	if len(list) == 0 {
		return true
	}
	for _, p := range list {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func packetSrcDst(b []byte) (src, dst netip.Addr, ok bool) {
	if len(b) < 1 {
		return netip.Addr{}, netip.Addr{}, false
	}
	switch b[0] >> 4 {
	case 4:
		if len(b) < 20 {
			return netip.Addr{}, netip.Addr{}, false
		}
		src = netip.AddrFrom4([4]byte(b[12:16]))
		dst = netip.AddrFrom4([4]byte(b[16:20]))
		return src, dst, true
	case 6:
		if len(b) < 40 {
			return netip.Addr{}, netip.Addr{}, false
		}
		src = netip.AddrFrom16([16]byte(b[8:24]))
		dst = netip.AddrFrom16([16]byte(b[24:40]))
		return src, dst, true
	default:
		return netip.Addr{}, netip.Addr{}, false
	}
}
