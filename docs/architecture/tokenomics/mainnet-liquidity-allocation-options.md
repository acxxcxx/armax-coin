# Mainnet Tokenomics Preservation and Mining-Funded Treasury Objective

**Status: POLICY DOCUMENTATION — no consensus change or treasury allocation is proposed or implemented here. Mainnet is locked and has not launched.**

## Immutable current policy

The project strategy is to preserve the existing Mainnet tokenomics exactly as coded:

| Parameter | Existing source value | Phase 5 treatment |
|---|---:|---|
| Maximum supply | 126,000,000 ARMX | Unchanged |
| Initial scheduled block reward | 60 ARMX | Unchanged; same reward rules for all miners |
| Halving interval | 1,050,000 blocks | Unchanged |
| Target block time | 120 seconds | Unchanged |
| Genesis | Existing configured genesis/hash | Unchanged |
| First-100-block reward override | None authorized | Do not introduce |
| Project genesis allocation/premine | None added by this strategy | Do not introduce |
| Mining destination rule | Miner selects coinbase recipient under existing rules | No project-specific destination enforced |

These are verified source parameters, not a claim that Mainnet is live. `core.MainnetLaunched` remains false. The Mainnet profile's ArmaxHash activation settings remain disabled; this document does not modify consensus or activation parameters.

The Mainnet genesis constructor contains one 60 ARMX coinbase transaction whose recipient is the canonical Bech32 address encoding a zero 32-byte payload. Source comments identify this as the burn address. This is a source-level genesis fact; it does not prove external agreements, key-generation history, or any off-chain asset arrangement. Therefore current-facing material should not make a blanket “zero-premine” claim. State only what code establishes: no separate project-treasury genesis output is defined in the current genesis constructor.

## Project mining objective

After launch and only if mining is separately authorized, project-operated miners may attempt to accumulate up to 1,000,000 ARMX through valid ordinary mining. They must use the same consensus rules, difficulty, subsidy, block validation, and coinbase rules as all miners. Rewards must be publicly observable on-chain. No block is guaranteed to be won by the project.

This figure is a long-term operational target, not a protocol variable, supply reservation, genesis allocation, premine, special mining period, or promise that the project will reach or retain that amount. No automatic split into bridge backing or DEX liquidity is approved. See the [mining accumulation model](mining-accumulation-plan.md).

## Bridge and liquidity are separate decisions

1. **Treasury holding:** ARMX earned by project-operated miners remains a project-controlled holding only if the miner's valid coinbase reward is paid to a project-controlled address and later becomes spendable under existing maturity rules.
2. **Bridge backing:** Native ARMX must be placed under a specifically approved custody mechanism and verified as locked before any corresponding wARMX is minted. A treasury balance, mining target, attestor statement, or DEX LP position is not proof of lock.
3. **DEX liquidity:** wARMX deposited into an AMM is still outstanding wARMX and must remain fully covered by verified native backing. Quote assets are a separate contribution and do not back wARMX. Locking LP tokens does not lock or prove the underlying native ARMX backing.
4. **Cross-chain supply:** Aggregate wARMX outstanding on BNB Smart Chain and Ethereum must never exceed globally verified native backing. Independent contracts cannot atomically observe each other's supply; a production mechanism must enforce conservative per-chain allocations under a shared backing cap or use independently verifiable cross-chain allocation state. The current prototype does neither.

BNB Smart Chain is the first proposed EVM destination; Ethereum is later and remains a separate integration. Neither order nor deployment is approved by this document. Do not deploy, mint, or seed liquidity based only on the 1,000,000 ARMX mining objective.

## Explicit non-goals

- Changing maximum supply, rewards, halving cadence, target time, genesis, consensus activation, coinbase validation, or emission.
- Introducing a first-100-block reward, privileged block production, guaranteed mining income, or enforced project coinbase destination.
- Creating or promising any protocol allocation or guaranteed pool reserve.
- Claiming zero premine without qualification beyond the verifiable source-level genesis facts above.
- Selecting an initial bridge reserve, pool size, quote asset, price, or launch date.

## Open decisions and blockers

- Whether, when, and by whom project-operated mining may be conducted after a future Mainnet launch.
- Treasury address governance, key custody, accounting disclosure, reward-maturity and reorg reconciliation.
- Native-chain escrow and proof-of-lock design; current source has no proven production multisig/threshold escrow.
- Global BSC/Ethereum backing allocation and supply enforcement.
- Independent security review and user-facing disclosure before any bridge or AMM action.
