# Armaxcoin (`armax-coin`)

**Canonical Repository:** [github.com/acxxcxx/armax-coin](https://github.com/acxxcxx/armax-coin)<br>
**Current Release:** `v1.0.1` (Public Open Testnet)<br>
**Go Module:** `github.com/acxxcxx/armax-coin`

Armaxcoin is an open-source, experimental Layer-1 Proof-of-Work (PoW) cryptocurrency engineered for post-quantum security, accessibility, and high performance across personal computers and ARM-based single-board devices (Armbian, Raspberry Pi, Orange Pi).

> [!WARNING]
> **EXPERIMENTAL OPEN TESTNET NOTICE:**
> Armaxcoin is experimental software under active development and has **not** undergone an independent third-party security audit. Testnet coins (`ARMX`) have **no monetary value**, no market liquidity, no guarantee of conversion, and no investment utility.
> The mainnet network (`ARMX-MAINNET-1`) is **locked** in source (`core.MainnetLaunched = false`). Invoking `-network mainnet` is strictly rejected by all binaries. All operations, mining, and testing are active on the **Public Testnet (`ARMX-TESTNET-1`)**.

---

## Authoritative Documentation Index

| Guide | Description |
|---|---|
| **[Open Testnet Guide](docs/TESTNET_GUIDE.md)** | Step-by-step instructions for installing, syncing, and testing transactions |
| **[CPU Mining Guide](docs/MINING_GUIDE.md)** | Mining with `armminer`, thread tuning, memory footprints, and tokenomics |
| **[Node Operations Guide](docs/NODE_OPERATIONS.md)** | Running full nodes, seed nodes, systemd units, TLS 1.3, and TOFU certificate security |
| **[Consensus & Network Specification](docs/CONSENSUS_AND_NETWORK.md)** | Mathematical specifications for LWMA-1, ArmaxHash, ML-DSA-65, and reorg limits |
| **[Contributor Reward Program (Proposal)](docs/CONTRIBUTOR_REWARDS.md)** | Proposed community incentive framework (marked `PROPOSED -- REQUIRES MAINTAINER APPROVAL`) |
| **[Security Policy & Disclosures](SECURITY.md)** | Threat model, responsible vulnerability reporting, and known limitations |
| **[Contribution Guidelines](CONTRIBUTING.md)** | Development workflow, code standards, Conventional Commits, and test gates |
| **[Code of Conduct](CODE_OF_CONDUCT.md)** | Community pledge and standards of behavior |
| **[Accessibility Statement](ACCESSIBILITY.md)** | Accessibility policies for GUI, CLI, documentation, and hardware |
| **[Release Checklist](docs/RELEASE_CHECKLIST.md)** | Publication gates and release verification runbook |
| **[Changelog](CHANGELOG.md)** | Complete version history adhering to Keep a Changelog |

---

## Consensus & Tokenomics Parameters

| Parameter | Specification | Details / Rules |
|---|---|---|
| **Consensus Mechanism** | Proof-of-Work (PoW) | Dynamic 256-bit numerical target difficulty adjustment via LWMA-1 ($N=60$) |
| **Fork Choice Rule** | Heaviest Chain (Satoshi Chainwork) | Cumulative Proof-of-Work ($\sum \frac{2^{256}}{\text{Target} + 1}$) strictly beats low-difficulty chains |
| **Max Reorg Depth** | 720 Blocks (~24 hours) | Finality boundary preventing deep history overwrites while accommodating natural network splits |
| **Max Block Transactions** | 1,000 Txs / Block | Hard consensus ceiling preventing block bloat and memory exhaustion attacks |
| **Block Size Limits** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB | MaxIncomingPacketSize = 4 MB; verified block capacity: up to 185 user transactions (186 total with coinbase; 10,761 B/tx in JSON wire format, see docs/PERF.md) |
| **Block Transactions** | Binary Merkle Tree Root | Canonical 32-byte Merkle root in header enabling SPV Light Wallet proofs |
| **Hashing Algorithm** | ArmaxHash (Argon2id 2MB, $H \ge 520$) / Legacy SHA-256d ($H < 520$) | Memory-hard Argon2id (2048 KiB, 1 pass, 1 lane) activated at height 520; historical blocks 0..519 preserved under SHA-256d; no absolute ASIC resistance is claimed |
| **Digital Signatures** | NIST FIPS 204 (ML-DSA-65) | Quantum-resistant signatures (NIST FIPS 204, ML-DSA-65): Public key 1,952 bytes, Signature 3,309 bytes |
| **Address Derivation** | BIP-173 Bech32 (`armx1...`) | Canonical lowercase prefix `armx` + 32-byte SHA-256(SigAlg \|\| PubKey) payload (63 chars total) |
| **Wallet Recovery** | BIP-39 Deterministic Mnemonic | Standard 24-word mnemonic (12-word accepted with warning), HKDF-SHA256, Argon2id encrypted keystore |
| **P2P Transport** | JSON over TCP with TLS 1.3 | Wire protocol is newline-delimited JSON over TCP; TLS 1.3 and TOFU pinning are enabled by default; plaintext is legacy-testnet-only |
| **P2P Synchronization** | Chunked Range & Chainwork Sync | Range-based block streaming (`GET_BLOCK_RANGE`) with cumulative chainwork reorg and parallel sync workers |
| **P2P Address Book** | 1,000 Addrs (`tried` & `new`) | Separate address book from active connections (MaxPeers=50), dead peer eviction after 3 failures |
| **P2P Eclipse Defense** | Subnet Connection Limiter | Maximum 3 concurrent peers/connections per IPv4 `/24` (or IPv6 `/48`) subnet (`disable_subnet_limit` available) |
| **P2P Flood Defense** | Seen-Filter Hash Cache & Orphan Pools | LRU deduplication cache; bounded 100 orphan blocks (max 10/peer) & 500 orphan txs pool |
| **DNS Seeds (Testnet)** | Dedicated Testnet Seeds | `testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335` |
| **Storage Engine** | bbolt ACID Key-Value Store | State snapshots every 1,000 blocks, undo logging (1,440 retention), and crash recovery |
| **Target Block Time** | 120 seconds (~2 minutes) | Balanced propagation window ensuring ultra-low orphan rates across heterogeneous nodes |
| **Difficulty Algorithm** | LWMA-1 (Linear Weighted Moving Average) | Continuous 256-bit numerical target adjusted per block across 60-block window; dynamic floor down to Difficulty 1 for ArmaxHash |
| **Candidate Block Extraction** | `/getblocktemplate` JSON RPC | Bounded block candidate endpoint (MaxBlockTransactions - 1) for external miners |
| **RPC Security** | Loopback Binding & Cookie/Basic Auth | Mutation endpoints strictly require auth/cookie or `-rpcuser`/`-rpcpassword`; `-rpcinsecure-localhost` flag; /chain bounded to 50 blocks |
| **Initial Block Reward** | 60 ARMX (6,000,000,000 Armoids) | Awarded to miner via single coinbase transaction at index 0 |
| **Halving Interval** | Every 1,050,000 blocks (~4 years) | Reward cuts in half ($60 \to 30 \to 15 \to \dots$ ARMX) |
| **Maximum Total Supply** | 126,000,000 ARMX | Strict hard cap (12,600,000,000,000,000 Armoids, 8 decimal places; real theoretical emission: 125,999,999.874 ARMX) |
| **Supply Accounting** | $O(1)$ Incremental State Cache | Constant-time circulating supply tracking without block history traversal |
| **Genesis Allocation** | 60 ARMX (Burned) | Hardcoded deterministic Genesis block burned to zero address (`armx1qqqq...plwcdd`) without premine |
| **Transfer Settlement (Standard)** | **6 Confirmations (Both Testnet & Mainnet)** | Standard account transfers are safely confirmed and settled after **6 block confirmations** on **BOTH Testnet and Mainnet** (~12 minutes). High-value payments recommend 12 confirmations for irreversible finality. Standard transfers have NO immature delay. |
| **Coinbase Reward Maturity** | Profile-Enforced Lock Queue | Newly mined block rewards enter an immature lock queue before becoming spendable: **Public Testnet: 10 confirmations**, **Mainnet: 100 confirmations**, Regtest: 1 confirmation. |
| **Network Profiles** | Mainnet (Unreleased / Locked) & Testnet Profiles | Mainnet (`ARMX-MAINNET-1`: P2P 19334, RPC 19332 - locked / rejected) \| Testnet (`ARMX-TESTNET-1`: P2P 19335, RPC 19333 - active) |

---

## Architecture & Components

| Module | Path | Description |
|---|---|---|
| **Core Blockchain** | `core/` | PoW consensus logic, LWMA-1, Merkle tree, HTLC layer-2 (Planned), PQC signatures, fork resolution, mempool, and persistence. |
| **Node Daemon & RPC** | `node/server.go`, `node/auth.go` | Thread-safe HTTP REST API server with cookie/basic auth, rate-limiting, and `getblocktemplate`. |
| **P2P Network** | `node/p2p.go`, `node/p2p_crypto.go` | TLS 1.3 encrypted P2P gossip mesh and subnet limiter. |
| **Wallet Engine** | `wallet/wallet.go`, `wallet/mnemonic.go` | BIP-173 Bech32 addresses, BIP-39 24-word (and 12-word legacy) mnemonic recovery, and keystore management. |
| **CLI Sender** | `cli/armsend/main.go` (`armsend`) | CLI tool for signing and broadcasting transactions with fee and nonce validation. |
| **CLI Wallet** | `cli/armwallet/main.go` (`armwallet`) | CLI tool for generating BIP-39 keys and querying confirmed/available balances. |
| **CPU Miner** | `miner/miner.go` (`armminer`) | Automated continuous CPU miner client with POST request and failover support. |
| **Desktop GUI** | `gui/main.go` (`armgui`) | Desktop GUI application bound strictly to localhost with embedded telemetry, wallet manager, and block inspector (Web Explorer: Planned / Removed). |

---

## Prerequisites & Environment

- **Go Compiler:** Go `1.27` or higher (required for standard library `crypto/mldsa` and toolchain compatibility).
- **Supported Architectures:**
  - `linux/arm64` (Armbian, Raspberry Pi 4/5, Orange Pi)
  - `linux/amd64` (Ubuntu, Debian, CentOS VPS/Servers)
  - `windows/amd64` (Windows 10/11)
  - `darwin/arm64` (Apple Silicon M1/M2/M3)
- **CI / Build Pipeline:** Multi-platform automated binaries are generated via GitHub Actions (`.github/workflows/release.yml`) on tagged releases.
- **Reference Guides:**
  - [Build Guide & Reproducible Instructions](docs/BUILD.md)
  - [Launch Checklist & Gate Procedures](docs/LAUNCH_CHECKLIST.md)
  - [Seed Node Operations & Systemd Guide](docs/TESTNET.md)

---

## Quickstart: Public Testnet (Windows, Linux, ARM64)

Follow these steps to join the public testnet (`ARMX-TESTNET-1`). All binaries default to testnet while mainnet remains locked.

### 1. Download & Verify Release Artifacts

Download pre-compiled binary packages and `SHA256SUMS` from [GitHub Releases](https://github.com/acxxcxx/armax-coin/releases):

#### On Linux / ARM64:
```bash
# Download package and checksum
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.1/armax-v1.0.1-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.1/SHA256SUMS

# Verify checksum integrity
sha256sum --check --ignore-missing SHA256SUMS

# Extract and make executable
tar -xzf armax-v1.0.1-linux-amd64.tar.gz
chmod +x armcoind armminer armsend armwallet
```

#### On Windows (PowerShell):
```powershell
# Compute SHA-256 hash of downloaded zip
Get-FileHash -Algorithm SHA256 .\armax-v1.0.1-windows-amd64.zip

# Compare against the published hash in SHA256SUMS
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.1-windows-amd64.zip"
```

### 2. Launch Testnet Node Daemon

Start the node daemon. By default, it connects to testnet seed nodes, binds P2P gossip to port `19335`, and restricts RPC to loopback `127.0.0.1:19333`:

```bash
# Linux / macOS / ARM64
./armcoind

# Windows
.\armcoind.exe
```

Expected startup output:
```text
WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited
[Network] Active Network Profile: ARMX-TESTNET-1
[Storage] Initialized state database at ~/.armax/testnet/data.db
[P2P] Listening for peers on 0.0.0.0:19335 (TLS 1.3 + TOFU)
[RPC] HTTP RPC daemon listening on 127.0.0.1:19333 (Loopback only)
```

### 3. Create a Testnet Wallet Keystore

Generate a new quantum-safe wallet with a 24-word recovery phrase:

```bash
# Linux / macOS / ARM64
./armwallet -create -wallet my_testnet_wallet.json

# Windows
.\armwallet.exe -create -wallet my_testnet_wallet.json
```
- Enter a passphrase (min 12 characters, confirmed twice).
- Securely write down the one-time 24-word BIP-39 mnemonic displayed on your terminal.
- Note your generated address (starts with `armx1...`).

### 4. Start Mining Testnet Blocks

### 4. Start Mining Testnet Blocks

Mine testnet blocks using your local CPU (multi-threaded, with real-time hashrate telemetry):

```bash
# Linux / macOS / ARM64 (e.g., 4 CPU worker threads)
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4

# Windows
.\armminer.exe -miner <YOUR_ARMX_ADDRESS> -threads 4

# Run CPU hashing benchmark (hash/s per thread & aggregate total)
.\armminer.exe -benchmark
```

### 5. Check Balance & Maturity

Query your address balance from the local daemon:

```bash
# Linux / macOS / ARM64
./armwallet -balance <YOUR_ARMX_ADDRESS>

# Windows
.\armwallet.exe -balance <YOUR_ARMX_ADDRESS>
```
Output includes:
- **Confirmed Balance:** Total confirmed on-chain balance.
- **Unconfirmed Balance:** Balance including pending mempool transactions.
- **Immature Coinbase:** Freshly mined block rewards awaiting the maturity rule (< 10 confirmations on Public Testnet, < 100 confirmations on Mainnet).
- **Spendable Balance:** Available balance ready for immediate transfer.

### 6. Send Testnet Transactions

Transfer testnet coins to another address (requires sufficient spendable balance). Amounts and fees are specified in **ARMX** by default (with raw base unit options `-amount-armoids` and `-fee-armoids` available):

```bash
# Linux / macOS / ARM64 (transfer 10 ARMX with automatic minimum fee)
./armsend -to <RECIPIENT_ADDRESS> -amount 10 -wallet my_testnet_wallet.json

# With explicit fee in ARMX and pre-flight dry-run validation (no broadcast):
./armsend -to <RECIPIENT_ADDRESS> -amount 10.5 -fee 0.0001 -dry-run

# Windows
.\armsend.exe -to <RECIPIENT_ADDRESS> -amount 10 -wallet my_testnet_wallet.json

# Wait for 1 confirmation after broadcast:
.\armsend.exe -to <RECIPIENT_ADDRESS> -amount 5.0 -wait

# Inspect or decode a transaction by TXID:
.\armsend.exe -decode <TXID>
```
> [!NOTE]
> **Transfer Settlement vs Coinbase Maturity:**
> - **Standard Transfers:** Funds require **6 block confirmations** on **BOTH Testnet and Mainnet** (~12 minutes) for standard safe payment settlement (12 confirmations recommended for high-value / irreversible finality). Standard transfers have NO immature delay.
> - **Mining Rewards (Coinbase):** Freshly mined coinbase rewards require consensus maturity confirmations (**10 on Public Testnet**, **100 on Mainnet**) before entering spendable balance.

### 7. Testnet Troubleshooting

| Symptom | Cause | Solution |
|---|---|---|
| `bind: address already in use (19335 or 19333)` | An existing `armcoind` process is already running. | Check with `lsof -i :19335` (Linux) or `netstat -ano \| findstr 19335` (Windows) and terminate the orphaned process. |
| `connection refused to 127.0.0.1:19333` | The node daemon is not running or still initializing. | Ensure `armcoind` is running in another terminal or as a service before running CLI tools. |
| `authentication failed (401 Unauthorized)` | Local `armax.cookie` token mismatch. | Ensure the CLI tool is executed by the same OS user running `armcoind`, or pass `-rpcuser` and `-rpcpassword`. |
| `insufficient spendable balance (Immature: X [< N confirmations])` | Freshly mined coinbase rewards require maturity confirmations (10 on Testnet, 100 on Mainnet). | Wait until the block containing your reward receives at least 10 confirmations on Testnet (100 on Mainnet) before spending. |
| `peer count = 0 (no peers connected)` | Outbound TCP port 19335 blocked by firewall/ISP. | Ensure outbound TCP 19335 is open, or pass explicit seed peers via `-peers <ip>:19335`. |
| `Mainnet has not been launched` | Attempting to pass `-network mainnet`. | Mainnet is locked prior to formal launch. Use `-network testnet` (the default). |

---

## Compiling All Binaries

### On Linux / Armbian:
```bash
chmod +x build.sh
./build.sh
```

### On Windows (PowerShell):
```powershell
.\build.ps1
```

The build script generates 5 binaries in the root directory:
1. `armcoind` : Main node daemon, P2P engine, and web RPC server
2. `armminer` : Multi-threaded CPU miner client with live hashrate telemetry
3. `armsend`  : CLI transaction sender tool
4. `armwallet`: CLI wallet manager tool
5. `armgui`   : Desktop GUI node, wallet, and miner (with embedded Common Controls 6 manifest)

---

## Usage Guide

### 1. Running the Node Daemon (`armcoind`)
```bash
# Standalone Node (RPC on 19333 bound to 127.0.0.1, P2P on 19335 on testnet)
./armcoind

# TLS 1.3 and TOFU certificate pinning are enabled by default.
# Do not disable them outside an isolated legacy test environment.

# Diagnostic & Inspection commands:
./armcoind -checkhealth       # Check RPC, database, P2P, and sync status
./armcoind -getblockchaininfo # Display live block height, tip hash, and chainwork
./armcoind -getpeerinfo       # Display connected peers and network addresses
./armcoind -getnetworkinfo    # Display active network profile, ports, and version
./armcoind -verifychain       # Verify local blockchain integrity
./armcoind -reindex           # Rebuild state from blockchain database

# Optional: Enable internal background CPU mining (Bitcoin/Litecoin standard flags)
./armcoind -gen -genproclimit 4 -miner <MINER_ADDRESS>

# Custom port or peer overrides:
./armcoind -rpcport 19333 -p2pport 19335 -peers testnet-seed1.armax-coin.cloud:19335
```

### 2. Managing Wallets via CLI (`armwallet`)
```bash
# Create a new quantum-safe wallet (saved to wallet.json with 0600 permissions, Argon2id + AES-256-GCM encryption)
./armwallet -network testnet -create -wallet testnet_wallet.json

# Display the local wallet address from keystore
./armwallet -address -wallet testnet_wallet.json

# List addresses in keystore
./armwallet -listaddresses -wallet testnet_wallet.json

# Check balance of an address on the daemon (shows confirmed, immature, and spendable balances)
./armwallet -network testnet -balance <WALLET_ADDRESS>

# Inspect transaction details by TXID
./armwallet -gettransaction <TXID>

# Safely backup wallet keystore file with 0600 permissions
./armwallet -backup /path/to/backup_wallet.json -wallet testnet_wallet.json

# Verify keystore decryption and cryptographic integrity
./armwallet -walletcheck -wallet testnet_wallet.json

# Rescan transaction history for the active wallet
./armwallet -rescan -wallet testnet_wallet.json
```

### 3. Sending Transactions via CLI (`armsend`)
```bash
# Send 20 ARMX with automatic minimum relay fee
./armsend -network testnet -to <RECIPIENT_ADDRESS> -amount 20.0 -wallet testnet_wallet.json

# Send with explicit miner fee in ARMX:
./armsend -network testnet -to <RECIPIENT_ADDRESS> -amount 10.5 -fee 0.0001 -wallet testnet_wallet.json

# Validate transaction parameters, balance, and signature without broadcasting (dry run):
./armsend -to <RECIPIENT_ADDRESS> -amount 10.5 -dry-run -wallet testnet_wallet.json

# Estimate transaction fee based on payload byte size:
./armsend -estimatefee

# Decode transaction details from node:
./armsend -decode <TXID>

# Query confirmation count:
./armsend -confirmations <TXID>

# Output machine-readable JSON for automation:
./armsend -to <RECIPIENT_ADDRESS> -amount 5.0 -json -dry-run
```

### 4. Automated CPU Mining (`armminer`)
```bash
# Mine on Testnet locally with 4 worker threads and live telemetry
./armminer -network testnet -miner <MINER_ADDRESS> -threads 4

# Configure telemetry reporting interval (e.g., every 10 seconds):
./armminer -network testnet -miner <MINER_ADDRESS> -threads 4 -statsinterval 10

# Inspect mining statistics from daemon:
./armminer -getmininginfo

# Inspect mining work template (difficulty, target, reward) from daemon:
./armminer -getworkinfo

# Verify mining configuration and connectivity without beginning hashing:
./armminer -dry-run -miner <MINER_ADDRESS>

# Benchmark local CPU hashing performance (hash/s per thread & aggregate total):
./armminer -benchmark
```

#### Testnet Target Specification & Hashrate Dynamics
- **Testnet Genesis Target (Legacy $H < 520$):** `0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (true 28 leading zero bits, SHA-256d baseline).
- **ArmaxHash Target (Active $H \ge 520$):** Activated at block `#520` on Testnet (`ARMX-TESTNET-1`). Memory-hard Argon2id (2048 KiB, 1 pass, 1 lane) with bootstrap target `000ffff...` (Difficulty 3 / 4,096 hashes).
- **Target Block Time:** 120 seconds ($T=120\text{s}$).
- **Coinbase Maturity:** Consensus-enforced queue per profile (Mainnet: 100 blocks, Public Testnet: 10 blocks, Regtest: 1 block). Freshly mined rewards enter an on-chain immature queue and become spendable only after the required block confirmations are appended to the chain.
- **Dynamic Difficulty Retargeting (LWMA-1):** Continuous 256-bit numerical target adjusted per block across an $N=60$ window ($N+1=61$ blocks evaluated).
  - Anti-timewarp clamping: $\text{solveTime} \in [-720\text{s}, +720\text{s}]$.
  - Dampening bounds: $\text{weightedSolveTimes} \in [\frac{\text{expected}}{4}, \text{expected} \times 4]$.
  - Dynamic Floor: Floor is opened to `DifficultyToTarget(1)`, enabling low-power single-thread ARM SBC miners (e.g., Armbian at ~20 H/s) to mine blocks smoothly while scaling to multi-node clusters.
  - Historical Window Normalization: Pre-activation SHA-256d blocks are normalized to `ArmaxHashPowLimit` during the 60-block transition to prevent legacy targets from dragging down the target.
- **Hardware Profile:**
  - ARM SBC (Armbian Orange Pi / Raspberry Pi, 1 worker thread): ~20-25 H/s
  - Multi-Core Desktop / Laptop CPU (2-8 worker threads): ~250-700 H/s
  - Network steady-state converges smoothly to the 120-second target block time.

#### Production Logging Standards
All binaries implement standard, structured logging:
- **`armcoind`**: Timestamped block receipts `YYYY/MM/DD HH:MM:SS [P2P] [OK] Block #N accepted from IP:port | hash: HASH... | txs: N | diff: N | dt: Ns`.
- **`armminer`**: Real-time hashrate formatting (`%.2f H/s` when $< 1000$ H/s, `%.2f kH/s` when $\ge 1000$ H/s), detailed block solved logs with hash prefix, and clean stale block handling (HTTP 409).
- **`armsend`**: Full transaction broadcast summary including TXID, Sender, Recipient, Amount in ARMX and Armoids, Fee, Nonce, Payload Size (ML-DSA-65 bytes), and Timestamp.
- **`armwallet`**: Detailed account report separating Spendable, Immature Coinbase Rewards (with block confirmation countdown), Confirmed, and Pending Mempool amounts, with query timestamp.


---

## Keystore Format & Wallet Security

### CLI Keystore (`wallet.json`)
The CLI and GUI tools store credentials locally in encrypted format conforming to NIST recommendations with `0600` permissions:
```json
{
  "version": 1,
  "address": "armx1psmx4e6yhg84z2h5wadg3698vjlx06flnynhec08fv8nv56z758sl9xl6c",
  "public_key": "3904_hex_chars_fips204_mldsa65_public_key",
  "crypto": {
    "cipher": "aes-256-gcm",
    "ciphertext": "64_hex_chars_encrypted_seed",
    "nonce": "24_hex_chars_aes_nonce",
    "kdf": "argon2id",
    "kdf_params": {
      "memory": 65536,
      "iterations": 3,
      "parallelism": 2,
      "salt": "32_hex_chars_salt"
    }
  }
}
```

**Security Best Practices for Keystores:**
1. **Restrict File Permissions:** On Linux/macOS, enforce strict read-only access for the current user:
   ```bash
   chmod 600 wallet.json
   ```
2. **Version Control Exclusion:** Ensure `wallet.json` is NEVER committed to Git (strictly included in `.gitignore`).
3. **Cold Storage / Offline Backup:** Write down the 24-word BIP-39 mnemonic phrase and store securely offline on paper. Private keys can always be restored via `./armwallet -restore "words"`.

### Desktop GUI Wallet Security Architecture & Practices
The Armax Desktop GUI (`armgui`) enforces strict cryptographic hygiene:
- **No Hardcoded Passphrases:** Default passphrases have been completely eliminated. Keystores require a user-chosen passphrase with a minimum of 12 characters, confirmed twice.
- **One-Time Mnemonic Display & Mandatory Verification:** The 24-word recovery phrase is displayed exactly once upon wallet generation. To prevent careless skipping, users must confirm backup by typing back 3 randomly selected words before the keystore is written to disk.
- **Immediate Memory Scrubbing (Best-Effort):** Plaintext mnemonic references are cleared from wallet data structures (`w.ClearMnemonic()`) immediately after seed derivation and verification. Note: In the Go runtime, strings are immutable and cannot be overwritten in-place in raw heap memory; reference clearing is strictly a best-effort defense subject to Go garbage collector recycling.
- **Encrypted Seed-Only Keystores:** Keystores store only the Argon2id + AES-256-GCM encrypted 32-byte seed (`seed32`). Neither the plaintext mnemonic nor unencrypted private keys are persisted.
- **Brute-Force Rate Limiting:** Failed unlock attempts introduce an artificial 1.5-second penalty delay to thwart automated local dictionary and brute-force attacks.
- **Forced Legacy Keystore Migration:** Any legacy keystore generated with deprecated default passphrases is detected automatically and requires immediate migration to a strong passphrase before unlocking.
- **Exact Decimal Accounting:** Balance, amounts, and fees are parsed and displayed using exact string-to-integer conversion down to 1 Armoid ($10^{-8}$) without floating-point inaccuracies.
- **Automatic Minimum Mempool Fee:** Outgoing transactions validate that fees meet or exceed `core.CalculateMinMempoolFee(signedTxSize)`. If a lower fee is entered, the GUI automatically prompts and upgrades to the minimum network relay fee to ensure the transaction is accepted by mempools.

### Web Wallet Security Considerations
- **Client-Side Signing Enforced:** Transaction signing is performed exclusively client-side via `window.crypto.subtle`. Private keys are NEVER sent to the daemon.
- **Secure Context Requirement:** Web Crypto API requires a **Secure Context** (`https://` or `http://localhost`). Accessing over unencrypted public IP will prevent signing.
- **Deployment Recommendation:** If hosting the Web Wallet publicly, always terminate TLS using a reverse proxy (such as Caddy or Nginx with Let's Encrypt) and bind the RPC daemon port 19332 (testnet 19333) and local services (19340-19343) strictly to `127.0.0.1`.
- **Storage Warning:** The browser wallet stores active keys in browser `localStorage`. Do not store high-value keys in browser storage on untrusted or shared machines.

---

## P2P Protocol Specification

Armaxcoin nodes communicate over raw TCP sockets using newline-delimited JSON messages (`\n`).

### 1. Packet Envelope
```json
{
  "network_id": "ARMX-MAINNET-1",
  "type": "<MESSAGE_TYPE>",
  "payload": { ... }
}
```

### 2. Message Types & Workflows

| Type | Direction | Description |
|---|---|---|
| `NEW_BLOCK` | Broadcast | Propagates a newly mined `core.Block`. Peers validate and append or trigger catch-up sync. |
| `GET_HEIGHT` | Direct | Queries peer height, latest block hash, and cumulative chainwork. |
| `HEIGHT_RESPONSE` | Direct | Returns `HeightResponse{Height, LatestHash, Chainwork}`. |
| `GET_BLOCK_RANGE` | Direct | Requests a chunk of blocks bounded to ~1.5 MB (`BlockRangeRequest`). |
| `BLOCK_RANGE_RESPONSE` | Direct | Returns chunked `[]core.Block` in response to `GET_BLOCK_RANGE`. |
| `REQUEST_CHAIN` | Direct | Requests candidate chain for fork resolution / reorganization. |
| `CHAIN_RESPONSE` | Direct | Returns candidate chain in response to local request (unsolicited responses rejected). |
| `NEW_TX` | Broadcast | Gossips an incoming `core.Transaction` to peer mempools. |
| `GET_PEERS` | Direct | Requests known active peer IP:Port addresses. |
| `PEERS_LIST` | Direct | Returns `[]string` containing discovered peer addresses. |

### 3. P2P Hardening & Transport Encryption
- **Wire Encryption:** Automatic TLS 1.3 handshake with ephemeral in-memory certificates prevents passive ISP wiretapping and DPI inspection. Note: P2P TLS transport is unauthenticated (does not verify peer identity via PKI/CA).
- **Sybil / Eclipse Defense:** Subnet limiter enforces a maximum of **3 concurrent peers per `/24` IPv4 or `/48` IPv6 subnet** (can be bypassed for LAN/testnet via `disable_subnet_limit`).
- **Decentralized Discovery:** Dedicated profile DNS seeds:
  - **Testnet:** `testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335`

---

## Network & Protocol Security Architecture

1. **P2P Transport Encryption:**
   - Wire transport uses newline-delimited JSON over TLS 1.3 by default; plaintext fallback is disabled.
   - TOFU certificate fingerprint pinning is enabled by default. Operators must distribute trusted seed fingerprints out-of-band to remove first-contact MITM risk.
   - Sockets enforce 20-second read/write deadlines to prevent Slowloris resource exhaustion.

2. **Fork Choice & Satoshi Cumulative Chainwork:**
   - Chain reorganization evaluates cumulative Proof-of-Work: $\text{Chainwork} = \sum \frac{2^{256}}{\text{Target}_i + 1}$.
   - Reorganization depth is strictly bounded by `MaxReorganizationDepth = 720` blocks (~24 hours) to prevent deep 51% history rewrites.

3. **HTTP RPC Endpoints & Bitcoin/Litecoin Compatibility:**
   - Administrative and state-mutating endpoints require `X-Mine-Key` token or configurable `rpcuser`/`rpcpassword` HTTP Basic Auth.
   - **`/getblocktemplate`**: Standard mining template returning current difficulty, target, candidate transactions, and fees.
   - **`/submitblock` & `/block/submit`**: Submit solved block candidate for consensus validation and peer gossip.
   - **`/getmininginfo`**: Standard mining statistics (`blocks`, `difficulty`, `target`, `networkhashps`, `pooledtx`, `chain`).
   - **`/getnetworkhashps`**: Real-time network hashrate estimation: $\text{hashesPerBlock} / 120$.
   - **`/balance`**: Detailed accounting of confirmed, unconfirmed, immature (< 10 confs on Testnet, < 100 confs on Mainnet), and spendable balances.
   - **`/mine`**: Dedicated fallback endpoint with CSRF origin validation and per-IP rate limiting.
   - Per-IP rate limiting and body size limits (`MaxBlockSize = 2 MB`, `MaxTxSize = 20 KB`) prevent DoS.

4. **Coinbase Maturity (Consensus Enforcement - v3 Schema):**
   - Enforces an on-chain immature reward queue in the account state ledger (`state.go`). Any block attempting to spend immature coinbase rewards is rejected by Layer-1 consensus with an explicit execution error.
   - Parameters per network profile: Mainnet: 100 blocks, Public Testnet: 10 blocks, Regtest: 1 block.
   - Protects users and services against loss of funds in the event of transient chain reorganizations. Storage schema upgraded to version 3 with clean Genesis state synchronization.

5. **Mempool Protection & Fee Market:**
   - Capacity capped at 5,000 transactions.
   - Dynamic minimum relay fee policy: $\text{Fee} \ge \text{MinRelayFee} \times \lceil\text{tx.Size}() / 1000\rceil$ Armoids.
   - Rate limit of 25 transactions per sender prevents single-account mempool exhaustion.
   - Eviction based on lowest fee rate when capacity is reached.

5. **Layer-2 Scalability Primitive (HTLC):**
   - *Status: Experimental Library Primitive.* Foundation for future off-chain payment channels and atomic swaps.

6. **Post-Quantum Cryptography (NIST FIPS 204, ML-DSA-65):**
   - **Quantum-resistant digital signatures (NIST FIPS 204, ML-DSA-65)** are actively enforced in Layer-1 consensus for all account transactions (Public key: 1,952 bytes, Signature: 3,309 bytes).
   - Protects account transactions against Shor's algorithm on quantum computers.
   - **Realistic Threat Model & Security Boundaries:** PoW SHA-256 is subject only to quadratic speedup under Grover's algorithm (retaining ~128-bit effective post-quantum security); TLS P2P transport is unauthenticated; the implementation has not yet undergone a formal external third-party security audit.

---

## Strategic Roadmap: Next-Generation Innovations

Armaxcoin is strategically architected across 7 core pillars:

1. **Post-Quantum Cryptography (PQC):** NIST FIPS 204 (ML-DSA-65) active in layer-1 consensus transactions.
2. **Advanced Financial Privacy (Future Research):** Confidential Transactions (CT) via Bulletproofs and optional Ring Signatures.
3. **Encrypted P2P Transport:** Fully operational TLS 1.3 socket encryption with TOFU pinning.
4. **Layer-2 Payment Channels (Planned):** Experimental HTLC primitives in core transactions for future off-chain microtransactions.
5. **Lightweight Smart Contracts (Future Research):** Sandboxed WebAssembly (WASM) VM executing expressive UTXO smart contracts.
6. **Small-Device PoW Consensus:** Responsive LWMA-1 difficulty adjustment ($N=60$) coupled with the memory-hard ArmaxHash algorithm (Argon2id + Blake2b + SHA-256) active from block #520 for equitable CPU mining.
7. **Democratic On-Chain Governance:** Block header miner/node signaling for smooth protocol upgrades.

---

## Known Limitations & Security Disclosures

1. **P2P Transport Security:** Wire transport encryption employs self-signed ephemeral X.509 certificates pinned via Trust On First Use (TOFU). While TOFU prevents ongoing passive surveillance and subsequent tampering, the initial contact remains vulnerable to active in-path MITM adversaries unless seeded with out-of-band certificate pins.
2. **Code Audit Status:** Armaxcoin is an experimental Layer-1 cryptocurrency under active testnet development. Neither the core consensus engine, the ArmaxHash implementation, nor the post-quantum signature integration has undergone a formal third-party cryptographic security audit.
3. **Dual PoW Evolution:** Historical blocks 0..519 were mined using double-SHA256 (SHA-256d). Active mining from block #520 onwards enforces the memory-hard ArmaxHash algorithm (Argon2id 2 MB), engineered to facilitate accessible CPU mining across commodity hardware without claiming permanent ASIC immunity.
4. **Initial Hashrate & 51% Risk:** Early testnet and bootstrap states operate with low aggregate network hashrate, leaving the blockchain temporarily vulnerable to 51% hash power reorganization attacks. Reorganizations are strictly bounded by `MaxReorganizationDepth = 720` blocks.
5. **ARM Verification Overhead:** NIST FIPS 204 ML-DSA-65 signatures require modular polynomial arithmetic, introducing measurable CPU verification latency on constrained ARM single-board computers (Raspberry Pi, Orange Pi).
6. **Minimum Go Version:** Compiling Armaxcoin requires Go 1.27+ due to reliance on the standard library `crypto/mldsa` package and toolchain alignment (`go.mod`).
7. **Windows GUI Resource Binary (`gui/rsrc_windows_amd64.syso`):** The repository includes a deliberate pre-compiled Windows resource object required by standard Go toolchains on Windows to link the GUI application manifest and taskbar icon. It can be reproducibly recompiled from source using `rsrc -manifest armgui.exe.manifest -ico armax_logo.ico -arch amd64 -o rsrc_windows_amd64.syso`.

---

## Release History & Changelog

### v1.0.1 (Public Open Testnet -- BIP-35 Mempool Sync & NAT Support) -- 2026-10-23

Maintenance and networking release introducing BIP-35 compliant mempool synchronization, automated mempool rebroadcasting, and outbound NAT synchronization.

#### Network Protocol & Mempool Sync
- **BIP-35 Mempool Synchronization (`GET_MEMPOOL`):** Nodes, miners, and external wallets joining the network can request the complete unmined transaction pool from connected peers upon connection.
- **NAT & Private IP Outbound Sync:** Supported full block history and mempool synchronization for private nodes behind NAT or firewalls without open inbound listening ports.
- **Periodic Mempool Rebroadcasting:** Daemon background loop automatically rebroadcasts pending mempool transactions every 60 seconds across all connected peer sessions to prevent transaction starvation.
- **Rate-Limiting & Memory Guards:** Protected against mempool amplification attacks by bounding `GET_MEMPOOL` requests (max 10 req/min per peer) and capping responses (max 1,000 transactions per response).

---

### v1.0.0 (Public Open Testnet -- Experimental) -- 2026-10-22

Initial official public testnet release of the ARMAXcoin Proof-of-Work Layer-1 blockchain on `ARMX-TESTNET-1`.

#### Post-Quantum Cryptography & Identity
- **NIST FIPS 204 (ML-DSA-65):** Native module-lattice post-quantum digital signature scheme for all user transactions and address derivations, implemented with Go 1.27+ standard library `crypto/mldsa`.
- **Bech32 Address Format:** Canonical `armx1...` human-readable addresses derived from public key hashes.
- **Passphrase Encryption:** BIP-39 24-word recovery mnemonics with PBKDF2/Argon2id keystore protection.

#### Consensus & PoW Hardening
- **Memory-Hard Hybrid PoW (ArmaxHash):** Dual-phase PoW migration enforcing memory-hard Argon2id (2 MB scratchpad, $t=1, p=1$), Blake2b-512, and SHA-256 at block height $\ge 520$ for decentralized CPU mining.
- **Continuous LWMA-1 Difficulty Adjustment:** Responsive sliding-window retargeting ($N=60$) with safety dampening bounds preventing oscillation, timewarp attacks, and hashrate collapse.
- **Chainwork & Fork Resolution:** Satoshi cumulative chainwork fork choice rule with a strict `MaxReorganizationDepth = 720` blocks (~24h finality limit).
- **Consensus Ceilings:** `MaxBlockTransactions = 1,000` block transaction cap and continuous circulating supply tracking.

#### Network Protocol & Transport Security
- **Mandatory TLS 1.3:** Ephemeral X.509 wire transport encryption enforced on all peer connections.
- **Trust On First Use (TOFU):** P2P certificate fingerprint pinning and persistent session tracking in `peers.json`.
- **Timing Oracle Immunity:** Constant-time pre-hashed credential comparison for RPC authentication.
- **Flood & Loop Defense:** LRU seen-message filters and self-peer loop filtering preventing relay amplification.

#### Tooling & Client Ecosystem
- **`armcoind`:** Full-featured core daemon and P2P validator.
- **`armminer`:** Multi-threaded CPU mining client with `-threads` concurrency flag and real-time telemetry.
- **`armwallet` / `armsend`:** Encrypted CLI wallet and transaction broadcasting suite.
- **`armgui`:** Desktop GUI wallet for Windows with native Win32 controls, real-time sync, and transaction confirmation modals.
- **Multi-Platform Automated Releases:** Automated GitHub Actions build pipeline producing stripped binaries and verified `SHA256SUMS` for Linux (amd64/arm64), Windows (amd64), and macOS (arm64).

---

## License

Armaxcoin is licensed under the open-source [MIT License](LICENSE).  
Copyright (c) 2026 acxxcxx.
