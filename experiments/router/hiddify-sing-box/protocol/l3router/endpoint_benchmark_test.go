package l3routerendpoint

import (
	"testing"

	rt "github.com/sagernet/sing-box/common/l3router"
	"github.com/sagernet/sing/common/buf"
	N "github.com/sagernet/sing/common/network"
)

func BenchmarkEndpointSessionConnParallel(b *testing.B) {
	e := &Endpoint{
		sessions: map[rt.SessionKey]N.PacketConn{
			"owner-a": nil,
		},
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = e.sessionConn("owner-a")
		}
	})
}

func BenchmarkEndpointEnqueueEgressQueueHitParallel(b *testing.B) {
	queue := make(chan *buf.Buffer, 1)
	e := &Endpoint{
		egressQueues:   map[rt.SessionKey]chan *buf.Buffer{"owner-a": queue},
		overflowPolicy: overflowPolicyDropOldest,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			payload := buf.As([]byte{0x45, 0x00, 0x00, 0x14})
			queued, _ := e.enqueueEgress("owner-a", payload)
			if !queued {
				payload.Release()
			}
		}
	})
	b.StopTimer()

	for {
		select {
		case stale := <-queue:
			if stale != nil {
				stale.Release()
			}
		default:
			return
		}
	}
}
