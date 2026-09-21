package security

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"sbibolet/internal/config"
)

// IPRateLimiter uchovava pocet pozadavku z dane IP adresy
type IPRateLimiter struct {
	requests int
	window   time.Time
}

var (
	ipLimits    = make(map[string]*IPRateLimiter)
	ipLimitsMux sync.Mutex
)

// GetClientIP extrahuje IP adresu klienta z HTTP pozadavku (reverse proxy aware)
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// CheckRateLimit overi, zda IP adresa neprekrocila limit pozadavku v danem okne
func CheckRateLimit(ip string, maxReqs int, window time.Duration) bool {
	if !config.Cfg.RateLimitEnabled {
		return true
	}
	ipLimitsMux.Lock()
	defer ipLimitsMux.Unlock()

	lim, exists := ipLimits[ip]
	now := time.Now()
	if !exists || now.Sub(lim.window) > window {
		ipLimits[ip] = &IPRateLimiter{requests: 1, window: now}
		return true
	}

	lim.requests++
	return lim.requests <= maxReqs
}

// CleanExpiredLimits odstrani stare zaznamy z rate limiteru
func CleanExpiredLimits() {
	ipLimitsMux.Lock()
	defer ipLimitsMux.Unlock()
	now := time.Now()
	for ip, lim := range ipLimits {
		if now.Sub(lim.window) > 10*time.Minute {
			delete(ipLimits, ip)
		}
	}
}
