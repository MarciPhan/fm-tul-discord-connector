package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	// Nastavime testovaci vychozi konfiguraci
	cfg = Config{
		MSALClientID:           "test-msal-client-id",
		MSALClientSecret:       "test-msal-client-secret",
		MSALTenantID:           "test-tenant-id",
		DiscordClientID:        "test-discord-client-id",
		DiscordClientSecret:    "test-discord-client-secret",
		DiscordToken:           "test-discord-token",
		DiscordGuildID:         "123456789",
		DiscordVerifiedID:      "111111",
		DiscordFMStudentID:     "222222",
		DiscordFMStaffID:       "333333",
		DiscordInviteURL:       "https://discord.gg/test-invite",
		DiscordVerifyEmoji:     "🎓",
		Host:                   "localhost",
		Port:                   "8000",
		BaseURL:                "http://localhost:8000",
		EnableDevMock:          true,
		SecureCookies:          false,
		RateLimitEnabled:       true,
	}
}

// ============================================================================
// KRYPTOGRAFICKÉ UTILITY
// ============================================================================

// TestGenerateID overuje delku a unikatnost generovanych ID
func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if len(id1) != 32 {
		t.Errorf("Ocekavana delka 32 znaku, ziskana: %d", len(id1))
	}
	if id1 == id2 {
		t.Errorf("Generovane ID musi byt unikatni, ziskana stejna: %s", id1)
	}

	// Test dostatecne entropie – vygenerujeme 100 ID a overime, ze jsou vsechna ruzna
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := generateID()
		if seen[id] {
			t.Fatalf("Kolize ID detekována po %d generacích", i)
		}
		seen[id] = true
	}
}

// TestGenerateStateToken overuje delku a unikatnost state tokenu pro CSRF ochranu
func TestGenerateStateToken(t *testing.T) {
	st1 := generateStateToken()
	st2 := generateStateToken()

	// state token ma byt 64 hex znaku (32 bajtu)
	if len(st1) != 64 {
		t.Errorf("State token ma mit 64 hex znaku, ma: %d", len(st1))
	}
	if st1 == st2 {
		t.Error("Dva state tokeny nesmi byt shodne")
	}
}

// TestPKCEGeneration overuje korektnost PKCE podle RFC 7636 (S256)
func TestPKCEGeneration(t *testing.T) {
	verifier1, challenge1 := generatePKCE()
	verifier2, challenge2 := generatePKCE()

	if len(verifier1) < 43 {
		t.Errorf("code_verifier musi mit alespon 43 znaku (RFC 7636), ma: %d", len(verifier1))
	}
	if challenge1 == "" {
		t.Error("code_challenge nesmi byt prazdny")
	}
	if verifier1 == verifier2 || challenge1 == challenge2 {
		t.Error("PKCE parametry musi byt unikatni pri kazdem volani")
	}

	// Overeni, ze challenge se lisi od verifier (protoze jde o hash)
	if verifier1 == challenge1 {
		t.Error("code_challenge musi byt hash code_verifier, nesmi byt shodny")
	}
}

// TestConstantTimeCompare overuje bezpecne porovnavani retezcu bez timing attacku
func TestConstantTimeCompare(t *testing.T) {
	// Shodne retezce
	if !constantTimeCompare("abc123", "abc123") {
		t.Error("Shodne retezce musi vratit true")
	}
	// Ruzne retezce
	if constantTimeCompare("abc123", "abc124") {
		t.Error("Ruzne retezce musi vratit false")
	}
	// Ruzna delka
	if constantTimeCompare("short", "much-longer-string") {
		t.Error("Retezce ruzne delky musi vratit false")
	}
	// Prazdne retezce
	if !constantTimeCompare("", "") {
		t.Error("Dva prazdne retezce musi vratit true")
	}
	// Jeden prazdny
	if constantTimeCompare("abc", "") {
		t.Error("Prazdny vs neprazdny retezec musi vratit false")
	}
}

// ============================================================================
// SESSION MANAGEMENT
// ============================================================================

// TestSessionManagement testuje ukladani, ziskavani a expiraci sessions
func TestSessionManagement(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	sess := getOrCreateSession(w, req)
	if sess == nil || sess.ID == "" {
		t.Fatal("Session nesmi byt nil ani prazdna")
	}

	cookieHeader := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, SessionCookieName) {
		t.Errorf("Set-Cookie hlavicka neobsahuje %s: %s", SessionCookieName, cookieHeader)
	}

	// Overeni HttpOnly flagu
	if !strings.Contains(cookieHeader, "HttpOnly") {
		t.Errorf("Session cookie musi byt HttpOnly: %s", cookieHeader)
	}

	// Overeni nacteni existujici session
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})

	retrieved := getExistingSession(req2)
	if retrieved == nil || retrieved.ID != sess.ID {
		t.Errorf("Ocekavano session ID %s, ziskano %+v", sess.ID, retrieved)
	}

	// Test mazani session
	w2 := httptest.NewRecorder()
	clearSession(w2)
	cookieHeader2 := w2.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader2, "Max-Age=0") && !strings.Contains(cookieHeader2, "Max-Age=-1") {
		t.Errorf("clearSession musi nastavit expirovany cookie: %s", cookieHeader2)
	}
}

// TestSessionExpiration overuje, ze expirovana session neni vracena
func TestSessionExpiration(t *testing.T) {
	sessionsMux.Lock()
	expiredID := generateID()
	sessions[expiredID] = &Session{
		ID:        expiredID,
		CreatedAt: time.Now().Add(-1 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Minute), // Jiz expirovano
	}
	sessionsMux.Unlock()

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: expiredID})

	retrieved := getExistingSession(req)
	if retrieved != nil {
		t.Error("Expirovana session NESMI byt vracena")
	}
}

// TestSessionSlidingWindow overuje, ze aktivni session se prodluzuje
func TestSessionSlidingWindow(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	sess := getOrCreateSession(w, req)
	originalExpiry := sess.ExpiresAt

	// Krasna pauza a znovupristup
	time.Sleep(10 * time.Millisecond)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	sess2 := getOrCreateSession(w2, req2)

	if sess2.ID != sess.ID {
		t.Error("Znovupristup musi vratit stejnou session")
	}
	if !sess2.ExpiresAt.After(originalExpiry) {
		t.Error("Session sliding window musi prodlouzit expiraci")
	}
}

// TestSessionInvalidCookie overuje, ze neplatny cookie nevrati session
func TestSessionInvalidCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "neexistujici-session-id"})

	retrieved := getExistingSession(req)
	if retrieved != nil {
		t.Error("Neplatny session cookie NESMI vratit session")
	}
}

// TestSessionNoCookie overuje, ze pozadavek bez cookie nevrati session
func TestSessionNoCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	retrieved := getExistingSession(req)
	if retrieved != nil {
		t.Error("Pozadavek bez cookie NESMI vratit session")
	}
}

// ============================================================================
// OAUTH2 URL GENEROVANI
// ============================================================================

// TestURLGeneration overuje spravne sestaveni autorizacnich URL vcetne PKCE a CSRF parametru state
func TestURLGeneration(t *testing.T) {
	origin := "http://localhost:8000"
	testState := "crypto-state-12345"
	testChallenge := "crypto-challenge-67890"

	// 1. Microsoft URL
	msURL := getMicrosoftLoginURL(origin, testState, testChallenge)
	parsedMS, err := url.Parse(msURL)
	if err != nil {
		t.Fatalf("Chyba parsovani MS URL: %v", err)
	}
	msQuery := parsedMS.Query()
	if msQuery.Get("client_id") != "test-msal-client-id" {
		t.Errorf("Spatne client_id v MS URL: %s", msQuery.Get("client_id"))
	}
	if msQuery.Get("redirect_uri") != "http://localhost:8000/msal" {
		t.Errorf("Spatne redirect_uri v MS URL: %s", msQuery.Get("redirect_uri"))
	}
	if msQuery.Get("state") != testState {
		t.Errorf("Spatny state v MS URL: %s", msQuery.Get("state"))
	}
	if msQuery.Get("code_challenge") != testChallenge || msQuery.Get("code_challenge_method") != "S256" {
		t.Errorf("Chybi nebo je neplatny PKCE code_challenge: %s", msQuery.Get("code_challenge"))
	}
	if msQuery.Get("response_type") != "code" {
		t.Errorf("response_type musi byt 'code': %s", msQuery.Get("response_type"))
	}
	if !strings.Contains(msQuery.Get("scope"), "User.Read") {
		t.Errorf("Scope musi obsahovat User.Read: %s", msQuery.Get("scope"))
	}

	// 2. Discord URL
	discordURL := getDiscordLoginURL(origin, testState)
	parsedDiscord, err := url.Parse(discordURL)
	if err != nil {
		t.Fatalf("Chyba parsovani Discord URL: %v", err)
	}
	discordQuery := parsedDiscord.Query()
	if discordQuery.Get("client_id") != "test-discord-client-id" {
		t.Errorf("Spatne client_id v Discord URL: %s", discordQuery.Get("client_id"))
	}
	if discordQuery.Get("state") != testState {
		t.Errorf("Spatny state v Discord URL: %s", discordQuery.Get("state"))
	}
	if !strings.Contains(discordQuery.Get("scope"), "guilds.join") {
		t.Errorf("Chybi guilds.join scope v Discord URL: %s", discordQuery.Get("scope"))
	}
	if !strings.Contains(discordQuery.Get("scope"), "identify") {
		t.Errorf("Chybi identify scope v Discord URL: %s", discordQuery.Get("scope"))
	}
	if discordQuery.Get("prompt") != "consent" {
		t.Errorf("Discord OAuth musi vynutit consent: %s", discordQuery.Get("prompt"))
	}
}

// TestMicrosoftURLTenantFallback overuje fallback na 'common' tenant pri prazdnem TenantID
func TestMicrosoftURLTenantFallback(t *testing.T) {
	origTenant := cfg.MSALTenantID
	cfg.MSALTenantID = ""
	defer func() { cfg.MSALTenantID = origTenant }()

	msURL := getMicrosoftLoginURL("http://localhost:8000", "state", "challenge")
	if !strings.Contains(msURL, "/common/oauth2") {
		t.Errorf("Pri prazdnem TenantID musi URL pouzit 'common': %s", msURL)
	}
}

// ============================================================================
// VALIDACE EMAILU
// ============================================================================

// TestTULEmailValidation overuje striktni pravidla pro domenove overeni TUL
func TestTULEmailValidation(t *testing.T) {
	valid := []string{
		"jan.novak@tul.cz",
		"petr.student@fm.tul.cz",
		"TEST.UZIVATEL@TUL.CZ",
		"marie@kky.tul.cz",
		"x@ips.tul.cz",
		"nekdo@a.b.c.tul.cz",
	}
	for _, e := range valid {
		if !isValidTULEmail(e) {
			t.Errorf("Email %s by mel byt vyhodnocen jako platny", e)
		}
	}

	invalid := []string{
		"utocnik@gmail.com",
		"fake@seznam.cz",
		"tul.cz@hacker.com",
		"jan.novak@tul.cz.attacker.com",
		"",
		"bez_zavinace",
		"@tul.cz",
		"jan@",
		"  @tul.cz",
		"user@nottul.cz",
		"user@tulx.cz",
		"user@xtul.cz",
		"user@@tul.cz",
	}
	for _, e := range invalid {
		if isValidTULEmail(e) {
			t.Errorf("Email %s NESMI byt vyhodnocen jako platny TUL email", e)
		}
	}
}

// TestTULEmailValidationCaseInsensitive overuje case-insensitive chovani
func TestTULEmailValidationCaseInsensitive(t *testing.T) {
	variants := []string{
		"Jan.Novak@TUL.CZ",
		"jan.novak@Tul.Cz",
		"JAN.NOVAK@tul.cz",
		"jan.novak@FM.TUL.CZ",
	}
	for _, e := range variants {
		if !isValidTULEmail(e) {
			t.Errorf("Email %s musi byt platny bez ohledu na velikost pismen", e)
		}
	}
}

// ============================================================================
// SANITIZACE PREZDIVEK
// ============================================================================

// TestNicknameSanitization testuje prevenci zneuziti Discord prezdivky
func TestNicknameSanitization(t *testing.T) {
	// 1. Zneuzitelne tagy
	dangerous := "Jan @everyone Novák <@12345> discord.gg/cheat"
	clean := sanitizeNickname(dangerous)
	if strings.Contains(clean, "@everyone") || strings.Contains(clean, "<@") || strings.Contains(clean, "discord.gg") {
		t.Errorf("Přezdívka obsahuje zakázané tagy: %s", clean)
	}

	// 2. Zero-width spaces a ridici znaky
	withZWS := "Pavel\u200B\u200C\uFEFFNovák\n\r"
	cleanZWS := sanitizeNickname(withZWS)
	if strings.ContainsAny(cleanZWS, "\u200B\u200C\uFEFF\n\r") {
		t.Errorf("Přezdívka obsahuje zero-width nebo řídicí znaky: %q", cleanZWS)
	}

	// 3. Limit 32 znaku
	veryLong := "TotoJeExtremneDlouheJmenoKterePresahujeLimitTridcetiDvouZnakuDiscordu"
	cleanLong := sanitizeNickname(veryLong)
	if len([]rune(cleanLong)) > 32 {
		t.Errorf("Přezdívka přesahuje 32 znaků: %d znaků", len([]rune(cleanLong)))
	}

	// 4. Fallback pri prazdnem jmenu
	empty := sanitizeNickname("   \u200B  ")
	if empty != "Student FM" {
		t.Errorf("Při prázdném jménu má být fallback 'Student FM', získáno: %q", empty)
	}
}

// TestNicknameSanitizationHTTPLinks overuje odstraneni HTTP odkazu
func TestNicknameSanitizationHTTPLinks(t *testing.T) {
	withHTTP := "Jan http://evil.com Novák"
	clean := sanitizeNickname(withHTTP)
	if strings.Contains(clean, "http://") {
		t.Errorf("Prezdivka nesmi obsahovat http:// odkazy: %s", clean)
	}

	withHTTPS := "Jan https://evil.com Novák"
	cleanS := sanitizeNickname(withHTTPS)
	if strings.Contains(cleanS, "https://") {
		t.Errorf("Prezdivka nesmi obsahovat https:// odkazy: %s", cleanS)
	}
}

// TestNicknameSanitizationAtHere overuje odstraneni @here tagu
func TestNicknameSanitizationAtHere(t *testing.T) {
	withHere := "Admin @here Novák"
	clean := sanitizeNickname(withHere)
	if strings.Contains(clean, "@here") {
		t.Errorf("Prezdivka nesmi obsahovat @here: %s", clean)
	}
}

// TestNicknameSanitizationNormalName overuje, ze bezne jmeno prochazi beze zmeny
func TestNicknameSanitizationNormalName(t *testing.T) {
	normal := "Jan Novák"
	clean := sanitizeNickname(normal)
	if clean != "Jan Novák" {
		t.Errorf("Bezne jmeno nesmi byt modifikovano: ocekavano 'Jan Novák', ziskano '%s'", clean)
	}
}

// ============================================================================
// ANTI-MULTI-ACCOUNTING (1:1 VAZBA)
// ============================================================================

// TestAntiMultiAccounting testuje striktni vazbu 1:1 mezi TUL uctem a Discord uctem
func TestAntiMultiAccounting(t *testing.T) {
	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDB["discord-user-1"] = &Student{
		MicrosoftID: "ms-student-1",
		DiscordID:   "discord-user-1",
		Name:        "Student Jedna",
		Email:       "student1@tul.cz",
	}
	usersDBMux.Unlock()

	// Pokus ověřit STEJNÉ Microsoft ID s JINÝM Discord ID (sdílení školního účtu s kamarádem)
	cheatAttempt1 := &Student{
		MicrosoftID: "ms-student-1",
		DiscordID:   "discord-user-2-kamarad",
		Name:        "Kamarád",
		Email:       "student1@tul.cz",
	}
	if err := checkBindingAllowed(cheatAttempt1); err == nil {
		t.Error("Systém MUSÍ zamítnout ověření jiného Discord účtu se stejným Microsoft ID")
	}

	// Pokus ověřit STEJNÝ Discord účet s JINÝM Microsoft ID
	cheatAttempt2 := &Student{
		MicrosoftID: "ms-student-2",
		DiscordID:   "discord-user-1",
		Name:        "Student Dva",
		Email:       "student2@tul.cz",
	}
	if err := checkBindingAllowed(cheatAttempt2); err == nil {
		t.Error("Systém MUSÍ zamítnout přepsání již ověřeného Discord účtu jiným Microsoft ID")
	}

	// Znovusparovani stejneho uctu musi projit
	validReauth := &Student{
		MicrosoftID: "ms-student-1",
		DiscordID:   "discord-user-1",
		Name:        "Student Jedna",
		Email:       "student1@tul.cz",
	}
	if err := checkBindingAllowed(validReauth); err != nil {
		t.Errorf("Reautentizace téhož účtu musí projít, chyba: %v", err)
	}
}

// TestAntiMultiAccountingEmpty overuje, ze prazdna databaze umozni jakykoliv ucet
func TestAntiMultiAccountingEmpty(t *testing.T) {
	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	student := &Student{
		MicrosoftID: "ms-new-student",
		DiscordID:   "dc-new-student",
	}
	if err := checkBindingAllowed(student); err != nil {
		t.Errorf("Novy ucet v prazdne databazi musi byt povolen: %v", err)
	}
}

// ============================================================================
// PERZISTENCE (users.json)
// ============================================================================

// TestUsersStorage testuje atomicky zapis a perzistenci
func TestUsersStorage(t *testing.T) {
	tempDir := t.TempDir()
	originalFile := UsersStorageFile
	UsersStorageFile = filepath.Join(tempDir, "test_users.json")
	defer func() { UsersStorageFile = originalFile }()

	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	student := &Student{
		MicrosoftID: "ms-secure-id",
		DiscordID:   "dc-secure-id",
		Name:        "Bezpečný Student",
		Email:       "bezpecny@tul.cz",
		Faculty:     "FM",
		Role:        "Student FM",
		VerifiedAt:  time.Now().UTC().Truncate(time.Second),
	}

	if err := saveUser(student); err != nil {
		t.Fatalf("Chyba saveUser: %v", err)
	}

	info, err := os.Stat(UsersStorageFile)
	if err != nil {
		t.Fatalf("Soubor %s nebyl vytvoren", UsersStorageFile)
	}

	// Kontrola prav 0600 (pouze vlastnik procesu)
	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Errorf("Soubor musi mit prava 0600, ma: %o", mode)
	}

	// Znovunacteni z disku
	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	loadUsersStorage()

	usersDBMux.RLock()
	loaded, exists := usersDB["dc-secure-id"]
	usersDBMux.RUnlock()

	if !exists || loaded.Name != "Bezpečný Student" {
		t.Errorf("Data nebyla po nacteni spravna: %+v", loaded)
	}
}

// TestUsersStorageJSONFormat overuje, ze ulozeny soubor je validni JSON
func TestUsersStorageJSONFormat(t *testing.T) {
	tempDir := t.TempDir()
	originalFile := UsersStorageFile
	UsersStorageFile = filepath.Join(tempDir, "test_json_format.json")
	defer func() { UsersStorageFile = originalFile }()

	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	student := &Student{
		MicrosoftID: "ms-json-test",
		DiscordID:   "dc-json-test",
		Name:        "JSON Test",
		Email:       "json@tul.cz",
		Faculty:     "FM",
		Role:        "Student FM",
		VerifiedAt:  time.Now().UTC(),
	}
	_ = saveUser(student)

	data, err := os.ReadFile(UsersStorageFile)
	if err != nil {
		t.Fatalf("Nelze nacist soubor: %v", err)
	}

	var parsed []*Student
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Soubor neni validni JSON: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Email != "json@tul.cz" {
		t.Errorf("JSON data neodpovidaji: %+v", parsed)
	}
}

// TestUsersStorageMultipleUsers overuje spravnost pri vice uzivatelich
func TestUsersStorageMultipleUsers(t *testing.T) {
	tempDir := t.TempDir()
	originalFile := UsersStorageFile
	UsersStorageFile = filepath.Join(tempDir, "test_multi.json")
	defer func() { UsersStorageFile = originalFile }()

	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	for i := 0; i < 5; i++ {
		s := &Student{
			MicrosoftID: generateID(),
			DiscordID:   generateID(),
			Name:        "Student",
			Email:       "test@tul.cz",
			Faculty:     "FM",
			Role:        "Student FM",
			VerifiedAt:  time.Now().UTC(),
		}
		if err := saveUser(s); err != nil {
			t.Fatalf("Chyba pri ukladani uzivatele %d: %v", i, err)
		}
	}

	usersDBMux.RLock()
	count := len(usersDB)
	usersDBMux.RUnlock()

	if count != 5 {
		t.Errorf("Ocekavano 5 uzivatelu, ziskano: %d", count)
	}
}

// ============================================================================
// MOCK ENDPOINTY (KARANTENA)
// ============================================================================

// TestMockQuarantine overuje, ze v produkcnim rezimu (ENABLE_DEV_MOCK=false) jsou mock endpointy zablokovany
func TestMockQuarantine(t *testing.T) {
	cfg.EnableDevMock = false
	defer func() { cfg.EnableDevMock = true }()

	// 1. Mock MSAL musi vratit 404 Not Found
	reqMS := httptest.NewRequest("GET", "/mock-msal", nil)
	wMS := httptest.NewRecorder()
	handleMockMSAL(wMS, reqMS)
	if wMS.Code != http.StatusNotFound {
		t.Errorf("Pri ENABLE_DEV_MOCK=false musi /mock-msal vratit 404, ziskano: %d", wMS.Code)
	}

	// 2. Mock Discord musi vratit 404 Not Found
	reqDC := httptest.NewRequest("GET", "/mock-discord", nil)
	wDC := httptest.NewRecorder()
	handleMockDiscord(wDC, reqDC)
	if wDC.Code != http.StatusNotFound {
		t.Errorf("Pri ENABLE_DEV_MOCK=false musi /mock-discord vratit 404, ziskano: %d", wDC.Code)
	}
}

// TestMockMSALDevMode overuje, ze v dev rezimu mock MSAL vytvori session se studentem
func TestMockMSALDevMode(t *testing.T) {
	cfg.EnableDevMock = true

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mock-msal", nil)
	handleMockMSAL(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("Mock MSAL v dev rezimu ma presmerovat (302), ziskano: %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/" {
		t.Errorf("Mock MSAL ma presmerovat na '/', presmerovava na: %s", location)
	}
}

// TestMockDiscordDevMode overuje, ze v dev rezimu mock Discord vytvori a ulozi uzivatele
func TestMockDiscordDevMode(t *testing.T) {
	tempDir := t.TempDir()
	originalFile := UsersStorageFile
	UsersStorageFile = filepath.Join(tempDir, "test_mock.json")
	defer func() { UsersStorageFile = originalFile }()

	usersDBMux.Lock()
	usersDB = make(map[string]*Student)
	usersDBMux.Unlock()

	cfg.EnableDevMock = true

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mock-discord", nil)
	handleMockDiscord(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("Mock Discord v dev rezimu ma presmerovat (302), ziskano: %d", w.Code)
	}
}

// ============================================================================
// RATE LIMITING
// ============================================================================

// TestRateLimiter testuje token bucket limiter
func TestRateLimiter(t *testing.T) {
	testIP := "192.168.1.100"
	maxReqs := 3
	window := 100 * time.Millisecond

	// Prvni 3 pozadavky musi projit
	for i := 0; i < maxReqs; i++ {
		if !checkRateLimit(testIP, maxReqs, window) {
			t.Fatalf("Pozadavek %d mel projit limitem", i+1)
		}
	}

	// 4. pozadavek musi byt zablokovan
	if checkRateLimit(testIP, maxReqs, window) {
		t.Error("Prekroceni limitu melo byt zablokovano")
	}

	// Po uplynuti okna musi pozadavek opet projit
	time.Sleep(150 * time.Millisecond)
	if !checkRateLimit(testIP, maxReqs, window) {
		t.Error("Po uplynuti okna mel pozadavek opet projit")
	}
}

// TestRateLimiterDisabled overuje, ze pri vypnutem rate limitingu vsechny pozadavky prochazi
func TestRateLimiterDisabled(t *testing.T) {
	cfg.RateLimitEnabled = false
	defer func() { cfg.RateLimitEnabled = true }()

	testIP := "10.0.0.1-disabled"
	for i := 0; i < 100; i++ {
		if !checkRateLimit(testIP, 1, time.Millisecond) {
			t.Fatalf("Pozadavek %d mel projit pri vypnutem rate limitingu", i+1)
		}
	}
}

// TestRateLimiterDifferentIPs overuje, ze ruzne IP maji nezavisle limity
func TestRateLimiterDifferentIPs(t *testing.T) {
	ip1 := "10.1.1.1-test"
	ip2 := "10.2.2.2-test"

	// Vycerpame limit pro IP 1
	for i := 0; i < 5; i++ {
		checkRateLimit(ip1, 5, time.Minute)
	}
	if checkRateLimit(ip1, 5, time.Minute) {
		t.Error("IP1 by mela byt zablokovana")
	}

	// IP 2 musi stale prochazet
	if !checkRateLimit(ip2, 5, time.Minute) {
		t.Error("IP2 musi prochazet nezavisle na IP1")
	}
}

// ============================================================================
// HTTP BEZPECNOSTNI HLAVICKY
// ============================================================================

// TestSecurityHeaders overuje pritomnost obrannych HTTP hlavicek
func TestSecurityHeaders(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	securityMiddleware(dummyHandler).ServeHTTP(w, req)

	expectedHeaders := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, expected := range expectedHeaders {
		if w.Header().Get(header) != expected {
			t.Errorf("Chybi %s: %s, ocekavano: %s", header, w.Header().Get(header), expected)
		}
	}

	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Errorf("Neplatna Content-Security-Policy: %s", w.Header().Get("Content-Security-Policy"))
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Chybi Cache-Control: no-store")
	}
	if !strings.Contains(w.Header().Get("Permissions-Policy"), "camera=()") {
		t.Errorf("Chybi Permissions-Policy: %s", w.Header().Get("Permissions-Policy"))
	}
}

// TestSecurityHeadersHSTS overuje HSTS hlavicku pri HTTPS
func TestSecurityHeadersHSTS(t *testing.T) {
	origSecure := cfg.SecureCookies
	cfg.SecureCookies = true
	defer func() { cfg.SecureCookies = origSecure }()

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	securityMiddleware(dummyHandler).ServeHTTP(w, req)

	hsts := w.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "max-age=") {
		t.Errorf("HSTS hlavicka chybi pri SecureCookies=true: %s", hsts)
	}
}

// TestSecurityMiddlewareRateLimit overuje, ze middleware vraci 429 pri prekroceni limitu
func TestSecurityMiddlewareRateLimit(t *testing.T) {
	cfg.RateLimitEnabled = true

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Pouzijeme unikatni IP
	testIP := "99.99.99.99"

	for i := 0; i < 60; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = testIP + ":12345"
		w := httptest.NewRecorder()
		securityMiddleware(dummyHandler).ServeHTTP(w, req)
	}

	// 61. pozadavek musi byt zablokovan
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = testIP + ":12345"
	w := httptest.NewRecorder()
	securityMiddleware(dummyHandler).ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Ocekavan status 429, ziskano: %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Chybi Retry-After hlavicka u 429 odpovedi")
	}
}

// ============================================================================
// IP DETEKCE
// ============================================================================

// TestGetClientIP overuje spravnou extrakci IP adresy z ruznych HTTP hlavicek
func TestGetClientIP(t *testing.T) {
	// 1. X-Forwarded-For (reverse proxy)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	ip := getClientIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("X-Forwarded-For: ocekavana IP 203.0.113.50, ziskana: %s", ip)
	}

	// 2. X-Real-IP
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Real-IP", "198.51.100.22")
	ip2 := getClientIP(req2)
	if ip2 != "198.51.100.22" {
		t.Errorf("X-Real-IP: ocekavana IP 198.51.100.22, ziskana: %s", ip2)
	}

	// 3. RemoteAddr fallback
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.RemoteAddr = "192.168.1.1:54321"
	ip3 := getClientIP(req3)
	if ip3 != "192.168.1.1" {
		t.Errorf("RemoteAddr: ocekavana IP 192.168.1.1, ziskana: %s", ip3)
	}

	// 4. X-Forwarded-For ma prioritu pred X-Real-IP
	req4 := httptest.NewRequest("GET", "/", nil)
	req4.Header.Set("X-Forwarded-For", "1.2.3.4")
	req4.Header.Set("X-Real-IP", "5.6.7.8")
	ip4 := getClientIP(req4)
	if ip4 != "1.2.3.4" {
		t.Errorf("X-Forwarded-For ma mit prioritu, ziskana IP: %s", ip4)
	}
}

// ============================================================================
// HTTP HANDLERY
// ============================================================================

// TestHandleIndex overuje, ze hlavni stranka vraci validni HTML s klikovymi prvky
func TestHandleIndex(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Index ma vratit 200, ziskano: %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "FM TUL") {
		t.Error("Index musi obsahovat 'FM TUL'")
	}
	if !strings.Contains(body, "Microsoft") || !strings.Contains(body, "login.microsoftonline.com") {
		t.Error("Index musi obsahovat odkaz na Microsoft prihlaseni")
	}
	if !strings.Contains(body, "text/html") {
		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Content-Type musi byt text/html: %s", contentType)
		}
	}
}

// TestHandleIndexWithSession overuje index stranku s existujici session (krok 2)
func TestHandleIndexWithSession(t *testing.T) {
	// Vytvorime session s overenim Microsoftem (ale bez Discordu)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	sess.Student = &Student{
		MicrosoftID: "test-ms-id",
		Name:        "Test Student",
		Email:       "test@tul.cz",
		Faculty:     "FM",
		Role:        "Student FM",
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleIndex(w2, req2)

	body := w2.Body.String()
	if !strings.Contains(body, "Test Student") {
		t.Error("Index s overenim musi zobrazit jmeno studenta")
	}
	if !strings.Contains(body, "discord.com") {
		t.Error("Index s overenim (krok 2) musi zobrazit odkaz na Discord prihlaseni")
	}
}

// TestHandleMSALWithoutSession overuje, ze MSAL callback bez platne session vraci chybu
func TestHandleMSALWithoutSession(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/msal?code=test-code&state=test-state", nil)
	handleMSAL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("MSAL bez session musi vratit 400, ziskano: %d", w.Code)
	}
}

// TestHandleMSALCSRFProtection overuje CSRF ochranu na MSAL callbacku
func TestHandleMSALCSRFProtection(t *testing.T) {
	// Vytvorime session s platnym state
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	sess.MSALState = "valid-state-token"

	// Pokus s NEPLATNYM state (CSRF utok)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/msal?code=test-code&state=invalid-csrf-state", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleMSAL(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("MSAL s neplatnym state musi vratit 403 (CSRF ochrana), ziskano: %d", w2.Code)
	}
}

// TestHandleMSALEmptyState overuje odmitnuti prazdneho state parametru
func TestHandleMSALEmptyState(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	sess.MSALState = "valid-state"

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/msal?code=test-code&state=", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleMSAL(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("MSAL s prazdnym state musi vratit 403, ziskano: %d", w2.Code)
	}
}

// TestHandleMSALNoCode overuje presmerovani pri chybejicim code (uzivatel zrusil prihlaseni)
func TestHandleMSALNoCode(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	validState := generateStateToken()
	sess.MSALState = validState

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/msal?state="+validState, nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleMSAL(w2, req2)

	if w2.Code != http.StatusFound {
		t.Errorf("MSAL bez code musi presmerovat (302), ziskano: %d", w2.Code)
	}
}

// TestHandleDiscordWithoutSession overuje, ze Discord callback bez session presmerovava
func TestHandleDiscordWithoutSession(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/discord?code=test-code&state=test-state", nil)
	handleDiscord(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("Discord bez session musi presmerovat (302), ziskano: %d", w.Code)
	}
}

// TestHandleDiscordCSRFProtection overuje CSRF ochranu na Discord callbacku
func TestHandleDiscordCSRFProtection(t *testing.T) {
	// Vytvorime session s overenim a platnym Discord state
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	sess.Student = &Student{
		MicrosoftID: "test-ms",
		Name:        "Test",
		Email:       "test@tul.cz",
	}
	sess.DiscordState = "valid-discord-state"

	// Pokus s NEPLATNYM state
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/discord?code=test-code&state=invalid-csrf", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleDiscord(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("Discord s neplatnym state musi vratit 403 (CSRF), ziskano: %d", w2.Code)
	}
}

// TestHandleDiscordWithoutStudent overuje, ze Discord callback bez overeni Microsoftem presmerovava
func TestHandleDiscordWithoutStudent(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)
	sess.Student = nil // Bez overeni Microsoftem

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/discord?code=test-code&state=test-state", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	handleDiscord(w2, req2)

	if w2.Code != http.StatusFound {
		t.Errorf("Discord bez studenta musi presmerovat (302), ziskano: %d", w2.Code)
	}
}

// TestHandleLogout overuje, ze logout smazne session a presmeruje
func TestHandleLogout(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logout", nil)
	handleLogout(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("Logout musi presmerovat (302), ziskano: %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/" {
		t.Errorf("Logout ma presmerovat na '/', presmerovava na: %s", location)
	}

	cookieHeader := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, SessionCookieName) {
		t.Error("Logout musi nastavit expirovany session cookie")
	}
}

// ============================================================================
// DISCORD REAKCE
// ============================================================================

// TestIsVerifyReaction testuje filtry reakci
func TestIsVerifyReaction(t *testing.T) {
	botID := "bot-id-123"
	userID := "user-id-456"

	if isVerifyReaction("🎓", "🎓", "msg-1", botID, botID) {
		t.Error("Vlastní reakce bota nesmí vyvolat ověření")
	}
	if !isVerifyReaction("🎓", "🎓", "msg-1", botID, userID) {
		t.Error("Reakce 🎓 od uživatele musí vyvolat ověření")
	}
	if isVerifyReaction("👍", "👍", "msg-1", botID, userID) {
		t.Error("Náhodné emoji nesmí vyvolat ověření")
	}
}

// TestIsVerifyReactionWithMessageID overuje filtr reakci s nastavenym VerifyMessageID
func TestIsVerifyReactionWithMessageID(t *testing.T) {
	origMsgID := cfg.DiscordVerifyMessageID
	cfg.DiscordVerifyMessageID = "specific-msg-123"
	defer func() { cfg.DiscordVerifyMessageID = origMsgID }()

	botID := "bot-id-123"
	userID := "user-id-456"

	// Reakce na spravne zprave musi projit (jakekoliv emoji kdyz je nastaven message ID)
	if !isVerifyReaction("🎓", "🎓", "specific-msg-123", botID, userID) {
		t.Error("Reakce na spravne zprave s 🎓 musi projit")
	}

	// Reakce na JINE zprave nesmi projit
	if isVerifyReaction("🎓", "🎓", "wrong-msg-999", botID, userID) {
		t.Error("Reakce na jine zprave nesmi projit pri nastavenem VerifyMessageID")
	}
}

// TestIsVerifyReactionCustomEmoji overuje vlastni emoji pro verifikaci
func TestIsVerifyReactionCustomEmoji(t *testing.T) {
	origEmoji := cfg.DiscordVerifyEmoji
	cfg.DiscordVerifyEmoji = "✅"
	defer func() { cfg.DiscordVerifyEmoji = origEmoji }()

	botID := "bot-id-123"
	userID := "user-id-456"

	if !isVerifyReaction("✅", "✅", "msg-1", botID, userID) {
		t.Error("Vlastni emoji ✅ musi vyvolat overeni")
	}
	if isVerifyReaction("🎓", "🎓", "msg-1", botID, userID) {
		t.Error("Vychozi 🎓 nesmi vyvolat overeni kdyz je vlastni emoji ✅")
	}
}

// ============================================================================
// URCENI ROLE
// ============================================================================

// TestDetermineRole testuje spravne urceni role
func TestDetermineRole(t *testing.T) {
	tests := []struct {
		name     string
		jobTitle string
		email    string
		want     string
	}{
		{"Student bez pozice", "", "jan.novak@tul.cz", "Student FM"},
		{"Zamestnanec s pozici", "odborný asistent", "karel@tul.cz", "Zaměstnanec FM"},
		{"Profesor", "profesor", "prof@tul.cz", "Zaměstnanec FM"},
		{"Doktorand bez pozice", "", "doktorand@fm.tul.cz", "Student FM"},
		{"Libovolna pozice", "sekretářka", "admin@tul.cz", "Zaměstnanec FM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineRole(tt.jobTitle, tt.email)
			if got != tt.want {
				t.Errorf("determineRole(%q, %q) = %q, ocekavano %q", tt.jobTitle, tt.email, got, tt.want)
			}
		})
	}
}

// ============================================================================
// MSAL RATE LIMITING
// ============================================================================

// TestHandleMSALRateLimit overuje rate limiting na MSAL endpointu
func TestHandleMSALRateLimit(t *testing.T) {
	cfg.RateLimitEnabled = true
	testIP := "88.88.88.88"

	// Vytvorime session
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sess := getOrCreateSession(w, req)

	// Vyplnime 10 pozadavku (limit na MSAL je 10/min)
	for i := 0; i < 10; i++ {
		sess.MSALState = generateStateToken()
		wr := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/msal?code=test&state=wrong", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
		r.RemoteAddr = testIP + ":12345"
		handleMSAL(wr, r)
	}

	// 11. pozadavek na MSAL musi byt zablokovan
	sess.MSALState = generateStateToken()
	wr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/msal?code=test&state=wrong", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	r.RemoteAddr = testIP + ":12345"
	handleMSAL(wr, r)

	if wr.Code != http.StatusTooManyRequests {
		t.Errorf("11. MSAL pozadavek musi vratit 429, ziskano: %d", wr.Code)
	}
}

