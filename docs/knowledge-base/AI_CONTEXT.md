# FM TUL Connector – AI Context Snapshot

*Generated at: 2026-09-21T14:42:46Z*

## Core Invariants for AI Agents
1. Storage: Zero external SQL/Redis. Atomic JSON writes only (`users.json`, `feeds.json`, `discord_config.json`) with `0600` permissions.
2. Concurrency: Never hold locks across network calls. Use `Session.Mux` helper methods (`GetStudent`, `SetStudent`, `ConsumeMSALState`, `ConsumeDiscordState`).
3. Discord: Always use `GetInteractionUser(i)`, `GetInteractionUsername(i)`, `GetInteractionUserID(i)` to avoid nil pointer panics in DMs.
4. Security: HMAC-SHA256 session signatures, PKCE RFC 7636, Origin header validation, anti-multi-accounting 1:1 binding, SSRF validation for RSS.
5. Config precedence: `discord_config.json` > `.env`.

## Registered Commands
- `/config set-channel` (Administrator): Configures the channel for selected feature (welcome or audit) (`cmd_config.go`)
- `/config set-message` (Administrator): Sets custom welcome message for newcomers (Markdown supported) (`cmd_config.go`)
- `/config set-role` (Administrator): Configures target role for selected group (verified, student, staff) (`cmd_config.go`)
- `/config view` (Administrator): Displays current configuration state (from JSON and .env) (`cmd_config.go`)
- `/delete` (Manage Messages): Safely deletes a message by its ID (managers only) (`cmd_delete.go`)
- `/edit` (Manage Messages): Edits a bot's own message in a channel (managers only) (`cmd_edit.go`)
- `/overit` (Everyone): Provides ephemeral verification link to the web portal (`cmd_verify.go`)
- `/rss add` (Manage Messages): Adds a new RSS feed for monitoring (SSRF protected) (`cmd_rss.go`)
- `/rss list` (Manage Messages): Lists all currently monitored RSS feeds (`cmd_rss.go`)
- `/rss remove` (Manage Messages): Removes a tracked RSS feed by ID (`cmd_rss.go`)
- `/say` (Manage Messages): Sends a message as the bot to a channel (managers only) (`cmd_say.go`)
- `/setup` (Administrator): Sends official verification embed with button and reaction (admin only) (`cmd_setup.go`)

## HTTP Routes
- `GET /` -> `HandleIndex`
- `GET /msal` -> `HandleMSAL`
- `GET /discord` -> `HandleDiscord`
- `GET /mock-msal` -> `HandleMockMSAL`
- `GET /mock-discord` -> `HandleMockDiscord`
- `GET /logout` -> `HandleLogout`
