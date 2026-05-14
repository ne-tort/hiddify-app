package sub

import (
	"encoding/json"
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
		clientEp, err := buildMasqueSubscriptionEndpoint(db, &ep, client, host)
		if err != nil || clientEp == nil {
			continue
		}
		tag, _ := clientEp["tag"].(string)
		if tag == "" {
			continue
		}
		mergeWireGuardEndpoint(jsonConfig, tag, clientEp)
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
	if !clientHasWGStyleMemberAccess(db, opt, client.Id) {
		return nil, nil
	}

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
	return m, nil
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
		"member_group_ids": {}, "member_client_ids": {},
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
