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
        client *http.Client
        out    chan<- []DexPair
}

func NewFetcher(out chan<- []DexPair) *Fetcher {
        transport := &http.Transport{
                MaxIdleConns:        10,
                IdleConnTimeout:     90 * time.Second,
                DisableCompression:  false,
                ForceAttemptHTTP2:   true,
        }
        return &Fetcher{
                client: &http.Client{
                        Transport: transport,
                        Timeout:   3 * time.Second,
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
                        pairs, err := f.fetch()
                        if err != nil {
                                logger.Printf("[fetcher] error: %v", err)
                                continue
                        }
                        stats.IncrCycle()
                        count := len(pairs)
                        // Adaptive polling: high activity → shorter interval
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
                                // Drop if pipeline is backed up
                        }
                }
        }
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
