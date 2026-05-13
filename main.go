package main

import (
        "log"
        "net/http"
        "os"
        "os/signal"
        "strings"
        "syscall"
        "time"
)

var logger = log.New(os.Stdout, "[hunter] ", log.LstdFlags)

func main() {
        port := os.Getenv("PORT")
        if port == "" {
                port = "8080"
        }

        // ── Risk level dari env var ───────────────────────────────────────────────
        riskLevel := strings.ToLower(os.Getenv("RISK_LEVEL"))
        cfg := ConfigForRisk(riskLevel)
        if riskLevel != "" {
                logger.Printf("[config] Risk level: %s (score≥%.0f liq≥$%.0f maxTrades=%d trailing=%.0f%%)",
                        cfg.RiskLevel, cfg.MinScore, cfg.MinLiquidityUSD, cfg.MaxOpenTrades, cfg.TrailingStopPct)
        } else {
                logger.Printf("[config] Risk level: normal (default)")
        }

        stats := NewStatsCounter()
        cache := NewCache()
        bl := NewBlacklist()
        rugStore := NewRugPatternStore()
        exec := NewExecutor()
        pm := NewPositionManager(cfg, exec, bl, rugStore)

        // ── Muat state dari disk (posisi + trade log + blacklist) ─────────────────
        if err := LoadState(pm, bl); err != nil {
                logger.Printf("[persist] ⚠️  Gagal muat state: %v", err)
        }

        // Buffer lebih besar: fetcher + factory watcher keduanya menulis ke channel ini
        rawPairs := make(chan []DexPair, 20)

        // stop ditutup saat menerima sinyal OS untuk graceful shutdown
        stop := make(chan struct{})
        go func() {
                sig := make(chan os.Signal, 1)
                signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
                <-sig
                logger.Println("Sinyal shutdown diterima — menyimpan state dan menghentikan goroutine...")
                if err := SaveState(pm, bl); err != nil {
                        logger.Printf("[persist] ⚠️  Gagal simpan state saat shutdown: %v", err)
                } else {
                        logger.Println("[persist] ✅ State tersimpan")
                }
                close(stop)
        }()

        // ── Auto-save state setiap menit ─────────────────────────────────────────
        go func() {
                ticker := time.NewTicker(time.Minute)
                defer ticker.Stop()
                for {
                        select {
                        case <-stop:
                                return
                        case <-ticker.C:
                                if err := SaveState(pm, bl); err != nil {
                                        logger.Printf("[persist] ⚠️  Auto-save gagal: %v", err)
                                }
                        }
                }
        }()

        // ── Cleanup blacklist kadaluarsa setiap jam ───────────────────────────────
        go func() {
                ticker := time.NewTicker(time.Hour)
                defer ticker.Stop()
                for {
                        select {
                        case <-stop:
                                return
                        case <-ticker.C:
                                bl.Cleanup()
                        }
                }
        }()

        fetcher := NewFetcher(rawPairs)
        go fetcher.Run(stop, stats)

        // Factory watcher: sumber utama koin BARU — monitor Aerodrome factory on-chain
        go RunFactoryWatcher(rawPairs)

        go runPipeline(rawPairs, cache, pm, stop)
        go cache.Cleanup(stop)

        server := NewAPIServer(cache, stats, pm, bl)
        addr := ":" + port
        logger.Printf("Base Meme Coin Hunter — LIVE — http://localhost%s", addr)

        if err := http.ListenAndServe(addr, server.Routes()); err != nil {
                logger.Fatalf("server: %v", err)
        }
}

func runPipeline(in <-chan []DexPair, cache *Cache, pm *PositionManager, stop <-chan struct{}) {
        for {
                select {
                case <-stop:
                        return
                case pairs, ok := <-in:
                        if !ok {
                                return
                        }
                        for i := range pairs {
                                p := &pairs[i]
                                t := Normalize(p)

                                // Bypass filter umur untuk token yang sudah ada posisi terbuka —
                                // kita tetap perlu update harga meski token sudah > 2 jam
                                hasPos := pm.HasOpenPosition(p.PairAddress)
                                if !hasPos && !Filter(t) {
                                        continue
                                }
                                t.Score = Score(t)
                                t.Category = Categorize(t.Score)
                                priorState, _ := cache.Upsert(t)
                                pm.OnTokenUpdate(t, &priorState)
                        }
                }
        }
}
