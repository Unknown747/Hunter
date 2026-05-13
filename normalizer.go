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

	totalTxns := p.Txns.H24.Buys + p.Txns.H24.Sells
	buyRatio := 0.5
	if totalTxns > 0 {
		buyRatio = float64(p.Txns.H24.Buys) / float64(totalTxns)
	}

	return &TokenInfo{
		PairAddress:   p.PairAddress,
		TokenAddress:  p.BaseToken.Address,
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
		BuyRatio:      buyRatio,
		// VolumeSpike is set later in the pipeline when we have a prior state
		UpdatedAt: time.Now(),
	}
}
