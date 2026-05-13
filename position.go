package main

import (
        "crypto/rand"
        "encoding/hex"
        "fmt"
        "math"
        "sort"
        "strings"
        "sync"
        "time"
)

const maxTradeLog = 500

// PositionManager melacak semua posisi terbuka dan tertutup.
type PositionManager struct {
        mu        sync.RWMutex
        positions map[string]*Position
        trades    []*TradeLog
        cfg       *StrategyConfig
        exec      Executor
        bl        *Blacklist
        rugStore  *RugPatternStore
}

func NewPositionManager(cfg *StrategyConfig, exec Executor, bl *Blacklist, rugStore *RugPatternStore) *PositionManager {
        return &PositionManager{
                positions: make(map[string]*Position),
                trades:    make([]*TradeLog, 0, maxTradeLog),
                cfg:       cfg,
                exec:      exec,
                bl:        bl,
                rugStore:  rugStore,
        }
}

// OnTokenUpdate dipanggil dari pipeline untuk setiap update token.
func (pm *PositionManager) OnTokenUpdate(t *TokenInfo, state *TokenState) {
        pm.checkExits(t)
        pm.checkEntry(t, state)
}

func (pm *PositionManager) checkEntry(t *TokenInfo, state *TokenState) {
        // Cek blacklist — blokir tanpa logging spam
        if pm.bl.IsBlacklisted(t.PairAddress) {
                return
        }

        // Cek smart rug pattern — blokir jika mirip rug historis
        if isRisky, reason := pm.rugStore.Check(t); isRisky {
                logger.Printf("[rugpattern] 🚫 BLOKIR entry %s: %s", t.Symbol, reason)
                return
        }

        // Cek awal dengan read lock (cepat)
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

        // exec.Buy() adalah operasi jaringan yang lambat — dilakukan di luar lock
        fill, err := pm.exec.Buy(t, pm.cfg.TradeSizeUSD)
        if err != nil {
                logger.Printf("[trader] BUY gagal %s: %v", t.Symbol, err)
                return
        }

        // Re-check di bawah write lock sebelum menyimpan posisi.
        // PENTING: Jika buy sudah berhasil on-chain, SELALU simpan posisinya meskipun
        // ada race condition — jika tidak, token nyangkut di wallet tanpa dicatat bot.
        pm.mu.Lock()
        defer pm.mu.Unlock()

        if pm.hasOpenFor(t.PairAddress) {
                // Posisi duplikat — aneh tapi tetap catat agar token tidak hilang
                logger.Printf("[trader] ⚠️  DUPLIKAT terdeteksi setelah buy %s — tetap mencatat posisi (tx=%s)",
                        t.Symbol, fill.TxHash)
        } else if pm.openCount() >= pm.cfg.MaxOpenTrades {
                // Melewati batas maks — tetap catat, jangan biarkan token orphan
                logger.Printf("[trader] ⚠️  Melebihi batas posisi setelah buy %s — tetap mencatat (tx=%s)",
                        t.Symbol, fill.TxHash)
        }

        pos := &Position{
                ID:            newID(),
                PairAddress:   t.PairAddress,
                TokenAddress:  t.TokenAddress,
                Symbol:        t.Symbol,
                EntryPrice:    fill.Price,
                EntryVolume:   t.Volume24h,
                CurrentPrice:  fill.Price,
                HighWaterMark: fill.Price,
                SizeUSD:       pm.cfg.TradeSizeUSD,
                RemainingPct:  100,
                EntryTime:     time.Now(),
                Status:        PositionOpen,
                GasCostUSD:    fill.GasUSD,
                Fills:         []Fill{fill},
                // Snapshot entry untuk smart rug pattern detection
                EntryAgeMinutes:  t.PairAgeHours * 60,
                EntryBuyRatio:    t.BuyRatio,
                EntryPricePump5m: t.PriceChange5m,
        }
        pm.positions[pos.ID] = pos

        logger.Printf("[trader] ✅ BUKA %s @ $%.6f | score=%.1f | %s | tx=%s",
                t.Symbol, fill.Price, t.Score, result.Reason, fill.TxHash)
        tg.NotifyEntry(pos, t, pm.cfg)
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
                        logger.Printf("[trader] SELL gagal %s: %v", t.Symbol, err)
                        continue
                }
                fill.Reason = exit.Reason
                pos.Fills = append(pos.Fills, fill)
                pos.GasCostUSD += fill.GasUSD

                realizedThisFill := fill.USD - (pos.SizeUSD * exit.Fraction * (pos.RemainingPct / 100))
                pos.RealizedUSD += realizedThisFill

                closedFrac := exit.Fraction * (pos.RemainingPct / 100)
                pos.RemainingPct -= closedFrac * 100

                if exit.Fraction >= 1.0 || pos.RemainingPct <= 0.01 {
                        pos.RemainingPct = 0
                        pos.Status = PositionClosed
                        pos.ExitReason = exit.Reason

                        // Catat stop loss di blacklist
                        if strings.Contains(exit.Reason, "STOP LOSS") || strings.Contains(exit.Reason, "TRAILING STOP") {
                                pm.bl.RecordStopLoss(pos.PairAddress, pos.Symbol)
                        }

                        // Simpan rug pattern untuk deteksi token serupa di masa depan
                        if pos.PnLPercent < 0 || strings.Contains(exit.Reason, "EMERGENCY") {
                                pm.rugStore.Record(pos, t, exit.Reason)
                        }

                        buyTx, sellTx := "", ""
                        for _, f := range pos.Fills {
                                if f.Action == "BUY" && buyTx == "" {
                                        buyTx = f.TxHash
                                }
                                if f.Action == "SELL" {
                                        sellTx = f.TxHash
                                }
                        }

                        netPnL := pos.RealizedUSD - pos.GasCostUSD
                        log := &TradeLog{
                                ID:          pos.ID,
                                PairAddress: pos.PairAddress,
                                Symbol:      pos.Symbol,
                                EntryPrice:  pos.EntryPrice,
                                ExitPrice:   fill.Price,
                                SizeUSD:     pos.SizeUSD,
                                PnLPercent:  pos.PnLPercent,
                                PnLUSD:      pos.RealizedUSD,
                                GasCostUSD:  pos.GasCostUSD,
                                NetPnLUSD:   netPnL,
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
                        logger.Printf("[trader] %s TUTUP %s @ $%.6f | pnl=%.2f%% | net=$%.4f | gas=$%.4f | %s",
                                mark, t.Symbol, fill.Price, pos.PnLPercent, netPnL, pos.GasCostUSD, exit.Reason)
                        tg.NotifyExit(pos, fill.Price, exit.Reason)

                } else {
                        // Partial close — tandai TP1
                        isTP1 := math.Abs(exit.Fraction-pm.cfg.TP1SellFrac) < 1e-9
                        if isTP1 {
                                pos.TP1Hit = true
                        }
                        logger.Printf("[trader] ✂️  PARSIAL %s | jual %.0f%% @ $%.6f | pnl=%.2f%% | %s",
                                t.Symbol, exit.Fraction*100, fill.Price, pos.PnLPercent, exit.Reason)
                        if isTP1 {
                                tg.NotifyTP1(pos, fill.Price, pm.cfg)
                        }
                }
        }
}

// CloseAll menutup semua posisi terbuka segera dengan alasan yang diberikan.
// Digunakan oleh endpoint /api/close-all dan sell.sh.
// Mengembalikan jumlah posisi yang berhasil ditutup.
func (pm *PositionManager) CloseAll(reason string) int {
        // Ambil snapshot posisi terbuka di bawah read lock
        pm.mu.RLock()
        var toClose []*Position
        for _, pos := range pm.positions {
                if pos.Status == PositionOpen {
                        toClose = append(toClose, pos)
                }
        }
        pm.mu.RUnlock()

        if len(toClose) == 0 {
                return 0
        }

        closed := 0
        for _, pos := range toClose {
                // Buat TokenInfo minimal dari data posisi yang tersimpan
                t := &TokenInfo{
                        PairAddress:  pos.PairAddress,
                        TokenAddress: pos.TokenAddress,
                        Symbol:       pos.Symbol,
                        Price:        pos.CurrentPrice,
                        Volume24h:    pos.EntryVolume,
                }
                if t.Price <= 0 {
                        t.Price = pos.EntryPrice
                }

                // Jual seluruh posisi — exec.Sell di luar lock
                fill, err := pm.exec.Sell(t, pos, 1.0, reason)
                if err != nil {
                        logger.Printf("[trader] CloseAll SELL gagal %s: %v", pos.Symbol, err)
                        continue
                }

                pm.mu.Lock()
                fill.Reason = reason
                pos.Fills = append(pos.Fills, fill)
                pos.GasCostUSD += fill.GasUSD
                pos.PnLPercent = (fill.Price/pos.EntryPrice - 1) * 100
                // Gunakan cost basis yang benar: hanya sisa posisi yang belum ditutup
                // (misalnya jika TP1 sudah jual 50%, remaining adalah 50% saja)
                pos.RealizedUSD += fill.USD - (pos.SizeUSD * pos.RemainingPct / 100.0)
                pos.Status = PositionClosed
                pos.ExitReason = reason
                pos.RemainingPct = 0

                netPnL := pos.RealizedUSD - pos.GasCostUSD
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
                        GasCostUSD:  pos.GasCostUSD,
                        NetPnLUSD:   netPnL,
                        Duration:    fmtDuration(time.Since(pos.EntryTime)),
                        ExitReason:  reason,
                        BuyTxHash:   buyTx,
                        SellTxHash:  sellTx,
                        OpenTime:    pos.EntryTime,
                        CloseTime:   time.Now(),
                }
                pm.trades = append([]*TradeLog{log}, pm.trades...)
                if len(pm.trades) > maxTradeLog {
                        pm.trades = pm.trades[:maxTradeLog]
                }
                pm.mu.Unlock()

                closed++
                logger.Printf("[trader] 🔴 CLOSE-ALL %s @ $%.6f | pnl=%.2f%% | %s | tx=%s",
                        pos.Symbol, fill.Price, pos.PnLPercent, reason, fill.TxHash)
        }
        return closed
}

// ManualBuy memaksa beli token tertentu tanpa melewati filter strategi.
// Digunakan untuk testing nyata via /api/manual-buy.
func (pm *PositionManager) ManualBuy(t *TokenInfo) (*Position, error) {
        pm.mu.RLock()
        already := pm.hasOpenFor(t.PairAddress)
        pm.mu.RUnlock()

        if already {
                return nil, fmt.Errorf("sudah ada posisi terbuka untuk %s", t.Symbol)
        }

        logger.Printf("[trader] 🛒 MANUAL BUY %s @ $%.6f (pair=%s)", t.Symbol, t.Price, t.PairAddress)

        fill, err := pm.exec.Buy(t, pm.cfg.TradeSizeUSD)
        if err != nil {
                return nil, fmt.Errorf("buy gagal: %w", err)
        }

        pm.mu.Lock()
        defer pm.mu.Unlock()

        pos := &Position{
                ID:            newID(),
                PairAddress:   t.PairAddress,
                TokenAddress:  t.TokenAddress,
                Symbol:        t.Symbol,
                EntryPrice:    fill.Price,
                EntryVolume:   t.Volume24h,
                CurrentPrice:  fill.Price,
                HighWaterMark: fill.Price,
                SizeUSD:       pm.cfg.TradeSizeUSD,
                RemainingPct:  100,
                EntryTime:     time.Now(),
                Status:        PositionOpen,
                GasCostUSD:    fill.GasUSD,
                Fills:         []Fill{fill},
                // Snapshot entry untuk smart rug pattern detection
                EntryAgeMinutes:  t.PairAgeHours * 60,
                EntryBuyRatio:    t.BuyRatio,
                EntryPricePump5m: t.PriceChange5m,
        }
        pm.positions[pos.ID] = pos

        logger.Printf("[trader] ✅ MANUAL BUY berhasil %s | tx=%s | price=$%.6f | gas=$%.4f",
                t.Symbol, fill.TxHash, fill.Price, fill.GasUSD)

        return pos, nil
}

// ManualSell memaksa jual semua posisi terbuka untuk token tertentu.
// Digunakan untuk testing nyata via /api/manual-sell.
func (pm *PositionManager) ManualSell(t *TokenInfo) (Fill, error) {
        pm.mu.RLock()
        var target *Position
        for _, p := range pm.positions {
                if p.PairAddress == t.PairAddress && p.Status == PositionOpen {
                        target = p
                        break
                }
        }
        pm.mu.RUnlock()

        if target == nil {
                return Fill{}, fmt.Errorf("tidak ada posisi terbuka untuk %s", t.Symbol)
        }

        logger.Printf("[trader] 💰 MANUAL SELL %s @ $%.6f (posisi=%s)", t.Symbol, t.Price, target.ID)

        fill, err := pm.exec.Sell(t, target, 1.0, "MANUAL SELL")
        if err != nil {
                return Fill{}, fmt.Errorf("sell gagal: %w", err)
        }

        pm.mu.Lock()
        defer pm.mu.Unlock()

        fill.Reason = "MANUAL SELL"
        target.Fills = append(target.Fills, fill)
        target.GasCostUSD += fill.GasUSD
        target.PnLPercent = (fill.Price/target.EntryPrice - 1) * 100
        target.RealizedUSD += fill.USD - (target.SizeUSD * target.RemainingPct / 100.0)
        target.Status = PositionClosed
        target.ExitReason = "MANUAL SELL"
        target.RemainingPct = 0

        netPnL := target.RealizedUSD - target.GasCostUSD

        buyTx, sellTx := "", ""
        for _, f := range target.Fills {
                if f.Action == "BUY" && buyTx == "" {
                        buyTx = f.TxHash
                }
                if f.Action == "SELL" {
                        sellTx = f.TxHash
                }
        }

        log := &TradeLog{
                ID:          target.ID,
                PairAddress: target.PairAddress,
                Symbol:      target.Symbol,
                EntryPrice:  target.EntryPrice,
                ExitPrice:   fill.Price,
                SizeUSD:     target.SizeUSD,
                PnLPercent:  target.PnLPercent,
                PnLUSD:      target.RealizedUSD,
                GasCostUSD:  target.GasCostUSD,
                NetPnLUSD:   netPnL,
                Duration:    fmtDuration(time.Since(target.EntryTime)),
                ExitReason:  "MANUAL SELL",
                BuyTxHash:   buyTx,
                SellTxHash:  sellTx,
                OpenTime:    target.EntryTime,
                CloseTime:   time.Now(),
        }
        pm.trades = append([]*TradeLog{log}, pm.trades...)
        if len(pm.trades) > maxTradeLog {
                pm.trades = pm.trades[:maxTradeLog]
        }

        mark := "🔴"
        if target.PnLPercent > 0 {
                mark = "🟢"
        }
        logger.Printf("[trader] %s MANUAL SELL selesai %s | pnl=%.2f%% | net=$%.4f | gas=$%.4f | buy_tx=%s | sell_tx=%s",
                mark, t.Symbol, target.PnLPercent, netPnL, target.GasCostUSD, buyTx, sellTx)

        return fill, nil
}

// ForceClose menutup posisi di software TANPA eksekusi on-chain.
// Gunakan untuk posisi yang gagal dijual (pool kosong, CL pool, dll).
func (pm *PositionManager) ForceClose(pairAddr, reason string) error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        var target *Position
        for _, p := range pm.positions {
                if p.PairAddress == pairAddr && p.Status == PositionOpen {
                        target = p
                        break
                }
        }
        if target == nil {
                return fmt.Errorf("posisi terbuka tidak ditemukan untuk pair %s", pairAddr)
        }

        target.Status = PositionClosed
        target.PnLPercent = (target.CurrentPrice/target.EntryPrice - 1) * 100
        // Estimasi realized P&L dari harga terakhir yang diketahui.
        // ForceClose tidak ada transaksi on-chain — hitung dari sisa posisi.
        if target.EntryPrice > 0 && target.CurrentPrice > 0 {
                remainingFrac := target.RemainingPct / 100.0
                saleValue := target.SizeUSD * remainingFrac * (target.CurrentPrice / target.EntryPrice)
                costBasis := target.SizeUSD * remainingFrac
                target.RealizedUSD += saleValue - costBasis
        }
        netPnL := target.RealizedUSD - target.GasCostUSD

        buyTx := ""
        for _, f := range target.Fills {
                if f.Action == "BUY" {
                        buyTx = f.TxHash
                }
        }

        log := &TradeLog{
                ID:          target.ID,
                PairAddress: target.PairAddress,
                Symbol:      target.Symbol,
                EntryPrice:  target.EntryPrice,
                ExitPrice:   target.CurrentPrice,
                SizeUSD:     target.SizeUSD,
                PnLPercent:  target.PnLPercent,
                PnLUSD:      target.RealizedUSD,
                GasCostUSD:  target.GasCostUSD,
                NetPnLUSD:   netPnL,
                Duration:    fmtDuration(time.Since(target.EntryTime)),
                ExitReason:  reason,
                BuyTxHash:   buyTx,
                SellTxHash:  "",
                OpenTime:    target.EntryTime,
                CloseTime:   time.Now(),
        }
        pm.trades = append([]*TradeLog{log}, pm.trades...)
        if len(pm.trades) > maxTradeLog {
                pm.trades = pm.trades[:maxTradeLog]
        }

        logger.Printf("[trader] ⚠️  FORCE CLOSE %s | reason=%s | entry=$%.6f | gas=$%.4f | buy_tx=%s",
                target.Symbol, reason, target.EntryPrice, target.GasCostUSD, buyTx)
        return nil
}

// AllPositions mengembalikan semua posisi, terbaru di depan.
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

// ClosedTrades mengembalikan log trade yang sudah selesai.
func (pm *PositionManager) ClosedTrades() []*TradeLog {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        cp := make([]*TradeLog, len(pm.trades))
        copy(cp, pm.trades)
        return cp
}

// Stats mengembalikan statistik trading secara agregat.
func (pm *PositionManager) Stats() TradingStats {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        st := TradingStats{
                OpenTrades:    pm.openCount(),
                MaxTrades:     pm.cfg.MaxOpenTrades,
                TotalTrades:   len(pm.trades),
                BestTradePct:  0,
                WorstTradePct: 0,
                RiskLevel:     pm.cfg.RiskLevel,
        }
        first := true
        for _, t := range pm.trades {
                st.TotalPnLUSD += t.PnLUSD
                st.TotalGasUSD += t.GasCostUSD
                st.NetPnLUSD += t.NetPnLUSD
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

// RugPatterns mengembalikan semua rug pattern tersimpan untuk API.
func (pm *PositionManager) RugPatterns() []RugPattern {
        return pm.rugStore.All()
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

// HasOpenPosition returns true jika ada posisi terbuka untuk pair ini.
// Dipakai oleh pipeline untuk bypass filter umur pada token yang sedang dipegang.
func (pm *PositionManager) HasOpenPosition(pairAddress string) bool {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.hasOpenFor(pairAddress)
}

func (pm *PositionManager) hasOpenFor(pairAddress string) bool {
        for _, p := range pm.positions {
                if p.PairAddress == pairAddress && p.Status == PositionOpen {
                        return true
                }
        }
        return false
}

// newID menghasilkan ID 8-karakter hex menggunakan crypto/rand.
func newID() string {
        b := make([]byte, 4)
        if _, err := rand.Read(b); err != nil {
                return fmt.Sprintf("%x", time.Now().UnixNano())
        }
        return hex.EncodeToString(b)
}

func fmtDuration(d time.Duration) string {
        if d < time.Minute {
                return fmt.Sprintf("%.0fs", d.Seconds())
        }
        m := int(d.Minutes())
        s := int(d.Seconds()) - m*60
        return fmt.Sprintf("%dm %ds", m, s)
}
