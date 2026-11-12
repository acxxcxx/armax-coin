# Armaxcoin Release, Build & Checksum Verification Guide

## 1. Toolchain & Go Version Requirements

Armaxcoin implements post-quantum cryptography conforming to NIST FIPS 204 (ML-DSA-65). It utilizes the standard library package `crypto/mldsa`.

- **Minimum Required Go Version:** **Go 1.27+** (as pinned in `go.mod` and verified in CI). Build releases with **Go 1.27.3 or newer**: earlier 1.27.x toolchains are affected by `GO-2026-6809` (`crypto/x509`). `go.mod` declares `toolchain go1.27.3`.
- **CGO Requirement:** `CGO_ENABLED=0` for pure Go deterministic cross-platform compilation of `armcoind`, `armminer`, `armsend`, and `armwallet`.
- **Platform Architecture Support:**
  - `linux/amd64` (Standard x86_64 servers and desktops)
  - `linux/arm64` (Single Board Computers, Raspberry Pi 4/5, Armbian, AWS Graviton)
  - `windows/amd64` (Windows 10/11, Windows Server 64-bit; includes `armgui.exe`)
  - `darwin/arm64` (macOS Apple Silicon M1/M2/M3/M4)

---

## 2. Compiling from Source

To build deterministic, stripped binaries matching official releases:

```bash
# Clone the repository
git clone https://github.com/acxxcxx/armax-coin.git
cd armax-coin

# Ensure Go 1.27+ is active
go version

# Compile core node daemon
go build -trimpath -ldflags="-s -w -buildid=" -o armcoind main.go

# Compile CPU miner client
go build -trimpath -ldflags="-s -w -buildid=" -o armminer miner/miner.go

# Compile CLI wallet tool
go build -trimpath -ldflags="-s -w -buildid=" -o armwallet ./cli/armwallet

# Compile CLI transaction sender
go build -trimpath -ldflags="-s -w -buildid=" -o armsend ./cli/armsend

# (Windows AMD64 only) Compile desktop GUI wallet
# Note: uses deliberate Windows resource object gui/rsrc_windows_amd64.syso (manifest & icon).
# To re-generate rsrc_windows_amd64.syso from source:
#   go install github.com/akavel/rsrc@latest
#   cd gui && rsrc -manifest armgui.exe.manifest -ico armax_logo.ico -arch amd64 -o rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -buildid=" -o armgui.exe ./gui
```

---

## 3. SHA256 Checksum Verification Procedure

Every official release tag (`v*`) publishes pre-compiled archive packages accompanied by a cryptographically canonical `SHA256SUMS` file.

### Verification on Linux / macOS
```bash
# Download archive and checksum file
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.10/armax-v1.0.10-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.10/SHA256SUMS

# Verify integrity using sha256sum
sha256sum --check --ignore-missing SHA256SUMS
```
Expected output:
```text
armax-v1.0.9-linux-amd64.tar.gz: OK
```

### Verification on Windows (PowerShell)
```powershell
# 1. Compute SHA-256 hash of downloaded zip
Get-FileHash -Algorithm SHA256 .\armax-v1.0.9-windows-amd64.zip

# 2. Compare against the published hash in SHA256SUMS
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.9-windows-amd64.zip"
```

---

## 4. Release Artifacts Policy

- **No unauthorized binary files are committed to Git:** All `.exe`, `blockchain.db`, `.cookie`, logs, and temporary build outputs are strictly blocked by `.gitignore`.
- **Intentional Windows Resource Object:** `gui/rsrc_windows_amd64.syso` is an intentional binary resource object embedding the manifest (`armgui.exe.manifest`) and icon (`armax_logo.ico`) required by the Go toolchain on Windows amd64. It can be reproducibly rebuilt using `rsrc` as documented in Section 2.
- **Reproducible Build Flags:** `-trimpath` removes file system paths from compiled binaries; `-ldflags="-s -w -buildid="` strips debug symbols and ensures reproducible output across build environments.

---

## 5. Official Release Status

- **Target Release (`v1.0.10`):** **Official Release**
  - **Release Notes:** [`docs/releases/v1.0.10.md`](releases/v1.0.10.md)
  - **Status:** Armax-ASERT-AW DAA Consensus Hard Fork at Block #13,335, 4-Hour Surge Cap & TEDR Boost, Immutable MiningJob & 3-Tier P2P Policy
  - **Target Architectures:**
    - `armax-v1.0.10-linux-amd64.tar.gz` (Linux 64-bit x86; includes Linux Web GUI)
    - `armax-v1.0.10-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC; includes Linux Web GUI)
    - `armax-v1.0.10-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.10-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)
  - **Verification:** Full automated test suite passes (`go test -tags testhooks ./...` 100% green; staticcheck and govulncheck 0 issues).

- **Previous Emergency Fork Release (`v1.0.9`):**
  - **Release Notes:** [`docs/releases/v1.0.9.md`](releases/v1.0.9.md)
  - **Status:** Testnet Anti-Stall Emergency Difficulty Adjustment (TEDR at Block #13,293), Zero-Allocation CPU Miner Optimization (`5.45x` Speedup), Linux GUI Embedded Daemon Auto-Start
  - **Target Architectures:**
    - `armax-v1.0.9-linux-amd64.tar.gz` (Linux 64-bit x86; includes Linux Web GUI)
    - `armax-v1.0.9-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC; includes Linux Web GUI)
    - `armax-v1.0.9-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.9-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)

- **Previous Maintenance Release (`v1.0.8`):**
  - **Release Notes:** [`docs/releases/v1.0.8.md`](releases/v1.0.8.md)
  - **Status:** Desktop GUI Authentication & First-Time Onboarding, P2P Residential Peer Privacy Protection, Snapshot Safety Precedence

- **Previous Consensus Fork Release (`v1.0.7`):**
  - **Release Notes:** [`docs/releases/v1.0.7.md`](releases/v1.0.7.md)
  - **Status:** Testnet Consensus Hard Fork at Block 20,000 (10-minute future timestamp ceiling), Sync & Storage Optimization, Subsystem Hardening
  - **Target Architectures:**
    - `armax-v1.0.7-linux-amd64.tar.gz` (Linux 64-bit x86)
    - `armax-v1.0.7-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC)
    - `armax-v1.0.7-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.7-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)
  - **Verification:** Full automated test suite passes (`go test -tags testhooks ./...` 100% green; `contracts/` npm test 11/11 passing).

- **Previous Maintenance Release (`v1.0.6`):**
  - **Release Tag:** [`v1.0.6`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.6)
  - **Status:** Public Open Testnet -- Node Maintenance, Data Race Hardening & Storage Isolation
  - **Target Architectures:**
    - `armax-v1.0.6-linux-amd64.tar.gz` (Linux 64-bit x86)
    - `armax-v1.0.6-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC)
    - `armax-v1.0.6-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.6-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)
  - **Verification:** Validated with race detection, staticcheck, govulncheck, and cryptographic SHA256SUMS aggregation.

- **Previous Hard-Fork Release (`v1.0.5`):**
  - **Release Tag:** [`v1.0.5`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.5)
  - **Status:** Public Open Testnet -- Display Ticker (tARMX), CLI Hardening & Release Prep (C1/C2 Hard Fork at Block #8,000)
  - **Target Architectures:**
    - `armax-v1.0.5-linux-amd64.tar.gz` (Linux 64-bit x86)
    - `armax-v1.0.5-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC)
    - `armax-v1.0.5-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.5-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)
  - **Verification:** Automatically validated via GitHub Actions with race detection, staticcheck, govulncheck, and cryptographic SHA256SUMS aggregation.

- **Previous Maintenance Release (`v1.0.4`):**
  - **Release Tag:** [`v1.0.4`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.4)
  - **Status:** Public Open Testnet -- Structured Diagnostics & Consensus Hardening

- **Previous Maintenance Release (`v1.0.3`):**
  - **Release Tag:** [`v1.0.3`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.3)
  - **Status:** Public Open Testnet -- ArmaxHash v2 Stream-Fast & Hardening

- **Previous Maintenance Release (`v1.0.1`):**
  - **Release Tag:** [`v1.0.1`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.1)
  - **Status:** Public Open Testnet -- BIP-35 Mempool Sync & Outbound NAT Support

- **Initial Open Testnet Release (`v1.0.0`):**
  - **Release Tag:** [`v1.0.0`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.0)
  - **Status:** Initial Public Open Testnet Release

---

## 6. Pre-Tagging Quality & Release Checklist

This checklist defines the formal verification gates required before tagging, packaging, and publishing an official release of `armax-coin`:

### 6.1 Codebase & Toolchain Verification
- [ ] **Go Toolchain:** Verified building with Go 1.27+ (`go version`).
- [ ] **Deterministic Formatting:** `gofmt -l .` reports zero unformatted files.
- [x] **Go Vet Analysis:** `go vet ./...` and `go vet -tags testhooks ./...` pass with 0 errors.
- [x] **Static Analysis:** `staticcheck -tags testhooks ./...` passes repository-wide with 0 diagnostics.
- [x] **Vulnerability Audit:** `govulncheck ./...` passes with 0 known vulnerabilities affecting code.
- [x] **Repository Hygiene:** `go test -v -run TestNoPersonalDataInTree .` passes (zero personal directories, emails, or credentials in tracked tree).

### 6.2 Test Suite & Concurrency Validation
- [x] **Core Test Suites:** `go test -tags testhooks -count=1 ./core ./miner ./node ./cli/armwallet ./cli/armsend ./wallet ./gui` all pass cleanly.
- [x] **Full Repository Suite:** `go test -tags testhooks ./...` passes 100% across all packages.
- [ ] **Race Detection:** `go test -tags testhooks -race -count=1 ./...` passes with 0 data races.
- [ ] **Multi-Node Soak Tests:** `go test -tags "soak testhooks" -v -timeout 30m -count=1 ./node -run "TestSoak"` passes.

### 6.3 Cross-Platform Compilation
- [x] **Linux amd64:** `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **Linux arm64:** `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **Windows amd64:** `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build .` compiles cleanly.
- [x] **macOS arm64:** `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build .` compiles cleanly.
- [ ] **Windows GUI Asset:** Verified `gui/rsrc_windows_amd64.syso` embeds manifest and icon without compilation warning.
- [ ] **Release Packaging:** Verified `build-all.ps1` or `.github/workflows/release.yml` produces stripped binaries with SHA256 checksums.

### 6.4 Documentation & Public Information Verification
- [x] **Canonical Module & Naming:** `go.mod` declares `module github.com/acxxcxx/armax-coin` and documentation consistently refers to `armax-coin`.
- [x] **Documentation Set Present & Synchronized:** `README.md`, `WHITEPAPER.md`, `SECURITY.md`, `CHANGELOG.md`, `RELEASE_NOTES.md`, `TREASURY_MIGRATION_PLAN.md`, `docs/TESTNET.md`, `docs/MINING_GUIDE.md`, `docs/NODE_OPERATIONS.md`, and `docs/CONSENSUS_AND_NETWORK.md`.
- [x] **Experimental Disclaimers:** Visible notices that testnet coins (`tARMX`) have no economic value and mainnet remains locked.

### 6.5 Seed Infrastructure & Live P2P Connectivity
- [ ] **Seed Nodes Active:** `testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335` responsive to inbound TLS P2P handshakes.
- [ ] **RPC Isolation Enforced:** Verified RPC port 19333 strictly rejects external public connections on all seed nodes.
- [ ] **DNS Records:** TTL set to 300s for testnet seed domains.

### 6.6 Git & Release Publication Procedures
- [ ] Ensure local working tree is clean (`git status`).
- [ ] Tag the release commit using strict SemVer (`v1.0.10`).
- [ ] Trigger `.github/workflows/release.yml` with version tag.
- [ ] Binaries compiled and published for all 4 target platforms.
- [ ] Master `SHA256SUMS` and detached PGP signature `SHA256SUMS.asc` generated and verified.
- [ ] Mark release as **Pre-release** on GitHub until mainnet readiness gates are satisfied.
