package model

import "encoding/json"

type GeoDataset struct {
	Id             uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Kind           string `json:"kind" gorm:"index:idx_geo_dataset_kind,unique;size:16;not null"`
	SourceUrl      string `json:"source_url" gorm:"size:512"`
	SourceVersion  string `json:"source_version" gorm:"size:128"`
	SourceSha256   string `json:"source_sha256" gorm:"size:128"`
	ItemCount      int    `json:"item_count"`
	Status         string `json:"status" gorm:"size:32;not null;default:'ready'"`
	ActiveRevision uint   `json:"active_revision"`
	LastSyncUnix   int64  `json:"last_sync_unix"`
	LastError      string `json:"last_error" gorm:"size:1024"`
}

type GeoCatalogRevision struct {
	Id           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	DatasetKind  string `json:"dataset_kind" gorm:"index:idx_geo_rev_kind_no,priority:1;size:16;not null"`
	RevisionNo   int    `json:"revision_no" gorm:"index:idx_geo_rev_kind_no,priority:2;not null"`
	BuiltFromVer string `json:"built_from_version" gorm:"size:128"`
	BuiltFromSha string `json:"built_from_sha" gorm:"size:128"`
	BuiltAtUnix  int64  `json:"built_at_unix"`
	Notes        string `json:"notes" gorm:"size:1024"`
}

type GeoTag struct {
	Id           uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	DatasetKind  string          `json:"dataset_kind" gorm:"index:idx_geo_tag_kind_norm,priority:1;size:16;not null"`
	TagNorm      string          `json:"tag_norm" gorm:"index:idx_geo_tag_kind_norm,priority:2;size:256;not null"`
	TagRaw       string          `json:"tag_raw" gorm:"size:256"`
	Origin       string          `json:"origin" gorm:"size:16;not null;default:'upstream'"`
	MetadataJson json.RawMessage `json:"metadata,omitempty" gorm:"type:json"`
	IsDeleted    bool            `json:"is_deleted" gorm:"index;default:false"`
}

type GeoTagItem struct {
	Id             uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	GeoTagId       uint   `json:"geo_tag_id" gorm:"index:idx_geo_tag_item_unique,priority:1;not null"`
	ItemType       string `json:"item_type" gorm:"index:idx_geo_tag_item_unique,priority:2;size:32;not null"`
	ValueNorm      string `json:"value_norm" gorm:"index:idx_geo_tag_item_unique,priority:3;size:1024;not null"`
	ValueRaw       string `json:"value_raw" gorm:"size:1024"`
	AttributesJson string `json:"attributes_json" gorm:"type:text"`
	Origin         string `json:"origin" gorm:"size:16;not null;default:'upstream'"`
	IsDeleted      bool   `json:"is_deleted" gorm:"index;default:false"`
}

type RoutingProfile struct {
	Id              uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string          `json:"name" gorm:"unique;size:128;not null"`
	Desc            string          `json:"desc" gorm:"size:512"`
	Enabled         bool            `json:"enabled" gorm:"default:true"`
	RouteOrder      json.RawMessage `json:"route_order" gorm:"type:json"`
	DirectSites     json.RawMessage `json:"direct_sites" gorm:"type:json"`
	DirectIp        json.RawMessage `json:"direct_ip" gorm:"type:json"`
	ProxySites      json.RawMessage `json:"proxy_sites" gorm:"type:json"`
	ProxyIp         json.RawMessage `json:"proxy_ip" gorm:"type:json"`
	BlockSites      json.RawMessage `json:"block_sites" gorm:"type:json"`
	BlockIp         json.RawMessage `json:"block_ip" gorm:"type:json"`
	DnsPolicy       json.RawMessage `json:"dns_policy" gorm:"type:json"`
	Compatibility   json.RawMessage `json:"compatibility" gorm:"type:json"`
	GeoCatalogVer   string          `json:"geo_catalog_version" gorm:"size:128"`
	LastValidateErr string          `json:"last_validate_error" gorm:"size:1024"`
}

type RoutingProfileClientMember struct {
	ProfileId uint `json:"profile_id" gorm:"primaryKey;autoIncrement:false"`
	ClientId  uint `json:"client_id" gorm:"primaryKey;autoIncrement:false;index"`
}

type RoutingProfileGroupMember struct {
	ProfileId uint `json:"profile_id" gorm:"primaryKey;autoIncrement:false"`
	GroupId   uint `json:"group_id" gorm:"primaryKey;autoIncrement:false;index"`
}
