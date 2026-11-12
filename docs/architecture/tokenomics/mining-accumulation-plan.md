# Developer Early-Mining Plan

**Status: OPERATIONAL POLICY — Mainnet is locked and has not launched.**

---

## 1. Scope & Execution

Before public mainnet opening, the developers will mine blocks 1 through 16,667. Developers will mine all blocks in this range.

- **Stop Rule:** Stop at block 16,667 or when total developer mining rewards reach 1,000,000 ARMX, whichever comes first.
- **Emission Math:** At 60 ARMX per block, 16,667 blocks yield up to 1,000,020 ARMX (16,666 blocks = 999,960 ARMX; block 16,667 yields 1,000,020 ARMX due to discrete block subsidies, representing ~0.7937% of the total 126,000,000 ARMX max supply). Because block subsidies are issued in discrete 60 ARMX increments, reaching the nominal 1,000,000 ARMX policy limit and reaching block 16,667 occur at the exact same block (Block 16,667).
- **Estimated Duration:** Estimated duration is approximately 23.15 days at 120-second target block time; actual duration depends on hashrate and difficulty.
- **Public Mainnet Opening:** TBA.
- **Treasury Address:** Treasury address will be published before mainnet launch.

---

## 2. Liquidity & Bridge Status

Liquidity and bridge design are not finalized and will be announced, with full technical details, before mainnet launch.

- The developer early-mining phase is intended to accumulate base inventory for eventual network bootstrapping and ecosystem liquidity. For the proposed allocation framework, see [`TREASURY_MIGRATION_PLAN.md`](../../../TREASURY_MIGRATION_PLAN.md).
- No commitments regarding specific decentralized exchanges, lockup structures, or bridging mechanisms are finalized at this time.
- All bridge and liquidity architectures remain subject to code completion, testing, and independent security auditing prior to any mainnet deployment.

---

## 3. Risks & Criticism

- **Initial Centralization:** Early developer mining represents an initial point of centralization and grants the developer team a material fraction (~0.79% of the total 126,000,000 ARMX supply) before the public opening.
- **Early Supply Distribution:** During this window, all block subsidies accrue to the developer treasury address rather than a distributed set of public participants.
- **Transition Risk:** Once block 16,667 is reached, the network opens to the public. If external hashrate joins abruptly or difficulty adjusts slowly under LWMA-1, block intervals may fluctuate during early public transition.
