# Panduan Live Trading (Base + Aerodrome)

## Prasyarat

1. **go-ethereum sudah termasuk** dalam dependensi proyek ini (tidak perlu install manual).

2. **Set environment variable** di VPS kamu:
   ```bash
   export LIVE_TRADING=true
   export PRIVATE_KEY=hex_private_key_tanpa_prefix_0x
   export BASE_RPC_URL=https://mainnet.base.org   # atau Alchemy/Infura untuk lebih andal
   export TRADE_SIZE_ETH=0.001                    # ukuran trade per posisi (dalam ETH)
   export SLIPPAGE_PCT=5.0                        # toleransi slippage maks (default: 5%)
   ```

3. **Isi wallet** dengan ETH di jaringan Base.
   - Setiap trade menggunakan nilai `TRADE_SIZE_ETH` ETH
   - Sisakan minimal 0.01 ETH untuk biaya gas

## Cara Kerja

Saat `LIVE_TRADING=true`:
1. Engine mendeteksi token yang memenuhi syarat melalui strategy engine
2. Query `getAmountsOut` ke router untuk mendapatkan estimasi output
3. Hitung `amountOutMin` = estimasi output × (1 - SLIPPAGE_PCT/100) → **proteksi sandwich attack**
4. Memanggil `swapExactETHForTokens` di Aerodrome Router V2 (Base)
   - Router: `0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43`
   - Rute: WETH → TOKEN (volatile pool)
5. Memantau P&L setiap siklus scan
6. Menjalankan `approve` + `swapExactTokensForETH` (juga dengan slippage guard) saat kondisi exit terpenuhi
7. Harga ETH/USD diambil live dari pool WETH/USDC Aerodrome (bukan hardcode)

## Slippage Protection

| `SLIPPAGE_PCT` | Efek |
|---|---|
| 1–2% | Sangat ketat — sering gagal di meme coin dengan spread lebar |
| 3–5% | **Rekomendasi** — keseimbangan antara proteksi dan success rate |
| 10%+ | Longgar — jarang gagal, tapi rentan dapat harga buruk |

Jika `getAmountsOut` gagal (RPC timeout, pool baru), transaksi tetap dikirim dengan `amountOutMin=0` + log peringatan.

## Keamanan

- **Jangan pernah commit PRIVATE_KEY ke git**
- Gunakan wallet khusus dengan dana terbatas (hot wallet)
- Mulai dengan paper mode dulu untuk validasi performa strategi
- Wallet sebaiknya hanya menyimpan ETH yang diperlukan untuk trading

## Alamat Kontrak (Base Mainnet)

| Kontrak           | Alamat                                     |
|-------------------|--------------------------------------------|
| Aerodrome Router  | 0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43 |
| Aerodrome Factory | 0x420DD381b31aEf6683db6B902084cB0FFECe40Da |
| WETH (Base)       | 0x4200000000000000000000000000000000000006 |
| USDC (Base)       | 0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913 |

## Cara Menggunakan via install.sh

```bash
# 1. Salin dan edit file konfigurasi
cp .env.example .env
nano .env

# 2. Isi nilai berikut di .env:
#    LIVE_TRADING=true
#    PRIVATE_KEY=xxxxx
#    BASE_RPC_URL=https://mainnet.base.org

# 3. Jalankan installer
sudo bash install.sh

# 4. Cek log
journalctl -u meme-hunter -f
```
