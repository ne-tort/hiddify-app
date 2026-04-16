package l3routerendpoint

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"time"

	"github.com/sagernet/sing-box/adapter"
	rt "github.com/sagernet/sing-box/common/l3router"
	"github.com/sagernet/sing/common/buf"
	singbufio "github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// defaultEgressQueueCap bounds per-peer buffering between ingress and the single egress writer.
// Too small ⇒ ingress blocks or drops under burst; too large ⇒ memory under attack.
const defaultEgressQueueCap = 2048

// egressWriteDeadlineMinInterval avoids SetWriteDeadline (typically one syscall per call on TCP) on every
// forwarded datagram — that capped effective throughput on high-PPS overlays.
const egressWriteDeadlineMinInterval = 400 * time.Millisecond

// egressWriteBlockBudget is the maximum duration a single blocked WritePacket may wait before the deadline fires.
const egressWriteBlockBudget = 30 * time.Second
const maxEgressBatchSize = 32

func (e *Endpoint) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	e.logger.WarnContext(ctx, "[l3router] inbound TCP is unsupported; expected UDP raw IP overlay")
	N.CloseOnHandshakeFailure(conn, onClose, os.ErrInvalid)
}

// NewPacketConnectionEx is the only entry that ties sing-box inbound identity to l3router:
// metadata.User → SessionKey → bindUserSession (Route.user). common/l3router never sees User.
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
	ingressPeer, hasIngressPeer := e.ingressPeerForSession(sk)
	go func() {
		defer e.leaveSession(sk)
		defer e.unregisterSession(sk, conn)
		if !hasIngressPeer {
			e.runOverlay(ctx, conn, sk, 0, onClose)
			return
		}
		e.runOverlay(ctx, conn, sk, ingressPeer, onClose)
	}()
}

func (e *Endpoint) runOverlay(ctx context.Context, conn N.PacketConn, ingressSession rt.SessionKey, ingress rt.PeerID, onClose N.CloseHandlerFunc) {
	const detailFlushEvery = 64
	var localDropFilterSource uint64
	var localDropFilterDest uint64
	var localFragmentDrops uint64
	flushDetail := func(force bool) {
		if !e.detailCountersEnabled() {
			localDropFilterSource = 0
			localDropFilterDest = 0
			localFragmentDrops = 0
			return
		}
		total := localDropFilterSource + localDropFilterDest + localFragmentDrops
		if !force && total < detailFlushEvery {
			return
		}
		if localDropFilterSource > 0 {
			if e.detailCountersEnabled() {
				e.dropFilterSource.Add(localDropFilterSource)
			}
			localDropFilterSource = 0
		}
		if localDropFilterDest > 0 {
			if e.detailCountersEnabled() {
				e.dropFilterDest.Add(localDropFilterDest)
			}
			localDropFilterDest = 0
		}
		if localFragmentDrops > 0 {
			if e.detailCountersEnabled() {
				e.fragmentDrops.Add(localFragmentDrops)
			}
			localFragmentDrops = 0
		}
	}
	defer flushDetail(true)
	for {
		buffer := buf.NewPacket()
		_, err := conn.ReadPacket(buffer)
		if err != nil {
			flushDetail(true)
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
		e.addIngressPackets(1)

		dec, egressSession, hasForward := e.resolveForwardDecision(pkt, ingressSession)
		if !hasForward {
			buffer.Release()
			e.addDropPackets(1)
			switch dec.DropReason {
			case rt.DropFilterSource:
				localDropFilterSource++
			case rt.DropFilterDestination:
				localDropFilterDest++
			}
			flushDetail(false)
			continue
		}
		if e.fragmentPolicy == fragmentPolicyDrop && isIPv4Fragment(pkt) {
			buffer.Release()
			localFragmentDrops++
			e.addDropPackets(1)
			flushDetail(false)
			continue
		}
		queued, queueFull := e.enqueueEgress(egressSession, buffer)
		if !queued {
			// enqueueEgress does not Release on failure; caller owns payload.
			buffer.Release()
			e.addDropPackets(1)
			if queueFull {
				e.addQueueOverflow(1)
			} else {
				e.addDropNoSession(1)
			}
			continue
		}
		e.addForwardPackets(1)
		// buffer ownership transferred to egressWorker (sing managed pool).
	}
}

func (e *Endpoint) resolveForwardDecision(packet []byte, ingressSession rt.SessionKey) (rt.Decision, rt.SessionKey, bool) {
	ingressPeer, ok := e.ingressPeerForSession(ingressSession)
	if !ok {
		return rt.Decision{Action: rt.ActionDrop, DropReason: rt.DropNoIngressRoute}, "", false
	}
	dec := e.engine.HandleIngressPeer(packet, ingressPeer)
	if dec.Action != rt.ActionForward {
		return dec, "", false
	}
	session, ok := e.egressSessionForPeer(dec.EgressPeerID)
	if ok && session != ingressSession {
		return dec, session, true
	}
	return rt.Decision{Action: rt.ActionDrop, DropReason: rt.DropNoEgressRoute}, "", false
}

// enqueueEgress queues payload for the egress session writer. On success, ownership moves to the worker.
// On failure, the caller must Release the buffer. Returns queueFull=true only when the egress queue rejected
// the datagram after an eviction attempt (real overflow).
func (e *Endpoint) enqueueEgress(session rt.SessionKey, payload *buf.Buffer) (queued bool, queueFull bool) {
	e.egressMu.RLock()
	queue, ok := e.egressQueues[session]
	e.egressMu.RUnlock()
	if ok {
		return e.tryEnqueue(queue, payload)
	}

	// Avoid spawning idle egress workers for unknown SessionKeys: if nobody has ever entered as this user,
	// there is no transient bind/register window to buffer for.
	if e.sessionConn(session) == nil {
		e.refMu.Lock()
		hasUserRef := e.userRef[session] > 0
		e.refMu.Unlock()
		if !hasUserRef {
			return false, false
		}
	}

	if !ok {
		e.egressMu.Lock()
		queue, ok = e.egressQueues[session]
		if !ok {
			queue = make(chan *buf.Buffer, defaultEgressQueueCap)
			e.egressQueues[session] = queue
			e.egressWg.Add(1)
			go e.egressWorker(session, queue)
		}
		e.egressMu.Unlock()
	}
	return e.tryEnqueue(queue, payload)
}

func (e *Endpoint) tryEnqueue(queue chan *buf.Buffer, payload *buf.Buffer) (queued bool, queueFull bool) {
	select {
	case queue <- payload:
		return true, false
	default:
		if e.overflowPolicy == overflowPolicyDropOldest {
			select {
			case stale := <-queue:
				if stale != nil {
					stale.Release()
				}
			default:
			}
			select {
			case queue <- payload:
				return true, false
			default:
			}
		}
		return false, true
	}
}

func (e *Endpoint) egressWorker(session rt.SessionKey, queue <-chan *buf.Buffer) {
	defer e.egressWg.Done()
	var nextDeadlineExtend time.Time
	var cachedConn N.PacketConn
	var cachedDeadlineConn interface{ SetWriteDeadline(time.Time) error }
	var deadlineCapKnown bool
	batch := e.getEgressBatch()
	defer e.putEgressBatch(batch)
	for {
		payload, ok := <-queue
		if !ok {
			return
		}
		batch.items = append(batch.items, payload)
		queueClosed := false
		for len(batch.items) < maxEgressBatchSize {
			select {
			case more, ok := <-queue:
				if !ok {
					queueClosed = true
					goto writeBatch
				}
				batch.items = append(batch.items, more)
			default:
				goto writeBatch
			}
		}

	writeBatch:
		out := e.sessionConn(session)
		if out == nil {
			for _, p := range batch.items {
				p.Release()
			}
			e.addDropNoSession(uint64(len(batch.items)))
			e.addEgressWriteFail(uint64(len(batch.items)))
			cachedConn = nil
			cachedDeadlineConn = nil
			deadlineCapKnown = false
			batch.items = batch.items[:0]
			continue
		}
		if out != cachedConn {
			cachedConn = out
			cachedDeadlineConn = nil
			deadlineCapKnown = false
		}
		// Amortize SetWriteDeadline: per-packet calls dominated syscall cost at high PPS (SMB/TCP in tunnel).
		if !deadlineCapKnown {
			deadlineCapKnown = true
			if deadlineConn, hasDeadline := out.(interface{ SetWriteDeadline(time.Time) error }); hasDeadline {
				cachedDeadlineConn = deadlineConn
			}
		}
		if cachedDeadlineConn != nil {
			now := time.Now()
			if now.After(nextDeadlineExtend) {
				_ = cachedDeadlineConn.SetWriteDeadline(now.Add(egressWriteBlockBudget))
				nextDeadlineExtend = now.Add(egressWriteDeadlineMinInterval)
			}
		}
		// Canonical sing path: sing/common/bufio.WritePacketBuffer — same headroom rules as route/UDP.
		for _, p := range batch.items {
			_, werr := singbufio.WritePacketBuffer(out, p, e.overlayDest)
			if werr != nil {
				if isTimeoutError(werr) {
					e.addWriteTimeout(1)
				}
				e.addEgressWriteFail(1)
			}
		}
		batch.items = batch.items[:0]
		if queueClosed {
			return
		}
	}
}

func isTimeoutError(err error) bool {
	type timeout interface{ Timeout() bool }
	if te, ok := err.(timeout); ok && te.Timeout() {
		return true
	}
	return false
}

func isIPv4Fragment(pkt []byte) bool {
	if len(pkt) < 20 || (pkt[0]>>4) != 4 {
		return false
	}
	flagsAndOffset := binary.BigEndian.Uint16(pkt[6:8])
	moreFragments := (flagsAndOffset & 0x2000) != 0
	fragmentOffset := (flagsAndOffset & 0x1fff) != 0
	return moreFragments || fragmentOffset
}

func (e *Endpoint) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	e.logger.WarnContext(ctx, "[l3router] outbound ", network, " dial to ", destination, " is unsupported")
	return nil, os.ErrInvalid
}

func (e *Endpoint) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	e.logger.WarnContext(ctx, "[l3router] outbound UDP listen to ", destination, " is unsupported")
	return nil, os.ErrInvalid
}
