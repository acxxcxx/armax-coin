# Armaxcoin Consensus, Cryptography & Network Specification

This document provides the technical specification of the Armaxcoin consensus engine, cryptographic primitives, difficulty retargeting algorithm, reorganization rules, and peer-to-peer wire protocol.

---

## 1. Cryptographic Primitives

### 1.1 Post-Quantum Digital Signatures (NIST FIPS 204 ML-DSA-65)
Armaxcoin is built from the ground up for the post-quantum era. All user transactions, coinbase recipient addresses, and transaction verification logic strictly enforce the **ML-DSA-65** lattice-based signature scheme (formerly known as Dilithium3) as standardized by NIST in FIPS 204:

- **Key Parameters:**
  - Public Key Size: `1,952 bytes`
  - Private Seed / Key Size: `32 bytes` (seed) / `4,032 bytes` (expanded private key)
  - Signature Size: `3,309 bytes`
  - Security Category: Level 3 (equivalent to AES-192 against quantum adversaries)
- **Standard Library Implementation:**
  Implemented using the official Go standard library package `crypto/mldsa` introduced in Go 1.27+.
- **Address Encoding:**
  Public keys are hashed and formatted as human-readable Bech32 addresses with the human-readable prefix:
  - Testnet: `armx1...`
  - Mainnet: `armx1...`
  - Network profile separation prevents cross-network transaction replay.

### 1.2 Hashing Functions
- **Block Hashing (Testnet, Blocks 0..519):** Double-SHA256 (`SHA-256d`).
- **Block Hashing (Testnet, Blocks 520..2499):** **ArmaxHash v1** (Argon2id 2048 KiB, time cost 2, 1 lane + BLAKE2b-512 + SHA-256).
- **Block Hashing (Testnet, Blocks 2500+):** **ArmaxHash v2 Stream-Fast** (chained BLAKE2b-512 + BLAKE2b-256 with domain separation).
- **Block Hashing (Mainnet Profile - Locked):** Double-SHA256 (`SHA-256d`).
- **Transaction ID & Merkle Trees:** SHA-256 hashing.
- **Address Derivation:** SHA-256(SigAlg || PublicKey) followed by BIP-173 Bech32 encoding.

---

## 2. Canonical Proof-of-Work Specification

Armaxcoin Proof-of-Work (PoW) consensus logic is implemented in `core/blockchain.go`, `core/target.go`, `core/network.go`, and the `armaxhash/` package. The algorithm in effect is strictly determined by the active network profile and current block height.

### 2.1 Public Testnet Profile (`ARMX-TESTNET-1`)

On the public testnet, PoW consensus evolves across three distinct height eras to preserve 100% backward compatibility:

#### Era 1: Legacy Double-SHA256 (Blocks 0 .. 519)
- **Algorithm:** Standard Bitcoin-style double-SHA256 (`SHA-256d`).
- **Parameters:** Two iterations of SHA-256 over serialized block header.
- **Status:** Historical bootstrap era. All historical blocks remain permanently valid.

#### Era 2: ArmaxHash v1 (Blocks 520 .. 2,499)
- **Algorithm:** Memory-oriented hybrid using Argon2id (`armaxhash.VersionV1 = 1`).
- **Code Parameters (`armaxhash/armaxhash.go`):**
  - Memory cost: `MemoryKiB = 2048` (2 MiB / 2,048 KiB)
  - Time cost: `TimeCost = 2` iterations
  - Parallelism: `Threads = 1` lane
  - Salt: First 16 bytes of block header (`SaltSize = 16`)
  - Key length: 32 bytes (`OutputSize = 32`)
- **Formula:**
  $$\text{D}_1 = \text{Argon2id}(H, \text{salt}=H[:16], \text{time}=2, \text{memory}=2048\text{ KiB}, \text{threads}=1)$$
  $$\text{ArmaxHash}_{v1}(H) = \text{SHA-256}\Big(\text{BLAKE2b-512}(\text{D}_1)\Big)$$
- **Hardware Profile & Limitations:** 2 MiB memory cost provides CPU cache affinity (fitting within L3 cache), but 2 MiB is modest by modern memory-hardness standards. Resistance to specialized hardware has not been evaluated; it serves as an architectural deterrent rather than permanent ASIC immunity.

#### Era 3: ArmaxHash v2 Stream-Fast (Blocks 2,500+)
- **Algorithm:** Dual-stage chained BLAKE2b (`armaxhash.VersionV2 = 2`), activated at block #2500 via scheduled hard fork (`core.TestnetParams.ArmaxHashV2ActivationHeight = 2500`).
- **Code Specification (`armaxhash/armaxhash.go:87-118`):**
  - **Stage 1 (BLAKE2b-512):**
    $$\text{D}_1 = \text{BLAKE2b-512}\Big(\text{"ARMX-POW-V2\x00"} \parallel \text{byte}(2) \parallel \text{byte}(\text{len}(\text{NetID})) \parallel \text{NetID} \parallel H\Big)$$
  - **Stage 2 (BLAKE2b-256):**
    $$\text{ArmaxHash}_{v2}(H) = \text{BLAKE2b-256}\Big(\text{D}_1\Big)$$
- **Technical Properties:**
  - **Zero Heap Allocations:** Mining loops execute with $0\text{ B/op}$ heap allocation, eliminating garbage collector pauses.
  - **Microsecond Verification:** Blocks verify in microseconds on commodity processors.
  - **Memory Hardness:** **ArmaxHash v2 is NOT memory-hard.** It is computationally intensive and designed for commodity CPU execution, but does not saturate DRAM. Resistance to specialized hardware (ASIC/FPGA) or high-end GPU acceleration has not been evaluated.

### 2.2 Mainnet Profile (`ARMX-MAINNET-1` - Unreleased / Locked)

In `core/network.go`:
- `MainnetParams.ArmaxHashActivationHeight = -1`
- `MainnetParams.ArmaxHashV2ActivationHeight = -1`

Unless changed by a future consensus hard-fork proposal, the mainnet profile in code specifies standard **Double-SHA256 (`SHA-256d`)** for all blocks. 
- *Hardware Profile:* SHA-256d can be implemented in specialized hardware (ASIC/GPU). It is custom-serialized and not compatible with standard Bitcoin mining firmware without modifications, but is **not claimed to be CPU-friendly, memory-hard, or ASIC-resistant**.

### 2.3 Regtest & Devnet Profiles
Both `regtest` and `devnet` profiles have activation heights set to `-1`, operating under standard Double-SHA256 (`SHA-256d`).

---

## 3. Difficulty Adjustment: LWMA-1

Armaxcoin adjusts mining difficulty on every block using the **Linear Weighted Moving Average (LWMA-1)** algorithm over a sliding window of $N=60$ blocks:

### 3.1 Mathematical Specification
Let $N = 60$ and target block time $T = 120$ seconds.

$$\text{Weight}_i = i \quad \text{for } i \in [1, N]$$

$$\text{WeightedSolveTime} = \sum_{i=1}^{N} \Big(\text{SolveTime}_i \times i\Big)$$

$$\text{SolveTime}_i = \text{Clamp}\Big(\text{Timestamp}_i - \text{Timestamp}_{i-1}, -6T, 6T\Big)$$

The target is then adjusted proportionally:

$$\text{NextTarget} = \text{AvgTarget} \times \frac{\text{WeightedSolveTime}}{\text{TargetSolveTime}}$$

Where:
$$\text{TargetSolveTime} = T \times \frac{N \times (N + 1)}{2} = 120 \times \frac{60 \times 61}{2} = 219,600 \text{ seconds}$$

### 3.2 Safety Bounds & Damping
- **Maximum Step Adjustment:** Target cannot change by more than a factor of $2.0$ (up or down) in a single block.
- **Minimum Difficulty Floor:** Target can never exceed `PowLimit` (the minimum difficulty ceiling).

---

## 4. Cumulative Chainwork & Fork Resolution

Armaxcoin follows Nakamoto consensus using **cumulative Proof-of-Work (Chainwork)** rather than simple block count:

$$\text{Work}(B) = \left\lfloor \frac{2^{256}}{\text{Target}(B) + 1} \right\rfloor$$

$$\text{Chainwork} = \sum_{B \in \text{Chain}} \text{Work}(B)$$

### Fork Choice Rule
When two valid competing branches exist:
1. The node selects the branch with the **strictly higher cumulative chainwork**.
2. If chainwork is identical, the node retains the currently accepted active tip (first-seen rule).

---

## 5. Maximum Reorganization Depth (`MaxReorganizationDepth`)

In `core/consensus.go`:
```go
const MaxReorganizationDepth = 720
```

- **Definition:** A node will strictly reject any reorganizing branch that attempts to fork the chain at a depth greater than **720 blocks** behind the current tip.
- **Time Equivalent:** At a 120-second target block time, 720 blocks corresponds to:
  $$720 \times 120\text{ seconds} = 86,400\text{ seconds} = 24\text{ hours}$$
- **Security Rationale:** Protects nodes from historical 51% reorganization attacks attempting to rewrite deeply settled transactions or replay past chain history after a day of network finality.

---

## 6. Audit & Technical Evaluation of the 720-Block Proposal

### 6.1 The Maintainer Proposal
The project maintainers are considering internally or privately mining 720 blocks prior to or during the transition to mainnet.

### 6.2 Disambiguation of "720"
It is critical to distinguish between two distinct concepts:
1. **`MaxReorganizationDepth = 720`:** An active security limit in code preventing chain reorganizations exceeding 720 blocks in depth.
2. **Proposed Internal Mining of 720 Blocks:** A potential launch tactic under discussion by maintainers.

### 6.3 Technical Implications & Security Analysis

Mining 720 blocks privately **does NOT** guarantee mainnet security, and introduces substantial operational and economic risks:

1. **Chainwork & Fork Vulnerability:**
   - Proof-of-Work networks derive security entirely from verifiable, cumulative chainwork produced under open competition.
   - If 720 blocks are mined on private hardware with low initial difficulty, an external adversary with modest public hashrate could generate an alternative branch with greater cumulative chainwork in a matter of hours, orphaning the entire 720-block history.
2. **Difficulty Misalignment:**
   - If 720 blocks are mined rapidly with dedicated hardware, the LWMA-1 difficulty retargeting will drive the network difficulty up to match the private hashrate.
   - When the network is subsequently opened to the public, if public miners provide less hashrate than the private mining session, block times will stall (block production could slow from 2 minutes to hours per block), crippling network usability.
3. **Economic Transparency & Premine Perception:**
   - Mining 720 blocks generates $720 \times 60 = 43,200 \text{ ARMX}$ in coinbase rewards.
   - If mined internally without public participation, the broader cryptocurrency community will perceive this as an undisclosed premine or unfair advantage, severely damaging community trust and open-source credibility.
4. **Reorganization Boundary Edge Cases:**
   - If independent public nodes start from genesis (height 0) and the private 720-block chain is released to the network, nodes that have already progressed past block 1 will treat this incoming 720-block chain as a deep reorganization.
   - Because `MaxReorganizationDepth` is exactly 720, any timing skew where public nodes have reached height 2 or 3 would cause the incoming chain to be rejected as exceeding the maximum allowed reorganization depth!

### 6.4 Recommended Mainnet Launch Strategy
Instead of private mining, the following transparent launch procedure is strongly recommended:

1. **Pre-Announced Genesis Timestamp:** Announce mainnet launch date and precise Unix timestamp publicly in advance.
2. **Open Public Bootstrap:** Deploy seed nodes with mainnet profile locked until the genesis moment.
3. **Zero Premine / Transparent Block 0:** Generate Block 0 burning the coinbase reward or attributing it to a verifiable genesis commitment.
4. **Public Mining from Height 1:** Allow all community miners to compete openly from block 1 onwards under real-time LWMA-1 difficulty adjustment.
5. **Periodic Public Checkpoints:** Once the public network reaches maturity, hardcode periodic checkpoints in source code (e.g., at height 10,000) through transparent community consensus.

---

## 7. P2P Wire Protocol & Handshake Flow

1. **Transport Layer:** Traffic is encrypted with TLS 1.3 using self-signed ephemeral certificates; peer identity is not verified through a PKI/CA; TOFU fingerprint pinning detects certificate changes after first contact; initial contact remains vulnerable to an active man-in-the-middle unless fingerprints are distributed out-of-band.
2. **Handshake:**
   - Initiator connects and performs TLS handshake.
   - Remote peer certificate public-key fingerprint is checked against `peers.json` (TOFU pinning). If fingerprint changed, connection terminates immediately.
   - Nodes exchange `VERSION` payload containing:
     - Software version (`1.0.0`)
     - Network ID (`ARMX-TESTNET-1`)
     - Current block height & best block hash
     - Cumulative chainwork
   - If network IDs do not match, the connection is instantly closed.
3. **Block & Transaction Synchronization:**
   - Nodes compare best block heights and chainwork.
   - The node with lower chainwork requests block headers (`GETHEADERS`) and blocks (`GETBLOCKS`).
   - Blocks are transferred in streaming batches, validated incrementally, and committed to LevelDB/BoltDB via atomic batch transactions.
