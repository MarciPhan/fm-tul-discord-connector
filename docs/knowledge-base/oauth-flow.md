# Průběh OAuth2 Autentizace a Propojení Účtů

Tento dokument detailně popisuje kompletní tok dvoufázové autentizace (Microsoft Entra ID + Discord OAuth2) s interakcí koncového uživatele a API rozhraní.

---

## 1. Sekvenční diagram toku

```mermaid
sequenceDiagram
    autonumber
    actor User as Uživatel (Student / Zaměstnanec)
    participant Browser as Prohlížeč
    participant App as FM TUL Bridge (Go Web)
    participant MSAL as Microsoft Entra ID (TUL)
    participant MSGraph as MS Graph API
    participant DiscordAuth as Discord OAuth2
    participant DiscordAPI as Discord Bot API
    participant Store as users.json

    User->>Browser: Klikne na odkaz /overit (nebo tlačítko na Discordu)
    Browser->>App: GET /
    App->>App: Vygeneruje Session (HMAC cookie), PKCE (verifier, challenge) a MSAL State
    App-->>Browser: Zobrazí Krok 1 (Přihlásit přes Microsoft) + Set-Cookie: tul_session
    
    User->>Browser: Klikne na "Přihlásit se přes Microsoft TUL"
    Browser->>MSAL: Přesměrování na authorize endpoint (state, challenge, scope: User.Read)
    User->>MSAL: Přihlášení školními údaji (@tul.cz)
    MSAL-->>Browser: Přesměrování zpět: GET /msal?code=XYZ&state=ABC
    
    Browser->>App: GET /msal?code=XYZ&state=ABC
    App->>App: Atomicky ověří a spotřebuje MSAL State
    App->>MSAL: POST /oauth2/v2.0/token (code, verifier, client_secret)
    MSAL-->>App: Vrací Access Token
    App->>MSGraph: GET /v1.0/me
    MSGraph-->>App: Vrací profil (jméno, e-mail, jobTitle)
    App->>App: Validace domény @tul.cz a určení role (Student FM / Zaměstnanec FM)
    App-->>Browser: Redirect na /
    
    Browser->>App: GET /
    App-->>Browser: Zobrazí Krok 2 (Ověřeno: Jméno, Role -> Tlačítko "Propojit Discord")
    
    User->>Browser: Klikne na "Propojit s Discord účtem"
    Browser->>DiscordAuth: Přesměrování na authorize endpoint (scope: identify guilds.join)
    User->>DiscordAuth: Schválení přístupu aplikace
    DiscordAuth-->>Browser: Přesměrování zpět: GET /discord?code=UVW&state=DEF
    
    Browser->>App: GET /discord?code=UVW&state=DEF
    App->>App: Atomicky ověří a spotřebuje Discord State
    App->>DiscordAuth: POST /api/v10/oauth2/token
    DiscordAuth-->>App: Vrací User Access Token
    App->>DiscordAPI: GET /api/v10/users/@me
    DiscordAPI-->>App: Vrací Discord ID a uživatelské jméno
    
    App->>Store: Kontrola vazby 1:1 (není účet již spárován?)
    App->>DiscordAPI: PUT /guilds/{id}/members/{id} (přidání na server, role, přezdívka)
    App->>Store: Atomické uložení záznamu do users.json
    App->>App: Odeslání záznamu do Audit Logu
    App-->>Browser: Přesměrování na Discord Invite URL
```

---

## 2. API Parametry a Scopes

### 2.1 Microsoft Graph
- **Scoping:** `User.Read openid profile email`
- **Koncový bod:** `https://graph.microsoft.com/v1.0/me?$select=id,displayName,mail,userPrincipalName,jobTitle,department`
- **Validace identity:**
  - E-mail je převzat z `mail`, případně záložně z `userPrincipalName`.
  - Prochází validací `IsValidTULEmail`, která striktně vyžaduje suffix `@tul.cz` nebo `.tul.cz`.
  - `DetermineRole` prověří přítomnost `jobTitle` nebo klíčového slova `zamestnanec` v e-mailu.

### 2.2 Discord OAuth2
- **Scoping:** `identify guilds.join`
- **Koncové body:**
  - Token exchange: `https://discord.com/api/v10/oauth2/token`
  - Profil uživatele: `https://discord.com/api/v10/users/@me`
  - Přidání člena na server / správa rolí: `https://discord.com/api/v10/guilds/{guild.id}/members/{user.id}`
