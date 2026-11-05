# Armaxcoin v1.0.7 Release Notes

**Target Release:** Armaxcoin v1.0.7 — Testnet Consensus Fork & Optimization Release  
**Status:** IN DEVELOPMENT (`core.Version = "1.0.7"`). Uncommitted, untagged.  
**Network:** `ARMX-TESTNET-1` (Public Testnet only). Mainnet is not launched (`core.MainnetLaunched = false`).  
**Detailed Technical Specification:** [docs/releases/v1.0.7.md](docs/releases/v1.0.7.md)

---

## 1. Release Overview & Objective

Armaxcoin v1.0.7 is a targeted consensus fork and stability release for the public testnet (`ARMX-TESTNET-1`). The release delivers four core objectives:

1. **Tightens Block Timestamp Rules (Proposal C6):** Enforces a maximum 10-minute future drift ceiling against the validator's local clock starting at block **20,000**, protecting difficulty adjustment from forward-timestamp manipulation.
2. **Sync & Storage Optimization:** Eliminates $O(N^2)$ disk write bottlenecks with incremental persistence, introduces optional BoltDB record compression, and optimizes initial block download (IBD).
3. **P2P & Transport Hardening:** Adds `PUSH_BLOCKS` for NAT-traversing branch propagation, negotiated DEFLATE wire compression, inbound slowloris connection timeouts, persistent ban storage, and SPKI certificate fingerprint pinning.
4. **Subsystem Security & Concurrency Safety:** Mitigates Web GUI DNS rebinding/CSRF vulnerabilities, adds masked CLI mnemonic input, eliminates `undoHistory` map data races during reorgs, and fixes circulating supply calculations across storage replays and reorganizations.

---

## 2. Scheduled Consensus Hard Fork (Testnet Block 20,000)

### Consensus Rule Change (Proposal C6)
- **Activation Height:** **Block 20,000** on network `ARMX-TESTNET-1`.
- **Validation Rule:** From block 20,000 onward, candidate block timestamps must not exceed **600 seconds (10 minutes)** ahead of the validating node's local clock (`FutureTimestampLimitActivationHeight = 20000`, `FutureTimestampLimitSeconds = 600`).
- **Legacy Era (Blocks 0 .. 19,999):** The legacy 7,200-second (2-hour) future timestamp limit remains active, ensuring 100% historical chain validity.
- **Historical Replay:** Stored blocks skip wall-clock validation during database replay and re-indexing.
- **Mainnet / Soak / Regtest Invariance:** Mainnet, soak, and regtest profiles have activation heights set to `-1` (disabled).

### Consensus Proposals Status Summary
- **C1 & C2 (MTP-55 & Monotonicity):** Active since Block 8,000 (v1.0.5).
- **C6 (10-Minute Future Timestamp Ceiling):** Active at **Block 20,000** (v1.0.7).
- **C3 (ML-DSA-65 Cryptographic Audit):** Completed in `core/c3_audit_test.go` (audit verification only; no consensus rule change required).
- **C4 (Transaction Time-To-Live / TTL) & C5 (Reorg Depth Policy):** Unimplemented design drafts; not active in code and not part of the block 20,000 fork.

### Operator & Miner Requirements
1. **[OPERATOR ACTION REQUIRED] Mandatory Upgrade Before Block 20,000:** All nodes, validators, and miners must upgrade to v1.0.7 before the chain tip reaches height 20,000.
2. **[OPERATOR ACTION REQUIRED] NTP Synchronization:** Maintain strict NTP time synchronization across all node, miner, and explorer hosts (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows).
3. **Chain-Split Hazard:** After block 20,000, un-upgraded v1.0.6 nodes will accept blocks stamped up to 2 hours ahead that v1.0.7 nodes reject, resulting in an unrecoverable chain split.

---

## 3. Protocol Invariants (Unchanged Consensus Parameters)

All underlying cryptographic, tokenomic, and Proof-of-Work parameters remain identical and inviolate:

- **Genesis Block:** Hash (`0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b`), Merkle root (`808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d`), timestamp (1710000001).
- **Network ID:** `ARMX-TESTNET-1`.
- **Proof-of-Work Schedule:** Double-SHA256 for $H < 520$, ArmaxHash v1 for $520 \le H < 2500$, ArmaxHash v2 Stream-Fast for $H \ge 2500$.
- **Target Block Cadence:** 120 seconds (2 minutes).
- **Difficulty Adjustment:** LWMA-1 ($N=60$ sliding window).
- **Historical Timestamp Rules:** MTP-11 for $H < 8000$, MTP-55 with non-decreasing parent timestamps for $H \ge 8000$.
- **Block Subsidy Schedule:** Initial reward of 60 tARMX, halving every 1,050,000 blocks.
- **Maximum Supply:** Hard ceiling of 126,000,000 ARMX ($10^8$ atomic Armoids).
- **Signatures:** NIST FIPS 204 (ML-DSA-65) post-quantum lattice signatures.
- **Max Reorg Depth:** 720 blocks (~24 hours at target cadence).

---

## 4. Key Improvements by Subsystem

### Consensus & Mining
- **Block 20,000 Timestamp Fork:** Implemented in `core/timestamp_rules.go` and `core/network.go`.
- **Miner Clock Skew Defense (`miner/miner.go`):** Aligns candidate timestamp to node template time when machine clock leads by > 60s (`ClockSkewWarnSeconds`); automatically pauses (up to 30s) instead of hashing candidate blocks that risk exceeding the future limit.
- **Immutable Activation Heights (`HIGH-02`):** PoW activation height environment overrides (`ARMX_ARMAXHASH_HEIGHT`, `ARMX_ARMAXHASH_V2_HEIGHT`) are restricted strictly to `regtest`, completely ignored on `testnet` and `mainnet`.
- **Stratum Float Guard (`HIGH-05`):** Protects Stratum `set_difficulty` RPC against `Inf` and `NaN` floats before integer conversion.

### P2P Networking & Transport
- **`PUSH_BLOCKS` (`node/fork_push.go`):** Enables non-dialable nodes behind NAT or firewalls to push competing block branches over existing outbound connections.
- **Multi-Chunk Staging Buffer (`HIGH-07`):** Staged in-memory session buffer (`pushBufferSession`, 30s TTL) accumulates multi-chunk competitor branches up to `MaxPushBlocks = 1000` until cumulative chainwork exceeds the local tip before triggering reorganization.
- **Wire DEFLATE Compression (`node/p2p_compress.go`):** Negotiated per-request compression for block ranges (`accept: ["deflate"]` / `enc: "deflate"`) with 32 MB inflation safety cap and 10% efficiency threshold. Disable with `ARMX_P2P_COMPRESSION=0`.
- **`PUSH_BLOCKS` Payload Expansion (`HIGH-08`):** Expands compressed blocks in pushed branches before validation.
- **Inbound Handshake Timeout (`node/p2p.go`):** Bounded 5-second timeout on initial message delivery from unauthenticated inbound connections.
- **Persistent Ban Store (`node/ban_store.go`):** Bans persist in `<dataDir>/<network>/bans.json` across node restarts.
- **SPKI Fingerprint Pinning (`node/seed_pins.go`):** Added optional `seed_fingerprints` config map and updated node identity generation (`CRIT-02`) to compute SPKI SHA-256 digests.
- **IBD Linkage Verification (`HIGH-09`):** Validates cryptographic hash continuity (`PrevHash == Blocks[i-1].Hash`) during initial block download chunks, applying 50 ban points and recording peer failures on broken segments.
- **Bounded Address Cache (`HIGH-03`):** Capped address-to-hex cache at 10,000 entries with 20% eviction.

### Blockchain State & Storage
- **Incremental Persistence (`core/storage.go`, `core/store.go`):** `ResolveFork` persists only blocks at or above the common fork point (`saveToFileFrom`), eliminating $O(N^2)$ disk write amplification.
- **Optional BoltDB Record Compression (`core/store_codec.go`, `core/store_rewrite.go`):** Compresses block, undo, and snapshot records with DEFLATE (default off, `-store-compression`, `ARMX_STORE_COMPRESSION=1`). Offline conversion tool: `-store-rewrite plain|compressed`.
- **Circulating Supply Parity (`HIGH-04`):** Fixed supply calculation during disk replay (`loadBlockchainFromPath`), state revalidation (`RebuildState`), live additions, and reorgs to accumulate `BlockMintedSubsidy` (`coinbase.Amount - totalFees`), maintaining exact supply parity even with underclaimed coinbase subsidies.
- **Explorer Compaction:** Added `Storage.Compact()` executing WAL checkpoints and conditional SQLite `VACUUM`.

### Subsystem Security & Reliability
- **Web GUI Authentication (`gui/main_unix.go`, `CRIT-01`):** Enforces loopback `Host` header validation (`127.0.0.1`, `localhost`, `::1`), requires cryptographically secure 32-byte session tokens (`X-Armx-GUI-Token`), and mandates wallet passphrase verification on `/api/send`.
- **Interactive Masked Mnemonic Input (`cli/armwallet/main.go`, `HIGH-06`):** Masked terminal passphrase prompt (`-restore-interactive`) to prevent seed exposure in process listings.
- **`X-Mine-Key` Priority Auth (`node/server.go`, `HIGH-10`):** Prioritizes `X-Mine-Key` matching before Basic Auth fallback, preventing miner IP lockouts.
- **Multisig Custody Separation (`bridge/evm_multisig_writer.go`, `HIGH-11`):** Decoupled threshold attestation signing across independent validator instances.
- **`undoHistory` Concurrency Protection (`HIGH-01`):** Dedicated `undoLock sync.RWMutex` on `core.Blockchain` and isolated record cloning eliminate map data races under parallel validation.

---

## 5. Storage Compatibility & Downgrade Warning

> [!CAUTION]
> **STORAGE COMPATIBILITY WARNING:**
> BoltDB record compression is **disabled by default**. Compressed records created by v1.0.7 **CANNOT be read by v1.0.6**.
> - If record compression is enabled, you **must not** downgrade to v1.0.6 without converting the database first.
> - **Always create a verified backup of `blockchain.db` before conversion.**
> - To safely convert a compressed database back to plain format:
>   ```bash
>   ./armcoind -network testnet -datadir <datadir> -store-rewrite plain
>   ```

---

## 6. Upgrade & Rollback Instructions

### Upgrading a Testnet Node (`armcoind`):
1. Verify the current node height:
   ```bash
   curl -s http://127.0.0.1:19333/status | jq .
   ```
2. Verify NTP clock synchronization:
   ```bash
   # Linux
   sudo timedatectl set-ntp true
   # Windows
   w32tm /resync
   ```
3. Stop the node daemon gracefully:
   ```bash
   kill -SIGINT <armcoind-pid>
   # or
   sudo systemctl stop armcoind-testnet
   ```
4. Back up `blockchain.db`:
   ```bash
   cp ~/.armax/testnet/blockchain.db ~/.armax/testnet/blockchain.db.pre-v107.bak
   ```
5. Replace binary with v1.0.7 and restart:
   ```bash
   ./armcoind -network testnet
   ```
6. Verify version output:
   ```bash
   curl -s http://127.0.0.1:19333/status | jq .version
   # Output must be: "1.0.7"
   ```

### Miner Upgrade (`armminer`):
- Update `armminer` to v1.0.7 on all mining hosts.
- Pool/Stratum operators building candidate headers must ensure their server clocks are synchronized with NTP.

### Emergency Rollback:
- **If compression was NOT enabled:** Replace binary with v1.0.6 and restart. Note that after block 20,000, v1.0.6 nodes risk following a split chain.
- **If compression WAS enabled:** Stop daemon, back up database, run `./armcoind -network testnet -store-rewrite plain`, then start v1.0.6.

---

## 7. Verification Status & Known Limitations

- **[VERIFIED] Automated Test Suite:** 100% pass rate (`go test -tags testhooks ./...` across all 15 packages).
- **[VERIFIED] Race Detection:** Core concurrency protections verified with `go test -race` (`TestUndoHistoryConcurrentReadWriteRace`).
- **[VERIFIED] Smart Contracts:** 100% pass rate (`npm test` in `contracts/`, 11/11 test suites passing).
- **[VERIFIED] Multi-Process Simulation:** Local multi-process regtest validated fork rehearsals across height boundaries, PUSH_BLOCKS reorg adoption, and skewed miner rejection.
- **[NOT VERIFIED] Live WAN / Multi-VPS Staging:** Rehearsal across live geographically separated VPS nodes and WAN network conditions has not yet been executed.
- **[KNOWN LIMITATION] Clock Non-Determinism:** The future-timestamp check relies on local wall-clock time; nodes with inaccurate clocks may temporarily reject valid blocks.
- **[KNOWN LIMITATION] Bridge Separation:** The cross-chain EVM bridge is an independent development workstream and is not included in the Layer-1 core release.

