package main

// Score computes a 0–100 score for a TokenInfo using weighted factors.
// Dioptimalkan untuk meme coin listing baru (< 2 jam).
func Score(t *TokenInfo) float64 {
	var score float64

	totalTxns := t.TxnsBuy + t.TxnsSell
	buyRatio := 0.5
	if totalTxns > 0 {
		buyRatio = float64(t.TxnsBuy) / float64(totalTxns)
	}

	// ── Early Age Factor (25%) ────────────────────────────────────────────────
	// Kurva lebih curam: maks di < 15 menit, nol di 2 jam
	// Token listing baru dapat keuntungan besar dari faktor ini
	ageFactor := 0.0
	if t.PairAgeHours > 0 && t.PairAgeHours <= 2 {
		ageFactor = 1.0 - (t.PairAgeHours / 2.0)
	}
	score += ageFactor * 25.0

	// ── Volume Acceleration (20%) ─────────────────────────────────────────────
	// Cap di 200k — lebih realistis untuk token baru listing
	volFactor := clamp(t.Volume24h/200_000, 0, 1)
	score += volFactor * 20.0

	// ── Buy Pressure (20%) ───────────────────────────────────────────────────
	score += buyRatio * 20.0

	// ── Volume / Liquidity Ratio (15%) ────────────────────────────────────────
	// Rasio tinggi = banyak aktivitas trading relatif terhadap pool size
	// Ideal untuk meme coin yang sedang panas
	volLiqRatio := 0.0
	if t.Liquidity > 0 {
		volLiqRatio = clamp(t.Volume24h/t.Liquidity/5.0, 0, 1) // maks di rasio 5x
	}
	score += volLiqRatio * 15.0

	// ── Price Trend (10%) ─────────────────────────────────────────────────────
	// 5m change lebih relevan untuk scalping daripada 1h
	// +20% dalam 5m = full points, -10% = 0 poin
	priceFactor5m := clamp((t.PriceChange5m+10)/30, 0, 1)
	score += priceFactor5m * 10.0

	// ── Transaction Activity (5%) ─────────────────────────────────────────────
	// Minimal 200 txn = full score, mendorong token dengan aktivitas nyata
	txnFactor := clamp(float64(totalTxns)/200.0, 0, 1)
	score += txnFactor * 5.0

	// ── Dynamic Boosts ────────────────────────────────────────────────────────

	// Bonus umur sangat muda (< 30 menit, umur diketahui)
	if t.PairAgeHours > 0 && t.PairAgeHours < 0.5 {
		score += 12.0
	}

	// Bonus tekanan beli sangat kuat
	if buyRatio > 0.70 {
		score += 8.0
	}
	if buyRatio > 0.80 {
		score += 5.0 // tambahan bonus jika luar biasa kuat
	}

	// Bonus momentum harga 5m positif kuat
	if t.PriceChange5m > 15 {
		score += 8.0
	}

	// Bonus volume/liq ratio sangat tinggi (hot pair)
	if t.Liquidity > 0 && t.Volume24h/t.Liquidity > 3 {
		score += 5.0
	}

	// Penalty: harga sudah pump terlalu jauh dalam 5m (hindari top-buy)
	if t.PriceChange5m > 60 {
		score -= 10.0
	}

	// Penalty: liquidity sangat rendah → slippage tinggi
	if t.Liquidity < 15_000 {
		score -= 5.0
	}

	return clamp(score, 0, 100)
}

// Categorize assigns a category string based on score.
func Categorize(score float64) string {
	switch {
	case score >= 75:
		return "GEM"
	case score >= 50:
		return "STRONG"
	default:
		return "WATCHLIST"
	}
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
