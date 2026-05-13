package main

import (
	"log"
	"net/http"
	"os"
)

var logger = log.New(os.Stdout, "[hunter] ", log.LstdFlags)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	stats := NewStatsCounter()
	cache := NewCache()
	cfg := DefaultConfig()
	exec := NewExecutor()
	pm := NewPositionManager(cfg, exec)

	// Channel-based pipeline
	rawPairs := make(chan []DexPair, 2)
	stop := make(chan struct{})

	// Stage 1: Fetcher
	fetcher := NewFetcher(rawPairs)
	go fetcher.Run(stop, stats)

	// Stage 2–7: Normalizer → Filter → Scorer → Signal → Cache → Trader
	go runPipeline(rawPairs, cache, pm)

	// Stage 8: TTL cleanup
	go cache.Cleanup(stop)

	// Stage 9: API Server
	server := NewAPIServer(cache, stats, pm)
	addr := ":" + port
	logger.Printf("Base Meme Coin Hunter running on http://localhost%s [mode=%s]", addr, exec.Mode())

	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

// runPipeline processes batches from the fetcher channel.
func runPipeline(in <-chan []DexPair, cache *Cache, pm *PositionManager) {
	for pairs := range in {
		for i := range pairs {
			p := &pairs[i]

			// Normalize
			t := Normalize(p)

			// Filter
			if !Filter(t) {
				continue
			}

			// Score + categorize
			t.Score = Score(t)
			t.Category = Categorize(t.Score)

			// Cache upsert — also computes VolumeSpike from prior state
			priorState, _ := cache.Upsert(t)

			// Feed into position manager (entry + exit evaluation)
			pm.OnTokenUpdate(t, &priorState)
		}
	}
}
