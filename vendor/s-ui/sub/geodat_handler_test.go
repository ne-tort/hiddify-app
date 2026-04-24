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
	"github.com/gin-gonic/gin"
)

func TestGeoDatFileHandler_OKAndNotFound(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("sui-geodat-%d.db", time.Now().UnixNano()))
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	if err := db.Create(&model.GeoDataset{Kind: "geoip", ActiveRevision: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	tag := model.GeoTag{DatasetKind: "geoip", TagNorm: "ru", TagRaw: "RU", Origin: "local"}
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

	gin.SetMode(gin.TestMode)
	h := &SubHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "kind", Value: "geoip.dat"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/geodat/geoip.dat", nil)
	h.geoDatFile(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("missing etag")
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Params = gin.Params{{Key: "kind", Value: "missing.dat"}}
	c2.Request = httptest.NewRequest(http.MethodGet, "/geodat/missing.dat", nil)
	h.geoDatFile(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w2.Code)
	}
}
