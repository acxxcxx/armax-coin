# Armaxcoin Open Testnet Publication & Release Checklist

This checklist defines the formal verification gates and operational procedures required before tagging, packaging, and publishing an official release of `armax-coin`. Build and artifact details live in [RELEASE.md](RELEASE.md).

---

## 1. Codebase & Toolchain Verification

- [ ] **Go Toolchain:** Verified building with Go 1.27+ (`go version`).
- [ ] **Deterministic Formatting:** `gofmt -l .` reports zero unformatted files.
- [ ] **Go Vet Analysis:**
  - `go vet ./...` passes with 0 errors.
  - `go vet -tags testhooks ./...` passes with 0 errors.
- [ ] **Static Analysis:**
  - `staticcheck -tags testhooks ./...` passes with 0 errors.
- [ ] **Vulnerability Audit:**
  - `govulncheck ./...` passes with 0 known vulnerabilities.
- [ ] **Repository Hygiene & Secret Scanning:**
  - `go test -v -run TestNoPersonalDataInTree .` passes (zero personal directories, emails, or credentials in tracked tree).
  - Gitleaks scan returns zero secret leaks.

---

## 2. Test Suite & Concurrency Validation

- [ ] **Core Unit Tests:** `go test -tags testhooks -count=1 ./core` passes.
- [ ] **ArmaxHash Tests:** `go test -tags testhooks -count=1 ./armaxhash` passes.
- [ ] **Miner Tests:** `go test -tags testhooks -count=1 ./miner` passes.
- [ ] **Node & P2P Tests:** `go test -tags testhooks -count=1 ./node` passes.
- [ ] **CLI & Wallet Tests:** `go test -tags testhooks -count=1 ./cli/armwallet ./cli/armsend ./wallet` passes.
- [ ] **Race Detection:** `go test -tags testhooks -race -count=1 ./...` passes in CI.
- [ ] **Multi-Node Chaos & Soak Tests:**
  - `go test -tags "soak testhooks" -v -timeout 30m -count=1 ./node -run "TestSoak"` passes (validates 5-node partition healing and LWMA retargeting).

---

## 3. Cross-Platform Compilation

- [ ] **Linux amd64:** `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` compiles cleanly.
- [ ] **Linux arm64:** `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...` compiles cleanly.
- [ ] **Windows amd64:** `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` compiles cleanly.
- [ ] **macOS arm64:** `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...` compiles cleanly.
- [ ] **Windows GUI Asset:** Verified `gui/rsrc_windows_amd64.syso` embeds manifest and icon without compilation warning.
- [ ] **Release Packaging:** Verified `build-all.ps1` or `.github/workflows/release.yml` produces stripped binaries with SHA256 checksums.

---

## 4. Documentation & Public Information Verification

- [ ] **Canonical Name Consistency:** Verified repository and public documentation consistently refer to `armax-coin`.
- [ ] **Go Module Integrity:** `go.mod` declares `module github.com/acxxcxx/armax-coin`.
- [ ] **Language Consistency:** All public documentation written in clear, professional English.
- [ ] **Documentation Set Present:**
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
- [ ] **Experimental Disclaimers:** Visible warnings that testnet coins have no economic value and software has not undergone an independent third-party audit.

---

## 5. Seed Infrastructure & Live P2P Connectivity

- [ ] **Seed Node 1:** `testnet-seed1.armax-coin.cloud:19335` responsive to inbound TLS P2P handshakes.
- [ ] **Seed Node 2:** `testnet-seed2.armax-coin.cloud:19335` responsive to inbound TLS P2P handshakes.
- [ ] **RPC Isolation:** Verified RPC port 19333 strictly rejects external public connections on all seed nodes.
- [ ] **DNS Records:** TTL set to 300s for testnet seed domains.

---

## 6. Git & Release Publication Steps

- [x] Ensure local working tree is clean (`git status`).
- [x] Tag the release commit using strict SemVer (`v1.0.0`).
- [x] Trigger `.github/workflows/release.yml` with version tag.
- [x] Quality gates and vulnerability checks completed successfully.
- [x] Binaries compiled and published for all 4 target platforms.
- [x] Master `SHA256SUMS` generated and verified.
- [x] Marked release as **Pre-release** on GitHub until mainnet readiness gates are satisfied.
