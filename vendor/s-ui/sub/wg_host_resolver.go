package sub

import (
	"strings"

	"github.com/alireza0/s-ui/service"
)

func resolveWGServerHostWithSettings(settingService service.SettingService, requestHost string) string {
	fallbackHost := strings.TrimSpace(requestHost)
	getSubDomainSafe := func() (string, bool) {
		defer func() {
			_ = recover()
		}()
		domain, err := settingService.GetSubDomain()
		if err != nil {
			return "", false
		}
		return domain, true
	}
	getSubURISafe := func() (string, bool) {
		defer func() {
			_ = recover()
		}()
		uri, err := settingService.GetSubURI()
		if err != nil {
			return "", false
		}
		return uri, true
	}

	if domain, ok := getSubDomainSafe(); ok {
		domain = strings.TrimSpace(domain)
		if domain != "" {
			return domain
		}
	}
	if uri, ok := getSubURISafe(); ok {
		if h := hostFromSubscriptionURI(strings.TrimSpace(uri)); h != "" {
			return h
		}
	}
	if fallbackHost != "" {
		return fallbackHost
	}
	return ""
}
