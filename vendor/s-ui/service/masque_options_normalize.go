package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"
)

// normalizeMasqueEndpointOptionsOnSave enforces panel semantics: server mode uses TLS profile only
// (sui_tls_id); inline certificate/key are stripped and not persisted.
func normalizeMasqueEndpointOptionsOnSave(ep *model.Endpoint) error {
	if ep == nil || ep.Type != masqueType || len(ep.Options) == 0 {
		return nil
	}
	var opt map[string]interface{}
	if err := json.Unmarshal(ep.Options, &opt); err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(opt["mode"])))
	if mode == "" {
		mode = "server"
	}
	if mode == "server" {
		tlsID := uintFromAny(opt["sui_tls_id"])
		if tlsID == 0 {
			return fmt.Errorf("masque server: выберите TLS-сертификат (sui_tls_id)")
		}
	}
	delete(opt, "certificate")
	delete(opt, "key")
	raw, err := json.MarshalIndent(opt, "", "  ")
	if err != nil {
		return err
	}
	ep.Options = raw
	return nil
}