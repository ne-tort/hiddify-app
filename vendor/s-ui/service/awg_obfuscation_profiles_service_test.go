package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAwgProfileUpdate_PersistsLastValidateError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.AwgObfuscationProfile{},
		&model.AwgObfuscationProfileClientMember{},
		&model.AwgObfuscationProfileGroupMember{},
	); err != nil {
		t.Fatal(err)
	}

	svc := &AwgObfuscationProfilesService{}
	jc, jmin, jmax := 5, 100, 220
	s1, s2 := 10, 11
	h1, h2, h3, h4 := "100-120", "200-220", "300-320", "400-420"
	payloadNew, _ := json.Marshal(map[string]interface{}{
		"name": "p1", "enabled": true,
		"jc": jc, "jmin": jmin, "jmax": jmax, "s1": s1, "s2": s2, "s4": 10,
		"h1": h1, "h2": h2, "h3": h3, "h4": h4,
	})
	if err := svc.Save(db, "new", payloadNew); err != nil {
		t.Fatal(err)
	}

	var row model.AwgObfuscationProfile
	if err := db.Where("name = ?", "p1").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	badS2 := 66 // S1+56
	payloadEdit, _ := json.Marshal(map[string]interface{}{
		"id": row.Id, "name": "p1", "enabled": true,
		"jc": jc, "jmin": jmin, "jmax": jmax, "s1": s1, "s2": badS2, "s4": 10,
		"h1": h1, "h2": h2, "h3": h3, "h4": h4,
	})
	if err := svc.Save(db, "edit", payloadEdit); err == nil {
		t.Fatal("expected validation error")
	}

	var rowAfter model.AwgObfuscationProfile
	if err := db.Where("id = ?", row.Id).First(&rowAfter).Error; err != nil {
		t.Fatal(err)
	}
	if rowAfter.LastValidateErr == "" {
		t.Fatal("expected last_validate_error to be persisted")
	}
}

