# Armaxcoin Architecture Documentation

This directory provides the authoritative reference structure for the engineering architecture of Armaxcoin (`armax-coin`). It establishes clear boundaries between active software implementations, completed release documentation, verified testnet baselines, historical architectural proposals, and future Mainnet designs.

---

## 1. Architectural Taxonomy

To prevent conflation between speculative design iterations and production-ready software, all engineering materials are classified into distinct tiers:

### Tier 1: Active Software Implementation
- **Scope:** The Go codebase actively running on full nodes, miners, wallets, and tools in this repository.
- **Source Version Metadata:** `v1.0.10` (Official Testnet Release, `core.Version = "1.0.10"`).
- **Core Principle:** Safety, reliability, memory bounds, clean shutdown, post-quantum cryptography, consensus fork coordination, and storage efficiency.

### Tier 2: Existing Testnet Compatibility Baseline
- **Network ID:** `ARMX-TESTNET-1` (Display ticker: `tARMX`).
- **Proof-of-Work:** Dual-pass SHA-256d (H < 520), ArmaxHash v1 (520 <= H < 2500), ArmaxHash v2 Stream-Fast (H >= 2500).
- **Target Block Time:** 120 seconds.
- **Difficulty Adjustment:** Armax-ASERT-AW (canonical pure integer `aserti3-2d` active from Block #13,335; previously TEDR anti-stall at #13,293 and legacy LWMA-1 $N=60$).
- **Block Reward Schedule:** Initial subsidy of 60 tARMX, halving every 1,050,000 blocks, total supply cap 126,000,000 ARMX, atomic base unit $10^8$ Armoids.
- **Timestamp Rules:** MTP-11 below block 8,000; MTP-55 with non-decreasing parent timestamps from block 8,000 onward. From block 20,000, the maximum allowed future-timestamp limit narrows from 7,200 seconds (2 hours) to 600 seconds (10 minutes). Under v1.0.10, minor skew ($\le 30\text{s}$) incurs 0 peer score penalty, with ban scoring for $> 600\text{s}$.
- **Persistent Storage:** Key-value storage in BoltDB (`bbolt`) with standard bucket hierarchy (`blocks`, `hashindex`, `txindex`, `chainwork`, `snapshots`, `undo`, `meta`). Optional record compression supported in v1.0.7+ (default off).
- **Signatures:** NIST FIPS 204 post-quantum ML-DSA-65.
- **Status:** **Immutable.** Any change to these rules forks or partitions running Testnet nodes and is strictly prohibited without explicit network coordination.

### Tier 3: Future Mainnet Architecture Work
- **Scope:** Proposed architectures specifically targeted for the independent Mainnet genesis (`ARMX-MAINNET-1`), including:
  - ArmaxHash v2 (domain-tagged BLAKE2b-512 into BLAKE2b-256).
  - Mainnet-specific genesis block vectors and mining seeds.
  - Dedicated network ports (Mainnet RPC: 19332, P2P: 19334, Stratum: 19338).
- **Status:** **Deferred / Experimental.** Mainnet code remains locked in source (`core.MainnetLaunched = false`). All Mainnet experimentation must run in isolated local environments with distinct data directories and ports.

### Tier 4: Archived Historical Plans & Bridge Designs
- **Scope:** Exploratory implementation plans (v1.0.27 through v1.0.45), bridge relayer specifications, and internal coordination logs.
- **Status:** **Archived.** All developmental bridge specifications and legacy planning iterations are securely archived in the private repository (`armax_coin_private_archive`).

---

## 2. Current Implementation Boundary

```text
+-------------------------------------------------------------------------+
|                  ARMAXCOIN ARCHITECTURAL BOUNDARY                       |
+-------------------------------------------------------------------------+
| [ACTIVE PROTOCOL IMPLEMENTATION - v1.0.10 CONSENSUS BASELINE]           |
| - Armax-ASERT-AW pure integer DAA at block 13,335 (tau=3600s, anchor #13,334)|
| - 4-Hour Surge Cap (14,400s) & TEDR Boost (7,200s drift injection)       |
| - Testnet Anti-Stall TEDR emergency fallback at block 13,293             |
| - Scheduled future timestamp 10-minute ceiling (Proposal C6 at #20,000)  |
| - Immutable thread-safe MiningJob engine & 4-point submission gate       |
| - Dual-model difficulty telemetry (work units and difficulty ratio)      |
| - 3-tier P2P clock skew policy (<=30s free, strike tracking, >600s ban)  |
| - P2P sync hardening with 2-hour unresponsive peer eviction             |
| - NIST FIPS 204 ML-DSA-65 post-quantum digital signatures                |
| - P2P transport TLS 1.3 with TOFU certificate pinning                    |
+-------------------------------------------------------------------------+
                                    |
                                    | STRICT SEPARATION
                                    v
+-------------------------------------------------------------------------+
| [DEFERRED ARCHITECTURE / EXPERIMENTAL WORK]                             |
| - Mainnet launch (remains locked: core.MainnetLaunched = false)         |
| - Bridge activation (remains IN DEVELOPMENT - NOT PRODUCTION READY)     |
| - Draft proposals C4 (Tx TTL) and C5 (Reorg depth limit)                |
|   (Note: C3 was audited without fork; C6 activates at Block 20,000)     |
+-------------------------------------------------------------------------+
```

---

## 3. Directory Navigation

- **[`../../TREASURY_MIGRATION_PLAN.md`](../../TREASURY_MIGRATION_PLAN.md):** Canonical 1,000,000 ARMX treasury, miner migration, and liquidity framework.
- **[`tokenomics/mining-accumulation-plan.md`](tokenomics/mining-accumulation-plan.md):** Developer early-mining operational policy (blocks 1..16,667).
- **[`consensus/mainnet-pow-activation.md`](consensus/mainnet-pow-activation.md):** Proposal and analysis for future Mainnet PoW activation.
- **[`../CONSENSUS_AND_NETWORK.md`](../CONSENSUS_AND_NETWORK.md):** Canonical Layer-1 mathematical consensus specification.
- **[`../NODE_OPERATIONS.md`](../NODE_OPERATIONS.md):** Operational guidance for running Testnet nodes.
- **[`../releases/v1.0.10.md`](../releases/v1.0.10.md):** Release notes for Core v1.0.10.
- **[`../releases/v1.0.9.md`](../releases/v1.0.9.md):** Release notes for Core v1.0.9.
- **[`../releases/v1.0.8.md`](../releases/v1.0.8.md):** Release notes for Core v1.0.8.
- **[`../releases/v1.0.7.md`](../releases/v1.0.7.md):** Release notes for Core v1.0.7.
- **[`../releases/v1.0.6.md`](../releases/v1.0.6.md):** Release notes for Core v1.0.6.
