package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

// mergeMasqueTLSPemFromStoredProfile merges TLS profile row (sui_tls_id) into masque options for sing-box:
// PEM certificate/key, and optional tls_server_name / insecure from the same JSON as inbound TLS profiles.
func mergeMasqueTLSPemFromStoredProfile(db *gorm.DB, ep *model.Endpoint) error {
	if db == nil || ep == nil || ep.Type != masqueType || len(ep.Options) == 0 {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	idf, ok := opt["sui_tls_id"]
	if !ok {
		return nil
	}
	tlsID := uintFromAny(idf)
	if tlsID == 0 {
		return nil
	}
	var tlsRow model.Tls
	if err := db.Where("id = ?", tlsID).First(&tlsRow).Error; err != nil {
		return fmt.Errorf("masque sui_tls_id=%d: %w", tlsID, err)
	}
	var srv map[string]interface{}
	if len(tlsRow.Server) > 0 {
		if err := json.Unmarshal(tlsRow.Server, &srv); err != nil {
			return err
		}
		if v, ok := srv["server_name"].(string); ok && strings.TrimSpace(v) != "" {
			if strings.TrimSpace(fmt.Sprint(opt["tls_server_name"])) == "" {
				opt["tls_server_name"] = strings.TrimSpace(v)
			}
		}
		if v, ok := srv["insecure"].(bool); ok {
			if _, had := opt["insecure"]; !had {
				opt["insecure"] = v
			}
		}
	}
	cert, key, pemOK := extractPemFromTlsServerJSON(tlsRow.Server)
	if pemOK {
		opt["certificate"] = cert
		opt["key"] = key
	}
	raw, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

func extractPemFromTlsServerJSON(server json.RawMessage) (cert string, key string, ok bool) {
	if len(server) == 0 {
		return "", "", false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(server, &m); err != nil {
		return "", "", false
	}
	cert = joinPemFields(m["certificate"])
	key = joinPemFields(m["key"])
	if cert != "" && key != "" {
		return cert, key, true
	}
	return "", "", false
}

func joinPemFields(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []interface{}:
		var parts []string
		for _, x := range t {
			s := strings.TrimSpace(fmt.Sprint(x))
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	case []string:
		var parts []string
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
