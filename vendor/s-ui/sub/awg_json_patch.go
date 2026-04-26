package sub

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/service"
	"gorm.io/gorm"
)

const awgEndpointType = "awg"

var awgInlineObfuscationKeys = []string{
	"jc", "jmin", "jmax", "s1", "s2", "s3", "s4",
	"h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5",
}

func parseMemberUintSlice(raw interface{}) []uint {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]uint, 0, len(v))
		for _, x := range v {
			if id := uintFromAny(x); id > 0 {
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

// clientHasWGStyleMemberAccess mirrors wireguard/awg endpoint membership: both lists empty means no clients.
func clientHasWGStyleMemberAccess(db *gorm.DB, opt map[string]interface{}, clientID uint) bool {
	if db == nil || opt == nil {
		return false
	}
	groups := parseMemberUintSlice(opt["member_group_ids"])
	clients := parseMemberUintSlice(opt["member_client_ids"])
	if len(groups) == 0 && len(clients) == 0 {
		return false
	}
	for _, cid := range clients {
		if cid == clientID {
			return true
		}
	}
	gs := service.GroupService{}
	for _, gid := range groups {
		ids, err := gs.ResolveMemberClientIDs(db, gid)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if id == clientID {
				return true
			}
		}
	}
	return false
}

func mergeAwgInlineObfuscation(dst map[string]interface{}, opt map[string]interface{}) {
	if dst == nil || opt == nil {
		return
	}
	intKeys := map[string]struct{}{
		"jc": {}, "jmin": {}, "jmax": {}, "s1": {}, "s2": {}, "s3": {}, "s4": {},
	}
	for _, k := range awgInlineObfuscationKeys {
		val, ok := opt[k]
		if !ok || val == nil {
			continue
		}
		if s, ok2 := val.(string); ok2 && strings.TrimSpace(s) == "" {
			continue
		}
		if _, isInt := intKeys[k]; isInt {
			if n, okn := jsonNumberToInt(val); okn {
				dst[k] = n
			}
			continue
		}
		dst[k] = val
	}
}

// jsonNumberToInt normalizes JSON-decoded numbers (float64 from encoding/json) for sing-box integer fields.
func jsonNumberToInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		x, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(x), true
	default:
		return 0, false
	}
}

func normalizeAwgObfuscationIntsInMap(m map[string]interface{}) {
	if m == nil {
		return
	}
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		n, okn := jsonNumberToInt(v)
		if !okn {
			delete(m, k)
			continue
		}
		m[k] = n
	}
}

func copyAwgEndpointScalarOptions(dst map[string]interface{}, opt map[string]interface{}) {
	if dst == nil || opt == nil {
		return
	}
	for _, k := range []string{"system", "name", "mtu", "workers", "udp_timeout", "preallocated_buffers_per_pool", "disable_pauses"} {
		if v, ok := opt[k]; ok && v != nil {
			dst[k] = v
		}
	}
}

func (j *JsonService) patchJsonForAwg(jsonConfig *map[string]interface{}, client *model.Client, requestHost string) error {
	return j.patchJsonForAwgDB(database.GetDB(), jsonConfig, client, requestHost)
}

// patchJsonForAwgDB injects sing-box "awg" endpoints for each DB awg endpoint the client belongs to (same keys as wireguard client identity).
func (j *JsonService) patchJsonForAwgDB(db *gorm.DB, jsonConfig *map[string]interface{}, client *model.Client, requestHost string) error {
	if db == nil || client == nil || jsonConfig == nil {
		return nil
	}
	serverHost := strings.TrimSpace(requestHost)
	if serverHost == "" {
		serverHost = strings.TrimSpace(j.resolveWGServerHost(""))
	}
	if serverHost == "" {
		return nil
	}
	clientPriv := clientWireGuardPrivateKey(client.Config)
	if strings.TrimSpace(clientPriv) == "" {
		return nil
	}
	clientPub := clientWireGuardPublicKey(client.Config)
	clientName := strings.TrimSpace(client.Name)

	var eps []model.Endpoint
	if err := db.Where("type = ?", awgEndpointType).Order("id ASC").Find(&eps).Error; err != nil {
		return err
	}

	for _, ep := range eps {
		var opt map[string]interface{}
		if len(ep.Options) > 0 {
			_ = json.Unmarshal(ep.Options, &opt)
		}
		if opt == nil {
			continue
		}
		if !clientHasWGStyleMemberAccess(db, opt, client.Id) {
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
		var match map[string]interface{}
		for _, p := range peers {
			if wireGuardPeerMatchesClient(p, client.Id, clientPub, clientName) {
				match = p
				break
			}
		}
		if match == nil {
			continue
		}
		localAddrs := toStringSlice(match["allowed_ips"])
		if len(localAddrs) == 0 {
			continue
		}
		peerTunnelRoutes := internetFullTunnelPeerRoutes()
		if !boolFromAnyDefaultTrueSub(opt["internet_allow"]) {
			peerTunnelRoutes = wgInferPeerTunnelRoutes(localAddrs, opt)
			if len(peerTunnelRoutes) == 0 {
				peerTunnelRoutes = append([]string(nil), localAddrs...)
			}
			peerTunnelRoutes = onlyIPv4PeerRoutes(peerTunnelRoutes)
			if len(peerTunnelRoutes) == 0 {
				v4Peers := cidrStringsIPv4Only(localAddrs)
				epV4 := endpointOptionsWithIPv4AddressesOnly(opt)
				peerTunnelRoutes = onlyIPv4PeerRoutes(wgInferPeerTunnelRoutes(v4Peers, epV4))
				if len(peerTunnelRoutes) == 0 {
					peerTunnelRoutes = append([]string(nil), cidrStringsIPv4Only(localAddrs)...)
				}
			}
		}
		peerAllowedCopy := append([]string(nil), peerTunnelRoutes...)

		awgPeer := map[string]interface{}{
			"address":     serverHost,
			"port":        listenPort,
			"public_key":  serverPub,
			"allowed_ips": stringSliceToIface(peerAllowedCopy),
		}
		if psk := strings.TrimSpace(fmt.Sprint(match["pre_shared_key"])); psk != "" && psk != "<nil>" {
			awgPeer["pre_shared_key"] = psk
		}
		if keepalive := intFromAny(opt["persistent_keepalive_interval"]); keepalive > 0 {
			awgPeer["persistent_keepalive_interval"] = keepalive
		}

		awgEndpoint := map[string]interface{}{
			"type":        awgEndpointType,
			"tag":         ep.Tag,
			"address":     listableFromStringSlice(localAddrs),
			"private_key": clientPriv,
			"peers":       []interface{}{awgPeer},
		}
		copyAwgEndpointScalarOptions(awgEndpoint, opt)

		if mtu := intFromAny(match["mtu"]); mtu > 0 {
			awgEndpoint["mtu"] = mtu
		}
		if w := intFromAny(match["workers"]); w > 0 {
			awgEndpoint["workers"] = w
		}

		// Obfuscation merge order (plan): linked profile (if id set and enabled) OR membership-resolved profile;
		// never fall back to Resolve when the endpoint explicitly references a disabled/missing profile id.
		profID := uintFromAny(opt["obfuscation_profile_id"])
		explicitProfile := profID > 0
		var prof *model.AwgObfuscationProfile
		if explicitProfile {
			if row, err := j.AwgObfuscationProfilesService.GetByID(db, profID); err == nil && row != nil && row.Enabled {
				prof = row
			}
		} else {
			prof, _ = j.AwgObfuscationProfilesService.ResolveObfuscationProfileForClient(db, client.Id)
		}
		if prof != nil {
			service.MergeAwgProfileIntoMap(awgEndpoint, prof)
		}
		mergeAwgInlineObfuscation(awgEndpoint, opt)
		normalizeAwgObfuscationIntsInMap(awgEndpoint)

		mergeWireGuardEndpoint(jsonConfig, ep.Tag, awgEndpoint)
		mergeWGTunAddresses(jsonConfig, localAddrs)
		insertWGRouteRules(jsonConfig, ep.Tag, routeIPCIDRsForWG(peerTunnelRoutes, localAddrs))
	}
	return nil
}
