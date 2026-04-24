package service

import (
	"bytes"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"github.com/sagernet/sing-box/common/srs"
)

func TestRuleSetService_BuildRuleSetSRS_GeoIP(t *testing.T) {
	db := openGeoTestDB(t)
	rs := &RuleSetService{}

	if err := db.Create(&model.GeoDataset{Kind: "geoip", ActiveRevision: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	tag := model.GeoTag{DatasetKind: "geoip", TagNorm: "ru", TagRaw: "ru", Origin: "local"}
	if err := db.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.GeoTagItem{
		{GeoTagId: tag.Id, ItemType: "cidr", ValueNorm: "5.8.0.0/13", ValueRaw: "5.8.0.0/13"},
		{GeoTagId: tag.Id, ItemType: "cidr", ValueNorm: "31.13.64.0/18", ValueRaw: "31.13.64.0/18"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}

	built, err := rs.BuildRuleSetSRS(db, "geoip", "ru")
	if err != nil {
		t.Fatalf("build srs: %v", err)
	}
	if len(built.Bytes) == 0 {
		t.Fatal("empty srs bytes")
	}
	if built.ETag == "" {
		t.Fatal("empty etag")
	}

	decoded, err := srs.Read(bytes.NewReader(built.Bytes), true)
	if err != nil {
		t.Fatalf("decode srs: %v", err)
	}
	if len(decoded.Options.Rules) == 0 {
		t.Fatal("decoded rules are empty")
	}
}

func TestRuleSetService_BuildRuleSetSRS_NotFound(t *testing.T) {
	db := openGeoTestDB(t)
	rs := &RuleSetService{}
	if _, err := rs.BuildRuleSetSRS(db, "geoip", "missing"); err == nil {
		t.Fatal("expected not found error")
	}
}
