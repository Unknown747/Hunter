package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// StatsCounter holds engine-level counters.
type StatsCounter struct {
	startTime    time.Time
	cycleCount   atomic.Int64
	mu           sync.RWMutex
	pollInterval time.Duration
}

func NewStatsCounter() *StatsCounter {
	return &StatsCounter{
		startTime:    time.Now(),
		pollInterval: 5 * time.Second,
	}
}

func (s *StatsCounter) IncrCycle() {
	s.cycleCount.Add(1)
}

func (s *StatsCounter) CycleCount() int64 {
	return s.cycleCount.Load()
}

func (s *StatsCounter) SetPollInterval(d time.Duration) {
	s.mu.Lock()
	s.pollInterval = d
	s.mu.Unlock()
}

func (s *StatsCounter) PollInterval() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pollInterval.String()
}

func (s *StatsCounter) Uptime() string {
	d := time.Since(s.startTime)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, sec)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}
