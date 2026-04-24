package database

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"time"

	"github.com/alireza0/s-ui/config"
	"github.com/alireza0/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func initUser() error {
	var count int64
	err := db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
}

func OpenDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, 01740)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL"
	db, err = gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if config.IsDebug() {
		db = db.Debug()
	}
	return nil
}

func InitDB(dbPath string) error {
	err := OpenDB(dbPath)
	if err != nil {
		return err
	}

	// Legacy parent_id → group_group_members + rebuild user_groups (before AutoMigrate).
	if err = MigrateUserGroupParentToEdges(db); err != nil {
		return err
	}

	// Default Outbounds
	if !db.Migrator().HasTable(&model.Outbound{}) {
		db.Migrator().CreateTable(&model.Outbound{})
		defaultOutbound := []model.Outbound{
			{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		}
		db.Create(&defaultOutbound)
	}

	err = db.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Service{},
		&model.Endpoint{},
		&model.L3RouterPeer{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
		&model.UserGroup{},
		&model.ClientGroupMember{},
		&model.GroupGroupMember{},
		&model.InboundUserPolicy{},
		&model.InboundPolicyGroup{},
		&model.InboundPolicyClient{},
		&model.GeoDataset{},
		&model.GeoCatalogRevision{},
		&model.GeoTag{},
		&model.GeoTagItem{},
		&model.RoutingProfile{},
		&model.RoutingProfileClientMember{},
		&model.RoutingProfileGroupMember{},
		&model.AwgObfuscationProfile{},
		&model.AwgObfuscationProfileClientMember{},
		&model.AwgObfuscationProfileGroupMember{},
	)
	if err != nil {
		return err
	}
	if err = MigrateL3RouterPeersFromEndpointOptions(db); err != nil {
		return err
	}
	if err = MigrateL3RouterPeerSerial(db); err != nil {
		return err
	}
	if err = MigrateLegacyInboundPolicies(db); err != nil {
		return err
	}
	if err = MigrateInboundPolicyModeAliases(db); err != nil {
		return err
	}
	err = initUser()
	if err != nil {
		return err
	}

	return nil
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
