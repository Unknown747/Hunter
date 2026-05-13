package main

import (
        "fmt"
        "time"
)

// EntryResult menyimpan hasil pemeriksaan kondisi entry.
type EntryResult struct {
        Allow  bool
        Reason string
}

// ExitResult menyimpan hasil pemeriksaan kondisi exit.
type ExitResult struct {
        ShouldExit bool
        Fraction   float64 // porsi posisi yang ditutup (0–1)
        Reason     string
}

// CheckEntry mengevaluasi semua kondisi BUY untuk sebuah token.
// Mengembalikan true hanya jika SEMUA kondisi terpenuhi.
func CheckEntry(t *TokenInfo, state *TokenState, cfg *StrategyConfig) EntryResult {
        ageMin := t.PairAgeHours * 60

        // ── Anti-rug filter ────────────────────────────────────────────────────────
        if t.PairAgeHours == 0 {
                return EntryResult{false, "anti-rug: umur tidak diketahui (pairCreatedAt=0)"}
        }
        if ageMin < 2 {
                return EntryResult{false, fmt.Sprintf("anti-rug: terlalu baru %.1f menit", ageMin)}
        }
        if t.PriceChange1h <= -60 {
                return EntryResult{false, fmt.Sprintf("anti-rug: harga dump %.1f%%", t.PriceChange1h)}
        }
        if t.Symbol == "" || t.Name == "" {
                return EntryResult{false, "anti-rug: nama/simbol kosong"}
        }

        // ── Skor utama ─────────────────────────────────────────────────────────────
        if t.Score < cfg.MinScore {
                return EntryResult{false, fmt.Sprintf("score %.1f < %.0f", t.Score, cfg.MinScore)}
        }

        // ── Tekanan beli ───────────────────────────────────────────────────────────
        if t.BuyRatio < cfg.MinBuyRatio {
                return EntryResult{false, fmt.Sprintf("buyRatio %.2f < %.2f", t.BuyRatio, cfg.MinBuyRatio)}
        }

        // ── Volume spike (vs volume pertama kali terlihat — konsisten dengan dashboard) ──
        // Gunakan t.VolumeSpike yang sudah dihitung cache (vs FirstVolume),
        // bukan tick-to-tick, agar konsisten dengan kolom SPIKE di UI.
        if state.FirstVolume <= 0 {
                return EntryResult{false, "menunggu riwayat volume"}
        }
        spike := t.VolumeSpike
        if spike < cfg.MinVolumeSpike {
                return EntryResult{false, fmt.Sprintf("volSpike %.2fx < %.1fx", spike, cfg.MinVolumeSpike)}
        }

        // ── Likuiditas ─────────────────────────────────────────────────────────────
        if t.Liquidity < cfg.MinLiquidityUSD {
                return EntryResult{false, fmt.Sprintf("liq $%.0f < $%.0f", t.Liquidity, cfg.MinLiquidityUSD)}
        }

        // ── Sweet spot umur ────────────────────────────────────────────────────────
        if ageMin < cfg.MinAgeMinutes {
                return EntryResult{false, fmt.Sprintf("terlalu baru: %.1f menit", ageMin)}
        }
        if ageMin > cfg.MaxAgeMinutes {
                return EntryResult{false, fmt.Sprintf("terlalu lama: %.1f menit", ageMin)}
        }

        // ── Hindari kejar token yang sudah pump ───────────────────────────────────
        if t.PriceChange5m > cfg.MaxPricePump5m {
                return EntryResult{false, fmt.Sprintf("sudah pump +%.1f%%", t.PriceChange5m)}
        }

        return EntryResult{true, fmt.Sprintf("score=%.1f buyR=%.2f spike=%.1fx age=%.0fm liq=$%.0f",
                t.Score, t.BuyRatio, spike, ageMin, t.Liquidity)}
}

// CheckExit mengevaluasi semua kondisi SELL untuk posisi yang sedang terbuka.
// PENTING: dipanggil di bawah write lock — boleh memutasikan field di *Position.
func CheckExit(p *Position, t *TokenInfo, cfg *StrategyConfig) ExitResult {
        if t.Price <= 0 || p.EntryPrice <= 0 {
                return ExitResult{}
        }

        pnlPct := (t.Price/p.EntryPrice - 1) * 100
        holdMin := time.Since(p.EntryTime).Minutes()

        // ── Emergency exit (selalu tutup penuh) ───────────────────────────────────
        if t.BuyRatio < cfg.EmergencyBuyRatio {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: buyRatio %.2f (collapse)", t.BuyRatio)}
        }
        if t.PriceChange5m <= cfg.SuddenDumpThreshold {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: dump mendadak %.1f%%", t.PriceChange5m)}
        }
        if p.EntryVolume > 0 && t.Volume24h < p.EntryVolume*cfg.VolumeDropFraction {
                return ExitResult{true, 1.0, fmt.Sprintf("EMERGENCY: volume turun %.0f%%", (1-t.Volume24h/p.EntryVolume)*100)}
        }

        // ── Stop loss ──────────────────────────────────────────────────────────────
        if pnlPct <= cfg.StopLossPct {
                return ExitResult{true, 1.0, fmt.Sprintf("STOP LOSS %.1f%%", pnlPct)}
        }

        // ── Trailing stop ──────────────────────────────────────────────────────────
        if cfg.TrailingStopPct > 0 {
                // Inisialisasi HWM ke harga entry pertama kali
                if p.HighWaterMark < p.EntryPrice {
                        p.HighWaterMark = p.EntryPrice
                }
                // Perbarui HWM ke harga tertinggi yang pernah dicapai
                if t.Price > p.HighWaterMark {
                        p.HighWaterMark = t.Price
                }
                // Aktifkan hanya setelah profit melewati threshold
                hwmPnlPct := (p.HighWaterMark/p.EntryPrice - 1) * 100
                if hwmPnlPct >= cfg.TrailingActivatePct {
                        dropFromHWM := (p.HighWaterMark - t.Price) / p.HighWaterMark * 100
                        if dropFromHWM >= cfg.TrailingStopPct {
                                return ExitResult{true, 1.0, fmt.Sprintf(
                                        "TRAILING STOP: -%.1f%% dari high $%.6f (peak +%.1f%%)",
                                        dropFromHWM, p.HighWaterMark, hwmPnlPct,
                                )}
                        }
                }
        }

        // ── Time exit (tidak ada momentum) ────────────────────────────────────────
        if holdMin >= cfg.MaxHoldMinutes && pnlPct < cfg.MinProfitForHold {
                return ExitResult{true, 1.0, fmt.Sprintf("TIME EXIT: hold %.0f menit, PnL %.1f%%", holdMin, pnlPct)}
        }

        // ── Take profit (multi-level) ──────────────────────────────────────────────
        if !p.TP1Hit && pnlPct >= cfg.TP1Pct {
                return ExitResult{true, cfg.TP1SellFrac, fmt.Sprintf("TP1 +%.1f%% → jual %.0f%%", pnlPct, cfg.TP1SellFrac*100)}
        }
        if p.TP1Hit && pnlPct >= cfg.TP2Pct {
                return ExitResult{true, 1.0, fmt.Sprintf("TP2 +%.1f%% → tutup posisi", pnlPct)}
        }

        return ExitResult{}
}
