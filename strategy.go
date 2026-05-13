package main

import (
        "fmt"
        "time"
)

// EntryResult holds the outcome of an entry condition check.
type EntryResult struct {
        Allow  bool
        Reason string
}

// ExitResult holds the outcome of an exit condition check.
type ExitResult struct {
        ShouldExit bool
        Fraction   float64 // portion of remaining position to close (0–1)
        Reason     string
}

// CheckEntry evaluates all BUY conditions for a token.
// Returns true only when ALL conditions are satisfied.
func CheckEntry(t *TokenInfo, state *TokenState, cfg *StrategyConfig) EntryResult {
        ageMin := t.PairAgeHours * 60

        // ── Anti-rug filter (SKIP_IF) ──────────────────────────────────────────
        if t.Liquidity < 10000 {
                return EntryResult{false, "anti-rug: liq < $10k"}
        }
        // If pairCreatedAt=0, age is unknown — skip to avoid trading old established pairs
        if t.PairAgeHours == 0 {
                return EntryResult{false, "anti-rug: age unknown (no pairCreatedAt)"}
        }
        if ageMin < 2 {
                return EntryResult{false, fmt.Sprintf("anti-rug: too new %.1fmin", ageMin)}
        }
        if t.PriceChange1h <= -60 {
                return EntryResult{false, fmt.Sprintf("anti-rug: price dump %.1f%%", t.PriceChange1h)}
        }
        if t.Symbol == "" || t.Name == "" {
                return EntryResult{false, "anti-rug: missing name/symbol"}
        }

        // ── Core score ─────────────────────────────────────────────────────────
        if t.Score < cfg.MinScore {
                return EntryResult{false, fmt.Sprintf("score %.1f < %.0f", t.Score, cfg.MinScore)}
        }

        // ── Buy pressure ───────────────────────────────────────────────────────
        if t.BuyRatio < cfg.MinBuyRatio {
                return EntryResult{false, fmt.Sprintf("buyRatio %.2f < %.2f", t.BuyRatio, cfg.MinBuyRatio)}
        }

        // ── Volume spike (requires at least one historical data point) ─────────
        if state.LastVolume <= 0 {
                return EntryResult{false, "waiting for volume history"}
        }
        spike := t.Volume24h / state.LastVolume
        if spike < cfg.MinVolumeSpike {
                return EntryResult{false, fmt.Sprintf("volSpike %.2fx < %.1fx", spike, cfg.MinVolumeSpike)}
        }

        // ── Liquidity ──────────────────────────────────────────────────────────
        if t.Liquidity < cfg.MinLiquidityUSD {
                return EntryResult{false, fmt.Sprintf("liq $%.0f < $%.0f", t.Liquidity, cfg.MinLiquidityUSD)}
        }

        // ── Age sweet spot ─────────────────────────────────────────────────────
        if ageMin < cfg.MinAgeMinutes {
                return EntryResult{false, fmt.Sprintf("too new: %.1fmin", ageMin)}
        }
        if ageMin > cfg.MaxAgeMinutes {
                return EntryResult{false, fmt.Sprintf("too old: %.1fmin", ageMin)}
        }

        // ── Avoid chasing pumped tokens ────────────────────────────────────────
        if t.PriceChange5m > cfg.MaxPricePump5m {
                return EntryResult{false, fmt.Sprintf("already pumped +%.1f%%", t.PriceChange5m)}
        }

        return EntryResult{true, fmt.Sprintf("score=%.1f buyR=%.2f spike=%.1fx age=%.0fm liq=$%.0f",
                t.Score, t.BuyRatio, spike, ageMin, t.Liquidity)}
}

// CheckExit evaluates all SELL conditions for an open position.
func CheckExit(p *Position, t *TokenInfo, cfg *StrategyConfig) ExitResult {
        if t.Price <= 0 || p.EntryPrice <= 0 {
                return ExitResult{}
        }

        pnlPct := (t.Price/p.EntryPrice - 1) * 100
        holdMin := time.Since(p.EntryTime).Minutes()

        // ── Emergency exits (always full close) ───────────────────────────────
        if t.BuyRatio < cfg.EmergencyBuyRatio {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: buyRatio %.2f (collapse)", t.BuyRatio)}
        }
        if t.PriceChange5m <= cfg.SuddenDumpThreshold {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: sudden dump %.1f%%", t.PriceChange5m)}
        }
        if p.EntryVolume > 0 && t.Volume24h < p.EntryVolume*cfg.VolumeDropFraction {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: volume dropped %.0f%%", (1-t.Volume24h/p.EntryVolume)*100)}
        }

        // ── Stop loss ──────────────────────────────────────────────────────────
        if pnlPct <= cfg.StopLossPct {
                return ExitResult{true, 1.0, fmt.Sprintf("STOP LOSS %.1f%%", pnlPct)}
        }

        // ── Time exit (no momentum) ────────────────────────────────────────────
        if holdMin >= cfg.MaxHoldMinutes && pnlPct < cfg.MinProfitForHold {
                return ExitResult{true, 1.0, fmt.Sprintf("TIME EXIT: %.0fmin hold, %.1f%% PnL", holdMin, pnlPct)}
        }

        // ── Take profit (multi-level) ──────────────────────────────────────────
        if !p.TP1Hit && pnlPct >= cfg.TP1Pct {
                return ExitResult{true, cfg.TP1SellFrac, fmt.Sprintf("TP1 +%.1f%% → sell %.0f%%", pnlPct, cfg.TP1SellFrac*100)}
        }
        if p.TP1Hit && pnlPct >= cfg.TP2Pct {
                return ExitResult{true, 1.0, fmt.Sprintf("TP2 +%.1f%% → close position", pnlPct)}
        }

        return ExitResult{}
}
