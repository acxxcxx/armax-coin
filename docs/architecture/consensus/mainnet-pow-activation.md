# Mainnet PoW Activation Design (Proposal)

**Status: OPEN DECISION — design only.** This document does not change `MainnetProfile`, production activation values, genesis data, or consensus code. Mainnet remains locked and is not ready to launch.

## Scope and compatibility boundary

The proposal preserves the existing Mainnet genesis block and activates ArmaxHash v2 for block 1. It is conditional on owner approval of the activation height, bootstrap target, and the target-calculation code changes described below. No activation parameter should be set from this document alone.

Relevant implementation is in `core/network.go`, `core/genesis.go`, `core/block.go`, `core/target.go`, and `core/consensus.go`. Mainnet currently has both ArmaxHash activation heights set to `-1`. Testnet has v1 at height 520 and v2 at height 2500; its parameters and historical validation must remain unchanged.

## Proposed state transitions

| Height / state | Header hash and PoW | Required target rule | LWMA state |
|---|---|---|---|
| Genesis, 0 | Existing SHA-256d digest and fixed nonce/hash | Existing fixed genesis target | Anchor only; no retarget |
| Block 1 | ArmaxHash v2 | First-v2 bootstrap target, provisionally `MainnetProfile.ArmaxHashPowLimitHex` (currently the difficulty-5 value) | No complete window; bootstrap |
| Blocks 2–60 | ArmaxHash v2 | Carry the bootstrap target forward | Fewer than 60 complete intervals; bootstrap |
| Block 61 | ArmaxHash v2 | First LWMA-1 result from blocks 1–60, subject to the approved target bounds | First complete N=60 window |
| Blocks 62 onward | ArmaxHash v2 | LWMA-1 over the immediately preceding 60 intervals | Normal retargeting |

This table is a proposed Mainnet policy, not current production behavior. For height 61, the window consists of blocks 0 through 60 as timestamps/endpoints, with blocks 1 through 60 contributing the 60 solve intervals and sampled targets. The bootstrap target for blocks 1–60 ensures that the genesis SHA-256d target does not contaminate the first v2 target average.

## Exact proposed behavior

### Fixed genesis vector

The immutable Mainnet genesis fields remain timestamp `1710000000`, nonce `7`, difficulty `1`, target `0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff`, the existing all-zero parent hash, Merkle root `ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76`, and SHA-256d block hash `0820d42962071e01c8a7d2e6a3aa5640a17f8a60e9006bf59b6e76ac0001535e`. These are invariants, not proposed replacements. The header encoding remains the existing canonical `index:timestamp:prevHash:merkleRoot:miner:nonce:target` byte sequence.

### Genesis and header hashing

The canonical genesis header remains exactly as encoded today: its timestamp, parent hash, Merkle root, miner, nonce, target, fixed SHA-256d hash, and genesis transaction remain unchanged. Activation is defined for positive block heights; height 0 is permanently SHA-256d even if an activation override is misconfigured to zero or a negative value.

`CalculateHashWithError` and `PoWAlgorithm` must agree for each block height. With the proposal, they return SHA-256d for height 0 and ArmaxHash v2 for every Mainnet height `>= 1`. No fallback from v2 hashing errors to SHA-256d is allowed.

### Difficulty bootstrap and LWMA-1

The first post-genesis block uses the v2 bootstrap target `00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (the current Mainnet profile's difficulty-5 value), not the genesis target. Until 60 post-genesis solve intervals exist, the target is carried forward unchanged. At height 61, LWMA-1 uses exactly N=60 intervals, `Timestamp[i] - Timestamp[i-1]` for `i=1..60`, and target samples from blocks 1–60. Existing timestamp clamping and weighted-time dampening remain in force.

The current target routine selects the bootstrap branch using `IsArmaxHashActive`, which is false for Mainnet when the v1 activation is `-1`. A v2-only activation therefore needs a carefully scoped dispatch change: use the effective active PoW family (v1 or v2) for the initial target reset, bootstrap carry-forward, target sample substitution, and post-activation target floor. Do not silently set v1 activation to 1 merely to reuse this logic: that would make block 1 v1, not v2. Testnet v1/v2 behavior must be byte-for-byte unchanged.

**OPEN DECISION:** Approve the exact bootstrap target and subsequent maximum-easiest target. The profile currently calls `ArmaxHashPowLimitHex` the initial bootstrap ceiling, while post-activation LWMA logic permits targets as easy as difficulty 1. The proposal above retains that existing distinction, but it must be confirmed as intentional for v2-only Mainnet activation.

### Validation, mining, and genesis

`ValidateGenesisBlockForNetwork` continues to validate the existing fixed genesis independently of the block-1 activation. Normal block validation, chain verification, mining, templates, sync, and fork-choice must all dispatch on the same height-dependent PoW predicate and use the exact expected target. Mainnet validation must reject a SHA-256d block at height 1 and reject a v2 block at height 0.

The existing genesis hash can remain unchanged while block 1 uses ArmaxHash v2, provided activation remains positive-height and genesis validation preserves its current fixed-block checks. This is a source-level compatibility conclusion; no production block-1 vector exists yet.

### Testnet and chainwork

No Testnet activation, target, genesis, wire message, serialization, or historical block may change. Testnet height 0 stays SHA-256d; heights before 520 remain legacy SHA-256d, v1 starts at 520, and v2 starts at 2500, subject to existing production rules.

Chainwork continues to use the target-based work formula. The first-v2 target must be included exactly as stored in each block. A reorganization crossing an activation boundary must validate every block under its own network profile and height before comparing cumulative work. There must be no special work discount or retrospective reinterpretation of pre-activation blocks.

## Golden vectors and regression tests

Before any activation change:

1. Freeze the current Mainnet genesis vector: canonical header bytes, SHA-256d hash, nonce, Merkle root, target, and the result of `ValidateGenesisBlockForNetwork`.
2. Freeze independent ArmaxHash v2 vectors for the exact canonical block-header encoding, including network ID/domain input, using the existing `armaxhash` test-vector conventions.
3. Generate and independently verify a block-1 vector from the unchanged Mainnet genesis, approved bootstrap target, v2 digest, and a nonce satisfying the target.
4. Record block-2 and block-60 expected targets and hashes; assert the bootstrap target remains stable.
5. Record the block-61 LWMA result from a deterministic 61-block timestamp/target fixture with exactly 60 intervals; test boundary cases at heights 60, 61, and 62.
6. Exercise the real production paths: `MineBlockContext`, `ValidateAndAddBlock`, `IsChainValid`, block-range ingestion, template generation, and reorganization/fork choice.
7. Assert height-0 v2 is inactive, height-1 v2 is active, height-1 SHA-256d is rejected, and an ArmaxHash computation error cannot fall back to another algorithm.
8. Run existing Testnet activation, golden-vector, migration, and historical-chain tests unchanged, including tests at heights 519/520/2499/2500.
9. Run race-enabled package tests and a local-only, no-public-peer Mainnet simulation after the activation code is explicitly approved.

## Upgrade and versioning risks

Changing the Mainnet activation height is a consensus change: nodes that disagree on algorithm or target will reject blocks and can split into incompatible histories. A software version bump alone does not coordinate activation. Before Mainnet exists, publish a versioned consensus specification, freeze vectors, and require every planned Mainnet binary to agree on activation, target bootstrap, and boundary tests. Do not apply this policy to Testnet or an existing database.
