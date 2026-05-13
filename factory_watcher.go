package main

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "math/big"
        "net/http"
        "os"
        "strings"
        "time"

        "github.com/ethereum/go-ethereum"
        "github.com/ethereum/go-ethereum/common"
        ethtypes "github.com/ethereum/go-ethereum/core/types"
        "github.com/ethereum/go-ethereum/ethclient"
)

const (
        // Aerodrome factory contracts di Base
        aeroV2Factory = "0x420DD381b31aEf6683db6B902084cB0FFECe40D"
        aeroCLFactory = "0x5e7BB104d84c7CB9B682AaC2F3d509f5F406809A"

        // Base rata-rata ~2 detik/block → 3600 block ≈ 2 jam
        watcherLookbackBlocks = 3600
        watcherPollSeconds    = 10
)

func getEnv(key, fallback string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return fallback
}

// RunFactoryWatcher memantau factory Aerodrome untuk event PairCreated/PoolCreated.
// Setiap pair baru yang ditemukan on-chain akan di-fetch dari DexScreener
// dan diinjeksikan ke rawPairs pipeline.
func RunFactoryWatcher(rawPairs chan<- []DexPair) {
        rpcURL := getEnv("BASE_RPC_URL", "https://mainnet.base.org")
        for {
                if err := factoryWatchLoop(rpcURL, rawPairs); err != nil {
                        logger.Printf("[factory] ⚠️  RPC terputus: %v — reconnect 30s", err)
                        time.Sleep(30 * time.Second)
                }
        }
}

func factoryWatchLoop(rpcURL string, rawPairs chan<- []DexPair) error {
        ctx := context.Background()
        client, err := ethclient.DialContext(ctx, rpcURL)
        if err != nil {
                return fmt.Errorf("dial: %w", err)
        }
        defer client.Close()

        factories := []common.Address{
                common.HexToAddress(aeroV2Factory),
                common.HexToAddress(aeroCLFactory),
        }

        head, err := client.BlockNumber(ctx)
        if err != nil {
                return fmt.Errorf("blockNumber awal: %w", err)
        }

        var lastBlock uint64
        if head > watcherLookbackBlocks {
                lastBlock = head - watcherLookbackBlocks
        }

        logger.Printf("[factory] 🔍 Monitoring Aerodrome factories dari block %d (head=%d, ~%.0f jam ke belakang)",
                lastBlock, head, float64(watcherLookbackBlocks)*2/3600)

        httpClient := &http.Client{Timeout: 12 * time.Second}

        for {
                time.Sleep(watcherPollSeconds * time.Second)

                ctxPoll, cancel := context.WithTimeout(ctx, 10*time.Second)
                head, err = client.BlockNumber(ctxPoll)
                cancel()
                if err != nil {
                        return fmt.Errorf("blockNumber poll: %w", err)
                }

                if head <= lastBlock {
                        continue
                }

                from := lastBlock + 1
                to := head
                // Cap range agar tidak timeout saat ada backlog besar
                if to-from > 1000 {
                        from = to - 1000
                }

                ctxLogs, cancelLogs := context.WithTimeout(ctx, 15*time.Second)
                logs, err := client.FilterLogs(ctxLogs, ethereum.FilterQuery{
                        FromBlock: new(big.Int).SetUint64(from),
                        ToBlock:   new(big.Int).SetUint64(to),
                        Addresses: factories,
                })
                cancelLogs()

                if err != nil {
                        logger.Printf("[factory] FilterLogs error (block %d-%d): %v", from, to, err)
                        lastBlock = to
                        continue
                }

                lastBlock = to

                for _, vlog := range logs {
                        pairAddr := decodePairAddress(vlog)
                        if pairAddr == "" {
                                continue
                        }
                        logger.Printf("[factory] 🆕 PAIR BARU on-chain: %s (block %d) — menunggu DexScreener index...",
                                pairAddr, vlog.BlockNumber)
                        go injectPairFromDex(pairAddr, httpClient, rawPairs)
                }
        }
}

// decodePairAddress mengekstrak alamat pair/pool dari event log factory.
//
// Aerodrome v2 PairCreated(address indexed token0, address indexed token1, bool stable, address pair, uint):
//   - 3 topics: [sig, token0, token1]
//   - data: stable(32) + pair(32) + idx(32) → pair address di data[44:64]
//
// Aerodrome CL PoolCreated(address indexed token0, address indexed token1, int24 indexed tickSpacing, address pool):
//   - 4 topics: [sig, token0, token1, tickSpacing]
//   - data: pool(32) → pool address di data[12:32]
func decodePairAddress(vlog ethtypes.Log) string {
        switch {
        case len(vlog.Topics) == 3 && len(vlog.Data) >= 64:
                // v2: pair address ada di word ke-2 dalam data (offset 32..63)
                addr := common.BytesToAddress(vlog.Data[44:64])
                return strings.ToLower(addr.Hex())
        case len(vlog.Topics) == 4 && len(vlog.Data) >= 32:
                // CL: pool address ada di word pertama dalam data (offset 0..31)
                addr := common.BytesToAddress(vlog.Data[12:32])
                return strings.ToLower(addr.Hex())
        }
        return ""
}

// injectPairFromDex menunggu DexScreener mengindeks pair baru, lalu inject ke pipeline.
// DexScreener biasanya butuh 30–180 detik setelah pair dibuat on-chain.
func injectPairFromDex(pairAddr string, httpClient *http.Client, rawPairs chan<- []DexPair) {
        url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/pairs/base/%s", pairAddr)

        delays := []time.Duration{45, 30, 30, 60, 60, 60}
        var waited time.Duration

        for i, delay := range delays {
                time.Sleep(delay * time.Second)
                waited += delay

                resp, err := httpClient.Get(url)
                if err != nil {
                        continue
                }
                body, _ := io.ReadAll(resp.Body)
                resp.Body.Close()

                var result struct {
                        Pair *DexPair `json:"pair"`
                }
                if err := json.Unmarshal(body, &result); err != nil || result.Pair == nil {
                        logger.Printf("[factory] Pair %s belum ter-index (attempt %d, sudah %.0fs)", pairAddr, i+1, waited.Seconds())
                        continue
                }

                p := result.Pair
                if p.ChainID != "base" {
                        return
                }

                logger.Printf("[factory] ✅ %s/%s masuk pipeline! liq=$%.0f",
                        p.BaseToken.Symbol, p.QuoteToken.Symbol, p.Liquidity.Usd)

                select {
                case rawPairs <- []DexPair{*p}:
                case <-time.After(5 * time.Second):
                        logger.Printf("[factory] channel penuh, drop pair %s", pairAddr)
                }
                return
        }

        logger.Printf("[factory] ⚠️  Pair %s tidak ter-index DexScreener setelah %.0f menit", pairAddr, waited.Minutes())
}
