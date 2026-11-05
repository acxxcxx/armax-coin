# Armaxcoin Launch Checklist

This checklist defines the formal progression gates for the **Sequential Launch Strategy** (`LAUNCH_MODE = SEQUENTIAL`): Public Testnet launches first; Mainnet remains locked until testnet soaking and community verification are complete.

---

## Phase 1: Testnet Candidate Preparation (Current Phase)

- [x] **Mainnet Locked in Source**:
  - `core.MainnetLaunched = false` enforced.
  - All binaries (`armcoind`, `armminer`, `armsend`, `armwallet`, `armgui`) strictly reject `-network mainnet` with `"Mainnet has not been launched; please use -network testnet"`.
  - Rejection bypassed solely under `//go:build testhooks` for automated unit tests.
- [x] **Testnet Configured as Default**:
  - All binaries default to `TestnetProfile` when `-network` is omitted.
  - Default P2P port: `19335`, default RPC port: `19333`.
  - Default data directory: `~/.armax/testnet` (Windows: `%APPDATA%\Armax\testnet`).
  - Warning banner displayed on binary startup: `WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited`.
- [x] **Consensus & Tokenomics Frozen**:
  - Algorithm: Double-SHA256 (SHA-256d) block headers with LWMA-1 difficulty retargeting ($N=60, T=120\text{s}$).
  - Digital signatures: NIST FIPS 204 (ML-DSA-65).
  - Hard supply ceiling: 126,000,000 ARMX mathematically bounded.
  - Tokenomics preserved: 126,000,000 ARMX maximum supply, 60 ARMX initial reward, 1,050,000-block halving interval, 120-second target, and existing genesis are unchanged.
  - No mining-funded treasury is guaranteed: after launch, project miners may seek up to 1,000,000 ARMX using ordinary mining and the same consensus rules; this objective is not a premine, genesis allocation, or consensus reserve.
  - Bridge backing and DEX liquidity are separate, unapproved decisions; no wARMX should be minted without verifiable native backing and approved custody/proof-of-lock controls.
- [x] **Quality Gates Status (Core Packages vs Repository-Wide)**:
  - `go build` passes cleanly for Core binaries (`.`, `armsend`, `armwallet`, `gui`) (exit 0).
  - `go vet ./...` and `go vet -tags testhooks ./...` pass with 0 errors (exit 0).
  - `staticcheck -tags testhooks ./...` passes repository-wide with 0 diagnostics (exit 0; all core, cli, node, gui, and bridge packages pass cleanly).
  - Core package unit test suites verified in TASK-012 Phase B: `core` (147.9s), `node` (103.4s), `miner` (11.8s), `wallet` (11.2s), `gui` (4.5s), and `cli/...` (28.9s) all pass cleanly (exit 0); Bridge tests rely on legacy single-signer Ganache fixtures and remain out-of-scope/blocked.
- [ ] **Repository Sanitization & Clean Snapshot**:
  - Public snapshot generator scripts verified: [`scripts/make_public_snapshot.sh`](../scripts/make_public_snapshot.sh) and [`scripts/make_public_snapshot.ps1`](../scripts/make_public_snapshot.ps1).
  - Prohibited file scan: 0 `*.cookie`, `*.exe`, `*.db`, `wallet.json`, `*.log`, or `scratch/` files.
  - Automated `gitleaks detect` verification: NOT VERIFIED — repository-wide Gitleaks evidence is not available (targeted scans executed in historical evidence; full repository scan not independently verified).
  - Private repo archived; public repo created as `armax-coin`.

---

## Phase 2: Public Testnet Operational Rollout

- [ ] **Seed Node Infrastructure**:
  - Deploy testnet seed nodes (`testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335`).
  - Ensure systemd unit [`systemd/armcoind-testnet.service`](../systemd/armcoind-testnet.service) is enabled and active.
  - Verify P2P port 19335 is open to the internet; RPC port 19333 restricted to `127.0.0.1`.
- [ ] **Release Artifact Publication**:
  - Trigger `.github/workflows/release.yml` with tag `v1.0.6`.
  - Verify SHA256SUMS and archive packages for all target platforms.
- [ ] **Community Announcement**:
  - Publish [`ANNOUNCEMENT_TESTNET.md`](../ANNOUNCEMENT_TESTNET.md) to community forums (Bitcointalk, Reddit, Discord).
  - Solicit external seed node operators and testnet miners.

---

## Phase 3: Testnet Soak & Stability Verification

- [ ] **Minimum Soaking Period**: Continuous testnet operation for $\ge 14$ days without unhandled consensus splits.
- [ ] **Difficulty Responsiveness**: Verify LWMA-1 adjusts difficulty smoothly across natural hashrate spikes and drops without unbounded oscillation.
- [ ] **Reorganization Handling**: Monitor chain reorg behavior in real testnet conditions (all within `MaxReorganizationDepth = 720`).
- [ ] **Peer Mesh Health**: Verify stable discovery through address gossip and DNS seeds.

---

## Phase 4: Mainnet Unlock & Production Protocol Activation Gates

- [ ] Fill all `<ISI>` placeholders in [`ANNOUNCEMENT_MAINNET.template.md`](../ANNOUNCEMENT_MAINNET.template.md).
- [ ] Set `core.MainnetLaunched = true` in a dedicated single consensus unlock commit.
- [ ] Switch default network profile in `node.DefaultConfig()` to mainnet.
- [ ] Deploy dedicated mainnet seed node cluster (`systemd/armcoind.service`, P2P port 19334).
- [ ] Tag official `v2.0.0` mainnet release (or subsequent major release).
