package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/gorm"
)

// hoistSubscriptionFieldsIntoSuiSub moves panel-only subscription overrides out of sing-box top-level
// keys so the stored endpoint always describes the server; subscription builder reads sui_sub.
func hoistSubscriptionFieldsIntoSuiSub(opt map[string]interface{}) {
	if opt == nil {
		return
	}
	keys := []string{"transport_mode", "http_layer", "addrs", "sui_client_auth_modes"}
	var sub map[string]interface{}
	if raw, ok := opt["sui_sub"].(map[string]interface{}); ok && raw != nil {
		sub = raw
	} else {
		sub = make(map[string]interface{})
	}
	for _, k := range keys {
		if v, ok := opt[k]; ok {
			sub[k] = v
			delete(opt, k)
		}
	}
	if len(sub) > 0 {
		opt["sui_sub"] = sub
	} else {
		delete(opt, "sui_sub")
	}
}

// normalizeMasqueEndpointOptionsOnSave enforces panel semantics: strip legacy flat TLS keys;
// generic masque in the panel is always a sing-box server endpoint; subscription-only knobs live in sui_sub.
// (expanded at export via mergeMasqueTLSPemFromStoredProfile).
func normalizeMasqueEndpointOptionsOnSave(ep *model.Endpoint) error {
	if ep == nil || len(ep.Options) == 0 {
		return nil
	}
	if ep.Type != masqueType && ep.Type != warpMasqueType {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	for _, k := range []string{"certificate", "key", "tls_server_name", "insecure"} {
		delete(opt, k)
	}
	if ep.Type == masqueType {
		hoistSubscriptionFieldsIntoSuiSub(opt)
	}
	if ep.Type == warpMasqueType {
		// Legacy UI stored transport/http_layer under sui_sub; sing-box reads top-level keys. Marshal strips sui_sub.
		migrateWarpMasqueSuiSubToTopLevel(opt)
		delete(opt, "sui_sub")
		delete(opt, "member_group_ids")
		delete(opt, "member_client_ids")
		delete(opt, "sui_tls_id")
		delete(opt, "sui_auth_modes")
		delete(opt, "sui_client_auth_modes")
		delete(opt, "addrs")
		delete(opt, "tls")
		delete(opt, "server")
		delete(opt, "server_port")
		delete(opt, "template_udp")
		delete(opt, "template_ip")
		delete(opt, "template_tcp")
		normalizeWarpMasqueProfileOnSave(ep, opt)
		ensureSingBoxMasqueMode(ep, opt)
		raw, err := json.MarshalIndent(opt, "", "  ")
		if err != nil {
			return err
		}
		ep.Options = raw
		return nil
	}
	// sing-box: empty mode means client (see hiddify-sing-box/protocol/masque normalizeMode).
	// Panel masque is always a listen server — persist explicit mode for core and DB.
	opt["mode"] = "server"
	if uintFromAny(opt["sui_tls_id"]) == 0 {
		return fmt.Errorf("masque server: выберите TLS-сертификат (sui_tls_id)")
	}
	delete(opt, "tls")
	delete(opt, "outbound_tls")
	stripMasqueServerAuthDefaultPolicyInOptions(opt)
	ensureSingBoxMasqueMode(ep, opt)
	raw, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

// stripMasqueServerAuthDefaultPolicyInOptions removes policy when it is the sing-box default (first_match).
func stripMasqueServerAuthDefaultPolicyInOptions(opt map[string]interface{}) {
	if opt == nil {
		return
	}
	sa, ok := opt["server_auth"].(map[string]interface{})
	if !ok || sa == nil {
		return
	}
	if p, ok := sa["policy"].(string); ok && strings.EqualFold(strings.TrimSpace(p), "first_match") {
		delete(sa, "policy")
	}
	if len(sa) == 0 {
		delete(opt, "server_auth")
	}
}

// validateMasqueServerListenPortAvailable rejects saving a generic masque server endpoint when
// listen_port is already used by an inbound or another masque / warp_masque endpoint.
func validateMasqueServerListenPortAvailable(tx *gorm.DB, ep *model.Endpoint) error {
	if ep == nil || tx == nil || ep.Type != masqueType {
		return nil
	}
	var opt map[string]interface{}
	if len(ep.Options) == 0 {
		return nil
	}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	if !masqueServerBindCandidate(opt) {
		return nil
	}
	port := intFromAny(opt["listen_port"])
	if port <= 0 || port > 65535 {
		return nil
	}
	var inbCnt int64
	if err := tx.Model(&model.Inbound{}).Where("CAST(json_extract(options, '$.listen_port') AS INTEGER) = ?", port).Count(&inbCnt).Error; err != nil {
		return err
	}
	if inbCnt > 0 {
		return fmt.Errorf("MASQUE: порт %d уже занят инбаундом", port)
	}
	var epCnt int64
	if err := tx.Model(&model.Endpoint{}).
		Where("type IN ? AND id <> ? AND CAST(json_extract(options, '$.listen_port') AS INTEGER) = ?", []string{masqueType, warpMasqueType}, ep.Id, port).
		Count(&epCnt).Error; err != nil {
		return err
	}
	if epCnt > 0 {
		return fmt.Errorf("MASQUE: порт %d уже занят другим эндпоинтом masque или warp_masque", port)
	}
	return nil
}

func masqueServerBindCandidate(opt map[string]interface{}) bool {
	if opt == nil {
		return false
	}
	if m, ok := opt["mode"].(string); ok && strings.EqualFold(strings.TrimSpace(m), "client") {
		return false
	}
	return intFromAny(opt["listen_port"]) > 0
}

func migrateWarpMasqueSuiSubToTopLevel(opt map[string]interface{}) {
	if opt == nil {
		return
	}
	sub, ok := opt["sui_sub"].(map[string]interface{})
	if !ok || sub == nil {
		return
	}
	for _, k := range []string{"transport_mode", "http_layer"} {
		if v, ok := sub[k]; ok {
			if cur, has := opt[k]; !has || strings.TrimSpace(fmt.Sprint(cur)) == "" {
				opt[k] = v
			}
		}
	}
}

func normalizeWarpMasqueProfileOnSave(ep *model.Endpoint, opt map[string]interface{}) {
	if ep == nil || opt == nil {
		return
	}
	prof, ok := opt["profile"].(map[string]interface{})
	if !ok || prof == nil {
		prof = make(map[string]interface{})
		opt["profile"] = prof
	}
	if lic := strings.TrimSpace(fmt.Sprint(prof["license"])); lic == "" || strings.EqualFold(lic, "<nil>") || strings.EqualFold(lic, "nil") {
		delete(prof, "license")
	}
	tok := strings.TrimSpace(fmt.Sprint(prof["auth_token"]))
	id := strings.TrimSpace(fmt.Sprint(prof["id"]))
	if tok == "" || id == "" {
		delete(prof, "compatibility")
	}
	if strings.TrimSpace(fmt.Sprint(prof["warp_masque_state_path"])) == "" && strings.TrimSpace(ep.Tag) != "" {
		prof["warp_masque_state_path"] = DefaultWarpMasqueStatePath(ep.Tag)
	}
}

// ensureSingBoxMasqueMode sets sing-box "mode" in options JSON. Empty mode defaults to client in core
// (normalizeMode), which breaks server_auth + listen; panel masque is always server, warp_masque client.
func ensureSingBoxMasqueMode(ep *model.Endpoint, opt map[string]interface{}) {
	if ep == nil || opt == nil {
		return
	}
	switch ep.Type {
	case masqueType:
		opt["mode"] = "server"
	case warpMasqueType:
		opt["mode"] = "client"
	}
}
