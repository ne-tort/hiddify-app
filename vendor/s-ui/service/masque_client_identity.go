package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"time"

	"github.com/alireza0/s-ui/database/model"
)

const masqueSuiIdentityKey = "sui_masque"

// EnsureMasqueClientIdentity generates a panel-local TLS leaf for generic MASQUE client mTLS
// (SPKI pin + PEM material in client.Config[masqueSuiIdentityKey]) when not already present.
func EnsureMasqueClientIdentity(client *model.Client) error {
	if client == nil {
		return nil
	}
	var root map[string]interface{}
	if len(client.Config) > 0 {
		if err := json.Unmarshal(client.Config, &root); err != nil {
			return err
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}
	if sm, ok := root[masqueSuiIdentityKey].(map[string]interface{}); ok && sm != nil {
		if s, ok := sm["client_leaf_spki_sha256"].(string); ok && strings.TrimSpace(s) != "" {
			return nil
		}
	}
	certPEM, keyPEM, spkiHex, err := generateMasqueClientLeaf()
	if err != nil {
		return err
	}
	root[masqueSuiIdentityKey] = map[string]interface{}{
		"client_certificate_pem":  certPEM,
		"client_private_key_pem":  keyPEM,
		"client_leaf_spki_sha256": spkiHex,
	}
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	client.Config = raw
	return nil
}

func generateMasqueClientLeaf() (certPEM string, keyPEM string, spkiHex string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", "", err
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "s-ui-masque-client",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, priv.Public(), priv)
	if err != nil {
		return "", "", "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "", "", "", err
	}
	spki := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	spkiHex = hex.EncodeToString(spki[:])

	var certBuf strings.Builder
	_ = pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", "", err
	}
	var keyBuf strings.Builder
	_ = pem.Encode(&keyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certBuf.String(), keyBuf.String(), spkiHex, nil
}

// MasqueClientLeafSPKIHex returns stored SPKI hex for MASQUE client identity, or empty.
func MasqueClientLeafSPKIHex(client *model.Client) string {
	if client == nil || len(client.Config) == 0 {
		return ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(client.Config, &root); err != nil {
		return ""
	}
	sm, ok := root[masqueSuiIdentityKey].(map[string]interface{})
	if !ok || sm == nil {
		return ""
	}
	s, _ := sm["client_leaf_spki_sha256"].(string)
	return strings.TrimSpace(s)
}
