# FM TUL ↔ Discord Connector 🎓

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Security: Hardened](https://img.shields.io/badge/Security-Hardened-red.svg)](https://github.com/Mapetr/discord-pslib-connector)

Profesionální integrační platforma pro propojení univerzitních identit (Microsoft Entra ID) se servery na síti Discord. Projekt je postavený na vysoce modulární architektuře v jazyce **Go** a je primárně optimalizován pro **Fakultu mechatroniky, informatiky a mezioborových studií TUL (FM TUL)**.

Umožňuje bezpečné automatické přiřazování rolí (Student/Zaměstnanec) s ochranou proti zneužití (1:1 vazba). 

---

## ✨ Hlavní funkce

*   **Identitní brána (OAuth2):** Provázání fakultní e-mailové adresy (`@tul.cz` / `@fm.tul.cz`) s Discord účtem.
*   **Automatizace Rolí & Identit:** Automatické přidělení rolí, synchronizace jména a zabezpečení přezdívek proti neviditelným znakům a Pings (`@everyone`).
*   **RSS Modul (Background Worker):** Automatické stahování novinek z webů TUL/FMTUL a jejich chytré publikování do vybraných Discord kanálů.
*   **Slash Command Registr:** Plně rozšiřitelný framework pro přidávání administračních příkazů:
    *   `/overit` – Soukromé zaslání odkazu na webový portál.
    *   `/say`, `/edit`, `/delete` – Nástroje pro řízení bota z pozice správce.
    *   `/rss add`, `/rss list`, `/rss remove` – Kompletní správa odběrů.
*   **Welcome Messages:** Elegantní a plně přizpůsobitelné "Welcome embeds" do vstupní místnosti.
*   **Vysoká propustnost & Nízké nároky:** Výsledkem buildu je jediná binárka. Žádný Node.js, žádný Python, žádná nutnost spravovat složité SQL servery.

---

## 🛡️ "Military-Grade" Bezpečnost

Na základě požadavků univerzitního prostředí aplikace obsahuje **brutální zabezpečení (Defense-in-Depth):**

1. **HMAC-SHA256 Podepisování Relací (Session Hijacking):** Cookie s vaším identifikátorem relace je digitálně podepsána. Jakákoliv modifikace u klienta má za následek zničení a odepření spojení.
2. **Subdoménová Ochrana (Cross-Site Forgery):** Nastavitelný `COOKIE_DOMAIN` a striktní validace `Origin` hlavičky zabraňují zneužití sessions z jiných podvržených univerzitních subdomén.
3. **Anti-DoS (Memory Exhaustion Ochrana):** `MaxBytesReader` okamžitě ukončí spojení, pokud by se někdo pokusil server zahltit nadměrně velkým požadavkem (limit 1 MB payloadu).
4. **Anti Multi-Accounting:** Striktní 1:1 mapování uvnitř atomicky zapisované databáze (`users.json`). Jeden univerzitní e-mail nelze spárovat s více Discord účty.
5. **PKCE (RFC 7636):** Eliminace útoků zachycením autorizačního kódu (Authorization Code Interception Attack).
6. **Rate Limiting:** IP based Token Bucket (60 req/minuta).

---

## 🚀 Rychlý Start (Deployment)

Pro rychlé a bezbolestné nasazení jsme připravili skripty pro automatickou kontrolu závislostí a sestavení:

### Linux & macOS
```bash
chmod +x ./scripts/start.sh
./scripts/start.sh
```

### Windows
```cmd
scripts\start.bat
```

Skript se vás při prvním spuštění automaticky dotáže na vytvoření konfiguračního souboru `.env`.

---

## ⚙️ Konfigurace prostředí (`.env`)

Před spuštěním bota je nutné upravit parametry ve vašem `.env` souboru (vycházejte z `.env.example`).

### 1. Krypto & Subdoména
*   `HMAC_SECRET`: Zcela náhodný dlouhý tajný klíč pro podepisování cookies (Nezbytné!).
*   `COOKIE_DOMAIN`: Volitelné – (např. `login.fm.tul.cz`), pokud provozujete na dedikované subdoméně.
*   `SECURE_COOKIES`: `true` (zapne vynucování HTTPS protokolu v cookies).

### 2. Microsoft Entra ID (TUL)
*   `MSAL_CLIENT_ID`: ID vaší Azure Aplikace.
*   `MSAL_CLIENT_SECRET`: Klientské tajemství pro webové volání.
*   `MSAL_TENANT_ID`: Directory (Tenant) ID Univerzity.

### 3. Discord Modul
*   `DISCORD_TOKEN`: Token získaný z [Discord Dev Portal](https://discord.com/developers/applications).
*   `DISCORD_GUILD_ID`: ID vašeho primárního Discord serveru.
*   `DISCORD_FM_STUDENT_ID` / `DISCORD_FM_STAFF_ID`: ID rolí, jež budou přiděleny.
*   `RSS_ENABLED`: `true` nebo `false` pro aktivaci stahování novinek.

---

## 📁 Architektura (Pro vývojáře)

Projekt je pečlivě strukturován pro maximální udržitelnost. Vývoj nového příkazu znamená pouze přidat strukturu implementující `Command` interface.

```text
sbibolet/
├── cmd/bot/main.go            # Entrypoint (Start serveru a bot workerů)
├── internal/
│   ├── config/                # Načítání a validace konfigurace
│   ├── discord/               # Správa bota (Příkazy, Události, RSS modul)
│   ├── security/              # Middleware, Krypto, Validace e-mailů a HMAC
│   ├── session/               # Cookie-based sliding sessions management
│   ├── storage/               # Perzistence (Ukládání uživatelů)
│   └── web/                   # Webový HTTP server a Oauth callbacky
└── scripts/                   # Nástroje pro rychlé sestavení
```

## 📝 Testování a vývoj

Pro spuštění rozsáhlé testovací sady, která prověří kryptografické podpisy a paralelizaci zpracování požadavků, využijte:

```bash
go test -v -race ./...
```
*(Zahrnuje testy proti DoS, Rate Limiteru, generování ID, Origin Spoofingu a chování HMAC Cookies).*

### Lokální vývoj (Mocking)
Pokud si chcete vyvíjet UI webu bez přístupu k MS Azure nebo bez čekání, nastavte v `.env`:
`ENABLE_DEV_MOCK="true"`

Získáte tak na domovské obrazovce speciální tlačítka pro simulování obou flow. **V produkci musí být vypnuto!**

---
*Vyrobeno pro FM TUL.*
