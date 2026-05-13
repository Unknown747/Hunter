package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// StatsCounter holds engine-level counters.
type StatsCounter struct {
	startTime     time.Time
	cycleCount    atomic.Int64
	mu            sync.RWMutex
	pollInterval  time.Duration
	lastTokenTime time.Time // terakhir kali ada token lolos filter

	// Pipeline diagnostics
	totalSeen    atomic.Int64
	totalPassed  atomic.Int64
	rejectCounts map[string]int // reason prefix → count (dijaga mu)
}

func NewStatsCounter() *StatsCounter {
	return &StatsCounter{
		startTime:    time.Now(),
		pollInterval: 5 * time.Second,
		rejectCounts: make(map[string]int),
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

// SetLastTokenTime mencatat waktu terakhir ada token lolos filter.
func (s *StatsCounter) SetLastTokenTime() {
	s.mu.Lock()
	s.lastTokenTime = time.Now()
	s.mu.Unlock()
}

// LastTokenAgo mengembalikan string berapa lama sejak token terakhir lolos filter.
func (s *StatsCounter) LastTokenAgo() string {
	s.mu.RLock()
	t := s.lastTokenTime
	s.mu.RUnlock()
	if t.IsZero() {
		return "belum ada"
	}
	return fmtDuration(time.Since(t))
}

// RecordSeen menambah counter pair yang diproses.
func (s *StatsCounter) RecordSeen() {
	s.totalSeen.Add(1)
}

// RecordPassed menambah counter pair yang lolos filter.
func (s *StatsCounter) RecordPassed() {
	s.totalPassed.Add(1)
}

// RecordReject mencatat satu penolakan dengan alasan singkat.
func (s *StatsCounter) RecordReject(reason string) {
	prefix := reasonPrefixFrom(reason)
	s.mu.Lock()
	s.rejectCounts[prefix]++
	s.mu.Unlock()
}

// PipelineStats mengembalikan snapshot statistik pipeline untuk API.
func (s *StatsCounter) PipelineStats() PipelineStatsResponse {
	seen := s.totalSeen.Load()
	passed := s.totalPassed.Load()
	rejected := seen - passed

	var passPct float64
	if seen > 0 {
		passPct = float64(passed) / float64(seen) * 100
	}

	s.mu.RLock()
	reasons := make([]RejectReason, 0, len(s.rejectCounts))
	for reason, count := range s.rejectCounts {
		pct := 0.0
		if rejected > 0 {
			pct = float64(count) / float64(rejected) * 100
		}
		reasons = append(reasons, RejectReason{
			Reason:  reason,
			Count:   count,
			Percent: pct,
		})
	}
	s.mu.RUnlock()

	// Urutkan berdasarkan count desc
	sort.Slice(reasons, func(i, j int) bool {
		return reasons[i].Count > reasons[j].Count
	})

	return PipelineStatsResponse{
		TotalSeen:     seen,
		TotalPassed:   passed,
		TotalRejected: rejected,
		PassPercent:   passPct,
		RejectReasons: reasons,
		LastTokenAgo:  s.LastTokenAgo(),
	}
}

// PipelineStatsResponse adalah response JSON untuk /api/pipeline-stats.
type PipelineStatsResponse struct {
	TotalSeen     int64          `json:"totalSeen"`
	TotalPassed   int64          `json:"totalPassed"`
	TotalRejected int64          `json:"totalRejected"`
	PassPercent   float64        `json:"passPercent"`
	RejectReasons []RejectReason `json:"rejectReasons"`
	LastTokenAgo  string         `json:"lastTokenAgo"`
}

// RejectReason adalah satu entri dalam breakdown alasan penolakan.
type RejectReason struct {
	Reason  string  `json:"reason"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

// reasonPrefixFrom mengekstrak kategori singkat dari alasan penolakan.
func reasonPrefixFrom(reason string) string {
	if len(reason) == 0 {
		return "unknown"
	}
	for i, c := range reason {
		if c == ':' || c == '=' || c == '<' || c == '>' {
			if i > 0 {
				return reason[:i]
			}
		}
	}
	if len(reason) > 25 {
		return reason[:25]
	}
	return reason
}
