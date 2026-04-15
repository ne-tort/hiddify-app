package l3router

import (
	"encoding/binary"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
)

var _ Engine = (*MemEngine)(nil)
var _ RouteStore = (*MemEngine)(nil)
var _ SessionBinding = (*MemEngine)(nil)

// MemEngine is a concrete Engine + RouteStore + SessionBinding for tests and in-process sing-box.
// It is safe for concurrent use.
type MemEngine struct {
	mu sync.Mutex

	state      atomic.Pointer[memEngineState]
	aclEnabled atomic.Bool
}

// NewMemEngine returns an empty router engine.
func NewMemEngine() *MemEngine {
	e := &MemEngine{}
	e.state.Store(&memEngineState{
		routes:         make(map[RouteID]compiledRoute),
		sessionIngress: make(map[SessionKey]RouteID),
		routeEgress:    make(map[RouteID]SessionKey),
	})
	// Keep MemEngine standalone behavior backward-compatible for direct callers/tests.
	// Endpoint-level JSON config can explicitly disable ACL via SetACLEnabled(false).
	e.aclEnabled.Store(true)
	return e
}

// SetACLEnabled toggles AllowedSrc/AllowedDst checks in HandleIngress.
// When disabled, dataplane keeps only parse+binding+LPM/no-loop routing checks.
func (e *MemEngine) SetACLEnabled(enabled bool) {
	e.aclEnabled.Store(enabled)
}

func (e *MemEngine) UpsertRoute(r Route) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	next.routes[r.ID] = cloneRoute(r)
	next.rebuildIndexes()
	e.state.Store(next)
}

func (e *MemEngine) RemoveRoute(id RouteID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	delete(next.routes, id)
	delete(next.routeEgress, id)
	for sk, rid := range next.sessionIngress {
		if rid == id {
			delete(next.sessionIngress, sk)
		}
	}
	next.rebuildIndexes()
	e.state.Store(next)
}

func (e *MemEngine) SetIngressSession(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	next.sessionIngress[session] = routeID
	e.state.Store(next)
}

func (e *MemEngine) ClearIngressSession(session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	delete(next.sessionIngress, session)
	e.state.Store(next)
}

func (e *MemEngine) ClearIngressSessionRoute(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	routeSet, exists := next.sessionIngress[session]
	if !exists {
		return
	}
	if routeSet == routeID {
		delete(next.sessionIngress, session)
	}
	e.state.Store(next)
}

func (e *MemEngine) SetEgressSession(routeID RouteID, session SessionKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	next.routeEgress[routeID] = session
	next.rebuildIndexes()
	e.state.Store(next)
}

func (e *MemEngine) ClearEgressSession(routeID RouteID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.state.Load()
	next := prev.clone()
	delete(next.routeEgress, routeID)
	next.rebuildIndexes()
	e.state.Store(next)
}

// HandleIngress implements [Engine].
func (e *MemEngine) HandleIngress(packet []byte, ingress SessionKey) Decision {
	if len(packet) < 1 {
		return Decision{Action: ActionDrop, DropReason: DropMalformedPacket}
	}
	state := e.state.Load()
	ingressRouteID, ok := state.sessionIngress[ingress]
	if !ok {
		return Decision{Action: ActionDrop, DropReason: DropNoIngressRoute}
	}
	ingressRoute, ok := state.routes[ingressRouteID]
	if !ok {
		return Decision{Action: ActionDrop, DropReason: DropNoIngressRoute}
	}
	enforceACL := e.aclEnabled.Load()
	switch packet[0] >> 4 {
	case 4:
		src4, dst4, ok := packetSrcDstV4(packet)
		if !ok {
			return Decision{Action: ActionDrop, DropReason: DropMalformedPacket}
		}
		if enforceACL && !ingressRoute.allowedSrcMatcher.containsV4(src4) {
			return Decision{Action: ActionDrop, DropReason: DropACLSource}
		}
		if enforceACL && ingressRoute.allowedDstMatcher.hasRules() && !ingressRoute.allowedDstMatcher.containsV4(dst4) {
			return Decision{Action: ActionDrop, DropReason: DropACLDestination}
		}
		egressSession, ok := state.fibLookupForwardSessionV4(dst4, ingress)
		if !ok {
			return Decision{Action: ActionDrop, DropReason: DropNoEgressRoute}
		}
		return Decision{Action: ActionForward, EgressSession: egressSession}
	case 6:
		src, dst, ok := packetSrcDst(packet)
		if !ok {
			return Decision{Action: ActionDrop, DropReason: DropMalformedPacket}
		}
		src16 := src.As16()
		dst16 := dst.As16()
		srcHi := binary.BigEndian.Uint64(src16[:8])
		srcLo := binary.BigEndian.Uint64(src16[8:])
		dstHi := binary.BigEndian.Uint64(dst16[:8])
		dstLo := binary.BigEndian.Uint64(dst16[8:])
		if enforceACL && !ingressRoute.allowedSrcMatcher.containsV6(srcHi, srcLo) {
			return Decision{Action: ActionDrop, DropReason: DropACLSource}
		}
		if enforceACL && ingressRoute.allowedDstMatcher.hasRules() && !ingressRoute.allowedDstMatcher.containsV6(dstHi, dstLo) {
			return Decision{Action: ActionDrop, DropReason: DropACLDestination}
		}
		egressSession, ok := state.fibLookupForwardSession(dst, ingress)
		if !ok {
			return Decision{Action: ActionDrop, DropReason: DropNoEgressRoute}
		}
		return Decision{Action: ActionForward, EgressSession: egressSession}
	default:
		return Decision{Action: ActionDrop, DropReason: DropMalformedPacket}
	}
}

type memEngineState struct {
	routes         map[RouteID]compiledRoute
	sessionIngress map[SessionKey]RouteID
	routeEgress    map[RouteID]SessionKey
	fib4           *fibTrieNodeV4
	fib6           *fibTrieNode
}

type compiledRoute struct {
	Route
	allowedSrcMatcher prefixMatcher
	allowedDstMatcher prefixMatcher
}

func (s *memEngineState) clone() *memEngineState {
	next := &memEngineState{
		routes:         make(map[RouteID]compiledRoute, len(s.routes)),
		sessionIngress: make(map[SessionKey]RouteID, len(s.sessionIngress)),
		routeEgress:    make(map[RouteID]SessionKey, len(s.routeEgress)),
	}
	for id, r := range s.routes {
		next.routes[id] = r
	}
	for sk, rid := range s.sessionIngress {
		next.sessionIngress[sk] = rid
	}
	for rid, sk := range s.routeEgress {
		next.routeEgress[rid] = sk
	}
	next.fib4 = s.fib4
	next.fib6 = s.fib6
	return next
}

func cloneRoute(r Route) compiledRoute {
	cp := compiledRoute{
		Route: Route{
			ID:               r.ID,
			Owner:            r.Owner,
			AllowedSrc:       append([]netip.Prefix(nil), r.AllowedSrc...),
			AllowedDst:       append([]netip.Prefix(nil), r.AllowedDst...),
			ExportedPrefixes: append([]netip.Prefix(nil), r.ExportedPrefixes...),
		},
	}
	cp.allowedSrcMatcher = newPrefixMatcher(cp.AllowedSrc)
	cp.allowedDstMatcher = newPrefixMatcher(cp.AllowedDst)
	return cp
}

func (s *memEngineState) rebuildIndexes() {
	s.fib4 = nil
	s.fib6 = nil
	for id, r := range s.routes {
		egressSession, ok := s.routeEgress[id]
		if !ok {
			continue
		}
		for _, p := range r.ExportedPrefixes {
			c := fibCandidate{
				routeID: id,
				bits:    p.Bits(),
				egress:  egressSession,
			}
			if p.Addr().Is4() {
				ip := p.Addr().As4()
				s.fib4 = fibInsertV4(s.fib4, binary.BigEndian.Uint32(ip[:]), p.Bits(), c)
			} else if p.Addr().Is6() {
				ip := p.Addr().As16()
				s.fib6 = fibInsert(s.fib6, ip[:], p.Bits(), c)
			}
		}
	}
	fibFinalizeV4(s.fib4)
	fibFinalize(s.fib6)
}

func (s *memEngineState) fibLookupForwardSession(addr netip.Addr, ingress SessionKey) (SessionKey, bool) {
	var bestSession SessionKey
	bestBits := -1
	if addr.Is4() {
		ip := addr.As4()
		bestBits, _, bestSession = fibLookupV4(s.fib4, binary.BigEndian.Uint32(ip[:]), ingress)
	} else if addr.Is6() {
		ip := addr.As16()
		bestBits, _, bestSession = fibLookup(s.fib6, ip[:], ingress)
	}
	if bestBits < 0 {
		return "", false
	}
	return bestSession, true
}

func (s *memEngineState) fibLookupForwardSessionV4(addr uint32, ingress SessionKey) (SessionKey, bool) {
	bestBits, _, bestSession := fibLookupV4(s.fib4, addr, ingress)
	if bestBits < 0 {
		return "", false
	}
	return bestSession, true
}

type prefixMatcher struct {
	allowAll bool
	v4       []maskedPrefixV4
	v6       []maskedPrefixV6
}

type maskedPrefixV4 struct {
	mask uint32
	net  uint32
}

type maskedPrefixV6 struct {
	maskHi uint64
	maskLo uint64
	netHi  uint64
	netLo  uint64
}

func newPrefixMatcher(list []netip.Prefix) prefixMatcher {
	if len(list) == 0 {
		return prefixMatcher{allowAll: true}
	}
	m := prefixMatcher{}
	for _, p := range list {
		if p.Addr().Is4() {
			ip := p.Addr().As4()
			mask := uint32(0)
			if p.Bits() > 0 {
				mask = ^uint32(0) << (32 - p.Bits())
			}
			v := (uint32(ip[0]) << 24) | (uint32(ip[1]) << 16) | (uint32(ip[2]) << 8) | uint32(ip[3])
			m.v4 = append(m.v4, maskedPrefixV4{mask: mask, net: v & mask})
			continue
		}
		if p.Addr().Is6() {
			ip := p.Addr().As16()
			bits := p.Bits()
			var maskHi uint64
			var maskLo uint64
			switch {
			case bits <= 0:
				maskHi, maskLo = 0, 0
			case bits < 64:
				maskHi = ^uint64(0) << (64 - bits)
				maskLo = 0
			case bits == 64:
				maskHi = ^uint64(0)
				maskLo = 0
			case bits < 128:
				maskHi = ^uint64(0)
				maskLo = ^uint64(0) << (128 - bits)
			default:
				maskHi = ^uint64(0)
				maskLo = ^uint64(0)
			}
			hi := (uint64(ip[0]) << 56) | (uint64(ip[1]) << 48) | (uint64(ip[2]) << 40) | (uint64(ip[3]) << 32) |
				(uint64(ip[4]) << 24) | (uint64(ip[5]) << 16) | (uint64(ip[6]) << 8) | uint64(ip[7])
			lo := (uint64(ip[8]) << 56) | (uint64(ip[9]) << 48) | (uint64(ip[10]) << 40) | (uint64(ip[11]) << 32) |
				(uint64(ip[12]) << 24) | (uint64(ip[13]) << 16) | (uint64(ip[14]) << 8) | uint64(ip[15])
			m.v6 = append(m.v6, maskedPrefixV6{
				maskHi: maskHi,
				maskLo: maskLo,
				netHi:  hi & maskHi,
				netLo:  lo & maskLo,
			})
		}
	}
	return m
}

func (m prefixMatcher) hasRules() bool {
	return !m.allowAll
}

func (m prefixMatcher) contains(addr netip.Addr) bool {
	if m.allowAll {
		return true
	}
	if addr.Is4() {
		ip := addr.As4()
		v := (uint32(ip[0]) << 24) | (uint32(ip[1]) << 16) | (uint32(ip[2]) << 8) | uint32(ip[3])
		for _, p := range m.v4 {
			if (v & p.mask) == p.net {
				return true
			}
		}
		return false
	}
	if addr.Is6() {
		ip := addr.As16()
		hi := (uint64(ip[0]) << 56) | (uint64(ip[1]) << 48) | (uint64(ip[2]) << 40) | (uint64(ip[3]) << 32) |
			(uint64(ip[4]) << 24) | (uint64(ip[5]) << 16) | (uint64(ip[6]) << 8) | uint64(ip[7])
		lo := (uint64(ip[8]) << 56) | (uint64(ip[9]) << 48) | (uint64(ip[10]) << 40) | (uint64(ip[11]) << 32) |
			(uint64(ip[12]) << 24) | (uint64(ip[13]) << 16) | (uint64(ip[14]) << 8) | uint64(ip[15])
		for _, p := range m.v6 {
			if (hi&p.maskHi) == p.netHi && (lo&p.maskLo) == p.netLo {
				return true
			}
		}
		return false
	}
	return false
}

func (m prefixMatcher) containsV4(v uint32) bool {
	if m.allowAll {
		return true
	}
	for _, p := range m.v4 {
		if (v & p.mask) == p.net {
			return true
		}
	}
	return false
}

func (m prefixMatcher) containsV6(hi, lo uint64) bool {
	if m.allowAll {
		return true
	}
	for _, p := range m.v6 {
		if (hi&p.maskHi) == p.netHi && (lo&p.maskLo) == p.netLo {
			return true
		}
	}
	return false
}

type fibCandidate struct {
	routeID RouteID
	bits    int
	egress  SessionKey
}

type fibTrieNode struct {
	child      [2]*fibTrieNode
	candidates []fibCandidate
}

type fibTrieNodeV4 struct {
	child      [2]*fibTrieNodeV4
	candidates []fibCandidate
}

func fibInsert(root *fibTrieNode, addr []byte, bits int, c fibCandidate) *fibTrieNode {
	if root == nil {
		root = &fibTrieNode{}
	}
	n := root
	for i := 0; i < bits; i++ {
		b := fibBitAt(addr, i)
		if n.child[b] == nil {
			n.child[b] = &fibTrieNode{}
		}
		n = n.child[b]
	}
	n.candidates = append(n.candidates, c)
	return root
}

func fibInsertV4(root *fibTrieNodeV4, addr uint32, bits int, c fibCandidate) *fibTrieNodeV4 {
	if root == nil {
		root = &fibTrieNodeV4{}
	}
	n := root
	for i := 0; i < bits; i++ {
		b := int((addr >> uint(31-i)) & 1)
		if n.child[b] == nil {
			n.child[b] = &fibTrieNodeV4{}
		}
		n = n.child[b]
	}
	n.candidates = append(n.candidates, c)
	return root
}

func fibLookup(root *fibTrieNode, addr []byte, ingress SessionKey) (int, RouteID, SessionKey) {
	if root == nil {
		return -1, 0, ""
	}
	bestBits := -1
	var bestRoute RouteID
	var bestSession SessionKey
	n := root
	update := func(c fibCandidate) {
		if c.egress == ingress {
			return
		}
		if c.bits > bestBits || (c.bits == bestBits && c.routeID < bestRoute) {
			bestBits = c.bits
			bestRoute = c.routeID
			bestSession = c.egress
		}
	}
	for _, c := range n.candidates {
		update(c)
	}
	for i := 0; i < len(addr)*8; i++ {
		b := fibBitAt(addr, i)
		n = n.child[b]
		if n == nil {
			break
		}
		for _, c := range n.candidates {
			update(c)
		}
	}
	return bestBits, bestRoute, bestSession
}

func fibLookupV4(root *fibTrieNodeV4, addr uint32, ingress SessionKey) (int, RouteID, SessionKey) {
	if root == nil {
		return -1, 0, ""
	}
	bestBits := -1
	var bestRoute RouteID
	var bestSession SessionKey
	n := root
	update := func(c fibCandidate) {
		if c.egress == ingress {
			return
		}
		if c.bits > bestBits || (c.bits == bestBits && c.routeID < bestRoute) {
			bestBits = c.bits
			bestRoute = c.routeID
			bestSession = c.egress
		}
	}
	for _, c := range n.candidates {
		update(c)
	}
	for i := 0; i < 32; i++ {
		b := int((addr >> uint(31-i)) & 1)
		n = n.child[b]
		if n == nil {
			break
		}
		for _, c := range n.candidates {
			update(c)
		}
	}
	return bestBits, bestRoute, bestSession
}

func fibFinalize(node *fibTrieNode) {
	if node == nil {
		return
	}
	sort.Slice(node.candidates, func(i, j int) bool {
		if node.candidates[i].bits != node.candidates[j].bits {
			return node.candidates[i].bits > node.candidates[j].bits
		}
		return node.candidates[i].routeID < node.candidates[j].routeID
	})
	fibFinalize(node.child[0])
	fibFinalize(node.child[1])
}

func fibFinalizeV4(node *fibTrieNodeV4) {
	if node == nil {
		return
	}
	sort.Slice(node.candidates, func(i, j int) bool {
		if node.candidates[i].bits != node.candidates[j].bits {
			return node.candidates[i].bits > node.candidates[j].bits
		}
		return node.candidates[i].routeID < node.candidates[j].routeID
	})
	fibFinalizeV4(node.child[0])
	fibFinalizeV4(node.child[1])
}

func fibBitAt(addr []byte, bit int) int {
	return int((addr[bit/8] >> (7 - uint(bit%8))) & 1)
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

func packetSrcDstV4(b []byte) (src, dst uint32, ok bool) {
	if len(b) < 20 || (b[0]>>4) != 4 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(b[12:16]), binary.BigEndian.Uint32(b[16:20]), true
}
