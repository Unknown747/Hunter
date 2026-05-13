package main

import (
	"sort"
	"sync"
	"time"
)

const (
	tokenTTL       = 10 * time.Minute
	cleanupInterval = 1 * time.Minute
	maxSignalLog   = 200
)

// Cache is the central in-memory state store.
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

// Upsert updates or inserts a TokenInfo and updates state.
// Returns detected signals.
func (c *Cache) Upsert(t *TokenInfo) []Signal {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.states[t.PairAddress]
	if !ok {
		state = &TokenState{}
		c.states[t.PairAddress] = state
	}

	sigs := DetectSignals(t, state)

	// Update signals list (ring buffer style)
	for _, s := range sigs {
		// Prepend
		c.signals = append([]Signal{s}, c.signals...)
		if len(c.signals) > maxSignalLog {
			c.signals = c.signals[:maxSignalLog]
		}
	}

	// Attach signal types to token
	t.Signals = nil
	for _, s := range sigs {
		t.Signals = append(t.Signals, s.Type)
	}

	// Update state
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
	return sigs
}

// AllTokens returns all tracked tokens sorted by score desc.
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

// Gems returns tokens with category == "GEM".
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

// TopN returns the top N tokens by score.
func (c *Cache) TopN(n int) []*TokenInfo {
	all := c.AllTokens()
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// Signals returns the signal log.
func (c *Cache) Signals() []Signal {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]Signal, len(c.signals))
	copy(cp, c.signals)
	return cp
}

// Stats returns current engine statistics.
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

// Cleanup removes tokens not seen within TTL.
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

// TopMovers returns tokens with biggest 5-min price change.
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

// HotPairs returns tokens with highest volume relative to liquidity.
func (c *Cache) HotPairs(n int) []*TokenInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*TokenInfo, 0, len(c.tokens))
	for _, t := range c.tokens {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		ratioI := list[i].Volume24h / max1(list[i].Liquidity)
		ratioJ := list[j].Volume24h / max1(list[j].Liquidity)
		return ratioI > ratioJ
	})
	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

func max1(v float64) float64 {
	if v < 1 {
		return 1
	}
	return v
}
