# Base Meme Coin Hunter Engine

Engine monitoring realtime + auto-trading untuk meme coin di jaringan Base (Aerodrome DEX) via DexScreener API.

## Struktur Proyek

```
├── main.go               ← entry point + pipeline orchestration
├── types.go              ← semua shared structs
├── fetcher.go            ← DexScreener HTTP ingestion
├── normalizer.go         ← raw → TokenInfo struct
├── filter.go             ← 3-layer filter engine
├── scorer.go             ← 0-100 weighted scoring
├── signal.go             ← NEW_LISTING / MOMENTUM / BREAKOUT detection
├── cache.go              ← in-memory state + VolumeSpike calculation
├── strategy.go           ← entry/exit rule engine (CheckEntry / CheckExit)
├── executor.go           ← PaperExecutor + LiveExecutor (Base + Aerodrome)
├── position.go           ← PositionManager (open/close/monitor trades)
├── factory_watcher.go    ← on-chain event monitor (PairCreated/PoolCreated)
├── telegram.go           ← Telegram notifier
├── telegram_commands.go  ← Telegram bot commands (/status, /pos, dll)
├── blacklist.go          ← token blacklist
├── rugpattern.go         ← rug pattern detection
├── persistence.go        ← save/load state ke state.json
├── api.go                ← REST endpoints + CORS
├── stats.go              ← uptime/cycle counters
├── go.mod / go.sum       ← Go module
├── .env.example          ← template konfigurasi environment
├── state.json            ← state posisi (auto-generated)
│
├── static/
│   └── index.html        ← dark dashboard (tabbed, realtime)
│
├── scripts/
│   ├── install.sh        ← installer otomatis untuk VPS
│   ├── update.sh         ← auto-update dari GitHub + graceful reload
│   ├── start.sh          ← mulai engine
│   ├── stop.sh           ← hentikan engine (jual posisi dulu)
│   └── sell.sh           ← tutup semua posisi via API
│
└── docs/
    └── LIVE_TRADING.md   ← panduan live trading on-chain
```

## Menjalankan di Replit

```bash
PORT=5000 go run .
```

## Instalasi di VPS (Satu Perintah)

```bash
# 1. Clone repo
git clone https://github.com/YOUR_USER/meme-hunter.git
cd meme-hunter

# 2. (Opsional) Setup konfigurasi dulu
cp .env.example .env
nano .env

# 3. Jalankan installer
sudo bash scripts/install.sh
```

Installer akan otomatis:
- Menginstal Go 1.21 jika belum ada (support x86_64 & ARM)
- Membangun binary yang dioptimasi (`-ldflags="-s -w"`)
- Membuat user sistem terbatas (`meme-hunter`)
- Mendaftarkan dan mengaktifkan systemd service (auto-start saat reboot)
- Menampilkan IP + port dashboard setelah selesai

## Stack Teknologi

- Go 1.21, `net/http` (tanpa framework)
- DexScreener API (search endpoint)
- TailwindCSS CDN + Vanilla JS dashboard
- Channel-based pipeline (tanpa lock di hot path)

## API Endpoints

| Endpoint               | Deskripsi |
|------------------------|-----------|
| GET /api/tokens        | Semua token (diurutkan berdasarkan score) |
| GET /api/gems          | Token kategori GEM (score ≥ 75) |
| GET /api/top           | Top 20 berdasarkan score |
| GET /api/signals       | Log sinyal (NEW_LISTING, MOMENTUM, BREAKOUT) |
| GET /api/stats         | Statistik engine (uptime, cycles, poll interval) |
| GET /api/movers        | Top 10 berdasarkan perubahan harga 5m |
| GET /api/hot           | Top 10 berdasarkan rasio volume/likuiditas |
| GET /api/positions     | Semua posisi (open + closed) |
| GET /api/trades        | Log trade (posisi yang sudah ditutup) |
| GET /api/trading-stats | Win rate, P&L, jumlah trade terbuka |

## Strategi Trading (EARLY_MOMENTUM_SCALP)

### Mode Normal (default)

**Entry (semua harus terpenuhi):**
- score ≥ 70, buyRatio ≥ 0.62, volumeSpike ≥ 2x dari baseline
- likuiditas ≥ $15k, umur token 5–90 menit
- pricePump5m ≤ 60% (hindari top-buy)

**Entry guard tambahan:**
- Token < 10 menit: buyRatio ≥ 0.72 & pump5m ≤ 40% (threshold lebih ketat)
- Token > 15 menit: vol/liq ratio ≥ 0.08 (pastikan ada aktivitas trading nyata)

**Exit:**
- TP1: +15% → jual 50%
- TP2: +28% → tutup semua (realistis dalam 20 menit)
- SL: -8% → tutup semua
- Trailing stop: aktif setelah +7%, stop jika turun 7% dari high
- Emergency: buyRatio < 0.48, dump mendadak -12% dalam 5m, volume turun >70%
- Time exit: hold ≥ 20 menit & profit < 5%

**Posisi:** maks 3 terbuka, $1 per trade (paper mode default)

### Mode Conservative (`RISK_LEVEL=conservative`)
- Entry: score ≥ 80, buyRatio ≥ 0.68, spike ≥ 2.5x, liq ≥ $25k, umur 7–60 menit, pump5m ≤ 40%
- Exit: TP1 +12%, TP2 +22%, SL -6%, trailing 5%, hold max 12 menit

### Mode Aggressive (`RISK_LEVEL=aggressive`)
- Entry: score ≥ 62, buyRatio ≥ 0.58, spike ≥ 1.5x, liq ≥ $10k, umur 3–90 menit, pump5m ≤ 80%
- Exit: TP1 +20%, TP2 +50%, SL -12%, trailing 10%, hold max 25 menit

## Mode Trading

```bash
# Paper trading (default, aman untuk testing — tanpa PRIVATE_KEY)
PORT=8080 ./meme-hunter

# Live trading on-chain (Base + Aerodrome — cukup set PRIVATE_KEY)
PRIVATE_KEY=your_hex_key \
BASE_RPC_URL=https://mainnet.base.org \
TRADE_SIZE_ETH=0.001 \
SLIPPAGE_PCT=5.0 \
PORT=8080 ./meme-hunter
```

Lihat `docs/LIVE_TRADING.md` untuk panduan lengkap eksekusi on-chain.

## Update Otomatis dari GitHub

```bash
# Update ke versi terbaru (pull + rebuild + graceful restart)
sudo bash /opt/meme-hunter/scripts/update.sh

# Cek apakah ada update tersedia (tanpa eksekusi)
sudo bash scripts/update.sh --dry-run

# Rollback ke versi sebelumnya jika ada masalah
sudo bash scripts/update.sh --rollback

# Build ulang saja tanpa restart service
sudo bash scripts/update.sh --no-restart
```

## Perintah VPS Setelah Instalasi

```bash
# Cek status
systemctl status meme-hunter

# Lihat log realtime
journalctl -u meme-hunter -f

# Restart manual
systemctl restart meme-hunter

# Edit konfigurasi (PORT, PRIVATE_KEY, dll)
nano /opt/meme-hunter/.env
systemctl restart meme-hunter

# Hentikan service
systemctl stop meme-hunter
```

## Script Operasional

```bash
bash scripts/start.sh          # Mulai engine
bash scripts/stop.sh           # Hentikan engine (jual posisi dulu)
bash scripts/stop.sh --force   # Stop paksa tanpa jual posisi
bash scripts/sell.sh           # Tutup semua posisi (engine tetap jalan)
sudo bash scripts/install.sh   # Install di VPS baru
sudo bash scripts/update.sh    # Update dari GitHub
```

## Arsitektur

- Channel pipeline: Fetcher → rawPairs → Normalizer → Filter → Scorer → Signal → Cache → PositionManager
- Adaptive polling: 3s (>200 pairs) / 8s (aktivitas rendah)
- In-memory only (< 50MB steady state)
- VolumeSpike dihitung vs volume pertama kali terlihat (bukan tick-to-tick)
- Paper trading default, live trading via env vars

## Catatan Penting

- DexScreener rate limit: jangan polling < 3s
- Token dengan `pairCreatedAt=0` (umur tidak diketahui) dilewati untuk entry
- Live trading membutuhkan go-ethereum — lihat `docs/LIVE_TRADING.md`
- Jangan commit `.env` ke git (sudah ada di `.gitignore`)

## Preferensi Pengguna

- Bahasa komunikasi: Bahasa Indonesia
