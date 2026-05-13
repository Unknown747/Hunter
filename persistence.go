package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const stateFile = "state.json"

// SaveState menyimpan posisi, trade log, dan blacklist ke disk.
func SaveState(pm *PositionManager, bl *Blacklist) error {
	pm.mu.RLock()
	state := PersistedState{
		Positions: pm.positions,
		Trades:    pm.trades,
		Blacklist: bl.Snapshot(),
		SavedAt:   time.Now(),
	}
	pm.mu.RUnlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Tulis ke file temp dulu, lalu rename — atomic agar tidak korup
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile)
}

// LoadState memuat state dari disk ke PositionManager dan Blacklist.
// Mengembalikan nil jika file tidak ada (fresh start).
func LoadState(pm *PositionManager, bl *Blacklist) error {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // fresh start, bukan error
		}
		return err
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	pm.mu.Lock()
	// Muat posisi — hanya yang masih OPEN (posisi CLOSED sudah ada di trades)
	for id, pos := range state.Positions {
		pm.positions[id] = pos
	}
	// Muat trade log
	pm.trades = state.Trades
	pm.mu.Unlock()

	// Muat blacklist
	if len(state.Blacklist) > 0 {
		bl.Load(state.Blacklist)
	}

	age := time.Since(state.SavedAt).Round(time.Second)
	openCount := 0
	for _, p := range state.Positions {
		if p.Status == PositionOpen {
			openCount++
		}
	}
	logger.Printf("[persist] ✅ State dimuat: %d posisi (%d open), %d trade, disimpan %v yang lalu",
		len(state.Positions), openCount, len(state.Trades), age)

	return nil
}

// stateFilePath mengembalikan path absolut ke state.json,
// menggunakan direktori yang sama dengan binary saat ini.
func stateFilePath() string {
	exe, err := os.Executable()
	if err != nil {
		return stateFile
	}
	return filepath.Join(filepath.Dir(exe), stateFile)
}
