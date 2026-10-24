package wallet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT15_WalletCreationCreatesBackupOnExistingFile verifies that creating or restoring
// a wallet over an existing wallet file creates an automatic backup without data loss [T15].
func TestT15_WalletCreationCreatesBackupOnExistingFile(t *testing.T) {
	tempDir := t.TempDir()
	walletPath := filepath.Join(tempDir, "wallet.json")

	// 1. Initial wallet creation
	w1, err := NewWallet()
	if err != nil {
		t.Fatalf("Failed to create wallet 1: %v", err)
	}
	pass1 := "initial-secret-pass"
	bak, err := CreateNewWalletKeystore(w1, pass1, walletPath, false)
	if err != nil {
		t.Fatalf("CreateNewWalletKeystore failed: %v", err)
	}
	if bak != "" {
		t.Fatalf("Expected no backup on fresh creation, got: %s", bak)
	}

	// 2. Overwrite attempt without force must create automatic backup
	w2, err := NewWallet()
	if err != nil {
		t.Fatalf("Failed to create wallet 2: %v", err)
	}
	pass2 := "second-secret-pass"
	bakPath, err := CreateNewWalletKeystore(w2, pass2, walletPath, false)
	if err != nil {
		t.Fatalf("CreateNewWalletKeystore failed on existing file: %v", err)
	}
	if bakPath == "" || !strings.Contains(bakPath, ".backup.") {
		t.Fatalf("Expected backup file path containing '.backup.', got: %s", bakPath)
	}

	// Verify backup file exists and decrypts to wallet 1
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("Backup file does not exist: %v", err)
	}
	reloadedOriginal, err := DecryptAndLoadKeystore(pass1, bakPath)
	if err != nil {
		t.Fatalf("Failed to decrypt backup keystore: %v", err)
	}
	if reloadedOriginal.Address != w1.Address {
		t.Fatalf("Backup address mismatch: expected %s, got %s", w1.Address, reloadedOriginal.Address)
	}

	// Verify active wallet file now decrypts to wallet 2
	reloadedNew, err := DecryptAndLoadKeystore(pass2, walletPath)
	if err != nil {
		t.Fatalf("Failed to decrypt new keystore: %v", err)
	}
	if reloadedNew.Address != w2.Address {
		t.Fatalf("New wallet address mismatch: expected %s, got %s", w2.Address, reloadedNew.Address)
	}
}
