# Armaxcoin (`armax-coin`)

**Canonical Repository:** [github.com/acxxcxx/armax-coin](https://github.com/acxxcxx/armax-coin)<br>
**Current Release:** `v1.0.10` (Armax-ASERT-AW DAA Hard Fork at Block #13,335, 4-Hour Surge Cap & TEDR Boost, Immutable MiningJob & 3-Tier P2P Policy)<br>
**Go Module:** `github.com/acxxcxx/armax-coin`

Armaxcoin is an open-source, experimental Layer-1 Proof-of-Work (PoW) cryptocurrency engineered for post-quantum security, accessibility, and high performance across personal computers and ARM-based single-board devices (Armbian, Raspberry Pi, Orange Pi).

> [!IMPORTANT]
> **ACTIVE & SCHEDULED TESTNET CONSENSUS FORKS (BLOCKS 13,293, 13,335 & 20,000):**
> - **Block 13,293 (v1.0.9 — Anti-Stall TEDR):** Activates Testnet Anti-Stall Emergency Difficulty Adjustment (`AntiStallActivationHeight = 13293`). When the elapsed interval since the parent block exceeds **1,200 seconds (20 minutes)**, candidate blocks qualify for an emergency stall-breaker target (`Difficulty 3`) with Model 3 Synthetic Target Normalization (`720s` solve time, anchored to `lastNormalTarget`). Permanently disabled on Mainnet (`-1`).
> - **Block 13,335 (v1.0.10 — Armax-ASERT-AW DAA):** Activates the canonical integer `aserti3-2d` difficulty adjustment algorithm (`ASERTActivationHeight = 13335`), anchored to Block `#13,334` with Anti-Poisoning fallback (DA-03), 4-hour surge cap (`14400s`), and TEDR Boost (`7200s`) for 200x hashrate crash recovery.
> - **Block 20,000 (v1.0.7+ — Proposal C6):** Tightens maximum allowed future block timestamp from 7,200 seconds (2 hours) down to **600 seconds (10 minutes)** ahead of the validating node's local clock.
> 
> **Operator Action Required:** All testnet node operators, miners, and pool maintainers **MUST** upgrade immediately to **v1.0.10** prior to Block `#13,335`, and ensure server clocks are synchronized with NTP (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows).
> 
> Detailed release notes and upgrade instructions: **[v1.0.10 Release Notes](docs/releases/v1.0.10.md)** | **[v1.0.9 Release Notes](docs/releases/v1.0.9.md)** | **[v1.0.8 Release Notes](docs/releases/v1.0.8.md)** | **[RELEASE_NOTES.md](RELEASE_NOTES.md)**.

> [!NOTE]
> **PUBLIC TESTNET ENVIRONMENT:**
> Armaxcoin is currently operating on the public testnet (`ARMX-TESTNET-1`). Testnet coins (`tARMX`) have **no monetary value**, no market liquidity, no guarantee of conversion, and no investment utility.
> The mainnet configuration (`ARMX-MAINNET-1`) remains locked in source (`core.MainnetLaunched = false`) while network stabilization and testing continue. Invoking `-network mainnet` is strictly rejected by all binaries. All node operations, CPU mining, and testing take place exclusively on the public testnet.

---

## Authoritative Documentation Index

| Guide | Description |
|---|---|
| **[Release Notes (v1.0.10)](RELEASE_NOTES.md)** | Authoritative v1.0.10 release notes, Block 13,335 Armax-ASERT-AW DAA fork, surge cap & TEDR boost, immutable MiningJob, and 3-tier P2P policy |
| **[Open Testnet Guide](docs/TESTNET_GUIDE.md)** | Step-by-step instructions for installing, syncing, and testing transactions |
| **[CPU Mining Guide](docs/MINING_GUIDE.md)** | Mining with `armminer`, thread tuning, memory footprints, and tokenomics |
| **[Developer Early-Mining Plan](docs/architecture/tokenomics/mining-accumulation-plan.md)** | Operational parameters, block boundaries, and disclosure for the developer early-mining phase |
| **[Node Operations Guide](docs/NODE_OPERATIONS.md)** | Running full nodes, seed nodes, systemd units, TLS 1.3, and TOFU certificate security |
| **[Consensus & Network Specification](docs/CONSENSUS_AND_NETWORK.md)** | Mathematical specifications for LWMA-1, ArmaxHash, ML-DSA-65, and reorg limits |
| **[Contributors](CONTRIBUTORS.md)** | List of project contributors derived from Git history |
| **[Security Policy & Disclosures](SECURITY.md)** | Threat model, responsible vulnerability reporting, and known limitations |
| **[Contribution Guidelines](CONTRIBUTING.md)** | Development workflow, code standards, Conventional Commits, and test gates |
| **[Code of Conduct](CODE_OF_CONDUCT.md)** | Community pledge and standards of behavior |
| **[Accessibility Statement](ACCESSIBILITY.md)** | Accessibility policies for GUI, CLI, documentation, and hardware |
| **[Release Checklist](docs/RELEASE_CHECKLIST.md)** | Publication gates and release verification runbook |
| **[Architecture Documentation](docs/architecture/README.md)** | Architecture taxonomy, plan inventory, and implementation boundaries |
| **[Changelog](CHANGELOG.md)** | Complete version history adhering to Keep a Changelog |
| **[Development Support](SUPPORT.md)** | Voluntary contributions and developer contact information |

---

## Consensus & Tokenomics Parameters

> Mainnet is locked and has not launched. Source code preserves the 126,000,000 ARMX maximum supply, 60 ARMX initial reward, 1,050,000-block halving interval, 120-second target block time, and existing genesis (60 ARMX genesis coinbase burned to the canonical zero-payload address). Before public mainnet opening, the developers will mine blocks 1 through 16,667. Stop rule: Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first. Developers will mine all blocks in this range. At 60 ARMX per block, 16,667 blocks yield up to 1,000,020 ARMX (16,666 blocks = 999,960 ARMX; block 16,667 yields 1,000,020 ARMX due to discrete block subsidies). Estimated duration is approximately 23.15 days at 120-second target block time; actual duration depends on hashrate and difficulty. Public mainnet opening: TBA. Treasury address will be published before mainnet launch. Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch. Risks / Criticism: Early developer mining represents an initial point of centralization and grants the developer team a material fraction (~0.79% of the total 126,000,000 ARMX supply) before the public opening.

| Parameter | Specification | Details / Rules |
|---|---|---|
| **Consensus Mechanism** | Proof-of-Work (PoW) | Dynamic 256-bit numerical target difficulty adjustment via LWMA-1 ($N=60$) |
| **Fork Choice Rule** | Heaviest Chain (Cumulative Chainwork) | Cumulative Proof-of-Work ($\sum \frac{2^{256}}{\text{Target} + 1}$) strictly beats low-difficulty chains |
| **Max Reorg Depth** | 720 Blocks (~24 hours at target cadence) | Local reorganization-depth policy limit; not protocol finality or a guarantee that transactions cannot be reorganized |
| **Max Block Transactions** | 1,000 Txs / Block | Consensus transaction-count limit; it does not replace block-size or other resource limits |
| **Block Size Limits** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB | MaxIncomingPacketSize = 4 MB; verified block capacity: up to 185 user transactions (186 total with coinbase; 10,761 B/tx in JSON wire format, see docs/PERF.md) |
| **Hashing Algorithm** | ArmaxHash v2 (Stream-Fast Chained BLAKE2b, $H \ge 2500$) / ArmaxHash v1 ($520 \le H < 2500$) / Legacy SHA-256d ($H < 520$) | The v2 hash-function benchmark reports 0 allocations/op for a fixed header; this does not describe allocations in the full block/miner path. See docs/CONSENSUS_AND_NETWORK.md |
| **Digital Signatures** | NIST FIPS 204 (ML-DSA-65) | Quantum-resistant signatures (NIST FIPS 204, ML-DSA-65): Public key 1,952 bytes, Signature 3,309 bytes |
| **Address Derivation** | BIP-173 Bech32 (`armx1...`) | Canonical lowercase prefix `armx` + 32-byte SHA-256(SigAlg \|\| PubKey) payload (63 chars total) |
| **Wallet Recovery** | BIP-39 Deterministic Mnemonic | Standard 24-word mnemonic (12-word accepted with warning), HKDF-SHA256, Argon2id encrypted keystore |
| **P2P Transport** | JSON over TCP with TLS 1.3 | Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity; TOFU pins the remote certificate public-key fingerprint after first contact. The initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band |
| **P2P Synchronization** | Chunked Range & Chainwork Sync | Range-based block streaming (`GET_BLOCK_RANGE`) with cumulative chainwork reorg and parallel sync workers |
| **P2P Address Book** | 1,000 Addrs (`tried` & `new`) | Separate address book from active connections (MaxPeers=50), dead peer eviction after 3 failures |
| **P2P Eclipse Defense** | Subnet Connection Limiter | Maximum 3 concurrent peers/connections per IPv4 `/24` (or IPv6 `/48`) subnet (`disable_subnet_limit` available) |
| **P2P Flood Defense** | Seen-Filter Hash Cache & Orphan Pools | LRU deduplication cache; bounded 100 orphan blocks (max 10/peer) & 500 orphan txs pool |
| **DNS Seeds (Testnet)** | Dedicated Testnet Seeds | `testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335` |
| **Storage Engine** | bbolt ACID Key-Value Store | State snapshots every 1,000 blocks, undo logging (1,440 retention), and crash recovery |
| **Target Block Time** | 120 seconds (~2 minutes) | Difficulty-adjustment target; observed block intervals and orphan rates vary with network conditions |
| **Difficulty Algorithm** | LWMA-1 (Linear Weighted Moving Average) | Continuous 256-bit numerical target adjusted per block across 60-block window; dynamic floor down to Difficulty 1 for ArmaxHash |
| **Candidate Block Extraction** | `/getblocktemplate` JSON RPC | Bounded block candidate endpoint (MaxBlockTransactions - 1) for external miners |
| **RPC Security** | Loopback Binding & Cookie/Basic Auth | Mutation endpoints strictly require auth/cookie or `-rpcuser`/`-rpcpassword`; `-rpcinsecure-localhost` flag; /chain bounded to 50 blocks |
| **Ticker** | `ARMX` (mainnet). Testnet GUI, explorer, and CLI (`armsend`, `armwallet`) display `tARMX` | Display label only. Not consensus. Network ID and on-chain values are unchanged |
| **Initial Block Reward** | 60 ARMX (6,000,000,000 Armoids) | Awarded to miner via single coinbase transaction at index 0 |
| **Halving Interval** | Every 1,050,000 blocks (~4 years) | Reward cuts in half ($60 \to 30 \to 15 \to \dots$ ARMX) |
| **Maximum Total Supply** | 126,000,000 ARMX | Strict hard cap (12,600,000,000,000,000 Armoids, 8 decimal places; real theoretical emission: 125,999,999.874 ARMX) |
| **Supply Accounting** | $O(1)$ Incremental State Cache | Constant-time circulating supply tracking without block history traversal |
| **Developer Early-Mining** | Up to 1,000,020 ARMX (Blocks 1..16,667) | Before public mainnet opening, developers mine all blocks in range 1..16,667. Stop rule: Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first. Estimated duration: ~23.15 days at 120s block time. Public opening: TBA. Treasury address will be published before mainnet launch. Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch. |
| **Transfer Confirmation Guidance** | 6 confirmations (standard), 12 (high-value) | These are operator guidance values exposed by RPC/UI, not consensus rules or irreversible finality. Ordinary transfers have no coinbase-maturity delay. |
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
  - [Seed Node Operations & Systemd Guide](docs/TESTNET.md) (operator view); [Open Testnet Guide](docs/TESTNET_GUIDE.md) (end-user view)
  - [Mainnet Readiness Gate](docs/MAINNET_READINESS.md)
  - Internal / historical records: [Repository Cleanup](docs/CLEANUP_REPO.md), [ArmaxHash Migration Audit](docs/ARMAXHASH_MIGRATION_AUDIT.md), [Soak Report](docs/SOAK_REPORT.md)

---

## Quickstart: Public Testnet (Windows, Linux, ARM64)

Follow these steps to join the public testnet (`ARMX-TESTNET-1`). All binaries default to testnet while mainnet remains locked.

### 1. Download & Verify Release Artifacts

Download pre-compiled binary packages and `SHA256SUMS` from [GitHub Releases](https://github.com/acxxcxx/armax-coin/releases):

#### On Linux / ARM64:
```bash
# Download package and checksum
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.9/armax-v1.0.9-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.9/SHA256SUMS

# Verify checksum integrity
sha256sum --check --ignore-missing SHA256SUMS

# Extract and make executable
tar -xzf armax-v1.0.9-linux-amd64.tar.gz
chmod +x armcoind armminer armsend armwallet armgui
```

Each archive contains: `armcoind` (node daemon), `armminer` (CPU miner), `armwallet` (keystore and balance), `armsend` (send and query transactions), `armgui` (local web wallet), `armax.conf.example`, `LICENSE`, `README.md`, and `SHA256SUMS`.

#### On Windows (PowerShell):
```powershell
# Compute SHA-256 hash of downloaded zip
Get-FileHash -Algorithm SHA256 .\armax-v1.0.9-windows-amd64.zip

# Compare against the published hash in SHA256SUMS
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.9-windows-amd64.zip"
```

#### Verifying releases

Release checksum manifests (`SHA256SUMS`) are detached-signed with the Armaxcoin Core PGP release key (`ARMAX-PGP-KEY.asc`). To verify the authenticity and integrity of a downloaded release:

1. **Import the PGP public key and verify its fingerprint:**
   ```bash
   gpg --import ARMAX-PGP-KEY.asc
   gpg --fingerprint dev@armax-coin.cloud
   ```
   Confirm that the primary key fingerprint matches the official Armaxcoin Core release signing key:
   ```text
   C857E67F3FC63BE35125C3D32ACE863ED4BF1460
   ```

2. **Verify the detached PGP signature on `SHA256SUMS`:**
   ```bash
   gpg --verify SHA256SUMS.asc SHA256SUMS
   ```
   Ensure GPG reports a `Good signature from "Armaxcoin Core (Official Release Signing Key) <dev@armax-coin.cloud>"` before proceeding.

3. **Verify the archive checksums:**
   ```bash
   sha256sum -c SHA256SUMS
   # Or, if you only downloaded a single platform archive:
   sha256sum --check --ignore-missing SHA256SUMS
   ```

4. **Windows Authenticode Signature Check (Optional):**
   On Windows, you can also inspect the Authenticode signature and timestamp on any `.exe` binary using PowerShell:
   ```powershell
   Get-AuthenticodeSignature .\armgui.exe | Format-List Status, StatusMessage, SignerCertificate, TimeStamperCertificate
   ```
   > **Note:** Never import self-signed certificates into your Windows Root Certificate Store (`Cert:\CurrentUser\Root` or `Cert:\LocalMachine\Root`). Always rely on PGP verification of `SHA256SUMS.asc` and/or reproducible builds from source (`go build -trimpath ...`) for cryptographic authenticity.

### 2. Launch Testnet Node Daemon

Start the node daemon. By default, it connects to testnet seed nodes, binds P2P gossip to port `19335`, and restricts RPC to loopback `127.0.0.1:19333`:

```bash
# Linux / macOS / ARM64
./armcoind

# Windows
.\armcoind.exe
```

The daemon prints a testnet warning, the active profile (`ARMX-TESTNET-1`), the data directory, the P2P port (`19335`), and the RPC address (`127.0.0.1:19333`). Log lines vary slightly between builds; check the status with `./armcoind -getblockchaininfo` and `./armcoind -getpeerinfo`.

To keep settings in a file, copy `armax.conf.example` to `armax.conf` next to the binary. Command-line flags override values in the file. Use `-config <FILE>` to load a different file.

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

There are two ways to mine. Use one or the other on a given node.

**Option A: mine from a separate miner process** (`armminer` talks to the daemon over RPC):

```bash
# Linux / macOS / ARM64 (4 CPU worker threads)
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4

# Windows
.\armminer.exe -miner <YOUR_ARMX_ADDRESS> -threads 4

# Check mining status from the daemon (height, difficulty, hashrate) and exit
.\armminer.exe -getmininginfo

# Run CPU hashing benchmark (hash/s per thread & aggregate total)
.\armminer.exe -benchmark
```

Useful options: `-daemon <URL>[,<URL>...]` for failover between daemons, `-remote` to delegate hashing to the daemon's `/mine` endpoint, `-user` / `-pass` for RPC Basic Auth (or `ARMX_RPC_PASSWORD`), `-statsinterval <SEC>` (default 10), `-delay <MS>` between rounds (default 2000), `-mempool` to list unconfirmed transactions, and `-stop` for shutdown instructions.

**Option B: mine inside the daemon**:

```bash
./armcoind -mine -miner <YOUR_ARMX_ADDRESS> -miningthreads 4
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

Transfer testnet coins to another address (requires sufficient spendable balance). Amounts and fees are specified in **tARMX** by default on testnet (**ARMX** on mainnet). Raw base unit options `-amount-armoids` and `-fee-armoids` are also available:

```bash
# Linux / macOS / ARM64 (transfer 10 tARMX with automatic minimum fee)
./armsend -to <RECIPIENT_ADDRESS> -amount 10 -wallet my_testnet_wallet.json

# With explicit fee in tARMX and pre-flight dry-run validation (no broadcast):
./armsend -to <RECIPIENT_ADDRESS> -amount 10.5 -fee 0.0001 -dry-run

# Machine-readable output for scripts (errors are JSON objects on stdout)
./armsend -to <RECIPIENT_ADDRESS> -amount 10 -json

# Estimate the minimum fee
./armsend -estimatefee

# Windows
.\armsend.exe -to <RECIPIENT_ADDRESS> -amount 10 -wallet my_testnet_wallet.json

# Wait until the transaction is included and has its first confirmation:
.\armsend.exe -to <RECIPIENT_ADDRESS> -amount 5.0 -wait

# Inspect or decode a transaction by TXID:
.\armsend.exe -decode <TXID>
```
> [!NOTE]
> **Transfer Confirmation Guidance vs Coinbase Maturity:**
> - **Standard Transfers:** Consensus does not require a fixed confirmation count before an ordinary transfer can be spent again. The RPC/UI reports 6 confirmations as general payment guidance and 12 for higher-value payments; neither count guarantees irreversible finality. `armsend -wait` waits for the first confirmation only.
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

## Command Reference (v1.0.9)

Run any binary with `-h` for the full, current flag list. Defaults target testnet.

**`armcoind`**: node daemon, RPC, and P2P
- Run: `./armcoind` (or `.\armcoind.exe`). Config file: `-config <FILE>` (default `armax.conf`). Data: `-datadir <DIR>`.
- Network and ports: `-network testnet|mainnet`, `-rpcport` (19333), `-p2pport` (19335), `-rpcbind`, `-peers <host:port,...>`, `-rpc-allow-non-loopback` (requires auth).
- RPC auth: `-rpcuser`, `-rpcpassword`, `-rpcinsecure-localhost` (unauthenticated localhost mutations; not recommended), `-minekey` (pre-shared key for remote miners).
- Mining: `-mine`, `-miner <ADDRESS>`, `-miningthreads <N>`.
- Status: `-getblockchaininfo`, `-getnetworkinfo`, `-getpeerinfo`, `-getmempoolinfo`, `-checkhealth`, `-verifychain`.
- Maintenance: `-shutdown` (asks a running daemon to stop via RPC), `-resync` (backs up the database and resyncs from genesis), `-reindex`.
- Storage: `-store-compression` (enables BoltDB record compression; default off), `-store-rewrite plain|compressed` (offline converter; node must be stopped).
- Logging: `-loglevel error|warn|info|debug|trace`, `-debug <net,sync,mempool,validation,rpc,discovery,miner|all>`.
- Transport & P2P: `-require-tls` (no unencrypted fallback), `-p2p-tofu` (TOFU certificate pinning). Configuration options include `seed_fingerprints` map and automatic persistent bans in `bans.json`. Environment `ARMX_P2P_COMPRESSION=0` disables requesting DEFLATE compression.

**`armminer`**: CPU miner client (see step 4)
- Mining: `-miner <ADDRESS>` (required), `-threads <N>` (0 = all cores), `-delay <MS>`, `-benchmark`, `-dry-run`, `-remote` (use the daemon's `/mine`).
- Daemon: `-daemon <URL,...>`, `-rpcport`, `-rpcuser`/`-user`, `-rpcpassword`/`-pass` (also `ARMX_RPC_PASSWORD`), `-key` (also `ARMX_MINE_KEY`, for remote node authorization).
- Info: `-getmininginfo`, `-getworkinfo`, `-mempool`, `-info`, `-statsinterval <SEC>`, `-stop`, `-version`.

**`armwallet`**: keystore, addresses, and balance
- Keystore: `-create`, `-restore-interactive` (secure masked terminal input with stdin fallback; recommended), `-restore "<24 WORDS>"` (legacy; warning logged), `-address`, `-listaddresses`, `-walletcheck`, `-backup <DEST>`, `-rescan`. Use `-wallet <FILE>` (alias for `-file`, default `wallet.json`). `-force` overwrites an existing file without a backup.
- Queries: `-balance <ADDRESS>`, `-gettransaction <TXID>`, `-mempool`, `-daemon <URL>`.
- Environment: `ARMX_WALLET_PASSPHRASE` skips the passphrase prompt. Keep it out of shell history and scripts; prefer the prompt.

**`armsend`**: transaction sender and query tool
- Send: `-to <ADDRESS>` with `-amount <DECIMAL>` or `-amount-armoids <N>`. Optional `-fee <DECIMAL>` or `-fee-armoids <N>`, `-wallet <FILE>`, `-dry-run`, `-wait` (first confirmation).
- Query: `-decode <TXID>`, `-confirmations <TXID>`, `-mempool`, `-estimatefee`, `-info`.
- Output: `-json`. Other: `-daemon`, `-rpcport`, `-rpcuser`/`-rpcpassword`, `-datadir`.

**`armgui`**: local GUI wallet, bound to localhost
- Windows (`armgui.exe`): desktop window. Flags: `-network`, `-rpcport`, `-p2pport`, `-datadir`.
- Linux / macOS / ARM64 (`./armgui`): serves a web UI strictly on loopback with session token authentication (`X-Armx-GUI-Token`) and Host header enforcement. Flags: `-port <PORT>` (default 18088), `-rpc <URL>` (default `http://127.0.0.1:19333`), `-no-browser` (do not open a browser automatically).
- On Linux / macOS, open `http://127.0.0.1:18088` in a browser after it starts. All transfers on `/api/send` require passphrase verification.

**Common flags:** `-version` (print version and exit) works on `armcoind`, `armminer`, `armwallet`, and `armsend`. `-info` (print the active network profile and exit) works on the same four.

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
# Send 20 tARMX with automatic minimum relay fee
./armsend -network testnet -to <RECIPIENT_ADDRESS> -amount 20.0 -wallet testnet_wallet.json

# Send with explicit miner fee in tARMX:
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
- **ArmaxHash Timeline & Targets:** Blocks 0..519 use bootstrap Double-SHA256 (`0000000ffff...`). Blocks 520..2499 use ArmaxHash v1 (derived BLAKE2b-512 input, Argon2id 2048 KiB, BLAKE2b-256 finalization). Blocks 2500+ use **ArmaxHash v2 Stream-Fast** (domain-separated chained BLAKE2b); the fixed-header hash benchmark reports 0 allocations/op, not end-to-end miner performance.
- **Target Block Time:** 120 seconds ($T=120\text{s}$).
- **Coinbase Maturity:** Consensus-enforced queue per profile (Mainnet: 100 blocks, Public Testnet: 10 blocks, Regtest: 1 block). Freshly mined rewards enter an on-chain immature queue and become spendable only after the required block confirmations are appended to the chain.
- **Dynamic Difficulty Retargeting (LWMA-1):** Continuous 256-bit numerical target adjusted per block across an $N=60$ window ($N+1=61$ blocks evaluated).
  - Anti-timewarp clamping: $\text{solveTime} \in [-720\text{s}, +720\text{s}]$.
  - Dampening bounds: $\text{weightedSolveTimes} \in [\frac{\text{expected}}{4}, \text{expected} \times 4]$.
  - Dynamic target ceiling: During ArmaxHash operation, the target may rise as high as `DifficultyToTarget(1)`. This permits difficulty to decrease but does not guarantee that a particular device can mine blocks at the target cadence.
  - Historical Window Normalization: Pre-activation SHA-256d blocks are normalized to `ArmaxHashPowLimit` during the 60-block transition to prevent legacy targets from dragging down the target.
- **Hardware Profile:**
  - Historical rates of roughly 20-25 H/s on ARM SBCs and 250-700 H/s on desktop CPUs refer to the Argon2id v1 era and are not representative of active v2 mining.
  - The v2 `HashV2` parallel benchmark on an Intel Core i5-8365U reported about 15.2 million hashes/s aggregate and 0 allocations/op for its fixed-header benchmark. This measures the hash primitive, not end-to-end mining throughput; results vary by hardware and workload.
  - The 120-second value is a target block interval, not a guarantee that observed intervals will converge smoothly or remain constant.

#### Production Logging Standards
All binaries implement standard, structured logging:
- **`armcoind`**: Timestamped block receipts `YYYY/MM/DD HH:MM:SS [P2P] [OK] Block #N accepted from IP:port | hash: HASH... | txs: N | diff: N | dt: Ns`.
- **`armminer`**: Real-time hashrate formatting (`%.2f H/s` when $< 1000$ H/s, `%.2f kH/s` when $\ge 1000$ H/s), detailed block solved logs with hash prefix, and clean stale block handling (HTTP 409).
- **`armsend`**: Full transaction broadcast summary including TXID, Sender, Recipient, Amount in the active ticker (`tARMX` on testnet, `ARMX` on mainnet) and Armoids, Fee, Nonce, Payload Size (ML-DSA-65 bytes), and Timestamp.
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

### Key Management & Transaction Signing Architecture
Key generation, keystore management, and post-quantum transaction signing are performed exclusively by the local client tools (`armwallet`, `armsend`, `armgui`). No private keys are generated, stored, or processed in web browsers; public web interfaces and explorers are read-only and broadcast-only.

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
| `GET_BLOCK_RANGE` | Direct | Requests a chunk of blocks bounded to ~1.5 MB (`BlockRangeRequest`). v1.0.7+ requesters may add `accept: ["deflate"]`. |
| `BLOCK_RANGE_RESPONSE` | Direct | Returns chunked `[]core.Block`, or (v1.0.7+, only if requested) `enc: "deflate"` with `z` = base64 DEFLATE of the block array. |
| `PUSH_BLOCKS` | Direct | (v1.0.7+) A node that cannot be dialed pushes blocks the peer lacks. Older nodes ignore it. |
| `REQUEST_CHAIN` | Direct | Requests candidate chain for fork resolution / reorganization. |
| `CHAIN_RESPONSE` | Direct | Returns candidate chain in response to local request (unsolicited responses rejected). |
| `NEW_TX` | Broadcast | Gossips an incoming `core.Transaction` to peer mempools. |
| `GET_PEERS` | Direct | Requests known active peer IP:Port addresses. |
| `PEERS_LIST` | Direct | Returns `[]string` containing discovered peer addresses. |

### 3. P2P Hardening & Transport Encryption
- **Wire Encryption:** P2P uses TLS 1.3 with a persistent self-signed node identity stored in `p2ptls.pem`; TOFU pins the peer certificate fingerprint after first contact. The initial connection is not authenticated by a PKI/CA and can be intercepted unless fingerprints are distributed through a trusted out-of-band channel.
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
   - **Realistic Threat Model & Security Boundaries:** PoW SHA-256 is subject only to quadratic speedup under Grover's algorithm (retaining ~128-bit effective post-quantum security); TLS P2P transport uses self-signed certificates with TOFU public key fingerprinting.

---

## Strategic Roadmap: Next-Generation Innovations

Armaxcoin is strategically architected across 7 core pillars:

1. **Post-Quantum Cryptography (PQC):** NIST FIPS 204 (ML-DSA-65) active in layer-1 consensus transactions.
2. **Advanced Financial Privacy (Future Research):** Confidential Transactions (CT) via Bulletproofs and optional Ring Signatures.
3. **Encrypted P2P Transport:** Fully operational TLS 1.3 socket encryption with TOFU pinning.
4. **Layer-2 Payment Channels (Planned):** Experimental HTLC primitives in core transactions for future off-chain microtransactions.
5. **Lightweight Smart Contracts (Future Research):** Sandboxed WebAssembly (WASM) VM executing expressive UTXO smart contracts.
6. **Small-Device PoW Consensus:** LWMA-1 difficulty adjustment ($N=60$) and **ArmaxHash v2 Stream-Fast** (active from block #2500 on testnet). The hash primitive is fast in the included benchmark; complete mining performance and device efficiency vary by hardware and workload.
7. **Democratic On-Chain Governance:** Block header miner/node signaling for smooth protocol upgrades.

---

## Known Limitations & Security Disclosures

1. **P2P Transport Security:** Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity stored in `p2ptls.pem`. TOFU pins the remote certificate public-key fingerprint after first contact; identity is not verified through a PKI/CA, so the initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band.
2. **PoW Consensus Evolution:** Historical blocks 0..519 use double-SHA256 (SHA-256d), blocks 520..2499 use ArmaxHash v1, and testnet blocks from #2500 use **ArmaxHash v2 Stream-Fast** (chained BLAKE2b). V2 omits the Argon2id memory-hard stage; throughput, power use, and specialized-hardware resistance have not been established across devices.
3. **Initial Hashrate & 51% Risk:** Early testnet and bootstrap states operate with low aggregate network hashrate, leaving the blockchain temporarily vulnerable to 51% hash power reorganization attacks. Reorganizations are strictly bounded by `MaxReorganizationDepth = 720` blocks.
4. **ARM Verification Overhead:** NIST FIPS 204 ML-DSA-65 signatures require modular polynomial arithmetic, introducing measurable CPU verification latency on constrained ARM single-board computers (Raspberry Pi, Orange Pi).
5. **Minimum Go Version:** Compiling Armaxcoin requires Go 1.27+ due to reliance on the standard library `crypto/mldsa` package and toolchain alignment (`go.mod`).
6. **Windows GUI Resource Binary (`gui/rsrc_windows_amd64.syso`):** The repository includes a deliberate pre-compiled Windows resource object required by standard Go toolchains on Windows to link the GUI application manifest and taskbar icon. It can be reproducibly recompiled from source using `rsrc -manifest armgui.exe.manifest -ico armax_logo.ico -arch amd64 -o rsrc_windows_amd64.syso`.

---

## Release History & Changelog

### v1.0.9 (Current -- Testnet Anti-Stall TEDR Fork at Block #13,293, Zero-Allocation CPU Miner & Linux GUI Auto-Start) -- 2026-11-10

Target release: `v1.0.9` (Official Release). Testnet (`ARMX-TESTNET-1`). Mainnet remains locked (`core.MainnetLaunched = false`).

#### 1. Testnet Anti-Stall Emergency Difficulty Adjustment (TEDR - Block #13,293)
- **Emergency Stall-Breaker Target (`Difficulty 3`):** Activates at **Block `#13,293`** on Testnet (`AntiStallActivationHeight = 13293`). When the elapsed time since the parent block exceeds **1,200 seconds (20 minutes)**, candidate blocks qualify for `ArmaxHashPowLimitHex` (`Difficulty 3`), immediately unsticking the chain from post-GPU difficulty shocks (`#13,292`).
- **Model 3 Synthetic Target Normalization:** Inside the 60-block LWMA-1 window, emergency blocks are normalized to `maxSolveTime = 720s` and anchored to `lastNormalTarget`, preventing low-difficulty emergency targets from contaminating `sumTargets`. Permanently disabled on Mainnet (`AntiStallActivationHeight = -1`).

#### 2. Zero-Allocation CPU Miner (`armminer`) Optimization (`5.45x` Speedup) & Benchmark Fix
- **Zero-Allocation Nonce Hot Loop (`0 B/op`, `0 allocs/op`):** Replaces per-nonce `fmt.Sprintf`, `armaxhash.HashV2` buffer allocation, hex string encoding/decoding, and `big.Int` parsing with stack-buffered prefix/suffix assembly (`strconv.AppendInt`), in-place BLAKE2b streaming (`computeHeaderDigestBytes`), 32-byte big-endian target comparisons (`hashBytesMeetTarget32`), and batched Stratum mutex checks (once per 256 hashes). Increases 8-thread CPU hashing speed from `1.13 MH/s` to `6.16 MH/s` (`5.45x` speedup).
- **Canonical Benchmark Fix:** Updates `armminer -benchmark` (`runMinerBenchmark`) and `armaxhash` benchmarks to measure canonical 284-byte `ArmaxHash v2` block headers (`Index = 13052`) instead of Genesis `SHA-256d` (`Index = 0`).

#### 3. Linux/Unix Desktop GUI Embedded Daemon Auto-Start
- **Single-Binary Startup (`gui/main_unix.go`):** Automatically launches an embedded background `armcoind` daemon when `./armgui` targets a local loopback RPC endpoint that is offline, matching Windows `armgui.exe` behavior.

For full technical specifications, test evidence, and operational runbooks, see **[v1.0.9 Release Notes](docs/releases/v1.0.9.md)**.

### v1.0.8 (Previous -- Desktop GUI Authentication, First-Time Onboarding & Peer Privacy Hardening) -- 2026-11-07

Target release: `v1.0.8` (Official Release). Testnet (`ARMX-TESTNET-1`). Mainnet remains locked (`core.MainnetLaunched = false`).

#### 1. Linux Desktop GUI Authentication Fix & Session Security
- **Transparent Session Injection:** Injects active session security tokens directly into the Web UI HTML and issues a secure `HttpOnly`, `SameSite=Strict` session cookie (`armax_gui_token`). Client-side JavaScript automatically attaches `X-Armx-GUI-Token` to all API requests, resolving the `401 Unauthorized` error when creating new wallets.
- **DNS Rebinding Protection:** Preserves strict validation of the `Host` header (`127.0.0.1`, `localhost`, `::1`), protecting users from malicious cross-origin websites attempting to access local node RPCs.

#### 2. First-Time Wallet Onboarding & UI Enhancements
- **Automated Setup Dialog (`#setupWalletModal`):** Automatically appears on first launch when no active keystore exists, guiding users through encrypted wallet creation (minimum 12-character passphrase) or restoring existing 24-word BIP-39 mnemonic seed phrases (`#restoreWalletModal`).
- **Overview Status Banner (`#noWalletCard`):** Displays a persistent onboarding card on the Overview tab when no active address is loaded, replacing the previous `undefined` address display.

#### 3. P2P Peer Display Privacy Filter
- **Residential Home Node Masking:** Filters out ephemeral non-listening outbound client sockets from the GUI's connected peers and diagnostics. Only DNS seeds, domain hostnames, and public IPs with open listening P2P ports (`19335`/`19334`) are displayed, shielding home node IPs from public exposure.

For full technical specifications, test evidence, and operational runbooks, see **[v1.0.8 Release Notes](docs/releases/v1.0.8.md)**.

### v1.0.7 (Previous -- Testnet Consensus Fork & Optimization Release) -- 2026-11-05

Target release: `v1.0.7` (`core.Version = "1.0.7"`). Testnet only (`ARMX-TESTNET-1`). Mainnet remains locked (`core.MainnetLaunched = false`).

> [!NOTE]
> **Known Issue in v1.0.7:** Linux and ARM GUI wallet creation encounters authentication errors in v1.0.7 due to session token handling. Linux and ARM users should upgrade to v1.0.8.

> [!WARNING]
> **STORAGE COMPATIBILITY & DOWNGRADE WARNING:**
> BoltDB record compression is **disabled by default**. Compressed records created by v1.0.7 cannot be parsed or read by v1.0.6. Operators enabling compression must back up `blockchain.db` before conversion. Downgrading to v1.0.6 requires stopping the daemon and converting database records back to plain format using:
> ```bash
> ./armcoind -network testnet -datadir <DIR> -store-rewrite plain
> ```

#### 1. Scheduled Consensus Fork (Block 20,000)
- **Tighter Future Timestamp Limit:** Activates at testnet block **20,000** (`FutureTimestampLimitActivationHeight`). Block timestamps may be at most **600 seconds (10 minutes)** ahead of the validating node's local clock (reduced from the legacy 7,200-second / 2-hour window). Below height 20,000, the 2-hour rule applies, preserving all historical blocks. Mainnet, soak, and regtest profiles remain disabled (`-1`).
- **Miner Clock Skew Defense & Template Waiting:** `armminer` detects when the local machine clock leads the node daemon clock by more than 60 seconds (`ClockSkewWarnSeconds`), aligns candidate header timestamps to the node's reference time, and automatically pauses (up to 30s) instead of hashing an invalid block.
- **Node Log Visibility:** Core block validation emits `[Mining] ... timestamp forced to minimum valid ... check NTP` and logs forward timestamps in `ahead: Ns` format.

#### 2. P2P Networking & Transport Hardening
- **`PUSH_BLOCKS` for NAT Nodes:** Allows nodes behind NAT or firewalls that cannot receive inbound connections to push valid competing blocks directly to public peers over their outbound connection.
- **Multi-Chunk Staging Buffer (`HIGH-07`):** Implemented in-memory `pushBufferSession` (30s TTL) to stage sequential competitor chunks up to `MaxPushBlocks = 1000` until cumulative chainwork exceeds the local tip before triggering reorganization.
- **Negotiated DEFLATE Compression:** `GET_BLOCK_RANGE` requests may advertise `accept: ["deflate"]` with server responding with `enc: "deflate"`. Bounded by 32 MB decompression limit and 10% compression efficiency threshold. Disable with `ARMX_P2P_COMPRESSION=0`.
- **`PUSH_BLOCKS` Decompression Handling (`HIGH-08`):** Expands compressed block ranges in pushed batches before applying them to local chain resolution.
- **Inbound Handshake Timeout:** Unauthenticated inbound connections must transmit their initial valid P2P message within 5 seconds (`InboundHandshakeTimeout`), eliminating slowloris connection-slot exhaustion.
- **Persistent Ban Store:** Peer bans persist to `<dataDir>/<network>/bans.json` across daemon restarts.
- **SPKI Fingerprint Pinning & Identity Alignment (`CRIT-02`):** Optional `seed_fingerprints` config map validates SHA-256 SPKI digests of seed certificates. Node identity generation computes SPKI hash rather than whole X.509 certificate.
- **IBD Linkage Verification & Ban Scoring (`HIGH-09`):** Validates cryptographic hash continuity (`PrevHash == Blocks[i-1].Hash`) during initial block download chunks; immediately applies 50 ban points and records peer failures for non-contiguous segments.
- **Bounded Address Cache Memory Cap (`HIGH-03`):** Replaced unbounded address map with a 10,000-entry capacity-capped, mutex-protected cache with 20% eviction.

#### 3. Blockchain Sync & Storage Optimization
- **Incremental Persistence (`saveToFileFrom`):** `ResolveFork` persists only blocks at or above the common fork point, eliminating the O(N²) disk rewrite overhead during initial block download.
- **Optional BoltDB Record Compression:** Block, undo, and snapshot records can be DEFLATE-compressed (`-store-compression` or `ARMX_STORE_COMPRESSION=1`). Offline conversion available via `-store-rewrite plain|compressed`.
- **Accurate Circulating Supply Parity (`HIGH-04`):** Fixed supply calculation during disk database replay (`loadBlockchainFromPath`), full revalidation (`RebuildState`), live additions, and reorgs to accumulate `BlockMintedSubsidy` (`coinbase.Amount - totalFees`), maintaining mathematical parity even with underclaimed coinbase subsidies.
- **Explorer Compaction:** Added `Storage.Compact()` executing WAL checkpointing and conditional `VACUUM` when >= 20% of SQLite pages are free.

#### 4. Subsystem Security & Reliability
- **Local Web GUI Hardening (`CRIT-01`):** Enforces loopback `Host` header validation (`127.0.0.1`, `localhost`, `::1`), requires cryptographically random session token (`X-Armx-GUI-Token`), and mandates wallet passphrase validation on `/api/send`.
- **Masked Interactive Mnemonic Input (`HIGH-06`):** Added `-restore-interactive` flag using terminal password masking (`golang.org/x/term`) to prevent seed phrase exposure in process listings (`ps aux`).
- **Stratum Inf/NaN Float Guard (`HIGH-05`):** Guarded `set_difficulty` RPC against non-finite float inputs before integer conversion.
- **`X-Mine-Key` Priority Authentication (`HIGH-10`):** Evaluates `X-Mine-Key` prior to falling back to HTTP Basic Auth and records authentication success to prevent false IP lockouts.
- **Bridge Multisig Key Custody Separation (`HIGH-11`):** Introduced `RemoteValidatorClient` and `MultisigAggregator` interfaces for threshold attestation collection across separate validator hosts.
- **Undo Record Concurrency Mutex (`HIGH-01`):** Added dedicated `undoLock sync.RWMutex` to `core.Blockchain` and implemented deep copy cloning (`cloneBlockUndoRecord()`), eliminating map data races under concurrent block processing.
- **Locked Consensus Height Overrides (`HIGH-02`):** PoW activation heights (`ARMX_ARMAXHASH_HEIGHT`, `ARMX_ARMAXHASH_V2_HEIGHT`) are strictly immutable on Mainnet and Testnet, honored only on Regtest.

For full technical specifications, test evidence, and operational runbooks, see **[v1.0.7 Release Notes](docs/releases/v1.0.7.md)**.

### v1.0.6 (Stabilization in Progress -- Testnet Maintenance & Reliability) -- 2026-10-31

Maintenance and stability release resolving verified node lifecycle issues, resource leaks, data races, and storage isolation. Requires no database migration, resync, or consensus hard fork (upgrading requires individual node process restart).

> **Release Status:** `v1.0.6` is an active testnet maintenance release. Core binaries compile and pass unit, diagnostic, and P2P verification suites under CI.

- **Daemon Graceful Shutdown & Database Integrity:** Implemented `ShutdownServerSafely` to reorder shutdown sequence, initiate HTTP listener termination, cleanly stop P2P network cycles, flush database, and close BoltDB file handle without deadlock. If HTTP handler draining fails or times out, database persistence is aborted (fails closed) to protect against corruption.
- **Background Resource Leaks Eliminated:** Added `Close()` method and completion signaling to `IPRateLimiter` background cleanup; pruned dead sockets on P2P heartbeat ping failure.
- **Data Race Hardening:** Converted block and transaction serialized size caches to `int32` synchronized with `sync/atomic`, eliminating data races under concurrent mempool admission and P2P block validation.
- **Outbound Work Admission on NAT Nodes:** `registerOutboundWork` safely admits work when `activeCycle == nil`, enabling client nodes and nodes behind NAT to reliably broadcast mined blocks and relay transactions.
- **Peer Store Non-Destructive Migration & Corruption Guard:** Legacy `<dataDir>/peers.json` is copied non-destructively to isolated `<dataDir>/<network>/peers.json` with `0600` permissions. Corrupted peer stores are preserved at `<path>.corrupt` and trigger a fail-closed halt to prevent MITM adoption.
- **Miner RPC Cookie Auto-Detection:** `armminer` queries `node.GetRPCAuthCookiePath()` first, supporting isolated network directories and legacy working directory locations.
- **RPC Security & Bounded Memory:** Enforced strict 5,000-entry memory cap with 15-minute expiration on RPC auth failure tracking with prioritized lockout eviction.
- **Software Version:** Upgraded core software version metadata constant to `1.0.6` (does not alter P2P wire protocol negotiation).

---

### v1.0.5 (Public Open Testnet -- Display Ticker, CLI Hardening & Release Prep) -- 2026-10-27

Hard-fork release. Timestamp rules C1/C2 (MTP-55, non-decreasing timestamps) activate at testnet block 8,000; all nodes must upgrade before then. Genesis, NetworkID, and hashing are unchanged, and existing testnet blocks remain valid.

- **Testnet display ticker `tARMX`:** daemon, miner, GUI, and CLI help use the active ticker (display-only). `ARMX` remains the mainnet ticker.
- **P2P block synchronization past block #4000:** Fixed integer precision loss for 64-bit nonces (> 2^53) during JSON deserialization in `GET_BLOCK_RANGE`, eliminating hash mismatch errors during peer sync.
- **Stratum v1 TCP Pool Client (`armminer`):** Native `stratum+tcp://` client with multi-endpoint failover, dynamic VarDiff support, and per-thread nonce partitioning.
- **Windows Defender false positive mitigation:** Enforced `-trimpath` and stripped symbols across PE executables to eliminate machine learning heuristic flags.
- **CLI standards:** non-zero exit codes on errors, JSON error output for `armsend -json`, warnings on stderr, deprecation notices for legacy aliases, and `-h`/`--help`.
- **Fixes:** Unix GUI miner RPC authentication (HTTP 401), missing TXID in the Unix GUI transactions tab, encrypted wallet creation/restore persistence in Unix Web GUI (`gui/main_unix.go`), `armwallet -rescan` JSON array parsing, `/getmininginfo` `armaxhash-v2` reporting, `/getblocktemplate` state overlay filtering, and full consensus/state verification in `IsChainValid()` (`armcoind -verifychain`).
- **Reconnect fix:** seed peers are never evicted and are re-dialed every ~60 s (quietly, without log spam); a node that lost its internet link rejoins automatically. A heavier fork deeper than 720 blocks logs an explicit `-resync` hint.
- **Security:** pinned `toolchain go1.27.3` (`GO-2026-6809`); fail-fast startup errors instead of panics.
- **Note:** `v1.0.2` was never released; the version sequence goes `v1.0.1` to `v1.0.3`.
- Consensus proposals C1 and C2 are implemented in this release; C3–C5 remain design-only (`docs/security/consensus-proposals.md`).

See [CHANGELOG.md](CHANGELOG.md) for details.

---

### v1.0.4 (Public Open Testnet -- Structured Diagnostics & Consensus Hardening) -- 2026-10-27

Maintenance, reliability, and consensus hardening release introducing structured RPC error handling, enhanced miner telemetry, continuous soak and CI verification automation, and synchronized explorer ecosystem documentation.

#### Node & Consensus Hardening
- **Consensus Version Bump:** Upgraded core protocol version to `v1.0.4` across node handshake, status outputs, and daemon runtime.
- **Enhanced Diagnostics & CI Automation:** Added full automated test suites including race-detector coverage and multi-node chaos soak tests (`TestSoak_FiveNodesChaos`, `TestSoak_NoTargetOverride_LWMA300Blocks`) validating LWMA-1 difficulty adjustment and reorg bounds.
- **Release Verification Script Fixes:** Streamlined cross-platform release verification tools (`scripts/verify-release.sh` and `scripts/verify-release.ps1`) targeting `v1.0.4`.

#### Mining & Tooling
- **Miner Telemetry & Diagnostic Logging:** Enhanced thread status tracking, hashrate telemetry, and RPC connectivity failover reporting.
- **GUI Statusbar Synchronization:** Updated desktop wallet GUI status indicator to reflect `v1.0.4`.

---

### v1.0.3 (Public Open Testnet -- ArmaxHash v2 Stream-Fast & Hardening) -- 2026-10-24

Major consensus evolution and stability hardening release activating ArmaxHash v2 Stream-Fast PoW hard fork at block #2500, introducing comprehensive storage replay protection, node fail-fast database guards, keystore auto-backups, and standardized RPC schemas.

#### Consensus & PoW Evolution (ArmaxHash v2)
- **ArmaxHash v2 Stream-Fast PoW (Hard Fork Block #2500):** Transitioned active mining from Argon2id v1 to high-efficiency two-stage Chained BLAKE2b (`BLAKE2b-512` prehash + `BLAKE2b-256` block hash).
- **Hash Primitive Benchmark:** The fixed-header `HashV2` benchmark reports 0 allocations/op. The full worker loop, including header construction, is not covered by that benchmark; no zero-allocation or DRAM-saturation claim is made for the complete miner.
- **Validation Performance:** The hash benchmark measures `HashV2` only; full block validation also performs transaction and state checks and is not characterized by this benchmark.
- **100% Historical Era Preserved:** Maintains backward consensus compatibility across all prior eras (Double-SHA256 blocks 0..519, ArmaxHash v1 blocks 520..2499).

#### Reliability, Storage & Security Hardening
- **Storage Replay Future Wall-Clock Fix [T13]:** Bypasses future timestamp drift checks during historical block replay and re-indexing.
- **Production Database Fail-Fast Guard [T12]:** Aborts startup on bbolt database open failure to prevent silent in-memory fallback and data loss.
- **Keystore Overwrite Protection & Auto-Backup [T15]:** Protects `wallet.json` against accidental overwriting by auto-generating timestamped backup files.
- **Standardized RPC Error Schema:** Enforces consistent `error`, `message`, and `status` JSON structures across all HTTP endpoints for mobile and explorer GUI interoperability.
- **Formal Consensus Improvement Proposals (C1-C5):** Documented specifications for reorg limits, timewarp mitigations, and mempool bounding in `docs/security/consensus-proposals.md`.

---

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
- **ArmaxHash v1 PoW:** Testnet blocks 520..2499 use a network/header-derived BLAKE2b-512 prehash, Argon2id (2048 KiB, $t=2$, $p=1$), and BLAKE2b-256 finalization. Later testnet blocks use v2; see `docs/CONSENSUS_AND_NETWORK.md` for the complete height schedule.
- **Continuous LWMA-1 Difficulty Adjustment:** Sliding-window retargeting ($N=60$) with solve-time clamps and weighted-sum bounds. These bounds limit retarget inputs; they do not eliminate timestamp manipulation or guarantee a particular block cadence.
- **Chainwork & Fork Resolution:** Cumulative chainwork fork choice with a local `MaxReorganizationDepth = 720` policy limit (~24 hours at target cadence); this is not transaction finality.
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

## Support Armaxcoin Development

Armaxcoin is an independent blockchain project built with a focus on open-source development, network reliability, and long-term experimentation. Community contributions help sustain ongoing development, testing, infrastructure, and future improvements.

Contributions are entirely voluntary and are appreciated, but never expected. Community contributions do not grant ownership, equity, governance privileges, guaranteed benefits, or financial returns.

- **Developer Contact:** [dev@armax-coin.cloud](mailto:dev@armax-coin.cloud)
- **Ethereum (ETH):** `0x6945d02B7c3A6061afc10f69732550dc6fD6d195`
- **Bitcoin (BTC):** `bc1qy7rg0u58c8mgq6mu29g8agc7qp9yceqmelsm5f`

> [!WARNING]
> Please verify the selected network and wallet address before sending. Cryptocurrency transactions are generally irreversible. Only send assets using the corresponding supported network.

For full details, see [SUPPORT.md](SUPPORT.md).

---

## License

Armaxcoin is licensed under the open-source [MIT License](LICENSE).  
Copyright (c) 2026 acxxcxx.
