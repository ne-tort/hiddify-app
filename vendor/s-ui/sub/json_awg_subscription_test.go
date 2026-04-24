package sub

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPatchJsonForAwgDB_injectsEndpointAndObfuscation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Endpoint{},
		&model.Client{},
		&model.AwgObfuscationProfile{},
	); err != nil {
		t.Fatal(err)
	}
	jcVal := 7
	prof := model.AwgObfuscationProfile{
		Name:    "p1",
		Enabled: true,
		Jc:      &jcVal,
	}
	if err := db.Create(&prof).Error; err != nil {
		t.Fatal(err)
	}

	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	opts := map[string]interface{}{
		"listen_port":              15555,
		"private_key":              serverPriv.String(),
		"address":                  []string{"10.20.0.1/24"},
		"member_client_ids":        []interface{}{float64(1)},
		"obfuscation_profile_id": float64(prof.Id),
		"jc":                       float64(99),
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(1),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.20.0.2/32"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-test", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	cl := model.Client{Id: 1, Name: "u1", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jc); err != nil {
		t.Fatal(err)
	}
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct", "awg-test"}},
		map[string]interface{}{"type": "urltest", "tag": "auto", "outbounds": []interface{}{"awg-test"}},
		map[string]interface{}{"type": "direct", "tag": "direct"},
	}

	j := &JsonService{}
	if err := j.patchJsonForAwgDB(db, &jc, &cl, "awg.example"); err != nil {
		t.Fatal(err)
	}

	eps, ok := jc["endpoints"].([]interface{})
	if !ok || len(eps) == 0 {
		t.Fatalf("expected endpoints, got %#v", jc["endpoints"])
	}
	ep0, ok := eps[len(eps)-1].(map[string]interface{})
	if !ok || ep0["type"] != "awg" || ep0["tag"] != "awg-test" {
		t.Fatalf("bad awg endpoint: %#v", ep0)
	}
	if intFromAny(ep0["jc"]) != 99 {
		t.Fatalf("inline jc should override profile: got %v", ep0["jc"])
	}
	peers, _ := ep0["peers"].([]interface{})
	if len(peers) == 0 {
		t.Fatal("no peers")
	}
	p0, _ := peers[0].(map[string]interface{})
	if p0["address"] != "awg.example" || intFromAny(p0["port"]) != 15555 {
		t.Fatalf("peer: %#v", p0)
	}
}

func TestPatchJsonForAwgDB_noMembersNoInjection(t *testing.T) {
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
		"listen_port": 11111,
		"private_key": serverPriv.String(),
		"member_client_ids": []interface{}{},
		"member_group_ids":  []interface{}{},
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(1),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.1.0.2/32"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-x", Options: optRaw}
	_ = db.Create(&ep).Error
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	cl := model.Client{Id: 1, Name: "u1", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}
	_ = db.Create(&cl).Error

	var jc map[string]interface{}
	_ = json.Unmarshal([]byte(defaultJson), &jc)
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct"}},
	}
	j := &JsonService{}
	if err := j.patchJsonForAwgDB(db, &jc, &cl, "h.example"); err != nil {
		t.Fatal(err)
	}
	if _, ok := jc["endpoints"]; ok {
		t.Fatal("expected no endpoints when member lists empty")
	}
}

// Disabled endpoint-linked profile must not fall back to membership-resolved profile (explicit id is intentional).
func TestPatchJsonForAwgDB_disabledLinkedProfileSkipsResolve(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Endpoint{},
		&model.Client{},
		&model.AwgObfuscationProfile{},
		&model.AwgObfuscationProfileClientMember{},
	); err != nil {
		t.Fatal(err)
	}
	jcGood := 100
	profGood := model.AwgObfuscationProfile{Name: "good", Enabled: true, Jc: &jcGood}
	if err := db.Create(&profGood).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AwgObfuscationProfileClientMember{ProfileId: profGood.Id, ClientId: 1}).Error; err != nil {
		t.Fatal(err)
	}
	jcBad := 50
	profBad := model.AwgObfuscationProfile{Name: "bad", Enabled: false, Jc: &jcBad}
	if err := db.Create(&profBad).Error; err != nil {
		t.Fatal(err)
	}
	// GORM Create skips false bool; match real DB row for disabled profile.
	if err := db.Model(&profBad).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	opts := map[string]interface{}{
		"listen_port":              16666,
		"private_key":              serverPriv.String(),
		"address":                  []string{"10.20.1.1/24"},
		"member_client_ids":        []interface{}{float64(1)},
		"obfuscation_profile_id":   float64(profBad.Id),
		"member_group_ids":         []interface{}{},
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(1),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.20.1.2/32"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-dis", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	cl := model.Client{Id: 1, Name: "u1", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	_ = json.Unmarshal([]byte(defaultJson), &jc)
	jc["outbounds"] = []interface{}{
		map[string]interface{}{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto", "direct", "awg-dis"}},
	}
	j := &JsonService{}
	if err := j.patchJsonForAwgDB(db, &jc, &cl, "x.example"); err != nil {
		t.Fatal(err)
	}
	eps, _ := jc["endpoints"].([]interface{})
	ep0, _ := eps[len(eps)-1].(map[string]interface{})
	if _, has := ep0["jc"]; has {
		t.Fatalf("jc must not come from resolved profile when explicit link is disabled; got %v", ep0["jc"])
	}
}
