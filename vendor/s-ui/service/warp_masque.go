package service

import (
	"encoding/json"
	"fmt"
	"strings"

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
}

// RegisterWarpMasque runs the same Cloudflare consumer registration as legacy WARP
// (RegisterWarp on a temporary wireguard-shaped endpoint), then rewrites Options for sing-box
// warp_masque with profile.license + profile.private_key so the core can bootstrap MASQUE at runtime.
func (s *WarpService) RegisterWarpMasque(ep *model.Endpoint) error {
	preserved := preserveWarpMasqueOptionsFrom(ep.Options)
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

	wm := map[string]interface{}{
		"transport_mode": "connect_udp",
		"http_layer":     "auto",
		"profile": map[string]interface{}{
			"compatibility":      "consumer",
			"license":            strings.TrimSpace(licenseKey),
			"private_key":        strings.TrimSpace(priv),
			"auto_enroll_masque": true,
		},
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
		"member_group_ids", "member_client_ids", "sui_tls_id",
		"transport_mode", "template_udp", "template_ip", "template_tcp",
		"tls_server_name", "http_layer", "listen", "listen_port", "mode",
		"server", "server_port", "insecure",
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
