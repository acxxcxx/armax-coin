package core

import (
	"strings"
	"testing"
)

// TestP2_4_ConstantsInvariants verifies P2-4 size limits and dynamic mempool fee policy formula
func TestP2_4_ConstantsInvariants(t *testing.T) {
	if MaxTxSize != 20_000 {
		t.Fatalf("Expected MaxTxSize = 20_000 B, got %d", MaxTxSize)
	}
	if MaxBlockSize != 2_000_000 {
		t.Fatalf("Expected MaxBlockSize = 2_000_000 B, got %d", MaxBlockSize)
	}

	// Dynamic mempool fee test: MinRelayFee * ceil(size/1000)
	testCases := []struct {
		size        int
		expectedFee int64
	}{
		{500, MinRelayFee * 1},
		{1000, MinRelayFee * 1},
		{1001, MinRelayFee * 2},
		{2000, MinRelayFee * 2},
		{5500, MinRelayFee * 6},
		{19999, MinRelayFee * 20},
		{20000, MinRelayFee * 20},
	}

	for _, tc := range testCases {
		calcFee := CalculateMinMempoolFee(tc.size)
		if calcFee != tc.expectedFee {
			t.Errorf("CalculateMinMempoolFee(%d): expected %d, got %d", tc.size, tc.expectedFee, calcFee)
		}
	}
}

// TestP2_4_MempoolFeePolicyEnforcement verifies that transactions below dynamic mempool fee are rejected
func TestP2_4_MempoolFeePolicyEnforcement(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	accSender := newTestAccount(t, "sender")
	privSender, pubSender, senderAddr := accSender.PrivKey, accSender.PubHex, accSender.Address
	recipAddr := newTestAccount(t, "recip").Address

	senderHex, _ := AddressToHex(senderAddr)
	bc.State.Lock.Lock()
	bc.State.Accounts[senderHex] = AccountState{
		Balance: 100 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	// 1. Transaction with fee < dynamic minimum fee (for ~5.5KB tx, min fee is 6000 Armoids)
	txLowFee := Transaction{
		Sender:    senderAddr,
		SenderPub: pubSender,
		Recipient: recipAddr,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee, // 1000 < ~6000 required
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := txLowFee.Sign(privSender); err != nil {
		t.Fatalf("txLowFee.Sign failed: %v", err)
	}

	accepted, reason := bc.AddTransaction(txLowFee)
	if accepted {
		t.Fatalf("AddTransaction should have rejected tx with fee %d below required dynamic mempool fee (%d)", txLowFee.Fee, CalculateMinMempoolFee(txLowFee.Size()))
	}
	if !strings.Contains(reason, "below required relay fee") {
		t.Fatalf("Expected 'below required relay fee' reason substring, got: %s", reason)
	}

	// 2. Transaction with fee >= dynamic minimum fee
	txValidFee := Transaction{
		Sender:    senderAddr,
		SenderPub: pubSender,
		Recipient: recipAddr,
		Amount:    10 * CoinUnit,
		Fee:       CalculateMinMempoolFee(txLowFee.Size()),
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := txValidFee.Sign(privSender); err != nil {
		t.Fatalf("txValidFee.Sign failed: %v", err)
	}

	acceptedValid, reasonValid := bc.AddTransaction(txValidFee)
	if !acceptedValid {
		t.Fatalf("AddTransaction rejected valid dynamic fee tx: %s", reasonValid)
	}
}
