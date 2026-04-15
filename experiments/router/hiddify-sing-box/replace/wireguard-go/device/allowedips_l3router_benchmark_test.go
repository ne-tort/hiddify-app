package device

import (
	"net/netip"
	"sync/atomic"
	"testing"
)

// These benchmarks mirror l3router synthetic scenarios:
// - single deterministic forward lookup
// - many parallel lookups for many flows that map to one owner/peer

func BenchmarkAllowedIPsLookupSingleFlowLikeL3Router(b *testing.B) {
	var table AllowedIPs
	peerA := new(Peer)
	peerB := new(Peer)

	// Equivalent of two owners exporting /24 prefixes.
	table.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	table.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)

	dst := netip.MustParseAddr("10.10.2.2").AsSlice()

	b.ReportAllocs()
	b.SetBytes(20) // match l3router benchmark packet size convention
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := table.Lookup(dst)
		if got != peerB {
			b.Fatalf("unexpected peer: %p", got)
		}
	}
}

func BenchmarkAllowedIPsLookupManyFlowsOneOwnerParallelLikeL3Router(b *testing.B) {
	var table AllowedIPs
	peerA := new(Peer)
	peerB := new(Peer)

	table.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	table.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)

	// 64 destinations within one exported prefix: analogous to many flows
	// routed to one owner in l3router parallel benchmark.
	flows := make([][]byte, 64)
	for i := 0; i < len(flows); i++ {
		ip := netip.AddrFrom4([4]byte{10, 10, 2, byte((i % 200) + 1)})
		flows[i] = ip.AsSlice()
	}

	var miss uint64
	var idx uint64

	b.ReportAllocs()
	b.SetBytes(20)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := atomic.AddUint64(&idx, 1)
			got := table.Lookup(flows[i%uint64(len(flows))])
			if got != peerB {
				atomic.AddUint64(&miss, 1)
			}
		}
	})
	b.StopTimer()
	b.ReportMetric(float64(miss), "misses")
	b.ReportMetric(float64(miss)/float64(b.N), "miss/op")
}

