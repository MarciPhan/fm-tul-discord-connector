package msalauth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeKeyPair(t *testing.T) (keyPath, certPath string, key *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath, certPath = filepath.Join(dir, "k.pem"), filepath.Join(dir, "c.pem")
	pk, _ := x509.MarshalPKCS8PrivateKey(key)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600)
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	return
}

func TestAssertionSignedAndClaims(t *testing.T) {
	keyPath, certPath, key := writeKeyPair(t)
	s, err := NewSigner("client-1", keyPath, certPath, "")
	if err != nil {
		t.Fatal(err)
	}
	const aud = "https://login.microsoftonline.com/t/oauth2/v2.0/token"
	jwt, err := s.Assertion(aud)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("očekávány 3 části, je %d", len(parts))
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("neplatný podpis: %v", err)
	}
	var h map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	json.Unmarshal(hb, &h)
	if h["alg"] != "RS256" || h["x5t#S256"] == "" {
		t.Fatalf("špatná hlavička: %v", h)
	}
	var c map[string]any
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(cb, &c)
	if c["aud"] != aud || c["iss"] != "client-1" || c["sub"] != "client-1" || c["jti"] == "" {
		t.Fatalf("špatné claimy: %v", c)
	}
}

func TestMismatchedCertRejected(t *testing.T) {
	keyPath, _, _ := writeKeyPair(t)
	_, otherCert, _ := writeKeyPair(t)
	if _, err := NewSigner("c", keyPath, otherCert, ""); err == nil {
		t.Fatal("certifikát od jiného klíče měl být odmítnut")
	}
}

func TestThumbprintFallback(t *testing.T) {
	keyPath, _, _ := writeKeyPair(t)
	s, err := NewSigner("c", keyPath, "", strings.Repeat("ab", 20))
	if err != nil {
		t.Fatal(err)
	}
	if s.x5t == "" {
		t.Fatal("x5t chybí")
	}
}
