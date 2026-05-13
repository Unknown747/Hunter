package main

import (
        "log"
        "net/http"
        "os"
        "os/signal"
        "strconv"
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

        // ── Market cap filter dari env var (opsional) ─────────────────────────────
        // MIN_MARKET_CAP=50000   → hanya masuk jika mcap ≥ $50k
        // MAX_MARKET_CAP=5000000 → hanya masuk jika mcap ≤ $5M
        if v := os.Getenv("MIN_MARKET_CAP"); v != "" {
                if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
                        cfg.MinMarketCapUSD = f
                        logger.Printf("[config] Min market cap: $%.0f", f)
                }
        }
        if v := os.Getenv("MAX_MARKET_CAP"); v != "" {
                if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
                        cfg.MaxMarketCapUSD = f
                        logger.Printf("[config] Max market cap: $%.0f", f)
                }
        }

        // ── Muat config tersimpan dari config.json (override env vars) ────────────
        // Jika pengguna pernah menyimpan config via /config save di Telegram,
        // config tersebut dipakai menggantikan nilai default dari env vars.
        if saved, err := LoadConfig(); err != nil {
                logger.Printf("[config] ⚠️  Gagal baca config.json: %v", err)
        } else if saved != nil {
                *cfg = *saved
                logger.Printf("[config] ✅ Config dimuat dari config.json (risk=%s score≥%.0f liq≥$%.0f)",
                        cfg.RiskLevel, cfg.MinScore, cfg.MinLiquidityUSD)
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

func runPipeline(in <-chan []DexPair, cache *Cache, pm *PositionManager, stats *StatsCounter, stop <-chan struct{}) {
        var (
                totalSeen   int
                totalPassed int

                // Skip cache: pair address yang sudah diketahui TERLALU TUA atau TANPA UMUR.
                // Sekali dimasukkan, tidak akan diproses lagi — mengurangi noise di log dan CPU.
                // Key: lowercase pair address. Value: kapan pertama kali diblokir (untuk log sekali saja).
                oldPairs    = make(map[string]struct{})
                skippedOld  int // counter: berapa pair yang di-skip dari cache
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
                        batchNew    := 0 // pair baru yang belum ada di skip cache
                        for i := range pairs {
                                p := &pairs[i]
                                addr := strings.ToLower(p.PairAddress)

                                // ── Skip cache: lewati langsung jika sudah diketahui tua ──────────
                                if _, isOld := oldPairs[addr]; isOld {
                                        skippedOld++
                                        continue
                                }

                                // Bypass filter umur untuk posisi terbuka — tetap update harga
                                hasPos := pm.HasOpenPosition(addr)
                                totalSeen++
                                batchNew++

                                t := Normalize(p)

                                if !hasPos {
                                        res := FilterWithReason(t)
                                        if !res.Pass {
                                                stats.RecordReject(res.Reason)
                                                stats.RecordSeen()

                                                // Jika ditolak karena umur (tua permanen atau tanpa data) →
                                                // masukkan ke skip cache agar tidak muncul lagi
                                                prefix := reasonPrefixFrom(res.Reason)
                                                if prefix == "terlalu tua" || prefix == "umur" {
                                                        oldPairs[addr] = struct{}{}
                                                        logger.Printf("[pipeline] 🚫 Skip permanen: %s (%s) — tidak akan di-scan lagi",
                                                                t.Symbol, res.Reason)
                                                }
                                                continue
                                        }
                                }

                                stats.RecordSeen()
                                totalPassed++
                                batchPassed++
                                t.Score = Score(t)
                                t.Category = Categorize(t.Score)
                                priorState, sigs := cache.Upsert(t)
                                pm.OnTokenUpdate(t, &priorState)
                                for _, sig := range sigs {
                                        tg.NotifySignal(sig, t)
                                }
                                stats.RecordPassed()
                                stats.SetLastTokenTime()
                        }

                        // Log ringkasan hanya jika ada pair baru yang diproses atau ada yang lolos
                        if batchPassed > 0 {
                                logger.Printf("[pipeline] ✅ %d pair lolos filter (total seen=%d passed=%d skip-cache=%d)",
                                        batchPassed, totalSeen, totalPassed, skippedOld)
                        } else if batchNew > 0 && skippedOld%500 == 1 {
                                // Log status skip cache periodik (setiap 500 skip) agar tahu cache bekerja
                                logger.Printf("[pipeline] 📊 Skip cache aktif: %d pair lama diabaikan | seen=%d passed=%d cache-size=%d",
                                        skippedOld, totalSeen, totalPassed, len(oldPairs))
                        }
                }
        }
}

