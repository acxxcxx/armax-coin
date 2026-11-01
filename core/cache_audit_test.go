package core

import (
	"encoding/json"
	"math/big"
	"sync"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestTransactionCacheAllConstructionAndDeserializationPaths verifies cachedSize and Size()
// across all instantiation, deserialization, and mutation pathways [FIX-03, v1.0.6]
func TestTransactionCacheAllConstructionAndDeserializationPaths(t *testing.T) {
	// 1. Zero-value transaction: var tx Transaction
	var zeroTx Transaction
	sz0 := zeroTx.Size()
	if sz0 <= 0 {
		t.Fatalf("Zero-value transaction Size() must be > 0, got %d", sz0)
	}
	// Verify subsequent calls return identical size from cache
	if zeroTx.Size() != sz0 {
		t.Fatalf("Expected cached size %d, got %d", sz0, zeroTx.Size())
	}

	// 2. Struct literal transaction
	litTx := &Transaction{
		Sender:    "armx1testsender",
		Recipient: "armx1testrecipient",
		Amount:    500000000,
		Fee:       10000,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	szLit := litTx.Size()
	if szLit <= 0 {
		t.Fatalf("Struct literal transaction Size() must be > 0, got %d", szLit)
	}

	// 3. Deserialized transaction from JSON
	data, err := json.Marshal(litTx)
	if err != nil {
		t.Fatalf("Failed to marshal litTx: %v", err)
	}
	var desTx Transaction
	if err := json.Unmarshal(data, &desTx); err != nil {
		t.Fatalf("Failed to unmarshal transaction: %v", err)
	}
	szDes := desTx.Size()
	if szDes != szLit {
		t.Fatalf("Deserialized transaction size %d mismatch against original %d", szDes, szLit)
	}

	// 4. New transaction signed with ML-DSA-65 (FIPS 204)
	priv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	signedTx := &Transaction{
		Sender:    "armx1testcreator",
		Recipient: "armx1testreceiver",
		Amount:    1000000000,
		Fee:       MinRelayFee,
		Nonce:     2,
	}
	preSignSize := signedTx.Size()
	if preSignSize <= 0 {
		t.Fatalf("Pre-sign size must be > 0, got %d", preSignSize)
	}
	if err := signedTx.Sign(priv); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	postSignSize := signedTx.Size()
	if postSignSize <= preSignSize {
		t.Fatalf("Post-sign size %d must be greater than pre-sign size %d (ML-DSA-65 signature is ~3.3KB)", postSignSize, preSignSize)
	}

	// 5. Field mutation before and after cache usage with InvalidateCache()
	mutTx := &Transaction{
		Sender:    "armx1sender",
		Recipient: "armx1recipient",
		Amount:    100,
	}
	szInitial := mutTx.Size()

	// Mutate field: add large payload to HashLock
	mutTx.HashLock = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// Before InvalidateCache(), Size() returns cached value
	if mutTx.Size() != szInitial {
		t.Fatalf("Expected cached size %d before invalidation, got %d", szInitial, mutTx.Size())
	}
	// After InvalidateCache(), Size() must recompute and reflect new length
	mutTx.InvalidateCache()
	szMutated := mutTx.Size()
	if szMutated <= szInitial {
		t.Fatalf("Mutated size %d must be greater than initial %d", szMutated, szInitial)
	}
}

// TestBlockCacheAllConstructionAndDeserializationPaths verifies cachedSize and Size()
// across all Block instantiation, normal construction, and deserialization pathways [FIX-03, v1.0.6]
func TestBlockCacheAllConstructionAndDeserializationPaths(t *testing.T) {
	// 1. Zero-value block: var b Block
	var zeroBlock Block
	sz0 := zeroBlock.Size()
	if sz0 <= 0 {
		t.Fatalf("Zero-value block Size() must be > 0, got %d", sz0)
	}
	if zeroBlock.Size() != sz0 {
		t.Fatalf("Expected cached size %d, got %d", sz0, zeroBlock.Size())
	}

	// 2. Struct literal block
	litBlock := &Block{
		Index:        1,
		Timestamp:    1710000000,
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		MerkleRoot:   "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d",
		Miner:        "armx1testminer",
		Difficulty:   1,
		Nonce:        12345,
		Transactions: []Transaction{},
	}
	szLit := litBlock.Size()
	if szLit <= 0 {
		t.Fatalf("Struct literal block size must be > 0, got %d", szLit)
	}

	// 3. Deserialized block from JSON
	data, err := json.Marshal(litBlock)
	if err != nil {
		t.Fatalf("Failed to marshal litBlock: %v", err)
	}
	var desBlock Block
	if err := json.Unmarshal(data, &desBlock); err != nil {
		t.Fatalf("Failed to unmarshal block: %v", err)
	}
	szDes := desBlock.Size()
	if szDes != szLit {
		t.Fatalf("Deserialized block size %d mismatch against original %d", szDes, szLit)
	}

	// 4. Normal constructed block via NewBlock()
	txs := []Transaction{
		{
			Sender:    "armx1miner",
			Recipient: "armx1recipient",
			Amount:    50 * CoinUnit,
		},
	}
	constructedBlock := NewBlock(txs, litBlock.PrevHash, 2, "armx1miner", big.NewInt(1))
	szConstructed := constructedBlock.Size()
	if szConstructed <= 0 {
		t.Fatalf("Constructed block size must be > 0, got %d", szConstructed)
	}

	// 5. Block mutation and InvalidateCache()
	szPreMut := constructedBlock.Size()
	// Add another transaction
	constructedBlock.Transactions = append(constructedBlock.Transactions, Transaction{
		Sender:    "armx1second",
		Recipient: "armx1other",
		Amount:    100 * CoinUnit,
	})
	// Cache still holds old size before invalidation
	if constructedBlock.Size() != szPreMut {
		t.Fatalf("Expected cached size %d before invalidation, got %d", szPreMut, constructedBlock.Size())
	}
	// Invalidate cache
	constructedBlock.InvalidateCache()
	szPostMut := constructedBlock.Size()
	if szPostMut <= szPreMut {
		t.Fatalf("Post-mutation size %d must be greater than pre-mutation %d", szPostMut, szPreMut)
	}
}

// TestParallelCacheReadAndInvalidate verifies race-free parallel execution
// of Size() and InvalidateCache() across concurrent goroutines [FIX-03, v1.0.6]
func TestParallelCacheReadAndInvalidate(t *testing.T) {
	tx := &Transaction{
		Sender:    "armx1sender",
		Recipient: "armx1recipient",
		Amount:    1000000,
	}

	block := GetGenesisBlock()

	var wg sync.WaitGroup
	const workers = 20
	const iterations = 100

	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sz := tx.Size()
				if sz <= 0 {
					t.Errorf("worker %d: invalid tx size %d", workerID, sz)
				}
				if j%20 == 0 {
					tx.InvalidateCache()
				}
			}
		}(i)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				bsz := block.Size()
				if bsz <= 0 {
					t.Errorf("worker %d: invalid block size %d", workerID, bsz)
				}
				if j%20 == 0 {
					block.InvalidateCache()
				}
			}
		}(i)
	}
	wg.Wait()
}
