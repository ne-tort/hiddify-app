package service

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

// PersistL3RouterRouteRules writes at most two route rules per l3router endpoint into Settings "config"
// (so they show on the Rules page). They are re-generated on each endpoint/group save that touches L3.
//
// Sing-box route rules are one conjunction per rule (no OR). L3 needs two different match shapes:
//
//  1) Overlay: UDP to overlay_destination (fake IP + port) from user inbounds → l3router outbound.
//     Inbound tags limit the rule to client-facing inbounds (same path as NewPacketConnectionEx / metadata.User).
//  2) Peer: auth_user and/or peer allowed_ips (ip_cidr) → same outbound, for traffic that is not the overlay tuple.
//
// Both can be required in one deployment; merging into one rule is impossible without a logical OR rule type.
func PersistL3RouterRouteRules(tx *gorm.DB) error {
	var st model.Setting
	if err := tx.Where("key = ?", "config").First(&st).Error; err != nil {
		return err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(st.Value), &cfg); err != nil {
		return err
	}

	var route map[string]interface{}
	if raw, ok := cfg["route"].(map[string]interface{}); ok && raw != nil {
		route = raw
	} else {
		route = map[string]interface{}{}
		cfg["route"] = route
	}

	var existing []interface{}
	if rules, ok := route["rules"].([]interface{}); ok {
		existing = append(existing, rules...)
	}
	// Do not strip l3router_managed before shouldDropStoredL3AutoRule: peer auto-rules need the
	// marker (or reliable shape) so old copies are removed; stripping caused duplicates when
	// ip_cidr had multiple prefixes or only auth_user was set.

	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&endpoints).Error; err != nil {
		return err
	}

	l3Tags := make(map[string]struct{})
	for _, ep := range endpoints {
		if strings.TrimSpace(ep.Tag) != "" {
			l3Tags[strings.TrimSpace(ep.Tag)] = struct{}{}
		}
	}

	inboundTags, err := collectInboundTags(tx)
	if err != nil {
		return err
	}

	filtered := make([]interface{}, 0, len(existing))
	for _, raw := range existing {
		if shouldDropStoredL3AutoRule(raw, l3Tags) {
			continue
		}
		filtered = append(filtered, raw)
	}

	insertAt := l3RuleInsertIndex(filtered)
	if insertAt > len(filtered) {
		insertAt = len(filtered)
	}
	var generated []interface{}

	for _, ep := range endpoints {
		tag := strings.TrimSpace(ep.Tag)
		if tag == "" {
			continue
		}
		var opt map[string]interface{}
		if len(ep.Options) > 0 {
			_ = json.Unmarshal(ep.Options, &opt)
		}
		overlayHost, overlayPort, err := parseOverlayDestination(opt)
		if err != nil {
			continue
		}
		ipStr := overlayHost.String()
		// One rule for all user inbounds: route overlay UDP (fake destination) to this L3 endpoint.
		// (Previously one duplicate rule per inbound tag; sing-box matches inbound as a list.)
		if len(inboundTags) > 0 {
			tags := append([]string(nil), inboundTags...)
			sort.Strings(tags)
			inboundList := make([]interface{}, len(tags))
			for i, t := range tags {
				inboundList[i] = t
			}
			generated = append(generated, map[string]interface{}{
				"inbound":  inboundList,
				"ip_cidr":  []interface{}{ipStr + "/32"},
				"port":     []interface{}{overlayPort},
				"network":  []interface{}{"udp"},
				"action":   "route",
				"outbound": tag,
			})
		}

		peersRaw, _ := opt["peers"].([]interface{})
		authSeen := make(map[string]struct{})
		cidrSeen := make(map[string]struct{})
		var authUsers []interface{}
		var allCIDRs []interface{}
		for _, pr := range peersRaw {
			peer, ok := pr.(map[string]interface{})
			if !ok {
				continue
			}
			if u, ok := peer["user"].(string); ok {
				u = strings.TrimSpace(u)
				if u != "" {
					if _, dup := authSeen[u]; !dup {
						authSeen[u] = struct{}{}
						authUsers = append(authUsers, u)
					}
				}
			}
			for _, cidr := range toStringSlice(peer["allowed_ips"]) {
				cidr = strings.TrimSpace(cidr)
				if cidr == "" || !isValidRoutableCIDR(cidr) {
					continue
				}
				if _, dup := cidrSeen[cidr]; !dup {
					cidrSeen[cidr] = struct{}{}
					allCIDRs = append(allCIDRs, cidr)
				}
			}
		}
		if len(allCIDRs) > 0 || len(authUsers) > 0 {
			gen := map[string]interface{}{
				"l3router_managed": true,
				"action":           "route",
				"outbound":         tag,
			}
			if len(authUsers) > 0 {
				gen["auth_user"] = authUsers
			}
			if len(allCIDRs) > 0 {
				gen["ip_cidr"] = allCIDRs
			}
			generated = append(generated, gen)
		}
	}

	out := make([]interface{}, 0, len(filtered)+len(generated))
	out = append(out, filtered[:insertAt]...)
	out = append(out, generated...)
	out = append(out, filtered[insertAt:]...)
	route["rules"] = out

	updated, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return tx.Model(&model.Setting{}).Where("key = ?", "config").Update("value", string(updated)).Error
}

func shouldDropStoredL3AutoRule(raw interface{}, l3Tags map[string]struct{}) bool {
	rule, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	outbound, _ := rule["outbound"].(string)
	outbound = strings.TrimSpace(outbound)
	if outbound == "" {
		return false
	}
	if _, isL3 := l3Tags[outbound]; !isL3 {
		// Only strip orphaned overlay rules (unique shape). Do not drop generic ip_cidr rules
		// (they would match normal traffic rules to "direct", "block", etc.).
		return isL3OverlayShape(rule)
	}
	if isL3OverlayShape(rule) {
		return true
	}
	if isL3PeerAutoShape(rule) {
		return true
	}
	return false
}

func isL3OverlayShape(rule map[string]interface{}) bool {
	if _, ok := rule["inbound"]; !ok {
		return false
	}
	if _, ok := rule["port"]; !ok {
		return false
	}
	netw := toStringSlice(rule["network"])
	if len(netw) != 1 || strings.ToLower(netw[0]) != "udp" {
		return false
	}
	ips := toStringSlice(rule["ip_cidr"])
	return len(ips) == 1
}

func isL3PeerAutoShape(rule map[string]interface{}) bool {
	if managed, ok := rule["l3router_managed"].(bool); ok && managed {
		return true
	}
	if _, ok := rule["inbound"]; ok {
		return false
	}
	if _, ok := rule["port"]; ok {
		return false
	}
	// Legacy auto rules (before marker was respected) or multi-CIDR / auth-only generated rules.
	if isL3OverlayShape(rule) {
		return false
	}
	a, _ := rule["action"].(string)
	if a != "" && !strings.EqualFold(a, "route") {
		return false
	}
	if hasAuthUserField(rule["auth_user"]) {
		return true
	}
	ips := toStringSlice(rule["ip_cidr"])
	return len(ips) > 0
}

func hasAuthUserField(v interface{}) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) != ""
	case []interface{}:
		return len(x) > 0
	case []string:
		return len(x) > 0
	default:
		return false
	}
}

func l3RuleInsertIndex(rules []interface{}) int {
	if len(rules) >= 2 && isSniffRule(rules[0]) && isHijackDNSRule(rules[1]) {
		return 2
	}
	if len(rules) >= 1 && isSniffRule(rules[0]) {
		return 1
	}
	return 0
}

func isSniffRule(raw interface{}) bool {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	a, _ := m["action"].(string)
	return strings.EqualFold(a, "sniff")
}

func isHijackDNSRule(raw interface{}) bool {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	a, _ := m["action"].(string)
	if !strings.EqualFold(a, "hijack-dns") {
		return false
	}
	p := m["protocol"]
	if p == nil {
		return true
	}
	switch v := p.(type) {
	case string:
		return strings.EqualFold(v, "dns")
	case []string:
		for _, x := range v {
			if strings.EqualFold(x, "dns") {
				return true
			}
		}
		return false
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.EqualFold(s, "dns") {
				return true
			}
		}
	}
	return false
}

func collectInboundTags(tx *gorm.DB) ([]string, error) {
	svc := InboundService{}
	raws, err := svc.GetAllConfig(tx)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, raw := range raws {
		var m struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &m); err != nil || strings.TrimSpace(m.Tag) == "" {
			continue
		}
		tags = append(tags, m.Tag)
	}
	return tags, nil
}

func parseOverlayDestination(opt map[string]interface{}) (netip.Addr, int, error) {
	raw := l3RouterDefaultOverlayDst
	if opt != nil {
		if s, ok := opt["overlay_destination"].(string); ok && strings.TrimSpace(s) != "" {
			raw = strings.TrimSpace(s)
		}
	}
	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return netip.Addr{}, 0, fmt.Errorf("invalid overlay port")
	}
	return addr, port, nil
}

func nthUsableIPv4InPool(pool netip.Prefix, index int) (netip.Prefix, error) {
	if !pool.Addr().Is4() {
		return netip.Prefix{}, errors.New("private_subnet: only IPv4 pools are supported")
	}
	bits := pool.Bits()
	if bits < 8 || bits > 30 {
		return netip.Prefix{}, errors.New("private_subnet: use mask between /8 and /30")
	}
	hostBits := 32 - bits
	max := (1 << hostBits)
	if bits < 31 {
		max -= 2 // exclude network and broadcast for typical subnets
	}
	if index < 0 || index >= max {
		return netip.Prefix{}, fmt.Errorf("private_subnet: pool exhausted (need more addresses)")
	}
	base := pool.Masked().Addr().As4()
	baseU := binary.BigEndian.Uint32(base[:])
	var firstHost uint32
	if bits >= 31 {
		firstHost = baseU
	} else {
		firstHost = baseU + 1
	}
	host := firstHost + uint32(index)
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], host)
	addr, ok := netip.AddrFromSlice(out[:])
	if !ok {
		return netip.Prefix{}, errors.New("private_subnet: address build failed")
	}
	return netip.PrefixFrom(addr, 32), nil
}

// EnsureL3RouterAutoPrivateSubnet sets options.private_subnet to the first free 10.x.y.0/24
// that does not overlap any other L3Router's private_subnet (excluding ep.Id).
// Returns true if a new subnet was written to ep.Options.
func EnsureL3RouterAutoPrivateSubnet(tx *gorm.DB, ep *model.Endpoint) (bool, error) {
	var options map[string]interface{}
	if len(ep.Options) > 0 {
		if err := json.Unmarshal(ep.Options, &options); err != nil {
			return false, err
		}
	} else {
		options = make(map[string]interface{})
	}
	if s, ok := options["private_subnet"].(string); ok && strings.TrimSpace(s) != "" {
		return false, nil
	}
	used, err := collectL3RouterPrivateSubnets(tx, ep.Id)
	if err != nil {
		return false, err
	}
	picked, err := pickFirstFreeL3RouterSubnet24(used)
	if err != nil {
		return false, err
	}
	options["private_subnet"] = picked.String()
	updated, err := json.MarshalIndent(options, "", " ")
	if err != nil {
		return false, err
	}
	ep.Options = updated
	return true, nil
}

// SuggestNextL3PrivateSubnet returns the first free 10.x.y.0/24 not overlapping existing L3Router private_subnet values.
func SuggestNextL3PrivateSubnet(db *gorm.DB, excludeID uint) (string, error) {
	used, err := collectL3RouterPrivateSubnets(db, excludeID)
	if err != nil {
		return "", err
	}
	p, err := pickFirstFreeL3RouterSubnet24(used)
	if err != nil {
		return "", err
	}
	return p.String(), nil
}

func collectL3RouterPrivateSubnets(tx *gorm.DB, excludeID uint) ([]netip.Prefix, error) {
	var eps []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Find(&eps).Error; err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, e := range eps {
		if excludeID != 0 && e.Id == excludeID {
			continue
		}
		if len(e.Options) == 0 {
			continue
		}
		var opt map[string]interface{}
		if err := json.Unmarshal(e.Options, &opt); err != nil {
			continue
		}
		s, _ := opt["private_subnet"].(string)
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			continue
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func pickFirstFreeL3RouterSubnet24(used []netip.Prefix) (netip.Prefix, error) {
	for second := 0; second < 256; second++ {
		for third := 0; third < 256; third++ {
			s := fmt.Sprintf("10.%d.%d.0/24", second, third)
			p, err := netip.ParsePrefix(s)
			if err != nil {
				continue
			}
			conflict := false
			for _, u := range used {
				if ipv4PrefixesOverlap(p, u) {
					conflict = true
					break
				}
			}
			if !conflict {
				return p, nil
			}
		}
	}
	return netip.Prefix{}, errors.New("no free 10.x.y.0/24 in 10.0.0.0/8 for L3Router")
}

func ipv4PrefixHostRange(p netip.Prefix) (first, last uint32, ok bool) {
	if !p.Addr().Is4() {
		return 0, 0, false
	}
	p = p.Masked()
	a4 := p.Addr().As4()
	base := binary.BigEndian.Uint32(a4[:])
	bits := p.Bits()
	if bits < 0 || bits > 32 {
		return 0, 0, false
	}
	hostBits := 32 - bits
	if hostBits == 0 {
		return base, base, true
	}
	nHosts := uint32(1) << hostBits
	lastAddr := base + nHosts - 1
	return base, lastAddr, true
}

func ipv4PrefixesOverlap(a, b netip.Prefix) bool {
	af, al, ok1 := ipv4PrefixHostRange(a.Masked())
	bf, bl, ok2 := ipv4PrefixHostRange(b.Masked())
	if !ok1 || !ok2 {
		return false
	}
	return af <= bl && bf <= al
}

func isPrivateRFC1918Prefix(p netip.Prefix) bool {
	if !p.Addr().Is4() {
		return false
	}
	a := p.Masked().Addr()
	b := a.As4()
	// 10.0.0.0/8
	if b[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if b[0] == 172 && b[1] >= 16 && b[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if b[0] == 192 && b[1] == 168 {
		return true
	}
	// 100.64.0.0/10 (CGNAT — часто используют как «внутреннюю» транспортную сеть)
	if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
		return true
	}
	return false
}
