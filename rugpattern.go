package main

import (
        "fmt"
        "math"
        "sync"
        "time"
)

const (
        rugPatternMaxSize  = 100            // simpan maks 100 pattern
        rugPatternWindow   = 48 * time.Hour // pattern > 48 jam dianggap kadaluarsa
        rugMatchThreshold  = 5              // butuh 5+ rug serupa — lebih ketat, kurangi false positive
        rugSimilarityNeeds = 4             // harus cocok di SEMUA 4 dimensi (dari 3/4)
)

// RugPattern mencatat snapshot karakteristik token saat posisi ditutup dengan kerugian.
type RugPattern struct {
        PairAddress    string    `json:"pairAddress"`
        Symbol         string    `json:"symbol"`
        AgeMinutes     float64   `json:"ageMinutes"`     // umur pair saat kita masuk
        BuyRatioEntry  float64   `json:"buyRatioEntry"`  // buy ratio saat kita masuk
        PricePump5m    float64   `json:"pricePump5m"`    // 5m pump saat kita masuk
        LiquidityUSD   float64   `json:"liquidityUsd"`   // likuiditas saat exit
        ExitReason     string    `json:"exitReason"`
        RecordedAt     time.Time `json:"recordedAt"`
}

// RugPatternStore menyimpan dan mencocokkan pola rug pull historis.
type RugPatternStore struct {
        mu       sync.RWMutex
        patterns []RugPattern
}

func NewRugPatternStore() *RugPatternStore {
        return &RugPatternStore{
                patterns: make([]RugPattern, 0, rugPatternMaxSize),
        }
}

// Record menyimpan pola dari posisi yang ditutup dengan kerugian.
// Dipanggil dari PositionManager saat SL / EMERGENCY / TRAILING STOP hit.
func (rs *RugPatternStore) Record(pos *Position, t *TokenInfo, exitReason string) {
        rs.mu.Lock()
        defer rs.mu.Unlock()

        p := RugPattern{
                PairAddress:   pos.PairAddress,
                Symbol:        pos.Symbol,
                AgeMinutes:    pos.EntryAgeMinutes,
                BuyRatioEntry: pos.EntryBuyRatio,
                PricePump5m:   pos.EntryPricePump5m,
                LiquidityUSD:  t.Liquidity,
                ExitReason:    exitReason,
                RecordedAt:    time.Now(),
        }

        // Tambahkan ke depan (terbaru di index 0), potong jika melebihi batas
        rs.patterns = append([]RugPattern{p}, rs.patterns...)
        if len(rs.patterns) > rugPatternMaxSize {
                rs.patterns = rs.patterns[:rugPatternMaxSize]
        }

        logger.Printf("[rugpattern] 📝 Rug dicatat: %s | age=%.0fm buyR=%.2f pump5m=%.1f%% liq=$%.0f | %s",
                pos.Symbol, p.AgeMinutes, p.BuyRatioEntry, p.PricePump5m, p.LiquidityUSD, exitReason)
}

// Check memeriksa apakah token baru cocok dengan pola rug historis.
// Mengembalikan (isRisky bool, alasan string).
func (rs *RugPatternStore) Check(t *TokenInfo) (bool, string) {
        rs.mu.RLock()
        defer rs.mu.RUnlock()

        if len(rs.patterns) == 0 {
                return false, ""
        }

        now := time.Now()
        ageMin := t.PairAgeHours * 60

        // ── Lapis 1: Exact pair address → selalu blokir ──────────────────────────
        for _, p := range rs.patterns {
                if p.PairAddress == t.PairAddress && now.Sub(p.RecordedAt) < rugPatternWindow {
                        return true, fmt.Sprintf("pair ini pernah rug [%s]", p.ExitReason)
                }
        }

        // ── Lapis 2: Kesamaan pola multi-dimensi ─────────────────────────────────
        // Skor setiap pattern: cocok di 3+ dari 4 dimensi = dianggap mirip
        matchCount := 0
        for _, p := range rs.patterns {
                if now.Sub(p.RecordedAt) > rugPatternWindow {
                        continue // pattern kadaluarsa
                }

                score := 0

                // Dimensi 1: umur pair serupa (±10 menit — lebih ketat dari ±15)
                if p.AgeMinutes > 0 && math.Abs(p.AgeMinutes-ageMin) < 10 {
                        score++
                }

                // Dimensi 2: buy ratio saat entry serupa (±0.08 — lebih ketat dari ±0.10)
                if math.Abs(p.BuyRatioEntry-t.BuyRatio) < 0.08 {
                        score++
                }

                // Dimensi 3: price pump 5m saat entry serupa (±10% — lebih ketat dari ±15%)
                if math.Abs(p.PricePump5m-t.PriceChange5m) < 10 {
                        score++
                }

                // Dimensi 4: likuiditas dalam range 2.5x (lebih ketat dari 3x)
                if p.LiquidityUSD > 0 && t.Liquidity > 0 {
                        ratio := p.LiquidityUSD / t.Liquidity
                        if ratio < 1 {
                                ratio = 1.0 / ratio
                        }
                        if ratio < 2.5 {
                                score++
                        }
                }

                if score >= rugSimilarityNeeds {
                        matchCount++
                }
        }

        if matchCount >= rugMatchThreshold {
                return true, fmt.Sprintf("mirip %d rug pattern historis (age=%.0fm buyR=%.2f pump5m=%.1f%%)",
                        matchCount, ageMin, t.BuyRatio, t.PriceChange5m)
        }

        return false, ""
}

// Load memuat patterns dari persistensi.
func (rs *RugPatternStore) Load(patterns []RugPattern) {
        rs.mu.Lock()
        defer rs.mu.Unlock()
        // Filter hanya yang belum kadaluarsa
        now := time.Now()
        valid := patterns[:0]
        for _, p := range patterns {
                if now.Sub(p.RecordedAt) < rugPatternWindow {
                        valid = append(valid, p)
                }
        }
        rs.patterns = valid
}

// Snapshot mengembalikan salinan semua pattern untuk persistensi.
func (rs *RugPatternStore) Snapshot() []RugPattern {
        rs.mu.RLock()
        defer rs.mu.RUnlock()
        cp := make([]RugPattern, len(rs.patterns))
        copy(cp, rs.patterns)
        return cp
}

// All mengembalikan semua pattern untuk API (salinan aman).
func (rs *RugPatternStore) All() []RugPattern {
        return rs.Snapshot()
}

// Count mengembalikan jumlah pattern tersimpan.
func (rs *RugPatternStore) Count() int {
        rs.mu.RLock()
        defer rs.mu.RUnlock()
        return len(rs.patterns)
}
