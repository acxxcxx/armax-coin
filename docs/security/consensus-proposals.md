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
| **C1** | Extended MTP Window & Max-Time-Past Guard | Header Timestamp Validation | Hard Fork | Proposed Block #15,000 |
| **C2** | Strict Monotonic Timestamps for LWMA-1 Difficulty | LWMA Difficulty Calculation | Hard Fork | Proposed Block #15,000 |
| **C3** | ML-DSA-65 Canonicality & Network Preimage Binding | Cryptographic Verification | Hard Fork | Proposed Block #20,000 |
| **C4** | Transaction Time-To-Live (TTL / Expiry Height) | Mempool & Transaction Structure | Hard Fork | Proposed Block #25,000 |
| **C5** | Reorg Depth Limit & Coinbase Maturity Invariant | Fork Resolution & State Replay | Consensus Rule | Proposed Block #15,000 |

---

## Proposal C1: Extended MTP Window & Max-Time-Past Guard

### 1. Problem Statement & Attack Scenario
The current consensus rules (`core/consensus.go:validateBlockContext`) validate block timestamps using:
1. **Median Time Past 11 (MTP-11):** `block.Timestamp > CalculateMedianTimePast(chainHistory, 11)`
2. **Future Time Limit:** `block.Timestamp <= time.Now() + 2 hours`

**Vulnerability:**
With an MTP window of only 11 blocks (~22 minutes at 120s target spacing), a coalition of colluding miners controlling ~25-35% of network hashrate can execute a "timewarp" attack:
- Miners artificially hold timestamps at `MTP + 1s` for consecutive blocks.
- The LWMA difficulty algorithm perceives a sudden spike in block generation speed (time delta compressed to near zero), followed by artificial delays, oscillating difficulty downwards.
- Furthermore, there is no **Max-Time-Past** ceiling (how far in the past a block timestamp can be relative to the active network time), enabling historical timestamp manipulation during long network partitions.

### 2. Proposed Consensus Rule Change
1. **Expand MTP Window to 55 Blocks (MTP-55):**
   - Sample the median timestamp across the previous 55 blocks (~110 minutes).
   - This matches proven defenses in modern PoW networks (e.g., Bitcoin Cash, DigiByte) against short-window timestamp manipulation.
2. **Enforce Max Past Timestamp Boundary:**
   - Require `block.Timestamp >= prev.Timestamp - MaxClockDrift` where `MaxClockDrift = 1800` (30 minutes).

```go
// Proposed Consensus Rule C1 Specification:
func ValidateBlockTimestampC1(b *Block, prev *Block, chainHistory []Block) error {
    mtp55 := CalculateMedianTimePast(chainHistory, 55)
    if b.Timestamp <= mtp55 {
        return fmt.Errorf("block timestamp %d <= MTP-55 %d", b.Timestamp, mtp55)
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

## Proposal C2: Strict Monotonic Timestamps for LWMA-1 Difficulty

### 1. Problem Statement & Attack Scenario
Currently, LWMA-1 calculates difficulty using the time intervals between adjacent blocks:
```go
solvetime := chain[i].Timestamp - chain[i-1].Timestamp
```
Because consensus currently only requires `Timestamp > MTP-11`, it is mathematically possible for `block[i].Timestamp < block[i-1].Timestamp` (negative solvetime).
When `solvetime < 0`:
- LWMA-1 clamps `solvetime` to a minimum value or incurs mathematical instability, distorting the difficulty target.
- Miners can deliberately mine blocks out-of-order within the MTP window to game difficulty downward.

### 2. Proposed Consensus Rule Change
Enforce **strict weak monotonicity** for block timestamps from activation height:
```go
// Proposed Consensus Rule C2 Specification:
if b.Index >= ActivationHeight_C2 {
    if b.Timestamp < prev.Timestamp {
        return fmt.Errorf("block #%d timestamp %d is earlier than previous block timestamp %d (monotonicity violation)",
            b.Index, b.Timestamp, prev.Timestamp)
    }
}
```
Optionally require `b.Timestamp >= prev.Timestamp + 1` to prevent zero-second block intervals.

### 3. Migration & Activation Plan
- Combined with Proposal C1 at `ActivationHeight = 15000`.

### 4. Backward Compatibility
- **Hard Fork:** Blocks with `Timestamp < prev.Timestamp` (valid under legacy rules) are rejected by upgraded nodes. Historical blocks (0..14999) remain valid under legacy rules.

### 5. Test Plan
- Regression test submitting candidate block with `Timestamp == prev.Timestamp - 1` (rejected post-activation, accepted pre-activation).
- Verification that LWMA solvetime inputs are strictly non-negative.

---

## Proposal C3: ML-DSA-65 Canonicality & Network Preimage Binding

### 1. Problem Statement & Attack Scenario
Armaxcoin uses NIST FIPS 204 ML-DSA-65 post-quantum digital signatures.
Two theoretical malleability risks exist:
1. **Signature Non-Canonicality:** ML-DSA-65 signatures contain polynomial vectors $(z, h, c)$. If the verification library accepts non-canonical encodings of coefficients (e.g. non-reduced modular arithmetic or alternate byte packings), an attacker can alter the signature bytes without invalidating the cryptographic signature. This changes the transaction hash (`tx.HashHex()`) without invalidating the witness, creating transaction malleability.
2. **Cross-Network Replay:** If a transaction signed on `ARMX-TESTNET-1` does not strictly bind the Network ID into the cryptographic signature preimage, the same signature could be replayed on `ARMX-MAINNET-1` once launched.

### 2. Proposed Consensus Rule Change
1. **Explicit Preimage Network ID Binding:**
   The canonical signing preimage must be formalized as:
   $$\text{Preimage} = \text{SHA-256}(\text{NetworkID} \parallel \text{SigAlg} \parallel \text{SenderAddress} \parallel \text{RecipientAddress} \parallel \text{Amount} \parallel \text{Fee} \parallel \text{Nonce})$$
2. **Canonical Witness Hash Verification:**
   Strictly verify that the ML-DSA-65 signature length is exactly 3309 bytes and passes canonical boundary reduction checks before caching or mempool acceptance.

### 3. Migration & Activation Plan
- Scheduled for activation height `ActivationHeight_C3 = 20000`.

### 4. Backward Compatibility
- **Hard Fork:** Transactions using the old preimage scheme will fail signature verification on post-activation nodes.

### 5. Test Plan
- Test attempting to replay a testnet-signed transaction on a regtest/mainnet node (must fail verification).
- Fuzzing of signature byte encodings to ensure alternate polynomial packings are rejected.

---

## Proposal C4: Transaction Expiry (Time-To-Live / Block Height Expiration)

### 1. Problem Statement & Attack Scenario
Currently, transactions in Armaxcoin do not expire.
If a user submits a transaction with low fee during a period of high network activity, the transaction may remain unconfirmed in mempools indefinitely.
- If the sender forgets about it and transfers funds months later, an adversary can broadcast the stale transaction, executing an unexpected transfer.
- Mempools can become bloated with ancient, unexecutable transactions.

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
Currently:
- `MaxReorganizationDepth = 720` blocks (~24 hours).
- `CoinbaseMaturity = 100` blocks (~3.3 hours).

**Catastrophic State Split Scenario:**
1. Miner mines block #1000 and receives 100 ARMX block reward.
2. At block #1100 (100 confirmations), the coinbase reward matures. The miner transfers 100 ARMX to a merchant in transaction $T_x$.
3. At block #1150, an alternative branch with higher chainwork appears, forking from block #990 (depth: 160 blocks, well within `MaxReorganizationDepth = 720`).
4. The node reorganizes to the alternative branch.
5. In the new branch, block #1000 was mined by a different miner. Therefore, the original miner's account at block #1100 has a balance of 0 ARMX!
6. $T_x$ now attempts to double-spend non-existent funds. The merchant has already shipped physical goods based on 50 confirmations of $T_x$.

### 2. Proposed Consensus Rule Change
**Rule:** Reorganization depth must NEVER exceed the coinbase maturity period:
$$\text{MaxReorganizationDepth} \le \text{CoinbaseMaturity}$$
**Implementation:**
- Option A: Reduce `MaxReorganizationDepth` from 720 to 100 blocks.
- Option B: Increase `CoinbaseMaturity` from 100 to 720 blocks.

**Recommendation:** Option A (Clamp `MaxReorganizationDepth = 100`) is strongly recommended. 100 blocks (3.3 hours) provides robust protection against selfish mining while guaranteeing that matured coinbase expenditures can NEVER be invalidated by a reorganization.

```go
// Proposed Consensus Constant Alignment:
const (
    CoinbaseMaturity       = 100 // confirmations required to spend coinbase rewards
    MaxReorganizationDepth = 100 // strictly <= CoinbaseMaturity [C5]
)
```

### 3. Migration & Activation Plan
- Can be activated at `ActivationHeight_C5 = 15000` alongside C1 and C2.

### 4. Backward Compatibility
- Node policy change: nodes simply reject candidate forks that diverge deeper than 100 blocks from the current tip.

### 5. Test Plan
- Simulate fork reorganization attempt at depth 101 blocks (must return `reorg depth exceeds MaxReorganizationDepth`).
- Verify that no spent coinbase output can ever be rolled back past maturity.

---

## Summary & Roadmap

| Milestone | Target Testnet Height | Target Date | Scope |
|---|---|---|---|
| **Phase 1** | Block #15,000 | Q1 2027 | C1 (MTP-55), C2 (Monotonic Time), C5 (Reorg Depth Clamp) |
| **Phase 2** | Block #20,000 | Q2 2027 | C3 (ML-DSA-65 Canonicality & Network Preimage Binding) |
| **Phase 3** | Block #25,000 | Q3 2027 | C4 (Transaction Expiry Height / TTL) |

*Document compiled in strict accordance with Armaxcoin Security Audit Guidelines.*
