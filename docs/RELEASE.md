# Armaxcoin Release, Build & Checksum Verification Guide

## 1. Toolchain & Go Version Requirements

Armaxcoin implements post-quantum cryptography conforming to NIST FIPS 204 (ML-DSA-65). It utilizes the standard library package `crypto/mldsa`.

- **Minimum Required Go Version:** **Go 1.27+** (as pinned in `go.mod` and verified in CI).
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
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.3/armax-v1.0.3-linux-amd64.tar.gz
curl -LO https://github.com/acxxcxx/armax-coin/releases/download/v1.0.3/SHA256SUMS

# Verify integrity using sha256sum
sha256sum --check --ignore-missing SHA256SUMS
```
Expected output:
```text
armax-v1.0.3-linux-amd64.tar.gz: OK
```

### Verification on Windows (PowerShell)
```powershell
# 1. Compute SHA-256 hash of downloaded zip
Get-FileHash -Algorithm SHA256 .\armax-v1.0.3-windows-amd64.zip

# 2. Compare against the published hash in SHA256SUMS
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.3-windows-amd64.zip"
```

---

## 4. Release Artifacts Policy

- **No unauthorized binary files are committed to Git:** All `.exe`, `blockchain.db`, `.cookie`, logs, and temporary build outputs are strictly blocked by `.gitignore`.
- **Intentional Windows Resource Object:** `gui/rsrc_windows_amd64.syso` is an intentional binary resource object embedding the manifest (`armgui.exe.manifest`) and icon (`armax_logo.ico`) required by the Go toolchain on Windows amd64. It can be reproducibly rebuilt using `rsrc` as documented in Section 2.
- **Reproducible Build Flags:** `-trimpath` removes file system paths from compiled binaries; `-ldflags="-s -w -buildid="` strips debug symbols and ensures reproducible output across build environments.

---

## 5. Official Release Status

- **Latest Release (`v1.0.3`):** **Active**
  - **Release Tag:** [`v1.0.3`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.3)
  - **Status:** Public Open Testnet -- ArmaxHash v2 Stream-Fast & Hardening
  - **Target Architectures:**
    - `armax-v1.0.3-linux-amd64.tar.gz` (Linux 64-bit x86)
    - `armax-v1.0.3-linux-arm64.tar.gz` (Linux 64-bit ARM / Raspberry Pi / SBC)
    - `armax-v1.0.3-windows-amd64.zip` (Windows 64-bit x86; includes `armgui.exe`)
    - `armax-v1.0.3-darwin-arm64.tar.gz` (macOS Apple Silicon ARM64)
  - **Verification:** Automatically validated via GitHub Actions with race detection, staticcheck, govulncheck, and cryptographic SHA256SUMS aggregation.

- **Previous Maintenance Release (`v1.0.1`):**
  - **Release Tag:** [`v1.0.1`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.1)
  - **Status:** Public Open Testnet -- BIP-35 Mempool Sync & Outbound NAT Support

- **Initial Open Testnet Release (`v1.0.0`):**
  - **Release Tag:** [`v1.0.0`](https://github.com/acxxcxx/armax-coin/releases/tag/v1.0.0)
  - **Status:** Initial Public Open Testnet Release

