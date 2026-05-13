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
	positions map[string]*Position
	trades    []*TradeLog
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
func (pm *PositionManager) OnTokenUpdate(t *TokenInfo, state *TokenState) {
	pm.checkExits(t)
	pm.checkEntry(t, state)
}

func (pm *PositionManager) checkEntry(t *TokenInfo, state *TokenState) {
	pm.mu.RLock()
	open := pm.openCount()
	already := pm.hasOpenFor(t.PairAddress)
	pm.mu.RUnlock()

	if already || open >= pm.cfg.MaxOpenTrades {
		return
	}

	result := CheckEntry(t, state, pm.cfg)
	if !result.Allow {
		return
	}

	fill, err := pm.exec.Buy(t, pm.cfg.TradeSizeUSD)
	if err != nil {
		logger.Printf("[trader] BUY failed %s: %v", t.Symbol, err)
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
		Fills:        []Fill{fill},
	}

	pm.mu.Lock()
	pm.positions[pos.ID] = pos
	pm.mu.Unlock()

	logger.Printf("[trader] ✅ OPEN %s @ $%.6f | score=%.1f | %s | tx=%s",
		t.Symbol, fill.Price, t.Score, result.Reason, fill.TxHash)
}

func (pm *PositionManager) checkExits(t *TokenInfo) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, pos := range pm.positions {
		if pos.Status != PositionOpen || pos.PairAddress != t.PairAddress {
			continue
		}

		pos.CurrentPrice = t.Price
		pos.PnLPercent = (t.Price/pos.EntryPrice - 1) * 100

		exit := CheckExit(pos, t, pm.cfg)
		if !exit.ShouldExit {
			continue
		}

		fill, err := pm.exec.Sell(t, pos, exit.Fraction, exit.Reason)
		if err != nil {
			logger.Printf("[trader] SELL failed %s: %v", t.Symbol, err)
			continue
		}
		fill.Reason = exit.Reason
		pos.Fills = append(pos.Fills, fill)

		realizedThisFill := fill.USD - (pos.SizeUSD * exit.Fraction * (pos.RemainingPct / 100))
		pos.RealizedUSD += realizedThisFill

		closedFrac := exit.Fraction * (pos.RemainingPct / 100)
		pos.RemainingPct -= closedFrac * 100

		if exit.Fraction >= 1.0 || pos.RemainingPct <= 0.01 {
			pos.RemainingPct = 0
			pos.Status = PositionClosed
			pos.ExitReason = exit.Reason

			buyTx, sellTx := "", ""
			for _, f := range pos.Fills {
				if f.Action == "BUY" && buyTx == "" {
					buyTx = f.TxHash
				}
				if f.Action == "SELL" {
					sellTx = f.TxHash
				}
			}

			log := &TradeLog{
				ID:          pos.ID,
				PairAddress: pos.PairAddress,
				Symbol:      pos.Symbol,
				EntryPrice:  pos.EntryPrice,
				ExitPrice:   fill.Price,
				SizeUSD:     pos.SizeUSD,
				PnLPercent:  pos.PnLPercent,
				PnLUSD:      pos.RealizedUSD,
				Duration:    fmtDuration(time.Since(pos.EntryTime)),
				ExitReason:  pos.ExitReason,
				BuyTxHash:   buyTx,
				SellTxHash:  sellTx,
				OpenTime:    pos.EntryTime,
				CloseTime:   time.Now(),
			}
			pm.trades = append([]*TradeLog{log}, pm.trades...)
			if len(pm.trades) > maxTradeLog {
				pm.trades = pm.trades[:maxTradeLog]
			}

			mark := "🔴"
			if pos.PnLPercent > 0 {
				mark = "🟢"
			}
			logger.Printf("[trader] %s CLOSE %s @ $%.6f | pnl=%.2f%% | %s | tx=%s",
				mark, t.Symbol, fill.Price, pos.PnLPercent, exit.Reason, fill.TxHash)

		} else {
			if exit.Fraction == pm.cfg.TP1SellFrac {
				pos.TP1Hit = true
			}
			logger.Printf("[trader] ✂️  PARTIAL %s | sold %.0f%% @ $%.6f | pnl=%.2f%% | %s | tx=%s",
				t.Symbol, exit.Fraction*100, fill.Price, pos.PnLPercent, exit.Reason, fill.TxHash)
		}
	}
}

// AllPositions returns all positions, newest first.
func (pm *PositionManager) AllPositions() []*Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	list := make([]*Position, 0, len(pm.positions))
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
		OpenTrades:    pm.openCount(),
		MaxTrades:     pm.cfg.MaxOpenTrades,
		TotalTrades:   len(pm.trades),
		BestTradePct:  0,
		WorstTradePct: 0,
	}
	first := true
	for _, t := range pm.trades {
		st.TotalPnLUSD += t.PnLUSD
		st.AvgPnLPct += t.PnLPercent
		if t.PnLPercent > 0 {
			st.WinCount++
		} else {
			st.LossCount++
		}
		if first || t.PnLPercent > st.BestTradePct {
			st.BestTradePct = t.PnLPercent
		}
		if first || t.PnLPercent < st.WorstTradePct {
			st.WorstTradePct = t.PnLPercent
		}
		first = false
	}
	if st.TotalTrades > 0 {
		st.WinRate = float64(st.WinCount) / float64(st.TotalTrades) * 100
		st.AvgPnLPct = st.AvgPnLPct / float64(st.TotalTrades)
	}
	return st
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (pm *PositionManager) openCount() int {
	n := 0
	for _, p := range pm.positions {
		if p.Status == PositionOpen {
			n++
		}
	}
	return n
}

func (pm *PositionManager) hasOpenFor(pairAddress string) bool {
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
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm %ds", m, s)
}
