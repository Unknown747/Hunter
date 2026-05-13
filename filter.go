package main

import (
        "fmt"
        "strings"
        "unicode"
)

// FilterResult menyimpan hasil filter: lulus atau alasan penolakan.
type FilterResult struct {
        Pass   bool
        Reason string
}

// Filter applies multi-layer filtering to a TokenInfo.
// Returns true if the token passes all filters (should be kept).
func Filter(t *TokenInfo) bool {
        return filterWithResult(t).Pass
}

// FilterWithReason mengembalikan alasan penolakan untuk logging diagnostik.
func FilterWithReason(t *TokenInfo) FilterResult {
        return filterWithResult(t)
}

func filterWithResult(t *TokenInfo) FilterResult {
        // ── Layer 1: Hard limits ──────────────────────────────────────────────────
        // Tolak token yang tidak punya data waktu listing (pairCreatedAt=0).
        // Ini adalah koin LAMA yang DexScreener tidak punya data tanggal listing-nya.
        // Jika diloloskan, umur=0 tampak seperti "baru saja listing" padahal itu SALAH.
        if t.PairAgeHours == 0 {
                return FilterResult{false, "umur=0 (pairCreatedAt tidak ada)"}
        }
        // Hanya track token < 45 menit (sedikit di atas MaxAgeMinutes 30m agar posisi yang ada tetap bisa diupdate)
        if t.PairAgeHours > 0.75 {
                return FilterResult{false, fmt.Sprintf("terlalu tua: %.0fm (max 45m)", t.PairAgeHours*60)}
        }

        ageMin := t.PairAgeHours * 60

        // Likuiditas minimum — disesuaikan berdasarkan umur token
        // Token yang baru beberapa menit listing wajar punya likuiditas rendah
        var minLiq float64
        switch {
        case ageMin < 15:
                minLiq = 2_000 // < 15 menit: $2k minimum
        case ageMin < 30:
                minLiq = 5_000 // 15-30 menit: $5k minimum
        default:
                minLiq = 10_000 // > 30 menit: $10k minimum
        }
        if t.Liquidity < minLiq {
                return FilterResult{false, fmt.Sprintf("liq=$%.0f < $%.0f (umur=%.0fm)", t.Liquidity, minLiq, ageMin)}
        }

        // Volume minimum — disesuaikan berdasarkan umur token
        switch {
        case ageMin < 15:
                // Sangat baru: tidak perlu volume minimum (pool baru saja dibuat)
                if t.Volume24h < 50 {
                        return FilterResult{false, fmt.Sprintf("vol=$%.0f sangat rendah untuk umur=%.0fm", t.Volume24h, ageMin)}
                }
        case ageMin < 30:
                // < 30 menit: aktivitas minimal
                if t.Volume24h < 200 {
                        return FilterResult{false, fmt.Sprintf("vol=$%.0f < $200 (umur=%.0fm)", t.Volume24h, ageMin)}
                }
        default:
                // > 30 menit: standar
                if t.Volume24h < 3_000 {
                        return FilterResult{false, fmt.Sprintf("vol=$%.0f < $3000 (umur=%.0fm)", t.Volume24h, ageMin)}
                }
        }

        // ── Layer 2: Quality filters ──────────────────────────────────────────────
        // Rasio likuiditas/market cap — jika terlalu kecil, potensi rug
        if t.MarketCap > 0 && t.Liquidity/t.MarketCap < 0.03 {
                return FilterResult{false, fmt.Sprintf("liq/mc=%.3f < 0.03", t.Liquidity/t.MarketCap)}
        }
        // FDV vs likuiditas — jika valuasi 500x lebih besar dari likuiditas = red flag
        if t.MarketCap > 0 && t.MarketCap/t.Liquidity > 500 {
                return FilterResult{false, fmt.Sprintf("mc/liq=%.0fx > 500x", t.MarketCap/t.Liquidity)}
        }

        // Jumlah transaksi minimum — disesuaikan berdasarkan umur
        totalTxns := t.TxnsBuy + t.TxnsSell
        var minTxns int
        switch {
        case ageMin < 15:
                minTxns = 1 // < 15 menit: minimal ada 1 transaksi
        case ageMin < 30:
                minTxns = 3 // 15-30 menit: minimal 3 transaksi
        default:
                minTxns = 5 // > 30 menit: minimal 5 transaksi
        }
        if totalTxns < minTxns {
                return FilterResult{false, fmt.Sprintf("txns=%d < %d (umur=%.0fm)", totalTxns, minTxns, ageMin)}
        }

        if !isValidSymbol(t.Symbol) || !isValidSymbol(t.Name) {
                return FilterResult{false, fmt.Sprintf("simbol/nama tidak valid: '%s'/'%s'", t.Symbol, t.Name)}
        }

        // ── Layer 3: Anti-rug heuristics ─────────────────────────────────────────
        // Dump besar dalam 1 jam terakhir
        if t.PriceChange1h < -50 {
                return FilterResult{false, fmt.Sprintf("dump 1h: %.1f%%", t.PriceChange1h)}
        }
        // Sell dominance ekstrem
        if totalTxns > 0 {
                sellRatio := float64(t.TxnsSell) / float64(totalTxns)
                if sellRatio > 0.90 {
                        return FilterResult{false, fmt.Sprintf("sell dominance: %.0f%%", sellRatio*100)}
                }
        }

        // ── Layer 4: Honeypot detection ───────────────────────────────────────────
        // Indikator 1: tidak ada sell txn sama sekali padahal buy txn sudah banyak
        // = kemungkinan besar token tidak bisa dijual (honeypot)
        if t.TxnsSell == 0 && t.TxnsBuy >= 15 {
                return FilterResult{false, fmt.Sprintf("honeypot: 0 sell tapi %d buy", t.TxnsBuy)}
        }
        // Indikator 2: sell ratio < 2% dengan txn yang sudah banyak
        // = hampir semua orang tidak bisa atau tidak mau jual = honeypot / lock
        if totalTxns >= 30 {
                sellRatio := float64(t.TxnsSell) / float64(totalTxns)
                if sellRatio < 0.02 {
                        return FilterResult{false, fmt.Sprintf("honeypot: sellRatio=%.1f%%", sellRatio*100)}
                }
        }
        // Indikator 3: harga naik ekstrem (>500% dalam 1 jam) tapi volume rendah
        // = manipulasi harga / wash trading
        if t.PriceChange1h > 500 && t.Volume24h < 20_000 {
                return FilterResult{false, fmt.Sprintf("manipulasi: +%.0f%% 1h tapi vol=$%.0f", t.PriceChange1h, t.Volume24h)}
        }

        return FilterResult{true, ""}
}

func isValidSymbol(s string) bool {
        s = strings.TrimSpace(s)
        if len(s) < 2 {
                return false
        }
        for _, r := range s {
                if !unicode.IsPrint(r) {
                        return false
                }
        }
        return true
}
