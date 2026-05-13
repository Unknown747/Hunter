package main

import (
	"strings"
	"unicode"
)

// Filter applies multi-layer filtering to a TokenInfo.
// Returns true if the token passes all filters (should be kept).
func Filter(t *TokenInfo) bool {
	// Layer 1: Hard filters
	if t.Liquidity < 8000 {
		return false
	}
	if t.Volume24h < 12000 {
		return false
	}
	if t.PairAgeHours > 12 {
		return false
	}

	// Layer 2: Quality filters
	if t.MarketCap > 0 && t.Liquidity/t.MarketCap < 0.03 {
		return false
	}
	totalTxns := t.TxnsBuy + t.TxnsSell
	if totalTxns < 10 {
		return false
	}
	if !isValidSymbol(t.Symbol) || !isValidSymbol(t.Name) {
		return false
	}

	// Layer 3: Anti-rug heuristics
	if t.PriceChange1h < -50 {
		return false
	}
	// Extreme sell dominance: sells > 90% of total
	if totalTxns > 0 {
		sellRatio := float64(t.TxnsSell) / float64(totalTxns)
		if sellRatio > 0.90 {
			return false
		}
	}

	return true
}

func isValidSymbol(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	// Reject symbols that are all non-printable or weird chars
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
