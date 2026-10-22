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

## 5. Peer-to-Peer Security & TOFU Pinning

Armaxcoin implements zero-trust transport security for node-to-node communication:

- **Mandatory TLS 1.3:** All P2P traffic is encrypted using TLS 1.3 cipher suites. Plaintext TCP connections are strictly rejected.
- **Trust On First Use (TOFU):** On first contact, the node pins the remote peer's public key certificate fingerprint in `peers.json`. If a peer later presents a changed certificate fingerprint, the connection is instantly severed to prevent in-path Man-in-the-Middle (MITM) attacks.
- **Persistent Peer Store:** Known and validated peers are saved atomically to `peers.json` with versioning and checksum verification.

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

All node state is stored within the data directory:
- `blockchain.db`: The append-only transactional blockchain database.
- `peers.json`: Known peer routing table and pinned TOFU certificates.
- `armax.cookie`: Ephemeral local authentication token (automatically rotated on daemon startup).

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
