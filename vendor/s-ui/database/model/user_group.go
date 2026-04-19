package model

// UserGroup is a first-class ACL-style group (not the legacy string field on Client).
// Nesting is modeled via GroupGroupMember (multiple parents per child).
type UserGroup struct {
	Id   uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name string `json:"name" gorm:"uniqueIndex;not null"`
	Desc string `json:"desc"`
}

// ClientGroupMember links a client to a group (many-to-many).
type ClientGroupMember struct {
	ClientId uint `json:"client_id" gorm:"primaryKey"`
	GroupId  uint `json:"group_id" gorm:"primaryKey"`
}
