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
	// Sumber utama: price monitoring pair Aerodrome yang sudah diketahui.
	// Endpoint ini dipakai untuk update harga token yang sudah di-cache
	// dan monitoring posisi terbuka — BUKAN untuk discovery koin baru.
	// Discovery koin baru ditangani oleh RunFactoryWatcher (factory_watcher.go).
	urlSearchAerodrome = "https://api.dexscreener.com/latest/dex/search?q=aerodrome+base"

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

// Run menjalankan polling harga untuk pair Aerodrome yang sudah ada di cache.
// Koin baru ditemukan oleh RunFactoryWatcher — bukan di sini.
func (f *Fetcher) Run(stop <-chan struct{}, stats *StatsCounter) {
	interval := 8 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Println("[fetcher] 🔄 Price monitor aktif (koin baru ditangani factory_watcher)")

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

			pairs, err := f.fetchSearch(urlSearchAerodrome)
			if err != nil {
				f.failCount++
				f.backoff = f.nextBackoff()
				logger.Printf("[fetcher] error (attempt %d): %v — backoff %.0fs",
					f.failCount, err, f.backoff.Seconds())
				continue
			}

			if f.failCount > 0 {
				logger.Printf("[fetcher] ✅ Terhubung kembali setelah %d error", f.failCount)
			}
			f.failCount = 0
			f.backoff = 0
			stats.IncrCycle()

			count := len(pairs)
			if count > 200 {
				interval = 5 * time.Second
			} else {
				interval = 8 * time.Second
			}
			ticker.Reset(interval)
			stats.SetPollInterval(interval)

			if len(pairs) > 0 {
				select {
				case f.out <- pairs:
				default:
				}
			}
		}
	}
}

// fetchSearch melakukan GET ke URL DexScreener dan mengembalikan pair
// yang difilter: chainId=base & dexId=aerodrome.
func (f *Fetcher) fetchSearch(url string) ([]DexPair, error) {
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

	filtered := result.Pairs[:0]
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.ChainID == "base" && p.DexID == "aerodrome" {
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

// DexTokenProfile — masih dipakai oleh types lain jika ada referensi
type DexTokenProfile struct {
	ChainID      string `json:"chainId"`
	TokenAddress string `json:"tokenAddress"`
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
