# FM TUL ↔ Discord Connector (Go)

Integrační webový konektor a Discord bot v jazyce **Go (Golang)** inspirovaný architekturou projektu [Mapetr/discord-pslib-connector](https://github.com/Mapetr/discord-pslib-connector), přizpůsobený pro studenty a zaměstnance **Fakulty mechatroniky, informatiky a mezioborových studií TUL (FM TUL)**.

Aplikace běží jako **jediná samostatná Go binárka** bez nutnosti instalovat Node.js, Python, Redis nebo externí SQL servery.

---

## 🚀 Jak proces funguje pro uživatele

1. **Uživatel na Discordu klikne na reakci 🎓 pod zprávou** v kanálu (např. `#overeni`).
2. Bot mu obratem zašle **soukromou zprávu (DM)** s přímým odkazem na webový konektor.
3. Uživatel otevře odkaz v prohlížeči a provede 2 jednoduché kroky:
   - **Krok 1:** Přihlásí se školním Microsoft účtem (`@tul.cz`).
   - **Krok 2:** Propojí svůj Discord účet.
4. Bot uživatele na Discord serveru automaticky zařadí:
   - Přidá mu roli **Student FM** (nebo **Zaměstnanec FM**) a **Ověřený**.
   - Sjednotí jeho přezdívku podle skutečného jména z TUL identit.
   - Odemknou se mu neveřejné fakultní místnosti.

---

## 🛠️ Konfigurace prostředí (`.env`)

Vytvořte soubor `.env` (zkopírováním ze vzoru `.env.example`):
```bash
cp .env.example .env
```

### 1. Microsoft OAuth2 (TUL Tenant / LIANE)
- `MSAL_CLIENT_ID`: ID aplikace z TUL tenantu
- `MSAL_CLIENT_SECRET`: Tajný klíč aplikace z TUL tenantu
- `MSAL_TENANT_ID`: Directory (tenant) ID univerzity TUL
*(Redirect URI pro LIANE je: `http://localhost:8000/msal` nebo `https://vase-domena/msal`)*

### 2. Discord Developer Portal (OAuth2 & Bot)
V [Discord Developer Portal](https://discord.com/developers/applications):
- **Záložka General Information / OAuth2:**
  - `DISCORD_CLIENT_ID`: Client ID aplikace
  - `DISCORD_CLIENT_SECRET`: Client Secret aplikace
  - **Redirects:** Přidejte `http://localhost:8000/discord` (pro vývoj) a `https://vase-domena/discord` (pro produkci)
- **Záložka Bot:**
  - `DISCORD_TOKEN`: Token bota
  - **Privileged Gateway Intents:** Zapněte *Server Members Intent*
- **Role a Server:**
  - `DISCORD_GUILD_ID`: ID vašeho Discord serveru
  - `DISCORD_VERIFIED_ID`: ID role pro ověřené uživatele (např. *Ověřený*)
  - `DISCORD_FM_STUDENT_ID`: ID role pro studenty FM (*Student FM*)
  - `DISCORD_FM_STAFF_ID`: ID role pro vyučující/zaměstnance (*Zaměstnanec FM*)
  - `DISCORD_INVITE_URL`: Odkaz na pozvánku na váš server

---

## 📢 Jak odeslat ověřovací zprávu na Discord (pro správce)
1. Připojte bota na váš server s právy číst a odesílat zprávy a přidávat reakce.
2. V libovolném kanálu (např. `#overeni` nebo `#pravidla`) napište jako správce příkaz:
   ```text
   !setup-overeni
   ```
3. Bot automaticky:
   - Smaže váš příkaz, aby byl kanál čistý.
   - Pošle přehlednou oficiální zprávu s tlačítkem *Ověřit identitu přes web*.
   - Automaticky přidá reakci **🎓**.
4. Od této chvíle stačí, když kterýkoliv student nebo zaměstnanec klikne na reakci **🎓** (nebo na tlačítko).

---

## 💻 Spuštění a kompilace

### Spuštění ve vývoji
```bash
go run main.go
```

### Sestavení produkční binárky
```bash
go build -buildvcs=false -o bot .
./bot
```

Aplikace spustí webový server (výchozí port `8000`) a připojí Discord bota.

---

## 🧪 Vývojářská simulace (Mock režim)

Pro vyzkoušení celého webového flow a přidělování rolí v Discordu **nemusíte čekat na vyřízení registrací**:
1. Otevřete v prohlížeči `http://localhost:8000`.
2. Ve spodní liště klikněte na **Simulovat MS** (nasimuluje přihlášení studenta FM).
3. Následně klikněte na **Simulovat Discord** (nasimuluje spárování s Discordem a uloží záznam).
4. Otestujte odhlášení tlačítkem **Odhlásit**.

---

## 🛡️ Bezpečnostní architektura (Hardening)

Aplikace implementuje ochranu na úrovni enterprise systémů (Defense-in-Depth):

1. **OAuth2 CSRF & PKCE (RFC 7636):**
   - Všechny požadavky na Microsoft i Discord jsou chráněny kryptografickým jednorázovým `state` tokenem (porovnávaným v konstantním čase `subtle.ConstantTimeCompare`).
   - Microsoft přihlášení vynucuje **PKCE** (`code_verifier` a `code_challenge` S256), což brání odposlechu nebo injektáži autorizačního kódu.

2. **Ochrana proti Multi-Accountingu (1:1 vazba):**
   - Jeden univerzitní TUL účet smí ověřit **výhradně jeden** Discord účet.
   - Pokus o ověření druhého Discord účtu stejným univerzitním emailem je okamžitě zamítnut (`409 Conflict`).

3. **Karanténa Mock Endpointů (`ENABLE_DEV_MOCK`):**
   - V produkci je `ENABLE_DEV_MOCK=false`. Vývojářské simulace jsou kompletně nedostupné (`404 Not Found`), což vylučuje neoprávněné získání role.

4. **Striktní validace univerzitní identity:**
   - E-mail uživatele musí končit striktně na `@tul.cz` nebo fakultní subdoménu (např. `@fm.tul.cz`). Jakékoliv cizí účty jsou odmítnuty (`403 Forbidden`).

5. **Sanitizace Discord přezdívek:**
   - Automatické odstranění nebezpečných tagů (`@everyone`, `@here`, `<@...>`, `discord.gg`).
   - Odstranění neviditelných znaků (Zero-Width Spaces), řídicích znaků a markdown formátování.
   - Limit délky přezdívky na 32 znaků (dle Discord API).

6. **Rate Limiting & DoS ochrana:**
   - Vestavěný token-bucket limiter na IP adresu (ochrana webového rozhraní).
   - Omezení frekvence reakcí na Discordu (cooldown 30 sekund na uživatele pro ochranu Discord API a zabránění DM spamu).
   - Hardened timeouty HTTP serveru (`ReadHeaderTimeout: 5s`, `IdleTimeout: 60s`, `MaxHeaderBytes: 1MB`).

7. **Bezpečnostní HTTP hlavičky:**
   - `Content-Security-Policy`: Striktní zamezení XSS a externích skriptů.
   - `X-Frame-Options: DENY`: Ochrana proti Clickjackingu.
   - `X-Content-Type-Options: nosniff`: Ochrana proti MIME sniffing.
   - `Cache-Control: no-store, no-cache`: Zamezení ukládání citlivých stránek do mezipaměti.
   - Ochrana cookies: `HttpOnly`, `SameSite=Lax`, `Secure`.

8. **Atomické ukládání databáze s právy `0600`:**
   - Soubor `users.json` je zapisován atomicky (přes dočasný soubor a `rename`) s přístupovými právy `0600` (pouze pro proces bota).

