package l3routerendpoint

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	rt "github.com/sagernet/sing-box/experimental/l3router"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func newEndpointForSurfaceTest(t *testing.T) *Endpoint {
	t.Helper()
	loggerFactory := log.NewNOPFactory()
	ep, err := NewEndpoint(context.Background(), nil, loggerFactory.Logger(), "l3router-test", option.L3RouterEndpointOptions{})
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	return ep.(*Endpoint)
}

func TestEndpointReadinessLifecycle(t *testing.T) {
	ep := newEndpointForSurfaceTest(t)
	if ep.IsReady() {
		t.Fatal("endpoint should not be ready before Start(PostStart)")
	}
	if err := ep.Start(0); err != nil {
		t.Fatalf("Start(init): %v", err)
	}
	if ep.IsReady() {
		t.Fatal("endpoint should remain not-ready before PostStart")
	}
	if err := ep.Start(2); err != nil {
		t.Fatalf("Start(post-start): %v", err)
	}
	if !ep.IsReady() {
		t.Fatal("endpoint should become ready after PostStart")
	}
}

func TestEndpointOutboundSurfaceUnsupported(t *testing.T) {
	ep := newEndpointForSurfaceTest(t)
	if _, err := ep.DialContext(context.Background(), N.NetworkTCP, M.ParseSocksaddr("1.1.1.1:443")); err == nil {
		t.Fatal("DialContext must be unsupported")
	}
	if _, err := ep.ListenPacket(context.Background(), M.ParseSocksaddr("1.1.1.1:53")); err == nil {
		t.Fatal("ListenPacket must be unsupported")
	}
}

func TestEndpointInboundTCPRejected(t *testing.T) {
	ep := newEndpointForSurfaceTest(t)
	local, remote := net.Pipe()
	defer remote.Close()
	called := false
	ep.NewConnectionEx(context.Background(), local, adapter.InboundContext{}, func(err error) {
		called = true
	})
	if !called {
		t.Fatal("onClose must be called on TCP rejection")
	}
}

func TestEndpointDefaultACLDisabledFromOptions(t *testing.T) {
	loggerFactory := log.NewNOPFactory()
	epAny, err := NewEndpoint(context.Background(), nil, loggerFactory.Logger(), "acl-off", option.L3RouterEndpointOptions{
		Routes: []option.L3RouterRouteOptions{
			{
				ID:               1,
				Owner:            "owner-a",
				AllowedSrc:       []string{"10.0.0.0/24"},
				ExportedPrefixes: []string{"10.0.0.0/24"},
			},
			{
				ID:               2,
				Owner:            "owner-b",
				AllowedSrc:       []string{"10.0.1.0/24"},
				ExportedPrefixes: []string{"10.0.1.0/24"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	ep := epAny.(*Endpoint)
	ep.enterSession("owner-a")
	ep.enterSession("owner-b")
	t.Cleanup(func() {
		ep.leaveSession("owner-b")
		ep.leaveSession("owner-a")
		_ = ep.Close()
	})

	pkt := makeIPv4([4]byte{192, 168, 1, 1}, [4]byte{10, 0, 1, 2})
	d := ep.engine.HandleIngress(pkt, rt.SessionKey("owner-a"))
	if d.Action != rt.ActionForward || d.EgressSession != rt.SessionKey("owner-b") {
		t.Fatalf("expected forward with default acl_enabled=false, got %+v", d)
	}
}

func TestEndpointACLEnabledOptionEnforcesPolicy(t *testing.T) {
	loggerFactory := log.NewNOPFactory()
	epAny, err := NewEndpoint(context.Background(), nil, loggerFactory.Logger(), "acl-on", option.L3RouterEndpointOptions{
		ACLEnabled: true,
		Routes: []option.L3RouterRouteOptions{
			{
				ID:               1,
				Owner:            "owner-a",
				AllowedSrc:       []string{"10.0.0.0/24"},
				AllowedDst:       []string{"10.0.1.0/24"},
				ExportedPrefixes: []string{"10.0.0.0/24"},
			},
			{
				ID:               2,
				Owner:            "owner-b",
				ExportedPrefixes: []string{"10.0.1.0/24"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	ep := epAny.(*Endpoint)
	ep.enterSession("owner-a")
	ep.enterSession("owner-b")
	t.Cleanup(func() {
		ep.leaveSession("owner-b")
		ep.leaveSession("owner-a")
		_ = ep.Close()
	})

	badSrc := makeIPv4([4]byte{192, 168, 1, 1}, [4]byte{10, 0, 1, 2})
	if d := ep.engine.HandleIngress(badSrc, rt.SessionKey("owner-a")); d.Action != rt.ActionDrop || d.DropReason != rt.DropACLSource {
		t.Fatalf("expected source ACL drop, got %+v", d)
	}

	badDst := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 9, 9})
	if d := ep.engine.HandleIngress(badDst, rt.SessionKey("owner-a")); d.Action != rt.ActionDrop || d.DropReason != rt.DropACLDestination {
		t.Fatalf("expected destination ACL drop, got %+v", d)
	}

	// Empty Allowed* on route 2 means allow-all even when ACL is enabled.
	r2 := rt.Route{
		ID:               2,
		Owner:            "owner-b",
		AllowedSrc:       nil,
		AllowedDst:       nil,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	}
	if err := ep.UpsertRoute(r2); err != nil {
		t.Fatalf("upsert route2: %v", err)
	}
	good := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 1, 2})
	if d := ep.engine.HandleIngress(good, rt.SessionKey("owner-a")); d.Action != rt.ActionForward {
		t.Fatalf("expected forward with empty Allowed* treated as allow-all, got %+v", d)
	}
}
