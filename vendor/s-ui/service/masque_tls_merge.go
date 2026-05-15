package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

// mergeMasqueTLSPemFromStoredProfile merges TLS profile row (sui_tls_id) into masque options for sing-box:
// generic masque (panel) always uses inbound "tls" from Tls.Server; warp_masque uses outbound_tls from Tls.Client.
// Legacy flat keys (certificate, key, tls_server_name, insecure) are removed from options.
func mergeMasqueTLSPemFromStoredProfile(db *gorm.DB, ep *model.Endpoint) error {
	if db == nil || ep == nil || len(ep.Options) == 0 {
		return nil
	}
	if ep.Type != masqueType && ep.Type != warpMasqueType {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	if ep.Type == warpMasqueType {
		stripLegacyMasqueTLSFlatKeys(opt)
		delete(opt, "tls")
		delete(opt, "outbound_tls")
		delete(opt, "sui_tls_id")
		ensureSingBoxMasqueMode(ep, opt)
		return marshalMasqueEndpointOptions(ep, opt)
	}
	stripLegacyMasqueTLSFlatKeys(opt)
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(opt["mode"])))
	if mode == "" {
		mode = "server"
	}
	if ep.Type == masqueType {
		// Panel generic masque is always a server listen endpoint; TLS profile maps to inbound "tls".
		mode = "server"
	}
	if ep.Type == warpMasqueType {
		mode = "client"
	}
	idf, ok := opt["sui_tls_id"]
	if !ok || uintFromAny(idf) == 0 {
		if mode == "server" {
			delete(opt, "tls")
			delete(opt, "outbound_tls")
		} else {
			delete(opt, "tls")
		}
		ensureSingBoxMasqueMode(ep, opt)
		return marshalMasqueEndpointOptions(ep, opt)
	}
	tlsID := uintFromAny(idf)
	var tlsRow model.Tls
	if err := db.Where("id = ?", tlsID).First(&tlsRow).Error; err != nil {
		return fmt.Errorf("masque sui_tls_id=%d: %w", tlsID, err)
	}
	if mode == "server" {
		var srv map[string]interface{}
		if len(tlsRow.Server) > 0 {
			if err := json.Unmarshal(tlsRow.Server, &srv); err != nil {
				return err
			}
		}
		if len(srv) == 0 {
			return fmt.Errorf("masque sui_tls_id=%d: empty tls.server profile", tlsID)
		}
		opt["tls"] = cloneJSONMap(srv)
		delete(opt, "outbound_tls")
	} else {
		var client map[string]interface{}
		if len(tlsRow.Client) > 0 {
			if err := json.Unmarshal(tlsRow.Client, &client); err != nil {
				return err
			}
		}
		if len(client) > 0 {
			opt["outbound_tls"] = cloneJSONMap(client)
		} else {
			delete(opt, "outbound_tls")
		}
		delete(opt, "tls")
	}
	ensureSingBoxMasqueMode(ep, opt)
	return marshalMasqueEndpointOptions(ep, opt)
}

func marshalMasqueEndpointOptions(ep *model.Endpoint, opt map[string]interface{}) error {
	raw, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}

func stripLegacyMasqueTLSFlatKeys(opt map[string]interface{}) {
	for _, k := range []string{"certificate", "key", "tls_server_name", "insecure"} {
		delete(opt, k)
	}
}

func cloneJSONMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
