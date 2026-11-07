// Package msalauth vytvari client assertion (podepsany JWT) pro prihlaseni
// aplikace k Microsoft Entra ID privatnim klicem misto client secretu.
package msalauth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// AssertionType je hodnota client_assertion_type z RFC 7523
	AssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	assertionTTL  = 5 * time.Minute
)

// Signer podepisuje client assertion privatnim klicem aplikace
type Signer struct {
	key      *rsa.PrivateKey
	x5t      string // x5t (SHA-1), base64url
	x5tS256  string // x5t#S256 (SHA-256), base64url
	clientID string
}

// NewSigner nacte privatni klic (PEM, PKCS#8 nebo PKCS#1) a odtud zjisti otisk certifikatu.
// Otisk se bere z verejneho certifikatu (certPath); pokud neni zadan, pouzije se
// thumbprint (hex, SHA-1 jak ho ukazuje Entra portal, nebo SHA-256).
func NewSigner(clientID, keyPath, certPath, thumbprint string) (*Signer, error) {
	if clientID == "" || keyPath == "" {
		return nil, errors.New("chybí MSAL_CLIENT_ID nebo MSAL_CLIENT_KEY_PATH")
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("čtení privátního klíče: %w", err)
	}
	key, err := parseKey(keyPEM)
	if err != nil {
		return nil, err
	}
	s := &Signer{key: key, clientID: clientID}

	switch {
	case certPath != "":
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("čtení certifikátu: %w", err)
		}
		block, _ := pem.Decode(certPEM)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("MSAL_CLIENT_CERT_PATH neobsahuje PEM certifikát")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsování certifikátu: %w", err)
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok || pub.N.Cmp(key.N) != 0 || pub.E != key.E {
			return nil, errors.New("certifikát nepatří k privátnímu klíči")
		}
		s1 := sha1.Sum(cert.Raw)
		s256 := sha256.Sum256(cert.Raw)
		s.x5t = base64.RawURLEncoding.EncodeToString(s1[:])
		s.x5tS256 = base64.RawURLEncoding.EncodeToString(s256[:])
	case thumbprint != "":
		raw, err := hex.DecodeString(strings.ReplaceAll(strings.ReplaceAll(thumbprint, ":", ""), " ", ""))
		if err != nil {
			return nil, fmt.Errorf("MSAL_CLIENT_CERT_THUMBPRINT není hex: %w", err)
		}
		switch len(raw) {
		case sha1.Size:
			s.x5t = base64.RawURLEncoding.EncodeToString(raw)
		case sha256.Size:
			s.x5tS256 = base64.RawURLEncoding.EncodeToString(raw)
		default:
			return nil, errors.New("MSAL_CLIENT_CERT_THUMBPRINT musí být SHA-1 (40 hex) nebo SHA-256 (64 hex)")
		}
	default:
		return nil, errors.New("zadejte MSAL_CLIENT_CERT_PATH nebo MSAL_CLIENT_CERT_THUMBPRINT")
	}
	return s, nil
}

func parseKey(data []byte) (*rsa.PrivateKey, error) {
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil, errors.New("MSAL_CLIENT_KEY_PATH neobsahuje PEM privátní klíč (RSA)")
		}
		switch block.Type {
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsování klíče: %w", err)
			}
			rk, ok := k.(*rsa.PrivateKey)
			if !ok {
				return nil, errors.New("podporován je pouze RSA klíč")
			}
			return rk, nil
		case "RSA PRIVATE KEY":
			rk, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsování klíče: %w", err)
			}
			return rk, nil
		}
	}
}

// Assertion vytvori novy podepsany JWT pro dany token endpoint (audience)
func (s *Signer) Assertion(tokenURL string) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if s.x5tS256 != "" {
		header["x5t#S256"] = s.x5tS256
	} else {
		header["x5t"] = s.x5t
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	now := time.Now()
	claims := map[string]any{
		"aud": tokenURL,
		"iss": s.clientID,
		"sub": s.clientID,
		"jti": hex.EncodeToString(jti),
		"nbf": now.Add(-time.Minute).Unix(),
		"exp": now.Add(assertionTTL).Unix(),
	}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
