package core

import (
	"strings"
	"testing"
)

// TestConsensus_StandardTransactionHTLCSoftFork documents and verifies the consensus tightening (Soft-Fork)
// where non-empty HashLock, TimeLock, and Preimage are rejected on STANDARD transactions [Follow-up Point 2].
//
// Classification:
//   - Prior behavior (v3.2.0 / C-29): tx.VerifyWithError() accepted STANDARD transactions with populated HashLock/TimeLock/Preimage,
//     even though state execution ignored them and tx.Hash() had field commitment asymmetry (malleability risk).
//   - New behavior (v3.2.1 / Phase 6): tx.VerifyWithError() strictly tightens validation by rejecting any STANDARD transaction
//     with HashLock != "", TimeLock != 0, or Preimage != "".
//   - Chain Invariant: No valid transaction in testnet, genesis, or existing wallets has ever populated these fields.
func TestConsensus_StandardTransactionHTLCSoftFork(t *testing.T) {
	accA := newTestAccount(t, "sender")
	accB := newTestAccount(t, "recipient")

	// 1. Legitimate STANDARD transaction: must PASS
	validTx := Transaction{
		Sender:    accA.Address,
		SenderPub: accA.PubHex,
		Recipient: accB.Address,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
		HashLock:  "",
		TimeLock:  0,
		Preimage:  "",
	}
	if err := validTx.Sign(accA.PrivKey); err != nil {
		t.Fatalf("Signing validTx failed: %v", err)
	}
	if err := validTx.VerifyWithError(); err != nil {
		t.Fatalf("Valid standard transaction unexpectedly failed: %v", err)
	}

	// 2. STANDARD transaction with HashLock: must be REJECTED (Consensus Soft-Fork tightening)
	txWithHashLock := validTx
	txWithHashLock.HashLock = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	txWithHashLock.Nonce = 2
	if err := txWithHashLock.Sign(accA.PrivKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}
	err := txWithHashLock.VerifyWithError()
	if err == nil || !strings.Contains(err.Error(), "unsupported field hash_lock in STANDARD transaction") {
		t.Fatalf("Expected rejection of HashLock on STANDARD transaction, got: %v", err)
	}

	// 3. STANDARD transaction with TimeLock: must be REJECTED (Consensus Soft-Fork tightening)
	txWithTimeLock := validTx
	txWithTimeLock.TimeLock = 1700000000
	txWithTimeLock.Nonce = 3
	if err := txWithTimeLock.Sign(accA.PrivKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}
	err = txWithTimeLock.VerifyWithError()
	if err == nil || !strings.Contains(err.Error(), "unsupported field time_lock in STANDARD transaction") {
		t.Fatalf("Expected rejection of TimeLock on STANDARD transaction, got: %v", err)
	}

	// 4. STANDARD transaction with Preimage: must be REJECTED (Consensus Soft-Fork tightening)
	txWithPreimage := validTx
	txWithPreimage.Preimage = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	txWithPreimage.Nonce = 4
	if err := txWithPreimage.Sign(accA.PrivKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}
	err = txWithPreimage.VerifyWithError()
	if err == nil || !strings.Contains(err.Error(), "unsupported field preimage in STANDARD transaction") {
		t.Fatalf("Expected rejection of Preimage on STANDARD transaction, got: %v", err)
	}

	// 5. Empty Type ("") with HashLock: must also be REJECTED
	txEmptyType := validTx
	txEmptyType.Type = ""
	txEmptyType.HashLock = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	txEmptyType.Nonce = 5
	if err := txEmptyType.Sign(accA.PrivKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}
	err = txEmptyType.VerifyWithError()
	if err == nil || !strings.Contains(err.Error(), "unsupported field hash_lock in STANDARD transaction") {
		t.Fatalf("Expected rejection of HashLock on empty-type transaction, got: %v", err)
	}
}
