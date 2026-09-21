# FM TUL ↔ Discord Connector 🎓

A secure, modular, and extensible Discord bot and web connector built in Go. It automatically verifies university identities via Microsoft Entra ID (TUL) and automatically assigns roles on the Discord server. 

## Key Features

- **Automated Identity Verification**: 1:1 binding between the university's Microsoft account (`@tul.cz`) and a Discord account.
- **Role Assignment**: Automatically assigns roles on Discord based on the user's status (Student / Employee).
- **Extensible Bot Framework**: Built with a command registry pattern for easy addition of new Slash commands.
- **Built-in Slash Commands**:
  - `/setup` - Creates and sends the interactive verification embed.
  - `/config` - Dynamically configures the bot directly from Discord (roles, channels, messages) without editing `.env`.
  - `/overit` - Provides a link to the web connector.
  - `/say`, `/edit`, `/delete` - Admin tools for bot message management.
  - `/rss` - Subscribe and manage RSS feeds (e.g. university/faculty news).
- **Asynchronous Audit Log**: Works as a "Big Brother", tracking web logins, blocked security threats (CSRF, Rate limits, Multi-accounting), admin actions, and native Discord member events in a dedicated channel using colored embeds.
- **RSS Automation**: Background worker checking for new RSS articles and posting them to designated channels.
- **Welcome Messages**: Customizable embeds for new guild members.
- **Hardened Security**: Features PKCE (RFC 7636), strict CSRF state tokens, IP-based Rate Limiting, Atomic File Writes for `users.json`, and secure HTTP headers.

---

## 🚀 Quick Start (Deployment)

We provide setup scripts for automated dependency checking, compilation, and starting the server.

### Linux & macOS
```bash
./scripts/start.sh
```
*Note: Make sure the script has execution rights: `chmod +x ./scripts/start.sh`*

### Windows
```cmd
scripts\start.bat
```

The script will automatically prompt you if your `.env` file is missing. 

---

## ⚙️ Configuration (.env)

Duplicate `.env.example` to `.env` and configure the essential parameters:

> [!TIP]
> You no longer need to configure Roles or Welcome/Audit channels in `.env`. You can configure them directly in Discord using the `/config` slash command. This will create a `discord_config.json` file which takes precedence over `.env`.

### Microsoft Entra ID (TUL)
- `MSAL_CLIENT_ID` - Client ID from Azure Portal
- `MSAL_CLIENT_SECRET` - Secret for the application
- `MSAL_TENANT_ID` - University Tenant ID

### Discord
- `DISCORD_TOKEN` - Bot token from the Discord Developer Portal
- `DISCORD_GUILD_ID` - The ID of your Discord Server
*(Note: Variables like `DISCORD_FM_STUDENT_ID` or `DISCORD_AUDIT_CHANNEL_ID` act as a fallback if you choose not to use the dynamic `/config` command.)*

### RSS Module
- `RSS_ENABLED="true"` - Enables the background RSS poller

### Security & Domains
- `HMAC_SECRET` - **CRITICAL:** Strong random key used to cryptographically sign session cookies.
- `COOKIE_DOMAIN` - Optional (e.g., `login.fm.tul.cz`), isolates cookies to a specific subdomain.
- `ENABLE_DEV_MOCK="false"` - **CRITICAL:** Set to false in production! Enables testing endpoints if true.
- `SECURE_COOKIES="true"` - Enforces HTTPS only cookies.

---

## 🛠️ Bot Commands

The bot utilizes Discord Slash Commands for intuitive interaction:

- `/setup` - Spawns the main verification Embed with a button and a reaction. *(Admin)*
- `/config` - Interactive sub-commands to set up roles, channels, and welcome messages dynamically. *(Admin)*
- `/overit` - Displays the verification link privately to the user.
- `/rss add <url> <channel>` - Adds a new RSS feed subscription to a channel. *(Admin)*
- `/rss list` - Lists all active RSS subscriptions. *(Admin)*
- `/rss remove <id>` - Removes a subscription. *(Admin)*
- `/say <channel> <message>` - Bot posts a message in the specified channel. *(Admin)*
- `/edit <channel> <message_id> <new_text>` - Bot edits its own message. *(Admin)*
- `/delete <channel> <message_id>` - Bot deletes a specific message. *(Admin)*

---

## 📁 Architecture

The project has been refactored into a scalable modular architecture:

- `cmd/bot/` - Main entrypoint (`main.go`).
- `internal/config/` - Environment loader and central configuration.
- `internal/security/` - Cryptography, rate limiter, validation and HTTP middleware.
- `internal/session/` - Cookie-based sessions with sliding expirations.
- `internal/storage/` - JSON-based atomic storage for user identities (`users.json`).
- `internal/web/` - HTTP Handlers and OAuth callbacks.
- `internal/discord/` - Bot lifecycle, event handlers, and the command registry.

---

## 🛡️ Security Details

1. **HMAC-SHA256 Session Signing**: Prevents session hijacking; any client-side cookie modification invalidates the session.
2. **Subdomain Protection**: Validates the `Origin` header and restricts `COOKIE_DOMAIN` to prevent cross-site request forgery.
3. **Anti-DoS Protection**: Utilizes `http.MaxBytesReader` to strictly limit payload sizes (e.g., 1MB) to prevent memory exhaustion attacks.
4. **Anti-Multi-Accounting**: The database ensures a strict 1:1 mapping. One TUL account can only link to one Discord account and vice-versa.
5. **PKCE & CSRF**: OAuth2 implementation utilizes the latest recommended standards.
6. **Atomic Writes**: `users.json` uses temporary file swapping and `0600` permissions to prevent data corruption during simultaneous verifications.
7. **Timing Attacks**: Token comparisons use `subtle.ConstantTimeCompare`.

## 📝 License
Built for the Faculty of Mechatronics, Informatics and Interdisciplinary Studies at Technical University of Liberec (TUL).
