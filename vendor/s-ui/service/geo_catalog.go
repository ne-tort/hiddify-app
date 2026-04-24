package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

const (
	geoKindIP   = "geoip"
	geoKindSite = "geosite"
	// Keep INSERT statements small enough for SQLite variable limits.
	geoItemsInsertBatchSize = 100
)

type GeoCatalogService struct{}

type geoTagSavePayload struct {
	Id          uint            `json:"id"`
	DatasetKind string          `json:"dataset_kind"`
	Tag         string          `json:"tag"`
	Metadata    json.RawMessage `json:"metadata"`
}

type geoTagItemSavePayload struct {
	Id         uint   `json:"id"`
	TagId      uint   `json:"tag_id"`
	ItemType   string `json:"item_type"`
	Value      string `json:"value"`
	Attributes string `json:"attributes"`
}

type geoSyncPayload struct {
	GeoIPURL   string `json:"geoip_url"`
	GeoSiteURL string `json:"geosite_url"`
}

type GeoTagView struct {
	model.GeoTag
	ItemCount int64 `json:"item_count"`
}

type GeoCatalogView struct {
	Datasets []model.GeoDataset         `json:"datasets"`
	Revs     []model.GeoCatalogRevision `json:"revisions"`
	Tags     []GeoTagView               `json:"tags"`
	Items    []model.GeoTagItem         `json:"items"`
}

func normalizeGeoKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case geoKindIP, geoKindSite:
		return kind
	default:
		return ""
	}
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func normalizeValue(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func (s *GeoCatalogService) GetAll(tx *gorm.DB, tagID uint) (GeoCatalogView, error) {
	var view GeoCatalogView
	if err := tx.Model(model.GeoDataset{}).Order("kind asc").Find(&view.Datasets).Error; err != nil {
		return view, err
	}
	if err := tx.Model(model.GeoCatalogRevision{}).Order("id desc").Limit(10).Find(&view.Revs).Error; err != nil {
		return view, err
	}

	var tags []model.GeoTag
	if err := tx.Model(model.GeoTag{}).Where("is_deleted = ?", false).Order("dataset_kind asc, tag_norm asc").Find(&tags).Error; err != nil {
		return view, err
	}
	countByTag := map[uint]int64{}
	type tagCountRow struct {
		GeoTagId uint  `gorm:"column:geo_tag_id"`
		Cnt      int64 `gorm:"column:cnt"`
	}
	var rows []tagCountRow
	if err := tx.Model(model.GeoTagItem{}).
		Select("geo_tag_id, COUNT(*) AS cnt").
		Where("is_deleted = ?", false).
		Group("geo_tag_id").
		Scan(&rows).Error; err != nil {
		return view, err
	}
	for _, r := range rows {
		countByTag[r.GeoTagId] = r.Cnt
	}

	view.Tags = make([]GeoTagView, 0, len(tags))
	for _, t := range tags {
		view.Tags = append(view.Tags, GeoTagView{GeoTag: t, ItemCount: countByTag[t.Id]})
	}
	// Heavy list is loaded only on-demand for selected tag.
	if tagID > 0 {
		if err := tx.Model(model.GeoTagItem{}).
			Where("geo_tag_id = ? AND is_deleted = ?", tagID, false).
			Order("id asc").
			Find(&view.Items).Error; err != nil {
			return view, err
		}
	} else {
		view.Items = []model.GeoTagItem{}
	}
	return view, nil
}

func (s *GeoCatalogService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	switch act {
	case "sync":
		payload := geoSyncPayload{
			GeoIPURL:   "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
			GeoSiteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
		}
		if len(data) > 0 {
			_ = json.Unmarshal(data, &payload)
		}
		return s.SyncFromURLs(tx, payload.GeoIPURL, payload.GeoSiteURL)
	case "new_tag":
		var p geoTagSavePayload
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		return s.newTag(tx, p)
	case "edit_tag":
		var p geoTagSavePayload
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		return s.editTag(tx, p)
	case "del_tag":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		return tx.Model(model.GeoTag{}).Where("id = ?", id).Update("is_deleted", true).Error
	case "upsert_item":
		var p geoTagItemSavePayload
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		return s.upsertItem(tx, p)
	case "del_item":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		return tx.Model(model.GeoTagItem{}).Where("id = ?", id).Update("is_deleted", true).Error
	case "rebuild":
		if err := s.rebuildRevision(tx, geoKindIP, "manual rebuild"); err != nil {
			return err
		}
		return s.rebuildRevision(tx, geoKindSite, "manual rebuild")
	default:
		return common.NewErrorf("unknown geo_catalog action: %s", act)
	}
}

func (s *GeoCatalogService) newTag(tx *gorm.DB, p geoTagSavePayload) error {
	kind := normalizeGeoKind(p.DatasetKind)
	if kind == "" {
		return common.NewErrorf("dataset_kind required: geoip|geosite")
	}
	tag := normalizeTag(p.Tag)
	if tag == "" {
		return common.NewErrorf("tag required")
	}
	var n int64
	if err := tx.Model(model.GeoTag{}).Where("dataset_kind = ? AND tag_norm = ? AND is_deleted = ?", kind, tag, false).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return common.NewErrorf("tag already exists")
	}
	return tx.Create(&model.GeoTag{
		DatasetKind:  kind,
		TagNorm:      tag,
		TagRaw:       strings.TrimSpace(p.Tag),
		Origin:       "local",
		MetadataJson: p.Metadata,
	}).Error
}

func (s *GeoCatalogService) editTag(tx *gorm.DB, p geoTagSavePayload) error {
	if p.Id == 0 {
		return common.NewErrorf("tag id required")
	}
	tag := normalizeTag(p.Tag)
	if tag == "" {
		return common.NewErrorf("tag required")
	}
	updates := map[string]interface{}{
		"tag_norm":      tag,
		"tag_raw":       strings.TrimSpace(p.Tag),
		"metadata_json": p.Metadata,
	}
	return tx.Model(model.GeoTag{}).Where("id = ?", p.Id).Updates(updates).Error
}

func (s *GeoCatalogService) upsertItem(tx *gorm.DB, p geoTagItemSavePayload) error {
	if p.TagId == 0 {
		return common.NewErrorf("tag_id required")
	}
	itemType := strings.TrimSpace(strings.ToLower(p.ItemType))
	if itemType == "" {
		return common.NewErrorf("item_type required")
	}
	valueRaw := strings.TrimSpace(p.Value)
	valueNorm := normalizeValue(valueRaw)
	if valueNorm == "" {
		return common.NewErrorf("value required")
	}
	if p.Id == 0 {
		return tx.Create(&model.GeoTagItem{
			GeoTagId:       p.TagId,
			ItemType:       itemType,
			ValueNorm:      valueNorm,
			ValueRaw:       valueRaw,
			AttributesJson: p.Attributes,
			Origin:         "local",
		}).Error
	}
	return tx.Model(model.GeoTagItem{}).Where("id = ?", p.Id).Updates(map[string]interface{}{
		"item_type":       itemType,
		"value_norm":      valueNorm,
		"value_raw":       valueRaw,
		"attributes_json": p.Attributes,
	}).Error
}

func downloadBytes(url string) ([]byte, string, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download failed (%d): %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	ver := strings.TrimSpace(resp.Header.Get("ETag"))
	if ver == "" {
		ver = strings.TrimSpace(resp.Header.Get("Last-Modified"))
	}
	if ver == "" {
		ver = time.Now().UTC().Format(time.RFC3339)
	}
	return body, ver, nil
}

func shaOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func codeOrCountry(code, country string) string {
	code = strings.TrimSpace(code)
	if code != "" {
		return normalizeTag(code)
	}
	return normalizeTag(country)
}

func cidrToString(c *routercommon.CIDR) string {
	if c == nil || len(c.Ip) == 0 {
		return ""
	}
	ip := net.IP(c.Ip)
	return fmt.Sprintf("%s/%d", ip.String(), c.Prefix)
}

func domainTypeToItemType(t routercommon.Domain_Type) string {
	switch t {
	case routercommon.Domain_Full:
		return "domain_full"
	case routercommon.Domain_Regex:
		return "domain_regex"
	case routercommon.Domain_RootDomain:
		return "domain_suffix"
	default:
		return "domain_keyword"
	}
}

func ensureDataset(tx *gorm.DB, kind string) (*model.GeoDataset, error) {
	var ds model.GeoDataset
	err := tx.Model(model.GeoDataset{}).Where("kind = ?", kind).First(&ds).Error
	if err == nil {
		return &ds, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	ds = model.GeoDataset{
		Kind:   kind,
		Status: "ready",
	}
	if err := tx.Create(&ds).Error; err != nil {
		return nil, err
	}
	return &ds, nil
}

func (s *GeoCatalogService) upsertUpstreamTag(tx *gorm.DB, kind string, tag string, meta json.RawMessage) (*model.GeoTag, error) {
	var row model.GeoTag
	err := tx.Model(model.GeoTag{}).Where("dataset_kind = ? AND tag_norm = ?", kind, tag).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		row = model.GeoTag{
			DatasetKind:  kind,
			TagNorm:      tag,
			TagRaw:       tag,
			Origin:       "upstream",
			MetadataJson: meta,
		}
		if err := tx.Create(&row).Error; err != nil {
			return nil, err
		}
		return &row, nil
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"tag_raw":       tag,
		"metadata_json": meta,
		"is_deleted":    false,
	}
	if row.Origin == "" {
		updates["origin"] = "upstream"
	}
	if err := tx.Model(model.GeoTag{}).Where("id = ?", row.Id).Updates(updates).Error; err != nil {
		return nil, err
	}
	row.TagRaw = tag
	row.MetadataJson = meta
	row.IsDeleted = false
	return &row, nil
}

func (s *GeoCatalogService) replaceUpstreamItems(tx *gorm.DB, tagID uint, items []model.GeoTagItem) error {
	if err := tx.Where("geo_tag_id = ? AND origin = ?", tagID, "upstream").Delete(model.GeoTagItem{}).Error; err != nil {
		return err
	}
	for i := range items {
		items[i].GeoTagId = tagID
		items[i].Origin = "upstream"
		items[i].IsDeleted = false
	}
	if len(items) == 0 {
		return nil
	}
	return tx.CreateInBatches(items, geoItemsInsertBatchSize).Error
}

func (s *GeoCatalogService) importGeoIP(tx *gorm.DB, raw []byte) (int, error) {
	var list routercommon.GeoIPList
	if err := proto.Unmarshal(raw, &list); err != nil {
		return 0, err
	}
	imported := 0
	for _, entry := range list.Entry {
		tag := codeOrCountry(entry.Code, entry.CountryCode)
		if tag == "" {
			continue
		}
		meta, _ := json.Marshal(map[string]interface{}{
			"cidr_count": len(entry.Cidr),
		})
		row, err := s.upsertUpstreamTag(tx, geoKindIP, tag, meta)
		if err != nil {
			return imported, err
		}
		items := make([]model.GeoTagItem, 0, len(entry.Cidr))
		for _, cidr := range entry.Cidr {
			val := strings.TrimSpace(cidrToString(cidr))
			if val == "" {
				continue
			}
			items = append(items, model.GeoTagItem{
				ItemType:  "cidr",
				ValueNorm: normalizeValue(val),
				ValueRaw:  val,
			})
		}
		if err := s.replaceUpstreamItems(tx, row.Id, items); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

func (s *GeoCatalogService) importGeoSite(tx *gorm.DB, raw []byte) (int, error) {
	var list routercommon.GeoSiteList
	if err := proto.Unmarshal(raw, &list); err != nil {
		return 0, err
	}
	imported := 0
	for _, entry := range list.Entry {
		tag := codeOrCountry(entry.Code, entry.CountryCode)
		if tag == "" {
			continue
		}
		meta, _ := json.Marshal(map[string]interface{}{
			"domain_count": len(entry.Domain),
		})
		row, err := s.upsertUpstreamTag(tx, geoKindSite, tag, meta)
		if err != nil {
			return imported, err
		}
		items := make([]model.GeoTagItem, 0, len(entry.Domain))
		for _, d := range entry.Domain {
			if d == nil {
				continue
			}
			value := strings.TrimSpace(d.Value)
			if value == "" {
				continue
			}
			attrs := ""
			if len(d.Attribute) > 0 {
				if b, err := json.Marshal(d.Attribute); err == nil {
					attrs = string(b)
				}
			}
			items = append(items, model.GeoTagItem{
				ItemType:       domainTypeToItemType(d.Type),
				ValueNorm:      normalizeValue(value),
				ValueRaw:       value,
				AttributesJson: attrs,
			})
		}
		if err := s.replaceUpstreamItems(tx, row.Id, items); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

func (s *GeoCatalogService) rebuildRevision(tx *gorm.DB, kind string, notes string) error {
	var ds model.GeoDataset
	if err := tx.Model(model.GeoDataset{}).Where("kind = ?", kind).First(&ds).Error; err != nil {
		return err
	}
	var maxNo int
	_ = tx.Model(model.GeoCatalogRevision{}).Where("dataset_kind = ?", kind).Select("COALESCE(MAX(revision_no), 0)").Scan(&maxNo).Error
	var itemCount int64
	if err := tx.Model(model.GeoTagItem{}).
		Joins("JOIN geo_tags ON geo_tags.id = geo_tag_items.geo_tag_id").
		Where("geo_tags.dataset_kind = ? AND geo_tags.is_deleted = ? AND geo_tag_items.is_deleted = ?", kind, false, false).
		Count(&itemCount).Error; err != nil {
		return err
	}
	rev := model.GeoCatalogRevision{
		DatasetKind:  kind,
		RevisionNo:   maxNo + 1,
		BuiltFromVer: ds.SourceVersion,
		BuiltFromSha: ds.SourceSha256,
		BuiltAtUnix:  time.Now().Unix(),
		Notes:        notes,
	}
	if err := tx.Create(&rev).Error; err != nil {
		return err
	}
	return tx.Model(model.GeoDataset{}).Where("id = ?", ds.Id).Updates(map[string]interface{}{
		"active_revision": rev.Id,
		"item_count":      int(itemCount),
		"status":          "ready",
		"last_error":      "",
	}).Error
}

func (s *GeoCatalogService) SyncFromURLs(tx *gorm.DB, geoIPURL string, geoSiteURL string) error {
	dsIP, err := ensureDataset(tx, geoKindIP)
	if err != nil {
		return err
	}
	dsSite, err := ensureDataset(tx, geoKindSite)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if err := tx.Model(model.GeoDataset{}).Where("id IN ?", []uint{dsIP.Id, dsSite.Id}).Updates(map[string]interface{}{"status": "updating", "last_sync_unix": now}).Error; err != nil {
		return err
	}

	rawIP, verIP, err := downloadBytes(geoIPURL)
	if err != nil {
		_ = tx.Model(model.GeoDataset{}).Where("id = ?", dsIP.Id).Updates(map[string]interface{}{"status": "error", "last_error": err.Error()}).Error
		return err
	}
	rawSite, verSite, err := downloadBytes(geoSiteURL)
	if err != nil {
		_ = tx.Model(model.GeoDataset{}).Where("id = ?", dsSite.Id).Updates(map[string]interface{}{"status": "error", "last_error": err.Error()}).Error
		return err
	}

	shaIP := shaOf(rawIP)
	shaSite := shaOf(rawSite)
	if _, err := s.importGeoIP(tx, rawIP); err != nil {
		_ = tx.Model(model.GeoDataset{}).Where("id = ?", dsIP.Id).Updates(map[string]interface{}{"status": "error", "last_error": err.Error()}).Error
		return err
	}
	if _, err := s.importGeoSite(tx, rawSite); err != nil {
		_ = tx.Model(model.GeoDataset{}).Where("id = ?", dsSite.Id).Updates(map[string]interface{}{"status": "error", "last_error": err.Error()}).Error
		return err
	}

	if err := tx.Model(model.GeoDataset{}).Where("id = ?", dsIP.Id).Updates(map[string]interface{}{
		"source_url":     strings.TrimSpace(geoIPURL),
		"source_version": verIP,
		"source_sha256":  shaIP,
		"last_sync_unix": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.Model(model.GeoDataset{}).Where("id = ?", dsSite.Id).Updates(map[string]interface{}{
		"source_url":     strings.TrimSpace(geoSiteURL),
		"source_version": verSite,
		"source_sha256":  shaSite,
		"last_sync_unix": now,
	}).Error; err != nil {
		return err
	}
	if err := s.rebuildRevision(tx, geoKindIP, "sync from upstream"); err != nil {
		return err
	}
	return s.rebuildRevision(tx, geoKindSite, "sync from upstream")
}
