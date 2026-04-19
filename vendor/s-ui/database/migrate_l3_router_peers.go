package database

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

const l3RouterEndpointType = "l3router"

// MigrateL3RouterPeersFromEndpointOptions copies legacy options.peers (and peer_ip_alloc hints) into l3_router_peers. Idempotent.
func MigrateL3RouterPeersFromEndpointOptions(db *gorm.DB) error {
	var endpoints []model.Endpoint
	if err := db.Model(model.Endpoint{}).Where("type = ?", l3RouterEndpointType).Find(&endpoints).Error; err != nil {
		return err
	}
	for i := range endpoints {
		ep := &endpoints[i]
		if len(ep.Options) == 0 {
			continue
		}
		var opt map[string]interface{}
		if err := json.Unmarshal(ep.Options, &opt); err != nil {
			continue
		}
		alloc := parsePeerIPAllocMapMigration(opt["peer_ip_alloc"])
		peersRaw, _ := opt["peers"].([]interface{})
		var maxSerial uint
		if err := db.Raw("SELECT COALESCE(MAX(peer_serial), 0) FROM l3_router_peers WHERE endpoint_id = ?", ep.Id).Scan(&maxSerial).Error; err != nil {
			return err
		}
		for _, pr := range peersRaw {
			pm, ok := pr.(map[string]interface{})
			if !ok {
				continue
			}
			cid := uintFromAnyMigration(pm["client_id"])
			if cid == 0 {
				continue
			}
			var n int64
			if err := db.Model(model.L3RouterPeer{}).Where("endpoint_id = ? AND client_id = ?", ep.Id, cid).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			allowed := stringSliceFromAnyMigration(pm["allowed_ips"])
			if len(allowed) == 0 {
				if a, ok := alloc[cid]; ok && strings.TrimSpace(a) != "" {
					allowed = []string{strings.TrimSpace(a)}
				}
			}
			if len(allowed) == 0 {
				continue
			}
			src := stringSliceFromAnyMigration(pm["filter_source_ips"])
			dst := stringSliceFromAnyMigration(pm["filter_destination_ips"])
			row := model.L3RouterPeer{
				EndpointId: ep.Id,
				ClientId:   cid,
				GroupID:    uintFromAnyMigration(pm["group_id"]),
			}
			var err error
			row.AllowedCIDRs, err = json.Marshal(allowed)
			if err != nil {
				continue
			}
			row.FilterSourceIPs, err = json.Marshal(src)
			if err != nil {
				continue
			}
			row.FilterDestinationIPs, err = json.Marshal(dst)
			if err != nil {
				continue
			}
			maxSerial++
			row.PeerSerial = maxSerial
			if err := db.Create(&row).Error; err != nil {
				return err
			}
		}
		delete(opt, "peer_ip_alloc")
		raw, err := json.MarshalIndent(opt, "", "  ")
		if err != nil {
			continue
		}
		if err := db.Model(model.Endpoint{}).Where("id = ?", ep.Id).Update("options", raw).Error; err != nil {
			return err
		}
	}
	return nil
}

func parsePeerIPAllocMapMigration(v interface{}) map[uint]string {
	out := make(map[uint]string)
	m, ok := v.(map[string]interface{})
	if !ok {
		return out
	}
	for k, val := range m {
		id64, err := strconv.ParseUint(k, 10, 64)
		if err != nil || id64 == 0 {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(val))
		if s != "" {
			out[uint(id64)] = s
		}
	}
	return out
}

func uintFromAnyMigration(v interface{}) uint {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return uint(n)
		}
	case int:
		if n > 0 {
			return uint(n)
		}
	case int64:
		if n > 0 {
			return uint(n)
		}
	}
	return 0
}

func stringSliceFromAnyMigration(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
