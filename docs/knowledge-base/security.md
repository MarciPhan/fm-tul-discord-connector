# Bezpečnostní model a Defense-in-Depth

Dokument detailně popisuje implementované bezpečnostní mechanismy aplikace `sbibolet` pro splnění vysokých bezpečnostních nároků univerzitního IT prostředí.

---

## 1. Ochrana relací (Sessions) a Cookies

### 1.1 Podepisování cookies pomocí HMAC-SHA256
- Session identifikátor (`ID`) je 128-bitový kryptograficky náhodný řetězec generovaný přes `crypto/rand`.
- Hodnota cookie `tul_session` je uložena ve formátu `<ID>.<SIGNATURE>`, kde signatura je Base64URL(HMAC-SHA256(ID, HMAC_SECRET)).
- Při každém požadavku funkce `VerifySessionID` porovná signaturu pomocí `subtle.ConstantTimeCompare` (prevence timing attacků).
- Jakákoliv manipulace s cookie u klienta okamžitě vede k odmítnutí session bez vyzrazení detailů.

### 1.2 Atributy Cookie
- `HttpOnly: true`: Zabraňuje krádeži session cookie skrze XSS útoky.
- `SameSite: Lax`: Ochrana proti Cross-Site Request Forgery (CSRF).
- `Secure`: Vynuceno při HTTPS spojení (`TLS != nil`, `X-Forwarded-Proto == https` nebo `SECURE_COOKIES=true`).
- `Domain`: Nastavitelná hodnota `COOKIE_DOMAIN` (např. `login.fm.tul.cz`), zamezující přístupu ze sousedních subdomén.

---

## 2. Prevence CSRF a Replay útoků

### 2.1 Dvoustupňové kryptografické State tokeny
- Microsoft i Discord OAuth toky generují jednorázový 256-bitový hexadecimální token (`GenerateStateToken`).
- Token je svázán se session na serveru.
- Při návratu z OAuth poskytovatele metoda `session.ConsumeMSALState(state)` resp. `ConsumeDiscordState(state)` provede atomické ověření v konstantním čase a okamžité smazání (consume).
- Replay útoky (opakované odeslání autorizačního kódu) jsou okamžitě zablokovány s kódem 403 Forbidden.

### 2.2 PKCE (Proof Key for Code Exchange – RFC 7636)
- Použita metoda `S256`.
- `code_verifier` (32 kryptografických bajtů) je uložen v bezpečné serverové session.
- `code_challenge` je SHA-256 hash verifieru odeslaný Microsoft Entra ID.
- Zabraňuje Authorization Code Interception na úrovni sítě.

### 2.3 Striktní validace hlavičky `Origin`
- HTTP middleware validuje hlavičku `Origin` proti normalizované `BASE_URL` (schéma a host, bez koncových lomítek).
- Pokusy o odeslání požadavků z neautorizovaných webových stránek jsou blokovány a zalogovány do auditu.

---

## 3. Ochrana databáze a Anti-Multi-Accounting

### 3.1 Vazba 1:1 (Jeden student = jeden Discord účet)
- Kontrola probíhá atomicky uvnitř `usersMux.Lock()` v balíčku `storage`.
- Testuje se:
  - Zda stejné `MicrosoftID` již není registrováno pod jiným `DiscordID`.
  - Zda stejný `Email` (case-insensitive) již není registrován pod jiným `DiscordID`.
  - Zda stejné `DiscordID` se nepokouší přepsat vazbu na jiné `MicrosoftID`.
- Zabraňuje sdílení univerzitních účtů mezi studenty i multi-accountingu.

### 3.2 Atomický zápis s minimálními právy (0600)
- Ukládání souborů `users.json`, `feeds.json` a `discord_config.json` probíhá atomickým dvoufázovým zápisem:
  1. Vytvoření dočasného souboru (`os.CreateTemp`).
  2. Omezení práv pouze na vlastníka procesu (`0600`).
  3. Zápis kompletních dat a `Close`.
  4. Atomický `os.Rename` na cílový soubor.
- Při jakékoliv chybě zápisu je dočasný soubor smazán, čímž je vyloučeno poškození existující databáze.

---

## 4. Ochrana proti DoS a síťovým útokům

1. **Memory Exhaustion DoS limit:**
   - Middleware aplikuje `http.MaxBytesReader(w, r.Body, 1<<20)` (max. 1 MB). Pokusy o odeslání obřích payloadů jsou okamžitě ukončeny s kódem 413.
2. **Slowloris Timeouty:**
   - Server definuje explicitní `ReadHeaderTimeout` (5s), `ReadTimeout` (10s), `WriteTimeout` (15s) a `IdleTimeout` (60s).
3. **Rate Limiting:**
   - Klouzavé okno 60 požadavků / minuta na IP adresu klienta.
   - Dedikované rate limity pro OAuth callbacky (10 požadavků / minuta).
   - Validace IP adresy pomocí `net.ParseIP` proti podvržení hlaviček.
4. **SSRF (Server-Side Request Forgery) v RSS:**
   - Funkce `isValidRSSURL` zakazuje lokální adresy (`localhost`, `127.0.0.0/8`, `::1`), link-local IP (`169.254.0.0/16`) i privátní sítě (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
5. **Open Redirect ochrana:**
   - Před přesměrováním na Discord pozvánku je ověřeno, že URL striktně směřuje na `discord.gg` nebo `discord.com` s protokolem HTTPS.
