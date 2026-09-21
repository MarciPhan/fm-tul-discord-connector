package session

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sbibolet/internal/config"
	"sbibolet/internal/security"
)

func init() {
	config.Cfg.HMACSecret = "test-secret-key-for-hmac-tests"
}

func TestSessionHMACSignature(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	sess := GetOrCreate(w, req)
	if sess == nil {
		t.Fatal("Failed to create session")
	}

	cookieHeader := w.Header().Get("Set-Cookie")
	if cookieHeader == "" {
		t.Fatal("Set-Cookie header missing")
	}

	// Simulace klienta, ktery odesila zpet podepsanou cookie
	parts := strings.Split(cookieHeader, ";")
	cookieVal := strings.TrimPrefix(parts[0], CookieName+"=")

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: cookieVal})
	
	sess2 := GetExisting(req2)
	if sess2 == nil {
		t.Fatal("Validní HMAC podpis musí projít")
	}
	if sess2.ID != sess.ID {
		t.Errorf("Očekáváno ID %s, ale bylo vráceno %s", sess.ID, sess2.ID)
	}
}

func TestSessionHMACForgery(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	_ = GetOrCreate(w, req)
	
	// Útočník se pokusí změnit své session ID, ale ponechá původní podpis (nebo ho zkusí uhodnout)
	forgeryID := security.GenerateID()
	cookieParts := strings.Split(w.Header().Get("Set-Cookie"), ";")[0]
	val := strings.TrimPrefix(cookieParts, CookieName+"=")
	
	// Val obsahuje format id.signature
	sigParts := strings.Split(val, ".")
	if len(sigParts) != 2 {
		t.Fatal("Cookie není ve formátu id.signature")
	}
	
	// Útočník podvrhne cookie
	forgedCookie := forgeryID + "." + sigParts[1]
	
	reqForged := httptest.NewRequest("GET", "/", nil)
	reqForged.AddCookie(&http.Cookie{Name: CookieName, Value: forgedCookie})

	sessForged := GetExisting(reqForged)
	if sessForged != nil {
		t.Fatal("BEZPEČNOSTNÍ CHYBA: Podvržená cookie s neplatným HMAC podpisem byla přijata!")
	}
	
	// Změna tajemství
	config.Cfg.HMACSecret = "different-key"
	reqOriginal := httptest.NewRequest("GET", "/", nil)
	reqOriginal.AddCookie(&http.Cookie{Name: CookieName, Value: val})
	
	if sessOriginal := GetExisting(reqOriginal); sessOriginal != nil {
		t.Fatal("BEZPEČNOSTNÍ CHYBA: Cookie podepsaná starým klíčem byla přijata novým klíčem!")
	}
	
	// Obnova klíče
	config.Cfg.HMACSecret = "test-secret-key-for-hmac-tests"
}

func TestSessionDomain(t *testing.T) {
	config.Cfg.CookieDomain = "login.fm.tul.cz"
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	GetOrCreate(w, req)
	
	cookieHeader := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, "Domain=login.fm.tul.cz") {
		t.Errorf("Očekávána Cookie s doménou login.fm.tul.cz, přijato: %s", cookieHeader)
	}
	
	config.Cfg.CookieDomain = "" // Reset
}

func TestSessionSlidingExpiration(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	sess := GetOrCreate(w, req)
	origExp := sess.ExpiresAt
	
	time.Sleep(10 * time.Millisecond) // Malá pauza
	
	// Simulace přečtení existující cookie pro obnovu okna
	req2 := httptest.NewRequest("GET", "/", nil)
	cookieVal := strings.TrimPrefix(strings.Split(w.Header().Get("Set-Cookie"), ";")[0], CookieName+"=")
	req2.AddCookie(&http.Cookie{Name: CookieName, Value: cookieVal})
	
	w2 := httptest.NewRecorder()
	sess2 := GetOrCreate(w2, req2)
	
	if !sess2.ExpiresAt.After(origExp) {
		t.Fatal("Sliding expirace neprodloužila platnost session!")
	}
}
