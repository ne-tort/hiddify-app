package sub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alireza0/s-ui/database/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAWGConfTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Setting{},
		&model.Endpoint{},
		&model.Client{},
		&model.AwgObfuscationProfile{},
		&model.AwgObfuscationProfileClientMember{},
		&model.ClientGroupMember{},
		&model.UserGroup{},
		&model.GroupGroupMember{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAWGConfService_ListClientFiles_MultipleEndpoints(t *testing.T) {
	db := setupAWGConfTestDB(t)
	serverPriv1, _ := wgtypes.GeneratePrivateKey()
	serverPriv2, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	if err := db.Create(&model.Client{Id: 1, Name: "alice", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	makeEP := func(tag string, port int, priv string, addr string) model.Endpoint {
		opts := map[string]interface{}{
			"listen_port":       port,
			"private_key":       priv,
			"member_client_ids": []interface{}{float64(1)},
			"peers": []map[string]interface{}{
				{
					"client_id":   float64(1),
					"private_key": clientPriv.String(),
					"public_key":  clientPriv.PublicKey().String(),
					"allowed_ips": []string{addr},
				},
			},
		}
		raw, _ := json.Marshal(opts)
		return model.Endpoint{Type: "awg", Tag: tag, Options: raw}
	}
	ep1 := makeEP("awg-a", 30001, serverPriv1.String(), "10.5.0.2/32")
	ep2 := makeEP("awg-b", 30002, serverPriv2.String(), "10.6.0.2/32")
	if err := db.Create(&ep1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ep2).Error; err != nil {
		t.Fatal(err)
	}

	svc := AWGConfService{}
	files, err := svc.ListClientFiles(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestAWGConfService_BuildClientFile_MergesProfileAndInline(t *testing.T) {
	db := setupAWGConfTestDB(t)
	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	jc := 5
	jmin := 470
	h1 := "100-200"
	if err := db.Create(&model.AwgObfuscationProfile{
		Name: "p1", Enabled: true, Jc: &jc, Jmin: &jmin, H1: &h1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	if err := db.Create(&model.Client{Id: 2, Name: "bob", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"listen_port":                   30387,
		"private_key":                   serverPriv.String(),
		"persistent_keepalive_interval": 25,
		"member_client_ids":             []interface{}{float64(2)},
		"obfuscation_profile_id":        float64(1),
		"jc":                            float64(9),
		"i1":                            "<b 0x111><t>",
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(2),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"0.0.0.0/0"},
			},
		},
	}
	raw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-main", Options: raw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}

	svc := AWGConfService{}
	filename, payload, err := svc.BuildClientFile(db, 2, ep.Id, "193.233.216.26")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filename, ".conf") {
		t.Fatalf("unexpected filename: %s", filename)
	}
	text := string(payload)
	mustContain := []string{
		"[Interface]",
		"PrivateKey = " + clientPriv.String(),
		"Address = 0.0.0.0/0",
		"Jc = 9",
		"Jmin = 470",
		"H1 = 100-200",
		"I1 = <b 0x111><t>",
		"[Peer]",
		"PersistentKeepalive = 25",
		"Endpoint = 193.233.216.26:30387",
	}
	for _, m := range mustContain {
		if !strings.Contains(text, m) {
			t.Fatalf("expected %q in conf:\n%s", m, text)
		}
	}
}

func TestAWGConfService_BuildClientFile_NotMember(t *testing.T) {
	db := setupAWGConfTestDB(t)
	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	if err := db.Create(&model.Client{Id: 3, Name: "carol", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"listen_port":       30010,
		"private_key":       serverPriv.String(),
		"member_client_ids": []interface{}{float64(99)},
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(99),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.7.0.2/32"},
			},
		},
	}
	raw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-x", Options: raw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}

	svc := AWGConfService{}
	if _, _, err := svc.BuildClientFile(db, 3, ep.Id, "example.com"); err == nil {
		t.Fatal("expected error for unavailable endpoint")
	}
}

func TestAWGConfService_BuildClientFile_UsesRequestHostWhenSettingsUnavailable(t *testing.T) {
	db := setupAWGConfTestDB(t)
	serverPriv, _ := wgtypes.GeneratePrivateKey()
	clientPriv, _ := wgtypes.GeneratePrivateKey()
	cfg, _ := json.Marshal(map[string]interface{}{
		"wireguard": map[string]interface{}{
			"private_key": clientPriv.String(),
			"public_key":  clientPriv.PublicKey().String(),
		},
	})
	if err := db.Create(&model.Client{Id: 4, Name: "dave", Enable: true, Config: cfg, Inbounds: json.RawMessage(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{
		"listen_port":       30011,
		"private_key":       serverPriv.String(),
		"member_client_ids": []interface{}{float64(4)},
		"peers": []map[string]interface{}{
			{
				"client_id":   float64(4),
				"private_key": clientPriv.String(),
				"public_key":  clientPriv.PublicKey().String(),
				"allowed_ips": []string{"10.8.0.2/32"},
			},
		},
	}
	raw, _ := json.Marshal(opts)
	ep := model.Endpoint{Type: "awg", Tag: "awg-h", Options: raw}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	svc := AWGConfService{}
	_, payload, err := svc.BuildClientFile(db, 4, ep.Id, "sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "Endpoint = sub.example.com:30011") {
		t.Fatalf("expected request host in generated conf, got:\n%s", string(payload))
	}
}
