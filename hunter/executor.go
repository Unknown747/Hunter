package main

import (
        "context"
        "crypto/ecdsa"
        "fmt"
        "math/big"
        "os"
        "strings"
        "time"

        geth "github.com/ethereum/go-ethereum"
        "github.com/ethereum/go-ethereum/accounts/abi"
        "github.com/ethereum/go-ethereum/common"
        "github.com/ethereum/go-ethereum/core/types"
        "github.com/ethereum/go-ethereum/crypto"
        "github.com/ethereum/go-ethereum/ethclient"
)

// ─── Contract addresses (Base mainnet) ────────────────────────────────────────

const (
        addrRouter  = "0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"
        addrFactory = "0x420DD381b31aEf6683db6B902084cB0FFECe40Da"
        addrWETH    = "0x4200000000000000000000000000000000000006"
        chainIDBase = int64(8453)
)

// ─── ABI definitions ──────────────────────────────────────────────────────────

const abiRouter = `[
{"inputs":[{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"components":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bool","name":"stable","type":"bool"},{"internalType":"address","name":"factory","type":"address"}],"internalType":"struct IRouter.Route[]","name":"routes","type":"tuple[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactETHForTokens","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"payable","type":"function"},
{"inputs":[{"internalType":"uint256","name":"amountIn","type":"uint256"},{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"components":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bool","name":"stable","type":"bool"},{"internalType":"address","name":"factory","type":"address"}],"internalType":"struct IRouter.Route[]","name":"routes","type":"tuple[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactTokensForETH","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"nonpayable","type":"function"}
]`

const abiERC20 = `[
{"inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},
{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

// ─── Executor interface ────────────────────────────────────────────────────────

// Executor abstracts on-chain trade execution.
type Executor interface {
        Buy(t *TokenInfo, sizeUSD float64) (Fill, error)
        Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error)
}

// ─── Route struct (matches Aerodrome IRouter.Route) ───────────────────────────

// AeroRoute must match the Solidity struct exactly (field order matters for ABI encoding).
type AeroRoute struct {
        From    common.Address
        To      common.Address
        Stable  bool
        Factory common.Address
}

// ─── LiveExecutor ─────────────────────────────────────────────────────────────

// LiveExecutor sends real transactions on Base mainnet via Aerodrome V2.
type LiveExecutor struct {
        client       *ethclient.Client
        key          *ecdsa.PrivateKey
        address      common.Address
        chainID      *big.Int
        tradeSizeWei *big.Int
        rABI         abi.ABI
        eABI         abi.ABI
}

// NewLiveExecutor creates a LiveExecutor from environment variables.
// Required: PRIVATE_KEY
// Optional: BASE_RPC_URL (default: https://mainnet.base.org), TRADE_SIZE_ETH (default: 0.0003)
func NewLiveExecutor() (*LiveExecutor, error) {
        rpcURL := os.Getenv("BASE_RPC_URL")
        if rpcURL == "" {
                rpcURL = "https://mainnet.base.org"
        }

        privKeyHex := os.Getenv("PRIVATE_KEY")
        if privKeyHex == "" {
                return nil, fmt.Errorf("PRIVATE_KEY env var is required")
        }

        tradeSizeETH := os.Getenv("TRADE_SIZE_ETH")
        if tradeSizeETH == "" {
                tradeSizeETH = "0.0003"
        }

        // Parse private key
        key, err := crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
        if err != nil {
                return nil, fmt.Errorf("invalid PRIVATE_KEY: %w", err)
        }
        pub := key.Public().(*ecdsa.PublicKey)
        address := crypto.PubkeyToAddress(*pub)

        // Parse trade size into wei
        weiPerETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
        sizeF, _, _ := big.ParseFloat(tradeSizeETH, 10, 256, big.ToNearestEven)
        sizeWei, _ := new(big.Float).Mul(sizeF, new(big.Float).SetInt(weiPerETH)).Int(nil)

        // Connect to RPC
        client, err := ethclient.Dial(rpcURL)
        if err != nil {
                return nil, fmt.Errorf("RPC dial failed (%s): %w", rpcURL, err)
        }

        // Verify we are on Base mainnet
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        chainID, err := client.ChainID(ctx)
        if err != nil {
                return nil, fmt.Errorf("cannot get chain ID: %w", err)
        }
        if chainID.Int64() != chainIDBase {
                return nil, fmt.Errorf("wrong chain: got %d, expected %d (Base mainnet)", chainID.Int64(), chainIDBase)
        }

        // Parse ABIs
        rABI, err := abi.JSON(strings.NewReader(abiRouter))
        if err != nil {
                return nil, fmt.Errorf("router ABI: %w", err)
        }
        eABI, err := abi.JSON(strings.NewReader(abiERC20))
        if err != nil {
                return nil, fmt.Errorf("ERC20 ABI: %w", err)
        }

        ethAmt := new(big.Float).Quo(new(big.Float).SetInt(sizeWei), new(big.Float).SetInt(weiPerETH))
        logger.Printf("[executor] wallet=%s  tradeSize=%s ETH  rpc=%s", address.Hex(), ethAmt.Text('f', 6), rpcURL)

        return &LiveExecutor{
                client:       client,
                key:          key,
                address:      address,
                chainID:      chainID,
                tradeSizeWei: sizeWei,
                rABI:         rABI,
                eABI:         eABI,
        }, nil
}

// Buy sends a swapExactETHForTokens transaction on Aerodrome.
func (e *LiveExecutor) Buy(t *TokenInfo, _ float64) (Fill, error) {
        if t.TokenAddress == "" {
                return Fill{}, fmt.Errorf("no token address for %s", t.Symbol)
        }

        routes := []AeroRoute{{
                From:    common.HexToAddress(addrWETH),
                To:      common.HexToAddress(t.TokenAddress),
                Stable:  false,
                Factory: common.HexToAddress(addrFactory),
        }}
        deadline := big.NewInt(time.Now().Add(60 * time.Second).Unix())

        data, err := e.rABI.Pack("swapExactETHForTokens",
                big.NewInt(0), // amountOutMin=0 — speed over slippage protection (meme sniping)
                routes,
                e.address,
                deadline,
        )
        if err != nil {
                return Fill{}, fmt.Errorf("pack buy: %w", err)
        }

        txHash, err := e.sendTx(common.HexToAddress(addrRouter), e.tradeSizeWei, data, 350_000)
        if err != nil {
                return Fill{}, fmt.Errorf("send buy: %w", err)
        }
        logger.Printf("[executor] BUY tx=%s  token=%s  price=$%.6f", txHash.Hex(), t.Symbol, t.Price)

        receipt, err := e.waitReceipt(txHash)
        if err != nil {
                return Fill{}, fmt.Errorf("buy receipt: %w", err)
        }
        if receipt.Status != 1 {
                return Fill{}, fmt.Errorf("buy reverted: %s", txHash.Hex())
        }

        return Fill{
                Action:    "BUY",
                Price:     t.Price,
                PctOfPos:  100,
                USD:       weiToFloat64(e.tradeSizeWei) * 3000, // approximate USD (ETH × $3000)
                PnLPct:    0,
                Reason:    "entry",
                TxHash:    txHash.Hex(),
                Timestamp: time.Now(),
        }, nil
}

// Sell executes approve + swapExactTokensForETH on Aerodrome.
func (e *LiveExecutor) Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error) {
        if t.TokenAddress == "" {
                return Fill{}, fmt.Errorf("no token address for %s", t.Symbol)
        }
        tokenAddr := common.HexToAddress(t.TokenAddress)

        // Get current on-chain balance
        balance, err := e.balanceOf(tokenAddr)
        if err != nil {
                return Fill{}, fmt.Errorf("balanceOf: %w", err)
        }
        if balance.Sign() == 0 {
                return Fill{}, fmt.Errorf("zero token balance for %s", t.Symbol)
        }

        // Sell `fraction` of the current balance
        amountToSell := new(big.Int).Set(balance)
        if fraction < 0.999 {
                f := new(big.Float).SetFloat64(fraction)
                af, _ := new(big.Float).Mul(new(big.Float).SetInt(balance), f).Int(nil)
                amountToSell = af
        }

        // 1. Approve router to spend tokens
        approveData, err := e.eABI.Pack("approve", common.HexToAddress(addrRouter), amountToSell)
        if err != nil {
                return Fill{}, fmt.Errorf("pack approve: %w", err)
        }
        approveTx, err := e.sendTx(tokenAddr, big.NewInt(0), approveData, 60_000)
        if err != nil {
                return Fill{}, fmt.Errorf("send approve: %w", err)
        }
        if _, err = e.waitReceipt(approveTx); err != nil {
                return Fill{}, fmt.Errorf("approve receipt: %w", err)
        }

        // 2. Swap tokens → ETH
        routes := []AeroRoute{{
                From:    tokenAddr,
                To:      common.HexToAddress(addrWETH),
                Stable:  false,
                Factory: common.HexToAddress(addrFactory),
        }}
        deadline := big.NewInt(time.Now().Add(60 * time.Second).Unix())
        swapData, err := e.rABI.Pack("swapExactTokensForETH",
                amountToSell,
                big.NewInt(0), // amountOutMin=0
                routes,
                e.address,
                deadline,
        )
        if err != nil {
                return Fill{}, fmt.Errorf("pack sell: %w", err)
        }

        txHash, err := e.sendTx(common.HexToAddress(addrRouter), big.NewInt(0), swapData, 350_000)
        if err != nil {
                return Fill{}, fmt.Errorf("send sell: %w", err)
        }

        receipt, err := e.waitReceipt(txHash)
        if err != nil {
                return Fill{}, fmt.Errorf("sell receipt: %w", err)
        }
        if receipt.Status != 1 {
                return Fill{}, fmt.Errorf("sell reverted: %s", txHash.Hex())
        }

        pnlPct := (t.Price/pos.EntryPrice - 1) * 100
        usdClosed := pos.SizeUSD * fraction * (pos.RemainingPct / 100)
        logger.Printf("[executor] SELL tx=%s  token=%s  pnl=%.2f%%  reason=%s", txHash.Hex(), t.Symbol, pnlPct, reason)

        return Fill{
                Action:    "SELL",
                Price:     t.Price,
                PctOfPos:  fraction * 100,
                USD:       usdClosed * (1 + pnlPct/100),
                PnLPct:    pnlPct,
                Reason:    reason,
                TxHash:    txHash.Hex(),
                Timestamp: time.Now(),
        }, nil
}

// ─── Internal helpers ──────────────────────────────────────────────────────────

func (e *LiveExecutor) sendTx(to common.Address, value *big.Int, data []byte, gasLimit uint64) (common.Hash, error) {
        ctx := context.Background()

        nonce, err := e.client.PendingNonceAt(ctx, e.address)
        if err != nil {
                return common.Hash{}, fmt.Errorf("nonce: %w", err)
        }

        gasPrice, err := e.client.SuggestGasPrice(ctx)
        if err != nil {
                return common.Hash{}, fmt.Errorf("gasPrice: %w", err)
        }
        // +20% buffer for faster inclusion
        gasPrice.Mul(gasPrice, big.NewInt(12))
        gasPrice.Div(gasPrice, big.NewInt(10))

        tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, data)
        signed, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.key)
        if err != nil {
                return common.Hash{}, fmt.Errorf("sign: %w", err)
        }

        if err := e.client.SendTransaction(ctx, signed); err != nil {
                return common.Hash{}, fmt.Errorf("broadcast: %w", err)
        }
        return signed.Hash(), nil
}

func (e *LiveExecutor) waitReceipt(txHash common.Hash) (*types.Receipt, error) {
        ctx := context.Background()
        deadline := time.Now().Add(90 * time.Second)
        for time.Now().Before(deadline) {
                receipt, err := e.client.TransactionReceipt(ctx, txHash)
                if err == nil {
                        return receipt, nil
                }
                time.Sleep(2 * time.Second)
        }
        return nil, fmt.Errorf("timeout waiting for %s", txHash.Hex())
}

func (e *LiveExecutor) balanceOf(token common.Address) (*big.Int, error) {
        data, err := e.eABI.Pack("balanceOf", e.address)
        if err != nil {
                return nil, err
        }
        result, err := e.client.CallContract(context.Background(), geth.CallMsg{
                To:   &token,
                Data: data,
        }, nil)
        if err != nil {
                return nil, err
        }
        if len(result) < 32 {
                return big.NewInt(0), nil
        }
        return new(big.Int).SetBytes(result[len(result)-32:]), nil
}

func weiToFloat64(wei *big.Int) float64 {
        weiPerETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
        f, _ := new(big.Float).Quo(
                new(big.Float).SetInt(wei),
                new(big.Float).SetInt(weiPerETH),
        ).Float64()
        return f
}

// ─── NoOpExecutor ─────────────────────────────────────────────────────────────

// NoOpExecutor runs when PRIVATE_KEY is not configured.
// The engine monitors and scores tokens but does NOT open any trades.
type NoOpExecutor struct{}

func (n *NoOpExecutor) Buy(t *TokenInfo, _ float64) (Fill, error) {
        return Fill{}, fmt.Errorf("PRIVATE_KEY not set — monitoring only, trading disabled")
}

func (n *NoOpExecutor) Sell(t *TokenInfo, pos *Position, _ float64, _ string) (Fill, error) {
        return Fill{}, fmt.Errorf("PRIVATE_KEY not set — monitoring only, trading disabled")
}

// ─── NewExecutor ──────────────────────────────────────────────────────────────

// NewExecutor returns a LiveExecutor if PRIVATE_KEY is set,
// or a NoOpExecutor (monitoring-only) if it is not.
func NewExecutor() Executor {
        if os.Getenv("PRIVATE_KEY") == "" {
                logger.Printf("[executor] ⚠️  PRIVATE_KEY not set — running in MONITORING-ONLY mode (no trades)")
                logger.Printf("[executor]    Set PRIVATE_KEY + BASE_RPC_URL + TRADE_SIZE_ETH on VPS for live trading")
                return &NoOpExecutor{}
        }
        exec, err := NewLiveExecutor()
        if err != nil {
                logger.Fatalf(
                        "[executor] FATAL — %v\n\n"+
                                "  Required:\n"+
                                "    PRIVATE_KEY=<hex, no 0x prefix>\n"+
                                "    BASE_RPC_URL=<url>     (default: https://mainnet.base.org)\n"+
                                "    TRADE_SIZE_ETH=<eth>   (default: 0.0003 ≈ $1)\n",
                        err,
                )
        }
        return exec
}
