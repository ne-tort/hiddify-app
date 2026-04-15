package l3router

import (
	"net/netip"
	"testing"
)

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

	packet := []byte{
		0x45, 0x00, 0x00, 0x20, 0x12, 0x34, 0x00, 0x00,
		0x40, 0x11, 0x00, 0x00, 10, 10, 1, 2, 10, 10, 2, 2,
		0x00, 0x35, 0x82, 0x35, 0x00, 0x0c, 0x00, 0x00,
		0xde, 0xad, 0xbe, 0xef,
	}

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
