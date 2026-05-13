package main

import (
	"sync"
	"time"
)

const (
	blacklistSLThreshold = 2             // blokir setelah N stop loss
	blacklistSLWindow    = 24 * time.Hour // dalam rentang waktu ini
	blacklistDuration    = 6 * time.Hour  // durasi blokir
)

// Blacklist melacak token yang diblokir sementara akibat performa buruk.
type Blacklist struct {
	mu      sync.RWMutex
	entries map[string]*BlacklistEntry
}

func NewBlacklist() *Blacklist {
	return &Blacklist{
		entries: make(map[string]*BlacklistEntry),
	}
}

// IsBlacklisted mengembalikan true jika token sedang dalam masa blokir.
func (b *Blacklist) IsBlacklisted(pairAddress string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	e, ok := b.entries[pairAddress]
	if !ok {
		return false
	}
	return time.Now().Before(e.ExpireAt)
}

// RecordStopLoss mencatat satu kejadian stop loss untuk pair ini.
// Jika SL terjadi >= blacklistSLThreshold dalam blacklistSLWindow, token diblokir.
func (b *Blacklist) RecordStopLoss(pairAddress, symbol string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	e, ok := b.entries[pairAddress]
	if !ok {
		e = &BlacklistEntry{
			PairAddress: pairAddress,
			Symbol:      symbol,
		}
		b.entries[pairAddress] = e
	}

	// Reset counter jika SL terakhir sudah melewati window
	if now.Sub(e.LastSL) > blacklistSLWindow {
		e.SLCount = 0
	}

	e.SLCount++
	e.LastSL = now
	e.Symbol = symbol

	if e.SLCount >= blacklistSLThreshold {
		e.ExpireAt = now.Add(blacklistDuration)
		e.Reason = "stop loss berulang"
		logger.Printf("[blacklist] 🚫 %s diblokir selama %v (SL ke-%d)",
			symbol, blacklistDuration, e.SLCount)
	}
}

// Load memuat entri dari snapshot yang tersimpan.
func (b *Blacklist) Load(entries map[string]*BlacklistEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for addr, e := range entries {
		if now.Before(e.ExpireAt) { // hanya muat yang masih berlaku
			b.entries[addr] = e
		}
	}
}

// Snapshot mengembalikan salinan semua entri untuk persistensi.
func (b *Blacklist) Snapshot() map[string]*BlacklistEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make(map[string]*BlacklistEntry, len(b.entries))
	for k, v := range b.entries {
		cp[k] = v
	}
	return cp
}

// All mengembalikan semua entri aktif untuk tampilan API.
func (b *Blacklist) All() []*BlacklistEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	var out []*BlacklistEntry
	for _, e := range b.entries {
		if now.Before(e.ExpireAt) {
			out = append(out, e)
		}
	}
	return out
}

// Cleanup menghapus entri yang sudah kadaluarsa dari memori.
func (b *Blacklist) Cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for addr, e := range b.entries {
		// Hapus entri > 48 jam (window + buffer)
		if now.Sub(e.ExpireAt) > 48*time.Hour {
			delete(b.entries, addr)
		}
	}
}
