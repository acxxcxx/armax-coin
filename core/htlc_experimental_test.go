package core

import (
	"testing"
	"time"
)

// TestHTLCFlow verifies the experimental HTLC flow (Generate secret, Verify preimage, ValidateHTLC).
// Note: HTLC transactions are experimental and strictly not permitted in consensus blocks [C-29].
func TestHTLCFlow(t *testing.T) {
	preimage, hashLock, err := GenerateHTLCSecret()
	if err != nil {
		t.Fatalf("GenerateHTLCSecret failed: %v", err)
	}
	if !VerifyHTLCPreimage(preimage, hashLock) {
		t.Fatalf("VerifyHTLCPreimage failed for valid preimage")
	}
	if VerifyHTLCPreimage("0000000000000000000000000000000000000000000000000000000000000000", hashLock) {
		t.Fatalf("VerifyHTLCPreimage should have failed for fake preimage")
	}

	// Validate HTLC Lock
	txLock := Transaction{
		Type:      TxTypeHTLCLock,
		HashLock:  hashLock,
		TimeLock:  time.Now().Unix() + 3600,
		Amount:    100,
		Fee:       1,
		Recipient: GenesisMiner,
	}
	if err := txLock.ValidateHTLC(10, time.Now().Unix()); err != nil {
		t.Fatalf("txLock ValidateHTLC failed: %v", err)
	}

	// Validate HTLC Claim
	txClaim := Transaction{
		Type:      TxTypeHTLCClaim,
		HashLock:  hashLock,
		Preimage:  preimage,
		Amount:    100,
		Fee:       1,
		Recipient: GenesisMiner,
	}
	if err := txClaim.ValidateHTLC(10, time.Now().Unix()); err != nil {
		t.Fatalf("txClaim ValidateHTLC failed: %v", err)
	}
}
