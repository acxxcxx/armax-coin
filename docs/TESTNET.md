# Armaxcoin Public Testnet Operations & Node Operator Guide

## 1. Network Profile & Port Specifications

The Armaxcoin Public Testnet (`ARMX-TESTNET-1`) operates under isolated network parameters defined in `core/network.go`. Testnet nodes NEVER peer with or accept blocks from Mainnet.

| Service | Port | Network Interface Binding | Firewall Policy |
|---|---|---|---|
| **P2P Gossip & Block Streaming** | **19335** | `0.0.0.0:19335` (All interfaces) | **ALLOW INBOUND** (Public TCP) |
| **JSON-RPC Management API** | **19333** | `127.0.0.1:19333` (Loopback only) | **DENY INBOUND** (Localhost Only) |
| **Web Explorer & Inspector** | **19341** | *(Planned / Removed)* | Inactive / Unreleased |

> [!CRITICAL]
> **RPC Security Isolation (Loopback Only):**  
> The JSON-RPC daemon (`armcoind` port 19333 on testnet, 19332 on mainnet) exposes internal node mutation and status endpoints (`/mine`, `/block/submit`, `/peers`). It **MUST NEVER** be bound to `0.0.0.0` or exposed to public inbound internet traffic.  
> Local administrative CLI tools authenticate via the ephemeral, restrictive `armax.cookie` file on localhost. If remote administrative access is required, node operators must use an SSH tunnel (`ssh -L 19333:127.0.0.1:19333 user@server`) or an authenticated reverse proxy with TLS.

---

## 2. Seed Node Infrastructure & DNS Setup

A dedicated testnet seed node provides reliable 24/7 block dissemination:
- **Operating System:** Ubuntu 22.04 LTS or Debian 12 (x86_64 or aarch64/ARM64).
- **Compute:** Minimum 2 vCPUs, 2 GB RAM, 20 GB SSD storage.
- **Network:** Public static IPv4 address, minimum 10 Mbps sustained uplink.
- **DNS Seed Configuration:**
  - Create standard DNS `A` records pointing to your public seed IP (e.g., `testnet-seed1.armax-coin.cloud` -> `<YOUR_SEED_PUBLIC_IP>`).
  - Set DNS TTL to **300 seconds** (5 minutes) to facilitate rapid failover if an IP address migrates.
  - Multi-seed round-robin: Multiple seed node IPs can be mapped to the same apex or subdomain (`testnet-seed.armax-coin.cloud`) to enable automatic client load balancing and peer discovery.

---

## 3. Firewall Configuration (UFW) & Health Monitoring

Enforce strict ingress policies on public seed and validator nodes:

```bash
# Allow SSH management
sudo ufw allow 22/tcp

# Allow Testnet P2P gossip from public peers
sudo ufw allow 19335/tcp comment 'Armax Testnet P2P'

# Explicitly ensure Testnet RPC is restricted to loopback (do NOT expose 19333!)
sudo ufw deny 19333/tcp comment 'Armax Testnet RPC Private'

# Enable firewall
sudo ufw enable
sudo ufw status verbose
```

### Automated Seed Health Verification:
Use the built-in health monitor script to verify connectivity and security:
```bash
# Verify local seed node (checks P2P port 19335, queries block height, peer count, version)
./scripts/check-seed.sh 127.0.0.1 testnet

# Audit remote public seed from outside (verifies P2P is OPEN and RPC is BLOCKED)
./scripts/check-seed.sh testnet-seed1.armax-coin.cloud testnet
```

---

## 4. Systemd Service Deployment

Create an unprivileged system user and install the testnet systemd unit:

```bash
# 1. Create dedicated armax system user and directory
sudo useradd -r -m -d /var/armax -s /bin/bash armax
sudo mkdir -p /var/armax/testnet
sudo chown -R armax:armax /var/armax

# 2. Deploy binary to /usr/local/bin
sudo cp armcoind /usr/local/bin/armcoind
sudo chmod 755 /usr/local/bin/armcoind

# 3. Install testnet service unit file
sudo cp systemd/armcoind-testnet.service /etc/systemd/system/armcoind-testnet.service

# 4. Reload systemd daemon, enable, and start service
sudo systemctl daemon-reload
sudo systemctl enable --now armcoind-testnet

# 5. Monitor service startup and initial block download
sudo journalctl -u armcoind-testnet -f
```

---

## 5. Mining on Testnet

Mining on testnet is executed using the multi-threaded `armminer` CPU mining engine:

```bash
# 1. Create a testnet wallet or generate reward address
./armwallet -network testnet -create -wallet my_testnet_wallet.json

# 2. Extract public address (Bech32 armx1...)
ADDR=$(grep -o '"address": "[^"]*' my_testnet_wallet.json | cut -d'"' -f4)

# 3. Launch the CPU miner with 4 worker threads (connects to localhost:19333 testnet RPC by default)
./armminer -network testnet -miner "${ADDR}" -threads 4

# 4. Or benchmark local CPU hashing speed:
./armminer -benchmark
```

### Mining Architecture & Telemetry
- **Local Multi-Threaded Engine:** `armminer` queries `/getblocktemplate` from `armcoind`, constructs the candidate block with coinbase reward, and runs parallel worker threads across `-threads N` local CPU workers.
- **Proof-of-Work Algorithm Migration:**
  - **Height $0 \le H < 520$:** Legacy SHA-256d PoW. All historical blocks remain valid and preserved.
   - **Height $520 \le H < 2500$:** **ArmaxHash v1** (BLAKE2b-512 prehash derived from the network ID and header, Argon2id with 2,048 KiB, 1 lane, 2 iterations, then BLAKE2b-256 finalization; see `docs/CONSENSUS_AND_NETWORK.md`).
   - **Height $H \ge 2500$ (active on testnet):** Stream-Fast **ArmaxHash v2** (BLAKE2b-512 over the `ARMX-POW-V2\0` domain, version, network ID, and header, followed by BLAKE2b-256). The fixed-header `HashV2` benchmark reports 0 allocations/op; it does not measure complete block validation or end-to-end miner throughput.
- **Console Telemetry:** Prints real-time hashrate metrics (`H/s`, `kH/s`, `MH/s`) and solve timestamps every stats interval.
- **Dynamic Tip Adaptation:** Automatically queries daemon tip height and aborts stale rounds if a competing peer mines a block first (`HTTP 409 Conflict`).
- **Coinbase Maturity (10 Confirmations on Testnet, 100 on Mainnet):** Freshly mined block rewards enter an on-chain immature queue and cannot be spent until 10 blocks (testnet) or 100 blocks (mainnet) have been confirmed on top of them (consensus-enforced, storage schema v3).
- **Daemon Internal Mining:** Alternatively, `armcoind` can mine in the background using standard flags:
  ```bash
  ./armcoind -gen -genproclimit 4 -miner "${ADDR}"
  ```

### Hashrate and Difficulty Targets
- **Legacy Target (H < 520):** `0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (SHA-256d baseline).
- **ArmaxHash v1 PoW Limit (H = 520 bootstrap):** `000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff` (~4,096 ArmaxHash v1 evaluations).
- **ArmaxHash v2 Target Adjustment (H >= 2500):** LWMA-1 applies the same per-block retargeting formula; actual block intervals depend on network hashrate, timestamps, and random solve times.
- **Dynamic LWMA-1 Target Ceiling:** During ArmaxHash operation the target may rise to Difficulty 1 (`0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff`). This does not guarantee that low-hashrate devices will produce blocks at the target interval.
- **Target Block Time:** 120 seconds (2 minutes).
- **Dynamic Difficulty:** Linear Weighted Moving Average across a 60-block window (LWMA-1). The 120-second block interval is a target, not a guarantee for individual blocks or short observation periods.

### Local Multi-Node Testing (29000-series Ports)
For running multiple local nodes on the same host, use ports in the 29000-range with separate `-datadir`:
```bash
# Node 1 (Bootstrap node)
./armcoind -network testnet -rpcport 29332 -p2pport 29334 -datadir ./node1_data

# Node 2 (Peer connecting to Node 1)
./armcoind -network testnet -rpcport 29336 -p2pport 29338 -peers 127.0.0.1:29334 -datadir ./node2_data

# Miner pointing to Node 1
./armminer -network testnet -rpcport 29332 -miner armx1...

# Wallet checking Node 1 balance
./armwallet -network testnet -rpcport 29332 -balance armx1...
```
*Note: `-rpcport` and `-p2pport` are preferred; legacy `-port` and `-p2p` are retained as backward-compatible aliases with a deprecation warning.*

---

## 6. Backup and Disaster Recovery

The testnet blockchain state is stored in `<datadir>/testnet/blockchain.db` (bbolt ACID key-value store).

### Cold Database Backup
```bash
# Stop daemon before copying bbolt database
sudo systemctl stop armcoind-testnet

# Create compressed backup archive
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
sudo tar -czvf /var/backups/armax_testnet_backup_${TIMESTAMP}.tar.gz -C /var/armax testnet/blockchain.db

# Restart daemon
sudo systemctl start armcoind-testnet
```

### Restoration Runbook
```bash
sudo systemctl stop armcoind-testnet
sudo rm -rf /var/armax/testnet/blockchain.db
sudo tar -xzvf /var/backups/armax_testnet_backup_${TIMESTAMP}.tar.gz -C /var/armax
sudo chown -R armax:armax /var/armax/testnet
sudo systemctl start armcoind-testnet
```

---

## 7. Node Upgrade and Rollback Runbook

### Seamless Upgrade Procedure
1. Verify the current tip height and peer connectivity:
   ```bash
   curl -s http://127.0.0.1:19333/status
   ```
2. Stop the running service:
   ```bash
   sudo systemctl stop armcoind-testnet
   ```
3. Backup the current working binary:
   ```bash
   sudo cp /usr/local/bin/armcoind /usr/local/bin/armcoind.bak
   ```
4. Install the new binary:
   ```bash
   sudo cp armcoind /usr/local/bin/armcoind
   sudo chmod 755 /usr/local/bin/armcoind
   ```
5. Start service and verify log output:
   ```bash
   sudo systemctl start armcoind-testnet
   sudo journalctl -u armcoind-testnet -n 50 --no-pager
   ```
6. Run smoke test script:
   ```bash
   ./scripts/testnet-smoke.sh
   ```

### Emergency Rollback Procedure
If the newly deployed version encounters an unrecoverable consensus failure or DB schema incompatibility:
```bash
sudo systemctl stop armcoind-testnet
sudo cp /usr/local/bin/armcoind.bak /usr/local/bin/armcoind
sudo systemctl start armcoind-testnet
```

---

## 8. Manual Testnet Faucet Operator Guide

To prevent sybil attacks, resource draining, and spam without introducing complex captcha services, Armaxcoin testnet operates on a **Manual Operator Faucet Model**.

> [!CAUTION]
> **DO NOT** deploy automated, unauthenticated public web faucets exposing hot private keys on the public internet. Testnet faucets must be managed manually by designated community operators.

### Operator Workflow:
1. **Faucet Wallet Initialization:**
   The operator generates an isolated faucet keystore on a secured machine:
   ```bash
   ./armwallet -network testnet -create -wallet faucet_keystore.json
   ```
2. **Funding the Faucet:**
   The operator mines blocks or receives testnet coins from genesis distribution.
3. **Fulfilling Community Requests:**
   Community members request testnet coins via the official forum or Discord by submitting their Bech32 address (`armx1...`).
4. **Disbursing Coins:**
   The operator executes manual single transactions:
   ```bash
   export ARMX_WALLET_PASSPHRASE="operator_secret_passphrase"
   ./armsend -network testnet \
     -wallet faucet_keystore.json \
     -to armx1<requester_address> \
     -amount-armoids 100000000 \
     -fee-armoids 11000
   ```
  *(Sends 1.0 ARMX [100,000,000 Armoids] with a fee of 11,000 Armoids).*
