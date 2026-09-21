package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

const (
	SessionCookieName = "tul_session"
	SessionDuration   = 15 * time.Minute
)

var UsersStorageFile = "users.json"

// Config nacteny z prostredi (.env)
type Config struct {
	// Microsoft OAuth2
	MSALClientID     string
	MSALClientSecret string
	MSALTenantID     string

	// Discord OAuth2 & Bot
	DiscordClientID     string
	DiscordClientSecret string
	DiscordToken        string
	DiscordGuildID      string
	DiscordVerifiedID   string
	DiscordFMStudentID  string
	DiscordFMStaffID    string
	DiscordInviteURL    string
	DiscordVerifyEmoji  string
	DiscordVerifyMessageID string

	// Web server
	Host    string
	Port    string
	BaseURL string

	// Bezpecnostni nastaveni
	EnableDevMock    bool
	SecureCookies    bool
	RateLimitEnabled bool
}

// Student reprezentuje overeneho studenta ci zamestnance
type Student struct {
	MicrosoftID string    `json:"microsoft_id"`
	DiscordID   string    `json:"discord_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Faculty     string    `json:"faculty"`
	Role        string    `json:"role"` // "Student FM" nebo "Zaměstnanec FM"
	VerifiedAt  time.Time `json:"verified_at"`
}

// Session uchovava bezpecnostni tokeny a stav pro OAuth2 flow (CSRF ochrana + PKCE)
type Session struct {
	ID               string
	Student          *Student
	MSALState        string
	MSALCodeVerifier string
	DiscordState     string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// RateLimiter uchovava pocet pozadavku z dane IP adresy
type IPRateLimiter struct {
	requests int
	window   time.Time
}

// Globalni stavove promenne
var (
	cfg Config
	dg  *discordgo.Session

	sessions    = make(map[string]*Session)
	sessionsMux sync.RWMutex

	usersDB    = make(map[string]*Student) // indexovano podle DiscordID
	usersDBMux sync.RWMutex

	ipLimits    = make(map[string]*IPRateLimiter)
	ipLimitsMux sync.Mutex

	reactionCooldowns    = make(map[string]time.Time)
	reactionCooldownsMux sync.Mutex
)

// --- KRYPTOGRAFIE A BEZPECNOSTNI POMOCNICI ---

func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("kriticke selhani crypto/rand: %v", err))
	}
	return b
}

func generateID() string {
	return hex.EncodeToString(generateRandomBytes(16))
}

func generateStateToken() string {
	return hex.EncodeToString(generateRandomBytes(32))
}

// generatePKCE vytvori code_verifier a code_challenge podle RFC 7636 (S256)
func generatePKCE() (verifier string, challenge string) {
	raw := generateRandomBytes(32)
	verifier = base64.RawURLEncoding.EncodeToString(raw)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// constantTimeCompare overi shodu retezcu bez moznosti timing-attacku
func constantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// isValidTULEmail overi, ze email patri striktne pod univerzitu TUL (@tul.cz nebo subdomena)
func isValidTULEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	domain := parts[1]
	return domain == "tul.cz" || strings.HasSuffix(domain, ".tul.cz")
}

// sanitizeNickname ocisti jmeno od zneuzitelnych znaku na Discordu
func sanitizeNickname(name string) string {
	var b strings.Builder
	for _, r := range name {
		// Odstraneni ridicich znaku
		if unicode.IsControl(r) {
			continue
		}
		// Odstraneni neviditelnych zero-width mezer (ZWS)
		if r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()

	// Odstraneni nebezpecnych tagu a formátovacích znaku
	disallowed := []string{"@everyone", "@here", "<@", "discord.gg", "http://", "https://"}
	for _, d := range disallowed {
		s = strings.ReplaceAll(s, d, "")
	}

	// Normalizace mezer
	s = strings.Join(strings.Fields(s), " ")

	// Orez na limit Discord API (max 32 run)
	runes := []rune(s)
	if len(runes) > 32 {
		runes = runes[:32]
	}
	res := strings.TrimSpace(string(runes))
	if res == "" {
		return "Student FM"
	}
	return res
}

// --- SPRAVA SESSIONS (S EXPIRACI A OCHRANOU FIXACE) ---

func getOrCreateSession(w http.ResponseWriter, r *http.Request) *Session {
	cookie, err := r.Cookie(SessionCookieName)
	sessionsMux.Lock()
	defer sessionsMux.Unlock()

	now := time.Now()
	if err == nil && cookie.Value != "" {
		if s, ok := sessions[cookie.Value]; ok {
			if now.Before(s.ExpiresAt) {
				s.ExpiresAt = now.Add(SessionDuration)
				return s
			}
			// Session expirovala
			delete(sessions, cookie.Value)
		}
	}

	// Vytvorime novou session
	newID := generateID()
	newSession := &Session{
		ID:        newID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionDuration),
	}
	sessions[newID] = newSession

	isSecure := cfg.SecureCookies || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    newID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionDuration.Seconds()),
	})

	return newSession
}

func getExistingSession(r *http.Request) *Session {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	sessionsMux.RLock()
	defer sessionsMux.RUnlock()

	s, ok := sessions[cookie.Value]
	if !ok || time.Now().After(s.ExpiresAt) {
		return nil
	}
	return s
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func cleanExpiredSessions() {
	ticker := time.NewTicker(2 * time.Minute)
	for range ticker.C {
		sessionsMux.Lock()
		now := time.Now()
		for id, s := range sessions {
			if now.After(s.ExpiresAt) {
				delete(sessions, id)
			}
		}
		sessionsMux.Unlock()

		// Cisteni starych IP rate limitu
		ipLimitsMux.Lock()
		for ip, lim := range ipLimits {
			if now.Sub(lim.window) > 10*time.Minute {
				delete(ipLimits, ip)
			}
		}
		ipLimitsMux.Unlock()
	}
}

// --- PERZISTENCE A OCHRANA PROTI MULTI-ACCOUNTINGU ---

// checkBindingAllowed zajistuje striktni vazbu 1:1 mezi TUL uctem a Discord uctem
func checkBindingAllowed(s *Student) error {
	usersDBMux.RLock()
	defer usersDBMux.RUnlock()

	for _, u := range usersDB {
		// Stejne Microsoft ID nesmi overit jiny Discord ucet
		if u.MicrosoftID == s.MicrosoftID && u.DiscordID != s.DiscordID {
			return fmt.Errorf("tento univerzitní TUL účet (%s) je již spárován s jiným Discord účtem", s.Email)
		}
		// Stejny Discord ucet nesmi ziskat jine Microsoft ID
		if u.DiscordID == s.DiscordID && u.MicrosoftID != s.MicrosoftID {
			return fmt.Errorf("tento Discord účet je již spárován s jiným univerzitním účtem")
		}
	}
	return nil
}

func loadUsersStorage() {
	usersDBMux.Lock()
	defer usersDBMux.Unlock()

	data, err := os.ReadFile(UsersStorageFile)
	if err != nil {
		return // Soubor zatim neexistuje
	}
	var list []*Student
	if err := json.Unmarshal(data, &list); err == nil {
		for _, u := range list {
			usersDB[u.DiscordID] = u
		}
		log.Printf("📂 Nacteno %d overenych uzivatelu z %s", len(list), UsersStorageFile)
	}
}

// saveUser provede bezpecny atomicky zapis s pravy 0600 (pouze pro tento proces)
func saveUser(s *Student) error {
	if err := checkBindingAllowed(s); err != nil {
		return err
	}

	usersDBMux.Lock()
	defer usersDBMux.Unlock()

	usersDB[s.DiscordID] = s

	list := make([]*Student, 0, len(usersDB))
	for _, u := range usersDB {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	// Atomicky zapis: zapis do docasneho souboru a nasledny rename
	dir := filepath.Dir(UsersStorageFile)
	tempFile, err := os.CreateTemp(dir, "users_*.tmp")
	if err != nil {
		return fmt.Errorf("chyba vytvoreni docasneho souboru: %w", err)
	}
	tempName := tempFile.Name()

	// Zabezpeceni prav: pouze vlastnik procesu muze cist a zapisovat
	_ = tempFile.Chmod(0600)

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}

	if err := os.Rename(tempName, UsersStorageFile); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

// --- RATE LIMITING MIDDLEWARE ---

func getClientIP(r *http.Request) string {
	// Pokud bezi za reverse proxy (Nginx, Traefik, Cloudflare)
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

func checkRateLimit(ip string, maxReqs int, window time.Duration) bool {
	if !cfg.RateLimitEnabled {
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

// securityMiddleware pridava striktni HTTP bezpecnostni hlavicky a rate limiting
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		// Max 60 pozadavku za minutu na IP pro cele rozhrani
		if !checkRateLimit(ip, 60, time.Minute) {
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

		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || cfg.SecureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

// --- DISCORD REST API (Automaticke pripojeni a prirazeni roli) ---

func performDiscordJoinAndRole(discordUserID, userAccessToken string, student *Student) error {
	guildID := cfg.DiscordGuildID
	if guildID == "" {
		return fmt.Errorf("DISCORD_GUILD_ID neni nakonfigurovano")
	}

	botToken := cfg.DiscordToken
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Zjistime, zda uzivatel jiz je na serveru
	memberURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordUserID)
	req, _ := http.NewRequest("GET", memberURL, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Urceni roli
	var rolesToAdd []string
	if cfg.DiscordVerifiedID != "" {
		rolesToAdd = append(rolesToAdd, cfg.DiscordVerifiedID)
	}
	if student.Role == "Zaměstnanec FM" && cfg.DiscordFMStaffID != "" {
		rolesToAdd = append(rolesToAdd, cfg.DiscordFMStaffID)
	} else if cfg.DiscordFMStudentID != "" {
		rolesToAdd = append(rolesToAdd, cfg.DiscordFMStudentID)
	}

	// Bezpecna ocista jmena pro Discord
	cleanNick := sanitizeNickname(student.Name)

	if resp.StatusCode == http.StatusNotFound {
		// Uzivatel neni na serveru -> Pridame ho pres PUT s OAuth2 access tokenem (guilds.join scope)
		log.Printf("➕ Uzivatel %s neni na serveru, pridavam ho pres guilds.join...", discordUserID)

		payload := map[string]interface{}{
			"access_token": userAccessToken,
			"roles":        rolesToAdd,
		}
		if cleanNick != "" {
			payload["nick"] = cleanNick
		}
		bodyBytes, _ := json.Marshal(payload)

		putReq, _ := http.NewRequest("PUT", memberURL, bytes.NewBuffer(bodyBytes))
		putReq.Header.Set("Authorization", "Bot "+botToken)
		putReq.Header.Set("Content-Type", "application/json")

		putResp, putErr := client.Do(putReq)
		if putErr != nil {
			return putErr
		}
		defer putResp.Body.Close()
		log.Printf("✅ Uzivatel pridan na server (status: %d)", putResp.StatusCode)
		return nil
	}

	// Uzivatel jiz na serveru je -> Pridame role a aktualizujeme prezdivku
	for _, roleID := range rolesToAdd {
		roleURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s/roles/%s", guildID, discordUserID, roleID)
		rReq, _ := http.NewRequest("PUT", roleURL, nil)
		rReq.Header.Set("Authorization", "Bot "+botToken)
		rResp, rErr := client.Do(rReq)
		if rErr == nil {
			rResp.Body.Close()
		}
	}

	// Aktualizace prezdivky s osetrenim
	if cleanNick != "" {
		nickPayload := map[string]string{"nick": cleanNick}
		nickBytes, _ := json.Marshal(nickPayload)
		patchReq, _ := http.NewRequest("PATCH", memberURL, bytes.NewBuffer(nickBytes))
		patchReq.Header.Set("Authorization", "Bot "+botToken)
		patchReq.Header.Set("Content-Type", "application/json")
		pResp, pErr := client.Do(patchReq)
		if pErr == nil {
			pResp.Body.Close()
		}
	}

	return nil
}

// isVerifyReaction rozhoduje, zda dana reakce ma vyvolat proces overeni
func isVerifyReaction(emojiName, emojiAPI, messageID, botUserID, reactingUserID string) bool {
	if reactingUserID == botUserID {
		return false // Ignorujeme vlastni reakce bota
	}
	if cfg.DiscordVerifyMessageID != "" && messageID != cfg.DiscordVerifyMessageID {
		return false
	}
	targetEmoji := cfg.DiscordVerifyEmoji
	if targetEmoji == "" {
		targetEmoji = "🎓"
	}
	if emojiName != targetEmoji && emojiAPI != targetEmoji {
		if cfg.DiscordVerifyMessageID == "" {
			return false
		}
	}
	return true
}

// determineRole urcuje, zda jde o studenta ci zamestnance podle pozice a emailu
func determineRole(jobTitle, email string) string {
	if jobTitle != "" || strings.Contains(strings.ToLower(email), "zamestnanec") {
		return "Zaměstnanec FM"
	}
	return "Student FM"
}

// --- DISCORD BOT LOGIKA ---

func setupDiscordBot() {
	if cfg.DiscordToken == "" || cfg.DiscordToken == "SEM_VLOZTE_DISCORD_BOT_TOKEN" {
		log.Println("ℹ️ DISCORD_TOKEN neni nastaven v .env – bot neni spusten (web konektor funguje dal).")
		return
	}

	var err error
	dg, err = discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		log.Printf("⚠️ Chyba pri vytvareni bota: %v", err)
		return
	}

	dg.Identify.Intents = discordgo.IntentsAllWithoutPrivileged | discordgo.IntentGuildMembers | discordgo.IntentGuildMessageReactions | discordgo.IntentDirectMessages

	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("🤖 Discord Bot %s je online a naslouchá reakcím!", s.State.User.Username)

		cmd := &discordgo.ApplicationCommand{
			Name:        "overit",
			Description: "Ověření identity studenta/zaměstnance FM TUL a získání rolí",
		}
		_, _ = s.ApplicationCommandCreate(s.State.User.ID, cfg.DiscordGuildID, cmd)
	})

	// REAKCE NA ZPRÁVU (s cooldown ochranou proti spamu)
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		botID := ""
		if s.State != nil && s.State.User != nil {
			botID = s.State.User.ID
		}
		if !isVerifyReaction(r.Emoji.Name, r.Emoji.APIName(), r.MessageID, botID, r.UserID) {
			return
		}

		// Cooldown kontrola (max 1 reakce za 30 sekund na uzivatele)
		reactionCooldownsMux.Lock()
		lastTime, exists := reactionCooldowns[r.UserID]
		now := time.Now()
		if exists && now.Sub(lastTime) < 30*time.Second {
			reactionCooldownsMux.Unlock()
			_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.APIName(), r.UserID)
			return
		}
		reactionCooldowns[r.UserID] = now
		reactionCooldownsMux.Unlock()

		log.Printf("🔔 Uživatel %s kliknul na reakci %s na zprávě %s", r.UserID, r.Emoji.Name, r.MessageID)

		dmChannel, err := s.UserChannelCreate(r.UserID)
		if err != nil {
			log.Printf("⚠️ Nepodařilo se otevřít DM pro %s: %v", r.UserID, err)
			return
		}

		embed := &discordgo.MessageEmbed{
			Title: "🎓 Ověření identity FM TUL",
			Description: fmt.Sprintf(
				"Ahoj!\n\nKliknul jsi na reakci pro ověření identity na Discord serveru **FM TUL**.\n\n"+
					"Pro propojení účtu a automatické získání rolí klikni na odkaz níže:\n\n"+
					"👉 **%s**\n\n"+
					"1. Přihlásíš se školním Microsoft účtem (`@tul.cz`)\n"+
					"2. Propojíš svůj Discord účet a role ti budou okamžitě přiděleny na serveru.",
				cfg.BaseURL,
			),
			Color: 0x2563eb,
		}

		btn := discordgo.Button{
			Label: "Přejít na ověření",
			Style: discordgo.LinkButton,
			URL:   cfg.BaseURL,
		}

		_, sendErr := s.ChannelMessageSendComplex(dmChannel.ID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{btn},
				},
			},
		})

		if sendErr != nil {
			log.Printf("⚠️ Nelze odeslat DM uživateli %s: %v", r.UserID, sendErr)
			tmpMsg, _ := s.ChannelMessageSend(r.ChannelID, fmt.Sprintf("<@%s> Chtěl jsem ti poslat odkaz k ověření, ale máš zablokované soukromé zprávy (DM). Povol si prosím příjem DM od členů serveru, nebo přejdi přímo na: %s", r.UserID, cfg.BaseURL))
			if tmpMsg != nil {
				go func(cID, mID string) {
					time.Sleep(15 * time.Second)
					_ = s.ChannelMessageDelete(cID, mID)
				}(r.ChannelID, tmpMsg.ID)
			}
		}

		_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.APIName(), r.UserID)
	})

	// PŘÍKAZ !setup-overeni PRO ODESLÁNÍ OVĚŘOVACÍ ZPRÁVY
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.Bot {
			return
		}

		if strings.TrimSpace(m.Content) == "!setup-overeni" {
			embed := &discordgo.MessageEmbed{
				Title: "🎓 Ověření studentů a zaměstnanců FM TUL",
				Description: "Vítejte na Discord serveru Fakulty mechatroniky, informatiky a mezioborových studií TUL!\n\n" +
					"Pro získání přístupu do neveřejných fakultních kanálů a místností **klikněte na reakci 🎓 pod touto zprávou** (nebo na tlačítko níže).\n\n" +
					"Bot vám obratem pošle privátní odkaz k ověření přes univerzitní Microsoft účet (`@tul.cz`).",
				Color: 0x0284c7,
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Ověření probíhá jednorázově skrze oficiální univerzitní Microsoft 365.",
				},
			}

			btn := discordgo.Button{
				Label: "Ověřit identitu přes web",
				Style: discordgo.LinkButton,
				URL:   cfg.BaseURL,
			}

			sentMsg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{btn},
					},
				},
			})

			if err == nil && sentMsg != nil {
				targetEmoji := cfg.DiscordVerifyEmoji
				if targetEmoji == "" {
					targetEmoji = "🎓"
				}
				_ = s.MessageReactionAdd(m.ChannelID, sentMsg.ID, targetEmoji)
				_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
				log.Printf("📢 Ověřovací zpráva s reakcí %s odeslána do kanálu %s (ID zprávy: %s)", targetEmoji, m.ChannelID, sentMsg.ID)
			}
		}
	})

	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		if i.ApplicationCommandData().Name == "overit" {
			embed := &discordgo.MessageEmbed{
				Title: "🎓 Ověření identity FM TUL",
				Description: fmt.Sprintf(
					"Ahoj <@%s>,\n\nPro získání přístupu a rolí na Discordu FM TUL se prosím ověř přes náš webový konektor:\n\n👉 **%s**\n\n1. Přihlásíš se školním Microsoft účtem (`@tul.cz`)\n2. Propojíš svůj Discord účet",
					i.Member.User.ID,
					cfg.BaseURL,
				),
				Color: 0x2563eb,
			}
			btn := discordgo.Button{
				Label: "Přejít na ověření",
				Style: discordgo.LinkButton,
				URL:   cfg.BaseURL,
			}
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}}},
					Flags:      discordgo.MessageFlagsEphemeral,
				},
			})
		}
	})

	_ = dg.Open()
}

// --- HTTP SERVER HANDLERY S OCHRANOU CSRF A PKCE ---

func getMicrosoftLoginURL(origin string, state, challenge string) string {
	tenant := cfg.MSALTenantID
	if tenant == "" {
		tenant = "common"
	}
	redirectURI := fmt.Sprintf("%s/msal", origin)
	values := url.Values{
		"client_id":             {cfg.MSALClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"response_mode":         {"query"},
		"scope":                 {"User.Read openid profile email"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?%s", tenant, values.Encode())
}

func getDiscordLoginURL(origin string, state string) string {
	redirectURI := fmt.Sprintf("%s/discord", origin)
	values := url.Values{
		"client_id":     {cfg.DiscordClientID},
		"response_type": {"code"},
		"scope":         {"identify guilds.join"},
		"redirect_uri":  {redirectURI},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return fmt.Sprintf("https://discord.com/oauth2/authorize?%s", values.Encode())
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	session := getOrCreateSession(w, r)
	origin := cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}

	// Vygenerujeme novy kryptograficky state a PKCE pro Microsoft krok
	session.MSALState = generateStateToken()
	verifier, challenge := generatePKCE()
	session.MSALCodeVerifier = verifier

	// Vygenerujeme state pro Discord krok
	session.DiscordState = generateStateToken()

	msURL := getMicrosoftLoginURL(origin, session.MSALState, challenge)
	discordURL := getDiscordLoginURL(origin, session.DiscordState)

	data := map[string]interface{}{
		"Session":       session.Student,
		"MSURL":         msURL,
		"DiscordURL":    discordURL,
		"InviteURL":     cfg.DiscordInviteURL,
		"EnableDevMock": cfg.EnableDevMock,
	}

	tmpl := template.Must(template.New("index").Parse(`
	<!DOCTYPE html>
	<html lang="cs">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>FM TUL ↔ Discord Connector</title>
		<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			* { margin: 0; padding: 0; box-sizing: border-box; font-family: 'Inter', sans-serif; }
			body { background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
			.card { background: #1e293b; border: 1px solid #334155; border-radius: 16px; width: 100%; max-width: 500px; padding: 36px; box-shadow: 0 25px 50px -12px rgba(0,0,0,0.5); }
			.badge { display: inline-block; padding: 4px 12px; border-radius: 9999px; font-size: 13px; font-weight: 600; margin-bottom: 14px; background: #0284c7; color: white; }
			h1 { font-size: 24px; font-weight: 700; margin-bottom: 8px; }
			p.desc { color: #94a3b8; font-size: 14px; line-height: 1.5; margin-bottom: 24px; }
			
			.steps { display: flex; gap: 8px; margin-bottom: 24px; }
			.step { flex: 1; height: 4px; border-radius: 2px; background: #334155; }
			.step.active { background: #38bdf8; }
			.step.completed { background: #10b981; }

			.btn { display: flex; align-items: center; justify-content: center; gap: 10px; width: 100%; padding: 13px 20px; border-radius: 10px; font-size: 15px; font-weight: 600; text-decoration: none; cursor: pointer; transition: all 0.2s; border: none; text-align: center; }
			.btn-ms { background: #2563eb; color: white; margin-bottom: 12px; }
			.btn-ms:hover { background: #1d4ed8; }
			.btn-discord { background: #5865F2; color: white; margin-bottom: 12px; }
			.btn-discord:hover { background: #4752c4; }
			.btn-success { background: #10b981; color: white; }
			.btn-success:hover { background: #059669; }
			
			.info-box { background: #0f172a; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-bottom: 20px; font-size: 14px; }
			.info-label { color: #94a3b8; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 4px; }
			.info-value { color: #f8fafc; font-weight: 600; }
			
			.dev-tools { margin-top: 24px; padding-top: 18px; border-top: 1px solid #334155; display: flex; justify-content: space-between; font-size: 12px; }
			.dev-tools a { color: #64748b; text-decoration: none; }
			.dev-tools a:hover { color: #94a3b8; text-decoration: underline; }
		</style>
	</head>
	<body>
		<div class="card">
			<span class="badge">FM TUL • Zabezpečený konektor</span>
			<h1>Propojení identity</h1>
			<p class="desc">Automatické ověření studentů a zaměstnanců Fakulty mechatroniky TUL a udělení rolí na Discordu.</p>

			<div class="steps">
				<div class="step {{if .Session}}completed{{else}}active{{end}}"></div>
				<div class="step {{if and .Session .Session.DiscordID}}completed{{else if .Session}}active{{end}}"></div>
			</div>

			{{if not .Session}}
				<!-- KROK 1: PŘIHLÁŠENÍ MICROSOFT -->
				<a href="{{.MSURL}}" class="btn btn-ms">
					<svg width="18" height="18" viewBox="0 0 21 21" fill="none">
						<rect x="1" y="1" width="9" height="9" fill="#f25022"/>
						<rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
						<rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
						<rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
					</svg>
					1. Přihlásit se přes Microsoft TUL
				</a>
			{{else if not .Session.DiscordID}}
				<!-- KROK 2: PŘIHLÁŠENÍ DISCORD -->
				<div class="info-box">
					<div class="info-label">Ověřený školní účet</div>
					<div class="info-value">✓ {{.Session.Name}} ({{.Session.Email}})</div>
					<div style="color: #38bdf8; font-size: 13px; margin-top: 4px;">{{.Session.Role}}</div>
				</div>

				<a href="{{.DiscordURL}}" class="btn btn-discord">
					<svg width="20" height="16" viewBox="0 0 127.14 96.36" fill="currentColor">
						<path d="M107.7,8.07A105.15,105.15,0,0,0,81.47,0a72.06,72.06,0,0,0-3.36,6.83A97.68,97.68,0,0,0,49,6.83,72.37,72.37,0,0,0,45.64,0,105.89,105.89,0,0,0,19.39,8.09C2.79,32.65-1.71,56.6.54,80.21h0A105.73,105.73,0,0,0,32.71,96.36,77.7,77.7,0,0,0,39.6,85.25a68.42,68.42,0,0,1-10.85-5.18c.91-.66,1.8-1.34,2.66-2a75.57,75.57,0,0,0,64.32,0c.87.71,1.76,1.39,2.66,2a68.68,68.68,0,0,1-10.87,5.19,77,77,0,0,0,6.89,11.1A105.25,105.25,0,0,0,126.6,80.22h0C129.24,52.84,122.09,29.11,107.7,8.07ZM42.45,65.69C36.18,65.69,31,60,31,53s5-12.74,11.43-12.74S54,45.91,53.89,53,48.84,65.69,42.45,65.69Zm42.24,0C78.41,65.69,73.25,60,73.25,53s5-12.74,11.44-12.74S96.23,45.91,96.12,53,91.08,65.69,84.69,65.69Z"/>
					</svg>
					2. Propojit s Discord účtem
				</a>
			{{else}}
				<!-- KROK 3: HOTOVO -->
				<div class="info-box" style="border-left: 4px solid #10b981;">
					<div class="info-label">Úspěšně propojeno a ověřeno</div>
					<div class="info-value">✓ {{.Session.Name}}</div>
					<div style="color: #38bdf8; font-size: 13px; margin-top: 4px;">Role: {{.Session.Role}}</div>
					<div style="color: #94a3b8; font-size: 13px; margin-top: 2px;">Role na Discordu byly automaticky uděleny.</div>
				</div>

				<a href="{{if .InviteURL}}{{.InviteURL}}{{else}}https://discord.com/app{{end}}" class="btn btn-success">
					Přejít na Discord Server 🚀
				</a>
			{{end}}

			<div class="dev-tools">
				<div>
					{{if .EnableDevMock}}
						<a href="/mock-msal">Simulovat MS</a> • <a href="/mock-discord">Simulovat Discord</a>
					{{else}}
						<span style="color: #475569;">Produkční zabezpečený režim</span>
					{{end}}
				</div>
				<div>
					{{if .Session}}<a href="/logout">Odhlásit</a>{{end}}
				</div>
			</div>
		</div>
	</body>
	</html>
	`))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

func handleMSAL(w http.ResponseWriter, r *http.Request) {
	ip := getClientIP(r)
	if !checkRateLimit("oauth_msal_"+ip, 10, time.Minute) {
		http.Error(w, "Příliš mnoho pokusů o přihlášení. Počkejte chvíli.", http.StatusTooManyRequests)
		return
	}

	session := getExistingSession(r)
	if session == nil {
		http.Error(w, "Neplatná nebo expirovaná relace. Zkuste to znovu z hlavní stránky.", http.StatusBadRequest)
		return
	}

	// 1. Striktni validace CSRF parametru state (v konstantnim case)
	reqState := r.URL.Query().Get("state")
	if reqState == "" || session.MSALState == "" || !constantTimeCompare(reqState, session.MSALState) {
		log.Printf("🚨 Bezpečnostní varování: Neplatný MSAL state parametr od IP %s", ip)
		http.Error(w, "Neplatný bezpečnostní token (možný pokus o CSRF útok).", http.StatusForbidden)
		return
	}
	// Spotrebujeme state token pro zamezeni replay utoku
	session.MSALState = ""

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	tenant := cfg.MSALTenantID
	if tenant == "" {
		tenant = "common"
	}

	origin := cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	redirectURI := fmt.Sprintf("%s/msal", origin)

	// 2. Vymena kodu za token s predanim PKCE code_verifier
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)
	form := url.Values{
		"client_id":     {cfg.MSALClientID},
		"client_secret": {cfg.MSALClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {session.MSALCodeVerifier},
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ MSAL Token error: status %v, err: %v", resp, err)
		http.Error(w, "Chyba při ověřování přihlášení u Microsoft TUL.", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil || tokenRes.AccessToken == "" {
		http.Error(w, "Neplatná odpověď z Microsoft TUL.", http.StatusBadGateway)
		return
	}

	// 3. Volani Graph API /v1.0/me
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://graph.microsoft.com/v1.0/me?$select=id,displayName,mail,userPrincipalName,jobTitle,department", nil)
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenRes.TokenType, tokenRes.AccessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	gResp, gErr := client.Do(req)
	if gErr != nil || gResp.StatusCode != http.StatusOK {
		http.Error(w, "Nepodařilo se načíst profil z Microsoft Graph API.", http.StatusBadGateway)
		return
	}
	defer gResp.Body.Close()

	var profile struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		JobTitle          string `json:"jobTitle"`
		Department        string `json:"department"`
	}
	if err := json.NewDecoder(gResp.Body).Decode(&profile); err != nil {
		http.Error(w, "Chyba čtení profilu.", http.StatusInternalServerError)
		return
	}

	email := profile.Mail
	if email == "" {
		email = profile.UserPrincipalName
	}

	// 4. Striktni validace univerzitni domeny TUL
	if !isValidTULEmail(email) {
		log.Printf("⛔ Zamítnuto neoprávněné přihlášení s emailem: %s", email)
		http.Error(w, "Přístup povolen pouze s univerzitním účtem TUL (@tul.cz).", http.StatusForbidden)
		return
	}

	role := determineRole(profile.JobTitle, email)

	session.Student = &Student{
		MicrosoftID: profile.ID,
		Name:        profile.DisplayName,
		Email:       email,
		Faculty:     "FM",
		Role:        role,
		VerifiedAt:  time.Now(),
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func handleDiscord(w http.ResponseWriter, r *http.Request) {
	ip := getClientIP(r)
	if !checkRateLimit("oauth_discord_"+ip, 10, time.Minute) {
		http.Error(w, "Příliš mnoho pokusů o propojení Discordu. Počkejte chvíli.", http.StatusTooManyRequests)
		return
	}

	session := getExistingSession(r)
	if session == nil || session.Student == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// 1. Striktni validace CSRF state parametru
	reqState := r.URL.Query().Get("state")
	if reqState == "" || session.DiscordState == "" || !constantTimeCompare(reqState, session.DiscordState) {
		log.Printf("🚨 Bezpečnostní varování: Neplatný Discord state parametr od IP %s", ip)
		http.Error(w, "Neplatný bezpečnostní token (možný pokus o CSRF útok).", http.StatusForbidden)
		return
	}
	session.DiscordState = ""

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	origin := cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	redirectURI := fmt.Sprintf("%s/discord", origin)

	// 2. Vymena Discord OAuth2 kodu za Access Token
	form := url.Values{
		"client_id":     {cfg.DiscordClientID},
		"client_secret": {cfg.DiscordClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm("https://discord.com/api/v10/oauth2/token", form)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ Discord OAuth token exchange selhal: %v", err)
		http.Error(w, "Chyba při komunikaci s Discord OAuth.", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tokenRes)

	// 3. Nacteni Discord profilu (@me)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://discord.com/api/v10/users/@me", nil)
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenRes.TokenType, tokenRes.AccessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	uResp, uErr := client.Do(req)
	if uErr != nil || uResp.StatusCode != http.StatusOK {
		http.Error(w, "Nepodařilo se načíst profil z Discord API.", http.StatusBadGateway)
		return
	}
	defer uResp.Body.Close()

	var discordUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	_ = json.NewDecoder(uResp.Body).Decode(&discordUser)

	session.Student.DiscordID = discordUser.ID

	// 4. Kontrola striktni vazby 1:1 (Anti-Multi-Accounting)
	if err := checkBindingAllowed(session.Student); err != nil {
		log.Printf("⛔ Zamítnuto vícenásobné spárování účtu: %v", err)
		http.Error(w, fmt.Sprintf("Bezpečnostní omezení: %v", err), http.StatusConflict)
		return
	}

	// 5. Automaticke pripojeni na server (guilds.join) a prirazeni roli
	err = performDiscordJoinAndRole(discordUser.ID, tokenRes.AccessToken, session.Student)
	if err != nil {
		log.Printf("⚠️ Chyba pri prirazeni roli na Discordu: %v", err)
	}

	// 6. Bezpecne ulozeni do perzistentni databaze (users.json)
	if err := saveUser(session.Student); err != nil {
		log.Printf("❌ Chyba pri ukladani uzivatele: %v", err)
		http.Error(w, "Chyba při ukládání registrace.", http.StatusInternalServerError)
		return
	}

	log.Printf("🎉 Úspěšně ověřen a spárován: %s (%s) <-> Discord ID: %s", session.Student.Name, session.Student.Email, session.Student.DiscordID)

	// Presmerovani na pozvanku Discord serveru nebo zpet na web
	if cfg.DiscordInviteURL != "" && cfg.DiscordInviteURL != "https://discord.gg/vase-pozvanka" {
		http.Redirect(w, r, cfg.DiscordInviteURL, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// Mock endpointy jsou striktne chraneny: pokud ENABLE_DEV_MOCK!=true, vraci 404
func handleMockMSAL(w http.ResponseWriter, r *http.Request) {
	if !cfg.EnableDevMock {
		http.NotFound(w, r)
		return
	}
	session := getOrCreateSession(w, r)
	session.Student = &Student{
		MicrosoftID: "mock-ms-id-12345",
		Name:        "Jakub Marcinka",
		Email:       "jakub.marcinka@tul.cz",
		Faculty:     "FM",
		Role:        "Student FM",
		VerifiedAt:  time.Now(),
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func handleMockDiscord(w http.ResponseWriter, r *http.Request) {
	if !cfg.EnableDevMock {
		http.NotFound(w, r)
		return
	}
	session := getOrCreateSession(w, r)
	if session.Student == nil {
		session.Student = &Student{
			MicrosoftID: "mock-ms-id-12345",
			Name:        "Jakub Marcinka",
			Email:       "jakub.marcinka@tul.cz",
			Faculty:     "FM",
			Role:        "Student FM",
			VerifiedAt:  time.Now(),
		}
	}
	session.Student.DiscordID = "mock-discord-id-98765"
	_ = saveUser(session.Student)
	http.Redirect(w, r, "/", http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// --- HLAVNI FUNKCE ---

func main() {
	_ = godotenv.Load()

	msClientID := os.Getenv("MSAL_CLIENT_ID")
	if msClientID == "" {
		msClientID = os.Getenv("MICROSOFT_CLIENT_ID")
	}
	msClientSecret := os.Getenv("MSAL_CLIENT_SECRET")
	if msClientSecret == "" {
		msClientSecret = os.Getenv("MICROSOFT_CLIENT_SECRET")
	}
	msTenantID := os.Getenv("MSAL_TENANT_ID")
	if msTenantID == "" {
		msTenantID = os.Getenv("MICROSOFT_TENANT_ID")
	}

	discordGuildID := os.Getenv("DISCORD_GUILD_ID")
	if discordGuildID == "" {
		discordGuildID = os.Getenv("GUILD_ID")
	}

	discordVerifiedID := os.Getenv("DISCORD_VERIFIED_ID")
	discordFMStudentID := os.Getenv("DISCORD_FM_STUDENT_ID")
	if discordFMStudentID == "" {
		discordFMStudentID = os.Getenv("FM_STUDENT_ROLE_ID")
	}
	discordFMStaffID := os.Getenv("DISCORD_FM_STAFF_ID")
	if discordFMStaffID == "" {
		discordFMStaffID = os.Getenv("FM_STAFF_ROLE_ID")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%s", port)
	}

	cfg = Config{
		MSALClientID:           msClientID,
		MSALClientSecret:       msClientSecret,
		MSALTenantID:           msTenantID,
		DiscordClientID:        os.Getenv("DISCORD_CLIENT_ID"),
		DiscordClientSecret:    os.Getenv("DISCORD_CLIENT_SECRET"),
		DiscordToken:           os.Getenv("DISCORD_TOKEN"),
		DiscordGuildID:         discordGuildID,
		DiscordVerifiedID:      discordVerifiedID,
		DiscordFMStudentID:     discordFMStudentID,
		DiscordFMStaffID:       discordFMStaffID,
		DiscordInviteURL:       os.Getenv("DISCORD_INVITE_URL"),
		DiscordVerifyEmoji:     os.Getenv("DISCORD_VERIFY_EMOJI"),
		DiscordVerifyMessageID: os.Getenv("DISCORD_VERIFY_MESSAGE_ID"),
		Host:                   host,
		Port:                   port,
		BaseURL:                baseURL,
		EnableDevMock:          strings.ToLower(os.Getenv("ENABLE_DEV_MOCK")) == "true",
		SecureCookies:          strings.ToLower(os.Getenv("SECURE_COOKIES")) == "true",
		RateLimitEnabled:       os.Getenv("RATE_LIMIT_ENABLED") != "false",
	}

	if cfg.EnableDevMock {
		log.Println("⚠️ UPOZORNĚNÍ: ENABLE_DEV_MOCK=true – simulované endpointy jsou povoleny. V produkci nastavte na false!")
	} else {
		log.Println("🛡️ Bezpečnostní režim: Dev mock endpointy jsou zakázány.")
	}

	// Nacteni stavajicich uzivatelu z disku
	loadUsersStorage()

	// Cisteni expirovanych sessions
	go cleanExpiredSessions()

	// Spusteni Discord bota
	setupDiscordBot()

	// Registrace HTTP handleru
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /msal", handleMSAL)
	mux.HandleFunc("GET /discord", handleDiscord)
	mux.HandleFunc("GET /mock-msal", handleMockMSAL)
	mux.HandleFunc("GET /mock-discord", handleMockDiscord)
	mux.HandleFunc("GET /logout", handleLogout)

	// Obaleni vsech handleru bezpecnostnim middlewarem (Hlavičky + Rate Limiting)
	secureHandler := securityMiddleware(mux)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           secureHandler,
		ReadHeaderTimeout: 5 * time.Second,  // Prevence Slowloris utoku
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // Max 1 MB hlavicky
	}

	go func() {
		log.Printf("🚀 FM TUL Discord Connector běží na %s (lokálně: http://%s)", cfg.BaseURL, addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Chyba HTTP serveru: %v", err)
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("🛑 Zastavuji server a Discord bota...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	if dg != nil {
		_ = dg.Close()
	}
	log.Println("👋 Aplikace byla korektně ukončena.")
}
