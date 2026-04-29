package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"
	"gorm.io/gorm"
)

type RoutingProfilesService struct{}

type routingProfileDTO struct {
	Id            uint            `json:"id"`
	Name          string          `json:"name"`
	Desc          string          `json:"desc"`
	Enabled       bool            `json:"enabled"`
	RouteOrder    []string        `json:"route_order"`
	DirectSites   []string        `json:"direct_sites"`
	DirectIp      []string        `json:"direct_ip"`
	ProxySites    []string        `json:"proxy_sites"`
	ProxyIp       []string        `json:"proxy_ip"`
	BlockSites    []string        `json:"block_sites"`
	BlockIp       []string        `json:"block_ip"`
	DnsPolicy     json.RawMessage `json:"dns_policy"`
	Compatibility json.RawMessage `json:"compatibility"`
	GeoCatalogVer string          `json:"geo_catalog_version"`
	LastError     string          `json:"last_validate_error"`
	ClientIds     []uint          `json:"client_ids,omitempty"`
	GroupIds      []uint          `json:"group_ids,omitempty"`
	// Backward compatibility for previously shipped frontend payloads.
	RouteOrderLegacy  []string        `json:"routeOrder,omitempty"`
	DirectSitesLegacy []string        `json:"directSites,omitempty"`
	DirectIpLegacy    []string        `json:"directIp,omitempty"`
	ProxySitesLegacy  []string        `json:"proxySites,omitempty"`
	ProxyIpLegacy     []string        `json:"proxyIp,omitempty"`
	BlockSitesLegacy  []string        `json:"blockSites,omitempty"`
	BlockIpLegacy     []string        `json:"blockIp,omitempty"`
	DnsPolicyLegacy   json.RawMessage `json:"dnsPolicy,omitempty"`
	ClientIdsLegacy   []uint          `json:"clientIds,omitempty"`
	GroupIdsLegacy    []uint          `json:"groupIds,omitempty"`
}

type routingValidationResult struct {
	Ok       bool     `json:"ok"`
	Warnings []string `json:"warnings"`
}

func (p *routingProfileDTO) normalizeLegacyAliases() {
	if len(p.RouteOrder) == 0 && len(p.RouteOrderLegacy) > 0 {
		p.RouteOrder = append([]string(nil), p.RouteOrderLegacy...)
	}
	if len(p.DirectSites) == 0 && len(p.DirectSitesLegacy) > 0 {
		p.DirectSites = append([]string(nil), p.DirectSitesLegacy...)
	}
	if len(p.DirectIp) == 0 && len(p.DirectIpLegacy) > 0 {
		p.DirectIp = append([]string(nil), p.DirectIpLegacy...)
	}
	if len(p.ProxySites) == 0 && len(p.ProxySitesLegacy) > 0 {
		p.ProxySites = append([]string(nil), p.ProxySitesLegacy...)
	}
	if len(p.ProxyIp) == 0 && len(p.ProxyIpLegacy) > 0 {
		p.ProxyIp = append([]string(nil), p.ProxyIpLegacy...)
	}
	if len(p.BlockSites) == 0 && len(p.BlockSitesLegacy) > 0 {
		p.BlockSites = append([]string(nil), p.BlockSitesLegacy...)
	}
	if len(p.BlockIp) == 0 && len(p.BlockIpLegacy) > 0 {
		p.BlockIp = append([]string(nil), p.BlockIpLegacy...)
	}
	if len(p.DnsPolicy) == 0 && len(p.DnsPolicyLegacy) > 0 {
		p.DnsPolicy = append(json.RawMessage(nil), p.DnsPolicyLegacy...)
	}
	if len(p.ClientIds) == 0 && len(p.ClientIdsLegacy) > 0 {
		p.ClientIds = append([]uint(nil), p.ClientIdsLegacy...)
	}
	if len(p.GroupIds) == 0 && len(p.GroupIdsLegacy) > 0 {
		p.GroupIds = append([]uint(nil), p.GroupIdsLegacy...)
	}
}

func normalizeGeoToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, "geoip:") || strings.HasPrefix(token, "geosite:") {
		if strings.HasSuffix(token, ":") {
			return ""
		}
		return token
	}
	return token
}

func normalizeStringList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := normalizeGeoToken(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func marshalList(v []string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func unmarshalList(raw json.RawMessage) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func (s *RoutingProfilesService) GetAll(tx *gorm.DB) ([]routingProfileDTO, error) {
	var rows []model.RoutingProfile
	if err := tx.Model(model.RoutingProfile{}).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]routingProfileDTO, 0, len(rows))
	for _, r := range rows {
		clientIDs, groupIDs, err := s.getMembership(tx, r.Id)
		if err != nil {
			return nil, err
		}
		out = append(out, routingProfileDTO{
			Id:            r.Id,
			Name:          r.Name,
			Desc:          r.Desc,
			Enabled:       r.Enabled,
			RouteOrder:    unmarshalList(r.RouteOrder),
			DirectSites:   unmarshalList(r.DirectSites),
			DirectIp:      unmarshalList(r.DirectIp),
			ProxySites:    unmarshalList(r.ProxySites),
			ProxyIp:       unmarshalList(r.ProxyIp),
			BlockSites:    unmarshalList(r.BlockSites),
			BlockIp:       unmarshalList(r.BlockIp),
			DnsPolicy:     r.DnsPolicy,
			Compatibility: r.Compatibility,
			GeoCatalogVer: r.GeoCatalogVer,
			LastError:     r.LastValidateErr,
			ClientIds:     clientIDs,
			GroupIds:      groupIDs,
		})
	}
	return out, nil
}

func (s *RoutingProfilesService) GetByID(tx *gorm.DB, id uint) (*model.RoutingProfile, error) {
	var row model.RoutingProfile
	if err := tx.Model(model.RoutingProfile{}).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *RoutingProfilesService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	switch act {
	case "new":
		var p routingProfileDTO
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		p.normalizeLegacyAliases()
		return s.create(tx, p)
	case "edit":
		var p routingProfileDTO
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		p.normalizeLegacyAliases()
		return s.update(tx, p)
	case "del":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(model.RoutingProfile{}).Error
	case "validate":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		res, err := s.ValidateProfile(tx, id)
		if err != nil {
			return err
		}
		lastErr := strings.Join(res.Warnings, "; ")
		return tx.Model(model.RoutingProfile{}).Where("id = ?", id).Update("last_validate_err", lastErr).Error
	case "setMembers":
		var payload struct {
			ProfileId uint   `json:"profile_id"`
			ClientIds []uint `json:"client_ids"`
			GroupIds  []uint `json:"group_ids"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		if payload.ProfileId == 0 {
			return common.NewErrorf("profile_id required")
		}
		return s.setMembership(tx, payload.ProfileId, payload.ClientIds, payload.GroupIds)
	default:
		return common.NewErrorf("unknown routing_profiles action: %s", act)
	}
}

func (s *RoutingProfilesService) create(tx *gorm.DB, p routingProfileDTO) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return common.NewErrorf("profile name required")
	}
	row := model.RoutingProfile{
		Name:          p.Name,
		Desc:          strings.TrimSpace(p.Desc),
		Enabled:       p.Enabled,
		RouteOrder:    marshalList(normalizeStringList(p.RouteOrder)),
		DirectSites:   marshalList(normalizeStringList(p.DirectSites)),
		DirectIp:      marshalList(normalizeStringList(p.DirectIp)),
		ProxySites:    marshalList(normalizeStringList(p.ProxySites)),
		ProxyIp:       marshalList(normalizeStringList(p.ProxyIp)),
		BlockSites:    marshalList(normalizeStringList(p.BlockSites)),
		BlockIp:       marshalList(normalizeStringList(p.BlockIp)),
		DnsPolicy:     p.DnsPolicy,
		Compatibility: p.Compatibility,
		GeoCatalogVer: strings.TrimSpace(p.GeoCatalogVer),
	}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	return s.setMembership(tx, row.Id, p.ClientIds, p.GroupIds)
}

func (s *RoutingProfilesService) update(tx *gorm.DB, p routingProfileDTO) error {
	if p.Id == 0 {
		return common.NewErrorf("profile id required")
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return common.NewErrorf("profile name required")
	}
	if err := tx.Model(model.RoutingProfile{}).Where("id = ?", p.Id).Updates(map[string]interface{}{
		"name":            p.Name,
		"desc":            strings.TrimSpace(p.Desc),
		"enabled":         p.Enabled,
		"route_order":     marshalList(normalizeStringList(p.RouteOrder)),
		"direct_sites":    marshalList(normalizeStringList(p.DirectSites)),
		"direct_ip":       marshalList(normalizeStringList(p.DirectIp)),
		"proxy_sites":     marshalList(normalizeStringList(p.ProxySites)),
		"proxy_ip":        marshalList(normalizeStringList(p.ProxyIp)),
		"block_sites":     marshalList(normalizeStringList(p.BlockSites)),
		"block_ip":        marshalList(normalizeStringList(p.BlockIp)),
		"dns_policy":      p.DnsPolicy,
		"compatibility":   p.Compatibility,
		"geo_catalog_ver": strings.TrimSpace(p.GeoCatalogVer),
	}).Error; err != nil {
		return err
	}
	return s.setMembership(tx, p.Id, p.ClientIds, p.GroupIds)
}

func uniqUint(in []uint) []uint {
	seen := map[uint]struct{}{}
	out := make([]uint, 0, len(in))
	for _, v := range in {
		if v == 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *RoutingProfilesService) setMembership(tx *gorm.DB, profileID uint, clientIDs []uint, groupIDs []uint) error {
	clientIDs = uniqUint(clientIDs)
	groupIDs = uniqUint(groupIDs)
	if err := tx.Where("profile_id = ?", profileID).Delete(model.RoutingProfileClientMember{}).Error; err != nil {
		return err
	}
	if err := tx.Where("profile_id = ?", profileID).Delete(model.RoutingProfileGroupMember{}).Error; err != nil {
		return err
	}
	for _, cid := range clientIDs {
		var n int64
		if err := tx.Model(model.Client{}).Where("id = ?", cid).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return common.NewErrorf("unknown client id: %d", cid)
		}
		if err := tx.Create(&model.RoutingProfileClientMember{ProfileId: profileID, ClientId: cid}).Error; err != nil {
			return err
		}
	}
	for _, gid := range groupIDs {
		var n int64
		if err := tx.Model(model.UserGroup{}).Where("id = ?", gid).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return common.NewErrorf("unknown group id: %d", gid)
		}
		if err := tx.Create(&model.RoutingProfileGroupMember{ProfileId: profileID, GroupId: gid}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *RoutingProfilesService) getMembership(tx *gorm.DB, profileID uint) ([]uint, []uint, error) {
	var clients []uint
	if err := tx.Model(model.RoutingProfileClientMember{}).Where("profile_id = ?", profileID).Pluck("client_id", &clients).Error; err != nil {
		return nil, nil, err
	}
	var groups []uint
	if err := tx.Model(model.RoutingProfileGroupMember{}).Where("profile_id = ?", profileID).Pluck("group_id", &groups).Error; err != nil {
		return nil, nil, err
	}
	return uniqUint(clients), uniqUint(groups), nil
}

func (s *RoutingProfilesService) ValidateProfile(tx *gorm.DB, id uint) (routingValidationResult, error) {
	var row model.RoutingProfile
	if err := tx.Model(model.RoutingProfile{}).Where("id = ?", id).First(&row).Error; err != nil {
		return routingValidationResult{}, err
	}
	tokens := make([]string, 0, 64)
	tokens = append(tokens, unmarshalList(row.DirectSites)...)
	tokens = append(tokens, unmarshalList(row.DirectIp)...)
	tokens = append(tokens, unmarshalList(row.ProxySites)...)
	tokens = append(tokens, unmarshalList(row.ProxyIp)...)
	tokens = append(tokens, unmarshalList(row.BlockSites)...)
	tokens = append(tokens, unmarshalList(row.BlockIp)...)

	warnings := make([]string, 0)
	seen := map[string]struct{}{}
	for _, tok := range tokens {
		tok = normalizeGeoToken(tok)
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		if strings.HasPrefix(tok, "geosite:") {
			tag := strings.TrimPrefix(tok, "geosite:")
			var n int64
			if err := tx.Model(model.GeoTag{}).Where("dataset_kind = ? AND tag_norm = ? AND is_deleted = ?", "geosite", tag, false).Count(&n).Error; err != nil {
				return routingValidationResult{}, err
			}
			if n == 0 {
				warnings = append(warnings, "unknown token "+tok)
			}
		}
		if strings.HasPrefix(tok, "geoip:") {
			tag := strings.TrimPrefix(tok, "geoip:")
			var n int64
			if err := tx.Model(model.GeoTag{}).Where("dataset_kind = ? AND tag_norm = ? AND is_deleted = ?", "geoip", tag, false).Count(&n).Error; err != nil {
				return routingValidationResult{}, err
			}
			if n == 0 {
				warnings = append(warnings, "unknown token "+tok)
			}
		}
	}
	sort.Strings(warnings)
	return routingValidationResult{
		Ok:       len(warnings) == 0,
		Warnings: warnings,
	}, nil
}

// BuildHappPayload compiles profile fields into Happ-compatible routing JSON.
func (s *RoutingProfilesService) BuildHappPayload(row model.RoutingProfile) (json.RawMessage, error) {
	payload := map[string]interface{}{
		"DirectSites": normalizeStringList(unmarshalList(row.DirectSites)),
		"DirectIp":    normalizeStringList(unmarshalList(row.DirectIp)),
		"ProxySites":  normalizeStringList(unmarshalList(row.ProxySites)),
		"ProxyIp":     normalizeStringList(unmarshalList(row.ProxyIp)),
		"BlockSites":  normalizeStringList(unmarshalList(row.BlockSites)),
		"BlockIp":     normalizeStringList(unmarshalList(row.BlockIp)),
		"RouteOrder":  normalizeStringList(unmarshalList(row.RouteOrder)),
	}
	if len(row.DnsPolicy) > 0 {
		var dns map[string]interface{}
		if err := json.Unmarshal(row.DnsPolicy, &dns); err == nil {
			for k, v := range dns {
				payload[k] = v
			}
		}
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BuildHappRoutingLink returns happ://routing/add/{base64json}.
func (s *RoutingProfilesService) BuildHappRoutingLink(row model.RoutingProfile) (string, error) {
	payload, err := s.BuildHappPayload(row)
	if err != nil {
		return "", err
	}
	return "happ://routing/add/" + base64.StdEncoding.EncodeToString(payload), nil
}

// BuildSingboxManagedRules emits managed route rules from profile tokens.
func (s *RoutingProfilesService) BuildSingboxManagedRules(row model.RoutingProfile) []map[string]interface{} {
	rules := make([]map[string]interface{}, 0)
	addRules := func(tokens []string, action string, site bool) {
		for _, tok := range normalizeStringList(tokens) {
			r := map[string]interface{}{
				"action": "route",
			}
			if action == "block" {
				r["action"] = "reject"
			} else if action == "proxy" {
				r["outbound"] = "select"
			} else {
				r["outbound"] = "direct"
			}
			if strings.HasPrefix(tok, "geosite:") {
				r["rule_set"] = []string{"geosite-" + strings.TrimPrefix(tok, "geosite:")}
			} else if strings.HasPrefix(tok, "geoip:") {
				r["rule_set"] = []string{"geoip-" + strings.TrimPrefix(tok, "geoip:")}
			} else if site {
				r["domain"] = []string{tok}
			} else {
				r["ip_cidr"] = []string{tok}
			}
			r["routing_profile_managed"] = true
			rules = append(rules, r)
		}
	}
	addRules(unmarshalList(row.BlockSites), "block", true)
	addRules(unmarshalList(row.BlockIp), "block", false)
	addRules(unmarshalList(row.DirectSites), "direct", true)
	addRules(unmarshalList(row.DirectIp), "direct", false)
	addRules(unmarshalList(row.ProxySites), "proxy", true)
	addRules(unmarshalList(row.ProxyIp), "proxy", false)
	return rules
}

func mergeRuleBuckets(rows []model.RoutingProfile) (blockSites, blockIPs, directSites, directIPs, proxySites, proxyIPs []string) {
	blockSet := map[string]struct{}{}
	directSet := map[string]struct{}{}
	proxySet := map[string]struct{}{}

	addOrdered := func(dst *[]string, set map[string]struct{}, tokens []string) {
		for _, tok := range normalizeStringList(tokens) {
			if _, ok := set[tok]; ok {
				continue
			}
			set[tok] = struct{}{}
			*dst = append(*dst, tok)
		}
	}
	for _, r := range rows {
		addOrdered(&blockSites, blockSet, unmarshalList(r.BlockSites))
		addOrdered(&blockIPs, blockSet, unmarshalList(r.BlockIp))
	}
	for _, r := range rows {
		tmp := make([]string, 0)
		for _, tok := range normalizeStringList(unmarshalList(r.DirectSites)) {
			if _, blocked := blockSet[tok]; blocked {
				continue
			}
			tmp = append(tmp, tok)
		}
		addOrdered(&directSites, directSet, tmp)
		tmp = tmp[:0]
		for _, tok := range normalizeStringList(unmarshalList(r.DirectIp)) {
			if _, blocked := blockSet[tok]; blocked {
				continue
			}
			tmp = append(tmp, tok)
		}
		addOrdered(&directIPs, directSet, tmp)
	}
	for _, r := range rows {
		tmp := make([]string, 0)
		for _, tok := range normalizeStringList(unmarshalList(r.ProxySites)) {
			if _, blocked := blockSet[tok]; blocked {
				continue
			}
			if _, directed := directSet[tok]; directed {
				continue
			}
			tmp = append(tmp, tok)
		}
		addOrdered(&proxySites, proxySet, tmp)
		tmp = tmp[:0]
		for _, tok := range normalizeStringList(unmarshalList(r.ProxyIp)) {
			if _, blocked := blockSet[tok]; blocked {
				continue
			}
			if _, directed := directSet[tok]; directed {
				continue
			}
			tmp = append(tmp, tok)
		}
		addOrdered(&proxyIPs, proxySet, tmp)
	}
	return
}

func (s *RoutingProfilesService) ResolveProfilesForClient(tx *gorm.DB, clientID uint) ([]model.RoutingProfile, error) {
	if clientID == 0 {
		return nil, common.NewErrorf("client id required")
	}
	idsSet := map[uint]struct{}{}
	var directProfileIDs []uint
	if err := tx.Model(model.RoutingProfileClientMember{}).Where("client_id = ?", clientID).Pluck("profile_id", &directProfileIDs).Error; err != nil {
		return nil, err
	}
	for _, id := range directProfileIDs {
		idsSet[id] = struct{}{}
	}
	var groupIDs []uint
	if err := tx.Model(model.ClientGroupMember{}).Where("client_id = ?", clientID).Pluck("group_id", &groupIDs).Error; err != nil {
		return nil, err
	}
	gs := GroupService{}
	expandedGroups := make([]uint, 0, len(groupIDs))
	for _, gid := range uniqUint(groupIDs) {
		desc, err := gs.DescendantGroupIDs(tx, gid)
		if err != nil {
			return nil, err
		}
		expandedGroups = append(expandedGroups, desc...)
	}
	expandedGroups = uniqUint(expandedGroups)
	if len(expandedGroups) > 0 {
		var groupProfileIDs []uint
		if err := tx.Model(model.RoutingProfileGroupMember{}).Where("group_id in ?", expandedGroups).Pluck("profile_id", &groupProfileIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range groupProfileIDs {
			idsSet[id] = struct{}{}
		}
	}
	ids := make([]uint, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return []model.RoutingProfile{}, nil
	}
	var rows []model.RoutingProfile
	if err := tx.Model(model.RoutingProfile{}).Where("id in ? AND enabled = ?", ids, true).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *RoutingProfilesService) BuildMergedHappPayload(rows []model.RoutingProfile) (json.RawMessage, error) {
	return s.BuildMergedHappPayloadWithGeoBase(rows, "")
}

func (s *RoutingProfilesService) BuildMergedHappPayloadWithGeoBase(rows []model.RoutingProfile, geoBaseURL string) (json.RawMessage, error) {
	blockSites, blockIPs, directSites, directIPs, proxySites, proxyIPs := mergeRuleBuckets(rows)
	ensureList := func(in []string) []string {
		if in == nil {
			return []string{}
		}
		return in
	}
	payload := map[string]interface{}{
		"DirectSites": ensureList(directSites),
		"DirectIp":    ensureList(directIPs),
		"ProxySites":  ensureList(proxySites),
		"ProxyIp":     ensureList(proxyIPs),
		"BlockSites":  ensureList(blockSites),
		"BlockIp":     ensureList(blockIPs),
		// Happ mobile is strict about this field shape.
		"RouteOrder": "block-direct-proxy",
		// Keep compatibility defaults aligned with Happ exported routing profile.
		"Name":             "Default",
		"GlobalProxy":      "true",
		"FakeDNS":          "false",
		"DomainStrategy":   "IPIfNonMatch",
		"RemoteDNSType":    "DoH",
		"RemoteDNSIP":      "1.1.1.1",
		"RemoteDNSDomain":  "https://cloudflare-dns.com/dns-query",
		"DomesticDNSType":  "DoH",
		"DomesticDNSIP":    "8.8.8.8",
		"DomesticDNSDomain":"https://dns.google/dns-query",
		"DnsHosts": map[string]string{
			"cloudflare-dns.com": "1.1.1.1",
			"dns.google":         "8.8.8.8",
		},
		"LastUpdated": time.Now().Unix(),
	}
	geoBaseURL = strings.TrimRight(strings.TrimSpace(geoBaseURL), "/")
	if geoBaseURL != "" {
		payload["Geoipurl"] = geoBaseURL + "/geodat/geoip.dat"
		payload["Geositeurl"] = geoBaseURL + "/geodat/geosite.dat"
	}
	return json.Marshal(payload)
}

func (s *RoutingProfilesService) BuildMergedHappRoutingLink(rows []model.RoutingProfile) (string, error) {
	return s.BuildMergedHappRoutingLinkWithGeoBase(rows, "")
}

func (s *RoutingProfilesService) BuildMergedHappRoutingLinkWithGeoBase(rows []model.RoutingProfile, geoBaseURL string) (string, error) {
	payload, err := s.BuildMergedHappPayloadWithGeoBase(rows, geoBaseURL)
	if err != nil {
		return "", err
	}
	return "happ://routing/add/" + base64.StdEncoding.EncodeToString(payload), nil
}

func (s *RoutingProfilesService) BuildMergedSingboxManagedRules(rows []model.RoutingProfile) []map[string]interface{} {
	blockSites, blockIPs, directSites, directIPs, proxySites, proxyIPs := mergeRuleBuckets(rows)
	combined := model.RoutingProfile{
		BlockSites:  marshalList(blockSites),
		BlockIp:     marshalList(blockIPs),
		DirectSites: marshalList(directSites),
		DirectIp:    marshalList(directIPs),
		ProxySites:  marshalList(proxySites),
		ProxyIp:     marshalList(proxyIPs),
	}
	return s.BuildSingboxManagedRules(combined)
}

func ParseRoutingProfileID(raw string) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid profile id")
	}
	return uint(n), nil
}
