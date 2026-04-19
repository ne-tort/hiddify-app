package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupGroupACLTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserGroup{}, &model.GroupGroupMember{}, &model.ClientGroupMember{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func setupGroupDeleteCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.UserGroup{},
		&model.GroupGroupMember{},
		&model.ClientGroupMember{},
		&model.Client{},
		&model.InboundPolicyGroup{},
		&model.Endpoint{},
		&model.Setting{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGroupEdgeCycleRejected(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	a := model.UserGroup{Name: "A"}
	b := model.UserGroup{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	// B under A
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: a.Id, ChildGroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	// A under B would cycle
	err := gs.replaceChildParentEdges(db, a.Id, []uint{b.Id})
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestResolveMemberNestedGroups(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	a := model.UserGroup{Name: "A"}
	b := model.UserGroup{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: a.Id, ChildGroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "u1", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := gs.ResolveMemberClientIDs(db, a.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("got %v want [%d]", ids, cl.Id)
	}
}

func TestResolveMemberDedup(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	a := model.UserGroup{Name: "A"}
	b := model.UserGroup{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: a.Id, ChildGroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "u1", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: a.Id}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := gs.ResolveMemberClientIDs(db, a.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("got %v want one id %d", ids, cl.Id)
	}
}

func TestBoundGroupNameFromOptions(t *testing.T) {
	raw := json.RawMessage(`{"bound_group_name": " test ", "bound_group_id": 0}`)
	if boundGroupNameFromOptions(raw) != "test" {
		t.Fatal()
	}
}

func TestGroupEditKeepsEdgesWhenParentsOmitted(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	a := model.UserGroup{Name: "A"}
	b := model.UserGroup{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: a.Id, ChildGroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"id":   b.Id,
		"name": "B_renamed",
		"desc": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "edit", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var cnt int64
	if err := db.Model(model.GroupGroupMember{}).Where("parent_group_id = ? AND child_group_id = ?", a.Id, b.Id).Count(&cnt).Error; err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("edge missing after name-only edit, cnt=%d", cnt)
	}
}

func TestSetMembersClientsOnlyKeepsEdges(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	g1 := model.UserGroup{Name: "G1"}
	g2 := model.UserGroup{Name: "G2"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&g2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: g1.Id, ChildGroupId: g2.Id}).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "c", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"group_id":   g2.Id,
		"client_ids": []uint{cl.Id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "setMembers", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var ecnt int64
	if err := db.Model(model.GroupGroupMember{}).Where("parent_group_id = ? AND child_group_id = ?", g1.Id, g2.Id).Count(&ecnt).Error; err != nil {
		t.Fatal(err)
	}
	if ecnt != 1 {
		t.Fatalf("group edge should remain when only client_ids sent, got %d", ecnt)
	}
}

func TestSetMembersUpdatesClientAndEdges(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	g1 := model.UserGroup{Name: "G1"}
	g2 := model.UserGroup{Name: "G2"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&g2).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "c", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"group_id":            g2.Id,
		"client_ids":          []uint{cl.Id},
		"member_of_group_ids": []uint{g1.Id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "setMembers", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(model.ClientGroupMember{}).Where("group_id = ? AND client_id = ?", g2.Id, cl.Id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("client membership want 1 got %d", n)
	}
	var ecnt int64
	if err := db.Model(model.GroupGroupMember{}).Where("parent_group_id = ? AND child_group_id = ?", g2.Id, g1.Id).Count(&ecnt).Error; err != nil {
		t.Fatal(err)
	}
	if ecnt != 1 {
		t.Fatalf("group edge want 1 got %d", ecnt)
	}
}

func TestSetMembersConflictingEdgeFieldsRejected(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	g1 := model.UserGroup{Name: "G1"}
	g2 := model.UserGroup{Name: "G2"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&g2).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"group_id":          g2.Id,
		"client_ids":        []uint{},
		"child_group_ids":   []uint{g1.Id},
		"parent_group_ids":  []uint{},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "setMembers", raw)
	})
	if err == nil {
		t.Fatal("expected error on conflicting child and parent fields")
	}
}

func TestNewGroupWithChildGroupIDsUsesParentChildrenDirection(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	g1 := model.UserGroup{Name: "G1"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "nested", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: g1.Id}).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"name":            "G2",
		"desc":            "",
		"child_group_ids": []uint{g1.Id},
		"client_ids":      []uint{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "new", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var g2 model.UserGroup
	if err := db.Where("name = ?", "G2").First(&g2).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := gs.ResolveMemberClientIDs(db, g2.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("expected nested client under G2, got %v", ids)
	}
}

func TestSetMembersWithChildGroupIDsUsesExpectedDirection(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	g1 := model.UserGroup{Name: "G1"}
	g2 := model.UserGroup{Name: "G2"}
	if err := db.Create(&g1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&g2).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "nested_client", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: g1.Id}).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"group_id":        g2.Id,
		"client_ids":      []uint{},
		"child_group_ids": []uint{g1.Id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "setMembers", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var ecnt int64
	if err := db.Model(model.GroupGroupMember{}).Where("parent_group_id = ? AND child_group_id = ?", g2.Id, g1.Id).Count(&ecnt).Error; err != nil {
		t.Fatal(err)
	}
	if ecnt != 1 {
		t.Fatalf("expected edge g2->g1, got %d", ecnt)
	}
	ids, err := gs.ResolveMemberClientIDs(db, g2.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("expected nested client in g2 effective ids, got %v", ids)
	}
}

func TestGetAllGroupsNestedEffectiveCounts(t *testing.T) {
	db := setupGroupACLTestDB(t)
	gs := GroupService{}
	a := model.UserGroup{Name: "A"}
	b := model.UserGroup{Name: "B"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: a.Id, ChildGroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "u1", Enable: true}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: b.Id}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := gs.GetAllGroups(db)
	if err != nil {
		t.Fatal(err)
	}
	var gotA *userGroupDTO
	for i := range rows {
		if rows[i].Id == a.Id {
			gotA = &rows[i]
			break
		}
	}
	if gotA == nil {
		t.Fatal("group A missing")
	}
	if gotA.DirectMemberCount != 0 {
		t.Fatalf("direct want 0 got %d", gotA.DirectMemberCount)
	}
	if gotA.EffectiveMemberCount != 1 {
		t.Fatalf("effective want 1 got %d", gotA.EffectiveMemberCount)
	}
	if gotA.MemberCount != 1 {
		t.Fatalf("member_count alias want 1 got %d", gotA.MemberCount)
	}
}

func TestDeleteGroupCleansInboundPolicyReferences(t *testing.T) {
	db := setupGroupDeleteCleanupTestDB(t)
	gs := GroupService{}
	g := model.UserGroup{Name: "G1"}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundPolicyGroup{InboundId: 10, GroupId: g.Id}).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g.Id)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "del", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var cnt int64
	if err := db.Model(model.InboundPolicyGroup{}).Where("group_id = ?", g.Id).Count(&cnt).Error; err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected inbound policy refs removed, got %d", cnt)
	}
}

func TestDeleteGroupCleansEndpointL3MemberGroups(t *testing.T) {
	db := setupGroupDeleteCleanupTestDB(t)
	gs := GroupService{}
	g := model.UserGroup{Name: "G1"}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	opt, err := json.Marshal(map[string]interface{}{
		"member_group_ids": []interface{}{float64(g.Id)},
		"foo":              "bar",
	})
	if err != nil {
		t.Fatal(err)
	}
	ep := model.Endpoint{Type: l3RouterType, Tag: "ep1", Options: opt}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g.Id)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "del", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var ep2 model.Endpoint
	if err := db.First(&ep2, ep.Id).Error; err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(ep2.Options, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["member_group_ids"]; ok {
		t.Fatalf("expected member_group_ids removed when empty, got %v", m["member_group_ids"])
	}
}

func TestClientShouldHaveL3RouterUsesMemberLists(t *testing.T) {
	db := setupGroupDeleteCleanupTestDB(t)
	gs := GroupService{}
	g := model.UserGroup{Name: "SyncG"}
	cl := model.Client{Name: "c1", Enable: true}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: g.Id}).Error; err != nil {
		t.Fatal(err)
	}
	opt, _ := json.Marshal(map[string]interface{}{
		"member_group_ids": []interface{}{float64(g.Id)},
	})
	ep := model.Endpoint{Type: l3RouterType, Tag: "l3e", Options: opt}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	ok, err := gs.ClientShouldHaveL3Router(db, cl.Id)
	if err != nil || !ok {
		t.Fatalf("ClientShouldHaveL3Router want true, got %v err %v", ok, err)
	}
	opt2, _ := json.Marshal(map[string]interface{}{
		"member_client_ids": []interface{}{float64(cl.Id)},
	})
	ep2 := model.Endpoint{Type: l3RouterType, Tag: "l3e2", Options: opt2}
	if err := db.Create(&ep2).Error; err != nil {
		t.Fatal(err)
	}
	ok2, err := gs.ClientShouldHaveL3Router(db, cl.Id)
	if err != nil || !ok2 {
		t.Fatalf("member_client_ids: ClientShouldHaveL3Router want true, got %v err %v", ok2, err)
	}
}

func TestDeleteGroupCleansConfigSUIAuthGroups(t *testing.T) {
	db := setupGroupDeleteCleanupTestDB(t)
	gs := GroupService{}
	g := model.UserGroup{Name: "G1"}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "route": { "rules": [ { "s_ui_auth_groups": [1,2] } ] },
  "dns": { "rules": [ { "type": "logical", "rules": [ { "s_ui_auth_groups": [1] } ] } ] }
}`
	if err := db.Create(&model.Setting{Key: "config", Value: cfg}).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g.Id)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return gs.Save(tx, "del", raw)
	}); err != nil {
		t.Fatal(err)
	}
	var row model.Setting
	if err := db.Where("key = ?", "config").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(row.Value), &out); err != nil {
		t.Fatal(err)
	}
	route := out["route"].(map[string]interface{})
	rules := route["rules"].([]interface{})
	rule0 := rules[0].(map[string]interface{})
	got := rule0["s_ui_auth_groups"].([]interface{})
	if len(got) != 1 || int(got[0].(float64)) != 2 {
		t.Fatalf("expected route groups [2], got %v", got)
	}
	dns := out["dns"].(map[string]interface{})
	dnsRules := dns["rules"].([]interface{})
	logical := dnsRules[0].(map[string]interface{})
	nested := logical["rules"].([]interface{})
	n0 := nested[0].(map[string]interface{})
	if _, ok := n0["s_ui_auth_groups"]; ok {
		t.Fatalf("expected nested empty group list key removed, got %v", n0["s_ui_auth_groups"])
	}
}
