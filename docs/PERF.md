# Armax Performance & Wire Size Metrics (Post-Quantum NIST FIPS 204 ML-DSA-65)

## 1. Executive Summary

This document presents empirical size and throughput measurements for the Armax post-quantum blockchain following the migration to NIST FIPS 204 ML-DSA-65 digital signatures.

Armax employs JSON wire serialization for P2P network blocks, mempool transactions, RPC endpoints, and bbolt database persistence. Because cryptographic public keys and signatures are represented as hexadecimal strings on the wire, post-quantum payload sizes reflect this encoding overhead.

---

## 2. Measured Transaction Sizes

| Component | Raw Binary Size | Hex Wire Characters | JSON Overhead & Metadata | Total Serialized Size |
|---|---|---|---|---|
| **Public Key (`sender_pub`)** | 1,952 bytes | 3,904 hex chars | Field key + quotes | ~3,920 bytes |
| **Signature (`signature`)** | 3,309 bytes | 6,618 hex chars | Field key + quotes | ~6,634 bytes |
| **Addresses (`sender`, `recipient`)** | ~64 bytes (Bech32) | 124 chars | Field keys + quotes | ~150 bytes |
| **Integers (`amount`, `fee`, `nonce`)**| 24 bytes (int64) | ASCII digits | Field keys + syntax | ~57 bytes |
| **1 Signed ML-DSA-65 Transaction** | **~5,385 bytes** | **10,522 hex chars** | **JSON formatting** | **10,761 bytes (~10.51 KB)** |

### Note on P2 Preliminary Measurement vs. Wire Serialization
- **Raw Cryptographic Payload**: ~5,540 bytes (reflecting in-memory raw binary representation of public key, signature, and state integers).
- **Actual JSON Wire Format**: **10,761 bytes** (reflecting 2x expansion from hexadecimal encoding of the 1,952-byte public key to 3,904 hex characters and the 3,309-byte signature to 6,618 hex characters, plus JSON keys and delimiters).

---

## 3. Measured Block Sizes

Armax enforces a consensus block size ceiling of `MaxBlockSize = 2,000,000 bytes` (2.0 MB). Each block contains 1 coinbase transaction (`Sender: SYSTEM`, `Signature: REWARD_SIG`) plus $N$ post-quantum user transactions.

| Block Configuration | Raw Byte Size (JSON) | Size in Kibibytes | Size in Megabytes | % of MaxBlockSize (2 MB) |
|---|---|---|---|---|
| **Empty Block (Coinbase only)** | 653 bytes | 0.64 KB | 0.0006 MB | 0.03% |
| **100 User Txs (+ 1 Coinbase = 101 Total)** | 1,076,253 bytes | 1,051.03 KB | 1.0264 MB | 53.81% |
| **150 User Txs (+ 1 Coinbase = 151 Total)** | 1,614,053 bytes | 1,576.22 KB | 1.5393 MB | 80.70% |
| **180 User Txs (+ 1 Coinbase = 181 Total)** | 1,936,733 bytes | 1,891.34 KB | 1.8470 MB | 96.84% |
| **100 Total Txs (1 Coinbase + 99 User Txs)** | 1,065,497 bytes | 1,040.52 KB | 1.0161 MB | 53.27% |
| **150 Total Txs (1 Coinbase + 149 User Txs)** | 1,603,297 bytes | 1,565.72 KB | 1.5290 MB | 80.16% |
| **180 Total Txs (1 Coinbase + 179 User Txs)** | 1,925,977 bytes | 1,880.84 KB | 1.8368 MB | 96.30% |

---

## 4. Maximum Block Capacity

Under the consensus limit `MaxBlockSize = 2,000,000 bytes`:

$$\text{Capacity} = \left\lfloor \frac{2,000,000 - 653}{10,761 + 1} \right\rfloor = 185 \text{ user transactions}$$

- **Maximum User Transactions per Block**: **185 transactions**
- **Maximum Total Transactions per Block (including Coinbase)**: **186 transactions**
- **Exact Serialized Block Size with 185 User Transactions**: **1,990,513 bytes** (~1.898 MB)
- **Safety Margin / Headroom**: **9,487 bytes** remaining below the 2,000,000-byte consensus limit.

---

## 5. Throughput and Mempool Economics

- **Target Block Time**: 120 seconds (2 minutes).
- **Maximum Transaction Throughput**:
  $$\text{Throughput} = \frac{185 \text{ transactions}}{120 \text{ seconds}} \approx 1.54 \text{ TPS}$$
- **Mempool Minimum Relay Fee**:
  $$\text{MinRelayFee} \times \lceil\text{txSize} / 1000\rceil = 1,000 \times \lceil 10,761 / 1,000 \rceil = 11,000 \text{ Armoids (0.00011 ARMX)}$$
- **Block Mining Bandwidth**:
  A full 2 MB block propagation requires ~1.99 MB of network transfer every 2 minutes (~133 Kbps sustained bandwidth), well within consumer and testnet network capabilities.
