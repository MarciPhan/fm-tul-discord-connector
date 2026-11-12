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
	"sync"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"
	"sbibolet/internal/discord"
	"sbibolet/internal/msalauth"
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

	// Nespotrebovane hodnoty znovu pouzijeme, aby refresh nebo druha zalozka
	// nezneplatnila odkazy v uz otevrene strance
	msState, verifier, discordState := sess.PendingAuth()
	if msState == "" || verifier == "" {
		msState = security.GenerateStateToken()
		verifier, _ = security.GeneratePKCE()
		sess.SetMSALAuth(msState, verifier)
	}
	challenge := security.PKCEChallenge(verifier)
	if discordState == "" {
		discordState = security.GenerateStateToken()
		sess.SetDiscordState(discordState)
	}

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
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	if config.Cfg.MSALClientKeyPath != "" {
		signer, err := msalSigner()
		if err != nil {
			log.Printf("MSAL client assertion: %v", err)
			http.Error(w, "Chyba konfigurace přihlášení k Microsoft TUL.", http.StatusInternalServerError)
			return
		}
		assertion, err := signer.Assertion(tokenURL)
		if err != nil {
			log.Printf("MSAL client assertion: %v", err)
			http.Error(w, "Chyba konfigurace přihlášení k Microsoft TUL.", http.StatusInternalServerError)
			return
		}
		form.Set("client_assertion_type", msalauth.AssertionType)
		form.Set("client_assertion", assertion)
	} else {
		form.Set("client_secret", config.Cfg.MSALClientSecret)
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		log.Printf("MSAL Token error: %v", err)
		http.Error(w, "Chyba při ověřování přihlášení u Microsoft TUL.", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("MSAL Token error: status %d", resp.StatusCode)
		http.Error(w, "Chyba při ověřování přihlášení u Microsoft TUL.", http.StatusBadGateway)
		return
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil || tokenRes.AccessToken == "" {
		http.Error(w, "Neplatná odpověď z Microsoft TUL.", http.StatusBadGateway)
		return
	}

	// 3. Volani Graph API /v1.0/me
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://graph.microsoft.com/v1.0/me?$select=id,displayName,mail,userPrincipalName,jobTitle,department,employeeType", nil)
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenRes.TokenType, tokenRes.AccessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	gResp, gErr := client.Do(req)
	if gErr != nil {
		log.Printf("Graph API error: %v", gErr)
		http.Error(w, "Nepodařilo se načíst profil z Microsoft Graph API.", http.StatusBadGateway)
		return
	}
	defer gResp.Body.Close()
	if gResp.StatusCode != http.StatusOK {
		log.Printf("Graph API error: status %d", gResp.StatusCode)
		http.Error(w, "Nepodařilo se načíst profil z Microsoft Graph API.", http.StatusBadGateway)
		return
	}

	var profile struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		JobTitle          string `json:"jobTitle"`
		Department        string `json:"department"`
		EmployeeType      string `json:"employeeType"`
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

	tokenRoles := security.IDTokenRoles(tokenRes.IDToken)
	role := security.DetermineRole(tokenRoles, profile.JobTitle, profile.EmployeeType, email)
	log.Printf("MS profil %s: roles=%v jobTitle=%q employeeType=%q department=%q -> %s", email, tokenRoles, profile.JobTitle, profile.EmployeeType, profile.Department, role)

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
	if err != nil {
		log.Printf("Discord OAuth token exchange selhal: %v", err)
		http.Error(w, "Chyba při komunikaci s Discord OAuth.", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Discord OAuth token exchange selhal: status %d", resp.StatusCode)
		http.Error(w, "Chyba při komunikaci s Discord OAuth.", http.StatusBadGateway)
		return
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil || tokenRes.AccessToken == "" {
		log.Printf("Discord OAuth: neplatná odpověď tokenu: %v", err)
		http.Error(w, "Neplatná odpověď z Discord OAuth.", http.StatusBadGateway)
		return
	}

	// 3. Nacteni Discord profilu (@me)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://discord.com/api/v10/users/@me", nil)
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenRes.TokenType, tokenRes.AccessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	uResp, uErr := client.Do(req)
	if uErr != nil {
		log.Printf("Discord users/@me error: %v", uErr)
		http.Error(w, "Nepodařilo se načíst profil z Discord API.", http.StatusBadGateway)
		return
	}
	defer uResp.Body.Close()
	if uResp.StatusCode != http.StatusOK {
		log.Printf("Discord users/@me error: status %d", uResp.StatusCode)
		http.Error(w, "Nepodařilo se načíst profil z Discord API.", http.StatusBadGateway)
		return
	}

	var discordUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(uResp.Body).Decode(&discordUser); err != nil || discordUser.ID == "" {
		log.Printf("Discord OAuth: neplatný uživatelský profil: %v", err)
		http.Error(w, "Nepodařilo se načíst Discord profil.", http.StatusBadGateway)
		return
	}

	student.DiscordID = discordUser.ID

	// 4. Kontrola striktni vazby 1:1 (Anti-Multi-Accounting)
	if err := storage.CheckBindingAllowed(student); err != nil {
		log.Printf("Zamítnuto vícenásobné spárování účtu: %v", err)
		audit.Log(audit.LevelDanger, "Zablokován Multi-Accounting", fmt.Sprintf("Uživatel **%s** (%s) se pokusil ověřit více Discord účtů.", student.Name, student.Email))
		http.Error(w, fmt.Sprintf("Bezpečnostní omezení: %v", err), http.StatusConflict)
		return
	}

	// 5. Automaticke pripojeni na server a prirazeni roli
	err = discord.PerformJoinAndRole(discordUser.ID, tokenRes.AccessToken, student)
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

const indexTemplate = `<!DOCTYPE html>
<html lang="cs">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<meta name="color-scheme" content="light">
	<title>Ověření pro Discord FM TUL</title>
	<link rel="preconnect" href="https://fonts.googleapis.com">
	<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
	<style>
		:root {
			--paper: #f7f4ef;
			--ink: #1b1a18;
			--muted: #5e5a54;
			--rule: #d8d1c6;
			--orange: #e8751a;
			--orange-ink: #a64705;
			--discord: #4752c4;
			--discord-hover: #3b45ab;
		}
		* { margin: 0; padding: 0; box-sizing: border-box; }
		html { -webkit-text-size-adjust: 100%; }
		body {
			background: var(--paper);
			color: var(--ink);
			font-family: 'IBM Plex Sans', system-ui, sans-serif;
			font-size: 17px;
			line-height: 1.55;
			min-height: 100vh;
			display: flex;
			flex-direction: column;
			border-top: 8px solid var(--orange);
		}
		::selection { background: var(--orange); color: var(--ink); }
		a { color: inherit; text-underline-offset: 0.2em; }
		:focus-visible { outline: 3px solid var(--ink); outline-offset: 3px; border-radius: 2px; }

		.wrap { width: 100%; max-width: 44rem; margin: 0 auto; padding: 0 1.25rem; }
		header { padding: 1.75rem 0 0; }
		.org { font-size: 0.9rem; color: var(--muted); line-height: 1.4; }
		.org strong { display: block; color: var(--ink); font-weight: 600; }

		main { flex: 1; padding: 3.5rem 0 3rem; }
		h1 {
			font-size: clamp(2rem, 6vw, 3.1rem);
			line-height: 1.08;
			font-weight: 700;
			letter-spacing: -0.02em;
			text-wrap: balance;
			max-width: 18ch;
		}
		.lead { margin-top: 1.1rem; max-width: 34rem; color: var(--muted); text-wrap: pretty; }

		ol.steps { list-style: none; margin-top: 3rem; border-top: 1px solid var(--rule); }
		.step { display: grid; grid-template-columns: 2.75rem 1fr; gap: 0 0.5rem; padding: 1.5rem 0; border-bottom: 1px solid var(--rule); }
		.mark {
			width: 1.9rem; height: 1.9rem; border-radius: 50%;
			border: 2px solid var(--rule); color: var(--muted);
			display: grid; place-items: center;
			font-size: 0.9rem; font-weight: 600; font-variant-numeric: tabular-nums;
			margin-top: 0.1rem;
		}
		.step h2 { font-size: 1.2rem; line-height: 1.3; font-weight: 600; }
		.step p { margin-top: 0.3rem; color: var(--muted); font-size: 0.98rem; max-width: 32rem; }
		.step.todo h2 { color: var(--muted); font-weight: 500; }
		.step.current .mark { border-color: var(--ink); background: var(--ink); color: var(--paper); }
		.step.done .mark { border-color: var(--orange); background: var(--orange); color: var(--ink); }

		.who { margin-top: 0.6rem; font-size: 0.98rem; }
		.who b { font-weight: 600; }
		.who span { color: var(--orange-ink); font-weight: 600; }

		.btn {
			display: inline-flex; align-items: center; gap: 0.7rem;
			margin-top: 1.1rem; padding: 0.8rem 1.4rem;
			border-radius: 4px; border: 2px solid transparent;
			font: inherit; font-size: 1rem; font-weight: 600; line-height: 1.2;
			text-decoration: none; cursor: pointer;
			transition: background-color 0.15s ease-out, border-color 0.15s ease-out, transform 0.15s ease-out;
		}
		.btn:active { transform: translateY(1px); }
		.btn svg { flex: none; }
		.btn-ms { background: var(--ink); color: var(--paper); }
		.btn-ms:hover { background: #000; }
		.btn-discord { background: var(--discord); color: #fff; }
		.btn-discord:hover { background: var(--discord-hover); }
		.btn-done { background: var(--orange); color: var(--ink); }
		.btn-done:hover { background: #f08a3a; }

		footer { border-top: 1px solid var(--rule); padding: 1.25rem 0 2rem; font-size: 0.85rem; color: var(--muted); }
		footer .wrap { display: flex; flex-wrap: wrap; gap: 0.5rem 1.5rem; justify-content: space-between; }
		footer p { max-width: 34rem; }
		footer a:hover { color: var(--ink); }

		@media (max-width: 30rem) {
			body { font-size: 16px; }
			main { padding-top: 2.5rem; }
			.btn { width: 100%; justify-content: center; }
		}
		@media (prefers-reduced-motion: reduce) { .btn { transition: none; } }
	</style>
</head>
<body>
	<header>
		<div class="wrap">
			<p class="org"><strong>Fakulta mechatroniky, informatiky a mezioborových studií</strong>Technická univerzita v Liberci</p>
		</div>
	</header>

	<main>
		<div class="wrap">
			<h1>Ověření pro Discord FM TUL</h1>
			<p class="lead">Přihlas se školním účtem, propoj ho s Discordem a na fakultním serveru dostaneš své role. Stačí to udělat jednou.</p>

			<ol class="steps">
				<li class="step {{if .Session}}done{{else}}current{{end}}">
					<span class="mark" aria-hidden="true">{{if .Session}}<svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 7.5l3 3 6-7"/></svg>{{else}}1{{end}}</span>
					<div>
						<h2>Školní účet</h2>
						{{if not .Session}}
							<p>Přihlas se účtem ve tvaru jmeno.prijmeni@tul.cz.</p>
							<a href="{{.MSURL}}" class="btn btn-ms">
								<svg width="18" height="18" viewBox="0 0 21 21" aria-hidden="true">
									<rect x="1" y="1" width="9" height="9" fill="#f25022"/>
									<rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
									<rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
									<rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
								</svg>
								Přihlásit se přes Microsoft TUL
							</a>
						{{else}}
							<p class="who"><b>{{.Session.Name}}</b> ({{.Session.Email}}) &middot; <span>{{.Session.Role}}</span></p>
						{{end}}
					</div>
				</li>

				<li class="step {{if not .Session}}todo{{else if .Session.DiscordID}}done{{else}}current{{end}}">
					<span class="mark" aria-hidden="true">{{if and .Session .Session.DiscordID}}<svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 7.5l3 3 6-7"/></svg>{{else}}2{{end}}</span>
					<div>
						<h2>Discord</h2>
						{{if and .Session (not .Session.DiscordID)}}
							<p>Propojíme účet s tvým Discordem a přidáme tě na server.</p>
							<a href="{{.DiscordURL}}" class="btn btn-discord">
								<svg width="20" height="16" viewBox="0 0 127.14 96.36" fill="currentColor" aria-hidden="true">
									<path d="M107.7,8.07A105.15,105.15,0,0,0,81.47,0a72.06,72.06,0,0,0-3.36,6.83A97.68,97.68,0,0,0,49,6.83,72.37,72.37,0,0,0,45.64,0,105.89,105.89,0,0,0,19.39,8.09C2.79,32.65-1.71,56.6.54,80.21h0A105.73,105.73,0,0,0,32.71,96.36,77.7,77.7,0,0,0,39.6,85.25a68.42,68.42,0,0,1-10.85-5.18c.91-.66,1.8-1.34,2.66-2a75.57,75.57,0,0,0,64.32,0c.87.71,1.76,1.39,2.66,2a68.68,68.68,0,0,1-10.87,5.19,77,77,0,0,0,6.89,11.1A105.25,105.25,0,0,0,126.6,80.22h0C129.24,52.84,122.09,29.11,107.7,8.07ZM42.45,65.69C36.18,65.69,31,60,31,53s5-12.74,11.43-12.74S54,45.91,53.89,53,48.84,65.69,42.45,65.69Zm42.24,0C78.41,65.69,73.25,60,73.25,53s5-12.74,11.44-12.74S96.23,45.91,96.12,53,91.08,65.69,84.69,65.69Z"/>
								</svg>
								Propojit s Discordem
							</a>
						{{else if .Session}}
							<p class="who">Účet je propojený.</p>
						{{end}}
					</div>
				</li>

				<li class="step {{if and .Session .Session.DiscordID}}current{{else}}todo{{end}}">
					<span class="mark" aria-hidden="true">3</span>
					<div>
						<h2>Hotovo</h2>
						{{if and .Session .Session.DiscordID}}
							<p>Role ti byly přiděleny automaticky. Fakultní kanály se ti odemkly.</p>
							<a href="{{if .InviteURL}}{{.InviteURL}}{{else}}https://discord.com/app{{end}}" class="btn btn-done">Otevřít server na Discordu</a>
						{{end}}
					</div>
				</li>
			</ol>
		</div>
	</main>

	<footer>
		<div class="wrap">
			<p>Ukládáme jméno, školní e-mail, roli (student nebo zaměstnanec) a ID tvého Discord účtu. Jeden školní účet jde propojit s jedním Discordem.</p>
			<p>
				{{if .EnableDevMock}}<a href="/mock-msal">Simulovat MS</a> &middot; <a href="/mock-discord">Simulovat Discord</a>{{end}}
				{{if .Session}}{{if .EnableDevMock}} &middot; {{end}}<a href="/logout">Odhlásit se</a>{{end}}
			</p>
		</div>
	</footer>
</body>
</html>
`

var (
	signerOnce sync.Once
	signer     *msalauth.Signer
	signerErr  error
)

// msalSigner nacte privatni klic pri prvnim pouziti a drzi ho v pameti
func msalSigner() (*msalauth.Signer, error) {
	signerOnce.Do(func() {
		c := config.Cfg
		signer, signerErr = msalauth.NewSigner(c.MSALClientID, c.MSALClientKeyPath, c.MSALClientCertPath, c.MSALClientThumbprint)
	})
	return signer, signerErr
}
