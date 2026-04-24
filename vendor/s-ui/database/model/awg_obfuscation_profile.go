package model

// AwgObfuscationProfile stores AmneziaWG 2.0 obfuscation parameters (aligned with sing-box AwgEndpointOptions).
// Nullable pointer fields mean "unset" in DB / omitted in JSON export.
type AwgObfuscationProfile struct {
	Id              uint    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string  `json:"name" gorm:"unique;size:128;not null"`
	Desc            string  `json:"desc" gorm:"size:512"`
	Enabled         bool    `json:"enabled" gorm:"default:true"`
	Jc              *int    `json:"jc,omitempty"`
	Jmin            *int    `json:"jmin,omitempty"`
	Jmax            *int    `json:"jmax,omitempty"`
	S1              *int    `json:"s1,omitempty"`
	S2              *int    `json:"s2,omitempty"`
	S3              *int    `json:"s3,omitempty"`
	S4              *int    `json:"s4,omitempty"`
	H1              *string `json:"h1,omitempty" gorm:"size:512"`
	H2              *string `json:"h2,omitempty" gorm:"size:512"`
	H3              *string `json:"h3,omitempty" gorm:"size:512"`
	H4              *string `json:"h4,omitempty" gorm:"size:512"`
	I1              *string `json:"i1,omitempty" gorm:"size:512"`
	I2              *string `json:"i2,omitempty" gorm:"size:512"`
	I3              *string `json:"i3,omitempty" gorm:"size:512"`
	I4              *string `json:"i4,omitempty" gorm:"size:512"`
	I5              *string `json:"i5,omitempty" gorm:"size:512"`
	// GeneratorSpec stores last Architect-style GeneratorInput JSON (UI round-trip); not exported to sing-box JSON.
	GeneratorSpec string `json:"generator_spec,omitempty" gorm:"column:generator_spec;type:text"`
	LastValidateErr string `json:"last_validate_error" gorm:"size:1024"`
}

type AwgObfuscationProfileClientMember struct {
	ProfileId uint `json:"profile_id" gorm:"primaryKey;autoIncrement:false"`
	ClientId  uint `json:"client_id" gorm:"primaryKey;autoIncrement:false;index"`
}

type AwgObfuscationProfileGroupMember struct {
	ProfileId uint `json:"profile_id" gorm:"primaryKey;autoIncrement:false"`
	GroupId   uint `json:"group_id" gorm:"primaryKey;autoIncrement:false;index"`
}
