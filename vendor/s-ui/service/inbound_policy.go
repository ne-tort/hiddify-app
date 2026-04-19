package service

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

const (
	InboundPolicyNone    = "none"
	InboundPolicyAll     = "all"
	InboundPolicyGroups  = "groups"
	InboundPolicyClients = "clients"
)

// normalizeInboundPolicyMode maps legacy/frontend aliases and normalizes casing for policy rows.
func normalizeInboundPolicyMode(m string) string {
	s := strings.TrimSpace(strings.ToLower(m))
	switch s {
	case "group":
		return InboundPolicyGroups
	case "client":
		return InboundPolicyClients
	case "", "none", "all", "groups", "clients":
		return s
	default:
		return s
	}
}

// InboundInitPayload is sent as JSON form field inboundInit (and parsed from legacy initUsers when absent).
type InboundInitPayload struct {
	Mode      string `json:"mode"`
	GroupIds  []uint `json:"group_ids"`
	ClientIds []uint `json:"client_ids"`
}

func (s *InboundService) saveInboundUserPolicy(tx *gorm.DB, inboundID uint, payload InboundInitPayload) error {
	if err := tx.Where("inbound_id = ?", inboundID).Delete(model.InboundPolicyGroup{}).Error; err != nil {
		return err
	}
	if err := tx.Where("inbound_id = ?", inboundID).Delete(model.InboundPolicyClient{}).Error; err != nil {
		return err
	}
	if err := tx.Where("inbound_id = ?", inboundID).Delete(model.InboundUserPolicy{}).Error; err != nil {
		return err
	}
	mode := normalizeInboundPolicyMode(payload.Mode)
	if mode == "" {
		mode = InboundPolicyNone
	}
	pol := model.InboundUserPolicy{InboundId: inboundID, Mode: mode}
	if err := tx.Create(&pol).Error; err != nil {
		return err
	}
	switch mode {
	case InboundPolicyGroups:
		for _, gid := range payload.GroupIds {
			if gid == 0 {
				continue
			}
			if err := tx.Create(&model.InboundPolicyGroup{InboundId: inboundID, GroupId: gid}).Error; err != nil {
				return err
			}
		}
	case InboundPolicyClients:
		for _, cid := range payload.ClientIds {
			if cid == 0 {
				continue
			}
			if err := tx.Create(&model.InboundPolicyClient{InboundId: inboundID, ClientId: cid}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolvePolicyClientIDs returns the desired client ids for an inbound policy (enabled clients only).
func (s *InboundService) ResolvePolicyClientIDs(tx *gorm.DB, inboundID uint) ([]uint, error) {
	var pol model.InboundUserPolicy
	if err := tx.Where("inbound_id = ?", inboundID).First(&pol).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	gs := GroupService{}
	switch normalizeInboundPolicyMode(pol.Mode) {
	case InboundPolicyNone, "":
		return nil, nil
	case InboundPolicyAll:
		var ids []uint
		if err := tx.Model(model.Client{}).Where("enable = ?", true).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return uniqSorted(ids), nil
	case InboundPolicyGroups:
		var gids []uint
		if err := tx.Model(model.InboundPolicyGroup{}).Where("inbound_id = ?", inboundID).Pluck("group_id", &gids).Error; err != nil {
			return nil, err
		}
		seen := map[uint]struct{}{}
		for _, gid := range gids {
			if gid == 0 {
				continue
			}
			ids, err := gs.ResolveMemberClientIDs(tx, gid)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				seen[id] = struct{}{}
			}
		}
		out := make([]uint, 0, len(seen))
		for id := range seen {
			out = append(out, id)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		// only enabled
		if len(out) == 0 {
			return nil, nil
		}
		var enabled []uint
		if err := tx.Model(model.Client{}).Where("id in ? AND enable = ?", out, true).Pluck("id", &enabled).Error; err != nil {
			return nil, err
		}
		sort.Slice(enabled, func(i, j int) bool { return enabled[i] < enabled[j] })
		return enabled, nil
	case InboundPolicyClients:
		var ids []uint
		if err := tx.Model(model.InboundPolicyClient{}).Where("inbound_id = ?", inboundID).Pluck("client_id", &ids).Error; err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		var enabled []uint
		if err := tx.Model(model.Client{}).Where("id in ? AND enable = ?", ids, true).Pluck("id", &enabled).Error; err != nil {
			return nil, err
		}
		sort.Slice(enabled, func(i, j int) bool { return enabled[i] < enabled[j] })
		return enabled, nil
	default:
		return nil, nil
	}
}

func uniqSorted(ids []uint) []uint {
	if len(ids) <= 1 {
		return ids
	}
	out := []uint{ids[0]}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1] {
			out = append(out, ids[i])
		}
	}
	return out
}

// ReconcileInboundClients syncs clients.inbounds and links to match policy for this inbound.
func (s *InboundService) ReconcileInboundClients(tx *gorm.DB, inboundID uint, hostname string) error {
	var in model.Inbound
	if err := tx.Model(model.Inbound{}).Preload("Tls").Where("id = ?", inboundID).First(&in).Error; err != nil {
		return err
	}
	if !s.hasUser(in.Type) {
		return nil
	}
	desired, err := s.ResolvePolicyClientIDs(tx, inboundID)
	if err != nil {
		return err
	}
	desiredSet := map[uint]struct{}{}
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}

	var current []uint
	if err := tx.Raw(`SELECT DISTINCT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?`, inboundID).Scan(&current).Error; err != nil {
		return err
	}
	currentSet := map[uint]struct{}{}
	for _, id := range current {
		currentSet[id] = struct{}{}
	}

	var toAdd, toRem []uint
	for id := range desiredSet {
		if _, ok := currentSet[id]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	for id := range currentSet {
		if _, ok := desiredSet[id]; !ok {
			toRem = append(toRem, id)
		}
	}
	sort.Slice(toAdd, func(i, j int) bool { return toAdd[i] < toAdd[j] })
	sort.Slice(toRem, func(i, j int) bool { return toRem[i] < toRem[j] })

	if len(toAdd) > 0 {
		initStr := joinUintSlice(toAdd)
		if err := s.ClientService.UpdateClientsOnInboundAdd(tx, initStr, inboundID, hostname); err != nil {
			return err
		}
	}
	for _, cid := range toRem {
		var cl model.Client
		if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
			continue
		}
		if err := s.ClientService.RemoveInboundFromClient(tx, &cl, inboundID, in.Tag); err != nil {
			return err
		}
	}
	return nil
}

// ParseInboundInit parses JSON field inboundInit; falls back to legacy comma-separated client ids as mode=clients.
// For act=="edit", empty inboundInit and empty initUserIds means skip policy update (preserve DB).
func ParseInboundInit(act, inboundInitJSON, initUserIds string) (InboundInitPayload, bool, error) {
	inboundInitJSON = strings.TrimSpace(inboundInitJSON)
	initUserIds = strings.TrimSpace(initUserIds)
	if inboundInitJSON != "" {
		var p InboundInitPayload
		if err := json.Unmarshal([]byte(inboundInitJSON), &p); err != nil {
			return InboundInitPayload{}, false, err
		}
		p.Mode = normalizeInboundPolicyMode(p.Mode)
		if p.Mode == "" {
			p.Mode = InboundPolicyNone
		}
		return p, false, nil
	}
	if act == "edit" && initUserIds == "" {
		return InboundInitPayload{}, true, nil
	}
	if initUserIds != "" {
		parts := strings.Split(initUserIds, ",")
		var ids []uint
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				continue
			}
			ids = append(ids, uint(n))
		}
		return InboundInitPayload{Mode: InboundPolicyClients, ClientIds: ids}, false, nil
	}
	return InboundInitPayload{Mode: InboundPolicyNone}, false, nil
}

func inboundIDsAffectedByClientChange(tx *gorm.DB, clientID uint) ([]uint, error) {
	var ids []uint
	err := tx.Raw(`
SELECT DISTINCT p.inbound_id FROM inbound_user_policies p
JOIN inbounds i ON i.id = p.inbound_id
WHERE p.mode IN (?, ?)
   OR (p.mode = ? AND EXISTS (
     SELECT 1 FROM inbound_policy_clients c WHERE c.inbound_id = p.inbound_id AND c.client_id = ?
   ))`,
		InboundPolicyAll, InboundPolicyGroups, InboundPolicyClients, clientID).Scan(&ids).Error
	return ids, err
}

func inboundIDsWithGroupPolicy(tx *gorm.DB) ([]uint, error) {
	var ids []uint
	err := tx.Model(model.InboundUserPolicy{}).
		Where("mode = ?", InboundPolicyGroups).
		Pluck("inbound_id", &ids).Error
	return ids, err
}

// ReconcileInboundPoliciesForClient runs reconcile for inbounds whose ACL may include this client.
func ReconcileInboundPoliciesForClient(tx *gorm.DB, clientID uint, hostname string) ([]uint, error) {
	s := &InboundService{}
	ids, err := inboundIDsAffectedByClientChange(tx, clientID)
	if err != nil {
		return nil, err
	}
	var out []uint
	for _, id := range ids {
		var in model.Inbound
		if err := tx.Model(model.Inbound{}).Where("id = ?", id).First(&in).Error; err != nil {
			continue
		}
		if !s.inboundSupportsUserPolicy(&in) {
			continue
		}
		if err := s.ReconcileInboundClients(tx, id, hostname); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// ReconcileInboundPoliciesForGroupMembers reconciles every inbound with mode=groups (DAG / membership changes).
func ReconcileInboundPoliciesForGroupMembers(tx *gorm.DB, hostname string) ([]uint, error) {
	s := &InboundService{}
	ids, err := inboundIDsWithGroupPolicy(tx)
	if err != nil {
		return nil, err
	}
	var out []uint
	for _, id := range ids {
		var in model.Inbound
		if err := tx.Model(model.Inbound{}).Where("id = ?", id).First(&in).Error; err != nil {
			continue
		}
		if !s.inboundSupportsUserPolicy(&in) {
			continue
		}
		if err := s.ReconcileInboundClients(tx, id, hostname); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// MergePolicyReconcile unions RestartInbounds targets with reconciles for the given client ids.
func MergePolicyReconcile(tx *gorm.DB, inboundIds []uint, clientIDs []uint, hostname string) ([]uint, error) {
	out := inboundIds
	for _, cid := range clientIDs {
		r, err := ReconcileInboundPoliciesForClient(tx, cid, hostname)
		if err != nil {
			return nil, err
		}
		out = common.UnionUintArray(out, r)
	}
	return out, nil
}

func joinUintSlice(ids []uint) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ",")
}
