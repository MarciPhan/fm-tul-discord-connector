package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"
	"sbibolet/internal/discord"
	"sbibolet/internal/security"
	"sbibolet/internal/session"
	"sbibolet/internal/storage"
)

// isValidDiscordInvite ověří, že zadaná URL je validní Discord invite (prevence Open Redirect)
func isValidDiscordInvite(rawURL string) bool {
	if rawURL == "" || rawURL == "https://discord.gg/vase-pozvanka" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "discord.gg" || host == "discord.com" || strings.HasSuffix(host, ".discord.com")
}

// --- OAUTH2 URL GENEROVANI ---

func getMicrosoftLoginURL(origin string, state, challenge string) string {
	tenant := config.Cfg.MSALTenantID
	if tenant == "" {
		tenant = "common"
	}
	redirectURI := fmt.Sprintf("%s/msal", origin)
	values := url.Values{
		"client_id":             {config.Cfg.MSALClientID},
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
		"client_id":     {config.Cfg.DiscordClientID},
		"response_type": {"code"},
		"scope":         {"identify guilds.join"},
		"redirect_uri":  {redirectURI},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return fmt.Sprintf("https://discord.com/oauth2/authorize?%s", values.Encode())
}

// --- HLAVNI STRANKA ---

func HandleIndex(w http.ResponseWriter, r *http.Request) {
	sess := session.GetOrCreate(w, r)
	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}

	// Vygenerujeme novy kryptograficky state a PKCE pro Microsoft krok
	msState := security.GenerateStateToken()
	verifier, challenge := security.GeneratePKCE()
	sess.SetMSALAuth(msState, verifier)

	// Vygenerujeme state pro Discord krok
	discordState := security.GenerateStateToken()
	sess.SetDiscordState(discordState)

	msURL := getMicrosoftLoginURL(origin, msState, challenge)
	discordURL := getDiscordLoginURL(origin, discordState)

	data := map[string]interface{}{
		"Session":       sess.GetStudent(),
		"MSURL":         msURL,
		"DiscordURL":    discordURL,
		"InviteURL":     config.Cfg.DiscordInviteURL,
		"EnableDevMock": config.Cfg.EnableDevMock,
	}

	tmpl := template.Must(template.New("index").Parse(indexTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

// --- MICROSOFT OAUTH2 CALLBACK ---

func HandleMSAL(w http.ResponseWriter, r *http.Request) {
	ip := security.GetClientIP(r)
	if !security.CheckRateLimit("oauth_msal_"+ip, 10, time.Minute) {
		http.Error(w, "Příliš mnoho pokusů o přihlášení. Počkejte chvíli.", http.StatusTooManyRequests)
		return
	}

	sess := session.GetExisting(r)
	if sess == nil {
		http.Error(w, "Neplatná nebo expirovaná relace. Zkuste to znovu z hlavní stránky.", http.StatusBadRequest)
		return
	}

	// 1. Striktni validace CSRF parametru state s atomickym spotrebovanim
	reqState := r.URL.Query().Get("state")
	verifier, ok := sess.ConsumeMSALState(reqState)
	if !ok {
		log.Printf("Bezpečnostní varování: Neplatný MSAL state parametr od IP %s", ip)
		http.Error(w, "Neplatný bezpečnostní token (možný pokus o CSRF útok nebo opakovaný požadavek).", http.StatusForbidden)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	tenant := config.Cfg.MSALTenantID
	if tenant == "" {
		tenant = "common"
	}

	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	redirectURI := fmt.Sprintf("%s/msal", origin)

	// 2. Vymena kodu za token s PKCE code_verifier
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)
	form := url.Values{
		"client_id":     {config.Cfg.MSALClientID},
		"client_secret": {config.Cfg.MSALClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("MSAL Token error: status %v, err: %v", resp, err)
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
	if !security.IsValidTULEmail(email) {
		log.Printf("Zamítnuto neoprávněné přihlášení s emailem: %s", email)
		http.Error(w, "Přístup povolen pouze s univerzitním účtem TUL (@tul.cz).", http.StatusForbidden)
		return
	}

	role := security.DetermineRole(profile.JobTitle, email)

	sess.SetStudent(&storage.Student{
		MicrosoftID: profile.ID,
		Name:        profile.DisplayName,
		Email:       email,
		Faculty:     "FM",
		Role:        role,
		VerifiedAt:  time.Now(),
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

// --- DISCORD OAUTH2 CALLBACK ---

func HandleDiscord(w http.ResponseWriter, r *http.Request) {
	ip := security.GetClientIP(r)
	if !security.CheckRateLimit("oauth_discord_"+ip, 10, time.Minute) {
		http.Error(w, "Příliš mnoho pokusů o propojení Discordu. Počkejte chvíli.", http.StatusTooManyRequests)
		return
	}

	sess := session.GetExisting(r)
	if sess == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	student := sess.GetStudent()
	if student == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// 1. Striktni validace CSRF state parametru s atomickym spotrebovanim
	reqState := r.URL.Query().Get("state")
	if !sess.ConsumeDiscordState(reqState) {
		log.Printf("Bezpečnostní varování: Neplatný Discord state parametr od IP %s", ip)
		http.Error(w, "Neplatný bezpečnostní token (možný pokus o CSRF útok nebo opakovaný požadavek).", http.StatusForbidden)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	redirectURI := fmt.Sprintf("%s/discord", origin)

	// 2. Vymena Discord OAuth2 kodu za Access Token
	form := url.Values{
		"client_id":     {config.Cfg.DiscordClientID},
		"client_secret": {config.Cfg.DiscordClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm("https://discord.com/api/v10/oauth2/token", form)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Discord OAuth token exchange selhal: %v", err)
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

	student.DiscordID = discordUser.ID

	// 4. Kontrola striktni vazby 1:1 (Anti-Multi-Accounting)
	if err := storage.CheckBindingAllowed(student); err != nil {
		log.Printf("Zamítnuto vícenásobné spárování účtu: %v", err)
		audit.Log(audit.LevelDanger, "Zablokován Multi-Accounting", fmt.Sprintf("Uživatel **%s** (%s) se pokusil ověřit více Discord účtů.", student.Name, student.Email))
		http.Error(w, fmt.Sprintf("Bezpečnostní omezení: %v", err), http.StatusConflict)
		return
	}

	// 5. Automaticke pripojeni na server a prirazeni roli
	err = discord.PerformJoinAndRoleDirect(discordUser.ID, tokenRes.AccessToken, student)
	if err != nil {
		log.Printf("Chyba při přiřazení rolí na Discordu: %v", err)
	}

	// 6. Bezpecne ulozeni
	if err := storage.SaveUser(student); err != nil {
		log.Printf("Chyba při ukládání uživatele: %v", err)
		http.Error(w, "Chyba při ukládání registrace.", http.StatusInternalServerError)
		return
	}

	sess.SetStudent(student)

	log.Printf("Úspěšně ověřen a spárován: %s (%s) <-> Discord ID: %s", student.Name, student.Email, student.DiscordID)
	audit.Log(audit.LevelSuccess, "Uživatel ověřen", fmt.Sprintf("Identita **%s** (%s) spárována s účtem <@%s>.", student.Name, student.Email, student.DiscordID))

	if isValidDiscordInvite(config.Cfg.DiscordInviteURL) {
		http.Redirect(w, r, config.Cfg.DiscordInviteURL, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// --- MOCK ENDPOINTY ---

func HandleMockMSAL(w http.ResponseWriter, r *http.Request) {
	if !config.Cfg.EnableDevMock {
		http.NotFound(w, r)
		return
	}
	sess := session.GetOrCreate(w, r)
	sess.SetStudent(&storage.Student{
		MicrosoftID: "mock-ms-id-12345",
		Name:        "Jakub Marcinka",
		Email:       "jakub.marcinka@tul.cz",
		Faculty:     "FM",
		Role:        "Student FM",
		VerifiedAt:  time.Now(),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func HandleMockDiscord(w http.ResponseWriter, r *http.Request) {
	if !config.Cfg.EnableDevMock {
		http.NotFound(w, r)
		return
	}
	sess := session.GetOrCreate(w, r)
	student := sess.GetStudent()
	if student == nil {
		student = &storage.Student{
			MicrosoftID: "mock-ms-id-12345",
			Name:        "Jakub Marcinka",
			Email:       "jakub.marcinka@tul.cz",
			Faculty:     "FM",
			Role:        "Student FM",
			VerifiedAt:  time.Now(),
		}
	}
	student.DiscordID = "mock-discord-id-98765"
	_ = storage.SaveUser(student)
	sess.SetStudent(student)
	http.Redirect(w, r, "/", http.StatusFound)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	session.Clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// --- HTML TEMPLATE ---

const indexTemplate = `
	<!DOCTYPE html>
	<html lang="cs">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>FM TUL - Discord Connector</title>
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
					<div class="info-value">{{.Session.Name}} ({{.Session.Email}})</div>
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
					<div class="info-value">{{.Session.Name}}</div>
					<div style="color: #38bdf8; font-size: 13px; margin-top: 4px;">Role: {{.Session.Role}}</div>
					<div style="color: #94a3b8; font-size: 13px; margin-top: 2px;">Role na Discordu byly automaticky uděleny.</div>
				</div>

				<a href="{{if .InviteURL}}{{.InviteURL}}{{else}}https://discord.com/app{{end}}" class="btn btn-success">
					Přejít na Discord Server
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
	`
