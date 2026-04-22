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

