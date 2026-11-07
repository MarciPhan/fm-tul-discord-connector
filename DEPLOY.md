# Nasazení (Docker)

Požadavky: Docker + Docker Compose, reverse proxy s TLS pro `https://discord.fm.tul.cz` (alias `apps.fm.tul.cz`).

1. `cp .env.example .env` a vyplnit hodnoty (secrety dodá Jakub Marcinka zabezpečeným kanálem).
   - `HMAC_SECRET`: `openssl rand -hex 32` (bez něj aplikace nenastartuje)
   - `MSAL_CLIENT_ID`, `MSAL_TENANT_ID`: z registrace aplikace v LIANE
   - Přihlášení k Microsoftu certifikátem (bez client secretu): privátní klíč (PEM) uložit jako `certs/msal.key`, veřejný certifikát jako `certs/msal.crt` (`certs/` je v `.gitignore`, do kontejneru se mountuje read-only na `/certs`). Klíč musí být čitelný uživatelem 10001: `sudo chown 10001 certs/msal.key && chmod 400 certs/msal.key`. Místo `msal.crt` lze nastavit `MSAL_CLIENT_CERT_THUMBPRINT` (hex SHA-1 otisk z Entra ID). Certifikát musí být nahraný u registrace aplikace.
   - `MSAL_CLIENT_SECRET` je jen záložní varianta, použije se pouze pokud není nastaven `MSAL_CLIENT_KEY_PATH`
   - `COOKIE_DOMAIN` ponechat prázdné, pokud nepoběží na více subdoménách
2. `bash deploy.sh` (univerzální skript: kontroluje .env, generuje HMAC_SECRET, staví, spouští, ověřuje běh; log v `deploy.log`, chyby vypíše i s logy kontejneru)
3. Reverse proxy: `discord.fm.tul.cz` -> `127.0.0.1:8000`, předávat hlavičky `Host` a `X-Forwarded-Proto`.
4. Kontrola: `docker compose logs -f` musí obsahovat "Dev mock endpointy jsou zakázány"; `https://discord.fm.tul.cz/` odpovídá.

Data (`users.json`, `feeds.json`, `discord_config.json`) jsou ve volume `bot-data` (`/data`). Zálohovat: `docker run --rm -v <projekt>_bot-data:/d -v $PWD:/b alpine tar czf /b/bot-data.tgz -C /d .`

Aktualizace: `git pull && bash deploy.sh`.

Redirect URI pro Microsoft: `https://discord.fm.tul.cz/msal`. Discord OAuth redirect: `https://discord.fm.tul.cz/discord`.
