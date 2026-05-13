# Live Trading Setup (Base + Aerodrome)

## Prerequisites

1. **Add go-ethereum** to your hunter module:
   ```bash
   cd hunter
   go get github.com/ethereum/go-ethereum@v1.14.8
   go mod tidy
   ```

2. **Set environment variables** on your VPS:
   ```bash
   export LIVE_TRADING=true
   export PRIVATE_KEY=your_wallet_private_key_hex_no_0x_prefix
   export BASE_RPC_URL=https://mainnet.base.org   # or Alchemy/Infura for reliability
   ```

3. **Fund your wallet** with ETH on Base network.
   - Each trade = TRADE_SIZE_USD worth of ETH (default $1)
   - Keep at least 0.01 ETH for gas

## How it works

When `LIVE_TRADING=true`:
1. Engine detects a qualifying token via the strategy engine
2. Calls `swapExactETHForTokens` on Aerodrome Router V2 (Base)
   - Router: `0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43`
   - Route: WETH → TOKEN (volatile pool)
3. Monitors P&L every scan cycle
4. Executes `approve` + `swapExactTokensForETH` when exit conditions trigger

## Security

- **Never commit your PRIVATE_KEY to git**
- Use a dedicated hot wallet with limited funds
- Start with paper mode to validate strategy performance
- The wallet should hold ONLY the ETH needed for trading

## Implementing full on-chain execution in executor.go

Replace the stub in `LiveExecutor.Buy()` with:
```go
// 1. Load key
privKey, _ := crypto.HexToECDSA(l.privateKey)
// 2. Get nonce
// 3. ABI-encode swapExactETHForTokens call
// 4. Sign + send transaction
// 5. Wait for receipt
```

See the Aerodrome Router V2 ABI for the exact function signatures.

## Contract Addresses (Base Mainnet)

| Contract          | Address                                    |
|-------------------|--------------------------------------------|
| Aerodrome Router  | 0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43 |
| Aerodrome Factory | 0x420DD381b31aEf6683db6B902084cB0FFECe40Da |
| WETH (Base)       | 0x4200000000000000000000000000000000000006 |
| USDC (Base)       | 0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913 |
