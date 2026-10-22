package wallet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestWallet_DeterministicMnemonicDerivation tests 24-word and 12-word BIP-39 mnemonic derivation
func TestWallet_DeterministicMnemonicDerivation(t *testing.T) {
	// Standard 24-word test vector
	mnemonic24 := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	w1, err := WalletFromMnemonic(mnemonic24, "", 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic (24-word) failed: %v", err)
	}

	w2, err := WalletFromMnemonic(mnemonic24, "", 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic second run failed: %v", err)
	}

	if w1.Address != w2.Address {
		t.Fatalf("Deterministic derivation failed: w1 addr %s != w2 addr %s", w1.Address, w2.Address)
	}
	if w1.PublicKey != w2.PublicKey {
		t.Fatalf("Deterministic public key mismatch: %s vs %s", w1.PublicKey, w2.PublicKey)
	}
	if !core.ValidateAddress(w1.Address) {
		t.Fatalf("Derived address failed core.ValidateAddress: %s", w1.Address)
	}

	// 12-word test vector (accepted with warning)
	mnemonic12 := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	w12, err := WalletFromMnemonic(mnemonic12, "", 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic (12-word) failed: %v", err)
	}
	if !core.ValidateAddress(w12.Address) {
		t.Fatalf("Derived 12-word address failed core.ValidateAddress: %s", w12.Address)
	}

	// Different account indices produce different addresses
	wAcc1, err := WalletFromMnemonic(mnemonic24, "", 1)
	if err != nil {
		t.Fatalf("WalletFromMnemonic account 1 failed: %v", err)
	}
	if w1.Address == wAcc1.Address {
		t.Fatalf("Account index 0 and 1 must produce different addresses!")
	}
}

// TestWallet_KeystoreEncryptionArgon2id tests encrypted keystore save and load with correct/wrong passphrase
func TestWallet_KeystoreEncryptionArgon2id(t *testing.T) {
	w, err := NewWallet()
	if err != nil || w == nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	tmpDir := t.TempDir()
	ksPath := filepath.Join(tmpDir, "keystore.json")
	passphrase := "StrongQuantumPassphrase!2026"

	// 1. Encrypt and save
	if err := EncryptAndSaveKeystore(w, passphrase, ksPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore failed: %v", err)
	}

	// Verify plaintext private key or seed does not exist in saved JSON
	data, err := os.ReadFile(ksPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if strings.Contains(string(data), "private_key") {
		t.Fatalf("Keystore must NOT contain plaintext private_key field!")
	}
	if strings.Contains(string(data), string(w.Seed32[:])) {
		t.Fatalf("Keystore must NOT contain raw seed bytes in plaintext!")
	}

	// 2. Load with correct passphrase
	loaded, err := DecryptAndLoadKeystore(passphrase, ksPath)
	if err != nil {
		t.Fatalf("DecryptAndLoadKeystore failed with correct passphrase: %v", err)
	}
	if loaded.Address != w.Address {
		t.Fatalf("Address mismatch after decrypt: expected %s, got %s", w.Address, loaded.Address)
	}
	if loaded.PublicKey != w.PublicKey {
		t.Fatalf("PublicKey mismatch after decrypt: expected %s, got %s", w.PublicKey, loaded.PublicKey)
	}

	// 3. Load with wrong passphrase must fail
	_, errWrong := DecryptAndLoadKeystore("WrongPassword123!", ksPath)
	if errWrong == nil {
		t.Fatalf("Expected decryption failure with wrong passphrase, but it succeeded!")
	}
}

// TestWallet_SignAndCoreVerify tests that a transaction signed with wallet private key passes core.Verify()
func TestWallet_SignAndCoreVerify(t *testing.T) {
	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	recip := core.GenesisMiner

	tx := core.Transaction{
		Sender:    w.Address,
		SenderPub: w.PublicKey,
		Recipient: recip,
		Amount:    100000000,
		Fee:       1000,
		Nonce:     1,
	}

	if err := tx.Sign(w.PrivateKey); err != nil {
		t.Fatalf("tx.Sign with wallet.PrivateKey failed: %v", err)
	}

	if tx.SigAlg != pqsig.SigAlgMLDSA65 {
		t.Fatalf("Expected tx.SigAlg to be ML-DSA-65 (0x01), got %d", tx.SigAlg)
	}
	if len(tx.SenderPub) != 2*pqsig.PublicKeySize {
		t.Fatalf("Expected SenderPub len %d, got %d", 2*pqsig.PublicKeySize, len(tx.SenderPub))
	}
	if len(tx.Signature) != 2*pqsig.SignatureSize {
		t.Fatalf("Expected Signature len %d, got %d", 2*pqsig.SignatureSize, len(tx.Signature))
	}

	if !tx.Verify() {
		t.Fatalf("tx.Verify() failed on transaction signed by wallet")
	}
}
