package database

import (
	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

func inboundTypeHasUsers(t string) bool {
	switch t {
	case "mixed", "socks", "http", "shadowsocks", "vmess", "trojan", "naive", "hysteria", "shadowtls", "tuic", "hysteria2", "vless", "anytls":
		return true
	}
	return false
}

// MigrateLegacyInboundPolicies infers inbound_user_policies from existing clients.inbounds for older DBs.
func MigrateLegacyInboundPolicies(db *gorm.DB) error {
	var inbounds []model.Inbound
	if err := db.Model(model.Inbound{}).Select("id", "type").Find(&inbounds).Error; err != nil {
		return err
	}
	var enabledTotal int64
	_ = db.Model(model.Client{}).Where("enable = ?", true).Count(&enabledTotal).Error

	for _, in := range inbounds {
		if !inboundTypeHasUsers(in.Type) {
			continue
		}
		var n int64
		if err := db.Model(model.InboundUserPolicy{}).Where("inbound_id = ?", in.Id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}

		var withInbound []uint
		if err := db.Raw(`
SELECT DISTINCT clients.id FROM clients, json_each(clients.inbounds) AS je
WHERE je.value = ? AND clients.enable = true`, in.Id).Scan(&withInbound).Error; err != nil {
			return err
		}

		pol := model.InboundUserPolicy{InboundId: in.Id}
		switch {
		case len(withInbound) == 0:
			pol.Mode = "none"
		case int64(len(withInbound)) == enabledTotal && enabledTotal > 0:
			pol.Mode = "all"
		default:
			pol.Mode = "clients"
		}
		if err := db.Create(&pol).Error; err != nil {
			return err
		}
		if pol.Mode == "clients" {
			for _, cid := range withInbound {
				if err := db.Create(&model.InboundPolicyClient{InboundId: in.Id, ClientId: cid}).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// MigrateInboundPolicyModeAliases fixes legacy rows where mode was stored as UI tokens "group"/"client".
func MigrateInboundPolicyModeAliases(db *gorm.DB) error {
	if err := db.Model(model.InboundUserPolicy{}).Where("LOWER(mode) = ?", "group").Update("mode", "groups").Error; err != nil {
		return err
	}
	if err := db.Model(model.InboundUserPolicy{}).Where("LOWER(mode) = ?", "client").Update("mode", "clients").Error; err != nil {
		return err
	}
	return nil
}
