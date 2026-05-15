package sub

import (
	"encoding/json"
	"fmt"
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
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}

	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}

	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"transport_mode":    "connect_udp",
		"sui_tls_id":        float64(1),
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
	if m["server"] != "masque.example" {
		t.Fatalf("server: %v (expected TLS profile server_name)", m["server"])
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
	if _, ok := m["tls"]; ok {
		t.Fatalf("server tls must not appear in subscription: %#v", m["tls"])
	}
	ot, ok := m["outbound_tls"].(map[string]interface{})
	if !ok || ot["server_name"] != "masque.example" {
		t.Fatalf("expected outbound_tls from TLS profile client JSON, got %#v", m["outbound_tls"])
	}
	if _, ok := m["sui_tls_id"]; ok {
		t.Fatalf("sui_tls_id must not leak to subscription")
	}
	if _, ok := m["sui_auth_modes"]; ok {
		t.Fatalf("sui_auth_modes must not leak to subscription")
	}
	if _, ok := m["tls_server_name"]; ok {
		t.Fatalf("tls_server_name must not leak to subscription")
	}
	if _, ok := m["insecure"]; ok {
		t.Fatalf("insecure must not leak to subscription")
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

func TestPatchMasqueSubscription_clientBasicCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"transport_mode":    "connect_udp",
		"sui_tls_id":        float64(1),
		"member_client_ids": []interface{}{float64(1)},
		"member_group_ids":  []interface{}{},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-basic", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token":            "tok",
			"client_basic_username":   "u1",
			"client_basic_password":   "p1",
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
		t.Fatalf("expected endpoints")
	}
	m, _ := eps[0].(map[string]interface{})
	if m["type"] != "masque" {
		t.Fatalf("type: %v", m["type"])
	}
	if m["client_basic_username"] != "u1" || m["client_basic_password"] != "p1" {
		t.Fatalf("basic creds: %#v", m)
	}
}

func TestPatchMasqueSubscription_outboundTLSDeepMerge(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"from-profile","nested":{"from_prof":1,"both":1}}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"transport_mode":    "connect_udp",
		"sui_tls_id":        float64(1),
		"member_client_ids": []interface{}{float64(1)},
		"member_group_ids":  []interface{}{},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-deep", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token": "tok",
			"outbound_tls": map[string]interface{}{
				"server_name": "cli-win",
				"nested": map[string]interface{}{
					"both":     2,
					"from_cli": 3,
				},
			},
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
		t.Fatalf("expected endpoints")
	}
	m, _ := eps[0].(map[string]interface{})
	ot, ok := m["outbound_tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("outbound_tls: %#v", m["outbound_tls"])
	}
	if ot["server_name"] != "cli-win" {
		t.Fatalf("server_name: %v", ot["server_name"])
	}
	nested, ok := ot["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested: %#v", ot["nested"])
	}
	if nested["from_prof"].(float64) != 1 {
		t.Fatalf("from_prof: %v", nested["from_prof"])
	}
	if nested["both"].(float64) != 2 {
		t.Fatalf("both (client wins): %v", nested["both"])
	}
	if nested["from_cli"].(float64) != 3 {
		t.Fatalf("from_cli: %v", nested["from_cli"])
	}
}

func TestPatchMasqueSubscription_authModesBearerOnlyStripsBasic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":                  "server",
		"listen":                "0.0.0.0",
		"listen_port":           float64(443),
		"transport_mode":        "connect_udp",
		"sui_tls_id":            float64(1),
		"member_client_ids":     []interface{}{float64(1)},
		"member_group_ids":      []interface{}{},
		"sui_auth_modes":        []interface{}{"bearer", "basic"},
		"sui_sub": map[string]interface{}{
			"sui_client_auth_modes": []interface{}{"bearer"},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-modes", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token":          "tok-bearer",
			"client_basic_username": "u1",
			"client_basic_password": "p1",
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
		t.Fatalf("expected endpoints")
	}
	m, _ := eps[0].(map[string]interface{})
	if m["server_token"] != "tok-bearer" {
		t.Fatalf("server_token: %v", m["server_token"])
	}
	if _, ok := m["client_basic_username"]; ok {
		t.Fatalf("basic username must be stripped when only bearer mode: %#v", m)
	}
	if _, ok := m["sui_auth_modes"]; ok {
		t.Fatalf("sui_auth_modes must not appear in subscription")
	}
}

func TestPatchMasqueSubscription_serverUsesTlsProfileServerName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"sui_tls_id":        float64(1),
		"member_client_ids": []interface{}{float64(1)},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-sn", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token": "t",
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
	if err := patchJsonForMasqueSubscriptionDB(db, nil, &jc, &cl, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	eps, ok := jc["endpoints"].([]interface{})
	if !ok || len(eps) == 0 {
		t.Fatalf("expected endpoints")
	}
	m, _ := eps[0].(map[string]interface{})
	if m["server"] != "masque.example" {
		t.Fatalf("server should use TLS profile server_name, got %v", m["server"])
	}
}

func TestPatchMasqueSubscription_serverAuthModesStripBasicWhenServerOmitsBasic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"sui_tls_id":        float64(1),
		"member_client_ids": []interface{}{float64(1)},
		"member_group_ids":  []interface{}{},
		"sui_auth_modes":    []interface{}{"bearer"},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-srv-basic", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token":          "tok-bearer",
			"client_basic_username": "u1",
			"client_basic_password": "p1",
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
		t.Fatalf("expected endpoints")
	}
	m, _ := eps[0].(map[string]interface{})
	if _, ok := m["client_basic_username"]; ok {
		t.Fatalf("basic must be stripped when server sui_auth_modes has no basic: %#v", m)
	}
}

func TestPatchMasqueSubscription_addrsExpandTags(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	tlsRow := &model.Tls{
		Id:     1,
		Name:   "panel-tls",
		Server: json.RawMessage(`{"enabled":true,"certificate_path":"/x.pem","key_path":"/y.pem"}`),
		Client: json.RawMessage(`{"enabled":true,"server_name":"masque.example"}`),
	}
	if err := db.Create(tlsRow).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"mode":              "server",
		"listen":            "0.0.0.0",
		"listen_port":       float64(443),
		"sui_tls_id":        float64(1),
		"member_client_ids": []interface{}{float64(1)},
		"member_group_ids":  []interface{}{},
		"sui_sub": map[string]interface{}{
			"addrs": []interface{}{
				map[string]interface{}{"server": "alt1.example", "server_port": float64(8443)},
				map[string]interface{}{"server": "alt2.example", "server_port": float64(8444), "remark": "b"},
			},
		},
	}
	optRaw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: masqueEndpointType, Tag: "mq-md", Options: optRaw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"masque": map[string]interface{}{
			"server_token": "t",
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
	if !ok || len(eps) != 2 {
		t.Fatalf("expected 2 endpoints, got %#v", jc["endpoints"])
	}
	byTag := map[string]map[string]interface{}{}
	for _, e := range eps {
		m, ok := e.(map[string]interface{})
		if !ok {
			t.Fatal("endpoint item type")
		}
		byTag[fmt.Sprint(m["tag"])] = m
	}
	if m := byTag["1.mq-md"]; m == nil || m["server"] != "alt1.example" || intFromAny(m["server_port"]) != 8443 {
		t.Fatalf("ep1: %#v", byTag["1.mq-md"])
	}
	if m := byTag["2.mq-mdb"]; m == nil || m["server"] != "alt2.example" {
		t.Fatalf("ep2: %#v", byTag["2.mq-mdb"])
	}
}
