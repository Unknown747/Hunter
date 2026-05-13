# Panduan Live Trading (Base + Aerodrome)

## Prasyarat

1. **go-ethereum sudah termasuk** dalam dependensi proyek ini (tidak perlu install manual).

2. **Set environment variable** di VPS kamu:
   ```bash
   export LIVE_TRADING=true
   export PRIVATE_KEY=hex_private_key_tanpa_prefix_0x
   export BASE_RPC_URL=https://mainnet.base.org   # atau Alchemy/Infura untuk lebih andal
   export TRADE_SIZE_ETH=0.001                    # ukuran trade per posisi
   ```

3. **Isi wallet** dengan ETH di jaringan Base.
   - Setiap trade menggunakan nilai `TRADE_SIZE_ETH` ETH
   - Sisakan minimal 0.01 ETH untuk biaya gas

## Cara Kerja

Saat `LIVE_TRADING=true`:
1. Engine mendeteksi token yang memenuhi syarat melalui strategy engine
2. Memanggil `swapExactETHForTokens` di Aerodrome Router V2 (Base)
   - Router: `0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43`
   - Rute: WETH → TOKEN (volatile pool)
3. Memantau P&L setiap siklus scan
4. Menjalankan `approve` + `swapExactTokensForETH` saat kondisi exit terpenuhi

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
