package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestParseWGExitSpecFromEndpoint_NoExitPeer(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"address": []interface{}{"10.5.0.1/24"},
		"peers": []map[string]interface{}{
			{"allowed_ips": []string{"10.5.0.2/32"}},
		},
	})
	ep := &model.Endpoint{Id: 3, Type: "awg", Options: opt}
	_, ok := parseWGExitSpecFromEndpoint(ep)
	if ok {
		t.Fatal("expected no exit spec")
	}
}

func TestParseWGExitSpecFromEndpoint_WithExitPeer(t *testing.T) {
	opt, _ := json.Marshal(map[string]interface{}{
		"name":    "awg",
		"address": []interface{}{"10.5.0.1/24"},
		"peers": []map[string]interface{}{
			{"allowed_ips": []string{"10.5.0.2/32", "0.0.0.0/0"}, "peer_exit": true},
		},
	})
	ep := &model.Endpoint{Id: 3, Type: "awg", Options: opt}
	spec, ok := parseWGExitSpecFromEndpoint(ep)
	if !ok {
		t.Fatal("expected exit spec")
	}
	if spec.IIF != "awg*" {
		t.Fatalf("expected awg* mask, got %q", spec.IIF)
	}
	if spec.SourceCIDR != "10.5.0.0/24" {
		t.Fatalf("unexpected source cidr: %q", spec.SourceCIDR)
	}
	if spec.Table != wgExitTableBase+3 {
		t.Fatalf("unexpected table: %d", spec.Table)
	}
	if spec.Mark != uint32(wgExitMarkBase+3) {
		t.Fatalf("unexpected mark: %#x", spec.Mark)
	}
}

func TestLoadWireGuardExitSpecs_Sorted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	awgOpt, _ := json.Marshal(map[string]interface{}{
		"address": []interface{}{"10.5.0.1/24"},
		"peers":   []map[string]interface{}{{"peer_exit": true}},
	})
	wgOpt, _ := json.Marshal(map[string]interface{}{
		"address": []interface{}{"10.8.0.1/24"},
		"peers":   []map[string]interface{}{{"peer_exit": true}},
	})
	if err := db.Create(&model.Endpoint{Id: 20, Type: wireGuardType, Tag: "wg-exit-20", Options: wgOpt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Endpoint{Id: 10, Type: awgType, Tag: "awg-exit-10", Options: awgOpt}).Error; err != nil {
		t.Fatal(err)
	}
	specs, err := loadWireGuardExitSpecs(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].EndpointID != 10 || specs[1].EndpointID != 20 {
		t.Fatalf("expected sorted ids [10,20], got [%d,%d]", specs[0].EndpointID, specs[1].EndpointID)
	}
}
