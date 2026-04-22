package sub

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPatchJsonForWireGuardWithDB_addsWGEndpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}

	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	opts := map[string]interface{}{
		"listen_port": 14290,
		"private_key": serverPriv.String(),
		"address":     []string{"10.8.0.1/24", "fe80::1/128"},
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(1),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.8.0.2/32"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "wireguard", Tag: "wg-1", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Id: 1, Name: "test", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jc); err != nil {
		t.Fatal(err)
	}
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct"}},
		map[string]interface{}{"type": "urltest", "tag": "auto", "outbounds": []interface{}{}},
		map[string]interface{}{"type": "direct", "tag": "direct"},
	}

	if err := patchJsonForWireGuardWithDB(db, &jc, &cl, "example.com"); err != nil {
		t.Fatal(err)
	}

	eps, ok := jc["endpoints"].([]interface{})
	if !ok || len(eps) == 0 {
		t.Fatal("expected endpoints")
	}
	ep0, ok := eps[0].(map[string]interface{})
	if !ok || ep0["type"] != "wireguard" || ep0["tag"] != "wg-client" {
		t.Fatalf("bad wg endpoint: %#v", ep0)
	}
	peers, _ := ep0["peers"].([]interface{})
	if len(peers) == 0 {
		t.Fatal("no peers on endpoint")
	}
	p0, _ := peers[0].(map[string]interface{})
	if p0["address"] != "example.com" {
		t.Fatalf("peer address: %v", p0["address"])
	}
	if intFromAny(p0["port"]) != 14290 {
		t.Fatalf("peer port: %v", p0["port"])
	}
	if p0["public_key"] == nil || p0["public_key"] == "" {
		t.Fatal("peer public_key empty")
	}
	inbounds, ok := jc["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds: %v", jc["inbounds"])
	}
	tun0, ok := inbounds[0].(map[string]interface{})
	if !ok || tun0["type"] != "tun" {
		t.Fatalf("expected first inbound=tun, got %v", inbounds[0])
	}
	tunAddrs := toStringSlice(tun0["address"])
	hasWGAddr := false
	for _, a := range tunAddrs {
		if a == "10.8.0.2/32" {
			hasWGAddr = true
			break
		}
	}
	if !hasWGAddr {
		t.Fatalf("tun.address must include wg /32, got %v", tunAddrs)
	}
	if got := toStringSlice(p0["allowed_ips"]); len(got) == 0 || got[0] != "10.8.0.0/24" {
		t.Fatalf("peer allowed_ips: %v", got)
	}

	route, ok := jc["route"].(map[string]interface{})
	if !ok {
		t.Fatal("expected route")
	}
	rules, _ := route["rules"].([]interface{})
	foundRule := false
	for _, r := range rules {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if m["outbound"] != "wg-client" {
			continue
		}
		foundRule = true
		cidrs := toStringSlice(m["ip_cidr"])
		if len(cidrs) == 0 {
			t.Fatal("wg route ip_cidr empty")
		}
		if cidrs[0] != "10.8.0.0/24" {
			t.Fatalf("unexpected wg route ip_cidr: %v", cidrs)
		}
	}
	if !foundRule {
		t.Fatal("no route rule for wg-client endpoint")
	}

	out := mapArrayFromConfig(jc["outbounds"])
	for _, ob := range out {
		if ob["tag"] == "wg-client" {
			t.Fatal("legacy wg outbound must not be present")
		}
	}
}

func TestPatchJsonForWireGuardWithDB_matchPeerByClientPublicKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}

	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	opts := map[string]interface{}{
		"listen_port": 14291,
		"private_key": serverPriv.String(),
		"peers": []map[string]interface{}{
			{
				"public_key":  clientPriv.PublicKey().String(),
				"private_key": clientPriv.String(),
				"allowed_ips": []string{"10.0.0.3/32"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "wireguard", Tag: "wg-pk", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	cl := model.Client{Id: 7, Name: "pkcli", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	_ = json.Unmarshal([]byte(defaultJson), &jc)
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct"}},
		map[string]interface{}{"type": "urltest", "tag": "auto", "outbounds": []interface{}{}},
		map[string]interface{}{"type": "direct", "tag": "direct"},
	}

	if err := patchJsonForWireGuardWithDB(db, &jc, &cl, "wg.example"); err != nil {
		t.Fatal(err)
	}
	eps, ok := jc["endpoints"].([]interface{})
	if !ok || len(eps) == 0 {
		t.Fatal("expected endpoints for public_key match")
	}
	ep0, _ := eps[0].(map[string]interface{})
	peers, _ := ep0["peers"].([]interface{})
	p0, _ := peers[0].(map[string]interface{})
	if p0["address"] != "wg.example" {
		t.Fatalf("peer address: %v", p0["address"])
	}
}

func TestPatchJsonForWireGuardWithDB_noPeerNoChanges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Id: 1, Name: "solo", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	var jc map[string]interface{}
	_ = json.Unmarshal([]byte(defaultJson), &jc)
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct"}},
		map[string]interface{}{"type": "urltest", "tag": "auto", "outbounds": []interface{}{}},
	}

	if err := patchJsonForWireGuardWithDB(db, &jc, &cl, "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := jc["endpoints"]; ok {
		t.Fatal("unexpected endpoints")
	}
}

func TestSelectWGCloakDetourTag_Disabled(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "vless", "tag": "vless-a"},
		{"type": "direct", "tag": "direct"},
	}
	if got := selectWGCloakDetourTag(map[string]interface{}{}, outbounds); got != "" {
		t.Fatalf("expected empty detour for disabled cloak, got %q", got)
	}
}

func TestSelectWGCloakDetourTag_ManualValid(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "selector", "tag": "select"},
		{"type": "vless", "tag": "vless-a"},
		{"type": "direct", "tag": "direct"},
	}
	got := selectWGCloakDetourTag(map[string]interface{}{
		"cloak_enabled":    true,
		"cloak_detour_tag": "select",
	}, outbounds)
	if got != "select" {
		t.Fatalf("expected manual detour tag, got %q", got)
	}
}

func TestSelectWGCloakDetourTag_ManualInvalidFallsBackByPriority(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "tuic", "tag": "tuic-a"},
		{"type": "trojan", "tag": "trojan-a"},
		{"type": "vless", "tag": "vless-a"},
		{"type": "direct", "tag": "direct"},
	}
	got := selectWGCloakDetourTag(map[string]interface{}{
		"cloak_enabled":    true,
		"cloak_detour_tag": "missing-tag",
	}, outbounds)
	if got != "vless-a" {
		t.Fatalf("expected vless priority fallback, got %q", got)
	}
}

func TestSelectWGCloakDetourTag_FallbackToDirect(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "selector", "tag": "proxy"},
		{"type": "urltest", "tag": "auto"},
	}
	got := selectWGCloakDetourTag(map[string]interface{}{
		"cloak_enabled": true,
	}, outbounds)
	if got != "direct" {
		t.Fatalf("expected direct fallback, got %q", got)
	}
}
