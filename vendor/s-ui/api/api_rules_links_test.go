package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRulesLinksTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Client{},
		&model.UserGroup{},
		&model.ClientGroupMember{},
		&model.GroupGroupMember{},
		&model.RoutingProfile{},
		&model.RoutingProfileClientMember{},
		&model.RoutingProfileGroupMember{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedClient(t *testing.T, db *gorm.DB, id uint, name string) {
	t.Helper()
	if err := db.Create(&model.Client{
		Id:       id,
		Name:     name,
		Enable:   true,
		Config:   json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func seedEnabledProfile(t *testing.T, db *gorm.DB, name string) uint {
	t.Helper()
	row := model.RoutingProfile{
		Name:       name,
		Enabled:    true,
		RouteOrder: json.RawMessage(`["direct"]`),
		DirectSites: json.RawMessage(`["geosite:google"]`),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.Id
}

func TestBuildClientRulesLinks_DirectMembership(t *testing.T) {
	db := setupRulesLinksTestDB(t)
	seedClient(t, db, 1, "u1")
	pid := seedEnabledProfile(t, db, "rp1")
	if err := db.Create(&model.RoutingProfileClientMember{ProfileId: pid, ClientId: 1}).Error; err != nil {
		t.Fatal(err)
	}

	svc := &ApiService{}
	links, err := svc.buildClientRulesLinks(db, 1, "https://example.com/sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 rules link, got %d", len(links))
	}
	if !strings.HasPrefix(links[0]["uri"].(string), "happ://routing/add/") {
		t.Fatalf("expected happ link, got %#v", links[0]["uri"])
	}
}

func TestBuildClientRulesLinks_NoProfiles(t *testing.T) {
	db := setupRulesLinksTestDB(t)
	seedClient(t, db, 2, "u2")

	svc := &ApiService{}
	links, err := svc.buildClientRulesLinks(db, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no links, got %d", len(links))
	}
}

func TestBuildClientRulesLinks_GroupMembership(t *testing.T) {
	db := setupRulesLinksTestDB(t)
	seedClient(t, db, 3, "u3")
	group := model.UserGroup{Name: "g1"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{GroupId: group.Id, ClientId: 3}).Error; err != nil {
		t.Fatal(err)
	}
	pid := seedEnabledProfile(t, db, "rp-group")
	if err := db.Create(&model.RoutingProfileGroupMember{ProfileId: pid, GroupId: group.Id}).Error; err != nil {
		t.Fatal(err)
	}

	svc := &ApiService{}
	links, err := svc.buildClientRulesLinks(db, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 group-derived rules link, got %d", len(links))
	}
}
