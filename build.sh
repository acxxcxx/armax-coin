#!/bin/bash
set -e

echo "=================================================="
echo "  COMPILING ARMAXCOIN (ARMX) ECOSYSTEM"
echo "=================================================="

echo "[+] Compiling Daemon Node (armcoind)..."
go build -ldflags="-s -w" -o armcoind main.go

echo "[+] Compiling CPU Miner (armminer)..."
go build -ldflags="-s -w" -o armminer miner/miner.go

echo "[+] Compiling Transaction Sender CLI (armsend)..."
go build -ldflags="-s -w" -o armsend ./cli/armsend

echo "[+] Compiling Wallet Manager CLI (armwallet)..."
go build -ldflags="-s -w" -o armwallet ./cli/armwallet

echo "=================================================="
echo "[OK] ALL BINARIES SUCCESSFULLY COMPILED!"
echo "    - armcoind   : Daemon Node, P2P Engine & Web Server"
echo "    - armminer   : CPU Miner Client"
echo "    - armsend    : CLI Transaction Sender Tool"
echo "    - armwallet  : CLI Wallet Manager Tool"
echo "=================================================="
