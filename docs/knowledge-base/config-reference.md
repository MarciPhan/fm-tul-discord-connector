# Referenční Příručka Konfigurace

Dokument detailně specifikuje všechny konfigurační parametry a hybridní systém konfigurace (`discord_config.json` vs `.env`).

---

## 1. Hybridní systém konfigurace

Aplikace využívá dvouvrstvou architekturu nastavení:
1. **Priorita 1 (Dynamická):** Soubor `discord_config.json` spravovaný administrátory za běhu přes slash příkaz `/config`. Umožňuje měnit role, kanály a uvítací zprávy bez restartu bota.
2. **Priorita 2 (Statická):** Proměnné prostředí ze souboru `.env` načítané při startu (`internal/config/config.go`).

Getter funkce (např. `config.GetAuditChannelID()`) nejprve zkontrolují přítomnost dynamické hodnoty; pokud není nastavena, použijí statickou hodnotu z `.env`.

---

## 2. Kompletní matice proměnných `.env`

| Proměnná | Typ | Výchozí hodnota | Popis a význam |
| :--- | :--- | :--- | :--- |
| `MSAL_CLIENT_ID` | String | *(Povinné)* | Application (Client) ID aplikace v Microsoft Entra ID. |
| `MSAL_CLIENT_SECRET` | String | *(Povinné)* | Klientské tajemství pro webové volání Microsoft Graph API. |
| `MSAL_TENANT_ID` | String | `common` | Directory (Tenant) ID TUL tenantu. |
| `DISCORD_CLIENT_ID` | String | *(Povinné)* | Aplikační ID bota v Discord Developer Portálu. |
| `DISCORD_CLIENT_SECRET` | String | *(Povinné)* | OAuth2 tajemství klienta na Discordu. |
| `DISCORD_TOKEN` | String | *(Povinné)* | Bot Token pro autorizaci Discord brány. |
| `DISCORD_GUILD_ID` | String | *(Povinné)* | ID primárního Discord serveru FM TUL. |
| `DISCORD_VERIFIED_ID` | String | `""` | ID role pro ověřené uživatele (fallback). |
| `DISCORD_FM_STUDENT_ID` | String | `""` | ID role pro studenty FM TUL (fallback). |
| `DISCORD_FM_STAFF_ID` | String | `""` | ID role pro zaměstnance FM TUL (fallback). |
| `DISCORD_INVITE_URL` | String | `""` | Cílová Discord pozvánka po úspěšném ověření. |
| `DISCORD_VERIFY_EMOJI` | String | `""` | Emoji použité pro reakční ověřování. |
| `DISCORD_AUDIT_CHANNEL_ID` | String | `""` | Kanál pro odesílání bezpečnostního auditu (fallback). |
| `DISCORD_WELCOME_CHANNEL_ID` | String | `""` | Kanál pro uvítací zprávy nováčků (fallback). |
| `DISCORD_WELCOME_MESSAGE` | String | Přednastavená | Výchozí text uvítací zprávy. |
| `RSS_ENABLED` | Boolean | `false` | Povolení modulu pro stahování novinek (`true`/`false`). |
| `HOST` | String | `0.0.0.0` | Síťové rozhraní pro HTTP server. |
| `PORT` | String | `8000` | Port HTTP serveru. |
| `BASE_URL` | String | `http://localhost:8000` | Veřejná URL adresa webového portálu (např. `https://login.fm.tul.cz`). |
| `COOKIE_DOMAIN` | String | `""` | Omezení platnosti cookies na subdoménu. |
| `HMAC_SECRET` | String | *(Povinné)* | Kryptografický klíč pro podepisování relací (min. 32 znaků). |
| `SECURE_COOKIES` | Boolean | `false` | Vynucení atributu `Secure` u cookies pro HTTPS. |
| `ENABLE_DEV_MOCK` | Boolean | `false` | Povolení tlačítek pro lokální testování bez Microsoftu/Discordu. |
| `RATE_LIMIT_ENABLED` | Boolean | `true` | Globální zapnutí/vypnutí IP rate limiteru. |

---

## 3. Schéma `discord_config.json`

```json
{
  "welcome_channel_id": "123456789012345678",
  "audit_channel_id": "123456789012345679",
  "verified_role_id": "123456789012345680",
  "fm_student_role_id": "123456789012345681",
  "fm_staff_role_id": "123456789012345682",
  "welcome_message": "Vítejte na oficiálním Discordu FM TUL!"
}
```
Zápis je chráněn atomickou výměnou dočasného souboru s právy `0600`.
