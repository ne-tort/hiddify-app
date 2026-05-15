package sub

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/service"
	"gorm.io/gorm"
)

const (
	masqueEndpointType     = "masque"
	warpMasqueEndpointType = "warp_masque"
)

// patchJsonForMasqueSubscription injects sing-box masque / warp_masque client endpoints for
// endpoints the client is allowed to use (member_client_ids / member_group_ids).
func (j *JsonService) patchJsonForMasqueSubscription(jsonConfig *map[string]interface{}, client *model.Client, requestHost string) error {
	return patchJsonForMasqueSubscriptionDB(database.GetDB(), j, jsonConfig, client, requestHost)
}

func patchJsonForMasqueSubscriptionDB(db *gorm.DB, j *JsonService, jsonConfig *map[string]interface{}, client *model.Client, requestHost string) error {
	if db == nil || client == nil || jsonConfig == nil {
		return nil
	}
	host := strings.TrimSpace(requestHost)
	if host == "" && j != nil {
		host = strings.TrimSpace(j.resolveWGServerHost(""))
	}
	if host == "" {
		return nil
	}

	var eps []model.Endpoint
	if err := db.Where("type IN ?", []string{masqueEndpointType, warpMasqueEndpointType}).Order("id ASC").Find(&eps).Error; err != nil {
		return err
	}

	for _, ep := range eps {
		if ep.Type == warpMasqueEndpointType {
			// WARP MASQUE is only for the panel's own sing-box (GetAllConfig). Do not inject into member subscriptions
			// (would duplicate one Cloudflare device / wrong security model for end users).
			continue
		}
		addrs := extractMasqueAddrsFromOptions(ep.Options)
		clientEp, err := buildMasqueSubscriptionEndpoint(db, &ep, client, host)
		if err != nil || clientEp == nil {
			continue
		}
		baseTag, _ := clientEp["tag"].(string)
		if baseTag == "" {
			continue
		}
		removeMasqueSubscriptionEndpointsForBaseTag(jsonConfig, baseTag)
		if len(addrs) == 0 {
			mergeWireGuardEndpoint(jsonConfig, baseTag, clientEp)
			continue
		}
		base := cloneMasqueEndpointMap(clientEp)
		if base == nil {
			continue
		}
		for idx, addr := range addrs {
			one := cloneMasqueEndpointMap(base)
			if one == nil {
				continue
			}
			applyMasqueAddrOverrides(one, addr)
			remark, _ := addr["remark"].(string)
			newTag := fmt.Sprintf("%d.%s%s", idx+1, baseTag, remark)
			one["tag"] = newTag
			mergeWireGuardEndpoint(jsonConfig, newTag, one)
		}
	}
	return nil
}

func buildMasqueSubscriptionEndpoint(db *gorm.DB, ep *model.Endpoint, client *model.Client, serverHost string) (map[string]interface{}, error) {
	if ep == nil {
		return nil, nil
	}
	work := *ep
	if work.Type == warpMasqueEndpointType {
		merged, err := service.MergeWarpMasqueOptionsWithExt(work.Options, work.Ext)
		if err != nil {
			return nil, err
		}
		work.Options = merged
	}

	var opt map[string]interface{}
	if len(work.Options) > 0 {
		if err := json.Unmarshal(work.Options, &opt); err != nil {
			return nil, err
		}
	}
	if opt == nil {
		return nil, nil
	}
	tlsProfileID := uintFromAny(opt["sui_tls_id"])
	if !clientHasWGStyleMemberAccess(db, opt, client.Id) {
		return nil, nil
	}
	clientSubModes := masqueSubscriptionClientModes(opt)
	serverSubModes := stringSliceFromInterface(opt["sui_auth_modes"])

	raw, err := work.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "server_auth")

	cfgKey := "masque"
	if work.Type == warpMasqueEndpointType {
		cfgKey = "warp_masque"
	}
	overlay := clientConfigObjectBranch(client.Config, cfgKey)
	clientEff := filterMasqueClientOverlayForSubscription(cfgKey, overlay, clientSubModes, serverSubModes)
	mtlsOK := authModePairOK(clientEff, serverSubModes, "mtls")

	if work.Type == masqueEndpointType && masqueServerModeMap(m) {
		m = transformMasqueServerMapToClient(m, serverHost)
	} else {
		if serverHost != "" {
			if cur, ok := m["server"].(string); !ok || strings.TrimSpace(cur) == "" {
				m["server"] = serverHost
			}
		}
	}
	mergeStringKeyedMap(m, overlay)

	var mergedOutboundTLS map[string]interface{}
	if work.Type == masqueEndpointType {
		mergedOutboundTLS = cloneJSONStringMapInterface(m["outbound_tls"])
	}

	injectMasqueOutboundTLSFromProfile(db, tlsProfileID, m)

	if work.Type == masqueEndpointType {
		mergeOutboundTLSShallow(m, mergedOutboundTLS)
		if mtlsOK {
			mergeSuiMasqueClientPEMIntoOutboundTLS(client, m)
		}
	}
	mergeMasqueSubscriptionFromSuiSub(opt, m)

	stripMasqueServerMaterialFromSubscription(m)
	applyMasqueSubscriptionServerFromTLSProfile(db, tlsProfileID, m)
	return m, nil
}

// mergeOutboundTLSShallow merges src into m["outbound_tls"] after profile injection; nested maps merge deeply, explicit client values win.
func mergeOutboundTLSShallow(m map[string]interface{}, src map[string]interface{}) {
	if m == nil || len(src) == 0 {
		return
	}
	dst, ok := m["outbound_tls"].(map[string]interface{})
	if !ok || dst == nil {
		m["outbound_tls"] = cloneJSONStringMapInterface(src)
		return
	}
	mergeStringKeyedMapDeep(dst, src)
}

// mergeStringKeyedMapDeep merges src into dst; for nested maps both sides must be maps, else src replaces dst[k].
func mergeStringKeyedMapDeep(dst, src map[string]interface{}) {
	if dst == nil || len(src) == 0 {
		return
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		cur, ok := dst[k]
		if !ok {
			dst[k] = v
			continue
		}
		cm, cOK := cur.(map[string]interface{})
		vm, vOK := v.(map[string]interface{})
		if cOK && vOK {
			mergeStringKeyedMapDeep(cm, vm)
			dst[k] = cm
			continue
		}
		dst[k] = v
	}
}

func cloneJSONStringMapInterface(v interface{}) map[string]interface{} {
	m, ok := v.(map[string]interface{})
	if !ok || m == nil {
		return nil
	}
	return cloneJSONStringMap(m)
}

// mergeSuiMasqueClientPEMIntoOutboundTLS adds panel-generated client leaf PEM into outbound_tls (generic masque only).
func mergeSuiMasqueClientPEMIntoOutboundTLS(client *model.Client, m map[string]interface{}) {
	if client == nil || m == nil || len(client.Config) == 0 {
		return
	}
	var root map[string]interface{}
	if err := json.Unmarshal(client.Config, &root); err != nil {
		return
	}
	sm, ok := root["sui_masque"].(map[string]interface{})
	if !ok || sm == nil {
		return
	}
	cert, _ := sm["client_certificate_pem"].(string)
	key, _ := sm["client_private_key_pem"].(string)
	if strings.TrimSpace(cert) == "" || strings.TrimSpace(key) == "" {
		return
	}
	ot, ok := m["outbound_tls"].(map[string]interface{})
	if !ok || ot == nil {
		ot = make(map[string]interface{})
		m["outbound_tls"] = ot
	}
	// sing-box OutboundTLSOptions: certificate / key as listable → JSON arrays of PEM lines is accepted as list of strings.
	ot["certificate"] = []interface{}{cert}
	ot["key"] = []interface{}{key}
}

func injectMasqueOutboundTLSFromProfile(db *gorm.DB, tlsID uint, m map[string]interface{}) {
	if db == nil || m == nil || tlsID == 0 {
		return
	}
	var tlsRow model.Tls
	if err := db.Where("id = ?", tlsID).First(&tlsRow).Error; err != nil {
		return
	}
	if len(tlsRow.Client) == 0 {
		return
	}
	var tlsClient map[string]interface{}
	if err := json.Unmarshal(tlsRow.Client, &tlsClient); err != nil || len(tlsClient) == 0 {
		return
	}
	m["outbound_tls"] = cloneJSONStringMap(tlsClient)
}

func cloneJSONStringMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// outboundTLSServerNameFromProfile returns TLS client profile server_name (SNI) when set.
func outboundTLSServerNameFromProfile(db *gorm.DB, tlsID uint) string {
	if db == nil || tlsID == 0 {
		return ""
	}
	var tlsRow model.Tls
	if err := db.Where("id = ?", tlsID).First(&tlsRow).Error; err != nil {
		return ""
	}
	if len(tlsRow.Client) == 0 {
		return ""
	}
	var tlsClient map[string]interface{}
	if err := json.Unmarshal(tlsRow.Client, &tlsClient); err != nil || len(tlsClient) == 0 {
		return ""
	}
	sn, _ := tlsClient["server_name"].(string)
	return strings.TrimSpace(sn)
}

// applyMasqueSubscriptionServerFromTLSProfile sets endpoint "server" to TLS profile server_name
// when present, so clients use the public hostname instead of the subscription request IP.
func applyMasqueSubscriptionServerFromTLSProfile(db *gorm.DB, tlsID uint, m map[string]interface{}) {
	if m == nil {
		return
	}
	host := outboundTLSServerNameFromProfile(db, tlsID)
	if host == "" {
		return
	}
	m["server"] = host
}

func stripMasqueServerMaterialFromSubscription(m map[string]interface{}) {
	if m == nil {
		return
	}
	delete(m, "tls")
	delete(m, "certificate")
	delete(m, "key")
	delete(m, "sui_tls_id")
	// Legacy flat TLS (must not appear in subscription even if old rows linger in DB).
	delete(m, "tls_server_name")
	delete(m, "insecure")
	delete(m, "sui_auth_modes")
	delete(m, "sui_client_auth_modes")
	delete(m, "addrs")
	delete(m, "sui_sub")
	// Client.Config branch may contain panel-only keys (e.g. display name); sing-box ignores them.
	delete(m, "name")
}

func masqueServerModeMap(m map[string]interface{}) bool {
	if m == nil {
		return false
	}
	if sm, ok := m["mode"].(string); ok && strings.EqualFold(strings.TrimSpace(sm), "server") {
		return true
	}
	if intFromAny(m["listen_port"]) > 0 {
		if _, ok := m["listen"]; ok {
			return true
		}
	}
	return false
}

func transformMasqueServerMapToClient(in map[string]interface{}, serverHost string) map[string]interface{} {
	out := make(map[string]interface{})
	skip := map[string]struct{}{
		"listen": {}, "listen_port": {}, "certificate": {}, "key": {}, "server_auth": {},
		"tls": {}, "outbound_tls": {}, "sui_tls_id": {},
		"member_group_ids": {}, "member_client_ids": {}, "sui_auth_modes": {}, "sui_client_auth_modes": {}, "addrs": {}, "sui_sub": {},
	}
	for k, v := range in {
		if _, s := skip[k]; s {
			continue
		}
		out[k] = v
	}
	out["mode"] = "client"
	out["server"] = strings.TrimSpace(serverHost)
	if lp := intFromAny(in["listen_port"]); lp > 0 {
		out["server_port"] = lp
	}
	if _, ok := out["type"]; !ok {
		out["type"] = masqueEndpointType
	}
	return out
}

func clientConfigObjectBranch(cfg json.RawMessage, key string) map[string]interface{} {
	if len(cfg) == 0 {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(cfg, &root); err != nil {
		return nil
	}
	v, ok := root[key]
	if !ok || v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func mergeStringKeyedMap(dst map[string]interface{}, src map[string]interface{}) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		dst[k] = v
	}
}

func authModesAllowAll(modes []string) bool {
	return len(modes) == 0
}

func authModesHas(modes []string, needle string) bool {
	n := strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return false
	}
	for _, m := range modes {
		if strings.EqualFold(strings.TrimSpace(m), n) {
			return true
		}
	}
	return false
}

func stringSliceFromInterface(v interface{}) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(x))
		for _, s := range x {
			out = append(out, strings.TrimSpace(s))
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, it := range x {
			out = append(out, strings.TrimSpace(fmt.Sprint(it)))
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

// filterMasqueClientOverlayForSubscription removes legacy overlay sui_auth_modes and drops overlay
// credential keys unless both endpoint client subscription modes (sui_client_auth_modes) and
// server ACL modes (sui_auth_modes) allow that mechanism. Empty slice on either side means "all".
// If the endpoint does not set sui_client_auth_modes, legacy per-client overlay sui_auth_modes (if any)
// is still honored before it is stripped from the merged JSON.
func filterMasqueClientOverlayForSubscription(cfgKey string, overlay map[string]interface{}, clientSubModes, serverSubModes []string) []string {
	if overlay == nil {
		return clientSubModes
	}
	legacyOverlayModes := stringSliceFromInterface(overlay["sui_auth_modes"])
	delete(overlay, "sui_auth_modes")
	clientEff := clientSubModes
	if authModesAllowAll(clientEff) && len(legacyOverlayModes) > 0 {
		clientEff = legacyOverlayModes
	}
	bearerOK := authModePairOK(clientEff, serverSubModes, "bearer")
	basicOK := authModePairOK(clientEff, serverSubModes, "basic")
	if cfgKey == "masque" {
		if !bearerOK {
			delete(overlay, "server_token")
		}
		if !basicOK {
			delete(overlay, "client_basic_username")
			delete(overlay, "client_basic_password")
		}
	}
	if cfgKey == "warp_masque" {
		if !bearerOK {
			delete(overlay, "server_token")
		}
	}
	return clientEff
}

func authModePairOK(clientSubModes, serverSubModes []string, needle string) bool {
	return (authModesAllowAll(clientSubModes) || authModesHas(clientSubModes, needle)) &&
		(authModesAllowAll(serverSubModes) || authModesHas(serverSubModes, needle))
}

func masquePanelSuiSub(opt map[string]interface{}) map[string]interface{} {
	if opt == nil {
		return nil
	}
	m, ok := opt["sui_sub"].(map[string]interface{})
	if !ok || m == nil {
		return nil
	}
	return m
}

func masqueSubscriptionClientModes(opt map[string]interface{}) []string {
	if sub := masquePanelSuiSub(opt); sub != nil {
		if v, ok := sub["sui_client_auth_modes"]; ok {
			return stringSliceFromInterface(v)
		}
	}
	return stringSliceFromInterface(opt["sui_client_auth_modes"])
}

func mergeMasqueSubscriptionFromSuiSub(opt map[string]interface{}, m map[string]interface{}) {
	sub := masquePanelSuiSub(opt)
	if sub == nil || m == nil {
		return
	}
	if v, ok := sub["transport_mode"]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
		m["transport_mode"] = v
	}
	if v, ok := sub["http_layer"]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
		m["http_layer"] = v
	}
}

func extractMasqueAddrsSlice(v interface{}) []map[string]interface{} {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(x))
		for _, it := range x {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func extractMasqueAddrsFromOptions(raw json.RawMessage) []map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(raw, &opt); err != nil || opt == nil {
		return nil
	}
	if sub := masquePanelSuiSub(opt); sub != nil {
		if out := extractMasqueAddrsSlice(sub["addrs"]); len(out) > 0 {
			return out
		}
	}
	return extractMasqueAddrsSlice(opt["addrs"])
}

func cloneMasqueEndpointMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func applyMasqueAddrOverrides(dst map[string]interface{}, addr map[string]interface{}) {
	if dst == nil || addr == nil {
		return
	}
	if s, ok := addr["server"].(string); ok && strings.TrimSpace(s) != "" {
		dst["server"] = strings.TrimSpace(s)
	}
	if _, ok := addr["server_port"]; ok {
		dst["server_port"] = intFromAny(addr["server_port"])
	}
	if tlsRaw, ok := addr["tls"].(map[string]interface{}); ok && len(tlsRaw) > 0 {
		mergeOutboundTLSShallow(dst, tlsRaw)
	}
}

func removeMasqueSubscriptionEndpointsForBaseTag(jsonConfig *map[string]interface{}, baseTag string) {
	if jsonConfig == nil || strings.TrimSpace(baseTag) == "" {
		return
	}
	escaped := regexp.QuoteMeta(strings.TrimSpace(baseTag))
	pattern := regexp.MustCompile(`^([0-9]+\.)` + escaped)
	var eps []interface{}
	if raw, ok := (*jsonConfig)["endpoints"]; ok && raw != nil {
		switch v := raw.(type) {
		case []interface{}:
			eps = v
		case []map[string]interface{}:
			for _, m := range v {
				eps = append(eps, m)
			}
		}
	}
	out := make([]interface{}, 0, len(eps))
	for _, e := range eps {
		m, ok := e.(map[string]interface{})
		if !ok {
			out = append(out, e)
			continue
		}
		tag := strings.TrimSpace(fmt.Sprint(m["tag"]))
		if tag == baseTag || pattern.MatchString(tag) {
			continue
		}
		out = append(out, e)
	}
	(*jsonConfig)["endpoints"] = out
}
