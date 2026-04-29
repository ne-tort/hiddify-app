package util

import "testing"

func TestNaiveOut_TCPForcesQUICFalse(t *testing.T) {
	out := map[string]interface{}{
		"type":                    "naive",
		"tag":                     "naive-1",
		"quic":                    true,
		"quic_congestion_control": "bbr2",
	}
	in := map[string]interface{}{"network": "tcp"}
	ApplyNaiveOutboundFromInbound(&out, in)
	if got, ok := out["quic"].(bool); !ok || got {
		t.Fatalf("quic must be explicit false for tcp, got %#v", out["quic"])
	}
	if _, ok := out["quic_congestion_control"]; ok {
		t.Fatalf("quic_congestion_control must be removed for tcp")
	}
}

func TestNaiveOut_UDPSetsQUIC(t *testing.T) {
	out := map[string]interface{}{"type": "naive", "tag": "n1"}
	in := map[string]interface{}{
		"network":                 "udp",
		"quic_congestion_control": "bbr2_variant",
	}
	ApplyNaiveOutboundFromInbound(&out, in)
	if out["quic"] != true {
		t.Fatalf("expected quic true for udp, got %#v", out["quic"])
	}
	if out["quic_congestion_control"] != "bbr2" {
		t.Fatalf("expected bbr2 mapping, got %#v", out["quic_congestion_control"])
	}
}

func TestNaiveOut_EmptyNetworkDefaultTCP(t *testing.T) {
	out := map[string]interface{}{"type": "naive", "quic": true}
	in := map[string]interface{}{}
	ApplyNaiveOutboundFromInbound(&out, in)
	if got, ok := out["quic"].(bool); !ok || got {
		t.Fatalf("empty network should default to tcp and set quic=false, got %#v", out["quic"])
	}
	if got, ok := out["force_ipv4_dns"].(bool); !ok || !got {
		t.Fatalf("force_ipv4_dns should default to true, got %#v", out["force_ipv4_dns"])
	}
}

func TestNaiveOut_KeepForceIPv4DNSOverride(t *testing.T) {
	out := map[string]interface{}{"type": "naive", "force_ipv4_dns": false}
	in := map[string]interface{}{"network": "tcp"}
	ApplyNaiveOutboundFromInbound(&out, in)
	if got, ok := out["force_ipv4_dns"].(bool); !ok || got {
		t.Fatalf("explicit force_ipv4_dns=false must be preserved, got %#v", out["force_ipv4_dns"])
	}
}
