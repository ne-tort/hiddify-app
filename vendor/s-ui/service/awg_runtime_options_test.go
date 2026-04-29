package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMergeAwgRuntimeObfuscationOptions_InlineOverrideWinsAndDropsIFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Endpoint{}, &model.AwgObfuscationProfile{}); err != nil {
		t.Fatal(err)
	}

	jc, jmin, jmax := 5, 300, 600
	s1, s2, s3, s4 := 11, 22, 33, 44
	h1, h2, h3, h4 := "100-200", "300-400", "500-600", "700-800"
	prof := model.AwgObfuscationProfile{
		Name:    "p1",
		Enabled: true,
		Jc:      &jc,
		Jmin:    &jmin,
		Jmax:    &jmax,
		S1:      &s1,
		S2:      &s2,
		S3:      &s3,
		S4:      &s4,
		H1:      &h1,
		H2:      &h2,
		H3:      &h3,
		H4:      &h4,
	}
	if err := db.Create(&prof).Error; err != nil {
		t.Fatal(err)
	}

	opt := map[string]interface{}{
		"obfuscation_profile_id": float64(prof.Id),
		"i1":                     "<payload>",
		"i2":                     "<payload2>",
		"jc":                     float64(1), // should override linked profile
	}
	raw, _ := json.Marshal(opt)
	ep := model.Endpoint{Type: awgType, Tag: "awg-x", Options: raw}
	svc := &EndpointService{}
	if err := svc.mergeAwgRuntimeObfuscationOptions(db, &ep); err != nil {
		t.Fatal(err)
	}
	var merged map[string]interface{}
	if err := json.Unmarshal(ep.Options, &merged); err != nil {
		t.Fatal(err)
	}
	if _, ok := merged["i1"]; ok {
		t.Fatalf("i1 must be removed from runtime options: %#v", merged)
	}
	if _, ok := merged["i2"]; ok {
		t.Fatalf("i2 must be removed from runtime options: %#v", merged)
	}
	if intFromAny(merged["jc"]) != 1 {
		t.Fatalf("inline jc should override profile jc: %#v", merged)
	}
	if intFromAny(merged["jmin"]) != jmin || intFromAny(merged["jmax"]) != jmax {
		t.Fatalf("profile ints were not merged: %#v", merged)
	}
	if intFromAny(merged["s1"]) != s1 || intFromAny(merged["s2"]) != s2 || intFromAny(merged["s3"]) != s3 || intFromAny(merged["s4"]) != s4 {
		t.Fatalf("profile padding ints were not merged: %#v", merged)
	}
	if merged["h1"] != h1 || merged["h2"] != h2 || merged["h3"] != h3 || merged["h4"] != h4 {
		t.Fatalf("profile h-fields were not merged: %#v", merged)
	}
}
