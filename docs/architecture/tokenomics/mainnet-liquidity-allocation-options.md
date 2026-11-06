# Mainnet Liquidity & Tokenomics Options (Internal Design)

**Status: DRAFT / NOT APPROVED. Options under evaluation, not commitments. Mainnet is locked and has not launched.**

---

## 1. Immutable Consensus Policy

The project strategy is to preserve the existing Mainnet tokenomics exactly as coded:

| Parameter | Existing source value | Treatment |
|---|---:|---|
| Maximum supply | 126,000,000 ARMX | Unchanged |
| Initial scheduled block reward | 60 ARMX | Unchanged |
| Halving interval | 1,050,000 blocks | Unchanged |
| Target block time | 120 seconds | Unchanged |
| Genesis | Existing configured genesis/hash | Unchanged |
| First-100-block reward override | None authorized | Do not introduce |
| Project genesis allocation/premine | None added by this strategy | Do not introduce |
| Mining destination rule | Miner selects coinbase recipient under existing rules | Standard BIP-173 Bech32 address format |

These are verified source parameters. `core.MainnetLaunched` remains false. The Mainnet genesis constructor contains one 60 ARMX coinbase transaction whose recipient is the canonical Bech32 address encoding a zero 32-byte payload (`armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd`), representing burned genesis coins.

---

## 2. Developer Early-Mining Phase Specification

Before public mainnet opening, the developers will mine blocks 1 through 16,667. Developers will mine all blocks in this range.

- **Stop Rule:** Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first.
- **Emission Math:** At 60 ARMX per block, 16,667 blocks yield up to 1,000,020 ARMX (16,666 blocks = 999,960 ARMX; block 16,667 yields 1,000,020 ARMX due to discrete block subsidies).
- **Estimated Duration:** Estimated duration is approximately 23.15 days at 120-second target block time; actual duration depends on hashrate and difficulty.
- **Public Mainnet Opening:** TBA.
- **Treasury Address:** Treasury address will be published before mainnet launch.
- **Risks / Criticism:** Early developer mining represents an initial point of centralization and grants the developer team a material fraction (~0.79% of the total 126,000,000 ARMX supply) before the public opening.

---

## 3. Liquidity and Bridge Evaluation

Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch.

The following architectural principles are under evaluation:
1. **Treasury Custody:** ARMX earned by developer miners will be held in the published treasury address and becomes spendable only after 100-confirmation coinbase maturity.
2. **Bridge Backing:** Native ARMX must be locked into verified custody before any corresponding wrapped asset is minted on destination chains.
3. **Decentralized Liquidity:** Options for bootstrapping trading pairs remain under technical and economic assessment.

---

## 4. Explicit Non-Goals

- Changing maximum supply, rewards, halving cadence, target time, genesis, consensus activation, coinbase validation, or emission.
- Introducing privileged block production, guaranteed mining income, or enforced protocol-level coinbase destinations.
- Secret or unannounced developer allocations outside of the transparent early-mining program.

---

## 5. Open Decisions and Blockers

- Publishing the official treasury address prior to mainnet launch.
- Finalizing the exact hardware and deployment schedule for developer-operated mining upon Mainnet launch.
- Multisig custody setup and architectural specification for any eventual liquidity pairing.
- Native-chain escrow and proof-of-lock audit for the bridge relayer.
- Independent security review and user-facing disclosure before any bridge or DEX deployment.
