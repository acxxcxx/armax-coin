# Armaxcoin Mining Guide

This guide details how to participate in Proof-of-Work (PoW) mining on the Armaxcoin network using the multi-threaded `armminer` CPU mining engine.

---

## 1. Mining Algorithm Architecture

Armaxcoin employs a dual-phase Proof-of-Work consensus timeline designed to transition from initial bootstrap hashing to accessible, memory-hard CPU mining:

| Block Range | Algorithm | Characteristics | Target Hashrate |
|---|---|---|---|
| **Blocks 0 .. 519** | **SHA-256d** | Double SHA-256 block header hashing | Legacy bootstrap phase |
| **Blocks 520 .. 2499** | **ArmaxHash v1** | Memory-hard hybrid: **Argon2id** (2 MB memory cost, 1 time iteration) chained with **BLAKE2b** | CPU-oriented, consumer hardware accessible (~50-300 H/s per thread) |
| **Blocks 2500+ (Active)** | **ArmaxHash v2** | Stream-Fast chained **BLAKE2b-512 + BLAKE2b-256** PoW with domain separation | High-throughput, 0-heap allocation, ARM & commodity CPU friendly (~1-5 MH/s per thread) |

### Why ArmaxHash v2?
ArmaxHash v1 utilized Argon2id (2 MB RAM per hash), which provided an accessible transition from SHA-256d. However, repeated 2 MB memory allocations per hash caused memory bus saturation and thermal throttling on cache-constrained edge devices (such as ARM SBCs with small L2/L3 caches).

ArmaxHash v2 (activated at block #2500 on testnet via scheduled hard fork) replaces memory-churning allocations with high-speed, zero-allocation chained BLAKE2b cryptographic hashing with strict network-domain separation (`ARMX-BLAKE2B-V2`). This achieves:
- **Zero GC Memory Overhead:** 0 B/op heap allocation in mining tight loops.
- **Microsecond Verification:** Instantaneous consensus validation across node networks.
- **ARM & Commodity CPU Friendly:** Runs smoothly on laptops, desktop CPUs, and low-power ARM devices without thermal throttling or RAM bus exhaustion.
- **100% Historical Continuity:** All blocks 0..519 (SHA-256d) and 520..2499 (ArmaxHash v1) remain fully valid and verifiable by all nodes on replay.

---

## 2. Tokenomics & Block Rewards

- **Initial Block Reward:** `60.00000000 ARMX` per block.
- **Halving Interval:** Every `1,050,000` blocks (~4 years at 120-second target block time).
- **Hard Supply Ceiling:** `126,000,000.00000000 ARMX` maximum supply.
- **Target Block Time:** `120` seconds (2 minutes).
- **Difficulty Adjustment:** **LWMA-1** (Linear Weighted Moving Average, window $N=60$ blocks).
- **Coinbase Maturity:** 10 confirmations on Testnet (100 blocks on Mainnet) before mined rewards can be spent.

---

## 3. Prerequisites

1. A running Armaxcoin full node (`armcoind`) with local RPC active on `127.0.0.1:19333`.
2. A valid Armaxcoin post-quantum recipient address (`armx1...`) generated via `armwallet` or `armgui`.
3. The compiled `armminer` binary.

---

## 4. Mining on Testnet

### Step 1: Obtain a Miner Address

```bash
# Generate a testnet wallet
./armwallet -network testnet -create -wallet miner_wallet.json

# View the address
./armwallet -network testnet -balance -wallet miner_wallet.json
```
Copy your public Bech32 address (`armx1...`).

### Step 2: Ensure Node is Running

The miner interacts with the local `armcoind` RPC daemon to fetch mining candidate block templates and submit completed nonces. Ensure your node is fully synced:

```bash
./armcoind -network testnet
```

### Step 3: Launch `armminer`

Launch `armminer` specifying your reward address and thread count:

```bash
# Mine with 4 threads
./armminer -network testnet \
  -miner armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308 \
  -threads 4 \
  -rpc http://127.0.0.1:19333
```

On Windows:
```powershell
.\armminer.exe -network testnet -miner armx1... -threads 4 -rpc http://127.0.0.1:19333
```

### Expected Output

```text
==================================================
 ARMAXCOIN (ARMX) - MULTI-THREADED MINER
==================================================
[+] Network Profile  : TESTNET
[+] Miner Address    : armx1...
[+] Worker Threads   : 4
[+] Target RPC Node  : http://127.0.0.1:19333
[+] Algorithm Active : armaxhash-v2 (Stream-Fast Chained BLAKE2b)
==================================================
[10:35:10] [*] Mining Block #2550 (armaxhash-v2) | Prev: 0029b3104e12... | Txs: 1 | Diff: 4.0000 | Reward: 60 ARMX
[10:35:20] [ Mining] Block #2550 | 4 threads | 5.82 MH/s (Total: 58200000 hashes, 10s)
[10:35:45] [OK BLOCK SOLVED!] Height #2550 | Hash: 0000b48f129c1e... | Nonce: 4920148 | Diff: 4
```

---

## 5. Performance & Thread Optimization

- **Thread Selection:**
  For optimal hashrate without system stutter, set `-threads` equal to the physical core count of your machine (e.g. `4` or `8`). Leaving 1 core free for system and node operations (`NumCPU - 1`) is recommended on dedicated mining desktops.
- **Memory Footprint:**
  Under ArmaxHash v2, memory consumption is virtually zero (a tiny stack buffer per worker thread, 0 heap allocations).
- **Thermal Management:**
  ArmaxHash v2 utilizes high-speed cryptographic hashing without memory bus stalling, keeping temperatures balanced across all CPU cores. Ensure adequate ventilation on sustained high-thread mining.

---

## 6. Mining Verification & Monitoring

To monitor your mining rewards:

```bash
# Check wallet balance
./armwallet -network testnet -balance -wallet miner_wallet.json
```

Or query block details via RPC:
```bash
curl -s http://127.0.0.1:19333/block/latest | jq .
```
Verify that `"miner"` matches your address and `"merkle_root"` reflects the coinbase transaction.

---

## 7. Pool Mining Notice

The current open testnet exclusively supports **solo mining** via HTTP RPC template polling (`/mine` and `/block/submit`). Stratum protocol support and mining pool software are planned for future development and are not currently active.
