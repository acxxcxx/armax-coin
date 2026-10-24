# Armaxcoin (ARMX) Technical Whitepaper

**Version:** 1.0.3 (Public Testnet Release)  
**Ticker:** `ARMX`  
**Network Profiles:** `ARMX-TESTNET-1` (active default) and `ARMX-MAINNET-1` (locked; not launched)  
**Consensus:** Proof-of-Work (PoW) with LWMA-1 ($N=60$)  
**Genesis Date:** 2026  
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
| **Fork Choice Rule** | Heaviest Chain (Satoshi Chainwork) | Cumulative Proof-of-Work ($\sum \frac{2^{256}}{\text{Target} + 1}$) strictly supersedes chain length |
| **Max Reorganization Depth** | 720 Blocks (~24 hours) | Finality boundary preventing deep history rewrites while accommodating natural network splits |
| **Max Block Transactions** | 1,000 Txs / Block | Hard consensus limit preventing block bloat and memory exhaustion attacks |
| **Block Size Limits** | MaxBlockSize = 2 MB, MaxTxSize = 20 KB | MaxIncomingPacketSize = 4 MB; verified block capacity: up to 185 user transactions (186 total with coinbase; 10,761 B/tx in JSON wire format, see docs/PERF.md) |
| **Block Structure** | Binary Merkle Tree | Canonical 32-byte Merkle root in header enabling SPV light proofs |
| **Hashing Algorithm** | ArmaxHash v2 (Stream-Fast Chained BLAKE2b) for $H \ge 2500$ / ArmaxHash v1 for $520 \le H < 2500$ / SHA-256d for $H < 520$ | High-throughput, 0-heap allocation, multi-core CPU & ARM friendly ($H \ge 2500$); historical blocks ($H < 2500$) preserved with 100% backward compatibility |
| **Digital Signatures** | NIST FIPS 204 (ML-DSA-65) | Quantum-resistant signatures (NIST FIPS 204, ML-DSA-65): Public key 1,952 bytes, Signature 3,309 bytes |
| **Address Encoding** | BIP-173 Bech32 (`armx1...`) | Canonical lowercase prefix `armx` + 32-byte SHA-256(SigAlg \|\| PubKey) payload (63 chars total) |
| **Wallet Recovery** | BIP-39 Deterministic Mnemonic | Standard 24-word mnemonic (12-word accepted with warning), HKDF-SHA256, Argon2id encrypted keystore |
| **Difficulty Algorithm** | LWMA-1 ($N=60$) | Linear Weighted Moving Average across 60-block window with continuous 256-bit target |
| **Target Block Time** | 120 seconds (~2 minutes) | Balanced propagation window ensuring ultra-low orphan rate across heterogeneous nodes |
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
| **P2P Transport Encryption** | TLS 1.3 with TOFU pinning | Strict TLS and TOFU are enabled by default; plaintext fallback is legacy-testnet-only; first-contact MITM remains possible without published seed fingerprints |
| **Storage Engine** | bbolt Key-Value ACID Store (v2) | Full persistence with undo log (1,440 retention), snapshots every 1,000 blocks, and automatic recovery |
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
$$\text{TxHash} = \text{SHA-256d}(\text{NetworkID} \parallel \text{Sender} \parallel \text{Recipient} \parallel \text{Amount} \parallel \text{Fee} \parallel \text{Nonce} \parallel \text{SigAlg})$$

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

This enables **Simplified Payment Verification (SPV)**: light wallets and mobile clients can mathematically prove a transaction exists within a block using an $O(\log n)$ Merkle audit path without downloading entire block payloads.

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

Because recent blocks carry linearly higher weights ($k = 1 \dots N$), the network responds smoothly to hashrate shifts across every block, maintaining a stable ~120-second interval with low variance.

### 4.3 Heaviest Chain Rule (Satoshi Cumulative Chainwork)
Armaxcoin does not rely on naive block count for fork resolution. Instead, each block contributes cumulative work based on Satoshi's work formula:
$$\text{Work}_i = \left\lfloor \frac{2^{256}}{\text{Target}_i + 1} \right\rfloor$$
$$\text{Chainwork} = \sum_{i=0}^{\text{Height}} \text{Work}_i$$

When competing branches appear, nodes select the branch possessing strictly greater $\text{Chainwork}$. An attacker generating many blocks at low difficulty can never overtake an honest chain with higher difficulty.

### 4.4 Reorganization Ceiling (Max Reorg Depth = 720 Blocks)
To establish permanent historical finality while accommodating natural transient network splits:
$$\text{ReorgDepth} = \text{CurrentHeight} - \text{ForkPointHeight}$$

If a candidate branch forks more than **720 blocks (~24 hours)** behind the active chain tip, the candidate chain is rejected immediately by consensus rules, preventing deep 51% history rewrites.

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
2. **Era 2 (Blocks $520 \le H < 2500$):** ArmaxHash v1 memory-hard PoW chaining Argon2id (2048 KiB memory footprint, 1 lane, 1 iteration) with BLAKE2b and SHA-256.
3. **Era 3 (Blocks $H \ge 2500$):** **ArmaxHash v2 Stream-Fast** PoW. Activated via scheduled hard fork at block #2500, ArmaxHash v2 eliminates GC heap allocation churn and RAM bus saturation by utilizing a dual-stage chained BLAKE2b cryptographic construction with network-level domain separation:
   $$\text{Prehash} = \text{Blake2b-512}\Big(\text{"ARMX-BLAKE2B-V2:"} \parallel \text{NetworkID} \parallel \text{":"} \parallel \text{Header}\Big)$$
   $$\text{ArmaxHash}_{v2} = \text{Blake2b-256}\Big(\text{Prehash} \parallel \text{Header}\Big)$$

**Key Technical Properties:**
- **0 Heap Allocations:** Executed within fixed stack buffers, zero GC pauses.
- **Microsecond Relay Validation:** Instantaneous validation across node networks.
- **ARM & Commodity CPU Friendly:** Runs smoothly on laptops, multi-core CPUs, and low-power ARM devices without thermal throttling or DRAM bus exhaustion.
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
- Attackers cannot isolate a victim node from the honest network (Eclipse Attack).

### 6.2 Packet Size & Socket Deadlines
- Incoming P2P TCP streams are bounded to 2 MB using `io.LimitReader` to prevent Out-Of-Memory (OOM) exhaustion.
- Sockets enforce 20-second read/write deadlines, mitigating Slowloris resource exhaustion.

### 6.3 RPC Authentication & Origin Hardening
- Administrative and state-mutating RPC endpoints require authentication or local non-CORS context.
- The `/mine` endpoint strictly requires HTTP POST with hostname origin validation.
- Per-IP rate limiters prevent API exhaustion.

### 6.4 Encrypted P2P Transport Layer
Armaxcoin provides decentralized wire encryption:
- Nodes generate and persist an ECDSA P-256 self-signed X.509 identity when no identity exists. A present but invalid identity fails closed and must be restored explicitly.
- Outbound and inbound connections negotiate **TLS 1.3**. Strict TLS and TOFU certificate pinning are enabled by default.
- Plaintext fallback is not part of the default operating mode and is reserved for isolated legacy test environments.
- TOFU protects subsequent contacts after a pin is stored; trusted seed fingerprints still need authenticated out-of-band distribution to address the first-contact MITM risk.

### 6.5 Decentralized Peer Discovery
- Multi-tier peer discovery: primary DNS seeds (`testnet-seed1.armax-coin.cloud:19335`, `testnet-seed2.armax-coin.cloud:19335`, `testnet-seed3.armax-coin.cloud:19335`) and P2P address book gossip.

### 6.6 P2P Flood Defense & Seen-Filter Hash Dedup
- Nodes maintain time-bounded LRU seen-filters for transactions (`isTxSeenOrMark`) and blocks (`isBlockSeenOrMark`).
- Redundant gossiped packets are discarded immediately upon arrival before invoking deserialization or cryptographic verification.

---

## 7. Layer-2 Scalability: Hashed Time-Locked Contracts (HTLC)

> [!NOTE]
> **Status:** Experimental Library Status. The HTLC primitives are implemented in the `core/` library for forward compatibility and layer-2 research; consensus activation is planned for a future protocol hard fork.

$$\text{HTLC} = (\text{HashLock}, \text{TimeLock}, \text{Preimage})$$

1. **Payment Channel Execution (Off-Chain Microtransactions):** The recipient claims funds by revealing secret preimage $S$ such that $\text{SHA-256}(S) == \text{HashLock}$.
2. **Refund Guarantee (Safety Fallback):** If the counterparty does not claim the payment before the expiration boundary, the sender safely reclaims the locked coins.

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
- CPU mining via ArmaxHash v2 Stream-Fast (chained BLAKE2b with zero heap allocation) active from block #2500, engineered for energy-efficient multi-core commodity CPUs, mobile devices, and ARM edge computers. Historical pre-fork blocks (0..519 SHA-256d and 520..2499 ArmaxHash v1 Argon2id) remain fully verified on-chain.

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
2. **Unauthenticated TLS P2P Transport:** Wire encryption utilizes self-signed ephemeral X.509 certificates (BIP-324 style decentralized transport). While this shields peer communications from ISP surveillance and passive Deep Packet Inspection (DPI), the transport does not authenticate peer identity against active man-in-the-middle attackers.
3. **Code Audit Status:** The codebase and cryptographic implementations have not yet undergone a formal third-party security audit.
4. **Reorganization Finality (720 Blocks):** The 720-block (~24-hour) reorganization depth prevents catastrophic history rewrites by majority hashrate attackers. If an extended network partition lasts longer than 720 blocks, manual node operator coordination is required.
5. **RPC Daemon Isolation:** The RPC daemon (`armcoind`) binds strictly to loopback (`127.0.0.1`). Exposing the RPC port to public interfaces without strong authentication introduces severe security risks.
6. **Client-Side Key Custody & Web Wallet:** Node servers do not custody private keys. Signing occurs client-side. Web browser wallets operate in read-only mode pending cross-platform ML-DSA-65 deterministic validation (Web Explorer/server-web: Planned / Removed).
7. **ASIC Minable:** Block headers utilize double-SHA256 (SHA-256d). SHA-256d can be implemented in specialized hardware (GPU/ASIC). The Armaxcoin block header format is custom, so existing Bitcoin miners cannot mine it without specialized software. It is not claimed to be CPU-friendly or ASIC-resistant.
8. **Initial Hashrate & 51% Risk:** Early testnet and launch states operate with low aggregate hashrate, rendering the network vulnerable to 51% hash power reorganization attacks.
9. **ARM Verification Overhead:** NIST FIPS 204 ML-DSA-65 signatures require modular polynomial arithmetic, introducing measurable CPU overhead and verification latency on constrained ARM single-board computers (Raspberry Pi, Orange Pi).
10. **Minimum Go Version:** Compiling Armaxcoin requires Go 1.27+ due to reliance on the Go standard library `crypto/mldsa` package and toolchain alignment (`go.mod`).

---

## 12. Open-Source License
Armaxcoin is released under the permissive [MIT License](LICENSE).
