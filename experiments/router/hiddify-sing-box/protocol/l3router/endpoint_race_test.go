package l3routerendpoint

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	rt "github.com/sagernet/sing-box/experimental/l3router"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
)

func TestEndpointConcurrentRouteAndSessionChurn(t *testing.T) {
	loggerFactory := log.NewNOPFactory()
	endpointAny, err := NewEndpoint(context.Background(), nil, loggerFactory.Logger(), "race-l3", option.L3RouterEndpointOptions{})
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	e := endpointAny.(*Endpoint)
	route := rt.Route{
		ID:               9,
		Owner:            "owner-a",
		AllowedSrc:       mustPrefixes(t, "10.0.0.0/24"),
		ExportedPrefixes: mustPrefixes(t, "10.9.0.0/24"),
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = e.UpsertRoute(route)
				e.RemoveRoute(route.ID)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				e.enterSession("owner-a")
				e.leaveSession("owner-a")
			}
		}()
	}
	wg.Wait()
}

func TestEgressQueueCountsOverflow(t *testing.T) {
	loggerFactory := log.NewNOPFactory()
	endpointAny, err := NewEndpoint(context.Background(), nil, loggerFactory.Logger(), "queue", option.L3RouterEndpointOptions{})
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	e := endpointAny.(*Endpoint)
	for i := 0; i < 400; i++ {
		_ = e.enqueueEgress("missing-session", buf.As([]byte{0x45, 0x00, 0x00, 0x14}))
	}
	time.Sleep(20 * time.Millisecond)
	if e.queueOverflow.Load() == 0 && e.egressWriteFail.Load() == 0 {
		t.Fatalf("expected queue or write failure counters to increment")
	}
}

func mustPrefixes(t *testing.T, values ...string) []netip.Prefix {
	t.Helper()
	result, err := ParsePrefixes(values)
	if err != nil {
		t.Fatalf("ParsePrefixes: %v", err)
	}
	return result
}
