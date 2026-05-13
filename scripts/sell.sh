#!/usr/bin/env bash
# scripts/sell.sh — Tutup semua posisi terbuka segera via API engine
# Gunakan: bash scripts/sell.sh [alasan]
# Contoh : bash scripts/sell.sh "emergency stop sebelum maintenance"

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

PORT="${PORT:-8080}"
BASE_URL="http://localhost:${PORT}"
REASON="${1:-MANUAL CLOSE-ALL via sell.sh}"

echo -e ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║    Meme Hunter — Close All Positions     ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

# ── 1. Cek apakah engine berjalan ─────────────────────────────────────────────
if ! curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
    echo -e "${RED}[ERR]${NC}  Engine tidak berjalan di port ${PORT}."
    echo -e "       Jalankan scripts/start.sh terlebih dahulu, atau cek: systemctl status meme-hunter"
    exit 1
fi

echo -e "${CYAN}[INFO]${NC} Engine terdeteksi di ${BASE_URL}"

# ── 2. Tampilkan posisi terbuka sebelum menutup ────────────────────────────────
echo -e "${CYAN}[INFO]${NC} Posisi terbuka saat ini:"
POSITIONS=$(curl -sf "${BASE_URL}/api/positions" 2>/dev/null || echo "[]")
OPEN_COUNT=$(echo "$POSITIONS" | grep -o '"OPEN"' | wc -l | tr -d ' ')

if [[ "$OPEN_COUNT" -eq 0 ]]; then
    echo -e "${YELLOW}[WARN]${NC} Tidak ada posisi terbuka. Tidak ada yang perlu ditutup."
    exit 0
fi

echo -e "       Ditemukan ${YELLOW}${OPEN_COUNT} posisi terbuka${NC}"
echo ""

# ── 3. Konfirmasi ─────────────────────────────────────────────────────────────
read -rp "$(echo -e "${YELLOW}Tutup semua ${OPEN_COUNT} posisi? [y/N]: ${NC}")" CONFIRM
if [[ "${CONFIRM,,}" != "y" ]]; then
    echo -e "${YELLOW}[BATAL]${NC} Tidak ada posisi yang ditutup."
    exit 0
fi

echo ""
echo -e "${CYAN}[INFO]${NC} Menutup semua posisi: \"${REASON}\"..."

# ── 4. Kirim perintah close-all ke engine ─────────────────────────────────────
# URL-encode reason dengan python3 (fallback: ganti spasi saja)
ENCODED_REASON=$(python3 -c "import urllib.parse, sys; print(urllib.parse.quote(sys.argv[1]))" "$REASON" 2>/dev/null \
    || echo "${REASON// /%20}")

RESULT=$(curl -sf -X POST \
    "${BASE_URL}/api/close-all?reason=${ENCODED_REASON}" \
    -H "Content-Type: application/json" 2>/dev/null || true)

if [[ -z "$RESULT" ]]; then
    echo -e "${RED}[ERR]${NC}  Gagal menghubungi engine. Cek log: journalctl -u meme-hunter -n 20"
    exit 1
fi

CLOSED=$(echo "$RESULT" | grep -o '"closed":[0-9]*' | grep -o '[0-9]*' || echo "?")

echo ""
echo -e "${GREEN}[OK]${NC}   ${CLOSED} posisi berhasil ditutup."
echo -e "${CYAN}[INFO]${NC} Alasan: ${REASON}"
echo ""

# ── 5. Verifikasi ─────────────────────────────────────────────────────────────
sleep 2
REMAINING=$(curl -sf "${BASE_URL}/api/positions" 2>/dev/null | grep -o '"OPEN"' | wc -l | tr -d ' ')
if [[ "$REMAINING" -eq 0 ]]; then
    echo -e "${GREEN}[OK]${NC}   Semua posisi telah ditutup. Engine aman untuk dihentikan."
else
    echo -e "${YELLOW}[WARN]${NC} Masih ada ${REMAINING} posisi yang belum tertutup (mungkin sedang diproses)."
    echo -e "       Cek trade log: ${BASE_URL}"
fi
echo ""
