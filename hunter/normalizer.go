package main

import (
	"strconv"
	"time"
)

// Normalize converts a raw DexPair into a TokenInfo.
func Normalize(p *DexPair) *TokenInfo {
	price, _ := strconv.ParseFloat(p.PriceUsd, 64)

	var ageHours float64
	if p.PairCreatedAt > 0 {
		created := time.UnixMilli(p.PairCreatedAt)
		ageHours = time.Since(created).Hours()
	}

	return &TokenInfo{
		PairAddress:   p.PairAddress,
		Name:          p.BaseToken.Name,
		Symbol:        p.BaseToken.Symbol,
		Price:         price,
		Liquidity:     p.Liquidity.Usd,
		Volume24h:     p.Volume.H24,
		TxnsBuy:       p.Txns.H24.Buys,
		TxnsSell:      p.Txns.H24.Sells,
		PairAgeHours:  ageHours,
		PriceChange5m: p.PriceChange.M5,
		PriceChange1h: p.PriceChange.H1,
		MarketCap:     p.FdvUsd,
		UpdatedAt:     time.Now(),
	}
}
