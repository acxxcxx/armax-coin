# Armaxcoin Consensus Improvement Proposals (AIP / C1–C5)

**Status:** PROPOSED -- DESIGN ONLY (DO NOT MERGE INTO ACTIVE CODE WITHOUT MAINTAINER APPROVAL)  
**Target Network:** `ARMX-TESTNET-1` and Future `ARMX-MAINNET-1`  
**Author:** Armaxcoin Core Security & Consensus Engineering  
**Date:** October 2026  

---

## Executive Summary

This document formalizes design proposals for five critical consensus upgrades identified during the static security audit of Armaxcoin Layer-1 PoW blockchain. 

In accordance with strict consensus safety protocols, **these proposals MUST NOT be implemented in active code until formal community review, testnet simulation, and scheduled activation heights are approved**. Modifying these rules without coordinated activation heights would result in unintended hard forks and permanent network partition.

---

## Table of Proposals

| Proposal ID | Title | Consensus Subsystem | Fork Type | Target Activation |
|---|---|---|---|---|
| **C1** | Extended MTP Window & Parent-Relative Timestamp Bound | Header Timestamp Validation | Hard Fork | Proposed Block #15,000 |
| **C2** | Nondecreasing Timestamps for LWMA-1 | LWMA Difficulty Calculation | Hard Fork | Proposed Block #15,000 |
| **C3** | ML-DSA-65 Canonicality & Cross-Network Replay Audit | Cryptographic Verification | Audit / possible rule change | No activation proposed |
| **C4** | Transaction Time-To-Live (TTL / Expiry Height) | Mempool & Transaction Structure | Hard Fork | Proposed Block #25,000 |
| **C5** | Reorg Depth Limit & Coinbase Maturity Invariant | Fork Resolution & State Replay | Local policy | No activation proposed |

---

## Proposal C1: Extended MTP Window & Parent-Relative Timestamp Bound

### 1. Problem Statement & Attack Scenario
The current consensus rules (`core/consensus.go:validateBlockContext`) validate block timestamps using:
1. **Median Time Past 11 (MTP-11):** `block.Timestamp > CalculateMedianTimePast(chainHistory, 11)`
2. **Future Time Limit:** `block.Timestamp <= time.Now() + 2 hours`

**Risk to evaluate:**
An 11-block MTP window allows timestamps to vary within the existing consensus constraints. However, the current LWMA implementation clamps individual solve times and bounds the weighted solve-time sum, so this proposal must not assume unbounded difficulty suppression. Any attack claim or hashrate threshold requires reproducible analysis against the implemented rules.

### 2. Proposed Consensus Rule Change
1. **Expand MTP Window to 55 Blocks (MTP-55):**
   - Sample the median timestamp across the previous 55 blocks (~110 minutes).
    - The security effect and liveness impact of this window size require simulation and review before adoption.
2. **Bound Parent-Relative Timestamp Regression:**
    - Consider requiring `block.Timestamp >= prev.Timestamp - MaxClockDrift`, where `MaxClockDrift = 1800` seconds.
    - This bounds regression relative to the parent timestamp; it does not bound age relative to wall-clock time. A wall-clock age check would be node policy unless clock-skew and historical-replay behavior are specified carefully.

```go
// Proposed Consensus Rule C1 Specification:
func ValidateBlockTimestampC1(b *Block, prev *Block, chainHistory []Block) error {
    mtp55 := CalculateMedianTimePast(chainHistory, 55)
    if b.Timestamp <= mtp55 {
        return fmt.Errorf("block timestamp %d <= MTP-55 %d", b.Timestamp, mtp55)
    }
    if b.Timestamp < prev.Timestamp-MaxClockDrift {
        return fmt.Errorf("block timestamp %d regresses too far from parent timestamp %d", b.Timestamp, prev.Timestamp)
    }
    // Wall-clock ceiling remains active only for live tip blocks
    if b.Timestamp > time.Now().Unix() + MaxFutureBlockTime {
        return fmt.Errorf("block timestamp %d exceeds wall-clock ceiling", b.Timestamp)
    }
    return nil
}
```

### 3. Migration & Activation Plan
- **Activation Gate:** Block height activation (`ActivationHeight_C1 = 15000`).
- For `b.Index < ActivationHeight_C1`: Evaluate legacy MTP-11.
- For `b.Index >= ActivationHeight_C1`: Strictly enforce MTP-55.

### 4. Backward Compatibility
- **Hard Fork:** Old nodes running MTP-11 would accept blocks that new nodes reject (if a block has `MTP-11 < Timestamp <= MTP-55`). All validators and mining pools must upgrade prior to activation.

### 5. Test Plan
- Unit tests verifying MTP-55 calculation with exactly 55, < 55, and > 55 historical blocks.
- Simulation of timewarp attack vectors demonstrating difficulty suppression failure under MTP-55.

---

## Proposal C2: Nondecreasing Timestamps for LWMA-1 Difficulty

### 1. Problem Statement & Attack Scenario
Currently, LWMA-1 calculates difficulty using the time intervals between adjacent blocks:
```go
solvetime := chain[i].Timestamp - chain[i-1].Timestamp
```
Because consensus currently only requires `Timestamp > MTP-11`, it is mathematically possible for `block[i].Timestamp < block[i-1].Timestamp` (negative solvetime).
When `solvetime < 0`, the current implementation clamps the interval and also bounds the accumulated weighted solve time. Negative intervals can still affect the retarget result within those bounds, but they do not cause the unbounded arithmetic instability described by this proposal's earlier draft. The economic impact should be measured before asserting an exploitable difficulty reduction.

### 2. Proposed Consensus Rule Change
Enforce **nondecreasing timestamps** from the activation height:
```go
// Proposed Consensus Rule C2 Specification:
if b.Index >= ActivationHeight_C2 {
    if b.Timestamp < prev.Timestamp {
        return fmt.Errorf("block #%d timestamp %d is earlier than previous block timestamp %d (monotonicity violation)",
            b.Index, b.Timestamp, prev.Timestamp)
    }
}
```
Optionally require `b.Timestamp >= prev.Timestamp + 1` for strictly increasing timestamps. The selected rule and its effect on valid historical timestamps must be tested before adoption.

### 3. Migration & Activation Plan
- Combined with Proposal C1 at `ActivationHeight = 15000`.

### 4. Backward Compatibility
- **Hard Fork:** Blocks with `Timestamp < prev.Timestamp` (valid under legacy rules) are rejected by upgraded nodes. Historical blocks (0..14999) remain valid under legacy rules.

### 5. Test Plan
- Regression test submitting candidate block with `Timestamp == prev.Timestamp - 1` (rejected post-activation, accepted pre-activation).
- Verification that LWMA solvetime inputs are strictly non-negative.

---

## Proposal C3: ML-DSA-65 Canonicality & Cross-Network Replay Audit

### 1. Problem Statement & Attack Scenario
Armaxcoin uses NIST FIPS 204 ML-DSA-65 post-quantum digital signatures.
Two questions warrant verification; this proposal does not establish either as a vulnerability:
1. **Signature Encoding:** Confirm the ML-DSA implementation's handling of malformed or non-canonical signature encodings against NIST FIPS 204 and its library contract. Do not claim signature malleability without a reproducible accepted alternate encoding.
2. **Cross-Network Replay:** The current transaction hash includes the active Network ID, and signature verification recomputes that hash. Add cross-network tests to preserve this behavior; do not describe Network ID binding as absent.

### 2. Proposed Consensus Rule Change
1. **Preimage Conformance Tests:** Document the existing canonical transaction record, including Network ID, SigAlg, sender public key, addresses, amount, fee, nonce, and applicable script fields; test that signing and verification use it consistently.
2. **Signature Validation Review:** Confirm exact signature-length and malformed-encoding rejection behavior in the deployed `crypto/mldsa` implementation. Add checks only where the library specification requires them and the current code lacks them.

### 3. Migration & Activation Plan
- No activation height should be selected unless review identifies a consensus-rule change. If implementation behavior changes, specify compatibility and activation requirements separately from this audit.

### 4. Backward Compatibility
- **Compatibility:** No transaction-format change is implied by auditing the current preimage. Any proposed signing-format change would need a separate compatibility analysis.

### 5. Test Plan
- Test attempting to replay a testnet-signed transaction on a regtest/mainnet node (must fail verification).
- Fuzzing of signature byte encodings to ensure alternate polynomial packings are rejected.

---

## Proposal C4: Transaction Expiry (Time-To-Live / Block Height Expiration)

### 1. Problem Statement & Attack Scenario
Standard transactions do not currently have an explicit expiry field. A low-fee transaction may remain in a node's mempool until it is evicted or becomes invalid. Because account nonces are sequential, confirming a later transaction from the same account makes an earlier nonce stale; an old transaction cannot later execute with an already-consumed nonce. An explicit expiry could still give users predictable transaction lifetimes and simplify mempool cleanup.

### 2. Proposed Consensus Rule Change
Add an optional or mandatory `ExpiryHeight` field to `Transaction`:
```go
type Transaction struct {
    // ... existing fields ...
    ExpiryHeight int64 `json:"expiry_height,omitempty"` // Block height after which tx cannot be mined
}
```
**Consensus Rules:**
1. A transaction is valid for inclusion in block $H$ if and only if:
   $$H \le \text{ExpiryHeight} \quad (\text{or } \text{ExpiryHeight} == 0 \text{ for perpetual legacy})$$
2. Nodes automatically purge mempool transactions when current tip height $> \text{ExpiryHeight}$.

### 3. Migration & Activation Plan
- Introduce version 2 transaction format (`TxVersion = 2`) at `ActivationHeight_C4 = 25000`.

### 4. Backward Compatibility
- Blocks containing expired transactions are deemed invalid by upgraded nodes.

### 5. Test Plan
- Mine block at height $H = 100$ containing transaction with `ExpiryHeight = 99` (must be rejected by consensus).
- Mempool eviction test verifying automatic garbage collection of expired transactions.

---

## Proposal C5: Reorg Depth Limit & Coinbase Maturity Invariant

### 1. Problem Statement & Attack Scenario
For Mainnet, the current profile uses `MaxReorganizationDepth = 720` blocks and `CoinbaseMaturity = 100` confirmations. Testnet maturity is 10 confirmations; the reorganization-depth limit is 720 blocks.

**Possible Reorganization Scenario:**
1. A miner receives the current 60 ARMX block reward in block #1000 and has no other funds.
2. At block #1099, that reward has 100 confirmations (`1099 - 1000 + 1`) and is mature under the Mainnet profile. The miner spends 60 ARMX in a transaction included at block #1100.
3. At block #1150, an alternative branch with higher chainwork appears, forking from block #990 (depth: 160 blocks, well within `MaxReorganizationDepth = 720`).
4. The node reorganizes to the alternative branch.
5. In the new branch, block #1000 was mined by a different miner. Assuming no other funds, the original miner cannot fund the transaction on that branch.
6. The reorganization removes the original reward and the block #1100 transaction from canonical history. A merchant relying on that transaction may lose settlement; this is a reorganization risk, not a separate consensus state split.

### 2. Proposed Consensus Rule Change
**Candidate policy:** Compare the maximum accepted fork depth with coinbase maturity, but do not assume that `MaxReorganizationDepth <= CoinbaseMaturity` alone guarantees that a matured coinbase spend cannot be reorganized. The definition of fork depth, tip confirmation count, and the point at which a spend is included must be tested together.
$$\text{MaxReorganizationDepth} \le \text{CoinbaseMaturity}$$
**Implementation:**
- Option A: Reduce `MaxReorganizationDepth` from 720 to a smaller value, recognizing that this also rejects deeper recovery branches after long partitions.
- Option B: Increase Mainnet's `CoinbaseMaturity` from 100 blocks (Testnet currently uses 10), which delays coinbase spending but does not prevent reorganizations of ordinary transfers.

**Recommendation:** Treat the reorganization limit as a recovery-policy tradeoff, not a finality guarantee. A smaller limit reduces the maximum rollback accepted by a node but does not prevent shallower reorganizations, ordinary transaction rollback, or selfish mining.

```go
// Mainnet example only; profile-specific values require separate review.
const (
    CoinbaseMaturity       = 100 // confirmations required to spend coinbase rewards
    MaxReorganizationDepth = 100 // example policy only; does not provide transaction finality
)
```

### 3. Migration & Activation Plan
- Specify whether a changed local reorganization policy takes effect immediately on software upgrade or is height-gated; this is not a consensus activation by itself.

### 4. Backward Compatibility
- Node policy change: nodes simply reject candidate forks that diverge deeper than 100 blocks from the current tip.

### 5. Test Plan
- Simulate fork reorganization attempt at depth 101 blocks (must return `reorg depth exceeds MaxReorganizationDepth`).
- Test fork depths around the configured boundary, including the exact maturity and confirmation-count edge cases.
- Verify that reorganized transactions are removed/replayed consistently and document that confirmation counts are risk guidance, not irreversible settlement.

---

## Summary & Roadmap

| Milestone | Target Testnet Height | Target Date | Scope |
|---|---|---|---|
| **Phase 1** | Block #15,000 | Q1 2027 | C1 (MTP-55), C2 (Monotonic Time), C5 (Reorg Depth Clamp) |
| **Phase 2** | No activation proposed | Q2 2027 | C3 (ML-DSA-65 Encoding and Cross-Network Replay Audit) |
| **Phase 3** | Block #25,000 | Q3 2027 | C4 (Transaction Expiry Height / TTL) |

*Document compiled in strict accordance with Armaxcoin Security Audit Guidelines.*
