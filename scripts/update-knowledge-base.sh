#!/usr/bin/env bash
# ==============================================================================
# Automatická aktualizace Knowledge Base pro projekt FM TUL Discord Connector
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${ROOT_DIR}"

echo "═══════════════════════════════════════════════════════════════"
echo "  Aktualizace Knowledge Base (FM TUL Discord Connector)"
echo "═══════════════════════════════════════════════════════════════"

# 1. Spustíme Go skript pro generování SUMMARY.md a AI_CONTEXT.md
go run ./scripts/update_kb.go

# 2. Ověříme integritu testů
echo ""
echo "Kontrola stavu testovací sady..."
go test -race ./...

echo ""
echo "Knowledge Base je plně synchronizována s aktuálním zdrojovým kódem."
echo "   - Dokumentace: ${ROOT_DIR}/docs/knowledge-base/"
echo "   - AI Pokyny:   ${ROOT_DIR}/AGENTS.md"
