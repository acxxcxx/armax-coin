# Armaxcoin Mining Guide

This guide details how to participate in Proof-of-Work (PoW) mining on the Armaxcoin network using the multi-threaded `armminer` CPU mining engine.

---

## 1. Mining Algorithm Architecture

Armaxcoin employs an evolutionary Proof-of-Work consensus timeline designed to transition from initial bootstrap hashing to accessible, high-performance CPU mining:

| Block Range | Algorithm | Characteristics |
|---|---|---|---|
| **Blocks 0 .. 519** | **SHA-256d** | Double SHA-256 block header hashing; legacy bootstrap phase |
| **Blocks 520 .. 2499** | **ArmaxHash v1** | Network/header-derived BLAKE2b-512 prehash, Argon2id ($m=2048$ KiB, $t=2$, $p=1$), and BLAKE2b-256 finalization |
| **Blocks 2500+ (Active)** | **ArmaxHash v2** | Stream-Fast chained **BLAKE2b-512 + BLAKE2b-256** PoW with domain separation; fixed-header hash benchmark reports 0 allocations/op, not end-to-end miner throughput |

> [!NOTE]
> For the complete canonical Proof-of-Work specification across all networks (Testnet and Mainnet profiles), see [docs/CONSENSUS_AND_NETWORK.md](CONSENSUS_AND_NETWORK.md). The unreleased Mainnet profile in code operates under standard Double-SHA256 (`SHA-256d`).

### Why ArmaxHash v2?
ArmaxHash v1 used Argon2id (2048 KiB per hash, 2 iterations). The current code uses the `golang.org/x/crypto` Argon2id implementation; mining performance and memory-system effects depend on the machine and workload.

ArmaxHash v2 is active from block #2500 on testnet. It uses chained BLAKE2b with the `ARMX-POW-V2\0` domain, version byte, network ID, and block header. It is not memory-hard; resistance to specialized hardware (ASIC/FPGA) or high-end GPU acceleration has not been evaluated.
- **Hash Primitive Allocation:** The fixed-header `HashV2` benchmark reports 0 allocations/op. Header construction and the full mining loop are not covered by this measurement.
- **Hardware Results:** Hashrate and thermal behavior vary by hardware and workload; no universal per-core throughput is claimed here.
- **Historical Compatibility:** The node selects the hashing algorithm by block height and validates the historical SHA-256d and ArmaxHash v1 eras before applying v2 from testnet height 2500.

---

## 2. Tokenomics & Block Rewards

- **Initial Block Reward:** `60.00000000 tARMX` per block on testnet (`ARMX` on mainnet, which is locked).
- **Halving Interval:** Every `1,050,000` blocks (~4 years at 120-second target block time).
- **Hard Supply Ceiling:** `126,000,000.00000000 tARMX` maximum testnet supply.
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
./armwallet -network testnet -address -wallet miner_wallet.json
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
# Solo mining via local daemon RPC (auto-detects cookie):
./armminer -network testnet \
  -miner armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308 \
  -threads 4 \
  -daemon http://127.0.0.1:19333
```

On Windows (PowerShell):
```powershell
.\armminer.exe -network testnet -miner armx1... -threads 4 -daemon http://127.0.0.1:19333
```

#### Miner Authentication Options
When connecting to `armcoind`, `armminer` supports three authentication mechanisms:
1. **RPC Cookie (Default):** Auto-detected from `<dataDir>/<network>/armax.cookie` (v1.0.6 isolated location), root `armax.cookie`, or `$ARMX_DATA_DIR`.
2. **Pre-Shared Mine Key (`X-Mine-Key`):** If the node was launched with `-minekey <secret>`, pass `-key <secret>` to `armminer`. In v1.0.7, `X-Mine-Key` authentication is evaluated first before falling back to HTTP Basic Auth and records authentication success, preventing legitimate miners from being locked out by IP rate limiters.
3. **HTTP Basic Auth:** If the node was configured with `-rpcuser <user> -rpcpassword <pass>`, pass `-user <user> -pass <pass>` to `armminer`.

---

## 5. Performance & Thread Optimization

- **Thread Selection:**
  For optimal hashrate without system stutter, set `-threads` equal to the physical core count of your machine (e.g. `4` or `8`). Leaving 1 core free for system and node operations (`NumCPU - 1`) is recommended on dedicated mining desktops.
- **Memory Footprint:**
  ArmaxHash v2 does not use the Argon2 memory-hard stage. Memory use of the complete miner and node depends on header construction, networking, and other runtime activity; the hash benchmark alone does not measure it.
- **Thermal Management:**
  Sustained CPU mining can increase power use and temperatures. Monitor the device and use an appropriate thread count and cooling setup.
- **Clock Synchronization & Block 20,000 Hard Fork (v1.0.7):**
  From testnet block **20,000**, candidate block timestamps may be at most **600 seconds (10 minutes)** ahead of the validating node's clock (down from the legacy 7,200s window).
  - In v1.0.7, `armminer` actively aligns candidate block timestamps to the node's reference template time when the local machine clock leads by > 60 seconds (`ClockSkewWarnSeconds`).
  - If a candidate block timestamp risks exceeding the 10-minute future ceiling, the miner automatically pauses (`futureLimitWait`, up to 30s) and refetches a fresh template rather than submitting an un-minable candidate.
  - **Operator Requirement:** All miners must maintain active NTP clock synchronization (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows). Stratum and remote miners stamp with host time and are not compensated by local RPC hooks.

---

## 6. Mining Verification & Monitoring

To monitor your mining rewards:

```bash
# Check wallet balance
./armwallet -network testnet -balance <YOUR_ARMX_ADDRESS>
```

Or query block details via RPC:
```bash
curl -s 'http://127.0.0.1:19333/block?index=<BLOCK_HEIGHT>' | jq .
```
Verify that `"miner"` matches your address and `"merkle_root"` reflects the coinbase transaction.

---

## 7. Pool & Stratum Support

- **Solo Mining Architecture:** The primary and verified testnet mining path is **solo mining** against the local `armcoind` daemon via HTTP RPC (`GET /getmininginfo`, `GET /getblocktemplate`, `POST /block/submit`). The node daemon itself does **not** host a Stratum server.
- **Stratum v1 Client:** `armminer` includes a built-in native Stratum v1 TCP client (`stratum+tcp://`) supporting difficulty target negotiation, per-thread nonce partitioning, clean job handling, and multi-endpoint failover:
  ```bash
  ./armminer -pool stratum+tcp://pool.example.com:3333 -user worker1 -pass x -threads 4
  ```
  In v1.0.7 (`HIGH-05`), `set_difficulty` RPC parsing guards against `Inf` and `NaN` float values before integer conversion, preventing thread crashes during remote difficulty updates.
- **Operational Boundary:** External Stratum mining pool software is operated independently outside this repository and has not been evaluated in live public staging. Node operators and miners should verify compatibility on isolated test setups before production deployment.
