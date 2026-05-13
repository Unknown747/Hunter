#!/usr/bin/env bash
# start.sh — Mulai engine Meme Coin Hunter
# Gunakan: bash start.sh
# Mode   : jika diinstall via install.sh (systemd), gunakan systemctl
#          jika dijalankan langsung, jalankan binary atau go run

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

INSTALL_DIR="/opt/meme-hunter"
BINARY="${INSTALL_DIR}/meme-hunter"
SERVICE="meme-hunter"

echo -e ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║    Base Meme Coin Hunter — Start         ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

# ── Muat .env jika ada ────────────────────────────────────────────────────────
ENV_FILE="${INSTALL_DIR}/.env"
if [[ -f ".env" ]]; then
    ENV_FILE=".env"
fi
if [[ -f "$ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
    echo -e "${CYAN}[INFO]${NC} Konfigurasi dimuat dari ${ENV_FILE}"
fi

PORT="${PORT:-8080}"
RISK="${RISK_LEVEL:-normal}"

# ── Mode 1: systemd (diinstall via install.sh) ────────────────────────────────
if command -v systemctl &>/dev/null && systemctl list-unit-files "${SERVICE}.service" &>/dev/null 2>&1; then
    echo -e "${CYAN}[INFO]${NC} Menjalankan via systemd..."
    systemctl start "${SERVICE}" 2>/dev/null || sudo systemctl start "${SERVICE}"
    sleep 2
    if systemctl is-active --quiet "${SERVICE}"; then
        echo -e "${GREEN}[OK]${NC}   Service '${SERVICE}' berjalan!"
        echo -e "       Dashboard : ${GREEN}http://localhost:${PORT}${NC}"
        echo -e "       Risk level: ${YELLOW}${RISK^^}${NC}"
        echo -e "       Log       : journalctl -u ${SERVICE} -f"
    else
        echo -e "${RED}[ERR]${NC}  Gagal start. Cek: journalctl -u ${SERVICE} -n 20"
        exit 1
    fi
    exit 0
fi

# ── Mode 2: Binary langsung ───────────────────────────────────────────────────
if [[ -f "$BINARY" ]]; then
    echo -e "${CYAN}[INFO]${NC} Menjalankan binary: ${BINARY}"
    echo -e "${CYAN}[INFO]${NC} Dashboard: http://localhost:${PORT}"
    echo -e "${YELLOW}[INFO]${NC} Tekan Ctrl+C untuk berhenti (atau gunakan stop.sh)"
    echo ""
    exec "$BINARY"
    exit 0
fi

# ── Mode 3: go run (development) ─────────────────────────────────────────────
if command -v go &>/dev/null; then
    echo -e "${CYAN}[INFO]${NC} Menjalankan via go run ..."
    echo -e "${CYAN}[INFO]${NC} Dashboard: http://localhost:${PORT}"
    echo -e "${YELLOW}[INFO]${NC} Tekan Ctrl+C untuk berhenti (atau gunakan stop.sh)"
    echo ""
    exec go run .
    exit 0
fi

echo -e "${RED}[ERR]${NC}  Tidak ditemukan: systemd service, binary, atau Go."
echo -e "       Jalankan install.sh terlebih dahulu."
exit 1
