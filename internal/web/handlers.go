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
	<link rel="preload" href="/static/fonts/TULMono-Bold.woff2" as="font" type="font/woff2" crossorigin>
	<link rel="preload" href="/static/fonts/Inter-Regular.woff2" as="font" type="font/woff2" crossorigin>
	<style>
		@font-face { font-family: 'TUL Mono'; src: url('/static/fonts/TULMono-Regular.woff2') format('woff2'); font-weight: 400 500; font-style: normal; font-display: swap; }
		@font-face { font-family: 'TUL Mono'; src: url('/static/fonts/TULMono-Bold.woff2') format('woff2'); font-weight: 600 800; font-style: normal; font-display: swap; }
		@font-face { font-family: 'Inter'; src: url('/static/fonts/Inter-Regular.woff2') format('woff2'); font-weight: 400 500; font-style: normal; font-display: swap; }
		@font-face { font-family: 'Inter'; src: url('/static/fonts/Inter-Bold.woff2') format('woff2'); font-weight: 600 800; font-style: normal; font-display: swap; }
		:root {
			--orange: #ea7603;
			--plum: #7e1a47;
			--text: #595758;
			--ink: #424242;
			--line: #e9e9e9;
			--soft: #f1f1f1;
			--white: #ffffff;
			--sans: 'Inter', 'Open Sans', system-ui, sans-serif;
			--mono: 'TUL Mono', ui-monospace, monospace;
		}
		* { margin: 0; padding: 0; box-sizing: border-box; }
		html { font-size: 16px; -webkit-text-size-adjust: 100%; }
		::selection { background: var(--orange); color: var(--white); }
		:focus-visible { outline: 3px solid var(--plum); outline-offset: 3px; border-radius: 4px; }
		a { color: var(--plum); text-underline-offset: 0.2em; }
		a:hover { color: var(--orange); }

		body { background: var(--soft); color: var(--text); font-family: var(--sans); font-size: 1rem; line-height: 1.6; min-height: 100vh; min-height: 100dvh; display: flex; flex-direction: column; }
		.wrap { width: 100%; max-width: 64rem; margin: 0 auto; padding: 0 1.5rem; }

		header { background: var(--white); border-bottom: 1px solid var(--line); }
		header .wrap { display: flex; justify-content: space-between; align-items: center; gap: 1rem; padding-top: 1.1rem; padding-bottom: 1.1rem; }
		.logo { display: block; text-decoration: none; }
		.logo svg { display: block; width: 15.5rem; max-width: 62vw; height: auto; overflow: visible; }
		.logo text { font-family: var(--mono); font-weight: 700; font-size: 20px; fill: var(--orange); }
		.out { font-size: 0.9rem; color: var(--text); }
		.out:hover { color: var(--plum); }

		main { flex: 1; }
		main .wrap { padding-top: clamp(1.8rem, 6vh, 3.5rem); padding-bottom: 2rem; display: grid; gap: 2rem 3.5rem; align-items: start; grid-template-columns: 1fr; }
		@media (min-width: 52rem) { main .wrap { grid-template-columns: minmax(0, 1fr) minmax(0, 26rem); } }

		h1 { font-family: var(--mono); font-weight: 700; font-size: clamp(1.9rem, 5vw, 2.9rem); line-height: 1.12; letter-spacing: -0.01em; color: var(--orange); text-wrap: balance; }
		.lead { margin-top: 1rem; max-width: 30rem; font-size: 1.08rem; color: var(--text); text-wrap: pretty; }
		.points { margin-top: 1.5rem; list-style: none; display: grid; gap: 0.7rem; max-width: 30rem; }
		.points li { position: relative; padding-left: 1.5rem; }
		.points li::before { content: ""; position: absolute; left: 0; top: 0.62em; width: 0.6rem; height: 0.6rem; background: var(--orange); border-radius: 1px; }

		.card { background: var(--white); border: 1px solid var(--line); border-radius: 0.5rem; padding: 1.6rem; }
		.progress { list-style: none; display: flex; gap: 0.4rem; margin-bottom: 1.4rem; font: 500 0.78rem/1.2 var(--mono); text-transform: uppercase; letter-spacing: 0.03em; }
		.progress li { flex: 1; padding-top: 0.55rem; border-top: 4px solid var(--line); color: #999999; }
		.progress li.done { border-color: var(--orange); color: var(--text); }
		.progress li.current { border-color: var(--plum); color: var(--plum); }
		.card h2 { font-family: var(--mono); font-weight: 700; font-size: 1.35rem; line-height: 1.2; color: var(--ink); }
		.card p { margin-top: 0.6rem; }
		.who { margin-top: 0.9rem; padding: 0.8rem 1rem; background: var(--soft); border-radius: 0.4rem; }
		.who b { color: var(--ink); }
		.chip { display: inline-block; margin-top: 0.4rem; padding: 0.15rem 0.6rem; border: 1px solid var(--orange); border-radius: 0.3rem; font-size: 0.85rem; font-weight: 500; color: var(--plum); background: var(--white); }

		.btn { display: flex; align-items: center; justify-content: center; gap: 0.7rem; width: 100%; margin-top: 1.2rem; padding: 0.95rem 1.2rem; border: 2px solid var(--plum); border-radius: 0.4rem; background: var(--plum); color: var(--white); font: 600 1rem/1.2 var(--sans); text-align: center; text-decoration: none; cursor: pointer; transition: background-color 0.15s, color 0.15s, border-color 0.15s; }
		.btn:hover { background: var(--white); color: var(--plum); }
		.btn svg { flex: none; }
		.btn-line { background: var(--white); color: var(--plum); margin-top: 0.7rem; }
		.btn-line:hover { background: var(--orange); border-color: var(--orange); color: var(--white); }
		.btn small { font: 500 0.85rem var(--mono); opacity: 0.85; }
		.pick { margin-top: 0.8rem; }
		.skip { display: block; margin: 1rem auto 0; padding: 0.2rem; background: none; border: 0; font: inherit; font-size: 0.92rem; color: var(--text); text-decoration: underline; cursor: pointer; }
		.skip:hover { color: var(--plum); }
		.alt { font-size: 0.93rem; }

		footer { border-top: 1px solid var(--line); background: var(--white); font-size: 0.84rem; color: var(--text); }
		footer .wrap { padding-top: 1rem; padding-bottom: 1rem; display: flex; flex-wrap: wrap; justify-content: space-between; gap: 0.4rem 2rem; }
		footer p { max-width: 42rem; }
	</style>
</head>
<body>
	<header>
		<div class="wrap">
			<a class="logo" href="/" aria-label="Fakulta mechatroniky, informatiky a mezioborových studií TUL">
				<svg viewBox="0 0 330 72" role="img" aria-hidden="true" focusable="false">
					<text x="0" y="20">Fakulta mechatroniky,</text>
					<text x="0" y="44">informatiky a mezioborových</text>
					<text x="0" y="68">studií TUL</text>
				</svg>
			</a>
			{{if .Session}}<a class="out" href="/logout">Odhlásit se</a>{{end}}
		</div>
	</header>

	<main>
		<div class="wrap">
			<section>
				{{if and .Session .Session.DiscordID}}
				<h1>Ověření dokončeno</h1>
				<p class="lead">Role ti byly přiděleny a fakultní kanály na Discordu se ti odemkly.</p>
				{{else if .Session}}
				<h1>Školní účet ověřen</h1>
				<p class="lead">Jsi poznán jako {{.Session.Role}}. Zbývá dokončit propojení s Discordem a dostaneš se na server.</p>
				{{else}}
				<h1>Ověř se školním účtem</h1>
				<p class="lead">Přihlas se účtem TUL a rovnou tě provedeme na Discord a na fakultní server, kde dostaneš své role. Stačí to udělat jednou.</p>
				<ul class="points">
					<li>Jedno přihlášení školním účtem ve tvaru jmeno.prijmeni@tul.cz.</li>
					<li>Obor a role se přidělí hned, bez dalšího psaní.</li>
					<li>Absolventi bez školního účtu mají vlastní cestu přes Discord.</li>
				</ul>
				{{end}}
			</section>

			<section class="card">
				<ol class="progress" aria-label="Postup ověření">
					<li class="{{if .Session}}done{{else}}current{{end}}">Školní účet</li>
					<li class="{{if not .Session}}{{else if .Session.DiscordID}}done{{else}}current{{end}}">Obor a Discord</li>
					<li class="{{if and .Session .Session.DiscordID}}current{{end}}">Server</li>
				</ol>

				{{if .NeedProgram}}
				<h2>Jaký jsi obor?</h2>
				<p>Vyber svůj studijní program, role ti přidělíme hned. Pak tě provedeme na Discord.</p>
				<form class="pick" action="/obor" method="get">
					<button class="btn btn-line" type="submit" name="p" value="ME">Mechatronika <small>ME</small></button>
					<button class="btn btn-line" type="submit" name="p" value="IT">Informační technologie <small>IT</small></button>
					<button class="btn btn-line" type="submit" name="p" value="AVI">Aplikované vědy v inženýrství <small>AVI</small></button>
					<button class="skip" type="submit" name="p" value="-">Obor teď nevybírám</button>
				</form>
				{{else if not .Session}}
				<h2>Přihlas se školním účtem</h2>
				<p>Dál tě provedeme sami: obor, Discord i server.</p>
				<a class="btn" href="/go">
					<svg width="18" height="18" viewBox="0 0 21 21" aria-hidden="true">
						<rect x="1" y="1" width="9" height="9" fill="#f25022"/>
						<rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
						<rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
						<rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
					</svg>
					Přihlásit se školním účtem
				</a>
				<p class="alt">Absolvent bez školního účtu? <a href="/absolvent">Pokračuj přes Discord</a></p>
				{{else if not .Session.DiscordID}}
				<h2>Propoj Discord</h2>
				<p>Připojíme tě na server a přidělíme role.</p>
				<div class="who"><b>{{.Session.Name}}</b><br>{{.Session.Email}}<br><span class="chip">{{.Session.Role}}</span></div>
				<a class="btn" href="{{.DiscordURL}}">Pokračovat na Discord</a>
				{{else}}
				<h2>Hotovo</h2>
				<p>Účet je propojený a role přidělené.</p>
				<div class="who"><b>{{.Session.Name}}</b><br>{{.Session.Email}}<br><span class="chip">{{.Session.Role}}</span></div>
				<a class="btn" href="{{if .ServerURL}}{{.ServerURL}}{{else}}https://discord.com/app{{end}}">Otevřít server na Discordu</a>
				{{end}}
			</section>
		</div>
	</main>

	<footer>
		<div class="wrap">
			<p>Ukládáme jméno, školní e-mail, roli (student nebo zaměstnanec) a ID tvého Discord účtu. Jeden školní účet jde propojit s jedním Discordem.</p>
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
