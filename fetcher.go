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
	// Sumber 1: pair populer Aerodrome (volume tinggi)
	urlSearchAerodrome = "https://api.dexscreener.com/latest/dex/search?q=aerodrome"

	// Sumber 2: listing terbaru di Base (dari DexScreener token-profiles)
	urlTokenProfiles = "https://api.dexscreener.com/token-profiles/latest/v1"

	// Sumber 3: token yang sedang di-boost (sering koin baru yang beriklan)
	urlTokenBoosts = "https://api.dexscreener.com/token-boosts/latest/v1"

	// Fetch pair data untuk daftar token address (max 30 per request)
	urlTokensBase = "https://api.dexscreener.com/latest/dex/tokens/"
)

// ─── Types untuk endpoint baru ─────────────────────────────────────────────────

type DexTokenProfile struct {
	ChainID      string `json:"chainId"`
	TokenAddress string `json:"tokenAddress"`
}

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
		MaxIdleConns:       10,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
		out: out,
	}
}

func (f *Fetcher) Run(stop <-chan struct{}, stats *StatsCounter) {
	// Sumber 1: polling cepat — pair populer Aerodrome
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Sumber 2+3: polling lambat — listing baru (setiap 30 detik)
	newListingTicker := time.NewTicker(30 * time.Second)
	defer newListingTicker.Stop()

	// Jalankan fetch listing baru sekali di awal
	go f.fetchAndSendNewListings()

	for {
		select {
		case <-stop:
			return

		case <-newListingTicker.C:
			go f.fetchAndSendNewListings()

		case <-ticker.C:
			if f.backoff > 0 {
				logger.Printf("[fetcher] ⏳ backoff %.0fs setelah %d error berturut-turut",
					f.backoff.Seconds(), f.failCount)
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
				interval = 3 * time.Second
			} else {
				interval = 8 * time.Second
			}
			ticker.Reset(interval)
			stats.SetPollInterval(interval)

			select {
			case f.out <- pairs:
			default:
			}
		}
	}
}

// fetchAndSendNewListings mengambil listing terbaru dari token-profiles dan
// token-boosts, lalu meneruskan pair Aerodrome Base yang ditemukan ke pipeline.
func (f *Fetcher) fetchAndSendNewListings() {
	addrs := f.collectNewBaseTokenAddresses()
	if len(addrs) == 0 {
		return
	}

	pairs := f.fetchPairsForTokens(addrs)
	if len(pairs) == 0 {
		return
	}

	logger.Printf("[fetcher] 🆕 %d pair baru dari token-profiles/boosts (Base+Aerodrome)", len(pairs))
	select {
	case f.out <- pairs:
	default:
	}
}

// collectNewBaseTokenAddresses mengambil daftar token address Base dari
// token-profiles dan token-boosts (dedup, max 30).
func (f *Fetcher) collectNewBaseTokenAddresses() []string {
	seen := make(map[string]bool)
	var addrs []string

	for _, url := range []string{urlTokenProfiles, urlTokenBoosts} {
		profiles, err := f.fetchTokenProfiles(url)
		if err != nil {
			continue
		}
		for _, p := range profiles {
			if strings.ToLower(p.ChainID) != "base" {
				continue
			}
			addr := strings.ToLower(p.TokenAddress)
			if addr == "" || seen[addr] {
				continue
			}
			seen[addr] = true
			addrs = append(addrs, addr)
			if len(addrs) >= 30 {
				return addrs
			}
		}
	}
	return addrs
}

// fetchTokenProfiles mengambil daftar token dari endpoint token-profiles/boosts.
func (f *Fetcher) fetchTokenProfiles(url string) ([]DexTokenProfile, error) {
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d dari %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var profiles []DexTokenProfile
	if err := json.Unmarshal(body, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// fetchPairsForTokens mengambil pair data untuk daftar token address (batch),
// lalu filter hanya pair Aerodrome di Base.
func (f *Fetcher) fetchPairsForTokens(addrs []string) []DexPair {
	// Batch: DexScreener mendukung comma-separated addresses
	const batchSize = 30
	var result []DexPair
	seen := make(map[string]bool)

	for i := 0; i < len(addrs); i += batchSize {
		end := i + batchSize
		if end > len(addrs) {
			end = len(addrs)
		}
		batch := addrs[i:end]
		url := urlTokensBase + strings.Join(batch, ",")

		pairs, err := f.fetchSearch(url)
		if err != nil {
			continue
		}
		for _, p := range pairs {
			key := strings.ToLower(p.PairAddress)
			if !seen[key] {
				seen[key] = true
				result = append(result, p)
			}
		}

		// Jangan terlalu cepat hit rate limit DexScreener
		if end < len(addrs) {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return result
}

// fetchSearch melakukan GET ke URL DexScreener dan mengembalikan pair
// yang sudah difilter: chainId=base & dexId=aerodrome.
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
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			return nil, fmt.Errorf("rate limit 429 (Retry-After: %s)", retryAfter)
		}
		return nil, fmt.Errorf("rate limit 429")
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
