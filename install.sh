#!/usr/bin/env bash
set -e

# ─── Warna ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC}   $1"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
error()   { echo -e "${RED}[ERR]${NC}  $1"; exit 1; }

# ─── Konfigurasi Default ──────────────────────────────────────────────────────
GO_VERSION="1.21.13"
INSTALL_DIR="/opt/meme-hunter"
SERVICE_NAME="meme-hunter"
SERVICE_USER="meme-hunter"
PORT="${PORT:-8080}"
BINARY_NAME="meme-hunter"

echo -e ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║    Base Meme Coin Hunter — VPS Install   ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

# ─── 1. Cek root ──────────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    error "Jalankan sebagai root: sudo bash install.sh"
fi

# ─── 2. Cek OS ────────────────────────────────────────────────────────────────
if ! command -v apt-get &>/dev/null && ! command -v yum &>/dev/null; then
    error "OS tidak didukung. Hanya Debian/Ubuntu/CentOS."
fi

# ─── 3. Install dependencies ──────────────────────────────────────────────────
info "Menginstal dependencies sistem..."
if command -v apt-get &>/dev/null; then
    apt-get update -qq
    apt-get install -y -qq curl wget git build-essential ca-certificates
else
    yum install -y curl wget git gcc ca-certificates
fi
success "Dependencies sistem terinstal"

# ─── 4. Install Go ────────────────────────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  GO_ARCH="amd64" ;;
    aarch64) GO_ARCH="arm64" ;;
    armv7l)  GO_ARCH="armv6l" ;;
    *)       error "Arsitektur CPU tidak didukung: $ARCH" ;;
esac

CURRENT_GO=$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//' || echo "")
if [[ "$CURRENT_GO" == "$GO_VERSION" ]]; then
    success "Go $GO_VERSION sudah terinstal"
else
    info "Menginstal Go $GO_VERSION ($GO_ARCH)..."
    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    wget -q "https://go.dev/dl/${GO_TAR}" -O /tmp/${GO_TAR}
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/${GO_TAR}
    rm /tmp/${GO_TAR}

    export PATH=$PATH:/usr/local/go/bin
    if ! grep -q '/usr/local/go/bin' /etc/profile; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    fi
    success "Go $GO_VERSION berhasil diinstal"
fi

export PATH=$PATH:/usr/local/go/bin
go version &>/dev/null || error "Go tidak bisa dijalankan setelah install"

# ─── 5. Buat user sistem ──────────────────────────────────────────────────────
if ! id "$SERVICE_USER" &>/dev/null; then
    info "Membuat user sistem '$SERVICE_USER'..."
    useradd -r -s /bin/false -d "$INSTALL_DIR" "$SERVICE_USER"
    success "User '$SERVICE_USER' berhasil dibuat"
fi

# ─── 6. Salin file proyek ─────────────────────────────────────────────────────
info "Menyalin file proyek ke $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

# Dapatkan direktori skrip ini
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Salin semua file Go + static
cp -r "$SCRIPT_DIR"/*.go "$INSTALL_DIR/" 2>/dev/null || true
cp -r "$SCRIPT_DIR/go.mod" "$INSTALL_DIR/"
cp -r "$SCRIPT_DIR/go.sum" "$INSTALL_DIR/"
cp -r "$SCRIPT_DIR/static" "$INSTALL_DIR/"

# Salin .env jika ada
if [[ -f "$SCRIPT_DIR/.env" ]]; then
    cp "$SCRIPT_DIR/.env" "$INSTALL_DIR/.env"
    warn ".env disalin — pastikan PRIVATE_KEY aman (chmod 600 $INSTALL_DIR/.env)"
fi

success "File proyek berhasil disalin ke $INSTALL_DIR"

# ─── 7. Build binary ──────────────────────────────────────────────────────────
info "Membangun binary ($BINARY_NAME)..."
cd "$INSTALL_DIR"
go mod download -x 2>&1 | tail -5
go build -ldflags="-s -w" -o "$BINARY_NAME" . || error "Build gagal"
chmod +x "$BINARY_NAME"
chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
success "Binary berhasil dibangun: $INSTALL_DIR/$BINARY_NAME"

# ─── 8. File .env ─────────────────────────────────────────────────────────────
ENV_FILE="$INSTALL_DIR/.env"
if [[ ! -f "$ENV_FILE" ]]; then
    info "Membuat file .env template..."
    cat > "$ENV_FILE" <<EOF
# ─── Port ─────────────────────────────────────────────────────────────────────
PORT=${PORT}

# ─── Live Trading ─────────────────────────────────────────────────────────────
# Catatan: cukup set PRIVATE_KEY untuk mengaktifkan live trading
# PRIVATE_KEY=hex_private_key_tanpa_0x
# BASE_RPC_URL=https://mainnet.base.org
# TRADE_SIZE_ETH=0.001
# SLIPPAGE_PCT=5.0
EOF
    chmod 600 "$ENV_FILE"
    chown "$SERVICE_USER:$SERVICE_USER" "$ENV_FILE"
    success "File .env dibuat di $ENV_FILE"
fi

# ─── 9. Systemd service ───────────────────────────────────────────────────────
info "Membuat systemd service..."
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=Base Meme Coin Hunter (Aerodrome)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# Hardening
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=${INSTALL_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"

sleep 2
if systemctl is-active --quiet "${SERVICE_NAME}"; then
    success "Service '${SERVICE_NAME}' berjalan!"
else
    warn "Service mungkin ada masalah. Cek: journalctl -u ${SERVICE_NAME} -n 30"
fi

# ─── 10. Selesai ──────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║          Instalasi Selesai!              ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Dashboard : ${GREEN}http://$(curl -s ifconfig.me 2>/dev/null || echo 'YOUR_IP'):${PORT}${NC}"
echo -e "  Config    : ${CYAN}${ENV_FILE}${NC}"
echo -e "  Binary    : ${CYAN}${INSTALL_DIR}/${BINARY_NAME}${NC}"
echo ""
echo -e "${BOLD}Perintah berguna:${NC}"
echo -e "  Status  : systemctl status ${SERVICE_NAME}"
echo -e "  Log     : journalctl -u ${SERVICE_NAME} -f"
echo -e "  Restart : systemctl restart ${SERVICE_NAME}"
echo -e "  Stop    : systemctl stop ${SERVICE_NAME}"
echo -e "  Config  : nano ${ENV_FILE}  (lalu restart)"
echo ""
echo -e "${YELLOW}Untuk mengaktifkan live trading:${NC}"
echo -e "  1. Edit ${ENV_FILE}"
echo -e "  2. Hapus komentar PRIVATE_KEY, BASE_RPC_URL, TRADE_SIZE_ETH"
echo -e "  3. systemctl restart ${SERVICE_NAME}"
echo ""
