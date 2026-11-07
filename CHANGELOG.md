# Changelog

All notable changes to the **Armaxcoin** (`armax-coin`) project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### [v1.0.8] - 2026-11-07

> **Desktop GUI Authentication, First-Time Onboarding & Peer Privacy Hardening.** Resolves Linux/Unix Desktop GUI session authentication failure on wallet creation, implements automated first-time wallet setup and mnemonic restore dialogs when no keystore exists, protects residential peer privacy by filtering ephemeral client ports, and fixes snapshot script flag precedence.

#### Added
- **First-Time Wallet Onboarding Wizard (`gui/main_unix.go`):** Added `#noWalletCard` overview alert and automatic `#setupWalletModal` modal dialog prompting first-time users to create an encrypted keystore (minimum 12-character passphrase) or import an existing 24-word BIP-39 mnemonic phrase (`#restoreWalletModal`).
- **Transparent GUI Session Auth Injection (`gui/main_unix.go`):** Injected active session security token into Web UI HTML (`window.GUI_SESSION_TOKEN = "{{SESSION_TOKEN}}"`) and issued `HttpOnly`, `SameSite=Strict`, `Path=/` session cookie (`armax_gui_token`). Client-side JavaScript automatically attaches `X-Armx-GUI-Token` to all API fetch requests.
- **P2P Peer Privacy Filter (`gui/main.go`, `gui/main_unix.go`):** Added `isSafeP2PDisplayPeer` and `filterSafePeersJSON` helper functions. Restricts displayed peers in active peer lists, `/peers` diagnostics, and `getpeerinfo` CLI outputs to DNS seeds, domain names, and public IPs listening on open P2P ports (`19335`/`19334`), masking home nodes on ephemeral client sockets.
- **Unit Test Coverage for Peer Filtering & Unix GUI (`gui/gui_test.go`, `gui/gui_unix_test.go`):** Added comprehensive test suites verifying ephemeral port filtering, DNS seed preservation, session token validation, and first-time wallet setup flow.

#### Fixed
- **Linux GUI Wallet Creation 401 Unauthorized Error (`gui/main_unix.go`):** Fixed authentication failure where `/api/wallet/create` was rejected with `{"error":"Unauthorized: Invalid or missing GUI session token"}` due to missing client-side credential injection.
- **Undefined Active Address State (`gui/main_unix.go`):** Handled missing keystore state in `/api/wallet/info` (`"address": "", "initialized": false`), preventing DOM elements from evaluating to `undefined`.
- **Public Snapshot Tooling Precedence (`scripts/make_public_snapshot.ps1`, `scripts/make_public_snapshot.sh`):** Prioritized `-SkipGitleaks` / `--skip-gitleaks` checks ahead of local `gitleaks` executable detection, ensuring test runs and automated snapshot tools bypass synthetic test fixtures cleanly.

### [v1.0.7] - 2026-11-05 (in development; `core.Version` is `1.0.7`)

> **Consensus fork at testnet block 20,000.** All nodes and miners must upgrade to v1.0.7 before the tip reaches 20,000. From that height, a block timestamp may be at most **10 minutes (600 seconds)** ahead of the validating node's clock (previously 2 hours / 7,200 seconds). Below 20,000, the 2-hour rule is unchanged, so existing blocks stay valid. Mainnet, soak, and regtest profiles keep the 2-hour rule (activation height `-1`). All consensus invariants (PoW schedule, 120s block target, LWMA-1 $N=60$, halving schedule, and 126,000,000 ARMX supply cap) remain strictly preserved.

#### Added
- **Future-Timestamp Consensus Fork (`core/timestamp_rules.go`, `core/network.go`):** Implemented `FutureTimestampLimitActivationHeight` (Testnet block 20,000) and 600-second future-limit validation via `validateFutureTimestamp` across both block validation paths. Wall-clock validation is skipped during historical replay.
- **Miner Clock Skew Defense & Safety Pausing (`miner/miner.go`):** Added automatic local clock alignment against node template time when clock leads by > 60 seconds (`ClockSkewWarnSeconds`), with rate-limited warnings. Added `futureLimitWait` logic to pause hashing up to 30s when candidate blocks risk exceeding future limits.
- **NAT Chain Synchronization via `PUSH_BLOCKS` (`node/fork_push.go`):** Enables nodes operating behind non-dialable NAT or firewalls to push competitor branch blocks directly to public peers over existing outbound connections.
- **Multi-Chunk Push Staging Session Buffer (`node/fork_push.go`, `HIGH-07`):** Implemented `pushBufferSession` (30-second TTL) per peer to accumulate sequential partial chunks of competitor branches up to `MaxPushBlocks = 1000` until cumulative chainwork exceeds the local tip before triggering reorg execution.
- **Negotiated Wire DEFLATE Compression (`node/p2p_compress.go`, `node/p2p_sync.go`):** Added optional per-request DEFLATE compression in `GET_BLOCK_RANGE` requests (`accept: ["deflate"]`) and responses (`enc: "deflate"`, base64 `z`). Governed by a 32 MB inflation safety cap and minimum 10% bandwidth savings threshold. Can be disabled via `ARMX_P2P_COMPRESSION=0`.
- **Persistent Ban Storage (`node/ban_store.go`):** Saves active peer bans to `<dataDir>/<network>/bans.json` with TTL timestamps, surviving node restarts.
- **Pinned Seed Certificate SPKI Fingerprints (`node/seed_pins.go`):** Added optional `seed_fingerprints` configuration map (`host:port` -> 64-hex SHA-256 SPKI fingerprint) to reject seed connections that do not match pinned cryptographic public keys.
- **Optional BoltDB Record Compression (`core/store_codec.go`, `core/store_rewrite.go`):** DEFLATE compression for block, undo, and snapshot database records (config `store_compression`, flag `-store-compression`, env `ARMX_STORE_COMPRESSION=1`). Includes offline conversion utility `-store-rewrite plain|compressed`. Default is disabled.
- **Interactive Masked Mnemonic Input (`cli/armwallet/main.go`, `HIGH-06`):** Added `-restore-interactive` flag utilizing `golang.org/x/term` masked password input with standard input fallback for automation.
- **Explorer Database Compaction (`armax_explorer/internal/storage/storage.go`):** Added `Storage.Compact()` performing WAL checkpointing and conditional `VACUUM` when >= 20% of SQLite pages are free.
- **Accepted Block Telemetry:** Added `ahead: Ns` field to accepted block log output for operator visibility into forward-stamped blocks.

#### Fixed
- **Incremental Persistence Performance (`core/storage.go`, `core/store.go`):** Replaced whole-chain rewrites in `ResolveFork` with `saveToFileFrom(ancestorHeight)`, eliminating the O(N²) disk persistence bottleneck during sync.
- **Circulating Supply Parity on Disk Replay and Reorgs (`core/storage.go`, `core/blockchain.go`, `core/consensus.go`, `HIGH-04`):** Replaced static theoretical subsidy accumulation with `BlockMintedSubsidy` (`coinbase.Amount - totalFees`) across `loadBlockchainFromPath`, `RebuildState`, `ValidateAndAddBlock`, and `ResolveFork`, ensuring identical mathematical supply calculation between live nodes and restarted nodes even when miners underclaim subsidies.
- **Data Race on `undoHistory` Reorg Map (`core/blockchain.go`, `core/store.go`, `core/consensus.go`, `core/storage.go`, `HIGH-01`):** Added dedicated `undoLock sync.RWMutex` to `core.Blockchain` and implemented deep-copy cloning (`cloneBlockUndoRecord()`), eliminating Go runtime `concurrent map read and map write` panics during concurrent block validation and fork resolution.
- **`PUSH_BLOCKS` DEFLATE Payload Decompression (`node/p2p.go`, `HIGH-08`):** Added payload expansion to unpack compressed block batches in `PUSH_BLOCKS` before forwarding to `handlePushedBlocks`.
- **IBD Cryptographic Hash Continuity & Ban Scoring (`node/p2p.go`, `HIGH-09`):** Added `PrevHash == Blocks[i-1].Hash` checks during initial block download chunk processing; applies immediate 50-point ban scoring and records peer failures upon receiving non-contiguous segments.
- **Inbound Handshake Slowloris Defense (`node/p2p.go`):** Requires unauthenticated inbound connections to deliver their first valid P2P message within 5 seconds (`InboundHandshakeTimeout`).
- **Stratum Infinite/NaN Difficulty Arithmetic Guard (`miner/miner.go`, `HIGH-05`):** Guarded `set_difficulty` RPC against non-finite float inputs before integer conversion, preventing thread crashes.
- **`X-Mine-Key` Priority Auth & False IP Lockout Prevention (`node/server.go`, `HIGH-10`):** Checks `X-Mine-Key` prior to falling back to HTTP Basic Auth and records authentication success to prevent legitimate miners from being banned by the IP lockout tracker.
- **Local Bridge Deployment Fixtures (`contracts/scripts/deploy-local.cjs`, `CRIT-04`):** Updated constructor parameters for `WrappedArmaxcoin.sol` from 4 arguments to 6 arguments, restoring clean local EVM testing.

#### Security
- **Local Web GUI Authentication & Host Validation (`gui/main_unix.go`, `CRIT-01`):** Added loopback `Host` header enforcement (`127.0.0.1`, `localhost`, `::1`), generated random 32-byte hexadecimal session tokens (`X-Armx-GUI-Token`) saved to a 0600 `.session` file, and mandated wallet passphrase verification on `/api/send` to prevent DNS rebinding and cross-origin wallet draining.
- **P2P Node Identity SPKI Fingerprint Alignment (`node/p2p_crypto.go`, `CRIT-02`):** Changed `InitNodeIdentity` fingerprint computation to SHA-256 hash `RawSubjectPublicKeyInfo` (SPKI) instead of the whole X.509 certificate, ensuring alignment with `seed_pins.go` pinning format.
- **Immutable Consensus Height Overrides (`core/network.go`, `HIGH-02`):** Restricted `ARMX_ARMAXHASH_HEIGHT` and `ARMX_ARMAXHASH_V2_HEIGHT` environment variable overrides strictly to `regtest`. Overrides are completely ignored on `mainnet` and `testnet`.
- **Bounded Core Address-to-Hex Cache (`core/address.go`, `HIGH-03`):** Replaced unbounded `sync.Map` with a 10,000-entry capacity-capped, mutex-protected cache with 20% eviction to eliminate heap exhaustion risks under address flooding.
- **Bridge Multisig Key Custody Separation (`bridge/evm_multisig_writer.go`, `HIGH-11`):** Designed `RemoteValidatorClient` and `MultisigAggregator` interfaces for threshold attestation collection across separate validator hosts; documented that `LocalEVMWriter` is strictly a local test harness.

#### Compatibility & Upgrade Notices
- **Storage Downgrade Warning:** Compressed BoltDB records are **not readable by v1.0.6**. Before downgrading a v1.0.7 node to v1.0.6, operators must stop the daemon, back up `blockchain.db`, and run `./armcoind -network testnet -store-rewrite plain`.
- **Wire Compatibility:** Older v1.0.6 nodes ignore the new `PUSH_BLOCKS` message and `accept: ["deflate"]` field, answering with uncompressed block ranges. However, after block 20,000, un-upgraded v1.0.6 nodes will accept blocks that v1.0.7 nodes reject if timestamps exceed 10 minutes, causing a network fork. All testnet operators must upgrade before height 20,000.

## [v1.0.6] - Stabilization in Progress

> **Release Status:** `v1.0.6` (Active Testnet Maintenance Release; Tagged: `v1.0.6`).
> Core node test suites, race detection, and binary compilations are validated under local CI pipelines. Maintenance release for active Testnet; no hard fork, no soft fork, no consensus alterations. Existing genesis, PoW, chainwork, and block timestamps remain unchanged.

### Added
- **Additive RPC Response Headers & Telemetry:**
  - Added additive `X-Network-ID` HTTP response header to mining endpoints (`/getmininginfo`, `/getblocktemplate`).
  - Added `peers_seeds` and `peers_candidates` telemetry fields to `/stats` and `/status` RPC endpoints.
  - Added `/peers?filter=active` query parameter to return strictly active verified P2P sessions.
- **Node Diagnostic Subsystem:**
  - Added node diagnostic CLI flags: `-getblockchaininfo`, `-getnetworkinfo`, `-getpeerinfo`, `-getmempoolinfo`, and `-checkhealth`.

### Fixed
- **Daemon Graceful Shutdown & Fail-Closed BoltDB Safety (`node/server.go`):** Implemented exported `ShutdownServerSafely(server, peerServer, limiter, bc, timeout)` which terminates HTTP listeners, P2P network cycles, and rate limiters. If HTTP shutdown times out or fails, BoltDB close/flush is explicitly ABORTED (fail-closed), guaranteeing that in-flight HTTP handlers cannot access a closed BoltDB instance or cause panics and corruption.
- **Background Goroutine Lifecycle in IP Rate Limiter (`node/server.go`):** Added `stopChan chan struct{}`, `doneChan chan struct{}`, `stopOnce sync.Once`, and `Close()` method to `IPRateLimiter`. `Close()` blocks on `doneChan`, guaranteeing that the background cleanup goroutine has completely terminated before `Close()` returns.
- **Concurrent Serialized Size Cache & Invalidation (`core/block.go`, `core/transaction.go`):** Converted `cachedSize` to `int32` synchronized with atomic load/store operations (`atomic.LoadInt32`, `atomic.StoreInt32`). Added `InvalidateCache()` on `Block` and `Transaction` and integrated cache clearing into `tx.Sign()`. Eliminates data race detected by `go test -race` during concurrent mempool admission, RPC queries, and P2P block validation.
- **P2P Heartbeat Session Pointer Identity (`node/p2p.go`):** In `startPeerSessionHeartbeat()`, captured exact session instance pointer `targetSess` into the heartbeat goroutine and passed it directly to `p.dropOutboundSession(targetAddr, targetSess)`, preventing dead session teardown from erroneously dropping replacement sessions registered under the same address.
- **P2P Outbound Work Admission & NAT Node Relay (`node/p2p.go`):** Added `registerOutboundWork() (done func(), admitted bool)` synchronized under `lifecycleLock.RLock()` to track outbound broadcasts and relays on the active cycle's `WaitGroup` while running, and reject work immediately once the server is stopping or stopped. Nodes without an active inbound listener cycle (`activeCycle == nil`, e.g. behind NAT or client nodes) safely admit work without panicking, enabling reliable block broadcast and transaction relay.
- **RPC Auth Tracker Memory Bounds & Prioritized Lockout Eviction (`node/auth.go`):** Added timestamp tracking (`lastFail time.Time`) and a 3-pass prioritized eviction policy: expired entries first, non-locked failure records (1-4 attempts) second (preserving actively failing IP), and an emergency cap at 5,000 entries. Strictly protects active unexpired lockouts from being flushed by high-volume IP flood scans while bounding memory usage.
- **P2P Dashboard & Seed Telemetry Deduplication (`node/p2p.go`, `node/server.go`, `gui/main.go`, `gui/main_unix.go`):** Fixed double-counting where configured DNS seeds and their resolved physical IPs were stored and displayed as separate entries. Added endpoint normalization (`seedHostToIPs`, `seedIPToHosts`), active verified session tracking (`activeInboundConns`, `GetPeersActive()`), and differentiated endpoints in `/stats`, `/status`, and `/peers?filter=active`.
- **P2P Endpoint IPv6 Normalization (`node/p2p.go`):** Preserved IPv6 bracket notation `[::1]:port` in `normalizePeerEndpoint` to prevent incorrect port stripping and collision with IPv4 loopback `127.0.0.1:port`.
- **Peer Store Migration Non-Destructive Invariant (`node/p2p.go`):** Migrated legacy peer store `<dataDir>/peers.json` into network-isolated `<dataDir>/<network>/peers.json` non-destructively with `0600` permissions. The legacy file is left intact and unmodified for safe downgrade compatibility.
- **Fail-Closed Peer Store Corruption Handling (`node/p2p.go`):** If a peer store file is corrupted or schema-invalid, it is preserved at `<path>.corrupt` (with timestamped forensic fallback), TOFU auto-pinning is disabled, and `savePeers()` aborts to prevent overwriting disk state.
- **Miner RPC Auth Cookie Path Resolution (`miner/miner.go`):** Updated `cookieCandidates` to query `node.GetRPCAuthCookiePath()` first, supporting both isolated `<dataDir>/<network>/armax.cookie` and legacy working directory locations.
- **Genesis Merkle Root Calculation Isolation (`core/genesis.go`):** Explicitly specified network profiles for Regtest and Soak genesis Merkle roots (`CalculateMerkleRootForNetwork`), preventing global network state mutation during parallel unit tests from altering consensus genesis identities.
- **GUI Pre-Sign Bounds Validation & Error Formatting (`gui/main.go`, `gui/gui_test.go`):** Added pre-sign validation bounds check in `queryNonceAndSignGUITransaction` rejecting non-positive amounts, out-of-bounds fees, or sums exceeding `MaxSupply` before contacting RPC or signing. Decoupled internal Go error string from UI modal alerts, returning lowercase `errors.New("transaction amount or fee exceeds permitted bounds")` per Go ST1005 convention.

### Security
- **Post-Quantum Cryptographic Integrity Audit (`core/pqsig/pqsig.go`, `core/transaction.go`):** Audited transaction signature and validation pipeline. Confirmed 100% adherence to NIST FIPS 204 (ML-DSA-65), SigAlg identifier `0x01` (`SigAlgMLDSA65`), Context `ARMX-TX-V1`. Confirmed zero ECDSA exists in transaction signing or validation logic.
- **TOFU Pinning Fail-Closed Protection:** Peer store corruption triggers immediate fail-closed state, disabling unpinned trust-on-first-use adoption.

### Tests
- **Test Hooks & Concurrency Validation (`core/testhooks.go`):** Added `SetTxCachedSizeForTesting` and deferred mainnet network unlocking in `LockNetworkForTesting()`.
- **Miner Flow In-Process Test Suite (`node/miner_compat_test.go`):** Added automated regression tests verifying mining info, block template structure, `X-Mine-Key`, and Basic auth flow.
- **P2P Exchange & Verification Suites (`node/p2p_block_exchange_test.go`, `node/p2p_peer_validation_test.go`):** Added in-depth validation of chunked block exchange, chainwork reorganization, and loopback address boundary checks.
- **Cross-Chain Bridge & EVM Contracts:** Baseline evidence records a 100% pass rate for local EVM smart contract tests (`npm test` in `contracts/`, 11/11 tests recorded passed) and Stage P3-A Go multisig relayer offline unit tests (7/7 suites passed). Full Go bridge package execution (`go test ./bridge`) is blocked by legacy fixture subprocess failures (`TestLocalGoRelayerE2E` and `TestSignedEVMWriter*`) caused by a 4-argument deployment fixture against the 6-argument `WrappedArmaxcoin` Revision 6.1.1 constructor and obsolete role checks. Full end-to-end bridge test suite re-execution has not been independently run with attached logs, and bridge publication readiness is tracked independently from Core Node v1.0.6.

### Documentation
- Consolidated and audited technical documentation across `README.md`, `CHANGELOG.md`, `WHITEPAPER.md`, `docs/CONSENSUS_AND_NETWORK.md`, `docs/NODE_OPERATIONS.md`, `docs/MINING_GUIDE.md`, and `docs/releases/v1.0.6.md`.

### Known Issues & Compatibility Notes
- **Live Mixed-Version Operation:** P2P wire framing remains backward-compatible between v1.0.5 and v1.0.6 nodes. Upgrading operators are advised to follow standard rolling restart procedures.
- **Mining Daemon Architecture:** The core daemon (`armcoind`) does not host a Stratum server; it provides HTTP RPC block templates. `armminer` includes a Stratum v1 TCP client for external pools, but pool infrastructure is operated independently.
- **Windows Test Suite I/O Cadence:** The full `node` test suite under Windows may require ~250 seconds due to bbolt database creation and teardown overhead under local antivirus filesystem hooks; full execution of this suite has not been independently re-run or verified with logs in this review cycle.

## [v1.0.5] - 2026-10-27

> **Note:** `v1.0.2` was never released or tagged. The release sequence is `v1.0.0`, `v1.0.1`, `v1.0.3`, `v1.0.4`, `v1.0.5`.
> See the [v1.0.5 release notes](docs/releases/v1.0.5.md), including the copied artifact checksums and their verification note.

### Version
- Bumped the protocol/software version to `1.0.5` (`core/consensus.go`, GUI footer, Windows manifest, `scripts/build-release.ps1`). `scripts/verify-release.sh` now takes the expected version as an optional second argument (default `v1.0.5`) and strips the surrounding quotes from the printed recovery phrase, which previously made its mnemonic-restore step fail.
- **HARD FORK at block 8,000 (testnet): all nodes and miners MUST upgrade to v1.0.5 before the tip reaches 8,000.** Implements C1 (block timestamp must exceed the median of the last 55 blocks, MTP-55) and C2 (timestamp must not be earlier than the parent). Below height 8,000 the legacy MTP-11 rule applies, so existing testnet blocks stay valid and sync from genesis is unchanged. The activation height must not be changed once the tip has passed it. Genesis, NetworkID, and hashing are unchanged. Miner and `/getblocktemplate` now use max(now, minimum valid timestamp). C3–C5 remain design-only; C4 stays at 25,000 and C5 is local policy.

### Fixed
- **P2P block synchronization past block #4000 (IEEE-754 float64 precision loss).** Fixed `node/p2p_sync.go` and `node/p2p.go` where 64-bit integer nonces exceeding 2^53 suffered precision loss when deserialized into generic `interface{}` payloads, causing `calculated hash mismatch` errors on candidate blocks. Added custom `UnmarshalJSON` preserving raw JSON bytes via `json.RawMessage`.
- **Stratum v1 TCP Pool Client (`armminer`).** Implemented native Stratum v1 protocol client (`stratum+tcp://`) supporting difficulty target negotiation, per-thread nonce partitioning, clean job handling, and multi-endpoint failover.
- **Windows Defender false positive mitigation.** Stripped symbol tables and debug paths (`-trimpath -ldflags="-s -w -buildid="`) across all Windows PE executables to eliminate machine learning heuristic flags (`Bearfoos.A!ml`).
- **Node never rejoined after a connectivity loss.** After an outage of ~45 s every peer (including the configured seeds) was evicted from the address book after 3 failed sync rounds and marked unreachable for 15 minutes; seeds were only read at startup, so the node stayed isolated and could mine a private branch until restarted. Now: seeds are never evicted and are re-dialed (DNS re-resolved) every ~60 s and immediately whenever the address book is empty; if every peer fails in the same round (local link down) no peer is penalized.
- Seed re-resolution runs silently; only the startup resolve prints `[DNS Seed]` lines, so `armcoind.log` is no longer flooded every minute.
- A node on a stale branch more than `MaxReorganizationDepth` (720) blocks deep now logs an explicit error (rate-limited) telling the operator to restart with `-resync`, instead of silently refusing.
- **Unix/macOS Web GUI wallet persistence (`gui/main_unix.go`).** Removed silent unencrypted session wallet fallback (`EncryptAndSaveKeystore` with empty passphrase), added `/api/wallet/create` with passphrase validation and 24-word recovery phrase display, implemented `executeCreateWallet()` and `executeRestoreWallet()` in the Web UI, and aligned `wallet.json`/`addressbook.json` path resolution and CLI flags with the Windows GUI.
- **`armwallet -rescan` history parsing.** Fixed `rescanWallet` to unmarshal the `[]TxRecord` JSON array returned by `/history` (with legacy object fallback).
- **RPC mining endpoints & standalone miner.** `/getmininginfo` now reports `"armaxhash-v2"` when active and includes `"generate"`; `/getblocktemplate` filters candidate mempool transactions through `SelectCandidateTransactionsLocked` state/nonce overlay simulation and includes `"mintime"`; `armminer` clamps candidate timestamps to `mintime` and advances timestamps during long mining rounds.
- **`IsChainValid()` full consensus verification.** Extended `IsChainValid` (`armcoind -verifychain`) to verify block/tx size limits, miner-coinbase address matching (`[C-26]`), duplicate transactions (`[C-18]`), MTP-11/MTP-55 timestamp rules (`validateBlockTimestampRules`), checkpoints, and full state/signature execution from genesis.
- **Release verification script (`scripts/verify-release.ps1`).** Updated version check to `v1.0.5` and stats supply field to `circulating_supply`.

### Security
- Pinned `toolchain go1.27.3` in `go.mod` to remediate `GO-2026-6809` (`crypto/x509`) reported by `govulncheck`.

### Changed
- Added `core.OpenBlockchain` and `node.RunServerWithConfig`, which return errors instead of panicking. `armcoind` and the GUI now exit with a clear message on database or startup failures. `core.NewBlockchain` and `node.StartServerWithConfig` keep their panicking behavior for existing callers and tests.
- Clarified why the P2P client TLS config skips chain verification (peer authentication is done by TOFU pinning).
- `go mod tidy` now lists direct imports as direct requirements.
- Documentation: Go 1.27.3+ guidance, mainnet-readiness gate status table, and cross-references between overlapping testnet/release guides.
- Testnet display ticker is now `tARMX` in the daemon, miner, and GUI banners and messages (via `core.ActiveTicker()`). The ticker change itself is display-only: NetworkID, genesis, hashing, and serialization are unchanged, so existing testnet blocks remain valid (the separate C1/C2 timestamp hard fork is described under Version). `ARMX` remains the mainnet ticker.
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
