package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateRandomBytes generuje kryptograficky bezpecne nahodne bajty
func GenerateRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("kriticke selhani crypto/rand: %v", err))
	}
	return b
}

// GenerateID generuje 32-znakove hex ID (128-bit entropie)
func GenerateID() string {
	return hex.EncodeToString(GenerateRandomBytes(16))
}

// GenerateStateToken generuje 64-znakovy hex CSRF state token (256-bit entropie)
func GenerateStateToken() string {
	return hex.EncodeToString(GenerateRandomBytes(32))
}

// GeneratePKCE vytvori code_verifier a code_challenge podle RFC 7636 (S256)
func GeneratePKCE() (verifier string, challenge string) {
	raw := GenerateRandomBytes(32)
	verifier = base64.RawURLEncoding.EncodeToString(raw)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// ConstantTimeCompare overi shodu retezcu bez moznosti timing-attacku
func ConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SignSessionID podepise session ID pomoci HMAC-SHA256 a vrati format id.signature
func SignSessionID(id, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%s", id, signature)
}

// VerifySessionID overi podpis session ID a vrati puvodni ID
func VerifySessionID(signedID, secret string) (string, bool) {
	parts := strings.Split(signedID, ".")
	if len(parts) != 2 {
		return "", false
	}
	id := parts[0]
	signature := parts[1]

	expectedMAC := hmac.New(sha256.New, []byte(secret))
	expectedMAC.Write([]byte(id))
	expectedSignature := base64.RawURLEncoding.EncodeToString(expectedMAC.Sum(nil))

	if ConstantTimeCompare(signature, expectedSignature) {
		return id, true
	}
	return "", false
}
