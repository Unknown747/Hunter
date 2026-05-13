package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type APIServer struct {
	cache     *Cache
	stats     *StatsCounter
	pm        *PositionManager
	bl        *Blacklist
	startTime time.Time
}

func NewAPIServer(cache *Cache, stats *StatsCounter, pm *PositionManager, bl *Blacklist) *APIServer {
	return &APIServer{
		cache:     cache,
		stats:     stats,
		pm:        pm,
		bl:        bl,
		startTime: time.Now(),
	}
}

func (a *APIServer) Routes() http.Handler {
	mux := http.NewServeMux()

	for _, prefix := range []string{"", "/hunter"} {
		p := prefix
		mux.HandleFunc(p+"/api/tokens", a.handleTokens)
		mux.HandleFunc(p+"/api/gems", a.handleGems)
		mux.HandleFunc(p+"/api/top", a.handleTop)
		mux.HandleFunc(p+"/api/signals", a.handleSignals)
		mux.HandleFunc(p+"/api/stats", a.handleStats)
		mux.HandleFunc(p+"/api/movers", a.handleMovers)
		mux.HandleFunc(p+"/api/hot", a.handleHot)
		mux.HandleFunc(p+"/api/positions", a.handlePositions)
		mux.HandleFunc(p+"/api/trades", a.handleTrades)
		mux.HandleFunc(p+"/api/trading-stats", a.handleTradingStats)
		mux.HandleFunc(p+"/api/blacklist", a.handleBlacklist)
		mux.HandleFunc(p+"/api/token/", a.handleTokenHistory) // /api/token/{addr}/history
		mux.HandleFunc(p+"/health", a.handleHealth)
	}

	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/hunter/", http.StripPrefix("/hunter", fs))
	mux.Handle("/", fs)

	return corsMiddleware(mux)
}

func (a *APIServer) handleTokens(w http.ResponseWriter, r *http.Request)       { writeJSON(w, a.cache.AllTokens()) }
func (a *APIServer) handleGems(w http.ResponseWriter, r *http.Request)         { writeJSON(w, a.cache.Gems()) }
func (a *APIServer) handleTop(w http.ResponseWriter, r *http.Request)          { writeJSON(w, a.cache.TopN(20)) }
func (a *APIServer) handleSignals(w http.ResponseWriter, r *http.Request)      { writeJSON(w, a.cache.Signals()) }
func (a *APIServer) handleMovers(w http.ResponseWriter, r *http.Request)       { writeJSON(w, a.cache.TopMovers(10)) }
func (a *APIServer) handleHot(w http.ResponseWriter, r *http.Request)          { writeJSON(w, a.cache.HotPairs(10)) }
func (a *APIServer) handlePositions(w http.ResponseWriter, r *http.Request)    { writeJSON(w, a.pm.AllPositions()) }
func (a *APIServer) handleTrades(w http.ResponseWriter, r *http.Request)       { writeJSON(w, a.pm.ClosedTrades()) }
func (a *APIServer) handleTradingStats(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.pm.Stats()) }
func (a *APIServer) handleBlacklist(w http.ResponseWriter, r *http.Request)    { writeJSON(w, a.bl.All()) }

func (a *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.cache.Stats(a.stats))
}

// handleHealth mengembalikan status engine untuk monitoring/uptime checker.
func (a *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	st := a.cache.Stats(a.stats)
	pos := a.pm.AllPositions()
	openCount := 0
	for _, p := range pos {
		if p.Status == PositionOpen {
			openCount++
		}
	}
	writeJSON(w, HealthStatus{
		Status:        "ok",
		Uptime:        st.Uptime,
		CycleCount:    st.CycleCount,
		TrackedTokens: st.TotalTracked,
		OpenPositions: openCount,
		RiskLevel:     a.pm.cfg.RiskLevel,
		LastPollAgo:   fmtDuration(time.Since(a.startTime)),
	})
}

// handleTokenHistory mengembalikan riwayat score untuk token tertentu.
// Path: /api/token/{pairAddress}/history
func (a *APIServer) handleTokenHistory(w http.ResponseWriter, r *http.Request) {
	// Ekstrak pair address dari path: /api/token/{addr}/history
	path := r.URL.Path
	// Hapus prefix dan suffix
	path = strings.TrimPrefix(path, "/api/token/")
	path = strings.TrimPrefix(path, "/hunter/api/token/")
	path = strings.TrimSuffix(path, "/history")
	pairAddr := strings.ToLower(strings.TrimSpace(path))

	if pairAddr == "" {
		http.Error(w, "pair address required", http.StatusBadRequest)
		return
	}

	history := a.cache.TokenHistory(pairAddr)
	writeJSON(w, history)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
