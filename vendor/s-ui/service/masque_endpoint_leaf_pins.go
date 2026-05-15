package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/gorm"
)

// MasqueRecalcServerAuthLeafPins rebuilds server_auth on every generic masque server endpoint:
// bearer_tokens and basic_credentials from member clients' config.masque, client_leaf_spki_sha256
// from sui_masque identity, filtered by endpoint sui_auth_modes (empty = all).
func MasqueRecalcServerAuthLeafPins(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	var eps []model.Endpoint
	if err := tx.Where("type = ?", masqueType).Find(&eps).Error; err != nil {
		return err
	}
	for i := range eps {
		if err := recalcMasqueEndpointServerAuth(tx, &eps[i]); err != nil {
			return err
		}
		if err := tx.Model(&model.Endpoint{}).Where("id = ?", eps[i].Id).Update("options", eps[i].Options).Error; err != nil {
			return err
		}
	}
	return nil
}

func recalcMasqueEndpointServerAuth(tx *gorm.DB, ep *model.Endpoint) error {
	if ep == nil || len(ep.Options) == 0 {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	if m, ok := opt["mode"].(string); ok && strings.EqualFold(strings.TrimSpace(m), "client") {
		// Legacy rows only: panel masque is server-side; subscription uses sui_sub, not mode=client.
		return nil
	}

	modes := masqueStringSliceFromAny(opt["sui_auth_modes"])
	allowAll := len(modes) == 0

	sa := map[string]interface{}{}

	if allowAll || masqueAuthModesHas(modes, "bearer") {
		toks := collectMasqueBearerTokens(tx, opt)
		if len(toks) > 0 {
			arr := make([]interface{}, len(toks))
			for i, t := range toks {
				arr[i] = t
			}
			sa["bearer_tokens"] = arr
		}
	}
	if allowAll || masqueAuthModesHas(modes, "basic") {
		basics := collectMasqueBasicCredentials(tx, opt)
		if len(basics) > 0 {
			arr := make([]interface{}, len(basics))
			for i, b := range basics {
				arr[i] = b
			}
			sa["basic_credentials"] = arr
		}
	}
	if allowAll || masqueAuthModesHas(modes, "mtls") {
		pins := collectMemberMasqueLeafPins(tx, opt)
		if len(pins) > 0 {
			arr := make([]interface{}, 0, len(pins))
			for _, p := range pins {
				arr = append(arr, p)
			}
			sa["client_leaf_spki_sha256"] = arr
		}
	}

	if len(sa) == 0 {
		delete(opt, "server_auth")
	} else {
		opt["server_auth"] = sa
	}

	stripMasqueServerAuthDefaultPolicyInOptions(opt)

	raw, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

func masqueStringSliceFromAny(v interface{}) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(x))
		for _, s := range x {
			out = append(out, strings.TrimSpace(s))
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, it := range x {
			out = append(out, strings.TrimSpace(fmt.Sprint(it)))
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func masqueAuthModesHas(modes []string, needle string) bool {
	n := strings.ToLower(strings.TrimSpace(needle))
	for _, m := range modes {
		if strings.EqualFold(strings.TrimSpace(m), n) {
			return true
		}
	}
	return false
}

func collectMasqueBearerTokens(tx *gorm.DB, opt map[string]interface{}) []string {
	seen := make(map[string]struct{})
	var out []string
	gs := GroupService{}

	for _, cid := range parseUintListFromAny(opt["member_client_ids"]) {
		if cid == 0 {
			continue
		}
		var cl model.Client
		if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
			continue
		}
		if t := masqueClientBearerToken(&cl); t != "" {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	for _, gid := range parseUintListFromAny(opt["member_group_ids"]) {
		if gid == 0 {
			continue
		}
		ids, err := gs.ResolveMemberClientIDs(tx, gid)
		if err != nil {
			continue
		}
		for _, cid := range ids {
			var cl model.Client
			if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
				continue
			}
			if t := masqueClientBearerToken(&cl); t != "" {
				if _, ok := seen[t]; !ok {
					seen[t] = struct{}{}
					out = append(out, t)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func masqueClientBearerToken(client *model.Client) string {
	if client == nil || len(client.Config) == 0 {
		return ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(client.Config, &root); err != nil {
		return ""
	}
	m, ok := root["masque"].(map[string]interface{})
	if !ok || m == nil {
		return ""
	}
	s, _ := m["server_token"].(string)
	return strings.TrimSpace(s)
}

func collectMasqueBasicCredentials(tx *gorm.DB, opt map[string]interface{}) []map[string]interface{} {
	type pair struct{ u, p string }
	seen := make(map[string]struct{})
	var pairs []pair
	gs := GroupService{}

	addClient := func(cl *model.Client) {
		u, pw := masqueClientBasicPair(cl)
		if u == "" || pw == "" {
			return
		}
		key := u + "\x00" + pw
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		pairs = append(pairs, pair{u: u, p: pw})
	}

	for _, cid := range parseUintListFromAny(opt["member_client_ids"]) {
		if cid == 0 {
			continue
		}
		var cl model.Client
		if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
			continue
		}
		addClient(&cl)
	}
	for _, gid := range parseUintListFromAny(opt["member_group_ids"]) {
		if gid == 0 {
			continue
		}
		ids, err := gs.ResolveMemberClientIDs(tx, gid)
		if err != nil {
			continue
		}
		for _, cid := range ids {
			var cl model.Client
			if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
				continue
			}
			addClient(&cl)
		}
	}
	out := make([]map[string]interface{}, 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, map[string]interface{}{
			"username": pr.u,
			"password": pr.p,
		})
	}
	return out
}

func masqueClientBasicPair(client *model.Client) (user, pass string) {
	if client == nil || len(client.Config) == 0 {
		return "", ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(client.Config, &root); err != nil {
		return "", ""
	}
	m, ok := root["masque"].(map[string]interface{})
	if !ok || m == nil {
		return "", ""
	}
	u, _ := m["client_basic_username"].(string)
	p, _ := m["client_basic_password"].(string)
	return strings.TrimSpace(u), strings.TrimSpace(p)
}

func collectMemberMasqueLeafPins(tx *gorm.DB, opt map[string]interface{}) []string {
	seen := make(map[string]struct{})
	var out []string
	gs := GroupService{}

	clients := parseUintListFromAny(opt["member_client_ids"])
	for _, cid := range clients {
		if cid == 0 {
			continue
		}
		var cl model.Client
		if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
			continue
		}
		if h := MasqueClientLeafSPKIHex(&cl); h != "" {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				out = append(out, h)
			}
		}
	}
	groups := parseUintListFromAny(opt["member_group_ids"])
	for _, gid := range groups {
		if gid == 0 {
			continue
		}
		ids, err := gs.ResolveMemberClientIDs(tx, gid)
		if err != nil {
			continue
		}
		for _, cid := range ids {
			var cl model.Client
			if err := tx.Where("id = ?", cid).First(&cl).Error; err != nil {
				continue
			}
			if h := MasqueClientLeafSPKIHex(&cl); h != "" {
				if _, ok := seen[h]; !ok {
					seen[h] = struct{}{}
					out = append(out, h)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
