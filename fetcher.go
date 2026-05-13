package main

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"
)

// ─── Endpoint URLs ─────────────────────────────────────────────────────────────

const (
        // Price monitor 1: update harga pair Aerodrome yang sudah ada di cache.
        urlSearchAerodrome = "https://api.dexscreener.com/latest/dex/search?q=aerodrome+base"

        // Price monitor 2: update harga pair Uniswap V3 di Base.
        // Uniswap V3 adalah DEX paling aktif untuk meme coin baru di Base.
        urlSearchUniswap = "https://api.dexscreener.com/latest/dex/search?q=uniswap+base"

        // Fetch pair data by pair address (dipakai factory watcher)
        urlPairsBase = "https://api.dexscreener.com/latest/dex/pairs/base/"
)

// ─── Fetcher ───────────────────────────────────────────────────────────────────

type Fetcher struct {
        client    *http.Client
        out       chan<- []DexPair
        failCount int
        backoff   time.Duration
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
                out: out,
        }
}

// Run menjalankan polling harga untuk pair Aerodrome + Uniswap V3 yang sudah ada di cache.
// Koin baru ditemukan oleh RunFactoryWatcher (on-chain) — bukan di sini.
func (f *Fetcher) Run(stop <-chan struct{}, stats *StatsCounter) {
        interval := 8 * time.Second
        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        logger.Println("[fetcher] 🔄 Price monitor aktif — Aerodrome + Uniswap V3 (koin baru via factory_watcher)")

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

                        // Poll Aerodrome pairs
                        aero, err := f.fetchSearch(urlSearchAerodrome, "aerodrome")
                        if err != nil {
                                f.failCount++
                                f.backoff = f.nextBackoff()
                                logger.Printf("[fetcher] error Aerodrome (attempt %d): %v — backoff %.0fs",
                                        f.failCount, err, f.backoff.Seconds())
                                continue
                        }

                        // Poll Uniswap V3 pairs (tidak block jika gagal)
                        uni, _ := f.fetchSearch(urlSearchUniswap, "uniswap")

                        allPairs := append(aero, uni...)

                        if f.failCount > 0 {
                                logger.Printf("[fetcher] ✅ Terhubung kembali setelah %d error", f.failCount)
                        }
                        f.failCount = 0
                        f.backoff = 0
                        stats.IncrCycle()

                        count := len(allPairs)
                        if count > 200 {
                                interval = 5 * time.Second
                        } else {
                                interval = 8 * time.Second
                        }
                        ticker.Reset(interval)
                        stats.SetPollInterval(interval)

                        if len(allPairs) > 0 {
                                select {
                                case f.out <- allPairs:
                                default:
                                }
                        }
                }
        }
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
