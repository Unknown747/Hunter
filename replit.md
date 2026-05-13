# Base Meme Coin Hunter Engine

Engine monitoring realtime + auto-trading untuk meme coin di jaringan Base (Aerodrome DEX) via DexScreener API.

## Struktur Proyek

```
main.go          ← entry point + pipeline orchestration
fetcher.go       ← DexScreener HTTP ingestion
normalizer.go    ← raw → TokenInfo struct
filter.go        ← 3-layer filter engine
scorer.go        ← 0-100 weighted scoring
signal.go        ← NEW_LISTING / MOMENTUM / BREAKOUT detection
cache.go         ← in-memory state + VolumeSpike calculation
strategy.go      ← entry/exit rule engine (CheckEntry / CheckExit)
executor.go      ← PaperExecutor + LiveExecutor (Base + Aerodrome)
position.go      ← PositionManager (open/close/monitor trades)
api.go           ← REST endpoints + CORS
stats.go         ← uptime/cycle counters
types.go         ← semua shared structs
go.mod           ← Go module
static/
  index.html     ← dark dashboard (tabbed, realtime)
install.sh       ← installer otomatis untuk VPS
.env.example     ← template konfigurasi environment
LIVE_TRADING.md  ← panduan live trading on-chain
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
sudo bash install.sh
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

**Entry (semua harus terpenuhi):**
- score ≥ 75, buyRatio ≥ 0.65, volumeSpike ≥ 2x
- likuiditas ≥ $15k, umur token 5–90 menit
- pricePump5m ≤ 120%

**Exit:**
- TP1: +12% → jual 50%
- TP2: +25% → tutup semua
- SL: -10% → tutup semua
- Emergency: buyRatio < 0.50, dump mendadak -15%, volume turun drastis
- Time exit: hold ≥ 8 menit & profit < 5%

**Posisi:** maks 3 terbuka, $1 per trade (paper mode default)

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

Lihat `LIVE_TRADING.md` untuk panduan lengkap eksekusi on-chain.

## Perintah VPS Setelah Instalasi

```bash
# Cek status
systemctl status meme-hunter

# Lihat log realtime
journalctl -u meme-hunter -f

# Restart
systemctl restart meme-hunter

# Edit konfigurasi (PORT, PRIVATE_KEY, dll)
nano /opt/meme-hunter/.env
systemctl restart meme-hunter

# Hentikan service
systemctl stop meme-hunter
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
- Live trading membutuhkan go-ethereum — lihat `LIVE_TRADING.md`
- Jangan commit `.env` ke git (sudah ada di `.gitignore`)

## Preferensi Pengguna

- Bahasa komunikasi: Bahasa Indonesia
