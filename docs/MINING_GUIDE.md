# Armaxcoin Mining Guide

This guide details how to participate in Proof-of-Work (PoW) CPU mining on the Armaxcoin network using the standalone `armminer` engine (integrated zero-alloc multi-threaded CPU + Stratum TCP/TLS) or the **CPU Miner** tab (`"Waiting for Sync"` protected) in Desktop Wallet GUI v2 (`armgui`).

---

## 1. Mining Algorithm Architecture

Armaxcoin employs an evolutionary Proof-of-Work consensus timeline designed to transition from initial bootstrap hashing to accessible, high-performance CPU mining:

| Block Range | Algorithm | Characteristics |
|---|---|---|
| **Blocks 0 .. 519** | **SHA-256d** | Double SHA-256 block header hashing; legacy bootstrap phase |
| **Blocks 520 .. 2499** | **ArmaxHash v1** | Network/header-derived BLAKE2b-512 prehash, Argon2id ($m=2048$ KiB, $t=2$, $p=1$), and BLAKE2b-256 finalization |
| **Blocks 2500+ (Active)** | **ArmaxHash v2** | Stream-Fast chained **BLAKE2b-512 + BLAKE2b-256** PoW with domain separation; integrated `V2JobHasher` stack prefix cache achieves 0 allocations/op |

> [!NOTE]
> For the complete canonical Proof-of-Work specification across network profiles, see [docs/CONSENSUS_AND_NETWORK.md](CONSENSUS_AND_NETWORK.md). `core.MainnetLaunched` remains strictly `false` (invoking `-network mainnet` fails closed). Parameters currently in `MainnetProfile` (**ArmaxHash v2 Stream-Fast** from Block `#1+`, immutable SHA-256d Genesis `#0`, **Armax-ASERT-AW**, C1/C2/C6 timestamps, and `TxBinaryV1`) constitute a **provisional pre-launch staging configuration** for isolated `-local-mainnet` testing — not a finalized Mainnet launch baseline — and remain subject to change prior to launch freeze.

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
- **Difficulty Adjustment:** **Armax-ASERT-AW** (Canonical integer `aserti3-2d`, active from Block #13,335; previously TEDR anti-stall at #13,293 and legacy LWMA-1 $N=60$).
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
# Generate a testnet wallet (-file is primary; -wallet is an alias)
./armwallet -network testnet -create -file miner_wallet.json

# View the address
./armwallet -network testnet -address -file miner_wallet.json
```
Copy your public Bech32 address (`armx1...`).

### Step 2: Ensure Node is Running

The miner interacts with the local `armcoind` RPC daemon to fetch mining candidate block templates and submit completed nonces. Both `/mine` and `/getblocktemplate` remain gated behind `HTTP 503 Service Unavailable` until initial peer synchronization completes:
- **`armgui` (CPU Miner tab):** Clicking Start while syncing enters **`"Waiting for Sync"`** mode (`WAITING FOR SYNC` badge and `#minerSyncBanner` progress bar), holding back `POST /mine` requests until sync finishes and starting automatically once `synced: true`. When paired with an older `v1.0.11` daemon lacking the `synced` field on `/stats`, `armgui` falls back cleanly to checking `peers_active >= 1`.
- **`armminer`:** Automatically detects `HTTP 503` syncing responses (`errNodeSyncing`) and waits until the local node catches up to peer tips before hashing.

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
2. **Pre-Shared Mine Key (`X-Mine-Key`):** If the node daemon was configured with a pre-shared mine key (`mine_key` in `armax.conf` or `ARMX_MINE_KEY` environment variable; `armcoind` does not accept `-minekey` as a CLI flag), pass `-key <secret>` to `armminer` (or set `ARMX_MINER_KEY`). In v1.0.7, `X-Mine-Key` authentication is evaluated first before falling back to HTTP Basic Auth and records authentication success, preventing legitimate miners from being locked out by IP rate limiters.
3. **HTTP Basic Auth:** If the node was configured with `-rpcuser <user> -rpcpassword <pass>`, pass `-user <user> -pass <pass>` to `armminer`.

---

## 5. Performance & Multi-Thread CPU Optimization

- **Integrated Prefix-Caching Engine (`armaxhash.V2JobHasher`):**
  Both `core.Blockchain.MineBlockContext` and `armminer` (solo RPC + Stratum v1) use the stack-allocated `armaxhash.V2JobHasher` (`NewV2JobHasher`, `UpdatePrefix`, `HashNonce`) with an inline `[768]byte` buffer to cache the immutable ArmaxHash v2 Stage-1 prefix across nonces (`0 B/op`, `0 allocs/op`, bit-identical with `HashV2`).
- **64-Byte Cache-Line Worker Slots & Sliding-EMA Telemetry (`miner/cpu_optimizer.go`, `core/blockchain.go`):**
  Per-thread counters use 64-byte cache-line padded `WorkerSlot` / `miningWorkerSlot` structs (`_ [56]byte`) and 1-second Sliding-EMA smoothing (`alpha = 0.3`), eliminating cross-core MESI cache-line false sharing during parallel hashing. Workers check cancellation via an isolated atomic `stopState` flag (every 256 hashes on fast PoW, every hash on Argon2id v1), and Stratum workers read target/job updates via lock-free `atomic.Pointer`.
- **Contiguous Positive Nonce Partitions & Thread Selection (`GetOptimalCPUWorkerThreads`):**
  Each worker receives a contiguous positive `int64` range (`startNonce + int64(workerID)*(1<<36)`, incrementing `b.Nonce++`), ensuring zero overlap and exact IEEE-754 `< 2^53` integer serialization compatibility with `v1.0.11`. When `-threads` is omitted in `armminer` or initialized in `armgui`, thread count defaults to physical CPU cores (`GetOptimalCPUWorkerThreads`), avoiding SMT/Hyper-Threading contention and leaving headroom for system and P2P tasks.
- **Memory Footprint:**
  ArmaxHash v2 does not use the Argon2 memory-hard stage. Memory use of the complete miner and node depends on header construction, networking, and other runtime activity; the hash benchmark alone does not measure it.
- **Thermal Management:**
  Sustained CPU mining can increase power use and temperatures. Monitor the device and use appropriate thread count settings and cooling.
- **Clock Synchronization & Active Unified Block 20,000 Rules (`v1.0.11+` / `v1.0.12`):**
  From testnet block **20,000** (active), Proposal C6 narrows candidate block timestamps to at most **600 seconds (10 minutes)** ahead of the validating node's clock (down from the legacy 7,200s window), and `TxBinaryV1` binary block size accounting (`BlockBinarySizeActivationHeight = 20000`) expands 2 MB block capacity from **185** to **375 user ML-DSA-65 transactions**.
  - `armminer` aligns candidate block timestamps to the node's reference template time when the local machine clock leads by > 60 seconds (`ClockSkewWarnSeconds`), while `armcoind` (`/getblocktemplate`) clamps candidate timestamps within `maxFutureLimit - minTemplateTimestampSafetyBuffer` (`540s` post-fork).
  - If a candidate block timestamp approaches the 10-minute future ceiling, `futureLimitWaitWithMinTime` in `miner/miner.go` applies an **adaptive wait margin**: **`inheritedTipSafetyMargin = 5` seconds** when clamping to an inherited forward-drifted parent (`blockTs <= minTs`, preventing honest miner starvation if competing miners mine `dt = 0` tips near `+600s`), vs **`futureLimitSafetyMargin = 30` seconds** when advancing under local clock skew (`blockTs > minTs`).
  - **Operator Requirement:** Maintain active NTP clock synchronization (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows). Stratum and remote miners stamp with host time and are not compensated by local RPC hooks.

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

## 7. Pool & Stratum Support (`TCP` & `TLS`)

- **Solo Mining Architecture:** The primary and verified testnet mining path is **solo mining** against the local `armcoind` daemon via HTTP RPC (`GET /getmininginfo`, `GET /getblocktemplate`, `POST /block/submit`). The node daemon itself does **not** host a Stratum server.
- **Stratum v1 Client (`stratum+tcp://` & `stratum+ssl://` / `stratum+tls://`):** `armminer` includes a built-in native Stratum v1 client supporting both plaintext TCP and TLS 1.2+ pool endpoints, difficulty target negotiation, worker naming (`.worker`), per-thread nonce partitioning, clean job handling, and multi-endpoint failover:
  ```bash
  # Plaintext Stratum TCP
  ./armminer -pool stratum+tcp://pool.example.com:3333 -user <YOUR_ARMX_ADDRESS>.worker1 -pass x -threads 4

  # Encrypted Stratum TLS
  ./armminer -pool stratum+ssl://pool.example.com:3443 -user <YOUR_ARMX_ADDRESS>.worker1 -pass x -threads 4
  ```
  In v1.0.7 (`HIGH-05`), `set_difficulty` RPC parsing guards against `Inf` and `NaN` float values before integer conversion, preventing thread crashes during remote difficulty updates.
- **Operational Boundary:** External Stratum mining pool software is operated independently outside this repository. Operators and miners should confirm TLS certificates and pool difficulty settings before sustained production deployment.
