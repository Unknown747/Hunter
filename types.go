package main

import "time"

// TokenInfo is the normalized internal representation of a pair/token.
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

// TokenState tracks per-token historical data for signal detection.
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

// ScoreSnapshot is a point-in-time record used for momentum detection.
type ScoreSnapshot struct {
	Score     float64
	Volume    float64
	Price     float64
	Timestamp time.Time
}

// Signal represents a detected market event.
type Signal struct {
	Type        string    `json:"type"`
	PairAddress string    `json:"pairAddress"`
	Symbol      string    `json:"symbol"`
	Score       float64   `json:"score"`
	Detail      string    `json:"detail"`
	Timestamp   time.Time `json:"timestamp"`
}

// EngineStats is returned by GET /api/stats.
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

// Position represents an open or closed trade on Base mainnet.
type Position struct {
	ID           string         `json:"id"`
	PairAddress  string         `json:"pairAddress"`
	TokenAddress string         `json:"tokenAddress"`
	Symbol       string         `json:"symbol"`
	EntryPrice   float64        `json:"entryPrice"`
	EntryVolume  float64        `json:"entryVolume"`
	CurrentPrice float64        `json:"currentPrice"`
	SizeUSD      float64        `json:"sizeUSD"`
	RemainingPct float64        `json:"remainingPct"` // 0–100
	EntryTime    time.Time      `json:"entryTime"`
	Status       PositionStatus `json:"status"`
	PnLPercent   float64        `json:"pnlPercent"`
	RealizedUSD  float64        `json:"realizedUSD"`
	ExitReason   string         `json:"exitReason,omitempty"`
	TP1Hit       bool           `json:"tp1Hit"`
	Fills        []Fill         `json:"fills"`
}

// Fill records each buy or partial/full sell action.
type Fill struct {
	Action    string    `json:"action"` // BUY / SELL
	Price     float64   `json:"price"`
	PctOfPos  float64   `json:"pctOfPos"`
	USD       float64   `json:"usd"`
	PnLPct    float64   `json:"pnlPct"`
	Reason    string    `json:"reason"`
	TxHash    string    `json:"txHash,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TradeLog is written when a position is fully closed.
type TradeLog struct {
	ID          string    `json:"id"`
	PairAddress string    `json:"pairAddress"`
	Symbol      string    `json:"symbol"`
	EntryPrice  float64   `json:"entryPrice"`
	ExitPrice   float64   `json:"exitPrice"`
	SizeUSD     float64   `json:"sizeUSD"`
	PnLPercent  float64   `json:"pnlPercent"`
	PnLUSD      float64   `json:"pnlUSD"`
	Duration    string    `json:"duration"`
	ExitReason  string    `json:"exitReason"`
	BuyTxHash   string    `json:"buyTxHash,omitempty"`
	SellTxHash  string    `json:"sellTxHash,omitempty"`
	OpenTime    time.Time `json:"openTime"`
	CloseTime   time.Time `json:"closeTime"`
}

// TradingStats is returned by GET /api/trading-stats.
type TradingStats struct {
	OpenTrades    int     `json:"openTrades"`
	MaxTrades     int     `json:"maxTrades"`
	TotalTrades   int     `json:"totalTrades"`
	WinCount      int     `json:"winCount"`
	LossCount     int     `json:"lossCount"`
	WinRate       float64 `json:"winRate"`
	TotalPnLUSD   float64 `json:"totalPnLUSD"`
	AvgPnLPct     float64 `json:"avgPnLPct"`
	BestTradePct  float64 `json:"bestTradePct"`
	WorstTradePct float64 `json:"worstTradePct"`
}

// StrategyConfig holds all tunable parameters.
type StrategyConfig struct {
	MinScore        float64
	MinBuyRatio     float64
	MinVolumeSpike  float64
	MinLiquidityUSD float64
	MinAgeMinutes   float64
	MaxAgeMinutes   float64
	MaxPricePump5m  float64
	TP1Pct          float64
	TP1SellFrac     float64
	TP2Pct          float64
	StopLossPct     float64
	EmergencyBuyRatio   float64
	SuddenDumpThreshold float64
	VolumeDropFraction  float64
	MaxHoldMinutes      float64
	MinProfitForHold    float64
	TradeSizeUSD        float64
	MaxOpenTrades       int
}

// DefaultConfig returns the EARLY_MOMENTUM_SCALP strategy.
func DefaultConfig() *StrategyConfig {
	return &StrategyConfig{
		MinScore:        75,
		MinBuyRatio:     0.65,
		MinVolumeSpike:  2.0,
		MinLiquidityUSD: 15000,
		MinAgeMinutes:   5,
		MaxAgeMinutes:   90,
		MaxPricePump5m:  120,
		TP1Pct:          12,
		TP1SellFrac:     0.5,
		TP2Pct:          25,
		StopLossPct:     -10,
		EmergencyBuyRatio:   0.50,
		SuddenDumpThreshold: -15,
		VolumeDropFraction:  0.5,
		MaxHoldMinutes:      8,
		MinProfitForHold:    5,
		TradeSizeUSD:        1.0,
		MaxOpenTrades:       3,
	}
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
