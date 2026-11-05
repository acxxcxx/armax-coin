package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// TestArmwalletVersionFlag verifies that armwallet CLI responds to -version
func TestArmwalletVersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armwallet v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}

// TestArmwalletShowAddress verifies that showAddress correctly inspects a keystore file
func TestArmwalletShowAddress(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "test_wallet.json")

	dummyKS := wallet.KeystoreFile{
		Version:   2,
		Address:   "armx17nqa4uyewe8elsn4w8qfygjv7aafkkzj25r8antzdmecwnhpd28sayl2yl",
		PublicKey: strings.Repeat("a", 3904),
	}

	data, err := json.Marshal(dummyKS)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(ksPath, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify showAddress execution does not panic
	showAddress(ksPath)
}

// TestArmwalletRestoreWarning verifies that passing mnemonic via -restore emits a warning
func TestArmwalletRestoreWarning(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "restore_test.json")

	// 24 valid BIP-39 words for test
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	cmd := exec.Command("go", "run", "main.go", "-restore", mnemonic, "-file", ksPath)
	cmd.Env = append(os.Environ(), "ARMX_WALLET_PASSPHRASE=testpassword123")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -restore failed: %v, output: %s", err, string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "WARNING: Passing mnemonic phrase via -restore flag leaks secret keys") {
		t.Errorf("Expected warning about process table leakage, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "[OK] Wallet successfully restored") {
		t.Errorf("Expected wallet successfully restored, got:\n%s", outStr)
	}
}

// TestArmwalletRestoreInteractive verifies restoring wallet via interactive stdin
func TestArmwalletRestoreInteractive(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "restore_interactive.json")

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	cmd := exec.Command("go", "run", "main.go", "-restore-interactive", "-file", ksPath)
	cmd.Env = append(os.Environ(), "ARMX_WALLET_PASSPHRASE=testpassword123")
	cmd.Stdin = strings.NewReader(mnemonic + "\n")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -restore-interactive failed: %v, output: %s", err, string(out))
	}

	outStr := string(out)
	if strings.Contains(outStr, "WARNING: Passing mnemonic phrase via -restore flag") {
		t.Errorf("Unexpected flag warning for interactive restore: %s", outStr)
	}
	if !strings.Contains(outStr, "[OK] Wallet successfully restored") {
		t.Errorf("Expected wallet successfully restored, got:\n%s", outStr)
	}
}
