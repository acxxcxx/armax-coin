# Armaxcoin (`armax-coin`)

**Canonical Repository:** [github.com/acxxcxx/armax-coin](https://github.com/acxxcxx/armax-coin)  
**Current Release:** `v1.0.10` (Armax-ASERT-AW DAA Hard Fork at Block #13,335, Surge Cap & TEDR Boost, Immutable MiningJob & 3-Tier P2P Policy)  
**Go Module:** `github.com/acxxcxx/armax-coin`

Armaxcoin is an open-source, experimental Layer-1 Proof-of-Work (PoW) cryptocurrency engineered for post-quantum transaction security (NIST FIPS 204, ML-DSA-65), accessibility, and high performance across commodity personal computers and ARM-based devices (Armbian, Raspberry Pi 4/5, Orange Pi).

> [!IMPORTANT]
> **ACTIVE & SCHEDULED TESTNET CONSENSUS FORKS (BLOCKS 13,293, 13,335 & 20,000):**
> - **Block 13,293 (v1.0.9 — Anti-Stall TEDR):** Activates Testnet Anti-Stall Emergency Difficulty Adjustment (`AntiStallActivationHeight = 13293`). When the elapsed interval since the parent block exceeds **1,200 seconds (20 minutes)**, candidate blocks qualify for an emergency stall-breaker target (`Difficulty 3`) with Model 3 Synthetic Target Normalization (`720s` solve time, anchored to `lastNormalTarget`). Permanently disabled on Mainnet (`-1`).
> - **Block 13,335 (v1.0.10 — Armax-ASERT-AW DAA):** Activates the canonical integer `aserti3-2d` difficulty adjustment algorithm (`ASERTActivationHeight = 13335`), anchored to Block `#13,334` with Anti-Poisoning fallback (DA-03), 4-hour surge cap (`14400s`), and TEDR Boost (`7200s`) for rapid 200x hashrate crash recovery.
> - **Block 20,000 (v1.0.7+ — Proposal C6):** Tightens maximum allowed future block timestamp from 7,200 seconds (2 hours) down to **600 seconds (10 minutes)** ahead of the validating node's local clock.
> 
> **Operator Action Required:** All testnet node operators, miners, and pool maintainers **MUST** upgrade immediately to **v1.0.10** prior to Block `#13,335`, and ensure server clocks are synchronized with NTP (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows).

> [!NOTE]
> **PUBLIC TESTNET ENVIRONMENT:**
> Armaxcoin is currently operating on the public testnet (`ARMX-TESTNET-1`). Testnet coins (`tARMX`) have **no monetary value**, no market liquidity, no guarantee of conversion, and no investment utility.
> The mainnet configuration (`ARMX-MAINNET-1`) remains locked in source (`core.MainnetLaunched = false`) while network stabilization and soak verification continue. Invoking `-network mainnet` is strictly rejected by all binaries.

> [!WARNING]
> **DEVELOPER EARLY-MINING PHASE DISCLOSURE & RISKS:**
> Mainnet is locked and has not launched. Source code preserves the 126,000,000 ARMX maximum supply, 60 ARMX initial reward, 1,050,000-block halving interval, 120-second target block time, and existing genesis (60 ARMX genesis coinbase burned to the canonical zero-payload address).
> Before public mainnet opening, the developers will mine blocks 1 through 16,667. Stop rule: Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first. Developers will mine all blocks in this range. At 60 ARMX per block, 16,667 blocks yield up to 1,000,020 ARMX (16,666 blocks = 999,960 ARMX; block 16,667 yields 1,000,020 ARMX due to discrete block subsidies, representing ~0.7937% of the total 126,000,000 ARMX supply). Estimated duration is approximately 23.15 days at 120-second target block time; actual duration depends on hashrate and difficulty. Public mainnet opening: TBA. Treasury address will be published before mainnet launch. Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch.
> **Risks / Criticism:** Early developer mining represents an initial point of centralization and grants the developer team a material fraction (~0.79% of the total 126,000,000 ARMX supply) before the public opening. For the proposed post-launch allocation draft, see [`TREASURY_MIGRATION_PLAN.md`](TREASURY_MIGRATION_PLAN.md) and [`docs/architecture/tokenomics/mining-accumulation-plan.md`](docs/architecture/tokenomics/mining-accumulation-plan.md).

---

## Authoritative Documentation Index

| Guide | Description |
|---|---|
| **[Release Notes (v1.0.10)](RELEASE_NOTES.md)** | Authoritative v1.0.10 release notes, Block 13,335 Armax-ASERT-AW DAA fork, surge cap & TEDR boost, immutable MiningJob, and 3-tier P2P policy |
| **[Changelog](CHANGELOG.md)** | Complete version history adhering to Keep a Changelog |
| **[Technical Whitepaper](WHITEPAPER.md)** | Core system architecture, post-quantum signatures, consensus rules, and tokenomics |
| **[Treasury & Miner Migration Plan](TREASURY_MIGRATION_PLAN.md)** | Proposed Mainnet treasury governance, testnet miner compensation, liquidity reserves, and soak criteria |
| **[Open Testnet Guide](docs/TESTNET.md)** | Comprehensive testnet guide: syncing, mining, CLI usage, multi-node setups, and troubleshooting |
| **[CPU Mining Guide](docs/MINING_GUIDE.md)** | Solo mining with `armminer`, thread tuning, memory footprints, and Stratum pool integration |
| **[Node Operations Guide](docs/NODE_OPERATIONS.md)** | Running full nodes, seed nodes, systemd deployment, TLS 1.3, and TOFU certificate security |
| **[Consensus & Network Specification](docs/CONSENSUS_AND_NETWORK.md)** | Detailed mathematical specifications for Armax-ASERT-AW, LWMA-1, ArmaxHash v2, ML-DSA-65, and reorg limits |
| **[Build Guide & Reproducible Binaries](docs/BUILD.md)** | Compiling from source across Linux, Windows, and macOS ARM64 with reproducible build instructions |
| **[Release Engineering & QA Checklist](docs/RELEASE.md)** | Versioning standards, reproducible builds, SHA-256 verification, and pre-tagging QA checklist |
| **[Mainnet Readiness Gate](docs/MAINNET_READINESS.md)** | Security evaluation, code freeze requirements, and mainnet unlock criteria |
| **[Security Policy & Disclosures](SECURITY.md)** | Threat model, responsible disclosure procedures, and known limitations |
| **[Architecture Documentation](docs/architecture/README.md)** | Taxonomy of architectural designs, consensus proposals, and implementation boundaries |
| **[Contribution Guidelines](CONTRIBUTING.md)** | Contributor workflows, coding standards, Conventional Commits, and test requirements |
| **[Code of Conduct](CODE_OF_CONDUCT.md)** | Community standards and enforcement guidelines |
| **[Contributors](CONTRIBUTORS.md)** | List of project contributors recognized across Git history |
| **[Development Support](SUPPORT.md)** | Voluntary contribution addresses and developer contact information |

---

## Core Network & Consensus Parameters

| Parameter | Testnet (`ARMX-TESTNET-1`) | Mainnet (`ARMX-MAINNET-1` - Locked) |
|---|---|---|
| **Launch Status** | **Active Public Testnet** | **Locked** (`core.MainnetLaunched = false`) |
| **Target Block Time** | 120 seconds (2 minutes) | 120 seconds (2 minutes) |
| **Difficulty Adjustment** | Armax-ASERT-AW (Block #13,335+; $\tau = 3600\text{s}$, 4h Surge Cap, TEDR Boost) | Double-SHA256 LWMA-1 ($N=60$) default |
| **Proof-of-Work Algorithm** | ArmaxHash v2 (BLAKE2b Stream-Fast, $H \ge 2500$); Historical: v1 & SHA-256d | Double-SHA256 (`SHA-256d`) baseline |
| **Digital Signatures** | NIST FIPS 204 ML-DSA-65 (Quantum-Safe) | NIST FIPS 204 ML-DSA-65 (Quantum-Safe) |
| **Address Encoding** | BIP-173 Bech32 (`armx1...`, 63 characters) | BIP-173 Bech32 (`armx1...`, 63 characters) |
| **Initial Block Subsidy** | 60 tARMX (coinbase at index 0) | 60 ARMX (coinbase at index 0) |
| **Halving Interval** | Every 1,050,000 blocks (~4 years) | Every 1,050,000 blocks (~4 years) |
| **Maximum Supply Cap** | 126,000,000 tARMX | 126,000,000 ARMX |
| **Coinbase Maturity** | **10 confirmations** | **100 confirmations** |
| **Max Reorg Depth** | 720 blocks (`MaxReorganizationDepth`) | 720 blocks (`MaxReorganizationDepth`) |
| **Block Size / Tx Limit** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB, 1,000 txs/block | MaxBlockSize = 2 MB, MaxTxSize = 20 KB, 1,000 txs/block |
| **P2P Gossip Port** | `19335` (TCP, TLS 1.3 with TOFU) | `19334` (TCP, TLS 1.3 with TOFU) |
| **JSON-RPC Port** | `127.0.0.1:19333` (Strictly Loopback) | `127.0.0.1:19332` (Strictly Loopback) |
| **Public Seed Nodes** | `testnet-seed1.armax-coin.cloud:19335`<br>`testnet-seed2.armax-coin.cloud:19335`<br>`testnet-seed3.armax-coin.cloud:19335` | TBA prior to mainnet launch |

---

## Quickstart: Running on Public Testnet

All binaries default to the active testnet profile (`ARMX-TESTNET-1`).

### 1. Download & Verify Release Artifacts

Pre-compiled binaries and signatures are published on [GitHub Releases](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.10):

```bash
# On Linux / ARM64:
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.10/armax-v1.0.10-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.10/SHA256SUMS
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.10/SHA256SUMS.asc
curl -LO https://raw.githubusercontent.com/acxxcxx/armax-coin/main/ARMAX-PGP-KEY.asc

# 1. Import official release key & verify fingerprint (C857E67F3FC63BE35125C3D32ACE863ED4BF1460):
gpg --import ARMAX-PGP-KEY.asc
gpg --fingerprint dev@armax-coin.cloud

# 2. Verify detached signature on SHA256SUMS:
gpg --verify SHA256SUMS.asc SHA256SUMS

# 3. Verify archive checksum:
sha256sum --check --ignore-missing SHA256SUMS

# Extract archive:
tar -xzf armax-v1.0.10-linux-amd64.tar.gz
chmod +x armcoind armminer armsend armwallet armgui
```

On Windows (PowerShell):
```powershell
# Verify SHA-256 checksum:
Get-FileHash -Algorithm SHA256 .\armax-v1.0.10-windows-amd64.zip
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.10-windows-amd64.zip"
```

### 2. Launch the Full Node Daemon

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
# Option A: Standalone multi-threaded CPU miner (armminer):
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4

# Option B: Internal daemon background mining:
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
