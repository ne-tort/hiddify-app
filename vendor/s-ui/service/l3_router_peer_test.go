package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupL3RouterPeerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Client{}, &model.L3RouterPeer{}, &model.UserGroup{}, &model.ClientGroupMember{}, &model.GroupGroupMember{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func clientWithL3(id uint, name string, peerID uint64) model.Client {
	cfg := map[string]map[string]interface{}{
		"l3router": {
			"peer_id": float64(peerID),
			"user":    name,
		},
	}
	raw, _ := json.Marshal(cfg)
	return model.Client{Id: id, Name: name, Enable: true, Config: raw, Inbounds: json.RawMessage(`[]`)}
}

func TestL3RouterSyncPreservesIPWhenSecondPeerAdded(t *testing.T) {
	db := setupL3RouterPeerTestDB(t)
	c1 := clientWithL3(1, "a", 101)
	c2 := clientWithL3(2, "b", 102)
	if err := db.Create(&c1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&c2).Error; err != nil {
		t.Fatal(err)
	}
	opt := map[string]interface{}{
		"member_group_ids":  []uint{},
		"member_client_ids":   []uint{1},
		"private_subnet":      "10.220.0.0/24",
		"packet_filter":       false,
		"overlay_destination": l3RouterDefaultOverlayDst,
	}
	ob, _ := json.MarshalIndent(opt, "", "  ")
	ep := model.Endpoint{Type: l3RouterType, Tag: "l3-test", Options: ob}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}

	es := EndpointService{}
	tx := db.Begin()
	changed, err := es.syncSingleL3RouterEndpoint(tx, &ep)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if !changed {
		t.Log("first sync: expected changes (defaults/peers)")
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}

	db.First(&ep, ep.Id)
	var row1 model.L3RouterPeer
	if err := db.Where("endpoint_id = ? AND client_id = ?", ep.Id, 1).First(&row1).Error; err != nil {
		t.Fatal(err)
	}
	ip1 := l3PeerDecodeAllowedCIDRs(row1.AllowedCIDRs)
	if len(ip1) != 1 {
		t.Fatalf("expected one CIDR, got %v", ip1)
	}
	firstCIDR := ip1[0]

	// Add second client to endpoint membership
	var opt2 map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt2); err != nil {
		t.Fatal(err)
	}
	opt2["member_client_ids"] = []uint{1, 2}
	ob2, _ := json.MarshalIndent(opt2, "", "  ")
	ep.Options = ob2
	if err := db.Model(&model.Endpoint{}).Where("id = ?", ep.Id).Update("options", ob2).Error; err != nil {
		t.Fatal(err)
	}
	db.First(&ep, ep.Id)

	tx2 := db.Begin()
	if _, err := es.syncSingleL3RouterEndpoint(tx2, &ep); err != nil {
		tx2.Rollback()
		t.Fatal(err)
	}
	if err := tx2.Commit().Error; err != nil {
		t.Fatal(err)
	}

	var row1b model.L3RouterPeer
	if err := db.Where("endpoint_id = ? AND client_id = ?", ep.Id, 1).First(&row1b).Error; err != nil {
		t.Fatal(err)
	}
	ip1b := l3PeerDecodeAllowedCIDRs(row1b.AllowedCIDRs)
	if len(ip1b) != 1 || ip1b[0] != firstCIDR {
		t.Fatalf("client 1 CIDR changed: was %s now %v", firstCIDR, ip1b)
	}
}

func TestL3RouterPeerUpdateIPConflict(t *testing.T) {
	db := setupL3RouterPeerTestDB(t)
	c1 := clientWithL3(1, "a", 201)
	c2 := clientWithL3(2, "b", 202)
	_ = db.Create(&c1)
	_ = db.Create(&c2)
	opt := map[string]interface{}{
		"member_client_ids":   []uint{1, 2},
		"member_group_ids":    []uint{},
		"private_subnet":      "10.221.0.0/24",
		"packet_filter":       false,
		"overlay_destination": l3RouterDefaultOverlayDst,
	}
	ob, _ := json.MarshalIndent(opt, "", "  ")
	ep := model.Endpoint{Type: l3RouterType, Tag: "l3-x", Options: ob}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	es := EndpointService{}
	tx := db.Begin()
	if _, err := es.syncSingleL3RouterEndpoint(tx, &ep); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	_ = tx.Commit()

	var r1, r2 model.L3RouterPeer
	db.Where("endpoint_id = ? AND client_id = ?", ep.Id, 1).First(&r1)
	db.Where("endpoint_id = ? AND client_id = ?", ep.Id, 2).First(&r2)
	ip2 := l3PeerDecodeAllowedCIDRs(r2.AllowedCIDRs)
	if len(ip2) != 1 {
		t.Fatal("expected peer 2 cidr")
	}

	tx3 := db.Begin()
	payload, _ := json.Marshal(map[string]interface{}{
		"endpoint_id": ep.Id,
		"client_id":   1,
		"allowed_ips": []string{ip2[0]},
	})
	err := es.SaveL3RouterPeer(tx3, payload)
	if err == nil {
		tx3.Rollback()
		t.Fatal("expected conflict error")
	}
	_ = tx3.Rollback()
}
