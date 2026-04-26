package service

import (
	"net/netip"
	"testing"
)

func TestWGValidateEndpointAddress_AcceptsHostAddress(t *testing.T) {
	if err := wgValidateEndpointAddress("10.0.0.1/24"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWGValidateEndpointAddress_RejectsNetworkAddress(t *testing.T) {
	if err := wgValidateEndpointAddress("10.0.0.0/24"); err == nil {
		t.Fatalf("expected validation error for network address")
	}
}

func TestWGCollectUsedPeerIPs_ReservesEndpointHostAddress(t *testing.T) {
	used := wgCollectUsedPeerIPs(nil, map[string]interface{}{
		"address": []interface{}{"10.8.0.1/24"},
	})
	if _, ok := used["10.8.0.1/32"]; !ok {
		t.Fatalf("expected server host ip reserved, used=%v", used)
	}
}

func TestWGPickLowestFreePeerIP_SkipsEndpointHostAddress(t *testing.T) {
	used := wgCollectUsedPeerIPs(nil, map[string]interface{}{
		"address": []interface{}{"10.8.0.1/24"},
	})
	pool, ok := wgPeerPoolPrefixFromEndpointAddress(map[string]interface{}{
		"address": []interface{}{"10.8.0.1/24"},
	})
	if !ok {
		t.Fatal("expected pool prefix")
	}
	got := wgPickLowestFreePeerIP(used, pool)
	if got != "10.8.0.2/32" {
		t.Fatalf("expected first peer .2/32, got %s", got)
	}
}

func TestValidateAndNormalizeWireGuardOptions_TrimsEmptyAddresses(t *testing.T) {
	options := map[string]interface{}{
		"address": []interface{}{"10.8.0.1/24", " ", ""},
	}
	if err := validateAndNormalizeWGFamilyOptions(options, wireGuardType); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	addr, ok := options["address"].([]string)
	if !ok {
		t.Fatalf("expected normalized []string address, got %#v", options["address"])
	}
	if len(addr) != 1 || addr[0] != "10.8.0.1/24" {
		t.Fatalf("unexpected normalized address: %#v", addr)
	}
}

func TestWGRebasePeerAllowedIPsToPool_RebasesHostPreservingOrder(t *testing.T) {
	pool, err := netip.ParsePrefix("10.6.0.1/24")
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	used := map[string]struct{}{"10.6.0.1/32": {}}
	in := []string{"10.5.0.2/32", "10.5.0.3/32"}
	out, changed := wgRebasePeerAllowedIPsToPool(in, pool, used)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(out) != 2 || out[0] != "10.6.0.2/32" || out[1] != "10.6.0.3/32" {
		t.Fatalf("unexpected rebased allowed_ips: %#v", out)
	}
}

func TestWGRebasePeerAllowedIPsToPool_ResolvesCollisions(t *testing.T) {
	pool, err := netip.ParsePrefix("10.6.0.1/24")
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	used := map[string]struct{}{
		"10.6.0.1/32": {},
		"10.6.0.2/32": {},
	}
	in := []string{"10.5.0.2/32"}
	out, changed := wgRebasePeerAllowedIPsToPool(in, pool, used)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(out) != 1 || out[0] != "10.6.0.3/32" {
		t.Fatalf("unexpected collision fallback result: %#v", out)
	}
}

