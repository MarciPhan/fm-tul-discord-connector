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

// getDiscordLoginURL: prompt=none znamena, ze kdo aplikaci uz jednou schvalil, souhlas nevidi;
// prvni schvaleni Discord vyzaduje vzdy.
func getDiscordLoginURL(origin string, state string) string {
	redirectURI := fmt.Sprintf("%s/discord", origin)
	values := url.Values{
		"client_id":     {config.Cfg.DiscordClientID},
		"response_type": {"code"},
		"scope":         {"identify guilds.join"},
		"redirect_uri":  {redirectURI},
		"prompt":        {"none"},
		"state":         {state},
	}
	return fmt.Sprintf("https://discord.com/oauth2/authorize?%s", values.Encode())
}

// programForRole: zamestnanec obor nevybira ("-"), student ho jeste musi vybrat ("")
func programForRole(role string) string {
	if role == "Zaměstnanec FM" {
		return "-"
	}
	return ""
}

// HandleProgram ulozi vybrany obor a pokracuje na Discord
func HandleProgram(w http.ResponseWriter, r *http.Request) {
	sess := session.GetExisting(r)
	if sess == nil || sess.GetStudent() == nil {
		http.Redirect(w, r, "/go", http.StatusFound)
		return
	}
	program := r.URL.Query().Get("p")
	switch program {
	case "ME", "IT", "AVI", "-":
	default:
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	st := sess.GetStudent()
	st.Program = program
	sess.SetStudent(st)

	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	discordState := security.GenerateStateToken()
	sess.SetDiscordState(discordState)
	http.Redirect(w, r, getDiscordLoginURL(origin, discordState), http.StatusFound)
}

// HandleAlumni: absolvent uz skolni ucet nema, proto jde rovnou na Discord a dostane jen roli Absolvent FM.
// Totoznost se neoveruje, role proto otevira jen kanaly pro absolventy.
func HandleAlumni(w http.ResponseWriter, r *http.Request) {
	sess := session.GetOrCreate(w, r)
	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	sess.SetStudent(&storage.Student{
		Faculty:    "FM",
		Role:       "Absolvent FM",
		Program:    "-",
		VerifiedAt: time.Now(),
	})
	discordState := security.GenerateStateToken()
	sess.SetDiscordState(discordState)
	http.Redirect(w, r, getDiscordLoginURL(origin, discordState), http.StatusFound)
}

// --- RYCHLY VSTUP (/go) ---

// HandleStart je vstup z tlacitek na Discordu: bez uvodni stranky rovnou presmeruje
// na Microsoft (nebo, kdyz uz je clovek prihlaseny skolnim uctem, na Discord).
func HandleStart(w http.ResponseWriter, r *http.Request) {
	sess := session.GetOrCreate(w, r)

	// Tlacitko na Discordu nese volbu: ME/IT/AVI = obor studenta, ABS = absolvent bez skolniho uctu
	switch choice := r.URL.Query().Get("p"); choice {
	case "ABS":
		http.Redirect(w, r, "/absolvent", http.StatusFound)
		return
	case "ME", "IT", "AVI":
		sess.SetPendingProgram(choice)
	}
	origin := config.Cfg.BaseURL
	if origin == "" {
		origin = "http://" + r.Host
	}

	if st := sess.GetStudent(); st != nil {
		if st.DiscordID != "" {
			http.Redirect(w, r, "/", http.StatusFound) // uz overen, stranka ukaze odkaz na server
			return
		}
		if st.Program == "" {
			st.Program = sess.GetPendingProgram()
		}
		if st.Program == "" {
			http.Redirect(w, r, "/", http.StatusFound) // jeste nevybral obor
			return
		}
		discordState := security.GenerateStateToken()
		sess.SetDiscordState(discordState)
		http.Redirect(w, r, getDiscordLoginURL(origin, discordState), http.StatusFound)
		return
	}

	msState, verifier, _ := sess.PendingAuth()
	if msState == "" || verifier == "" {
		msState = security.GenerateStateToken()
		verifier, _ = security.GeneratePKCE()
		sess.SetMSALAuth(msState, verifier)
	}
	http.Redirect(w, r, getMicrosoftLoginURL(origin, msState, security.PKCEChallenge(verifier)), http.StatusFound)
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

	serverURL := config.Cfg.DiscordInviteURL
	if st := sess.GetStudent(); st != nil && st.DiscordID != "" {
		serverURL = discord.ServerLink(st.DiscordID, serverURL)
	}

	data := map[string]interface{}{
		"Session":       sess.GetStudent(),
		"ServerURL":     serverURL,
		"MSURL":         msURL,
		"DiscordURL":    discordURL,
		"InviteURL":     config.Cfg.DiscordInviteURL,
		"NeedProgram":   sess.GetStudent() != nil && sess.GetStudent().DiscordID == "" && sess.GetStudent().Program == "",
		"EnableDevMock": config.Cfg.EnableDevMock,
	}
	data["Cmd"], data["Channels"] = terminalLines(randomChannels(channelsShown))

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
	program := programForRole(role) // zamestnanec "-", student ""
	if program == "" {
		program = sess.GetPendingProgram() // obor z tlacitka na Discordu
	}
	log.Printf("MS profil %s: roles=%v jobTitle=%q employeeType=%q department=%q -> %s", email, tokenRoles, profile.JobTitle, profile.EmployeeType, profile.Department, role)

	sess.SetStudent(&storage.Student{
		MicrosoftID: profile.ID,
		Name:        profile.DisplayName,
		Email:       email,
		Faculty:     "FM",
		Role:        role,
		VerifiedAt:  time.Now(),
		Program:     program,
	})

	// Obor z tlacitka jde rovnou dal; jinak ho student vybere na uvodni strance (zalozni cesta)
	if program == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	discordState := security.GenerateStateToken()
	sess.SetDiscordState(discordState)
	http.Redirect(w, r, getDiscordLoginURL(origin, discordState), http.StatusFound)
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
	if student.Role == "Absolvent FM" {
		student.MicrosoftID = storage.AlumniPrefix + discordUser.ID
		student.Email = discordUser.ID + "@absolvent.invalid"
		student.Name = discordUser.Username
	}

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
		audit.Log(audit.LevelDanger, "Ověření nedokončeno", fmt.Sprintf("Uživateli **%s** (%s) se nepodařilo přidat roli nebo členství na serveru: %v", student.Name, student.Email, err))
		http.Error(w, "Školní účet je ověřený, ale na Discordu se nepodařilo přidělit role. Zkus to prosím za chvíli znovu, nebo napiš správcům.", http.StatusBadGateway)
		return
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

	// Kdo na serveru je, jde rovnou na něj; kdo odešel, dostane pozvánku
	link := discord.ServerLink(student.DiscordID, config.Cfg.DiscordInviteURL)
	if link != "" && (link == config.Cfg.DiscordInviteURL && isValidDiscordInvite(link) || strings.HasPrefix(link, "https://discord.com/channels/")) {
		http.Redirect(w, r, link, http.StatusFound)
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
	<meta name="theme-color" content="#ea7603">
	<title>Ověření pro Discord FM TUL</title>
	<script src="/static/fit.js" defer></script>
	<script src="/static/busy.js" defer></script>
	<link rel="preload" href="/static/fonts/TULMono-Bold.woff2" as="font" type="font/woff2" crossorigin>
	<link rel="preload" href="/static/fonts/Inter-Regular.woff2" as="font" type="font/woff2" crossorigin>
	<style>
		@font-face { font-family: 'TUL Mono'; src: url('/static/fonts/TULMono-Regular.woff2') format('woff2'); font-weight: 400 500; font-style: normal; font-display: swap; }
		@font-face { font-family: 'TUL Mono'; src: url('/static/fonts/TULMono-Bold.woff2') format('woff2'); font-weight: 600 800; font-style: normal; font-display: swap; }
		@font-face { font-family: 'Inter'; src: url('/static/fonts/Inter-Regular.woff2') format('woff2'); font-weight: 400 500; font-style: normal; font-display: swap; }
		@font-face { font-family: 'Inter'; src: url('/static/fonts/Inter-Bold.woff2') format('woff2'); font-weight: 600 800; font-style: normal; font-display: swap; }
		:root {
			--bg: #f1f1f1;
			--surface: #ffffff;
			--ink: #424242;
			--muted: #595758;
			--line: #e9e9e9;
			--orange: #ea7603;
			--orange-soft: #fdebd9;
			--orange-deep: #ea7603;
			--ease: cubic-bezier(0.16, 1, 0.3, 1);
			--sans: 'Inter', system-ui, sans-serif;
			--mono: 'TUL Mono', ui-monospace, monospace;
		}
		* { margin: 0; padding: 0; box-sizing: border-box; }
		html { font-size: 16px; -webkit-text-size-adjust: 100%; scrollbar-width: none; }
		::-webkit-scrollbar { display: none; }
		::selection { background: var(--orange); color: var(--ink); }
		:focus-visible { outline: 3px solid var(--ink); outline-offset: 3px; border-radius: 6px; }
		a { color: inherit; text-underline-offset: 0.22em; text-decoration-thickness: 1px; }

		body {
			background: var(--bg);
			color: var(--ink);
			font-family: var(--sans);
			font-size: 1.03rem;
			line-height: 1.55;
			min-height: 100vh;
			min-height: 100dvh;
			display: flex; flex-direction: column;
			position: relative; overflow-x: hidden;
			caret-color: var(--orange);
		}
		body { --turn: 0deg; }
		body.s2 { --turn: 30deg; } body.s3 { --turn: 60deg; }

		.bg { position: fixed; inset: 0; overflow: hidden; pointer-events: none; z-index: 0; }
		.circuit { position: absolute; top: 0; right: 0; width: min(44rem, 80vw); height: 40vh; opacity: 0.9; animation: fade 1.4s ease-out 0.1s backwards; -webkit-mask-image: linear-gradient(to left, #000 35%, transparent 100%); mask-image: linear-gradient(to left, #000 35%, transparent 100%); }
		.band { position: absolute; top: 0; bottom: 0; width: calc((100vw - 72rem) / 2 + 1rem); background: url("data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 300%22 fill=%22none%22 stroke=%22%23ea7603%22 stroke-width=%222.2%22 stroke-linecap=%22round%22 stroke-linejoin=%22round%22%3E%3Cpath d=%22M0 30H45L65 50H80%22/%3E%3Cpath d=%22M0 110H30L50 130V175%22/%3E%3Cpath d=%22M0 235H60L75 220%22/%3E%3Cpath d=%22M0 285H35%22/%3E%3Cg fill=%22%23fbf9f6%22%3E%3Ccircle cx=%2280%22 cy=%2250%22 r=%224.5%22/%3E%3Ccircle cx=%2250%22 cy=%22175%22 r=%224.5%22/%3E%3Ccircle cx=%2275%22 cy=%22220%22 r=%224.5%22/%3E%3Ccircle cx=%2235%22 cy=%22285%22 r=%224.5%22/%3E%3C/g%3E%3C/svg%3E") repeat-y left top / 100% auto; opacity: 0.85; animation: fade 1.4s ease-out 0.15s backwards; -webkit-mask-image: linear-gradient(to right, #000 45%, transparent 100%); mask-image: linear-gradient(to right, #000 45%, transparent 100%); }
		.band.l { left: 0; }
		.band.r { right: 0; top: 42vh; transform: scaleX(-1); }
		@media (max-width: 76rem) { .band { display: none; } }
		.circuit path { fill: none; stroke: var(--orange); stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
		.circuit circle { fill: var(--bg); stroke: var(--orange); stroke-width: 2; }
		.top, main, footer { position: relative; z-index: 1; }
		.wrap { width: 100%; max-width: 72rem; margin: 0 auto; padding: 0 2rem; }

		.top { padding: clamp(0.75rem, 2.4vh, 1.6rem) 0; }
		.top .wrap { display: flex; justify-content: space-between; align-items: center; gap: 1rem; }
		.logo { display: block; text-decoration: none; }
		.logo-img { display: block; width: 15.5rem; max-width: 62vw; height: auto; }
		.brand { display: block; text-decoration: none; font: 500 0.78rem/1.35 var(--mono); letter-spacing: 0.04em; text-transform: uppercase; color: var(--orange-deep); }
		.brand span { display: block; }
		.brand u { text-decoration-thickness: 2px; text-underline-offset: 0.18em; }
		.brand { display: flex; align-items: center; gap: 0.85rem; }
		.brand img { width: 2.9rem; height: 2.9rem; flex: none; display: block; }
		.brand-words { display: block; }
		.chlist { margin-top: clamp(0.7rem, 2vh, 1.4rem); max-width: 25rem; }
		.term { color: var(--ink); padding: 0; font: 400 0.9rem/1.8 var(--mono); letter-spacing: 0; overflow: hidden; }
		.tl { display: flex; align-items: baseline; gap: 0.7ch; min-height: 1.75em; animation: show 0.01s linear var(--d, 0s) backwards; }
		.term .pr { color: var(--orange); }
		.term .hash { color: #9a9488; }
		.tx { box-sizing: content-box; display: inline-block; overflow: hidden; white-space: nowrap; width: calc(var(--n) * 1ch); vertical-align: bottom; border-right: 0.55ch solid transparent; animation: type calc(var(--n) * 0.06s) steps(var(--n), end) var(--d) backwards, caret calc(var(--n) * 0.06s) steps(1, end) var(--d) backwards; }
				.term .st { margin-left: auto; font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase; color: #8a8579; animation: show 0.3s ease-out var(--e) backwards; }
		.chlist.open .term .st { color: var(--orange-deep); }
		.term .last { animation-delay: 0.3s; }
		.term .cursor { margin-left: 0; background: var(--orange); }
		.hint { margin-top: 0.9rem; font-size: 0.82rem; color: var(--muted); }
		.cmd { font: 500 0.78rem/1 var(--mono); padding: 0.2rem 0.45rem; border-radius: 0.35rem; background: #e9ebfd; color: #3b45ab; }
		.role { display: inline-flex; align-items: center; gap: 0.45rem; margin-top: 0.55rem; padding: 0.22rem 0.6rem 0.22rem 0.5rem; border: 1px solid var(--line); border-radius: 0.4rem; font-size: 0.82rem; font-weight: 500; color: var(--ink); background: #faf8f4; }
		.role i { width: 0.7rem; height: 0.7rem; border-radius: 50%; background: var(--orange); flex: none; }
		.lk { position: relative; width: 14px; height: 14px; flex: none; color: #8e8a82; }
		.chlist.open .lk { color: var(--orange-deep); }
		.lk svg { position: absolute; inset: 0; }
		.lk-o { opacity: 0; }
		.chlist.open .lk-c { animation: lock-out 0.3s var(--ease) calc(1.1s + var(--i) * 0.16s) forwards; }
		.chlist.open .lk-o { opacity: 1; animation: lock-in 0.4s var(--ease) calc(1.2s + var(--i) * 0.16s) backwards; }
		body.s3 .medal::after { content: ""; position: absolute; inset: 22%; border: 2px solid var(--orange); border-radius: 50%; pointer-events: none; animation: ripple 1.8s ease-out 0.9s backwards; }
		/* spoje pod formulářem: postup jako signál */
		.side { display: flex; flex-direction: column; min-width: 0; }
		.trace { display: block; width: 100%; max-width: 30rem; margin: clamp(0.5rem, 1.6vh, 1.2rem) auto 0; overflow: visible; animation: rise 0.9s var(--ease) 0.3s backwards; }
		.trace path { fill: none; stroke-width: 3.5; stroke-linejoin: round; stroke-linecap: round; }
		.trace .base { stroke: #d8d1c4; }
		.trace .lit { stroke: var(--orange); display: none; }
		.trace .lit.on { display: block; stroke-dasharray: 100 100; animation: wire 1.2s var(--ease) 0.5s backwards; }
		.trace .pad { fill: var(--bg); stroke: #d8d1c4; stroke-width: 3.5; }
		.trace .pad.cur { stroke: var(--orange); }
		.trace .pad.on { fill: var(--orange); stroke: var(--orange); }
		.trace text { font: 500 10.5px var(--mono); letter-spacing: 0.1em; text-transform: uppercase; fill: var(--muted); }
		.trace text.on { fill: var(--ink); }
		.out { font-size: 0.9rem; color: var(--muted); white-space: nowrap; }
		.out:hover { color: var(--ink); }

		main { flex: 1; display: flex; align-items: center; padding: clamp(0.5rem, 2.5vh, 2.5rem) 0 clamp(1rem, 4vh, 3.5rem); }
		main .wrap { display: grid; grid-template-columns: minmax(0, 1.05fr) minmax(0, 0.95fr); gap: 4rem; align-items: center; }

		h1 {
			font-family: var(--mono); font-size: clamp(1.9rem, min(4.2vw, 7.5vh), 4rem); line-height: 1.08; font-weight: 700; letter-spacing: -0.035em;
			text-wrap: balance; animation: rise 0.9s var(--ease) 0.05s backwards;
		}
		h1 mark {
			color: inherit; background: none;
			background-image: linear-gradient(transparent 66%, rgba(236, 122, 28, 0.38) 66% 92%, transparent 92%);
			padding: 0 0.04em; margin: 0 -0.04em;
		}
		.lead { margin-top: clamp(0.7rem, 2vh, 1.5rem); max-width: 29rem; font-size: 1.12rem; color: var(--muted); text-wrap: pretty; animation: rise 0.9s var(--ease) 0.15s backwards; }
		.facts { margin-top: clamp(0.8rem, 2.4vh, 2.2rem); display: flex; flex-wrap: wrap; gap: 0.6rem; animation: rise 0.9s var(--ease) 0.25s backwards; }
		.facts li { list-style: none; font-size: 0.84rem; padding: 0.4rem 0.85rem; border: 1px solid var(--line); border-radius: 999px; background: rgba(255, 255, 255, 0.7); color: var(--muted); }

		/* panel s kroky */
		.panel {
			background: var(--surface); border: 1px solid var(--line); border-radius: 1.75rem;
			box-shadow: 0 1px 2px rgba(26, 25, 22, 0.04), 0 30px 60px -28px rgba(60, 40, 10, 0.28);
			padding: 1.1rem; animation: rise 0.9s var(--ease) 0.2s backwards;
		}
		ol.steps { list-style: none; }
		.step { --sy: clamp(0.65rem, 1.8vh, 1.15rem); position: relative; display: grid; grid-template-columns: 2.4rem minmax(0, 1fr); gap: 0 1.1rem; padding: var(--sy) 1.2rem calc(var(--sy) + 0.1rem); border-radius: 1.2rem; transition: background-color 0.3s var(--ease); }
		.step.current { background: #fdf4ea; }
		.node {
			width: 2.4rem; height: 2.4rem; border-radius: 50%; display: grid; place-items: center;
			border: 2px solid var(--line); background: var(--surface); color: var(--muted);
			font: 500 0.85rem/1 var(--mono); position: relative; z-index: 1;
		}
		.step.current .node { border-color: var(--orange); color: var(--ink); background: var(--surface); }
		.step.done .node { border-color: var(--orange); background: var(--orange); color: var(--ink); }
		/* spojnice mezi uzly */
		.step:not(:last-child)::before {
			content: ""; position: absolute; left: calc(1.2rem + 1.2rem - 1px); top: calc(var(--sy) + 2.4rem); bottom: calc(var(--sy) * -1); width: 2px; background: var(--line); z-index: 0;
		}
		.step.done:not(:last-child)::before { background: var(--orange); animation: grow 0.7s var(--ease) 0.5s backwards; transform-origin: top; }

		.head { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; min-height: 2.4rem; }
		.step h2 { font-size: 1.22rem; font-weight: 600; letter-spacing: -0.02em; line-height: 1.2; }
		.step.todo h2 { color: var(--muted); font-weight: 500; }
		.chip { font: 500 0.7rem/1 var(--mono); letter-spacing: 0.04em; text-transform: uppercase; padding: 0.4rem 0.6rem; border-radius: 999px; white-space: nowrap; }
		.step.current .chip { background: var(--orange-soft); color: var(--orange-deep); }
		.step.done .chip { background: #eef6ec; color: #2c5b24; }
		.step.todo .chip { color: var(--muted); border: 1px solid var(--line); }
		.step p { color: var(--muted); font-size: 0.97rem; max-width: 24rem; text-wrap: pretty; }
		.step.current p { color: #4a463f; }
		.body { grid-column: 2; margin-top: -0.2rem; }
		.step:not(.current) .body p:not(.who) { display: none; }
		.step:not(.current) .body { margin-top: 0; }
		.step:not(.current) .who { font-size: 0.9rem; }
		.who { color: var(--ink) !important; }
		.who b { font-weight: 600; }

		.btn {
			display: inline-flex; align-items: center; gap: 0.75rem;
			margin-top: clamp(0.8rem, 1.8vh, 1.15rem); padding: 0.9rem 1.25rem 0.9rem 1.3rem;
			border-radius: 0.95rem; border: 0; background: var(--ink); color: #fff;
			font: 600 1rem/1.15 var(--sans); letter-spacing: -0.005em; text-decoration: none; cursor: pointer;
			box-shadow: 0 10px 22px -10px rgba(26, 25, 22, 0.55);
			transition: background-color 0.2s var(--ease), color 0.2s var(--ease), transform 0.2s var(--ease), box-shadow 0.2s var(--ease);
		}
		.btn svg { flex: none; }
		.btn .arrow { transition: transform 0.3s var(--ease); }
		.btn:hover { background: var(--orange); color: var(--ink); box-shadow: 0 14px 26px -12px rgba(236, 122, 28, 0.7); }
		.btn:hover .arrow { transform: translateX(4px); }
		.btn.busy { pointer-events: none; opacity: 0.85; }
		.btn.busy .arrow { display: none; }
		.btn.busy::after { content: ""; flex: none; width: 1em; height: 1em; border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%; animation: spin 0.7s linear infinite; }
		@keyframes spin { to { transform: rotate(360deg); } }
		/* vyrazne tlacitka: oranzova s pulzem a sipkou, aby bylo jasne, ze se na ne ma kliknout */
		.btn { font-size: 1.05rem; }
		.btn-ms, .btn-discord { background: var(--orange); color: var(--ink); box-shadow: 0 10px 22px -10px rgba(234, 118, 3, 0.8); animation: pulse 2.2s ease-out infinite; }
		.btn-ms:hover, .btn-discord:hover { background: var(--ink); color: #fff; animation: none; }
		.btn-ms .arrow, .btn-discord .arrow { animation: nudge 1.4s ease-in-out infinite; }
		.pick .btn { background: #fff; color: var(--ink); border: 2px solid var(--orange); box-shadow: none; justify-content: center; }
		.pick .btn:hover { background: var(--orange); color: var(--ink); }
		.btn.busy { animation: none; }
		@keyframes pulse { 0% { box-shadow: 0 0 0 0 rgba(234, 118, 3, 0.55); } 70% { box-shadow: 0 0 0 16px rgba(234, 118, 3, 0); } 100% { box-shadow: 0 0 0 0 rgba(234, 118, 3, 0); } }
		@keyframes nudge { 50% { transform: translateX(6px); } }
		@media (prefers-reduced-motion: reduce) { .btn-ms, .btn-discord, .btn .arrow { animation: none; } }
		.btn-ms { background: #2f2f2f; color: #fff; }
		.btn-ms:hover { background: #1f1f1f; color: #fff; box-shadow: 0 14px 26px -12px rgba(47, 47, 47, 0.7); }
		.btn-discord { background: #5865f2; color: #fff; box-shadow: 0 10px 22px -10px rgba(88, 101, 242, 0.7); }
		.btn-discord:hover { background: #4752c4; color: #fff; box-shadow: 0 14px 26px -12px rgba(88, 101, 242, 0.8); }
		.pick { padding: clamp(0.8rem, 2vh, 1.4rem) 1.4rem; display: flex; flex-direction: column; align-items: stretch; }
		.pick h2 { font-size: 1.3rem; font-weight: 600; letter-spacing: -0.02em; line-height: 1.2; }
		.pick p { margin-top: 0.4rem; color: #4a463f; font-size: 0.97rem; text-wrap: pretty; }
		.pick .btn { width: 100%; justify-content: center; }
		.alt { margin-top: 0.8rem !important; font-size: 0.9rem !important; }
		.alt a { color: var(--ink); text-decoration: underline; }
		.skip { margin-top: 0.7rem; align-self: center; background: none; border: 0; padding: 0.2rem; color: var(--muted); font: inherit; font-size: 0.9rem; text-decoration: underline; cursor: pointer; }
		.skip:hover { color: var(--ink); }
		.btn:active { transform: translateY(1px); box-shadow: 0 4px 10px -6px rgba(26, 25, 22, 0.5); }

		.console { margin: 0.4rem 1.2rem 0.5rem; padding-top: 1rem; border-top: 1px solid var(--line); font: 400 0.8rem/1.6 var(--mono); color: var(--muted); }
		.console .ln { animation: line 0.5s var(--ease) var(--d, 0s) backwards; }
		.console b { color: var(--ink); font-weight: 500; }
		.cursor { display: inline-block; width: 0.55em; height: 1.05em; background: var(--orange); vertical-align: -0.2em; margin-left: 0.25rem; border-radius: 1px; animation: blink 1.1s steps(1) infinite; }

		footer { padding: clamp(0.5rem, 1.6vh, 1.5rem) 0 clamp(0.8rem, 2vh, 2.2rem); font-size: 0.8rem; line-height: 1.4; color: var(--muted); }
		footer .wrap { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 0.6rem 2rem; }
		footer p { max-width: 44rem; }
		footer .legal span { display: block; text-wrap: balance; }
		footer a:hover { color: var(--ink); }

		@keyframes wire { from { stroke-dasharray: 0 100; } to { stroke-dasharray: 100 100; } }
		@keyframes caret { from, to { border-right-color: var(--orange); } }
		@keyframes type { from { width: 0; } }
		@keyframes show { from { opacity: 0; } }
		@keyframes fade { from { opacity: 0; } }
		@keyframes line { from { opacity: 0; transform: translateX(-8px); } }
		@keyframes ripple { from { opacity: 0.9; transform: scale(0.6); } to { opacity: 0; transform: scale(2.4); } }
		@keyframes lock-out { to { opacity: 0; transform: scale(0.6); } }
		@keyframes lock-in { from { opacity: 0; transform: translateY(2px) scale(0.6); } }
		@keyframes slide { from { opacity: 0; transform: translateX(-14px); } }
		.fit body { height: 100vh; height: 100dvh; overflow: hidden; }
		@keyframes rise { from { opacity: 0; transform: translateY(22px); } }
		@keyframes grow { from { transform: scaleY(0); } }
		@keyframes click { from { transform: rotate(calc(var(--turn) - 45deg)); } }
		@keyframes blink { 50% { opacity: 0; } }

		@media (max-width: 62rem) {
			main .wrap { grid-template-columns: 1fr; gap: 2.5rem; }
					}
		@media (max-height: 860px) { .medal { display: none; } }
		@media (max-height: 760px) {
			.console { display: none; }
			.panel { padding: 0.6rem; }
			.chlist li { padding: 0.28rem 0.7rem; }
			.hint { margin-top: 0.5rem; }
			h1 { font-size: clamp(1.7rem, min(4vw, 6.6vh), 3.2rem); }
		}
		@media (max-height: 640px) {
			.hint, .lead { display: none; }
			.cat { margin-bottom: 0.3rem; }
			.chlist li { padding: 0.2rem 0.7rem; font-size: 0.86rem; }
			.step { --sy: 0.55rem; }
		}
		@media (max-height: 500px) {
			.chlist, .trace { display: none; }
		}
		@media (max-width: 34rem) {
			body { font-size: 16px; }
			.facts, .medal { display: none; }
			.brand img { width: 2.3rem; height: 2.3rem; }
			.brand { gap: 0.65rem; }
			.brand-words { font-size: 0.6rem; line-height: 1.3; letter-spacing: 0.02em; }
			.brand-words span { display: inline; }
			.btn { padding: 0.85rem 1rem; gap: 0.6rem; font-size: 0.97rem; }
			.btn .arrow { display: none; }
			.top { padding: 0.7rem 0; }
			main .wrap { gap: 1.4rem; }
			.lead { font-size: 1rem; margin-top: 0.8rem; }
			.chlist { margin-top: 0.9rem; }
			.chlist .hint { display: none; }
			.term { font-size: 0.74rem; }
				.circuit { opacity: 0.55; height: 9vh; }
			.wrap { padding: 0 1.1rem; }
			main { padding: 0.4rem 0 1rem; }
			.panel { padding: 0.5rem; border-radius: 1.4rem; }
			.step { grid-template-columns: 2.1rem minmax(0, 1fr); gap: 0 0.8rem; padding: var(--sy) 0.8rem calc(var(--sy) + 0.1rem); }
			.node { width: 2.1rem; height: 2.1rem; }
			.step:not(:last-child)::before { left: calc(0.8rem + 1.05rem - 1px); top: calc(var(--sy) + 2.1rem); }
			.btn { width: 100%; justify-content: center; }
			.console { margin: 0.4rem 0.8rem 0.5rem; }
		}
		@media (max-width: 34rem) and (max-height: 700px) {
			.chlist, .trace { display: none; }
			footer { padding-bottom: 0.7rem; }
			footer .legal { font-size: 0.68rem; }
			footer .legal span { display: inline; }
			main .wrap { gap: 1rem; }
		}
		@media (prefers-reduced-motion: reduce) {
			*, *::before, *::after { animation: none !important; transition: none !important; }
			.tx { animation: none !important; }
		}
	</style>
</head>
<body class="s{{if not .Session}}1{{else if not .Session.DiscordID}}2{{else}}3{{end}}">

	<div class="bg" aria-hidden="true">
		<div class="band l"></div><div class="band r"></div>
		<svg class="circuit" viewBox="0 0 520 300" preserveAspectRatio="xMaxYMin meet" aria-hidden="true">
			<path d="M520 60H410L370 100H240"/>
			<path d="M520 180H450L410 220H320"/>
			<path d="M450 0V45L410 85H350"/>
			<path d="M330 0V40L290 80V140"/>
			<path d="M200 0V60"/>
			<circle cx="240" cy="100" r="5"/><circle cx="320" cy="220" r="5"/><circle cx="350" cy="85" r="5"/><circle cx="290" cy="140" r="5"/><circle cx="200" cy="60" r="5"/>
		</svg>
	</div>

	<header class="top">
		<div class="wrap">
			<a class="logo" href="/" aria-label="Fakulta mechatroniky, informatiky a mezioborových studií TUL">
					<img class="logo-img" src="/static/tul-fm-logo.svg" alt="Fakulta mechatroniky, informatiky a mezioborových studií TUL" width="955" height="168">
				</a>
			{{if .Session}}<a class="out" href="/logout">Odhlásit se</a>{{end}}
		</div>
	</header>

	<main>
		<div class="wrap">
			<div>
				{{if not .Session}}
				<h1>Ověř se <mark>školním účtem</mark></h1>
				<p class="lead">Přihlas se účtem TUL, vyber obor a rovnou tě provedeme na Discord a na fakultní server, kde dostaneš své role. Stačí to udělat jednou.</p>
				{{else if not .Session.DiscordID}}
				<h1>Školní účet <mark>ověřen</mark></h1>
				<p class="lead">Jsi poznán jako {{.Session.Role}}. Zbývá vybrat obor a propojit Discord, pak se dostaneš na server.</p>
				{{else}}
				<h1>Ověření <mark>dokončeno</mark></h1>
				<p class="lead">Role ti byly přiděleny. Fakultní kanály na Discordu se ti odemkly.</p>
				{{end}}
				<div class="chlist{{if and .Session .Session.DiscordID}} open{{end}}">
					<div class="term" role="img" aria-label="Kanály, které se po ověření odemknou">
						<p class="tl" style="--n:{{.Cmd.N}};--d:{{.Cmd.Delay}};--e:{{.Cmd.End}}"><span class="pr">$</span><span class="tx">{{.Cmd.Name}}</span></p>
						{{range .Channels}}<p class="tl ch" style="--n:{{.N}};--d:{{.Delay}};--e:{{.End}}"><span class="hash">#</span><span class="tx">{{.Name}}</span><span class="st">{{if and $.Session $.Session.DiscordID}}odemčeno{{else}}zamčeno{{end}}</span></p>
						{{end}}<p class="tl last"><span class="pr">$</span><span class="cursor" aria-hidden="true"></span></p>
					</div>
					<p class="hint">Ověření je jednorázové a po něm tě rovnou přesměrujeme na server</p>
				</div>
			</div>

			<div class="side"><div class="panel">
				{{if .NeedProgram}}
				<form class="pick" action="/obor" method="get">
					<h2>Jaký jsi obor?</h2>
					<p>Vyber svůj studijní program, role ti přidělíme rovnou. Pak tě provedeme na Discord.</p>
					<button class="btn" type="submit" name="p" value="ME">Mechatronika (ME)</button>
					<button class="btn" type="submit" name="p" value="IT">Informační technologie (IT)</button>
					<button class="btn" type="submit" name="p" value="AVI">Aplikované vědy v inženýrství (AVI)</button>
					<button class="skip" type="submit" name="p" value="-">Obor teď nevybírám</button>
				</form>
				{{else}}
				<ol class="steps">
					<li class="step {{if .Session}}done{{else}}current{{end}}">
						<span class="node" aria-hidden="true">{{if .Session}}<svg width="15" height="15" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 7.5l3 3 6-7"/></svg>{{else}}1{{end}}</span>
						<div class="head"><h2>Školní účet</h2><span class="chip">{{if .Session}}Hotovo{{else}}Na řadě{{end}}</span></div>
						<div class="body">
							{{if not .Session}}
								<p>Přihlas se účtem ve tvaru jmeno.prijmeni@tul.cz. Dál tě provedeme sami.</p>
								<a href="/go" class="btn btn-ms">
									<svg width="18" height="18" viewBox="0 0 21 21" aria-hidden="true">
										<rect x="1" y="1" width="9" height="9" fill="#f25022"/>
										<rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
										<rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
										<rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
									</svg>
									Přihlásit se pomocí Microsoftu
									<svg class="arrow" width="18" height="12" viewBox="0 0 18 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 6h15M11 1l5 5-5 5"/></svg>
								</a>
									<p class="alt">Absolvent bez školního účtu? <a href="/absolvent">Pokračuj přes Discord</a></p>
							{{else}}
								<p class="who"><b>{{.Session.Name}}</b><br>{{.Session.Email}}</p>
								<span class="role"><i></i>{{.Session.Role}}</span>
							{{end}}
						</div>
					</li>

					<li class="step {{if not .Session}}todo{{else if .Session.DiscordID}}done{{else}}current{{end}}">
						<span class="node" aria-hidden="true">{{if and .Session .Session.DiscordID}}<svg width="15" height="15" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 7.5l3 3 6-7"/></svg>{{else}}<svg width="16" height="12" viewBox="0 0 127.14 96.36" fill="currentColor"><path d="M107.7,8.07A105.15,105.15,0,0,0,81.47,0a72.06,72.06,0,0,0-3.36,6.83A97.68,97.68,0,0,0,49,6.83,72.37,72.37,0,0,0,45.64,0,105.89,105.89,0,0,0,19.39,8.09C2.79,32.65-1.71,56.6.54,80.21h0A105.73,105.73,0,0,0,32.71,96.36,77.7,77.7,0,0,0,39.6,85.25a68.42,68.42,0,0,1-10.85-5.18c.91-.66,1.8-1.34,2.66-2a75.57,75.57,0,0,0,64.32,0c.87.71,1.76,1.39,2.66,2a68.68,68.68,0,0,1-10.87,5.19,77,77,0,0,0,6.89,11.1A105.25,105.25,0,0,0,126.6,80.22h0C129.24,52.84,122.09,29.11,107.7,8.07ZM42.45,65.69C36.18,65.69,31,60,31,53s5-12.74,11.43-12.74S54,45.91,53.89,53,48.84,65.69,42.45,65.69Zm42.24,0C78.41,65.69,73.25,60,73.25,53s5-12.74,11.44-12.74S96.23,45.91,96.12,53,91.08,65.69,84.69,65.69Z"/></svg>{{end}}</span>
						<div class="head"><h2>Discord</h2><span class="chip">{{if not .Session}}Čeká{{else if .Session.DiscordID}}Hotovo{{else}}Na řadě{{end}}</span></div>
						<div class="body">
							{{if and .Session (not .Session.DiscordID)}}
								<p>Propojíme účet s tvým Discordem a přidáme tě na server.</p>
								<a href="{{.DiscordURL}}" class="btn btn-discord">
									<svg width="20" height="16" viewBox="0 0 127.14 96.36" fill="currentColor" aria-hidden="true">
										<path d="M107.7,8.07A105.15,105.15,0,0,0,81.47,0a72.06,72.06,0,0,0-3.36,6.83A97.68,97.68,0,0,0,49,6.83,72.37,72.37,0,0,0,45.64,0,105.89,105.89,0,0,0,19.39,8.09C2.79,32.65-1.71,56.6.54,80.21h0A105.73,105.73,0,0,0,32.71,96.36,77.7,77.7,0,0,0,39.6,85.25a68.42,68.42,0,0,1-10.85-5.18c.91-.66,1.8-1.34,2.66-2a75.57,75.57,0,0,0,64.32,0c.87.71,1.76,1.39,2.66,2a68.68,68.68,0,0,1-10.87,5.19,77,77,0,0,0,6.89,11.1A105.25,105.25,0,0,0,126.6,80.22h0C129.24,52.84,122.09,29.11,107.7,8.07ZM42.45,65.69C36.18,65.69,31,60,31,53s5-12.74,11.43-12.74S54,45.91,53.89,53,48.84,65.69,42.45,65.69Zm42.24,0C78.41,65.69,73.25,60,73.25,53s5-12.74,11.44-12.74S96.23,45.91,96.12,53,91.08,65.69,84.69,65.69Z"/>
									</svg>
									Propojit s Discordem
									<svg class="arrow" width="18" height="12" viewBox="0 0 18 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 6h15M11 1l5 5-5 5"/></svg>
								</a>
							{{else if .Session}}
								<p class="who">Účet je propojený.</p>
							{{else}}
								<p>Naváže se samo po výběru oboru.</p>
							{{end}}
						</div>
					</li>

					<li class="step {{if and .Session .Session.DiscordID}}current{{else}}todo{{end}}">
						<span class="node" aria-hidden="true">3</span>
						<div class="head"><h2>Server</h2><span class="chip">{{if and .Session .Session.DiscordID}}Na řadě{{else}}Čeká{{end}}</span></div>
						<div class="body">
							{{if and .Session .Session.DiscordID}}
								<p>Role ti byly přiděleny automaticky a fakultní kanály se ti odemkly.</p>
								<a href="{{if .ServerURL}}{{.ServerURL}}{{else}}https://discord.com/app{{end}}" class="btn">
									Otevřít server na Discordu
									<svg class="arrow" width="18" height="12" viewBox="0 0 18 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 6h15M11 1l5 5-5 5"/></svg>
								</a>
							{{else}}
								<p>Po přihlášení tě přesměrujeme na server s přidělenými rolemi.</p>
							{{end}}
						</div>
					</li>
				</ol>
<div class="console" aria-live="polite">
					{{if not .Session}}
					<p class="ln" style="--d:0.3s">&gt; <b>čeká na přihlášení školním účtem</b><span class="cursor" aria-hidden="true"></span></p>
					{{else if not .Session.DiscordID}}
					<p class="ln" style="--d:0.4s">&gt; školní účet … <b>ověřen</b></p>
					<p class="ln" style="--d:1.1s">&gt; discord … <b>čeká na propojení</b><span class="cursor" aria-hidden="true"></span></p>
					{{else}}
					<p class="ln" style="--d:0.4s">&gt; školní účet … <b>ověřen</b></p>
					<p class="ln" style="--d:1.0s">&gt; discord … <b>propojen</b></p>
					<p class="ln" style="--d:1.6s">&gt; role … <b>{{.Session.Role}}</b></p>
					<p class="ln" style="--d:2.2s">&gt; <b>hotovo</b><span class="cursor" aria-hidden="true"></span></p>
					{{end}}
				</div>
				{{end}}
			</div>
			<svg class="trace" viewBox="0 0 480 64" role="img" aria-label="Postup ověření: krok {{if not .Session}}1{{else if not .Session.DiscordID}}2{{else}}3{{end}} ze 3">
				<path class="base" d="M24 38H118L146 14H240"/>
				<path class="base" d="M240 14H334L362 38H456"/>
				<path class="lit {{if .Session}}on{{end}}" pathLength="100" d="M24 38H118L146 14H240"/>
				<path class="lit {{if and .Session .Session.DiscordID}}on{{end}}" pathLength="100" d="M240 14H334L362 38H456"/>
				<circle class="pad {{if .Session}}on{{else}}cur{{end}}" cx="24" cy="38" r="9"/>
				<circle class="pad {{if not .Session}}{{else if .Session.DiscordID}}on{{else}}cur{{end}}" cx="240" cy="14" r="9"/>
				<circle class="pad {{if and .Session .Session.DiscordID}}on{{end}}" cx="456" cy="38" r="9"/>
				<text x="24" y="62" text-anchor="start" class="{{if .Session}}on{{end}}">Účet</text>
				<text x="240" y="44" text-anchor="middle" class="{{if and .Session .Session.DiscordID}}on{{end}}">Discord</text>
				<text x="456" y="62" text-anchor="end" class="{{if and .Session .Session.DiscordID}}on{{end}}">Server</text>
			</svg>
			</div>
		</div>
	</main>

	<footer>
		<div class="wrap">
			<p class="legal"><span>Ukládáme jméno, školní e-mail, roli (student nebo zaměstnanec) a ID tvého Discord účtu.</span> <span>Jeden školní účet jde propojit s jedním Discordem.</span></p>
			{{if .EnableDevMock}}<p><a href="/mock-msal">Simulovat MS</a> &middot; <a href="/mock-discord">Simulovat Discord</a></p>{{end}}
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
