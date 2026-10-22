package core

import (
	"sync"
	"testing"
)

// TestC28_C34_CachedSizeOnBlockAndTx verifies that Size() caches its serialized length [C-28, C-34]
func TestC28_C34_CachedSizeOnBlockAndTx(t *testing.T) {
	acc := newTestAccount(t)
	pubHex, addr := acc.PubHex, acc.Address
	recipAddr := newTestAccount(t).Address

	tx := Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: recipAddr,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}

	sz1 := tx.Size()
	if sz1 <= 0 {
		t.Fatalf("expected positive tx size, got %d", sz1)
	}
	if tx.cachedSize != sz1 {
		t.Fatalf("expected tx.cachedSize to be set to %d, got %d", sz1, tx.cachedSize)
	}

	b := GetGenesisBlock()
	bsz1 := b.Size()
	if bsz1 <= 0 {
		t.Fatalf("expected positive block size, got %d", bsz1)
	}
	if b.cachedSize != bsz1 {
		t.Fatalf("expected b.cachedSize to be set to %d, got %d", bsz1, b.cachedSize)
	}
}

// TestC28_C34_ConcurrentReadUnderRLockNoDataRace verifies that GetBlockReward and GetCirculatingSupply
// are pure reads with no internal writes under concurrent access [C-28, C-34]
func TestC28_C34_ConcurrentReadUnderRLockNoDataRace(t *testing.T) {
	bc, _, _, _ := setupTestChainWithFunds(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				bc.Lock.RLock()
				_ = bc.GetBlockReward(1)
				_ = bc.GetCirculatingSupply()
				bc.Lock.RUnlock()
			}
		}()
	}
	wg.Wait()
}
