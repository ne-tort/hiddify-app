package database

import (
	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

// MigrateL3RouterPeerSerial assigns peer_serial for rows that have 0: either 1..n by id (all zero), or max+1 for orphans.
func MigrateL3RouterPeerSerial(db *gorm.DB) error {
	var endpointIDs []uint
	if err := db.Model(&model.L3RouterPeer{}).Distinct("endpoint_id").Pluck("endpoint_id", &endpointIDs).Error; err != nil {
		return err
	}
	for _, eid := range endpointIDs {
		var rows []model.L3RouterPeer
		if err := db.Where("endpoint_id = ?", eid).Order("id asc").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			continue
		}
		allZero := true
		for i := range rows {
			if rows[i].PeerSerial != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			for i := range rows {
				if err := db.Model(&model.L3RouterPeer{}).Where("id = ?", rows[i].Id).Update("peer_serial", uint(i)+1).Error; err != nil {
					return err
				}
			}
			continue
		}
		var maxS uint
		if err := db.Model(&model.L3RouterPeer{}).Where("endpoint_id = ? AND peer_serial > 0", eid).Select("COALESCE(MAX(peer_serial), 0)").Scan(&maxS).Error; err != nil {
			return err
		}
		next := maxS
		for i := range rows {
			if rows[i].PeerSerial != 0 {
				continue
			}
			next++
			if err := db.Model(&model.L3RouterPeer{}).Where("id = ?", rows[i].Id).Update("peer_serial", next).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
