package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

// Config drzi konfiguraci aplikace nactenou z prostredi
type Config struct {
	MicrosoftClientID     string
	MicrosoftClientSecret string
	MicrosoftTenantID     string
	RedirectURI           string

	DiscordToken     string
	GuildID          string
	FMStudentRoleID  string
	FMStaffRoleID    string

	Host string
	Port string
}

// VerificationSession uchovava informace o probihajicim overeni
type VerificationSession struct {
	DiscordID string
	CreatedAt time.Time
}

// UserInfo drzi udaje ziskane z Microsoft Graph API
type UserInfo struct {
	Name     string
	Email    string
	IsStaff  bool
	JobTitle string
	Faculty  string
}

// DiscordResult reprezentuje vysledek prirazeni role v Discordu
type DiscordResult struct {
	Success         bool
	RoleName        string
	MemberName      string
	NicknameChanged bool
	Message         string
}

// Globální stavové proměnné
var (
	cfg                 Config
	dg                  *discordgo.Session
	verificationStates  = make(map[string]VerificationSession)
	statesMutex         sync.RWMutex
	appStartTime        = time.Now()
)

// generateRandomToken vygeneruje bezpecny nahodny token pro OAuth state
func generateRandomToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Sprava overovacich session
func createVerificationSession(discordID string) string {
	token := generateRandomToken(16)
	statesMutex.Lock()
	defer statesMutex.Unlock()
	verificationStates[token] = VerificationSession{
		DiscordID: discordID,
		CreatedAt: time.Now(),
	}
	return token
}

func getVerificationSession(token string) (VerificationSession, bool) {
	statesMutex.RLock()
	defer statesMutex.RUnlock()
	s, ok := verificationStates[token]
	return s, ok
}

func consumeVerificationSession(token string) (VerificationSession, bool) {
	statesMutex.Lock()
	defer statesMutex.Unlock()
	s, ok := verificationStates[token]
	if ok {
		delete(verificationStates, token)
	}
	return s, ok
}

// cleanExpiredSessions periodicky maze session starsi nez 15 minut
func cleanExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		statesMutex.Lock()
		now := time.Now()
		for token, sess := range verificationStates {
			if now.Sub(sess.CreatedAt) > 15*time.Minute {
				delete(verificationStates, token)
			}
		}
		statesMutex.Unlock()
	}
}

// --- DISCORD BOT LOGIKA ---

func setupDiscordBot() (*discordgo.Session, error) {
	if cfg.DiscordToken == "" || cfg.DiscordToken == "SEM_VLOZTE_DISCORD_BOT_TOKEN" {
		log.Println("ℹ️ DISCORD_TOKEN neni nastaven v .env – bot nebude pripojen (web bezi dal).")
		return nil, nil
	}

	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("chyba vytvoreni Discord relace: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsAllWithoutPrivileged | discordgo.IntentGuildMembers

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("✅ Discord Bot %s je online a pripraven!", s.State.User.Username)
		registerSlashCommands(s)
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		if i.ApplicationCommandData().Name == "overit" {
			handleSlashOverit(s, i)
		}
	})

	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.Bot {
			return
		}
		if strings.TrimSpace(m.Content) == "!overit" {
			handlePrefixOverit(s, m)
		}
	})

	err = session.Open()
	if err != nil {
		return nil, fmt.Errorf("nepodarilo se otevrit websocket k Discordu: %w", err)
	}

	return session, nil
}

func registerSlashCommands(s *discordgo.Session) {
	cmd := &discordgo.ApplicationCommand{
		Name:        "overit",
		Description: "Ověření identity pro studenty a zaměstnance FM TUL",
	}

	// Registrujeme prednostne na konkterni guild (projevi se okamzite)
	targetGuild := cfg.GuildID
	_, err := s.ApplicationCommandCreate(s.State.User.ID, targetGuild, cmd)
	if err != nil {
		log.Printf("⚠️ Nepodarilo se zaregistrovat slash command: %v", err)
	} else {
		log.Println("⚡ Slash command /overit uspesne zaregistrovan.")
	}
}

func getBaseURL() string {
	parts := strings.Split(cfg.RedirectURI, "/callback")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "http://localhost:" + cfg.Port
}

func handleSlashOverit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	token := createVerificationSession(userID)
	verifyURL := fmt.Sprintf("%s/login?state=%s", getBaseURL(), token)

	embed := &discordgo.MessageEmbed{
		Title: "🎓 Ověření identity FM TUL",
		Description: fmt.Sprintf(
			"Ahoj <@%s>,\n\nPro získání přístupu do fakultních kanálů FM se prosím přihlas svým univerzitním **Microsoft účtem TUL** (`@tul.cz`).\n\nKlikni na tlačítko níže pro zahájení ověření.",
			userID,
		),
		Color: 0x0284c7,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Odkaz je platný 15 minut a je určen pouze pro vás.",
		},
	}

	btn := discordgo.Button{
		Label: "Přihlásit se přes TUL",
		Style: discordgo.LinkButton,
		URL:   verifyURL,
	}

	row := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{btn},
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{row},
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})
}

func handlePrefixOverit(s *discordgo.Session, m *discordgo.MessageCreate) {
	token := createVerificationSession(m.Author.ID)
	verifyURL := fmt.Sprintf("%s/login?state=%s", getBaseURL(), token)

	dmChannel, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Nemohl jsem ti otevřít soukromou zprávu. Povol si prosím příjem DM od členů serveru.")
		return
	}

	msg := fmt.Sprintf("Ahoj! Zde je tvůj privátní odkaz pro ověření identity FM TUL:\n%s\nOdkaz je platný 15 minut.", verifyURL)
	_, _ = s.ChannelMessageSend(dmChannel.ID, msg)
	if m.GuildID != "" {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "Poslal jsem ti odkaz do soukromé zprávy! 📩", m.Reference())
	}
}

func assignDiscordRole(guildID, discordID string, user UserInfo) DiscordResult {
	if dg == nil {
		return DiscordResult{Success: false, Message: "Discord bot neni aktivni."}
	}
	if guildID == "" {
		return DiscordResult{Success: false, Message: "GUILD_ID neni nakonfigurovano."}
	}

	member, err := dg.GuildMember(guildID, discordID)
	if err != nil {
		return DiscordResult{Success: false, Message: "Uživatel zatím není připojen na Discord serveru."}
	}

	roleID := cfg.FMStudentRoleID
	roleName := "Student FM"

	if user.IsStaff && cfg.FMStaffRoleID != "" {
		roleID = cfg.FMStaffRoleID
		roleName = "Zaměstnanec FM"
	}

	if roleID != "" {
		err = dg.GuildMemberRoleAdd(guildID, discordID, roleID)
		if err != nil {
			log.Printf("⚠️ Chyba pri prirazeni role (%s): %v", roleID, err)
		}
	}

	nicknameChanged := false
	if user.Name != "" {
		err := dg.GuildMemberNickname(guildID, discordID, user.Name)
		if err == nil {
			nicknameChanged = true
		}
	}

	return DiscordResult{
		Success:         true,
		RoleName:        roleName,
		MemberName:      member.User.Username,
		NicknameChanged: nicknameChanged,
	}
}

// --- HTTP SERVER & MICROSOFT OAUTH ---

func handleHome(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	botStatus := "Offline / Nenakonfigurován 🟡"
	guildName := "Nenastaveno"

	if dg != nil && dg.State != nil && dg.State.User != nil {
		botStatus = "Online 🟢"
		if cfg.GuildID != "" {
			if g, err := dg.Guild(cfg.GuildID); err == nil && g != nil {
				guildName = g.Name
			}
		}
	}

	loginHref := "/login"
	mockHref := "/mock-login"
	if state != "" {
		loginHref += "?state=" + url.QueryEscape(state)
		mockHref += "?state=" + url.QueryEscape(state)
	}

	tmpl := template.Must(template.New("home").Parse(`
	<!DOCTYPE html>
	<html lang="cs">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>FM TUL Discord Bridge</title>
		<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			* { margin: 0; padding: 0; box-sizing: border-box; font-family: 'Inter', sans-serif; }
			body { background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
			.card { background: #1e293b; border: 1px solid #334155; border-radius: 16px; width: 100%; max-width: 540px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.4); }
			.badge { display: inline-block; padding: 4px 12px; border-radius: 9999px; font-size: 13px; font-weight: 600; margin-bottom: 12px; background: #0284c7; color: white; }
			h1 { font-size: 26px; font-weight: 700; margin-bottom: 12px; }
			p.desc { color: #94a3b8; font-size: 15px; line-height: 1.5; margin-bottom: 24px; }
			.btn { display: flex; align-items: center; justify-content: center; gap: 10px; width: 100%; padding: 14px 20px; border-radius: 10px; font-size: 16px; font-weight: 600; text-decoration: none; cursor: pointer; transition: all 0.2s; border: none; }
			.btn-ms { background: #2563eb; color: white; margin-bottom: 12px; }
			.btn-ms:hover { background: #1d4ed8; transform: translateY(-1px); }
			.btn-mock { background: #334155; color: #e2e8f0; }
			.btn-mock:hover { background: #475569; }
			.status-box { margin-top: 28px; padding: 16px; background: #0f172a; border-radius: 10px; border: 1px solid #1e293b; font-size: 13px; color: #94a3b8; }
			.status-item { display: flex; justify-content: space-between; margin-bottom: 6px; }
			.status-item:last-child { margin-bottom: 0; }
			.status-value { color: #f1f5f9; font-weight: 500; }
		</style>
	</head>
	<body>
		<div class="card">
			<span class="badge">FM TUL • Ověření identity (Go)</span>
			<h1>Fakultní Discord Bridge</h1>
			<p class="desc">
				Pro získání přístupu do interních studentských kanálů Fakulty mechatroniky se přihlaste svým univerzitním Microsoft účtem TUL.
			</p>
			
			<a href="{{.LoginHref}}" class="btn btn-ms">
				<svg width="20" height="20" viewBox="0 0 21 21" fill="none" xmlns="http://www.w3.org/2000/svg">
					<rect x="1" y="1" width="9" height="9" fill="#f25022"/>
					<rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
					<rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
					<rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
				</svg>
				Přihlásit se přes Microsoft TUL
			</a>
			
			<a href="{{.MockHref}}" class="btn btn-mock">
				🧪 Vývojářská simulace (Mock Login)
			</a>
			
			<div class="status-box">
				<div class="status-item">
					<span>Discord Bot:</span>
					<span class="status-value">{{.BotStatus}}</span>
				</div>
				<div class="status-item">
					<span>Discord Server:</span>
					<span class="status-value">{{.GuildName}}</span>
				</div>
				<div class="status-item">
					<span>Session:</span>
					<span class="status-value">{{if .State}}{{.State}}{{else}}Přímo z webu{{end}}</span>
				</div>
			</div>
		</div>
	</body>
	</html>
	`))

	_ = tmpl.Execute(w, map[string]interface{}{
		"LoginHref": loginHref,
		"MockHref":  mockHref,
		"BotStatus": botStatus,
		"GuildName": guildName,
		"State":     state,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if cfg.MicrosoftClientID == "" || cfg.MicrosoftClientID == "SEM_VLOZTE_CLIENT_ID" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`
			<body style="font-family: sans-serif; background: #0f172a; color: #f8fafc; padding: 40px; text-align: center;">
				<h2>⚠️ Microsoft OAuth dosud není nakonfigurován</h2>
				<p style="color: #94a3b8; margin: 16px 0;">V souboru <code>.env</code> je potřeba nastavit <code>MICROSOFT_CLIENT_ID</code> a <code>MICROSOFT_CLIENT_SECRET</code> zadané od LIANE.</p>
				<p>Pro testování funkčnosti můžete využít <a href="/mock-login" style="color: #38bdf8;">Vývojářskou simulaci (Mock Login)</a>.</p>
			</body>
		`))
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		state = createVerificationSession("")
	}

	tenant := cfg.MicrosoftTenantID
	if tenant == "" {
		tenant = "common"
	}

	authEndpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", tenant)
	values := url.Values{
		"client_id":     {cfg.MicrosoftClientID},
		"response_type": {"code"},
		"redirect_uri":  {cfg.RedirectURI},
		"response_mode": {"query"},
		"scope":         {"User.Read openid profile email"},
		"state":         {state},
	}

	targetURL := fmt.Sprintf("%s?%s", authEndpoint, values.Encode())
	http.Redirect(w, r, targetURL, http.StatusFound)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errCode := q.Get("error"); errCode != "" {
		errDesc := q.Get("error_description")
		renderResultPage(w, false, "Přihlášení bylo zamítnuto",
			fmt.Sprintf("Chyba při přihlašování: %s %s", errCode, errDesc),
			"Pokud nejste studentem či zaměstnancem FM, přístup do interních sekcí není povolen.",
			nil,
		)
		return
	}

	code := q.Get("code")
	if code == "" {
		http.Error(w, "Chybí autorizační kód (code).", http.StatusBadRequest)
		return
	}
	state := q.Get("state")

	tenant := cfg.MicrosoftTenantID
	if tenant == "" {
		tenant = "common"
	}
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)

	form := url.Values{
		"client_id":     {cfg.MicrosoftClientID},
		"client_secret": {cfg.MicrosoftClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURI},
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil || resp.StatusCode != http.StatusOK {
		renderResultPage(w, false, "Chyba při získávání tokenu",
			"Nepodařilo se ověřit autorizační kód vůči Microsoft TUL.",
			"Zkontrolujte prosím připojení a platnost Client Secret.",
			nil,
		)
		return
	}
	defer resp.Body.Close()

	var tokenResult struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResult); err != nil || tokenResult.AccessToken == "" {
		renderResultPage(w, false, "Chyba tokenu", "Odpověď Microsoftu neobsahuje přístupový token.", "", nil)
		return
	}

	// Volani Microsoft Graph API /v1.0/me
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	graphResp, err := client.Do(req)
	if err != nil || graphResp.StatusCode != http.StatusOK {
		renderResultPage(w, false, "Chyba profilu", "Nepodařilo se načíst profil z Microsoft Graph API.", "", nil)
		return
	}
	defer graphResp.Body.Close()

	bodyBytes, _ := io.ReadAll(graphResp.Body)
	var profile struct {
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		JobTitle          string `json:"jobTitle"`
	}
	_ = json.Unmarshal(bodyBytes, &profile)

	email := profile.Mail
	if email == "" {
		email = profile.UserPrincipalName
	}

	isStaff := profile.JobTitle != "" || strings.Contains(strings.ToLower(email), "zamestnanec")
	user := UserInfo{
		Name:     profile.DisplayName,
		Email:    email,
		IsStaff:  isStaff,
		JobTitle: profile.JobTitle,
		Faculty:  "FM",
	}

	var discordRes *DiscordResult
	if sess, ok := consumeVerificationSession(state); ok && sess.DiscordID != "" {
		res := assignDiscordRole(cfg.GuildID, sess.DiscordID, user)
		discordRes = &res
	}

	renderResultPage(w, true, "Ověření proběhlo úspěšně! 🎉",
		fmt.Sprintf("Vítej, <strong>%s</strong> (%s).", user.Name, user.Email),
		"Tvoje univerzitní identita FM TUL byla úspěšně ověřena.",
		discordRes,
	)
}

func handleMockLogin(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	user := UserInfo{
		Name:     "Jakub Marcinka",
		Email:    "jakub.marcinka@tul.cz",
		IsStaff:  false,
		JobTitle: "",
		Faculty:  "FM",
	}

	var discordRes *DiscordResult
	if sess, ok := consumeVerificationSession(state); ok && sess.DiscordID != "" {
		res := assignDiscordRole(cfg.GuildID, sess.DiscordID, user)
		discordRes = &res
	}

	renderResultPage(w, true, "Vývojářské ověření (Mock) úspěšné 🧪",
		fmt.Sprintf("Simulováno přihlášení studenta: <strong>%s</strong> (%s).", user.Name, user.Email),
		"Toto je simulovaný režim pro lokální testování.",
		discordRes,
	)
}

func renderResultPage(w http.ResponseWriter, success bool, title, message, details string, discordResult *DiscordResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !success {
		w.WriteHeader(http.StatusBadRequest)
	}

	icon := "✅"
	if !success {
		icon = "❌"
	}

	discordBoxHTML := ""
	if discordResult != nil {
		if discordResult.Success {
			discordBoxHTML = fmt.Sprintf(`
				<div style="background: #0f172a; border-radius: 10px; padding: 16px; margin-top: 20px; border-left: 4px solid #10b981; text-align: left;">
					<p style="color: #f8fafc; font-weight: 600; margin-bottom: 4px;">Účet na Discordu spárován!</p>
					<p style="color: #94a3b8; font-size: 14px;">Byla vám přidělena role: <span style="color: #38bdf8; font-weight: 600;">%s</span>.</p>
					<p style="color: #94a3b8; font-size: 13px; margin-top: 6px;">Můžete se vrátit zpět do aplikace Discord.</p>
				</div>
			`, discordResult.RoleName)
		} else {
			discordBoxHTML = fmt.Sprintf(`
				<div style="background: #0f172a; border-radius: 10px; padding: 16px; margin-top: 20px; border-left: 4px solid #eab308; text-align: left;">
					<p style="color: #f8fafc; font-weight: 600; margin-bottom: 4px;">Upozornění k Discord účtu:</p>
					<p style="color: #94a3b8; font-size: 14px;">%s</p>
				</div>
			`, discordResult.Message)
		}
	}

	tmpl := template.Must(template.New("res").Parse(`
	<!DOCTYPE html>
	<html lang="cs">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>{{.Title}} - FM TUL</title>
		<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
		<style>
			* { margin: 0; padding: 0; box-sizing: border-box; font-family: 'Inter', sans-serif; }
			body { background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
			.card { background: #1e293b; border: 1px solid #334155; border-radius: 16px; width: 100%; max-width: 520px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.4); text-align: center; }
			.icon { font-size: 48px; margin-bottom: 16px; }
			h1 { font-size: 24px; font-weight: 700; margin-bottom: 12px; color: #f8fafc; }
			p.msg { color: #cbd5e1; font-size: 16px; line-height: 1.5; margin-bottom: 16px; }
			p.details { color: #94a3b8; font-size: 14px; margin-bottom: 24px; }
			a.btn { display: inline-block; padding: 12px 24px; background: #2563eb; color: white; border-radius: 8px; text-decoration: none; font-weight: 600; font-size: 15px; }
			a.btn:hover { background: #1d4ed8; }
		</style>
	</head>
	<body>
		<div class="card">
			<div class="icon">{{.Icon}}</div>
			<h1>{{.Title}}</h1>
			<p class="msg">{{.Message}}</p>
			{{if .Details}}<p class="details">{{.Details}}</p>{{end}}
			{{.DiscordBox}}
			<div style="margin-top: 24px;">
				<a href="/" class="btn">Zpět na hlavní stránku</a>
			</div>
		</div>
	</body>
	</html>
	`))

	_ = tmpl.Execute(w, map[string]interface{}{
		"Title":      title,
		"Icon":       icon,
		"Message":    template.HTML(message),
		"Details":    details,
		"DiscordBox": template.HTML(discordBoxHTML),
	})
}

// --- HLAVNI SPUSTENI ---

func main() {
	_ = godotenv.Load()

	cfg = Config{
		MicrosoftClientID:     os.Getenv("MICROSOFT_CLIENT_ID"),
		MicrosoftClientSecret: os.Getenv("MICROSOFT_CLIENT_SECRET"),
		MicrosoftTenantID:     os.Getenv("MICROSOFT_TENANT_ID"),
		RedirectURI:           os.Getenv("REDIRECT_URI"),
		DiscordToken:          os.Getenv("DISCORD_TOKEN"),
		GuildID:               os.Getenv("GUILD_ID"),
		FMStudentRoleID:       os.Getenv("FM_STUDENT_ROLE_ID"),
		FMStaffRoleID:         os.Getenv("FM_STAFF_ROLE_ID"),
		Host:                  os.Getenv("HOST"),
		Port:                  os.Getenv("PORT"),
	}

	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == "" {
		cfg.Port = "8000"
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = "http://localhost:" + cfg.Port + "/callback"
	}

	// Spusteni cistice starych session na pozadi
	go cleanExpiredSessions()

	// Inicializace a spusteni Discord bota
	var err error
	dg, err = setupDiscordBot()
	if err != nil {
		log.Printf("⚠️ Varování při startu Discord bota: %v", err)
	}

	// Registrace HTTP handleru
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleHome)
	mux.HandleFunc("GET /login", handleLogin)
	mux.HandleFunc("GET /callback", handleCallback)
	mux.HandleFunc("GET /mock-login", handleMockLogin)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("🚀 Webový server FM TUL Bridge (Go) běží na http://%s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Chyba HTTP serveru: %v", err)
		}
	}()

	// Graceful shutdown pri SIGINT nebo SIGTERM
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
