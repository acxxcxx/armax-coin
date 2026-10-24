package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/acxxcxx/armax-coin/armaxhash"
)

type Block struct {
	Index        int           `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	MerkleRoot   string        `json:"merkle_root"` // 32-byte Binary Merkle Tree Root (BIP Standard)
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce"`
	Miner        string        `json:"miner"`
	Difficulty   int           `json:"difficulty"` // Human-readable difficulty integer
	Target       string        `json:"target"`     // Exact 256-bit Proof of Work target ceiling (64 hex characters)
	cachedSize   int           `json:"-"`          // Cached serialized byte size [C-28, C-34]
}

// CalculateMerkleRoot returns the binary Merkle root of the block's transactions
func (b *Block) CalculateMerkleRoot() string {
	return CalculateMerkleRoot(b.Transactions)
}

// GetTarget returns the 256-bit target integer represented by the block's Target field
func (b *Block) GetTarget() *big.Int {
	if len(b.Target) == 64 {
		tBytes, err := hex.DecodeString(b.Target)
		if err == nil {
			return new(big.Int).SetBytes(tBytes)
		}
	}
	return DifficultyToTarget(b.Difficulty)
}

// HeaderBytes returns the canonical serialized block header representation.
func (b *Block) HeaderBytes() []byte {
	targetStr := b.Target
	if targetStr == "" {
		targetStr = fmt.Sprintf("%064x", DifficultyToTarget(b.Difficulty))
	}
	record := fmt.Sprintf("%d:%d:%s:%s:%s:%d:%s", b.Index, b.Timestamp, b.PrevHash, b.MerkleRoot, b.Miner, b.Nonce, targetStr)
	return []byte(record)
}

// CalculateSha256dHash computes the legacy double-SHA256 block header hash.
func (b *Block) CalculateSha256dHash() string {
	h1 := sha256.Sum256(b.HeaderBytes())
	h2 := sha256.Sum256(h1[:])
	return hex.EncodeToString(h2[:])
}

// CalculateArmaxHash computes the ArmaxHash v1 block header digest.
func (b *Block) CalculateArmaxHash() (string, error) {
	digest, err := armaxhash.Hash(b.HeaderBytes(), NetworkID())
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest[:]), nil
}

// CalculateArmaxHashV2 computes the ArmaxHash v2 (Stream-Fast Chained BLAKE2b) block header digest.
func (b *Block) CalculateArmaxHashV2() (string, error) {
	digest, err := armaxhash.HashV2(b.HeaderBytes(), NetworkID())
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest[:]), nil
}

// CalculateHashWithError computes the canonical block header hash according to active consensus rules.
// When ArmaxHash v2 is active, CalculateArmaxHashV2 is evaluated.
// When ArmaxHash v1 is active, CalculateArmaxHash is evaluated.
// If hashing fails, the error is returned immediately.
// It NEVER falls back to SHA-256d or returns an empty hash under failure.
func (b *Block) CalculateHashWithError() (string, error) {
	if IsArmaxHashV2Active(b.Index) {
		return b.CalculateArmaxHashV2()
	}
	if IsArmaxHashActive(b.Index) {
		return b.CalculateArmaxHash()
	}
	return b.CalculateSha256dHash(), nil
}

// CalculateHash deterministically computes the block header hash according to the
// active PoW consensus rules for this block's height.
//
// DEPRECATED for consensus validation: This method returns a string only and cannot
// distinguish between a valid hash and an error without inspecting string contents.
// All consensus paths (block validation, mining, P2P block ingestion, orphan handling,
// chain validation, and template generation) MUST use CalculateHashWithError().
//
// If an error occurs under active ArmaxHash, it returns an explicit unmatchable error
// sentinel prefixed with "INVALID_HASH_ERROR:". This sentinel is guaranteed to fail
// 32-byte hex decoding, target verification, PrevHash linkage, and database storage.
func (b *Block) CalculateHash() string {
	hash, err := b.CalculateHashWithError()
	if err != nil {
		return "INVALID_HASH_ERROR:" + err.Error()
	}
	return hash
}

// PoWAlgorithm returns the PoW algorithm name used for this block ("sha256d", "armaxhash", or "armaxhash-v2").
func (b *Block) PoWAlgorithm() string {
	if IsArmaxHashV2Active(b.Index) {
		return "armaxhash-v2"
	}
	if IsArmaxHashActive(b.Index) {
		return "armaxhash"
	}
	return "sha256d"
}

// Size returns the approximate serialized byte size of the block, caching the result [C-28, C-34]
func (b *Block) Size() int {
	if b.cachedSize > 0 {
		return b.cachedSize
	}
	data, err := json.Marshal(b)
	if err != nil {
		return 0
	}
	b.cachedSize = len(data)
	return b.cachedSize
}

// NewBlock constructs a new Block instance with Merkle root computation
func NewBlock(transactions []Transaction, prevHash string, index int, miner string, target *big.Int) *Block {
	if target == nil || target.Sign() <= 0 {
		target = DifficultyToTarget(1)
	}
	targetHex := fmt.Sprintf("%064x", target)
	return &Block{
		Index:        index,
		Timestamp:    time.Now().Unix(),
		Transactions: transactions,
		PrevHash:     prevHash,
		MerkleRoot:   CalculateMerkleRoot(transactions),
		Nonce:        0,
		Miner:        miner,
		Difficulty:   TargetToDifficulty(target),
		Target:       targetHex,
	}
}
