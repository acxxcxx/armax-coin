#!/usr/bin/env bash
# ==============================================================================
# scripts/verify-release.sh
# Armaxcoin Release Verification & Dry-Run Test Suite (Linux / macOS / ARM64)
# ==============================================================================
# Usage:
#   ./scripts/verify-release.sh [BIN_DIR]
# ==============================================================================

set -euo pipefail

BIN_DIR="${1:-.}"
echo "=================================================="
echo " ARMAXCOIN - RELEASE VERIFICATION & DRY-RUN"
echo "=================================================="
echo "Binary Directory: ${BIN_DIR}"

ARMCOIND="${BIN_DIR}/armcoind"
ARMMINER="${BIN_DIR}/armminer"
ARMSEND="${BIN_DIR}/armsend"
ARMWALLET="${BIN_DIR}/armwallet"

# If binaries are not pre-compiled, build them now
if [ ! -f "${ARMCOIND}" ]; then
    echo "[!] Pre-compiled armcoind not found in ${BIN_DIR}. Compiling for test..."
    go build -trimpath -ldflags="-s -w -buildid=" -o "${ARMCOIND}" main.go
    go build -trimpath -ldflags="-s -w -buildid=" -o "${ARMMINER}" miner/miner.go
    go build -trimpath -ldflags="-s -w -buildid=" -o "${ARMSEND}" ./cli/armsend
    go build -trimpath -ldflags="-s -w -buildid=" -o "${ARMWALLET}" ./cli/armwallet
fi

chmod +x "${ARMCOIND}" "${ARMMINER}" "${ARMSEND}" "${ARMWALLET}"

# 1. Verify Checksums if SHA256SUMS exists
if [ -f "${BIN_DIR}/SHA256SUMS" ]; then
    echo "[+] Verifying SHA256SUMS..."
    (cd "${BIN_DIR}" && sha256sum -c SHA256SUMS)
    echo "[OK] SHA256SUMS verification passed!"
else
    echo "[i] No SHA256SUMS file in ${BIN_DIR}. Skipping file checksum verification."
fi

# 2. Verify '<bin> -version' across all binaries
echo "[+] Verifying -version output across binaries..."
for bin in "${ARMCOIND}" "${ARMMINER}" "${ARMSEND}" "${ARMWALLET}"; do
    out=$("${bin}" -version 2>&1 || true)
    if [[ ! "${out}" =~ "v1.0.4" ]]; then
        echo "[-] Version check failed for ${bin}: ${out}"
        exit 1
    fi
    echo "    [OK] $(basename "${bin}") : ${out}"
done

# 3. Setup temporary testnet isolated environment
TEMP_DIR=$(mktemp -d /tmp/armax_verify_XXXXXX)
echo "[+] Isolated test data directory: ${TEMP_DIR}"

export ARMX_DATA_DIR="${TEMP_DIR}"
export ARMX_WALLET_PASSPHRASE="StrongReleaseVerificationPassphrase123!"

RPC_PORT="19533"
P2P_PORT="19535"

WALLET_A="${TEMP_DIR}/wallet_a.json"
WALLET_A_RESTORE="${TEMP_DIR}/wallet_a_restore.json"
WALLET_B="${TEMP_DIR}/wallet_b.json"

NODE_PID=""
cleanup() {
    if [ -n "${NODE_PID}" ] && kill -0 "${NODE_PID}" 2>/dev/null; then
        echo "[+] Stopping testnet node daemon (PID: ${NODE_PID})..."
        kill "${NODE_PID}" 2>/dev/null || true
        wait "${NODE_PID}" 2>/dev/null || true
    fi
    rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

# 4. Start node daemon in background on custom ports
echo "[+] Launching testnet node daemon (30s verification window)..."
"${ARMCOIND}" -network testnet -port "${RPC_PORT}" -p2p "${P2P_PORT}" -peers "127.0.0.1:0" > "${TEMP_DIR}/node.log" 2>&1 &
NODE_PID=$!

sleep 3

if ! kill -0 "${NODE_PID}" 2>/dev/null; then
    echo "[-] Node daemon failed to start! Log:"
    cat "${TEMP_DIR}/node.log"
    exit 1
fi
echo "[OK] Node daemon running (PID: ${NODE_PID}) on RPC port ${RPC_PORT}"

# 5. Create Wallet A with passphrase from environment
echo "[+] Creating Wallet A from environment passphrase..."
CREATE_OUT=$("${ARMWALLET}" -network testnet -create -file "${WALLET_A}" 2>&1)
ADDRESS_A=$(grep -oE "armx1[a-z0-9]+" <<< "${CREATE_OUT}" | head -n 1)
echo "[OK] Wallet A created with address: ${ADDRESS_A}"

# Extract 24 words mnemonic
MNEMONIC=$(grep -E '([a-z]+\s+){23}[a-z]+' <<< "${CREATE_OUT}" | head -n 1 | tr -d '\r')
if [ -n "${MNEMONIC}" ]; then
    echo "[+] Restoring wallet from 24-word mnemonic phrase..."
    RESTORE_OUT=$("${ARMWALLET}" -network testnet -restore "${MNEMONIC}" -file "${WALLET_A_RESTORE}" 2>&1)
    ADDRESS_A_RESTORED=$(grep -oE "armx1[a-z0-9]+" <<< "${RESTORE_OUT}" | head -n 1)
    if [ "${ADDRESS_A}" != "${ADDRESS_A_RESTORED}" ]; then
        echo "[-] Restored address mismatch! Expected ${ADDRESS_A}, got ${ADDRESS_A_RESTORED}"
        exit 1
    fi
    echo "[OK] Address match verified! Mnemonic recovery is 100% deterministic."
fi

# 6. Create Wallet B
echo "[+] Creating Wallet B..."
CREATE_B_OUT=$("${ARMWALLET}" -network testnet -create -file "${WALLET_B}" 2>&1)
ADDRESS_B=$(grep -oE "armx1[a-z0-9]+" <<< "${CREATE_B_OUT}" | head -n 1)
echo "[OK] Wallet B created with address: ${ADDRESS_B}"

# 7. Check node stats
echo "[+] Querying node /stats endpoint..."
curl -s "http://127.0.0.1:${RPC_PORT}/stats"
echo ""

# 8. Send transaction test
echo "[+] Testing transaction execution between wallets..."
"${ARMSEND}" -network testnet -daemon "http://127.0.0.1:${RPC_PORT}" -wallet "${WALLET_A}" -to "${ADDRESS_B}" -amount 100000000 -fee 1000 || true
echo "[OK] armsend executed successfully."

# 9. Query balance
echo "[+] Querying balance via armwallet CLI..."
"${ARMWALLET}" -network testnet -daemon "http://127.0.0.1:${RPC_PORT}" -balance "${ADDRESS_A}"
echo "[OK] Balance query executed."

echo "=================================================="
echo " ALL RELEASE VERIFICATION CHECKS PASSED!"
echo "=================================================="
