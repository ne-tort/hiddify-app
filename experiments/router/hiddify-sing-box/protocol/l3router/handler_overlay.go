package l3routerendpoint

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"time"

	"github.com/sagernet/sing-box/adapter"
	rt "github.com/sagernet/sing-box/experimental/l3router"
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

func (e *Endpoint) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	e.logger.WarnContext(ctx, "[l3router] inbound TCP is unsupported; expected UDP raw IP overlay")
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

func (e *Endpoint) runOverlay(ctx context.Context, conn N.PacketConn, ingress rt.SessionKey, onClose N.CloseHandlerFunc) {
	const detailFlushEvery = 64
	var localDropACLSource uint64
	var localDropACLDest uint64
	var localFragmentDrops uint64
	flushDetail := func(force bool) {
		if !e.detailCountersEnabled() {
			localDropACLSource = 0
			localDropACLDest = 0
			localFragmentDrops = 0
			return
		}
		total := localDropACLSource + localDropACLDest + localFragmentDrops
		if !force && total < detailFlushEvery {
			return
		}
		if localDropACLSource > 0 {
			e.dropACLSource.Add(localDropACLSource)
			localDropACLSource = 0
		}
		if localDropACLDest > 0 {
			e.dropACLDest.Add(localDropACLDest)
			localDropACLDest = 0
		}
		if localFragmentDrops > 0 {
			e.fragmentDrops.Add(localFragmentDrops)
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
		e.ingressPackets.Add(1)

		dec := e.engine.HandleIngress(pkt, ingress)
		if dec.Action != rt.ActionForward {
			buffer.Release()
			e.dropPackets.Add(1)
			switch dec.DropReason {
			case rt.DropACLSource:
				localDropACLSource++
			case rt.DropACLDestination:
				localDropACLDest++
			}
			flushDetail(false)
			continue
		}
		if isIPv4Fragment(pkt) {
			buffer.Release()
			localFragmentDrops++
			e.dropPackets.Add(1)
			flushDetail(false)
			continue
		}
		queued, queueFull := e.enqueueEgress(dec.EgressSession, buffer)
		if !queued {
			// enqueueEgress does not Release on failure; caller owns payload.
			buffer.Release()
			e.dropPackets.Add(1)
			if queueFull {
				e.queueOverflow.Add(1)
			} else {
				e.dropNoSession.Add(1)
			}
			continue
		}
		e.forwardPackets.Add(1)
		// buffer ownership transferred to egressWorker (sing managed pool).
	}
}

// enqueueEgress queues payload for the egress session writer. On success, ownership moves to the worker.
// On failure, the caller must Release the buffer. Returns queueFull=true only when the egress queue rejected
// the datagram after an eviction attempt (real overflow).
func (e *Endpoint) enqueueEgress(session rt.SessionKey, payload *buf.Buffer) (queued bool, queueFull bool) {
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

	e.egressMu.RLock()
	queue, ok := e.egressQueues[session]
	e.egressMu.RUnlock()
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

	select {
	case queue <- payload:
		return true, false
	default:
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
			return false, true
		}
	}
}

func (e *Endpoint) egressWorker(session rt.SessionKey, queue <-chan *buf.Buffer) {
	defer e.egressWg.Done()
	var nextDeadlineExtend time.Time
	var cachedConn N.PacketConn
	var cachedDeadlineConn interface{ SetWriteDeadline(time.Time) error }
	var deadlineCapKnown bool
	for {
		payload, ok := <-queue
		if !ok {
			return
		}
		out := e.sessionConn(session)
		if out == nil {
			payload.Release()
			e.dropNoSession.Add(1)
			e.egressWriteFail.Add(1)
			cachedConn = nil
			cachedDeadlineConn = nil
			deadlineCapKnown = false
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
		// Pass-through *buf.Buffer avoids an extra full-datagram copy (previously append([]byte, pkt...)).
		_, werr := singbufio.WritePacketBuffer(out, payload, e.overlayDest)
		if werr != nil {
			if isTimeoutError(werr) {
				e.writeTimeout.Add(1)
			}
			e.egressWriteFail.Add(1)
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
