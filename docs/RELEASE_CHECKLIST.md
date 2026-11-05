# Armaxcoin Open Testnet Publication & Release Checklist

This checklist defines the formal verification gates and operational procedures required before tagging, packaging, and publishing an official release of `armax-coin`. Build and artifact details live in [RELEASE.md](RELEASE.md).

---

## 1. Codebase & Toolchain Verification

- [ ] **Go Toolchain:** Verified building with Go 1.27+ (`go version`).
- [ ] **Deterministic Formatting:** `gofmt -l .` reports zero unformatted files.
- [x] **Go Vet Analysis:**
  - `go vet ./...` passes with 0 errors.
  - `go vet -tags testhooks ./...` passes with 0 errors.
- [x] **Static Analysis:**
  - `staticcheck -tags testhooks ./...` passes repository-wide with 0 errors (all core, cli, node, gui, and bridge packages pass cleanly).
- [x] **Vulnerability Audit:**
  - `govulncheck ./...` passes with 0 known vulnerabilities affecting code (gorilla/websocket upgraded to v1.5.3).
- [ ] **Repository Hygiene & Secret Scanning:**
  - [x] `go test -v -run TestNoPersonalDataInTree .` passes (zero personal directories, emails, or credentials in tracked tree).
  - Gitleaks scan: NOT VERIFIED — repository-wide Gitleaks evidence is not available (targeted scans executed in historical evidence; full repository scan not independently verified).

---

## 2. Test Suite & Concurrency Validation

- [x] **Core Unit Tests:** `go test -tags testhooks -count=1 ./core` passes.
- [x] **Miner Tests:** `go test -tags testhooks -count=1 ./miner` passes.
- [x] **Node & P2P Tests:** `go test -tags testhooks -count=1 ./node` passes (including IBD verification, push staging, and handshake timeout).
- [x] **CLI & Wallet Tests:** `go test -tags testhooks -count=1 ./cli/armwallet ./cli/armsend ./wallet` passes.
- [x] **GUI Tests:** `go test -tags testhooks -count=1 ./gui` passes.
- [x] **Bridge & Relayer Tests:** `go test -tags testhooks -count=1 ./bridge ./cmd/armx-bridge-relayer` passes.
- [x] **Full Repository Suite:** `go test -tags testhooks ./...` passes 100% across all 15 packages (exit code 0).
- [ ] **Race Detection:** `go test -tags testhooks -race -count=1 ./...` (Core packages verified with zero race conditions, e.g. `TestUndoHistoryConcurrentReadWriteRace`; full repository-wide execution pending on heavy CI runner).
- [ ] **Multi-Node Chaos & Soak Tests:**
  - `go test -tags "soak testhooks" -v -timeout 30m -count=1 ./node -run "TestSoak"` passes (validates 5-node partition healing and LWMA retargeting).

---

## 3. Cross-Platform Compilation

- [x] **Linux amd64:** `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **Linux arm64:** `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **Windows amd64:** `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **macOS arm64:** `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build .` compiles cleanly.
- [ ] **Windows GUI Asset:** Verified `gui/rsrc_windows_amd64.syso` embeds manifest and icon without compilation warning.
- [ ] **Release Packaging:** Verified `build-all.ps1` or `.github/workflows/release.yml` produces stripped binaries with SHA256 checksums.

---

## 4. Documentation & Public Information Verification

- [x] **Canonical Name Consistency:** Verified repository and public documentation consistently refer to `armax-coin`.
- [x] **Go Module Integrity:** `go.mod` declares `module github.com/acxxcxx/armax-coin`.
- [x] **Language Consistency:** All public documentation written in clear, professional English.
- [x] **Documentation Set Present:**
  - `README.md`
  - `CONTRIBUTING.md`
  - `CODE_OF_CONDUCT.md`
  - `SECURITY.md`
  - `CHANGELOG.md`
  - `docs/TESTNET_GUIDE.md`
  - `docs/MINING_GUIDE.md`
  - `docs/NODE_OPERATIONS.md`
  - `docs/CONSENSUS_AND_NETWORK.md`
  - `CONTRIBUTORS.md`
  - `SUPPORT.md`
- [x] **Experimental Disclaimers:** Visible warnings that testnet coins have no economic value and software has not undergone an independent third-party audit.

---

## 5. Seed Infrastructure & Live P2P Connectivity

- [ ] **Seed Node 1:** `testnet-seed1.armax-coin.cloud:19335` responsive to inbound TLS P2P handshakes.
- [ ] **Seed Node 2:** `testnet-seed2.armax-coin.cloud:19335` responsive to inbound TLS P2P handshakes.
- [ ] **RPC Isolation:** Verified RPC port 19333 strictly rejects external public connections on all seed nodes.
- [ ] **DNS Records:** TTL set to 300s for testnet seed domains.

---

## 6. Git & Release Publication Procedures

- [ ] Ensure local working tree is clean (`git status`).
- [ ] Tag the release commit using strict SemVer (`v1.0.7`).
- [ ] Trigger `.github/workflows/release.yml` with version tag.
- [ ] Quality gates and vulnerability checks completed successfully.
- [ ] Binaries compiled and published for all 4 target platforms.
- [ ] Master `SHA256SUMS` generated and verified.
- [ ] Mark release as **Pre-release** on GitHub until mainnet readiness gates are satisfied.
