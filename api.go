package main

import (
        "encoding/json"
        "net/http"
        "strings"
)

type APIServer struct {
        cache *Cache
        stats *StatsCounter
        pm    *PositionManager
        bl    *Blacklist
}

func NewAPIServer(cache *Cache, stats *StatsCounter, pm *PositionManager, bl *Blacklist) *APIServer {
        return &APIServer{
                cache: cache,
                stats: stats,
                pm:    pm,
                bl:    bl,
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
                mux.HandleFunc(p+"/api/rug-patterns", a.handleRugPatterns)
                mux.HandleFunc(p+"/api/pipeline-stats", a.handlePipelineStats)
                mux.HandleFunc(p+"/api/close-all", a.handleCloseAll)
                mux.HandleFunc(p+"/api/manual-buy", a.handleManualBuy)
                mux.HandleFunc(p+"/api/manual-sell", a.handleManualSell)
                mux.HandleFunc(p+"/api/force-close", a.handleForceClose)
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
func (a *APIServer) handleRugPatterns(w http.ResponseWriter, r *http.Request)  { writeJSON(w, a.pm.RugPatterns()) }
func (a *APIServer) handleBlacklist(w http.ResponseWriter, r *http.Request)    { writeJSON(w, a.bl.All()) }

// handleCloseAll menutup semua posisi terbuka segera.
// Hanya menerima POST untuk mencegah penutupan tidak sengaja.
func (a *APIServer) handleCloseAll(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "gunakan POST", http.StatusMethodNotAllowed)
                return
        }
        reason := r.URL.Query().Get("reason")
        if reason == "" {
                reason = "MANUAL CLOSE-ALL"
        }
        n := a.pm.CloseAll(reason)
        writeJSON(w, map[string]any{
                "closed": n,
                "reason": reason,
                "status": "ok",
        })
}

func (a *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.Stats(a.stats))
}

// handlePipelineStats mengembalikan statistik filter pipeline (berapa token lolos/ditolak dan mengapa).
func (a *APIServer) handlePipelineStats(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.stats.PipelineStats())
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
                LastPollAgo:   a.stats.LastTokenAgo(),
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

// handleManualBuy memaksa beli token tertentu dari daftar yang di-track.
// POST /api/manual-buy?pair=<pairAddress>
func (a *APIServer) handleManualBuy(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "gunakan POST", http.StatusMethodNotAllowed)
                return
        }
        pairAddr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("pair")))
        if pairAddr == "" {
                http.Error(w, "parameter 'pair' wajib diisi", http.StatusBadRequest)
                return
        }

        t := a.cache.GetToken(pairAddr)
        if t == nil {
                http.Error(w, "token tidak ditemukan di cache — pastikan pair address benar", http.StatusNotFound)
                return
        }

        pos, err := a.pm.ManualBuy(t)
        if err != nil {
                http.Error(w, "manual buy gagal: "+err.Error(), http.StatusInternalServerError)
                return
        }
        writeJSON(w, map[string]any{
                "status":   "ok",
                "symbol":   t.Symbol,
                "price":    t.Price,
                "position": pos,
        })
}

// handleManualSell memaksa jual posisi terbuka tertentu.
// POST /api/manual-sell?pair=<pairAddress>
func (a *APIServer) handleManualSell(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "gunakan POST", http.StatusMethodNotAllowed)
                return
        }
        pairAddr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("pair")))
        if pairAddr == "" {
                http.Error(w, "parameter 'pair' wajib diisi", http.StatusBadRequest)
                return
        }

        t := a.cache.GetToken(pairAddr)
        if t == nil {
                http.Error(w, "token tidak ditemukan di cache", http.StatusNotFound)
                return
        }

        fill, err := a.pm.ManualSell(t)
        if err != nil {
                http.Error(w, "manual sell gagal: "+err.Error(), http.StatusInternalServerError)
                return
        }
        writeJSON(w, map[string]any{
                "status": "ok",
                "symbol": t.Symbol,
                "price":  t.Price,
                "fill":   fill,
        })
}

// handleForceClose menutup posisi di software TANPA eksekusi on-chain.
// Gunakan saat sell gagal (pool CL, pool kosong, dll).
// POST /api/force-close?pair=<pairAddress>&reason=<alasan>
func (a *APIServer) handleForceClose(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                http.Error(w, "gunakan POST", http.StatusMethodNotAllowed)
                return
        }
        pairAddr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("pair")))
        if pairAddr == "" {
                http.Error(w, "parameter 'pair' wajib diisi", http.StatusBadRequest)
                return
        }
        reason := r.URL.Query().Get("reason")
        if reason == "" {
                reason = "FORCE CLOSE (pool tidak kompatibel)"
        }
        if err := a.pm.ForceClose(pairAddr, reason); err != nil {
                http.Error(w, "force close gagal: "+err.Error(), http.StatusInternalServerError)
                return
        }
        writeJSON(w, map[string]any{
                "status": "ok",
                "pair":   pairAddr,
                "reason": reason,
        })
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
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
                if r.Method == http.MethodOptions {
                        w.WriteHeader(http.StatusNoContent)
                        return
                }
                next.ServeHTTP(w, r)
        })
}
