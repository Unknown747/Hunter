package main

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "sync"
        "time"
)

// ─── Endpoint URLs ─────────────────────────────────────────────────────────────

const (
        // Price monitor 1: update harga pair Aerodrome yang sudah ada di cache.
        urlSearchAerodrome = "https://api.dexscreener.com/latest/dex/search?q=aerodrome+base"

        // Price monitor 2: update harga pair Uniswap V3 di Base.
        urlSearchUniswap = "https://api.dexscreener.com/latest/dex/search?q=uniswap+base"

        // Fetch pair data by pair address (dipakai factory watcher)
        urlPairsBase = "https://api.dexscreener.com/latest/dex/pairs/base/"

        // Fetch semua pair untuk token address tertentu
        urlTokenPairsBase = "https://api.dexscreener.com/latest/dex/tokens/base/"

        // Token profiles terbaru dari DexScreener — sumber listing baru
        urlTokenProfiles = "https://api.dexscreener.com/token-profiles/latest/v1"

        // Batas umur pair yang masih dianggap "baru" untuk filter profil (45 menit — sinkron dengan filter.go)
        maxNewPairAgeHours = 0.75
)

// ─── Fetcher ───────────────────────────────────────────────────────────────────

type Fetcher struct {
        client    *http.Client
        out       chan<- []DexPair
        failCount int
        backoff   time.Duration

        // Rotasi kata kunci meme — index keyword berikutnya
        memeIdx int

        // Dedup profiles: catat token address yang sudah pernah di-fetch
        // agar tidak spam ke API untuk token yang sama setiap menit
        profilesSeen   map[string]time.Time
        profilesSeenMu sync.Mutex
}

const (
        backoffBase = 10 * time.Second
        backoffMax  = 5 * time.Minute
)

func NewFetcher(out chan<- []DexPair) *Fetcher {
        transport := &http.Transport{
                MaxIdleConns:      10,
                IdleConnTimeout:   90 * time.Second,
                ForceAttemptHTTP2: true,
        }
        return &Fetcher{
                client: &http.Client{
                        Transport: transport,
                        Timeout:   15 * time.Second,
                },
                out:          out,
                profilesSeen: make(map[string]time.Time),
        }
}

// Run menjalankan empat sumber data secara paralel:
//  1. Price monitor utama (Aerodrome + Uniswap) — setiap 5-8 detik
//  2. New listing scanner (rotasi 8 query DexScreener) — setiap 15 detik
//  3. Token profiles watcher (listing baru DexScreener) — setiap 20 detik
//  4. Factory watcher (on-chain PairCreated events) — dijalankan dari main.go
func (f *Fetcher) Run(stop <-chan struct{}, stats *StatsCounter) {
        logger.Println("[fetcher] 🔄 Price monitor aktif — Aerodrome + Uniswap V3 (setiap 5-8s)")
        logger.Println("[fetcher] 🆕 New listing scanner aktif — rotasi query, filter < 45m (setiap 15s)")
        logger.Println("[fetcher] 🔍 Token profiles watcher aktif — listing baru DexScreener (setiap 20s)")

        // Goroutine 2: new listing scanner (ganti keyword rotation yang tidak efektif)
        go f.runNewListingScanner(stop)

        // Goroutine 3: token profiles watcher (dipercepat 60s → 20s)
        go f.runProfilesWatcher(stop)

        // Goroutine utama: price monitor Aerodrome + Uniswap
        interval := 8 * time.Second
        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        for {
                select {
                case <-stop:
                        return

                case <-ticker.C:
                        if f.backoff > 0 {
                                logger.Printf("[fetcher] ⏳ backoff %.0fs setelah %d error", f.backoff.Seconds(), f.failCount)
                                select {
                                case <-time.After(f.backoff):
                                case <-stop:
                                        return
                                }
                        }

                        aero, err := f.fetchSearch(urlSearchAerodrome, "aerodrome")
                        if err != nil {
                                f.failCount++
                                f.backoff = f.nextBackoff()
                                logger.Printf("[fetcher] error Aerodrome (attempt %d): %v — backoff %.0fs",
                                        f.failCount, err, f.backoff.Seconds())
                                continue
                        }

                        uni, _ := f.fetchSearch(urlSearchUniswap, "uniswap")

                        allPairs := append(aero, uni...)

                        if f.failCount > 0 {
                                logger.Printf("[fetcher] ✅ Terhubung kembali setelah %d error", f.failCount)
                        }
                        f.failCount = 0
                        f.backoff = 0
                        stats.IncrCycle()

                        if len(allPairs) > 200 {
                                interval = 5 * time.Second
                        } else {
                                interval = 8 * time.Second
                        }
                        ticker.Reset(interval)
                        stats.SetPollInterval(interval)

                        f.send(allPairs)
                }
        }
}

// runNewListingScanner polling listing BARU di Base setiap 15 detik.
// Strategi: cari pair di Base yang baru dibuat, diurutkan berdasarkan waktu terbaru.
// Ini jauh lebih efektif dari keyword rotation karena langsung target pair baru.
func (f *Fetcher) runNewListingScanner(stop <-chan struct{}) {
        // Tunda sedikit agar tidak bentrok dengan startup
        select {
        case <-stop:
                return
        case <-time.After(5 * time.Second):
        }

        ticker := time.NewTicker(15 * time.Second)
        defer ticker.Stop()

        // Rotasi beberapa query untuk mendapatkan coverage lebih luas dari pair baru
        queries := []string{
                "https://api.dexscreener.com/latest/dex/search?q=base+new",
                "https://api.dexscreener.com/latest/dex/search?q=aerodrome+base",
                "https://api.dexscreener.com/latest/dex/search?q=uniswap+v3+base",
                "https://api.dexscreener.com/latest/dex/search?q=meme+base",
                "https://api.dexscreener.com/latest/dex/search?q=pepe+base",
                "https://api.dexscreener.com/latest/dex/search?q=inu+base",
                "https://api.dexscreener.com/latest/dex/search?q=doge+base",
                "https://api.dexscreener.com/latest/dex/search?q=moon+base",
        }
        queryIdx := 0

        for {
                select {
                case <-stop:
                        return
                case <-ticker.C:
                        url := queries[queryIdx%len(queries)]
                        queryIdx++

                        pairs, err := f.fetchSearchBase(url)
                        if err != nil {
                                logger.Printf("[fetcher/new] error scan: %v", err)
                                continue
                        }

                        // Filter ketat: hanya pair berumur < 45 menit
                        fresh := filterFreshPairs(pairs)
                        if len(fresh) > 0 {
                                logger.Printf("[fetcher/new] 🆕 %d pair segar (< 45m) dari %d hasil — masuk pipeline",
                                        len(fresh), len(pairs))
                                f.send(fresh)
                        }
                }
        }
}

// runProfilesWatcher polling endpoint token-profiles DexScreener setiap 20 detik.
// Ini adalah sumber terbaik untuk listing baru karena DexScreener secara aktif
// melacak token yang baru di-submit/listed. Hanya token di Base yang diproses.
func (f *Fetcher) runProfilesWatcher(stop <-chan struct{}) {
        // Tunda 15 detik saat startup agar tidak bentrok dengan inisialisasi
        select {
        case <-stop:
                return
        case <-time.After(15 * time.Second):
        }

        ticker := time.NewTicker(20 * time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-stop:
                        return
                case <-ticker.C:
                        f.pollProfiles()
                }
        }
}

// pollProfiles mengambil daftar token profil terbaru dan mem-fetch pair data-nya.
func (f *Fetcher) pollProfiles() {
        resp, err := f.client.Get(urlTokenProfiles)
        if err != nil {
                logger.Printf("[fetcher/profiles] error fetch: %v", err)
                return
        }
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusTooManyRequests {
                logger.Println("[fetcher/profiles] ⚠️  rate limit 429 — skip siklus ini")
                return
        }
        if resp.StatusCode != http.StatusOK {
                return
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return
        }

        var profiles []struct {
                ChainID      string `json:"chainId"`
                TokenAddress string `json:"tokenAddress"`
        }
        if err := json.Unmarshal(body, &profiles); err != nil {
                return
        }

        // Ambil token Base yang belum pernah kita fetch, maks 5 per siklus
        // agar tidak spam ke API saat ada banyak listing baru sekaligus
        const maxPerCycle = 5
        fetched := 0
        now := time.Now()

        f.profilesSeenMu.Lock()
        // Bersihkan entri lama (> 3 jam) dari map dedup
        for addr, seenAt := range f.profilesSeen {
                if now.Sub(seenAt) > 3*time.Hour {
                        delete(f.profilesSeen, addr)
                }
        }

        var toFetch []string
        for _, p := range profiles {
                if p.ChainID != "base" || p.TokenAddress == "" {
                        continue
                }
                addr := strings.ToLower(p.TokenAddress)
                if _, seen := f.profilesSeen[addr]; seen {
                        continue
                }
                f.profilesSeen[addr] = now
                toFetch = append(toFetch, addr)
                if len(toFetch) >= maxPerCycle {
                        break
                }
        }
        f.profilesSeenMu.Unlock()

        if len(toFetch) == 0 {
                logger.Printf("[fetcher/profiles] 🔍 Cek profiles — tidak ada token Base baru (sudah %d terlihat sebelumnya)",
                        len(f.profilesSeen))
                return
        }

        logger.Printf("[fetcher/profiles] 🆕 %d token Base baru ditemukan di profiles", len(toFetch))

        for _, tokenAddr := range toFetch {
                pairs, err := f.fetchTokenPairs(tokenAddr)
                if err != nil || len(pairs) == 0 {
                        continue
                }

                fresh := filterFreshPairs(pairs)
                if len(fresh) > 0 {
                        logger.Printf("[fetcher/profiles] ✅ Token %s...%s → %d pair baru (umur < 2j)",
                                tokenAddr[:6], tokenAddr[len(tokenAddr)-4:], len(fresh))
                        f.send(fresh)
                        fetched++
                }

                // Jeda kecil antar request untuk hindari rate limit
                time.Sleep(500 * time.Millisecond)
        }

        if fetched > 0 {
                logger.Printf("[fetcher/profiles] 📊 Total %d token berhasil diinjek ke pipeline", fetched)
        }
}

// fetchTokenPairs mengambil semua pair untuk satu token address di Base.
func (f *Fetcher) fetchTokenPairs(tokenAddr string) ([]DexPair, error) {
        url := urlTokenPairsBase + strings.ToLower(tokenAddr)
        resp, err := f.client.Get(url)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusTooManyRequests {
                return nil, fmt.Errorf("rate limit 429")
        }
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("status %d", resp.StatusCode)
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, err
        }

        var result DexResponse
        if err := json.Unmarshal(body, &result); err != nil {
                return nil, err
        }

        var filtered []DexPair
        for i := range result.Pairs {
                p := &result.Pairs[i]
                if p.ChainID == "base" {
                        filtered = append(filtered, *p)
                }
        }
        return filtered, nil
}

// fetchSearch melakukan GET ke URL DexScreener dan mengembalikan pair
// yang difilter: chainId=base & dexId sesuai parameter.
func (f *Fetcher) fetchSearch(url string, dexID string) ([]DexPair, error) {
        req, err := http.NewRequest(http.MethodGet, url, nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("Accept", "application/json")

        resp, err := f.client.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusTooManyRequests {
                return nil, fmt.Errorf("rate limit 429")
        }
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("status %d", resp.StatusCode)
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, err
        }

        var result DexResponse
        if err := json.Unmarshal(body, &result); err != nil {
                return nil, err
        }

        var filtered []DexPair
        for i := range result.Pairs {
                p := &result.Pairs[i]
                if p.ChainID == "base" && strings.HasPrefix(p.DexID, dexID) {
                        filtered = append(filtered, *p)
                }
        }
        return filtered, nil
}

// fetchSearchBase seperti fetchSearch tapi tanpa filter dexID —
// mengembalikan semua pair di Base dari hasil pencarian apapun.
func (f *Fetcher) fetchSearchBase(url string) ([]DexPair, error) {
        req, err := http.NewRequest(http.MethodGet, url, nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("Accept", "application/json")

        resp, err := f.client.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusTooManyRequests {
                return nil, fmt.Errorf("rate limit 429")
        }
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("status %d", resp.StatusCode)
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, err
        }

        var result DexResponse
        if err := json.Unmarshal(body, &result); err != nil {
                return nil, err
        }

        var filtered []DexPair
        for i := range result.Pairs {
                p := &result.Pairs[i]
                if p.ChainID == "base" {
                        filtered = append(filtered, *p)
                }
        }
        return filtered, nil
}

// filterFreshPairs menyaring pair yang berumur < maxNewPairAgeHours.
// Pair dengan pairCreatedAt=0 (tidak diketahui) dilewati — bukan koin baru.
func filterFreshPairs(pairs []DexPair) []DexPair {
        now := time.Now()
        var fresh []DexPair
        for i := range pairs {
                p := &pairs[i]
                if p.PairCreatedAt <= 0 {
                        continue
                }
                created := time.UnixMilli(p.PairCreatedAt)
                ageHours := now.Sub(created).Hours()
                if ageHours <= maxNewPairAgeHours {
                        fresh = append(fresh, *p)
                }
        }
        return fresh
}

// send mengirim batch pair ke pipeline, non-blocking.
func (f *Fetcher) send(pairs []DexPair) {
        if len(pairs) == 0 {
                return
        }
        select {
        case f.out <- pairs:
        default:
        }
}

func (f *Fetcher) nextBackoff() time.Duration {
        if f.backoff == 0 {
                return backoffBase
        }
        next := f.backoff * 2
        if next > backoffMax {
                return backoffMax
        }
        return next
}

// fetchPairByAddress digunakan oleh factory watcher untuk mengambil data pair
// berdasarkan alamat kontrak pair-nya.
func fetchPairByAddress(httpClient *http.Client, pairAddr string) (*DexPair, error) {
        url := urlPairsBase + strings.ToLower(pairAddr)
        resp, err := httpClient.Get(url)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, err
        }

        var result struct {
                Pair *DexPair `json:"pair"`
        }
        if err := json.Unmarshal(body, &result); err != nil {
                return nil, err
        }
        return result.Pair, nil
}
