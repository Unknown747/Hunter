package main

import (
        "encoding/json"
        "errors"
        "os"
        "time"
)

const stateFile  = "state.json"
const configFile = "config.json"

// SaveConfig menyimpan StrategyConfig ke config.json secara atomic.
// Dipanggil otomatis setiap kali config diubah via Telegram /config.
func SaveConfig(cfg StrategyConfig) error {
        data, err := json.MarshalIndent(cfg, "", "  ")
        if err != nil {
                return err
        }
        tmp := configFile + ".tmp"
        if err := os.WriteFile(tmp, data, 0600); err != nil {
                return err
        }
        return os.Rename(tmp, configFile)
}

// LoadConfig memuat StrategyConfig dari config.json.
// Mengembalikan nil, nil jika file tidak ada (fresh start — gunakan default).
func LoadConfig() (*StrategyConfig, error) {
        data, err := os.ReadFile(configFile)
        if err != nil {
                if errors.Is(err, os.ErrNotExist) {
                        return nil, nil
                }
                return nil, err
        }
        var cfg StrategyConfig
        if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, err
        }
        return &cfg, nil
}

// SaveState menyimpan posisi, trade log, dan blacklist ke disk.
// Deep-copy dilakukan di dalam lock agar tidak ada race condition dengan
// goroutine yang menulis ke map/slice secara bersamaan.
func SaveState(pm *PositionManager, bl *Blacklist) error {
        pm.mu.RLock()
        // Deep-copy positions (map adalah reference type — harus disalin dulu)
        positions := make(map[string]*Position, len(pm.positions))
        for k, v := range pm.positions {
                cp := *v
                // Salin slice Fills agar tidak shared
                if len(v.Fills) > 0 {
                        cp.Fills = make([]Fill, len(v.Fills))
                        copy(cp.Fills, v.Fills)
                }
                positions[k] = &cp
        }
        // Deep-copy trades slice
        trades := make([]*TradeLog, len(pm.trades))
        copy(trades, pm.trades)
        pm.mu.RUnlock()

        // Marshal di luar lock — data sudah aman karena sudah disalin
        state := PersistedState{
                Positions:   positions,
                Trades:      trades,
                Blacklist:   bl.Snapshot(),
                RugPatterns: pm.rugStore.Snapshot(),
                SavedAt:     time.Now(),
        }

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
        for id, pos := range state.Positions {
                pm.positions[id] = pos
        }
        pm.trades = state.Trades
        pm.mu.Unlock()

        if len(state.Blacklist) > 0 {
                bl.Load(state.Blacklist)
        }
        if len(state.RugPatterns) > 0 {
                pm.rugStore.Load(state.RugPatterns)
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
