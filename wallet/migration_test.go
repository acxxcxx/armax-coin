//go:build testhooks

package wallet

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestLegacyWalletMigration(t *testing.T) {
	tempDir := t.TempDir()
	walletPath := filepath.Join(tempDir, "wallet.json")

	// 1. Create a wallet encrypted with legacy default passphrase
	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	legacyPass := GetLegacyDefaultPassphrase()
	if err := EncryptAndSaveKeystore(w, legacyPass, walletPath); err != nil {
		t.Fatalf("Failed to save keystore: %v", err)
	}

	// 2. Detect legacy passphrase usage
	if !IsKeystoreUsingLegacyPassphrase(walletPath) {
		t.Fatalf("Expected IsKeystoreUsingLegacyPassphrase to return true")
	}

	// 3. Reject migration to short passphrase (< 12 chars)
	_, err = MigrateKeystorePassphrase(walletPath, legacyPass, "shortpass")
	if err == nil {
		t.Fatalf("Expected error for short passphrase, got nil")
	}

	// 4. Reject migration to legacy passphrase itself
	_, err = MigrateKeystorePassphrase(walletPath, legacyPass, legacyPass)
	if err == nil {
		t.Fatalf("Expected error when migrating to legacy passphrase, got nil")
	}

	// 5. Successful migration to strong passphrase (>= 12 chars)
	newPass := "SuperStrongPassphrase2026!"
	migratedW, err := MigrateKeystorePassphrase(walletPath, legacyPass, newPass)
	if err != nil {
		t.Fatalf("MigrateKeystorePassphrase failed: %v", err)
	}
	if migratedW.Address != w.Address {
		t.Fatalf("Address mismatch after migration: %s vs %s", migratedW.Address, w.Address)
	}

	// 6. Keystore can no longer be unlocked with legacy passphrase
	if IsKeystoreUsingLegacyPassphrase(walletPath) {
		t.Fatalf("Keystore should no longer be unlockable with legacy passphrase")
	}

	// 7. Keystore unlocks with new passphrase
	unlockedW, err := DecryptAndLoadKeystore(newPass, walletPath)
	if err != nil {
		t.Fatalf("Failed to unlock keystore with new passphrase: %v", err)
	}
	if unlockedW.Address != w.Address {
		t.Fatalf("Address mismatch after unlock: %s vs %s", unlockedW.Address, w.Address)
	}
}

func TestMnemonicVerification(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	indices := [3]int{1, 12, 24}
	answers := [3]string{"abandon", "abandon", "art"}

	if !VerifyMnemonicWords(mnemonic, indices, answers) {
		t.Fatalf("VerifyMnemonicWords failed with valid answers")
	}

	wrongAnswers := [3]string{"abandon", "abandon", "wrong"}
	if VerifyMnemonicWords(mnemonic, indices, wrongAnswers) {
		t.Fatalf("VerifyMnemonicWords should fail with incorrect answer")
	}

	// Test PickRandomWordIndices produces 3 distinct in-bounds indices
	randIndices := PickRandomWordIndices()
	if randIndices[0] < 1 || randIndices[2] > 24 {
		t.Fatalf("Indices out of bounds: %v", randIndices)
	}
	if randIndices[0] >= randIndices[1] || randIndices[1] >= randIndices[2] {
		t.Fatalf("Indices not strictly ascending and unique: %v", randIndices)
	}
}

func TestAutoFeeMempoolAcceptance(t *testing.T) {
	// Initialize in-memory blockchain
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)
	_ = core.SetNetwork("mainnet")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	w := MustNewWallet()
	recipient := MustNewWallet().Address

	// Fund sender in local state
	senderHex, _ := core.AddressToHex(w.Address)
	bc.Lock.Lock()
	bc.State.Accounts[senderHex] = core.AccountState{
		Balance: 100 * core.CoinUnit,
		Nonce:   0,
	}
	bc.Lock.Unlock()

	// 1. Transaction with fee = 1000 (MinRelayFee is 1000/KB, but ML-DSA-65 signed tx is ~5.4KB, requiring 6000 Armoids)
	txSmallFee := core.Transaction{
		Sender:    w.Address,
		SenderPub: w.PublicKey,
		Recipient: recipient,
		Amount:    1 * core.CoinUnit,
		Fee:       1000,
		Nonce:     1,
	}
	if err := txSmallFee.Sign(w.PrivateKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}

	ok, errMsg := bc.AddTransaction(txSmallFee)
	if ok {
		t.Fatalf("Expected transaction with fee 1000 to be rejected, but it was accepted!")
	}
	if !strings.Contains(errMsg, "below required relay fee") {
		t.Fatalf("Expected error message to contain 'below required relay fee', got: '%s'", errMsg)
	}

	// 2. Transaction with automatic fee: CalculateMinMempoolFee(tx.Size())
	autoFee := core.CalculateMinMempoolFee(txSmallFee.Size())
	if autoFee <= 1000 {
		t.Fatalf("Expected auto fee for ML-DSA-65 tx to be > 1000 Armoids, got %d", autoFee)
	}

	txAutoFee := core.Transaction{
		Sender:    w.Address,
		SenderPub: w.PublicKey,
		Recipient: recipient,
		Amount:    1 * core.CoinUnit,
		Fee:       autoFee,
		Nonce:     1,
	}
	if err := txAutoFee.Sign(w.PrivateKey); err != nil {
		t.Fatalf("Signing failed: %v", err)
	}

	okAuto, errMsgAuto := bc.AddTransaction(txAutoFee)
	if !okAuto {
		t.Fatalf("Transaction with auto fee (%d Armoids) was rejected: %s", autoFee, errMsgAuto)
	}
}
