package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alireza0/s-ui/database/model"
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
	if spec.IIF != "wg2" || spec.OIF != "wg2" {
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

