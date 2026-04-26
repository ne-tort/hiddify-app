package service

import "testing"

func TestValidateAndNormalizeWGFamilyOptions_TwoExitPeers(t *testing.T) {
	opt := map[string]interface{}{
		"address":     []string{"10.8.0.1/24"},
		"private_key": "k",
		"listen_port": float64(51820),
		"peers": []map[string]interface{}{
			{"public_key": "a", "allowed_ips": []string{"10.8.0.2/32"}, "peer_exit": true},
			{"public_key": "b", "allowed_ips": []string{"10.8.0.3/32"}, "peer_exit": true},
		},
	}
	err := validateAndNormalizeWGFamilyOptions(opt, wireGuardType)
	if err == nil {
		t.Fatal("expected error for two exit peers")
	}
}

func TestValidateAndNormalizeWGFamilyOptions_HubClientMode(t *testing.T) {
	opt := map[string]interface{}{
		"address":         []string{"10.5.0.2/32"},
		"private_key":     "k",
		"hub_client_mode": true,
		"peers": []map[string]interface{}{
			{
				"public_key":  "hubpub",
				"address":     "198.51.100.1",
				"port":        float64(30387),
				"allowed_ips": []string{"10.5.0.0/24"},
			},
		},
	}
	if err := validateAndNormalizeWGFamilyOptions(opt, awgType); err != nil {
		t.Fatal(err)
	}
	if opt["listen_port"] != nil {
		t.Fatalf("listen_port should be cleared: %#v", opt["listen_port"])
	}
}

func TestMergeExitPeerAllowedIPs(t *testing.T) {
	out := mergeExitPeerAllowedIPs([]string{"10.8.0.2/32"})
	has4, has6 := false, false
	for _, s := range out {
		if s == "0.0.0.0/0" {
			has4 = true
		}
		if s == "::/0" {
			has6 = true
		}
	}
	if !has4 || !has6 {
		t.Fatalf("merge: %#v", out)
	}
}

func TestStripExitPeerAllowedIPs(t *testing.T) {
	out := stripExitPeerAllowedIPs([]string{"10.8.0.2/32", "0.0.0.0/0", "::/0"})
	if len(out) != 1 || out[0] != "10.8.0.2/32" {
		t.Fatalf("strip: %#v", out)
	}
}

func TestValidateAndNormalizeWGFamilyOptions_AWGKernelFlagsDefaults(t *testing.T) {
	opt := map[string]interface{}{
		"system":  true,
		"address": []string{"10.5.0.1/24"},
	}
	if err := validateAndNormalizeWGFamilyOptions(opt, awgType); err != nil {
		t.Fatal(err)
	}
	if got, ok := opt["gso_enabled"].(bool); !ok || !got {
		t.Fatalf("expected gso_enabled=true by default, got %#v", opt["gso_enabled"])
	}
	if got, ok := opt["kernel_path_enabled"].(bool); !ok || got {
		t.Fatalf("expected kernel_path_enabled=false by default, got %#v", opt["kernel_path_enabled"])
	}
}

func TestValidateAndNormalizeWGFamilyOptions_AWGKernelFlagsClearedInUserspace(t *testing.T) {
	opt := map[string]interface{}{
		"system":              false,
		"gso_enabled":         false,
		"kernel_path_enabled": true,
		"address":             []string{"10.5.0.1/24"},
	}
	if err := validateAndNormalizeWGFamilyOptions(opt, awgType); err != nil {
		t.Fatal(err)
	}
	if _, ok := opt["gso_enabled"]; ok {
		t.Fatalf("gso_enabled should be removed in userspace mode, got %#v", opt["gso_enabled"])
	}
	if _, ok := opt["kernel_path_enabled"]; ok {
		t.Fatalf("kernel_path_enabled should be removed in userspace mode, got %#v", opt["kernel_path_enabled"])
	}
}
