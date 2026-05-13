package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	rawPairs := make(chan []DexPair, 2)

	// stop ditutup saat menerima sinyal OS untuk graceful shutdown
	stop := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		logger.Println("Sinyal shutdown diterima, menghentikan goroutine...")
		close(stop)
	}()

	fetcher := NewFetcher(rawPairs)
	go fetcher.Run(stop, stats)
	go runPipeline(rawPairs, cache, pm)
	go cache.Cleanup(stop)

	server := NewAPIServer(cache, stats, pm)
	addr := ":" + port
	logger.Printf("Base Meme Coin Hunter — LIVE — http://localhost%s", addr)

	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		logger.Fatalf("server: %v", err)
	}
}

func runPipeline(in <-chan []DexPair, cache *Cache, pm *PositionManager) {
	for pairs := range in {
		for i := range pairs {
			p := &pairs[i]
			t := Normalize(p)
			if !Filter(t) {
				continue
			}
			t.Score = Score(t)
			t.Category = Categorize(t.Score)
			priorState, _ := cache.Upsert(t)
			pm.OnTokenUpdate(t, &priorState)
		}
	}
}
