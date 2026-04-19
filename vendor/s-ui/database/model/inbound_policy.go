package model

// InboundUserPolicy stores how clients are associated with a user-capable inbound (ACL).
// Mode: none | all | groups | clients
type InboundUserPolicy struct {
	InboundId uint   `json:"inbound_id" gorm:"primaryKey"`
	Mode      string `json:"mode" gorm:"not null"`
}

func (InboundUserPolicy) TableName() string {
	return "inbound_user_policies"
}

// InboundPolicyGroup links an inbound to a user group (effective clients = ResolveMemberClientIDs for that group).
type InboundPolicyGroup struct {
	InboundId uint `json:"inbound_id" gorm:"primaryKey"`
	GroupId   uint `json:"group_id" gorm:"primaryKey"`
}

func (InboundPolicyGroup) TableName() string {
	return "inbound_policy_groups"
}

// InboundPolicyClient links an inbound to explicitly listed clients (mode=clients).
type InboundPolicyClient struct {
	InboundId uint `json:"inbound_id" gorm:"primaryKey"`
	ClientId  uint `json:"client_id" gorm:"primaryKey"`
}

func (InboundPolicyClient) TableName() string {
	return "inbound_policy_clients"
}
