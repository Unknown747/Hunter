# Base Meme Coin Hunter Engine

Real-time monitoring + auto-trading engine untuk meme coin di Base network (Aerodrome DEX) via DexScreener API.

## Struktur Proyek

```
main.go        ← entry point + pipeline orchestration
fetcher.go     ← DexScreener HTTP ingestion
normalizer.go  ← raw → TokenInfo struct
filter.go      ← 3-layer filter engine
scorer.go      ← 0-100 weighted scoring
signal.go      ← NEW_LISTING / MOMENTUM / BREAKOUT detection
cache.go       ← in-memory state + VolumeSpike calculation
strategy.go    ← entry/exit rule engine (CheckEntry / CheckExit)
executor.go    ← PaperExecutor + LiveExecutor (Base + Aerodrome)
position.go    ← PositionManager (open/close/monitor trades)
api.go         ← REST endpoints + CORS
stats.go       ← uptime/cycle counters
types.go       ← semua shared structs
go.mod         ← Go module
static/
  index.html   ← dark dashboard (tabbed, realtime)
install.sh     ← VPS one-command installer
.env.example   ← template konfigurasi environment
LIVE_TRADING.md ← panduan live trading on-chain
```

## Run (Replit)

```bash
PORT=5000 go run .
```

## VPS Install (Satu Perintah)

```bash
# 1. Clone repo
git clone https://github.com/YOUR_USER/meme-hunter.git
cd meme-hunter

# 2. (Opsional) Salin dan edit config
cp .env.example .env
nano .env

# 3. Jalankan installer
sudo bash install.sh
```

Installer akan automatik:
- Pasang Go 1.21 jika belum ada
- Build binary optimized (`-ldflags="-s -w"`)
- Cipta user sistem terhad (`meme-hunter`)
- Daftar dan aktifkan systemd service
- Paparkan IP + port dashboard

## Stack

- Go 1.21, `net/http` (no framework)
- DexScreener API (search endpoint)
- TailwindCSS CDN + Vanilla JS dashboard
- Channel-based pipeline (no locks on hot path)

## API Endpoints

| Endpoint              | Deskripsi |
|-----------------------|-----------|
| GET /api/tokens       | Semua token (sorted by score) |
| GET /api/gems         | Token kategori GEM (score ≥ 75) |
| GET /api/top          | Top 20 by score |
| GET /api/signals      | Signal log (NEW_LISTING, MOMENTUM, BREAKOUT) |
| GET /api/stats        | Engine stats (uptime, cycles, poll interval) |
| GET /api/movers       | Top 10 by 5m price change |
| GET /api/hot          | Top 10 by volume/liquidity ratio |
| GET /api/positions    | Semua posisi (open + closed) |
| GET /api/trades       | Trade log (closed positions) |
| GET /api/trading-stats | Win rate, P&L, open trades |

## Strategi Trading (EARLY_MOMENTUM_SCALP)

**Entry (semua harus terpenuhi):**
- score ≥ 75, buyRatio ≥ 0.65, volumeSpike ≥ 2x
- liquidity ≥ $15k, age 5–90 minit
- pricePump5m ≤ 120%

**Exit:**
- TP1: +12% → sell 50%
- TP2: +25% → close semua
- SL: -10% → close semua
- Emergency: buyRatio < 0.50, sudden dump -15%, volume drop
- Time exit: hold ≥ 8 minit & profit < 5%

**Position:** max 3 open, $1 per trade (paper mode default)

## Mode Trading

```bash
# Paper trading (default, selamat)
PORT=8080 ./meme-hunter

# Live trading on-chain (Base + Aerodrome)
LIVE_TRADING=true \
PRIVATE_KEY=your_hex_key \
BASE_RPC_URL=https://mainnet.base.org \
PORT=8080 ./meme-hunter
```

Lihat `LIVE_TRADING.md` untuk setup lengkap on-chain execution.

## Perintah VPS Selepas Install

```bash
# Status
systemctl status meme-hunter

# Log realtime
journalctl -u meme-hunter -f

# Restart
systemctl restart meme-hunter

# Edit config (PORT, PRIVATE_KEY, dll)
nano /opt/meme-hunter/.env
systemctl restart meme-hunter

# Stop
systemctl stop meme-hunter
```

## Arsitektur

- Channel pipeline: Fetcher → rawPairs → Normalizer → Filter → Scorer → Signal → Cache → PositionManager
- Adaptive polling: 3s (>200 pairs) / 8s (low activity)
- In-memory only (< 50MB steady state)
- VolumeSpike dihitung vs first-seen volume (bukan tick-to-tick)
- Paper trading default, live trading via env vars

## Catatan Penting

- DexScreener rate limit: jangan polling < 3s
- Token dengan `pairCreatedAt=0` (age unknown) di-skip untuk entry
- Live trading butuh go-ethereum — lihat LIVE_TRADING.md
- Jangan commit `.env` ke git (sudah di `.gitignore`)

## User Preferences

- Bahasa komunikasi: Bahasa Melayu / Indonesia
