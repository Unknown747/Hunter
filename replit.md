# Base Meme Coin Hunter Engine

Real-time monitoring engine that detects early meme coin opportunities on Base network (Aerodrome DEX) using a channel-based Go pipeline.

## Run & Operate

- `cd hunter && go run .` — run the hunter engine (port 8080 on VPS, 8099 on Replit)
- `cd hunter && go build -o hunter-engine . && ./hunter-engine` — build & run binary
- Dashboard: http://localhost:8080/ (VPS) or /hunter/ (Replit preview)
- Required env: `PORT` — HTTP port (default: 8080)

## Stack

- Go 1.21, net/http (no framework)
- Event-driven channel pipeline
- DexScreener API (search endpoint)
- TailwindCSS CDN + Vanilla JS dashboard

## Where things live

- `hunter/` — Go source (all pipeline stages)
  - `main.go` — entry point + pipeline orchestration
  - `fetcher.go` — DexScreener HTTP ingestion
  - `normalizer.go` — raw → TokenInfo struct
  - `filter.go` — 3-layer filter engine
  - `scorer.go` — 0-100 weighted scoring
  - `signal.go` — NEW_LISTING / MOMENTUM / BREAKOUT detection
  - `cache.go` — in-memory state with TTL cleanup
  - `api.go` — REST endpoints + CORS
  - `stats.go` — uptime/cycle counters
  - `types.go` — shared structs
  - `static/index.html` — real-time dashboard (dark, tabbed UI)
  - `go.mod` — Go module

## Architecture decisions

- Channel-based pipeline: Fetcher → rawPairs channel → single pipeline goroutine (Normalizer+Filter+Scorer+Signal+Cache) — avoids locking overhead on the hot path
- DexScreener search endpoint (`?q=aerodrome`) is the only working bulk query; `/pairs/base` returns 404
- Adaptive polling: 3s when >200 pairs found, 8s otherwise — reduces load on low-activity periods
- In-memory cache only (no DB) — optimized for low-RAM VPS (< 50MB at steady state)
- Both `/hunter/*` and bare `/*` paths served — works behind Replit proxy and standalone on VPS

## Product

- Tracks Base/Aerodrome pairs in real-time via DexScreener
- Filters: liquidity ≥ $8k, volume24h ≥ $12k, age ≤ 12h, anti-rug heuristics
- Scores 0–100: Early Age (25%) + Volume (25%) + Buy Pressure (20%) + Liquidity (15%) + Price Trend (15%)
- Signal detection: NEW_LISTING, MOMENTUM, BREAKOUT with 5-min cooldown
- Dashboard tabs: All Tokens / Gems / Signals / Top Movers / Hot Pairs

## User preferences

- Golang backend, no heavy frameworks
- Optimized for 1 core / 1GB RAM VPS
- Must be upgradeable to on-chain listener

## Gotchas

- DexScreener search API has rate limits — keep polling interval ≥ 3s
- Port 8080 is used by Replit's API server; use PORT=8099 on Replit
- Pairs with pairCreatedAt=0 get ageHours=0 (treated as brand new — slight false positive risk)

## VPS Deployment

```bash
# 1. Install Go
wget https://go.dev/dl/go1.21.13.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.13.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc

# 2. Clone / upload the hunter directory
git clone <your-repo> && cd <repo>/hunter
# or: scp -r hunter/ user@vps:~/hunter/

# 3. Build binary
go build -o hunter-engine .

# 4. Run (as service with systemd)
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
sudo systemctl status hunter

# 5. Access dashboard
# http://YOUR_VPS_IP:8080/
```

## API Reference

| Endpoint | Description |
|---|---|
| GET /api/tokens | All tracked tokens (sorted by score) |
| GET /api/gems | Only GEM-category tokens (score ≥ 75) |
| GET /api/top | Top 20 tokens by score |
| GET /api/signals | Signal log (NEW_LISTING, MOMENTUM, BREAKOUT) |
| GET /api/stats | Engine stats (uptime, cycle count, poll interval) |
| GET /api/movers | Top 10 by 5-min price change |
| GET /api/hot | Top 10 by volume/liquidity ratio |
