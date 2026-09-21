# Knowledge Base Index – FM TUL Discord Connector

*Automaticky vygenerováno: 2026-09-21 16:42:46 CEST*

## Dokumentace v Knowledge Base

- [Architektura systému](architecture.md) – Moduly, toky dat, background workery.
- [Bezpečnostní model](security.md) – HMAC sessions, PKCE, Origin validace, anti-multi-accounting, SSRF ochrana.
- [OAuth2 Průběh](oauth-flow.md) – Sekvenční diagram, Microsoft Entra ID a Discord OAuth2 integrace.
- [Slash Příkazy](slash-commands.md) – Registr a specifikace příkazů pro Discord.
- [Konfigurace](config-reference.md) – Kompletní matice proměnných `.env` a `discord_config.json`.
- [AI Context Snapshot](AI_CONTEXT.md) – Kompaktní přehled projektu pro rychlé načtení do kontextu AI.

## Metriky projektu

- **Celkem zdrojových Go souborů:** 32
- **Testovací soubory:** 6
- **Celkový počet řádků Go kódu:** ~4075
- **Aktivních Slash příkazů a podpříkazů:** 12
- **Aktivních HTTP endpointů:** 6

## Slash Příkazy v repozitáři

| Příkaz | Oprávnění | Popis | Zdrojový soubor |
| :--- | :--- | :--- | :--- |
| `/config set-channel` | Administrátor | Nastaví kanál pro vybranou funkci | `internal/discord/cmd_config.go` |
| `/config set-message` | Administrátor | Nastaví vlastní uvítací zprávu (podporuje Markdown) | `internal/discord/cmd_config.go` |
| `/config set-role` | Administrátor | Nastaví roli pro vybranou skupinu | `internal/discord/cmd_config.go` |
| `/config view` | Administrátor | Zobrazí aktuální stav konfigurace (z JSON a .env) | `internal/discord/cmd_config.go` |
| `/delete` | Správa zpráv | Bot smaže zprávu podle ID (pouze pro správce) | `internal/discord/cmd_delete.go` |
| `/edit` | Správa zpráv | Bot upraví svou vlastní zprávu (pouze pro správce) | `internal/discord/cmd_edit.go` |
| `/overit` | Všichni členové | Ověření identity studenta/zaměstnance FM TUL a získání rolí | `internal/discord/cmd_verify.go` |
| `/rss add` | Správa zpráv | Přidá nový RSS feed | `internal/discord/cmd_rss.go` |
| `/rss list` | Správa zpráv | Zobrazí sledované RSS feedy | `internal/discord/cmd_rss.go` |
| `/rss remove` | Správa zpráv | Odstraní RSS feed | `internal/discord/cmd_rss.go` |
| `/say` | Správa zpráv | Bot odešle zprávu do zvoleného kanálu (pouze pro správce) | `internal/discord/cmd_say.go` |
| `/setup` | Administrátor | Odešle oficiální ověřovací zprávu s tlačítkem (pouze administrátor) | `internal/discord/cmd_setup.go` |

## HTTP Endpointy (`internal/web/server.go`)

| Metoda | Cesta | Handler |
| :--- | :--- | :--- |
| `GET` | `/` | `HandleIndex` |
| `GET` | `/msal` | `HandleMSAL` |
| `GET` | `/discord` | `HandleDiscord` |
| `GET` | `/mock-msal` | `HandleMockMSAL` |
| `GET` | `/mock-discord` | `HandleMockDiscord` |
| `GET` | `/logout` | `HandleLogout` |
