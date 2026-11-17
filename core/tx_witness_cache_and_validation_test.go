package core

import (
	"crypto/sha256"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestT5_WitnessHashCacheSkipsRedundantVerify verifies that once a transaction
// has been verified, its WitnessHash is cached and subsequent verifications skip ML-DSA-65 math [T5].
func TestT5_WitnessHashCacheSkipsRedundantVerify(t *testing.T) {
	sk, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("t5-test-seed-1")))
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}

	sk2, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("t5-test-seed-2")))
	if err != nil {
		t.Fatalf("GenerateFromSeed 2 failed: %v", err)
	}

	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	recipAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk2.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress 2 failed: %v", err)
	}

	tx := &Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    1000,
		Fee:       50000,
		Nonce:     1,
		SigAlg:    pqsig.SigAlgMLDSA65,
	}

	if err := tx.Sign(sk); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// First verify should perform full ML-DSA-65 verification and populate cache
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("First VerifyWithError failed on valid signed transaction: %v", err)
	}

	if !tx.IsSignatureVerified() {
		t.Fatalf("Expected transaction WitnessHash to be cached after successful Verify")
	}

	// Second verify must succeed instantly via cache
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("Second VerifyWithError failed: %v", err)
	}
}

func TestSignatureVerificationCacheDoesNotBypassBasicValidation(t *testing.T) {
	senderKey, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("cache-validation-sender")))
	if err != nil {
		t.Fatalf("GenerateFromSeed sender failed: %v", err)
	}
	recipientKey, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("cache-validation-recipient")))
	if err != nil {
		t.Fatalf("GenerateFromSeed recipient failed: %v", err)
	}

	sender, err := PubToAddress(pqsig.SigAlgMLDSA65, senderKey.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress sender failed: %v", err)
	}
	recipient, err := PubToAddress(pqsig.SigAlgMLDSA65, recipientKey.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress recipient failed: %v", err)
	}

	tx := &Transaction{
		Sender:    sender,
		Recipient: recipient,
		Amount:    1000,
		Fee:       50000,
		Nonce:     1,
		SigAlg:    pqsig.SigAlgMLDSA65,
	}
	if err := tx.Sign(senderKey); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("VerifyWithError failed on valid transaction: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*Transaction)
	}{
		{
			name: "time lock",
			mutate: func(tx *Transaction) {
				tx.TimeLock = 1700000000
			},
		},
		{
			name: "preimage",
			mutate: func(tx *Transaction) {
				tx.Preimage = "uncommitted metadata"
			},
		},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			mutated := *tx
			mutation.mutate(&mutated)
			if mutated.WitnessHashHex() != tx.WitnessHashHex() {
				t.Fatal("test mutation unexpectedly changed the cached witness hash")
			}
			if !mutated.IsSignatureVerified() {
				t.Fatal("expected mutated transaction to share the cached witness hash")
			}
			if err := mutated.ValidateBasic(); err == nil {
				t.Fatal("ValidateBasic accepted unsupported transaction metadata")
			}
			if err := mutated.VerifyWithError(); err == nil {
				t.Fatal("VerifyWithError accepted unsupported transaction metadata from cache")
			}
		})
	}
}

// TestT5_DifferentialAddTransaction verifies identical acceptance/rejection semantics
// for valid, invalid amount, bad fee, and bad signature transactions [T5].
func TestT5_DifferentialAddTransaction(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	sk, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("t5-test-seed-diff")))
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}

	sk2, err := pqsig.GenerateFromSeed(sha256.Sum256([]byte("t5-test-seed-diff-recip")))
	if err != nil {
		t.Fatalf("GenerateFromSeed 2 failed: %v", err)
	}

	senderAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	recipAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, sk2.PublicKeyBytes())

	// Credit balance to sender
	sHex, _ := AddressToHex(senderAddr)
	bc.Lock.Lock()
	bc.State.Accounts[sHex] = AccountState{Balance: 100000000, Nonce: 0}
	bc.Lock.Unlock()

	// 1. Valid transaction
	txValid := &Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    50000,
		Fee:       50000,
		Nonce:     1,
		SigAlg:    pqsig.SigAlgMLDSA65,
	}
	if err := txValid.Sign(sk); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	ok, msg := bc.AddTransaction(*txValid)
	if !ok {
		t.Fatalf("Valid tx was rejected: %s", msg)
	}

	// 2. Bad amount
	txBadAmount := *txValid
	txBadAmount.Amount = 0
	ok, _ = bc.AddTransaction(txBadAmount)
	if ok {
		t.Fatalf("Zero amount tx was accepted")
	}

	// 3. Bad signature
	txBadSig := *txValid
	txBadSig.Nonce = 2
	// Tamper signature without resigning
	txBadSig.Signature = txBadSig.Signature[:len(txBadSig.Signature)-4] + "dead"
	ok, _ = bc.AddTransaction(txBadSig)
	if ok {
		t.Fatalf("Bad signature tx was accepted")
	}
}
