package l3router

import (
	"net/netip"
	"sync"
	"testing"
)

func TestMemEngineConcurrentIngressAndControlPlane(t *testing.T) {
	engine := NewMemEngine()
	route := Route{
		ID:               1,
		Owner:            "owner-a",
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/24")},
	}
	engine.UpsertRoute(route)
	engine.SetIngressSession(route.ID, "owner-a")
	engine.SetEgressSession(route.ID, "owner-a-egress")
	packet := []byte{
		0x45, 0x00, 0x00, 0x14, 0x00, 0x00, 0x00, 0x00,
		0x40, 0x11, 0x00, 0x00,
		10, 0, 0, 2,
		10, 10, 0, 7,
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = engine.HandleIngress(packet, "owner-a")
			}
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				engine.UpsertRoute(route)
				engine.SetIngressSession(route.ID, "owner-a")
				engine.SetEgressSession(route.ID, "owner-a-egress")
				engine.RemoveRoute(route.ID)
				engine.UpsertRoute(route)
			}
		}()
	}
	wg.Wait()
}
