# Armaxcoin Documentation

This documentation directory contains official user guides, operator manuals, specifications, and verification runbooks for **Armaxcoin** (`armax-coin`).

## 1. Getting Started & User Guides

- [Project Overview & Quickstart](../README.md): High-level introduction, key features, and architecture.
- [Public Testnet Guide](TESTNET.md): Comprehensive node setup, building from source, wallet management, transactions, and mining.
- [Mining Guide](MINING_GUIDE.md): Solo mining, multi-threaded CPU miner configuration, Stratum pool setup, and LWMA-1 difficulty details.
- [Build and Compilation Guide](BUILD.md): Step-by-step toolchain and cross-compilation instructions.
- [Development Support](../SUPPORT.md): Official developer contact and voluntary contribution channels.

## 2. Technical Specifications & Architecture

- [Consensus, Cryptography & Network Specification](CONSENSUS_AND_NETWORK.md): LWMA-1 difficulty adjustment, NIST FIPS 204 ML-DSA-65 signatures, and ArmaxHash v2 PoW specification.
- [Architecture Overview](architecture/README.md): Engineering boundaries, consensus invariants, and repository taxonomy.
- [Performance & Wire Size Benchmarks](PERF.md): Wire size metrics, post-quantum signature size analysis, and mempool throughput.
- [Security Disclosure Policy](../SECURITY.md): Vulnerability reporting channels, threat boundaries, and audit process.

## 3. Operations & Maintenance

- [Node Operations Guide](NODE_OPERATIONS.md): Daemon configuration flags, JSON-RPC endpoints, `armax.conf`, cookie authentication, and systemd units.
- [Testnet Backup & Reset Runbook](operations/TESTNET_BACKUP_AND_RESET_RUNBOOK.md): Procedures for database backup, disaster recovery, and testing resets.
- [Launch Checklist](LAUNCH_CHECKLIST.md): Pre-flight verification gates and consensus deployment checklist.
- [Release Engineering Guide](RELEASE.md): Versioning standards, reproducible builds, and SHA-256 checksum verification.
- [Release Checklist](RELEASE_CHECKLIST.md): Quality assurance criteria and release gate verification.

## 4. Release History

- [Release Notes Overview (v1.0.10)](../RELEASE_NOTES.md): Top-level summary of the active release and upgrade requirements.
- [v1.0.10 Release Notes](releases/v1.0.10.md): Armax-ASERT-AW DAA Consensus Fork at Block #13,335, Surge Cap & TEDR Boost, Immutable MiningJob & 3-Tier P2P Policy.
- [v1.0.9 Release Notes](releases/v1.0.9.md): Testnet Anti-Stall TEDR Fork at Block #13,293, 5.45x Zero-Alloc CPU Miner & Linux GUI Auto-Start.
- [v1.0.8 Release Notes](releases/v1.0.8.md): Desktop GUI Security, First-Time Onboarding Flow & Peer Privacy Hardening.
- [v1.0.7 Release Notes](releases/v1.0.7.md): Testnet Consensus Fork (Block 20,000), Sync & Storage Optimization, and Subsystem Hardening.
- [v1.0.6 Release Notes](releases/v1.0.6.md): Testnet Core v1.0.6 stability, security hardening, and P2P deduplication.

---

> [!NOTE]
> Internal planning revisions, historical soak test artifacts, and developmental bridge documentation have been preserved in the private engineering archive (`armax_coin_private_archive`) and are decoupled from the public Layer-1 Core distribution.
