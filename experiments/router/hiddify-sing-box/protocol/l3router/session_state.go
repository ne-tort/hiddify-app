package l3routerendpoint

import (
	rt "github.com/sagernet/sing-box/experimental/l3router"
	"github.com/sagernet/sing/common/buf"
	N "github.com/sagernet/sing/common/network"
)

func (e *Endpoint) enterSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	if e.activeOwnerSession == nil {
		e.activeOwnerSession = make(map[string]rt.SessionKey)
	}
	e.userRef[sk]++
	if e.userRef[sk] == 1 {
		e.activeOwnerSession[string(sk)] = sk
		e.bindUserSessions(sk)
	}
}

func (e *Endpoint) leaveSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	e.userRef[sk]--
	if e.userRef[sk] == 0 {
		delete(e.userRef, sk)
		delete(e.activeOwnerSession, string(sk))
		e.unbindUserSessions(sk)
	}
}

func (e *Endpoint) bindUserSessions(sk rt.SessionKey) {
	user := string(sk)
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	if e.ownerRoutes == nil {
		return
	}
	routeIDs := e.ownerRoutes[user]
	for rid := range routeIDs {
		e.engine.SetIngressSession(rid, sk)
		e.engine.SetEgressSession(rid, sk)
	}
}

func (e *Endpoint) unbindUserSessions(sk rt.SessionKey) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	if e.ownerRoutes == nil {
		return
	}
	routeIDs := e.ownerRoutes[string(sk)]
	for rid := range routeIDs {
		e.engine.ClearIngressSessionRoute(rid, sk)
		e.engine.ClearEgressSession(rid)
	}
}

func (e *Endpoint) registerSession(sk rt.SessionKey, conn N.PacketConn) {
	var oldConn N.PacketConn
	e.sessMu.Lock()
	if old := e.sessions[sk]; old != nil {
		oldConn = old
	}
	e.sessions[sk] = conn
	e.sessMu.Unlock()
	if oldConn != nil {
		oldConn.Close()
	}
}

func (e *Endpoint) unregisterSession(sk rt.SessionKey, conn N.PacketConn) {
	var queue chan *buf.Buffer
	e.sessMu.Lock()
	if c, ok := e.sessions[sk]; ok && c == conn {
		delete(e.sessions, sk)
	}
	e.sessMu.Unlock()
	e.egressMu.Lock()
	if q, ok := e.egressQueues[sk]; ok {
		queue = q
		delete(e.egressQueues, sk)
	}
	e.egressMu.Unlock()
	if queue != nil {
		close(queue)
	}
}

func (e *Endpoint) sessionConn(sk rt.SessionKey) N.PacketConn {
	e.sessMu.RLock()
	defer e.sessMu.RUnlock()
	return e.sessions[sk]
}
