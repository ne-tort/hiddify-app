package sub

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDefaultL3RouterCIDRForSub(t *testing.T) {
	t.Parallel()
	// Matches service.defaultL3RouterCIDR: 10.250.(peerID>>8 & 255).(peerID & 255)/32
	pid := uint64(0x0102)
	got := defaultL3RouterCIDRForSub(pid)
	want := "10.250.1.2/32"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPatchJsonInboundsForL3RouterWithDB_firstEndpointWins(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.L3RouterPeer{}); err != nil {
		t.Fatal(err)
	}

	opt1 := map[string]interface{}{
		"private_subnet":      "10.10.0.0/24",
		"overlay_destination": "198.18.0.1:33333",
	}
	ob1, _ := json.Marshal(opt1)
	ep1 := model.Endpoint{Type: "l3router", Tag: "l3-a", Options: ob1}
	if err := db.Create(&ep1).Error; err != nil {
		t.Fatal(err)
	}

	opt2 := map[string]interface{}{
		"private_subnet":      "10.20.0.0/24",
		"overlay_destination": "198.18.0.1:33333",
	}
	ob2, _ := json.Marshal(opt2)
	ep2 := model.Endpoint{Type: "l3router", Tag: "l3-b", Options: ob2}
	if err := db.Create(&ep2).Error; err != nil {
		t.Fatal(err)
	}

	cfg := map[string]map[string]interface{}{
		"l3router": {"peer_id": float64(0x0102), "user": "u1"},
	}
	cfgRaw, _ := json.Marshal(cfg)
	cl := model.Client{Id: 1, Name: "u1", Enable: true, Config: cfgRaw, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	allowed1, _ := json.Marshal([]string{"10.10.0.99/32"})
	allowed2, _ := json.Marshal([]string{"10.20.0.99/32"})
	// Higher endpoint_id first in DB insert order: still ORDER BY endpoint_id ASC picks ep1
	if err := db.Create(&model.L3RouterPeer{EndpointId: ep2.Id, ClientId: 1, AllowedCIDRs: allowed2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.L3RouterPeer{EndpointId: ep1.Id, ClientId: 1, AllowedCIDRs: allowed1}).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jc); err != nil {
		t.Fatal(err)
	}
	if err := patchJsonInboundsForL3RouterWithDB(db, &jc, &cl); err != nil {
		t.Fatal(err)
	}
	raw, ok := jc["inbounds"].([]interface{})
	if !ok || len(raw) < 1 {
		t.Fatalf("inbounds: %v", jc["inbounds"])
	}
	tun, ok := raw[0].(map[string]interface{})
	if !ok {
		t.Fatal("first inbound not map")
	}
	route, ok := tun["l3_overlay_route_address"].([]interface{})
	if !ok || len(route) != 1 {
		t.Fatalf("l3_overlay_route_address: %v", tun["l3_overlay_route_address"])
	}
	if route[0] != "10.10.0.0/24" {
		t.Fatalf("expected subnet from first endpoint_id, got %v", route[0])
	}
	addr, ok := tun["address"].([]interface{})
	if !ok || len(addr) != 1 || addr[0] != "10.10.0.99/32" {
		t.Fatalf("address: %v", tun["address"])
	}
}

func TestPatchJsonInboundsForL3RouterWithDB_noPeerLeavesDefaultTun(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.L3RouterPeer{}); err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Id: 1, Name: "solo", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}

	var jc map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jc); err != nil {
		t.Fatal(err)
	}
	if err := patchJsonInboundsForL3RouterWithDB(db, &jc, &cl); err != nil {
		t.Fatal(err)
	}
	raw, ok := jc["inbounds"].([]interface{})
	if !ok {
		t.Fatal("no inbounds")
	}
	tun, ok := raw[0].(map[string]interface{})
	if !ok {
		t.Fatal("first inbound not map")
	}
	if tun["l3_overlay_outbound"] != nil {
		t.Fatalf("expected no L3 patch, got %v", tun["l3_overlay_outbound"])
	}
}
