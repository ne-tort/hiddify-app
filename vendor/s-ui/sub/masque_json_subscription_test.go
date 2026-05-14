package sub

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPatchMasqueSubscription_serverToClientStripsServerAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}

	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"transport_mode":    "connect_udp",
		"tls_server_name":   "masque.example",
		"member_client_ids": []interface{}{float64(1)},
		"member_group_ids":  []interface{}{},
		"server_auth":       map[string]interface{}{"policy": "first_match", "bearer_tokens": []interface{}{"secret"}},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-srv", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token": "client-bearer",
		},
	}
	cfgRaw, _ := json.Marshal(cfg)
	cl := model.Client{Id: 1, Name: "test", Enable: true, Config: cfgRaw, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jc); err != nil {
		t.Fatal(err)
	}
	jc["outbounds"] = []interface{}{}

	if err := patchJsonForMasqueSubscriptionDB(db, nil, &jc, &cl, "sub.example.com"); err != nil {
		t.Fatal(err)
	}
	eps, ok := jc["endpoints"].([]interface{})
	if !ok || len(eps) == 0 {
		t.Fatalf("expected endpoints, got %#v", jc["endpoints"])
	}
	m, _ := eps[0].(map[string]interface{})
	if m["type"] != "masque" {
		t.Fatalf("type: %v", m["type"])
	}
	if m["server"] != "sub.example.com" {
		t.Fatalf("server: %v", m["server"])
	}
	if intFromAny(m["server_port"]) != 443 {
		t.Fatalf("server_port: %v", m["server_port"])
	}
	if _, ok := m["server_auth"]; ok {
		t.Fatalf("server_auth must not be in subscription output: %#v", m["server_auth"])
	}
	if m["server_token"] != "client-bearer" {
		t.Fatalf("expected client overlay server_token, got %v", m["server_token"])
	}
	if m["listen"] != nil {
		t.Fatalf("listen should be stripped for client: %v", m["listen"])
	}
}

func TestPatchMasqueSubscription_noMembersNoEndpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"member_client_ids": []interface{}{},
		"member_group_ids":  []interface{}{},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-empty", Options: optRaw}
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
	if err := patchJsonForMasqueSubscriptionDB(db, nil, &jc, &cl, "sub.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := jc["endpoints"]; ok {
		t.Fatalf("expected no endpoints when membership empty")
	}
}
