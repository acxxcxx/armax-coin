# Armaxcoin Mining Guide

This guide details how to participate in Proof-of-Work (PoW) mining on the Armaxcoin network using the multi-threaded `armminer` CPU mining engine.

---

## 1. Mining Algorithm Architecture

Armaxcoin employs a dual-phase Proof-of-Work consensus timeline designed to transition from initial bootstrap hashing to accessible, memory-hard CPU mining:

| Block Range | Algorithm | Characteristics | Target Hashrate |
|---|---|---|---|
| **Blocks 0 .. 519** | **SHA-256d** | Double SHA-256 block header hashing | Legacy bootstrap phase |
| **Blocks 520+ (Active)** | **ArmaxHash** | Memory-hard hybrid: **Argon2id** (2 MB memory cost, 1 time iteration) chained with **BLAKE2b** | CPU-oriented, consumer hardware accessible |

### Why ArmaxHash?
ArmaxHash was specifically engineered and activated on testnet at block #520 to transition away from pure SHA-256 and encourage community CPU mining. Each hash evaluation requires 2 MB of memory access, keeping hashrates bound to CPU/RAM bandwidth without asserting unprovable permanent ASIC immunity.

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
[+] Algorithm Active : ArmaxHash (Argon2id + Blake2b + SHA-256)
==================================================
[10:35:10] Mining Block #560 | Target: 001478a4... | Difficulty: 3
[10:35:45] [OK] BLOCK FOUND! #560 successfully mined! Reward: 60.00000000 ARMX credited
```

---

## 5. Performance & Thread Optimization

- **Thread Selection:**
  For optimal hashrate without system stutter, set `-threads` equal to the physical core count of your machine (e.g. `4` or `8`). Leaving 1 core free for system and node operations (`NumCPU - 1`) is recommended on dedicated mining desktops.
- **Memory Footprint:**
  Each active mining worker allocates approximately 2 MB of RAM for Argon2id state. Mining on 8 threads consumes ~16 MB of scratch memory.
- **Thermal Management:**
  Continuous multi-threaded CPU mining generates substantial CPU heat. Ensure adequate cooling on laptops or small single-board computers (Raspberry Pi).

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
