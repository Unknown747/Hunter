#!/usr/bin/env bash
# stop.sh — Hentikan engine dengan aman (jual semua posisi dulu)
# Gunakan: bash stop.sh [--force]
#   --force : langsung stop tanpa menutup posisi (darurat)

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

SERVICE="meme-hunter"
PORT="${PORT:-8080}"
FORCE=false

for arg in "$@"; do
    [[ "$arg" == "--force" ]] && FORCE=true
done

echo -e ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║    Base Meme Coin Hunter — Stop          ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

# ── Muat PORT dari .env jika ada ──────────────────────────────────────────────
for f in "/opt/meme-hunter/.env" ".env"; do
    if [[ -f "$f" ]]; then
        P=$(grep -E '^PORT=' "$f" 2>/dev/null | cut -d= -f2 | tr -d ' ' || true)
        [[ -n "$P" ]] && PORT="$P"
        break
    fi
done

BASE_URL="http://localhost:${PORT}"

# ── 1. Jual semua posisi dulu (kecuali --force) ───────────────────────────────
if [[ "$FORCE" == false ]]; then
    ENGINE_UP=false
    if curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
        ENGINE_UP=true
    fi

    if [[ "$ENGINE_UP" == true ]]; then
        OPEN=$(curl -sf "${BASE_URL}/api/positions" 2>/dev/null | grep -o '"OPEN"' | wc -l | tr -d ' ')
        if [[ "$OPEN" -gt 0 ]]; then
            echo -e "${YELLOW}[WARN]${NC} Ada ${OPEN} posisi terbuka. Menutup terlebih dahulu..."
            bash "$(dirname "$0")/sell.sh" "ENGINE STOP — auto close"
        else
            echo -e "${GREEN}[OK]${NC}   Tidak ada posisi terbuka."
        fi
    else
        echo -e "${YELLOW}[WARN]${NC} Engine tidak merespon — langsung stop service."
    fi
else
    echo -e "${YELLOW}[WARN]${NC} Mode --force: skip close positions!"
fi

# ── 2. Hentikan engine ────────────────────────────────────────────────────────
echo ""
echo -e "${CYAN}[INFO]${NC} Menghentikan engine..."

STOPPED=false

# Coba systemd
if command -v systemctl &>/dev/null && systemctl is-active --quiet "${SERVICE}" 2>/dev/null; then
    systemctl stop "${SERVICE}" 2>/dev/null || sudo systemctl stop "${SERVICE}"
    STOPPED=true
fi

# Coba kill PID jika tidak ada systemd
if [[ "$STOPPED" == false ]]; then
    PID=$(pgrep -f "meme-hunter" 2>/dev/null || true)
    if [[ -n "$PID" ]]; then
        kill -SIGTERM "$PID" 2>/dev/null || true
        sleep 2
        kill -SIGKILL "$PID" 2>/dev/null || true
        STOPPED=true
    fi
fi

if [[ "$STOPPED" == true ]]; then
    echo -e "${GREEN}[OK]${NC}   Engine dihentikan."
    echo -e "${CYAN}[INFO]${NC} State tersimpan di state.json — posisi akan dilanjutkan saat start berikutnya."
else
    echo -e "${YELLOW}[WARN]${NC} Tidak ada proses engine yang ditemukan."
fi
echo ""
