package database

import "gorm.io/gorm"

// NormalizeEndpointJSONStorage ensures endpoint JSON payload columns are stored as bytes.
// Some manual SQL edits may write TEXT values and break scanning into json.RawMessage.
func NormalizeEndpointJSONStorage(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasTable("endpoints") {
		return nil
	}
	// Convert accidentally text-typed payloads back to byte storage.
	if err := db.Exec("UPDATE endpoints SET options = CAST(options AS BLOB) WHERE typeof(options) = 'text'").Error; err != nil {
		return err
	}
	if err := db.Exec("UPDATE endpoints SET ext = CAST(ext AS BLOB) WHERE typeof(ext) = 'text'").Error; err != nil {
		return err
	}
	return nil
}
