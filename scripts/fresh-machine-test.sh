#!/usr/bin/env bash
# ==============================================================================
# scripts/fresh-machine-test.sh
# Fresh Machine Verification via Clean Ubuntu Docker Container
# ==============================================================================
set -euo pipefail

echo "=================================================="
echo " ARMAXCOIN - FRESH MACHINE DOCKER VERIFICATION"
echo "=================================================="

if ! command -v docker &> /dev/null; then
    echo "[!] Docker is not installed on this system."
    echo "    To test on a clean machine, install Docker or use an Ubuntu VPS."
    exit 1
fi

echo "[+] Launching fresh Ubuntu 24.04 container with Go 1.27..."
docker run --rm -v "$(pwd):/build" -w /build ubuntu:24.04 bash -c '
    set -euo pipefail
    apt-get update && apt-get install -y curl git build-essential
    echo "[+] Downloading official Go 1.27 toolchain..."
    curl -fsSL https://go.dev/dl/go1.27.0.linux-amd64.tar.gz -o go.tar.gz
    tar -C /usr/local -xzf go.tar.gz
    export PATH=/usr/local/go/bin:$PATH
    go version

    echo "[+] Running reproducible compilation on clean container..."
    go build -trimpath -ldflags="-s -w -buildid=" -o /tmp/armcoind main.go
    go build -trimpath -ldflags="-s -w -buildid=" -o /tmp/armminer miner/miner.go
    go build -trimpath -ldflags="-s -w -buildid=" -o /tmp/armsend ./cli/armsend
    go build -trimpath -ldflags="-s -w -buildid=" -o /tmp/armwallet ./cli/armwallet

    echo "[+] Running release verification inside clean container..."
    ./scripts/verify-release.sh /tmp
    echo " Fresh machine container test PASSED!"
'
