package main

import (
	"fmt"
	"time"
)

// DetectSignals compares current TokenInfo against stored state and returns signals.
func DetectSignals(t *TokenInfo, state *TokenState) []Signal {
	var signals []Signal
	now := time.Now()

	// Cooldown: emit sinyal max setiap 3 menit per token (lebih responsif)
	if now.Before(state.AlertCooldown) {
		return nil
	}

	totalTxns := t.TxnsBuy + t.TxnsSell
	buyRatio := 0.5
	if totalTxns > 0 {
		buyRatio = float64(t.TxnsBuy) / float64(totalTxns)
	}

	// ── 1. NEW_LISTING SIGNAL ─────────────────────────────────────────────────
	// Token baru terdeteksi pertama kali, umur < 30 menit
	if t.PairAgeHours > 0 && t.PairAgeHours < 0.5 && state.LastScore == 0 {
		signals = append(signals, Signal{
			Type:        "NEW_LISTING",
			PairAddress: t.PairAddress,
			Symbol:      t.Symbol,
			Score:       t.Score,
			Detail: fmt.Sprintf("🆕 Listing baru %.0f menit | Liq: $%.0f | Buy%%: %.0f%%",
				t.PairAgeHours*60, t.Liquidity, buyRatio*100),
			Timestamp: now,
		})
	}

	// ── 2. EARLY_GEM SIGNAL ───────────────────────────────────────────────────
	// Token muda (< 20 menit) dengan score bagus dan buy pressure kuat
	// Ini adalah sinyal paling berharga untuk scalping early entry
	if t.PairAgeHours > 0 && t.PairAgeHours < 0.33 &&
		t.Score >= 70 && buyRatio >= 0.65 && state.LastScore == 0 {
		signals = append(signals, Signal{
			Type:        "EARLY_GEM",
			PairAddress: t.PairAddress,
			Symbol:      t.Symbol,
			Score:       t.Score,
			Detail: fmt.Sprintf("⚡ Early gem %.0f menit | Score: %.0f | Buy%%: %.0f%% | Liq: $%.0f",
				t.PairAgeHours*60, t.Score, buyRatio*100, t.Liquidity),
			Timestamp: now,
		})
	}

	// ── 3. MOMENTUM SIGNAL ────────────────────────────────────────────────────
	// Score naik > 15 poin dalam satu siklus (lebih sensitif dari sebelumnya)
	if state.LastScore > 0 && t.Score-state.LastScore > 15 {
		signals = append(signals, Signal{
			Type:        "MOMENTUM",
			PairAddress: t.PairAddress,
			Symbol:      t.Symbol,
			Score:       t.Score,
			Detail: fmt.Sprintf("📈 Score naik +%.1f (%.1f→%.1f) | Buy%%: %.0f%%",
				t.Score-state.LastScore, state.LastScore, t.Score, buyRatio*100),
			Timestamp: now,
		})
	}

	// ── 4. BREAKOUT SIGNAL ────────────────────────────────────────────────────
	// Volume naik signifikan + harga naik = potensi breakout nyata
	// Threshold lebih ketat: volume 2x (dari 1.5x) dan harga +8% dalam 1h
	volumeUp := state.LastVolume > 0 && t.Volume24h > state.LastVolume*2.0
	priceUp := t.PriceChange1h > 8 && t.PriceChange5m > 5
	if volumeUp && priceUp {
		signals = append(signals, Signal{
			Type:        "BREAKOUT",
			PairAddress: t.PairAddress,
			Symbol:      t.Symbol,
			Score:       t.Score,
			Detail: fmt.Sprintf("🚀 Vol surge +%.0f%% | 1h: +%.1f%% | 5m: +%.1f%%",
				((t.Volume24h/state.LastVolume)-1)*100, t.PriceChange1h, t.PriceChange5m),
			Timestamp: now,
		})
	}

	// ── 5. SELL_PRESSURE SIGNAL ───────────────────────────────────────────────
	// Peringatan dini: buy ratio mulai turun drastis
	// Berguna untuk exit sebelum terlambat
	if state.LastScore > 0 && buyRatio < 0.40 && t.PriceChange5m < -5 {
		signals = append(signals, Signal{
			Type:        "SELL_PRESSURE",
			PairAddress: t.PairAddress,
			Symbol:      t.Symbol,
			Score:       t.Score,
			Detail: fmt.Sprintf("⚠️ Sell pressure | Buy%%: %.0f%% | 5m: %.1f%%",
				buyRatio*100, t.PriceChange5m),
			Timestamp: now,
		})
	}

	if len(signals) > 0 {
		state.AlertCooldown = now.Add(3 * time.Minute)
	}
	return signals
}
