package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const dexScreenerURL = "https://api.dexscreener.com/latest/dex/search?q=aerodrome"

type Fetcher struct {
	client    *http.Client
	out       chan<- []DexPair
	failCount int           // jumlah error berturut-turut
	backoff   time.Duration // backoff saat ini
}

const (
	backoffBase = 10 * time.Second
	backoffMax  = 5 * time.Minute
)

func NewFetcher(out chan<- []DexPair) *Fetcher {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
		out: out,
	}
}

func (f *Fetcher) Run(stop <-chan struct{}, stats *StatsCounter) {
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Jika dalam kondisi backoff, tunggu dulu
			if f.backoff > 0 {
				logger.Printf("[fetcher] ⏳ backoff %.0fs setelah %d error berturut-turut",
					f.backoff.Seconds(), f.failCount)
				select {
				case <-time.After(f.backoff):
				case <-stop:
					return
				}
			}

			pairs, err := f.fetch()
			if err != nil {
				f.failCount++
				f.backoff = f.nextBackoff()
				logger.Printf("[fetcher] error (attempt %d): %v — backoff %.0fs",
					f.failCount, err, f.backoff.Seconds())
				continue
			}

			// Sukses — reset backoff
			if f.failCount > 0 {
				logger.Printf("[fetcher] ✅ Terhubung kembali setelah %d error", f.failCount)
			}
			f.failCount = 0
			f.backoff = 0
			stats.IncrCycle()

			count := len(pairs)
			// Adaptive polling: aktivitas tinggi → interval lebih pendek
			if count > 200 {
				interval = 3 * time.Second
			} else {
				interval = 8 * time.Second
			}
			ticker.Reset(interval)
			stats.SetPollInterval(interval)

			select {
			case f.out <- pairs:
			default:
				// Drop jika pipeline sudah penuh
			}
		}
	}
}

// nextBackoff menghitung durasi backoff berikutnya (exponential, max backoffMax).
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

func (f *Fetcher) fetch() ([]DexPair, error) {
	req, err := http.NewRequest(http.MethodGet, dexScreenerURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Tangani rate limit (HTTP 429) — kembalikan sebagai error khusus
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			return nil, fmt.Errorf("rate limit 429 (Retry-After: %s)", retryAfter)
		}
		return nil, fmt.Errorf("rate limit 429 — terlalu banyak request")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result DexResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Filter: chainId == "base" && dexId == "aerodrome"
	filtered := result.Pairs[:0]
	for i := range result.Pairs {
		p := &result.Pairs[i]
		if p.ChainID == "base" && p.DexID == "aerodrome" {
			filtered = append(filtered, *p)
		}
	}
	return filtered, nil
}
