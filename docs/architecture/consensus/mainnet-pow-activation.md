# Mainnet PoW & Consensus Staging Profile (Provisional — Locked & Subject to Change)

**Status: PROVISIONAL PRE-LAUNCH STAGING PROFILE (`core.MainnetLaunched = false`).** Mainnet remains strictly locked: invoking `-network mainnet` fails closed (`"Mainnet has not been launched; please use -network testnet"`) until all security, open independent review, and operational gates in [`../../MAINNET_READINESS.md`](../../MAINNET_READINESS.md) are completed and formally frozen.

> [!IMPORTANT]
> The parameter values documented below reflect the **current provisional staging profile** in `MainnetProfile` for isolated local dry-run testing (`-local-mainnet`) and code readiness verification. **`v1.0.12` is a Public Testnet (`ARMX-TESTNET-1`) software release and does NOT constitute a finalized or binding Mainnet launch specification.** All Mainnet consensus, PoW, difficulty, and launch parameters remain subject to revision prior to official Mainnet launch freeze.

## Current Provisional Staging Configuration (`ARMX-MAINNET-1`)

The staging profile preserves the immutable Mainnet SHA-256d genesis block (`#0`) while configuring current Layer-1 consensus primitives on Block `#1+` across `core/network.go`, `core/genesis.go`, `core/block.go`, `core/target.go`, `core/asert.go`, and `core/consensus.go`. Public Testnet parameters (`v1 = 520`, `v2 = 2500`, `C1/C2 = 8000`, `TEDR = 13293`, `ASERT = 13335`, `C6/TxBinaryV1 = 20000`) and historical Testnet validation remain byte-for-byte unaffected.

| Height / parameter | Header hash / PoW | Difficulty / consensus rules | Current staging setting (`MainnetProfile`, subject to change) |
|---|---|---|---|
| Genesis, `#0` | Immutable SHA-256d digest and fixed nonce (`7`) / hash (`0820d429...`) | Fixed Difficulty 1 genesis target (`GenesisTargetHex`); ASERT & `TxBinaryV1` anchor | `Checkpoints[0] = GenesisHash` |
| Blocks `#1+` (PoW) | ArmaxHash v2 Stream-Fast (legacy Argon2id v1 skipped: `-1`) | Bootstrap target Difficulty 5 (`ArmaxHashPowLimitHex`: `00000fff...`) | `ArmaxHashV2ActivationHeight: 1`, `ArmaxHashActivationHeight: -1` |
| Blocks `#1+` (DAA) | — | Armax-ASERT-AW (`aserti3-2d`, $\tau = 3600\text{s}$, 4h Surge Cap `14400s`) anchored to Genesis `#0` at `ArmaxHashPowLimitHex`; TEDR disabled | `ASERTActivationHeight: 0`, `AntiStallActivationHeight: -1` |
| Blocks `#1+` (Time / Size) | — | C1/C2 MTP-55 non-decreasing timestamps, C6 10-minute (`+600s`) future limit, and `TxBinaryV1` (`BinarySize()`) active from Block `#1` (`TxBinaryV1` `#0+`) | `TimestampRulesActivationHeight: 0`, `FutureTimestampLimitActivationHeight: 0`, `BlockBinarySizeActivationHeight: 0` |

## Behavior Under Current Staging Profile

### Fixed genesis vector

The immutable Mainnet genesis fields remain timestamp `1710000000`, nonce `7`, difficulty `1`, target `0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff`, the existing all-zero parent hash, Merkle root `ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76`, and SHA-256d block hash `0820d42962071e01c8a7d2e6a3aa5640a17f8a60e9006bf59b6e76ac0001535e`. These are invariants, not proposed replacements. The header encoding remains the existing canonical `index:timestamp:prevHash:merkleRoot:miner:nonce:target` byte sequence.

### Genesis and header hashing

The canonical genesis header remains as encoded today: its timestamp, parent hash, Merkle root, miner, nonce, target, fixed SHA-256d hash, and genesis transaction remain unchanged. PoW activation applies only to positive block heights; height `0` remains permanently SHA-256d even if an override is set to zero or a negative value.

`CalculateHashWithError` and `PoWAlgorithm` agree for every height: under the current staging profile, they return SHA-256d for height `0` and ArmaxHash v2 for Mainnet heights `>= 1`, without falling back from v2 hashing errors to SHA-256d.

### Difficulty Bootstrap and Armax-ASERT-AW

Under the current staging configuration, the first post-genesis block uses the v2 bootstrap target `00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (`MainnetProfile.ArmaxHashPowLimitHex`, Difficulty 5) rather than the Difficulty 1 SHA-256d genesis target. `ASERTActivationHeight = 0` anchors `ARMX-MAINNET-1` to Genesis `#0` using `GetArmaxHashPowLimit()` (`Difficulty 5`) via `ResolveASERTAnchor` (`core/asert.go`), applying integer `aserti3-2d` (`ASERT_TAU = 3600s`, `MaxASERTBlockInterval = 14400s`, TEDR disabled via `AntiStallActivationHeight = -1`) from Block `#1` onward without LWMA-1 or SHA-256d scale contamination.

### Validation, Mining, and Genesis

`ValidateGenesisBlockForNetwork` validates the immutable SHA-256d Genesis `#0` independently of Block `#1` ArmaxHash v2 activation. Normal block validation, chain verification, mining, templates, P2P `PUSH_BLOCKS` (`headerPoWHash` with `IsAnyArmaxHashActive`), sync, and fork-choice (`validateBlockContext` with profile-isolated `calculateExpectedTargetForProfile` and `CalculateHashWithErrorForProfile`) all dispatch consistently and reject legacy SHA-256d hashes at height `>= 1` in isolated `-local-mainnet` simulations.

### Testnet Isolation

No Testnet activation height, target, genesis, wire message, serialization, or historical block rule is altered (`v1 = 520`, `v2 = 2500`, `C1/C2 = 8000`, `TEDR = 13293`, `ASERT = 13335`, `C6/TxBinaryV1 = 20000`). `core.GetEffectiveArmaxHashActivationHeight()` and `core.IsAnyArmaxHashActive()` preserve Testnet's existing height-520/2500 behavior identically.

## Regression Verification Tests

Locked staging isolation and Testnet preservation are verified by:
1. Frozen Testnet consensus and genesis checks (`TestConsensusParametersFrozen`, `TestL1_6_MainnetGenesisUntouched`, `TestGenesisBlockIntegrity`).
2. Locked profile, Genesis `#0` -> Block `#1` ASERT Difficulty 5 anchor, SHA-256d rejection at Block `#1`, C6 `+600s`, and `TxBinaryV1` checks (`TestMainnetRemainLocked_WithGenesisASERTAndBinaryReady` in `core/transaction_binary_test.go`, `TestTestnetProfileASERTActivationHeight13335`, `TestFutureTimestampLimit_ProfileDefaults`).
3. Multi-node isolated `-local-mainnet` lifecycle and 3-node simulation (`TestLocalMainnet_MiningStorageRestartLifecycle` and `TestLocalMainnet_ThreeNodeFullSimulation` in `node/`) verifying isolated mining, reorgs, restart persistence, and loopback RPC/P2P isolation while `-network mainnet` remains fail-closed without `-local-mainnet`.

## Release Governance & Mutability Notice

Mainnet has not launched (`core.MainnetLaunched = false`). Any transition toward Mainnet release requires independent satisfaction of all gates in [`../../MAINNET_READINESS.md`](../../MAINNET_READINESS.md), an explicit Mainnet configuration freeze review, and coordinated seed-operator release approval. Until that freeze occurs, the parameters in `MainnetProfile` may be changed.
