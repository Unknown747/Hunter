package main

import "time"

// TokenInfo adalah representasi internal yang sudah dinormalisasi dari sebuah pair/token.
type TokenInfo struct {
	PairAddress   string    `json:"pairAddress"`
	TokenAddress  string    `json:"tokenAddress"`
	Name          string    `json:"name"`
	Symbol        string    `json:"symbol"`
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
	HighWaterMark  float64        `json:"highWaterMark"`  // trailing stop — harga tertinggi yang pernah dicapai
	GasCostUSD     float64        `json:"gasCostUSD"`     // akumulasi biaya gas
	Fills          []Fill         `json:"fills"`
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
	Positions map[string]*Position      `json:"positions"`
	Trades    []*TradeLog               `json:"trades"`
	Blacklist map[string]*BlacklistEntry `json:"blacklist"`
	SavedAt   time.Time                 `json:"savedAt"`
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
func DefaultConfig() *StrategyConfig {
	return &StrategyConfig{
		MinScore:            75,
		MinBuyRatio:         0.65,
		MinVolumeSpike:      2.0,
		MinLiquidityUSD:     15000,
		MinAgeMinutes:       5,
		MaxAgeMinutes:       90,
		MaxPricePump5m:      120,
		TP1Pct:              12,
		TP1SellFrac:         0.5,
		TP2Pct:              25,
		StopLossPct:         -10,
		TrailingStopPct:     8,
		TrailingActivatePct: 8,
		EmergencyBuyRatio:   0.50,
		SuddenDumpThreshold: -15,
		VolumeDropFraction:  0.5,
		MaxHoldMinutes:      8,
		MinProfitForHold:    5,
		TradeSizeUSD:        1.0,
		MaxOpenTrades:       3,
		RiskLevel:           "normal",
	}
}

// ConfigForRisk mengembalikan konfigurasi sesuai level risiko.
// RISK_LEVEL=conservative | normal | aggressive
func ConfigForRisk(level string) *StrategyConfig {
	cfg := DefaultConfig()
	switch level {
	case "conservative":
		cfg.MinScore = 85
		cfg.MinBuyRatio = 0.70
		cfg.MinLiquidityUSD = 30000
		cfg.MaxAgeMinutes = 60
		cfg.StopLossPct = -7
		cfg.TrailingStopPct = 5
		cfg.TrailingActivatePct = 5
		cfg.EmergencyBuyRatio = 0.55
		cfg.MaxOpenTrades = 2
		cfg.RiskLevel = "conservative"
	case "aggressive":
		cfg.MinScore = 65
		cfg.MinBuyRatio = 0.60
		cfg.MinLiquidityUSD = 10000
		cfg.MaxAgeMinutes = 120
		cfg.MaxPricePump5m = 150
		cfg.StopLossPct = -15
		cfg.TrailingStopPct = 12
		cfg.TrailingActivatePct = 12
		cfg.EmergencyBuyRatio = 0.45
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
