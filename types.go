package main

import "time"

// TokenInfo adalah representasi internal yang sudah dinormalisasi dari sebuah pair/token.
type TokenInfo struct {
        PairAddress       string    `json:"pairAddress"`
        TokenAddress      string    `json:"tokenAddress"`
        QuoteTokenAddress string    `json:"quoteTokenAddress"` // token lawan dalam pair (WETH, USDC, dll)
        Name              string    `json:"name"`
        Symbol            string    `json:"symbol"`
        Price         float64   `json:"price"`
        Liquidity     float64   `json:"liquidity"`
        Volume24h     float64   `json:"volume24h"`
        TxnsBuy       int       `json:"txnsBuy"`
        TxnsSell      int       `json:"txnsSell"`
        PairAgeHours  float64   `json:"pairAgeHours"`
        PriceChange5m float64   `json:"priceChange5m"`
        PriceChange1h float64   `json:"priceChange1h"`
        MarketCap     float64   `json:"marketCap"`
        Score         float64   `json:"score"`
        Category      string    `json:"category"` // GEM / STRONG / WATCHLIST
        VolumeSpike   float64   `json:"volumeSpike"`
        BuyRatio      float64   `json:"buyRatio"`
        Signals       []string  `json:"signals"`
        UpdatedAt     time.Time `json:"updatedAt"`
}

// TokenState melacak data historis per-token untuk deteksi sinyal.
type TokenState struct {
        LastScore     float64
        LastVolume    float64
        LastPrice     float64
        FirstVolume   float64
        FirstSeen     time.Time
        LastSeen      time.Time
        AlertCooldown time.Time
        History       []ScoreSnapshot
}

// ScoreSnapshot adalah rekaman point-in-time untuk deteksi momentum.
type ScoreSnapshot struct {
        Score     float64   `json:"score"`
        Volume    float64   `json:"volume"`
        Price     float64   `json:"price"`
        Timestamp time.Time `json:"timestamp"`
}

// Signal merepresentasikan event market yang terdeteksi.
type Signal struct {
        Type        string    `json:"type"`
        PairAddress string    `json:"pairAddress"`
        Symbol      string    `json:"symbol"`
        Score       float64   `json:"score"`
        Detail      string    `json:"detail"`
        Timestamp   time.Time `json:"timestamp"`
}

// EngineStats dikembalikan oleh GET /api/stats.
type EngineStats struct {
        TotalTracked   int    `json:"totalTracked"`
        TotalGems      int    `json:"totalGems"`
        TotalStrong    int    `json:"totalStrong"`
        TotalWatchlist int    `json:"totalWatchlist"`
        TotalSignals   int    `json:"totalSignals"`
        PollInterval   string `json:"pollInterval"`
        Uptime         string `json:"uptime"`
        CycleCount     int64  `json:"cycleCount"`
}

// ─── Trading types ────────────────────────────────────────────────────────────

type PositionStatus string

const (
        PositionOpen   PositionStatus = "OPEN"
        PositionClosed PositionStatus = "CLOSED"
)

// Position merepresentasikan trade terbuka atau tertutup di Base mainnet.
type Position struct {
        ID             string         `json:"id"`
        PairAddress    string         `json:"pairAddress"`
        TokenAddress   string         `json:"tokenAddress"`
        Symbol         string         `json:"symbol"`
        EntryPrice     float64        `json:"entryPrice"`
        EntryVolume    float64        `json:"entryVolume"`
        CurrentPrice   float64        `json:"currentPrice"`
        SizeUSD        float64        `json:"sizeUSD"`
        RemainingPct   float64        `json:"remainingPct"` // 0–100
        EntryTime      time.Time      `json:"entryTime"`
        Status         PositionStatus `json:"status"`
        PnLPercent     float64        `json:"pnlPercent"`
        RealizedUSD    float64        `json:"realizedUSD"`
        ExitReason     string         `json:"exitReason,omitempty"`
        TP1Hit         bool           `json:"tp1Hit"`
        HighWaterMark  float64        `json:"highWaterMark"`   // trailing stop — harga tertinggi yang pernah dicapai
        GasCostUSD     float64        `json:"gasCostUSD"`      // akumulasi biaya gas
        Fills          []Fill         `json:"fills"`
        // Snapshot saat entry — dipakai untuk smart rug pattern detection
        EntryAgeMinutes  float64 `json:"entryAgeMinutes"`
        EntryBuyRatio    float64 `json:"entryBuyRatio"`
        EntryPricePump5m float64 `json:"entryPricePump5m"`
}

// Fill mencatat setiap aksi beli atau jual parsial/penuh.
type Fill struct {
        Action    string    `json:"action"` // BUY / SELL
        Price     float64   `json:"price"`
        PctOfPos  float64   `json:"pctOfPos"`
        USD       float64   `json:"usd"`
        PnLPct    float64   `json:"pnlPct"`
        GasUSD    float64   `json:"gasUSD,omitempty"` // biaya gas transaksi ini
        Reason    string    `json:"reason"`
        TxHash    string    `json:"txHash,omitempty"`
        Timestamp time.Time `json:"timestamp"`
}

// TradeLog ditulis saat posisi ditutup sepenuhnya.
type TradeLog struct {
        ID          string    `json:"id"`
        PairAddress string    `json:"pairAddress"`
        Symbol      string    `json:"symbol"`
        EntryPrice  float64   `json:"entryPrice"`
        ExitPrice   float64   `json:"exitPrice"`
        SizeUSD     float64   `json:"sizeUSD"`
        PnLPercent  float64   `json:"pnlPercent"`
        PnLUSD      float64   `json:"pnlUSD"`
        GasCostUSD  float64   `json:"gasCostUSD"`            // total biaya gas
        NetPnLUSD   float64   `json:"netPnLUSD"`             // PnLUSD dikurangi gas
        Duration    string    `json:"duration"`
        ExitReason  string    `json:"exitReason"`
        BuyTxHash   string    `json:"buyTxHash,omitempty"`
        SellTxHash  string    `json:"sellTxHash,omitempty"`
        OpenTime    time.Time `json:"openTime"`
        CloseTime   time.Time `json:"closeTime"`
}

// TradingStats dikembalikan oleh GET /api/trading-stats.
type TradingStats struct {
        OpenTrades    int     `json:"openTrades"`
        MaxTrades     int     `json:"maxTrades"`
        TotalTrades   int     `json:"totalTrades"`
        WinCount      int     `json:"winCount"`
        LossCount     int     `json:"lossCount"`
        WinRate       float64 `json:"winRate"`
        TotalPnLUSD   float64 `json:"totalPnLUSD"`
        TotalGasUSD   float64 `json:"totalGasUSD"`
        NetPnLUSD     float64 `json:"netPnLUSD"`
        AvgPnLPct     float64 `json:"avgPnLPct"`
        BestTradePct  float64 `json:"bestTradePct"`
        WorstTradePct float64 `json:"worstTradePct"`
        RiskLevel     string  `json:"riskLevel"`
}

// BlacklistEntry mencatat token yang diblokir sementara.
type BlacklistEntry struct {
        PairAddress string    `json:"pairAddress"`
        Symbol      string    `json:"symbol"`
        Reason      string    `json:"reason"`
        SLCount     int       `json:"slCount"`
        LastSL      time.Time `json:"lastSL"`
        ExpireAt    time.Time `json:"expireAt"`
}

// PersistedState adalah snapshot lengkap yang disimpan ke disk.
type PersistedState struct {
        Positions   map[string]*Position      `json:"positions"`
        Trades      []*TradeLog               `json:"trades"`
        Blacklist   map[string]*BlacklistEntry `json:"blacklist"`
        RugPatterns []RugPattern              `json:"rugPatterns,omitempty"`
        SavedAt     time.Time                 `json:"savedAt"`
}

// HealthStatus dikembalikan oleh GET /health.
type HealthStatus struct {
        Status        string `json:"status"`
        Uptime        string `json:"uptime"`
        CycleCount    int64  `json:"cycleCount"`
        TrackedTokens int    `json:"trackedTokens"`
        OpenPositions int    `json:"openPositions"`
        RiskLevel     string `json:"riskLevel"`
        LastPollAgo   string `json:"lastPollAgo"`
}

// StrategyConfig menyimpan semua parameter yang bisa diatur.
type StrategyConfig struct {
        MinScore            float64
        MinBuyRatio         float64
        MinVolumeSpike      float64
        MinLiquidityUSD     float64
        MinAgeMinutes       float64
        MaxAgeMinutes       float64
        MaxPricePump5m      float64
        TP1Pct              float64
        TP1SellFrac         float64
        TP2Pct              float64
        StopLossPct         float64
        TrailingStopPct     float64 // trailing stop % dari high water mark (0 = nonaktif)
        TrailingActivatePct float64 // aktifkan trailing stop setelah profit ini %
        EmergencyBuyRatio   float64
        SuddenDumpThreshold float64
        VolumeDropFraction  float64
        MaxHoldMinutes      float64
        MinProfitForHold    float64
        TradeSizeUSD        float64
        MaxOpenTrades       int
        RiskLevel           string
}

// DefaultConfig mengembalikan strategi EARLY_MOMENTUM_SCALP (normal).
// Dioptimalkan untuk scalping meme coin listing baru (< 90 menit).
func DefaultConfig() *StrategyConfig {
        return &StrategyConfig{
                // ── Entry conditions ────────────────────────────────────────────────
                MinScore:        70,    // score minimum — scorer memprioritaskan token baru + momentum kuat
                MinBuyRatio:     0.62,  // minimal 62% transaksi adalah buy (tekanan beli dominan)
                MinVolumeSpike:  2.0,   // 2x dari volume pertama terlihat — konfirmasi momentum nyata
                MinLiquidityUSD: 15000, // min $15k — likuiditas yang cukup untuk reduce slippage
                MinAgeMinutes:   5,     // min 5 menit — cukup waktu untuk validasi, kurangi risiko rug
                MaxAgeMinutes:   30,    // max 30 menit — meme coin pump biasanya selesai dalam 30 menit pertama
                MaxPricePump5m:  60,    // hindari top-buy: max +60% dalam 5m (scorer penalti >60%)
                // ── Exit: Take Profit ───────────────────────────────────────────────
                TP1Pct:      15,  // TP1: +15% → jual 50% (amankan sebagian profit)
                TP1SellFrac: 0.5, // jual 50% saat TP1
                TP2Pct:      28,  // TP2: +28% → tutup semua (realistis dicapai dalam 20 menit)
                // ── Exit: Stop Loss ─────────────────────────────────────────────────
                StopLossPct:         -8, // SL: -8% dari entry
                TrailingStopPct:     7,  // trailing stop 7% dari high water mark
                TrailingActivatePct: 7,  // aktifkan trailing setelah profit mencapai 7%
                // ── Exit: Emergency ─────────────────────────────────────────────────
                // Emergency: tanda distribusi/dump aktif — keluar segera tanpa tunggu SL
                EmergencyBuyRatio:   0.48, // buyRatio collapse < 48% = distribusi besar-besaran
                SuddenDumpThreshold: -12,  // dump > 12% dalam 5m = crash mendadak
                VolumeDropFraction:  0.30, // exit jika volume H24 turun ke < 30% dari saat entry
                //                            (70% drop = trading hampir mati — lebih responsif dari 80%)
                // ── Exit: Time ──────────────────────────────────────────────────────
                MaxHoldMinutes:   20, // max hold 20 menit — cukup waktu untuk TP2 (+28%) tercapai
                MinProfitForHold: 5,  // butuh min 5% profit untuk hold lebih dari 20 menit
                // ── Position sizing ─────────────────────────────────────────────────
                TradeSizeUSD:  1.0, // $1 per trade (paper mode default)
                MaxOpenTrades: 3,   // max 3 posisi terbuka bersamaan
                RiskLevel:     "normal",
        }
}

// ConfigForRisk mengembalikan konfigurasi sesuai level risiko.
// RISK_LEVEL=conservative | normal | aggressive
func ConfigForRisk(level string) *StrategyConfig {
        cfg := DefaultConfig()
        switch level {
        case "conservative":
                // ── Conservative: selektif maksimal, prioritas modal aman ─────────────
                // Hanya masuk jika semua sinyal sangat kuat, exit lebih cepat
                cfg.MinScore = 80           // score lebih tinggi = sinyal lebih kuat
                cfg.MinBuyRatio = 0.68      // dominasi beli yang jelas
                cfg.MinVolumeSpike = 2.5    // spike lebih besar = momentum lebih terkonfirmasi
                cfg.MinLiquidityUSD = 25000 // likuiditas lebih tinggi = slippage lebih kecil
                cfg.MinAgeMinutes = 7       // lebih banyak waktu validasi
                cfg.MaxAgeMinutes = 20      // hanya fokus pada 20 menit pertama (paling volatile untuk conservative)
                cfg.MaxPricePump5m = 40     // hindari token yang sudah pump
                cfg.TP1Pct = 12             // TP1 lebih konservatif
                cfg.TP2Pct = 22             // TP2 realistis dalam 12 menit (diturunkan dari 30)
                cfg.StopLossPct = -6        // SL lebih ketat
                cfg.TrailingStopPct = 5
                cfg.TrailingActivatePct = 5
                cfg.EmergencyBuyRatio = 0.52
                cfg.SuddenDumpThreshold = -8  // lebih sensitif pada dump (dari -10)
                cfg.VolumeDropFraction = 0.25 // exit jika volume turun >75% dari entry
                cfg.MaxHoldMinutes = 12       // diperpanjang 7→12 agar TP2 bisa tercapai
                cfg.MinProfitForHold = 3
                cfg.MaxOpenTrades = 2 // hanya 2 posisi bersamaan
                cfg.RiskLevel = "conservative"
        case "aggressive":
                // ── Aggressive: maksimalkan peluang, toleransi risiko lebih tinggi ────
                // Masuk lebih awal, target profit lebih besar, tahan lebih lama
                cfg.MinScore = 62           // lebih inklusif, percaya pada sinyal yang sedikit lebih lemah
                cfg.MinBuyRatio = 0.58      // toleransi buy pressure sedikit lebih rendah
                cfg.MinVolumeSpike = 1.5    // spike lebih kecil ok karena kita masuk lebih awal
                cfg.MinLiquidityUSD = 10000 // toleransi likuiditas lebih rendah
                cfg.MinAgeMinutes = 3       // masuk lebih awal (risiko lebih tinggi)
                cfg.MaxAgeMinutes = 45      // max 45 menit untuk aggressive mode
                cfg.MaxPricePump5m = 80     // toleransi pump lebih tinggi (momentum bisa berlanjut)
                cfg.TP1Pct = 20             // TP1 lebih besar — tunggu momentum lebih jauh
                cfg.TP2Pct = 50             // TP2 realistis dalam 25 menit (diturunkan dari 60)
                cfg.StopLossPct = -12       // SL lebih longgar — kasih ruang bernapas
                cfg.TrailingStopPct = 10    // trailing lebih longgar
                cfg.TrailingActivatePct = 12
                cfg.EmergencyBuyRatio = 0.42
                cfg.SuddenDumpThreshold = -18
                cfg.VolumeDropFraction = 0.35 // exit jika volume turun >65% — lebih responsif
                cfg.MaxHoldMinutes = 25       // diperpanjang 15→25 untuk kejar pump besar
                cfg.MinProfitForHold = 7      // butuh 7% profit untuk hold lebih dari 25 menit
                cfg.MaxOpenTrades = 5 // 5 posisi bersamaan
                cfg.RiskLevel = "aggressive"
        default:
                cfg.RiskLevel = "normal"
        }
        return cfg
}

// ─── DexScreener raw API response structs ─────────────────────────────────────

type DexResponse struct {
        Pairs []DexPair `json:"pairs"`
}

type DexPair struct {
        ChainID       string       `json:"chainId"`
        DexID         string       `json:"dexId"`
        PairAddress   string       `json:"pairAddress"`
        BaseToken     DexToken     `json:"baseToken"`
        QuoteToken    DexToken     `json:"quoteToken"`
        PriceUsd      string       `json:"priceUsd"`
        Liquidity     DexLiquidity `json:"liquidity"`
        Volume        DexVolume    `json:"volume"`
        Txns          DexTxns      `json:"txns"`
        PriceChange   DexChange    `json:"priceChange"`
        FdvUsd        float64      `json:"fdv"`
        PairCreatedAt int64        `json:"pairCreatedAt"`
}

type DexToken struct {
        Address string `json:"address"`
        Name    string `json:"name"`
        Symbol  string `json:"symbol"`
}

type DexLiquidity struct {
        Usd float64 `json:"usd"`
}

type DexVolume struct {
        H24 float64 `json:"h24"`
}

type DexTxns struct {
        H24 DexTxnSide `json:"h24"`
}

type DexTxnSide struct {
        Buys  int `json:"buys"`
        Sells int `json:"sells"`
}

type DexChange struct {
        M5 float64 `json:"m5"`
        H1 float64 `json:"h1"`
}
