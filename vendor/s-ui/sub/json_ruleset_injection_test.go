package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alireza0/s-ui/database"
)

func TestExtractManagedRuleSetTags(t *testing.T) {
	in := []map[string]interface{}{
		{"rule_set": []interface{}{"geoip-ru", "geosite-ads"}},
		{"rule_set": []string{"geoip-ru", "geosite-ru"}},
		{"ip_cidr": []string{"1.1.1.1/32"}},
	}
	got := extractManagedRuleSetTags(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique tags, got %d: %v", len(got), got)
	}
}

func TestEnsureManagedRuleSetDefinitions_AppendsMissing(t *testing.T) {
	j := &JsonService{}
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("sui-json-ruleset-%d.db", time.Now().UnixNano()))
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	jsonConfig := map[string]interface{}{
		"route": map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{"action": "sniff"}},
		},
	}
	managed := []map[string]interface{}{
		{"rule_set": []interface{}{"geoip-ru", "geosite-ads"}},
	}
	ensureManagedRuleSetDefinitions(j, &jsonConfig, managed, "example.com")
	route := jsonConfig["route"].(map[string]interface{})
	rs, ok := route["rule_set"].([]interface{})
	if !ok || len(rs) == 0 {
		t.Fatalf("expected rule_set definitions, got %v", route["rule_set"])
	}
	raw, _ := json.Marshal(rs)
	s := string(raw)
	if !strings.Contains(s, "geoip-ru") || !strings.Contains(s, "geosite-ads") {
		t.Fatalf("missing expected tags in rule_set: %s", s)
	}
}
