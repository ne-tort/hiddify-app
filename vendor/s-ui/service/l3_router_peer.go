package service

import (
	"encoding/json"
	"net/netip"
	"sort"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

func l3PeerEmptyJSONArray() json.RawMessage {
	b, _ := json.Marshal([]string{})
	return b
}

func l3PeerDecodeStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func l3PeerEncodeStringSlice(s []string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		return l3PeerEmptyJSONArray()
	}
	return b
}

func l3PeerDecodeAllowedCIDRs(raw json.RawMessage) []string {
	s := l3PeerDecodeStringSlice(raw)
	return sanitizeL3RouterAllowedIPs(s)
}

func (s *EndpointService) loadL3PeerRows(tx *gorm.DB, endpointID uint) ([]model.L3RouterPeer, error) {
	var rows []model.L3RouterPeer
	if err := tx.Where("endpoint_id = ?", endpointID).Order("peer_serial asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func nextL3PeerSerial(tx *gorm.DB, endpointID uint) (uint, error) {
	var maxS uint
	if err := tx.Raw("SELECT COALESCE(MAX(peer_serial), 0) FROM l3_router_peers WHERE endpoint_id = ?", endpointID).Scan(&maxS).Error; err != nil {
		return 0, err
	}
	return maxS + 1, nil
}

// sortL3IdentitiesByPeerSerial orders identities by DB peer_serial (add order), then client_id.
func sortL3IdentitiesByPeerSerial(identities []l3RouterClientIdentity, by map[uint]*model.L3RouterPeer) []l3RouterClientIdentity {
	out := append([]l3RouterClientIdentity(nil), identities...)
	sort.SliceStable(out, func(i, j int) bool {
		si := peerSerialForSort(out[i].ClientID, by)
		sj := peerSerialForSort(out[j].ClientID, by)
		if si != sj {
			return si < sj
		}
		return out[i].ClientID < out[j].ClientID
	})
	return out
}

func peerSerialForSort(clientID uint, by map[uint]*model.L3RouterPeer) uint {
	if r := by[clientID]; r != nil && r.PeerSerial > 0 {
		return r.PeerSerial
	}
	return ^uint(0)
}

// l3AppendPeerUIOrder sets peer_order for default table sort (not sent to sing-box — stripped from stored options).
func l3AppendPeerUIOrder(peers []map[string]interface{}, rows []model.L3RouterPeer) {
	by := make(map[uint]uint, len(rows))
	for i := range rows {
		by[rows[i].ClientId] = rows[i].PeerSerial
	}
	for _, p := range peers {
		cid := uintFromAny(p["client_id"])
		if cid == 0 {
			continue
		}
		if v, ok := by[cid]; ok && v > 0 {
			p["peer_order"] = v
		}
	}
}

func l3PeerRowsByClientID(rows []model.L3RouterPeer) map[uint]*model.L3RouterPeer {
	m := make(map[uint]*model.L3RouterPeer, len(rows))
	for i := range rows {
		m[rows[i].ClientId] = &rows[i]
	}
	return m
}

func collectUsedPoolCIDRsFromRows(rows []model.L3RouterPeer, poolPrefix netip.Prefix, skipClientID uint) map[string]struct{} {
	used := make(map[string]struct{})
	if !poolPrefix.IsValid() {
		return used
	}
	for i := range rows {
		if rows[i].ClientId == skipClientID {
			continue
		}
		for _, c := range l3PeerDecodeAllowedCIDRs(rows[i].AllowedCIDRs) {
			pr, err := netip.ParsePrefix(c)
			if err != nil || !poolPrefix.Contains(pr.Addr()) {
				continue
			}
			used[pr.Masked().String()] = struct{}{}
		}
	}
	return used
}

func validateL3PoolNoDuplicateCIDRS(peers []map[string]interface{}, poolPrefix netip.Prefix) error {
	if !poolPrefix.IsValid() {
		return nil
	}
	seen := make(map[string]uint)
	for _, p := range peers {
		cid := uintFromAny(p["client_id"])
		for _, c := range sanitizeL3RouterAllowedIPs(toStringSlice(p["allowed_ips"])) {
			pr, err := netip.ParsePrefix(strings.TrimSpace(c))
			if err != nil || !poolPrefix.Contains(pr.Addr()) {
				continue
			}
			k := pr.Masked().String()
			if other, found := seen[k]; found {
				if other != cid {
					return common.NewErrorf("l3router: duplicate pool IP %s (clients %d and %d); change one via l3router_peer save", k, other, cid)
				}
			} else {
				seen[k] = cid
			}
		}
	}
	return nil
}

func validateNonPoolNoDuplicateCIDRS(peers []map[string]interface{}) error {
	seen := make(map[string]uint)
	for _, p := range peers {
		cid := uintFromAny(p["client_id"])
		for _, c := range sanitizeL3RouterAllowedIPs(toStringSlice(p["allowed_ips"])) {
			cidr := strings.TrimSpace(c)
			if cidr == "" {
				continue
			}
			if other, found := seen[cidr]; found {
				if other != cid {
					return common.NewErrorf("l3router: duplicate allowed_ips %s (clients %d and %d)", cidr, other, cid)
				}
			} else {
				seen[cidr] = cid
			}
		}
	}
	return nil
}

// buildL3PeersViewForEndpoint returns peers for API/config from l3_router_peers + client identities.
func (s *EndpointService) buildL3PeersViewForEndpoint(db *gorm.DB, endpoint *model.Endpoint) ([]map[string]interface{}, error) {
	identities, err := s.collectL3RouterClientIdentitiesForEndpoint(db, endpoint)
	if err != nil {
		return nil, err
	}
	rows, err := s.loadL3PeerRows(db, endpoint.Id)
	if err != nil {
		return nil, err
	}
	by := l3PeerRowsByClientID(rows)
	allocOrder := sortL3IdentitiesByPeerSerial(identities, by)
	return s.materializeL3RouterPeerMaps(allocOrder, by)
}

// materializeL3RouterPeerMaps builds sing-box peer maps from identities and DB rows (same order as identities input).
func (s *EndpointService) materializeL3RouterPeerMaps(identities []l3RouterClientIdentity, rowsByClient map[uint]*model.L3RouterPeer) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(identities))
	for _, id := range identities {
		row := rowsByClient[id.ClientID]
		peer := map[string]interface{}{
			"peer_id":     id.PeerID,
			"user":        id.User,
			"client_id":   id.ClientID,
			"client_name": id.ClientName,
		}
		if id.GroupID > 0 {
			peer["group_id"] = id.GroupID
		}
		var allowed []string
		if row != nil {
			allowed = l3PeerDecodeAllowedCIDRs(row.AllowedCIDRs)
		}
		if len(allowed) == 0 {
			allowed = []string{defaultL3RouterCIDR(id.PeerID)}
			allowed = sanitizeL3RouterAllowedIPs(allowed)
			if len(allowed) == 0 {
				allowed = []string{defaultL3RouterCIDR(id.PeerID)}
			}
		}
		peer["allowed_ips"] = allowed
		if row != nil {
			if src := l3PeerDecodeStringSlice(row.FilterSourceIPs); len(src) > 0 {
				peer["filter_source_ips"] = src
			}
			if dst := l3PeerDecodeStringSlice(row.FilterDestinationIPs); len(dst) > 0 {
				peer["filter_destination_ips"] = dst
			}
		}
		out = append(out, peer)
	}
	return out, nil
}

func (s *EndpointService) writeL3PeersToEndpointOptions(endpoint *model.Endpoint, peers []map[string]interface{}) error {
	var options map[string]interface{}
	if len(endpoint.Options) > 0 {
		if err := json.Unmarshal(endpoint.Options, &options); err != nil {
			return err
		}
	} else {
		options = make(map[string]interface{})
	}
	if options == nil {
		options = make(map[string]interface{})
	}
	delete(options, l3PeerIPAllocKey)
	options["peers"] = peers
	raw, err := json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}
	endpoint.Options = raw
	return nil
}

// SaveL3RouterPeer updates stored L3 peer row(s) for allowed_ips / filters. object=l3router_peer, action=update.
// Pointer fields: nil = leave unchanged; non-nil = replace (empty slice clears filters).
func (s *EndpointService) SaveL3RouterPeer(tx *gorm.DB, data json.RawMessage) error {
	var req struct {
		EndpointID           uint       `json:"endpoint_id"`
		ClientID             uint       `json:"client_id"`
		AllowedIPs           *[]string  `json:"allowed_ips"`
		FilterSourceIPs      *[]string  `json:"filter_source_ips"`
		FilterDestinationIPs *[]string  `json:"filter_destination_ips"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	if req.EndpointID == 0 || req.ClientID == 0 {
		return common.NewErrorf("l3router_peer: endpoint_id and client_id required")
	}
	if req.AllowedIPs == nil && req.FilterSourceIPs == nil && req.FilterDestinationIPs == nil {
		return common.NewErrorf("l3router_peer: nothing to update")
	}
	var ep model.Endpoint
	if err := tx.First(&ep, req.EndpointID).Error; err != nil {
		return err
	}
	if ep.Type != l3RouterType {
		return common.NewErrorf("l3router_peer: not an l3router endpoint")
	}
	var row model.L3RouterPeer
	err := tx.Where("endpoint_id = ? AND client_id = ?", req.EndpointID, req.ClientID).First(&row).Error
	if err != nil {
		return common.NewErrorf("l3router_peer: peer row not found for this client on endpoint")
	}

	var options map[string]interface{}
	if len(ep.Options) > 0 {
		_ = json.Unmarshal(ep.Options, &options)
	}
	if options == nil {
		options = make(map[string]interface{})
	}
	var poolPrefix netip.Prefix
	if ps, ok := options["private_subnet"].(string); ok {
		if t := strings.TrimSpace(ps); t != "" {
			p, err := netip.ParsePrefix(t)
			if err == nil && p.Addr().Is4() && isPrivateRFC1918Prefix(p) {
				poolPrefix = p.Masked()
			}
		}
	}

	if req.AllowedIPs != nil {
		allowed := sanitizeL3RouterAllowedIPs(*req.AllowedIPs)
		if len(allowed) == 0 {
			return common.NewErrorf("l3router_peer: no valid allowed_ips")
		}
		if poolPrefix.IsValid() {
			for _, c := range allowed {
				pr, err := netip.ParsePrefix(c)
				if err != nil || !poolPrefix.Contains(pr.Addr()) {
					return common.NewErrorf("l3router_peer: allowed_ip %q must be inside private_subnet %s", c, poolPrefix.String())
				}
			}
			var others []model.L3RouterPeer
			if err := tx.Where("endpoint_id = ? AND client_id <> ?", req.EndpointID, req.ClientID).Find(&others).Error; err != nil {
				return err
			}
			for _, o := range others {
				for _, c := range l3PeerDecodeAllowedCIDRs(o.AllowedCIDRs) {
					pr, err := netip.ParsePrefix(c)
					if err != nil || !poolPrefix.Contains(pr.Addr()) {
						continue
					}
					k := pr.Masked().String()
					for _, na := range allowed {
						npr, err := netip.ParsePrefix(na)
						if err != nil {
							continue
						}
						if npr.Masked().String() == k {
							return common.NewErrorf("l3router_peer: pool address %s already used by another peer", k)
						}
					}
				}
			}
		}
		row.AllowedCIDRs = l3PeerEncodeStringSlice(allowed)
	}
	if req.FilterSourceIPs != nil {
		row.FilterSourceIPs = l3PeerEncodeStringSlice(sanitizeL3RouterAllowedIPs(*req.FilterSourceIPs))
	}
	if req.FilterDestinationIPs != nil {
		row.FilterDestinationIPs = l3PeerEncodeStringSlice(sanitizeL3RouterAllowedIPs(*req.FilterDestinationIPs))
	}
	if err := tx.Save(&row).Error; err != nil {
		return err
	}

	identities, err := s.collectL3RouterClientIdentitiesForEndpoint(tx, &ep)
	if err != nil {
		return err
	}
	rows, err := s.loadL3PeerRows(tx, ep.Id)
	if err != nil {
		return err
	}
	by := l3PeerRowsByClientID(rows)
	peers, err := s.materializeL3RouterPeerMaps(identities, by)
	if err != nil {
		return err
	}
	if poolPrefix.IsValid() {
		if err := validateL3PoolNoDuplicateCIDRS(peers, poolPrefix); err != nil {
			return err
		}
	} else {
		if err := validateNonPoolNoDuplicateCIDRS(peers); err != nil {
			return err
		}
	}
	if err := s.writeL3PeersToEndpointOptions(&ep, peers); err != nil {
		return err
	}
	return tx.Model(&model.Endpoint{}).Where("id = ?", ep.Id).Update("options", ep.Options).Error
}
