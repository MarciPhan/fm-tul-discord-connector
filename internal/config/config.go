package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config nacteny z prostredi (.env)
type Config struct {
	// Microsoft OAuth2 (TUL Entra ID)
	MSALClientID     string
	MSALClientSecret string
	MSALTenantID     string

	// Discord OAuth2 & Bot
	DiscordClientID        string
	DiscordClientSecret    string
	DiscordToken           string
	DiscordGuildID         string
	DiscordVerifiedID      string
	DiscordFMStudentID     string
	DiscordFMStaffID       string
	DiscordInviteURL       string
	DiscordVerifyEmoji     string
	DiscordVerifyMessageID string
	DiscordAuditChannelID  string

	// Welcome zpravy
	DiscordWelcomeChannelID string
	DiscordWelcomeMessage   string

	// RSS
	RSSEnabled      bool
	RSSCheckInterval time.Duration

	// Web server
	Host         string
	Port         string
	BaseURL      string
	CookieDomain string

	// Bezpecnostni nastaveni
	HMACSecret       string
	EnableDevMock    bool
	SecureCookies    bool
	RateLimitEnabled bool
}

// Cfg je globalni konfigurace aplikace
var Cfg Config

// Load nacte konfiguraci z .env a prostredi
func Load() {
	_ = godotenv.Load()

	msClientID := getEnvFallback("MSAL_CLIENT_ID", "MICROSOFT_CLIENT_ID")
	msClientSecret := getEnvFallback("MSAL_CLIENT_SECRET", "MICROSOFT_CLIENT_SECRET")
	msTenantID := getEnvFallback("MSAL_TENANT_ID", "MICROSOFT_TENANT_ID")
	discordGuildID := getEnvFallback("DISCORD_GUILD_ID", "GUILD_ID")
	discordFMStudentID := getEnvFallback("DISCORD_FM_STUDENT_ID", "FM_STUDENT_ROLE_ID")
	discordFMStaffID := getEnvFallback("DISCORD_FM_STAFF_ID", "FM_STAFF_ROLE_ID")

	port := getEnvOr("PORT", "8000")
	host := getEnvOr("HOST", "0.0.0.0")
	baseURL := getEnvOr("BASE_URL", fmt.Sprintf("http://localhost:%s", port))

	rssInterval := 30 * time.Minute // Výchozí interval

	Cfg = Config{
		MSALClientID:     msClientID,
		MSALClientSecret: msClientSecret,
		MSALTenantID:     msTenantID,

		DiscordClientID:        os.Getenv("DISCORD_CLIENT_ID"),
		DiscordClientSecret:    os.Getenv("DISCORD_CLIENT_SECRET"),
		DiscordToken:           os.Getenv("DISCORD_TOKEN"),
		DiscordGuildID:         discordGuildID,
		DiscordVerifiedID:      os.Getenv("DISCORD_VERIFIED_ID"),
		DiscordFMStudentID:     discordFMStudentID,
		DiscordFMStaffID:       discordFMStaffID,
		DiscordInviteURL:       os.Getenv("DISCORD_INVITE_URL"),
		DiscordVerifyEmoji:     os.Getenv("DISCORD_VERIFY_EMOJI"),
		DiscordVerifyMessageID: os.Getenv("DISCORD_VERIFY_MESSAGE_ID"),
		DiscordAuditChannelID:  os.Getenv("DISCORD_AUDIT_CHANNEL_ID"),

		DiscordWelcomeChannelID: os.Getenv("DISCORD_WELCOME_CHANNEL_ID"),
		DiscordWelcomeMessage:   getEnvOr("DISCORD_WELCOME_MESSAGE", "Vítej na serveru FM TUL! Pro ověření identity klikni na reakci v kanálu #overeni."),

		RSSEnabled:       strings.ToLower(os.Getenv("RSS_ENABLED")) == "true",
		RSSCheckInterval: rssInterval,

		Host:         host,
		Port:         port,
		BaseURL:      baseURL,
		CookieDomain: os.Getenv("COOKIE_DOMAIN"),

		HMACSecret:       getEnvOr("HMAC_SECRET", "super-secret-fallback-key-change-in-production"),
		EnableDevMock:    strings.ToLower(os.Getenv("ENABLE_DEV_MOCK")) == "true",
		SecureCookies:    strings.ToLower(os.Getenv("SECURE_COOKIES")) == "true",
		RateLimitEnabled: os.Getenv("RATE_LIMIT_ENABLED") != "false",
	}

	if Cfg.EnableDevMock {
		log.Println("UPOZORNĚNÍ: ENABLE_DEV_MOCK=true – simulované endpointy jsou povoleny. V produkci nastavte na false!")
	} else {
		log.Println("Bezpečnostní režim: Dev mock endpointy jsou zakázány.")
	}
}

// getEnvOr vrati hodnotu promenne prostredi nebo fallback
func getEnvOr(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

// getEnvFallback zkusi primarni klic, pak sekundarni
func getEnvFallback(primary, secondary string) string {
	val := os.Getenv(primary)
	if val == "" {
		val = os.Getenv(secondary)
	}
	return val
}
