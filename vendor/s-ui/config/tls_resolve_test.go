package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWebTLSPaths_DBWins(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "c.pem")
	key := filepath.Join(dir, "k.pem")
	_ = os.WriteFile(cert, []byte("c"), 0o600)
	_ = os.WriteFile(key, []byte("k"), 0o600)
	c, k := ResolveWebTLSPaths(cert, key)
	if c != cert || k != key {
		t.Fatalf("expected db paths, got %q %q", c, k)
	}
}

func TestResolveWebTLSPaths_EnvWhenDBBroken(t *testing.T) {
	dir := t.TempDir()
	ec := filepath.Join(dir, "ec.pem")
	ek := filepath.Join(dir, "ek.pem")
	_ = os.WriteFile(ec, []byte("c"), 0o600)
	_ = os.WriteFile(ek, []byte("k"), 0o600)
	t.Setenv("SUI_WEB_TLS_CERT", ec)
	t.Setenv("SUI_WEB_TLS_KEY", ek)
	c, k := ResolveWebTLSPaths("/no/such/cert", "/no/such/key")
	if c != ec || k != ek {
		t.Fatalf("expected env paths, got %q %q", c, k)
	}
}

func TestResolveSubTLSPaths_ReusesWeb(t *testing.T) {
	dir := t.TempDir()
	wc := filepath.Join(dir, "w.pem")
	wk := filepath.Join(dir, "wk.pem")
	_ = os.WriteFile(wc, []byte("c"), 0o600)
	_ = os.WriteFile(wk, []byte("k"), 0o600)
	sc, sk := ResolveSubTLSPaths("", "", wc, wk)
	if sc != wc || sk != wk {
		t.Fatalf("expected web pair, got %q %q", sc, sk)
	}
}
