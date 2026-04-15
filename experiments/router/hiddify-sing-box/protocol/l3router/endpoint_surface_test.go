package l3routerendpoint

import (
	"context"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
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
