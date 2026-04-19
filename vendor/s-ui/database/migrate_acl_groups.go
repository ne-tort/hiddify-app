package database

import (
	"github.com/alireza0/s-ui/database/model"

	"gorm.io/gorm"
)

func columnExists(db *gorm.DB, table, col string) bool {
	var n int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('"+table+"') WHERE name = ?", col).Scan(&n)
	return n > 0
}

func tableExists(db *gorm.DB, table string) bool {
	var n int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
	return n > 0
}

// MigrateUserGroupParentToEdges copies legacy user_groups.parent_id into group_group_members,
// then rebuilds user_groups without parent_id. (SQLite self-FK breaks ALTER DROP COLUMN.)
// Must run before AutoMigrate on UserGroup so GORM never sees parent_id.
func MigrateUserGroupParentToEdges(db *gorm.DB) error {
	if !tableExists(db, "user_groups") || !columnExists(db, "user_groups", "parent_id") {
		return nil
	}

	if !db.Migrator().HasTable(&model.GroupGroupMember{}) {
		if err := db.Migrator().CreateTable(&model.GroupGroupMember{}); err != nil {
			return err
		}
	}

	type row struct {
		Id       uint
		ParentId *uint
	}
	var rows []row
	if err := db.Raw("SELECT id, parent_id FROM user_groups WHERE parent_id IS NOT NULL AND parent_id != 0").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if r.ParentId == nil || *r.ParentId == 0 {
			continue
		}
		if err := db.Exec("INSERT OR IGNORE INTO group_group_members (parent_group_id, child_group_id) VALUES (?, ?)", *r.ParentId, r.Id).Error; err != nil {
			return err
		}
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
			return err
		}
		// Match model.UserGroup (no parent_id)
		if err := tx.Exec(`
CREATE TABLE user_groups__acl_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	"desc" TEXT,
	UNIQUE(name)
)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO user_groups__acl_new (id, name, "desc") SELECT id, name, "desc" FROM user_groups`).Error; err != nil {
			return err
		}
		if err := tx.Exec("DROP TABLE user_groups").Error; err != nil {
			return err
		}
		if err := tx.Exec("ALTER TABLE user_groups__acl_new RENAME TO user_groups").Error; err != nil {
			return err
		}
		var maxID uint
		_ = tx.Raw("SELECT COALESCE(MAX(id), 0) FROM user_groups").Scan(&maxID)
		if maxID > 0 {
			_ = tx.Exec("DELETE FROM sqlite_sequence WHERE name = 'user_groups'").Error
			if err := tx.Exec("INSERT INTO sqlite_sequence (name, seq) VALUES ('user_groups', ?)", maxID).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			return err
		}
		return nil
	})
}
