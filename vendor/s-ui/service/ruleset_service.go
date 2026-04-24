package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alireza0/s-ui/database/model"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/common/srs"
	"github.com/sagernet/sing-box/option"
	"gorm.io/gorm"
)

type RuleSetService struct {
	cache sync.Map
}

type RuleSetBuildResult struct {
	Bytes    []byte
	Kind     string
	Tag      string
	Revision uint
	ETag     string
}

type ruleSetCacheEntry struct {
	Revision uint
	Result   RuleSetBuildResult
}

var ErrRuleSetNotFound = errors.New("ruleset not found")

func normalizeRuleSetKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "geoip", "geosite":
		return kind
	default:
		return ""
	}
}

func normalizeRuleSetTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func rulesetCacheKey(kind string, tag string) string {
	return kind + ":" + tag
}

func buildRuleSetETag(raw []byte) string {
	sum := sha256.Sum256(raw)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func (s *RuleSetService) BuildRuleSetSRS(tx *gorm.DB, kind string, tag string) (RuleSetBuildResult, error) {
	var out RuleSetBuildResult
	kind = normalizeRuleSetKind(kind)
	if kind == "" {
		return out, fmt.Errorf("unsupported ruleset kind")
	}
	tag = normalizeRuleSetTag(tag)
	if tag == "" {
		return out, fmt.Errorf("empty ruleset tag")
	}

	var geoTag model.GeoTag
	if err := tx.Model(model.GeoTag{}).
		Where("dataset_kind = ? AND tag_norm = ? AND is_deleted = ?", kind, tag, false).
		First(&geoTag).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, ErrRuleSetNotFound
		}
		return out, err
	}

	var dataset model.GeoDataset
	if err := tx.Model(model.GeoDataset{}).Where("kind = ?", kind).First(&dataset).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return out, err
	}

	cacheKey := rulesetCacheKey(kind, tag)
	if cached, ok := s.cache.Load(cacheKey); ok {
		entry := cached.(ruleSetCacheEntry)
		if entry.Revision == dataset.ActiveRevision {
			return entry.Result, nil
		}
	}

	var items []model.GeoTagItem
	if err := tx.Model(model.GeoTagItem{}).
		Where("geo_tag_id = ? AND is_deleted = ?", geoTag.Id, false).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return out, err
	}

	defaultRule := option.DefaultHeadlessRule{}
	for _, it := range items {
		value := strings.TrimSpace(it.ValueRaw)
		if value == "" {
			value = strings.TrimSpace(it.ValueNorm)
		}
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(it.ItemType)) {
		case "cidr":
			defaultRule.IPCIDR = append(defaultRule.IPCIDR, value)
		case "domain_full":
			defaultRule.Domain = append(defaultRule.Domain, value)
		case "domain_suffix":
			defaultRule.DomainSuffix = append(defaultRule.DomainSuffix, value)
		case "domain_keyword":
			defaultRule.DomainKeyword = append(defaultRule.DomainKeyword, value)
		case "domain_regex":
			defaultRule.DomainRegex = append(defaultRule.DomainRegex, value)
		}
	}

	plain := option.PlainRuleSet{
		Rules: []option.HeadlessRule{
			{
				Type:           C.RuleTypeDefault,
				DefaultOptions: defaultRule,
			},
		},
	}

	var buf bytes.Buffer
	if err := srs.Write(&buf, plain, C.RuleSetVersionCurrent); err != nil {
		return out, err
	}
	raw := buf.Bytes()
	out = RuleSetBuildResult{
		Bytes:    raw,
		Kind:     kind,
		Tag:      tag,
		Revision: dataset.ActiveRevision,
		ETag:     buildRuleSetETag(raw),
	}
	s.cache.Store(cacheKey, ruleSetCacheEntry{
		Revision: dataset.ActiveRevision,
		Result:   out,
	})
	return out, nil
}
