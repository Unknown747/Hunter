package main

import (
        "strings"
        "unicode"
)

// Filter applies multi-layer filtering to a TokenInfo.
// Returns true if the token passes all filters (should be kept).
func Filter(t *TokenInfo) bool {
        // ── Layer 1: Hard limits ──────────────────────────────────────────────────
        // Tolak token yang tidak punya data waktu listing (pairCreatedAt=0).
        // Ini adalah koin LAMA yang DexScreener tidak punya data tanggal listing-nya.
        // Jika diloloskan, umur=0 tampak seperti "baru saja listing" padahal itu SALAH.
        if t.PairAgeHours == 0 {
                return false
        }
        // Hanya track token < 2 jam (fokus meme coin baru)
        if t.PairAgeHours > 2 {
                return false
        }
        // Likuiditas minimum — di bawah ini slippage terlalu besar
        if t.Liquidity < 10_000 {
                return false
        }
        // Volume minimum — sangat rendah agar token muda tetap masuk pipeline
        // Token listing < 30 menit bisa punya volume 24h sangat kecil karena baru saja dibuat
        // Kita naikkan threshold hanya untuk token yang sudah cukup lama (> 30 menit)
        if t.PairAgeHours >= 0.5 && t.Volume24h < 3_000 {
                return false
        }
        // Token sangat baru (< 30 menit): minimal ada sedikit aktivitas trading
        if t.PairAgeHours > 0 && t.PairAgeHours < 0.5 && t.Volume24h < 200 {
                return false
        }

        // ── Layer 2: Quality filters ──────────────────────────────────────────────
        // Rasio likuiditas/market cap — jika terlalu kecil, potensi rug
        if t.MarketCap > 0 && t.Liquidity/t.MarketCap < 0.03 {
                return false
        }
        // FDV vs likuiditas — jika valuasi 500x lebih besar dari likuiditas = red flag
        if t.MarketCap > 0 && t.MarketCap/t.Liquidity > 500 {
                return false
        }
        totalTxns := t.TxnsBuy + t.TxnsSell
        if totalTxns < 5 {
                return false
        }
        if !isValidSymbol(t.Symbol) || !isValidSymbol(t.Name) {
                return false
        }

        // ── Layer 3: Anti-rug heuristics ─────────────────────────────────────────
        // Dump besar dalam 1 jam terakhir
        if t.PriceChange1h < -50 {
                return false
        }
        // Sell dominance ekstrem
        if totalTxns > 0 {
                sellRatio := float64(t.TxnsSell) / float64(totalTxns)
                if sellRatio > 0.90 {
                        return false
                }
        }

        // ── Layer 4: Honeypot detection ───────────────────────────────────────────
        // Indikator 1: tidak ada sell txn sama sekali padahal buy txn sudah banyak
        // = kemungkinan besar token tidak bisa dijual (honeypot)
        if t.TxnsSell == 0 && t.TxnsBuy >= 15 {
                return false
        }
        // Indikator 2: sell ratio < 2% dengan txn yang sudah banyak
        // = hampir semua orang tidak bisa atau tidak mau jual = honeypot / lock
        if totalTxns >= 30 {
                sellRatio := float64(t.TxnsSell) / float64(totalTxns)
                if sellRatio < 0.02 {
                        return false
                }
        }
        // Indikator 3: harga naik ekstrem (>500% dalam 1 jam) tapi volume rendah
        // = manipulasi harga / wash trading
        if t.PriceChange1h > 500 && t.Volume24h < 20_000 {
                return false
        }

        return true
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
