# Armaxcoin Mainnet Tokenomics & Testnet Migration Proposal

**Document Status:** PROPOSAL / AWAITING HUMAN DECISION  
**Target Architecture:** Armaxcoin Mainnet Protocol (`ARMX-MAINNET-1`) & Multi-Repo Ecosystem  
**Target Scope:** `armax_coin`, `armax_explorer`, `armax_android`, and Ecosystem Documentation  
**Date:** 2026-10-24  
**Author:** Armaxcoin Core Architecture Working Group  

---

## 1. Executive Summary & Repository Inventory

This document presents the Phase 1 Audit and Phase 2 Technical Proposal for the coordinated migration of Armaxcoin from the experimental public testnet (`ARMX-TESTNET-1`) to the production-ready mainnet (`ARMX-MAINNET-1`).

### 1.1 Repository Inventory & Initial Commits
The Armaxcoin project is partitioned across three separate Git repositories. Prior to executing any code modifications, dedicated local migration branches were created across all three repositories:

| Repository | Path | Base Branch | Migration Branch | Initial Commit SHA |
|---|---|---|---|---|
| **`armax_coin`** | `d:\new_armax\armax_coin` | `main` | `feat/mainnet-tokenomics-migration` | `32671920d8628f0833765538ff8b2f4bde8c4d05` |
| **`armax_explorer`** | `d:\new_armax\armax_explorer` | `main` | `feat/mainnet-tokenomics-migration` | `67e4f6e347c9b80b484f407e1afb18939b07a33e` |
| **`armax_android`** | `d:\new_armax\armax_android` | `main` | `feat/mainnet-tokenomics-migration` | `62dcbeaca84a13350439aa61f1b40218b241f3f4` |

> [!IMPORTANT]
> **Safety Constraints:**
> - Under strict developer constraints, **no commits will be pushed to remote GitHub repositories**. All commits remain local.
> - The live public testnet (`ARMX-TESTNET-1`) remains fully operational and backwards compatible. Its consensus parameters, genesis hash, network magic, and seed nodes are preserved.
> - Mainnet activation remains strictly gated (`MainnetLaunched = false`) until all human governance decisions are formally ratified and automated verification suites pass.

---

## 2. Phase 1 — Comprehensive Architecture & Consensus Audit

### 2.1 Current Tokenomics & Mathematical Invariants
In the existing codebase (`core/blockchain.go`, `core/genesis.go`, `core/supply_genesis_accounting_test.go`):
- `MaxSupply`: `int64(126_000_000) * CoinUnit` = `12,600,000,000,000,000` Armoids.
- `CoinUnit`: `100_000_000` (8 decimal places).
- `InitialReward`: `60 * CoinUnit` (`6,000,000,000` Armoids).
- `HalvingInterval`: `1,050,000` blocks (~4 years on 120s block time).
- `GenesisReward`: Exactly 60 ARMX allocated to the canonical burn address `GenesisMiner` (`armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd`).

#### Critical Mathematical Finding:
In the current design, PoW mining emission is calculated via geometric series:
$$\sum_{e=0}^{32} (60 \cdot 10^8 \gg e) \times 1,050,000 = 125,999,999.87400000 \text{ AMX}$$
This occupies the entire 126,000,000 AMX cap. If a **5,000,000 AMX premine** is introduced at Genesis without adjusting the PoW subsidy schedule, the total issuance would reach $130,999,999.874 \text{ AMX}$, violating the hard cap. If a hard cap cut-off is applied without adjusting the initial block reward, PoW emission would abruptly terminate at block ~1,875,000 midway through Era 1/2, creating an unexpected 0-reward cliff for miners.

### 2.2 Genesis Block Architecture
- `core/genesis.go`: Currently only contains a single coinbase transaction of 60 ARMX to `GenesisMiner`.
- No native support exists in `GetMainnetGenesisBlock()` for multi-output allocations or predefined treasury/vesting outputs.
- `ValidateGenesisBlock` strictly validates block hash, Merkle root, timestamp, and single coinbase transaction format.

### 2.3 Account State & Vesting Mechanism
- `core/state.go`: `AccountState` currently tracks:
  ```go
  type AccountState struct {
      Balance  int64            `json:"balance"`
      Nonce    int64            `json:"nonce"`
      Immature []ImmatureReward `json:"immature,omitempty"`
  }
  ```
- **Consensus Deficit:** Armaxcoin does not support Bitcoin Script (`OP_CHECKLOCKTIMEVERIFY` / `OP_CHECKSEQUENCEVERIFY`) or EVM smart contracts.
- Currently, there is **zero native on-chain time-lock or vesting enforcement** on standard account balances.
- `core/htlc.go` exists as an experimental off-chain library, but is not evaluated or enforced during `StateManager.VerifyAndApplyBlock`.
- Therefore, vesting must be implemented as a first-class Layer-1 consensus primitive in `AccountState` and `StateManager`.

### 2.4 RPC, API & Explorer State
- `node/server.go`:
  - `/api/supply` and `/stats` return `circulating_supply`, `total_supply`, and `max_supply`.
  - Currently, `circulating_supply` is assumed to equal `total_supply`. No accounting exists for `locked_supply` or `vested_supply`.
  - `/balance` returns `balance`, `pending_balance`, `immature_balance`, and `spendable_balance`. It lacks `locked_vesting_balance` and `vested_balance`.
- `armax_explorer`:
  - `web/chainParams.js` hardcodes `MAX_SUPPLY_ARMX: 126000000`.
  - `web/index.html` and `web/whitepaper.html` state "Zero premine, zero founder/VC allocation".
  - Explorer UI lacks vesting schedule inspectors, lockup countdowns, and treasury allocation views.

### 2.5 Android Mobile Client State
- `armax_android`:
  - `BalanceInfo.kt` tracks `balance`, `pendingBalance`, `immatureBalance`, and `spendableBalance`.
  - `WalletDashboardScreen.kt` displays total balance and spendable balance.
  - No interface exists for displaying vesting schedules, cliff countdowns, or differentiating locked non-transferable tokens.

---

## 3. Phase 2 — Proposed Target Tokenomics Specification

### 3.1 Exact Integer Accounting in Base Units (Armoids)
All consensus rules and internal state balances must operate exclusively using 64-bit signed integers (`int64`), with 1 AMX = $10^8$ Armoids. Floating-point arithmetic is strictly prohibited in consensus.

| Allocation Category | AMX Amount | Armoids (Integer Base Units) | Percentage of Max Supply |
|---|---:|---:|---:|
| **PoW Mining Emission** | 121,000,000 | 12,100,000,000,000,000 | 96.031746% |
| **Testnet Airdrop & Bounty** | 1,000,000 | 100,000,000,000,000 | 0.793651% |
| **Early Contributors & Advisory** | 1,500,000 | 150,000,000,000,000 | 1.190476% |
| **Dev Treasury & Security Fund** | 2,500,000 | 250,000,000,000,000 | 1.984127% |
| **Total Max Supply** | **126,000,000** | **12,600,000,000,000,000** | **100.000000%** |
| **Total Premine Allocation** | **5,000,000** | **500,000,000,000,000** | **3.968254%** |

### 3.2 Consensus Definition of Supply Metrics
To prevent confusion across public explorers, wallets, and third-party indexers (CoinGecko, CoinMarketCap):

1. **Max Supply (`max_supply`):** Strictly hardcoded ceiling of $12,600,000,000,000,000$ Armoids (126,000,000.00000000 AMX).
2. **Total Issued Supply (`total_supply`):** Total coins minted into existence to date = $\text{Genesis Premine} + \sum \text{PoW Coinbase Rewards}$.
3. **Burned Supply (`burned_supply`):** Coins sent to Provably Unspendable Addresses (such as `GenesisMiner` `armx1qqqq...plwcdd`).
4. **Locked Supply (`locked_supply`):** Sum of all unvested balances held under on-chain consensus lockup schedules + Immature coinbase rewards.
5. **Circulating Supply (`circulating_supply`):** Free-floating coins available for immediate market transfer:
   $$\text{CirculatingSupply} = \text{TotalIssuedSupply} - \text{BurnedSupply} - \text{LockedSupply}$$

---

## 4. PoW Mining Emission Schedule & Halving Mechanics

Because the premine allocation is 5,000,000 AMX, PoW emission must be constrained to exactly 121,000,000 AMX.

### 4.1 Comparison of PoW Emission Strategies

#### Option A: Harmonized Integer Reward (Recommended)
Adjust the initial block reward from 60 AMX to:
$$\text{InitialReward}_{\text{Mainnet}} = 5,761,904,761 \text{ Armoids } (\approx 57.61904761 \text{ AMX})$$
- **Halving Interval:** Unchanged at $1,050,000$ blocks (~4.0 years at 120s blocks).
- **Behavior:** Standard bit-shift halving ($R \gg e$) across 32 epochs produces an emission of **$120,999,999.77100000$ AMX**.
- **Theoretical Variance:** Within $0.229$ AMX of 121M due to integer arithmetic truncation (identical to Bitcoin's 20,999,999.9769 BTC emission).
- **Advantage:** Predictable, smooth 4-year halving curve without sudden subsidy drops or arbitrary hard stops.

#### Option B: Halving Interval Shortening
Maintain `InitialReward = 60 AMX` and reduce `HalvingInterval` to $1,008,333$ blocks (~3.84 years).
- **Disadvantage:** Alters block cadence perception and creates odd era durations.

#### Option C: Hard Cutoff Truncation
Maintain `InitialReward = 60 AMX` and `HalvingInterval = 1,050,000`, but enforce a hard cutoff when PoW reaches 121M.
- **Disadvantage:** Abruptly cuts mining rewards to zero during Era 4 at block ~1,875,000, penalizing long-term miners.

> [!TIP]
> **Recommendation:** Adopt **Option A**. It preserves protocol elegance and guarantees mathematical convergence to 121,000,000 AMX.

---

## 5. Layer-1 Native On-Chain Vesting Architecture

Because Armaxcoin does not incorporate complex VM smart contracts, vesting must be implemented as a **deterministic, native Layer-1 consensus rule**.

### 5.1 Vesting Schedule Data Model in `AccountState`
Extend `AccountState` in `core/state.go` with deterministic lockup definitions:

```go
type VestingEntry struct {
    Category        string `json:"category"`         // "airdrop", "advisory", "treasury"
    TotalAllocated  int64  `json:"total_allocated"`  // Total base units granted
    StartBlock      int    `json:"start_block"`      // Genesis = 0 or specific activation height
    CliffBlock      int    `json:"cliff_block"`      // Block height before which 0 vests
    IntervalBlocks  int    `json:"interval_blocks"`  // Blocks per release tranche (e.g. 21,600 = 30 days)
    TranchesCount   int    `json:"tranches_count"`   // Total number of release intervals
    AmountPerTranche int64 `json:"amount_per_tranche"` // Base units unlocked per interval
    ImmediateUnlock int64  `json:"immediate_unlock"` // Unlocked immediately at StartBlock (e.g. 25% for airdrop)
}

type AccountState struct {
    Balance  int64            `json:"balance"`
    Nonce    int64            `json:"nonce"`
    Immature []ImmatureReward `json:"immature,omitempty"`
    Vesting  []VestingEntry   `json:"vesting,omitempty"`
}
```

### 5.2 Deterministic Block-Height Cadence
To eliminate reliance on device local system clocks and prevent "timewarp" manipulation, all vesting stages are indexed strictly to **Block Height**. At a target block time of 120 seconds:
- **1 Hour:** 30 blocks
- **1 Day (24h):** 720 blocks
- **1 Month (30 Days):** 21,600 blocks
- **3 Months (90 Days / Cliff):** 64,800 blocks
- **6 Months (180 Days):** 129,600 blocks

### 5.3 Consensus Enforcement in `StateManager.VerifyAndApplyBlock`
When evaluating any spending transaction ($idx \ge 1$):
$$\text{Vested}(\text{Entry}, H) = \text{ImmediateUnlock} + \begin{cases} 0 & \text{if } H < \text{CliffBlock} \\ \min(\text{TotalAllocated} - \text{ImmediateUnlock}, \lfloor \frac{H - \text{CliffBlock}}{\text{IntervalBlocks}} + 1 \rfloor \times \text{AmountPerTranche}) & \text{if } H \ge \text{CliffBlock} \end{cases}$$
$$\text{LockedVesting}(H) = \sum \max(0, \text{TotalAllocated} - \text{Vested}(\text{Entry}, H))$$
$$\text{SpendableBalance}(H) = \text{Balance} - \text{ImmatureRewards}(H) - \text{LockedVesting}(H)$$

Any transaction attempting to transfer more than $\text{SpendableBalance}(H) - \text{Fee}$ is immediately rejected by node consensus with:
`"insufficient spendable balance: funds are locked under on-chain vesting schedule until block #X"`.

### 5.4 Reorg & Replay Safety
Because vesting is calculated purely as a deterministic function of $(H, \text{AccountState})$, chain reorganizations, rollbacks, and node restarts automatically recalculate correct available balances with zero state drift.

---

## 6. Allocation Vesting Schedules Detail

### 6.1 Category A: Testnet Airdrop & Bounty (1,000,000 AMX)
- **Total:** $100,000,000,000,000$ Armoids.
- **Tranches:** 4 monthly releases of 250,000 AMX ($25,000,000,000,000$ Armoids) each:
  - **Launch (Block 0):** 25% (250,000 AMX) immediately liquid.
  - **Month 1 (Block 21,600):** +25% (250,000 AMX).
  - **Month 2 (Block 43,200):** +25% (250,000 AMX).
  - **Month 3 (Block 64,800):** Final 25% (250,000 AMX) fully liquid.

### 6.2 Category B: Early Contributors & Advisory (1,500,000 AMX)
- **Total:** $150,000,000,000,000$ Armoids.
- **Cliff:** 3 months ($64,800$ blocks). $0$ AMX spendable before block 64,800.
- **Vesting Duration:** 10 months following cliff ($21,600$ blocks per tranche).
- **Tranche Amount:** 10% = 150,000 AMX ($15,000,000,000,000$ Armoids) per month.
- **Schedule:**
  - Blocks $0 \le H < 64,800$: 0 AMX unlocked.
  - Month 4 (Block 64,800): 150,000 AMX unlocked.
  - Month 5 (Block 86,400): 150,000 AMX unlocked (Cumulative 300,000 AMX).
  - ...
  - Month 13 (Block 259,200): Final 150,000 AMX unlocked (100% vested).

### 6.3 Category C: Dev Treasury & Security Fund (2,500,000 AMX)
- **Total:** $250,000,000,000,000$ Armoids.
- **Lockup:** 6 months ($129,600$ blocks). $0$ AMX spendable before block 129,600.
- **Open Decision (Post-Month 6 Policy):**
  - *Option 1:* Linear release of 10% per month across months 7–16 (250,000 AMX/month).
  - *Option 2:* Quarterly milestone-based disbursement subject to public governance.
  - *Operational Reserve Carve-Out:* If operational costs (audits, infrastructure, exchange listing fees) require funding prior to Month 6, a separate operational tranche (e.g. 250,000 AMX liquid, 2,250,000 AMX locked 6-months) should be formally approved rather than leaving the dev team with 0 operational liquidity.

---

## 7. Testnet Migration, Snapshot & Conversion Methodology

### 7.1 Proposed Conversion Ratio: 10 AMX Testnet : 1 AMX Mainnet
- **Total Testnet Reward Cap:** 1,000,000 AMX Mainnet allocation pool.
- At a 10:1 conversion ratio, exactly 10,000,000 testnet AMX can be redeemed.
- **Eligibility Criteria:**
  1. Only balances accrued prior to the designated **Snapshot Block Height** on `ARMX-TESTNET-1` qualify.
  2. Faucet spam addresses, Sybil clusters, and unverified bot transfers must be filtered out.
  3. Mining proof-of-work rewards and legitimate testnet node operator addresses are prioritized.
  4. Individual maximum claim cap: e.g. Max 25,000 AMX mainnet per participant to ensure broad distribution.

### 7.2 Cryptographic Claim Verification
To prove legitimate custody of testnet funds:
- Claimants sign a structured message containing their target mainnet address using their testnet ML-DSA-65 private key:
  $$\text{ClaimMessage} = \text{"ARMX-MIGRATION-CLAIM:"} \parallel \text{TestnetAddress} \parallel \text{":"} \parallel \text{MainnetAddress}$$
- A standalone verification tool (`armclaim` / explorer claim portal) validates the signature against the testnet address before recording the allocation.

---

## 8. Open Decisions Requiring Human Approval (Checklist)

Before any consensus or database code is committed to `armax_coin`, the following decisions must be formally selected by project leadership:

- [ ] **Decision 1: PoW Initial Reward Formulation**
  - **Option 1A (Recommended):** Set Mainnet `InitialReward = 5,761,904,761` Armoids (57.61904761 AMX) to smoothly emit 121M AMX over standard 4-year halvings.
  - **Option 1B:** Keep 60 AMX and shorten halving interval to 1,008,333 blocks.
  - **Option 1C:** Keep 60 AMX and abruptly terminate mining rewards once 121M AMX is reached.

- [ ] **Decision 2: Dev Treasury Operational Carve-out**
  - **Option 2A (Full 6-Month Lock):** Lock all 2,500,000 AMX until Month 6 (no funds available for audits/servers/listings).
  - **Option 2B (Recommended Operational Split):** 250,000 AMX (10%) available at launch for infrastructure/security audits, 2,250,000 AMX locked with a 6-month cliff.

- [ ] **Decision 3: Treasury Release Schedule After Month 6**
  - **Option 3A (Recommended):** Linear monthly release of 10% per month over 10 months.
  - **Option 3B:** 100% unlocked at block 129,600 (Month 6).

- [ ] **Decision 4: Mainnet Genesis PoW Algorithm**
  - **Option 4A (Recommended):** Launch Mainnet directly with **ArmaxHash v2 Stream-Fast** active from Block #0 (`ArmaxHashV2ActivationHeight = 0`), avoiding legacy SHA-256d or Argon2id transitions on Mainnet.
  - **Option 4B:** Mirror testnet history (SHA-256d for 520 blocks, etc.).

- [ ] **Decision 5: Testnet Snapshot Block & Claim Ratio**
  - Ratify target snapshot block height on `ARMX-TESTNET-1` and confirm 10:1 conversion ratio with per-user cap.

---

## 9. Next Steps (Phases 3–5 Roadmap)

1. **Phase 3 (Coordinated Implementation):**
   - Core consensus: Implement `VestingSchedule` in `core/state.go`, `VerifyAndApplyBlock`, and `blockchain.go`.
   - Update Genesis Block generation with explicit 3-output premine allocations.
   - Update RPC endpoints `/api/supply`, `/stats`, `/balance` in `node/server.go`.
   - Update `armax_explorer` supply calculations, circulating metrics, and address vesting inspection modal.
   - Update `armax_android` `BalanceInfo` and UI to render spendable vs locked balances.
   - Synchronize all `*.md` documentation (Whitepaper, README, Contributor Rewards).
2. **Phase 4 (Testing & Verification):**
   - Run unit tests, regression tests, supply conservation tests, and simulated chain reorgs with active vesting.
3. **Phase 5 (Reporting & Local Commit):**
   - Compile final `TOKENOMICS_MIGRATION_REPORT.md` and commit locally.
