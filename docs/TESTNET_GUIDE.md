# Armaxcoin Open Testnet Guide (`ARMX-TESTNET-1`)

Welcome to the public open testnet for **Armaxcoin** (`armax-coin`). This guide provides complete, step-by-step instructions for setting up a node, generating a post-quantum wallet, syncing the blockchain, and testing transactions.

> [!WARNING]
> **TESTNET WARNING:**  
> The Armaxcoin public testnet is an experimental environment for development and security testing. Testnet coins (`ARMX`) hold **no monetary value**, cannot be traded for fiat or real assets, and carry no guarantee of conversion to mainnet assets. Chain state may be reset if critical consensus issues are identified.

---

## 1. Network Profile Specifications

| Parameter | Testnet Value | Note |
|---|---|---|
| **Network ID** | `ARMX-TESTNET-1` | Enforces network isolation from Mainnet/Regtest |
| **Default P2P Port** | `19335` | Used for peer-to-peer gossip & block streaming |
| **Default RPC Port** | `19333` | Bound strictly to `127.0.0.1` (loopback only) |
| **Target Block Time** | `120` seconds (2 minutes) | Controlled by LWMA-1 difficulty adjustment ($N=60$) |
| **Initial Block Reward** | `60.00000000 ARMX` | Halves every 1,050,000 blocks |
| **PoW Algorithm** | ArmaxHash v2 (active from block #2500) | Chained BLAKE2b Stream-Fast (CPU & ARM optimized). Historical: SHA-256d (0-519), ArmaxHash v1 (520-2499) |
| **Signature Scheme** | NIST FIPS 204 ML-DSA-65 | Post-quantum lattice-based digital signatures |
| **Data Directory** | Linux: `~/.armax/testnet`<br>Windows: `%APPDATA%\Armax\testnet` | Default database and keystore location |

---

## 2. System Requirements

- **Supported Platforms:**
  - Linux `amd64` (x86_64 servers & desktops)
  - Linux `arm64` (Raspberry Pi 4/5, Armbian, AWS Graviton)
  - Windows `amd64` (Windows 10/11, Windows Server 64-bit)
  - macOS `arm64` (Apple Silicon M1/M2/M3/M4)
- **Minimum Hardware:**
  - 2 CPU cores
  - 2 GB RAM (4 GB recommended if running both node and miner)
  - 10 GB free disk space (SSD recommended)
- **Toolchain (if building from source):**
  - **Go 1.27 or higher** (required for `crypto/mldsa`)
  - Git

---

## 3. Quickstart: Building & Running

### Step 1: Clone and Build

```bash
git clone https://github.com/acxxcxx/armax-coin.git
cd armax-coin

# Verify Go version
go version

# Build all binaries
go build -o armcoind main.go
go build -o armwallet ./cli/armwallet
go build -o armsend ./cli/armsend
go build -o armminer miner/miner.go
```

On Windows (PowerShell):
```powershell
.\build-all.ps1
```

### Step 2: Start the Testnet Node (`armcoind`)

```bash
# In testnet mode (default when -network is omitted)
./armcoind -network testnet
```

Expected startup output:
```text
==================================================
 ARMAXCOIN (ARMX) - DAEMON & P2P NODE
==================================================
[+] Network Profile  : TESTNET
[+] Blockchain Height: ...
[P2P] Loaded known peers from .../peers.json
[DNS Seed] Resolved 'testnet-seed1.armax-coin.cloud'...
[OK] RPC Daemon active on 127.0.0.1:19333 | P2P Network active on port 19335
[P2P] Node actively listening on port 19335 (Network ID: ARMX-TESTNET-1)...
```

### Step 3: Check Node Status

In a second terminal:
```bash
curl -s http://127.0.0.1:19333/stats | jq .
```
Response will display current block height, difficulty, cumulative chainwork, peer count, and network identity.

---

## 4. Wallet Management (`armwallet`)

Armaxcoin uses post-quantum ML-DSA-65 keys derived from standard 24-word BIP-39 recovery mnemonics. Keystores are protected using Argon2id password encryption.

### Create a New Testnet Wallet

```bash
./armwallet -network testnet -create -wallet testnet_wallet.json
```
You will be prompted to enter a strong passphrase and securely record your 24-word recovery phrase.

### Check Wallet Balance

```bash
# Display the address stored in the keystore, then query its balance
./armwallet -network testnet -address -wallet testnet_wallet.json
./armwallet -network testnet -balance <WALLET_ADDRESS>
```

---

## 5. Sending Testnet Transactions (`armsend`)

To send testnet coins to another address:

```bash
./armsend -network testnet \
  -wallet testnet_wallet.json \
  -to <RECIPIENT_ADDRESS> \
  -amount 10.0 \
  -fee 0.001
```

All transaction signing occurs strictly client-side within `armsend`. Your encrypted wallet private keys are never transmitted to the node or network.

---

## 6. Official Testnet Seed Nodes

If your node does not discover peers automatically via DNS, you can manually connect to the official testnet seeds:

- `testnet-seed1.armax-coin.cloud:19335`
- `testnet-seed2.armax-coin.cloud:19335`
- `testnet-seed3.armax-coin.cloud:19335`

Manual peer registration via RPC requires configured RPC credentials:
```bash
curl -X POST http://127.0.0.1:19333/peers \
  -u "$ARMX_RPC_USER:$ARMX_RPC_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{"peer": "testnet-seed1.armax-coin.cloud:19335"}'
```

---

## 7. Troubleshooting

- **Node cannot find peers:**
  Ensure outbound TCP port `19335` is not blocked by your local router or firewall.
- **Port already in use:**
  Verify another `armcoind` process is not running:
  ```bash
  # Linux
  lsof -i :19335
  # Windows
  netstat -ano | findstr 19335
  ```
- **Corrupted testnet database:**
  To perform a clean resynchronization:
  ```bash
  # Stop node, then remove testnet data
  rm -rf ~/.armax/testnet/blockchain.db ~/.armax/testnet/peers.json
  ./armcoind -network testnet
  ```
