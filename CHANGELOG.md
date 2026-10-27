# Changelog

All notable changes to the **Armaxcoin** (`armax-coin`) project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.0.5] - 2026-10-27

> **Note:** `v1.0.2` was never released or tagged. The release sequence is `v1.0.0`, `v1.0.1`, `v1.0.3`, `v1.0.4`, `v1.0.5`.

### Version
- Bumped the protocol/software version to `1.0.5` (`core/consensus.go`, GUI footer, Windows manifest, `scripts/build-release.ps1`). `scripts/verify-release.sh` now takes the expected version as an optional second argument (default `v1.0.5`) and strips the surrounding quotes from the printed recovery phrase, which previously made its mnemonic-restore step fail.
- No consensus rule, genesis, NetworkID, or hashing change. Consensus proposals C1–C5 remain design-only; the proposal document now states this explicitly, uses activation height 6,500 for C1/C2, and lists C5 as local policy.

### Security
- Pinned `toolchain go1.27.3` in `go.mod` to remediate `GO-2026-6809` (`crypto/x509`) reported by `govulncheck`.

### Changed
- Added `core.OpenBlockchain` and `node.RunServerWithConfig`, which return errors instead of panicking. `armcoind` and the GUI now exit with a clear message on database or startup failures. `core.NewBlockchain` and `node.StartServerWithConfig` keep their panicking behavior for existing callers and tests.
- Clarified why the P2P client TLS config skips chain verification (peer authentication is done by TOFU pinning).
- `go mod tidy` now lists direct imports as direct requirements.
- Documentation: Go 1.27.3+ guidance, mainnet-readiness gate status table, and cross-references between overlapping testnet/release guides.
- Testnet display ticker is now `tARMX` in the daemon, miner, and GUI banners and messages (via `core.ActiveTicker()`). This is display-only: NetworkID, genesis, hashing, serialization, and consensus rules are unchanged, so no hard fork is required and existing testnet blocks remain valid. `ARMX` remains the mainnet ticker.
- The miner's fee line in the daemon self-test now shows base units (`Armoids`) instead of `ARMX`, because the value is in Armoids.

### CLI
- `armwallet` commands exit non-zero (1) on errors; previously failures printed `[-]` and exited 0.
- `armsend -json` prints fatal errors as `{"status":"error","error":"..."}` on stdout.
- Deprecation and note warnings (`[!]`) go to stderr, so stdout stays clean for scripts. Progress lines use `[~]` consistently.
- Legacy aliases still work but print a deprecation notice naming the replacement (`armwallet`: `-newaddress`, `-getbalance`, `-getmempool`, `-getmempoolinfo`, `-wallet`; `armsend`: `-amount-armx`, `-getmempoolinfo`, `-file`).
- `-h` and `--help` show grouped usage, environment variables, and examples, and exit 0.

### Tests
- Added in-process tests for the miner RPC helpers (`fetchBlockTemplate`, `submitSolvedBlock`), `armwallet` balance rendering, and `RunServerWithConfig` error handling.

## [v1.0.4] - 2026-10-27

### Added
- **Structured Categorized Logging Subsystem (`core/logger.go`):**
  - Implemented categorized logging (`net`, `sync`, `mempool`, `validation`, `rpc`, `miner`) with configurable log levels (`DEBUG`, `INFO`, `WARN`, `ERROR`).
  - Added rolling `debug.log` persistence with automatic size-based rotation and file-backed error diagnostic capture.
- **Node CLI Diagnostic Suite:**
  - Added CLI diagnostic flags to the daemon binary: `-getblockchaininfo`, `-getnetworkinfo`, `-getpeerinfo`, `-getmempoolinfo`, and `-checkhealth`.
  - Supports live RPC query with offline database fallback inspection when the daemon process is stopped.

### Fixed
- **Transaction Pre-Validation Before Signature Cache Insertion:**
  - Hardened `core/transaction.go` to strictly validate transaction fields (amounts, nonces, addresses, and bounds) prior to querying or writing to the signature verification cache, preventing cache poisoning vectors.
- **Competing Fork Discovery & Automatic Fork Recovery (`core/consensus.go`, `node/p2p.go`):**
  - Resolved chain stalling when competing fork blocks are received at equal or lower heights by triggering automated fork synchronization and reorganization evaluations.
- **P2P Rate-Limiter Tuning for Continuous IBD:**
  - Stabilized peer connection throughput under heavy network synchronization, ensuring reliable Initial Block Download without false-positive peer throttling.

### Changed
- **Documentation Parity & Accuracy Alignment:**
  - Aligned all documentation across `README.md`, `WHITEPAPER.md`, `docs/CONSENSUS_AND_NETWORK.md`, `docs/MINING_GUIDE.md`, and `docs/security/consensus-proposals.md` to reflect active v1.0.4 specifications and consensus proposal statuses.

---

## [v1.0.3] - 2026-10-24

### Added
- **ArmaxHash v2 Stream-Fast PoW Consensus (Hard Fork at Block #2500):**
  - Activated high-efficiency two-stage Chained BLAKE2b Proof-of-Work:
    - Stage 1: `D1 = BLAKE2b-512("ARMX-POW-V2\0" || 0x02 || byte(len(NetworkID)) || NetworkID || Header)`
    - Stage 2: `BlockHash = BLAKE2b-256(D1)`
  - The fixed-header `HashV2` benchmark reports 0 allocations/op. This does not establish zero allocations or a fixed verification time for the full block/miner path.
  - 100% backward verification compatibility preserving historical block eras (Blocks 0..519 on Double-SHA256, Blocks 520..2499 on ArmaxHash v1 Argon2id).
- **Consensus Improvement Proposals (C1-C5):**
  - Published formal consensus enhancement specifications (`docs/security/consensus-proposals.md`) addressing reorg finality boundaries, timestamp clamping, and mempool DoS mitigation.

### Fixed
- **Storage Replay Future Wall-Clock Check [T13]:** Bypassed future drift wall-clock check during historical storage replay and node re-indexing so valid past blocks are never rejected during initial block download.
- **Bitcoin/Litecoin Standard Token-Bucket Rate Limiter & Continuous IBD Sync:** Replaced rigid 5-req/min rolling window for `GET_BLOCK_RANGE` and `GET_FORKPOINT` with a token-bucket rate limiter (Burst: 120 chunks / 30,000 blocks, Refill: 20 chunks/s), eliminated false-positive ban score penalization during block history synchronization, and enabled pipelined continuous Initial Block Download with automatic multi-peer failover.
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
- **ArmaxHash v1 Proof-of-Work:** Testnet blocks from height 520 use a network/header-derived BLAKE2b-512 prehash, Argon2id (2048 KiB, $t=2$, $p=1$), and BLAKE2b-256 finalization (see `docs/CONSENSUS_AND_NETWORK.md`).
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
- **Open Testnet Documentation:** Complete English documentation suite including Testnet Guide, Mining Guide, Node Operations Guide, Consensus Specifications, Contributor Recognition, and Release Checklists.

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
