package core

import (
	"testing"
)

// TestC29_RejectNonStandardTypesInTxVerify tests that tx.Verify rejects all types other than "" and TxTypeStandard.
// Specifically, HTLC types ("HTLC_LOCK", "HTLC_CLAIM", "HTLC_REFUND") must be rejected by consensus Verify().
func TestC29_RejectNonStandardTypesInTxVerify(t *testing.T) {
	accA := newTestAccount(t, "userA")
	privA, pubA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	addrB := newTestAccount(t, "userB").Address

	disallowedTypes := []string{
		"HTLC_LOCK",
		"HTLC_CLAIM",
		"HTLC_REFUND",
		"CONTRACT_DEPLOY",
		"TOKEN_MINT",
		"UNKNOWN_TYPE",
	}

	for _, badType := range disallowedTypes {
		tx := Transaction{
			Sender:    addrA,
			SenderPub: pubA,
			Recipient: addrB,
			Amount:    10 * CoinUnit,
			Fee:       MinRelayFee,
			Nonce:     1,
			Type:      badType,
		}
		if err := tx.Sign(privA); err != nil {
			t.Fatalf("signing failed: %v", err)
		}

		if tx.Verify() {
			t.Errorf("tx.Verify() should REJECT non-standard transaction type '%s', but it accepted it!", badType)
		}
	}

	// Verify that "" and TxTypeStandard ("STANDARD") ARE accepted
	allowedTypes := []string{"", TxTypeStandard}
	for _, okType := range allowedTypes {
		tx := Transaction{
			Sender:    addrA,
			SenderPub: pubA,
			Recipient: addrB,
			Amount:    10 * CoinUnit,
			Fee:       MinRelayFee,
			Nonce:     1,
			Type:      okType,
		}
		if err := tx.Sign(privA); err != nil {
			t.Fatalf("signing failed: %v", err)
		}

		if !tx.Verify() {
			t.Errorf("tx.Verify() should ACCEPT valid transaction type '%s', but it rejected it!", okType)
		}
	}
}
