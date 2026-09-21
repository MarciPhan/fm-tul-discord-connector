#!/usr/bin/env bash
# ============================================================================
# FM TUL Discord Connector – Start Script (Linux / macOS)
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY_NAME="fm-tul-bot"

cd "$PROJECT_DIR"

# --- Barvy pro výstup ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   FM TUL - Discord Connector                ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════╝${NC}"
echo ""

# --- 1. Kontrola Go ---
if ! command -v go &> /dev/null; then
    echo -e "${RED}[ERROR] Go není nainstalováno!${NC}"
    echo "   Nainstalujte Go z: https://go.dev/dl/"
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}')
echo -e "${GREEN}[OK] Go nalezeno: ${GO_VERSION}${NC}"

# --- 2. Kontrola .env ---
if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        echo -e "${YELLOW}[WARN] .env soubor nenalezen. Kopíruji z .env.example...${NC}"
        cp .env.example .env
        echo -e "${YELLOW}   Prosím upravte .env a vyplňte konfiguraci!${NC}"
        echo ""
    else
        echo -e "${RED}[ERROR] .env ani .env.example nenalezeny!${NC}"
        exit 1
    fi
fi
echo -e "${GREEN}[OK] Konfigurace .env nalezena${NC}"

# --- 3. Stažení závislostí ---
echo -e "${BLUE}Stahuji závislosti...${NC}"
go mod tidy
echo -e "${GREEN}[OK] Závislosti připraveny${NC}"

# --- 4. Kompilace ---
echo -e "${BLUE}Kompiluji ${BINARY_NAME}...${NC}"
go build -buildvcs=false -o "${BINARY_NAME}" ./cmd/bot/
echo -e "${GREEN}[OK] Kompilace úspěšná: ./${BINARY_NAME}${NC}"

# --- 5. Spuštění ---
echo ""
echo -e "${BLUE}Spouštím FM TUL Discord Connector...${NC}"
echo "──────────────────────────────────────────────"
exec "./${BINARY_NAME}"
