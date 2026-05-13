package main

// Score computes a 0–100 score for a TokenInfo using weighted factors.
func Score(t *TokenInfo) float64 {
        var score float64

        // --- Early Age Factor (25%) ---
        // Max points if age < 30 min, scales down to 0 at 12h
        ageFactor := 0.0
        if t.PairAgeHours <= 12 {
                ageFactor = 1.0 - (t.PairAgeHours / 12.0)
        }
        score += ageFactor * 25.0

        // --- Volume Acceleration (25%) ---
        // Normalize volume: cap at 500k for max score
        volFactor := clamp(t.Volume24h/500_000, 0, 1)
        score += volFactor * 25.0

        // --- Buy Pressure (20%) ---
        totalTxns := t.TxnsBuy + t.TxnsSell
        buyRatio := 0.5
        if totalTxns > 0 {
                buyRatio = float64(t.TxnsBuy) / float64(totalTxns)
        }
        score += buyRatio * 20.0

        // --- Liquidity Depth (15%) ---
        // Normalize: cap at 200k for max score
        liqFactor := clamp(t.Liquidity/200_000, 0, 1)
        score += liqFactor * 15.0

        // --- Price Trend (15%) ---
        // 1h price change: +10% → full points, -10% → 0 points
        priceFactor := clamp((t.PriceChange1h+10)/20, 0, 1)
        score += priceFactor * 15.0

        // --- Dynamic Boosts ---
        // Hanya beri bonus jika umur DIKETAHUI dan benar-benar < 1 jam
        // PairAgeHours == 0 artinya pairCreatedAt tidak tersedia, bukan benar-benar baru
        if t.PairAgeHours > 0 && t.PairAgeHours < 1 {
                score += 15.0
        }
        // Volume spike proxy: use 5-min price change as signal of acceleration
        if t.PriceChange5m > 10 {
                score += 10.0
        }
        if buyRatio > 0.70 {
                score += 10.0
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
