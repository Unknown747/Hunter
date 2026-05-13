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
