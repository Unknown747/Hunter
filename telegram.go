package main

import (
        "bytes"
        "encoding/json"
        "fmt"
        "net/http"
        "os"
        "strings"
        "sync"
        "time"
)

// TelegramNotifier mengirim notifikasi ke Telegram Bot secara async dan non-blocking.
// Pesan diantri ke channel buffer — pipeline TIDAK pernah diblokir oleh Telegram.
//
// Env vars yang dibutuhkan:
//   TG_BOT_TOKEN  — token dari @BotFather  (contoh: 123456:ABCdef...)
//   TG_CHAT_ID    — chat ID tujuan         (contoh: -1001234567890 atau 987654321)
//   TG_SIGNALS    — jenis sinyal yang dikirim, dipisah koma (default: EARLY_GEM,BREAKOUT,MOMENTUM)
//                   Set ke "none" untuk menonaktifkan notifikasi sinyal
type TelegramNotifier struct {
        token     string
        chatID    string
        enabled   bool
        signals   map[string]bool // sinyal mana yang di-notify
        queue     chan string      // buffer pesan async
        rateLimit *time.Ticker    // maks 1 pesan per 1.5 detik (Telegram: 30/menit per chat)

        // Dedup: cegah spam sinyal yang sama untuk token yang sama dalam 5 menit
        dedupMu sync.Mutex
        dedup   map[string]time.Time // key: "pairAddr:sigType" → waktu terakhir dikirim
}

// NewTelegramNotifier membaca konfigurasi dari env dan membuat notifier.
// Jika TG_BOT_TOKEN atau TG_CHAT_ID tidak di-set, notifier dinonaktifkan secara diam-diam.
func NewTelegramNotifier() *TelegramNotifier {
        token := os.Getenv("TG_BOT_TOKEN")
        chatID := os.Getenv("TG_CHAT_ID")

        tg := &TelegramNotifier{
                token:   token,
                chatID:  chatID,
                enabled: token != "" && chatID != "",
                signals: map[string]bool{
                        "EARLY_GEM": true,
                        "BREAKOUT":  true,
                        "MOMENTUM":  true,
                },
                queue:     make(chan string, 64),
                rateLimit: time.NewTicker(1500 * time.Millisecond),
                dedup:     make(map[string]time.Time),
        }

        // Override daftar sinyal dari env jika ada
        if raw := os.Getenv("TG_SIGNALS"); raw != "" {
                if strings.EqualFold(raw, "none") {
                        tg.signals = map[string]bool{}
                } else {
                        tg.signals = map[string]bool{}
                        for _, s := range strings.Split(raw, ",") {
                                tg.signals[strings.TrimSpace(strings.ToUpper(s))] = true
                        }
                }
        }

        if tg.enabled {
                logger.Printf("[telegram] ✅ Notifikasi aktif → chat_id=%s | sinyal=%v",
                        chatID, tg.activeSignalList())
                go tg.drainQueue()
        } else {
                logger.Printf("[telegram] ℹ️  Tidak aktif (set TG_BOT_TOKEN + TG_CHAT_ID untuk mengaktifkan)")
        }

        return tg
}

// drainQueue mengambil pesan dari queue dan mengirimnya dengan rate limiting.
// Berjalan sebagai goroutine terpisah seumur hidup program.
func (tg *TelegramNotifier) drainQueue() {
        for msg := range tg.queue {
                <-tg.rateLimit.C // tunggu slot rate limit
                if err := tg.doSend(msg); err != nil {
                        logger.Printf("[telegram] ⚠️  Gagal kirim: %v", err)
                }
        }
}

// send memasukkan pesan ke queue secara non-blocking.
// Jika queue penuh (64 pesan), pesan dibuang daripada memblokir pipeline.
func (tg *TelegramNotifier) send(text string) {
        if !tg.enabled {
                return
        }
        select {
        case tg.queue <- text:
        default:
                logger.Printf("[telegram] ⚠️  Queue penuh — pesan dibuang")
        }
}

// doSend mengirim pesan ke Telegram API (synchronous, dipanggil dari drainQueue).
func (tg *TelegramNotifier) doSend(text string) error {
        url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tg.token)
        body, _ := json.Marshal(map[string]interface{}{
                "chat_id":                  tg.chatID,
                "text":                     text,
                "parse_mode":               "HTML",
                "disable_web_page_preview": true,
        })

        resp, err := http.Post(url, "application/json", bytes.NewReader(body))
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        if resp.StatusCode != 200 {
                return fmt.Errorf("Telegram API %d", resp.StatusCode)
        }
        return nil
}

// ─── Notifikasi Entry ──────────────────────────────────────────────────────────

// NotifyEntry dikirim saat posisi baru dibuka (paper atau live).
func (tg *TelegramNotifier) NotifyEntry(pos *Position, t *TokenInfo, cfg *StrategyConfig) {
        if !tg.enabled {
                return
        }

        tp1Price := pos.EntryPrice * (1 + float64(cfg.TP1Pct)/100)
        tp2Price := pos.EntryPrice * (1 + float64(cfg.TP2Pct)/100)
        slPrice := pos.EntryPrice * (1 + float64(cfg.StopLossPct)/100)

        dex := dexLink(pos.PairAddress)
        ageMin := t.PairAgeHours * 60
        totalTxns := t.TxnsBuy + t.TxnsSell
        buyPct := 0.0
        if totalTxns > 0 {
                buyPct = float64(t.TxnsBuy) / float64(totalTxns) * 100
        }

        msg := fmt.Sprintf(
                "🟢 <b>MASUK POSISI</b> — <code>$%s</code>\n"+
                        "\n"+
                        "📊 Score: <b>%.0f</b> | Buy: <b>%.0f%%</b> | Umur: <b>%.0fm</b>\n"+
                        "📈 Spike: <b>%.1fx</b> | Liq: <b>$%.0f</b> | Pump5m: <b>+%.1f%%</b>\n"+
                        "📦 Txn: <b>%d</b> | Size: <b>$%.2f</b>\n"+
                        "\n"+
                        "💰 Entry: <code>$%.8f</code>\n"+
                        "🎯 TP1 (+%d%%): <code>$%.8f</code>\n"+
                        "🎯 TP2 (+%d%%): <code>$%.8f</code>\n"+
                        "🛑 SL (-%d%%): <code>$%.8f</code>\n"+
                        "\n"+
                        "🔗 <a href=\"%s\">DexScreener</a>",
                t.Symbol,
                t.Score, buyPct, ageMin,
                t.VolumeSpike, t.Liquidity, t.PriceChange5m,
                totalTxns, pos.SizeUSD,
                pos.EntryPrice,
                cfg.TP1Pct, tp1Price,
                cfg.TP2Pct, tp2Price,
                -cfg.StopLossPct, slPrice,
                dex,
        )
        tg.send(msg)
}

// ─── Notifikasi Exit ───────────────────────────────────────────────────────────

// NotifyExit dikirim saat posisi ditutup sepenuhnya.
func (tg *TelegramNotifier) NotifyExit(pos *Position, exitPrice float64, reason string) {
        if !tg.enabled {
                return
        }

        holdDur := fmtDuration(time.Since(pos.EntryTime))
        netPnL := pos.RealizedUSD - pos.GasCostUSD

        // Pilih emoji berdasarkan hasil
        mark := "🔴"
        pnlStr := fmt.Sprintf("%.2f%%", pos.PnLPercent)
        if pos.PnLPercent > 0 {
                mark = "🟢"
                pnlStr = "+" + pnlStr
        }

        netStr := fmt.Sprintf("$%.4f", netPnL)
        if netPnL > 0 {
                netStr = "+" + netStr
        }

        dex := dexLink(pos.PairAddress)

        // Ringkas alasan exit untuk tampilan
        reasonShort := reason
        if len(reason) > 30 {
                reasonShort = reason[:30]
        }

        msg := fmt.Sprintf(
                "%s <b>POSISI DITUTUP</b> — <code>$%s</code>\n"+
                        "\n"+
                        "📋 Alasan: <b>%s</b>\n"+
                        "💰 P&L: <b>%s</b> | Net: <b>%s</b>\n"+
                        "⏱ Hold: <b>%s</b> | Gas: <b>$%.4f</b>\n"+
                        "📊 Entry: <code>$%.8f</code> → Exit: <code>$%.8f</code>\n"+
                        "\n"+
                        "🔗 <a href=\"%s\">DexScreener</a>",
                mark, pos.Symbol,
                reasonShort,
                pnlStr, netStr,
                holdDur, pos.GasCostUSD,
                pos.EntryPrice, exitPrice,
                dex,
        )
        tg.send(msg)
}

// NotifyTP1 dikirim saat TP1 (partial sell) terkena.
func (tg *TelegramNotifier) NotifyTP1(pos *Position, exitPrice float64, cfg *StrategyConfig) {
        if !tg.enabled {
                return
        }

        pnlPct := (exitPrice/pos.EntryPrice - 1) * 100
        dex := dexLink(pos.PairAddress)

        msg := fmt.Sprintf(
                "✂️ <b>TP1 TERKENA</b> — <code>$%s</code>\n"+
                        "\n"+
                        "💰 Jual 50%% @ <b>+%.1f%%</b>\n"+
                        "📊 Entry: <code>$%.8f</code> → TP1: <code>$%.8f</code>\n"+
                        "🎯 Target TP2: <b>+%d%%</b> | Sisa: <b>50%%</b> posisi\n"+
                        "\n"+
                        "🔗 <a href=\"%s\">DexScreener</a>",
                pos.Symbol,
                pnlPct,
                pos.EntryPrice, exitPrice,
                cfg.TP2Pct,
                dex,
        )
        tg.send(msg)
}

// ─── Notifikasi Sinyal ─────────────────────────────────────────────────────────

// NotifySignal dikirim untuk sinyal EARLY_GEM / BREAKOUT / MOMENTUM.
// Jenis sinyal yang dikirim diatur oleh env var TG_SIGNALS.
// Dedup: sinyal yang sama untuk token yang sama diabaikan dalam 5 menit.
func (tg *TelegramNotifier) NotifySignal(sig Signal, t *TokenInfo) {
        if !tg.enabled || !tg.signals[sig.Type] {
                return
        }

        // Cek dedup — cegah spam sinyal yang sama per token
        dedupKey := sig.PairAddress + ":" + sig.Type
        tg.dedupMu.Lock()
        if last, ok := tg.dedup[dedupKey]; ok && time.Since(last) < 5*time.Minute {
                tg.dedupMu.Unlock()
                return
        }
        tg.dedup[dedupKey] = time.Now()
        // Bersihkan entry lama (> 30 menit) untuk cegah memory leak
        for k, v := range tg.dedup {
                if time.Since(v) > 30*time.Minute {
                        delete(tg.dedup, k)
                }
        }
        tg.dedupMu.Unlock()

        totalTxns := t.TxnsBuy + t.TxnsSell
        buyPct := 0.0
        if totalTxns > 0 {
                buyPct = float64(t.TxnsBuy) / float64(totalTxns) * 100
        }
        ageMin := t.PairAgeHours * 60

        icon := signalIcon(sig.Type)
        dex := dexLink(t.PairAddress)

        msg := fmt.Sprintf(
                "%s <b>%s</b> — <code>$%s</code>\n"+
                        "\n"+
                        "📊 Score: <b>%.0f</b> | Buy: <b>%.0f%%</b> | Umur: <b>%.0fm</b>\n"+
                        "💧 Liq: <b>$%.0f</b> | Vol24h: <b>$%.0f</b>\n"+
                        "📈 Pump5m: <b>+%.1f%%</b> | Pump1h: <b>+%.1f%%</b>\n"+
                        "\n"+
                        "📝 %s\n"+
                        "\n"+
                        "🔗 <a href=\"%s\">DexScreener</a>",
                icon, sig.Type, t.Symbol,
                t.Score, buyPct, ageMin,
                t.Liquidity, t.Volume24h,
                t.PriceChange5m, t.PriceChange1h,
                sig.Detail,
                dex,
        )
        tg.send(msg)
}

// ─── Helper ────────────────────────────────────────────────────────────────────

func dexLink(pairAddr string) string {
        return "https://dexscreener.com/base/" + pairAddr
}

func signalIcon(sigType string) string {
        switch sigType {
        case "EARLY_GEM":
                return "⚡"
        case "BREAKOUT":
                return "🚀"
        case "MOMENTUM":
                return "📈"
        case "NEW_LISTING":
                return "🆕"
        case "SELL_PRESSURE":
                return "⚠️"
        default:
                return "🔔"
        }
}

func (tg *TelegramNotifier) activeSignalList() []string {
        var out []string
        for k, v := range tg.signals {
                if v {
                        out = append(out, k)
                }
        }
        return out
}

// SendTest mengirim pesan test untuk memverifikasi konfigurasi bot.
// Dipanggil via /api/telegram-test.
func (tg *TelegramNotifier) SendTest() error {
        if !tg.enabled {
                return fmt.Errorf("TG_BOT_TOKEN atau TG_CHAT_ID belum di-set")
        }
        return tg.doSend(
                "✅ <b>Base Meme Coin Hunter</b> — Test berhasil!\n\n" +
                        "Bot siap mengirim notifikasi:\n" +
                        "🟢 Entry posisi\n" +
                        "🔴 Exit posisi (TP/SL/Emergency)\n" +
                        "⚡ Sinyal EARLY_GEM / BREAKOUT / MOMENTUM",
        )
}
