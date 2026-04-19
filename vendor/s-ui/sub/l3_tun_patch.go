package sub

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

const (
	l3TunInboundTag      = "tun-in"
	l3OverlayOutboundTag = "select" // client wires this to their outbound tag (e.g. selector named "select")
	l3DefaultOverlayDst  = "198.18.0.1:33333"
)

// firstL3RouterPeer loads the L3RouterPeer row with minimum endpoint_id for this client.
func firstL3RouterPeer(db *gorm.DB, clientID uint) (*model.L3RouterPeer, *model.Endpoint, error) {
	var peers []model.L3RouterPeer
	db.Where("client_id = ?", clientID).Order("endpoint_id ASC").Limit(1).Find(&peers)
	if len(peers) == 0 {
		return nil, nil, nil
	}
	p := peers[0]
	var ep model.Endpoint
	if err := db.Where("id = ? AND type = ?", p.EndpointId, l3RouterEndpointType).First(&ep).Error; err != nil {
		return nil, nil, err
	}
	return &p, &ep, nil
}

const l3RouterEndpointType = "l3router"

func patchJsonInboundsForL3Router(jsonConfig *map[string]interface{}, client *model.Client) error {
	return patchJsonInboundsForL3RouterWithDB(database.GetDB(), jsonConfig, client)
}

func patchJsonInboundsForL3RouterWithDB(db *gorm.DB, jsonConfig *map[string]interface{}, client *model.Client) error {
	if db == nil {
		return nil
	}
	peer, ep, err := firstL3RouterPeer(db, client.Id)
	if err != nil || peer == nil || ep == nil {
		return err
	}

	var opt map[string]interface{}
	if len(ep.Options) > 0 {
		_ = json.Unmarshal(ep.Options, &opt)
	}
	if opt == nil {
		opt = map[string]interface{}{}
	}
	privateSubnet := strings.TrimSpace(fmt.Sprint(opt["private_subnet"]))
	if privateSubnet == "" {
		return nil
	}
	if _, err := netip.ParsePrefix(privateSubnet); err != nil {
		return nil
	}

	overlayDest := strings.TrimSpace(fmt.Sprint(opt["overlay_destination"]))
	if overlayDest == "" {
		overlayDest = l3DefaultOverlayDst
	}

	addrs := allowedCIDRsForPeer(peer, client)
	if len(addrs) == 0 {
		return nil
	}

	tunInbound := map[string]interface{}{
		"type":                     "tun",
		"tag":                      l3TunInboundTag,
		"address":                  stringSliceToIface(addrs),
		"mtu":                      1500,
		"auto_route":               true,
		"strict_route":             false,
		"stack":                    "system",
		"l3_overlay_outbound":      l3OverlayOutboundTag,
		"l3_overlay_destination":   overlayDest,
		"l3_overlay_route_address": []interface{}{privateSubnet},
	}

	raw, ok := (*jsonConfig)["inbounds"]
	if !ok {
		(*jsonConfig)["inbounds"] = []interface{}{tunInbound}
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		(*jsonConfig)["inbounds"] = []interface{}{tunInbound}
		return nil
	}
	replaced := false
	for i, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if typ, _ := m["type"].(string); strings.EqualFold(typ, "tun") {
			list[i] = tunInbound
			replaced = true
			break
		}
	}
	if !replaced {
		out := make([]interface{}, 0, len(list)+1)
		out = append(out, tunInbound)
		out = append(out, list...)
		(*jsonConfig)["inbounds"] = out
	} else {
		(*jsonConfig)["inbounds"] = list
	}
	return nil
}

func allowedCIDRsForPeer(peer *model.L3RouterPeer, client *model.Client) []string {
	var raw []string
	if len(peer.AllowedCIDRs) > 0 {
		_ = json.Unmarshal(peer.AllowedCIDRs, &raw)
	}
	var out []string
	seen := map[string]struct{}{}
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := netip.ParsePrefix(c); err != nil {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) > 0 {
		return out
	}
	pid, ok := l3PeerIDFromClientConfig(client.Config)
	if !ok {
		return nil
	}
	return []string{defaultL3RouterCIDRForSub(pid)}
}

// Same formula as service.defaultL3RouterCIDR (peer identity CIDR on 10.250.x.y/32).
func defaultL3RouterCIDRForSub(peerID uint64) string {
	octet3 := (peerID >> 8) & 255
	octet4 := peerID & 255
	return fmt.Sprintf("%d.%d.%d.%d/32", 10, 250, octet3, octet4)
}

func l3PeerIDFromClientConfig(config json.RawMessage) (uint64, bool) {
	var configs map[string]map[string]interface{}
	if len(config) == 0 {
		return 0, false
	}
	if err := json.Unmarshal(config, &configs); err != nil {
		return 0, false
	}
	l3, ok := configs["l3router"]
	if !ok || l3 == nil {
		return 0, false
	}
	return parsePeerIDLoose(l3["peer_id"])
}

func parsePeerIDLoose(raw interface{}) (uint64, bool) {
	switch v := raw.(type) {
	case float64:
		if v <= 0 || math.Trunc(v) != v {
			return 0, false
		}
		return uint64(v), true
	case int:
		if v > 0 {
			return uint64(v), true
		}
	case int64:
		if v > 0 {
			return uint64(v), true
		}
	case uint64:
		if v > 0 {
			return v, true
		}
	case string:
		if strings.TrimSpace(v) == "" {
			return 0, false
		}
		var n uint64
		_, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		if err != nil || n == 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func stringSliceToIface(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i := range s {
		out[i] = s[i]
	}
	return out
}
