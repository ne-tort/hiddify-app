package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMasqueRecalcServerAuthLeafPins_setsPinsFromClientSuiMasque(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}, &model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	const pin = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg := map[string]interface{}{
		"sui_masque": map[string]interface{}{
			"client_leaf_spki_sha256": pin,
		},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Id: 1, Name: "c", Enable: true, Config: raw, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen_port":       float64(443),
		"member_client_ids": []interface{}{float64(1)},
	}
	optRaw, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	ep := model.Endpoint{Type: masqueType, Tag: "m1", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	if err := MasqueRecalcServerAuthLeafPins(db); err != nil {
		t.Fatal(err)
	}
	var got model.Endpoint
	if err := db.Where("id = ?", ep.Id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(got.Options, &opt); err != nil {
		t.Fatal(err)
	}
	sa, ok := opt["server_auth"].(map[string]interface{})
	if !ok || sa == nil {
		t.Fatalf("expected server_auth, got %#v", opt["server_auth"])
	}
	arr, ok := sa["client_leaf_spki_sha256"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("expected one pin, got %#v", sa["client_leaf_spki_sha256"])
	}
	if s, _ := arr[0].(string); s != pin {
		t.Fatalf("pin: got %q want %q", s, pin)
	}
}

func TestMasqueRecalcServerAuth_syncsBearerAndBasic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}, &model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token":          "bear-one",
			"client_basic_username": "bu",
			"client_basic_password": "bp",
		},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Id: 1, Name: "c", Enable: true, Config: raw, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen_port":       float64(443),
		"member_client_ids": []interface{}{float64(1)},
		"sui_auth_modes":    []interface{}{"bearer", "basic"},
	}
	optRaw, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	ep := model.Endpoint{Type: masqueType, Tag: "m2", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	if err := MasqueRecalcServerAuthLeafPins(db); err != nil {
		t.Fatal(err)
	}
	var got model.Endpoint
	if err := db.Where("id = ?", ep.Id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(got.Options, &opt); err != nil {
		t.Fatal(err)
	}
	sa, ok := opt["server_auth"].(map[string]interface{})
	if !ok || sa == nil {
		t.Fatalf("expected server_auth")
	}
	bt, ok := sa["bearer_tokens"].([]interface{})
	if !ok || len(bt) != 1 || bt[0] != "bear-one" {
		t.Fatalf("bearer_tokens: %#v", sa["bearer_tokens"])
	}
	bc, ok := sa["basic_credentials"].([]interface{})
	if !ok || len(bc) != 1 {
		t.Fatalf("basic_credentials: %#v", sa["basic_credentials"])
	}
	b0, _ := bc[0].(map[string]interface{})
	if b0["username"] != "bu" || b0["password"] != "bp" {
		t.Fatalf("basic pair: %#v", b0)
	}
}
