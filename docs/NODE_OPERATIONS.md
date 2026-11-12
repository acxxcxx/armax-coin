# Armaxcoin Node Operations Guide

This guide provides operational guidance for deploying, running, hardening, and maintaining Armaxcoin full nodes and seed infrastructure.

---

## 1. Network Topology & Port Bindings

Armaxcoin enforces strict separation between public peer-to-peer transport and local node management:

| Network Profile | P2P Gossip Port (Public) | JSON-RPC Port (Strictly Loopback) | Default Data Directory |
|---|---|---|---|
| **Testnet (`ARMX-TESTNET-1`)** | `19335` (TCP) | `127.0.0.1:19333` | `~/.armax/testnet` |
| **Mainnet (`ARMX-MAINNET-1`)** | `19334` (TCP) | `127.0.0.1:19332` | `~/.armax/mainnet` |
| **Regtest (`ARMX-REGTEST-1`)** | Dynamic / Configurable | Dynamic / Configurable | Custom / In-Memory |

> [!CRITICAL]
> **RPC Isolation Policy:**  
> The JSON-RPC service exposes administrative endpoints (block submission, peer mutation, mining templates). It **MUST NEVER** be bound to `0.0.0.0` or exposed directly to the public internet without an authenticated reverse proxy and TLS. The daemon rejects public binding unless strong HTTP Basic Auth and TLS credentials are explicitly provided.

---

## 2. Hardware & Resource Recommendations

### Standard Full Node
- **CPU:** 2 modern 64-bit cores (x86_64 or ARM64)
- **RAM:** 2 GB minimum (4 GB recommended)
- **Disk:** 20 GB SSD storage
- **Network:** 10 Mbps stable internet connection

### Public Seed / Relay Node
- **CPU:** 4 cores
- **RAM:** 8 GB
- **Disk:** 50 GB NVMe SSD
- **Network:** Static public IPv4, minimum 50 Mbps uplink, 99.9% uptime SLA

---

## 3. Firewall Configuration (UFW / iptables)

For public nodes, permit inbound traffic strictly on the P2P gossip port:

```bash
# Allow SSH access
sudo ufw allow 22/tcp

# Allow Testnet P2P traffic from external peers
sudo ufw allow 19335/tcp comment 'Armax Testnet P2P'

# Explicitly ensure RPC remains inaccessible externally
sudo ufw deny 19333/tcp comment 'Armax RPC Loopback Only'

# Enable firewall
sudo ufw enable
```

---

## 4. Production Systemd Service Deployment

A production-grade systemd unit file is provided in `systemd/armcoind-testnet.service`.

### Installation Steps

1. Create a dedicated system user:
   ```bash
   sudo useradd -r -m -d /var/armax -s /bin/bash armax
   sudo mkdir -p /var/armax/testnet
   sudo chown -R armax:armax /var/armax
   ```

2. Copy the binary:
   ```bash
   sudo cp armcoind /usr/local/bin/armcoind
   sudo chmod 755 /usr/local/bin/armcoind
   ```

3. Install the systemd service file:
   ```bash
   sudo cp systemd/armcoind-testnet.service /etc/systemd/system/armcoind-testnet.service
   ```

4. Enable and start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now armcoind-testnet
   ```

5. Monitor service logs:
   ```bash
   sudo journalctl -u armcoind-testnet -f -o cat
   ```

---

## 5. Peer-to-Peer Security, Storage Isolation & TOFU Pinning

Armaxcoin implements transport encryption for node-to-node communication:

- **P2P Transport Security:** The default daemon configuration requires TLS 1.3 and enables TOFU, using a persistent, self-signed ECDSA P-256 node identity stored in `p2ptls.pem`. The certificate is not independently verified through a PKI/CA; first contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed through a trusted out-of-band channel. This is not CA-authenticated mutual TLS.
- **Inbound identity limitation:** The current inbound TOFU set is scoped to the network, not bound to a stable peer address or protocol-level peer identity. A first-seen inbound fingerprint is accepted and remembered; because the wire protocol carries no stable peer identity, this does not authenticate a named peer. Outbound TOFU pins are associated with the configured endpoint. When TOFU is disabled, TLS provides encryption only; a successful `GET_HEIGHT` handshake can be counted as an active protocol session, but is not an authenticated identity.
- **Network-Isolated Peer Store (`peers.json`):** Known outbound peers and fingerprints are persisted per network in `<dataDir>/<network>/peers.json` (e.g. `~/.armax/testnet/peers.json`).
  - **Non-Destructive Migration:** If upgrading from v1.0.5 where `peers.json` existed directly under `<dataDir>`, the daemon automatically copies it into the network subdirectory with `0600` permissions. The original legacy file is left intact and never deleted, preserving safe downgrade compatibility.
  - **Fail-Closed Corruption Handling:** If a peer store file is corrupt or has an unsupported schema version, it is backed up to `<path>.corrupt` (with timestamped forensic fallback). The daemon marks the store corrupted, disables TOFU auto-pinning to prevent Person-In-The-Middle key adoption, and aborts any `savePeers()` writes to protect disk state.
- **NAT & Outbound-Only Nodes:** Nodes operating behind NAT or configured without an active incoming listening cycle (`activeCycle == nil`) safely register outbound work without WaitGroup panics, allowing them to reliably broadcast locally mined blocks and relay transactions to seed peers.
- **`PUSH_BLOCKS` NAT Branch Delivery (v1.0.7):** Nodes behind NAT or non-dialable firewalls can proactively deliver competitor block branches directly to public peers over existing outbound connections. In v1.0.7, `handlePushedBlocks` includes a multi-chunk staging buffer (`pushBufferSession`, 30s TTL) that accumulates candidate chunks up to `MaxPushBlocks = 1000` before triggering reorganization when cumulative chainwork exceeds the local tip.
- **Negotiated DEFLATE Wire Compression (v1.0.7):** P2P block range queries (`GET_BLOCK_RANGE`) support optional DEFLATE compression (`accept: ["deflate"]` / `enc: "deflate"`). Protected by a 32 MB decompression limit and 10% minimum compression efficiency threshold. Operators can disable compression requests by setting `ARMX_P2P_COMPRESSION=0`.
- **IBD Continuity Verification & Ban Scoring (v1.0.7):** Chunk-based initial block download enforces cryptographic continuity (`PrevHash == Blocks[i-1].Hash`). Receiving non-contiguous segments immediately penalizes the peer with 50 ban points and records a peer failure.
- **Mixed-Version Compatibility & Hard-Fork Boundaries:** While P2P wire framing remains backward-compatible with legacy wire formats, **nodes prior to v1.0.10 will permanently split from the canonical chain at Block 13,293 (TEDR emergency difficulty) and Block 13,335 (Armax-ASERT-AW DAA)**, as well as at Block 20,000 (10-minute future-timestamp rule). All node operators, pool operators, and miners must run v1.0.10 for full consensus compatibility.
- **Persistent ban store (v1.0.7):** bans are saved to `<dataDir>/<network>/bans.json` and survive restarts until they expire.
- **Pinned seed fingerprints (v1.0.7, optional):** add `"seed_fingerprints": {"host:port": "<64-hex SHA-256 SPKI fingerprint>"}` to the config file to reject a seed whose certificate does not match. A malformed entry aborts startup.
- **Inbound handshake timeout (v1.0.7):** an unauthenticated inbound connection must send its first valid message within 5 seconds.
- **Clock (NTP) requirement (v1.0.7+ / v1.0.10):** from testnet block 20,000 a block more than 10 minutes ahead of the validator's clock is rejected, so enable NTP on nodes and miners (`timedatectl set-ntp true` on Linux, `w32tm /resync` on Windows). A mining machine that is ahead of its node is corrected automatically; a node whose clock is behind logs `check NTP`. Under v1.0.10, minor future skew ($\le 30\text{s}$) incurs 0 peer score penalty, while severe skew ($> 600\text{s}$) triggers an immediate ban.

---

## 6. Remote Management via Secure SSH Tunneling

If you must access the node's RPC API from a remote workstation, **do not expose the RPC port**. Instead, establish an encrypted SSH tunnel:

```bash
# Forward remote port 19333 to local port 19333
ssh -N -L 19333:127.0.0.1:19333 user@your-node-ip
```

Once established, local CLI tools can communicate seamlessly with the remote daemon:
```bash
curl -s http://127.0.0.1:19333/stats | jq .
```

---

## 7. Backup, Snapshot & Recovery

All node state is stored within the network data directory (`~/.armax/<network>` on Linux, `%APPDATA%\Armax\<network>` on Windows):
- `blockchain.db`: The append-only transactional blockchain database.
- `peers.json`: Known peer routing table and pinned TOFU certificates.
- `armax.cookie`: Ephemeral local authentication token (automatically rotated on daemon startup, located in `<dataDir>/<network>/armax.cookie`).

### Daemon Shutdown & Fail-Closed BoltDB Safety

Armaxcoin v1.0.6 executes a hardened shutdown sequence via `ShutdownServerSafely()`:
1. Stops accepting incoming HTTP RPC requests.
2. Gracefully signals P2P worker cycles and rate limiters to drain active goroutines.
3. If in-flight handlers drain cleanly within timeout, flushes and persists the blockchain database to disk.
4. **Fail-Closed Protection:** If HTTP handler draining times out, BoltDB database close and flush are explicitly aborted to prevent in-flight handlers from writing to a closed database handle or corrupting state.

### Creating a Safe Database Backup

1. Gracefully stop the daemon:
   ```bash
   sudo systemctl stop armcoind-testnet
   ```
2. Backup the state directory:
   ```bash
   tar -czvf armax_testnet_backup_$(date +%F).tar.gz -C /var/armax/testnet blockchain.db peers.json
   ```
3. Restart the daemon:
   ```bash
   sudo systemctl start armcoind-testnet
   ```

### Recovery from Inconsistent State

If a power failure or disk error causes database corruption:
```bash
sudo systemctl stop armcoind-testnet
rm -rf /var/armax/testnet/blockchain.db /var/armax/testnet/peers.json
sudo systemctl start armcoind-testnet
```
The node will automatically connect to seed nodes and rebuild the chain through initial block download (IBD).

### Optional Store Compression and Downgrade (v1.0.7)

> [!CAUTION]
> **STORAGE COMPATIBILITY & DOWNGRADE WARNING:**
> BoltDB record compression is **disabled by default**. Compressed records created by v1.0.7 are **completely unreadable by v1.0.6**.
> - Operators enabling record compression must understand that rolling back to v1.0.6 without converting records will cause database load failures.
> - **Always create a full database backup before running store conversion.**
> - To safely downgrade a compressed database, stop the daemon completely and run:
>   `./armcoind -network testnet -datadir <datadir> -store-rewrite plain`

BoltDB block, undo and snapshot records can be stored compressed. It is **off by default**; enable it with `"store_compression": true` in the config file, `-store-compression`, or `ARMX_STORE_COMPRESSION=1`. Readers always accept both plain and compressed records, so toggling the setting is safe within v1.0.7. To convert a data directory, stop the node and run:

```bash
# Convert to uncompressed format (v1.0.6-compatible)
armcoind -network testnet -datadir /var/armax -store-rewrite plain

# Convert to compressed format
armcoind -network testnet -datadir /var/armax -store-rewrite compressed
```

Back up the database first (Section 7). Measured on synthetic blocks, a block with one ML-DSA-65 transaction shrinks to about half; coinbase-only blocks are too small to compress.

## 8. Isolated Local Testnet Runs

For local operational checks, use an explicit Testnet network ID, a fresh data directory, loopback-only RPC and P2P binds, and a separate log directory. `-noseeds` disables both configured and default seed peers:

```powershell
$isolatedRoot = Join-Path $env:TEMP 'armx-isolated-testnet'
.\armcoind.exe -network testnet `
  -config (Join-Path $isolatedRoot 'isolated-testnet.json') `
  -datadir (Join-Path $isolatedRoot 'data') `
  -logdir (Join-Path $isolatedRoot 'logs') `
  -rpcbind 127.0.0.1 -rpcport 29433 `
  -p2pbind 127.0.0.1 -p2pport 29435 `
  -noseeds
```

Keep the isolated directory outside existing Mainnet/Testnet storage. Verify it contains no wallet material before startup, inspect the network ID and database location in the running process, and stop the daemon gracefully before restarting or inspecting persisted state. This mode is for local testing only; it does not make Mainnet ready.
