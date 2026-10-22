# Changelog

All notable changes to the **Armaxcoin** (`armax-coin`) project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
