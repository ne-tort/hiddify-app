package service

import (
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/gorm"
)

var awgInlineObfuscationKeys = []string{
	"jc", "jmin", "jmax", "s1", "s2", "s3", "s4",
	"h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5",
}

// ResolveEffectiveAwgObfuscation merges obfuscation values using one contract:
// linked profile (or membership-resolved profile when no explicit link), then endpoint inline override.
func ResolveEffectiveAwgObfuscation(tx *gorm.DB, endpointOptions map[string]interface{}, clientID uint) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	if endpointOptions == nil {
		return out, nil
	}
	profID := uintFromAny(endpointOptions["obfuscation_profile_id"])
	explicitProfile := profID > 0

	var prof *model.AwgObfuscationProfile
	profSvc := AwgObfuscationProfilesService{}
	if explicitProfile {
		if row, err := profSvc.GetByID(tx, profID); err == nil && row != nil && row.Enabled {
			prof = row
		}
	} else if clientID > 0 {
		row, err := profSvc.ResolveObfuscationProfileForClient(tx, clientID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no such table") {
				return out, nil
			}
			return nil, err
		}
		prof = row
	}
	if prof != nil {
		MergeAwgProfileIntoMap(out, prof)
	}
	MergeAwgInlineObfuscationOptions(out, endpointOptions)
	return out, nil
}

// MergeAwgInlineObfuscationOptions applies endpoint inline obfuscation values into dst.
func MergeAwgInlineObfuscationOptions(dst map[string]interface{}, endpointOptions map[string]interface{}) {
	if dst == nil || endpointOptions == nil {
		return
	}
	for _, k := range awgInlineObfuscationKeys {
		val, ok := endpointOptions[k]
		if !ok || val == nil {
			continue
		}
		if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		dst[k] = val
	}
}
