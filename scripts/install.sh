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

# Direktori script ini dan root proyek (satu level di atas scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo -e ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║    Base Meme Coin Hunter — VPS Install   ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

# ─── 1. Cek root ──────────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    error "Jalankan sebagai root: sudo bash scripts/install.sh"
fi

# ─── 2. Cek OS ────────────────────────────────────────────────────────────────
if ! command -v apt-get &>/dev/null && ! command -v yum &>/dev/null; then
    error "OS tidak didukung. Hanya Debian/Ubuntu/CentOS."
fi

# ─── 3. Install dependencies sistem ──────────────────────────────────────────
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
    wget -q "https://go.dev/dl/${GO_TAR}" -O "/tmp/${GO_TAR}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TAR}"
    rm "/tmp/${GO_TAR}"

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

# Salin Go source dari root proyek
cp -r "${PROJECT_DIR}"/*.go   "$INSTALL_DIR/" 2>/dev/null || true
cp    "${PROJECT_DIR}/go.mod" "$INSTALL_DIR/"
cp    "${PROJECT_DIR}/go.sum" "$INSTALL_DIR/"
cp -r "${PROJECT_DIR}/static" "$INSTALL_DIR/"

# Salin script operasional ke INSTALL_DIR (agar bisa dijalankan langsung)
for script in sell.sh start.sh stop.sh; do
    if [[ -f "${SCRIPT_DIR}/${script}" ]]; then
        cp "${SCRIPT_DIR}/${script}" "$INSTALL_DIR/$script"
        chmod +x "$INSTALL_DIR/$script"
    fi
done

if [[ -f "${PROJECT_DIR}/.env" ]]; then
    cp "${PROJECT_DIR}/.env" "$INSTALL_DIR/.env"
    warn ".env disalin — pastikan PRIVATE_KEY aman (chmod 600 $INSTALL_DIR/.env)"
fi

success "File proyek berhasil disalin ke $INSTALL_DIR"

# ─── 7. Build binary ──────────────────────────────────────────────────────────
info "Membangun binary ($BINARY_NAME)..."
cd "$INSTALL_DIR"
go mod download 2>&1 | tail -3
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
# Cukup set PRIVATE_KEY untuk mengaktifkan live trading
# PRIVATE_KEY=hex_private_key_tanpa_0x
# BASE_RPC_URL=https://mainnet.base.org
# BASE_RPC_URL_BACKUP=https://base-mainnet.g.alchemy.com/v2/YOUR_KEY
# TRADE_SIZE_ETH=0.001
# SLIPPAGE_PCT=5.0

# ─── Risk Level ───────────────────────────────────────────────────────────────
# RISK_LEVEL=normal   # conservative | normal (default) | aggressive
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

# ─── 10. Nginx + SSL (opsional) ───────────────────────────────────────────────
VPS_IP=$(curl -s ifconfig.me 2>/dev/null || echo "YOUR_IP")

echo ""
echo -e "${BOLD}━━━ Nginx + SSL (Opsional) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "  Dashboard saat ini bisa diakses via: ${GREEN}http://${VPS_IP}:${PORT}${NC}"
echo -e "  Jika kamu punya domain, Nginx + SSL otomatis bisa dipasang sekarang."
echo ""
read -rp "$(echo -e "${YELLOW}Pasang Nginx + SSL sekarang? [y/N]: ${NC}")" NGINX_CONFIRM

if [[ "${NGINX_CONFIRM,,}" == "y" ]]; then
    read -rp "$(echo -e "${CYAN}Masukkan nama domain (contoh: hunter.domainku.com): ${NC}")" DOMAIN
    DOMAIN="${DOMAIN// /}"

    if [[ -z "$DOMAIN" ]]; then
        warn "Domain kosong — lewati konfigurasi Nginx."
    else
        # Install Nginx + Certbot
        info "Menginstal Nginx dan Certbot..."
        if command -v apt-get &>/dev/null; then
            apt-get install -y -qq nginx certbot python3-certbot-nginx
        else
            yum install -y nginx certbot python3-certbot-nginx
        fi
        success "Nginx dan Certbot terinstal"

        # Konfigurasi Nginx
        info "Mengkonfigurasi Nginx untuk ${DOMAIN}..."
        cat > "/etc/nginx/sites-available/${SERVICE_NAME}" <<NGINXCONF
server {
    listen 80;
    server_name ${DOMAIN};

    # Redirect semua HTTP ke HTTPS (diisi oleh certbot)
    location / {
        proxy_pass         http://127.0.0.1:${PORT};
        proxy_http_version 1.1;
        proxy_set_header   Upgrade \$http_upgrade;
        proxy_set_header   Connection keep-alive;
        proxy_set_header   Host \$host;
        proxy_set_header   X-Real-IP \$remote_addr;
        proxy_set_header   X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
        proxy_read_timeout 60s;
    }
}
NGINXCONF

        # Aktifkan site
        mkdir -p /etc/nginx/sites-enabled
        ln -sf "/etc/nginx/sites-available/${SERVICE_NAME}" \
               "/etc/nginx/sites-enabled/${SERVICE_NAME}"

        # Hapus default site jika ada
        rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true

        nginx -t && systemctl reload nginx
        success "Nginx dikonfigurasi untuk ${DOMAIN}"

        # Ambil SSL certificate
        info "Mengambil SSL certificate dari Let's Encrypt untuk ${DOMAIN}..."
        read -rp "$(echo -e "${CYAN}Email untuk Let's Encrypt (untuk notifikasi expire): ${NC}")" LE_EMAIL

        if [[ -n "$LE_EMAIL" ]]; then
            certbot --nginx \
                -d "${DOMAIN}" \
                --non-interactive \
                --agree-tos \
                --email "${LE_EMAIL}" \
                --redirect \
                && success "SSL berhasil! Dashboard: https://${DOMAIN}" \
                || warn "Certbot gagal. Pastikan domain ${DOMAIN} mengarah ke IP ${VPS_IP} dan port 80 terbuka."
        else
            warn "Email kosong — SSL tidak dipasang. Jalankan manual: certbot --nginx -d ${DOMAIN}"
        fi

        # Auto-renew SSL via cron
        if ! crontab -l 2>/dev/null | grep -q "certbot renew"; then
            (crontab -l 2>/dev/null; echo "0 3 * * * certbot renew --quiet && systemctl reload nginx") | crontab -
            success "Auto-renew SSL ditambahkan ke cron (setiap hari jam 03:00)"
        fi
    fi
fi

# ─── Selesai ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║          Instalasi Selesai!              ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════╝${NC}"
echo ""

if [[ "${NGINX_CONFIRM,,}" == "y" && -n "${DOMAIN:-}" ]]; then
    echo -e "  Dashboard : ${GREEN}https://${DOMAIN}${NC}"
fi
echo -e "  Dashboard : ${GREEN}http://${VPS_IP}:${PORT}${NC}"
echo -e "  Config    : ${CYAN}${ENV_FILE}${NC}"
echo -e "  State     : ${CYAN}${INSTALL_DIR}/state.json${NC}  (posisi tersimpan)"
echo ""
echo -e "${BOLD}Script operasional:${NC}"
echo -e "  ${GREEN}bash ${INSTALL_DIR}/start.sh${NC}   — Mulai engine"
echo -e "  ${GREEN}bash ${INSTALL_DIR}/stop.sh${NC}    — Hentikan engine (jual posisi dulu)"
echo -e "  ${GREEN}bash ${INSTALL_DIR}/sell.sh${NC}    — Tutup semua posisi (engine tetap jalan)"
echo ""
echo -e "${BOLD}Perintah systemd:${NC}"
echo -e "  systemctl status  ${SERVICE_NAME}"
echo -e "  journalctl -u ${SERVICE_NAME} -f"
echo -e "  systemctl restart ${SERVICE_NAME}"
echo ""
echo -e "${YELLOW}Untuk mengaktifkan live trading:${NC}"
echo -e "  1. Edit ${ENV_FILE}"
echo -e "  2. Hapus komentar PRIVATE_KEY, BASE_RPC_URL, TRADE_SIZE_ETH"
echo -e "  3. systemctl restart ${SERVICE_NAME}"
echo ""
echo -e "${BOLD}Alur stop aman:${NC}"
echo -e "  bash ${INSTALL_DIR}/stop.sh          — jual semua posisi, lalu stop"
echo -e "  bash ${INSTALL_DIR}/stop.sh --force  — stop paksa tanpa jual posisi"
echo ""
