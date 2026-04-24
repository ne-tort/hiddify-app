package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openGeoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.GeoDataset{},
		&model.GeoCatalogRevision{},
		&model.GeoTag{},
		&model.GeoTagItem{},
		&model.RoutingProfile{},
		&model.RoutingProfileClientMember{},
		&model.RoutingProfileGroupMember{},
		&model.Client{},
		&model.UserGroup{},
		&model.ClientGroupMember{},
		&model.GroupGroupMember{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGeoCatalogService_NewTagAndItem(t *testing.T) {
	db := openGeoTestDB(t)
	svc := &GeoCatalogService{}

	tagPayload, _ := json.Marshal(map[string]interface{}{
		"dataset_kind": "geosite",
		"tag":          "ALIBABA",
	})
	if err := svc.Save(db, "new_tag", tagPayload); err != nil {
		t.Fatalf("new tag: %v", err)
	}

	var tag model.GeoTag
	if err := db.Model(model.GeoTag{}).Where("dataset_kind = ? AND tag_norm = ?", "geosite", "alibaba").First(&tag).Error; err != nil {
		t.Fatalf("tag not found: %v", err)
	}

	itemPayload, _ := json.Marshal(map[string]interface{}{
		"tag_id":     tag.Id,
		"item_type":  "domain_suffix",
		"value":      "Alibaba.com",
		"attributes": "{}",
	})
	if err := svc.Save(db, "upsert_item", itemPayload); err != nil {
		t.Fatalf("upsert item: %v", err)
	}

	var item model.GeoTagItem
	if err := db.Model(model.GeoTagItem{}).Where("geo_tag_id = ?", tag.Id).First(&item).Error; err != nil {
		t.Fatalf("item not found: %v", err)
	}
	if item.ValueNorm != "alibaba.com" {
		t.Fatalf("expected normalized value, got %q", item.ValueNorm)
	}
}
