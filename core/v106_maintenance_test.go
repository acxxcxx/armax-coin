package core

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestBlockAndTransactionConcurrentCacheRace verifies that concurrent Size() invocations
// on Block and Transaction pointers do not trigger data races [v1.0.6].
func TestBlockAndTransactionConcurrentCacheRace(t *testing.T) {
	acc := newTestAccount(t)
	recip := newTestAccount(t)

	tx := &Transaction{
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: recip.Address,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}

	block := GetGenesisBlock()

	var wg sync.WaitGroup
	const numGoroutines = 30
	const iterations = 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sz := tx.Size()
				if sz <= 0 {
					t.Errorf("expected positive tx size, got %d", sz)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				bsz := block.Size()
				if bsz <= 0 {
					t.Errorf("expected positive block size, got %d", bsz)
				}
			}
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&tx.cachedSize) <= 0 {
		t.Fatalf("expected cachedSize > 0, got %d", atomic.LoadInt32(&tx.cachedSize))
	}
	if atomic.LoadInt32(&block.cachedSize) <= 0 {
		t.Fatalf("expected cachedSize > 0, got %d", atomic.LoadInt32(&block.cachedSize))
	}

	// Verify Sign invalidates cachedSize
	priv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate pqsig key: %v", err)
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if atomic.LoadInt32(&tx.cachedSize) != 0 {
		t.Fatalf("expected cachedSize to be reset to 0 after Sign, got %d", atomic.LoadInt32(&tx.cachedSize))
	}
	recomputed := tx.Size()
	if recomputed <= 0 || atomic.LoadInt32(&tx.cachedSize) != int32(recomputed) {
		t.Fatalf("expected recomputed size %d, got %d", recomputed, atomic.LoadInt32(&tx.cachedSize))
	}
}

// Concurrency Contract Documentation [FIX-03, v1.0.6]:
// In Armaxcoin, Transaction values do not contain internal mutexes and are intentionally
// passed by value across consensus and network boundaries (e.g., bc.AddTransaction(tx)).
// Therefore, concurrent mutation (Sign) and reading (Size) on the same Transaction instance
// without external synchronization is NOT supported by the API design.
// Transactions must follow an "immutable after publication" lifecycle:
// 1. A transaction is constructed and signed sequentially by its single-threaded creator.
// 2. Once published (e.g. submitted to mempool, broadcast via P2P, or included in a block),
//    the transaction is immutable.
// 3. Concurrent callers may safely invoke tx.Size() on an immutable transaction pointer;
//    atomic operations on cachedSize prevent data races on the cached length integer.

// TestTransactionSequentialSignAndSize verifies the supported lifecycle:
// a transaction is prepared and signed sequentially, resetting cachedSize,
// and subsequent Size() correctly computes the signed length [FIX-03, v1.0.6].
func TestTransactionSequentialSignAndSize(t *testing.T) {
	acc := newTestAccount(t)
	recip := newTestAccount(t)

	priv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	tx := &Transaction{
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: recip.Address,
		Amount:    50 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     2,
		Type:      TxTypeStandard,
	}

	// 1. Initial size before signing
	unsignedSize := tx.Size()
	if unsignedSize <= 0 {
		t.Fatalf("expected positive unsigned size, got %d", unsignedSize)
	}
	if atomic.LoadInt32(&tx.cachedSize) != int32(unsignedSize) {
		t.Fatalf("expected cachedSize %d, got %d", unsignedSize, atomic.LoadInt32(&tx.cachedSize))
	}

	// 2. Sign transaction sequentially (mutating signature and public key)
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("Sign error: %v", err)
	}

	// Sign must invalidate cachedSize
	if atomic.LoadInt32(&tx.cachedSize) != 0 {
		t.Fatalf("expected cachedSize 0 after Sign, got %d", atomic.LoadInt32(&tx.cachedSize))
	}

	// 3. Size recalculation after signing
	signedSize := tx.Size()
	if signedSize <= unsignedSize {
		t.Fatalf("expected signedSize %d > unsignedSize %d (signature adds bytes)", signedSize, unsignedSize)
	}
	if atomic.LoadInt32(&tx.cachedSize) != int32(signedSize) {
		t.Fatalf("expected cachedSize %d, got %d", signedSize, atomic.LoadInt32(&tx.cachedSize))
	}
}

// TestTransactionConcurrentSizeAfterPublication verifies that once published (immutable),
// concurrent goroutines calling Size() on a shared Transaction pointer are race-free [FIX-03, v1.0.6].
func TestTransactionConcurrentSizeAfterPublication(t *testing.T) {
	acc := newTestAccount(t)
	recip := newTestAccount(t)

	priv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	tx := &Transaction{
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: recip.Address,
		Amount:    50 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     2,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("Sign error: %v", err)
	}

	// Transaction is now published and immutable.
	expectedSize := tx.Size()
	tx.InvalidateCache() // Reset to verify concurrent first-time calculation

	var wg sync.WaitGroup
	const numReaders = 20
	const iterations = 50

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sz := tx.Size()
				if sz != expectedSize {
					t.Errorf("expected size %d, got %d", expectedSize, sz)
				}
			}
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&tx.cachedSize) != int32(expectedSize) {
		t.Fatalf("expected cachedSize %d, got %d", expectedSize, atomic.LoadInt32(&tx.cachedSize))
	}
}

// TestBlockMutationCacheInvalidation verifies that mutating block transactions
// and calling InvalidateCache produces accurate recomputed sizes [FIX-03, v1.0.6].
func TestBlockMutationCacheInvalidation(t *testing.T) {
	block := GetGenesisBlock()
	initialSize := block.Size()
	if initialSize <= 0 {
		t.Fatalf("expected positive initial size, got %d", initialSize)
	}

	// Add dummy transaction to block
	block.Transactions = append(block.Transactions, Transaction{
		Sender:    "armx1testsender",
		Recipient: "armx1testrecipient",
		Amount:    1000,
		Fee:       10,
		Nonce:     1,
		Type:      TxTypeStandard,
	})

	// Before invalidation, cachedSize still reports initial size
	if block.Size() != initialSize {
		t.Fatalf("expected cached size %d before invalidation, got %d", initialSize, block.Size())
	}

	// Invalidate cache
	block.InvalidateCache()
	if atomic.LoadInt32(&block.cachedSize) != 0 {
		t.Fatalf("expected block cachedSize to be 0 after InvalidateCache")
	}

	// Recompute size
	newSize := block.Size()
	if newSize <= initialSize {
		t.Fatalf("expected newSize %d > initialSize %d after adding transaction", newSize, initialSize)
	}
}
