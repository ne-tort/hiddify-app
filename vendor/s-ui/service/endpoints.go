package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

type EndpointService struct {
	WarpService
}

type EndpointRuntimeAction struct {
	Action     string
	EndpointID uint
	OldTag     string
	Tag        string
	NeedsReload bool
}

const (
	l3RouterType              = "l3router"
	l3RouterDefaultOverlayDst = "198.18.0.1:33333"
	l3PeerIPAllocKey          = "peer_ip_alloc"
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
		if endpoint.Type == l3RouterType {
			if view, err := o.buildL3PeersViewForEndpoint(db, endpoint); err == nil {
				rowsUI, errR := o.loadL3PeerRows(db, endpoint.Id)
				if errR == nil {
					l3AppendPeerUIOrder(view, rowsUI)
				}
				epData["peers"] = view
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
		if endpoint.Type == l3RouterType {
			var opt map[string]interface{}
			if len(endpoint.Options) > 0 {
				if err := json.Unmarshal(endpoint.Options, &opt); err != nil {
					return nil, err
				}
			} else {
				opt = make(map[string]interface{})
			}
			view, err := o.buildL3PeersViewForEndpoint(db, endpoint)
			if err != nil {
				return nil, err
			}
			opt["peers"] = view
			delete(opt, l3PeerIPAllocKey)
			raw, err := json.MarshalIndent(opt, "", " ")
			if err != nil {
				return nil, err
			}
			endpoint.Options = raw
		}
		endpointJson, err := endpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		endpointsJson = append(endpointsJson, endpointJson)
	}
	return endpointsJson, nil
}

func (s *EndpointService) Save(tx *gorm.DB, act string, data json.RawMessage) (*EndpointRuntimeAction, error) {
	var err error
	var runtimeAction *EndpointRuntimeAction

	switch act {
	case "new", "edit":
		var endpoint model.Endpoint
		err = endpoint.UnmarshalJSON(data)
		if err != nil {
			return nil, err
		}

		if endpoint.Type == "warp" {
			if act == "new" {
				err = s.WarpService.RegisterWarp(&endpoint)
				if err != nil {
					return nil, err
				}
			} else {
				var old_license string
				err = tx.Model(model.Endpoint{}).Select("json_extract(ext, '$.license_key')").Where("id = ?", endpoint.Id).Find(&old_license).Error
				if err != nil {
					return nil, err
				}
				err = s.WarpService.SetWarpLicense(old_license, &endpoint)
				if err != nil {
					return nil, err
				}
			}
		}
		oldTag := ""
		if act == "edit" {
			err = tx.Model(model.Endpoint{}).Select("tag").Where("id = ?", endpoint.Id).Find(&oldTag).Error
			if err != nil {
				return nil, err
			}
		}
		if endpoint.Type == l3RouterType {
			if err := stripL3RouterClientControlledOptions(&endpoint); err != nil {
				return nil, err
			}
		}
		// Persist endpoint payload first so ClientShouldHaveL3Router sees fresh member_* options
		// during l3 identity sync on the same save cycle.
		err = tx.Save(&endpoint).Error
		if err != nil {
			return nil, err
		}
		if endpoint.Type == l3RouterType {
			_, err = s.syncSingleL3RouterEndpoint(tx, &endpoint)
			if err != nil {
				return nil, err
			}
			err = tx.Save(&endpoint).Error
			if err != nil {
				return nil, err
			}
		}
		runtimeAction = &EndpointRuntimeAction{
			Action:     act,
			EndpointID: endpoint.Id,
			OldTag:     oldTag,
			Tag:        endpoint.Tag,
			NeedsReload: endpoint.Type == l3RouterType,
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return nil, err
		}
		var epDel model.Endpoint
		if err := tx.Where("tag = ?", tag).First(&epDel).Error; err == nil {
			_ = tx.Where("endpoint_id = ?", epDel.Id).Delete(&model.L3RouterPeer{}).Error
		}
		var endpointType string
		_ = tx.Model(model.Endpoint{}).Where("tag = ?", tag).Select("type").Find(&endpointType).Error
		err = tx.Where("tag = ?", tag).Delete(model.Endpoint{}).Error
		if err != nil {
			return nil, err
		}
		runtimeAction = &EndpointRuntimeAction{Action: act, Tag: tag, NeedsReload: endpointType == l3RouterType}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
	if err := PersistL3RouterRouteRules(tx); err != nil {
		return nil, err
	}
	return runtimeAction, nil
}

func (s *EndpointService) SyncAllL3RouterPeers(tx *gorm.DB) (bool, error) {
	changedAny := false
	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return false, err
	}
	for i := range endpoints {
		changed, err := s.syncSingleL3RouterEndpoint(tx, &endpoints[i])
		if err != nil {
			return false, err
		}
		if !changed {
			continue
		}
		changedAny = true
		if err := tx.Model(model.Endpoint{}).Where("id = ?", endpoints[i].Id).Update("options", endpoints[i].Options).Error; err != nil {
			return false, err
		}
	}
	return changedAny, nil
}

func (s *EndpointService) ApplyRuntimeAction(action *EndpointRuntimeAction) error {
	if action == nil || !corePtr.IsRunning() {
		return nil
	}
	switch action.Action {
	case "new", "edit":
		var endpoint model.Endpoint
		if err := database.GetDB().Where("id = ?", action.EndpointID).First(&endpoint).Error; err != nil {
			return err
		}
		configData, err := endpoint.MarshalJSON()
		if err != nil {
			return err
		}
		if action.Action == "edit" {
			removeTag := action.OldTag
			if removeTag == "" {
				removeTag = action.Tag
			}
			if err := corePtr.RemoveEndpoint(removeTag); err != nil && err != os.ErrInvalid {
				return err
			}
		}
		return corePtr.AddEndpoint(configData)
	case "del":
		if action.Tag == "" {
			return nil
		}
		if err := corePtr.RemoveEndpoint(action.Tag); err != nil && err != os.ErrInvalid {
			return err
		}
		return nil
	default:
		return common.NewErrorf("unknown runtime action: %s", action.Action)
	}
}

func stripL3RouterClientControlledOptions(ep *model.Endpoint) error {
	if ep.Type != l3RouterType || len(ep.Options) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(ep.Options, &m); err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	delete(m, "peers")
	delete(m, l3PeerIPAllocKey)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

func (s *EndpointService) syncSingleL3RouterEndpoint(tx *gorm.DB, endpoint *model.Endpoint) (bool, error) {
	subnetAssigned, err := EnsureL3RouterAutoPrivateSubnet(tx, endpoint)
	if err != nil {
		return false, err
	}
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

	changed := subnetAssigned
	if _, exists := options["packet_filter"]; !exists {
		options["packet_filter"] = false
		changed = true
	}
	if _, exists := options["member_group_ids"]; !exists {
		options["member_group_ids"] = []uint{}
		changed = true
	}
	if _, exists := options["member_client_ids"]; !exists {
		options["member_client_ids"] = []uint{}
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
	_, hadAlloc := options[l3PeerIPAllocKey]

	identities, err := s.collectL3RouterClientIdentitiesForEndpoint(tx, endpoint)
	if err != nil {
		return false, err
	}

	wantClient := make(map[uint]struct{}, len(identities))
	for _, id := range identities {
		wantClient[id.ClientID] = struct{}{}
	}

	var poolPrefix netip.Prefix
	if s, ok := options["private_subnet"].(string); ok {
		t := strings.TrimSpace(s)
		if t != "" {
			p, err := netip.ParsePrefix(t)
			if err != nil || !p.Addr().Is4() || !isPrivateRFC1918Prefix(p) {
				return false, common.NewErrorf("private_subnet: leave empty for auto or set a private IPv4 CIDR (RFC1918 / 100.64/10)")
			}
			bits := p.Bits()
			if bits < 8 || bits > 30 {
				return false, common.NewErrorf("private_subnet: prefix length must be between /8 and /30")
			}
			poolPrefix = p.Masked()
		}
	}

	rows, err := s.loadL3PeerRows(tx, endpoint.Id)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if _, ok := wantClient[r.ClientId]; !ok {
			if err := tx.Delete(&model.L3RouterPeer{}, r.Id).Error; err != nil {
				return false, err
			}
			changed = true
		}
	}

	rows, err = s.loadL3PeerRows(tx, endpoint.Id)
	if err != nil {
		return false, err
	}
	byClient := l3PeerRowsByClientID(rows)

	for _, id := range identities {
		row := byClient[id.ClientID]
		if row != nil {
			if row.GroupID != id.GroupID {
				if err := tx.Model(&model.L3RouterPeer{}).Where("id = ?", row.Id).Update("group_id", id.GroupID).Error; err != nil {
					return false, err
				}
				changed = true
			}
			continue
		}
		var allowed []string
		if poolPrefix.IsValid() {
			used := collectUsedPoolCIDRsFromRows(rows, poolPrefix, 0)
			cidr, aerr := assignStablePoolCIDR(poolPrefix, used)
			if aerr != nil {
				return false, common.NewErrorf("l3router: private_subnet pool exhausted: %v", aerr)
			}
			allowed = []string{cidr}
		} else {
			allowed = []string{defaultL3RouterCIDR(id.PeerID)}
			allowed = sanitizeL3RouterAllowedIPs(allowed)
			if len(allowed) == 0 {
				allowed = []string{defaultL3RouterCIDR(id.PeerID)}
			}
		}
		ns, err := nextL3PeerSerial(tx, endpoint.Id)
		if err != nil {
			return false, err
		}
		newRow := model.L3RouterPeer{
			EndpointId:           endpoint.Id,
			ClientId:             id.ClientID,
			PeerSerial:           ns,
			AllowedCIDRs:         l3PeerEncodeStringSlice(allowed),
			FilterSourceIPs:      l3PeerEmptyJSONArray(),
			FilterDestinationIPs: l3PeerEmptyJSONArray(),
			GroupID:              id.GroupID,
		}
		if err := tx.Create(&newRow).Error; err != nil {
			return false, err
		}
		rows = append(rows, newRow)
		byClient[id.ClientID] = &rows[len(rows)-1]
		changed = true
	}

	rows, err = s.loadL3PeerRows(tx, endpoint.Id)
	if err != nil {
		return false, err
	}
	byClient = l3PeerRowsByClientID(rows)

	allocOrder := sortL3IdentitiesByPeerSerial(identities, byClient)

	newPeers, err := s.materializeL3RouterPeerMaps(allocOrder, byClient)
	if err != nil {
		return false, err
	}
	if poolPrefix.IsValid() {
		if err := validateL3PoolNoDuplicateCIDRS(newPeers, poolPrefix); err != nil {
			return false, err
		}
	} else {
		if err := validateNonPoolNoDuplicateCIDRS(newPeers); err != nil {
			return false, err
		}
	}

	delete(options, l3PeerIPAllocKey)
	if hadAlloc {
		changed = true
	}
	options["peers"] = newPeers
	if !peersEqual(existingPeers, newPeers) {
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

func assignStablePoolCIDR(poolPrefix netip.Prefix, used map[string]struct{}) (string, error) {
	hostBits := 32 - poolPrefix.Bits()
	total := 1 << hostBits
	usable := total - 2
	if usable <= 0 {
		return "", fmt.Errorf("pool has no usable hosts")
	}
	for idx := 0; idx < usable; idx++ {
		pr, err := nthUsableIPv4InPool(poolPrefix, idx)
		if err != nil {
			continue
		}
		cidr := pr.String()
		if _, exists := used[cidr]; exists {
			continue
		}
		return cidr, nil
	}
	return "", fmt.Errorf("no free CIDR in pool")
}

type l3RouterClientIdentity struct {
	ClientID   uint
	ClientName string
	User       string
	PeerID     uint64
	GroupID    uint
}

func (s *EndpointService) collectL3RouterClientIdentitiesForEndpoint(tx *gorm.DB, endpoint *model.Endpoint) ([]l3RouterClientIdentity, error) {
	var options map[string]interface{}
	if len(endpoint.Options) > 0 {
		_ = json.Unmarshal(endpoint.Options, &options)
	}
	groupIDs := parseUintListFromAny(options["member_group_ids"])
	// Legacy: одна привязанная группа (до member_group_ids) — как в ClientShouldHaveL3Router.
	if bg := uintFromAny(options["bound_group_id"]); bg > 0 {
		groupIDs = append(groupIDs, bg)
	}
	groupIDs = dedupeUintSorted(groupIDs)
	clientIDs := parseUintListFromAny(options["member_client_ids"])
	if len(groupIDs) == 0 && len(clientIDs) == 0 {
		return nil, nil
	}
	gs := GroupService{}
	clientToGroup := map[uint]uint{}
	for _, gid := range groupIDs {
		if gid == 0 {
			continue
		}
		memberIDs, err := gs.ResolveMemberClientIDs(tx, gid)
		if err != nil {
			return nil, err
		}
		for _, cid := range memberIDs {
			if _, exists := clientToGroup[cid]; !exists {
				clientToGroup[cid] = gid
			}
		}
	}
	for _, cid := range clientIDs {
		if cid == 0 {
			continue
		}
		if _, exists := clientToGroup[cid]; !exists {
			clientToGroup[cid] = 0
		}
	}
	cs := ClientService{}
	identities := make([]l3RouterClientIdentity, 0, len(clientToGroup))
	for mid, sourceGroup := range clientToGroup {
		var client model.Client
		if err := tx.Model(model.Client{}).Where("id = ?", mid).Select("id", "name", "config").First(&client).Error; err != nil {
			continue
		}
		cfgChanged, err := cs.ensureL3RouterIdentityWithResult(tx, &client)
		if err != nil {
			return nil, err
		}
		if cfgChanged {
			if err := tx.Model(model.Client{}).Where("id = ?", client.Id).Update("config", client.Config).Error; err != nil {
				return nil, err
			}
		}
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
		peerID, ok, err := parsePeerID(l3cfg["peer_id"])
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		user := client.Name
		if cfgUser, _ := l3cfg["user"].(string); cfgUser != "" {
			user = cfgUser
		}
		identities = append(identities, l3RouterClientIdentity{
			ClientID:   client.Id,
			ClientName: client.Name,
			User:       user,
			PeerID:     peerID,
			GroupID:    sourceGroup,
		})
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].ClientName == identities[j].ClientName {
			return identities[i].PeerID < identities[j].PeerID
		}
		return identities[i].ClientName < identities[j].ClientName
	})
	return identities, nil
}

func parseUintListFromAny(raw interface{}) []uint {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]uint, 0, len(v))
		for _, x := range v {
			id := uintFromAny(x)
			if id > 0 {
				out = append(out, id)
			}
		}
		return out
	case []uint:
		return v
	default:
		return nil
	}
}

func dedupeUintSorted(ids []uint) []uint {
	if len(ids) == 0 {
		return ids
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := ids[:0]
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if len(out) == 0 || out[len(out)-1] != id {
			out = append(out, id)
		}
	}
	return out
}

func uintFromAny(v interface{}) uint {
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
	case json.Number:
		x, _ := n.Int64()
		if x > 0 {
			return uint(x)
		}
	}
	return 0
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

func sanitizeL3RouterAllowedIPs(values []string) []string {
	uniq := make(map[string]struct{}, len(values))
	clean := make([]string, 0, len(values))
	for _, value := range values {
		cidr := strings.TrimSpace(value)
		if cidr == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		addr := prefix.Addr()
		// Guard against obvious self-route/loopback mistakes from manual payload edits.
		if addr.IsLoopback() || addr.IsLinkLocalUnicast() {
			continue
		}
		if _, exists := uniq[cidr]; exists {
			continue
		}
		uniq[cidr] = struct{}{}
		clean = append(clean, cidr)
	}
	return clean
}
