package security

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"
)

// Middleware pridava striktni HTTP bezpecnostni hlavicky a rate limiting,
// a omezuje velikost tela pozadavku proti DoS.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Brutal Hardening: Limit velikosti těla požadavku (max 1 MB) pro prevenci Memory Exhaustion DoS
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		// Striktní Origin Validace (Ochrana proti Cross-Site Request Forgery nad rámec state tokenu)
		origin := r.Header.Get("Origin")
		if origin != "" && config.Cfg.BaseURL != "" {
			expectedOrigin := strings.TrimRight(config.Cfg.BaseURL, "/")
			if u, err := url.Parse(config.Cfg.BaseURL); err == nil && u.Host != "" {
				expectedOrigin = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
			}
			// Origin musí odpovídat naší BaseURL
			if !strings.EqualFold(origin, expectedOrigin) {
				audit.Log(audit.LevelDanger, "Zablokován CSRF útok", fmt.Sprintf("Neplatná Origin hlavička: `%s`", origin))
				http.Error(w, "Forbidden: Invalid Origin", http.StatusForbidden)
				return
			}
		}

		ip := GetClientIP(r)
		// Max 60 pozadavku za minutu na IP pro cele rozhrani
		if !CheckRateLimit(ip, 60, time.Minute) {
			audit.Log(audit.LevelWarning, "Rate Limit překročen", fmt.Sprintf("IP adresa `%s` odeslala příliš mnoho požadavků.", ip))
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Příliš mnoho požadavků (Rate limit překročen). Zkuste to za chvíli.", http.StatusTooManyRequests)
			return
		}

		// Striktni bezpecnostni hlavicky (Defense-in-Depth)
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")

		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || config.Cfg.SecureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}
