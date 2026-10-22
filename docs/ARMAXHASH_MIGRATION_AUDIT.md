# ArmaxHash v1 Migration Audit & Consensus Technical Specification

> **Active Consensus Algorithm:** Double-SHA256 (SHA-256d) up to block #519.  
> **Activation Status:** Scheduled for block **#520** on Testnet (`TestnetProfile.ArmaxHashActivationHeight = 520`). Mainnet remains disabled (`-1`).  
> **Database Compatibility:** All historical testnet blocks (0..493+) remain **100% valid** without database reset.  
> **Repository Integrity:** Genesis blocks unchanged, no commits, no push to remote.

---

## 1. Comprehensive Audit of Current PoW Subsystems

### 1.1 Block Header Hash Computation
- **Primary Method:** `Block.CalculateHash()` (`core/block.go`).
- **Canonical Header Commitment:**
  `record := fmt.Sprintf("%d:%d:%s:%s:%s:%d:%s", b.Index, b.Timestamp, b.PrevHash, b.MerkleRoot, b.Miner, b.Nonce, targetStr)`
  The cryptographic commitment binds exactly 7 canonical fields:
  1. `Index`: Block height (integer)
  2. `Timestamp`: Unix epoch timestamp in seconds
  3. `PrevHash`: Previous block hash (64 hex characters)
  4. `MerkleRoot`: BIP-standard binary Merkle tree root (64 hex characters)
  5. `Miner`: Miner reward address in Bech32 format (`armx1...`)
  6. `Nonce`: Mining nonce (`int64`)
  7. `Target`: 256-bit Proof of Work target ceiling (64 hex characters, left-padded)
- **Legacy Construction:** Double-SHA256 (SHA-256d): `h1 = sha256(record)`, `h2 = sha256(h1)`, formatted as 64 lowercase hexadecimal characters.

### 1.2 Block and Header Validation
- **Consensus Core:** `ValidateBlockLocked(b Block)` (`core/consensus.go`):
  - Step 6: `if b.Hash != b.CalculateHash()` verifies header commitment integrity.
  - Step 7: `expectedTarget := CalculateExpectedTarget(...)` followed by `if !HashMeetsTarget(b.Hash, expectedTarget)`.
- **Node & P2P Ingestion:**
  - `ValidateAndAddBlock(newBlock Block)` (`core/consensus.go`): Performs full state overlay simulation before committing atomically to the `bbolt` transactional database.
  - `AddOrphanBlock(b Block, peerIP string)` (`node/p2p.go`): Verifies `b.CalculateHash() == b.Hash` and `HashMeetsTarget` prior to storing orphan blocks in memory, preventing memory-exhaustion DoS attacks.
  - `IsChainValid()` (`core/blockchain.go`): Sequentially scans the local chain verifying `CalculateHash()` and `HashMeetsTarget` across every block from genesis to tip.

### 1.3 Mining and Nonce Search
- **Daemon In-Process Miner:** `MineBlockContext` (`core/blockchain.go`):
  - Reads the 256-bit target, partitions goroutines across CPU threads (`stride = numThreads`), and increments `b.Nonce += stride`.
- **External CPU Miner Client:** `armminer` (`miner/miner.go`):
  - Queries mining work via `/getblocktemplate`, constructs coinbase transaction, calculates transaction Merkle root, randomizes initial nonce partition, and evaluates candidate solutions.

### 1.4 Target Difficulty and Byte Order
- **Target Encoding:** Unsigned 256-bit big-endian integer, formatted as a 64-character hexadecimal string (`%064x`).
- **Numerical Verification:** `HashMeetsTarget(hashHex string, target *big.Int)`:
  - Decodes `hashHex` into a 32-byte slice.
  - Computes `hashNum := new(big.Int).SetBytes(hashBytes)`
  - Returns `hashNum.Cmp(target) <= 0`.
  - Big-endian lexicographical byte comparison `bytes.Compare(hash, targetBytes) <= 0` is mathematically equivalent to `hashNum.Cmp(target) <= 0`.
- **Difficulty Retargeting:** Zawy's LWMA-1 algorithm across an $N=60$ block window with a 120-second target block time (`core/target.go`).

### 1.5 Genesis, Synchronization, Fork Resolution, and Reorg
- **Genesis Block Anchors:**
  - Mainnet: `MainnetGenesisTargetHex = "00000000ffff0000..."`
  - Testnet: `TestnetGenesisTargetHex = "0000000dc84202e6..."`
  - Genesis block hashes are immutable and verified in `core/consensus_freeze_test.go`.
- **Fork Choice Rule (Satoshi Cumulative Chainwork):**
  $$\text{Work}_i = \left\lfloor \frac{2^{256}}{\text{Target}_i + 1} \right\rfloor, \quad \text{Chainwork} = \sum_{i=0}^{\text{Height}} \text{Work}_i$$
  - Nodes select the branch possessing strictly greater cumulative chainwork (*heaviest chain rule*), not the longest chain.
  - Maximum reorganization depth is bounded at 720 blocks.

### 1.6 RPC and API Interfaces
- `/getblocktemplate`: Returns candidate block parameters, difficulty, target hex, transactions, and `pow_algo`.
- `/getmininginfo`: Reports block height, difficulty, target, network hashrate, and `pow_algo`.
- `/stats` & `/getblockchaininfo`: Returns chain height, best block hash, and cumulative chainwork.
- `/block/{hash}`: Retrieves full block data by hash.

---

## 2. Block Hash Caller Audit & Safe Consensus Error Handling

The following table maps all files, functions, and execution paths interacting with block hash calculation, detailing their error-handling mechanisms and consensus safety guarantees:

| No | File & Line | Calling Function | Invocation & Error Handling | Consensus Impact & Safety Rationale |
|---|---|---|---|---|
| 1 | `core/block.go:73` | `Block.CalculateHashWithError()` | Core algorithm dispatcher. Directly returns `CalculateArmaxHash()` when $h \ge H_{\text{act}}$. On failure, returns `("", err)` immediately with **zero fallback to SHA-256d**. | **Zero Fallback:** Prevents malicious/oversized blocks from masquerading as SHA-256d blocks. |
| 2 | `core/block.go:84` | `Block.CalculateHash()` | Compatibility wrapper. If `CalculateHashWithError()` returns an error, returns an explicit sentinel: `"INVALID_HASH_ERROR:" + err.Error()`. | **Anti-Empty-Trap:** Never returns empty string `""` on error, eliminating `"" == ""` accidental matches. |
| 3 | `core/consensus.go:118` | `validateBlockContext()` | `calcHash, err := b.CalculateHashWithError()`. Rejects with `fmt.Errorf("block hash computation failed: %w", err)` if `err != nil`. Verifies `b.Hash != "" && b.Hash == calcHash`. | **Primary Consensus Gate:** Blocks failing PoW computation are rejected immediately and cannot enter chain state. Used identically by `ValidateBlockLocked` and `ResolveChain` (reorg). |
| 4 | `core/blockchain.go:978` | `IsChainValid()` | `calcHash, err := currentBlock.CalculateHashWithError()`. Returns `false` if `err != nil`, `currentBlock.Hash == ""`, or `currentBlock.Hash != calcHash`. | **Integrity Verification:** Reindexing or startup scans reject any corrupted or uncomputable block headers. |
| 5 | `node/p2p.go:1150` | `AddOrphanBlock()` | `calcHash, err := b.CalculateHashWithError()`. If `err != nil`, `b.Hash == ""`, or `b.Hash != calcHash`, applies ban score (+20) and returns `false`. | **DoS Mitigation:** Prevents peers from flooding orphan pools with uncomputable or miscalculated blocks. |
| 6 | `core/genesis.go:122` | `MineMainnetGenesis()` | `b.Hash, err = b.CalculateHashWithError()`. Verifies `err == nil` and `HashMeetsTarget` before accepting hash. | **Anchor Safety:** Genesis mining loop checks errors explicitly. |
| 7 | `core/genesis.go:160` | `GetRegtestGenesisBlock()` | `b.Hash, err = b.CalculateHashWithError()`. Verifies `err == nil` and `HashMeetsTarget` before accepting hash. | **Anchor Safety:** Regtest genesis generation checks errors explicitly. |
| 8 | `core/genesis.go:215` | `ValidateGenesisBlock()` | `calcHash, err := b.CalculateHashWithError()`. Returns error on computation failure or mismatch with immutable genesis hash. | **Anchor Safety:** Ensures genesis block verification handles errors explicitly without silent fallback. |
| 9 | `miner/miner.go:508` | `main() (local mining)` | `b.Hash, err = b.CalculateHashWithError()`. If `err != nil`, advances nonce stride and skips iteration without fallback. | **Miner Resilience:** Avoids submitting corrupted block templates or invalid hashes to daemon. |
| 10 | `node/server.go:680` | `/getblocktemplate` | Assembles next block candidate template, assigning `PoWAlgo: b.PoWAlgorithm()`. | **Template Accuracy:** Miners receive exact algorithm metadata for the target height. |
| 11 | `node/server.go:726` | `/getmininginfo` | Reports active PoW algorithm identifier along with target and difficulty. | **Observability:** RPC clients can monitor upcoming algorithm transitions. |

### 2.1 Remaining Usages of `CalculateHash()` and Boundaries
The string-returning API `CalculateHash()` has been **strictly deprecated for all consensus paths**. It is retained only in the following non-consensus areas:
1. **CPU Mining Throughput Benchmark (`miner/miner.go:801`):** In `runCPUBenchmark()`, worker threads execute `_ = b.CalculateHash()` in a closed loop to benchmark raw CPU hashing speed without touching consensus state.
2. **Test Setup Fixtures (`*_test.go`):** In unit test mocks and fixtures where historical blocks are constructed under disabled activation (`ArmaxHashActivationHeight = -1`).
3. **Usage Boundaries:**
   - `CalculateHash()` MUST NEVER be called in block validation, mining submission, orphan caching, chain reorganization, or state transitions.
   - Any consensus-critical caller must use `CalculateHashWithError()` and explicitly handle non-nil errors.

---

## 3. Explicit Specification of ArmaxHash v1

1. **Canonical Header Representation:**
   `record = Index || ":" || Timestamp || ":" || PrevHash || ":" || MerkleRoot || ":" || Miner || ":" || Nonce || ":" || TargetHex`
   `HeaderBytes = []byte(record)`
2. **Network ID Consistency:**
   - Mainnet: `"ARMX-MAINNET-1"`
   - Testnet: `"ARMX-TESTNET-1"`
   - Regtest: `"ARMX-REGTEST-1"`
   - Soak: `"ARMX-SOAK-1"`
3. **Domain Separation & Prehash Derivation:**
   $$\text{Domain} = \text{"ARMX-POW}\backslash 0\text{"}\ (9\ \text{bytes})$$
   $$\text{Payload} = \text{Domain} \parallel \text{Version (1 byte = 0x01)} \parallel \text{len(netID) (1 byte)} \parallel \text{netID} \parallel \text{HeaderBytes}$$
   $$D = \text{BLAKE2b-512}(\text{Payload})\ (64\ \text{bytes})$$
   $$\text{Password} = D[0:32]\ (32\ \text{bytes}), \quad \text{Salt} = D[32:48]\ (16\ \text{bytes})$$
   The length prefix on `netID` guarantees unambiguous parsing, preventing cross-network canonicalization collisions.
4. **Memory-Hard Core (Argon2id v1.3):**
   $$A = \text{Argon2id}(\text{Password}, \text{Salt}, \text{TimeCost}=2, \text{MemoryKiB}=2048, \text{Threads}=1, \text{OutputLen}=32)$$
   - Conforms strictly to RFC 9106.
   - Operates with single-thread lanes (`Threads=1`) to eliminate inter-thread lock contention when mining with multiple cores.
5. **Finalization Digest:**
   $$\text{FinalDigest} = \text{BLAKE2b-256}(\text{Password} \parallel A)\ (32\ \text{bytes})$$
6. **Target Comparison & Bridge:**
   - A digest satisfies the target ceiling if `bytes.Compare(FinalDigest, TargetBytes) <= 0`.
   - `BigIntToTarget(target *big.Int)` pads leading zeros to ensure a strict 32-byte representation without truncation bugs.
   - `MeetsBigIntTarget(hash []byte, target *big.Int)` provides a direct, verified bridge to consensus targets.

---

## 4. Independent Golden Vectors & Differential Testing

### 4.1 Independent Golden Vectors
The following reference vectors are implemented and verified in `armaxhash/armaxhash_test.go` (`TestMultiGoldenVectors`):

| Network / Case | Input Header | Expected ArmaxHash Digest (Hex) | Status |
|---|---|---|---|
| **Consensus Vector 999** | `"armax-stdlib-parity-verification-header-999"` (`armax-mainnet`) | `a6449126c335ddb0e9919a3bf4c8a025e5a25b205f08f7d8fed9bae41d423cf4` | **PASS** |
| **Mainnet Canonical Header 1** | `1:1710000100:0000000dc842...:armx1qqq...:12345:0000ffff...` (`ARMX-MAINNET-1`) | `67a90bb986f7a6e1bf32c4f178956aea138aef97a55654364caef74d33151355` | **PASS** |
| **Testnet Canonical Header 1** | `1:1710000100:0000000dc842...:armx1qqq...:12345:0000ffff...` (`ARMX-TESTNET-1`) | `53db64a3ae40c2ca99a4ba88c6eaa16a59b00bcee67ab8938282ec5038b50b85` | **PASS** |
| **Regtest Canonical Header 1** | `1:1710000100:0000000dc842...:armx1qqq...:12345:0000ffff...` (`ARMX-REGTEST-1`) | `f4dd187e00eb9e199b08dfaeaf0402bec6cf3c6ecbb0d36275abcbfb464222f3` | **PASS** |

### 4.2 Differential Target Verification (`TestDifferentialTargetComparison`)
Differential testing evaluates 30 distinct permutations across:
1. `new(big.Int).SetBytes(digest).Cmp(target) <= 0`
2. `armaxhash.MeetsTarget(digest, targetBytes)`
3. `armaxhash.MeetsBigIntTarget(digest, targetBigInt)`
**Result:** 100% equivalence across boundary targets ($0$, $1$, $0\text{xffff}$, $2^{128}$, $2^{240}$, $2^{256}-1$).

### 4.3 Preservation of Frozen Legacy Genesis Hashes
`core/consensus_freeze_test.go` verifies that legacy genesis hashes remain completely unchanged:
- Testnet Genesis Hash: `0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b` (**PASS**)
- Mainnet Genesis Hash: `00000000490b4d4586dbba29c36209b552b7b5ea3f405527a08ecda977926830` (**PASS**)

---

## 5. Consensus Separation & Explicit Activation Rules

1. **Proof of Invariant Under Deactivated State:**
   - Default state across all profiles is `ArmaxHashActivationHeight = -1`.
   - The consensus dispatcher in `core/network.go` enforces:
     ```go
     func IsArmaxHashActive(blockIndex int) bool {
         act := GetArmaxHashActivationHeight()
         if act <= 0 {
             return false
         }
         if blockIndex < 0 {
             return false
         }
         return int64(blockIndex) >= act
     }
     ```
   - When `act <= 0` (e.g. `-1` or `0`), the function returns `false` unconditionally.
   - Verified via unit test `TestActivationHeightDeactivatedInvariant`: across block heights from $0$ to $1,000,000$, `IsArmaxHashActive(h)` returns `false`, ensuring that all historical blocks, database stores, and new candidate blocks evaluate strictly with SHA-256d.
   - **No blockchain database reset is required; existing testnet blocks remain completely valid.**
2. **Deterministic Fork Activation:**
   - When an activation height $H > 0$ is scheduled:
     - Blocks $< H$: verified with SHA-256d.
     - Blocks $\ge H$: verified with ArmaxHash.
   - Older nodes rejecting block $H$ will cleanly identify a hardfork boundary rather than experiencing silent state corruption.
3. **Target Reset Bootstrap (Preventing Chain Stalls):**
   - At block $H$, `CalculateExpectedTarget` resets the target ceiling to `GetPowLimit()`.
   - This prevents difficulty carryover from high-hashrate SHA-256d to memory-hard Argon2id, allowing CPU miners to produce block $H$ without stalling, after which LWMA-60 automatically retargets.
4. **P0 Blocker Resolution: Zero-Fallback, Finite Mining Termination & Absolute Storage Immunity:**
   - **Vulnerability Identified:** Previously, if `CalculateArmaxHash()` failed (e.g., if a block header exceeded 1 MiB or had invalid network parameters), `CalculateHash()` would catch the error and silently fall back to `CalculateSha256dHash()`. This could allow an attacker to craft an oversized block that bypassed ArmaxHash entirely, creating a catastrophic consensus split.
   - **Remediation & Hardening:**
     - **Error-Returning Consensus API:** Implemented `CalculateHashWithError() (string, error)`: When `IsArmaxHashActive(b.Index)` is true, it strictly calls `CalculateArmaxHash()`. If `CalculateArmaxHash()` returns an error, that error is returned immediately -- **zero fallback to SHA-256d**.
     - **Finite Mining Termination & Error Delivery (`MineBlockContext`):**
       - Workers buffer errors onto `errChan := make(chan error, threads)` and record the first root error atomically via `recordError` (`sync.Once`).
       - `cancelWorkers()` signals all workers via `workerCtx.Done()` to stop immediately.
       - In `select`, if `ctx.Done()` fires while an error occurred, `if firstErr != nil { return Block{}, firstErr }` ensures the original failure cause is delivered to the caller rather than masked by `context.Canceled`.
       - `wg.Wait()` ensures all worker goroutines have stopped before returning.
       - No block is added to `bc.Chain` and chainwork is not modified.
     - **External Miner Throttled Cancellation (`armminer`):** `sync.Once` logs the fatal hashing error once per round without flooding logs, and `cancelRound()` stops all threads immediately.
     - **Deadlock-Safe NetworkID Hook:** `NetworkID()` snapshots `networkIDOverrideHook` under `netMu.RLock()`, releases the lock via `netMu.RUnlock()`, and only then invokes the hook. This eliminates re-entrant deadlock risks. Tests modifying hooks are serialized with `testGlobalHookLock sync.Mutex`.
     - **Absolute Storage Immunity:** `core/store.go` (`SaveBlockToStore`) implements explicit defense-in-depth verification calling `CalculateHashWithError()`. Blocks with computation errors, hash mismatches, or sentinel strings are rejected before entering `bbolt` buckets.
     - **Sentinel Protection & Anti-Empty String Trap:** `CalculateHash() string` wraps `CalculateHashWithError()`. On error, it returns an explicit unmatchable sentinel string: `"INVALID_HASH_ERROR:" + err.Error()`. All consensus validation paths verify `err == nil` and reject empty strings.
   - **Verification:** Unit and consensus tests prove:
     - Oversized headers (>1 MiB) return `armaxhash.ErrHeaderTooLarge` and fail consensus with `block hash computation failed`.
     - Corrupted/empty network IDs return `armaxhash.ErrInvalidNet` and abort mining immediately without advancing chain or modifying DB.
     - `SaveBlockToStore` directly rejects blocks with sentinels or hash mismatches.
     - Direct bolt bucket checks prove no rejected blocks exist on disk.
     - SHA-256d is **never** returned as a fallback when activation is active.
     - SHA-256d is **strictly** used across all heights when activation is disabled (`ArmaxHashActivationHeight = -1`).

---

## 6. Empirical Performance Benchmarks & Hardware Profile

### 6.1 Benchmark Methodology (Latency vs. Aggregate Throughput)
Benchmarks were executed on an Intel Core i5-8365U CPU @ 1.60GHz (x86-64 / amd64, 4 physical cores, 8 logical threads):

| Metric | Single-Core (1 Thread) | Multi-Core (8 Logical Threads) |
|---|---|---|
| **Benchmarked Mode** | `BenchmarkHashSingleCore` | `BenchmarkHashParallel` (`b.RunParallel`) |
| **Individual Execution Latency** | **~3.11 ms** (`3,114,979 ns/op`) | **~11.3 ms** per worker thread |
| **Normalized Operation Time** | `3,114,979 ns/op` | `1,413,974 ns/op` (wall-clock / total ops) |
| **Aggregate Network Throughput** | **~321.0 H/s** | **~707.2 H/s** (all 8 cores combined) |
| **Memory Allocation per Hash** | **2,099,505 B/op** (~2.002 MiB) | **2,099,488 B/op** (~2.002 MiB) |
| **Allocations per Hash** | 25 allocs/op | 25 allocs/op |

**Methodology Details:**
- **Latency per Hash:** Single-thread latency is ~3.11 ms. Under full 8-worker saturation, individual hash latency increases to ~11.3 ms due to CPU frequency throttling (AVX2 thermal headroom) and cache/memory bus contention.
- **Aggregate Throughput:** Because 8 threads execute concurrently, the system produces 6,022 hashes in ~8.5 seconds, yielding an aggregate throughput of **~707.2 hashes/second**.
- **Memory Allocation:** The algorithm is **not** zero-allocation. Each execution allocates 2,099,505 bytes (~2.002 MiB) for the Argon2id memory buffer.

### 6.2 Realistic Hardware Profile & ASIC Resistance Limitations
- **No Economic ASIC Immunity:** Claims of absolute "ASIC resistance" or "ASIC proof" are technically invalid. While 2 MiB requires far more silicon area than a bare SHA-256 engine, modern semiconductor fabrication (e.g. 7nm, 5nm, 3nm nodes) can easily pack dozens or hundreds of 2 MiB SRAM banks onto a single ASIC die.
- **ASIC Deterrent, Not Immunity:** The 2 MiB memory hardness serves as an **economic deterrent** rather than an insurmountable barrier. It forces potential ASIC developers to incorporate dedicated SRAM per core, significantly raising fabrication costs compared to trivial SHA-256 ASICs.
- **CPU L3 Cache Affinity:** The primary architectural achievement of 2 MiB is **CPU L3 cache affinity**. Modern desktop and server CPUs have 16 MB to 96 MB of L3 cache, allowing 8 to 32 parallel CPU mining threads to operate within high-speed CPU cache without saturating the system DRAM bus.

---

---

## 7. Verification Status & Repository Cleanliness

### 7.1 MineBlockContext Concurrent Fault Handling & Post-Select Error Verification
- **Post-Select Error Inspection:** In `core/blockchain.go`, `MineBlockContext` checks `if firstErr != nil { return Block{}, firstErr }` immediately after the `select` statement and `wg.Wait()` before passing `winningBlock` to `ValidateAndAddBlock`. If any worker records a hashing error or tip staleness during the round, the mined block is rejected and never added to the chain.
- **Deterministic Race Test:** `TestMineBlockContextConcurrentErrorRejectsBlock` in `core/armaxhash_migration_test.go` uses Go channel coordination (`miningDeterministicRaceHook`) to simulate a concurrent worker hashing failure right as a winning block arrives on `resultChan`. It verifies that:
  1. `MineBlockContext` returns the error from the faulted round;
  2. The returned block is empty (`Hash == ""`);
  3. The blockchain length and tip remain unchanged;
  4. All workers are cleanly terminated without leaving active workers or leaking goroutines.
  5. The test runs deterministically without relying on `time.Sleep()`.

### 7.2 Reentrant NetworkID Hook Deadlock Prevention & Global Serialization
- **Deadlock-Free Hook Execution:** `NetworkID()` in `core/network.go` snapshots the hook function under `netMu.RLock()` and immediately releases the lock before invoking the hook.
- **Reentrant Mutex Test:** `TestNetworkIDHookReentrantLockNoDeadlock` in `core/armaxhash_migration_test.go` proves that `NetworkID()` can execute a hook that re-entrantly calls functions requiring `netMu.Lock()` (e.g. `SetArmaxHashActivationHeight` or `SetNetwork`) without deadlocking.
- **Global Test Serialization:** `LockNetworkForTesting(t testing.TB)` in `core/testhooks.go` and `AcquireNetworkTestLock()` in `core/network.go` serialize all unit tests that mutate network profiles, activation heights, or network hooks. All cleanup is managed via `t.Cleanup(...)`, guaranteeing restoration even if a test fails or panics, with zero usage of `t.Parallel()` during global mutations.

### 7.3 Verification Test Suite Results
- **Full Test Suite (`go test -tags testhooks ./...`):**
  - `github.com/acxxcxx/armax-coin/armaxhash`: **PASS** (14 test cases + benchmarks)
  - `github.com/acxxcxx/armax-coin/core`: **PASS** (171s, includes dynamic retargeting & convergence tests)
  - `github.com/acxxcxx/armax-coin/miner`: **PASS** (11s)
  - `github.com/acxxcxx/armax-coin/node`: **PASS** (129s)
  - `github.com/acxxcxx/armax-coin/cli/armsend`: **PASS**
  - `github.com/acxxcxx/armax-coin/cli/armwallet`: **PASS**
  - `github.com/acxxcxx/armax-coin/gui`: **PASS**
  - `github.com/acxxcxx/armax-coin/wallet`: **PASS**
- **Go Vet (`go vet -tags testhooks ./...`):** **0 errors**.
- **Concurrency & Goroutine Safety:** Verified race-free across parallel workers via `TestConcurrentHashing` (8 concurrent goroutines), deterministic mining race tests, and dual-algorithm chain reorg tests.

---

## 8. Post-Activation Dynamic Retargeting & Live Network Validation

### 8.1 ArmaxHash Bootstrap & Floor Unlocking
- **Historical Invariant Preservation ($H < 520$):** `IsArmaxHashActive(h)` returns `false`. All historical blocks 0..519 remain 100% valid under genesis SHA-256d consensus rules and legacy `powFloor` clamping.
- **Activation Block Bootstrap ($H = 520$):** Target resets to `ArmaxHashPowLimitHex` (`000ffff...`, Difficulty 3, 4,096 hashes), enabling heterogeneous hardware (from 20 H/s single-board computers to 500+ H/s laptop CPUs) to mine immediately.
- **Dynamic Floor ($H \ge 520$):** `powFloor` is opened to `DifficultyToTarget(1)`, allowing LWMA-1 to lower difficulty smoothly if hashrate drops, avoiding chain stalls.
- **Historical Window Normalization:** Pre-activation SHA-256d blocks within the 60-block sliding window are normalized to `GetArmaxHashPowLimit()` to prevent legacy SHA-256d difficulty from dragging down the target and spiking post-fork difficulty.

### 8.2 Live Network Multi-Node Consensus Verification
The ArmaxHash activation at block #520 was deployed to live testnet nodes:
- **Seed Nodes:** `testnet-seed1.armax-coin.cloud` (`202.10.34.180`) and `testnet-seed2.armax-coin.cloud` (`139.162.46.17`) verified online and peering.
- **SBC Mining Validation:** An ARM64 SBC running Armbian (`x96q-v1-3`, 1 worker thread @ ~21.3 H/s) successfully mined block `#571` on live testnet (`armx1m0zc657c6an7zjtlqkd06qvlrkwzmh3c2vj302ge7u5dh2nuv59qyymq62`), which was accepted by all network peers.
- **Stale Block Resolution:** When concurrent nodes mined competing solutions, daemons correctly issued `HTTP 409 Stale Block`, and miners cleanly aborted stale rounds and advanced to the new tip without forks or orphan chain growth.
- **Cadence Stabilization:** Block times successfully converged from initial bootstrap intervals (~10s) towards the target steady-state of **120 seconds (2 minutes)** per block as difficulty scaled from Difficulty 3 to Difficulty 4.

### 8.3 Production Logging Standards
All binaries were upgraded to conform to enterprise blockchain logging standards:
- **`armcoind`:** Outputs timestamped block receipts with peer IP, truncated 256-bit hash, transaction count, difficulty, and solve time (`dt`).
- **`armminer`:** Formats hashrate dynamically (`H/s` when $<1000$ H/s, `kH/s` when $\ge 1000$ H/s) and provides explicit block solved, accepted, and stale notices.
- **`armsend` & `armwallet`:** Provide full post-quantum transaction broadcast summaries with ML-DSA-65 payload sizes, sender/recipient addresses, fee details, and query timestamps.


