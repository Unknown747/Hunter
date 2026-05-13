package main

import "time"

// TokenInfo is the normalized internal representation of a pair/token.
type TokenInfo struct {
	PairAddress   string  `json:"pairAddress"`
	Name          string  `json:"name"`
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	Liquidity     float64 `json:"liquidity"`
	Volume24h     float64 `json:"volume24h"`
	TxnsBuy       int     `json:"txnsBuy"`
	TxnsSell      int     `json:"txnsSell"`
	PairAgeHours  float64 `json:"pairAgeHours"`
	PriceChange5m float64 `json:"priceChange5m"`
	PriceChange1h float64 `json:"priceChange1h"`
	MarketCap     float64 `json:"marketCap"`
	Score         float64 `json:"score"`
	Category      string  `json:"category"` // GEM / STRONG / WATCHLIST
	Signals       []string `json:"signals"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TokenState tracks per-token historical data for signal detection.
type TokenState struct {
	LastScore     float64
	LastVolume    float64
	LastPrice     float64
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
	TotalTracked  int     `json:"totalTracked"`
	TotalGems     int     `json:"totalGems"`
	TotalStrong   int     `json:"totalStrong"`
	TotalWatchlist int    `json:"totalWatchlist"`
	TotalSignals  int     `json:"totalSignals"`
	PollInterval  string  `json:"pollInterval"`
	Uptime        string  `json:"uptime"`
	CycleCount    int64   `json:"cycleCount"`
}

// --- DexScreener raw API response structs ---

type DexResponse struct {
	Pairs []DexPair `json:"pairs"`
}

type DexPair struct {
	ChainID     string       `json:"chainId"`
	DexID       string       `json:"dexId"`
	PairAddress string       `json:"pairAddress"`
	BaseToken   DexToken     `json:"baseToken"`
	PriceUsd    string       `json:"priceUsd"`
	Liquidity   DexLiquidity `json:"liquidity"`
	Volume      DexVolume    `json:"volume"`
	Txns        DexTxns      `json:"txns"`
	PriceChange DexChange    `json:"priceChange"`
	FdvUsd      float64      `json:"fdv"`
	PairCreatedAt int64      `json:"pairCreatedAt"`
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
