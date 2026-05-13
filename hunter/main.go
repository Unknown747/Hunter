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

	// Channel-based pipeline
	rawPairs := make(chan []DexPair, 2)

	stop := make(chan struct{})

	// Stage 1: Fetcher
	fetcher := NewFetcher(rawPairs)
	go fetcher.Run(stop, stats)

	// Stage 2–6: Normalizer → Filter → Scorer → Signal → Cache (pipeline worker)
	go runPipeline(rawPairs, cache)

	// Stage 7: TTL cleanup goroutine
	go cache.Cleanup(stop)

	// Stage 8: API Server
	server := NewAPIServer(cache, stats)
	addr := ":" + port
	logger.Printf("Base Meme Coin Hunter running on http://localhost%s", addr)

	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

// runPipeline processes batches from the fetcher channel.
func runPipeline(in <-chan []DexPair, cache *Cache) {
	for pairs := range in {
		for i := range pairs {
			p := &pairs[i]

			// Normalize
			t := Normalize(p)

			// Filter
			if !Filter(t) {
				continue
			}

			// Score
			t.Score = Score(t)
			t.Category = Categorize(t.Score)

			// Signal + Cache upsert
			cache.Upsert(t)
		}
	}
}
