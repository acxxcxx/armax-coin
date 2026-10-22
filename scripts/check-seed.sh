#!/usr/bin/env bash
# ==============================================================================
# scripts/check-seed.sh
# Armaxcoin Seed Node Health & Connectivity Inspector [L4-6]
# ==============================================================================
# Usage:
#   ./scripts/check-seed.sh [HOST] [NETWORK]
# Example:
#   ./scripts/check-seed.sh 127.0.0.1 testnet
#   ./scripts/check-seed.sh testnet-seed1.armax-coin.cloud testnet
# ==============================================================================

set -euo pipefail

HOST="${1:-127.0.0.1}"
NETWORK="${2:-testnet}"

if [ "${NETWORK}" = "mainnet" ]; then
    P2P_PORT=19334
    RPC_PORT=19332
else
    P2P_PORT=19335
    RPC_PORT=19333
fi

echo "=================================================="
echo " ARMAXCOIN - SEED NODE HEALTH MONITOR"
echo "=================================================="
echo "Target Host    : ${HOST}"
echo "Network Profile: ${NETWORK^^}"
echo "Expected P2P   : ${P2P_PORT} (Public Ingress)"
echo "Expected RPC   : ${RPC_PORT} (Strict Loopback Only)"
echo "=================================================="

ERRORS=0

# 1. Probe P2P Port (Must be OPEN)
echo -n "[+] Testing P2P port ${P2P_PORT} connectivity... "
P2P_OPEN=0

if command -v nc &>/dev/null; then
    if nc -z -w 3 "${HOST}" "${P2P_PORT}" 2>/dev/null; then
        P2P_OPEN=1
    fi
elif (echo > "/dev/tcp/${HOST}/${P2P_PORT}") 2>/dev/null; then
    P2P_OPEN=1
fi

if [ "${P2P_OPEN}" -eq 1 ]; then
    echo "OPEN (PASS)"
else
    echo "CLOSED or UNREACHABLE (FAIL)"
    echo "    [-] ERROR: Seed node P2P port ${P2P_PORT} is not accepting connections!"
    ERRORS=$((ERRORS + 1))
fi

# 2. Probe RPC Port Security & Local Metrics
is_loopback=0
if [ "${HOST}" = "127.0.0.1" ] || [ "${HOST}" = "localhost" ] || [ "${HOST}" = "::1" ]; then
    is_loopback=1
fi

if [ "${is_loopback}" -eq 1 ]; then
    echo "[+] Probing local RPC daemon on 127.0.0.1:${RPC_PORT}..."
    STATUS_JSON=$(curl -s --max-time 3 "http://127.0.0.1:${RPC_PORT}/status" 2>/dev/null || true)
    STATS_JSON=$(curl -s --max-time 3 "http://127.0.0.1:${RPC_PORT}/stats" 2>/dev/null || true)

    if [ -n "${STATS_JSON}" ]; then
        HEIGHT=$(grep -o '"height":[0-9]*' <<< "${STATS_JSON}" | cut -d: -f2 || echo "N/A")
        PEERS_ACTIVE=$(grep -o '"peers_active":[0-9]*' <<< "${STATS_JSON}" | cut -d: -f2 || echo "0")
        PEERS_KNOWN=$(grep -o '"peers_known":[0-9]*' <<< "${STATS_JSON}" | cut -d: -f2 || echo "0")
        VERSION=$(grep -o '"version":"[^"]*' <<< "${STATS_JSON}" | cut -d'"' -f4 || echo "N/A")
        SUPPLY=$(grep -o '"circulating_supply":[^,]*' <<< "${STATS_JSON}" | cut -d: -f2 || echo "N/A")

        echo "    [*] Node Version  : ${VERSION}"
        echo "    [*] Current Height: ${HEIGHT}"
        echo "    [*] Active Peers  : ${PEERS_ACTIVE} (Known: ${PEERS_KNOWN})"
        echo "    [*] Circ. Supply  : ${SUPPLY} ARMX"
        echo "[OK] Local RPC interface responsive and healthy."
    else
        echo "    [-] Warning: Local RPC daemon did not respond on port ${RPC_PORT}."
    fi
else
    # External target host: RPC port MUST be closed/firewalled to the public internet!
    echo -n "[+] Security Audit: Verifying RPC port ${RPC_PORT} is BLOCKED from public internet... "
    RPC_EXPOSED=0
    if command -v nc &>/dev/null; then
        if nc -z -w 3 "${HOST}" "${RPC_PORT}" 2>/dev/null; then
            RPC_EXPOSED=1
        fi
    elif (echo > "/dev/tcp/${HOST}/${RPC_PORT}") 2>/dev/null; then
        RPC_EXPOSED=1
    fi

    if [ "${RPC_EXPOSED}" -eq 1 ]; then
        echo "EXPOSED! (CRITICAL SECURITY FAILURE)"
        echo "    [-] CRITICAL: RPC port ${RPC_PORT} is accessible publicly on ${HOST}!"
        echo "        RPC MUST be restricted strictly to 127.0.0.1 loopback."
        ERRORS=$((ERRORS + 1))
    else
        echo "BLOCKED / CLOSED (PASS)"
        echo "    [OK] Security invariant verified: RPC daemon is not exposed to public traffic."
    fi
fi

echo "=================================================="
if [ "${ERRORS}" -eq 0 ]; then
    echo " Seed node check PASSED! All invariants satisfied."
    exit 0
else
    echo "ERROR Seed node check FAILED with ${ERRORS} error(s)."
    exit 1
fi
