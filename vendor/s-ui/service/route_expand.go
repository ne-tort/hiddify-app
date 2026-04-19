package service

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

const suiAuthGroupsKey = "s_ui_auth_groups"

// ExpandSUIFieldsInSingboxConfig expands s_ui_auth_groups into auth_user and removes s-ui-only keys
// (в т.ч. l3router_managed в правилах маршрутизации) перед передачей конфига в sing-box.
func ExpandSUIFieldsInSingboxConfig(db *gorm.DB, raw []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw, err
	}
	gs := GroupService{}
	if route, ok := root["route"].(map[string]interface{}); ok {
		if err := expandRouteOrDNSRules(db, &gs, route, "rules"); err != nil {
			return nil, err
		}
	}
	if dns, ok := root["dns"].(map[string]interface{}); ok {
		if err := expandRouteOrDNSRules(db, &gs, dns, "rules"); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return out, nil
}

func expandRouteOrDNSRules(db *gorm.DB, gs *GroupService, container map[string]interface{}, key string) error {
	rules, ok := container[key].([]interface{})
	if !ok {
		return nil
	}
	for i, r := range rules {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if err := expandOneRule(db, gs, m); err != nil {
			return err
		}
		// logical rules: recurse into nested rules
		if typ, _ := m["type"].(string); typ == "logical" {
			if nested, ok := m["rules"].([]interface{}); ok {
				for j, nr := range nested {
					if nm, ok := nr.(map[string]interface{}); ok {
						if err := expandOneRule(db, gs, nm); err != nil {
							return err
						}
						nested[j] = nm
					}
				}
				m["rules"] = nested
			}
		}
		rules[i] = m
	}
	container[key] = rules
	return nil
}

func expandOneRule(db *gorm.DB, gs *GroupService, m map[string]interface{}) error {
	delete(m, "l3router_managed")
	rawIDs, ok := m[suiAuthGroupsKey]
	if !ok || rawIDs == nil {
		delete(m, suiAuthGroupsKey)
		return nil
	}
	delete(m, suiAuthGroupsKey)
	var groupIDs []uint
	switch v := rawIDs.(type) {
	case []interface{}:
		for _, x := range v {
			groupIDs = append(groupIDs, toUint(x))
		}
	case []float64:
		for _, x := range v {
			groupIDs = append(groupIDs, uint(x))
		}
	default:
		return nil
	}
	nameSet := map[string]struct{}{}
	for _, id := range groupIDs {
		if id == 0 {
			continue
		}
		names, err := gs.ResolveMemberUsernames(db, id)
		if err != nil {
			return err
		}
		for _, n := range names {
			nameSet[strings.TrimSpace(n)] = struct{}{}
		}
	}
	existing := toStringSlice(m["auth_user"])
	for _, n := range existing {
		if n != "" {
			nameSet[n] = struct{}{}
		}
	}
	if len(nameSet) == 0 {
		if len(existing) > 0 {
			m["auth_user"] = interfaceSliceFromStrings(existing)
		} else {
			delete(m, "auth_user")
		}
		return nil
	}
	merged := make([]string, 0, len(nameSet))
	for n := range nameSet {
		merged = append(merged, n)
	}
	// stable sort
	for i := 0; i < len(merged); i++ {
		for j := i + 1; j < len(merged); j++ {
			if merged[j] < merged[i] {
				merged[i], merged[j] = merged[j], merged[i]
			}
		}
	}
	m["auth_user"] = interfaceSliceFromStrings(merged)
	return nil
}

func interfaceSliceFromStrings(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, x := range s {
		out[i] = x
	}
	return out
}

func toUint(v interface{}) uint {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return uint(n)
		}
	case int:
		if n > 0 {
			return uint(n)
		}
	case int64:
		if n > 0 {
			return uint(n)
		}
	case json.Number:
		x, _ := n.Int64()
		if x > 0 {
			return uint(x)
		}
	}
	return 0
}
