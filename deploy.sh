#!/usr/bin/env bash
# FM TUL Discord Connector - nasazení jedním příkazem: bash deploy.sh
# Přepínače: --no-build (jen restart), --logs (po nasazení sledovat logy), --help
# Log: deploy.log (každý běh se připojuje, každý řádek má časové razítko)
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
LOG_FILE="deploy.log"
BUILD=1; FOLLOW=0
for a in "$@"; do
  case "$a" in
    --no-build) BUILD=0 ;;
    --logs) FOLLOW=1 ;;
    -h|--help) sed -n '2,4p' "$0"; exit 0 ;;
    *) echo "Neznámý přepínač: $a" >&2; exit 2 ;;
  esac
done

exec > >(while IFS= read -r l; do printf '%s %s\n' "$(date '+%F %T')" "$l"; done | tee -a "$LOG_FILE") 2>&1

info() { echo "[INFO]  $*"; }
ok()   { echo "[ OK ]  $*"; }
warn() { echo "[WARN]  $*"; }
die()  { echo "[CHYBA] $*" >&2; exit 1; }

COMPOSE=()
on_error() {
  local code=$? line=$1 cmd=$2
  echo "[CHYBA] Selhalo (kód $code) na řádku $line: $cmd" >&2
  if [ ${#COMPOSE[@]} -gt 0 ]; then
    echo "[CHYBA] Stav kontejneru:" >&2; "${COMPOSE[@]}" ps 2>&1 || true
    echo "[CHYBA] Posledních 40 řádků logu aplikace:" >&2; "${COMPOSE[@]}" logs --tail 40 2>&1 || true
  fi
  echo "[CHYBA] Plný log nasazení: $PWD/$LOG_FILE" >&2
}
trap 'on_error $LINENO "$BASH_COMMAND"' ERR

info "Nasazení FM TUL Discord Connector ($(git rev-parse --short HEAD 2>/dev/null || echo 'bez gitu'))"

# 1. Container runtime
if docker compose version >/dev/null 2>&1; then COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then COMPOSE=(docker-compose)
elif podman compose version >/dev/null 2>&1; then COMPOSE=(podman compose)
else die "Nenalezen Docker Compose (docker compose / docker-compose / podman compose)."; fi
ok "Compose: ${COMPOSE[*]}"
"${COMPOSE[@]}" version >/dev/null 2>&1 || die "Compose je nainstalované, ale nefunguje (běží démon? má uživatel práva?)."

# 2. .env
get() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- | sed -E 's/^"(.*)"$/\1/'; }
if [ ! -f .env ]; then
  [ -f .env.example ] || die ".env.example nenalezen."
  cp .env.example .env && chmod 600 .env
  warn ".env neexistoval - vytvořen z .env.example. Vyplňte ho a spusťte skript znovu."
fi
if [ -z "$(get HMAC_SECRET)" ]; then
  command -v openssl >/dev/null || die "Chybí openssl pro generování HMAC_SECRET - nastavte ho ručně v .env."
  secret=$(openssl rand -hex 32)
  if grep -qE '^HMAC_SECRET=' .env; then sed -i -E "s|^HMAC_SECRET=.*|HMAC_SECRET=\"$secret\"|" .env
  else printf '\nHMAC_SECRET="%s"\n' "$secret" >> .env; fi
  ok "Vygenerován nový HMAC_SECRET (uložen do .env)"
fi

missing=()
for v in MSAL_CLIENT_ID MSAL_CLIENT_SECRET MSAL_TENANT_ID DISCORD_CLIENT_ID DISCORD_CLIENT_SECRET DISCORD_TOKEN DISCORD_GUILD_ID; do
  val=$(get "$v")
  if [ -z "$val" ] || echo "$val" | grep -qiE '^(SEM_|SET_|YOUR_|CHANGE|TODO|xxx)'; then missing+=("$v"); fi
done
[ ${#missing[@]} -eq 0 ] || die "V .env chybí nebo jsou nevyplněné: ${missing[*]}"
[ "$(get ENABLE_DEV_MOCK | tr A-Z a-z)" != "true" ] || warn "ENABLE_DEV_MOCK=true v .env (compose ho v produkci přepíše na false)."
ok ".env zkontrolován"

# 3. Build + start
if [ "$BUILD" -eq 1 ]; then info "Build image..."; "${COMPOSE[@]}" build; fi
info "Spouštím kontejner..."
"${COMPOSE[@]}" up -d --remove-orphans

# 4. Health check (jakákoli HTTP odpověď = server žije)
port=$(get PORT); port=${port:-8000}
info "Čekám na odpověď http://127.0.0.1:$port/ ..."
up=0
for _ in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$port/" || true)
  if [ "$code" != "000" ] && [ -n "$code" ]; then up=1; break; fi
  state=$("${COMPOSE[@]}" ps --status exited -q 2>/dev/null || true)
  [ -z "$state" ] || die "Kontejner skončil (viz log výše)."
  sleep 2
done
[ "$up" -eq 1 ] || die "Aplikace neodpovídá do 60 s."
ok "Aplikace běží (HTTP $code)"

if "${COMPOSE[@]}" logs --tail 200 2>&1 | grep -q 'Dev mock endpointy jsou zakázány'; then ok "Dev mock je vypnutý"
else warn "V logu jsem nenašel potvrzení vypnutého dev mocku - zkontrolujte ručně."; fi

info "Hotovo. Další kroky: reverse proxy discord.fm.tul.cz -> 127.0.0.1:$port (TLS)."
[ "$FOLLOW" -eq 1 ] && exec "${COMPOSE[@]}" logs -f
exit 0
