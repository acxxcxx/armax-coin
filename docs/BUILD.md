# Armaxcoin Build & Compilation Guide

This document describes the compilation process, toolchain requirements, cross-compilation commands, and reproducible build procedures for the Armaxcoin suite.

---

## 1. Prerequisites

- **Go Toolchain:** Go `1.27` or higher is required; use **`1.27.4`** or newer (remediates `GO-2026-6809` and `GO-2026-6859`). `go.mod` pins `go 1.27.0` and `toolchain go1.27.4`.
  - Reason: Armaxcoin relies on the Go standard library package `crypto/mldsa` (implementing NIST FIPS 204 Module-Lattice-Based Digital Signature Algorithm ML-DSA-65).
  - Check your compiler version:
    ```bash
    go version
    ```
- **Operating Systems Supported:**
  - Linux (`amd64`, `arm64`)
  - Windows (`amd64`)
  - macOS (`arm64`)

---

## 2. Standard Local Compilation

To build all 5 binaries locally:

### On Linux / macOS:
```bash
# Make build script executable and compile
chmod +x scripts/build.sh
./scripts/build.sh
```

Or compile individual binaries manually:
```bash
# 1. Main Node Daemon (armcoind)
go build -trimpath -o armcoind ./main.go

# 2. CPU Miner (armminer — pure Go CGO_ENABLED=0)
CGO_ENABLED=0 go build -trimpath -o armminer ./miner

# 3. CLI Transaction Sender (armsend)
go build -trimpath -o armsend ./cli/armsend

# 4. CLI Wallet Manager (armwallet)
go build -trimpath -o armwallet ./cli/armwallet

# 5. Desktop GUI Wallet v2 & Node Explorer (armgui — pure Go CGO_ENABLED=0)
# Uses embedded Microsoft Edge WebView2 + Shell_NotifyIconW system tray on Windows,
# and local web server with browser interop + POSIX 0600 fallback on Linux/WSL/macOS.
# MUST compile package directory './gui' to embed rsrc_windows_amd64.syso (manifest version 1.0.12.0 & icon).
CGO_ENABLED=0 go build -trimpath -ldflags="-H=windowsgui" -o armgui.exe ./gui
```

### On Windows (PowerShell):
```powershell
.\scripts\build.ps1
# Or cross-compile all 3 core targets (15 binaries):
.\scripts\build-all.ps1
```

---

## 3. Cross-Compilation Commands

To compile for target platforms from any host OS (all 5 binaries compile cleanly with `CGO_ENABLED=0`):

### Linux / amd64:
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-amd64/armcoind ./main.go
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-amd64/armminer ./miner
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-amd64/armsend ./cli/armsend
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-amd64/armwallet ./cli/armwallet
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-amd64/armgui ./gui
```

### Linux / arm64 (Raspberry Pi, Orange Pi, Armbian):
```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-arm64/armcoind ./main.go
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-arm64/armminer ./miner
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-arm64/armsend ./cli/armsend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-arm64/armwallet ./cli/armwallet
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/linux-arm64/armgui ./gui
```

### Windows / amd64:
```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/windows-amd64/armcoind.exe ./main.go
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/windows-amd64/armminer.exe ./miner
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/windows-amd64/armsend.exe ./cli/armsend
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/windows-amd64/armwallet.exe ./cli/armwallet
# Package path './gui' links rsrc_windows_amd64.syso (manifest version 1.0.12.0 & icon) and dynamically loads WebView2Loader.dll at runtime
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid= -H windowsgui" -o dist/windows-amd64/armgui.exe ./gui
cp gui/armgui.exe.manifest dist/windows-amd64/
```

### macOS / arm64 (Apple Silicon):
```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/darwin-arm64/armcoind ./main.go
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/darwin-arm64/armminer ./miner
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/darwin-arm64/armsend ./cli/armsend
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/darwin-arm64/armwallet ./cli/armwallet
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o dist/darwin-arm64/armgui ./gui
```

---

## 4. Reproducible Builds

Armaxcoin supports bit-for-bit deterministic and reproducible compilation across clean build environments:

### Required Compiler Flags:
1. **`-trimpath`**: Strips host-specific filesystem paths from all compiled debug and symbol tables.
2. **`-ldflags="-s -w -buildid="`**:
   - `-s`: Strips symbol table.
   - `-w`: Strips DWARF debug information.
   - `-buildid=`: Strips the non-deterministic compiler build identifier hash.
3. **`CGO_ENABLED=0`**: Ensures static linking without dynamic host C library dependencies.

### Verification of Reproducibility:
To verify that two consecutive builds on the same Go toolchain produce bit-for-bit identical outputs:

```bash
# Build 1
go build -trimpath -ldflags="-s -w -buildid=" -o armcoind_1 ./main.go
sha256sum armcoind_1

# Build 2
go build -trimpath -ldflags="-s -w -buildid=" -o armcoind_2 ./main.go
sha256sum armcoind_2

# Compare
cmp armcoind_1 armcoind_2 && echo "Builds are bit-for-bit identical!"
```

Checksum generation for release packages:
```bash
sha256sum armcoind armminer armsend armwallet armgui > SHA256SUMS
```

---

## 5. Running Verification Test Suites

Armaxcoin maintains comprehensive regression and concurrency test suites. Test hooks enable isolated profile overrides during testing:

```bash
# Static analysis and vet check
go vet -tags testhooks ./...

# Run complete repository unit tests
go test -tags testhooks ./...

# Run core consensus tests with race detector
go test -v -race -tags testhooks ./core

# Run node P2P network tests
go test -v -tags testhooks ./node
```
