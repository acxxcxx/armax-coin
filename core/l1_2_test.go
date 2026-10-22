//go:build testhooks

package core

import (
	"strings"
	"testing"
)

// TestL1_2_DynamicNetworkID_TxCrossNetworkRejection verifies:
// 1. Identical tx data produces different tx.Hash() across mainnet vs testnet
// 2. Tx signed in testnet is rejected by Verify() in mainnet and vice-versa
// 3. SetNetwork cannot be called twice in non-testing mode
func TestL1_2_DynamicNetworkID_TxCrossNetworkRejection(t *testing.T) {
	// Enable test override mode
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)

	// Step 1: Set to testnet
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet: %v", err)
	}
	if NetworkID() != "ARMX-TESTNET-1" {
		t.Fatalf("Expected ARMX-TESTNET-1, got %s", NetworkID())
	}

	acc := newTestAccount(t)
	txTestnet := Transaction{
		SigAlg:    1,
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		Amount:    1000000,
		Fee:       12000,
		Nonce:     1,
	}
	if err := txTestnet.Sign(acc.PrivKey); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	hashTestnet := txTestnet.HashHex()

	// Verify valid in testnet
	if err := txTestnet.VerifyWithError(); err != nil {
		t.Fatalf("txTestnet should be valid in testnet: %v", err)
	}

	// Step 2: Switch to mainnet
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet: %v", err)
	}
	if NetworkID() != "ARMX-MAINNET-1" {
		t.Fatalf("Expected ARMX-MAINNET-1, got %s", NetworkID())
	}

	hashMainnet := txTestnet.HashHex()
	if hashTestnet == hashMainnet {
		t.Fatalf("Identical tx data MUST produce different tx.Hash() across networks! got same: %s", hashTestnet)
	}

	// Step 3: Verify tx signed on testnet is REJECTED in mainnet
	errVerifyMainnet := txTestnet.VerifyWithError()
	if errVerifyMainnet == nil {
		t.Fatalf("Transaction signed on testnet MUST be rejected in mainnet!")
	}
	if !strings.Contains(errVerifyMainnet.Error(), "signature verification failed") {
		t.Fatalf("Expected signature verification failure error, got: %v", errVerifyMainnet)
	}

	// Step 4: Sign on mainnet, verify in mainnet, then switch to testnet and verify rejected
	txMainnet := Transaction{
		SigAlg:    1,
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		Amount:    1000000,
		Fee:       12000,
		Nonce:     1,
	}
	if err := txMainnet.Sign(acc.PrivKey); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if err := txMainnet.VerifyWithError(); err != nil {
		t.Fatalf("txMainnet should be valid in mainnet: %v", err)
	}

	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet: %v", err)
	}
	errVerifyTestnet := txMainnet.VerifyWithError()
	if errVerifyTestnet == nil {
		t.Fatalf("Transaction signed on mainnet MUST be rejected in testnet!")
	}
	if !strings.Contains(errVerifyTestnet.Error(), "signature verification failed") {
		t.Fatalf("Expected signature verification failure error, got: %v", errVerifyTestnet)
	}

	// Step 5: Test SetNetwork once-only enforcement when SetNetworkForTesting(false)
	SetNetworkForTesting(false)
	errSecond := SetNetwork("mainnet")
	if errSecond == nil {
		t.Fatalf("Expected second call to SetNetwork to fail when testing override is disabled!")
	}
	if !strings.Contains(errSecond.Error(), "already initialized") {
		t.Fatalf("Expected 'already initialized' error message, got: %v", errSecond)
	}
}
