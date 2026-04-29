package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestParseWGForwardSpecFromEndpoint_Disabled(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": false,
		"name":          "wg2",
	})
	ep := &model.Endpoint{Id: 10, Type: "wireguard", Options: opt}
	_, ok := parseWGForwardSpecFromEndpoint(ep)
	if ok {
		t.Fatal("expected disabled endpoint to be skipped")
	}
}

func TestParseWGForwardSpecFromEndpoint_ExplicitName(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
		"name":          "wg2",
	})
	ep := &model.Endpoint{Id: 11, Type: "wireguard", Options: opt}
	spec, ok := parseWGForwardSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected endpoint spec")
	}
	if spec.IIF != "wg2*" || spec.OIF != "wg2*" {
		t.Fatalf("unexpected interface match: %#v", spec)
	}
}

func TestParseWGForwardSpecFromEndpoint_AutoMaskWhenNoName(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
	})
	ep := &model.Endpoint{Id: 12, Type: "wireguard", Options: opt}
	spec, ok := parseWGForwardSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected endpoint spec")
	}
	if spec.IIF != "wg*" || spec.OIF != "wg*" {
		t.Fatalf("expected wg* mask, got %#v", spec)
	}
}

func TestParseWGForwardSpecFromEndpoint_AwgAutoMaskWhenNoName(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
	})
	ep := &model.Endpoint{Id: 14, Type: "awg", Options: opt}
	spec, ok := parseWGForwardSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected endpoint spec")
	}
	if spec.IIF != "awg*" || spec.OIF != "awg*" {
		t.Fatalf("expected awg* mask, got %#v", spec)
	}
}

func TestParseWGForwardSpecFromEndpoint_AwgPrefixNameGetsWildcard(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
		"name":          "awg",
	})
	ep := &model.Endpoint{Id: 22, Type: "awg", Options: opt}
	spec, ok := parseWGForwardSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected endpoint spec")
	}
	if spec.IIF != "awg*" || spec.OIF != "awg*" {
		t.Fatalf("expected awg* mask for awg prefix name, got %#v", spec)
	}
}

func TestParseWGForwardSpecFromEndpoint_RejectsInvalidName(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
		"name":          "wg2;rm -rf /",
	})
	ep := &model.Endpoint{Id: 13, Type: "wireguard", Options: opt}
	_, ok := parseWGForwardSpecFromEndpoint(ep)
	if ok {
		t.Fatal("expected invalid interface name to be rejected")
	}
}

func TestParseWGForwardSpecFromEndpoint_RejectsUnsupportedEndpointType(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"forward_allow": true,
		"name":          "wg2",
	})
	ep := &model.Endpoint{Id: 15, Type: "vless", Options: opt}
	_, ok := parseWGForwardSpecFromEndpoint(ep)
	if ok {
		t.Fatal("expected unsupported endpoint type to be skipped")
	}
}

func TestParseWGInternetSpecFromEndpoint_DefaultEnabled(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"address": []interface{}{"10.5.0.1/24"},
	})
	ep := &model.Endpoint{Id: 16, Type: "awg", Options: opt}
	spec, ok := parseWGInternetSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected internet spec with default-enabled flag")
	}
	if spec.IIF != "awg*" {
		t.Fatalf("expected awg* interface mask, got %#v", spec)
	}
	if spec.SourceCIDR != "10.5.0.0/24" {
		t.Fatalf("expected masked source cidr 10.5.0.0/24, got %q", spec.SourceCIDR)
	}
}

func TestParseWGInternetSpecFromEndpoint_PrefixNameGetsWildcard(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"address": []interface{}{"10.5.0.1/24"},
		"name":    "awg",
	})
	ep := &model.Endpoint{Id: 23, Type: "awg", Options: opt}
	spec, ok := parseWGInternetSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected internet spec")
	}
	if spec.IIF != "awg*" {
		t.Fatalf("expected awg* interface mask for awg prefix name, got %#v", spec)
	}
}

func TestParseWGInternetSpecFromEndpoint_Disabled(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"internet_allow": false,
		"address":        []interface{}{"10.5.0.1/24"},
	})
	ep := &model.Endpoint{Id: 17, Type: "awg", Options: opt}
	_, ok := parseWGInternetSpecFromEndpoint(ep)
	if ok {
		t.Fatal("expected disabled internet spec to be skipped")
	}
}

func TestParseWGInternetSpecFromEndpoint_HubClientModeUsesPeerAllowedRange(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"hub_client_mode": true,
		"address":         []interface{}{"10.5.0.2/32"},
		"peers": []interface{}{
			map[string]interface{}{
				"allowed_ips": []interface{}{"10.5.0.0/24", "0.0.0.0/0"},
			},
		},
	})
	ep := &model.Endpoint{Id: 24, Type: "awg", Options: opt}
	spec, ok := parseWGInternetSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected internet spec in hub_client_mode")
	}
	if spec.SourceCIDR != "10.5.0.0/24" {
		t.Fatalf("expected source cidr from peer allowed_ips, got %q", spec.SourceCIDR)
	}
}

func TestBoolFromAny(t *testing.T) {
	cases := []struct {
		in   interface{}
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"false", false},
		{1, true},
		{0, false},
		{float64(1), true},
		{float64(0), false},
	}
	for i, tc := range cases {
		if got := boolFromAny(tc.in); got != tc.want {
			t.Fatalf("case %d: expected %v got %v", i, tc.want, got)
		}
	}
}

func TestParseNFTJumpHandles(t *testing.T) {
	dump := `
table ip filter {
	chain DOCKER-USER {
		jump SUI_WG_FORWARD comment "sui-wg-forward-jump" # handle 30
		jump SUI_WG_FORWARD comment "sui-wg-forward-jump" # handle 31
		jump OTHER_CHAIN # handle 40
	}
}
`
	got := parseNFTJumpHandles(dump, "SUI_WG_FORWARD")
	want := []int{30, 31}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected handles: got=%v want=%v", got, want)
	}
}

func TestLoadWireGuardRuleSpecs_IncludesAwgAndSortsByEndpointID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	wgOpt, _ := json.Marshal(map[string]interface{}{"forward_allow": true, "internet_allow": true, "address": []interface{}{"10.8.0.1/24"}})
	awgOpt, _ := json.Marshal(map[string]interface{}{"forward_allow": true, "internet_allow": true, "address": []interface{}{"10.5.0.1/24"}})
	if err := db.Create(&model.Endpoint{Id: 20, Type: "wireguard", Tag: "wg-a", Options: wgOpt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Endpoint{Id: 10, Type: "awg", Tag: "awg-a", Options: awgOpt}).Error; err != nil {
		t.Fatal(err)
	}
	specs, internetSpecs, err := loadWireGuardRuleSpecs(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].EndpointID != 10 || specs[1].EndpointID != 20 {
		t.Fatalf("expected sorted ids [10 20], got [%d %d]", specs[0].EndpointID, specs[1].EndpointID)
	}
	if specs[0].IIF != "awg*" || specs[1].IIF != "wg*" {
		t.Fatalf("unexpected interface masks: %#v", specs)
	}
	if len(internetSpecs) != 2 {
		t.Fatalf("expected 2 internet specs, got %d", len(internetSpecs))
	}
	if internetSpecs[0].SourceCIDR != "10.5.0.0/24" || internetSpecs[1].SourceCIDR != "10.8.0.0/24" {
		t.Fatalf("unexpected internet source cidrs: %#v", internetSpecs)
	}
}

