package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
)

func TestMergeWarpMasqueOptionsWithExt(t *testing.T) {
	opt := []byte(`{"type":"warp_masque","tag":"t1","profile":{"compatibility":"consumer"}}`)
	ext := []byte(`{"auth_token":"tok","private_key":"pk"}`)
	out, err := MergeWarpMasqueOptionsWithExt(opt, ext)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	prof, ok := m["profile"].(map[string]interface{})
	if !ok {
		t.Fatalf("profile: %#v", m["profile"])
	}
	if prof["auth_token"] != "tok" || prof["private_key"] != "pk" {
		t.Fatalf("ext not merged into profile: %#v", prof)
	}
	if prof["compatibility"] != "consumer" {
		t.Fatalf("lost existing profile field: %#v", prof)
	}
}

func TestMergeWarpMasqueOptionsWithExtSyncsLicenseFromLicenseKey(t *testing.T) {
	opt := []byte(`{"type":"warp_masque","tag":"t1","profile":{"compatibility":"consumer","license":"OLD","private_key":"pk"}}`)
	ext := []byte(`{"license_key":"NEW","access_token":"tok","device_id":"dev"}`)
	out, err := MergeWarpMasqueOptionsWithExt(opt, ext)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	prof, ok := m["profile"].(map[string]interface{})
	if !ok {
		t.Fatalf("profile: %#v", m["profile"])
	}
	if prof["license"] != "NEW" {
		t.Fatalf("expected license synced from license_key, got %#v", prof["license"])
	}
	if _, ok := prof["license_key"]; ok {
		t.Fatalf("license_key should be stripped from profile, got %#v", prof)
	}
	if _, ok := prof["access_token"]; ok {
		t.Fatalf("access_token should be stripped from profile")
	}
	if _, ok := prof["device_id"]; ok {
		t.Fatalf("device_id should be stripped from profile")
	}
	if prof["private_key"] != "pk" {
		t.Fatalf("private_key: %#v", prof["private_key"])
	}
}

func TestWarpMasqueNeedsCloudflareRegister_licenseInExtOnly(t *testing.T) {
	ep := &model.Endpoint{
		Options: json.RawMessage(`{"profile":{"compatibility":"consumer","private_key":"abc"}}`),
		Ext:     json.RawMessage(`{"license_key":"LIC","device_id":"x"}`),
	}
	if warpMasqueNeedsCloudflareRegister(ep) {
		t.Fatal("expected false when license_key in ext and private_key in profile")
	}
}
