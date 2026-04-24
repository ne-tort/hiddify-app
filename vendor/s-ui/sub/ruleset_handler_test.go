package sub

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/service"
	"github.com/gin-gonic/gin"
)

func TestRuleSetFileHandler_OKAndNotFound(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("sui-ruleset-%d.db", time.Now().UnixNano()))
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	if err := db.Create(&model.GeoDataset{Kind: "geoip", ActiveRevision: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	tag := model.GeoTag{DatasetKind: "geoip", TagNorm: "ru", TagRaw: "ru", Origin: "local"}
	if err := db.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GeoTagItem{
		GeoTagId:  tag.Id,
		ItemType:  "cidr",
		ValueNorm: "5.8.0.0/13",
		ValueRaw:  "5.8.0.0/13",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&service.RuleSetService{}).BuildRuleSetSRS(db, "geoip", "ru"); err != nil {
		t.Fatalf("pre-check ruleset build failed: %v", err)
	}

	gin.SetMode(gin.TestMode)
	h := &SubHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{
		{Key: "kind", Value: "geoip"},
		{Key: "tag", Value: "ru.srs"},
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/ruleset/geoip/ru.srs", nil)
	h.ruleSetFile(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(w.Body.Bytes()) == 0 {
		t.Fatal("empty .srs response")
	}
	if got := w.Header().Get("ETag"); got == "" {
		t.Fatal("missing etag")
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Params = gin.Params{
		{Key: "kind", Value: "geoip"},
		{Key: "tag", Value: "ru.srs"},
	}
	c2.Request = httptest.NewRequest(http.MethodGet, "/ruleset/geoip/ru.srs", nil)
	c2.Request.Header.Set("If-None-Match", w.Header().Get("ETag"))
	h.ruleSetFile(c2)
	if w2.Code != http.StatusOK && w2.Code != http.StatusNotModified {
		t.Fatalf("expected 200/304, got %d", w2.Code)
	}
}
