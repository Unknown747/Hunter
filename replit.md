# Base Meme Coin Hunter Engine

Real-time monitoring + auto-trading engine untuk meme coin di Base network (Aerodrome DEX) via DexScreener API.

## Struktur Proyek

```
hunter/          ← seluruh source Go
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
  LIVE_TRADING.md ← panduan live trading on-chain
```

## Run

```bash
# Replit (preview di /hunter/)
cd hunter && PORT=8099 go run .

# VPS
cd hunter
go build -o hunter-engine .
PORT=8080 ./hunter-engine
```

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
- liquidity ≥ $15k, age 5–90 menit
- pricePump5m ≤ 120%

**Exit:**
- TP1: +12% → sell 50%
- TP2: +25% → close semua
- SL: -10% → close semua
- Emergency: buyRatio < 0.50, sudden dump -15%, volume drop
- Time exit: hold ≥ 8 menit & profit < 5%

**Position:** max 3 open, $1 per trade (paper mode default)

## Mode Trading

```bash
# Paper trading (default, aman)
PORT=8080 ./hunter-engine

# Live trading on-chain (Base + Aerodrome)
LIVE_TRADING=true \
PRIVATE_KEY=your_hex_key \
BASE_RPC_URL=https://mainnet.base.org \
PORT=8080 ./hunter-engine
```

Lihat `LIVE_TRADING.md` untuk setup lengkap on-chain execution.

## VPS Deployment

```bash
# 1. Install Go
wget https://go.dev/dl/go1.21.13.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.13.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc

# 2. Build
cd hunter
go build -o hunter-engine .

# 3. Run sebagai service
sudo tee /etc/systemd/system/hunter.service > /dev/null <<EOF
[Unit]
Description=Base Meme Coin Hunter
After=network.target

[Service]
ExecStart=/home/ubuntu/hunter/hunter-engine
WorkingDirectory=/home/ubuntu/hunter
Restart=always
RestartSec=5
Environment=PORT=8080

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable hunter
sudo systemctl start hunter
```

## Arsitektur

- Channel pipeline: Fetcher → rawPairs → Normalizer → Filter → Scorer → Signal → Cache → PositionManager
- Adaptive polling: 3s (>200 pairs) / 8s (low activity)
- In-memory only (< 50MB steady state)
- VolumeSpike dihitung vs first-seen volume (bukan tick-to-tick)
- Paper trading default, live trading via env vars

## Catatan Penting

- DexScreener rate limit: jangan polling < 3s
- Port 8080 dipakai Replit system → gunakan PORT=8099 di Replit
- Token dengan `pairCreatedAt=0` (age unknown) di-skip untuk entry
- Live trading butuh go-ethereum — lihat LIVE_TRADING.md
