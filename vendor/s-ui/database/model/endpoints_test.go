package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndpointMarshalJSON_WireGuardStripsForwardAllow(t *testing.T) {
	options := map[string]interface{}{
		"address":          []string{"10.8.0.1/24"},
		"private_key":      "k",
		"listen_port":      51820,
		"forward_allow":    true,
		"cloak_enabled":    true,
		"cloak_detour_tag": "vless-main",
		"peers": []map[string]interface{}{
			{
				"public_key":  "p",
				"allowed_ips": []string{"10.8.0.2/32"},
			},
		},
	}
	raw, _ := json.Marshal(options)
	ep := Endpoint{
		Type:    "wireguard",
		Tag:     "wg-test",
		Options: raw,
	}

	out, err := ep.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(out), "forward_allow") {
		t.Fatalf("forward_allow leaked to runtime json: %s", string(out))
	}
	if strings.Contains(string(out), "cloak_enabled") {
		t.Fatalf("cloak_enabled leaked to runtime json: %s", string(out))
	}
	if strings.Contains(string(out), "cloak_detour_tag") {
		t.Fatalf("cloak_detour_tag leaked to runtime json: %s", string(out))
	}
}

func TestEndpointMarshalJSON_AwgStripsUIPeersAndProfileRef(t *testing.T) {
	options := map[string]interface{}{
		"address":                  []string{"10.9.0.1/24"},
		"private_key":              "srv",
		"listen_port":              51821,
		"obfuscation_profile_id":   float64(3),
		"member_client_ids":        []interface{}{float64(1)},
		"jc":                       float64(4),
		"peers": []map[string]interface{}{
			{
				"public_key": "pk", "allowed_ips": []string{"10.9.0.2/32"},
				"client_id": float64(1), "managed": true, "private_key": "sec",
			},
		},
	}
	raw, _ := json.Marshal(options)
	ep := Endpoint{Type: "awg", Tag: "awg-test", Options: raw}
	out, err := ep.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "obfuscation_profile_id") {
		t.Fatalf("obfuscation_profile_id leaked: %s", s)
	}
	if strings.Contains(s, "member_client_ids") {
		t.Fatalf("member_client_ids leaked: %s", s)
	}
	if strings.Contains(s, `"managed"`) {
		t.Fatalf("managed leaked: %s", s)
	}
	if !strings.Contains(s, "jc") {
		t.Fatalf("expected jc in output: %s", s)
	}
}

