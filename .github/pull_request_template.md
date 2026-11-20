## Description
Provide a concise summary of the changes in this pull request and the rationale behind them.

Fixes #(issue)

## Type of Change
Please check the options that are relevant:
- [ ] **Bug fix** (non-breaking change which fixes an issue)
- [ ] **New feature** (non-breaking change which adds functionality)
- [ ] **Breaking change** (fix or feature that alters existing functionality or wire format)
- [ ] **Consensus rule change** (affects block validation, ArmaxHash, Armax-ASERT-AW/LWMA, `TxBinaryV1`, or transaction format)
- [ ] **Documentation / Chore** (updates to docs, CI, or build configurations)

## Subsystem / Component
- [ ] `core/` (Blockchain state, block validation, transactions, crypto/ML-DSA)
- [ ] `armaxhash/` (ArmaxHash v1: Argon2id and BLAKE2b; v2: domain-separated chained BLAKE2b + prefix cache)
- [ ] `node/` (P2P mesh, sync gate, TLS 1.3/TOFU transport, RPC daemon)
- [ ] `miner/` (Multi-threaded CPU mining engine & Stratum TCP/TLS client)
- [ ] `wallet/` & `cli/` (CLI tools: `armwallet`, `armsend`, multi-wallet keystore)
- [ ] `gui/` (Desktop GUI v2: WebView2, system tray, multi-wallet & CPU miner)
- [ ] Docs, Scripts, or Workflows

## Consensus & Network Compatibility Checklist
If this PR touches `core/`, `node/`, or `armaxhash/`:
- [ ] Changes preserve deterministic block and transaction evaluation across platforms.
- [ ] Wire format changes maintain backwards compatibility or define an explicit version bump.
- [ ] No sensitive keys, test mnemonic seeds, or private credentials are included.

## Quality & Verification Checklist
Before submitting this PR, please confirm:
- [ ] Code compiles cleanly on Go 1.27.4+ (`go build ./...`).
- [ ] Unit and integration tests pass locally with race/hook checks (`go test -tags testhooks -race ./...`).
- [ ] Added unit or integration test cases covering the new behavior.
- [ ] Code follows existing project conventions (`go fmt` / `staticcheck`).
- [ ] Documentation has been updated to reflect changes (if applicable).
- [ ] Commit history is clean, logically organized, and contains clear commit messages.
