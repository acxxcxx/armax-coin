# Armaxcoin (`armax-coin`)

**Canonical Repository:** [github.com/acxxcxx/armax-coin](https://github.com/acxxcxx/armax-coin)  
**Current Release:** `v1.0.12` (Desktop Wallet GUI v2 `"Waiting for Sync"`, Node Sync Gate `HTTP 503`, Multi-Thread CPU Miner Optimization, Stratum TLS, Go `1.27.4`; Rolling Compatible with `v1.0.11`)  
**Go Module:** `github.com/acxxcxx/armax-coin`

Armaxcoin is an open-source, experimental Layer-1 Proof-of-Work (PoW) cryptocurrency engineered for post-quantum transaction security (NIST FIPS 204, ML-DSA-65), accessibility, and high performance across commodity personal computers and ARM-based devices (Armbian, Raspberry Pi 4/5, Orange Pi).

> [!IMPORTANT]
> **ACTIVE TESTNET CONSENSUS FORKS (BLOCKS 13,293, 13,335 & 20,000):**
> - **Block 13,293 (v1.0.9 — Anti-Stall TEDR, Active):** Activates Testnet Anti-Stall Emergency Difficulty Adjustment (`AntiStallActivationHeight = 13293`). When the elapsed interval since the parent block exceeds **1,200 seconds (20 minutes)**, candidate blocks qualify for an emergency stall-breaker target (`Difficulty 3`) with Model 3 Synthetic Target Normalization (`720s` solve time, anchored to `lastNormalTarget`). Disabled in the provisional Mainnet staging profile (`-1`).
> - **Block 13,335 (v1.0.10 — Armax-ASERT-AW DAA, Active):** Activates the canonical integer `aserti3-2d` difficulty adjustment algorithm (`ASERTActivationHeight = 13335`), anchored to Block `#13,334` with Anti-Poisoning fallback (DA-03), 4-hour surge cap (`14400s`), TEDR Boost (`7200s`), and bounded drift cache pruning (`10,000 -> 2,048` entries).
> - **Block 20,000 (v1.0.11+ — Unified C6 + `TxBinaryV1`, Active):** Enforces the 10-minute maximum future block timestamp limit (**600 seconds**, paired with adaptive `5s` inherited-tip / `30s` local-clock miner safety margins) and canonical `TxBinaryV1` binary block/transaction size accounting (`BlockBinarySizeActivationHeight = 20000` on Testnet, `0` in locked provisional Mainnet staging), expanding 2 MB block capacity from 185 to **375 user ML-DSA-65 transactions**. `v1.0.12` introduces zero hard forks or activation-height changes (100% rolling-compatible with `v1.0.11` on `ARMX-TESTNET-1`).
> 
> **Operator Guidance:** Public nodes, miners, and desktop wallet users running `v1.0.11` can upgrade cleanly to **`v1.0.12`** at any time while keeping server/miner clocks synchronized with NTP (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows).

> [!NOTE]
> **PUBLIC TESTNET ENVIRONMENT & PROVISIONAL MAINNET LOCK:**
> Armaxcoin is currently operating strictly as Public Testnet software (`ARMX-TESTNET-1`). Testnet coins (`tARMX`) have **no monetary value**, no market liquidity, no guarantee of conversion, and no investment utility.
> Mainnet (`ARMX-MAINNET-1`) remains strictly locked (`core.MainnetLaunched = false`); invoking `-network mainnet` is rejected across all binaries. Values currently present in `MainnetProfile` represent a **provisional pre-launch staging configuration** for isolated local testing (`-local-mainnet`) and are **not** a finalized Mainnet launch baseline — all Mainnet parameters remain subject to revision prior to official Mainnet launch freeze after satisfying [docs/MAINNET_READINESS.md](docs/MAINNET_READINESS.md).

> [!WARNING]
> **DEVELOPER EARLY-MINING PHASE DISCLOSURE & RISKS:**
> Mainnet is locked and has not launched. Source code preserves the 126,000,000 ARMX maximum supply, 60 ARMX initial reward, 1,050,000-block halving interval, 120-second target block time, and existing genesis (60 ARMX genesis coinbase burned to the canonical zero-payload address).
> Before public mainnet opening, the developers will mine blocks 1 through 16,667. Stop rule: Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first. Developers will mine all blocks in this range. At 60 ARMX per block, 16,667 blocks yield up to 1,000,020 ARMX (16,666 blocks = 999,960 ARMX; block 16,667 yields 1,000,020 ARMX due to discrete block subsidies, representing ~0.7937% of the total 126,000,000 ARMX supply). Estimated duration is approximately 23.15 days at 120-second target block time; actual duration depends on hashrate and difficulty. Public mainnet opening: TBA. Treasury address will be published before mainnet launch. Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch.
> **Risks / Criticism:** Early developer mining represents an initial point of centralization and grants the developer team a material fraction (~0.79% of the total 126,000,000 ARMX supply) before the public opening. For the proposed post-launch allocation draft, see [`docs/governance/TREASURY_MIGRATION_PLAN.md`](docs/governance/TREASURY_MIGRATION_PLAN.md) and [`docs/architecture/tokenomics/mining-accumulation-plan.md`](docs/architecture/tokenomics/mining-accumulation-plan.md).

---

## Authoritative Documentation Index

| Guide | Description |
|---|---|
| **[Release Notes (v1.0.12)](docs/releases/RELEASE_NOTES.md)** | Authoritative v1.0.12 testnet release notes: Desktop GUI v2 (`"Waiting for Sync"`, WebView2, System Tray, Multi-Wallet), Node Sync Gate (`HTTP 503`), integrated CPU multi-thread optimization, Stratum TLS, and Go `1.27.4` |
| **[Changelog](CHANGELOG.md)** | Complete version history adhering to Keep a Changelog |
| **[Technical Whitepaper](docs/architecture/WHITEPAPER.md)** | Core system architecture, post-quantum signatures, consensus rules, and tokenomics |
| **[Treasury & Miner Migration Plan](docs/governance/TREASURY_MIGRATION_PLAN.md)** | Proposed Mainnet treasury governance, testnet miner compensation, liquidity reserves, and soak criteria |
| **[Open Testnet Guide](docs/TESTNET.md)** | Comprehensive testnet guide: 1-click GUI (`armgui`), syncing, mining, CLI usage, multi-node setups, and troubleshooting |
| **[CPU Mining Guide](docs/MINING_GUIDE.md)** | Solo and Stratum (`tcp://`/`ssl://`) CPU mining with `armminer` and `armgui`, thread tuning, and active consensus rules |
| **[Node Operations Guide](docs/NODE_OPERATIONS.md)** | Running full nodes, seed nodes, systemd deployment, TLS 1.3, and TOFU certificate security |
| **[Consensus & Network Specification](docs/CONSENSUS_AND_NETWORK.md)** | Detailed mathematical specifications for Armax-ASERT-AW, LWMA-1, ArmaxHash v2, ML-DSA-65, and reorg limits |
| **[Build Guide & Reproducible Binaries](docs/BUILD.md)** | Compiling from source across Linux, Windows, and macOS ARM64 with reproducible build instructions |
| **[Release Engineering & QA Checklist](docs/RELEASE.md)** | Versioning standards, reproducible builds, SHA-256 verification, and pre-tagging QA checklist |
| **[Mainnet Readiness Gate](docs/MAINNET_READINESS.md)** | Security evaluation, code freeze requirements, and mainnet unlock criteria |
| **[Security Policy & Disclosures](SECURITY.md)** | Threat model, responsible disclosure procedures, and known limitations |
| **[Architecture Documentation](docs/architecture/README.md)** | Taxonomy of architectural designs, consensus proposals, and implementation boundaries |
| **[Contribution Guidelines](CONTRIBUTING.md)** | Contributor workflows, coding standards, Conventional Commits, and test requirements |
| **[Code of Conduct](CODE_OF_CONDUCT.md)** | Community standards and enforcement guidelines |
| **[Contributors](docs/community/CONTRIBUTORS.md)** | List of project contributors recognized across Git history |
| **[Development Support](SUPPORT.md)** | Voluntary contribution addresses and developer contact information |

---

## Core Network & Consensus Parameters

| Parameter | Testnet (`ARMX-TESTNET-1`) | Mainnet (`ARMX-MAINNET-1` — Locked Provisional Staging) |
|---|---|---|
| **Launch Status** | **Active Public Testnet** | **Locked (`core.MainnetLaunched = false`); Provisional & Subject to Change** |
| **Target Block Time** | 120 seconds (2 minutes) | 120 seconds (2 minutes) |
| **Difficulty Adjustment** | Armax-ASERT-AW (Block #13,335+; $\tau = 3600\text{s}$, 4h Surge Cap, TEDR Boost) | *Provisional Staging:* Armax-ASERT-AW (Block #1+; $\tau = 3600\text{s}$, 4h Surge Cap; TEDR disabled) |
| **Proof-of-Work Algorithm** | ArmaxHash v2 (BLAKE2b Stream-Fast, $H \ge 2500$); Historical: v1 & SHA-256d | *Provisional Staging:* ArmaxHash v2 (Block #1+); Immutable Genesis #0: SHA-256d |
| **Digital Signatures** | NIST FIPS 204 ML-DSA-65 (Quantum-Safe) | NIST FIPS 204 ML-DSA-65 (Quantum-Safe) |
| **Address Encoding** | BIP-173 Bech32 (`armx1...`, 63 characters) | BIP-173 Bech32 (`armx1...`, 63 characters) |
| **Initial Block Subsidy** | 60 tARMX (coinbase at index 0) | 60 ARMX (coinbase at index 0) |
| **Halving Interval** | Every 1,050,000 blocks (~4 years) | Every 1,050,000 blocks (~4 years) |
| **Maximum Supply Cap** | 126,000,000 tARMX | 126,000,000 ARMX |
| **Coinbase Maturity** | **10 confirmations** | *Provisional:* **100 confirmations** |
| **Max Reorg Depth** | 720 blocks (`MaxReorganizationDepth`) | *Provisional:* 720 blocks (`MaxReorganizationDepth`) |
| **Block Size / Tx Limit** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB, 1,000 txs/block (`TxBinaryV1` at #20,000 -> 375 user txs) | *Provisional Staging:* MaxBlockSize = 2 MB, MaxTxSize = 20 KB, 1,000 txs/block (`TxBinaryV1`, C1/C2 & C6 `+600s`) |
| **P2P Gossip Port** | `19335` (TCP, TLS 1.3 with TOFU) | `19334` (TCP, TLS 1.3 with TOFU) |
| **JSON-RPC Port** | `127.0.0.1:19333` (Strictly Loopback) | `127.0.0.1:19332` (Strictly Loopback) |
| **Public Seed Nodes** | `testnet-seed1.armax-coin.cloud:19335`<br>`testnet-seed2.armax-coin.cloud:19335`<br>`testnet-seed3.armax-coin.cloud:19335` | TBA prior to mainnet launch freeze |

---

## Quickstart: Running on Public Testnet

All binaries default to the active testnet profile (`ARMX-TESTNET-1`).

### 1. Download & Verify Release Artifacts

Pre-compiled binaries and signatures are published on [GitHub Releases](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.12):

```bash
# On Linux / ARM64:
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.12/armax-v1.0.12-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.12/SHA256SUMS
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.12/SHA256SUMS.asc
curl -LO https://raw.githubusercontent.com/acxxcxx/armax-coin/main/ARMAX-PGP-KEY.asc

# 1. Import official release key & verify fingerprint (C857E67F3FC63BE35125C3D32ACE863ED4BF1460):
gpg --import ARMAX-PGP-KEY.asc
gpg --fingerprint dev@armax-coin.cloud

# 2. Verify detached signature on SHA256SUMS:
gpg --verify SHA256SUMS.asc SHA256SUMS

# 3. Verify archive checksum:
sha256sum --check --ignore-missing SHA256SUMS

# Extract archive:
tar -xzf armax-v1.0.12-linux-amd64.tar.gz
chmod +x armcoind armminer armsend armwallet armgui
```

On Windows (PowerShell):
```powershell
# Verify SHA-256 checksum:
Get-FileHash -Algorithm SHA256 .\armax-v1.0.12-windows-amd64.zip
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.12-windows-amd64.zip"
```

### 2. Launch the Desktop Wallet (`armgui`) or Full Node Daemon (`armcoind`)

**1-Click Desktop Wallet GUI v2 (`armgui`):** Double-click `armgui.exe` on Windows (native WebView2 window with **Minimize to System Tray**, Multi-Wallet switcher, and **CPU Miner** tab featuring `"Waiting for Sync"` pre-sync protection) or run `./armgui` on Linux/WSL/macOS (automatically falls back to `~/.config/ArmaxCoin` and opens your browser). When no local daemon is running, `armgui` starts an embedded testnet node automatically.

For headless servers, start `armcoind` directly:

```bash
# Linux / macOS / ARM64
./armcoind

# Windows
.\armcoind.exe
```
The daemon connects to public seed nodes, initiates chunked IBD, and binds RPC strictly to `127.0.0.1:19333`.

### 3. Create a Quantum-Safe Keystore

```bash
# Create a new wallet keystore (-file is primary; -wallet is an alias):
./armwallet -create -file my_testnet_wallet.json

# Display recipient address (armx1...):
./armwallet -address -file my_testnet_wallet.json
```
> [!TIP]
> Record your 24-word recovery phrase on paper. Keystores are encrypted via Argon2id + AES-256-GCM with `0600` permissions. To restore safely without process-table exposure, always use `./armwallet -restore-interactive`.

### 4. Start Mining Testnet Blocks

```bash
# Option A: Standalone multi-threaded CPU miner (solo against local armcoind):
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4

# Option B: Encrypted Stratum TLS pool client:
./armminer -pool stratum+ssl://pool.example.com:3443 -user <YOUR_ARMX_ADDRESS>.worker1 -pass x -threads 4

# Option C: Internal daemon background mining (or use the CPU Miner tab in armgui):
./armcoind -mine -miningthreads 4 -miner <YOUR_ARMX_ADDRESS>
```

### 5. Check Balance & Send Transactions

```bash
# Query balance from the daemon:
./armwallet -balance <YOUR_ARMX_ADDRESS>

# Send testnet coins (transfer 10 tARMX; -wallet is primary on armsend):
./armsend -to <RECIPIENT_ADDRESS> -amount 10 -wallet my_testnet_wallet.json
```
Newly mined block rewards require **10 confirmations** on Testnet (100 on Mainnet) before becoming spendable. For complete command syntax and options, see [`docs/TESTNET.md`](docs/TESTNET.md), [`docs/MINING_GUIDE.md`](docs/MINING_GUIDE.md), and [`docs/NODE_OPERATIONS.md`](docs/NODE_OPERATIONS.md).

---

## Known Limitations & Threat Model Summary

- **P2P Transport Security:** Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity (`p2ptls.pem`). TOFU pins remote certificate public-key fingerprints after first contact; initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band.
- **Quantum Preimage Security:** Transaction signatures use NIST FIPS 204 ML-DSA-65 (resistant to Shor's algorithm). PoW hashing (historical SHA-256d and active BLAKE2b ArmaxHash v2) is subject only to quadratic speedup under Grover's algorithm (~128 bits post-quantum security).
- **Early Network Centralization:** Testnet hashrate is modest, leaving early chains susceptible to transient 51% reorganizations bounded by `MaxReorganizationDepth = 720`.
- **Hardware Requirements:** ML-DSA-65 modular polynomial arithmetic introduces measurable verification latency on low-power ARM devices. Compiling from source requires Go 1.27 or higher (`crypto/mldsa`).
- For full security disclosures and vulnerability reporting procedures, see [`SECURITY.md`](SECURITY.md).

---

## License & Support

Armaxcoin is released under the [MIT License](LICENSE).

- **Official Release Signing Key:** PGP Fingerprint `C857E67F3FC63BE35125C3D32ACE863ED4BF1460`
- **Developer Contact:** `dev@armax-coin.cloud`
- **Voluntary Project Support:** See [`SUPPORT.md`](SUPPORT.md) for official development contribution addresses.
