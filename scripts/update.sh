#!/usr/bin/env bash
# scripts/update.sh — Auto-update Base Meme Coin Hunter dari GitHub
# Strategi: build binary baru dulu → swap atomik → graceful restart via systemd
# State posisi tersimpan ke disk sebelum shutdown (persist.go), dimuat ulang otomatis.
#
# Penggunaan:
#   sudo bash scripts/update.sh                   # update normal
#   sudo bash scripts/update.sh --rollback        # rollback ke versi sebelumnya
#   sudo bash scripts/update.sh --no-restart      # build saja, tanpa restart service
#   sudo bash scripts/update.sh --dry-run         # cek update tersedia, tanpa eksekusi
set -euo pipefail

# ─── Warna ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC}  $1"; }
success() { echo -e "${GREEN}[OK]${NC}    $1"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $1"; }
error()   { echo -e "${RED}[ERR]${NC}   $1"; exit 1; }
step()    { echo -e "\n${BOLD}▶ $1${NC}"; }

# ─── Konfigurasi ──────────────────────────────────────────────────────────────
INSTALL_DIR="/opt/meme-hunter"
SERVICE_NAME="meme-hunter"
BINARY_NAME="meme-hunter"
BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME}"
BACKUP_PATH="${INSTALL_DIR}/${BINARY_NAME}.prev"
NEW_BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME}.new"
LOG_FILE="${INSTALL_DIR}/update.log"

# ─── Parse argumen ────────────────────────────────────────────────────────────
DO_ROLLBACK=false
NO_RESTART=false
DRY_RUN=false

for arg in "$@"; do
    case "$arg" in
        --rollback)   DO_ROLLBACK=true ;;
        --no-restart) NO_RESTART=true ;;
        --dry-run)    DRY_RUN=true ;;
        --help|-h)
            echo "Penggunaan: sudo bash scripts/update.sh [--rollback|--no-restart|--dry-run]"
            exit 0 ;;
    esac
done

# ─── Banner ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   Base Meme Coin Hunter — Auto Updater      ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""

# ─── Cek root ─────────────────────────────────────────────────────────────────
[[ $EUID -ne 0 ]] && error "Jalankan sebagai root: sudo bash scripts/update.sh"

# ─── Cek direktori install ────────────────────────────────────────────────────
[[ ! -d "$INSTALL_DIR" ]] && error "Direktori install tidak ditemukan: $INSTALL_DIR\nJalankan scripts/install.sh terlebih dahulu."

export PATH=$PATH:/usr/local/go/bin
go version &>/dev/null || error "Go tidak ditemukan. Pastikan Go sudah terinstal."

# ═════════════════════════════════════════════════════════════════════════════
# MODE: ROLLBACK
# ═════════════════════════════════════════════════════════════════════════════
if $DO_ROLLBACK; then
    step "Rollback ke versi sebelumnya"

    [[ ! -f "$BACKUP_PATH" ]] && error "Tidak ada backup binary di ${BACKUP_PATH}. Rollback tidak bisa dilakukan."

    BACKUP_SIZE=$(du -sh "$BACKUP_PATH" 2>/dev/null | cut -f1)
    BACKUP_TIME=$(stat -c '%y' "$BACKUP_PATH" 2>/dev/null | cut -d. -f1)
    info "Binary backup: ${BACKUP_SIZE} — dibuat: ${BACKUP_TIME}"

    # Swap: ganti current dengan backup
    cp -f "$BINARY_PATH" "${BINARY_PATH}.rollback_failed" 2>/dev/null || true
    mv -f "$BACKUP_PATH" "$BINARY_PATH"
    chmod +x "$BINARY_PATH"
    rm -f "${BINARY_PATH}.rollback_failed"
    success "Binary berhasil dikembalikan ke versi sebelumnya"

    step "Restart service"
    systemctl restart "$SERVICE_NAME"
    sleep 3

    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "Service '${SERVICE_NAME}' berjalan dengan versi sebelumnya"
        echo -e "\n  Status  : $(systemctl is-active "$SERVICE_NAME")"
        echo -e "  Log     : journalctl -u ${SERVICE_NAME} -f"
    else
        error "Service gagal start setelah rollback. Cek: journalctl -u ${SERVICE_NAME} -n 30"
    fi
    exit 0
fi

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 1: Cek apakah ini repo Git
# ═════════════════════════════════════════════════════════════════════════════
step "Cek sumber kode"
cd "$INSTALL_DIR"

if [[ ! -d ".git" ]]; then
    warn "Direktori ${INSTALL_DIR} bukan repo Git."
    warn "Update otomatis dari GitHub membutuhkan repo Git."
    echo ""
    echo -e "  Opsi 1: Clone ulang ke direktori temp, lalu jalankan scripts/install.sh"
    echo -e "  Opsi 2: Salin file .go terbaru manual, lalu: sudo bash scripts/update.sh --no-restart && sudo systemctl restart ${SERVICE_NAME}"
    echo ""
    error "Batalkan: bukan repo Git."
fi

REMOTE_URL=$(git remote get-url origin 2>/dev/null || echo "")
if [[ -z "$REMOTE_URL" ]]; then
    error "Tidak ada remote 'origin'. Set dengan: git remote add origin https://github.com/USER/REPO.git"
fi

info "Remote: ${REMOTE_URL}"

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 2: Cek update tersedia
# ═════════════════════════════════════════════════════════════════════════════
step "Cek update dari GitHub"

LOCAL_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
LOCAL_SHORT="${LOCAL_COMMIT:0:7}"

# Fetch tanpa merge dulu (aman, tidak ubah kode)
info "Fetching dari remote..."
git fetch origin --quiet 2>/dev/null || {
    warn "Tidak bisa reach GitHub. Cek koneksi internet."
    exit 1
}

BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
REMOTE_COMMIT=$(git rev-parse "origin/${BRANCH}" 2>/dev/null || echo "")

if [[ -z "$REMOTE_COMMIT" ]]; then
    REMOTE_COMMIT=$(git rev-parse "origin/main" 2>/dev/null || echo "")
    BRANCH="main"
fi

REMOTE_SHORT="${REMOTE_COMMIT:0:7}"

if [[ "$LOCAL_COMMIT" == "$REMOTE_COMMIT" ]]; then
    success "Sudah versi terbaru (commit: ${LOCAL_SHORT})"
    if $DRY_RUN; then
        echo -e "\n  Tidak ada update tersedia."
        exit 0
    fi
    echo ""
    read -rp "$(echo -e "${YELLOW}Tidak ada perubahan baru. Rebuild + restart tetap dilanjutkan? [y/N]: ${NC}")" CONFIRM
    [[ "${CONFIRM,,}" != "y" ]] && { info "Dibatalkan."; exit 0; }
else
    COMMIT_COUNT=$(git rev-list --count "${LOCAL_COMMIT}..${REMOTE_COMMIT}" 2>/dev/null || echo "?")
    echo ""
    echo -e "  Versi saat ini : ${YELLOW}${LOCAL_SHORT}${NC}"
    echo -e "  Versi terbaru  : ${GREEN}${REMOTE_SHORT}${NC} (+${COMMIT_COUNT} commit baru)"
    echo ""
    echo -e "${BOLD}  Changelog:${NC}"
    git log --oneline "${LOCAL_COMMIT}..${REMOTE_COMMIT}" 2>/dev/null | head -15 | while read -r line; do
        echo -e "    ${CYAN}•${NC} $line"
    done
    echo ""

    if $DRY_RUN; then
        info "Dry-run selesai. Jalankan tanpa --dry-run untuk mengupdate."
        exit 0
    fi
fi

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 3: Cek local changes (proteksi .env dan state)
# ═════════════════════════════════════════════════════════════════════════════
step "Cek file lokal"

CHANGED=$(git status --porcelain 2>/dev/null | grep -v '\.env' | grep -v 'state\.json' | grep -v 'update\.log' || true)
if [[ -n "$CHANGED" ]]; then
    warn "Ada perubahan lokal yang belum di-commit:"
    echo "$CHANGED" | while read -r line; do
        echo -e "    ${YELLOW}$line${NC}"
    done
    echo ""
    echo -e "  File ${CYAN}.env${NC} dan ${CYAN}state.json${NC} TIDAK akan disentuh."
    read -rp "$(echo -e "${YELLOW}Lanjutkan? Perubahan lokal akan di-stash. [y/N]: ${NC}")" STASH_CONFIRM
    [[ "${STASH_CONFIRM,,}" != "y" ]] && { info "Dibatalkan."; exit 0; }

    info "Menyimpan perubahan lokal sementara (git stash)..."
    # Kumpulkan file yang perlu di-stash (kecuali .env dan state.json)
    STASH_FILES=$(git ls-files --modified --others --exclude-standard \
        | grep -v '\.env' | grep -v 'state\.json' || true)
    if [[ -n "$STASH_FILES" ]]; then
        git stash push --include-untracked \
            --message "auto-stash sebelum update $(date +%Y%m%dT%H%M%S)" \
            -- $STASH_FILES 2>/dev/null || true
    fi
fi

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 4: Pull kode terbaru
# ═════════════════════════════════════════════════════════════════════════════
step "Pull kode terbaru dari GitHub"

git pull origin "$BRANCH" --ff-only 2>&1 | while read -r line; do
    info "$line"
done || {
    warn "Fast-forward tidak bisa dilakukan. Coba dengan --rebase..."
    git pull origin "$BRANCH" --rebase 2>&1 | tail -5
}

NEW_COMMIT=$(git rev-parse HEAD)
NEW_SHORT="${NEW_COMMIT:0:7}"
success "Kode diperbarui ke commit ${NEW_SHORT}"

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 5: Download Go modules (jika ada perubahan go.mod)
# ═════════════════════════════════════════════════════════════════════════════
if git diff "${LOCAL_COMMIT}..HEAD" -- go.mod go.sum 2>/dev/null | grep -q '^[+-]'; then
    step "Download Go modules (go.mod berubah)"
    go mod download 2>&1 | tail -5
    success "Modules diperbarui"
fi

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 6: Build binary baru (tanpa menghentikan service yang berjalan)
# ═════════════════════════════════════════════════════════════════════════════
step "Build binary baru"

BUILD_START=$(date +%s)
info "Compiling... (service tidak terganggu selama ini)"

go build -ldflags="-s -w -X main.BuildCommit=${NEW_SHORT} -X main.BuildTime=$(date -u +%Y%m%dT%H%M%SZ)" \
    -o "$NEW_BINARY_PATH" . 2>&1 || {
    rm -f "$NEW_BINARY_PATH"
    error "Build gagal! Service lama tetap berjalan — tidak ada downtime."
}

BUILD_END=$(date +%s)
BUILD_SEC=$((BUILD_END - BUILD_START))
BUILD_SIZE=$(du -sh "$NEW_BINARY_PATH" | cut -f1)
chmod +x "$NEW_BINARY_PATH"
success "Binary baru berhasil dibangun (${BUILD_SIZE}, ${BUILD_SEC}s)"

# ─── Validasi binary tidak kosong ─────────────────────────────────────────────
BINARY_BYTES=$(stat -c%s "$NEW_BINARY_PATH" 2>/dev/null || echo 0)
[[ "$BINARY_BYTES" -lt 1000000 ]] && {
    rm -f "$NEW_BINARY_PATH"
    error "Binary terlalu kecil (${BINARY_BYTES} bytes) — kemungkinan build bermasalah."
}

if $NO_RESTART; then
    # Swap binary tanpa restart
    [[ -f "$BINARY_PATH" ]] && cp -f "$BINARY_PATH" "$BACKUP_PATH"
    mv -f "$NEW_BINARY_PATH" "$BINARY_PATH"
    chown "${SERVICE_NAME}:${SERVICE_NAME}" "$BINARY_PATH" 2>/dev/null || true
    success "Binary diupdate. Service belum di-restart (--no-restart aktif)."
    echo -e "\n  Untuk menerapkan update: ${CYAN}systemctl restart ${SERVICE_NAME}${NC}"
    exit 0
fi

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 7: Swap binary + Graceful restart
# ═════════════════════════════════════════════════════════════════════════════
step "Swap binary + Graceful restart"

# Backup binary lama untuk rollback
if [[ -f "$BINARY_PATH" ]]; then
    cp -f "$BINARY_PATH" "$BACKUP_PATH"
    info "Binary lama dibackup ke: ${BACKUP_PATH}"
fi

# Swap atomik (mv pada filesystem yang sama = instan)
mv -f "$NEW_BINARY_PATH" "$BINARY_PATH"
chown "${SERVICE_NAME}:${SERVICE_NAME}" "$BINARY_PATH" 2>/dev/null || true
success "Binary baru sudah aktif (swap atomik)"

# Cek apakah service berjalan — jika iya, kirim SIGTERM dulu
if systemctl is-active --quiet "$SERVICE_NAME"; then
    info "Mengirim graceful restart ke service..."
    systemctl restart "$SERVICE_NAME"
else
    info "Service tidak berjalan — starting fresh..."
    systemctl start "$SERVICE_NAME"
fi

# ─── Tunggu service naik ──────────────────────────────────────────────────────
info "Menunggu service naik..."
WAIT=0
MAX_WAIT=30
while [[ $WAIT -lt $MAX_WAIT ]]; do
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        break
    fi
    sleep 1
    WAIT=$((WAIT + 1))
done

# ═════════════════════════════════════════════════════════════════════════════
# LANGKAH 8: Verifikasi health check
# ═════════════════════════════════════════════════════════════════════════════
step "Verifikasi health check"

if ! systemctl is-active --quiet "$SERVICE_NAME"; then
    echo ""
    error "⚠️  Service gagal naik setelah update!\n   Jalankan rollback: sudo bash scripts/update.sh --rollback"
fi

# Ambil port dari .env
PORT_FROM_ENV=$(grep -E '^PORT=' "${INSTALL_DIR}/.env" 2>/dev/null | cut -d= -f2 | tr -d ' ' || echo "8080")
PORT_FROM_ENV="${PORT_FROM_ENV:-8080}"

# Coba hit /api/stats untuk konfirmasi API berjalan
HEALTH_OK=false
for attempt in 1 2 3 4 5; do
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
        "http://127.0.0.1:${PORT_FROM_ENV}/api/stats" \
        --max-time 3 2>/dev/null || echo "000")
    if [[ "$HTTP_CODE" == "200" ]]; then
        HEALTH_OK=true
        break
    fi
    sleep 2
done

if ! $HEALTH_OK; then
    warn "API health check timeout. Service berjalan tapi mungkin masih starting."
    warn "Cek manual: curl http://127.0.0.1:${PORT_FROM_ENV}/api/stats"
fi

# ─── Catat log update ─────────────────────────────────────────────────────────
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Update: ${LOCAL_SHORT} → ${NEW_SHORT} (${COMMIT_COUNT:-?} commit) — OK" \
    >> "$LOG_FILE" 2>/dev/null || true

# ═════════════════════════════════════════════════════════════════════════════
# Selesai
# ═════════════════════════════════════════════════════════════════════════════
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║           Update Berhasil!                  ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Versi lama : ${YELLOW}${LOCAL_SHORT}${NC}"
echo -e "  Versi baru : ${GREEN}${NEW_SHORT}${NC}"
echo -e "  Build time : ${BUILD_SEC}s"
echo -e "  Binary     : $(du -sh "$BINARY_PATH" | cut -f1)"
echo -e "  Status     : $(systemctl is-active "$SERVICE_NAME")"
echo ""
echo -e "${BOLD}Perintah berguna:${NC}"
echo -e "  journalctl -u ${SERVICE_NAME} -f              — lihat log realtime"
echo -e "  sudo bash scripts/update.sh --rollback        — rollback ke versi sebelumnya"
echo -e "  cat ${LOG_FILE}                    — riwayat update"
echo ""
if $HEALTH_OK; then
    success "API merespons normal di port ${PORT_FROM_ENV}"
else
    warn "API belum merespons — mungkin masih warm-up. Cek sebentar lagi."
fi
echo ""
