package security

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sbibolet/internal/config"
)

func TestOriginValidation(t *testing.T) {
	config.Cfg.BaseURL = "https://login.fm.tul.cz"
	
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := Middleware(dummyHandler)

	// Platný Origin
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set("Origin", "https://login.fm.tul.cz")
	w1 := httptest.NewRecorder()
	middleware.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("Očekáváno 200 OK pro platný Origin, získáno: %d", w1.Code)
	}

	// Neplatný Origin (CSRF pokus z jiné stránky)
	req2 := httptest.NewRequest("POST", "/msal", nil)
	req2.Header.Set("Origin", "https://evil.com")
	w2 := httptest.NewRecorder()
	middleware.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("Očekáváno 403 Forbidden pro neplatný Origin, získáno: %d", w2.Code)
	}
	
	// Bez Originu (přímý přístup) by mělo projít (např. OAuth redirect z Microsoftu neposílá Origin)
	req3 := httptest.NewRequest("GET", "/msal", nil)
	w3 := httptest.NewRecorder()
	middleware.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("Očekáváno 200 OK pro požadavek bez Originu, získáno: %d", w3.Code)
	}
}

func TestPayloadSizeLimit(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Čtení těla požadavku, což by mělo spustit MaxBytesReader ochranu
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			http.Error(w, "Payload Too Large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	middleware := Middleware(dummyHandler)

	// Payload > 1 MB
	hugePayload := make([]byte, (1<<20)+10)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(hugePayload))
	w := httptest.NewRecorder()
	
	middleware.ServeHTTP(w, req)
	
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Očekáváno 413 Payload Too Large (Memory Exhaustion DoS obrana), získáno: %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := Middleware(dummyHandler)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	
	headers := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for key, expected := range headers {
		if val := w.Header().Get(key); val != expected {
			t.Errorf("Chybí nebo je chybná hlavička %s: %s (Očekáváno: %s)", key, val, expected)
		}
	}
	
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Error("Chybí striktní Content-Security-Policy")
	}
}

func TestOriginTrailingSlash(t *testing.T) {
	// BaseURL s lomítkem na konci nesmí rozbít legitimní Origin bez lomítka
	config.Cfg.BaseURL = "https://login.fm.tul.cz/"

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := Middleware(dummyHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://login.fm.tul.cz")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Očekáváno 200 OK pro normalizovaný Origin, získáno: %d", w.Code)
	}
}

func TestGetClientIPValidation(t *testing.T) {
	// 1. Platná IP v X-Forwarded-For
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set("X-Forwarded-For", "195.113.120.5, 10.0.0.1")
	if ip := GetClientIP(req1); ip != "195.113.120.5" {
		t.Errorf("Očekávána IP 195.113.120.5, získáno: %s", ip)
	}

	// 2. Podvržený neplatný řetězec v X-Forwarded-For nesmí projít jako IP
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "192.0.2.1:12345"
	req2.Header.Set("X-Forwarded-For", "evil-spoofed-header<script>")
	if ip := GetClientIP(req2); ip != "192.0.2.1" {
		t.Errorf("Očekáván fallback na RemoteAddr 192.0.2.1, získáno: %s", ip)
	}

	// 3. X-Real-IP s platnou IP
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("X-Real-IP", "147.230.1.1")
	if ip := GetClientIP(req3); ip != "147.230.1.1" {
		t.Errorf("Očekávána IP 147.230.1.1, získáno: %s", ip)
	}
}
