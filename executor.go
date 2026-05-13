package main

import (
        "context"
        "crypto/ecdsa"
        "fmt"
        "math/big"
        "os"
        "strconv"
        "strings"
        "sync"
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
        addrUSDC    = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
        chainIDBase = int64(8453)
)

// ─── ABI definitions ──────────────────────────────────────────────────────────

const abiRouter = `[
{"inputs":[{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"components":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bool","name":"stable","type":"bool"},{"internalType":"address","name":"factory","type":"address"}],"internalType":"struct IRouter.Route[]","name":"routes","type":"tuple[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactETHForTokens","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"payable","type":"function"},
{"inputs":[{"internalType":"uint256","name":"amountIn","type":"uint256"},{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"components":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bool","name":"stable","type":"bool"},{"internalType":"address","name":"factory","type":"address"}],"internalType":"struct IRouter.Route[]","name":"routes","type":"tuple[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactTokensForETH","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"nonpayable","type":"function"},
{"inputs":[{"internalType":"uint256","name":"amountIn","type":"uint256"},{"components":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bool","name":"stable","type":"bool"},{"internalType":"address","name":"factory","type":"address"}],"internalType":"struct IRouter.Route[]","name":"routes","type":"tuple[]"}],"name":"getAmountsOut","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"view","type":"function"}
]`

const abiERC20 = `[
{"inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},
{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

// ─── Executor interface ────────────────────────────────────────────────────────

type Executor interface {
        Buy(t *TokenInfo, sizeUSD float64) (Fill, error)
        Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error)
}

// ─── Route struct (matches Aerodrome IRouter.Route) ───────────────────────────

type AeroRoute struct {
        From    common.Address
        To      common.Address
        Stable  bool
        Factory common.Address
}

// ─── LiveExecutor ─────────────────────────────────────────────────────────────

type LiveExecutor struct {
        client       *ethclient.Client
        clientMu     sync.Mutex // melindungi penggantian client saat reconnect
        primaryURL   string
        backupURL    string
        key          *ecdsa.PrivateKey
        address      common.Address
        chainID      *big.Int
        tradeSizeWei *big.Int
        slippagePct  float64
        rABI         abi.ABI
        eABI         abi.ABI
}

// NewLiveExecutor membuat LiveExecutor dari environment variables.
//
// Wajib   : PRIVATE_KEY
// Opsional: BASE_RPC_URL        (default: https://mainnet.base.org)
//
//      BASE_RPC_URL_BACKUP  (fallback jika primary gagal)
//      TRADE_SIZE_ETH       (default: 0.0003)
//      SLIPPAGE_PCT         (default: 5.0 — 5%)
func NewLiveExecutor() (*LiveExecutor, error) {
        primaryURL := os.Getenv("BASE_RPC_URL")
        if primaryURL == "" {
                primaryURL = "https://mainnet.base.org"
        }
        backupURL := os.Getenv("BASE_RPC_URL_BACKUP")

        privKeyHex := os.Getenv("PRIVATE_KEY")
        if privKeyHex == "" {
                return nil, fmt.Errorf("PRIVATE_KEY env var is required")
        }

        tradeSizeETH := os.Getenv("TRADE_SIZE_ETH")
        if tradeSizeETH == "" {
                tradeSizeETH = "0.0003"
        }

        slippagePct := 5.0
        if s := os.Getenv("SLIPPAGE_PCT"); s != "" {
                if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 && v < 100 {
                        slippagePct = v
                }
        }

        key, err := crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
        if err != nil {
                return nil, fmt.Errorf("invalid PRIVATE_KEY: %w", err)
        }
        pub := key.Public().(*ecdsa.PublicKey)
        address := crypto.PubkeyToAddress(*pub)

        weiPerETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
        sizeF, _, _ := big.ParseFloat(tradeSizeETH, 10, 256, big.ToNearestEven)
        sizeWei, _ := new(big.Float).Mul(sizeF, new(big.Float).SetInt(weiPerETH)).Int(nil)

        client, err := ethclient.Dial(primaryURL)
        if err != nil {
                return nil, fmt.Errorf("RPC dial failed (%s): %w", primaryURL, err)
        }

        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        chainID, err := client.ChainID(ctx)
        if err != nil {
                return nil, fmt.Errorf("cannot get chain ID: %w", err)
        }
        if chainID.Int64() != chainIDBase {
                return nil, fmt.Errorf("wrong chain: got %d, expected %d (Base mainnet)", chainID.Int64(), chainIDBase)
        }

        rABI, err := abi.JSON(strings.NewReader(abiRouter))
        if err != nil {
                return nil, fmt.Errorf("router ABI: %w", err)
        }
        eABI, err := abi.JSON(strings.NewReader(abiERC20))
        if err != nil {
                return nil, fmt.Errorf("ERC20 ABI: %w", err)
        }

        ethAmt := new(big.Float).Quo(new(big.Float).SetInt(sizeWei), new(big.Float).SetInt(weiPerETH))
        backupInfo := ""
        if backupURL != "" {
                backupInfo = "  backup=" + backupURL
        }
        logger.Printf("[executor] wallet=%s  tradeSize=%s ETH  slippage=%.1f%%  rpc=%s%s",
                address.Hex(), ethAmt.Text('f', 6), slippagePct, primaryURL, backupInfo)

        return &LiveExecutor{
                client:       client,
                primaryURL:   primaryURL,
                backupURL:    backupURL,
                key:          key,
                address:      address,
                chainID:      chainID,
                tradeSizeWei: sizeWei,
                slippagePct:  slippagePct,
                rABI:         rABI,
                eABI:         eABI,
        }, nil
}

// ─── RPC Failover ─────────────────────────────────────────────────────────────

// reconnect mencoba menghubungkan ulang ke primary RPC, lalu backup jika gagal.
func (e *LiveExecutor) reconnect() error {
        e.clientMu.Lock()
        defer e.clientMu.Unlock()

        // Coba primary dulu
        if c, err := ethclient.Dial(e.primaryURL); err == nil {
                e.client = c
                logger.Printf("[executor] 🔄 Terhubung kembali ke RPC primary: %s", e.primaryURL)
                return nil
        }

        // Coba backup
        if e.backupURL != "" {
                if c, err := ethclient.Dial(e.backupURL); err == nil {
                        e.client = c
                        logger.Printf("[executor] 🔄 Beralih ke RPC backup: %s", e.backupURL)
                        return nil
                }
        }

        return fmt.Errorf("semua RPC endpoint tidak tersedia")
}

// isConnErr mendeteksi error koneksi yang memerlukan reconnect.
func isConnErr(err error) bool {
        if err == nil {
                return false
        }
        s := err.Error()
        return strings.Contains(s, "connection refused") ||
                strings.Contains(s, "connection reset") ||
                strings.Contains(s, "EOF") ||
                strings.Contains(s, "no such host") ||
                strings.Contains(s, "broken pipe") ||
                strings.Contains(s, "dial tcp")
}

// ─── Gas cost helper ──────────────────────────────────────────────────────────

// calcGasUSD menghitung biaya gas transaksi dalam USD dari receipt.
func (e *LiveExecutor) calcGasUSD(receipt *types.Receipt) float64 {
        if receipt == nil || receipt.EffectiveGasPrice == nil {
                return 0
        }
        gasWei := new(big.Int).Mul(
                big.NewInt(int64(receipt.GasUsed)),
                receipt.EffectiveGasPrice,
        )
        return weiToFloat64(gasWei) * e.ethPriceUSD()
}

// ─── Smart Route Finder ───────────────────────────────────────────────────────

// findBestBuyRoute mencoba beberapa strategi route untuk ETH → TOKEN dan
// mengembalikan route terbaik beserta estimasi output-nya.
// Urutan prioritas:
//  1. ETH → TOKEN langsung (volatile) — jika quoteToken adalah WETH
//  2. ETH → TOKEN langsung (stable)
//  3. ETH → USDC → TOKEN (dua hop, volatile–volatile)
func (e *LiveExecutor) findBestBuyRoute(tokenAddr, quoteTokenAddr string) ([]AeroRoute, *big.Int) {
        token := common.HexToAddress(tokenAddr)
        weth := common.HexToAddress(addrWETH)
        usdc := common.HexToAddress(addrUSDC)
        factory := common.HexToAddress(addrFactory)

        candidates := []struct {
                label  string
                routes []AeroRoute
        }{
                // 1. Langsung volatile (cocok untuk pair baru meme coin vs WETH)
                {"WETH→TOKEN volatile", []AeroRoute{{From: weth, To: token, Stable: false, Factory: factory}}},
                // 2. Langsung stable (beberapa token stablecoin-adjacent pakai stable pool)
                {"WETH→TOKEN stable", []AeroRoute{{From: weth, To: token, Stable: true, Factory: factory}}},
                // 3. Dua hop: WETH→USDC→TOKEN (untuk token yang hanya punya pair USDC)
                {"WETH→USDC→TOKEN", []AeroRoute{
                        {From: weth, To: usdc, Stable: false, Factory: factory},
                        {From: usdc, To: token, Stable: false, Factory: factory},
                }},
                // 4. Dua hop stable leg kedua
                {"WETH→USDC→TOKEN(stable)", []AeroRoute{
                        {From: weth, To: usdc, Stable: false, Factory: factory},
                        {From: usdc, To: token, Stable: true, Factory: factory},
                }},
        }

        // Jika quote token diketahui BUKAN WETH, prioritaskan dua-hop via quote token
        if quoteTokenAddr != "" && quoteTokenAddr != addrWETH {
                quoteAddr := common.HexToAddress(quoteTokenAddr)
                hopViaQuote := []AeroRoute{
                        {From: weth, To: quoteAddr, Stable: false, Factory: factory},
                        {From: quoteAddr, To: token, Stable: false, Factory: factory},
                }
                // Sisipkan di depan kandidat lainnya
                candidates = append([]struct {
                        label  string
                        routes []AeroRoute
                }{{"WETH→QuoteToken→TOKEN", hopViaQuote}}, candidates...)
        }

        bestOut := big.NewInt(0)
        bestRoutes := candidates[0].routes // fallback ke route pertama

        for _, c := range candidates {
                out, err := e.getAmountsOut(e.tradeSizeWei, c.routes)
                if err != nil || out == nil || out.Sign() <= 0 {
                        logger.Printf("[executor] route %s: tidak tersedia (%v)", c.label, err)
                        continue
                }
                logger.Printf("[executor] route %s: estimasi output = %s", c.label, out.String())
                if out.Cmp(bestOut) > 0 {
                        bestOut = out
                        bestRoutes = c.routes
                }
        }

        return bestRoutes, bestOut
}

// findBestSellRoute mencoba beberapa strategi route untuk TOKEN → ETH.
func (e *LiveExecutor) findBestSellRoute(tokenAddr, quoteTokenAddr string, amountIn *big.Int) ([]AeroRoute, *big.Int) {
        token := common.HexToAddress(tokenAddr)
        weth := common.HexToAddress(addrWETH)
        usdc := common.HexToAddress(addrUSDC)
        factory := common.HexToAddress(addrFactory)

        candidates := []struct {
                label  string
                routes []AeroRoute
        }{
                {"TOKEN→WETH volatile", []AeroRoute{{From: token, To: weth, Stable: false, Factory: factory}}},
                {"TOKEN→WETH stable", []AeroRoute{{From: token, To: weth, Stable: true, Factory: factory}}},
                {"TOKEN→USDC→WETH", []AeroRoute{
                        {From: token, To: usdc, Stable: false, Factory: factory},
                        {From: usdc, To: weth, Stable: false, Factory: factory},
                }},
                {"TOKEN→USDC(stable)→WETH", []AeroRoute{
                        {From: token, To: usdc, Stable: true, Factory: factory},
                        {From: usdc, To: weth, Stable: false, Factory: factory},
                }},
        }

        if quoteTokenAddr != "" && quoteTokenAddr != addrWETH {
                quoteAddr := common.HexToAddress(quoteTokenAddr)
                hopViaQuote := []AeroRoute{
                        {From: token, To: quoteAddr, Stable: false, Factory: factory},
                        {From: quoteAddr, To: weth, Stable: false, Factory: factory},
                }
                candidates = append([]struct {
                        label  string
                        routes []AeroRoute
                }{{"TOKEN→QuoteToken→WETH", hopViaQuote}}, candidates...)
        }

        bestOut := big.NewInt(0)
        bestRoutes := candidates[0].routes

        for _, c := range candidates {
                out, err := e.getAmountsOut(amountIn, c.routes)
                if err != nil || out == nil || out.Sign() <= 0 {
                        continue
                }
                if out.Cmp(bestOut) > 0 {
                        bestOut = out
                        bestRoutes = c.routes
                }
        }

        return bestRoutes, bestOut
}

// ─── Buy ──────────────────────────────────────────────────────────────────────

func (e *LiveExecutor) Buy(t *TokenInfo, _ float64) (Fill, error) {
        if t.TokenAddress == "" {
                return Fill{}, fmt.Errorf("no token address for %s", t.Symbol)
        }

        routes, expectedOut := e.findBestBuyRoute(t.TokenAddress, t.QuoteTokenAddress)
        amountOutMin := applySlippage(expectedOut, e.slippagePct)

        if expectedOut.Sign() > 0 {
                logger.Printf("[executor] BUY quote %s: expect %s tokens, min %s (slippage %.1f%%)",
                        t.Symbol, expectedOut.String(), amountOutMin.String(), e.slippagePct)

        } else {
                logger.Printf("[executor] ⚠️  BUY %s: semua route gagal estimasi, lanjut dengan amountOutMin=0", t.Symbol)
        }

        deadline := big.NewInt(time.Now().Add(60 * time.Second).Unix())
        data, err := e.rABI.Pack("swapExactETHForTokens", amountOutMin, routes, e.address, deadline)
        if err != nil {
                return Fill{}, fmt.Errorf("pack buy: %w", err)
        }

        txHash, err := e.sendTxWithRetry(common.HexToAddress(addrRouter), e.tradeSizeWei, data, 500_000)
        if err != nil {
                return Fill{}, fmt.Errorf("send buy: %w", err)
        }

        receipt, err := e.waitReceipt(txHash)
        if err != nil {
                return Fill{}, fmt.Errorf("buy receipt: %w", err)
        }
        if receipt.Status != 1 {
                return Fill{}, fmt.Errorf("buy reverted: %s", txHash.Hex())
        }

        gasUSD := e.calcGasUSD(receipt)
        tradeUSD := weiToFloat64(e.tradeSizeWei) * e.ethPriceUSD()

        logger.Printf("[executor] ✅ BUY tx=%s  token=%s  price=$%.6f  gas=$%.4f",
                txHash.Hex(), t.Symbol, t.Price, gasUSD)

        return Fill{
                Action:    "BUY",
                Price:     t.Price,
                PctOfPos:  100,
                USD:       tradeUSD,
                PnLPct:    0,
                GasUSD:    gasUSD,
                Reason:    "entry",
                TxHash:    txHash.Hex(),
                Timestamp: time.Now(),
        }, nil
}

// ─── Sell ─────────────────────────────────────────────────────────────────────

func (e *LiveExecutor) Sell(t *TokenInfo, pos *Position, fraction float64, reason string) (Fill, error) {
        if t.TokenAddress == "" {
                return Fill{}, fmt.Errorf("no token address for %s", t.Symbol)
        }
        tokenAddr := common.HexToAddress(t.TokenAddress)

        balance, err := e.balanceOf(tokenAddr)
        if err != nil {
                return Fill{}, fmt.Errorf("balanceOf: %w", err)
        }
        if balance.Sign() == 0 {
                return Fill{}, fmt.Errorf("zero token balance for %s", t.Symbol)
        }

        amountToSell := new(big.Int).Set(balance)
        if fraction < 0.999 {
                f := new(big.Float).SetFloat64(fraction)
                af, _ := new(big.Float).Mul(new(big.Float).SetInt(balance), f).Int(nil)
                amountToSell = af
        }

        routes, expectedOut := e.findBestSellRoute(t.TokenAddress, t.QuoteTokenAddress, amountToSell)
        amountOutMin := applySlippage(expectedOut, e.slippagePct)

        if expectedOut.Sign() > 0 {
                logger.Printf("[executor] SELL quote %s: estimasi ETH keluar = %s wei", t.Symbol, expectedOut.String())
        } else {
                logger.Printf("[executor] ⚠️  SELL %s: semua route gagal estimasi, lanjut dengan amountOutMin=0", t.Symbol)
        }

        // 1. Approve
        approveData, err := e.eABI.Pack("approve", common.HexToAddress(addrRouter), amountToSell)
        if err != nil {
                return Fill{}, fmt.Errorf("pack approve: %w", err)
        }
        approveTx, err := e.sendTxWithRetry(tokenAddr, big.NewInt(0), approveData, 60_000)
        if err != nil {
                return Fill{}, fmt.Errorf("send approve: %w", err)
        }
        approveReceipt, err := e.waitReceipt(approveTx)
        if err != nil {
                return Fill{}, fmt.Errorf("approve receipt: %w", err)
        }
        approveGasUSD := e.calcGasUSD(approveReceipt)

        // 2. Swap tokens → ETH
        deadline := big.NewInt(time.Now().Add(60 * time.Second).Unix())
        swapData, err := e.rABI.Pack("swapExactTokensForETH", amountToSell, amountOutMin, routes, e.address, deadline)
        if err != nil {
                return Fill{}, fmt.Errorf("pack sell: %w", err)
        }

        txHash, err := e.sendTxWithRetry(common.HexToAddress(addrRouter), big.NewInt(0), swapData, 500_000)
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

        swapGasUSD := e.calcGasUSD(receipt)
        totalGasUSD := approveGasUSD + swapGasUSD
        pnlPct := (t.Price/pos.EntryPrice - 1) * 100
        usdClosed := pos.SizeUSD * fraction * (pos.RemainingPct / 100)

        logger.Printf("[executor] SELL tx=%s  token=%s  pnl=%.2f%%  gas=$%.4f  reason=%s",
                txHash.Hex(), t.Symbol, pnlPct, totalGasUSD, reason)

        return Fill{
                Action:    "SELL",
                Price:     t.Price,
                PctOfPos:  fraction * 100,
                USD:       usdClosed * (1 + pnlPct/100),
                PnLPct:    pnlPct,
                GasUSD:    totalGasUSD,
                Reason:    reason,
                TxHash:    txHash.Hex(),
                Timestamp: time.Now(),
        }, nil
}

// ─── Internal helpers ──────────────────────────────────────────────────────────

func (e *LiveExecutor) getAmountsOut(amountIn *big.Int, routes []AeroRoute) (*big.Int, error) {
        data, err := e.rABI.Pack("getAmountsOut", amountIn, routes)
        if err != nil {
                return nil, fmt.Errorf("pack getAmountsOut: %w", err)
        }

        routerAddr := common.HexToAddress(addrRouter)
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        e.clientMu.Lock()
        c := e.client
        e.clientMu.Unlock()

        result, err := c.CallContract(ctx, geth.CallMsg{To: &routerAddr, Data: data}, nil)
        if err != nil {
                return nil, fmt.Errorf("call getAmountsOut: %w", err)
        }

        out, err := e.rABI.Unpack("getAmountsOut", result)
        if err != nil {
                return nil, fmt.Errorf("unpack getAmountsOut: %w", err)
        }
        if len(out) == 0 {
                return nil, fmt.Errorf("empty getAmountsOut result")
        }

        amounts, ok := out[0].([]*big.Int)
        if !ok || len(amounts) == 0 {
                return nil, fmt.Errorf("unexpected getAmountsOut type")
        }
        return amounts[len(amounts)-1], nil
}

func applySlippage(amount *big.Int, slippagePct float64) *big.Int {
        if amount.Sign() == 0 {
                return big.NewInt(0)
        }
        slippageBps := int64(slippagePct * 100)
        numerator := new(big.Int).Mul(amount, big.NewInt(10000-slippageBps))
        return new(big.Int).Div(numerator, big.NewInt(10000))
}

// ethPriceUSD mengambil harga ETH/USD live dari pool WETH/USDC Aerodrome.
// Fallback ke $3000 jika query gagal.
func (e *LiveExecutor) ethPriceUSD() float64 {
        oneETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
        usdcAddr := common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
        routes := []AeroRoute{{
                From:    common.HexToAddress(addrWETH),
                To:      usdcAddr,
                Stable:  false,
                Factory: common.HexToAddress(addrFactory),
        }}

        data, err := e.rABI.Pack("getAmountsOut", oneETH, routes)
        if err != nil {
                return 3000
        }

        routerAddr := common.HexToAddress(addrRouter)
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        e.clientMu.Lock()
        c := e.client
        e.clientMu.Unlock()

        result, err := c.CallContract(ctx, geth.CallMsg{To: &routerAddr, Data: data}, nil)
        if err != nil || len(result) == 0 {
                return 3000
        }

        out, err := e.rABI.Unpack("getAmountsOut", result)
        if err != nil {
                return 3000
        }
        amounts, ok := out[0].([]*big.Int)
        if !ok || len(amounts) < 2 {
                return 3000
        }

        usdc := amounts[len(amounts)-1]
        price, _ := new(big.Float).Quo(
                new(big.Float).SetInt(usdc),
                new(big.Float).SetInt(big.NewInt(1_000_000)),
        ).Float64()

        if price < 100 || price > 100_000 {
                return 3000
        }
        return price
}

// sendTxWithRetry mengirim transaksi, dan jika terjadi connection error, coba reconnect lalu retry sekali.
func (e *LiveExecutor) sendTxWithRetry(to common.Address, value *big.Int, data []byte, gasLimit uint64) (common.Hash, error) {
        hash, err := e.sendTx(to, value, data, gasLimit)
        if err != nil && isConnErr(err) {
                logger.Printf("[executor] 🔌 Connection error, mencoba reconnect: %v", err)
                if rErr := e.reconnect(); rErr != nil {
                        return common.Hash{}, fmt.Errorf("reconnect gagal: %w (original: %v)", rErr, err)
                }
                // Retry sekali setelah reconnect
                return e.sendTx(to, value, data, gasLimit)
        }
        return hash, err
}

func (e *LiveExecutor) sendTx(to common.Address, value *big.Int, data []byte, gasLimit uint64) (common.Hash, error) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        e.clientMu.Lock()
        c := e.client
        e.clientMu.Unlock()

        nonce, err := c.PendingNonceAt(ctx, e.address)
        if err != nil {
                return common.Hash{}, fmt.Errorf("nonce: %w", err)
        }

        gasPrice, err := c.SuggestGasPrice(ctx)
        if err != nil {
                return common.Hash{}, fmt.Errorf("gasPrice: %w", err)
        }
        // +20% buffer untuk inklusi lebih cepat
        gasPrice.Mul(gasPrice, big.NewInt(12))
        gasPrice.Div(gasPrice, big.NewInt(10))

        tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, data)
        signed, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.key)
        if err != nil {
                return common.Hash{}, fmt.Errorf("sign: %w", err)
        }

        if err := c.SendTransaction(ctx, signed); err != nil {
                return common.Hash{}, fmt.Errorf("broadcast: %w", err)
        }
        return signed.Hash(), nil
}

func (e *LiveExecutor) waitReceipt(txHash common.Hash) (*types.Receipt, error) {
        ctx := context.Background()
        deadline := time.Now().Add(90 * time.Second)
        for time.Now().Before(deadline) {
                e.clientMu.Lock()
                c := e.client
                e.clientMu.Unlock()

                receipt, err := c.TransactionReceipt(ctx, txHash)
                if err == nil {
                        return receipt, nil
                }
                if isConnErr(err) {
                        _ = e.reconnect()
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

        e.clientMu.Lock()
        c := e.client
        e.clientMu.Unlock()

        result, err := c.CallContract(context.Background(), geth.CallMsg{To: &token, Data: data}, nil)
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

// NoOpExecutor dipakai saat PRIVATE_KEY tidak di-set — hanya monitoring.
type NoOpExecutor struct{}

func (n *NoOpExecutor) Buy(t *TokenInfo, _ float64) (Fill, error) {
        return Fill{}, fmt.Errorf("PRIVATE_KEY tidak di-set — hanya monitoring, trading dinonaktifkan")
}

func (n *NoOpExecutor) Sell(t *TokenInfo, pos *Position, _ float64, _ string) (Fill, error) {
        return Fill{}, fmt.Errorf("PRIVATE_KEY tidak di-set — hanya monitoring, trading dinonaktifkan")
}

// ─── NewExecutor ──────────────────────────────────────────────────────────────

func NewExecutor() Executor {
        if os.Getenv("PRIVATE_KEY") == "" {
                logger.Printf("[executor] ⚠️  PRIVATE_KEY tidak di-set — mode MONITORING SAJA (tidak ada trade)")
                logger.Printf("[executor]    Set PRIVATE_KEY + BASE_RPC_URL + TRADE_SIZE_ETH di VPS untuk live trading")
                return &NoOpExecutor{}
        }
        exec, err := NewLiveExecutor()
        if err != nil {
                logger.Fatalf(
                        "[executor] FATAL — %v\n\n"+
                                "  Wajib:\n"+
                                "    PRIVATE_KEY=<hex, tanpa prefix 0x>\n"+
                                "  Opsional:\n"+
                                "    BASE_RPC_URL=<url>         (default: https://mainnet.base.org)\n"+
                                "    BASE_RPC_URL_BACKUP=<url>  (fallback RPC)\n"+
                                "    TRADE_SIZE_ETH=<eth>       (default: 0.0003)\n"+
                                "    SLIPPAGE_PCT=<pct>         (default: 5.0 — 5%%)\n",
                        err,
                )
        }
        return exec
}
