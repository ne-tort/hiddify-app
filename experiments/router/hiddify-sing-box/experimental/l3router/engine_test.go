package l3router

import (
	"net/netip"
	"sort"
	"testing"
)

func TestMemEngineForward(t *testing.T) {
	e := NewMemEngine()
	const (
		rA RouteID = 1
		rB RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rB,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	})
	const sessA SessionKey = "user-a"
	const sessB SessionKey = "user-b"
	e.SetIngressSession(rA, sessA)
	e.SetIngressSession(rB, sessB)
	e.SetEgressSession(rA, sessA)
	e.SetEgressSession(rB, sessB)

	// IPv4: src 10.0.0.2 -> dst 10.0.1.2 (A sends to B's subnet)
	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 1, 2})
	d := e.HandleIngress(pkt, sessA)
	if d.Action != ActionForward || d.EgressSession != sessB {
		t.Fatalf("forward: got %+v", d)
	}
}

func TestMemEngineDropUnknownIngress(t *testing.T) {
	e := NewMemEngine()
	e.UpsertRoute(Route{
		ID:               1,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.SetEgressSession(1, "u1")
	pkt := makeIPv4([4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2})
	if d := e.HandleIngress(pkt, "nobody"); d.Action != ActionDrop {
		t.Fatal(d)
	}
}

func TestMemEngineDropBadSrc(t *testing.T) {
	e := NewMemEngine()
	const r RouteID = 1
	e.UpsertRoute(Route{
		ID:               r,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.SetIngressSession(r, "u1")
	e.SetEgressSession(r, "u1")
	pkt := makeIPv4([4]byte{192, 168, 1, 1}, [4]byte{10, 0, 0, 2})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatal(d)
	}
}

func TestMemEngineDropSelfForwardLoop(t *testing.T) {
	e := NewMemEngine()
	const r RouteID = 1
	e.UpsertRoute(Route{
		ID:               r,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.SetIngressSession(r, "u1")
	e.SetEgressSession(r, "u1")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 0, 3})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop for self-forward loop, got %+v", d)
	}
}

func TestMemEnginePreferNonLoopEgressOnSamePrefix(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rAlt     RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rIngress, "u1")
	e.SetEgressSession(rAlt, "u2")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 10, 1, 2})
	d := e.HandleIngress(pkt, "u1")
	if d.Action != ActionForward || d.EgressSession != "u2" {
		t.Fatalf("expected forwarding to non-loop egress session, got %+v", d)
	}
}

func TestMemEnginePreferShorterNonLoopOverLongerLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rAlt     RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rIngress, "u1")
	e.SetEgressSession(rAlt, "u2")

	// Longest prefix points to self-loop, so engine must pick shorter non-loop route.
	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 10, 1, 20})
	d := e.HandleIngress(pkt, "u1")
	if d.Action != ActionForward || d.EgressSession != "u2" {
		t.Fatalf("expected fallback to shorter non-loop route, got %+v", d)
	}
}

func TestMemEngineEqualPrefixLenUsesStableRouteID(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rHi      RouteID = 100
		rLo      RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	// Same prefix length and address space for both routes; route id tie-break must be stable.
	e.UpsertRoute(Route{
		ID:               rHi,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
	})
	e.UpsertRoute(Route{
		ID:               rLo,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rHi, "hi")
	e.SetEgressSession(rLo, "lo")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 20, 1, 2})
	for i := 0; i < 32; i++ {
		d := e.HandleIngress(pkt, "u1")
		if d.Action != ActionForward || d.EgressSession != "lo" {
			t.Fatalf("iteration %d: expected stable route selection to lower route id, got %+v", i, d)
		}
	}
}

func TestMemEngineRouteChurnPreservesNonLoopForwarding(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoopHi  RouteID = 2
		rGood    RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoopHi,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.1.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rGood,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.SetIngressSession(rIngress, "ingress")
	e.SetEgressSession(rLoopHi, "ingress") // intentionally looping candidate
	e.SetEgressSession(rGood, "egress-good")

	pkt := makeIPv4([4]byte{10, 0, 0, 10}, [4]byte{10, 10, 1, 55})
	if d := e.HandleIngress(pkt, "ingress"); d.Action != ActionForward || d.EgressSession != "egress-good" {
		t.Fatalf("must fallback to non-loop route, got %+v", d)
	}

	e.RemoveRoute(rGood)
	if d := e.HandleIngress(pkt, "ingress"); d.Action != ActionDrop {
		t.Fatalf("must drop when only loop route remains, got %+v", d)
	}

	e.UpsertRoute(Route{
		ID:               rGood,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.SetEgressSession(rGood, "egress-good-2")
	if d := e.HandleIngress(pkt, "ingress"); d.Action != ActionForward || d.EgressSession != "egress-good-2" {
		t.Fatalf("must recover forwarding after route re-add, got %+v", d)
	}
}

func TestMemEngineIPv6LoopCandidateDoesNotWinLPM(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoopV6  RouteID = 2
		rAltV6   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/64")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/64")},
	})
	e.UpsertRoute(Route{
		ID:               rLoopV6,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:2::/64")},
	})
	e.UpsertRoute(Route{
		ID:               rAltV6,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")},
	})
	e.SetIngressSession(rIngress, "ingress-v6")
	e.SetEgressSession(rLoopV6, "ingress-v6") // longest match but loop
	e.SetEgressSession(rAltV6, "egress-v6")

	pkt := makeIPv6(
		netip.MustParseAddr("2001:db8:1::10"),
		netip.MustParseAddr("2001:db8:2::20"),
	)
	d := e.HandleIngress(pkt, "ingress-v6")
	if d.Action != ActionForward || d.EgressSession != "egress-v6" {
		t.Fatalf("must prefer non-loop IPv6 route, got %+v", d)
	}
}

func TestMemEngineRouteChurnWithOverlappingPrefixesKeepsNonLoopChoice(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoop    RouteID = 2
		rA       RouteID = 3
		rB       RouteID = 4
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.30.1.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.30.0.0/16")},
	})
	e.UpsertRoute(Route{
		ID:               rB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.30.1.128/25")},
	})
	e.SetIngressSession(rIngress, "ing")
	e.SetEgressSession(rLoop, "ing") // loop candidate with longer prefix than /16.
	e.SetEgressSession(rA, "eg-a")
	e.SetEgressSession(rB, "eg-b")

	pkt := makeIPv4([4]byte{10, 0, 0, 20}, [4]byte{10, 30, 1, 140})
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg-b" {
		t.Fatalf("expected best non-loop /25 route, got %+v", d)
	}

	e.RemoveRoute(rB)
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg-a" {
		t.Fatalf("expected fallback to /16 non-loop route, got %+v", d)
	}

	e.SetEgressSession(rA, "ing") // force only loop candidates
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionDrop {
		t.Fatalf("expected drop when all candidates loop, got %+v", d)
	}

	e.SetEgressSession(rA, "eg-a-2")
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg-a-2" {
		t.Fatalf("expected forwarding recovery after non-loop session restore, got %+v", d)
	}
}

func TestMemEngineClearIngressSessionBlocksReusedSourcePrefix(t *testing.T) {
	e := NewMemEngine()
	const (
		rA RouteID = 1
		rB RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rB,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	})
	e.SetIngressSession(rA, "sess-a")
	e.SetEgressSession(rB, "sess-b")

	pktFromA := makeIPv4([4]byte{10, 0, 0, 9}, [4]byte{10, 0, 1, 9})
	if d := e.HandleIngress(pktFromA, "sess-a"); d.Action != ActionForward || d.EgressSession != "sess-b" {
		t.Fatalf("expected forward before ingress clear, got %+v", d)
	}

	e.ClearIngressSession("sess-a")
	if d := e.HandleIngress(pktFromA, "sess-a"); d.Action != ActionDrop {
		t.Fatalf("expected drop after ingress clear, got %+v", d)
	}

	// Re-binding another session with same source subnet must not resurrect old session.
	e.SetIngressSession(rA, "sess-a-new")
	if d := e.HandleIngress(pktFromA, "sess-a"); d.Action != ActionDrop {
		t.Fatalf("stale session should remain dropped, got %+v", d)
	}
	if d := e.HandleIngress(pktFromA, "sess-a-new"); d.Action != ActionForward || d.EgressSession != "sess-b" {
		t.Fatalf("new session should forward, got %+v", d)
	}
}

func TestMemEngineLPMPreference(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rWide    RouteID = 2
		rNarrow  RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rWide,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	e.UpsertRoute(Route{
		ID:               rNarrow,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.2.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	})
	e.SetIngressSession(rIngress, "ingress")
	e.SetEgressSession(rWide, "wide")
	e.SetEgressSession(rNarrow, "narrow")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 10, 1, 2})
	d := e.HandleIngress(pkt, "ingress")
	if d.Action != ActionForward || d.EgressSession != "narrow" {
		t.Fatalf("expected narrow LPM route, got %+v", d)
	}
}

func TestMemEngineDropByAllowedDst(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rEgress  RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.0.2.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rEgress,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rEgress, "u2")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 1, 2})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop by AllowedDst, got %+v", d)
	}
}

func TestMemEngineIPv6Forward(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rEgress  RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/64")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("2001:db8:2::/64")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/64")},
	})
	e.UpsertRoute(Route{
		ID:               rEgress,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:2::/64")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rEgress, "u2")

	pkt := makeIPv6(
		netip.MustParseAddr("2001:db8:1::10"),
		netip.MustParseAddr("2001:db8:2::20"),
	)
	d := e.HandleIngress(pkt, "u1")
	if d.Action != ActionForward || d.EgressSession != "u2" {
		t.Fatalf("expected IPv6 forward, got %+v", d)
	}
}

func TestMemEngineDropWhenEgressSessionMissing(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rEgress  RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rEgress,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	})
	e.SetIngressSession(rIngress, "u1")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 1, 2})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop when egress session missing, got %+v", d)
	}
}

func TestMemEngineClearSessionsStopsForwarding(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rEgress  RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rEgress,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")},
	})
	e.SetIngressSession(rIngress, "u1")
	e.SetEgressSession(rEgress, "u2")

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 1, 2})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionForward {
		t.Fatalf("expected initial forward, got %+v", d)
	}

	e.ClearIngressSession("u1")
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop after ingress clear, got %+v", d)
	}

	e.SetIngressSession(rIngress, "u1")
	e.ClearEgressSession(rEgress)
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop after egress clear, got %+v", d)
	}
}

func TestMemEngineRemoveRouteClearsIngressBinding(t *testing.T) {
	e := NewMemEngine()
	const r RouteID = 1
	e.UpsertRoute(Route{
		ID:               r,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.SetIngressSession(r, "u1")
	e.SetEgressSession(r, "u1")
	e.RemoveRoute(r)

	pkt := makeIPv4([4]byte{10, 0, 0, 2}, [4]byte{10, 0, 0, 3})
	if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
		t.Fatalf("expected drop after route removal, got %+v", d)
	}
}

func TestMemEngineDropMalformedPackets(t *testing.T) {
	e := NewMemEngine()
	e.UpsertRoute(Route{
		ID:               1,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.SetIngressSession(1, "u1")
	e.SetEgressSession(1, "u1")

	cases := [][]byte{
		nil,
		{},
		{0x40},           // ipv4 marker but too short
		make([]byte, 19), // short ipv4 packet
		append([]byte{0x60}, make([]byte, 38)...), // short ipv6 packet
		{0x10, 0x00, 0x00, 0x00},                  // unknown ip version
	}
	for i, pkt := range cases {
		if d := e.HandleIngress(pkt, "u1"); d.Action != ActionDrop {
			t.Fatalf("case %d expected drop, got %+v", i, d)
		}
	}
}

func TestMemEngineEqualPrefixLoopRouteRemovalKeepsForwarding(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoop    RouteID = 2
		rAlt     RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.50.0.0/16")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.50.0.0/16")},
	})
	e.SetIngressSession(rIngress, "ing")
	e.SetEgressSession(rLoop, "ing")
	e.SetEgressSession(rAlt, "eg")

	pkt := makeIPv4([4]byte{10, 0, 0, 10}, [4]byte{10, 50, 20, 30})
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg" {
		t.Fatalf("must choose non-loop equal-prefix route, got %+v", d)
	}

	e.RemoveRoute(rLoop)
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg" {
		t.Fatalf("must keep forwarding after loop route removal, got %+v", d)
	}
}

func TestMemEngineRouteUpdateCanTurnForwardIntoDropWithoutLoopFallback(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rDst     RouteID = 2
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.60.0.0/16")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDst,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.60.0.0/16")},
	})
	e.SetIngressSession(rIngress, "ing")
	e.SetEgressSession(rDst, "eg")

	pkt := makeIPv4([4]byte{10, 0, 0, 11}, [4]byte{10, 60, 1, 2})
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionForward || d.EgressSession != "eg" {
		t.Fatalf("expected initial forward, got %+v", d)
	}

	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.61.0.0/16")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionDrop {
		t.Fatalf("must drop after allowed destination update, got %+v", d)
	}
}

func TestMemEngineLoopCandidateRouteChurnKeepsStableNonLoopForwarding(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoop    RouteID = 2
		rStable  RouteID = 9
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rStable,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.70.0.0/16")},
	})
	e.SetIngressSession(rIngress, "ing")
	e.SetEgressSession(rStable, "eg-stable")

	pkt := makeIPv4([4]byte{10, 0, 0, 42}, [4]byte{10, 70, 1, 42})
	for i := 0; i < 16; i++ {
		if i%2 == 0 {
			e.UpsertRoute(Route{
				ID:               rLoop,
				ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.70.1.0/24")},
			})
			e.SetEgressSession(rLoop, "ing")
		} else {
			e.RemoveRoute(rLoop)
		}
		d := e.HandleIngress(pkt, "ing")
		if d.Action != ActionForward || d.EgressSession != "eg-stable" {
			t.Fatalf("iteration %d: expected stable non-loop forward, got %+v", i, d)
		}
	}

	e.SetEgressSession(rStable, "ing")
	if d := e.HandleIngress(pkt, "ing"); d.Action != ActionDrop {
		t.Fatalf("expected drop when only loop candidates remain, got %+v", d)
	}
}

func TestMemEngineOwnerChainStyleRouteRebindAvoidsLoopAcrossSamePrefix(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rPeerB   RouteID = 2
		rPeerC   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rPeerB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rPeerC,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.9.0/24")},
	})
	e.SetIngressSession(rIngress, "owner-a")
	e.SetEgressSession(rPeerB, "owner-b")
	e.SetEgressSession(rPeerC, "owner-c")

	pkt := makeIPv4([4]byte{10, 0, 0, 8}, [4]byte{10, 0, 9, 8})
	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			e.SetEgressSession(rPeerB, "owner-a") // loop candidate during churn.
			e.SetEgressSession(rPeerC, "owner-c")
		} else {
			e.SetEgressSession(rPeerB, "owner-b")
			e.SetEgressSession(rPeerC, "owner-a") // opposite loop candidate.
		}
		d := e.HandleIngress(pkt, "owner-a")
		if d.Action != ActionForward {
			t.Fatalf("iteration %d: expected forward during churn, got %+v", i, d)
		}
		if d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: loopback egress selected: %+v", i, d)
		}
	}
}

func TestMemEngineDropWhenPrefixChurnLeavesOnlyLoopCandidate(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1
		rLoop    RouteID = 2
		rWide    RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.88.1.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rWide,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16")},
	})
	e.SetIngressSession(rIngress, "sess-a")
	e.SetEgressSession(rLoop, "sess-a")
	e.SetEgressSession(rWide, "sess-z")

	pkt := makeIPv4([4]byte{10, 0, 0, 42}, [4]byte{10, 88, 1, 10})
	if d := e.HandleIngress(pkt, "sess-a"); d.Action != ActionForward || d.EgressSession != "sess-z" {
		t.Fatalf("expected non-loop fallback via wide prefix, got %+v", d)
	}

	// Churn removes the only non-loop alternative for the same destination space.
	e.RemoveRoute(rWide)
	if d := e.HandleIngress(pkt, "sess-a"); d.Action != ActionDrop {
		t.Fatalf("expected drop when only loop route remains, got %+v", d)
	}

	// Re-add with a non-loop session to prove recovery without stale loop state.
	e.UpsertRoute(Route{
		ID:               rWide,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16")},
	})
	e.SetEgressSession(rWide, "sess-z-new")
	if d := e.HandleIngress(pkt, "sess-a"); d.Action != ActionForward || d.EgressSession != "sess-z-new" {
		t.Fatalf("expected forward recovery after non-loop rebind, got %+v", d)
	}
}

func TestMemEngineOwnerShiftAtoBtoCWithCompetingPrefixNeverLoopsOrFlaps(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rDst   RouteID = 2
		rPeer  RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.1.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDst,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rPeer,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/24")},
	})

	pktA := makeIPv4([4]byte{10, 1, 0, 10}, [4]byte{10, 9, 0, 10})
	pktB := makeIPv4([4]byte{10, 1, 1, 10}, [4]byte{10, 9, 0, 10})
	pktC := makeIPv4([4]byte{10, 1, 2, 10}, [4]byte{10, 9, 0, 10})

	// A phase.
	e.SetIngressSession(rOwner, "owner-a")
	e.SetEgressSession(rDst, "dst-z")
	e.SetEgressSession(rPeer, "owner-a") // competing loop candidate
	if d := e.HandleIngress(pktA, "owner-a"); d.Action != ActionForward || d.EgressSession != "dst-z" {
		t.Fatalf("A phase must forward to non-loop egress, got %+v", d)
	}

	// B phase.
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.1.1.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/24")},
	})
	e.SetIngressSession(rOwner, "owner-b")
	e.SetEgressSession(rDst, "owner-b") // turn route 2 into loop candidate for B
	e.SetEgressSession(rPeer, "dst-z")
	if d := e.HandleIngress(pktA, "owner-a"); d.Action != ActionDrop {
		t.Fatalf("stale owner-a must drop after A->B, got %+v", d)
	}
	if d := e.HandleIngress(pktB, "owner-b"); d.Action != ActionForward || d.EgressSession != "dst-z" {
		t.Fatalf("B phase must avoid loop and forward to dst-z, got %+v", d)
	}

	// C phase.
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.1.2.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.0/24")},
	})
	e.SetIngressSession(rOwner, "owner-c")
	e.SetEgressSession(rDst, "dst-z-2")
	e.SetEgressSession(rPeer, "owner-c") // loop candidate for C
	if d := e.HandleIngress(pktB, "owner-b"); d.Action != ActionDrop {
		t.Fatalf("stale owner-b must drop after B->C, got %+v", d)
	}
	if d := e.HandleIngress(pktC, "owner-c"); d.Action != ActionForward || d.EgressSession != "dst-z-2" {
		t.Fatalf("C phase must forward via non-loop egress, got %+v", d)
	}
}

func TestMemEngineRapidOwnerFlipWithRouteChurnKeepsNonLoopForwarding(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rDstA  RouteID = 2
		rDstB  RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.3.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.3.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.3.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.3.9.0/24")},
	})
	e.SetIngressSession(rOwner, "owner-a")
	e.SetEgressSession(rDstA, "egress-z")
	e.SetEgressSession(rDstB, "owner-a") // start with one loop candidate.

	pktA := makeIPv4([4]byte{10, 3, 0, 10}, [4]byte{10, 3, 9, 10})
	pktB := makeIPv4([4]byte{10, 3, 1, 10}, [4]byte{10, 3, 9, 10})
	pktC := makeIPv4([4]byte{10, 3, 2, 10}, [4]byte{10, 3, 9, 10})
	owners := []struct {
		srcSubnet netip.Prefix
		session   SessionKey
		packet    []byte
	}{
		{netip.MustParsePrefix("10.3.0.0/24"), "owner-a", pktA},
		{netip.MustParsePrefix("10.3.1.0/24"), "owner-b", pktB},
		{netip.MustParsePrefix("10.3.2.0/24"), "owner-c", pktC},
	}
	for i := 0; i < len(owners); i++ {
		cur := owners[i]
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{cur.srcSubnet},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.3.0.0/24")},
		})
		e.SetIngressSession(rOwner, cur.session)
		// Flip loop candidate and churn competing destination route each step.
		if i%2 == 0 {
			e.RemoveRoute(rDstB)
			e.UpsertRoute(Route{
				ID:               rDstB,
				ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.3.9.0/24")},
			})
			e.SetEgressSession(rDstA, "egress-z")
			e.SetEgressSession(rDstB, cur.session)
		} else {
			e.SetEgressSession(rDstA, cur.session)
			e.SetEgressSession(rDstB, "egress-z")
		}

		d := e.HandleIngress(cur.packet, cur.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected non-loop forward to egress-z, got %+v", i, d)
		}

		if i > 0 {
			prev := owners[i-1]
			if dPrev := e.HandleIngress(prev.packet, prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerAtoBtoCWithCompetingChurnNeverSelectsIngressSession(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rMain  RouteID = 2
		rAlt   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.5.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.5.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rMain,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.5.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.5.9.0/24")},
	})
	e.SetEgressSession(rMain, "egress-z")
	e.SetEgressSession(rAlt, "egress-y")

	stages := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.5.0.0/24"), "owner-a", [4]byte{10, 5, 0, 10}},
		{netip.MustParsePrefix("10.5.1.0/24"), "owner-b", [4]byte{10, 5, 1, 10}},
		{netip.MustParsePrefix("10.5.2.0/24"), "owner-c", [4]byte{10, 5, 2, 10}},
	}
	dstIP := [4]byte{10, 5, 9, 10}

	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{stage.srcPrefix},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.5.9.0/24")},
		})
		e.SetIngressSession(rOwner, stage.session)

		// Toggle loop candidate between competing routes while preserving non-loop alternative.
		if i%2 == 0 {
			e.SetEgressSession(rMain, stage.session)
			e.SetEgressSession(rAlt, "egress-y")
		} else {
			e.SetEgressSession(rMain, "egress-z")
			e.SetEgressSession(rAlt, stage.session)
		}

		curPkt := makeIPv4(stage.srcIP, dstIP)
		d := e.HandleIngress(curPkt, stage.session)
		if d.Action != ActionForward {
			t.Fatalf("stage %d: expected forward, got %+v", i, d)
		}
		if d.EgressSession == stage.session {
			t.Fatalf("stage %d: self-egress detected: %+v", i, d)
		}

		if i > 0 {
			prev := stages[i-1]
			prevPkt := makeIPv4(prev.srcIP, dstIP)
			if dPrev := e.HandleIngress(prevPkt, prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerAtoBtoCWithDestinationRouteRemoveReAddKeepsNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rDstA  RouteID = 2
		rDstB  RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.7.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
	})

	stages := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.7.0.0/24"), "owner-a", [4]byte{10, 7, 0, 8}},
		{netip.MustParsePrefix("10.7.1.0/24"), "owner-b", [4]byte{10, 7, 1, 8}},
		{netip.MustParsePrefix("10.7.2.0/24"), "owner-c", [4]byte{10, 7, 2, 8}},
	}
	dst := [4]byte{10, 7, 9, 80}

	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{stage.srcPrefix},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
		})
		e.SetIngressSession(rOwner, stage.session)

		// Each stage removes and re-adds one competing destination route.
		if i%2 == 0 {
			e.RemoveRoute(rDstA)
			e.UpsertRoute(Route{
				ID:               rDstA,
				ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
			})
			e.SetEgressSession(rDstA, "egress-z")
			e.SetEgressSession(rDstB, stage.session) // force loop candidate on rDstB
		} else {
			e.RemoveRoute(rDstB)
			e.UpsertRoute(Route{
				ID:               rDstB,
				ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.7.9.0/24")},
			})
			e.SetEgressSession(rDstA, stage.session) // force loop candidate on rDstA
			e.SetEgressSession(rDstB, "egress-z")
		}

		curPkt := makeIPv4(stage.srcIP, dst)
		d := e.HandleIngress(curPkt, stage.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("stage %d: expected non-loop forward to egress-z, got %+v", i, d)
		}

		if i > 0 {
			prev := stages[i-1]
			prevPkt := makeIPv4(prev.srcIP, dst)
			if dPrev := e.HandleIngress(prevPkt, prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineIPv6OwnerAtoBtoCWithCompetingPrefixNeverLoops(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rMain  RouteID = 2
		rAlt   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("2001:db8:10:1::/64")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:10:9::/64")},
	})
	e.UpsertRoute(Route{
		ID:               rMain,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:10:9::/64")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:10:9::/64")},
	})
	e.SetEgressSession(rMain, "eg-main")
	e.SetEgressSession(rAlt, "eg-alt")

	stages := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     netip.Addr
	}{
		{netip.MustParsePrefix("2001:db8:10:1::/64"), "owner-a", netip.MustParseAddr("2001:db8:10:1::10")},
		{netip.MustParsePrefix("2001:db8:10:2::/64"), "owner-b", netip.MustParseAddr("2001:db8:10:2::10")},
		{netip.MustParsePrefix("2001:db8:10:3::/64"), "owner-c", netip.MustParseAddr("2001:db8:10:3::10")},
	}
	dstIP := netip.MustParseAddr("2001:db8:10:9::10")

	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{stage.srcPrefix},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8:10:9::/64")},
		})
		e.SetIngressSession(rOwner, stage.session)

		// Alternate which same-prefix route is loop candidate for current owner.
		if i%2 == 0 {
			e.SetEgressSession(rMain, stage.session)
			e.SetEgressSession(rAlt, "eg-alt")
		} else {
			e.SetEgressSession(rMain, "eg-main")
			e.SetEgressSession(rAlt, stage.session)
		}

		pkt := makeIPv6(stage.srcIP, dstIP)
		d := e.HandleIngress(pkt, stage.session)
		if d.Action != ActionForward {
			t.Fatalf("stage %d: expected forward, got %+v", i, d)
		}
		if d.EgressSession == stage.session {
			t.Fatalf("stage %d: unexpected self-loop egress: %+v", i, d)
		}

		if i > 0 {
			prev := stages[i-1]
			prevPkt := makeIPv6(prev.srcIP, dstIP)
			if dPrev := e.HandleIngress(prevPkt, prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerAtoBtoCWithAuthFailSymptomGuardNeverChoosesIngressAsEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rMain  RouteID = 2
		rAlt   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.11.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.11.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rMain,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.11.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.11.9.0/24")},
	})

	owners := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.11.0.0/24"), "owner-a", [4]byte{10, 11, 0, 21}},
		{netip.MustParsePrefix("10.11.1.0/24"), "owner-b", [4]byte{10, 11, 1, 21}},
		{netip.MustParsePrefix("10.11.2.0/24"), "owner-c", [4]byte{10, 11, 2, 21}},
	}
	dst := [4]byte{10, 11, 9, 21}

	for i, owner := range owners {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{owner.srcPrefix},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.11.9.0/24")},
		})
		e.SetIngressSession(rOwner, owner.session)

		// Alternate loop candidate between equal-prefix routes while preserving one non-loop route.
		if i%2 == 0 {
			e.SetEgressSession(rMain, owner.session)
			e.SetEgressSession(rAlt, "egress-z")
		} else {
			e.SetEgressSession(rMain, "egress-z")
			e.SetEgressSession(rAlt, owner.session)
		}

		curPkt := makeIPv4(owner.srcIP, dst)
		d := e.HandleIngress(curPkt, owner.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("stage %d: expected non-loop forward to egress-z, got %+v", i, d)
		}

		if i > 0 {
			prev := owners[i-1]
			prevPkt := makeIPv4(prev.srcIP, dst)
			if dPrev := e.HandleIngress(prevPkt, prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerAtoBtoCStepwiseLoopOnlyDropAndRecover(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rDst   RouteID = 2
		rPeer  RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.13.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.13.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDst,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.13.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rPeer,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.13.9.0/24")},
	})

	steps := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.13.0.0/24"), "owner-a", [4]byte{10, 13, 0, 10}},
		{netip.MustParsePrefix("10.13.1.0/24"), "owner-b", [4]byte{10, 13, 1, 10}},
		{netip.MustParsePrefix("10.13.2.0/24"), "owner-c", [4]byte{10, 13, 2, 10}},
	}
	dstIP := [4]byte{10, 13, 9, 10}

	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcPrefix},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.13.9.0/24")},
		})
		e.SetIngressSession(rOwner, step.session)

		// Simulate critical window: only self-loop candidates available -> must drop.
		e.SetEgressSession(rDst, step.session)
		e.SetEgressSession(rPeer, step.session)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstIP), step.session); d.Action != ActionDrop {
			t.Fatalf("step %d: loop-only window must drop, got %+v", i, d)
		}

		// Recover with one non-loop egress and verify forwarding stability.
		e.SetEgressSession(rPeer, "egress-z")
		d := e.HandleIngress(makeIPv4(step.srcIP, dstIP), step.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected recovery forward to egress-z, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dstIP), prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineAllowedDstOwnerChurnWithCompetingPrefixNeverLoops(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1
		rMain  RouteID = 2
		rAlt   RouteID = 3
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.15.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rMain,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
	})
	e.SetEgressSession(rMain, "eg-z")
	e.SetEgressSession(rAlt, "eg-y")

	stages := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.15.0.0/24"), "owner-a", [4]byte{10, 15, 0, 10}},
		{netip.MustParsePrefix("10.15.1.0/24"), "owner-b", [4]byte{10, 15, 1, 10}},
		{netip.MustParsePrefix("10.15.2.0/24"), "owner-c", [4]byte{10, 15, 2, 10}},
	}
	goodDst := [4]byte{10, 15, 9, 33}
	badDst := [4]byte{10, 15, 8, 33}

	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{stage.srcPrefix},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.15.9.0/24")},
		})
		e.SetIngressSession(rOwner, stage.session)

		// Alternate self-loop candidate between competing equal-prefix routes.
		if i%2 == 0 {
			e.SetEgressSession(rMain, stage.session)
			e.SetEgressSession(rAlt, "eg-z")
		} else {
			e.SetEgressSession(rMain, "eg-z")
			e.SetEgressSession(rAlt, stage.session)
		}

		if d := e.HandleIngress(makeIPv4(stage.srcIP, badDst), stage.session); d.Action != ActionDrop {
			t.Fatalf("stage %d: expected drop on disallowed destination, got %+v", i, d)
		}

		d := e.HandleIngress(makeIPv4(stage.srcIP, goodDst), stage.session)
		if d.Action != ActionForward || d.EgressSession != "eg-z" {
			t.Fatalf("stage %d: expected forward to eg-z, got %+v", i, d)
		}
		if d.EgressSession == stage.session {
			t.Fatalf("stage %d: self-loop egress selected: %+v", i, d)
		}

		if i > 0 {
			prev := stages[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, goodDst), prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerChurnWithDestinationWithdrawAndCompetingLoopCandidate(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 11
		rDstA  RouteID = 12
		rDstB  RouteID = 13
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.17.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
	})

	steps := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.17.0.0/24"), "owner-a", [4]byte{10, 17, 0, 7}},
		{netip.MustParsePrefix("10.17.1.0/24"), "owner-b", [4]byte{10, 17, 1, 7}},
		{netip.MustParsePrefix("10.17.2.0/24"), "owner-c", [4]byte{10, 17, 2, 7}},
	}
	dst := [4]byte{10, 17, 9, 77}

	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcPrefix},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
		})
		e.SetIngressSession(rOwner, step.session)

		// Destination route withdraw window: dataplane must drop instead of selecting self-loop.
		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop during destination withdraw, got %+v", i, d)
		}

		// Re-add competing destination routes and force one of them to ingress session.
		e.UpsertRoute(Route{
			ID:               rDstA,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
		})
		e.UpsertRoute(Route{
			ID:               rDstB,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.17.9.0/24")},
		})
		if i%2 == 0 {
			e.SetEgressSession(rDstA, step.session)
			e.SetEgressSession(rDstB, "egress-z")
		} else {
			e.SetEgressSession(rDstA, "egress-z")
			e.SetEgressSession(rDstB, step.session)
		}
		d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected non-loop forward to egress-z, got %+v", i, d)
		}
		if d.EgressSession == step.session {
			t.Fatalf("step %d: self-loop selected: %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dst), prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineRapidOwnerFlipWithDualWithdrawNeverFallsIntoSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 21
		rDstA  RouteID = 22
		rDstB  RouteID = 23
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.19.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
	})

	steps := []struct {
		srcPrefix netip.Prefix
		session   SessionKey
		srcIP     [4]byte
	}{
		{netip.MustParsePrefix("10.19.0.0/24"), "owner-a", [4]byte{10, 19, 0, 10}},
		{netip.MustParsePrefix("10.19.1.0/24"), "owner-b", [4]byte{10, 19, 1, 10}},
		{netip.MustParsePrefix("10.19.2.0/24"), "owner-c", [4]byte{10, 19, 2, 10}},
		{netip.MustParsePrefix("10.19.3.0/24"), "owner-a", [4]byte{10, 19, 3, 10}},
		{netip.MustParsePrefix("10.19.4.0/24"), "owner-c", [4]byte{10, 19, 4, 10}},
	}
	dst := [4]byte{10, 19, 9, 90}

	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcPrefix},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
		})
		e.SetIngressSession(rOwner, step.session)

		// Full destination withdraw window: must drop, never try loopback fallback.
		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in dual-withdraw window, got %+v", i, d)
		}

		e.UpsertRoute(Route{
			ID:               rDstA,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
		})
		e.UpsertRoute(Route{
			ID:               rDstB,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.19.9.0/24")},
		})
		if i%2 == 0 {
			e.SetEgressSession(rDstA, step.session)
			e.SetEgressSession(rDstB, "egress-z")
		} else {
			e.SetEgressSession(rDstA, "egress-z")
			e.SetEgressSession(rDstB, step.session)
		}
		d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected forward to non-loop egress-z, got %+v", i, d)
		}
	}
}

func TestMemEngineAllowedDstOwnerFlipWithCompetingReAddNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 31
		rMain  RouteID = 32
		rAlt   RouteID = 33
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.21.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rMain,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rAlt,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
	})

	steps := []struct {
		session  SessionKey
		srcCIDR  netip.Prefix
		srcIP    [4]byte
		prevSess SessionKey
		prevIP   [4]byte
	}{
		{
			session: "owner-a",
			srcCIDR: netip.MustParsePrefix("10.21.0.0/24"),
			srcIP:   [4]byte{10, 21, 0, 9},
		},
		{
			session:  "owner-b",
			srcCIDR:  netip.MustParsePrefix("10.21.1.0/24"),
			srcIP:    [4]byte{10, 21, 1, 9},
			prevSess: "owner-a",
			prevIP:   [4]byte{10, 21, 0, 9},
		},
		{
			session:  "owner-c",
			srcCIDR:  netip.MustParsePrefix("10.21.2.0/24"),
			srcIP:    [4]byte{10, 21, 2, 9},
			prevSess: "owner-b",
			prevIP:   [4]byte{10, 21, 1, 9},
		},
	}
	dstAllowed := [4]byte{10, 21, 9, 90}
	dstDenied := [4]byte{10, 21, 8, 90}
	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcCIDR},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
		})
		e.SetIngressSession(rOwner, step.session)

		if d := e.HandleIngress(makeIPv4(step.srcIP, dstDenied), step.session); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop on disallowed destination, got %+v", i, d)
		}

		e.RemoveRoute(rMain)
		e.RemoveRoute(rAlt)
		e.UpsertRoute(Route{
			ID:               rMain,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
		})
		e.UpsertRoute(Route{
			ID:               rAlt,
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.21.9.0/24")},
		})
		if i%2 == 0 {
			e.SetEgressSession(rMain, step.session)
			e.SetEgressSession(rAlt, "egress-z")
		} else {
			e.SetEgressSession(rMain, "egress-z")
			e.SetEgressSession(rAlt, step.session)
		}

		d := e.HandleIngress(makeIPv4(step.srcIP, dstAllowed), step.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected non-loop forward to egress-z, got %+v", i, d)
		}
		if d.EgressSession == step.session {
			t.Fatalf("step %d: selected self-egress %+v", i, d)
		}

		if i > 0 {
			dPrev := e.HandleIngress(makeIPv4(step.prevIP, dstAllowed), step.prevSess)
			if dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, step.prevSess, dPrev)
			}
		}
	}
}

func TestMemEngineRapidIngressRebindWithRouteRemoveReAddKeepsNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 51
		rDstA  RouteID = 52
		rDstB  RouteID = 53
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.23.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstA,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDstB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
	})

	stages := []struct {
		session SessionKey
		srcCIDR netip.Prefix
		srcIP   [4]byte
	}{
		{session: "owner-a", srcCIDR: netip.MustParsePrefix("10.23.0.0/24"), srcIP: [4]byte{10, 23, 0, 12}},
		{session: "owner-b", srcCIDR: netip.MustParsePrefix("10.23.1.0/24"), srcIP: [4]byte{10, 23, 1, 12}},
		{session: "owner-c", srcCIDR: netip.MustParsePrefix("10.23.2.0/24"), srcIP: [4]byte{10, 23, 2, 12}},
		{session: "owner-a", srcCIDR: netip.MustParsePrefix("10.23.3.0/24"), srcIP: [4]byte{10, 23, 3, 12}},
	}
	dst := [4]byte{10, 23, 9, 12}

	var prevSession SessionKey
	var prevIP [4]byte
	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{stage.srcCIDR},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")},
		})
		e.ClearIngressSession(prevSession)
		e.SetIngressSession(rOwner, stage.session)

		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.session); d.Action != ActionDrop {
			t.Fatalf("stage %d: expected drop during dual-withdraw, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")}})
		e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.23.9.0/24")}})
		if i%2 == 0 {
			e.SetEgressSession(rDstA, stage.session)
			e.SetEgressSession(rDstB, "egress-z")
		} else {
			e.SetEgressSession(rDstA, "egress-z")
			e.SetEgressSession(rDstB, stage.session)
		}

		d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("stage %d: expected forward to non-loop egress-z, got %+v", i, d)
		}

		if i > 0 {
			if dPrev := e.HandleIngress(makeIPv4(prevIP, dst), prevSession); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prevSession, dPrev)
			}
		}
		prevSession = stage.session
		prevIP = stage.srcIP
	}
}

func TestMemEngineOwnerRotationWithDestinationOscillationNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 61
		rDstA  RouteID = 62
		rDstB  RouteID = 63
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.25.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")}})

	steps := []struct {
		session SessionKey
		srcCIDR netip.Prefix
		srcIP   [4]byte
	}{
		{session: "owner-a", srcCIDR: netip.MustParsePrefix("10.25.0.0/24"), srcIP: [4]byte{10, 25, 0, 20}},
		{session: "owner-b", srcCIDR: netip.MustParsePrefix("10.25.1.0/24"), srcIP: [4]byte{10, 25, 1, 20}},
		{session: "owner-c", srcCIDR: netip.MustParsePrefix("10.25.2.0/24"), srcIP: [4]byte{10, 25, 2, 20}},
	}
	dst := [4]byte{10, 25, 9, 20}

	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcCIDR},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")},
		})
		e.SetIngressSession(rOwner, step.session)

		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop while destination withdrawn, got %+v", i, d)
		}
		e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")}})
		e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.25.9.0/24")}})
		e.SetEgressSession(rDstA, step.session)
		e.SetEgressSession(rDstB, "egress-z")

		d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.session)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected forward to non-loop egress-z, got %+v", i, d)
		}
		if d.EgressSession == step.session {
			t.Fatalf("step %d: unexpected self-egress %+v", i, d)
		}
	}
}

func TestMemEngineOwnerRotationWithStrictAllowedDstAndCompetingLoopNeverLeaks(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 70
		rLoop  RouteID = 71
		rGood  RouteID = 72
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.40.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.40.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.40.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.40.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.40.9.0/24")}})

	steps := []struct {
		owner   SessionKey
		srcCIDR netip.Prefix
		srcIP   [4]byte
	}{
		{owner: "client-a", srcCIDR: netip.MustParsePrefix("10.40.0.0/24"), srcIP: [4]byte{10, 40, 0, 11}},
		{owner: "client-b", srcCIDR: netip.MustParsePrefix("10.40.1.0/24"), srcIP: [4]byte{10, 40, 1, 11}},
		{owner: "client-c", srcCIDR: netip.MustParsePrefix("10.40.2.0/24"), srcIP: [4]byte{10, 40, 2, 11}},
	}
	dstAllowed := [4]byte{10, 40, 9, 11}
	dstDenied := [4]byte{10, 40, 8, 11}
	var prevOwner SessionKey
	var prevSrc [4]byte
	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{step.srcCIDR},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.40.9.0/24")},
			ExportedPrefixes: []netip.Prefix{step.srcCIDR},
		})
		e.SetIngressSession(rOwner, step.owner)
		e.SetEgressSession(rLoop, step.owner)
		e.SetEgressSession(rGood, "egress-ok")

		if d := e.HandleIngress(makeIPv4(step.srcIP, dstAllowed), step.owner); d.Action != ActionForward || d.EgressSession != "egress-ok" {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstDenied), step.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected AllowedDst drop, got %+v", i, d)
		}
		if i > 0 {
			if d := e.HandleIngress(makeIPv4(prevSrc, dstAllowed), prevOwner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner must drop, got %+v", i, d)
			}
		}
		prevOwner = step.owner
		prevSrc = step.srcIP
	}
}

func TestMemEngineOwnerRotationWithLoopOnlyWindowDropsThenRecoversWithoutSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 80
		rLoop  RouteID = 81
		rGood  RouteID = 82
	)
	dst := [4]byte{10, 50, 9, 9}
	steps := []struct {
		owner   SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{owner: "client-a", srcCIDR: "10.50.0.2/32", srcIP: [4]byte{10, 50, 0, 2}},
		{owner: "client-b", srcCIDR: "10.50.0.3/32", srcIP: [4]byte{10, 50, 0, 3}},
		{owner: "client-c", srcCIDR: "10.50.0.4/32", srcIP: [4]byte{10, 50, 0, 4}},
	}
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.50.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.50.9.0/24")}})
	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.50.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
		})
		e.SetIngressSession(rOwner, step.owner)
		e.SetEgressSession(rLoop, step.owner)
		e.SetEgressSession(rGood, "egress-z")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner); d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}
		e.RemoveRoute(rGood)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop with loop-only egress, got %+v", i, d)
		}
		e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.50.9.0/24")}})
		e.SetEgressSession(rGood, "egress-z")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner); d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected recovery forward, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner); d.EgressSession == step.owner {
			t.Fatalf("step %d: unexpected self-egress %+v", i, d)
		}
	}
}

func TestMemEngineOwnerAtoBtoCWithLoopCandidateAndMissingEgressDropsUntilRecovery(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 90
		rDstA  RouteID = 91
		rDstB  RouteID = 92
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.62.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.62.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.62.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.62.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.62.9.0/24")}})

	steps := []struct {
		owner   SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{owner: "owner-a", srcCIDR: "10.62.0.0/24", srcIP: [4]byte{10, 62, 0, 2}},
		{owner: "owner-b", srcCIDR: "10.62.1.0/24", srcIP: [4]byte{10, 62, 1, 2}},
		{owner: "owner-c", srcCIDR: "10.62.2.0/24", srcIP: [4]byte{10, 62, 2, 2}},
	}
	dst := [4]byte{10, 62, 9, 2}
	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.62.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
		})
		e.SetIngressSession(rOwner, step.owner)

		// Loop on rDstA + missing egress on rDstB must result in drop.
		e.SetEgressSession(rDstA, step.owner)
		e.ClearEgressSession(rDstB)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop with loop+missing egress, got %+v", i, d)
		}

		// Restore healthy egress and verify forwarding recovers without self-egress.
		e.SetEgressSession(rDstB, "egress-z")
		d := e.HandleIngress(makeIPv4(step.srcIP, dst), step.owner)
		if d.Action != ActionForward || d.EgressSession != "egress-z" {
			t.Fatalf("step %d: expected forward to recovered egress-z, got %+v", i, d)
		}
		if d.EgressSession == step.owner {
			t.Fatalf("step %d: unexpected self-egress %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerAtoBtoCWithCompetingSMBPrefixLoopWindowNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 100
		rDstLo RouteID = 101
		rDstHi RouteID = 102
	)
	steps := []struct {
		owner   SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{owner: "owner-a", srcCIDR: "10.70.0.0/24", srcIP: [4]byte{10, 70, 0, 9}},
		{owner: "owner-b", srcCIDR: "10.70.1.0/24", srcIP: [4]byte{10, 70, 1, 9}},
		{owner: "owner-c", srcCIDR: "10.70.2.0/24", srcIP: [4]byte{10, 70, 2, 9}},
	}
	dstSMB := [4]byte{10, 70, 9, 10}

	e.UpsertRoute(Route{ID: rDstLo, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.70.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstHi, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.70.9.10/32")}})

	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.70.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
		})
		e.SetIngressSession(rOwner, step.owner)

		// Highest-prefix candidate loops back to ingress, lower-prefix candidate is healthy.
		e.SetEgressSession(rDstHi, step.owner)
		e.SetEgressSession(rDstLo, "dst-smb")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected forward to non-loop destination, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.EgressSession == step.owner {
			t.Fatalf("step %d: unexpected self-egress after healthy fallback, got %+v", i, d)
		}

		// Simulate runtime race: healthy route temporarily loses egress -> must drop, never loop.
		e.ClearEgressSession(rDstLo)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in loop-only SMB window, got %+v", i, d)
		}

		// Restore healthy egress and ensure recovery.
		e.SetEgressSession(rDstLo, "dst-smb")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected forward recovery after restore, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dstSMB), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineAllowedDstSMB32OwnerChurnDropsOnLoopOnlyAndRecoversNonLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 201
		rDst24 RouteID = 202
		rDst32 RouteID = 203
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.80.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.80.9.10/32")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.80.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.80.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.80.9.10/32")}})

	steps := []struct {
		owner   SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{owner: "owner-a", srcCIDR: "10.80.0.0/24", srcIP: [4]byte{10, 80, 0, 9}},
		{owner: "owner-b", srcCIDR: "10.80.1.0/24", srcIP: [4]byte{10, 80, 1, 9}},
		{owner: "owner-c", srcCIDR: "10.80.2.0/24", srcIP: [4]byte{10, 80, 2, 9}},
	}
	dstSMB := [4]byte{10, 80, 9, 10}
	for i, step := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.80.9.10/32")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(step.srcCIDR)},
		})
		e.SetIngressSession(rOwner, step.owner)

		// Longest-prefix /32 is loop, /24 is healthy fallback.
		e.SetEgressSession(rDst32, step.owner)
		e.SetEgressSession(rDst24, "egress-smb")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionForward || d.EgressSession != "egress-smb" {
			t.Fatalf("step %d: expected forward via non-loop /24 fallback, got %+v", i, d)
		}

		// Loop-only window (healthy /24 removed) must drop.
		e.RemoveRoute(rDst24)
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
		}

		// Restore /24 and verify deterministic recovery.
		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.80.9.0/24")}})
		e.SetEgressSession(rDst24, "egress-smb")
		if d := e.HandleIngress(makeIPv4(step.srcIP, dstSMB), step.owner); d.Action != ActionForward || d.EgressSession != "egress-smb" {
			t.Fatalf("step %d: expected forward recovery to egress-smb, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dstSMB), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineBiDirectionalSMBAuthSymptomLoopWindowNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 301
		rOwnerC RouteID = 302
		rDstA32 RouteID = 303
		rDstC32 RouteID = 304
		rDst24  RouteID = 305
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.91.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.91.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.91.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.91.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.91.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.91.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.91.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.91.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.91.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 91, 0, 10}, [4]byte{10, 91, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 91, 2, 10}, [4]byte{10, 91, 9, 10})
	for i := 0; i < 6; i++ {
		// Keep /32 routes as loop candidates for both directions.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")

		// Healthy /24 egress alternates to emulate runtime owner churn.
		if i%2 == 0 {
			e.SetEgressSession(rDst24, "dst-z")
		} else {
			e.SetEgressSession(rDst24, "dst-z-2")
		}

		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C expected non-loop forward, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A expected non-loop forward, got %+v", i, d)
		}

		// With /24 withdrawn, forwarding may continue via opposite /32 owner,
		// but it must still never select ingress session as egress.
		e.ClearEgressSession(rDst24)
		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C withdrawn-/24 window must avoid self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A withdrawn-/24 window must avoid self-egress, got %+v", i, d)
		}
	}
}

func TestMemEngineSMB32OwnerFlipWithFallbackChurnDropsLoopOnlyAndRecoversNonLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 410
		rDst24 RouteID = 411
		rDst32 RouteID = 412
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.95.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.95.9.10/32")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.95.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.95.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.95.9.10/32")}})

	stages := []struct {
		owner   SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{owner: "owner-a", srcCIDR: "10.95.0.0/24", srcIP: [4]byte{10, 95, 0, 10}},
		{owner: "owner-b", srcCIDR: "10.95.1.0/24", srcIP: [4]byte{10, 95, 1, 10}},
		{owner: "owner-c", srcCIDR: "10.95.2.0/24", srcIP: [4]byte{10, 95, 2, 10}},
	}
	dst := [4]byte{10, 95, 9, 10}
	for i, stage := range stages {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(stage.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.95.9.10/32")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(stage.srcCIDR)},
		})
		e.SetIngressSession(rOwner, stage.owner)

		// /32 route intentionally loops to ingress owner, /24 must be selected.
		e.SetEgressSession(rDst32, stage.owner)
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("stage %d: expected non-loop /24 fallback forward, got %+v", i, d)
		}

		// Remove /24 to create loop-only window: must drop, never self-forward.
		e.RemoveRoute(rDst24)
		if d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.owner); d.Action != ActionDrop {
			t.Fatalf("stage %d: expected drop in /32 loop-only window, got %+v", i, d)
		}

		// Re-add /24 route and verify deterministic recovery.
		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.95.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("stage %d: expected forward recovery after /24 re-add, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(stage.srcIP, dst), stage.owner); d.EgressSession == stage.owner {
			t.Fatalf("stage %d: unexpected self-egress after recovery: %+v", i, d)
		}

		if i > 0 {
			prev := stages[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("stage %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineBidirectionalSMB32OwnerRotationWithTieChurnNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 501
		rOwnerC RouteID = 502
		rDstA32 RouteID = 503
		rDstC32 RouteID = 504
		rDst24X RouteID = 505
		rDst24Y RouteID = 506
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.97.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.97.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24X, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst24Y, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 97, 0, 10}, [4]byte{10, 97, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 97, 2, 10}, [4]byte{10, 97, 9, 10})
	for i := 0; i < 8; i++ {
		// /32 targets always self-loop for their matching ingress owners.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")

		// Equal-prefix /24 routes churn to ensure deterministic non-loop selection.
		e.RemoveRoute(rDst24X)
		e.RemoveRoute(rDst24Y)
		e.UpsertRoute(Route{ID: rDst24X, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")}})
		e.UpsertRoute(Route{ID: rDst24Y, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.97.9.0/24")}})
		if i%2 == 0 {
			e.SetEgressSession(rDst24X, "dst-x")
			e.SetEgressSession(rDst24Y, "dst-y")
		} else {
			e.SetEgressSession(rDst24X, "dst-y")
			e.SetEgressSession(rDst24Y, "dst-x")
		}

		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C must avoid self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A must avoid self-egress, got %+v", i, d)
		}
	}
}

func TestMemEngineBidirectionalSMBLoopOnlyWindowDropsAndRecoversWithoutSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 601
		rOwnerC RouteID = 602
		rDstA32 RouteID = 603
		rDstC32 RouteID = 604
		rDst24  RouteID = 605
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.99.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.99.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.99.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.99.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 99, 0, 10}, [4]byte{10, 99, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 99, 2, 10}, [4]byte{10, 99, 9, 10})
	for i := 0; i < 6; i++ {
		// /32 routes always self-loop; /24 path must carry non-loop forwarding.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: expected A->C non-loop forward before loop-only window, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: expected C->A non-loop forward before loop-only window, got %+v", i, d)
		}

		// Withdraw /24: path may still forward via opposite /32 owner, but must never self-loop.
		e.RemoveRoute(rDst24)
		if d := e.HandleIngress(pktAtoC, "owner-a"); d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C loop-only window must avoid self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A loop-only window must avoid self-egress, got %+v", i, d)
		}

		// Re-add /24 and confirm forwarding recovery is deterministic and non-loop.
		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.99.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: expected A->C non-loop recovery, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: expected C->A non-loop recovery, got %+v", i, d)
		}
	}
}

func TestMemEngineSMBAuthSymptomGuardWithAlternatingCompetingRouteNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 701
		rOwnerC RouteID = 702
		rDstA32 RouteID = 703
		rDstC32 RouteID = 704
		rDst24X RouteID = 705
		rDst24Y RouteID = 706
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.100.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.100.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24X, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst24Y, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 100, 0, 10}, [4]byte{10, 100, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 100, 2, 10}, [4]byte{10, 100, 9, 10})
	for i := 0; i < 6; i++ {
		// Force /32 self-loop candidates for both directions.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")

		// Alternate competing /24 route ownership/churn.
		e.RemoveRoute(rDst24X)
		e.RemoveRoute(rDst24Y)
		e.UpsertRoute(Route{ID: rDst24X, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")}})
		e.UpsertRoute(Route{ID: rDst24Y, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.100.9.0/24")}})
		if i%2 == 0 {
			e.SetEgressSession(rDst24X, "dst-smb")
			e.SetEgressSession(rDst24Y, "dst-fallback")
		} else {
			e.SetEgressSession(rDst24X, "dst-fallback")
			e.SetEgressSession(rDst24Y, "dst-smb")
		}

		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C must forward without self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A must forward without self-egress, got %+v", i, d)
		}
	}
}

func TestMemEngineSMBAuthSymptomGuardWithOwnerRotationAndAllowedDstNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 801
		rDst32 RouteID = 802
		rDst24 RouteID = 803
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.110.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.110.9.10/32")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.110.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDst32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.110.9.10/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.110.9.0/24")}})

	owners := []struct {
		session SessionKey
		srcCIDR string
		srcIP   [4]byte
	}{
		{session: "owner-a", srcCIDR: "10.110.0.0/24", srcIP: [4]byte{10, 110, 0, 10}},
		{session: "owner-b", srcCIDR: "10.110.1.0/24", srcIP: [4]byte{10, 110, 1, 10}},
		{session: "owner-c", srcCIDR: "10.110.2.0/24", srcIP: [4]byte{10, 110, 2, 10}},
	}
	dstAllowed := [4]byte{10, 110, 9, 10}
	dstDenied := [4]byte{10, 110, 9, 11}

	for i, owner := range owners {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(owner.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.110.9.10/32")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(owner.srcCIDR)},
		})
		e.SetIngressSession(rOwner, owner.session)
		e.SetEgressSession(rDst32, owner.session) // longest-prefix loop candidate
		e.SetEgressSession(rDst24, "egress-smb")

		if d := e.HandleIngress(makeIPv4(owner.srcIP, dstDenied), owner.session); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop on denied destination, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(owner.srcIP, dstAllowed), owner.session); d.Action != ActionForward || d.EgressSession != "egress-smb" {
			t.Fatalf("step %d: expected non-loop forward to egress-smb, got %+v", i, d)
		}
		if d := e.HandleIngress(makeIPv4(owner.srcIP, dstAllowed), owner.session); d.EgressSession == owner.session {
			t.Fatalf("step %d: unexpected self-egress detected: %+v", i, d)
		}
		if i > 0 {
			prev := owners[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dstAllowed), prev.session); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.session, dPrev)
			}
		}
	}
}

func TestMemEngineBidirectionalSMBLoopGuardWithRouteIDTieChurnNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 901
		rOwnerC RouteID = 902
		rDstA32 RouteID = 903
		rDstC32 RouteID = 904
		rDstX24 RouteID = 905
		rDstY24 RouteID = 906
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.120.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.120.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.11/32")}})
	e.UpsertRoute(Route{ID: rDstX24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstY24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 120, 0, 11}, [4]byte{10, 120, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 120, 2, 11}, [4]byte{10, 120, 9, 10})
	for i := 0; i < 5; i++ {
		// Force longest-prefix loop candidates for both directions.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")

		// Rebuild same-prefix /24 candidates to stress deterministic route-id tie-break.
		e.RemoveRoute(rDstX24)
		e.RemoveRoute(rDstY24)
		e.UpsertRoute(Route{ID: rDstX24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")}})
		e.UpsertRoute(Route{ID: rDstY24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.120.9.0/24")}})
		if i%2 == 0 {
			e.SetEgressSession(rDstX24, "dst-smb")
			e.SetEgressSession(rDstY24, "dst-fallback")
		} else {
			e.SetEgressSession(rDstX24, "dst-fallback")
			e.SetEgressSession(rDstY24, "dst-smb")
		}

		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C must forward without self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A must forward without self-egress, got %+v", i, d)
		}
	}
}

func TestMemEngineBidirectionalSMBOwnerStepTiePrefixLoopOnlyDropNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 1001
		rOwnerC RouteID = 1002
		rDstA32 RouteID = 1003
		rDstC32 RouteID = 1004
		rDst24  RouteID = 1005
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.130.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.130.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.130.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.130.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 130, 0, 10}, [4]byte{10, 130, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 130, 2, 10}, [4]byte{10, 130, 9, 10})
	for i := 0; i < 3; i++ {
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: expected non-loop A->C forward before loop-only window, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: expected non-loop C->A forward before loop-only window, got %+v", i, d)
		}

		e.RemoveRoute(rDst24)
		if d := e.HandleIngress(pktAtoC, "owner-a"); (d.Action != ActionDrop && d.Action != ActionForward) || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: A->C loop-only window must avoid self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); (d.Action != ActionDrop && d.Action != ActionForward) || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: C->A loop-only window must avoid self-egress, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.130.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
	}
}

func TestMemEngineBidirectionalSMBAuthSymptomOwnerStepWithStaleEgressChurnNeverLoops(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwnerA RouteID = 1101
		rOwnerC RouteID = 1102
		rDstA32 RouteID = 1103
		rDstC32 RouteID = 1104
		rDst24  RouteID = 1105
	)
	e.UpsertRoute(Route{
		ID:               rOwnerA,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.140.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.140.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rOwnerC,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.140.2.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.140.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.2.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.9.0/24")}})
	e.SetIngressSession(rOwnerA, "owner-a")
	e.SetIngressSession(rOwnerC, "owner-c")

	pktAtoC := makeIPv4([4]byte{10, 140, 0, 10}, [4]byte{10, 140, 9, 11})
	pktCtoA := makeIPv4([4]byte{10, 140, 2, 10}, [4]byte{10, 140, 9, 10})
	for i := 0; i < 6; i++ {
		// Highest-prefix /32 routes intentionally loop to ingress owners.
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")
		e.SetEgressSession(rDst24, "dst-smb")

		if d := e.HandleIngress(pktAtoC, "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: expected non-loop A->C forward, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); d.Action != ActionForward || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: expected non-loop C->A forward, got %+v", i, d)
		}

		// Churn /24 egress in a stale window. No self-egress is allowed.
		if i%2 == 0 {
			e.ClearEgressSession(rDst24)
		} else {
			e.RemoveRoute(rDst24)
		}
		if d := e.HandleIngress(pktAtoC, "owner-a"); (d.Action != ActionForward && d.Action != ActionDrop) || d.EgressSession == "owner-a" {
			t.Fatalf("iteration %d: stale-window A->C must avoid self-egress, got %+v", i, d)
		}
		if d := e.HandleIngress(pktCtoA, "owner-c"); (d.Action != ActionForward && d.Action != ActionDrop) || d.EgressSession == "owner-c" {
			t.Fatalf("iteration %d: stale-window C->A must avoid self-egress, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.140.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
	}
}

func TestMemEngineBidirectionalSMBOwnerStepClearSessionStaleWindowNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner  RouteID = 1201
		rDstA32 RouteID = 1202
		rDstC32 RouteID = 1203
		rDst24  RouteID = 1204
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.150.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.150.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.150.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.150.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC32, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.150.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.150.9.0/24")}})
	e.SetEgressSession(rDst24, "dst-smb")

	type step struct {
		owner   SessionKey
		srcIP   [4]byte
		dstIP   [4]byte
		srcCIDR string
		stale   SessionKey
	}
	steps := []step{
		{owner: "owner-a", srcIP: [4]byte{10, 150, 0, 10}, dstIP: [4]byte{10, 150, 9, 11}, srcCIDR: "10.150.0.0/24"},
		{owner: "owner-b", srcIP: [4]byte{10, 150, 1, 10}, dstIP: [4]byte{10, 150, 9, 10}, srcCIDR: "10.150.1.0/24", stale: "owner-a"},
		{owner: "owner-c", srcIP: [4]byte{10, 150, 2, 10}, dstIP: [4]byte{10, 150, 9, 11}, srcCIDR: "10.150.2.0/24", stale: "owner-b"},
	}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.srcCIDR)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.150.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.srcCIDR)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rDstA32, "owner-a")
		e.SetEgressSession(rDstC32, "owner-c")
		e.SetEgressSession(rDst24, "dst-smb")

		if d := e.HandleIngress(makeIPv4(s.srcIP, s.dstIP), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward for %s, got %+v", i, s.owner, d)
		}
		if s.stale != "" {
			if d := e.HandleIngress(makeIPv4(steps[i-1].srcIP, steps[i-1].dstIP), s.stale); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %s must drop after ingress clear, got %+v", i, s.stale, d)
			}
		}

		if i%2 == 0 {
			e.ClearEgressSession(rDst24)
		} else {
			e.RemoveRoute(rDst24)
		}
		if d := e.HandleIngress(makeIPv4(s.srcIP, s.dstIP), s.owner); (d.Action != ActionDrop && d.Action != ActionForward) || d.EgressSession == s.owner {
			t.Fatalf("step %d: stale window must avoid self-egress, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.150.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
	}
}

func TestMemEngineBidirectionalSMBOwnerStepStaleRouteReaddWithIngressClearPreservesNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1301
		rDstA  RouteID = 1302
		rDstC  RouteID = 1303
		rDst24 RouteID = 1304
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.160.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.160.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.160.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.160.9.10/32")}})
	e.UpsertRoute(Route{ID: rDstC, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.160.9.11/32")}})
	e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.160.9.0/24")}})
	e.SetEgressSession(rDst24, "dst-smb")

	type step struct {
		owner SessionKey
		src   [4]byte
		dst   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 160, 0, 10}, dst: [4]byte{10, 160, 9, 11}, cidr: "10.160.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 160, 1, 10}, dst: [4]byte{10, 160, 9, 10}, cidr: "10.160.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 160, 2, 10}, dst: [4]byte{10, 160, 9, 11}, cidr: "10.160.2.0/24"},
	}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.160.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rDstA, "owner-a")
		e.SetEgressSession(rDstC, "owner-c")
		e.SetEgressSession(rDst24, "dst-smb")

		if d := e.HandleIngress(makeIPv4(s.src, s.dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, prev.dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, d)
			}
		}

		// Clear current ingress + remove fallback route: stale window must never self-egress.
		e.ClearIngressSession(s.owner)
		e.RemoveRoute(rDst24)
		e.SetIngressSession(rOwner, s.owner)
		d := e.HandleIngress(makeIPv4(s.src, s.dst), s.owner)
		if d.Action != ActionDrop && d.Action != ActionForward {
			t.Fatalf("step %d: expected drop/forward in stale window, got %+v", i, d)
		}
		if d.EgressSession == s.owner {
			t.Fatalf("step %d: stale window selected self-egress: %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDst24, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.160.9.0/24")}})
		e.SetEgressSession(rDst24, "dst-smb")
		if d := e.HandleIngress(makeIPv4(s.src, s.dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward recovery, got %+v", i, d)
		}
	}
}

func TestMemEngineServerRestartStyleOwnerStepChurnKeepsNoLoopAndDropsStale(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1401
		rDstA  RouteID = 1402
		rDstB  RouteID = 1403
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.170.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.170.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")}})

	type step struct {
		owner SessionKey
		srcIP [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", srcIP: [4]byte{10, 170, 0, 10}, cidr: "10.170.0.0/24"},
		{owner: "owner-b", srcIP: [4]byte{10, 170, 1, 10}, cidr: "10.170.1.0/24"},
		{owner: "owner-c", srcIP: [4]byte{10, 170, 2, 10}, cidr: "10.170.2.0/24"},
	}
	dst := [4]byte{10, 170, 9, 11}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)

		// Emulate short server restart: destination routes disappear briefly.
		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(s.srcIP, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop during restart window, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")}})
		e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.170.9.0/24")}})
		if i%2 == 0 {
			e.SetEgressSession(rDstA, s.owner) // loop candidate
			e.SetEgressSession(rDstB, "dst-smb")
		} else {
			e.SetEgressSession(rDstA, "dst-smb")
			e.SetEgressSession(rDstB, s.owner) // loop candidate
		}

		d := e.HandleIngress(makeIPv4(s.srcIP, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop recovery forward, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.srcIP, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineServerRestartChurnSMBAuthSymptomGuardNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1501
		rDstA  RouteID = 1502
		rDstB  RouteID = 1503
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.180.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.180.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")}})

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 180, 0, 10}, cidr: "10.180.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 180, 1, 10}, cidr: "10.180.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 180, 2, 10}, cidr: "10.180.2.0/24"},
	}
	dst := [4]byte{10, 180, 9, 11}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)

		e.RemoveRoute(rDstA)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in restart window, got %+v", i, d)
		}

		e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")}})
		e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.180.9.0/24")}})
		e.SetEgressSession(rDstA, s.owner) // force loop candidate
		e.SetEgressSession(rDstB, "dst-smb")

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop recovery forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineServerRestartChurnOwnerStepEgressFlapMaintainsNoLoopRecovery(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1601
		rDstA  RouteID = 1602
		rDstB  RouteID = 1603
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.190.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.190.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.190.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.190.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.190.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 190, 0, 10}, cidr: "10.190.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 190, 1, 10}, cidr: "10.190.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 190, 2, 10}, cidr: "10.190.2.0/24"},
	}
	dst := [4]byte{10, 190, 9, 11}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.190.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)

		// Loop-only restart window: only stale/loop candidate exists.
		e.SetEgressSession(rDstA, s.owner)
		e.RemoveRoute(rDstB)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
		}

		// Recover one non-loop route and keep one loop candidate.
		e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.190.9.0/24")}})
		e.SetEgressSession(rDstB, "dst-smb")
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forward after recovery, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineLoopbackSymptomDriftGatePatternDropsUntilDataplaneRecovers(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1701
		rLoop  RouteID = 1702
		rGood  RouteID = 1703
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.200.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.200.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.200.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.200.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.200.9.0/24")}})
	e.SetIngressSession(rOwner, "owner-a")
	e.SetEgressSession(rGood, "dst-smb")

	src := [4]byte{10, 200, 0, 10}
	dst := [4]byte{10, 200, 9, 11}
	pkt := makeIPv4(src, dst)

	// Loop-only symptom window: SMB/auth may fail while ingress/forward stagnate.
	e.SetEgressSession(rLoop, "owner-a")
	e.RemoveRoute(rGood)
	if d := e.HandleIngress(pkt, "owner-a"); d.Action != ActionDrop {
		t.Fatalf("expected drop in loopback symptom window, got %+v", d)
	}

	// Dataplane recovery: non-loop egress is restored.
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.200.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")
	if d := e.HandleIngress(pkt, "owner-a"); d.Action != ActionForward || d.EgressSession != "dst-smb" {
		t.Fatalf("expected recovered non-loop forwarding, got %+v", d)
	}
}

func TestMemEngineLoopbackSymptomGuardWithNTStatusLikeChurnKeepsDataplaneProgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1711
		rLoop  RouteID = 1712
		rGood  RouteID = 1713
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.211.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.211.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.211.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.211.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.211.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 211, 0, 10}, cidr: "10.211.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 211, 1, 10}, cidr: "10.211.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 211, 2, 10}, cidr: "10.211.2.0/24"},
	}
	dst := [4]byte{10, 211, 9, 11}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.211.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)

		// Keep a competing loop-candidate alive but ensure engine still chooses non-loop egress.
		e.SetEgressSession(rLoop, s.owner)
		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forward under churn, got %+v", i, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepFalsePositiveAuthSymptomGuardKeepsLiveDataplaneNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1721
		rLoop  RouteID = 1722
		rGood  RouteID = 1723
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.213.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.213.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.213.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.213.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.213.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 213, 0, 10}, cidr: "10.213.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 213, 1, 10}, cidr: "10.213.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 213, 2, 10}, cidr: "10.213.2.0/24"},
	}
	dst := [4]byte{10, 213, 9, 11}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.213.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner) // competing loop candidate that must never win

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forward under auth-symptom guard, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepFalsePositiveGuardReasonHistogramChurnNoSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1731
		rLoop  RouteID = 1732
		rGood  RouteID = 1733
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.215.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.215.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.215.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.215.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.215.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 215, 0, 10}, cidr: "10.215.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 215, 1, 10}, cidr: "10.215.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 215, 2, 10}, cidr: "10.215.2.0/24"},
	}
	dst := [4]byte{10, 215, 9, 11}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.215.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forwarding, got %+v", i, d)
		}

		// Simulate short loop-only churn window and ensure engine drops instead of self-egress.
		e.RemoveRoute(rGood)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop when only loop candidate is available, got %+v", i, d)
		}
		e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.215.9.0/24")}})
		e.SetEgressSession(rGood, "dst-smb")

		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepGuardSeveritySplitLoopOnlyWindowNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1801
		rLoop  RouteID = 1802
		rGood  RouteID = 1803
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.230.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.230.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.230.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.230.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.230.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	type step struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}
	steps := []step{
		{owner: "owner-a", src: [4]byte{10, 230, 0, 10}, cidr: "10.230.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 230, 1, 10}, cidr: "10.230.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 230, 2, 10}, cidr: "10.230.2.0/24"},
	}
	dst := [4]byte{10, 230, 9, 9}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.230.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected live dataplane forward, got %+v", i, d)
		}

		e.RemoveRoute(rGood)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected stall-window drop, got %+v", i, d)
		}
		e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.230.9.0/24")}})
		e.SetEgressSession(rGood, "dst-smb")

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected recovered non-loop forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepSeverityTraceAtoBtoCNoLoopAcrossLiveAndStall(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1811
		rLoop  RouteID = 1812
		rGood  RouteID = 1813
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.232.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.232.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.232.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.232.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.232.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}{
		{owner: "owner-a", src: [4]byte{10, 232, 0, 10}, cidr: "10.232.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 232, 1, 10}, cidr: "10.232.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 232, 2, 10}, cidr: "10.232.2.0/24"},
	}
	dst := [4]byte{10, 232, 9, 20}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.232.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected live no-loop forwarding, got %+v", i, d)
		}

		e.RemoveRoute(rGood)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
			t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
		}
		e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.232.9.0/24")}})
		e.SetEgressSession(rGood, "dst-smb")
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected recovered non-loop forwarding, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepDataplaneStallWindowProducesDropWithoutSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1821
		rLoop  RouteID = 1822
		rGood  RouteID = 1823
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.234.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.234.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.234.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.234.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.234.9.0/24")}})
	e.SetIngressSession(rOwner, "owner-a")
	e.SetEgressSession(rLoop, "owner-a")
	e.SetEgressSession(rGood, "dst-smb")

	src := [4]byte{10, 234, 0, 11}
	dst := [4]byte{10, 234, 9, 44}
	if d := e.HandleIngress(makeIPv4(src, dst), "owner-a"); d.Action != ActionForward || d.EgressSession != "dst-smb" {
		t.Fatalf("expected non-loop forward before stall window, got %+v", d)
	}

	e.RemoveRoute(rGood)
	for i := 0; i < 2; i++ {
		if d := e.HandleIngress(makeIPv4(src, dst), "owner-a"); d.Action != ActionDrop {
			t.Fatalf("stall probe %d: expected drop with loop-only candidate, got %+v", i, d)
		}
	}

	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.234.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")
	if d := e.HandleIngress(makeIPv4(src, dst), "owner-a"); d.Action != ActionForward || d.EgressSession == "owner-a" {
		t.Fatalf("expected recovered non-loop forwarding, got %+v", d)
	}
}

func TestMemEngineOwnerStepControlPlaneJitterLoopOnlyWindowDropsAndRecoversNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1831
		rLoop  RouteID = 1832
		rGood  RouteID = 1833
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.237.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.237.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.237.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.237.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.237.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		loopWindow bool
	}{
		{owner: "owner-a", src: [4]byte{10, 237, 0, 10}, cidr: "10.237.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 237, 1, 10}, cidr: "10.237.1.0/24", loopWindow: true},
		{owner: "owner-c", src: [4]byte{10, 237, 2, 10}, cidr: "10.237.2.0/24"},
	}
	dst := [4]byte{10, 237, 9, 33}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.237.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.loopWindow {
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.237.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepControlPlaneResultBaselineAllVsAnomalyNoSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1841
		rLoop  RouteID = 1842
		rGood  RouteID = 1843
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.239.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.239.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.239.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.239.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.239.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner   SessionKey
		src     [4]byte
		cidr    string
		anomaly bool
	}{
		{owner: "owner-a", src: [4]byte{10, 239, 0, 10}, cidr: "10.239.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 239, 1, 10}, cidr: "10.239.1.0/24", anomaly: true},
		{owner: "owner-c", src: [4]byte{10, 239, 2, 10}, cidr: "10.239.2.0/24"},
	}
	dst := [4]byte{10, 239, 9, 77}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.239.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.anomaly {
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d anomaly: expected drop in loop-only window, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.239.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}
	}
}

func TestMemEngineOwnerStepGuardControlPlaneCrossCheckCompetingPrefixNoSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1851
		rLoop  RouteID = 1852
		rGood  RouteID = 1853
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.241.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.241.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.241.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.241.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.241.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		loopWindow bool
	}{
		{owner: "owner-a", src: [4]byte{10, 241, 0, 10}, cidr: "10.241.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 241, 1, 10}, cidr: "10.241.1.0/24", loopWindow: true},
		{owner: "owner-c", src: [4]byte{10, 241, 2, 10}, cidr: "10.241.2.0/24"},
	}
	dst := [4]byte{10, 241, 9, 50}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.241.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.loopWindow {
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d loop-window: expected drop with loop-only candidate, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.241.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected recovered non-loop forwarding, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepGuardControlPlaneCrossCheckPerResultBaselineNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1861
		rLoop  RouteID = 1862
		rGood  RouteID = 1863
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.243.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		loopWindow bool
	}{
		{owner: "owner-a", src: [4]byte{10, 243, 0, 10}, cidr: "10.243.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 243, 1, 10}, cidr: "10.243.1.0/24", loopWindow: true},
		{owner: "owner-c", src: [4]byte{10, 243, 2, 10}, cidr: "10.243.2.0/24", loopWindow: true},
	}
	dst := [4]byte{10, 243, 9, 40}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.loopWindow {
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d loop-window: expected drop with loop-only candidate, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward after recovery, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepGuardControlPlaneTrendBaselineNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1871
		rLoop  RouteID = 1872
		rGood  RouteID = 1873
	)
	e.UpsertRoute(Route{
		ID:               rOwner,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.245.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.245.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.245.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.245.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.245.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	type trendStep struct {
		owner  SessionKey
		src    [4]byte
		cidr   string
		stall  bool
		jitter bool
	}
	steps := []trendStep{
		{owner: "owner-a", src: [4]byte{10, 245, 0, 10}, cidr: "10.245.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 245, 1, 10}, cidr: "10.245.1.0/24", stall: true, jitter: true},
		{owner: "owner-c", src: [4]byte{10, 245, 2, 10}, cidr: "10.245.2.0/24"},
		{owner: "owner-a", src: [4]byte{10, 245, 3, 10}, cidr: "10.245.3.0/24"},
		{owner: "owner-b", src: [4]byte{10, 245, 4, 10}, cidr: "10.245.4.0/24", stall: true},
		{owner: "owner-c", src: [4]byte{10, 245, 5, 10}, cidr: "10.245.5.0/24"},
	}

	dst := [4]byte{10, 245, 9, 99}
	allSteps := 0
	anomalySteps := 0
	allStallWithoutDrop := 0
	anomalyStallWithoutDrop := 0

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rOwner,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.245.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		allSteps++
		dropSeen := false
		if s.stall {
			anomalySteps++
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				if !s.jitter {
					anomalyStallWithoutDrop++
				}
				allStallWithoutDrop++
				t.Fatalf("step %d: expected drop in stall window, got %+v", i, d)
			}
			dropSeen = true
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.245.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}
		if s.stall && !s.jitter && !dropSeen {
			allStallWithoutDrop++
			anomalyStallWithoutDrop++
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected recovered non-loop forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}

	if allSteps != len(steps) || anomalySteps != 2 {
		t.Fatalf("unexpected trend counters: all=%d anomaly=%d", allSteps, anomalySteps)
	}
	if allStallWithoutDrop != 0 || anomalyStallWithoutDrop != 0 {
		t.Fatalf("trend baseline must not report stall-without-drop in no-loop dataplane: all=%d anomaly=%d",
			allStallWithoutDrop, anomalyStallWithoutDrop)
	}
}

func TestMemEngineOwnerStepBudgetDecayCooldownWindowKeepsNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rOwner RouteID = 1901
		rLoop  RouteID = 1902
		rGood  RouteID = 1903
	)
	e.UpsertRoute(Route{ID: rOwner, AllowedSrc: []netip.Prefix{netip.MustParsePrefix("10.250.0.0/24")}, AllowedDst: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")}, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.0.0/24")}})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")}})
	e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")}})
	e.SetEgressSession(rGood, "dst-smb")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		loopWindow bool
	}{
		{owner: "owner-a", src: [4]byte{10, 250, 0, 11}, cidr: "10.250.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 250, 1, 11}, cidr: "10.250.1.0/24", loopWindow: true}, // old anomaly
		{owner: "owner-c", src: [4]byte{10, 250, 2, 11}, cidr: "10.250.2.0/24"},
		{owner: "owner-a", src: [4]byte{10, 250, 3, 11}, cidr: "10.250.3.0/24"},
	}
	dst := [4]byte{10, 250, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{ID: rOwner, AllowedSrc: []netip.Prefix{netip.MustParsePrefix(s.cidr)}, AllowedDst: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")}, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)}})
		e.SetIngressSession(rOwner, s.owner)
		e.SetEgressSession(rLoop, s.owner) // force loop candidate

		if s.loopWindow {
			e.RemoveRoute(rGood)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rGood, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")}})
			e.SetEgressSession(rGood, "dst-smb")
		}

		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession == s.owner {
			t.Fatalf("step %d: expected non-loop forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineBidirectionalSMBAuthSymptomOwnerFlapNeverSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rSrc  RouteID = 980
		rDst  RouteID = 981
		rLoop RouteID = 982
	)
	e.UpsertRoute(Route{
		ID:               rSrc,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.252.0.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rDst,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
	})
	e.SetEgressSession(rDst, "dst-smb")

	steps := []struct {
		owner string
		src   [4]byte
		cidr  string
	}{
		{owner: "owner-a", src: [4]byte{10, 252, 0, 11}, cidr: "10.252.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 252, 1, 11}, cidr: "10.252.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 252, 2, 11}, cidr: "10.252.2.0/24"},
		{owner: "owner-a", src: [4]byte{10, 252, 3, 11}, cidr: "10.252.3.0/24"},
	}
	dst := [4]byte{10, 252, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rSrc,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rSrc, SessionKey(s.owner))
		e.SetEgressSession(rLoop, SessionKey(s.owner)) // loop candidate with same dst prefix

		if d := e.HandleIngress(makeIPv4(s.src, dst), SessionKey(s.owner)); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected forwarding to non-loop SMB egress, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), SessionKey(prev.owner)); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after owner flap, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepNoisySuppressionWindowStillPreventsSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1201
		rLoop    RouteID = 1202
		rSMB     RouteID = 1203
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.254.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rSMB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
	})
	e.SetEgressSession(rSMB, "dst-smb")

	steps := []struct {
		owner SessionKey
		src   [4]byte
		cidr  string
		stall bool
	}{
		{owner: "owner-a", src: [4]byte{10, 254, 0, 11}, cidr: "10.254.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 254, 1, 11}, cidr: "10.254.1.0/24", stall: true},
		{owner: "owner-c", src: [4]byte{10, 254, 2, 11}, cidr: "10.254.2.0/24"},
		{owner: "owner-b", src: [4]byte{10, 254, 3, 11}, cidr: "10.254.3.0/24"},
	}
	dst := [4]byte{10, 254, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner) // competing loop candidate
		if s.stall {
			e.RemoveRoute(rSMB)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in loop-only stall window, got %+v", i, d)
			}
			e.UpsertRoute(Route{
				ID:               rSMB,
				ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
			})
			e.SetEgressSession(rSMB, "dst-smb")
		}
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop SMB forward, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepAgeWeightedSuppressionFlapStillPreventsLoopback(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1301
		rLoop    RouteID = 1302
		rSMB     RouteID = 1303
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.250.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.0.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rLoop,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")},
	})
	e.UpsertRoute(Route{
		ID:               rSMB,
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")},
	})
	e.SetEgressSession(rSMB, "dst-smb")

	steps := []struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}{
		{owner: "owner-a", src: [4]byte{10, 250, 0, 10}, cidr: "10.250.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 250, 1, 10}, cidr: "10.250.1.0/24"},
		{owner: "owner-a", src: [4]byte{10, 250, 2, 10}, cidr: "10.250.2.0/24"},
		{owner: "owner-c", src: [4]byte{10, 250, 3, 10}, cidr: "10.250.3.0/24"},
		{owner: "owner-c", src: [4]byte{10, 250, 4, 10}, cidr: "10.250.4.0/24"},
	}
	dst := [4]byte{10, 250, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.250.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forwarding to dst-smb, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepTrendAgeStabilityLoopOnlyWindowDropsThenRecoversNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1401
		rLoop    RouteID = 1402
		rDst     RouteID = 1403
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.248.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.248.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.248.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.248.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.248.9.0/24")}})
	e.SetEgressSession(rDst, "dst-smb")

	steps := []struct {
		owner SessionKey
		src   [4]byte
		cidr  string
		stall bool
	}{
		{owner: "owner-a", src: [4]byte{10, 248, 0, 10}, cidr: "10.248.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 248, 1, 10}, cidr: "10.248.1.0/24", stall: true},
		{owner: "owner-c", src: [4]byte{10, 248, 2, 10}, cidr: "10.248.2.0/24"},
	}
	dst := [4]byte{10, 248, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.248.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)
		if s.stall {
			e.RemoveRoute(rDst)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rDst, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.248.9.0/24")}})
			e.SetEgressSession(rDst, "dst-smb")
		}
		if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected recovered non-loop forwarding, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepAuthSymptomCompetingRouteChurnWithLoopWindowNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1501
		rLoop    RouteID = 1502
		rDstA    RouteID = 1503
		rDstB    RouteID = 1504
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.247.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		withdrawA  bool
		withdrawB  bool
		expectDest SessionKey
	}{
		{owner: "owner-a", src: [4]byte{10, 247, 0, 10}, cidr: "10.247.0.0/24", expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 247, 1, 10}, cidr: "10.247.1.0/24", withdrawA: true, expectDest: "dst-b"},
		{owner: "owner-c", src: [4]byte{10, 247, 2, 10}, cidr: "10.247.2.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-b", src: [4]byte{10, 247, 3, 10}, cidr: "10.247.3.0/24", expectDest: "dst-a"},
	}
	dst := [4]byte{10, 247, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.withdrawA {
			e.RemoveRoute(rDstA)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
		}
		if s.withdrawB {
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.247.9.0/24")}})
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDest == "" {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in loop-only window, got %+v", i, d)
			}
		} else if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d: expected forward to %q, got %+v", i, s.expectDest, d)
		}

		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepConfidenceBucketStyleLoopGuardMaintainsNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1551
		rLoop    RouteID = 1552
		rDst     RouteID = 1553
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.246.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.246.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.246.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.246.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.246.9.0/24")}})
	e.SetEgressSession(rDst, "dst-smb")

	steps := []struct {
		owner       SessionKey
		src         [4]byte
		cidr        string
		withdrawDst bool
		expectDrop  bool
	}{
		{owner: "owner-a", src: [4]byte{10, 246, 0, 10}, cidr: "10.246.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 246, 1, 10}, cidr: "10.246.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 246, 2, 10}, cidr: "10.246.2.0/24", withdrawDst: true, expectDrop: true},
		{owner: "owner-b", src: [4]byte{10, 246, 3, 10}, cidr: "10.246.3.0/24"},
	}
	dst := [4]byte{10, 246, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.246.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)
		if s.withdrawDst {
			e.RemoveRoute(rDst)
		} else {
			e.UpsertRoute(Route{ID: rDst, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.246.9.0/24")}})
			e.SetEgressSession(rDst, "dst-smb")
		}
		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDrop {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in low-confidence loop-only window, got %+v", i, d)
			}
		} else if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected forward to dst-smb, got %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if d := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); d.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, d)
			}
		}
	}
}

func TestMemEngineOwnerStepConfidenceBucketShiftLoopbackSymptomWindowNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1651
		rLoop    RouteID = 1652
		rDstA    RouteID = 1653
		rDstB    RouteID = 1654
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.243.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		withdrawA  bool
		withdrawB  bool
		expectDest SessionKey
	}{
		{owner: "owner-a", src: [4]byte{10, 243, 0, 10}, cidr: "10.243.0.0/24", expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 243, 1, 10}, cidr: "10.243.1.0/24", withdrawA: true, expectDest: "dst-b"},
		{owner: "owner-c", src: [4]byte{10, 243, 2, 10}, cidr: "10.243.2.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-b", src: [4]byte{10, 243, 3, 10}, cidr: "10.243.3.0/24", expectDest: "dst-a"},
	}
	dst := [4]byte{10, 243, 9, 44}

	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.withdrawA {
			e.RemoveRoute(rDstA)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
		}
		if s.withdrawB {
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.243.9.0/24")}})
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDest == "" {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in bucket-shift loopback symptom window, got %+v", i, d)
			}
		} else if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d: expected non-loop forward to %q, got %+v", i, s.expectDest, d)
		}
		if d.Action == ActionForward && d.EgressSession == s.owner {
			t.Fatalf("step %d: self-loop detected %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepAnomalyClusterStyleRotationAvoidsLoopAndDropsStale(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1601
		rLoop    RouteID = 1602
		rDst     RouteID = 1603
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.249.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.249.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.249.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.249.9.0/24")}})
	e.UpsertRoute(Route{ID: rDst, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.249.9.0/24")}})
	e.SetEgressSession(rDst, "dst-smb")

	steps := []struct {
		owner SessionKey
		src   [4]byte
		cidr  string
	}{
		{owner: "owner-a", src: [4]byte{10, 249, 0, 10}, cidr: "10.249.0.0/24"},
		{owner: "owner-b", src: [4]byte{10, 249, 1, 10}, cidr: "10.249.1.0/24"},
		{owner: "owner-c", src: [4]byte{10, 249, 2, 10}, cidr: "10.249.2.0/24"},
	}
	dst := [4]byte{10, 249, 9, 44}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.249.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != "dst-smb" {
			t.Fatalf("step %d: expected non-loop forward to dst-smb, got %+v", i, d)
		}
		if d.EgressSession == s.owner {
			t.Fatalf("step %d: self-loop detected %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepRiskScorePatternBucketShiftJitterAndLoopbackGuards(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1701
		rLoop    RouteID = 1702
		rDstA    RouteID = 1703
		rDstB    RouteID = 1704
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.251.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		withdrawA  bool
		withdrawB  bool
		expectDest SessionKey
	}{
		{owner: "owner-a", src: [4]byte{10, 251, 0, 10}, cidr: "10.251.0.0/24", expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 251, 1, 10}, cidr: "10.251.1.0/24", withdrawA: true, expectDest: "dst-b"},
		{owner: "owner-c", src: [4]byte{10, 251, 2, 10}, cidr: "10.251.2.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-a", src: [4]byte{10, 251, 3, 10}, cidr: "10.251.3.0/24", expectDest: "dst-a"},
	}
	dst := [4]byte{10, 251, 9, 44}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.withdrawA {
			e.RemoveRoute(rDstA)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
		}
		if s.withdrawB {
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.251.9.0/24")}})
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDest == "" {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in risk-score loopback window, got %+v", i, d)
			}
		} else if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d: expected forward to %q, got %+v", i, s.expectDest, d)
		}
		if d.Action == ActionForward && d.EgressSession == s.owner {
			t.Fatalf("step %d: self-loop detected %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
}

func TestMemEngineOwnerStepSustainedHighRiskWindowDropsWithoutLoopback(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1711
		rLoop    RouteID = 1712
		rDstA    RouteID = 1713
		rDstB    RouteID = 1714
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.252.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		withdrawA  bool
		withdrawB  bool
		expectDest SessionKey
	}{
		{owner: "owner-a", src: [4]byte{10, 252, 0, 10}, cidr: "10.252.0.0/24", expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 252, 1, 10}, cidr: "10.252.1.0/24", withdrawA: true, expectDest: "dst-b"},
		{owner: "owner-c", src: [4]byte{10, 252, 2, 10}, cidr: "10.252.2.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-a", src: [4]byte{10, 252, 3, 10}, cidr: "10.252.3.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-b", src: [4]byte{10, 252, 4, 10}, cidr: "10.252.4.0/24", expectDest: "dst-a"},
	}
	dst := [4]byte{10, 252, 9, 44}
	sustainedDrops := 0
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.withdrawA {
			e.RemoveRoute(rDstA)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
		}
		if s.withdrawB {
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.252.9.0/24")}})
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDest == "" {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in sustained high-risk window, got %+v", i, d)
			}
			sustainedDrops++
		} else if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d: expected forward to %q, got %+v", i, s.expectDest, d)
		}
		if d.Action == ActionForward && d.EgressSession == s.owner {
			t.Fatalf("step %d: self-loop detected %+v", i, d)
		}
	}
	if sustainedDrops < 2 {
		t.Fatalf("expected sustained high-risk drop window (>=2), got %d", sustainedDrops)
	}
}

func TestMemEngineOwnerStepRiskTierQueueStyleChurnKeepsNoSelfLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1721
		rLoop    RouteID = 1722
		rDstA    RouteID = 1723
		rDstB    RouteID = 1724
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.253.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		withdrawA  bool
		withdrawB  bool
		expectDest SessionKey
	}{
		{owner: "owner-a", src: [4]byte{10, 253, 0, 10}, cidr: "10.253.0.0/24", expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 253, 1, 10}, cidr: "10.253.1.0/24", withdrawA: true, expectDest: "dst-b"},
		{owner: "owner-c", src: [4]byte{10, 253, 2, 10}, cidr: "10.253.2.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-a", src: [4]byte{10, 253, 3, 10}, cidr: "10.253.3.0/24", withdrawA: true, withdrawB: true},
		{owner: "owner-b", src: [4]byte{10, 253, 4, 10}, cidr: "10.253.4.0/24", expectDest: "dst-a"},
	}
	dst := [4]byte{10, 253, 9, 44}
	forwardCount := 0
	dropCount := 0
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		if s.withdrawA {
			e.RemoveRoute(rDstA)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
		}
		if s.withdrawB {
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.253.9.0/24")}})
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if s.expectDest == "" {
			if d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in risk-tier churn window, got %+v", i, d)
			}
			dropCount++
		} else if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d: expected forward to %q, got %+v", i, s.expectDest, d)
		} else {
			forwardCount++
		}
		if d.Action == ActionForward && d.EgressSession == s.owner {
			t.Fatalf("step %d: self-loop detected %+v", i, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if dPrev := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); dPrev.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop, got %+v", i, prev.owner, dPrev)
			}
		}
	}
	if forwardCount < 3 || dropCount < 2 {
		t.Fatalf("expected mixed tier behavior (forward>=3 drop>=2), got forward=%d drop=%d", forwardCount, dropCount)
	}
}

func TestMemEngineOwnerStepRiskTierJitterBudgetWindowKeepsDeterministicNoLoop(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1731
		rLoop    RouteID = 1732
		rDstA    RouteID = 1733
		rDstB    RouteID = 1734
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.254.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	steps := []struct {
		owner      SessionKey
		src        [4]byte
		cidr       string
		tier       string
		jitter     bool
		expectDest SessionKey
	}{
		{owner: "owner-c", src: [4]byte{10, 254, 3, 10}, cidr: "10.254.3.0/24", tier: "P3", jitter: true, expectDest: "dst-a"},
		{owner: "owner-b", src: [4]byte{10, 254, 2, 10}, cidr: "10.254.2.0/24", tier: "P2", jitter: true, expectDest: "dst-a"},
		{owner: "owner-a", src: [4]byte{10, 254, 1, 10}, cidr: "10.254.1.0/24", tier: "P1", jitter: false, expectDest: "dst-a"},
	}
	dst := [4]byte{10, 254, 9, 40}
	for i, s := range steps {
		e.UpsertRoute(Route{
			ID:               rIngress,
			AllowedSrc:       []netip.Prefix{netip.MustParsePrefix(s.cidr)},
			AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")},
			ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix(s.cidr)},
		})
		e.SetIngressSession(rIngress, s.owner)
		e.SetEgressSession(rLoop, s.owner)

		// Jitter-window must never collapse into self-loop; loop-only path is allowed only as drop.
		if s.jitter && s.tier != "P3" {
			e.RemoveRoute(rDstA)
			e.RemoveRoute(rDstB)
			if d := e.HandleIngress(makeIPv4(s.src, dst), s.owner); d.Action != ActionDrop {
				t.Fatalf("step %d (%s): expected drop in loop-only jitter window, got %+v", i, s.tier, d)
			}
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")}})
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.254.9.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(s.src, dst), s.owner)
		if d.Action != ActionForward || d.EgressSession != s.expectDest {
			t.Fatalf("step %d (%s): expected deterministic forward to %q, got %+v", i, s.tier, s.expectDest, d)
		}
		if d.EgressSession == s.owner {
			t.Fatalf("step %d (%s): self-loop detected %+v", i, s.tier, d)
		}
		if i > 0 {
			prev := steps[i-1]
			if stale := e.HandleIngress(makeIPv4(prev.src, dst), prev.owner); stale.Action != ActionDrop {
				t.Fatalf("step %d: stale owner %q must drop after rebind, got %+v", i, prev.owner, stale)
			}
		}
	}
}

func TestMemEngineOwnerStepDeterministicQueueTieBreakWindowNoLoopAndStableEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1741
		rLoop    RouteID = 1742
		rDstLow  RouteID = 1743
		rDstHigh RouteID = 1744
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.255.0.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.0.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstLow, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")}})
	e.UpsertRoute(Route{ID: rDstHigh, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")}})
	e.SetEgressSession(rDstLow, "dst-low")
	e.SetEgressSession(rDstHigh, "dst-high")
	src := [4]byte{10, 255, 0, 11}
	dst := [4]byte{10, 255, 9, 41}
	owners := []SessionKey{"owner-c", "owner-b", "owner-a", "owner-c", "owner-a", "owner-b"}
	expected := []SessionKey{"dst-low", "dst-high", "dst-low", "dst-high", "dst-low", "dst-high"}
	for i, owner := range owners {
		e.SetIngressSession(rIngress, owner)
		e.SetEgressSession(rLoop, owner)
		if i%2 == 0 {
			e.RemoveRoute(rDstHigh)
			e.UpsertRoute(Route{ID: rDstLow, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")}})
			e.SetEgressSession(rDstLow, "dst-low")
		} else {
			e.RemoveRoute(rDstLow)
			e.UpsertRoute(Route{ID: rDstHigh, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.9.0/24")}})
			e.SetEgressSession(rDstHigh, "dst-high")
		}
		d := e.HandleIngress(makeIPv4(src, dst), owner)
		if d.Action != ActionForward || d.EgressSession != expected[i] {
			t.Fatalf("step %d: expected stable forward to %q, got %+v", i, expected[i], d)
		}
		if d.EgressSession == owner {
			t.Fatalf("step %d: loopback chosen for %q, got %+v", i, owner, d)
		}
	}
}

func TestMemEngineOwnerStepJitterBudgetOverrunWindowDropsLoopOnlyAndRecoversDeterministically(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1751
		rLoop    RouteID = 1752
		rDstLow  RouteID = 1753
		rDstHigh RouteID = 1754
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.255.10.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.10.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
	e.UpsertRoute(Route{ID: rDstLow, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
	e.UpsertRoute(Route{ID: rDstHigh, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
	e.SetEgressSession(rDstLow, "dst-low")
	e.SetEgressSession(rDstHigh, "dst-high")
	src := [4]byte{10, 255, 10, 11}
	dst := [4]byte{10, 255, 11, 41}
	owners := []SessionKey{"owner-a", "owner-b", "owner-c", "owner-a", "owner-b", "owner-c"}

	for i, owner := range owners {
		e.SetIngressSession(rIngress, owner)
		e.SetEgressSession(rLoop, owner)
		if i < 3 {
			// Jitter budget overrun window: only loop candidate is present, must drop.
			e.RemoveRoute(rDstLow)
			e.RemoveRoute(rDstHigh)
			if d := e.HandleIngress(makeIPv4(src, dst), owner); d.Action != ActionDrop {
				t.Fatalf("step %d: expected drop in jitter overrun loop-only window, got %+v", i, d)
			}
			e.UpsertRoute(Route{ID: rDstLow, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
			e.UpsertRoute(Route{ID: rDstHigh, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
			e.SetEgressSession(rDstLow, "dst-low")
			e.SetEgressSession(rDstHigh, "dst-high")
			continue
		}
		if i%2 == 0 {
			e.RemoveRoute(rDstHigh)
			e.UpsertRoute(Route{ID: rDstLow, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
			e.SetEgressSession(rDstLow, "dst-low")
		} else {
			e.RemoveRoute(rDstLow)
			e.UpsertRoute(Route{ID: rDstHigh, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.11.0/24")}})
			e.SetEgressSession(rDstHigh, "dst-high")
		}
		d := e.HandleIngress(makeIPv4(src, dst), owner)
		if d.Action != ActionForward || d.EgressSession == owner {
			t.Fatalf("step %d: expected non-loop forward after recovery, got %+v", i, d)
		}
	}
}

func TestMemEngineOwnerStepBudgetOverrunQueueOrderCorrelationNoSelfEgress(t *testing.T) {
	e := NewMemEngine()
	const (
		rIngress RouteID = 1761
		rLoop    RouteID = 1762
		rDstA    RouteID = 1763
		rDstB    RouteID = 1764
	)
	e.UpsertRoute(Route{
		ID:               rIngress,
		AllowedSrc:       []netip.Prefix{netip.MustParsePrefix("10.255.20.0/24")},
		AllowedDst:       []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")},
		ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.20.0/24")},
	})
	e.UpsertRoute(Route{ID: rLoop, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")}})
	e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")}})
	e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")}})
	e.SetEgressSession(rDstA, "dst-a")
	e.SetEgressSession(rDstB, "dst-b")

	type ownerStep struct {
		owner        SessionKey
		cpResult     string
		queueScore   int
		expectAction Action
		expectEgress SessionKey
	}
	steps := []ownerStep{
		{owner: "owner-a", cpResult: "fail", queueScore: 95, expectAction: ActionDrop},
		{owner: "owner-b", cpResult: "ok-soft-timeout", queueScore: 70, expectAction: ActionDrop},
		{owner: "owner-c", cpResult: "ok", queueScore: 30, expectAction: ActionForward, expectEgress: "dst-a"},
	}
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].queueScore == steps[j].queueScore {
			return steps[i].owner < steps[j].owner
		}
		return steps[i].queueScore > steps[j].queueScore
	})

	dst := [4]byte{10, 255, 21, 50}
	srcByOwner := map[SessionKey][4]byte{
		"owner-a": {10, 255, 20, 11},
		"owner-b": {10, 255, 20, 12},
		"owner-c": {10, 255, 20, 13},
	}
	for i, step := range steps {
		e.SetIngressSession(rIngress, step.owner)
		e.SetEgressSession(rLoop, step.owner)
		if step.cpResult == "fail" || step.cpResult == "ok-soft-timeout" {
			e.RemoveRoute(rDstA)
			e.RemoveRoute(rDstB)
		} else {
			e.UpsertRoute(Route{ID: rDstA, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")}})
			e.UpsertRoute(Route{ID: rDstB, ExportedPrefixes: []netip.Prefix{netip.MustParsePrefix("10.255.21.0/24")}})
			e.SetEgressSession(rDstA, "dst-a")
			e.SetEgressSession(rDstB, "dst-b")
		}

		d := e.HandleIngress(makeIPv4(srcByOwner[step.owner], dst), step.owner)
		if d.Action != step.expectAction {
			t.Fatalf("step %d(%s): expected action=%v, got %+v", i, step.owner, step.expectAction, d)
		}
		if step.expectAction == ActionForward && d.EgressSession != step.expectEgress {
			t.Fatalf("step %d(%s): expected egress=%q, got %+v", i, step.owner, step.expectEgress, d)
		}
		if d.Action == ActionForward && d.EgressSession == step.owner {
			t.Fatalf("step %d(%s): self-loop detected %+v", i, step.owner, d)
		}
	}
}

func makeIPv4(src, dst [4]byte) []byte {
	b := make([]byte, 20)
	b[0] = 0x45
	b[9] = 0 // proto
	copy(b[12:16], src[:])
	copy(b[16:20], dst[:])
	return b
}

func makeIPv6(src, dst netip.Addr) []byte {
	b := make([]byte, 40)
	b[0] = 0x60
	src16 := src.As16()
	dst16 := dst.As16()
	copy(b[8:24], src16[:])
	copy(b[24:40], dst16[:])
	return b
}
