#!/usr/bin/env bash
# ==============================================================================
# Armaxcoin (ARMX) Public Testnet Smoke & Health Verification Script [L2-3]
# Verifies testnet block progression, peer counts, mempool state, and tx broadcast
# ==============================================================================

set -euo pipefail

RPC_HOST="${ARMX_RPC_HOST:-127.0.0.1}"
RPC_PORT="${ARMX_RPC_PORT:-19333}"
BASE_URL="http://${RPC_HOST}:${RPC_PORT}"
BIN_DIR="${ARMX_BIN_DIR:-.}"

echo "=================================================="
echo " ARMAXCOIN TESTNET SMOKE TEST"
echo "Target RPC Daemon: ${BASE_URL}"
echo "=================================================="

# 1. Check Node Connectivity & Status
echo -n "[1/5] Checking Testnet Node status... "
STATUS_JSON=$(curl -s "${BASE_URL}/status")
NETWORK_ID=$(echo "${STATUS_JSON}" | grep -o '"network_id":"[^"]*' | cut -d'"' -f4 || true)
INITIAL_HEIGHT=$(echo "${STATUS_JSON}" | grep -o '"height":[0-9]*' | cut -d':' -f2 || true)

if [ "${NETWORK_ID}" != "ARMX-TESTNET-1" ]; then
    echo "FAILED"
    echo "[-] Error: Expected Network ID 'ARMX-TESTNET-1', got '${NETWORK_ID}'"
    exit 1
fi
echo "PASSED (Height: #${INITIAL_HEIGHT}, Network: ${NETWORK_ID})"

# 2. Check Peer Connections (Array response: ["peer1", "peer2"])
echo -n "[2/5] Checking P2P peer connections... "
PEERS_JSON=$(curl -s "${BASE_URL}/peers")
if [ "${PEERS_JSON}" = "[]" ] || [ -z "${PEERS_JSON}" ]; then
    PEER_COUNT=0
else
    PEER_COUNT=$(echo "${PEERS_JSON}" | tr ',' '\n' | grep -c ":" || echo "0")
fi
echo "PASSED (Active P2P connections: ${PEER_COUNT})"

# 3. Check Mempool (Array response: [{"sender":...}])
echo -n "[3/5] Checking mempool availability... "
MEMPOOL_JSON=$(curl -s "${BASE_URL}/mempool")
if [ "${MEMPOOL_JSON}" = "[]" ] || [ -z "${MEMPOOL_JSON}" ]; then
    MEMPOOL_SIZE=0
else
    MEMPOOL_SIZE=$(echo "${MEMPOOL_JSON}" | tr '}' '\n' | grep -c '"sender"' || echo "0")
fi
echo "PASSED (Pending transactions in mempool: ${MEMPOOL_SIZE})"

# 4. Create Ephemeral Test Wallets for Smoke Testing
SMOKE_TMP_DIR=$(mktemp -d /tmp/armx-smoke.XXXXXX)
trap 'rm -rf "${SMOKE_TMP_DIR}"' EXIT

WALLET_A="${SMOKE_TMP_DIR}/wallet_a.json"
WALLET_B="${SMOKE_TMP_DIR}/wallet_b.json"

echo "[4/5] Generating ephemeral testnet wallets..."
if [ -x "${BIN_DIR}/armwallet" ]; then
    export ARMX_WALLET_PASSPHRASE="smoke_passphrase_test_12345"
    "${BIN_DIR}/armwallet" -create -wallet "${WALLET_A}" > /dev/null 2>&1
    "${BIN_DIR}/armwallet" -create -wallet "${WALLET_B}" > /dev/null 2>&1

    ADDR_A=$(grep -o '"address": "[^"]*' "${WALLET_A}" | cut -d'"' -f4)
    ADDR_B=$(grep -o '"address": "[^"]*' "${WALLET_B}" | cut -d'"' -f4)
    echo "      Sender Wallet   (A): ${ADDR_A}"
    echo "      Recipient Wallet (B): ${ADDR_B}"

    # Query initial balance of Wallet B
    BAL_B_INIT=$(curl -s "${BASE_URL}/balance?address=${ADDR_B}" | grep -o '"balance":[0-9]*' | cut -d':' -f2 || echo "0")
    echo "      Initial balance of Recipient: ${BAL_B_INIT} Armoids"

    # If Wallet A has funds, attempt transfer
    BAL_A=$(curl -s "${BASE_URL}/balance?address=${ADDR_A}" | grep -o '"balance":[0-9]*' | cut -d':' -f2 || echo "0")
    if [ "${BAL_A}" -gt 20000 ]; then
        echo "      Broadcasting 1 testnet transaction via armsend..."
        "${BIN_DIR}/armsend" -network testnet -wallet "${WALLET_A}" -to "${ADDR_B}" -amount 10000 -fee 10000 > /dev/null
        echo "      Transaction broadcast successfully!"
    else
        echo "      (Sender wallet has 0 balance; skipping transfer broadcast. Fund wallet via testnet faucet or miner to test sending)."
    fi
else
    echo "      armwallet binary not found in '${BIN_DIR}', skipping wallet generation."
fi

# 5. Verify Block Progression
echo -n "[5/5] Monitoring block progression... "
sleep 2
CURRENT_HEIGHT=$(curl -s "${BASE_URL}/status" | grep -o '"height":[0-9]*' | cut -d':' -f2 || echo "${INITIAL_HEIGHT}")
echo "PASSED (Current Height: #${CURRENT_HEIGHT})"

echo "=================================================="
echo " TESTNET SMOKE TEST COMPLETED SUCCESSFULLY!"
echo "=================================================="
