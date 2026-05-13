package main

// telegram_commands.go — Bot dua arah: polling perintah dari Telegram
//
// Perintah yang tersedia:
//   /help      — daftar semua perintah
//   /status    — ringkasan engine (uptime, cycles, posisi, P&L)
//   /pos       — posisi terbuka + P&L realtime
//   /trades    — 5 trade terakhir yang sudah ditutup
//   /gems      — top 5 token berdasarkan score
//   /closeall  — tutup semua posisi (konfirmasi via tombol)
//   /pause     — hentikan entry baru sementara (exit tetap berjalan)
//   /resume    — aktifkan entry baru kembali
//
// Keamanan: hanya chat_id yang di-set di TG_CHAT_ID yang diproses.

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "os"
        "strconv"
        "strings"
        "time"
)

// ─── Telegram API types ────────────────────────────────────────────────────────

type tgUpdateResp struct {
        OK     bool       `json:"ok"`
        Result []tgUpdate `json:"result"`
}

type tgUpdate struct {
        UpdateID      int64            `json:"update_id"`
        Message       *tgMessage       `json:"message"`
        CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgMessage struct {
        MessageID int64   `json:"message_id"`
        From      *tgUser `json:"from"`
        Chat      struct {
                ID int64 `json:"id"`
        } `json:"chat"`
        Text string `json:"text"`
}

type tgUser struct {
        ID        int64  `json:"id"`
        Username  string `json:"username"`
        FirstName string `json:"first_name"`
}

type tgCallbackQuery struct {
        ID      string     `json:"id"`
        From    *tgUser    `json:"from"`
        Message *tgMessage `json:"message"`
        Data    string     `json:"data"`
}

type tgInlineButton struct {
        Text         string `json:"text"`
        CallbackData string `json:"callback_data"`
}

// ─── StartPolling ──────────────────────────────────────────────────────────────

// StartPolling memulai loop long-polling Telegram di goroutine terpisah.
func (tg *TelegramNotifier) StartPolling(pm *PositionManager, c *Cache, st *StatsCounter, stop <-chan struct{}) {
        if !tg.enabled {
                return
        }
        logger.Printf("[telegram] 🤖 Bot perintah aktif — menunggu pesan dari chat_id=%s", tg.chatID)
        tg.send("🟢 <b>Bot aktif</b> — engine berjalan.\nKetik /help untuk daftar perintah.")

        go func() {
                var offset int64
                client := &http.Client{Timeout: 35 * time.Second}

                for {
                        select {
                        case <-stop:
                                tg.send("🔴 <b>Bot berhenti</b> — engine shutdown.")
                                return
                        default:
                        }

                        updates, err := tg.pollUpdates(client, offset)
                        if err != nil {
                                if !isTgTimeoutErr(err) {
                                        logger.Printf("[telegram] polling error: %v", err)
                                }
                                time.Sleep(3 * time.Second)
                                continue
                        }

                        for _, upd := range updates {
                                offset = upd.UpdateID + 1

                                if upd.CallbackQuery != nil {
                                        tg.handleCallback(upd.CallbackQuery, pm)
                                        continue
                                }
                                if upd.Message == nil || upd.Message.Text == "" {
                                        continue
                                }

                                // Keamanan: tolak pesan dari chat yang tidak diotorisasi
                                chatID := strconv.FormatInt(upd.Message.Chat.ID, 10)
                                if chatID != tg.chatID {
                                        logger.Printf("[telegram] ⚠️  Perintah dari chat tidak diotorisasi: %d — diabaikan", upd.Message.Chat.ID)
                                        continue
                                }
                                tg.handleCommand(upd.Message, pm, c, st)
                        }
                }
        }()
}

// pollUpdates memanggil getUpdates dengan long-polling 25 detik.
func (tg *TelegramNotifier) pollUpdates(client *http.Client, offset int64) ([]tgUpdate, error) {
        url := fmt.Sprintf(
                "https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=25&allowed_updates=message,callback_query",
                tg.token, offset,
        )
        resp, err := client.Get(url)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, err
        }

        var result tgUpdateResp
        if err := json.Unmarshal(body, &result); err != nil {
                return nil, err
        }
        if !result.OK {
                return nil, fmt.Errorf("telegram API not ok: %s", string(body))
        }
        return result.Result, nil
}

// ─── Command Router ────────────────────────────────────────────────────────────

func (tg *TelegramNotifier) handleCommand(msg *tgMessage, pm *PositionManager, c *Cache, st *StatsCounter) {
        text := strings.TrimSpace(msg.Text)
        // Hapus mention bot (contoh: /status@NamaBotKu)
        if idx := strings.Index(text, "@"); idx > 0 {
                text = text[:idx]
        }
        parts := strings.Fields(text)
        if len(parts) == 0 {
                return
        }
        cmd := strings.ToLower(parts[0])
        logger.Printf("[telegram] 📨 Perintah diterima: %s", cmd)

        switch cmd {
        case "/help", "/start":
                tg.cmdHelp()
        case "/status", "/s":
                tg.cmdStatus(pm, c, st)
        case "/pos", "/positions":
                tg.cmdPositions(pm)
        case "/trades", "/t":
                tg.cmdTrades(pm)
        case "/gems", "/g":
                tg.cmdGems(c)
        case "/closeall":
                tg.cmdCloseAllConfirm()
        case "/pause":
                tg.cmdPause(pm)
        case "/resume":
                tg.cmdResume(pm)
        case "/alert", "/a":
                tg.cmdAlert(parts[1:])
        case "/config", "/cfg":
                tg.cmdConfig(pm, parts[1:])
        default:
                tg.send(fmt.Sprintf("❓ Perintah tidak dikenal: <code>%s</code>\nKetik /help untuk bantuan.", cmd))
        }
}

// handleCallback memproses klik tombol inline keyboard.
func (tg *TelegramNotifier) handleCallback(cb *tgCallbackQuery, pm *PositionManager) {
        tg.answerCallback(cb.ID, "")

        // Validasi chat ID
        if cb.Message != nil {
                chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
                if chatID != tg.chatID {
                        return
                }
        }

        switch cb.Data {
        case "closeall_confirm":
                n := pm.CloseAll("MANUAL — tutup via Telegram")
                if n == 0 {
                        tg.send("ℹ️ Tidak ada posisi terbuka yang perlu ditutup.")
                } else {
                        tg.send(fmt.Sprintf("✅ <b>%d posisi ditutup.</b>\nNotifikasi exit akan menyusul.", n))
                }
        case "closeall_cancel":
                tg.send("❌ <b>Dibatalkan</b> — posisi terbuka tetap berjalan.")
        case "pause_confirm":
                pm.Pause()
                tg.send("⏸ <b>Entry baru dijeda.</b>\nPosisi yang ada tetap dipantau dan akan exit normal.\nKetik /resume untuk mengaktifkan kembali.")
        case "pause_cancel":
                tg.send("❌ <b>Dibatalkan</b> — engine tetap aktif.")
        }
}

// ─── Command Handlers ──────────────────────────────────────────────────────────

func (tg *TelegramNotifier) cmdHelp() {
        tg.send(
                "🤖 <b>Base Meme Coin Hunter</b>\n\n" +
                        "<b>📊 Info Engine</b>\n" +
                        "  /status — uptime, cycles, P&L ringkasan\n" +
                        "  /pos    — posisi terbuka + P&L realtime\n" +
                        "  /trades — 5 trade terakhir\n" +
                        "  /gems   — top 5 token berdasarkan score\n\n" +
                        "<b>🎛 Kontrol Trading</b>\n" +
                        "  /closeall — tutup semua posisi (ada konfirmasi)\n" +
                        "  /pause    — jeda entry baru (exit tetap jalan)\n" +
                        "  /resume   — aktifkan entry kembali\n\n" +
                        "<b>⚙️ Konfigurasi Strategi</b>\n" +
                        "  /config              — lihat semua parameter aktif\n" +
                        "  /config mcap min 50000   — min market cap $50k\n" +
                        "  /config mcap max 5000000 — max market cap $5M\n" +
                        "  /config mcap reset       — nonaktifkan filter mcap\n" +
                        "  /config score 75         — ubah min score\n" +
                        "  /config buyratio 0.65    — ubah min buy ratio\n" +
                        "  /config liq 20000        — ubah min liquidity\n" +
                        "  /config risk normal      — ganti preset risiko\n" +
                        "  /config reset            — reset ke default\n\n" +
                        "<b>🔔 Filter Alert Sinyal</b>\n" +
                        "  /alert            — lihat filter aktif\n" +
                        "  /alert score 80   — hanya notif score ≥ 80\n" +
                        "  /alert liq 20000  — hanya notif liq ≥ $20k\n" +
                        "  /alert pump 40    — hanya notif pump5m ≤ 40%\n" +
                        "  /alert signal EARLY_GEM  — toggle sinyal on/off\n" +
                        "  /alert signal all|none   — aktifkan/nonaktifkan semua\n" +
                        "  /alert reset      — reset semua filter ke default\n\n" +
                        "<b>🔔 Notifikasi otomatis</b>\n" +
                        "  🟢 MASUK posisi baru\n" +
                        "  ✂️ TP1 terkena (jual 50%)\n" +
                        "  🔴/🟢 Posisi ditutup (TP/SL/Emergency)\n" +
                        "  ⚡ Sinyal EARLY_GEM / BREAKOUT / MOMENTUM",
        )
}

func (tg *TelegramNotifier) cmdStatus(pm *PositionManager, c *Cache, st *StatsCounter) {
        pipeStats := st.PipelineStats()
        tradingStats := pm.Stats()
        positions := pm.AllPositions()

        openCount := 0
        unrealizedPnL := 0.0
        for _, p := range positions {
                if p.Status == PositionOpen {
                        openCount++
                        unrealizedPnL += p.PnLPercent * p.SizeUSD / 100
                }
        }

        mode := "🔴 Paper Trading"
        if os.Getenv("PRIVATE_KEY") != "" {
                mode = "🟡 Live Trading (on-chain)"
        }

        entryStatus := "🟢 Aktif"
        if pm.IsPaused() {
                entryStatus = "⏸ Dijeda — /resume untuk aktifkan"
        }

        winStr := "— (belum ada trade)"
        if tradingStats.TotalTrades > 0 {
                winStr = fmt.Sprintf("%.0f%% (%d/%d trade)", tradingStats.WinRate, tradingStats.WinCount, tradingStats.TotalTrades)
        }

        fmtPnL := func(v float64) string {
                if v >= 0 {
                        return fmt.Sprintf("+$%.4f", v)
                }
                return fmt.Sprintf("-$%.4f", -v)
        }

        cacheStats := c.Stats(st)

        tg.send(fmt.Sprintf(
                "📊 <b>Engine Status</b>\n\n"+
                        "⏱ Uptime: <b>%s</b>\n"+
                        "🔄 Diproses: <b>%s pair</b>\n"+
                        "🎯 Token terlacak: <b>%d</b>\n"+
                        "📂 Posisi: <b>%d/%d terbuka</b>\n\n"+
                        "💰 Unrealized P&L: <b>%s</b>\n"+
                        "💵 Realized P&L: <b>%s</b>\n"+
                        "🏆 Win rate: <b>%s</b>\n\n"+
                        "%s\n"+
                        "🚦 Entry: <b>%s</b>",
                cacheStats.Uptime,
                fmtThousands(int(pipeStats.TotalSeen)),
                cacheStats.TotalTracked,
                openCount, pm.cfg.MaxOpenTrades,
                fmtPnL(unrealizedPnL),
                fmtPnL(tradingStats.TotalPnLUSD),
                winStr,
                mode,
                entryStatus,
        ))
}

func (tg *TelegramNotifier) cmdPositions(pm *PositionManager) {
        positions := pm.AllPositions()

        var open []*Position
        for _, p := range positions {
                if p.Status == PositionOpen {
                        open = append(open, p)
                }
        }

        if len(open) == 0 {
                tg.send("📂 <b>Tidak ada posisi terbuka saat ini.</b>")
                return
        }

        lines := []string{fmt.Sprintf("📂 <b>Posisi Terbuka (%d)</b>\n", len(open))}
        for i, p := range open {
                emoji := "🔴"
                pnlSign := ""
                if p.PnLPercent > 0 {
                        emoji = "🟢"
                        pnlSign = "+"
                }
                pnlUSD := p.PnLPercent * p.SizeUSD / 100
                usdSign := ""
                if pnlUSD >= 0 {
                        usdSign = "+"
                }
                hold := fmtDuration(time.Since(p.EntryTime).Round(time.Second))

                lines = append(lines, fmt.Sprintf(
                        "%d. %s <code>$%s</code> — <b>%s%.1f%%</b> (%s$%.4f) | %s\n"+
                                "   Entry: <code>$%.8f</code> → Cur: <code>$%.8f</code>\n"+
                                "   🔗 <a href=\"%s\">DexScreener</a>",
                        i+1, emoji, p.Symbol,
                        pnlSign, p.PnLPercent,
                        usdSign, pnlUSD,
                        hold,
                        p.EntryPrice, p.CurrentPrice,
                        dexLink(p.PairAddress),
                ))
        }
        tg.send(strings.Join(lines, "\n"))
}

func (tg *TelegramNotifier) cmdTrades(pm *PositionManager) {
        trades := pm.ClosedTrades()
        if len(trades) == 0 {
                tg.send("📈 <b>Belum ada trade yang selesai.</b>")
                return
        }

        limit := 5
        if len(trades) < limit {
                limit = len(trades)
        }

        lines := []string{fmt.Sprintf("📈 <b>%d Trade Terakhir</b>\n", limit)}
        for i, tr := range trades[:limit] {
                emoji := "🔴"
                pnlSign := ""
                if tr.PnLPercent > 0 {
                        emoji = "🟢"
                        pnlSign = "+"
                }
                netSign := ""
                if tr.NetPnLUSD >= 0 {
                        netSign = "+"
                }
                lines = append(lines, fmt.Sprintf(
                        "%d. %s <code>$%s</code> <b>%s%.1f%%</b> (%s$%.4f net) | %s | %s",
                        i+1, emoji, tr.Symbol,
                        pnlSign, tr.PnLPercent,
                        netSign, tr.NetPnLUSD,
                        tr.Duration,
                        shortenExitReason(tr.ExitReason),
                ))
        }

        stats := pm.Stats()
        if stats.TotalTrades > 0 {
                netSign := ""
                if stats.TotalPnLUSD >= 0 {
                        netSign = "+"
                }
                lines = append(lines, fmt.Sprintf(
                        "\n<b>Total: %s$%.4f</b> dari %d trade | Win: <b>%.0f%%</b>",
                        netSign, stats.TotalPnLUSD, stats.TotalTrades, stats.WinRate,
                ))
        }
        tg.send(strings.Join(lines, "\n"))
}

func (tg *TelegramNotifier) cmdGems(c *Cache) {
        tokens := c.TopN(5)
        if len(tokens) == 0 {
                tg.send("💎 <b>Cache kosong.</b> Engine mungkin baru saja restart.")
                return
        }

        lines := []string{"💎 <b>Top 5 Token (Score Tertinggi)</b>\n"}
        for i, t := range tokens {
                ageMin := t.PairAgeHours * 60
                totalTxns := t.TxnsBuy + t.TxnsSell
                buyPct := 0.0
                if totalTxns > 0 {
                        buyPct = float64(t.TxnsBuy) / float64(totalTxns) * 100
                }
                pumpStr := fmt.Sprintf("%.1f%%", t.PriceChange5m)
                if t.PriceChange5m > 0 {
                        pumpStr = "+" + pumpStr
                }
                lines = append(lines, fmt.Sprintf(
                        "%d. <code>$%s</code> — Score: <b>%.0f</b> [%s]\n"+
                                "   Buy: <b>%.0f%%</b> | Liq: <b>$%.0f</b> | 5m: <b>%s</b> | Umur: <b>%.0fm</b>\n"+
                                "   🔗 <a href=\"%s\">DexScreener</a>",
                        i+1, t.Symbol, t.Score, t.Category,
                        buyPct, t.Liquidity, pumpStr, ageMin,
                        dexLink(t.PairAddress),
                ))
        }
        tg.send(strings.Join(lines, "\n"))
}

func (tg *TelegramNotifier) cmdCloseAllConfirm() {
        tg.sendWithKeyboard(
                "⚠️ <b>Tutup Semua Posisi?</b>\n\n"+
                        "Semua posisi terbuka akan ditutup sekarang.\n"+
                        "<i>Exit di harga pasar saat ini.</i>",
                [][]tgInlineButton{{
                        {Text: "✅ Ya, tutup semua", CallbackData: "closeall_confirm"},
                        {Text: "❌ Batal", CallbackData: "closeall_cancel"},
                }},
        )
}

func (tg *TelegramNotifier) cmdPause(pm *PositionManager) {
        if pm.IsPaused() {
                tg.send("ℹ️ Entry sudah dalam kondisi dijeda.\nKetik /resume untuk mengaktifkan kembali.")
                return
        }
        tg.sendWithKeyboard(
                "⏸ <b>Jeda Entry Baru?</b>\n\n"+
                        "Engine akan berhenti membuka posisi baru.\n"+
                        "Posisi yang sudah terbuka tetap dipantau dan akan exit normal.",
                [][]tgInlineButton{{
                        {Text: "✅ Ya, jeda sekarang", CallbackData: "pause_confirm"},
                        {Text: "❌ Batal", CallbackData: "pause_cancel"},
                }},
        )
}

func (tg *TelegramNotifier) cmdResume(pm *PositionManager) {
        if !pm.IsPaused() {
                tg.send("ℹ️ Entry sudah aktif. Tidak ada yang perlu diaktifkan kembali.")
                return
        }
        pm.Resume()
        tg.send("▶️ <b>Entry diaktifkan kembali.</b>\nEngine kembali mencari posisi baru.")
}

// cmdAlert mengatur filter threshold untuk notifikasi sinyal.
//
// Contoh:
//
//      /alert              — tampilkan pengaturan saat ini
//      /alert score 80     — hanya notif jika score ≥ 80
//      /alert liq 20000    — hanya notif jika liq ≥ $20k
//      /alert pump 40      — hanya notif jika pump5m ≤ 40%
//      /alert signal EARLY_GEM — toggle jenis sinyal on/off
//      /alert signal all   — aktifkan semua jenis sinyal
//      /alert signal none  — nonaktifkan semua sinyal
//      /alert reset        — reset semua ke default (tanpa filter)
func (tg *TelegramNotifier) cmdAlert(args []string) {
        if len(args) == 0 {
                tg.alertShow()
                return
        }

        sub := strings.ToLower(args[0])

        switch sub {
        case "reset":
                tg.alertMu.Lock()
                tg.alertMinScore = 0
                tg.alertMinLiq = 0
                tg.alertMaxPump = 0
                tg.alertMu.Unlock()

                // Reset jenis sinyal ke default
                tg.signalsMu.Lock()
                tg.signals = map[string]bool{"EARLY_GEM": true, "BREAKOUT": true, "MOMENTUM": true}
                tg.signalsMu.Unlock()

                tg.send("♻️ <b>Semua filter direset ke default.</b>\nSemua sinyal EARLY_GEM, BREAKOUT, MOMENTUM aktif tanpa filter tambahan.")
                return

        case "score":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/alert score 80</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 || v > 100 {
                        tg.send("❌ Nilai score harus angka 0–100. Contoh: <code>/alert score 80</code>")
                        return
                }
                tg.alertMu.Lock()
                tg.alertMinScore = v
                tg.alertMu.Unlock()
                if v == 0 {
                        tg.send("✅ <b>Filter score dihapus</b> — semua score akan dinotifikasi.")
                } else {
                        tg.send(fmt.Sprintf("✅ <b>Filter score diset: ≥ %.0f</b>\nSinyal dengan score di bawah %.0f tidak akan dikirim.", v, v))
                }

        case "liq":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/alert liq 20000</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 {
                        tg.send("❌ Nilai liq harus angka positif dalam USD. Contoh: <code>/alert liq 20000</code>")
                        return
                }
                tg.alertMu.Lock()
                tg.alertMinLiq = v
                tg.alertMu.Unlock()
                if v == 0 {
                        tg.send("✅ <b>Filter liquidity dihapus</b> — semua liq akan dinotifikasi.")
                } else {
                        tg.send(fmt.Sprintf("✅ <b>Filter liquidity diset: ≥ $%.0f</b>\nSinyal dengan liq di bawah $%.0f tidak akan dikirim.", v, v))
                }

        case "pump":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/alert pump 40</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 {
                        tg.send("❌ Nilai pump harus angka positif (%). Contoh: <code>/alert pump 40</code>")
                        return
                }
                tg.alertMu.Lock()
                tg.alertMaxPump = v
                tg.alertMu.Unlock()
                if v == 0 {
                        tg.send("✅ <b>Filter pump dihapus</b> — semua pump5m akan dinotifikasi.")
                } else {
                        tg.send(fmt.Sprintf("✅ <b>Filter pump diset: ≤ %.0f%%</b>\nSinyal dengan pump5m di atas %.0f%% tidak akan dikirim.", v, v))
                }

        case "signal":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/alert signal EARLY_GEM</code> atau <code>/alert signal all</code> / <code>none</code>")
                        return
                }
                sigArg := strings.ToUpper(args[1])

                tg.signalsMu.Lock()
                switch sigArg {
                case "ALL":
                        tg.signals["EARLY_GEM"] = true
                        tg.signals["BREAKOUT"] = true
                        tg.signals["MOMENTUM"] = true
                        tg.signalsMu.Unlock()
                        tg.send("✅ <b>Semua jenis sinyal diaktifkan:</b> EARLY_GEM, BREAKOUT, MOMENTUM.")
                case "NONE":
                        tg.signals["EARLY_GEM"] = false
                        tg.signals["BREAKOUT"] = false
                        tg.signals["MOMENTUM"] = false
                        tg.signalsMu.Unlock()
                        tg.send("✅ <b>Semua jenis sinyal dinonaktifkan.</b>\nHanya notifikasi entry/exit yang akan dikirim.")
                case "EARLY_GEM", "BREAKOUT", "MOMENTUM":
                        current := tg.signals[sigArg]
                        tg.signals[sigArg] = !current
                        newState := tg.signals[sigArg]
                        tg.signalsMu.Unlock()
                        stateStr := "✅ Diaktifkan"
                        if !newState {
                                stateStr = "❌ Dinonaktifkan"
                        }
                        tg.send(fmt.Sprintf("%s: sinyal <b>%s</b>", stateStr, sigArg))
                default:
                        tg.signalsMu.Unlock()
                        tg.send(fmt.Sprintf(
                                "❌ Sinyal tidak dikenal: <code>%s</code>\nPilihan valid: <code>EARLY_GEM</code>, <code>BREAKOUT</code>, <code>MOMENTUM</code>, <code>all</code>, <code>none</code>",
                                sigArg,
                        ))
                }

        default:
                tg.send(fmt.Sprintf(
                        "❓ Sub-perintah tidak dikenal: <code>%s</code>\n\n"+
                                "Contoh penggunaan:\n"+
                                "  /alert             — lihat filter aktif\n"+
                                "  /alert score 80    — min score 80\n"+
                                "  /alert liq 20000   — min liq $20k\n"+
                                "  /alert pump 40     — max pump5m 40%%\n"+
                                "  /alert signal BREAKOUT — toggle sinyal\n"+
                                "  /alert reset       — hapus semua filter",
                        sub,
                ))
        }
}

// alertShow menampilkan ringkasan filter yang sedang aktif.
func (tg *TelegramNotifier) alertShow() {
        tg.alertMu.RLock()
        minScore := tg.alertMinScore
        minLiq := tg.alertMinLiq
        maxPump := tg.alertMaxPump
        tg.alertMu.RUnlock()

        scoreStr := "—"
        if minScore > 0 {
                scoreStr = fmt.Sprintf("≥ %.0f", minScore)
        }
        liqStr := "—"
        if minLiq > 0 {
                liqStr = fmt.Sprintf("≥ $%.0f", minLiq)
        }
        pumpStr := "—"
        if maxPump > 0 {
                pumpStr = fmt.Sprintf("≤ %.0f%%", maxPump)
        }

        tg.signalsMu.RLock()
        var active, inactive []string
        for _, sig := range []string{"EARLY_GEM", "BREAKOUT", "MOMENTUM"} {
                if tg.signals[sig] {
                        active = append(active, sig)
                } else {
                        inactive = append(inactive, sig)
                }
        }
        tg.signalsMu.RUnlock()

        sigActive := "semua nonaktif"
        if len(active) > 0 {
                sigActive = strings.Join(active, ", ")
        }
        sigInactive := "—"
        if len(inactive) > 0 {
                sigInactive = strings.Join(inactive, ", ")
        }

        tg.send(fmt.Sprintf(
                "🔔 <b>Filter Alert Sinyal Aktif</b>\n\n"+
                        "📊 Score minimum: <b>%s</b>\n"+
                        "💧 Liquidity minimum: <b>%s</b>\n"+
                        "📈 Pump 5m maksimum: <b>%s</b>\n\n"+
                        "✅ Sinyal aktif: <b>%s</b>\n"+
                        "❌ Sinyal nonaktif: <b>%s</b>\n\n"+
                        "<i>Ketik /alert reset untuk hapus semua filter.\n"+
                        "Ketik /help untuk melihat contoh perintah lengkap.</i>",
                scoreStr, liqStr, pumpStr,
                sigActive, sigInactive,
        ))
}

// ─── /config command ──────────────────────────────────────────────────────────

// cmdConfig menampilkan atau mengubah parameter strategi secara realtime.
//
// Contoh:
//
//      /config                  — tampilkan semua parameter aktif
//      /config mcap min 50000   — set min market cap $50k
//      /config mcap max 5000000 — set max market cap $5M
//      /config mcap reset       — nonaktifkan filter market cap
//      /config score 75         — ubah min score
//      /config buyratio 0.65    — ubah min buy ratio
//      /config liq 20000        — ubah min liquidity
//      /config risk normal      — ganti risk level preset
//      /config reset            — reset semua ke default
func (tg *TelegramNotifier) cmdConfig(pm *PositionManager, args []string) {
        if len(args) == 0 {
                tg.configShow(pm)
                return
        }

        sub := strings.ToLower(args[0])

        switch sub {
        case "reset":
                pm.UpdateConfig(func(c *StrategyConfig) {
                        risk := c.RiskLevel
                        *c = *ConfigForRisk(risk)
                })
                tg.configShow(pm)
                tg.send("♻️ <b>Konfigurasi direset ke default.</b>")

        case "risk":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/config risk normal</code> | <code>conservative</code> | <code>aggressive</code>")
                        return
                }
                level := strings.ToLower(args[1])
                if level != "normal" && level != "conservative" && level != "aggressive" {
                        tg.send("❌ Risk level tidak valid. Pilihan: <code>normal</code>, <code>conservative</code>, <code>aggressive</code>")
                        return
                }
                pm.UpdateConfig(func(c *StrategyConfig) {
                        newCfg := ConfigForRisk(level)
                        newCfg.MinMarketCapUSD = c.MinMarketCapUSD
                        newCfg.MaxMarketCapUSD = c.MaxMarketCapUSD
                        *c = *newCfg
                })
                tg.send(fmt.Sprintf("✅ <b>Risk level diubah ke: %s</b>\n<i>(filter market cap dipertahankan)</i>", level))
                tg.configShow(pm)

        case "mcap":
                if len(args) < 2 {
                        tg.send("❌ Format:\n  <code>/config mcap min 50000</code>\n  <code>/config mcap max 5000000</code>\n  <code>/config mcap reset</code>")
                        return
                }
                sub2 := strings.ToLower(args[1])
                switch sub2 {
                case "reset":
                        pm.UpdateConfig(func(c *StrategyConfig) {
                                c.MinMarketCapUSD = 0
                                c.MaxMarketCapUSD = 0
                        })
                        tg.send("✅ <b>Filter market cap dinonaktifkan.</b>\nEntry tidak dibatasi oleh market cap.")
                case "min":
                        if len(args) < 3 {
                                tg.send("❌ Format: <code>/config mcap min 50000</code>")
                                return
                        }
                        v, err := strconv.ParseFloat(args[2], 64)
                        if err != nil || v < 0 {
                                tg.send("❌ Nilai harus angka positif dalam USD. Contoh: <code>/config mcap min 50000</code>")
                                return
                        }
                        pm.UpdateConfig(func(c *StrategyConfig) { c.MinMarketCapUSD = v })
                        if v == 0 {
                                tg.send("✅ <b>Min market cap dihapus</b> — tidak ada batas bawah market cap.")
                        } else {
                                tg.send(fmt.Sprintf("✅ <b>Min market cap: $%.0f</b>\nToken dengan mcap di bawah $%.0f akan diabaikan.", v, v))
                        }
                case "max":
                        if len(args) < 3 {
                                tg.send("❌ Format: <code>/config mcap max 5000000</code>")
                                return
                        }
                        v, err := strconv.ParseFloat(args[2], 64)
                        if err != nil || v < 0 {
                                tg.send("❌ Nilai harus angka positif dalam USD. Contoh: <code>/config mcap max 5000000</code>")
                                return
                        }
                        pm.UpdateConfig(func(c *StrategyConfig) { c.MaxMarketCapUSD = v })
                        if v == 0 {
                                tg.send("✅ <b>Max market cap dihapus</b> — tidak ada batas atas market cap.")
                        } else {
                                tg.send(fmt.Sprintf("✅ <b>Max market cap: $%.0f</b>\nToken dengan mcap di atas $%.0f akan diabaikan.", v, v))
                        }
                default:
                        tg.send("❌ Sub-perintah tidak dikenal.\nGunakan: <code>min</code>, <code>max</code>, atau <code>reset</code>")
                }

        case "score":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/config score 75</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 || v > 100 {
                        tg.send("❌ Score harus angka 0–100. Contoh: <code>/config score 75</code>")
                        return
                }
                pm.UpdateConfig(func(c *StrategyConfig) { c.MinScore = v })
                tg.send(fmt.Sprintf("✅ <b>Min score diubah: ≥ %.0f</b>", v))

        case "buyratio":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/config buyratio 0.65</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 || v > 1 {
                        tg.send("❌ Buy ratio harus angka 0–1. Contoh: <code>/config buyratio 0.65</code>")
                        return
                }
                pm.UpdateConfig(func(c *StrategyConfig) { c.MinBuyRatio = v })
                tg.send(fmt.Sprintf("✅ <b>Min buy ratio diubah: ≥ %.2f (%.0f%%)</b>", v, v*100))

        case "liq":
                if len(args) < 2 {
                        tg.send("❌ Format: <code>/config liq 20000</code>")
                        return
                }
                v, err := strconv.ParseFloat(args[1], 64)
                if err != nil || v < 0 {
                        tg.send("❌ Nilai harus angka positif USD. Contoh: <code>/config liq 20000</code>")
                        return
                }
                pm.UpdateConfig(func(c *StrategyConfig) { c.MinLiquidityUSD = v })
                tg.send(fmt.Sprintf("✅ <b>Min liquidity diubah: ≥ $%.0f</b>", v))

        default:
                tg.send(fmt.Sprintf(
                        "❓ Sub-perintah tidak dikenal: <code>%s</code>\n\n"+
                                "Contoh penggunaan:\n"+
                                "  /config              — lihat config aktif\n"+
                                "  /config mcap min 50000\n"+
                                "  /config mcap max 5000000\n"+
                                "  /config mcap reset\n"+
                                "  /config score 75\n"+
                                "  /config buyratio 0.65\n"+
                                "  /config liq 20000\n"+
                                "  /config risk normal\n"+
                                "  /config reset",
                        sub,
                ))
        }
}

// configShow menampilkan ringkasan semua parameter config yang sedang aktif.
func (tg *TelegramNotifier) configShow(pm *PositionManager) {
        cfg := pm.GetConfig()

        mcapMin := "— (nonaktif)"
        if cfg.MinMarketCapUSD > 0 {
                mcapMin = fmt.Sprintf("≥ $%.0f", cfg.MinMarketCapUSD)
        }
        mcapMax := "— (nonaktif)"
        if cfg.MaxMarketCapUSD > 0 {
                mcapMax = fmt.Sprintf("≤ $%.0f", cfg.MaxMarketCapUSD)
        }

        tg.send(fmt.Sprintf(
                "⚙️ <b>Konfigurasi Strategi Aktif</b>\n\n"+
                        "🎯 Risk Level: <b>%s</b>\n\n"+
                        "<b>📥 Entry Conditions</b>\n"+
                        "  Score min:    <b>≥ %.0f</b>\n"+
                        "  Buy ratio:    <b>≥ %.2f (%.0f%%)</b>\n"+
                        "  Vol spike:    <b>≥ %.1fx</b>\n"+
                        "  Liquidity:    <b>≥ $%.0f</b>\n"+
                        "  Umur token:   <b>%.0f–%.0f menit</b>\n"+
                        "  Pump 5m max:  <b>≤ %.0f%%</b>\n"+
                        "  Mcap min:     <b>%s</b>\n"+
                        "  Mcap max:     <b>%s</b>\n\n"+
                        "<b>📤 Exit Rules</b>\n"+
                        "  TP1: <b>+%.0f%%</b> → jual <b>%.0f%%</b>\n"+
                        "  TP2: <b>+%.0f%%</b> → tutup semua\n"+
                        "  SL:  <b>%.0f%%</b>\n"+
                        "  Trailing: <b>%.0f%%</b> (aktif setelah +%.0f%%)\n"+
                        "  Max hold: <b>%.0f menit</b>\n\n"+
                        "<b>📦 Position Sizing</b>\n"+
                        "  Size: <b>$%.2f</b> per trade\n"+
                        "  Max posisi: <b>%d</b>\n\n"+
                        "<i>Ketik /config &lt;param&gt; &lt;nilai&gt; untuk mengubah.\nKetik /config reset untuk kembali ke default.</i>",
                cfg.RiskLevel,
                cfg.MinScore,
                cfg.MinBuyRatio, cfg.MinBuyRatio*100,
                cfg.MinVolumeSpike,
                cfg.MinLiquidityUSD,
                cfg.MinAgeMinutes, cfg.MaxAgeMinutes,
                cfg.MaxPricePump5m,
                mcapMin, mcapMax,
                cfg.TP1Pct, cfg.TP1SellFrac*100,
                cfg.TP2Pct,
                cfg.StopLossPct,
                cfg.TrailingStopPct, cfg.TrailingActivatePct,
                cfg.MaxHoldMinutes,
                cfg.TradeSizeUSD,
                cfg.MaxOpenTrades,
        ))
}

// ─── Inline Keyboard & Callback ────────────────────────────────────────────────

// sendWithKeyboard mengirim pesan dengan tombol inline keyboard.
func (tg *TelegramNotifier) sendWithKeyboard(text string, buttons [][]tgInlineButton) {
        if !tg.enabled {
                return
        }

        // Bangun JSON inline_keyboard
        var rowStrs []string
        for _, row := range buttons {
                var btnStrs []string
                for _, btn := range row {
                        b, _ := json.Marshal(btn)
                        btnStrs = append(btnStrs, string(b))
                }
                rowStrs = append(rowStrs, "["+strings.Join(btnStrs, ",")+"]")
        }
        keyboard := `{"inline_keyboard":[` + strings.Join(rowStrs, ",") + `]}`

        go func() {
                url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tg.token)
                payload, _ := json.Marshal(map[string]interface{}{
                        "chat_id":                  tg.chatID,
                        "text":                     text,
                        "parse_mode":               "HTML",
                        "disable_web_page_preview": true,
                        "reply_markup":             json.RawMessage(keyboard),
                })
                resp, err := http.Post(url, "application/json", strings.NewReader(string(payload)))
                if err != nil {
                        logger.Printf("[telegram] keyboard send error: %v", err)
                        return
                }
                resp.Body.Close()
        }()
}

// answerCallback mengakui callback query (wajib agar spinner Telegram hilang).
func (tg *TelegramNotifier) answerCallback(callbackID, text string) {
        go func() {
                url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", tg.token)
                payload, _ := json.Marshal(map[string]interface{}{
                        "callback_query_id": callbackID,
                        "text":              text,
                })
                resp, err := http.Post(url, "application/json", strings.NewReader(string(payload)))
                if err == nil {
                        resp.Body.Close()
                }
        }()
}

// ─── Utility ───────────────────────────────────────────────────────────────────

func isTgTimeoutErr(err error) bool {
        if err == nil {
                return false
        }
        s := err.Error()
        return strings.Contains(s, "timeout") ||
                strings.Contains(s, "deadline") ||
                strings.Contains(s, "EOF") ||
                strings.Contains(s, "connection reset")
}

func shortenExitReason(r string) string {
        switch {
        case strings.Contains(r, "TP2"):
                return "✅ TP2"
        case strings.Contains(r, "TP1"):
                return "✂️ TP1"
        case strings.Contains(r, "TRAILING"):
                return "📉 Trailing"
        case strings.Contains(r, "STOP LOSS"):
                return "🛑 SL"
        case strings.Contains(r, "EMERGENCY"):
                return "⚠️ Emergency"
        case strings.Contains(r, "TIME"):
                return "⏱ Time"
        case strings.Contains(r, "MANUAL"):
                return "🖐 Manual"
        default:
                if len(r) > 18 {
                        return r[:18] + "…"
                }
                return r
        }
}

func fmtThousands(n int) string {
        if n < 1000 {
                return strconv.Itoa(n)
        }
        return fmtThousands(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}
