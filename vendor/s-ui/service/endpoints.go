package service

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

type EndpointService struct {
	WarpService
}

const (
	l3RouterType              = "l3router"
	l3RouterDefaultOverlayDst = "198.18.0.1:33333"
)

func (o *EndpointService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	endpoints := []*model.Endpoint{}
	err := db.Model(model.Endpoint{}).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, endpoint := range endpoints {
		epData := map[string]interface{}{
			"id":   endpoint.Id,
			"type": endpoint.Type,
			"tag":  endpoint.Tag,
			"ext":  endpoint.Ext,
		}
		if endpoint.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(endpoint.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				epData[k] = v
			}
		}
		data = append(data, epData)
	}
	return &data, nil
}

func (o *EndpointService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var endpointsJson []json.RawMessage
	var endpoints []*model.Endpoint
	err := db.Model(model.Endpoint{}).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		endpointJson, err := endpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		endpointsJson = append(endpointsJson, endpointJson)
	}
	return endpointsJson, nil
}

func (s *EndpointService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	var err error

	switch act {
	case "new", "edit":
		var endpoint model.Endpoint
		err = endpoint.UnmarshalJSON(data)
		if err != nil {
			return err
		}

		if endpoint.Type == "warp" {
			if act == "new" {
				err = s.WarpService.RegisterWarp(&endpoint)
				if err != nil {
					return err
				}
			} else {
				var old_license string
				err = tx.Model(model.Endpoint{}).Select("json_extract(ext, '$.license_key')").Where("id = ?", endpoint.Id).Find(&old_license).Error
				if err != nil {
					return err
				}
				err = s.WarpService.SetWarpLicense(old_license, &endpoint)
				if err != nil {
					return err
				}
			}
		}
		if endpoint.Type == l3RouterType {
			_, err = s.syncSingleL3RouterEndpoint(tx, &endpoint)
			if err != nil {
				return err
			}
		}

		if corePtr.IsRunning() {
			configData, err := endpoint.MarshalJSON()
			if err != nil {
				return err
			}
			if act == "edit" {
				var oldTag string
				err = tx.Model(model.Endpoint{}).Select("tag").Where("id = ?", endpoint.Id).Find(&oldTag).Error
				if err != nil {
					return err
				}
				err = corePtr.RemoveEndpoint(oldTag)
				if err != nil && err != os.ErrInvalid {
					return err
				}
			}
			err = corePtr.AddEndpoint(configData)
			if err != nil {
				return err
			}
		}

		err = tx.Save(&endpoint).Error
		if err != nil {
			return err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveEndpoint(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}
		err = tx.Where("tag = ?", tag).Delete(model.Endpoint{}).Error
		if err != nil {
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
	return nil
}

func (s *EndpointService) SyncAllL3RouterPeers(tx *gorm.DB) error {
	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return err
	}
	for i := range endpoints {
		changed, err := s.syncSingleL3RouterEndpoint(tx, &endpoints[i])
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if err := tx.Model(model.Endpoint{}).Where("id = ?", endpoints[i].Id).Update("options", endpoints[i].Options).Error; err != nil {
			return err
		}
		if corePtr.IsRunning() {
			configData, err := endpoints[i].MarshalJSON()
			if err != nil {
				return err
			}
			if err := corePtr.RemoveEndpoint(endpoints[i].Tag); err != nil && err != os.ErrInvalid {
				return err
			}
			if err := corePtr.AddEndpoint(configData); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *EndpointService) syncSingleL3RouterEndpoint(tx *gorm.DB, endpoint *model.Endpoint) (bool, error) {
	var options map[string]interface{}
	if len(endpoint.Options) > 0 {
		if err := json.Unmarshal(endpoint.Options, &options); err != nil {
			return false, err
		}
	} else {
		options = make(map[string]interface{})
	}
	if options == nil {
		options = make(map[string]interface{})
	}

	changed := false
	if _, exists := options["packet_filter"]; !exists {
		options["packet_filter"] = false
		changed = true
	}
	if overlay, _ := options["overlay_destination"].(string); overlay == "" {
		options["overlay_destination"] = l3RouterDefaultOverlayDst
		changed = true
	}

	var existingPeers []map[string]interface{}
	if peersRaw, ok := options["peers"].([]interface{}); ok {
		for _, rawPeer := range peersRaw {
			if peerMap, ok := rawPeer.(map[string]interface{}); ok {
				existingPeers = append(existingPeers, peerMap)
			}
		}
	}
	byUser := make(map[string]map[string]interface{})
	byID := make(map[uint64]map[string]interface{})
	for _, peer := range existingPeers {
		if user, _ := peer["user"].(string); user != "" {
			byUser[user] = peer
		}
		if id, ok := parsePeerID(peer["peer_id"]); ok {
			byID[id] = peer
		}
	}

	identities, err := s.collectL3RouterClientIdentities(tx)
	if err != nil {
		return false, err
	}
	newPeers := make([]map[string]interface{}, 0, len(identities))
	for _, identity := range identities {
		src := byUser[identity.User]
		if src == nil {
			src = byID[identity.PeerID]
		}
		peer := map[string]interface{}{
			"peer_id": identity.PeerID,
			"user":    identity.User,
		}
		allowed := toStringSlice(nil)
		if src != nil {
			allowed = toStringSlice(src["allowed_ips"])
		}
		if len(allowed) == 0 {
			allowed = []string{defaultL3RouterCIDR(identity.PeerID)}
		}
		peer["allowed_ips"] = allowed
		if src != nil {
			if srcIPs := toStringSlice(src["filter_source_ips"]); len(srcIPs) > 0 {
				peer["filter_source_ips"] = srcIPs
			}
			if dstIPs := toStringSlice(src["filter_destination_ips"]); len(dstIPs) > 0 {
				peer["filter_destination_ips"] = dstIPs
			}
		}
		newPeers = append(newPeers, peer)
	}

	if !peersEqual(existingPeers, newPeers) {
		options["peers"] = newPeers
		changed = true
	}
	if !changed {
		return false, nil
	}
	updatedOptions, err := json.MarshalIndent(options, "", " ")
	if err != nil {
		return false, err
	}
	endpoint.Options = updatedOptions
	return true, nil
}

type l3RouterClientIdentity struct {
	User   string
	PeerID uint64
}

func (s *EndpointService) collectL3RouterClientIdentities(tx *gorm.DB) ([]l3RouterClientIdentity, error) {
	var clients []model.Client
	if err := tx.Model(model.Client{}).Select("id, name, config").Find(&clients).Error; err != nil {
		return nil, err
	}
	identities := make([]l3RouterClientIdentity, 0, len(clients))
	for _, client := range clients {
		var configs map[string]map[string]interface{}
		if len(client.Config) == 0 {
			continue
		}
		if err := json.Unmarshal(client.Config, &configs); err != nil {
			continue
		}
		l3cfg, ok := configs["l3router"]
		if !ok {
			continue
		}
		peerID, ok := parsePeerID(l3cfg["peer_id"])
		if !ok {
			continue
		}
		user := client.Name
		if cfgUser, _ := l3cfg["user"].(string); cfgUser != "" {
			user = cfgUser
		}
		identities = append(identities, l3RouterClientIdentity{
			User:   user,
			PeerID: peerID,
		})
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].User == identities[j].User {
			return identities[i].PeerID < identities[j].PeerID
		}
		return identities[i].User < identities[j].User
	})
	return identities, nil
}

func defaultL3RouterCIDR(peerID uint64) string {
	octet3 := (peerID >> 8) & 255
	octet4 := peerID & 255
	return fmt.Sprintf("%d.%d.%d.%d/32", 10, 250, octet3, octet4)
}

func toStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func peersEqual(existing []map[string]interface{}, generated []map[string]interface{}) bool {
	if len(existing) != len(generated) {
		return false
	}
	oldJSON, err := json.Marshal(existing)
	if err != nil {
		return false
	}
	newJSON, err := json.Marshal(generated)
	if err != nil {
		return false
	}
	return string(oldJSON) == string(newJSON)
}
