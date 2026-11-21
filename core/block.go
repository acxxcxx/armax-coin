package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/armaxcoincore/armax-coin/armaxhash"
)

const (
	// BlockBinaryVersionV1 identifies version 1 of the binary block serialization format.
	BlockBinaryVersionV1 byte = 0x01

	blkFlagCanonicalFixed byte = 1 << 0
	blkFlagHasHash        byte = 1 << 1
)

type Block struct {
	Index            int           `json:"index"`
	Timestamp        int64         `json:"timestamp"`
	Transactions     []Transaction `json:"transactions"`
	PrevHash         string        `json:"prev_hash"`
	MerkleRoot       string        `json:"merkle_root"` // 32-byte Binary Merkle Tree Root (BIP Standard)
	Hash             string        `json:"hash"`
	Nonce            int64         `json:"nonce"`
	Miner            string        `json:"miner"`
	Difficulty       int           `json:"difficulty"` // Human-readable difficulty integer
	Target           string        `json:"target"`     // Exact 256-bit Proof of Work target ceiling (64 hex characters)
	cachedSize       int32         `json:"-"`          // Cached JSON serialized byte size [C-28, C-34, v1.0.6]
	cachedBinarySize int32         `json:"-"`          // Cached binary serialized byte size
}

// CalculateMerkleRoot returns the binary Merkle root of the block's transactions
func (b *Block) CalculateMerkleRoot() string {
	return CalculateMerkleRoot(b.Transactions)
}

// CalculateMerkleRootForNetwork returns the binary Merkle root of the block's transactions for a specific network ID [C-33]
func (b *Block) CalculateMerkleRootForNetwork(netID string) string {
	return CalculateMerkleRootForNetwork(b.Transactions, netID)
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

// CalculateArmaxHash computes the ArmaxHash v1 block header digest for the active network.
func (b *Block) CalculateArmaxHash() (string, error) {
	return b.CalculateArmaxHashForNetwork(NetworkID())
}

// CalculateArmaxHashForNetwork computes the ArmaxHash v1 block header digest for a specific network ID.
func (b *Block) CalculateArmaxHashForNetwork(netID string) (string, error) {
	digest, err := armaxhash.Hash(b.HeaderBytes(), netID)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest[:]), nil
}

// CalculateArmaxHashV2 computes the ArmaxHash v2 (Stream-Fast Chained BLAKE2b) block header digest for the active network.
func (b *Block) CalculateArmaxHashV2() (string, error) {
	return b.CalculateArmaxHashV2ForNetwork(NetworkID())
}

// CalculateArmaxHashV2ForNetwork computes the ArmaxHash v2 block header digest for a specific network ID.
func (b *Block) CalculateArmaxHashV2ForNetwork(netID string) (string, error) {
	digest, err := armaxhash.HashV2(b.HeaderBytes(), netID)
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

// CalculateHashWithErrorForProfile evaluates the block header hash against an explicit NetworkProfile
// without reading global ActiveNetwork state.
func (b *Block) CalculateHashWithErrorForProfile(prof NetworkProfile) (string, error) {
	if prof.ArmaxHashV2ActivationHeight > 0 && b.Index > 0 && int64(b.Index) >= prof.ArmaxHashV2ActivationHeight {
		return b.CalculateArmaxHashV2ForNetwork(prof.NetworkID)
	}
	if prof.ArmaxHashActivationHeight > 0 && b.Index > 0 && int64(b.Index) >= prof.ArmaxHashActivationHeight {
		return b.CalculateArmaxHashForNetwork(prof.NetworkID)
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

// Size returns the approximate serialized byte size of the block, caching the result [C-28, C-34, v1.0.6]
func (b *Block) Size() int {
	if cached := atomic.LoadInt32(&b.cachedSize); cached > 0 {
		return int(cached)
	}
	data, err := json.Marshal(b)
	if err != nil {
		return 0
	}
	sz := int32(len(data))
	atomic.StoreInt32(&b.cachedSize, sz)
	return int(sz)
}

// InvalidateCache resets the cached serialized byte sizes of the block [v1.0.6]
func (b *Block) InvalidateCache() {
	atomic.StoreInt32(&b.cachedSize, 0)
	atomic.StoreInt32(&b.cachedBinarySize, 0)
}

// EffectiveBlockSize returns the consensus-effective serialized byte size of b at b.Index.
// Before BlockBinarySizeActivationHeight it returns legacy JSON b.Size(); from activation onward
// it returns b.BinarySize() (falling back to b.Size() if any transaction is malformed).
func EffectiveBlockSize(b *Block) int {
	if b == nil {
		return 0
	}
	if IsBlockBinarySizeActive(b.Index) {
		if binSz := b.BinarySize(); binSz > 0 {
			return binSz
		}
	}
	return b.Size()
}

func decodeCanonicalHex32(s string) ([]byte, bool) {
	if len(s) != 64 {
		return nil, false
	}
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != s {
		return nil, false
	}
	return raw, true
}

func appendBlockBinaryHeader(buf []byte, b *Block) ([]byte, error) {
	prevRaw, prevOK := decodeCanonicalHex32(b.PrevHash)
	merkleRaw, merkleOK := decodeCanonicalHex32(b.MerkleRoot)
	targetRaw, targetOK := decodeCanonicalHex32(b.Target)
	hrp, minerRaw, minerErr := Bech32Decode(b.Miner)
	minerOK := minerErr == nil && hrp == Bech32HRP && len(minerRaw) == AddressPayloadLen

	var hashRaw []byte
	hasHash := b.Hash != ""
	hashOK := true
	if hasHash {
		hashRaw, hashOK = decodeCanonicalHex32(b.Hash)
	}

	var flags byte
	if hasHash {
		flags |= blkFlagHasHash
	}
	canonical := prevOK && merkleOK && targetOK && minerOK && hashOK
	if canonical {
		flags |= blkFlagCanonicalFixed
	}

	buf = append(buf, BlockBinaryVersionV1, flags)

	var u64 [8]byte
	var u32 [4]byte

	binary.LittleEndian.PutUint64(u64[:], uint64(b.Index))
	buf = append(buf, u64[:]...)
	binary.LittleEndian.PutUint64(u64[:], uint64(b.Timestamp))
	buf = append(buf, u64[:]...)

	if canonical {
		buf = append(buf, prevRaw...)
		buf = append(buf, merkleRaw...)
		if hasHash {
			buf = append(buf, hashRaw...)
		}
		binary.LittleEndian.PutUint64(u64[:], uint64(b.Nonce))
		buf = append(buf, u64[:]...)
		buf = append(buf, minerRaw...)
		binary.LittleEndian.PutUint32(u32[:], uint32(b.Difficulty))
		buf = append(buf, u32[:]...)
		buf = append(buf, targetRaw...)
	} else {
		var err error
		if buf, err = appendU16Bytes(buf, []byte(b.PrevHash)); err != nil {
			return nil, err
		}
		if buf, err = appendU16Bytes(buf, []byte(b.MerkleRoot)); err != nil {
			return nil, err
		}
		if hasHash {
			if buf, err = appendU16Bytes(buf, []byte(b.Hash)); err != nil {
				return nil, err
			}
		}
		binary.LittleEndian.PutUint64(u64[:], uint64(b.Nonce))
		buf = append(buf, u64[:]...)
		if buf, err = appendU16Bytes(buf, []byte(b.Miner)); err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(u32[:], uint32(b.Difficulty))
		buf = append(buf, u32[:]...)
		if buf, err = appendU16Bytes(buf, []byte(b.Target)); err != nil {
			return nil, err
		}
	}

	binary.LittleEndian.PutUint32(u32[:], uint32(len(b.Transactions)))
	buf = append(buf, u32[:]...)
	return buf, nil
}

// BinarySize computes and caches the exact binary serialized byte size of the block
// (194-byte canonical header + length-prefixed TxBinaryV1 Coinbase & regular transactions).
func (b *Block) BinarySize() int {
	if cached := atomic.LoadInt32(&b.cachedBinarySize); cached > 0 {
		return int(cached)
	}
	hdrBuf, err := appendBlockBinaryHeader(make([]byte, 0, 200), b)
	if err != nil {
		return 0
	}
	total := len(hdrBuf)
	for i := range b.Transactions {
		txSize := b.Transactions[i].BinarySize()
		if txSize <= 0 {
			return 0
		}
		total += 4 + txSize
	}
	atomic.StoreInt32(&b.cachedBinarySize, int32(total))
	return total
}

// MarshalBinary encodes the block header and all transactions into compact binary format.
func (b *Block) MarshalBinary() ([]byte, error) {
	if b == nil {
		return nil, errors.New("cannot marshal nil block")
	}
	hdrBuf, err := appendBlockBinaryHeader(make([]byte, 0, 200+len(b.Transactions)*5324), b)
	if err != nil {
		return nil, err
	}
	buf := hdrBuf
	var u32 [4]byte
	for i := range b.Transactions {
		txBytes, err := b.Transactions[i].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("tx #%d MarshalBinary failed: %w", i, err)
		}
		binary.LittleEndian.PutUint32(u32[:], uint32(len(txBytes)))
		buf = append(buf, u32[:]...)
		buf = append(buf, txBytes...)
	}
	atomic.StoreInt32(&b.cachedBinarySize, int32(len(buf)))
	return buf, nil
}

// UnmarshalBinary decodes a compact binary block buffer back into Block.
func (b *Block) UnmarshalBinary(data []byte) error {
	if b == nil {
		return errors.New("cannot unmarshal into nil block")
	}
	if len(data) < 22 {
		return errors.New("block binary buffer too short")
	}
	if data[0] != BlockBinaryVersionV1 {
		return fmt.Errorf("unsupported block binary version: 0x%02x", data[0])
	}
	flags := data[1]
	canonical := (flags & blkFlagCanonicalFixed) != 0
	hasHash := (flags & blkFlagHasHash) != 0
	off := 2

	var decoded Block
	decoded.Index = int(binary.LittleEndian.Uint64(data[off : off+8]))
	off += 8
	decoded.Timestamp = int64(binary.LittleEndian.Uint64(data[off : off+8]))
	off += 8

	if canonical {
		need := 32 + 32 + 8 + 32 + 4 + 32 + 4
		if hasHash {
			need += 32
		}
		if off+need > len(data) {
			return errors.New("unexpected EOF reading canonical binary block header")
		}
		decoded.PrevHash = hex.EncodeToString(data[off : off+32])
		off += 32
		decoded.MerkleRoot = hex.EncodeToString(data[off : off+32])
		off += 32
		if hasHash {
			decoded.Hash = hex.EncodeToString(data[off : off+32])
			off += 32
		}
		decoded.Nonce = int64(binary.LittleEndian.Uint64(data[off : off+8]))
		off += 8
		minerAddr, err := Bech32Encode(Bech32HRP, data[off:off+AddressPayloadLen])
		if err != nil {
			return fmt.Errorf("failed encoding block miner address: %w", err)
		}
		decoded.Miner = minerAddr
		off += AddressPayloadLen
		decoded.Difficulty = int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		decoded.Target = hex.EncodeToString(data[off : off+32])
		off += 32
	} else {
		pBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading prev_hash: %w", err)
		}
		decoded.PrevHash = string(pBytes)

		mBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading merkle_root: %w", err)
		}
		decoded.MerkleRoot = string(mBytes)

		if hasHash {
			hBytes, err := readU16Bytes(data, &off)
			if err != nil {
				return fmt.Errorf("failed reading block hash: %w", err)
			}
			decoded.Hash = string(hBytes)
		}

		if off+8 > len(data) {
			return errors.New("unexpected EOF reading nonce")
		}
		decoded.Nonce = int64(binary.LittleEndian.Uint64(data[off : off+8]))
		off += 8

		minerBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading miner: %w", err)
		}
		decoded.Miner = string(minerBytes)

		if off+4 > len(data) {
			return errors.New("unexpected EOF reading difficulty")
		}
		decoded.Difficulty = int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4

		tBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading target: %w", err)
		}
		decoded.Target = string(tBytes)
	}

	if off+4 > len(data) {
		return errors.New("unexpected EOF reading tx count")
	}
	txCount := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4

	if txCount < 0 || txCount > MaxBlockTransactions || off+txCount*4 > len(data) {
		return fmt.Errorf("invalid tx count in binary block: %d", txCount)
	}
	decoded.Transactions = make([]Transaction, txCount)
	for i := 0; i < txCount; i++ {
		if off+4 > len(data) {
			return fmt.Errorf("unexpected EOF reading tx #%d length", i)
		}
		txLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		if txLen <= 0 || off+txLen > len(data) {
			return fmt.Errorf("invalid tx #%d length %d at offset %d", i, txLen, off)
		}
		if err := decoded.Transactions[i].UnmarshalBinary(data[off : off+txLen]); err != nil {
			return fmt.Errorf("tx #%d UnmarshalBinary failed: %w", i, err)
		}
		off += txLen
	}

	if off != len(data) {
		return fmt.Errorf("trailing bytes in block binary payload: consumed %d of %d", off, len(data))
	}

	atomic.StoreInt32(&decoded.cachedBinarySize, int32(len(data)))
	*b = decoded
	return nil
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
