package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alireza0/s-ui/config"
	"github.com/alireza0/s-ui/database/model"
)

// MergeWarpMasqueOptionsWithExt merges JSON from Endpoint.Ext into Options["profile"]
// so sing-box runtime receives a single profile object (Ext holds secrets like legacy WARP).
func MergeWarpMasqueOptionsWithExt(options json.RawMessage, ext json.RawMessage) (json.RawMessage, error) {
	if len(ext) == 0 {
		return options, nil
	}
	var opt map[string]interface{}
	if len(options) > 0 {
		if err := json.Unmarshal(options, &opt); err != nil {
			return nil, fmt.Errorf("warp_masque options: %w", err)
		}
	}
	if opt == nil {
		opt = make(map[string]interface{})
	}
	var extMap map[string]interface{}
	if err := json.Unmarshal(ext, &extMap); err != nil {
		return nil, fmt.Errorf("warp_masque ext: %w", err)
	}
	prof, ok := opt["profile"].(map[string]interface{})
	if !ok || prof == nil {
		prof = make(map[string]interface{})
		opt["profile"] = prof
	}
	for k, v := range extMap {
		prof[k] = v
	}
	finalizeWarpMasqueMergedProfile(prof)
	out, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// finalizeWarpMasqueMergedProfile aligns s-ui Ext (legacy WARP shape) with sing-box
// WarpMasqueProfileOptions JSON: consumer bootstrap reads profile.license / profile.private_key,
// not license_key. Ext also stores access_token and device_id for SetWarpLicense — those are
// not sing-box profile fields and are stripped after merge so they do not shadow auth_token/id.
func finalizeWarpMasqueMergedProfile(prof map[string]interface{}) {
	if prof == nil {
		return
	}
	if lk := strings.TrimSpace(fmt.Sprint(prof["license_key"])); lk != "" {
		prof["license"] = lk
	}
	delete(prof, "license_key")
	delete(prof, "access_token")
	delete(prof, "device_id")
	lic := strings.TrimSpace(fmt.Sprint(prof["license"]))
	if lic == "" || strings.EqualFold(lic, "<nil>") || strings.EqualFold(lic, "nil") {
		delete(prof, "license")
	}
}

// DefaultWarpMasqueStatePath returns a persistent JSON path next to the panel DB (e.g. /app/db on Docker stand)
// so Cloudflare device tokens and MASQUE ECDSA keys survive container restarts.
func DefaultWarpMasqueStatePath(tag string) string {
	dir := filepath.Dir(config.GetDBPath())
	return filepath.Join(dir, "warp_masque_"+warpMasqueSafeFileToken(tag)+"_state.json")
}

func warpMasqueSafeFileToken(tag string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(tag) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "default"
	}
	return s
}

// RegisterWarpMasque runs the same Cloudflare consumer registration as legacy WARP
// (RegisterWarp on a temporary wireguard-shaped endpoint), then writes sing-box warp_masque
// options: license + private_key, auto MASQUE ECDSA enroll, persistent warp_masque_state_path next to the panel DB.
func (s *WarpService) RegisterWarpMasque(ep *model.Endpoint) error {
	preserved := preserveWarpMasqueOptionsFrom(ep.Options)
	var oldProf map[string]interface{}
	if len(ep.Options) > 0 {
		var om map[string]interface{}
		if err := json.Unmarshal(ep.Options, &om); err == nil {
			if p, ok := om["profile"].(map[string]interface{}); ok && p != nil {
				oldProf = p
			}
		}
	}
	tmp := &model.Endpoint{
		Type:    "warp",
		Tag:     ep.Tag,
		Options: json.RawMessage(`{}`),
		Ext:     nil,
	}
	if err := s.RegisterWarp(tmp); err != nil {
		return err
	}
	ep.Ext = tmp.Ext

	var wgOpt map[string]interface{}
	if err := json.Unmarshal(tmp.Options, &wgOpt); err != nil {
		return err
	}
	priv, _ := wgOpt["private_key"].(string)
	var extMap map[string]interface{}
	if err := json.Unmarshal(tmp.Ext, &extMap); err != nil {
		return err
	}
	licenseKey, _ := extMap["license_key"].(string)

	statePath := DefaultWarpMasqueStatePath(ep.Tag)
	if oldProf != nil {
		if sp := strings.TrimSpace(fmt.Sprint(oldProf["warp_masque_state_path"])); sp != "" {
			statePath = sp
		}
	}
	prof := map[string]interface{}{
		"license":                strings.TrimSpace(licenseKey),
		"private_key":            strings.TrimSpace(priv),
		"auto_enroll_masque":     true,
		"warp_masque_state_path": statePath,
	}
	if oldProf != nil {
		for _, k := range []string{"detour", "dataplane_port_strategy", "dataplane_port", "masque_device_name", "recreate", "disable_masque_peer_public_key_pin"} {
			if v, ok := oldProf[k]; ok {
				prof[k] = v
			}
		}
	}

	wm := map[string]interface{}{
		"transport_mode": "connect_udp",
		"http_layer":     "auto",
		"profile":        prof,
	}
	for k, v := range preserved {
		wm[k] = v
	}
	raw, err := json.MarshalIndent(wm, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

func preserveWarpMasqueOptionsFrom(options json.RawMessage) map[string]interface{} {
	out := make(map[string]interface{})
	if len(options) == 0 {
		return out
	}
	var m map[string]interface{}
	if err := json.Unmarshal(options, &m); err != nil {
		return out
	}
	for _, k := range []string{
		"transport_mode", "template_udp", "template_ip", "template_tcp",
		"http_layer", "listen", "listen_port", "mode",
		"server", "server_port", "detour",
	} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func warpMasqueNeedsCloudflareRegister(ep *model.Endpoint) bool {
	if ep == nil || len(ep.Options) == 0 {
		return true
	}
	var m map[string]interface{}
	if err := json.Unmarshal(ep.Options, &m); err != nil {
		return true
	}
	prof, _ := m["profile"].(map[string]interface{})
	if prof == nil {
		return true
	}
	tok := strings.TrimSpace(fmt.Sprint(prof["auth_token"]))
	id := strings.TrimSpace(fmt.Sprint(prof["id"]))
	if tok != "" && id != "" {
		return false
	}
	lic := strings.TrimSpace(fmt.Sprint(prof["license"]))
	if lic == "" && len(ep.Ext) > 0 {
		var extm map[string]interface{}
		if err := json.Unmarshal(ep.Ext, &extm); err == nil {
			if lk := strings.TrimSpace(fmt.Sprint(extm["license_key"])); lk != "" {
				lic = lk
			}
		}
	}
	pk := strings.TrimSpace(fmt.Sprint(prof["private_key"]))
	if lic != "" && pk != "" {
		return false
	}
	return true
}
