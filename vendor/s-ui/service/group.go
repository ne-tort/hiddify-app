package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

type GroupService struct{}

type userGroupDTO struct {
	Id                   uint   `json:"id"`
	Name                 string `json:"name"`
	Desc                 string `json:"desc"`
	ParentGroupIds       []uint `json:"parent_group_ids"`
	MemberOfGroupIds     []uint `json:"member_of_group_ids"`
	DirectMemberCount    int    `json:"direct_member_count"`
	EffectiveMemberCount int    `json:"effective_member_count"`
	// MemberCount matches effective_member_count for older API consumers.
	MemberCount         int    `json:"member_count"`
	MemberClientIds     []uint `json:"member_client_ids"`
	EffectiveMemberClientIds []uint `json:"effective_member_client_ids,omitempty"`
}

// GetAllGroups returns flat list of groups (for UI tables).
func (s *GroupService) GetAllGroups(db *gorm.DB) ([]userGroupDTO, error) {
	var rows []model.UserGroup
	if err := db.Model(model.UserGroup{}).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	parentsByChild := map[uint][]uint{}
	var edges []model.GroupGroupMember
	if err := db.Model(model.GroupGroupMember{}).Find(&edges).Error; err != nil {
		return nil, err
	}
	for _, e := range edges {
		parentsByChild[e.ChildGroupId] = append(parentsByChild[e.ChildGroupId], e.ParentGroupId)
	}
	for id := range parentsByChild {
		sort.Slice(parentsByChild[id], func(i, j int) bool { return parentsByChild[id][i] < parentsByChild[id][j] })
	}

	out := make([]userGroupDTO, 0, len(rows))
	for _, g := range rows {
		var n int64
		_ = db.Model(model.ClientGroupMember{}).Where("group_id = ?", g.Id).Count(&n).Error
		ids, _ := s.GetGroupMemberClientIDs(db, g.Id)
		pg := parentsByChild[g.Id]
		if pg == nil {
			pg = []uint{}
		}
		effIDs, err := s.ResolveMemberClientIDs(db, g.Id)
		if err != nil {
			return nil, err
		}
		var effEnabled []uint
		if len(effIDs) > 0 {
			if err := db.Model(model.Client{}).Where("id in ? AND enable = ?", effIDs, true).Order("id").Pluck("id", &effEnabled).Error; err != nil {
				return nil, err
			}
		}
		direct := int(n)
		effective := len(effEnabled)
		out = append(out, userGroupDTO{
			Id:                       g.Id,
			Name:                     g.Name,
			Desc:                     g.Desc,
			ParentGroupIds:           pg,
			MemberOfGroupIds:         pg,
			DirectMemberCount:        direct,
			EffectiveMemberCount:     effective,
			MemberCount:              effective,
			MemberClientIds:          ids,
			EffectiveMemberClientIds: effEnabled,
		})
	}
	return out, nil
}

// GetGroupMemberClientIDs returns direct members only.
func (s *GroupService) GetGroupMemberClientIDs(tx *gorm.DB, groupID uint) ([]uint, error) {
	var ids []uint
	err := tx.Model(model.ClientGroupMember{}).Where("group_id = ?", groupID).Pluck("client_id", &ids).Error
	return ids, err
}

// ChildGroupIDs returns immediate child groups (nested under parentID).
func (s *GroupService) ChildGroupIDs(tx *gorm.DB, parentID uint) ([]uint, error) {
	var ids []uint
	err := tx.Model(model.GroupGroupMember{}).Where("parent_group_id = ?", parentID).Pluck("child_group_id", &ids).Error
	return ids, err
}

// DescendantGroupIDs returns rootID and all descendant group ids (BFS along child links).
func (s *GroupService) DescendantGroupIDs(tx *gorm.DB, rootID uint) ([]uint, error) {
	seen := map[uint]struct{}{rootID: {}}
	queue := []uint{rootID}
	out := []uint{rootID}
	for len(queue) > 0 {
		gid := queue[0]
		queue = queue[1:]
		children, err := s.ChildGroupIDs(tx, gid)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out, nil
}

// ResolveMemberClientIDs returns every client that is a direct member of this group or any descendant group.
func (s *GroupService) ResolveMemberClientIDs(tx *gorm.DB, groupID uint) ([]uint, error) {
	gids, err := s.DescendantGroupIDs(tx, groupID)
	if err != nil {
		return nil, err
	}
	if len(gids) == 0 {
		return nil, nil
	}
	var ids []uint
	err = tx.Model(model.ClientGroupMember{}).Where("group_id in ?", gids).Distinct().Pluck("client_id", &ids).Error
	if err != nil {
		return nil, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	uniq := ids[:0]
	var prev uint
	for i, id := range ids {
		if i == 0 || id != prev {
			uniq = append(uniq, id)
			prev = id
		}
	}
	return uniq, nil
}

// ResolveMemberUsernames returns client names for ResolveMemberClientIDs.
func (s *GroupService) ResolveMemberUsernames(tx *gorm.DB, groupID uint) ([]string, error) {
	ids, err := s.ResolveMemberClientIDs(tx, groupID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var clients []model.Client
	if err := tx.Model(model.Client{}).Where("id in ?", ids).Select("id", "name").Find(&clients).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, len(clients))
	for _, c := range clients {
		if strings.TrimSpace(c.Name) != "" {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ClientShouldHaveL3Router returns true if the client is listed in member_client_ids or belongs to
// member_group_ids (including nested groups via ResolveMemberClientIDs) for any l3router endpoint.
// Legacy bound_group_id is still honored until migrated into member_group_ids.
func (s *GroupService) ClientShouldHaveL3Router(tx *gorm.DB, clientID uint) (bool, error) {
	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return false, err
	}
	for _, ep := range endpoints {
		var opt map[string]interface{}
		if len(ep.Options) == 0 {
			continue
		}
		if err := json.Unmarshal(ep.Options, &opt); err != nil {
			continue
		}
		for _, cid := range uintListFromInterface(opt["member_client_ids"]) {
			if cid == clientID {
				return true, nil
			}
		}
		groupIDs := uintListFromInterface(opt["member_group_ids"])
		if bg := uintFromOptionsAny(opt["bound_group_id"]); bg > 0 {
			groupIDs = append(groupIDs, bg)
		}
		seenG := map[uint]struct{}{}
		for _, gid := range groupIDs {
			if gid == 0 {
				continue
			}
			if _, dup := seenG[gid]; dup {
				continue
			}
			seenG[gid] = struct{}{}
			ids, err := s.ResolveMemberClientIDs(tx, gid)
			if err != nil {
				return false, err
			}
			for _, id := range ids {
				if id == clientID {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func uintFromOptionsAny(v interface{}) uint {
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

func uintListFromInterface(raw interface{}) []uint {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]uint, 0, len(v))
		for _, x := range v {
			id := uintFromOptionsAny(x)
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

func boundGroupIDFromOptions(raw json.RawMessage) uint {
	if len(raw) == 0 {
		return 0
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(raw, &opt); err != nil {
		return 0
	}
	v, ok := opt["bound_group_id"]
	if !ok {
		return 0
	}
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

func boundGroupNameFromOptions(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(raw, &opt); err != nil {
		return ""
	}
	v, ok := opt["bound_group_name"]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// FillClientGroupIDs sets GroupIds on each client (API-only field).
func (s *GroupService) FillClientGroupIDs(tx *gorm.DB, clients *[]model.Client) {
	if clients == nil {
		return
	}
	cl := *clients
	if len(cl) == 0 {
		return
	}
	ids := make([]uint, 0, len(cl))
	idx := map[uint]int{}
	for i := range cl {
		if cl[i].Id == 0 {
			continue
		}
		ids = append(ids, cl[i].Id)
		idx[cl[i].Id] = i
	}
	if len(ids) == 0 {
		return
	}
	var members []model.ClientGroupMember
	_ = tx.Where("client_id IN ?", ids).Find(&members).Error
	byClient := map[uint][]uint{}
	for _, m := range members {
		byClient[m.ClientId] = append(byClient[m.ClientId], m.GroupId)
	}
	for id, gids := range byClient {
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		if i, ok := idx[id]; ok {
			cl[i].GroupIds = gids
		}
	}
	for i := range cl {
		if cl[i].Id == 0 {
			continue
		}
		if _, has := byClient[cl[i].Id]; !has {
			cl[i].GroupIds = []uint{}
		}
	}
	*clients = cl
}

// SyncClientGroupMemberships replaces all group memberships for a client.
func (s *GroupService) SyncClientGroupMemberships(tx *gorm.DB, clientID uint, groupIDs []uint) error {
	if clientID == 0 {
		return common.NewErrorf("sync groups: invalid client id (0)")
	}
	if err := tx.Where("client_id = ?", clientID).Delete(model.ClientGroupMember{}).Error; err != nil {
		return err
	}
	seen := map[uint]struct{}{}
	for _, gid := range groupIDs {
		if gid == 0 {
			continue
		}
		if _, ok := seen[gid]; ok {
			continue
		}
		seen[gid] = struct{}{}
		var n int64
		if err := tx.Model(model.UserGroup{}).Where("id = ?", gid).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return common.NewErrorf("unknown group id: %d", gid)
		}
		if err := tx.Create(&model.ClientGroupMember{ClientId: clientID, GroupId: gid}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *GroupService) wouldEdgeCreateCycle(tx *gorm.DB, parentID, childID uint) (bool, error) {
	if parentID == childID {
		return true, nil
	}
	desc, err := s.DescendantGroupIDs(tx, childID)
	if err != nil {
		return false, err
	}
	for _, id := range desc {
		if id == parentID {
			return true, nil
		}
	}
	return false, nil
}

func (s *GroupService) replaceChildParentEdges(tx *gorm.DB, childID uint, parentIDs []uint) error {
	seen := map[uint]struct{}{}
	uniq := make([]uint, 0, len(parentIDs))
	for _, p := range parentIDs {
		if p == 0 || p == childID {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		var cnt int64
		if err := tx.Model(model.UserGroup{}).Where("id = ?", p).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt == 0 {
			return common.NewErrorf("unknown parent group id: %d", p)
		}
		uniq = append(uniq, p)
	}
	if err := tx.Where("child_group_id = ?", childID).Delete(model.GroupGroupMember{}).Error; err != nil {
		return err
	}
	for _, p := range uniq {
		cycle, err := s.wouldEdgeCreateCycle(tx, p, childID)
		if err != nil {
			return err
		}
		if cycle {
			return common.NewErrorf("group edge would create a cycle")
		}
		if err := tx.Create(&model.GroupGroupMember{ParentGroupId: p, ChildGroupId: childID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *GroupService) replaceParentChildEdges(tx *gorm.DB, parentID uint, childIDs []uint) error {
	seen := map[uint]struct{}{}
	uniq := make([]uint, 0, len(childIDs))
	for _, c := range childIDs {
		if c == 0 || c == parentID {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		var cnt int64
		if err := tx.Model(model.UserGroup{}).Where("id = ?", c).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt == 0 {
			return common.NewErrorf("unknown child group id: %d", c)
		}
		uniq = append(uniq, c)
	}
	if err := tx.Where("parent_group_id = ?", parentID).Delete(model.GroupGroupMember{}).Error; err != nil {
		return err
	}
	for _, c := range uniq {
		cycle, err := s.wouldEdgeCreateCycle(tx, parentID, c)
		if err != nil {
			return err
		}
		if cycle {
			return common.NewErrorf("group edge would create a cycle")
		}
		if err := tx.Create(&model.GroupGroupMember{ParentGroupId: parentID, ChildGroupId: c}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *GroupService) setGroupClientMembers(tx *gorm.DB, groupID uint, clientIDs []uint) error {
	if err := tx.Where("group_id = ?", groupID).Delete(model.ClientGroupMember{}).Error; err != nil {
		return err
	}
	for _, cid := range clientIDs {
		if cid == 0 {
			continue
		}
		if err := tx.Create(&model.ClientGroupMember{ClientId: cid, GroupId: groupID}).Error; err != nil {
			return err
		}
	}
	return nil
}

type groupSavePayload struct {
	Id               uint    `json:"id"`
	Name             string  `json:"name"`
	Desc             string  `json:"desc"`
	ChildGroupIds    *[]uint `json:"child_group_ids"`
	ParentGroupIds   *[]uint `json:"parent_group_ids"`
	MemberOfGroupIds *[]uint `json:"member_of_group_ids"`
	ClientIds        *[]uint `json:"client_ids"`
}

// childEdgesFromPayload returns nested child IDs and whether edges should be updated.
// Canonical API: child_group_ids (parent -> children). Legacy parent/memberOf keys are aliases.
func childEdgesFromPayload(child, parent, memberOf *[]uint, requireForNew bool) (children []uint, updateEdges bool, err error) {
	if child != nil && (parent != nil || memberOf != nil) {
		return nil, false, common.NewErrorf("conflicting group edge fields: child_group_ids with parent/member_of")
	}
	if child != nil {
		return *child, true, nil
	}
	if memberOf != nil {
		return *memberOf, true, nil
	}
	if parent != nil {
		return *parent, true, nil
	}
	if requireForNew {
		return nil, true, nil
	}
	return nil, false, nil
}

func (s *GroupService) cleanupGroupReferences(tx *gorm.DB, groupID uint) error {
	if err := tx.Where("group_id = ?", groupID).Delete(model.InboundPolicyGroup{}).Error; err != nil {
		return err
	}
	if err := s.cleanupEndpointL3GroupReferences(tx, groupID); err != nil {
		return err
	}
	if err := s.cleanupConfigAuthGroupReferences(tx, groupID); err != nil {
		return err
	}
	return nil
}

func (s *GroupService) cleanupEndpointL3GroupReferences(tx *gorm.DB, groupID uint) error {
	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return err
	}
	for _, ep := range endpoints {
		var opt map[string]interface{}
		if len(ep.Options) > 0 {
			if err := json.Unmarshal(ep.Options, &opt); err != nil {
				return err
			}
		} else {
			continue
		}
		changed := false
		if raw, ok := opt["member_group_ids"]; ok {
			if pruned, rm := pruneGroupIDList(raw, groupID); rm {
				changed = true
				if len(pruned) == 0 {
					delete(opt, "member_group_ids")
				} else {
					opt["member_group_ids"] = pruned
				}
			}
		}
		if uintFromOptionsAny(opt["bound_group_id"]) == groupID {
			delete(opt, "bound_group_id")
			delete(opt, "bound_group_name")
			changed = true
		}
		if !changed {
			continue
		}
		raw, err := json.MarshalIndent(opt, "", " ")
		if err != nil {
			return err
		}
		if err := tx.Model(model.Endpoint{}).Where("id = ?", ep.Id).Update("options", raw).Error; err != nil {
			return err
		}
	}
	return nil
}

func pruneGroupIDList(raw interface{}, groupID uint) ([]interface{}, bool) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	changed := false
	out := make([]interface{}, 0, len(arr))
	for _, x := range arr {
		if toUint(x) == groupID {
			changed = true
			continue
		}
		out = append(out, x)
	}
	return out, changed
}

func pruneGroupIDFromRules(rules []interface{}, groupID uint) bool {
	changed := false
	for _, rule := range rules {
		m, ok := rule.(map[string]interface{})
		if !ok {
			continue
		}
		if raw, ok := m[suiAuthGroupsKey]; ok {
			if pruned, rm := pruneGroupIDList(raw, groupID); rm {
				changed = true
				if len(pruned) == 0 {
					delete(m, suiAuthGroupsKey)
				} else {
					m[suiAuthGroupsKey] = pruned
				}
			}
		}
		if nested, ok := m["rules"].([]interface{}); ok {
			if pruneGroupIDFromRules(nested, groupID) {
				changed = true
			}
		}
	}
	return changed
}

func (s *GroupService) cleanupConfigAuthGroupReferences(tx *gorm.DB, groupID uint) error {
	var row model.Setting
	if err := tx.Model(model.Setting{}).Where("key = ?", "config").First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(row.Value), &cfg); err != nil {
		return err
	}
	changed := false
	if route, ok := cfg["route"].(map[string]interface{}); ok {
		if rules, ok := route["rules"].([]interface{}); ok {
			if pruneGroupIDFromRules(rules, groupID) {
				changed = true
			}
		}
	}
	if dns, ok := cfg["dns"].(map[string]interface{}); ok {
		if rules, ok := dns["rules"].([]interface{}); ok {
			if pruneGroupIDFromRules(rules, groupID) {
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return tx.Model(model.Setting{}).Where("key = ?", "config").Update("value", string(raw)).Error
}

func (s *GroupService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	switch act {
	case "new":
		var p groupSavePayload
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return common.NewErrorf("group name required")
		}
		g := model.UserGroup{Name: p.Name, Desc: p.Desc}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		children, _, err := childEdgesFromPayload(p.ChildGroupIds, p.ParentGroupIds, p.MemberOfGroupIds, true)
		if err != nil {
			return err
		}
		if err := s.replaceParentChildEdges(tx, g.Id, children); err != nil {
			return err
		}
		if p.ClientIds != nil {
			if err := s.setGroupClientMembers(tx, g.Id, *p.ClientIds); err != nil {
				return err
			}
		}
		if err := MasqueRecalcServerAuthLeafPins(tx); err != nil {
			return err
		}
		return nil
	case "edit":
		var p groupSavePayload
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return common.NewErrorf("group name required")
		}
		if p.Id == 0 {
			return common.NewErrorf("group id required")
		}
		if err := tx.Model(model.UserGroup{}).Where("id = ?", p.Id).Updates(map[string]interface{}{
			"name": p.Name,
			"desc": p.Desc,
		}).Error; err != nil {
			return err
		}
		children, updateEdges, err := childEdgesFromPayload(p.ChildGroupIds, p.ParentGroupIds, p.MemberOfGroupIds, false)
		if err != nil {
			return err
		}
		if updateEdges {
			if err := s.replaceParentChildEdges(tx, p.Id, children); err != nil {
				return err
			}
		}
		if p.ClientIds != nil {
			if err := s.setGroupClientMembers(tx, p.Id, *p.ClientIds); err != nil {
				return err
			}
		}
		if err := MasqueRecalcServerAuthLeafPins(tx); err != nil {
			return err
		}
		return nil
	case "del":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		if err := s.cleanupGroupReferences(tx, id); err != nil {
			return err
		}
		if err := tx.Where("parent_group_id = ? OR child_group_id = ?", id, id).Delete(model.GroupGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(model.ClientGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", id).Delete(model.UserGroup{}).Error; err != nil {
			return err
		}
		if err := MasqueRecalcServerAuthLeafPins(tx); err != nil {
			return err
		}
		return nil
	case "setMembers":
		var payload struct {
			GroupId            uint    `json:"group_id"`
			ClientIds          []uint  `json:"client_ids"`
			ChildGroupIds      *[]uint `json:"child_group_ids"`
			ParentGroupIds     *[]uint `json:"parent_group_ids"`
			MemberOfGroupIds   *[]uint `json:"member_of_group_ids"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		if payload.GroupId == 0 {
			return common.NewErrorf("group_id required")
		}
		if err := s.setGroupClientMembers(tx, payload.GroupId, payload.ClientIds); err != nil {
			return err
		}
		children, updateEdges, err := childEdgesFromPayload(payload.ChildGroupIds, payload.ParentGroupIds, payload.MemberOfGroupIds, false)
		if err != nil {
			return err
		}
		if updateEdges {
			if err := s.replaceParentChildEdges(tx, payload.GroupId, children); err != nil {
				return err
			}
		}
		if err := MasqueRecalcServerAuthLeafPins(tx); err != nil {
			return err
		}
		return nil
	default:
		return common.NewErrorf("unknown groups action: %s", act)
	}
}

// RunDataMigrations runs post-migrate setup (must not import package database from db.go).
func (s *GroupService) RunDataMigrations(db *gorm.DB) error {
	tx := db.Begin()
	if err := MigrateL3RouterBoundGroups(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

// MigrateL3RouterBoundGroups moves legacy bound_group_id into member_group_ids and drops bound_* keys.
// It does not create UserGroups.
func MigrateL3RouterBoundGroups(tx *gorm.DB) error {
	var endpoints []model.Endpoint
	if err := tx.Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return err
	}
	for _, ep := range endpoints {
		bound := boundGroupIDFromOptions(ep.Options)
		if bound == 0 {
			continue
		}
		var opt map[string]interface{}
		if len(ep.Options) > 0 {
			if err := json.Unmarshal(ep.Options, &opt); err != nil {
				continue
			}
		} else {
			opt = make(map[string]interface{})
		}
		existing := uintListFromInterface(opt["member_group_ids"])
		seen := map[uint]struct{}{}
		merged := make([]interface{}, 0, len(existing)+1)
		for _, g := range existing {
			if _, ok := seen[g]; ok {
				continue
			}
			seen[g] = struct{}{}
			merged = append(merged, float64(g))
		}
		if _, ok := seen[bound]; !ok {
			merged = append(merged, float64(bound))
		}
		opt["member_group_ids"] = merged
		delete(opt, "bound_group_id")
		delete(opt, "bound_group_name")
		raw, err := json.MarshalIndent(opt, "", " ")
		if err != nil {
			return err
		}
		if err := tx.Model(model.Endpoint{}).Where("id = ?", ep.Id).Update("options", raw).Error; err != nil {
			return err
		}
	}
	return nil
}
