# Armaxcoin (ARMX) Technical Whitepaper

**Version:** 1.0.3 (Public Testnet Release)  
**Ticker:** `ARMX`  
**Network Profiles:** `ARMX-TESTNET-1` (active default) and `ARMX-MAINNET-1` (locked; not launched)  
**Consensus:** Proof-of-Work (PoW) with LWMA-1 ($N=60$)  
**Genesis Date:** 2024-03-09 (genesis timestamp date)
**Seed Nodes:** Operated by core developers; community operators sought  
**DNS Seed Peers (Testnet):** `testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335`  

---

## 1. Abstract
Armaxcoin (ARMX) is an open-source Proof-of-Work Layer-1 protocol under public-testnet development. It is experimental software, not a production mainnet system or an investment product.

ARMX combines a 126,000,000 supply cap, a roughly four-year halving schedule, and cumulative chainwork with BIP-173 Bech32 addresses, 24-word BIP-39 recovery phrases (12-word import is legacy-only), LWMA-1 difficulty adjustment across a 60-block window, Merkle proofs, and bbolt-backed state persistence. Claims about production security remain subject to the mainnet readiness gates and independent audit.

---

## 2. Core Specifications & Tokenomics

| Parameter | Specification | Details & Consensus Rules |
|---|---|---|
| **Consensus Mechanism** | Proof-of-Work (PoW) | Dynamic per-block numerical target adjustment via LWMA-1 ($N=60$) |
| **Fork Choice Rule** | Heaviest Chain (Cumulative Chainwork) | Among eligible branches, the node prefers greater cumulative work, subject to its reorganization-depth limit |
| **Max Reorganization Depth** | 720 Blocks (~24 hours at target cadence) | Local reorganization-depth policy limit; not protocol finality or a guarantee that transactions cannot be reorganized |
| **Max Block Transactions** | 1,000 Txs / Block | Consensus transaction-count limit; block-size and packet-size limits also apply |
| **Block Size Limits** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB | MaxIncomingPacketSize = 4 MB; verified block capacity: up to 185 user transactions (186 total with coinbase; 10,761 B/tx in JSON wire format, see docs/PERF.md) |
| **Block Structure** | Binary Merkle Tree | Canonical 32-byte Merkle root in header enabling SPV light proofs |
| **Hashing Algorithm** | ArmaxHash v2 (Stream-Fast Chained BLAKE2b) for $H \ge 2500$ / ArmaxHash v1 for $520 \le H < 2500$ / SHA-256d for $H < 520$ | The v2 hash-function benchmark reports 0 allocations/op for a fixed header; this does not establish zero allocations for the full block/miner path. See docs/CONSENSUS_AND_NETWORK.md |
| **Digital Signatures** | NIST FIPS 204 (ML-DSA-65) | Quantum-resistant signatures (NIST FIPS 204, ML-DSA-65): Public key 1,952 bytes, Signature 3,309 bytes |
| **Address Encoding** | BIP-173 Bech32 (`armx1...`) | Canonical lowercase prefix `armx` + 32-byte SHA-256(SigAlg \|\| PubKey) payload (63 chars total) |
| **Wallet Recovery** | BIP-39 Deterministic Mnemonic | Standard 24-word mnemonic (12-word accepted with warning), HKDF-SHA256, Argon2id encrypted keystore |
| **Difficulty Algorithm** | LWMA-1 ($N=60$) | Linear Weighted Moving Average across 60-block window with continuous 256-bit target |
| **Target Block Time** | 120 seconds (~2 minutes) | Difficulty-adjustment target; observed block intervals vary with network hashrate and randomness |
| **Initial Block Reward** | 60 ARMX (6,000,000,000 Armoids) | Awarded to miner via single coinbase transaction at index 0 |
| **Halving Interval** | Every 1,050,000 blocks (~4 years) | Reward cuts in half ($60 \to 30 \to 15 \to \dots$ ARMX) |
| **Maximum Total Supply** | 126,000,000 ARMX | Strict hard cap (12,600,000,000,000,000 Armoids, 8 decimal places; real theoretical emission: 125,999,999.874 ARMX) |
| **Supply Accounting** | $O(1)$ Incremental State Cache | Constant-time circulating supply tracking without block history traversal |
| **Genesis Allocation** | 60 ARMX (Burned) | Hardcoded deterministic Genesis block burned to zero address (`armx1qqqq...plwcdd`) without premine |
| **Block Template Interface** | `/getblocktemplate` JSON RPC | Bounded candidate block template endpoint for external miners |
| **RPC Security** | Loopback by default; authenticated TLS for public RPC | A non-loopback bind requires explicit opt-in, Basic Auth, and configured TLS certificate/key files; `/chain` is bounded to 50 blocks |
| **P2P Synchronization** | Chunked Range & Chainwork Sync | Range-based block streaming (`GET_BLOCK_RANGE`) with cumulative chainwork reorg |
| **P2P Address Book** | 1,000 Addrs (`tried` & `new`) | Separate address book from active connections (MaxPeers=50), dead peer eviction after 3 failures |
| **P2P Eclipse Defense** | Subnet Connection Limiter | Maximum 3 concurrent connections per IPv4 `/24` (or IPv6 `/48`) subnet (`disable_subnet_limit` available) |
| **P2P Flood Defense** | Seen-Filter Hash Cache & Orphan Pools | LRU deduplication cache; bounded 100 orphan blocks (max 10/peer) & 500 orphan txs pool |
| **P2P Transport Encryption** | TLS 1.3 with TOFU pinning | Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity; TOFU pins the remote certificate public-key fingerprint after first contact. The initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band |
| **Storage Engine** | bbolt Key-Value ACID Store (schema v3) | Persistence with undo log (1,440-block retention), snapshots every 1,000 blocks, and recovery support |
| **Network Profiles** | Mainnet & Testnet Profiles | Mainnet (`ARMX-MAINNET-1`: P2P 19334, RPC 19332) \| Testnet (`ARMX-TESTNET-1`: P2P 19335, RPC 19333) |

---

## 3. Cryptographic Primitives & Identity

### 3.1 Keypair Generation (NIST FIPS 204, ML-DSA-65)
Armaxcoin employs **quantum-resistant signatures (NIST FIPS 204, ML-DSA-65)** based on module-lattice cryptography (Module-Lattice-Based Digital Signature Standard):
- **Seed Derivation:** 32-byte deterministic seed derived via HKDF-SHA256:
  $$\text{Seed}_{32} = \text{HKDF-SHA256}(\text{IKM}=\text{BIP-39 Seed}_{64\text{B}}, \text{Salt}=\text{"armax-pqc-v1"}, \text{Info}=\text{"ml-dsa-65/account/0"})$$
- **Public Key Size:** 1,952 bytes (hex-encoded: 3,904 hex characters).
- **Signature Size:** 3,309 bytes (hex-encoded: 6,618 hex characters).
- **Signature Algorithm ID (`SigAlg`):** `0x01` (ML-DSA-65).

### 3.2 Address Derivation & BIP-173 Bech32 Human-Readable Identity
To protect against quantum preimage attacks and avoid typos, Armaxcoin employs the **BIP-173 Bech32** encoding standard with full 32-byte payload length:
1. Compute the 32-byte public key hash with signature algorithm prefix:
   $$\text{Payload} = \text{SHA-256}(\text{SigAlg}_{0x01} \parallel \text{PublicKey}_{1952\text{B}})$$
2. Convert the 8-bit payload (32 bytes) to 5-bit base32 groups.
3. Compute the 6-character BCH polynomial error-detecting checksum with Human-Readable Part (HRP) `"armx"`:
   $$\text{Checksum} = \text{BCHPolymod}(\text{HRP}=\text{"armx"}, \text{Data})$$
4. Concatenate HRP, separator `'1'`, and data/checksum characters:
   $$\text{Address} = \text{"armx1"} \parallel \text{Base32}(\text{Payload}) \parallel \text{Checksum}$$

Every Armaxcoin address begins with **`armx1`** and is exactly 63 characters long in lowercase Bech32 (e.g. `armx17nqa4uyewe8elsn4w8qfygjv7aafkkzj25r8antzdmecwnhpd28sayl2yl`). The Genesis miner burn address consists of 32 zero bytes (`armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd`).

### 3.3 BIP-39 Deterministic Mnemonic Recovery & Keystore
Armaxcoin defaults to 256 bits of cryptographic entropy producing a **24-word BIP-39 mnemonic phrase** (12-word phrases are supported with warning). 

Keystores only store the 32-byte seed encrypted via **Argon2id** key derivation and **AES-256-GCM** (or XChaCha20-Poly1305) authenticated encryption with strict file permissions (`0600`). Passphrases are prompted securely from the terminal or environment variables and never logged or accepted via CLI command-line arguments.

### 3.4 Transaction Verification & Anti-Replay Protection
For a standard transaction, the implementation hashes the UTF-8 encoding of the following colon-delimited record with SHA-256d:
$$\text{NetworkID} : \text{SigAlg} : \text{lowercase(Sender)} : \text{SenderPubHex} : \text{lowercase(Recipient)} : \text{Amount} : \text{Fee} : \text{Nonce}$$
The transaction hash commits to the network ID and sender public key as well as the amounts, fee, and nonce. HTLC transaction fields are appended for the corresponding transaction types.

Transactions are signed using ML-DSA-65 with NIST FIPS 204 domain separation context string:
$$\sigma = \text{Sign}(\text{PrivKey}, \text{Context}=\text{"ARMX-TX-V1"}, \text{TxHash})$$

Consensus verification strictly validates:
1. `SigAlg == 0x01` (ML-DSA-65).
2. `len(SenderPub) == 1952` bytes and `len(Signature) == 3309` bytes.
3. `Sender` equals canonical Bech32 address computed from `(SigAlg, SenderPub)`.
4. Deterministic signature verification: `pqsig.Verify(SenderPub, "ARMX-TX-V1", TxHash, Signature) == true`.
5. Nonce sequence and state balance constraints.

---

## 4. Consensus, Proof-of-Work & Block Structure

### 4.1 Binary Merkle Tree Root & SPV Support
Transactions within a block are arranged in a deterministic binary Merkle tree. If a block contains an odd number of transactions at any level, the trailing hash is duplicated (Satoshi standard).

The 32-byte Merkle root is recorded directly in the block header:
$$\text{MerkleRoot} = \text{TreeRoot}(\text{Tx}_0, \text{Tx}_1, \dots, \text{Tx}_n)$$

This Merkle structure can support **Simplified Payment Verification (SPV)**: a client can verify transaction inclusion with an $O(\log n)$ Merkle path and a trusted block header. This describes a protocol capability; it does not claim that an SPV client is included in this repository.

### 4.2 LWMA-60 Difficulty Adjustment Algorithm
Traditional PoW cryptocurrencies that adjust difficulty over multi-week windows (such as Bitcoin's 2,016-block cycle) suffer from vulnerability to "flash-mining" and "hashrate hopping", where large external mining operations briefly inflate difficulty and abandon the network, stranding honest miners with frozen block intervals.

Armaxcoin solves this through **LWMA-60 (Linear Weighted Moving Average across 60 blocks)**:
- **Evaluation Window:** $N = 60$ blocks.
- **Target Block Time:** $T_{\text{target}} = 120\text{ seconds}$ (~2 minutes).
- **Solve Time Clamping:** $\text{SolveTime}_i = \max(-6T, \min(6T, \text{Timestamp}_i - \text{Timestamp}_{i-1}))$ to neutralize timewarp attacks.
- **Weighted Average:**
  $$L = \sum_{k=1}^{N} k \times \text{SolveTime}_k$$
  $$E = \frac{N(N+1)}{2} \times T_{\text{target}}$$
  $$\text{NextTarget} = \frac{\sum_{k=1}^N \text{Target}_k}{N} \times \frac{L}{E}$$

Because recent blocks carry linearly higher weights ($k = 1 \dots N$), the target responds to recent solve times on each retarget. The 120-second value is a target interval, not a guarantee of stable timing or low variance.

### 4.3 Heaviest Chain Rule (Satoshi Cumulative Chainwork)
Armaxcoin does not rely on naive block count for fork resolution. Instead, each block contributes cumulative work based on Satoshi's work formula:
$$\text{Work}_i = \left\lfloor \frac{2^{256}}{\text{Target}_i + 1} \right\rfloor$$
$$\text{Chainwork} = \sum_{i=0}^{\text{Height}} \text{Work}_i$$

Among branches eligible under the local reorganization-depth policy, nodes prefer the branch with greater cumulative $\text{Chainwork}$. A branch with many low-difficulty blocks can still win if its accumulated work exceeds the competitor's; block count alone does not determine the result.

### 4.4 Reorganization Ceiling (Max Reorg Depth = 720 Blocks)
To bound local chain reorganizations while accommodating transient network splits:
$$\text{ReorgDepth} = \text{CurrentHeight} - \text{ForkPointHeight}$$

If a candidate branch forks more than **720 blocks (~24 hours at target cadence)** behind the active tip, this node rejects it. This local policy limit is not cryptographic or economic finality; reorganizations within the limit remain possible.

### 4.5 Consensus-Enforced Coinbase Maturity Rule
To prevent double-spending and loss of funds from orphaned mining rewards during short-term chain reorganizations, Armaxcoin enforces **Coinbase Maturity** as a strict Layer-1 consensus rule in `state.go`:
$$\text{Confirmations}(\text{Block}_i) = \text{TipHeight} - i + 1$$
$$\text{Spendable}(\text{CoinbaseTx}) \iff \text{Confirmations} \ge \text{CoinbaseMaturity}$$

- **Maturity by Network Profile:**
  - **Mainnet:** 100 blocks (`CoinbaseMaturity = 100`)
  - **Public Testnet:** 10 blocks (`CoinbaseMaturity = 10`)
  - **Regtest:** 1 block (`CoinbaseMaturity = 1`)
- **State Queue & Schema v3:**
  The `AccountState` ledger maintains an on-chain queue of `(amount, height)` entries for immature rewards. An account's spendable balance is strictly defined as $\text{Spendable} = \text{Balance} - \sum \text{Immature}$.
- **Chain Reorganization & Recovery:** When a chain reorganization occurs, the state is rolled back and replayed from the common ancestor or snapshot, deterministically restoring the immature queues. Storage schema is upgraded to version 3.

### 4.6 Proof-of-Work Consensus Evolution: ArmaxHash v2
To accommodate consumer hardware, commodity desktop CPUs, and cache-constrained ARM single-board computers, Armaxcoin implements a backward-compatible three-phase Proof-of-Work timeline:
1. **Era 1 (Blocks $0 \le H < 520$):** Legacy double-SHA256 (`SHA-256d`). All bootstrap blocks remain permanently valid.
2. **Era 2 (Blocks $520 \le H < 2500$):** ArmaxHash v1 PoW using a network- and header-derived BLAKE2b-512 prehash, Argon2id (2048 KiB, 1 lane, 2 iterations), and BLAKE2b-256 finalization; see `docs/CONSENSUS_AND_NETWORK.md`.
3. **Era 3 (Blocks $H \ge 2500$):** **ArmaxHash v2 Stream-Fast** PoW. Active from block #2500 on testnet, v2 uses a dual-stage chained BLAKE2b construction with network-level domain separation:
  $$D_1 = \text{BLAKE2b-512}(\text{"ARMX-POW-V2\\0"} \parallel 0x02 \parallel \text{byte}(\text{len(NetID)}) \parallel \text{NetID} \parallel H)$$
  $$\text{ArmaxHash}_{v2}(H) = \text{BLAKE2b-256}(D_1)$$

**Key Technical Properties:**
- **Hash Primitive Allocation:** The existing benchmark reports 0 allocations/op for `HashV2` with a fixed header. Header construction and the full mining loop are outside that benchmark and may allocate.
- **Validation Performance:** The hash-function benchmark does not measure full block validation, which also includes transaction and state checks.
- **Hardware Profile:** v2 does not use an Argon2 memory-hard stage. Mining speed and thermal behavior depend on the hardware and workload; ASIC/FPGA/GPU resistance has not been evaluated.
- **Zero Historical Disruption:** Node validation dynamically selects the active hashing algorithm based on block height, ensuring 100% historical chain integrity from genesis onward.

---

## 5. Mining Ecosystem & Infrastructure

### 5.1 Bounded `/getblocktemplate` JSON RPC Interface
Armaxcoin exposes an optimized, bounded `/getblocktemplate` RPC endpoint conforming to Bitcoin Core specifications:
- Provides `previousblockhash`, `height`, `curtime`, 256-bit `target`, `difficulty`, `coinbasevalue`, bounded candidate `transactions`, and `merkleroot`.
- Transaction count is strictly bounded to `MaxBlockTransactions - 1` with aggregate byte size limits, preventing oversize block creation.
- Solved blocks are submitted via `/block/submit` or the standard `/submitblock` alias, validated atomically against consensus rules, and immediately gossiped across the global P2P mesh.

### 5.2 Mining Diagnostics & Telemetry (`getmininginfo`, `getnetworkhashps`)
Node operators and external mining software can inspect live consensus metrics:
- **`getmininginfo`**: Reports block height, active difficulty, target, candidate transactions, mempool size, and estimated network hashrate.
- **`getnetworkhashps`**: Calculates instantaneous network hash power estimation:
  $$\text{NetworkHashPS} = \frac{2^{256}}{\text{Target} \times 120\text{s}}$$

### 5.3 Dedicated Multi-Threaded CPU Miner Client (`armminer`)
Armaxcoin provides an autonomous, multi-threaded CPU miner client (`armminer`):
- Connects to one or more failover daemon nodes via `/getblocktemplate`.
- Spawns $N$ concurrent worker threads (configured via `-threads <N>`, defaulting to all CPU cores).
- Explores non-overlapping 32-bit nonce partitions with live console hashrate telemetry (hash/s, kH/s, MH/s).
- Listens for new network tips to automatically abandon stale work and avoid wasted hashing power.
- Delivers an integrated hardware benchmark suite via `-benchmark`.

---

## 6. Network Security & Defense-in-Depth

### 6.1 Subnet-Based Sybil & Eclipse Defense
To prevent an attacker from saturating all node connection slots through rented cloud virtual machines sharing common IP ranges, Armaxcoin enforces **Subnet Connection Limiting**:
- Incoming TCP handshakes and outbound peer registrations are grouped by IPv4 `/24` CIDR (or IPv6 `/48` prefix).
- A maximum of **3 peers per subnet** are permitted concurrently.
- The subnet limiter raises the cost of some connection-concentration strategies but does not prevent eclipse attacks.

### 6.2 Packet Size & Socket Deadlines
- Ordinary inbound newline-delimited P2P messages are limited to 4 MiB. The block-range synchronization reader has a separate 5 MiB limit; the consensus block-size limit is 2,000,000 bytes.
- Sockets enforce 20-second read/write deadlines, mitigating Slowloris resource exhaustion.

### 6.3 RPC Authentication & Origin Hardening
- Administrative and state-mutating RPC endpoints require authentication or local non-CORS context.
- The `/mine` endpoint strictly requires HTTP POST with hostname origin validation.
- Per-IP rate limiters prevent API exhaustion.

### 6.4 Encrypted P2P Transport Layer
Armaxcoin provides decentralized wire encryption:
- Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity stored in `p2ptls.pem`. TOFU pins the remote certificate public-key fingerprint after first contact; identity is not verified through a PKI/CA, so the initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band.
- Nodes generate and persist an ECDSA P-256 self-signed X.509 identity (`p2ptls.pem`) when no identity exists. An existing identity that is unreadable, empty, or invalid fails closed and must be restored explicitly.
- Plaintext fallback is not part of the default operating mode and is reserved for isolated legacy test environments.

### 6.5 Decentralized Peer Discovery
- Multi-tier peer discovery: primary DNS seeds (`testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335`) and P2P address book gossip.

### 6.6 P2P Flood Defense & Seen-Filter Hash Dedup
- Nodes maintain time-bounded LRU seen-filters for transactions (`isTxSeenOrMark`) and blocks (`isBlockSeenOrMark`).
- Seen filters deduplicate repeated block and transaction gossip; they do not replace message parsing, consensus validation, or other peer-diversity defenses.

---

## 7. Layer-2 Scalability: Hashed Time-Locked Contracts (HTLC)

> [!NOTE]
> **Status:** Experimental Library Status. The HTLC primitives are implemented in the `core/` library for forward compatibility and layer-2 research; consensus activation is planned for a future protocol hard fork.

$$\text{HTLC} = (\text{HashLock}, \text{TimeLock}, \text{Preimage})$$

1. **Payment Channel Execution (Off-Chain Microtransactions):** The recipient claims funds by revealing secret preimage $S$ such that $\text{SHA-256}(S) == \text{HashLock}$.
2. **Intended Refund Path:** The experimental HTLC primitive describes a refund after its timelock. HTLC consensus activation is not implemented, so this must not be treated as a currently available or guaranteed recovery mechanism.

---

## 8. Quantum Resistance & Post-Quantum Cryptography (PQC)

> [!NOTE]
> **Status:** Active Layer-1 Consensus. Armaxcoin uses quantum-resistant digital signatures adhering strictly to the NIST FIPS 204 standard (ML-DSA-65).

Conventional cryptocurrencies rely on elliptic curves (ECDSA, Schnorr, Ed25519) that are mathematically vulnerable to Shor's algorithm on large-scale quantum computers. In contrast, Armaxcoin implements post-quantum security across its transaction layer:

- **Signature Scheme:** NIST FIPS 204 ML-DSA-65 (Module-Lattice-Based Digital Signature Algorithm).
- **Cryptographic Sizes:** Public Key: 1,952 bytes; Signature: 3,309 bytes; Entropy Seed: 32 bytes derived via HKDF-SHA256 from standard 24-word BIP-39 recovery mnemonics (`ikm` = 64-byte BIP-39 seed, `salt` = `"armax-pqc-v1"`, `info` = `"ml-dsa-65/account/<index>"`).
- **Address Format:** Standard Bech32 with human-readable prefix `armx` encoding a full 32-byte cryptographic payload generated via $\text{SHA-256}(\text{SigAlg} \parallel \text{PublicKey})$ (`AddressPayloadLen = 32`).
- **Throughput & Capacity:** Max block size of 2,000,000 bytes (2 MB) and max transaction size of 20,000 bytes, supporting up to 185 user transactions per block (186 total including coinbase, ~10.51 KB per wire JSON transaction; empirical throughput of ~1.54 TPS) with dynamic size-based mempool relay fees (documented in `docs/PERF.md`).
- **PoW & Grover's Algorithm:** Proof-of-Work SHA-256 hashing is subject only to quadratic speedup under Grover's algorithm ($\mathcal{O}(\sqrt{N})$), maintaining an effective post-quantum security margin of 128 bits against quantum miners.

---

## 9. Strategic Roadmap: Next-Generation Innovations

To surpass the limitations of first-generation blockchains, the Armaxcoin core team has established the following development roadmap:

```mermaid
graph TD
    A[Armaxcoin Next-Gen Ecosystem] --> B[1. Post-Quantum Cryptography (ML-DSA-65)]
    A --> C[2. Advanced Privacy / CT]
    A --> D[3. Encrypted P2P Transport]
    A --> E[4. Layer-2 Payment Channels]
    A --> F[5. Lightweight Smart Contracts]
    A --> G[6. Small-Hardware PoW Consensus]
    A --> H[7. Democratic On-Chain Governance]
    
    style B fill:#2ecc71,stroke:#27ae60,stroke-width:2px,color:#fff
    style D fill:#2ecc71,stroke:#27ae60,stroke-width:2px,color:#fff
    style E fill:#3498db,stroke:#2980b9,stroke-width:2px,color:#fff
    style C fill:#9b59b6,stroke:#8e44ad,stroke-width:2px,color:#fff
```

### 1. Post-Quantum Cryptography (Implemented in Consensus)
- Active consensus enforcement of NIST FIPS 204 ML-DSA-65 signatures for all user transactions.
- Cryptographic resistance against Shor's algorithm for discrete logarithm and factoring problems.

### 2. Advanced Privacy (Future Research: Confidential Transactions & Ring Signatures)
- Future research into Bulletproofs for Confidential Transactions (CT) to hide transaction amounts on-chain while proving solvency without trusted setups.
- Optional Ring Signatures under study to obscure transaction originators.

### 3. Encrypted P2P Wire Transport (Implemented)
- Transport-layer TLS 1.3 socket encryption preventing ISP traffic classification.
- Decentralized DNS seeds combined with peer address gossip.

### 4. Layer-2 Payment Channels & State Channels (Planned)
- Native HTLC primitive in core transactions.
- Rapid off-chain microtransactions with minimal transaction fees.

### 5. Lightweight Smart Contracts & WebAssembly VM (Future Research)
- Exploratory design for a sandboxed WebAssembly (WASM) runtime executing expressive UTXO-based contracts (similar to ErgoScript) without EVM state bloat.

### 6. Small-Hardware PoW Optimization (LWMA-1 & ArmaxHash v2 Stream-Fast)
- ArmaxHash v2 (chained BLAKE2b) is active from block #2500 on testnet. Its fixed-header hash benchmark reports 0 allocations/op; this does not imply zero allocations across the full node or miner. Historical blocks (0..519 SHA-256d and 520..2499 ArmaxHash v1) remain part of the validation history.

### 7. Democratic On-Chain Governance & Signaling
- Miner and node signaling bits in block headers for seamless, non-contentious soft forks and protocol upgrades.

---

## 10. Ecosystem Architecture

Armaxcoin is compiled into standalone, zero-dependency static binaries:

| Binary | Role | Description |
|---|---|---|
| `armcoind` | Daemon Node | PoW engine, LWMA-60 consensus, TLS P2P mesh, bounded REST API, `/getblocktemplate`. |
| `armminer` | CPU Miner | Continuous multi-threaded CPU mining client with POST requests and auto-failover. |
| `armsend` | CLI Sender | Transaction constructor, ML-DSA-65 signer, and broadcast utility with fee and nonce validation. |
| `armwallet` | CLI Wallet | BIP-39 24-word mnemonic generation, Argon2id encrypted keystore, recovery, and balance viewer. |
| `armgui` | Desktop GUI | Pure Go native desktop GUI wallet and visual blockchain ledger explorer bound to localhost. |

---

## 11. Threat Model & Security Boundaries

A rigorous blockchain design requires transparency regarding operational boundaries and threat assumptions:

1. **Quantum-Resistant Digital Signatures (NIST FIPS 204, ML-DSA-65):** User transactions employ module-lattice signatures resistant to Shor's algorithm. However, this does not eliminate all quantum attack vectors: SHA-256 PoW mining remains subject to quadratic Grover speedups ($\approx 128\text{-bit}$ effective quantum security).
2. **P2P Transport Security:** Traffic is encrypted with TLS 1.3 using a persistent, self-signed ECDSA P-256 node identity stored in `p2ptls.pem`. TOFU pins the remote certificate public-key fingerprint after first contact; identity is not verified through a PKI/CA, so the initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band.
3. **Code Audit Status:** The codebase and cryptographic implementations have not yet undergone a formal third-party security audit.
4. **Reorganization Policy (720 Blocks):** The node rejects reorganizations deeper than 720 blocks. Reorganizations within that limit remain possible, so the limit is not transaction finality. A partition beyond the limit may require coordinated operator recovery.
5. **RPC Daemon Isolation:** The RPC daemon (`armcoind`) binds strictly to loopback (`127.0.0.1`). Exposing the RPC port to public interfaces without strong authentication introduces severe security risks.
6. **Key Management & Signing Architecture:** Key generation, keystore management, and post-quantum signing are performed exclusively by the local client tools (`armwallet`, `armsend`, `armgui`). No private keys are generated, stored, or processed in web browsers; public web interfaces and explorers are read-only and broadcast-only.
7. **Proof-of-Work Hardware Considerations:** Historical testnet blocks 0..519 and the unreleased mainnet profile use Double-SHA256 (SHA-256d), which is not claimed to be CPU-friendly or ASIC-resistant. Active testnet blocks 2500+ use ArmaxHash v2 Stream-Fast (chained BLAKE2b), which is computation-bound and designed for CPU mining; its resistance to specialized hardware (ASIC/FPGA) or high-end GPU acceleration has not been evaluated (see `docs/CONSENSUS_AND_NETWORK.md`).
8. **Initial Hashrate & 51% Risk:** Early testnet and launch states operate with low aggregate hashrate, rendering the network vulnerable to 51% hash power reorganization attacks.
9. **ARM Verification Overhead:** NIST FIPS 204 ML-DSA-65 signatures require modular polynomial arithmetic, introducing measurable CPU overhead and verification latency on constrained ARM single-board computers (Raspberry Pi, Orange Pi).
10. **Minimum Go Version:** Compiling Armaxcoin requires Go 1.27+ due to reliance on the Go standard library `crypto/mldsa` package and toolchain alignment (`go.mod`).

---

## 12. Open-Source License
Armaxcoin is released under the permissive [MIT License](LICENSE).
