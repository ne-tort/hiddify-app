package service

import (
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

func TestGeoDatService_BuildGeoDat_GeoIP(t *testing.T) {
	db := openGeoTestDB(t)
	svc := &GeoDatService{}
	if err := db.Create(&model.GeoDataset{Kind: "geoip", ActiveRevision: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	tag := model.GeoTag{DatasetKind: "geoip", TagNorm: "ru", TagRaw: "RU", Origin: "local"}
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
	res, err := svc.BuildGeoDat(db, "geoip")
	if err != nil {
		t.Fatalf("build geodat: %v", err)
	}
	if len(res.Bytes) == 0 || res.ETag == "" {
		t.Fatalf("invalid geodat result: %+v", res)
	}
	var list routercommon.GeoIPList
	if err := proto.Unmarshal(res.Bytes, &list); err != nil {
		t.Fatalf("decode geodat: %v", err)
	}
	if len(list.Entry) == 0 {
		t.Fatal("empty geoip list")
	}
}

func TestGeoDatService_BuildGeoDat_GeoSite(t *testing.T) {
	db := openGeoTestDB(t)
	svc := &GeoDatService{}
	if err := db.Create(&model.GeoDataset{Kind: "geosite", ActiveRevision: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	tag := model.GeoTag{DatasetKind: "geosite", TagNorm: "category-ru", TagRaw: "CATEGORY-RU", Origin: "local"}
	if err := db.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.GeoTagItem{
		{GeoTagId: tag.Id, ItemType: "domain_suffix", ValueNorm: "yandex.ru", ValueRaw: "yandex.ru"},
		{GeoTagId: tag.Id, ItemType: "domain_full", ValueNorm: "2ip.ru", ValueRaw: "2ip.ru"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	res, err := svc.BuildGeoDat(db, "geosite")
	if err != nil {
		t.Fatalf("build geodat: %v", err)
	}
	var list routercommon.GeoSiteList
	if err := proto.Unmarshal(res.Bytes, &list); err != nil {
		t.Fatalf("decode geodat: %v", err)
	}
	if len(list.Entry) == 0 {
		t.Fatal("empty geosite list")
	}
}
