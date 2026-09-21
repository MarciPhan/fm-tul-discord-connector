package session

import (
	"net/http"
	"sync"
	"time"

	"sbibolet/internal/config"
	"sbibolet/internal/security"
	"sbibolet/internal/storage"
)

const (
	CookieName = "tul_session"
	Duration   = 15 * time.Minute
)

// Session uchovava bezpecnostni tokeny a stav pro OAuth2 flow
type Session struct {
	ID               string
	Student          *storage.Student
	MSALState        string
	MSALCodeVerifier string
	DiscordState     string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	Mux              sync.RWMutex
}

// GetStudent bezpečně vrátí studenta
func (s *Session) GetStudent() *storage.Student {
	s.Mux.RLock()
	defer s.Mux.RUnlock()
	return s.Student
}

// SetStudent bezpečně nastaví studenta
func (s *Session) SetStudent(student *storage.Student) {
	s.Mux.Lock()
	defer s.Mux.Unlock()
	s.Student = student
}

// SetMSALAuth atomicky nastaví state a PKCE verifier pro Microsoft OAuth
func (s *Session) SetMSALAuth(state, verifier string) {
	s.Mux.Lock()
	defer s.Mux.Unlock()
	s.MSALState = state
	s.MSALCodeVerifier = verifier
}

// SetDiscordState atomicky nastaví state pro Discord OAuth
func (s *Session) SetDiscordState(state string) {
	s.Mux.Lock()
	defer s.Mux.Unlock()
	s.DiscordState = state
}

// ConsumeMSALState atomicky ověří a spotřebuje MSAL state token (ochrana proti CSRF a replay útokům)
func (s *Session) ConsumeMSALState(expectedState string) (verifier string, ok bool) {
	s.Mux.Lock()
	defer s.Mux.Unlock()
	if expectedState == "" || s.MSALState == "" || !security.ConstantTimeCompare(expectedState, s.MSALState) {
		return "", false
	}
	v := s.MSALCodeVerifier
	s.MSALState = ""
	return v, true
}

// ConsumeDiscordState atomicky ověří a spotřebuje Discord state token (ochrana proti CSRF a replay útokům)
func (s *Session) ConsumeDiscordState(expectedState string) bool {
	s.Mux.Lock()
	defer s.Mux.Unlock()
	if expectedState == "" || s.DiscordState == "" || !security.ConstantTimeCompare(expectedState, s.DiscordState) {
		return false
	}
	s.DiscordState = ""
	return true
}

var (
	sessions = make(map[string]*Session)
	mu       sync.RWMutex
)

// GetOrCreate vytvori novou session nebo vrati existujici
func GetOrCreate(w http.ResponseWriter, r *http.Request) *Session {
	cookie, err := r.Cookie(CookieName)
	mu.Lock()
	defer mu.Unlock()

	now := time.Now()
	if err == nil && cookie.Value != "" {
		// Overeni podpisu cookie
		if id, ok := security.VerifySessionID(cookie.Value, config.Cfg.HMACSecret); ok {
			if s, exists := sessions[id]; exists {
				if now.Before(s.ExpiresAt) {
					s.ExpiresAt = now.Add(Duration) // Sliding window
					return s
				}
				// Session expirovala
				delete(sessions, id)
			}
		}
	}

	// Vytvorime novou session
	newID := security.GenerateID()
	newSession := &Session{
		ID:        newID,
		CreatedAt: now,
		ExpiresAt: now.Add(Duration),
	}
	sessions[newID] = newSession

	signedID := security.SignSessionID(newID, config.Cfg.HMACSecret)
	isSecure := config.Cfg.SecureCookies || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	
	c := &http.Cookie{
		Name:     CookieName,
		Value:    signedID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(Duration.Seconds()),
	}
	if config.Cfg.CookieDomain != "" {
		c.Domain = config.Cfg.CookieDomain
	}
	http.SetCookie(w, c)

	return newSession
}

// GetExisting vrati existujici session z cookie, pokud je platna
func GetExisting(r *http.Request) *Session {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	id, ok := security.VerifySessionID(cookie.Value, config.Cfg.HMACSecret)
	if !ok {
		return nil // Neplatny nebo falesny podpis
	}

	mu.RLock()
	defer mu.RUnlock()

	s, exists := sessions[id]
	if !exists || time.Now().After(s.ExpiresAt) {
		return nil
	}
	return s
}

// Clear smaze session cookie
func Clear(w http.ResponseWriter) {
	c := &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	}
	if config.Cfg.CookieDomain != "" {
		c.Domain = config.Cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

// CleanExpired odstrani expirovane sessions
func CleanExpired() {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	for id, s := range sessions {
		if now.After(s.ExpiresAt) {
			delete(sessions, id)
		}
	}
}

// StartCleaner spusti periodicky cistici goroutine
func StartCleaner() {
	ticker := time.NewTicker(2 * time.Minute)
	go func() {
		for range ticker.C {
			CleanExpired()
			security.CleanExpiredLimits()
		}
	}()
}

// InjectExpired vlozi expirovanou session (pouzivano v testech)
func InjectExpired(id string) {
	mu.Lock()
	defer mu.Unlock()
	sessions[id] = &Session{
		ID:        id,
		CreatedAt: time.Now().Add(-1 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
}
