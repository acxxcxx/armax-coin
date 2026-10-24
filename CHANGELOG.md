# Changelog

All notable changes to the **Armaxcoin** (`armax-coin`) project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.0.3] - 2026-10-24

### Added
- **ArmaxHash v2 Stream-Fast PoW Consensus (Hard Fork at Block #2500):**
  - Activated high-efficiency two-stage Chained BLAKE2b Proof-of-Work:
    - Stage 1 (Prehash): `BLAKE2b-512("ARMX-BLAKE2B-V2:" || NetworkID || ":" || Header)`
    - Stage 2 (BlockHash): `BLAKE2b-256(Prehash || Header)`
  - Achieves zero heap allocations (`0 B/op`) in tight worker loops, eliminating DRAM memory bus saturation on multi-core PCs, ARM single-board computers (Raspberry Pi/Orange Pi), and mobile devices.
  - Sub-millisecond block verification enabling fast, low-latency block propagation across global peer meshes.
  - 100% backward verification compatibility preserving historical block eras (Blocks 0..519 on Double-SHA256, Blocks 520..2499 on ArmaxHash v1 Argon2id).
- **Consensus Improvement Proposals (C1-C5):**
  - Published formal consensus enhancement specifications (`docs/security/consensus-proposals.md`) addressing reorg finality boundaries, timestamp clamping, and mempool DoS mitigation.
- **Mainnet Tokenomics & Vesting Architecture Proposal:**
  - Added comprehensive tokenomics audit and proposal (`docs/TOKENOMICS_MIGRATION_PROPOSAL.md`) detailing the 126,000,000 ARMX hard cap preservation, block-height-indexed vesting schedules, and 10:1 testnet migration formula.

### Fixed
- **Storage Replay Future Wall-Clock Check [T13]:** Bypassed future drift wall-clock check during historical storage replay and node re-indexing so valid past blocks are never rejected during initial block download.
- **Production Database Fail-Fast Guard [T12]:** Prevented silent fallback to in-memory database on database open failures; node terminates immediately to protect against state loss.
- **Keystore Overwrite Protection & Auto-Backup [T15]:** Hardened CLI (`armwallet`) and GUI against accidental keystore overwrites by automatically creating timestamped `.bak` files when new keys are generated.
- **HTTP RPC Error Schema Standardization:** Standardized `writeJSONError` across node endpoints to consistently return `error`, `message`, and `status` fields for robust GUI, mobile, and explorer client interoperability.

### Changed
- **Mining Profile Transparency:** Refined documentation and whitepaper to remove unprovable "ASIC-resistant" marketing claims in favor of accurate, scientifically sound "accessible commodity CPU mining profile".
- **Documentation Alignment:** Synchronized documentation across all guides to reflect active ArmaxHash v2 consensus while maintaining full historical records of v1.

---

## [v1.0.1] - 2026-10-23

### Added
- **BIP-35 Mempool Synchronization Protocol:** Implemented P2P `GET_MEMPOOL` command and `MEMPOOL_RESPONSE` handler allowing nodes, newly connected peers, and miners to instantly synchronize unconfirmed transactions upon connection.
- **NAT & Private Subnet Outbound Sync:** Supported outbound synchronization for nodes operating behind NAT or dynamic IPs without public listening ports, enabling full block/mempool catch-up and block submission over established outbound TCP sessions.
- **Periodic Mempool Rebroadcast:** Added background worker loop periodically broadcasting local mempool transactions to active peers to prevent transaction starvation.
- **Rate-Limiting & Memory Guards:** Bounded `GET_MEMPOOL` requests per peer (10 requests/minute) and enforced size limits (max 1,000 transactions per response) to eliminate mempool flooding amplification vectors.

### Fixed
- **Mempool Transaction Propagation:** Resolved transaction isolation where unmined transactions submitted to non-mining daemons were not relayed to miners that joined the network later.
- **Unit Test Coinbase Maturity:** Fixed test mocks in P2P sync suites to explicitly account for coinbase maturity constraints during mock generation.

---

## [v1.0.0] - 2026-10-22

### Added
- **Post-Quantum Cryptography:** Full implementation and consensus enforcement of NIST FIPS 204 ML-DSA-65 digital signatures for all transactions and addresses via Go standard library `crypto/mldsa`.
- **ArmaxHash Proof-of-Work:** Memory-hard CPU-oriented PoW algorithm chaining Argon2id (2 MB scratchpad, $t=1, p=1$), Blake2b-512, and SHA-256, activated at block #520 on testnet.
- **Dynamic Difficulty Retargeting:** Real-time Linear Weighted Moving Average (LWMA-1) algorithm with $N=60$ sliding window and 120-second target block time.
- **P2P Transport Security:** Mandatory TLS 1.3 encryption across all peer-to-peer gossip connections with Trust On First Use (TOFU) certificate fingerprint pinning in `peers.json`.
- **Concurrency & Concurrency Hardening:** Lockless chainwork queries (`GetTotalChainworkUnsafe`), deadlock elimination in P2P sync loops, and batch commit optimizations during initial block download.
- **Multi-Node Chaos Testing:** Automated 5-node network soak and chaos test suite (`TestSoak_FiveNodesChaos`) validating network partition healing, reorg resolution, and ML-DSA-65 transaction throughput.
- **Cross-Platform Tooling:**
  - `armcoind`: Core node daemon and P2P validator.
  - `armminer`: Multi-threaded CPU mining engine with thread auto-tuning.
  - `armwallet`: CLI wallet supporting 24-word BIP-39 recovery mnemonics and Argon2id encrypted keystores.
  - `armsend`: Lightweight offline client transaction generator and sender.
  - `armgui`: Windows 64-bit desktop graphical wallet with embedded manifest and icons.
- **Open Testnet Documentation:** Complete English documentation suite including Testnet Guide, Mining Guide, Node Operations Guide, Consensus Specifications, Contributor Reward Proposal, and Release Checklists.

### Changed
- **Repository Migration:** Standardized canonical project repository and module path to `github.com/acxxcxx/armax-coin`.
- **RPC Daemon Security:** Enforced strict loopback binding (`127.0.0.1:19333` on testnet) and ephemeral cookie authentication (`armax.cookie`).
- **Release Automation:** Hardened GitHub Actions release workflow with strict SemVer enforcement, automated test gates, `gitleaks` secret audits, and reproducible stripped cross-compilation.

### Deprecated
- Legacy SHA-256d mining on testnet deprecated from block #520 in favor of ArmaxHash.

### Security
- Eliminated all default passphrases and development credentials.
- Bounded maximum reorganization depth (`MaxReorganizationDepth = 720` blocks) to prevent deep historical 51% reorganization attacks.
- Implemented automated repository hygiene tests (`TestNoPersonalDataInTree`) guaranteeing zero personal file paths, emails, or credentials in tracked git trees.
