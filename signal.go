package main

import (
        "fmt"
        "time"
)

// DetectSignals compares current TokenInfo against stored state and returns signals.
func DetectSignals(t *TokenInfo, state *TokenState) []Signal {
        var signals []Signal
        now := time.Now()

        // Cooldown: only emit signals every 5 minutes per token
        if now.Before(state.AlertCooldown) {
                return nil
        }

        // 1. NEW LISTING SIGNAL
        // PairAgeHours > 0 wajib — age=0 artinya tidak diketahui, bukan benar-benar baru
        if t.PairAgeHours > 0 && t.PairAgeHours < 0.5 && state.LastScore == 0 {
                signals = append(signals, Signal{
                        Type:        "NEW_LISTING",
                        PairAddress: t.PairAddress,
                        Symbol:      t.Symbol,
                        Score:       t.Score,
                        Detail:      fmt.Sprintf("New pair listed %.0f min ago | Liq: $%.0f", t.PairAgeHours*60, t.Liquidity),
                        Timestamp:   now,
                })
        }

        // 2. MOMENTUM SIGNAL (score jumped > 20 in one cycle)
        if state.LastScore > 0 && t.Score-state.LastScore > 20 {
                signals = append(signals, Signal{
                        Type:        "MOMENTUM",
                        PairAddress: t.PairAddress,
                        Symbol:      t.Symbol,
                        Score:       t.Score,
                        Detail:      fmt.Sprintf("Score jumped +%.1f (%.1f→%.1f)", t.Score-state.LastScore, state.LastScore, t.Score),
                        Timestamp:   now,
                })
        }

        // 3. BREAKOUT SIGNAL (volume up + price up)
        volumeUp := state.LastVolume > 0 && t.Volume24h > state.LastVolume*1.5
        priceUp := t.PriceChange1h > 5
        if volumeUp && priceUp {
                signals = append(signals, Signal{
                        Type:        "BREAKOUT",
                        PairAddress: t.PairAddress,
                        Symbol:      t.Symbol,
                        Score:       t.Score,
                        Detail:      fmt.Sprintf("Vol surge +%.0f%% | Price +%.1f%%", ((t.Volume24h/state.LastVolume)-1)*100, t.PriceChange1h),
                        Timestamp:   now,
                })
        }

        if len(signals) > 0 {
                state.AlertCooldown = now.Add(5 * time.Minute)
        }
        return signals
}
