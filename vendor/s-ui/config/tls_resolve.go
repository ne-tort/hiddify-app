package config

import (
	"os"
	"strings"
)

const (
	// DefaultFallbackTLSCert is the last-resort path when DB and env do not yield valid files (e.g. docker bind mount).
	DefaultFallbackTLSCert = "/app/cert/fullchain.pem"
	// DefaultFallbackTLSKey pairs with DefaultFallbackTLSCert.
	DefaultFallbackTLSKey = "/app/cert/privkey.pem"
)

// tlsPairOK returns true if both paths are non-empty and both files exist.
func tlsPairOK(certPath, keyPath string) bool {
	certPath = strings.TrimSpace(certPath)
	keyPath = strings.TrimSpace(keyPath)
	if certPath == "" || keyPath == "" {
		return false
	}
	if _, err := os.Stat(certPath); err != nil {
		return false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}
	return true
}

// ResolveWebTLSPaths resolves panel TLS file paths:
//  1) DB paths when both exist on disk
//  2) SUI_WEB_TLS_CERT + SUI_WEB_TLS_KEY when both exist
//  3) SUI_TLS_FALLBACK_CERT + SUI_TLS_FALLBACK_KEY, or DefaultFallbackTLSCert/DefaultFallbackTLSKey, when both exist
// Returns "", "" if TLS should not be used.
func ResolveWebTLSPaths(dbCert, dbKey string) (cert, key string) {
	if tlsPairOK(dbCert, dbKey) {
		return strings.TrimSpace(dbCert), strings.TrimSpace(dbKey)
	}
	ec := strings.TrimSpace(os.Getenv("SUI_WEB_TLS_CERT"))
	ek := strings.TrimSpace(os.Getenv("SUI_WEB_TLS_KEY"))
	if tlsPairOK(ec, ek) {
		return ec, ek
	}
	fc := strings.TrimSpace(os.Getenv("SUI_TLS_FALLBACK_CERT"))
	fk := strings.TrimSpace(os.Getenv("SUI_TLS_FALLBACK_KEY"))
	if fc == "" {
		fc = DefaultFallbackTLSCert
	}
	if fk == "" {
		fk = DefaultFallbackTLSKey
	}
	if tlsPairOK(fc, fk) {
		return fc, fk
	}
	return "", ""
}

// ResolveSubTLSPaths resolves subscription server TLS paths:
//  1) DB sub paths when both exist
//  2) SUI_SUB_TLS_CERT + SUI_SUB_TLS_KEY when both exist
//  3) Reuse webCert/webKey (already fully resolved, including env/fallback) when both exist
func ResolveSubTLSPaths(dbCert, dbKey string, webCert, webKey string) (cert, key string) {
	if tlsPairOK(dbCert, dbKey) {
		return strings.TrimSpace(dbCert), strings.TrimSpace(dbKey)
	}
	sc := strings.TrimSpace(os.Getenv("SUI_SUB_TLS_CERT"))
	sk := strings.TrimSpace(os.Getenv("SUI_SUB_TLS_KEY"))
	if tlsPairOK(sc, sk) {
		return sc, sk
	}
	if tlsPairOK(webCert, webKey) {
		return webCert, webKey
	}
	return "", ""
}
