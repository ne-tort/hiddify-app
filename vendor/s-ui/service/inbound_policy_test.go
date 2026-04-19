package service

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupInboundPolicyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(
		&model.Inbound{},
		&model.Client{},
		&model.InboundUserPolicy{},
		&model.InboundPolicyGroup{},
		&model.InboundPolicyClient{},
		&model.UserGroup{},
		&model.GroupGroupMember{},
		&model.ClientGroupMember{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestResolvePolicyClientIDs_ModeAliasGroup(t *testing.T) {
	db := setupInboundPolicyTestDB(t)
	is := &InboundService{}
	in := model.Inbound{Type: "vless", Tag: "v-alias"}
	if err := db.Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	g := model.UserGroup{Name: "G"}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "u", Enable: true, Inbounds: []byte("[]"), Config: []byte("{}"), Links: []byte("[]")}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: g.Id}).Error; err != nil {
		t.Fatal(err)
	}
	// Legacy/bug: mode stored as "group" instead of "groups"
	if err := db.Create(&model.InboundUserPolicy{InboundId: in.Id, Mode: "group"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundPolicyGroup{InboundId: in.Id, GroupId: g.Id}).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := is.ResolvePolicyClientIDs(db, in.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("got %v want [%d]", ids, cl.Id)
	}
}

func TestResolvePolicyClientIDs_All(t *testing.T) {
	db := setupInboundPolicyTestDB(t)
	is := &InboundService{}
	in := model.Inbound{Type: "vless", Tag: "v1"}
	if err := db.Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundUserPolicy{InboundId: in.Id, Mode: InboundPolicyAll}).Error; err != nil {
		t.Fatal(err)
	}
	c1 := model.Client{Name: "a", Enable: true, Inbounds: []byte("[]"), Config: []byte("{}"), Links: []byte("[]")}
	c2 := model.Client{Name: "b", Enable: false, Inbounds: []byte("[]"), Config: []byte("{}"), Links: []byte("[]")}
	if err := db.Create(&c1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&c2).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := is.ResolvePolicyClientIDs(db, in.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != c1.Id {
		t.Fatalf("got %v want [%d]", ids, c1.Id)
	}
}

func TestResolvePolicyClientIDs_GroupsNested(t *testing.T) {
	db := setupInboundPolicyTestDB(t)
	is := &InboundService{}

	parent := model.UserGroup{Name: "P"}
	child := model.UserGroup{Name: "C"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GroupGroupMember{ParentGroupId: parent.Id, ChildGroupId: child.Id}).Error; err != nil {
		t.Fatal(err)
	}
	cl := model.Client{Name: "u", Enable: true, Inbounds: []byte("[]"), Config: []byte("{}"), Links: []byte("[]")}
	if err := db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroupMember{ClientId: cl.Id, GroupId: child.Id}).Error; err != nil {
		t.Fatal(err)
	}

	in := model.Inbound{Type: "vless", Tag: "v2"}
	if err := db.Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundUserPolicy{InboundId: in.Id, Mode: InboundPolicyGroups}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundPolicyGroup{InboundId: in.Id, GroupId: parent.Id}).Error; err != nil {
		t.Fatal(err)
	}

	ids, err := is.ResolvePolicyClientIDs(db, in.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != cl.Id {
		t.Fatalf("got %v want [%d]", ids, cl.Id)
	}
}

func TestReconcileInboundClients_AddRemove(t *testing.T) {
	db := setupInboundPolicyTestDB(t)
	is := &InboundService{}
	in := model.Inbound{Type: "vless", Tag: "v3", TlsId: 0, Addrs: []byte("[]"), OutJson: []byte("{}"), Options: []byte(`{"listen":"::","listen_port":443}`)}
	if err := db.Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	c1 := model.Client{Name: "x", Enable: true, Inbounds: []byte("[]"), Config: []byte(`{"vless":{}}`), Links: []byte("[]")}
	c2 := model.Client{Name: "y", Enable: true, Inbounds: []byte("[]"), Config: []byte(`{"vless":{}}`), Links: []byte("[]")}
	if err := db.Create(&c1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&c2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundUserPolicy{InboundId: in.Id, Mode: InboundPolicyClients}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundPolicyClient{InboundId: in.Id, ClientId: c1.Id}).Error; err != nil {
		t.Fatal(err)
	}
	if err := is.ReconcileInboundClients(db, in.Id, "example.com"); err != nil {
		t.Fatal(err)
	}
	var c1db model.Client
	if err := db.Where("id = ?", c1.Id).First(&c1db).Error; err != nil {
		t.Fatal(err)
	}
	var ib []uint
	if err := json.Unmarshal(c1db.Inbounds, &ib); err != nil {
		t.Fatal(err)
	}
	if len(ib) != 1 || ib[0] != in.Id {
		t.Fatalf("client1 inbounds %v", ib)
	}

	if err := db.Where("inbound_id = ?", in.Id).Delete(&model.InboundPolicyClient{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundPolicyClient{InboundId: in.Id, ClientId: c2.Id}).Error; err != nil {
		t.Fatal(err)
	}
	if err := is.ReconcileInboundClients(db, in.Id, "example.com"); err != nil {
		t.Fatal(err)
	}
	_ = db.Where("id = ?", c1.Id).First(&c1).Error
	var ib1 []uint
	_ = json.Unmarshal(c1.Inbounds, &ib1)
	if len(ib1) != 0 {
		t.Fatalf("expected c1 cleared, got %v", ib1)
	}
}
