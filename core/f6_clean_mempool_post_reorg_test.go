//go:build testhooks

package core

import (
	"crypto/rand"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestF6_CleanMempoolPostReorgValidation tests that CleanMempoolLocked thoroughly re-validates
// deduplication, canonical self-transfers (AddressToHex), nonces, and balances against the active state [F6, F6-PERF].
func TestF6_CleanMempoolPostReorgValidation(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(false)
	_ = SetNetwork("regtest")

	bc := MustNewBlockchain(1)

	// Generate sender wallet
	var seed [32]byte
	_, _ = rand.Read(seed[:])
	sk, err := pqsig.GenerateFromSeed(seed)
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}
	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	// Generate recipient wallet
	var recSeed [32]byte
	_, _ = rand.Read(recSeed[:])
	recSk, err := pqsig.GenerateFromSeed(recSeed)
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}
	recAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, recSk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	// Fund sender in state with 10,000,000 atomic units
	senderHex, _ := AddressToHex(senderAddr)
	bc.Lock.Lock()
	bc.State.Accounts[senderHex] = AccountState{
		Balance: 10000000,
		Nonce:   0,
	}
	bc.Lock.Unlock()

	// Helper to create and sign a transaction
	createTx := func(nonce int64, amount int64, fee int64) Transaction {
		tx := Transaction{
			Sender:    senderAddr,
			Recipient: recAddr,
			Amount:    amount,
			Fee:       fee,
			Nonce:     nonce,
		}
		if err := tx.Sign(sk); err != nil {
			t.Fatalf("tx.Sign failed: %v", err)
		}
		return tx
	}

	// Create valid Tx1: amount 1,000,000, fee 10,000, nonce 1
	tx1 := createTx(1, 1000000, 10000)

	// Create valid Tx2: amount 2,000,000, fee 10,000, nonce 2
	tx2 := createTx(2, 2000000, 10000)

	// Create Tx3 with self-transfer (must be purged via AddressToHex canonical comparison) [F6-PERF]
	tx3SelfTransfer := createTx(3, 500000, 10000)
	tx3SelfTransfer.Recipient = senderAddr

	// Create Tx4 with invalid nonce (nonce 5 instead of 3)
	tx4BadNonce := createTx(5, 500000, 10000)

	// Create Tx5 which exceeds balance (amount 20,000,000)
	tx5Excessive := createTx(3, 20000000, 10000)

	// Verify that AddTransaction rejects an invalid signature upon entry [F6-PERF]
	txBadSig := createTx(3, 500000, 10000)
	sigChars := []byte(txBadSig.Signature)
	if sigChars[10] == '0' {
		sigChars[10] = '1'
	} else {
		sigChars[10] = '0'
	}
	txBadSig.Signature = string(sigChars)
	if ok, _ := bc.AddTransaction(txBadSig); ok {
		t.Fatalf("AddTransaction should have rejected transaction with invalid signature")
	}

	// Populate mempool with:
	// - tx1 (valid)
	// - tx1 duplicate (should be purged by dedup)
	// - tx3SelfTransfer (should be purged by AddressToHex self-transfer check)
	// - tx4BadNonce (should be purged by nonce sequence check)
	// - tx5Excessive (should be purged by balance check)
	// - tx2 (valid)
	bc.Mempool = []Transaction{tx1, tx1, tx3SelfTransfer, tx4BadNonce, tx5Excessive, tx2}

	bc.Lock.Lock()
	purged := bc.CleanMempoolLocked()
	bc.Lock.Unlock()

	// 4 transactions should have been purged (duplicate tx1, self-transfer tx3, bad nonce tx4, excessive tx5)
	if purged != 4 {
		t.Fatalf("Expected 4 transactions purged, got %d", purged)
	}

	// Mempool should retain exactly tx1 and tx2 in sequential order
	if len(bc.Mempool) != 2 {
		t.Fatalf("Expected 2 transactions in mempool, got %d", len(bc.Mempool))
	}
	if bc.Mempool[0].HashHex() != tx1.HashHex() || bc.Mempool[1].HashHex() != tx2.HashHex() {
		t.Fatalf("Mempool did not preserve valid transactions in order")
	}

	// Now simulate post-reorg state change: account balance is drained by reorg block
	bc.Lock.Lock()
	bc.State.Accounts[senderHex] = AccountState{
		Balance: 500000, // only 500k left, tx1 requires 1010000
		Nonce:   0,
	}
	purgedReorg := bc.CleanMempoolLocked()
	bc.Lock.Unlock()

	// Both tx1 and tx2 should now be purged due to insufficient balance post-reorg
	if purgedReorg != 2 || len(bc.Mempool) != 0 {
		t.Fatalf("Expected 2 transactions purged post-reorg drain, got %d purged, %d remaining", purgedReorg, len(bc.Mempool))
	}
}
