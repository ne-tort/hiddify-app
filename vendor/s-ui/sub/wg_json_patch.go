package sub

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/gorm"
)

const wgEndpointType = "wireguard"

func patchJsonForWireGuard(jsonConfig *map[string]interface{}, client *model.Client, serverHost string) error {
	return patchJsonForWireGuardWithDB(database.GetDB(), jsonConfig, client, serverHost)
}

func patchJsonForWireGuardWithDB(db *gorm.DB, jsonConfig *map[string]interface{}, client *model.Client, serverHost string) error {
	if db == nil || client == nil {
		return nil
	}
	serverHost = strings.TrimSpace(serverHost)
	if serverHost == "" {
		return nil
	}

	peer, ep, epOpt, err := firstWireGuardPeerForClient(db, client)
	if err != nil || peer == nil || ep == nil {
		return err
	}

	listenPort := intFromAny(peer["listen_port"])
	if listenPort <= 0 {
		return nil
	}
	clientPrivateKey, _ := peer["private_key"].(string)
	clientPrivateKey = strings.TrimSpace(clientPrivateKey)
	if clientPrivateKey == "" {
		return nil
	}
	serverPublicKey, _ := peer["server_public_key"].(string)
	serverPublicKey = strings.TrimSpace(serverPublicKey)
	if serverPublicKey == "" {
		return nil
	}
	localAddrs := toStringSlice(peer["allowed_ips"])
	if len(localAddrs) == 0 {
		return nil
	}

	outbounds := mapArrayFromConfig((*jsonConfig)["outbounds"])
	if len(outbounds) == 0 {
		return nil
	}

	wgTag := "wg-client"
	peerTunnelRoutes := internetFullTunnelPeerRoutes()
	if !boolFromAnyDefaultTrueSub(epOpt["internet_allow"]) {
		peerTunnelRoutes = wgInferPeerTunnelRoutes(localAddrs, epOpt)
		if len(peerTunnelRoutes) == 0 {
			peerTunnelRoutes = append([]string(nil), localAddrs...)
		}
		peerTunnelRoutes = onlyIPv4PeerRoutes(peerTunnelRoutes)
		if len(peerTunnelRoutes) == 0 {
			// e.g. peer had only IPv6 local addrs: infer again from IPv4-only peer list + no IPv6 in server address
			v4Peers := cidrStringsIPv4Only(localAddrs)
			epV4 := endpointOptionsWithIPv4AddressesOnly(epOpt)
			peerTunnelRoutes = onlyIPv4PeerRoutes(wgInferPeerTunnelRoutes(v4Peers, epV4))
			if len(peerTunnelRoutes) == 0 {
				peerTunnelRoutes = append([]string(nil), cidrStringsIPv4Only(localAddrs)...)
			}
		}
	}

	peerAllowedCopy := append([]string(nil), peerTunnelRoutes...)
	wgPeer := map[string]interface{}{
		"address":     serverHost,
		"port":        listenPort,
		"public_key":  serverPublicKey,
		"allowed_ips": stringSliceToIface(peerAllowedCopy),
	}
	if psk := strings.TrimSpace(fmt.Sprint(peer["pre_shared_key"])); psk != "" && psk != "<nil>" {
		wgPeer["pre_shared_key"] = psk
	}
	if keepalive := intFromAny(epOpt["persistent_keepalive_interval"]); keepalive > 0 {
		wgPeer["persistent_keepalive_interval"] = keepalive
	}

	wgEndpoint := map[string]interface{}{
		"type":        "wireguard",
		"tag":         wgTag,
		"address":     listableFromStringSlice(localAddrs),
		"private_key": clientPrivateKey,
		"peers":       []interface{}{wgPeer},
	}
	if detour := selectWGCloakDetourTag(epOpt, outbounds); detour != "" {
		wgEndpoint["detour"] = detour
	}
	if mtu := intFromAny(peer["mtu"]); mtu > 0 {
		wgEndpoint["mtu"] = mtu
	}
	if workers := intFromAny(peer["workers"]); workers > 0 {
		wgEndpoint["workers"] = workers
	}

	mergeWireGuardEndpoint(jsonConfig, wgTag, wgEndpoint)
	mergeWGTunAddresses(jsonConfig, localAddrs)
	insertWGRouteRules(jsonConfig, wgTag, routeIPCIDRsForWG(peerTunnelRoutes, localAddrs))
	return nil
}

// internetFullTunnelPeerRoutes is used when internet_allow is true: dual-stack default route
// in peer.allowed_ips (client sends both IPv4 and IPv6 over the tunnel).
func internetFullTunnelPeerRoutes() []string {
	return []string{"0.0.0.0/0", "::/0"}
}

// onlyIPv4PeerRoutes strips IPv6 CIDRs from peer routes (e.g. subscription must not
// expose ::/0 or ULA when internet_allow is off, even if the endpoint has ULA in address).
func onlyIPv4PeerRoutes(routes []string) []string {
	var out []string
	for _, c := range routes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ip, _, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			out = append(out, c)
		}
	}
	return out
}

func cidrStringsIPv4Only(in []string) []string {
	var out []string
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ip, _, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			out = append(out, c)
		}
	}
	return out
}

func endpointOptionsWithIPv4AddressesOnly(ep map[string]interface{}) map[string]interface{} {
	if ep == nil {
		return nil
	}
	n := make(map[string]interface{}, len(ep))
	for k, v := range ep {
		if k != "address" {
			n[k] = v
			continue
		}
		addrs := cidrStringsIPv4Only(toStringSlice(v))
		if len(addrs) == 0 {
			continue
		}
		if raw, ok := v.([]string); ok && len(raw) > 0 {
			n[k] = addrs
			continue
		}
		if raw, ok := v.([]interface{}); ok && len(raw) > 0 {
			out := make([]interface{}, 0, len(addrs))
			for _, s := range addrs {
				out = append(out, s)
			}
			n[k] = out
			continue
		}
		n[k] = addrs
	}
	return n
}

// wgInferPeerTunnelRoutes builds sing-box WireGuard peer "allowed_ips" (routes via the server)
// from the client's tunnel addresses plus endpoint network addresses.
func wgInferPeerTunnelRoutes(localAddrs []string, epOpt map[string]interface{}) []string {
	seen := map[string]struct{}{}
	var out []string
	addCIDR := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	// Client local addresses (usually /32); infer network route for IPv4.
	for _, a := range localAddrs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ip, n, err := net.ParseCIDR(a)
		if err != nil || n == nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			ones, _ := n.Mask.Size()
			// /32 from peer assignment should route the whole /24 tunnel segment.
			if ones == 32 {
				addCIDR(fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2]))
			} else {
				addCIDR(n.String())
			}
			continue
		}
		// ULA/GUA: prefer /64 for typical /128 peer assignment; skip link-local (fe80).
		if ip.IsLinkLocalUnicast() {
			continue
		}
		ones, _ := n.Mask.Size()
		if ones == 128 {
			m64 := net.CIDRMask(64, 128)
			if m64 != nil {
				n64 := &net.IPNet{IP: ip.Mask(m64), Mask: m64}
				if n64.IP != nil {
					addCIDR(n64.String())
				}
			}
		} else {
			addCIDR(n.String())
		}
	}
	// Endpoint addresses are server-side source of truth for tunnel subnet.
	if epOpt != nil {
		for _, a := range toStringSlice(epOpt["address"]) {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			ip, n, err := net.ParseCIDR(a)
			if err != nil || n == nil {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				addCIDR(n.String())
				continue
			}
			if ip.IsLinkLocalUnicast() {
				continue
			}
			ones, _ := n.Mask.Size()
			if ones == 128 {
				m64 := net.CIDRMask(64, 128)
				if m64 != nil {
					n64 := &net.IPNet{IP: ip.Mask(m64), Mask: m64}
					if n64.IP != nil {
						addCIDR(n64.String())
					}
				}
			} else {
				addCIDR(n.String())
			}
		}
	}
	return out
}

func routeIPCIDRsForWG(peerRoutes []string, localAddrs []string) []string {
	if len(peerRoutes) > 0 {
		return append([]string(nil), peerRoutes...)
	}
	return append([]string(nil), localAddrs...)
}

func selectWGCloakDetourTag(epOpt map[string]interface{}, outbounds []map[string]interface{}) string {
	if !boolFromAnySub(epOpt["cloak_enabled"]) {
		return ""
	}
	manualTag := strings.TrimSpace(fmt.Sprint(epOpt["cloak_detour_tag"]))
	if manualTag != "" && !strings.EqualFold(manualTag, "<nil>") && hasOutboundTag(outbounds, manualTag) {
		return manualTag
	}
	for _, typ := range []string{"vless", "naive", "anytls", "trojan", "hysteria2", "tuic"} {
		if tag := firstOutboundTagByType(outbounds, typ); tag != "" {
			return tag
		}
	}
	return "direct"
}

func hasOutboundTag(outbounds []map[string]interface{}, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, ob := range outbounds {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(ob["tag"])), target) {
			return true
		}
	}
	return false
}

func firstOutboundTagByType(outbounds []map[string]interface{}, wantType string) string {
	wantType = strings.TrimSpace(strings.ToLower(wantType))
	if wantType == "" {
		return ""
	}
	for _, ob := range outbounds {
		typ := strings.TrimSpace(strings.ToLower(fmt.Sprint(ob["type"])))
		tag := strings.TrimSpace(fmt.Sprint(ob["tag"]))
		if tag == "" {
			continue
		}
		if typ == wantType {
			return tag
		}
	}
	return ""
}

func boolFromAnySub(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

func boolFromAnyDefaultTrueSub(v interface{}) bool {
	if v == nil {
		return true
	}
	return boolFromAnySub(v)
}

func mergeWireGuardEndpoint(jsonConfig *map[string]interface{}, tag string, wgEndpoint map[string]interface{}) {
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
	out := make([]interface{}, 0, len(eps)+1)
	for _, e := range eps {
		m, ok := e.(map[string]interface{})
		if ok && strings.TrimSpace(fmt.Sprint(m["tag"])) == tag {
			continue
		}
		out = append(out, e)
	}
	out = append(out, wgEndpoint)
	(*jsonConfig)["endpoints"] = out
}

func insertWGRouteRules(jsonConfig *map[string]interface{}, wgTag string, cidrs []string) {
	if len(cidrs) == 0 {
		return
	}
	wgRule := map[string]interface{}{
		"ip_cidr":  cidrs,
		"outbound": wgTag,
	}
	routeRaw, ok := (*jsonConfig)["route"]
	if !ok || routeRaw == nil {
		(*jsonConfig)["route"] = map[string]interface{}{
			"auto_detect_interface": true,
			"final":                 "proxy",
			"rules":                 []interface{}{map[string]interface{}{"action": "sniff"}, wgRule},
		}
		return
	}
	route, ok := routeRaw.(map[string]interface{})
	if !ok {
		return
	}
	rulesRaw, ok := route["rules"]
	if !ok || rulesRaw == nil {
		route["rules"] = []interface{}{map[string]interface{}{"action": "sniff"}, wgRule}
		return
	}
	rules, ok := rulesRaw.([]interface{})
	if !ok {
		return
	}
	insertAt := 0
	for i, r := range rules {
		if m, ok := r.(map[string]interface{}); ok {
			if action, _ := m["action"].(string); action == "sniff" {
				insertAt = i + 1
				break
			}
		}
	}
	newRules := make([]interface{}, 0, len(rules)+1)
	newRules = append(newRules, rules[:insertAt]...)
	newRules = append(newRules, wgRule)
	newRules = append(newRules, rules[insertAt:]...)
	route["rules"] = newRules
}

func mergeWGTunAddresses(jsonConfig *map[string]interface{}, localAddrs []string) {
	if len(localAddrs) == 0 {
		return
	}
	raw, ok := (*jsonConfig)["inbounds"]
	if !ok || raw == nil {
		return
	}
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		return
	}
	for i, it := range list {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		typ := strings.TrimSpace(fmt.Sprint(m["type"]))
		if !strings.EqualFold(typ, "tun") {
			continue
		}
		seen := map[string]struct{}{}
		merged := make([]string, 0, 8)
		for _, a := range toStringSlice(m["address"]) {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if _, exists := seen[a]; exists {
				continue
			}
			seen[a] = struct{}{}
			merged = append(merged, a)
		}
		for _, a := range localAddrs {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if _, exists := seen[a]; exists {
				continue
			}
			seen[a] = struct{}{}
			merged = append(merged, a)
		}
		if len(merged) > 0 {
			m["address"] = stringSliceToIface(merged)
			list[i] = m
			(*jsonConfig)["inbounds"] = list
		}
		return
	}
}

func firstWireGuardPeerForClient(db *gorm.DB, client *model.Client) (map[string]interface{}, *model.Endpoint, map[string]interface{}, error) {
	if client == nil {
		return nil, nil, nil, nil
	}
	clientID := client.Id
	clientPub := clientWireGuardPublicKey(client.Config)
	clientName := strings.TrimSpace(client.Name)

	var endpoints []model.Endpoint
	if err := db.Where("type = ?", wgEndpointType).Order("id ASC").Find(&endpoints).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, ep := range endpoints {
		var opt map[string]interface{}
		if len(ep.Options) > 0 {
			_ = json.Unmarshal(ep.Options, &opt)
		}
		if opt == nil {
			continue
		}
		listenPort := intFromAny(opt["listen_port"])
		if listenPort <= 0 {
			continue
		}
		serverPub := wireGuardEndpointPublicKey(ep, opt)
		if serverPub == "" {
			continue
		}
		peers := normalizePeerMaps(opt["peers"])
		for _, p := range peers {
			if !wireGuardPeerMatchesClient(p, clientID, clientPub, clientName) {
				continue
			}
			priv := strings.TrimSpace(fmt.Sprint(p["private_key"]))
			if priv == "" || priv == "<nil>" {
				priv = clientWireGuardPrivateKey(client.Config)
			}
			row := map[string]interface{}{
				"listen_port":                   listenPort,
				"server_public_key":             serverPub,
				"private_key":                   priv,
				"allowed_ips":                   toStringSlice(p["allowed_ips"]),
				"pre_shared_key":                strings.TrimSpace(fmt.Sprint(p["pre_shared_key"])),
				"persistent_keepalive_interval": intFromAny(opt["persistent_keepalive_interval"]),
				"mtu":                           intFromAny(opt["mtu"]),
				"workers":                       intFromAny(opt["workers"]),
				"dns":                           toStringSlice(opt["dns"]),
			}
			return row, &ep, opt, nil
		}
	}
	return nil, nil, nil, nil
}

func wireGuardKeyFromClientConfig(config json.RawMessage, field string) string {
	if len(config) == 0 {
		return ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(config, &root); err != nil {
		return ""
	}
	raw, ok := root["wireguard"]
	if !ok || raw == nil {
		return ""
	}
	wg, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	pk, _ := wg[field].(string)
	return strings.TrimSpace(pk)
}

func clientWireGuardPublicKey(config json.RawMessage) string {
	return wireGuardKeyFromClientConfig(config, "public_key")
}

func clientWireGuardPrivateKey(config json.RawMessage) string {
	return wireGuardKeyFromClientConfig(config, "private_key")
}

func wireGuardPeerMatchesClient(p map[string]interface{}, clientID uint, clientPub, clientName string) bool {
	if uintFromAny(p["client_id"]) == clientID {
		return true
	}
	if clientPub != "" {
		pp := strings.TrimSpace(fmt.Sprint(p["public_key"]))
		if pp != "" && pp == clientPub {
			return true
		}
	}
	if clientName != "" {
		cn := strings.TrimSpace(fmt.Sprint(p["client_name"]))
		user := strings.TrimSpace(fmt.Sprint(p["user"]))
		if strings.EqualFold(cn, clientName) || strings.EqualFold(user, clientName) {
			return true
		}
	}
	return false
}

func wireGuardEndpointPublicKey(ep model.Endpoint, opt map[string]interface{}) string {
	if len(ep.Ext) > 0 {
		var ext map[string]interface{}
		if err := json.Unmarshal(ep.Ext, &ext); err == nil {
			if k, _ := ext["public_key"].(string); strings.TrimSpace(k) != "" {
				return strings.TrimSpace(k)
			}
		}
	}
	if pk, _ := opt["private_key"].(string); strings.TrimSpace(pk) != "" {
		if key, err := wgtypes.ParseKey(strings.TrimSpace(pk)); err == nil {
			return key.PublicKey().String()
		}
	}
	return ""
}

func mapArrayFromConfig(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, it := range v {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]interface{}:
		return v
	case *[]map[string]interface{}:
		if v == nil {
			return nil
		}
		return *v
	default:
		return nil
	}
}

func intFromAny(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		x, _ := strconv.Atoi(strings.TrimSpace(n))
		return x
	case json.Number:
		x, _ := n.Int64()
		return int(x)
	default:
		return 0
	}
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
	case string:
		x, err := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		if err == nil && x > 0 {
			return uint(x)
		}
	}
	return 0
}

func toStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case *[]string:
		if v == nil {
			return nil
		}
		return append([]string(nil), *v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func normalizePeerMaps(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, p := range v {
			if m, ok := p.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// NormalizeWireGuardOutboundsToEndpointsJSON moves legacy sing-box WireGuard outbounds (local_address + peer_public_key)
// into top-level "endpoints", removes them from outbounds, and strips their tags from selector/urltest/balancer groups.
// Safe to call multiple times.
func NormalizeWireGuardOutboundsToEndpointsJSON(jsonConfig *map[string]interface{}) {
	if jsonConfig == nil {
		return
	}
	outbounds := mapArrayFromConfig((*jsonConfig)["outbounds"])
	if len(outbounds) == 0 {
		return
	}
	removed := map[string]struct{}{}
	keep := make([]map[string]interface{}, 0, len(outbounds))
	for _, ob := range outbounds {
		typ, _ := ob["type"].(string)
		if typ != "wireguard" && typ != "awg" {
			keep = append(keep, ob)
			continue
		}
		tag := strings.TrimSpace(fmt.Sprint(ob["tag"]))
		if tag == "" {
			keep = append(keep, ob)
			continue
		}
		var ep map[string]interface{}
		if typ == "awg" {
			ep = legacyAwgOutboundMapToEndpoint(ob)
		} else {
			ep = legacyWireGuardOutboundMapToEndpoint(ob)
		}
		if ep == nil {
			keep = append(keep, ob)
			continue
		}
		mergeWireGuardEndpoint(jsonConfig, tag, ep)
		removed[tag] = struct{}{}
	}
	stripWireGuardTagsFromGroupOutbounds(keep, removed)
	outIf := make([]interface{}, len(keep))
	for i := range keep {
		outIf[i] = keep[i]
	}
	(*jsonConfig)["outbounds"] = outIf
}

func legacyWireGuardOutboundMapToEndpoint(ob map[string]interface{}) map[string]interface{} {
	localAddrs := toStringSlice(ob["local_address"])
	if len(localAddrs) == 0 {
		localAddrs = toStringSlice(ob["address"])
	}
	priv := strings.TrimSpace(fmt.Sprint(ob["private_key"]))
	if priv == "" || len(localAddrs) == 0 {
		return nil
	}
	peersJSON := buildWireGuardPeersFromLegacyOutboundMap(ob, localAddrs)
	if len(peersJSON) == 0 {
		return nil
	}
	tag := strings.TrimSpace(fmt.Sprint(ob["tag"]))
	ep := map[string]interface{}{
		"type":        "wireguard",
		"tag":         tag,
		"address":     stringSliceToIface(localAddrs),
		"private_key": priv,
		"peers":       peersJSON,
	}
	if mtu := intFromAny(ob["mtu"]); mtu > 0 {
		ep["mtu"] = mtu
	}
	if w := intFromAny(ob["workers"]); w > 0 {
		ep["workers"] = w
	}
	if detour := strings.TrimSpace(fmt.Sprint(ob["detour"])); detour != "" {
		ep["detour"] = detour
	}
	if noise, ok := ob["noise"]; ok && noise != nil {
		ep["noise"] = noise
	}
	return ep
}

func legacyAwgOutboundMapToEndpoint(ob map[string]interface{}) map[string]interface{} {
	localAddrs := toStringSlice(ob["local_address"])
	if len(localAddrs) == 0 {
		localAddrs = toStringSlice(ob["address"])
	}
	priv := strings.TrimSpace(fmt.Sprint(ob["private_key"]))
	if priv == "" || len(localAddrs) == 0 {
		return nil
	}
	peersJSON := buildWireGuardPeersFromLegacyOutboundMap(ob, localAddrs)
	if len(peersJSON) == 0 {
		return nil
	}
	tag := strings.TrimSpace(fmt.Sprint(ob["tag"]))
	ep := map[string]interface{}{
		"type":        awgEndpointType,
		"tag":         tag,
		"address":     stringSliceToIface(localAddrs),
		"private_key": priv,
		"peers":       peersJSON,
	}
	if mtu := intFromAny(ob["mtu"]); mtu > 0 {
		ep["mtu"] = mtu
	}
	if w := intFromAny(ob["workers"]); w > 0 {
		ep["workers"] = w
	}
	if detour := strings.TrimSpace(fmt.Sprint(ob["detour"])); detour != "" {
		ep["detour"] = detour
	}
	return ep
}

func buildWireGuardPeersFromLegacyOutboundMap(ob map[string]interface{}, localAddrs []string) []interface{} {
	rawPeers := normalizePeerMaps(ob["peers"])
	if len(rawPeers) > 0 {
		out := make([]interface{}, 0, len(rawPeers))
		for _, p := range rawPeers {
			addr := strings.TrimSpace(fmt.Sprint(p["address"]))
			if addr == "" {
				addr = strings.TrimSpace(fmt.Sprint(p["server"]))
			}
			port := intFromAny(p["port"])
			if port == 0 {
				port = intFromAny(p["server_port"])
			}
			pk := strings.TrimSpace(fmt.Sprint(p["public_key"]))
			if addr == "" || port <= 0 || pk == "" {
				continue
			}
			pm := map[string]interface{}{
				"address":    addr,
				"port":       port,
				"public_key": pk,
			}
			if a := toStringSlice(p["allowed_ips"]); len(a) > 0 {
				pm["allowed_ips"] = stringSliceToIface(a)
			} else {
				pm["allowed_ips"] = stringSliceToIface(wgInferPeerTunnelRoutes(localAddrs, nil))
			}
			if psk := normalizePSKJSON(p["pre_shared_key"]); psk != "" {
				pm["pre_shared_key"] = psk
			}
			out = append(out, pm)
		}
		if len(out) > 0 {
			return out
		}
	}
	server := strings.TrimSpace(fmt.Sprint(ob["server"]))
	port := intFromAny(ob["server_port"])
	pk := strings.TrimSpace(fmt.Sprint(ob["peer_public_key"]))
	if server == "" || port <= 0 || pk == "" {
		return nil
	}
	peer := map[string]interface{}{
		"address":     server,
		"port":        port,
		"public_key":  pk,
		"allowed_ips": stringSliceToIface(routeIPCIDRsForWG(wgInferPeerTunnelRoutes(localAddrs, nil), localAddrs)),
	}
	if psk := normalizePSKJSON(ob["pre_shared_key"]); psk != "" {
		peer["pre_shared_key"] = psk
	}
	return []interface{}{peer}
}

func normalizePSKJSON(v interface{}) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || strings.EqualFold(s, "<nil>") {
		return ""
	}
	return s
}

func stripWireGuardTagsFromGroupOutbounds(outbounds []map[string]interface{}, removed map[string]struct{}) {
	if len(removed) == 0 {
		return
	}
	for i := range outbounds {
		typ, _ := outbounds[i]["type"].(string)
		switch typ {
		case "selector", "urltest", "balancer":
			arr := filterRemovedStringTags(toStringSlice(outbounds[i]["outbounds"]), removed)
			outbounds[i]["outbounds"] = stringsToIfaceSlice(arr)
			if typ == "selector" {
				def := strings.TrimSpace(fmt.Sprint(outbounds[i]["default"]))
				if def != "" {
					if _, gone := removed[def]; gone && len(arr) > 0 {
						outbounds[i]["default"] = arr[0]
					}
				}
			}
		}
	}
}

func filterRemovedStringTags(tags []string, removed map[string]struct{}) []string {
	if len(tags) == 0 || len(removed) == 0 {
		return tags
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if _, drop := removed[t]; drop {
			continue
		}
		out = append(out, t)
	}
	return out
}

func stringsToIfaceSlice(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i := range s {
		out[i] = s[i]
	}
	return out
}
