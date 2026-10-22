package core

import (
	"testing"
)

// TestC21_InvalidHighFeeTxDoesNotEvictValidTx tests that an invalid transaction with a high fee
// does NOT evict an honest transaction from a full mempool.
func TestC21_InvalidHighFeeTxDoesNotEvictValidTx(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	accA := newTestAccount(t, "userA")
	privA, pubA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	_ = privA
	addrB := newTestAccount(t, "userB").Address

	hexA, _ := AddressToHex(addrA)
	bc.State.Lock.Lock()
	bc.State.Accounts[hexA] = AccountState{
		Balance: 1000 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	// Fill mempool to MaxMempoolSize with valid txs from synthetic accounts
	bc.Lock.Lock()
	for i := 0; i < MaxMempoolSize; i++ {
		dummyTx := Transaction{
			Sender:    addrA,
			SenderPub: pubA,
			Recipient: addrB,
			Amount:    1 * CoinUnit,
			Fee:       MinRelayFee * 12,
			Nonce:     int64(i + 1),
			Type:      TxTypeStandard,
		}
		bc.Mempool = append(bc.Mempool, dummyTx)
	}
	originalMempoolLen := len(bc.Mempool)
	firstTx := bc.Mempool[0]
	bc.Lock.Unlock()

	// Now try to submit an INVALID tx (invalid signature) with a huge fee
	invalidTx := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrB,
		Amount:    1 * CoinUnit,
		Fee:       20 * MinRelayFee,
		Nonce:     int64(MaxMempoolSize + 1),
		Type:      TxTypeStandard,
		Signature: "bad_signature_hex",
	}

	ok, _ := bc.AddTransaction(invalidTx)
	if ok {
		t.Fatalf("invalid transaction should be rejected")
	}

	// Verify that mempool was NOT modified and the first transaction was NOT evicted
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()

	if len(bc.Mempool) != originalMempoolLen {
		t.Errorf("mempool length changed: expected %d, got %d", originalMempoolLen, len(bc.Mempool))
	}
	if bc.Mempool[0].Nonce != firstTx.Nonce {
		t.Errorf("honest transaction was illegally evicted by invalid high-fee transaction!")
	}
}

// TestC21_EvictionPreservesNonceContinuityNoGap tests that when eviction occurs,
// only the highest-nonce (tail) transaction of a sender is evicted, preserving nonce continuity without gaps.
func TestC21_EvictionPreservesNonceContinuityNoGap(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	accA := newTestAccount(t, "userA")
	privA, pubA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	accB := newTestAccount(t, "userB")
	privB, pubB, addrB := accB.PrivKey, accB.PubHex, accB.Address
	addrC := newTestAccount(t, "userC").Address

	hexA, _ := AddressToHex(addrA)
	hexB, _ := AddressToHex(addrB)
	bc.State.Lock.Lock()
	bc.State.Accounts[hexA] = AccountState{
		Balance: 1000 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Accounts[hexB] = AccountState{
		Balance: 1000 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	// Fill mempool up to MaxMempoolSize:
	// Account A has 3 transactions with nonces 1, 2, 3 with fee 12000.
	// The rest are from other accounts with fee 15000.
	bc.Lock.Lock()
	txA1 := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrC,
		Amount:    1 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = txA1.Sign(privA)

	txA2 := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrC,
		Amount:    1 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     2,
		Type:      TxTypeStandard,
	}
	_ = txA2.Sign(privA)

	txA3 := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrC,
		Amount:    1 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     3,
		Type:      TxTypeStandard,
	}
	_ = txA3.Sign(privA)

	bc.Mempool = []Transaction{txA1, txA2, txA3}
	for i := 3; i < MaxMempoolSize; i++ {
		dummyTx := Transaction{
			Sender:    addrC,
			Recipient: addrB,
			Amount:    1 * CoinUnit,
			Fee:       MinRelayFee * 15,
			Nonce:     int64(i + 1),
			Type:      TxTypeStandard,
		}
		bc.Mempool = append(bc.Mempool, dummyTx)
	}
	bc.CleanMempoolLocked()
	bc.Lock.Unlock()

	// Now submit a new valid tx from B with fee 13000 (higher than A's fee 12000)
	txNew := Transaction{
		Sender:    addrB,
		SenderPub: pubB,
		Recipient: addrC,
		Amount:    1 * CoinUnit,
		Fee:       MinRelayFee * 13,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = txNew.Sign(privB)

	ok, msg := bc.AddTransaction(txNew)
	if !ok {
		t.Fatalf("new higher fee transaction should be added with eviction, got: %s", msg)
	}

	// Verify that if account A was evicted, ONLY nonce 3 was evicted, and nonces 1 and 2 remain!
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()

	var noncesA []int64
	for _, tx := range bc.Mempool {
		if tx.Sender == addrA {
			noncesA = append(noncesA, tx.Nonce)
		}
	}

	// Nonce list for A should be either [1, 2] (if txA3 was evicted) or [1, 2, 3] (if something else was evicted)
	// It must NEVER be [2, 3] or [1, 3] which would be a broken nonce chain!
	if len(noncesA) == 2 {
		if noncesA[0] != 1 || noncesA[1] != 2 {
			t.Errorf("eviction broke nonce continuity! got nonces: %v", noncesA)
		}
	}
}
