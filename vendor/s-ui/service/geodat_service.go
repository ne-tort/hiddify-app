package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alireza0/s-ui/database/model"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

type GeoDatService struct {
	cache sync.Map
}

type GeoDatBuildResult struct {
	Bytes        []byte
	Kind         string
	Revision     uint
	SettingsHash string
	ETag         string
}

type geoDatCacheEntry struct {
	Revision     uint
	SettingsHash string
	Result       GeoDatBuildResult
}

var ErrGeoDatNotFound = errors.New("geodat not found")
var ErrGeoDatInvalidKind = errors.New("invalid geodat kind")

func normalizeGeoDatKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case geoKindIP, geoKindSite:
		return kind
	default:
		return ""
	}
}

func geoDatCacheKey(kind string) string {
	return "geodat:" + kind
}

func isDBLockedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "database is locked")
}

func withReadRetry[T any](fn func() (T, error)) (T, error) {
	var zero T
	var out T
	var err error
	for i := 0; i < 3; i++ {
		out, err = fn()
		if err == nil {
			return out, nil
		}
		if !isDBLockedError(err) || i == 2 {
			return zero, err
		}
		time.Sleep(time.Duration(120*(i+1)) * time.Millisecond)
	}
	return zero, err
}

func buildGeoDatETag(raw []byte) string {
	sum := sha256.Sum256(raw)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func datasetSettingsHash(ds model.GeoDataset) string {
	key := fmt.Sprintf("%s|%s|%s|%s|%d", ds.SourceUrl, ds.SourceVersion, ds.SourceSha256, ds.Status, ds.ActiveRevision)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *GeoDatService) BuildGeoDat(tx *gorm.DB, kind string) (GeoDatBuildResult, error) {
	var out GeoDatBuildResult
	kind = normalizeGeoDatKind(kind)
	if kind == "" {
		return out, ErrGeoDatInvalidKind
	}

	ds, err := withReadRetry(func() (model.GeoDataset, error) {
		var dataset model.GeoDataset
		err := tx.Model(model.GeoDataset{}).Where("kind = ?", kind).First(&dataset).Error
		return dataset, err
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, ErrGeoDatNotFound
		}
		return out, err
	}
	settingsHash := datasetSettingsHash(ds)

	cacheKey := geoDatCacheKey(kind)
	if cached, ok := s.cache.Load(cacheKey); ok {
		entry := cached.(geoDatCacheEntry)
		if entry.Revision == ds.ActiveRevision && entry.SettingsHash == settingsHash {
			return entry.Result, nil
		}
	}

	tags, err := withReadRetry(func() ([]model.GeoTag, error) {
		var rows []model.GeoTag
		err := tx.Model(model.GeoTag{}).
			Where("dataset_kind = ? AND is_deleted = ?", kind, false).
			Order("tag_norm ASC").
			Find(&rows).Error
		return rows, err
	})
	if err != nil {
		return out, err
	}
	if len(tags) == 0 {
		return out, ErrGeoDatNotFound
	}

	tagIDs := make([]uint, 0, len(tags))
	tagByID := make(map[uint]model.GeoTag, len(tags))
	for _, t := range tags {
		tagIDs = append(tagIDs, t.Id)
		tagByID[t.Id] = t
	}
	items, err := withReadRetry(func() ([]model.GeoTagItem, error) {
		var rows []model.GeoTagItem
		err := tx.Model(model.GeoTagItem{}).
			Where("geo_tag_id IN ? AND is_deleted = ?", tagIDs, false).
			Order("geo_tag_id ASC, id ASC").
			Find(&rows).Error
		return rows, err
	})
	if err != nil {
		return out, err
	}

	raw, err := buildGeoDatBytes(kind, tagByID, items)
	if err != nil {
		return out, err
	}

	out = GeoDatBuildResult{
		Bytes:        raw,
		Kind:         kind,
		Revision:     ds.ActiveRevision,
		SettingsHash: settingsHash,
		ETag:         buildGeoDatETag(raw),
	}
	s.cache.Store(cacheKey, geoDatCacheEntry{
		Revision:     out.Revision,
		SettingsHash: out.SettingsHash,
		Result:       out,
	})
	return out, nil
}

func buildGeoDatBytes(kind string, tagByID map[uint]model.GeoTag, items []model.GeoTagItem) ([]byte, error) {
	switch kind {
	case geoKindIP:
		byCode := map[string][]*routercommon.CIDR{}
		for _, item := range items {
			code := tagByID[item.GeoTagId].TagNorm
			if code == "" {
				continue
			}
			val := strings.TrimSpace(item.ValueRaw)
			if val == "" {
				val = strings.TrimSpace(item.ValueNorm)
			}
			if strings.ToLower(strings.TrimSpace(item.ItemType)) != "cidr" || val == "" {
				continue
			}
			c, err := parseCIDR(val)
			if err != nil {
				continue
			}
			byCode[code] = append(byCode[code], c)
		}
		list := &routercommon.GeoIPList{
			Entry: make([]*routercommon.GeoIP, 0, len(byCode)),
		}
		for _, tag := range sortedTagValues(tagByID) {
			cidrs := byCode[tag]
			if len(cidrs) == 0 {
				continue
			}
			list.Entry = append(list.Entry, &routercommon.GeoIP{
				CountryCode: strings.ToUpper(tag),
				Cidr:        cidrs,
			})
		}
		if len(list.Entry) == 0 {
			return nil, ErrGeoDatNotFound
		}
		return proto.Marshal(list)
	case geoKindSite:
		byCode := map[string][]*routercommon.Domain{}
		for _, item := range items {
			code := tagByID[item.GeoTagId].TagNorm
			if code == "" {
				continue
			}
			value := strings.TrimSpace(item.ValueRaw)
			if value == "" {
				value = strings.TrimSpace(item.ValueNorm)
			}
			d, ok := toGeoSiteDomain(item.ItemType, value, item.AttributesJson)
			if !ok {
				continue
			}
			byCode[code] = append(byCode[code], d)
		}
		list := &routercommon.GeoSiteList{
			Entry: make([]*routercommon.GeoSite, 0, len(byCode)),
		}
		for _, tag := range sortedTagValues(tagByID) {
			domains := byCode[tag]
			if len(domains) == 0 {
				continue
			}
			list.Entry = append(list.Entry, &routercommon.GeoSite{
				CountryCode: strings.ToUpper(tag),
				Domain:      domains,
			})
		}
		if len(list.Entry) == 0 {
			return nil, ErrGeoDatNotFound
		}
		return proto.Marshal(list)
	default:
		return nil, fmt.Errorf("unsupported geodat kind")
	}
}

func sortedTagValues(tagByID map[uint]model.GeoTag) []string {
	values := make([]string, 0, len(tagByID))
	seen := map[string]struct{}{}
	for _, t := range tagByID {
		v := strings.TrimSpace(strings.ToLower(t.TagNorm))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		values = append(values, v)
	}
	// Keep deterministic order.
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
	return values
}

func toGeoSiteDomain(itemType string, value string, attributesJSON string) (*routercommon.Domain, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	d := &routercommon.Domain{Value: value}
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "domain_full":
		d.Type = routercommon.Domain_Full
	case "domain_suffix":
		d.Type = routercommon.Domain_RootDomain
	case "domain_keyword":
		d.Type = routercommon.Domain_Plain
	case "domain_regex":
		d.Type = routercommon.Domain_Regex
	default:
		return nil, false
	}
	if strings.TrimSpace(attributesJSON) != "" {
		var attrs []*routercommon.Domain_Attribute
		_ = json.Unmarshal([]byte(attributesJSON), &attrs)
		d.Attribute = attrs
	}
	return d, true
}

func parseCIDR(raw string) (*routercommon.CIDR, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty cidr")
	}
	if strings.Contains(raw, "/") {
		ip, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		ones, _ := network.Mask.Size()
		ipBytes := ip.To16()
		if ip4 := ip.To4(); ip4 != nil {
			ipBytes = ip4
		}
		return &routercommon.CIDR{
			Ip:     ipBytes,
			Prefix: uint32(ones),
		}, nil
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	if ip4 := ip.To4(); ip4 != nil {
		return &routercommon.CIDR{Ip: ip4, Prefix: 32}, nil
	}
	return &routercommon.CIDR{Ip: ip.To16(), Prefix: 128}, nil
}
