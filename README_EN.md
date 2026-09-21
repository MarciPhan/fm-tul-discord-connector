# FM TUL ↔ Discord Connector 🎓

A secure, modular, and extensible Discord bot and web connector built in Go. It automatically verifies university identities via Microsoft Entra ID (TUL) and automatically assigns roles on the Discord server. 

## Key Features

- **Automated Identity Verification**: 1:1 binding between the university's Microsoft account (`@tul.cz`) and a Discord account.
- **Role Assignment**: Automatically assigns roles on Discord based on the user's status (Student / Employee).
- **Extensible Bot Framework**: Built with a command registry pattern for easy addition of new Slash commands.
- **Built-in Slash Commands**:
  - `/overit` - Provides a link to the web connector.
  - `/say`, `/edit`, `/delete` - Admin tools for bot message management.
  - `/rss` - Subscribe and manage RSS feeds (e.g. university/faculty news).
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

Duplicate `.env.example` to `.env` and configure the following parameters:

### Microsoft Entra ID (TUL)
- `MSAL_CLIENT_ID` - Client ID from Azure Portal
- `MSAL_CLIENT_SECRET` - Secret for the application
- `MSAL_TENANT_ID` - University Tenant ID

### Discord
- `DISCORD_TOKEN` - Bot token from the Discord Developer Portal
- `DISCORD_GUILD_ID` - The ID of your Discord Server
- `DISCORD_VERIFIED_ID` - Role ID for verified users
- `DISCORD_FM_STUDENT_ID` - Role ID for students
- `DISCORD_FM_STAFF_ID` - Role ID for staff

### RSS Module
- `RSS_ENABLED="true"` - Enables the background RSS poller

### Security
- `ENABLE_DEV_MOCK="false"` - **CRITICAL:** Set to false in production! Enables testing endpoints if true.
- `SECURE_COOKIES="true"` - Enforces HTTPS only cookies.

---

## 🛠️ Bot Commands

The bot utilizes Discord Slash Commands for intuitive interaction:

- `/overit` - Displays the verification link privately to the user.
- `/rss add <url> <channel>` - Adds a new RSS feed subscription to a channel. *(Admin)*
- `/rss list` - Lists all active RSS subscriptions. *(Admin)*
- `/rss remove <id>` - Removes a subscription. *(Admin)*
- `/say <channel> <message>` - Bot posts a message in the specified channel. *(Admin)*
- `/edit <channel> <message_id> <new_text>` - Bot edits its own message. *(Admin)*
- `/delete <channel> <message_id>` - Bot deletes a specific message. *(Admin)*
- `!setup-overeni` - Text command to spawn the main verification Embed with a button and a reaction.

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

1. **Anti-Multi-Accounting**: The database ensures a strict 1:1 mapping. One TUL account can only link to one Discord account and vice-versa.
2. **PKCE & CSRF**: OAuth2 implementation utilizes the latest recommended standards.
3. **Atomic Writes**: `users.json` uses temporary file swapping and `0600` permissions to prevent data corruption during simultaneous verifications.
4. **Timing Attacks**: Token comparisons use `subtle.ConstantTimeCompare`.

## 📝 License
Built for the Faculty of Mechatronics, Informatics and Interdisciplinary Studies at Technical University of Liberec (TUL).
