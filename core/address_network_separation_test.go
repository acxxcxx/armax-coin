//go:build testhooks

package core

import (
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestAddress_HRPAndBech32Validation verifies Section C.1 & C.2:
// - HRP enforcement ('armx' only)
// - Mixed-case/uppercase rejection
// - Checksum typo detection
// - Cross-HRP rejection
func TestAddress_HRPAndBech32Validation(t *testing.T) {
	sk, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	validAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	// 1. Valid address must pass ValidateAddress
	if !ValidateAddress(validAddr) {
		t.Fatalf("Expected valid address %s to pass ValidateAddress", validAddr)
	}

	// 2. Uppercase and mixed-case must be REJECTED [C-20]
	upperAddr := strings.ToUpper(validAddr)
	if ValidateAddress(upperAddr) {
		t.Fatalf("ValidateAddress accepted uppercase address: %s", upperAddr)
	}
	mixedAddr := "Armx1" + validAddr[5:]
	if ValidateAddress(mixedAddr) {
		t.Fatalf("ValidateAddress accepted mixed-case address: %s", mixedAddr)
	}

	// 3. Typo/checksum error must be REJECTED
	corruptChar := byte('q')
	if validAddr[len(validAddr)-1] == 'q' {
		corruptChar = 'p'
	}
	corruptAddr := validAddr[:len(validAddr)-1] + string(corruptChar)
	if ValidateAddress(corruptAddr) {
		t.Fatalf("ValidateAddress accepted address with corrupted checksum: %s", corruptAddr)
	}

	// 4. Non-'armx' HRP (e.g. testnet HRP 'tarmx' or bitcoin 'bc') must be REJECTED by canonical validator
	hrp, payload, err := Bech32Decode(validAddr)
	if err != nil || hrp != Bech32HRP {
		t.Fatalf("Bech32Decode failed: %v", err)
	}
	tarmxAddr, err := Bech32Encode("tarmx", payload)
	if err != nil {
		t.Fatalf("Bech32Encode with tarmx failed: %v", err)
	}
	if ValidateAddress(tarmxAddr) {
		t.Fatalf("ValidateAddress accepted non-armx HRP address: %s", tarmxAddr)
	}
	t.Logf("[OK] Bech32 HRP enforcement and typo rejection verified for: %s", validAddr)
}

// TestTransaction_CrossNetworkReplayProtection verifies Section C.1 & C.2:
// A transaction created and signed on TESTNET (netID = ARMX-TESTNET-1) commits to
// ARMX-TESTNET-1 in its SHA256d hash. When presented to a MAINNET node (netID = ARMX-MAINNET-1),
// the transaction hash recalculates with ARMX-MAINNET-1, causing tx.Verify() to FAIL.
func TestTransaction_CrossNetworkReplayProtection(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("mainnet")
	}()

	// Initialize keypair
	sk, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	recipientAddr := GenesisMiner

	// 1. Create transaction on TESTNET
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}
	if NetworkID() != "ARMX-TESTNET-1" {
		t.Fatalf("Expected NetworkID ARMX-TESTNET-1, got %s", NetworkID())
	}

	tx := Transaction{
		Sender:    senderAddr,
		Recipient: recipientAddr,
		Amount:    500000000,
		Fee:       1000000,
		Nonce:     1,
	}

	// Sign transaction under TESTNET domain
	if err := tx.Sign(sk); err != nil {
		t.Fatalf("tx.Sign failed on testnet: %v", err)
	}

	// Verify passes on Testnet
	if !tx.Verify() {
		t.Fatalf("tx.Verify failed on legitimate testnet environment")
	}
	t.Logf("[OK] Transaction successfully signed and verified on TESTNET (Hash: %s)", tx.HashHex())

	// 2. Replay transaction on MAINNET
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}
	if NetworkID() != "ARMX-MAINNET-1" {
		t.Fatalf("Expected NetworkID ARMX-MAINNET-1, got %s", NetworkID())
	}

	// Under Mainnet rules, tx.Hash() recalculates with "ARMX-MAINNET-1",
	// so the signature over "ARMX-TESTNET-1" MUST be invalid!
	if tx.Verify() {
		t.Fatalf("CRITICAL SECURITY FLAW: Testnet transaction was accepted as valid on Mainnet!")
	}
	t.Logf("[OK] Mainnet node strictly rejected Testnet transaction replay (Domain separation verified)")
}
