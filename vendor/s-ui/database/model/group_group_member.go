package model

// GroupGroupMember links a child group to a parent group (many parents per child; DAG, no cycles).
type GroupGroupMember struct {
	ParentGroupId uint `json:"parent_group_id" gorm:"primaryKey"`
	ChildGroupId  uint `json:"child_group_id" gorm:"primaryKey"`
}

func (GroupGroupMember) TableName() string {
	return "group_group_members"
}
