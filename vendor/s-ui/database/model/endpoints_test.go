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
		"internet_allow":   false,
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
	if strings.Contains(string(out), "internet_allow") {
		t.Fatalf("internet_allow leaked to runtime json: %s", string(out))
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

func TestEndpointMarshalJSON_MasqueStripsMemberLists(t *testing.T) {
	options := map[string]interface{}{
		"mode":               "server",
		"listen":             "0.0.0.0",
		"listen_port":        float64(8443),
		"transport_mode":     "connect_udp",
		"member_client_ids":  []interface{}{float64(1), float64(2)},
		"member_group_ids":   []interface{}{float64(3)},
		"server_auth":        map[string]interface{}{"policy": "first_match"},
		"sui_tls_id":         float64(9),
	}
	raw, _ := json.Marshal(options)
	ep := Endpoint{Type: "masque", Tag: "mq-test", Options: raw}
	out, err := ep.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "sui_tls_id") {
		t.Fatalf("sui_tls_id leaked: %s", s)
	}
	if strings.Contains(s, "member_client_ids") {
		t.Fatalf("member_client_ids leaked: %s", s)
	}
	if strings.Contains(s, "member_group_ids") {
		t.Fatalf("member_group_ids leaked: %s", s)
	}
	if !strings.Contains(s, "server_auth") {
		t.Fatalf("expected server_auth in server runtime json: %s", s)
	}
}

func TestEndpointMarshalJSON_WarpMasqueStripsMemberLists(t *testing.T) {
	options := map[string]interface{}{
		"mode":              "client",
		"server":            "example.com",
		"server_port":       float64(443),
		"member_client_ids": []interface{}{float64(1)},
		"profile":           map[string]interface{}{"compatibility": "consumer"},
	}
	raw, _ := json.Marshal(options)
	ep := Endpoint{Type: "warp_masque", Tag: "wm-test", Options: raw}
	out, err := ep.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var outMap map[string]interface{}
	if err := json.Unmarshal(out, &outMap); err != nil {
		t.Fatal(err)
	}
	if outMap["type"] != "warp_masque" {
		t.Fatalf("expected type warp_masque, got %v", outMap["type"])
	}
	if _, ok := outMap["member_client_ids"]; ok {
		t.Fatal("member_client_ids leaked in warp_masque marshal")
	}
}

func TestEndpointMarshalJSON_WireGuardStripsHubClientAndPeerExit(t *testing.T) {
	options := map[string]interface{}{
		"address":         []string{"10.8.0.2/32"},
		"private_key":     "k",
		"hub_client_mode": true,
		"peers": []map[string]interface{}{
			{
				"public_key":  "hubpk",
				"address":     "1.2.3.4",
				"port":        float64(51820),
				"peer_exit":   true,
				"allowed_ips": []string{"10.8.0.0/24"},
			},
		},
	}
	raw, _ := json.Marshal(options)
	ep := Endpoint{Type: "wireguard", Tag: "wg-c", Options: raw}
	out, err := ep.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "hub_client_mode") {
		t.Fatalf("hub_client_mode leaked: %s", s)
	}
	if strings.Contains(s, "peer_exit") {
		t.Fatalf("peer_exit leaked: %s", s)
	}
}

