package l3router

import (
	"encoding/binary"
	"net/netip"
	"sync/atomic"
	"testing"
)

const benchmarkPacketSize = 1280

func BenchmarkMemEngineHandleIngress(b *testing.B) {
	engine := NewMemEngine()
	engine.UpsertRoute(Route{
		ID:               1,
		Owner:            "client-a",
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
	})
	engine.UpsertRoute(Route{
		ID:               2,
		Owner:            "client-b",
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.10.2.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.2.0/24")},
	})
	engine.SetIngressSession(1, SessionKey("client-a"))
	engine.SetEgressSession(1, SessionKey("client-a"))
	engine.SetIngressSession(2, SessionKey("client-b"))
	engine.SetEgressSession(2, SessionKey("client-b"))

	packet := makeBenchmarkIPv4UDPPacket(benchmarkPacketSize, 10, 10, 1, 2, 10, 10, 2, 2)

	b.ReportAllocs()
	b.SetBytes(int64(len(packet)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision := engine.HandleIngress(packet, SessionKey("client-a"))
		if decision.Action != ActionForward {
			b.Fatalf("unexpected decision: %+v", decision)
		}
	}
}

func BenchmarkMemEngineHandleIngressManyFlowsOneOwnerParallel(b *testing.B) {
	engine := NewMemEngine()
	engine.UpsertRoute(Route{
		ID:               1,
		Owner:            "client-a",
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
	})
	engine.UpsertRoute(Route{
		ID:               2,
		Owner:            "client-b",
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.10.2.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.2.0/24")},
	})
	engine.SetIngressSession(1, SessionKey("client-a"))
	engine.SetEgressSession(1, SessionKey("client-a"))
	engine.SetIngressSession(2, SessionKey("client-b"))
	engine.SetEgressSession(2, SessionKey("client-b"))

	flowPackets := make([][]byte, 64)
	for i := 0; i < len(flowPackets); i++ {
		pkt := makeBenchmarkIPv4UDPPacket(benchmarkPacketSize, 10, 10, 1, 2, 10, 10, 2, 2)
		// Rotate source host inside the same owner subnet to emulate many flows on one owner.
		pkt[15] = byte((i % 200) + 1)
		flowPackets[i] = pkt
	}

	var dropCount uint64
	var flowIndex uint64

	b.ReportAllocs()
	b.SetBytes(int64(len(flowPackets[0])))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&flowIndex, 1)
			pkt := flowPackets[idx%uint64(len(flowPackets))]
			decision := engine.HandleIngress(pkt, SessionKey("client-a"))
			if decision.Action != ActionForward || decision.EgressSession != SessionKey("client-b") {
				atomic.AddUint64(&dropCount, 1)
			}
		}
	})
	b.StopTimer()

	b.ReportMetric(float64(dropCount), "drops")
	b.ReportMetric(float64(dropCount)/float64(b.N), "drop/op")
}

func makeBenchmarkIPv4UDPPacket(totalLen int, srcA, srcB, srcC, srcD, dstA, dstB, dstC, dstD byte) []byte {
	if totalLen < 28 {
		totalLen = 28
	}
	pkt := make([]byte, totalLen)
	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)
	binary.BigEndian.PutUint16(pkt[6:8], 0x0000)
	pkt[8] = 0x40
	pkt[9] = 0x11 // UDP
	pkt[10] = 0x00
	pkt[11] = 0x00
	pkt[12], pkt[13], pkt[14], pkt[15] = srcA, srcB, srcC, srcD
	pkt[16], pkt[17], pkt[18], pkt[19] = dstA, dstB, dstC, dstD
	binary.BigEndian.PutUint16(pkt[20:22], 53)
	binary.BigEndian.PutUint16(pkt[22:24], 33333)
	binary.BigEndian.PutUint16(pkt[24:26], uint16(totalLen-20))
	binary.BigEndian.PutUint16(pkt[26:28], 0)
	for i := 28; i < len(pkt); i++ {
		pkt[i] = byte(i)
	}
	return pkt
}
