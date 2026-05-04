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

func wgTestPoolOptions() map[string]interface{} {
	return map[string]interface{}{
		"address": []interface{}{"10.5.0.1/24"},
	}
}

func TestWGTwoPass_NewClientAlphabeticallyFirst_DoesNotStealIP(t *testing.T) {
	opt := wgTestPoolOptions()
	used := wgCollectUsedPeerIPs(nil, opt)
	pool, ok := wgPeerPoolPrefixFromEndpointAddress(opt)
	if !ok {
		t.Fatal("expected pool")
	}
	// Order: alice (empty), bob .2, charlie .3 — same as collectWireGuardClientIdentities ASC by name.
	managed := []map[string]interface{}{
		{"client_name": "alice", "allowed_ips": []string{}},
		{"client_name": "bob", "allowed_ips": []string{"10.5.0.2/32"}},
		{"client_name": "charlie", "allowed_ips": []string{"10.5.0.3/32"}},
	}
	_ = wgReserveAssignedAllowedIPs(used, managed, true, pool)
	if _, err := wgAssignFreePeerIPs(used, managed, true, pool, wireGuardType); err != nil {
		t.Fatal(err)
	}
	if got := managed[0]["allowed_ips"].([]string)[0]; got != "10.5.0.4/32" {
		t.Fatalf("alice want 10.5.0.4/32 got %q", got)
	}
	if got := managed[1]["allowed_ips"].([]string)[0]; got != "10.5.0.2/32" {
		t.Fatalf("bob want .2 got %q", got)
	}
	if got := managed[2]["allowed_ips"].([]string)[0]; got != "10.5.0.3/32" {
		t.Fatalf("charlie want .3 got %q", got)
	}
}

func TestWGTwoPass_NewClientInMiddle_GetsNextFree(t *testing.T) {
	opt := wgTestPoolOptions()
	used := wgCollectUsedPeerIPs(nil, opt)
	pool, ok := wgPeerPoolPrefixFromEndpointAddress(opt)
	if !ok {
		t.Fatal("expected pool")
	}
	managed := []map[string]interface{}{
		{"client_name": "bob", "allowed_ips": []string{"10.5.0.2/32"}},
		{"client_name": "charlie", "allowed_ips": []string{"10.5.0.3/32"}},
		{"client_name": "dave", "allowed_ips": []string{}},
		{"client_name": "eve", "allowed_ips": []string{"10.5.0.5/32"}},
	}
	if wgReserveAssignedAllowedIPs(used, managed, true, pool) {
		t.Fatal("unexpected reserve change")
	}
	if _, err := wgAssignFreePeerIPs(used, managed, true, pool, wireGuardType); err != nil {
		t.Fatal(err)
	}
	if got := managed[2]["allowed_ips"].([]string)[0]; got != "10.5.0.4/32" {
		t.Fatalf("dave want 10.5.0.4/32 got %q", got)
	}
}

func TestWGTwoPass_TwoPeersSameIP_FirstInOrderWins(t *testing.T) {
	opt := wgTestPoolOptions()
	used := wgCollectUsedPeerIPs(nil, opt)
	pool, ok := wgPeerPoolPrefixFromEndpointAddress(opt)
	if !ok {
		t.Fatal("expected pool")
	}
	managed := []map[string]interface{}{
		{"client_name": "alice", "allowed_ips": []string{"10.5.0.2/32"}},
		{"client_name": "bob", "allowed_ips": []string{"10.5.0.2/32"}},
	}
	if !wgReserveAssignedAllowedIPs(used, managed, true, pool) {
		t.Fatal("expected reserve to clear duplicate")
	}
	if _, err := wgAssignFreePeerIPs(used, managed, true, pool, wireGuardType); err != nil {
		t.Fatal(err)
	}
	if got := managed[0]["allowed_ips"].([]string)[0]; got != "10.5.0.2/32" {
		t.Fatalf("alice keeps .2 got %q", got)
	}
	if got := managed[1]["allowed_ips"].([]string)[0]; got != "10.5.0.3/32" {
		t.Fatalf("bob reassigned to .3 got %q", got)
	}
}

func TestWGTwoPass_ManualPeerEmpty_DoesNotStealAssignedIP(t *testing.T) {
	opt := wgTestPoolOptions()
	used := wgCollectUsedPeerIPs(nil, opt)
	pool, ok := wgPeerPoolPrefixFromEndpointAddress(opt)
	if !ok {
		t.Fatal("expected pool")
	}
	// Stable order: first peer has .2, second has no IP — second must not take .2.
	manual := []map[string]interface{}{
		{"client_id": 0, "allowed_ips": []string{"10.5.0.2/32"}},
		{"client_id": 0, "allowed_ips": []string{}},
	}
	if wgReserveAssignedAllowedIPs(used, manual, true, pool) {
		t.Fatal("unexpected reserve change")
	}
	if _, err := wgAssignFreePeerIPs(used, manual, true, pool, wireGuardType); err != nil {
		t.Fatal(err)
	}
	if got := manual[0]["allowed_ips"].([]string)[0]; got != "10.5.0.2/32" {
		t.Fatalf("first manual keeps .2 got %q", got)
	}
	if got := manual[1]["allowed_ips"].([]string)[0]; got != "10.5.0.3/32" {
		t.Fatalf("second manual want .3 got %q", got)
	}
}

