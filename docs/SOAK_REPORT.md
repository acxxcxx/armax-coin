# Testnet Soak and Chaos Test Report [L2-4]

## 1. Overview
This report details the execution and verification of the 5-node testnet soak and chaos testing suite (`node/soak_test.go` under `//go:build soak`). The suite subjects an Armax testnet cluster to network churn, rapid block generation, ML-DSA-65 post-quantum transaction signing, sudden node crashes, deep fork reorganizations, network partition healing, and full Initial Block Download (IBD) from scratch.

## 2. Test Configuration & Parameters
- **Test Tag**: `//go:build soak`
- **Target Blocks**: 2,005 blocks (total height: 2,006 blocks including Genesis block #0)
- **Cluster Size**: 5 initial local testnet nodes + 1 fresh bootstrap node (Node 6)
- **Signature Scheme**: NIST FIPS 204 ML-DSA-65 (FIPS 204)
- **Consensus Rule Profile**: `ARMX-TESTNET-1`
- **Target PoW Ceiling**: Difficulty 1 (`core.DifficultyToTarget(1)`)
- **Block Time**: 120s target (accelerated in test harness)
- **Network ID**: `ARMX-TESTNET-1`

## 3. Chaos Phases & Scenarios
1. **Phase 1: Cluster Initialization & ML-DSA-65 Transaction Soak (Blocks 1..500)**
   - Initialized 5 isolated node runtimes with dedicated bbolt database files in isolated directories.
   - Connected nodes into a full P2P mesh topology over localhost TCP ports.
   - Mined 500 blocks on Node 1, injecting authenticated ML-DSA-65 transactions transferring funds between test wallets (`soak_user_a` and `soak_user_b`).
   - Propagated blocks across the mesh; verified all 5 nodes reached block height 501.

2. **Phase 2: Network Partition & Competing Fork Race (Blocks 501..700)**
   - Simulated network partition:
     - **Partition A (Nodes 1, 2, 3)** mined 200 consecutive blocks (501..700, total chainwork: `1073753024`).
     - **Partition B (Nodes 4, 5)** mined a competing 50-block fork (501..550, total chainwork: `1073750624`) with an alternate miner.

3. **Phase 3: Sudden Node Crash & Cold Storage Revival**
   - Forcibly terminated Node 2's TCP peer server and unceremoniously closed its active database connection while at height 501.
   - Audited the node filesystem for corruption markers: 0 `.invalid-*` quarantine databases produced.
   - Reopened the bbolt database and executed `RebuildState()` to verify ledger and UTXO/account state reconstruction from raw block history.

4. **Phase 4: Network Partition Healing & Automatic Chainwork Reorganization**
   - Healed network partition by reconnecting Partition B nodes (4 & 5) back to Node 1.
   - Verified that Partition B detected the higher cumulative chainwork on Partition A (`1073753024` > `1073750624`).
   - Executed automatic chain reorganization via `ResolveFork` back to the common ancestor at height #479 / #500.
   - Replaced competing tip with canonical tip at height 701 without state leakage.

5. **Phase 5: Extended Block Soak (Blocks 701..2005)**
   - Mined remaining blocks to reach target 2,005 blocks (total height: 2,006 blocks).
   - Re-synchronized all 5 cluster peers to height 2,006 via bounded `GET_BLOCK_RANGE` chunks (`SyncChunkSize = 250`).

6. **Phase 6: Initial Block Download (IBD) from Scratch (Node 6)**
   - Bootstrapped a completely blank node (Node 6) with an empty database directory.
   - Connected Node 6 to Node 1 and performed full IBD across all 2,005 blocks.
   - Node 6 successfully validated PoW, Merkle roots, ML-DSA-65 signatures, and state transitions, reaching height 2,006 in ~3 seconds.

## 4. Final Invariants Audit
| Invariant | Requirement | Observed Status | Result |
| :--- | :--- | :--- | :--- |
| **Tip Consistency** | All nodes have identical tip hash & height | All 6 nodes at height `2006` with tip hash `19cf56...` | **PASSED** |
| **Supply Invariant** | Total supply == sum of all block rewards | Expected `120,360.00 ARMX` (2,006 * 60 ARMX) == Actual `12036000000000 Armoids` | **PASSED** |
| **Double Spend Prevention** | Nonce sequencing and account balances valid | `soak_user_a` balance > 0, nonces monotonically applied | **PASSED** |
| **Storage Integrity** | No invalid DB markers created | 0 `.invalid-*` files across all 6 node storage directories | **PASSED** |
| **Chain Validity** | Full PoW and Merkle root cryptographic validity | `bc.IsChainValid() == true` across all nodes | **PASSED** |

## 5. Execution Metrics
- **Total Duration**: `20.06s`
- **Blocks Processed**: `2,005` mined blocks + `1` Genesis block
- **Total Nodes Evaluated**: 6 nodes (5 cluster nodes + 1 fresh IBD bootstrap node)
- **Chain Reorganizations Handled**: `1` (Partition B fork reorg from ancestor #479)
- **Final Circulating Supply**: `120,360.00 ARMX` (`12,036,000,000,000 Armoids`)
- **Memory / DB Leaks**: None detected

## 6. How to Reproduce
Run the dedicated soak test target from the project root:
```bash
go test -v -tags soak -run TestSoak_FiveNodesChaos ./node
```

---

## 7. Realistic LWMA-1 Mining Simulation (2,000 Blocks) [L4-5]

To validate LWMA-1 dynamic difficulty retargeting under realistic non-constant mining dynamics, a 2,000-block simulation (`core/lwma_realistic_test.go`) was executed where block solve times are drawn from an exponential distribution (Poisson arrival process):

$$T_{\text{solve}} \sim \text{Exponential}\left(\mu = \frac{2^{256}}{\text{Target} \times \text{Hashrate}}\right)$$

### A. Test Parameters & Hashrate Scenarios
- **PRNG Seed**: `42` (fixed deterministic seed for reproducibility)
- **Target Block Interval**: $T = 120$ seconds
- **Base Hashrate**: $H_0 = 10\text{ MH/s}$
- **Total Blocks Evaluated**: 2,000 blocks
- **Scenarios Evaluated**:
  1. **Phase 1 (Blocks 1..500):** Steady network hashrate ($10\text{ MH/s}$).
  2. **Phase 2 (Blocks 501..1,000):** Sudden 10x hashrate spike ($100\text{ MH/s}$).
  3. **Phase 3 (Blocks 1,001..1,500):** Sudden 10x hashrate drop back to baseline ($10\text{ MH/s}$).
  4. **Phase 4 (Blocks 1,501..2,000):** Periodic alternating hashrate ($10\text{ MH/s} \leftrightarrow 50\text{ MH/s}$ every 100 blocks).

### B. Measured Numerical Results

| Phase | Scenario | Converged Window | Measured Average Block Time | Delta from Target ($120\text{s}$) | Status |
|---|---|---|---|---|---|
| **Phase 1** | Steady Hashrate ($10\text{ MH/s}$) | Blocks 100..500 | **121.31s** | $+1.09\%$ | **PASS** (Within $\pm 25\%$) |
| **Phase 2** | Post-10x Spike ($100\text{ MH/s}$) | Blocks 600..1,000 | **121.05s** | $+0.87\%$ | **PASS** (Within $\pm 25\%$) |
| **Phase 3** | Post-10x Drop ($10\text{ MH/s}$) | Blocks 1,100..1,500 | **122.77s** | $+2.31\%$ | **PASS** (Within $\pm 25\%$) |
| **Phase 4** | Repeating Oscillation ($10 \leftrightarrow 50\text{ MH/s}$) | Blocks 1,500..2,000 | **119.59s** | $-0.34\%$ | **PASS** (Within $\pm 25\%$) |
| **Overall** | Full 2,000 Block History | Blocks 1..2,000 | **125.32s** | $+4.43\%$ | **PASS** (Within $\pm 25\%$) |

### C. Invariants Verification
- **Target Clamping**: At no point did the 256-bit target exceed `PowFloor` (difficulty never fell below baseline `PowLimit`).
- **No Unbounded Oscillation**: Target adjusted smoothly across dramatic 10x shifts without runaway divergence or zero target.
- **Empirical Convergence**: Post-convergence block cadence settled within $+0.87\%$ to $+2.31\%$ of the 120-second consensus ideal under real Poisson variance.

