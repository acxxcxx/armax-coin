# Armaxcoin Public Testnet Operations & Node Operator Guide

Welcome to the public open testnet for **Armaxcoin** (`armax-coin`). This comprehensive guide provides complete, step-by-step instructions for setting up a full node, generating a post-quantum wallet, syncing the blockchain, mining, testing transactions, and operating production seed/validator infrastructure.

> [!WARNING]
> **TESTNET WARNING:**
> The Armaxcoin public testnet is an experimental environment for development, integration, and security testing. Testnet coins (`tARMX`) currently have **no market value**, cannot be redeemed for fiat, and carry no guaranteed financial return; however, verified external testnet mining rewards are planned for a future Mainnet migration program with non-final, TBA conversion terms (see [TREASURY_MIGRATION_PLAN.md](governance/TREASURY_MIGRATION_PLAN.md)). Chain state may be reset if critical consensus issues are identified.

---

## 1. Network Profile & Port Specifications

The Armaxcoin Public Testnet (`ARMX-TESTNET-1`) operates under isolated network parameters defined in `core/network.go`. Testnet nodes NEVER peer with or accept blocks from Mainnet.

**Ticker:** The GUI, explorer, and CLI (`armsend`, `armwallet`) display `tARMX` on testnet. Network ID, on-chain values, and addresses are unchanged. Mainnet uses `ARMX`.

| Service | Port | Network Interface Binding | Firewall Policy |
|---|---|---|---|
| **P2P Gossip & Block Streaming** | **19335** | `0.0.0.0:19335` (All interfaces) | **ALLOW INBOUND** (Public TCP) |
| **JSON-RPC Management API** | **19333** | `127.0.0.1:19333` (Loopback only) | **DENY INBOUND** (Localhost Only) |
| **Web Explorer & Inspector** | **19341** | *(Planned / Removed)* | Inactive / Unreleased |

| Parameter | Testnet Value | Note |
|---|---|---|
| **Network ID** | `ARMX-TESTNET-1` | Enforces network isolation from Mainnet/Regtest |
| **Target Block Time** | `120` seconds (2 minutes) | Controlled by Armax-ASERT-AW DAA (Block #13,335+; previously TEDR at #13,293 and LWMA-1 $N=60$) |
| **Initial Block Reward** | `60.00000000 tARMX` | Halves every 1,050,000 blocks |
| **PoW Algorithm** | ArmaxHash v2 (active from block #2500) | Chained BLAKE2b Stream-Fast (CPU & ARM optimized). Historical: SHA-256d (0-519), ArmaxHash v1 (520-2499) |
| **Signature Scheme** | NIST FIPS 204 ML-DSA-65 | Post-quantum lattice-based digital signatures |
| **Data Directory** | Linux: `~/.armax/testnet`<br>Windows: `%APPDATA%\Armax\testnet` | Default database and keystore location |

> [!CRITICAL]
> **RPC Security Isolation (Loopback Only):**
> The JSON-RPC daemon (`armcoind` port 19333 on testnet, 19332 on mainnet) exposes internal node mutation and status endpoints (`/mine`, `/block/submit`, `/peers`). It **MUST NEVER** be bound to `0.0.0.0` or exposed to public inbound internet traffic.
> Local administrative CLI tools authenticate via the ephemeral, restrictive `armax.cookie` file on localhost. If remote administrative access is required, node operators must use an SSH tunnel (`ssh -L 19333:127.0.0.1:19333 user@server`) or an authenticated reverse proxy with TLS.

---

## 2. System Requirements & Quickstart

### System Requirements

- **Supported Platforms:**
  - Linux `amd64` (x86_64 servers & desktops, Ubuntu 22.04+, Debian 12+)
  - Linux `arm64` (Raspberry Pi 4/5, Armbian, AWS Graviton)
  - Windows `amd64` (Windows 10/11, Windows Server 64-bit)
  - macOS `arm64` (Apple Silicon M1/M2/M3/M4)
- **Minimum Hardware:**
  - 2 CPU cores
  - 2 GB RAM (4 GB recommended if running both node and miner)
  - 10 GB free disk space (SSD recommended)
- **Toolchain (if building from source):**
  - **Go 1.27.4 or higher** (required for `crypto/mldsa` and `GO-2026-6859` remediation)
  - Git

### Step 1: Clone and Build

```bash
git clone https://github.com/armaxcoincore/armax-coin.git
cd armax-coin

# Verify Go version (requires Go 1.27+)
go version

# Build all binaries
go build -o armcoind main.go
go build -o armwallet ./cli/armwallet
go build -o armsend ./cli/armsend
go build -o armminer ./miner
CGO_ENABLED=0 go build -ldflags="-H=windowsgui" -o armgui.exe ./gui   # Windows
CGO_ENABLED=0 go build -o armgui ./gui                               # Linux / macOS
```

On Windows (PowerShell):
```powershell
.\scripts\build.ps1
```

### Step 2: Launch Desktop Wallet GUI v2 (`armgui`) or Headless Node (`armcoind`)

- **1-Click Desktop GUI (`armgui`):** Launch `./armgui.exe` (Windows native WebView2 window with **Minimize to System Tray**, Light/Dark themes, Multi-Wallet switcher, `bbolt` local paged history cache, ML-DSA-65 fee estimator, and **CPU Miner** tab featuring **`"Waiting for Sync"`** pre-sync protection) or `./armgui` on Linux/WSL/macOS (automatically verifies POSIX `0600` writability and falls back cleanly from `/mnt/*` to `~/.config/ArmaxCoin`, opening your host browser with fallback `Launch URL` console output). If no standalone node is running on `127.0.0.1:19333`, `armgui` boots an embedded testnet node automatically. Clicking Start in the CPU Miner tab while initial sync is still running queues mining in `"Waiting for Sync"` mode (`WAITING FOR SYNC`) without issuing `/mine` RPC calls until `/stats` confirms `synced: true`.
- **Headless CLI Daemon (`armcoind`):**

```bash
# In testnet mode (default when -network is omitted)
./armcoind -network testnet
```

Expected startup output:
```text
==================================================
 ARMAXCOIN (tARMX) - DAEMON & P2P NODE
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
Response displays the current block height, difficulty, cumulative chainwork, peer count, and network identity.

---

## 3. Seed Node Infrastructure & DNS Setup

A dedicated testnet seed node provides reliable 24/7 block dissemination:
- **Operating System:** Ubuntu 22.04 LTS or Debian 12 (x86_64 or aarch64/ARM64).
- **Compute:** Minimum 2 vCPUs, 2 GB RAM, 20 GB SSD storage.
- **Network:** Public static IPv4 address, minimum 10 Mbps sustained uplink.
- **DNS Seed Configuration:**
  - Create standard DNS `A` records pointing to your public seed IP (e.g., `testnet-seed1.armax-coin.cloud` -> `<YOUR_SEED_PUBLIC_IP>`).
  - Set DNS TTL to **300 seconds** (5 minutes) to facilitate rapid failover if an IP address migrates.
  - Multi-seed round-robin: Multiple seed node IPs can be mapped to the same apex or subdomain (`testnet-seed.armax-coin.cloud`) to enable automatic client load balancing and peer discovery.
- **Official Testnet Seed Nodes:**
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

## 4. Firewall Configuration (UFW) & Health Monitoring

Enforce strict ingress policies on public seed and validator nodes:

```bash
# Allow SSH management
sudo ufw allow 22/tcp

# Allow Testnet P2P gossip from public peers
sudo ufw allow 19335/tcp comment 'Armax Testnet P2P'

# Explicitly ensure Testnet RPC is restricted to loopback (do NOT expose 19333!)
sudo ufw deny 19333/tcp comment 'Armax Testnet RPC Private'

# Enable firewall
sudo ufw enable
sudo ufw status verbose
```

### Automated Seed Health Verification:
Use the built-in health monitor script to verify connectivity and security:
```bash
# Verify local seed node (checks P2P port 19335, queries block height, peer count, version)
./scripts/check-seed.sh 127.0.0.1 testnet

# Audit remote public seed from outside (verifies P2P is OPEN and RPC is BLOCKED)
./scripts/check-seed.sh testnet-seed1.armax-coin.cloud testnet
```

---

## 5. Systemd Service Deployment

Create an unprivileged system user and install the testnet systemd unit:

```bash
# 1. Create dedicated armax system user and directory
sudo useradd -r -m -d /var/armax -s /bin/bash armax
sudo mkdir -p /var/armax/testnet
sudo chown -R armax:armax /var/armax

# 2. Deploy binary to /usr/local/bin
sudo cp armcoind /usr/local/bin/armcoind
sudo chmod 755 /usr/local/bin/armcoind

# 3. Install testnet service unit file
sudo cp systemd/armcoind-testnet.service /etc/systemd/system/armcoind-testnet.service

# 4. Reload systemd daemon, enable, and start service
sudo systemctl daemon-reload
sudo systemctl enable --now armcoind-testnet

# 5. Monitor service startup and initial block download
sudo journalctl -u armcoind-testnet -f
```

---

## 6. Mining on Testnet

Mining on testnet is executed using the multi-threaded `armminer` CPU mining engine:

```bash
# 1. Create a testnet wallet or generate reward address
./armwallet -network testnet -create -wallet my_testnet_wallet.json

# 2. Extract public address (Bech32 armx1...)
ADDR=$(grep -o '"address": "[^"]*' my_testnet_wallet.json | cut -d'"' -f4)

# 3. Launch the CPU miner with 4 worker threads (connects to localhost:19333 testnet RPC by default)
./armminer -network testnet -miner "${ADDR}" -threads 4

# 4. Or benchmark local CPU hashing speed:
./armminer -benchmark
```

### Mining Architecture & Telemetry
- **Local Multi-Threaded Engine:** `armminer` queries `/getblocktemplate` from `armcoind` (automatically waiting if the node returns `HTTP 503` during initial peer sync), constructs the candidate block with coinbase reward, and runs parallel workers defaulting to physical CPU core count (`GetOptimalCPUWorkerThreads`) using integrated `armaxhash.V2JobHasher` stack prefix caching (`0 B/op`, `0 allocs/op`), 64-byte cache-line padded worker slots, and non-overlapping `1<<36` positive nonce spans.
- **Proof-of-Work Algorithm Migration:**
  - **Height $0 \le H < 520$:** Legacy SHA-256d PoW. All historical blocks remain valid and preserved.
  - **Height $520 \le H < 2500$:** **ArmaxHash v1** (BLAKE2b-512 prehash derived from the network ID and header, Argon2id with 2,048 KiB, 1 lane, 2 iterations, then BLAKE2b-256 finalization; see `docs/CONSENSUS_AND_NETWORK.md`).
  - **Height $H \ge 2500$ (active on testnet):** Stream-Fast **ArmaxHash v2** (BLAKE2b-512 over the `ARMX-POW-V2\0` domain, version, network ID, and header, followed by BLAKE2b-256). `V2JobHasher` caches Stage-1 header prefixes inline on the worker stack with 0 allocations/op.
- **Console Telemetry:** Prints real-time 1s Sliding-EMA hashrate metrics (`H/s`, `kH/s`, `MH/s`) and solve timestamps every stats interval.
- **Dynamic Tip Adaptation & Clean Cancellation:** Automatically aborts stale rounds on tip changes, waits through `HTTP 503 syncing` states rather than mining behind peers, and supports explicit `POST /mine/stop` cancellation with clean drain handling (avoiding spurious `HTTP 409 Conflict` notices on quick restarts).
- **Coinbase Maturity (10 Confirmations on Testnet, 100 on Mainnet):** Freshly mined block rewards enter an on-chain immature queue and cannot be spent until 10 blocks (testnet) or 100 blocks (mainnet) have been confirmed on top of them (consensus-enforced, storage schema v3).
- **Daemon Internal Mining:** Alternatively, `armcoind` can mine in the background using standard flags:
  ```bash
  # Canonical flags: -mine and -miningthreads (legacy aliases: -gen and -genproclimit)
  ./armcoind -mine -miningthreads 4 -miner "${ADDR}"
  ```

### Hashrate and Difficulty Targets
- **Legacy Target (H < 520):** `0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (SHA-256d baseline).
- **ArmaxHash v1 PoW Limit (H = 520 bootstrap):** `000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (~4,096 ArmaxHash v1 evaluations).
- **Historical LWMA-1 Era (H < 13293):** Linear Weighted Moving Average across a 60-block window (LWMA-1).
- **Anti-Stall TEDR Era (13293 <= H < 13335):** Fallback to emergency target (`ArmaxHashPowLimitHex`, Difficulty 3) after 1,200s solve time, normalized in the LWMA window.
- **Armax-ASERT-AW Era (H >= 13335):** Canonical pure integer `aserti3-2d` difficulty adjustment with $\tau = 3600\text{s}$, 4-hour surge cap (`14400s`), and TEDR Boost (`7200s`).
- **Dynamic Target Bounds:** Target adjusts dynamically to maintain the 120-second target block time. During ArmaxHash operation, the easiest allowable target is capped by the profile ceiling.
- **Target Block Time:** 120 seconds (2 minutes).

### Local Multi-Node Testing (29000-series Ports)
For running multiple local nodes on the same host, use ports in the 29000-range with separate `-datadir`:
```bash
# Node 1 (Bootstrap node)
./armcoind -network testnet -rpcport 29332 -p2pport 29334 -datadir ./node1_data

# Node 2 (Peer connecting to Node 1)
./armcoind -network testnet -rpcport 29336 -p2pport 29338 -peers 127.0.0.1:29334 -datadir ./node2_data

# Miner pointing to Node 1
./armminer -network testnet -rpcport 29332 -miner armx1...

# Wallet checking Node 1 balance
./armwallet -network testnet -rpcport 29332 -balance armx1...
```
*Note: `-rpcport` and `-p2pport` are preferred; legacy `-port` and `-p2p` are retained as backward-compatible aliases with a deprecation warning.*

---

## 7. Client CLI Operations: Wallet & Transactions

Armaxcoin uses post-quantum ML-DSA-65 keys derived from standard 24-word BIP-39 recovery mnemonics. Keystores are protected using Argon2id password encryption.

### Wallet Management (`armwallet`)

#### Create a New Testnet Wallet:
```bash
./armwallet -network testnet -create -wallet testnet_wallet.json
```
You will be prompted to enter a strong passphrase and securely record your 24-word recovery phrase.

#### Check Wallet Address and Balance:
```bash
# Display the address stored in the keystore
./armwallet -network testnet -address -wallet testnet_wallet.json

# Query the address balance from the running node
./armwallet -network testnet -balance <WALLET_ADDRESS>
```

### Sending Testnet Transactions (`armsend`)

To send testnet coins to another address:
```bash
./armsend -network testnet \
  -wallet testnet_wallet.json \
  -to <RECIPIENT_ADDRESS> \
  -amount 10.0 \
  -fee 0.00011
```
Or specify amounts directly in atomic Armoids ($1\text{ tARMX} = 10^8\text{ Armoids}$):
```bash
./armsend -network testnet \
  -wallet testnet_wallet.json \
  -to <RECIPIENT_ADDRESS> \
  -amount-armoids 1000000000 \
  -fee-armoids 11000
```

All transaction signing occurs strictly client-side within `armsend`. Your encrypted wallet private keys are never transmitted to the node or network.

---

## 8. Backup and Disaster Recovery

The testnet blockchain state is stored in `<datadir>/testnet/blockchain.db` (bbolt ACID key-value store).

### Cold Database Backup
```bash
# Stop daemon before copying bbolt database
sudo systemctl stop armcoind-testnet

# Create compressed backup archive
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
sudo tar -czvf /var/backups/armax_testnet_backup_${TIMESTAMP}.tar.gz -C /var/armax testnet/blockchain.db

# Restart daemon
sudo systemctl start armcoind-testnet
```

### Restoration Runbook
```bash
sudo systemctl stop armcoind-testnet
sudo rm -rf /var/armax/testnet/blockchain.db
sudo tar -xzvf /var/backups/armax_testnet_backup_${TIMESTAMP}.tar.gz -C /var/armax
sudo chown -R armax:armax /var/armax/testnet
sudo systemctl start armcoind-testnet
```

---

## 9. Node Upgrade and Rollback Runbook (v1.0.12 / v1.0.11)

> [!IMPORTANT]
> **ACTIVE TESTNET CONSENSUS FORKS (BLOCKS 13,293, 13,335 & 20,000):**
> - **Block 13,293 (`v1.0.9` — Anti-Stall TEDR, Active):** Activates Testnet Anti-Stall Emergency Difficulty Adjustment (`AntiStallActivationHeight = 13293`). When a candidate block's timestamp exceeds the parent block's timestamp by more than **1,200 seconds (20 minutes)**, the candidate block qualifies for the emergency fallback target (`ArmaxHashPowLimitHex`, Difficulty 3) with Model 3 Synthetic Target Normalization (`720s` solve time, anchored to `lastNormalTarget`).
> - **Block 13,335 (`v1.0.10` — Armax-ASERT-AW, Active):** Transitions the Testnet DAA from LWMA-1 ($N=60$) to **Armax-ASERT-AW (`aserti3-2d`)** (`ASERTActivationHeight = 13335`, anchored to Block `#13,334`, $T = 120\text{s}$, $\tau = 3600\text{s}$, per-block surge cap `14,400s`, TEDR boost `7,200s`).
> - **Block 20,000 (`v1.0.11+` — Unified Proposal C6 + `TxBinaryV1`, Active):** Enforces `FutureTimestampLimitActivationHeight = 20000` (**`+600s`** forward timestamp limit with adaptive `5s`/`30s` miner margins) and `BlockBinarySizeActivationHeight = 20000` (`TxBinaryV1` binary accounting allowing **375 user ML-DSA-65 transactions** per 2 MB block). **`v1.0.12`** preserves 100% consensus & P2P wire compatibility with `v1.0.11` (zero hard forks or activation-height changes) while adding Desktop GUI v2 (`"Waiting for Sync"` queue, WebView2, Windows Tray, Multi-Wallet), Node Sync Gate (`HTTP 503`), integrated multi-thread CPU optimization (`V2JobHasher` + padded worker slots + physical-core default), Stratum TLS, additive Node RPC, and Go `1.27.4`.
> - All node operators, miners, and GUI users should upgrade cleanly to **`v1.0.12`** while keeping clocks synchronized with NTP.

> [!CAUTION]
> **STORAGE COMPATIBILITY & ROLLBACK WARNING:**
> BoltDB record compression is **disabled by default**. Compressed records created by v1.0.7+ **cannot be read by v1.0.6**.
> If record compression was enabled (`-store-compression`), you **MUST** convert the database back to plain format before starting a v1.0.6 binary:
> ```bash
> ./armcoind -network testnet -datadir /var/armax -store-rewrite plain
> ```
> Always back up `blockchain.db` before executing store conversion.

### Seamless Upgrade Procedure to v1.0.12
1. Verify the current tip height and peer connectivity:
   ```bash
   curl -s http://127.0.0.1:19333/status | jq .
   ```
2. Ensure system clock is NTP-synchronized:
   ```bash
   # Linux
   sudo timedatectl set-ntp true
   timedatectl status
   # Windows
   w32tm /resync
   ```
3. Gracefully stop the running service:
   ```bash
   sudo systemctl stop armcoind-testnet
   ```
4. Back up the database and working binary:
   ```bash
   sudo cp /usr/local/bin/armcoind /usr/local/bin/armcoind.v1011.bak
   sudo tar -czvf /var/backups/armax_db_pre_v1012_$(date +%F).tar.gz -C /var/armax testnet/blockchain.db
   ```
5. Install the new v1.0.12 binary:
   ```bash
   sudo cp armcoind /usr/local/bin/armcoind
   sudo chmod 755 /usr/local/bin/armcoind
   ```
6. Start service and verify log output:
   ```bash
   sudo systemctl start armcoind-testnet
   sudo journalctl -u armcoind-testnet -n 50 --no-pager
   ```
7. Verify running software version is `"1.0.12"`:
   ```bash
   curl -s http://127.0.0.1:19333/status | jq .version
   # Expected output: "1.0.12"
   ```

### Emergency Rollback Procedure (to v1.0.11)
Because `v1.0.12` and `v1.0.11` share identical consensus and BoltDB state schemas:
1. Stop the daemon:
   ```bash
   sudo systemctl stop armcoind-testnet
   ```
2. Restore the `v1.0.11` binary (`armcoind.v1011.bak`) and restart `armcoind-testnet` (note: do **not** roll back below `v1.0.11` on a post-Block `#20,000` chain). See [`releases/v1.0.12.md`](releases/v1.0.12.md) for full details.

---

## 10. Manual Testnet Faucet Operator Guide

To prevent sybil attacks, resource draining, and spam without introducing complex captcha services, Armaxcoin testnet operates on a **Manual Operator Faucet Model**.

> [!CAUTION]
> **DO NOT** deploy automated, unauthenticated public web faucets exposing hot private keys on the public internet. Testnet faucets must be managed manually by designated community operators.

### Operator Workflow:
1. **Faucet Wallet Initialization:**
   The operator generates an isolated faucet keystore on a secured machine:
   ```bash
   ./armwallet -network testnet -create -wallet faucet_keystore.json
   ```
2. **Funding the Faucet:**
   The operator mines blocks or receives testnet coins from genesis distribution.
3. **Fulfilling Community Requests:**
   Community members request testnet coins via the official forum or Discord by submitting their Bech32 address (`armx1...`).
4. **Disbursing Coins:**
   The operator executes manual single transactions:
   ```bash
   export ARMX_WALLET_PASSPHRASE="operator_secret_passphrase"
   ./armsend -network testnet \
     -wallet faucet_keystore.json \
     -to armx1<requester_address> \
     -amount-armoids 100000000 \
     -fee-armoids 11000
   ```
   *(Sends 1.0 tARMX [100,000,000 Armoids] with a fee of 11,000 Armoids).*

---

## 11. Troubleshooting & Diagnostics

- **Node cannot find peers:**
  Ensure outbound TCP port `19335` is not blocked by your local firewall or ISP router.
  Check DNS resolution of seeds:
  ```bash
  nslookup testnet-seed1.armax-coin.cloud
  ```
- **Port already in use:**
  Verify another `armcoind` process is not already running:
  ```bash
  # Linux / macOS
  lsof -i :19335
  # Windows
  netstat -ano | findstr 19335
  ```
- **Connection refused on RPC:**
  Ensure `armcoind` has completed startup and created `armax.cookie`. The RPC daemon binds strictly to `127.0.0.1:19333` on testnet.
- **Mining returns `HTTP 503` or GUI stays in `"Waiting for Sync"`:**
  Your node is still completing Initial Block Download (IBD) or awaiting peer tip verification (`synced: false` in `/stats`). Leave the node connected until `sync_progress` reaches `100%`; `armgui` and `armminer` automatically begin hashing as soon as synchronization finishes, protecting your public IP from being banned by seed nodes for relaying behind-tip blocks.
- **Corrupted testnet database:**
  To perform a clean resynchronization:
  ```bash
  # Linux
  rm -rf ~/.armax/testnet/blockchain.db ~/.armax/testnet/peers.json
  ./armcoind -network testnet
  ```
  On Windows:
  ```powershell
  Remove-Item "$env:APPDATA\Armax\testnet\blockchain.db", "$env:APPDATA\Armax\testnet\peers.json"
  .\armcoind.exe -network testnet
  ```
