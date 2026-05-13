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

// tg adalah notifier Telegram global — non-nil selalu, tapi disabled jika env var tidak di-set.
var tg *TelegramNotifier

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
        tg = NewTelegramNotifier()
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

        // Factory watcher: sumber utama koin BARU — monitor factory on-chain
        // Mendeteksi event PairCreated langsung dari blockchain Base, lebih cepat dari API manapun
        go RunFactoryWatcher(rawPairs)

        // Bot dua arah: polling perintah dari Telegram (/status, /pos, /closeall, dll)
        tg.StartPolling(pm, cache, stats, stop)

        go runPipeline(rawPairs, cache, pm, stats, stop)
        go cache.Cleanup(stop)

        server := NewAPIServer(cache, stats, pm, bl)
        addr := ":" + port
        logger.Printf("Base Meme Coin Hunter — LIVE — http://localhost%s", addr)

        if err := http.ListenAndServe(addr, server.Routes()); err != nil {
                logger.Fatalf("server: %v", err)
        }
}

// rejectSample menyimpan sample penolakan untuk logging diagnostik periodik.
type rejectSample struct {
        symbol string
        age    float64
        reason string
}

func runPipeline(in <-chan []DexPair, cache *Cache, pm *PositionManager, stats *StatsCounter, stop <-chan struct{}) {
        var (
                totalSeen   int
                totalPassed int
                loggedAt    int

                // Diagnostik: kumpulkan contoh penolakan tiap jendela 100 token
                rejectSamples  []rejectSample
                rejectReasons  = make(map[string]int) // reason prefix → count
                sampleWindowAt int                     // totalSeen saat window terakhir di-reset
        )

        for {
                select {
                case <-stop:
                        return
                case pairs, ok := <-in:
                        if !ok {
                                return
                        }
                        batchPassed := 0
                        for i := range pairs {
                                p := &pairs[i]
                                t := Normalize(p)

                                // Bypass filter umur untuk token yang sudah ada posisi terbuka —
                                // kita tetap perlu update harga meski token sudah > 2 jam
                                hasPos := pm.HasOpenPosition(p.PairAddress)
                                totalSeen++

                                if !hasPos {
                                        res := FilterWithReason(t)
                                        if !res.Pass {
                                                // Kumpulkan sample untuk diagnostik lokal (maks 5 per window)
                                                if len(rejectSamples) < 5 {
                                                        rejectSamples = append(rejectSamples, rejectSample{
                                                                symbol: t.Symbol,
                                                                age:    t.PairAgeHours,
                                                                reason: res.Reason,
                                                        })
                                                }
                                                // Hitung frekuensi setiap jenis penolakan (lokal + global)
                                                prefix := reasonPrefix(res.Reason)
                                                rejectReasons[prefix]++
                                                stats.RecordReject(res.Reason)
                                                stats.RecordSeen()
                                                continue
                                        }
                                }

                                stats.RecordSeen()
                                totalPassed++
                                batchPassed++
                                t.Score = Score(t)
                                t.Category = Categorize(t.Score)
                                priorState, _ := cache.Upsert(t)
                                pm.OnTokenUpdate(t, &priorState)
                                // Deteksi sinyal dan forward ke Telegram
                                if sigs := DetectSignals(t, &priorState); len(sigs) > 0 {
                                        for _, sig := range sigs {
                                                tg.NotifySignal(sig, t)
                                        }
                                }
                                stats.RecordPassed()
                                stats.SetLastTokenTime()
                        }

                        // Log ringkasan setiap 100 token yang diproses
                        if totalSeen/100 > loggedAt/100 {
                                loggedAt = totalSeen
                                pct := float64(totalPassed) / float64(totalSeen) * 100

                                logger.Printf("[pipeline] 📊 Diproses: %d pair total | Lolos: %d (%.1f%%) | Ditolak: %d",
                                        totalSeen, totalPassed, pct, totalSeen-totalPassed)

                                // Tampilkan distribusi alasan penolakan sejak window terakhir
                                windowSize := totalSeen - sampleWindowAt
                                if windowSize > 0 && len(rejectReasons) > 0 {
                                        logger.Printf("[pipeline] 🔍 Alasan penolakan (last %d token):", windowSize)
                                        for reason, count := range rejectReasons {
                                                logger.Printf("[pipeline]   • %s → %d token (%.0f%%)",
                                                        reason, count, float64(count)/float64(windowSize)*100)
                                        }
                                        // Tampilkan beberapa contoh konkret
                                        for _, s := range rejectSamples {
                                                logger.Printf("[pipeline]     contoh: %s (%.0fm) — %s", s.symbol, s.age*60, s.reason)
                                        }
                                }

                                // Reset window diagnostik
                                rejectSamples = rejectSamples[:0]
                                rejectReasons = make(map[string]int)
                                sampleWindowAt = totalSeen
                        }

                        // Notifikasi segera jika ada token baru yang lolos (khusus dari factory watcher)
                        if batchPassed > 0 {
                                logger.Printf("[pipeline] ✅ Batch %d pair → %d lolos filter", len(pairs), batchPassed)
                        }
                }
        }
}

// reasonPrefix mengekstrak kata pertama dari alasan penolakan untuk pengelompokan.
func reasonPrefix(reason string) string {
        if len(reason) == 0 {
                return "unknown"
        }
        // Ambil kata pertama (sebelum ':' atau ' ')
        for i, c := range reason {
                if c == ':' || c == '=' {
                        if i > 0 {
                                return reason[:i]
                        }
                }
        }
        // Jika tidak ada pemisah, ambil 20 karakter pertama
        if len(reason) > 20 {
                return reason[:20]
        }
        return reason
}
