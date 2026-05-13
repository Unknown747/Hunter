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
// Dioptimalkan untuk scalping meme coin listing baru (< 2 jam).
func DefaultConfig() *StrategyConfig {
        return &StrategyConfig{
                // ── Entry conditions ────────────────────────────────────────────────
                MinScore:        70,    // lebih inklusif — scorer baru sudah lebih selektif
                MinBuyRatio:     0.62,  // minimal 62% transaksi adalah buy
                MinVolumeSpike:  1.5,   // 1.5x dari volume pertama terlihat (realistis untuk listing baru)
                MinLiquidityUSD: 15000, // min $15k likuiditas untuk masuk
                MinAgeMinutes:   3,     // masuk lebih awal: 3 menit (anti-rug tetap aktif)
                MaxAgeMinutes:   90,    // max 90 menit (masih dalam window 2 jam)
                MaxPricePump5m:  80,    // hindari token yang sudah pump > 80% dalam 5m
                // ── Exit: Take Profit ───────────────────────────────────────────────
                TP1Pct:      15,  // TP1: +15% → jual 50% (lebih baik dari 12%)
                TP1SellFrac: 0.5, // jual 50% saat TP1
                TP2Pct:      35,  // TP2: +35% → tutup semua (biarkan winner berjalan)
                // ── Exit: Stop Loss ─────────────────────────────────────────────────
                StopLossPct:         -8, // SL lebih ketat: -8% (dari -10%)
                TrailingStopPct:     7,  // trailing stop 7% dari high water mark
                TrailingActivatePct: 7,  // aktifkan trailing setelah profit 7%
                // ── Exit: Emergency ─────────────────────────────────────────────────
                EmergencyBuyRatio:   0.48, // exit darurat jika buy ratio < 48%
                SuddenDumpThreshold: -12,  // exit darurat jika dump > 12% dalam 5m
                VolumeDropFraction:  0.40, // exit jika volume turun > 60% dari entry
                // ── Exit: Time ──────────────────────────────────────────────────────
                MaxHoldMinutes:   6, // max hold 6 menit jika tidak ada profit (scalping ketat)
                MinProfitForHold: 3, // butuh min 3% profit untuk hold lebih dari 6 menit
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
                // Selektif maksimal — hanya token dengan confidence tinggi
                cfg.MinScore = 80
                cfg.MinBuyRatio = 0.68
                cfg.MinVolumeSpike = 2.0
                cfg.MinLiquidityUSD = 25000
                cfg.MinAgeMinutes = 5
                cfg.MaxAgeMinutes = 60
                cfg.MaxPricePump5m = 50
                cfg.TP1Pct = 12
                cfg.TP2Pct = 25
                cfg.StopLossPct = -6
                cfg.TrailingStopPct = 5
                cfg.TrailingActivatePct = 5
                cfg.EmergencyBuyRatio = 0.52
                cfg.SuddenDumpThreshold = -10
                cfg.VolumeDropFraction = 0.35
                cfg.MaxHoldMinutes = 5
                cfg.MaxOpenTrades = 2
                cfg.RiskLevel = "conservative"
        case "aggressive":
                // Lebih banyak peluang, risiko lebih tinggi
                cfg.MinScore = 60
                cfg.MinBuyRatio = 0.58
                cfg.MinVolumeSpike = 1.2
                cfg.MinLiquidityUSD = 10000
                cfg.MinAgeMinutes = 2
                cfg.MaxAgeMinutes = 110
                cfg.MaxPricePump5m = 100
                cfg.TP1Pct = 20
                cfg.TP2Pct = 50
                cfg.StopLossPct = -12
                cfg.TrailingStopPct = 10
                cfg.TrailingActivatePct = 10
                cfg.EmergencyBuyRatio = 0.42
                cfg.SuddenDumpThreshold = -15
                cfg.VolumeDropFraction = 0.45
                cfg.MaxHoldMinutes = 10
                cfg.MaxOpenTrades = 5
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
