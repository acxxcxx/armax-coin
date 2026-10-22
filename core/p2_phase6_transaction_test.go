package core

import (
	"strings"
	"testing"
)

// TestPhase6_StandardTransactionHTLCMetadataRejection verifies that as long as consensus
// only accepts STANDARD transactions, unsupported HTLC metadata fields (HashLock, TimeLock, Preimage)
// are strictly rejected by ValidateBasic() [Phase 6].
func TestPhase6_StandardTransactionHTLCMetadataRejection(t *testing.T) {
	accSender := newTestAccount(t, "sender")
	accRecip := newTestAccount(t, "recip")

	baseTx := func() Transaction {
		tx := Transaction{
			Type:      TxTypeStandard,
			Sender:    accSender.Address,
			SenderPub: accSender.PubHex,
			Recipient: accRecip.Address,
			Amount:    100,
			Fee:       1,
			Nonce:     1,
			SigAlg:    1,
		}
		if err := tx.Sign(accSender.PrivKey); err != nil {
			t.Fatalf("Sign failed: %v", err)
		}
		return tx
	}

	// 1. Valid standard transaction passes
	validTx := baseTx()
	if err := validTx.ValidateBasic(); err != nil {
		t.Fatalf("Expected valid standard tx to pass ValidateBasic, got: %v", err)
	}

	// 2. HashLock attached to standard tx must be rejected
	txHashLock := baseTx()
	txHashLock.HashLock = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_ = txHashLock.Sign(accSender.PrivKey)
	err := txHashLock.ValidateBasic()
	if err == nil || !strings.Contains(err.Error(), "unsupported field hash_lock in STANDARD transaction") {
		t.Fatalf("Expected rejection of hash_lock in standard tx, got: %v", err)
	}

	// 3. TimeLock attached to standard tx must be rejected
	txTimeLock := baseTx()
	txTimeLock.TimeLock = 1700000000
	_ = txTimeLock.Sign(accSender.PrivKey)
	err = txTimeLock.ValidateBasic()
	if err == nil || !strings.Contains(err.Error(), "unsupported field time_lock in STANDARD transaction") {
		t.Fatalf("Expected rejection of time_lock in standard tx, got: %v", err)
	}

	// 4. Preimage attached to standard tx must be rejected
	txPreimage := baseTx()
	txPreimage.Preimage = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	_ = txPreimage.Sign(accSender.PrivKey)
	err = txPreimage.ValidateBasic()
	if err == nil || !strings.Contains(err.Error(), "unsupported field preimage in STANDARD transaction") {
		t.Fatalf("Expected rejection of preimage in standard tx, got: %v", err)
	}

	// 5. Non-STANDARD tx types (e.g. unactivated HTLC) must be rejected by consensus
	txHTLCLock := baseTx()
	txHTLCLock.Type = "HTLC_LOCK"
	_ = txHTLCLock.Sign(accSender.PrivKey)
	err = txHTLCLock.ValidateBasic()
	if err == nil || (!strings.Contains(err.Error(), "invalid transaction type") && !strings.Contains(err.Error(), "not activated in consensus")) {
		t.Fatalf("Expected rejection of unactivated tx type, got: %v", err)
	}
}

// TestPhase6_AllAcceptedFieldsCommittedToHash proves that every field accepted by consensus
// participates in the canonical hash and signature commitment [Phase 6].
func TestPhase6_AllAcceptedFieldsCommittedToHash(t *testing.T) {
	accSender := newTestAccount(t, "sender")
	accRecip := newTestAccount(t, "recip")

	baseTx := Transaction{
		Type:      TxTypeStandard,
		Sender:    accSender.Address,
		SenderPub: accSender.PubHex,
		Recipient: accRecip.Address,
		Amount:    100,
		Fee:       1,
		Nonce:     1,
		SigAlg:    1,
	}
	baseHash := baseTx.HashHex()

	// Mutate Amount -> Hash must change
	txAmt := baseTx
	txAmt.Amount = 101
	if txAmt.HashHex() == baseHash {
		t.Fatalf("Mutating Amount did not change transaction hash!")
	}

	// Mutate Fee -> Hash must change
	txFee := baseTx
	txFee.Fee = 2
	if txFee.HashHex() == baseHash {
		t.Fatalf("Mutating Fee did not change transaction hash!")
	}

	// Mutate Nonce -> Hash must change
	txNonce := baseTx
	txNonce.Nonce = 2
	if txNonce.HashHex() == baseHash {
		t.Fatalf("Mutating Nonce did not change transaction hash!")
	}

	// Mutate SenderPub -> Hash must change
	txPub := baseTx
	txPub.SenderPub = strings.Repeat("aa", 1952)
	if txPub.HashHex() == baseHash {
		t.Fatalf("Mutating SenderPub did not change transaction hash!")
	}

	// Mutate Recipient -> Hash must change
	accOther := newTestAccount(t, "other")
	txRecip := baseTx
	txRecip.Recipient = accOther.Address
	if txRecip.HashHex() == baseHash {
		t.Fatalf("Mutating Recipient did not change transaction hash!")
	}

	// Mutate SigAlg -> Hash must change
	txSigAlg := baseTx
	txSigAlg.SigAlg = 2
	if txSigAlg.HashHex() == baseHash {
		t.Fatalf("Mutating SigAlg did not change transaction hash!")
	}
}
