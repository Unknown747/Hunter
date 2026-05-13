package main

import (
        "encoding/json"
        "net/http"
)

type APIServer struct {
        cache *Cache
        stats *StatsCounter
}

func NewAPIServer(cache *Cache, stats *StatsCounter) *APIServer {
        return &APIServer{cache: cache, stats: stats}
}

func (a *APIServer) Routes() http.Handler {
        mux := http.NewServeMux()

        // Support both /hunter/... prefix (via Replit proxy) and bare paths (direct / VPS)
        for _, prefix := range []string{"", "/hunter"} {
                p := prefix
                mux.HandleFunc(p+"/api/tokens", a.handleTokens)
                mux.HandleFunc(p+"/api/gems", a.handleGems)
                mux.HandleFunc(p+"/api/top", a.handleTop)
                mux.HandleFunc(p+"/api/signals", a.handleSignals)
                mux.HandleFunc(p+"/api/stats", a.handleStats)
                mux.HandleFunc(p+"/api/movers", a.handleMovers)
                mux.HandleFunc(p+"/api/hot", a.handleHot)
        }

        // Serve static files under both prefixes
        fs := http.FileServer(http.Dir("static"))
        mux.Handle("/hunter/", http.StripPrefix("/hunter", fs))
        mux.Handle("/", fs)

        return corsMiddleware(mux)
}

func (a *APIServer) handleTokens(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.AllTokens())
}

func (a *APIServer) handleGems(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.Gems())
}

func (a *APIServer) handleTop(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.TopN(20))
}

func (a *APIServer) handleSignals(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.Signals())
}

func (a *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.Stats(a.stats))
}

func (a *APIServer) handleMovers(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.TopMovers(10))
}

func (a *APIServer) handleHot(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, a.cache.HotPairs(10))
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
