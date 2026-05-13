package main

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

const maxTradeLog = 500

// PositionManager tracks all open and closed positions.
type PositionManager struct {
	mu        sync.RWMutex
	positions map[string]*Position // positionID → Position
	trades    []*TradeLog          // completed trade history (newest first)
	cfg       *StrategyConfig
	exec      Executor
}

func NewPositionManager(cfg *StrategyConfig, exec Executor) *PositionManager {
	return &PositionManager{
		positions: make(map[string]*Position),
		trades:    make([]*TradeLog, 0, maxTradeLog),
		cfg:       cfg,
		exec:      exec,
	}
}

// OnTokenUpdate is called from the pipeline for every token update.
// It evaluates entry conditions for new positions and exit conditions for open ones.
func (pm *PositionManager) OnTokenUpdate(t *TokenInfo, state *TokenState) {
	// 1. Check exits on any open position in this token
	pm.checkExits(t)

	// 2. Check entry — only if below max open trades
	pm.checkEntry(t, state)
}

func (pm *PositionManager) checkEntry(t *TokenInfo, state *TokenState) {
	pm.mu.RLock()
	openCount := pm.openCount()
	alreadyOpen := pm.hasOpenPositionFor(t.PairAddress)
	pm.mu.RUnlock()

	if alreadyOpen {
		return
	}
	if openCount >= pm.cfg.MaxOpenTrades {
		return
	}

	result := CheckEntry(t, state, pm.cfg)
	if !result.Allow {
		return
	}

	// Execute buy
	fill, err := pm.exec.Buy(t, pm.cfg.TradeSizeUSD)
	if err != nil {
		logger.Printf("[trader] BUY failed for %s: %v", t.Symbol, err)
		return
	}

	pos := &Position{
		ID:           newID(),
		PairAddress:  t.PairAddress,
		TokenAddress: t.TokenAddress,
		Symbol:       t.Symbol,
		EntryPrice:   fill.Price,
		EntryVolume:  t.Volume24h,
		CurrentPrice: fill.Price,
		SizeUSD:      pm.cfg.TradeSizeUSD,
		RemainingPct: 100,
		EntryTime:    time.Now(),
		Status:       PositionOpen,
		Mode:         pm.exec.Mode(),
		Fills:        []Fill{fill},
	}

	pm.mu.Lock()
	pm.positions[pos.ID] = pos
	pm.mu.Unlock()

	logger.Printf("[trader] ✅ OPEN %s | %s @ $%.6f | score=%.1f | reason: %s",
		pm.exec.Mode(), t.Symbol, fill.Price, t.Score, result.Reason)
}

func (pm *PositionManager) checkExits(t *TokenInfo) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, pos := range pm.positions {
		if pos.Status != PositionOpen || pos.PairAddress != t.PairAddress {
			continue
		}

		// Update current price & P&L
		pos.CurrentPrice = t.Price
		pos.PnLPercent = (t.Price/pos.EntryPrice - 1) * 100

		exit := CheckExit(pos, t, pm.cfg)
		if !exit.ShouldExit {
			continue
		}

		fill, err := pm.exec.Sell(t, pos, exit.Fraction, exit.Reason)
		if err != nil {
			logger.Printf("[trader] SELL failed for %s: %v", t.Symbol, err)
			continue
		}
		fill.Reason = exit.Reason
		pos.Fills = append(pos.Fills, fill)

		// Track realized P&L (USD returned from partial sell)
		realizedThisFill := fill.USD - (pos.SizeUSD * exit.Fraction * (pos.RemainingPct / 100))
		pos.RealizedUSD += realizedThisFill

		// Update remaining
		closedFrac := exit.Fraction * (pos.RemainingPct / 100)
		pos.RemainingPct -= closedFrac * 100

		if exit.Fraction >= 1.0 || pos.RemainingPct <= 0.01 {
			// Full close
			pos.RemainingPct = 0
			pos.Status = PositionClosed
			pos.ExitReason = exit.Reason

			duration := time.Since(pos.EntryTime)
			log := &TradeLog{
				ID:          pos.ID,
				PairAddress: pos.PairAddress,
				Symbol:      pos.Symbol,
				EntryPrice:  pos.EntryPrice,
				ExitPrice:   fill.Price,
				SizeUSD:     pos.SizeUSD,
				PnLPercent:  pos.PnLPercent,
				PnLUSD:      pos.RealizedUSD,
				Duration:    fmtDuration(duration),
				ExitReason:  pos.ExitReason,
				Mode:        pos.Mode,
				OpenTime:    pos.EntryTime,
				CloseTime:   time.Now(),
			}

			pm.trades = append([]*TradeLog{log}, pm.trades...)
			if len(pm.trades) > maxTradeLog {
				pm.trades = pm.trades[:maxTradeLog]
			}

			emoji := "🔴"
			if pos.PnLPercent > 0 {
				emoji = "🟢"
			}
			logger.Printf("[trader] %s CLOSE %s | %s @ $%.6f | PnL: %.2f%% | reason: %s",
				emoji, pm.exec.Mode(), pos.Symbol, fill.Price, pos.PnLPercent, exit.Reason)

		} else {
			// Partial close
			if exit.Fraction == pm.cfg.TP1SellFrac {
				pos.TP1Hit = true
			}
			logger.Printf("[trader] ✂️  PARTIAL %s | %s | sold %.0f%% @ $%.6f | PnL: %.2f%% | reason: %s",
				pm.exec.Mode(), pos.Symbol, exit.Fraction*100, fill.Price, pos.PnLPercent, exit.Reason)
		}
	}
}

// OpenPositions returns all currently open positions, newest first.
func (pm *PositionManager) OpenPositions() []*Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var list []*Position
	for _, p := range pm.positions {
		if p.Status == PositionOpen {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].EntryTime.After(list[j].EntryTime)
	})
	return list
}

// AllPositions returns all positions (open + closed), newest first.
func (pm *PositionManager) AllPositions() []*Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var list []*Position
	for _, p := range pm.positions {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].EntryTime.After(list[j].EntryTime)
	})
	return list
}

// ClosedTrades returns the completed trade log.
func (pm *PositionManager) ClosedTrades() []*TradeLog {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	cp := make([]*TradeLog, len(pm.trades))
	copy(cp, pm.trades)
	return cp
}

// Stats returns aggregated trading statistics.
func (pm *PositionManager) Stats() TradingStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	st := TradingStats{
		Mode:      pm.exec.Mode(),
		OpenTrades: pm.openCount(),
		MaxTrades:  pm.cfg.MaxOpenTrades,
		TotalTrades: len(pm.trades),
		BestTradePct:  -999,
		WorstTradePct: 999,
	}
	for _, t := range pm.trades {
		st.TotalPnLUSD += t.PnLUSD
		st.TotalPnLPct += t.PnLPercent
		if t.PnLPercent > 0 {
			st.WinCount++
		} else {
			st.LossCount++
		}
		if t.PnLPercent > st.BestTradePct {
			st.BestTradePct = t.PnLPercent
		}
		if t.PnLPercent < st.WorstTradePct {
			st.WorstTradePct = t.PnLPercent
		}
	}
	if st.TotalTrades > 0 {
		st.WinRate = float64(st.WinCount) / float64(st.TotalTrades) * 100
		st.TotalPnLPct = st.TotalPnLPct / float64(st.TotalTrades)
	}
	if st.BestTradePct == -999 {
		st.BestTradePct = 0
	}
	if st.WorstTradePct == 999 {
		st.WorstTradePct = 0
	}
	return st
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (pm *PositionManager) openCount() int {
	count := 0
	for _, p := range pm.positions {
		if p.Status == PositionOpen {
			count++
		}
	}
	return count
}

func (pm *PositionManager) hasOpenPositionFor(pairAddress string) bool {
	for _, p := range pm.positions {
		if p.PairAddress == pairAddress && p.Status == PositionOpen {
			return true
		}
	}
	return false
}

func newID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%.0fm %.0fs", d.Minutes(), d.Seconds()-d.Minutes()*60)
}
