package l3routerendpoint

import (
	rt "github.com/sagernet/sing-box/common/l3router"
	"github.com/sagernet/sing/common/buf"
	N "github.com/sagernet/sing/common/network"
)

func (e *Endpoint) enterSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	if e.activeUserSession == nil {
		e.activeUserSession = make(map[string]rt.SessionKey)
	}
	if e.sessionIngressPeer == nil {
		e.sessionIngressPeer = make(map[rt.SessionKey]rt.PeerID)
	}
	if e.peerEgressSession == nil {
		e.peerEgressSession = make(map[rt.PeerID]rt.SessionKey)
	}
	e.userRef[sk]++
	if e.userRef[sk] == 1 {
		user := string(sk)
		e.sessMu.Lock()
		e.activeUserSession[user] = sk
		e.sessMu.Unlock()
		e.bindUserSession(user, sk)
	}
}

func (e *Endpoint) leaveSession(sk rt.SessionKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	e.userRef[sk]--
	if e.userRef[sk] == 0 {
		user := string(sk)
		delete(e.userRef, sk)
		e.sessMu.Lock()
		delete(e.activeUserSession, user)
		e.sessMu.Unlock()
		e.unbindUserSession(user, sk)
	}
}

func (e *Endpoint) bindUserSession(user string, sk rt.SessionKey) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	if e.userPeers == nil {
		return
	}
	peerIDs := e.userPeers[user]
	if len(peerIDs) == 0 {
		delete(e.sessionIngressPeer, sk)
		e.publishBindingSnapshotLocked()
		return
	}
	for rid := range peerIDs {
		e.sessionIngressPeer[sk] = rt.PeerID(rid)
		e.peerEgressSession[rt.PeerID(rid)] = sk
	}
	e.publishBindingSnapshotLocked()
}

func (e *Endpoint) unbindUserSession(user string, sk rt.SessionKey) {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	peerIDs := e.userPeers[user]
	for rid := range peerIDs {
		if ingress, ok := e.sessionIngressPeer[sk]; ok && ingress == rt.PeerID(rid) {
			delete(e.sessionIngressPeer, sk)
		}
		peer := rt.PeerID(rid)
		if mapped, ok := e.peerEgressSession[peer]; ok && mapped == sk {
			delete(e.peerEgressSession, peer)
		}
	}
	e.publishBindingSnapshotLocked()
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

func (e *Endpoint) ingressPeerForSession(sk rt.SessionKey) (rt.PeerID, bool) {
	snapshot := e.bindings.Load()
	if snapshot == nil {
		return 0, false
	}
	peer, ok := snapshot.ingress[sk]
	return peer, ok
}

func (e *Endpoint) egressSessionForPeer(peer rt.PeerID) (rt.SessionKey, bool) {
	snapshot := e.bindings.Load()
	if snapshot == nil {
		return "", false
	}
	session, ok := snapshot.egress[peer]
	return session, ok
}
