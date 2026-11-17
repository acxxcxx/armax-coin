#!/bin/bash
set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"
mkdir -p bin

echo "=================================================="
echo "  COMPILING ARMAXCOIN (ARMX) ECOSYSTEM"
echo "=================================================="

echo "[+] Compiling Daemon Node (bin/armcoind)..."
go build -ldflags="-s -w" -o bin/armcoind main.go

echo "[+] Compiling Miner (bin/armminer)..."
go build -ldflags="-s -w" -o bin/armminer ./miner

echo "[+] Compiling Transaction Sender CLI (bin/armsend)..."
go build -ldflags="-s -w" -o bin/armsend ./cli/armsend

echo "[+] Compiling Wallet Manager CLI (bin/armwallet)..."
go build -ldflags="-s -w" -o bin/armwallet ./cli/armwallet

echo "=================================================="
echo "[OK] ALL BINARIES SUCCESSFULLY COMPILED into bin/!"
echo "    - bin/armcoind   : Daemon Node, P2P Engine & Web Server"
echo "    - bin/armminer   : PoW Miner Client"
echo "    - bin/armsend    : CLI Transaction Sender Tool"
echo "    - bin/armwallet  : CLI Wallet Manager Tool"
echo "=================================================="