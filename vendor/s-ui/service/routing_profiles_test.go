package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alireza0/s-ui/database/model"
)

func TestRoutingProfilesService_SaveAndValidate(t *testing.T) {
	db := openGeoTestDB(t)
	rpSvc := &RoutingProfilesService{}
	geoSvc := &GeoCatalogService{}

	tagPayload, _ := json.Marshal(map[string]interface{}{
		"dataset_kind": "geosite",
		"tag":          "alibaba",
	})
	if err := geoSvc.Save(db, "new_tag", tagPayload); err != nil {
		t.Fatalf("new geosite tag: %v", err)
	}

	createPayload, _ := json.Marshal(map[string]interface{}{
		"name":         "Default",
		"enabled":      true,
		"route_order":  []string{"block", "direct", "proxy"},
		"direct_sites": []string{"geosite:ALIBABA"},
		"proxy_sites":  []string{"geosite:unknown-tag"},
	})
	if err := rpSvc.Save(db, "new", createPayload); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	var row model.RoutingProfile
	if err := db.Model(model.RoutingProfile{}).Where("name = ?", "Default").First(&row).Error; err != nil {
		t.Fatalf("find profile: %v", err)
	}

	result, err := rpSvc.ValidateProfile(db, row.Id)
	if err != nil {
		t.Fatalf("validate profile: %v", err)
	}
	if result.Ok {
		t.Fatalf("expected warnings for unknown token")
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "unknown-tag") {
		t.Fatalf("expected warning about unknown token, got %v", result.Warnings)
	}

	link, err := rpSvc.BuildHappRoutingLink(row)
	if err != nil {
		t.Fatalf("build happ link: %v", err)
	}
	if !strings.HasPrefix(link, "happ://routing/onadd/") {
		t.Fatalf("unexpected happ link: %s", link)
	}

	rules := rpSvc.BuildSingboxManagedRules(row)
	if len(rules) == 0 {
		t.Fatalf("expected generated sing-box rules")
	}
}

func TestRoutingProfilesService_ResolveAndMergeWithMemberships(t *testing.T) {
	db := openGeoTestDB(t)
	rpSvc := &RoutingProfilesService{}

	c1 := model.Client{Id: 1, Name: "u1", Enable: true}
	if err := db.Create(&c1).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	g1 := model.UserGroup{Id: 10, Name: "g1"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: c1.Id, GroupId: g1.Id}).Error; err != nil {
		t.Fatalf("bind client-group: %v", err)
	}

	p1Raw, _ := json.Marshal(map[string]interface{}{
		"name":         "p1",
		"enabled":      true,
		"direct_sites": []string{"geosite:a"},
		"proxy_sites":  []string{"geosite:b"},
		"block_sites":  []string{"geosite:c"},
		"client_ids":   []uint{c1.Id},
	})
	if err := rpSvc.Save(db, "new", p1Raw); err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2Raw, _ := json.Marshal(map[string]interface{}{
		"name":         "p2",
		"enabled":      true,
		"direct_sites": []string{"geosite:b"},
		"proxy_sites":  []string{"geosite:c", "geosite:d"},
		"group_ids":    []uint{g1.Id},
	})
	if err := rpSvc.Save(db, "new", p2Raw); err != nil {
		t.Fatalf("create p2: %v", err)
	}

	rows, err := rpSvc.ResolveProfilesForClient(db, c1.Id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(rows))
	}
	rules := rpSvc.BuildMergedSingboxManagedRules(rows)
	if len(rules) == 0 {
		t.Fatalf("expected merged rules")
	}
	joined, _ := json.Marshal(rules)
	s := string(joined)
	if strings.Contains(s, "geosite-c\"],\"outbound\":\"select") {
		t.Fatalf("blocked token leaked into proxy rules: %s", s)
	}
	if !strings.Contains(s, "geosite-d") {
		t.Fatalf("expected proxy token geosite:d in merged output: %s", s)
	}
}

func TestRoutingProfilesService_BuildMergedHappPayloadWithGeoBase(t *testing.T) {
	db := openGeoTestDB(t)
	rpSvc := &RoutingProfilesService{}
	raw, _ := json.Marshal(map[string]interface{}{
		"name":         "p-happ",
		"enabled":      true,
		"direct_sites": []string{"geosite:category-ru"},
		"direct_ip":    []string{"geoip:ru"},
	})
	if err := rpSvc.Save(db, "new", raw); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	var row model.RoutingProfile
	if err := db.Model(model.RoutingProfile{}).Where("name = ?", "p-happ").First(&row).Error; err != nil {
		t.Fatalf("find profile: %v", err)
	}
	payload, err := rpSvc.BuildMergedHappPayloadWithGeoBase([]model.RoutingProfile{row}, "https://example.com/sub")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if out["Geoipurl"] != "https://example.com/sub/geodat/geoip.dat" {
		t.Fatalf("unexpected Geoipurl: %v", out["Geoipurl"])
	}
	if out["Geositeurl"] != "https://example.com/sub/geodat/geosite.dat" {
		t.Fatalf("unexpected Geositeurl: %v", out["Geositeurl"])
	}
}
