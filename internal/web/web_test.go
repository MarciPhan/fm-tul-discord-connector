package web

import (
	"net/http"
	"net/http/httptest"
	"sbibolet/internal/config"
	"strings"
	"sync"
	"testing"
	"time"

	"sbibolet/internal/session"
	"sbibolet/internal/storage"
)

func TestIsValidDiscordInvite(t *testing.T) {
	cases := []struct {
		url   string
		valid bool
	}{
		{"https://discord.gg/fm-tul", true},
		{"https://discord.com/invite/abc123xyz", true},
		{"https://discord.gg/vase-pozvanka", false}, // placeholder
		{"http://discord.gg/fm-tul", false},         // must be https
		{"https://evil-site.com/?discord.gg", false},
		{"javascript:alert(1)", false},
		{"", false},
	}

	for _, tc := range cases {
		res := isValidDiscordInvite(tc.url)
		if res != tc.valid {
			t.Errorf("Pro URL '%s' očekáváno valid=%v, ale získáno: %v", tc.url, tc.valid, res)
		}
	}
}

func TestConcurrentSessionAccess(t *testing.T) {
	sess := &session.Session{
		ID:        "test-concurrent-sess",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}

	var wg sync.WaitGroup
	workers := 20

	// Gorutiny zapisující studenta
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess.SetStudent(&storage.Student{
				MicrosoftID: "ms-123",
				Name:        "Test Student",
				Email:       "test@tul.cz",
			})
			_ = sess.GetStudent()
		}(i)
	}

	// Gorutiny pracující se state tokeny
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess.SetMSALAuth("state123", "verifier456")
			_, _ = sess.ConsumeMSALState("state123")

			sess.SetDiscordState("dstate789")
			_ = sess.ConsumeDiscordState("dstate789")
		}(i)
	}

	wg.Wait()
}

func TestIndexOnlyMatchesRootAndKeepsState(t *testing.T) {
	config.Cfg.HMACSecret = "test-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", HandleIndex)
	mux.HandleFunc("GET /go", HandleStart)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	cookies := rec.Result().Cookies()
	if rec.Code != 200 || len(cookies) == 0 {
		t.Fatalf("index: code=%d cookies=%d", rec.Code, len(cookies))
	}
	if !strings.Contains(rec.Body.String(), `href="/go"`) {
		t.Fatal("uvodni stranka ma vest na rychly vstup /go")
	}

	// /favicon.ico nesmi spustit HandleIndex
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	req.AddCookie(cookies[0])
	mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("favicon: code=%d, ocekavano 404", rec.Code)
	}

	// opakovany vstup pres /go zachova stejny OAuth state (druha zalozka nezneplatni prvni)
	var first string
	for i := 0; i < 2; i++ {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/go", nil)
		req.AddCookie(cookies[0])
		mux.ServeHTTP(rec, req)
		loc := rec.Header().Get("Location")
		if rec.Code != http.StatusFound || !strings.HasPrefix(loc, "https://login.microsoftonline.com/") {
			t.Fatalf("/go: code=%d location=%q", rec.Code, loc)
		}
		if i == 0 {
			first = loc
		} else if loc != first {
			t.Fatalf("druhy vstup pres /go zmenil OAuth state:\n%q\n%q", first, loc)
		}
	}
}
