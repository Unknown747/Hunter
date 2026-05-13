package main

import (
	"sort"
	"sync"
	"time"
)

const (
	tokenTTL        = 10 * time.Minute
	cleanupInterval = 1 * time.Minute
	maxSignalLog    = 200
)

// Cache adalah in-memory state store utama.
type Cache struct {
	mu      sync.RWMutex
	tokens  map[string]*TokenInfo
	states  map[string]*TokenState
	signals []Signal
}

func NewCache() *Cache {
	return &Cache{
		tokens:  make(map[string]*TokenInfo),
		states:  make(map[string]*TokenState),
		signals: make([]Signal, 0, maxSignalLog),
	}
}

// Upsert memperbarui atau menyisipkan TokenInfo dan memperbarui state.
// Mengembalikan prior state (untuk perhitungan volume spike) dan sinyal yang terdeteksi.
func (c *Cache) Upsert(t *TokenInfo) (priorState TokenState, sigs []Signal) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.states[t.PairAddress]
	if !ok {
		state = &TokenState{}
		c.states[t.PairAddress] = state
	}

	// Simpan prior state sebelum diubah
	priorState = *state

	// Hitung volume spike vs volume pertama kali terlihat (bukan tick sebelumnya).
	// Ini menangkap "volume tumbuh 2x sejak pertama kali kita lihat token ini"
	// bukan perubahan kecil antar polling 8 detik.
	if state.FirstVolume > 0 {
		t.VolumeSpike = t.Volume24h / state.FirstVolume
	}

	sigs = DetectSignals(t, state)

	// Update signal list (ring buffer, terbaru di depan)
	for _, s := range sigs {
		c.signals = append([]Signal{s}, c.signals...)
		if len(c.signals) > maxSignalLog {
			c.signals = c.signals[:maxSignalLog]
		}
	}

	// Tempel tipe sinyal ke token
	t.Signals = nil
	for _, s := range sigs {
		t.Signals = append(t.Signals, s.Type)
	}

	// Update state
	if state.FirstVolume == 0 && t.Volume24h > 0 {
		state.FirstVolume = t.Volume24h
		state.FirstSeen = time.Now()
	}
	state.LastScore = t.Score
	state.LastVolume = t.Volume24h
	state.LastPrice = t.Price
	state.LastSeen = time.Now()
	state.History = append(state.History, ScoreSnapshot{
		Score:     t.Score,
		Volume:    t.Volume24h,
		Price:     t.Price,
		Timestamp: time.Now(),
	})
	if len(state.History) > 20 {
		state.History = state.History[len(state.History)-20:]
	}

	c.tokens[t.PairAddress] = t
	return priorState, sigs
}

// AllTokens mengembalikan semua token yang dilacak, diurutkan berdasarkan score desc.
func (c *Cache) AllTokens() []*TokenInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*TokenInfo, 0, len(c.tokens))
	for _, t := range c.tokens {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Score > list[j].Score
	})
	return list
}

// Gems mengembalikan token dengan category == "GEM".
func (c *Cache) Gems() []*TokenInfo {
	all := c.AllTokens()
	out := all[:0:0]
	for _, t := range all {
		if t.Category == "GEM" {
			out = append(out, t)
		}
	}
	return out
}

// TopN mengembalikan N token teratas berdasarkan score.
func (c *Cache) TopN(n int) []*TokenInfo {
	all := c.AllTokens()
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// Signals mengembalikan log sinyal.
func (c *Cache) Signals() []Signal {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]Signal, len(c.signals))
	copy(cp, c.signals)
	return cp
}

// Stats mengembalikan statistik engine saat ini.
func (c *Cache) Stats(stats *StatsCounter) EngineStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var gems, strong, watchlist int
	for _, t := range c.tokens {
		switch t.Category {
		case "GEM":
			gems++
		case "STRONG":
			strong++
		default:
			watchlist++
		}
	}
	return EngineStats{
		TotalTracked:   len(c.tokens),
		TotalGems:      gems,
		TotalStrong:    strong,
		TotalWatchlist: watchlist,
		TotalSignals:   len(c.signals),
		PollInterval:   stats.PollInterval(),
		Uptime:         stats.Uptime(),
		CycleCount:     stats.CycleCount(),
	}
}

// Cleanup menghapus token yang tidak terlihat melebihi TTL.
func (c *Cache) Cleanup(stop <-chan struct{}) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := time.Now().Add(-tokenTTL)
	for addr, state := range c.states {
		if state.LastSeen.Before(cutoff) {
			delete(c.states, addr)
			delete(c.tokens, addr)
		}
	}
}

// TopMovers mengembalikan token dengan perubahan harga 5m terbesar.
func (c *Cache) TopMovers(n int) []*TokenInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*TokenInfo, 0, len(c.tokens))
	for _, t := range c.tokens {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].PriceChange5m > list[j].PriceChange5m
	})
	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

// HotPairs mengembalikan token dengan rasio volume/likuiditas tertinggi.
func (c *Cache) HotPairs(n int) []*TokenInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*TokenInfo, 0, len(c.tokens))
	for _, t := range c.tokens {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		ratioI := list[i].Volume24h / atLeastOne(list[i].Liquidity)
		ratioJ := list[j].Volume24h / atLeastOne(list[j].Liquidity)
		return ratioI > ratioJ
	})
	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

func atLeastOne(v float64) float64 {
	if v < 1 {
		return 1
	}
	return v
}
