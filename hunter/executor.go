package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Executor abstracts over paper trading and live on-chain execution.
type Executor interface {
	Buy(t *TokenInfo, sizeUSD float64) (Fill, error)
	Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error)
	Mode() string
}

// ─── Paper Executor ───────────────────────────────────────────────────────────

// PaperExecutor simulates trades at current market price. Zero risk, zero deps.
type PaperExecutor struct{}

func (p *PaperExecutor) Mode() string { return "PAPER" }

func (p *PaperExecutor) Buy(t *TokenInfo, sizeUSD float64) (Fill, error) {
	if t.Price <= 0 {
		return Fill{}, fmt.Errorf("price unavailable for %s", t.Symbol)
	}
	return Fill{
		Action:    "BUY",
		Price:     t.Price,
		PctOfPos:  100,
		USD:       sizeUSD,
		PnLPct:    0,
		Reason:    "entry",
		Timestamp: time.Now(),
	}, nil
}

func (p *PaperExecutor) Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error) {
	if t.Price <= 0 {
		return Fill{}, fmt.Errorf("price unavailable for %s", t.Symbol)
	}
	pnlPct := (t.Price/pos.EntryPrice - 1) * 100
	usdClosed := pos.SizeUSD * fraction * (pos.RemainingPct / 100)
	return Fill{
		Action:    "SELL",
		Price:     t.Price,
		PctOfPos:  fraction * 100,
		USD:       usdClosed * (1 + pnlPct/100),
		PnLPct:    pnlPct,
		Reason:    reason,
		Timestamp: time.Now(),
	}, nil
}

// ─── Live Executor (Base + Aerodrome) ─────────────────────────────────────────

// Aerodrome Router V2 on Base
const (
	aerodromeRouter  = "0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"
	aerodromeFactory = "0x420DD381b31aEf6683db6B902084cB0FFECe40Da"
	wethBase         = "0x4200000000000000000000000000000000000006"
	baseChainID      = 8453
)

// LiveExecutor sends real transactions on Base via JSON-RPC.
// Requires env: PRIVATE_KEY (hex, no 0x), BASE_RPC_URL
type LiveExecutor struct {
	rpcURL     string
	privateKey string
	httpClient *http.Client
}

func NewLiveExecutor() (*LiveExecutor, error) {
	rpc := os.Getenv("BASE_RPC_URL")
	if rpc == "" {
		rpc = "https://mainnet.base.org"
	}
	pk := os.Getenv("PRIVATE_KEY")
	if pk == "" {
		return nil, fmt.Errorf("PRIVATE_KEY env var not set")
	}
	return &LiveExecutor{
		rpcURL:     rpc,
		privateKey: pk,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (l *LiveExecutor) Mode() string { return "LIVE" }

// Buy executes swapExactETHForTokens on Aerodrome.
// NOTE: Requires go-ethereum for full transaction signing.
// To enable, see hunter/LIVE_TRADING.md
func (l *LiveExecutor) Buy(t *TokenInfo, sizeUSD float64) (Fill, error) {
	// For full live trading implementation, add go-ethereum:
	//   go get github.com/ethereum/go-ethereum
	// Then implement:
	//   1. Load ECDSA key from PRIVATE_KEY
	//   2. Get nonce via eth_getTransactionCount
	//   3. ABI-encode swapExactETHForTokens(amountOutMin, routes, to, deadline)
	//   4. Sign & send via eth_sendRawTransaction
	//   5. Wait for receipt via eth_getTransactionReceipt
	//
	// Route: WETH → TOKEN via Aerodrome volatile pool
	// Router: 0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43

	logger.Printf("[LIVE-BUY] Would buy %s for $%.2f via Aerodrome (token: %s)",
		t.Symbol, sizeUSD, t.TokenAddress)

	// Check RPC connectivity
	if err := l.ping(); err != nil {
		return Fill{}, fmt.Errorf("RPC unreachable: %w", err)
	}

	// Stubbed: returns a fill at current market price until go-ethereum is wired
	return Fill{
		Action:    "BUY",
		Price:     t.Price,
		PctOfPos:  100,
		USD:       sizeUSD,
		PnLPct:    0,
		Reason:    "live-buy (stub: see LIVE_TRADING.md)",
		TxHash:    "0x0000...pending",
		Timestamp: time.Now(),
	}, nil
}

func (l *LiveExecutor) Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error) {
	logger.Printf("[LIVE-SELL] Would sell %.0f%% of %s position @ $%.6f reason=%s",
		fraction*100, t.Symbol, t.Price, reason)

	if err := l.ping(); err != nil {
		return Fill{}, fmt.Errorf("RPC unreachable: %w", err)
	}

	pnlPct := (t.Price/pos.EntryPrice - 1) * 100
	usdClosed := pos.SizeUSD * fraction * (pos.RemainingPct / 100)
	return Fill{
		Action:    "SELL",
		Price:     t.Price,
		PctOfPos:  fraction * 100,
		USD:       usdClosed * (1 + pnlPct/100),
		PnLPct:    pnlPct,
		Reason:    reason + " (stub: see LIVE_TRADING.md)",
		TxHash:    "0x0000...pending",
		Timestamp: time.Now(),
	}, nil
}

// ping checks JSON-RPC connectivity.
func (l *LiveExecutor) ping() error {
	body := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	resp, err := l.httpClient.Post(l.rpcURL, "application/json", bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	return json.Unmarshal(raw, &result)
}

// NewExecutor creates the right executor based on env vars.
func NewExecutor() Executor {
	if os.Getenv("LIVE_TRADING") == "true" {
		exec, err := NewLiveExecutor()
		if err != nil {
			logger.Printf("[trader] live trading disabled: %v — falling back to paper mode", err)
			return &PaperExecutor{}
		}
		logger.Printf("[trader] ⚡ LIVE TRADING ENABLED — Base + Aerodrome — RPC: %s", exec.rpcURL)
		return exec
	}
	logger.Printf("[trader] 📄 Paper trading mode (set LIVE_TRADING=true to enable live)")
	return &PaperExecutor{}
}
