package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_DefaultWalletNetworkPath verifies that 'armwallet -create' without -file/-wallet
// creates the wallet in <ARMX_DATA_DIR>/<network>/wallet.json.
func TestCLI_DefaultWalletNetworkPath(t *testing.T) {
	tempDir := t.TempDir()
	cmd := exec.Command("go", "run", "main.go", "-network", "testnet", "-create")
	cmd.Env = append(os.Environ(),
		"ARMX_DATA_DIR="+tempDir,
		"ARMX_WALLET_PASSPHRASE=testpassword123",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -create failed: %v, output: %s", err, string(out))
	}

	expectedWallet := filepath.Join(tempDir, "testnet", "wallet.json")
	if _, err := os.Stat(expectedWallet); os.IsNotExist(err) {
		t.Fatalf("expected wallet to be created at %s, but file does not exist. Output:\n%s", expectedWallet, string(out))
	}

	// Verify root wallet.json was NOT created
	rootWallet := filepath.Join(tempDir, "wallet.json")
	if _, err := os.Stat(rootWallet); err == nil {
		t.Fatalf("root wallet.json was unexpectedly created at %s", rootWallet)
	}
}

// TestCLI_ExplicitFileFlagOverridesNetworkDefault verifies that explicit -file overrides
// the network default path without prepending a network subdirectory.
func TestCLI_ExplicitFileFlagOverridesNetworkDefault(t *testing.T) {
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "custom_named_wallet.json")

	cmd := exec.Command("go", "run", "main.go", "-network", "testnet", "-file", customPath, "-create")
	cmd.Env = append(os.Environ(),
		"ARMX_DATA_DIR="+tempDir,
		"ARMX_WALLET_PASSPHRASE=testpassword123",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -file failed: %v, output: %s", err, string(out))
	}

	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Fatalf("expected wallet to be created at explicit path %s, but does not exist. Output:\n%s", customPath, string(out))
	}

	// Verify testnet/wallet.json was NOT created
	testnetWallet := filepath.Join(tempDir, "testnet", "wallet.json")
	if _, err := os.Stat(testnetWallet); err == nil {
		t.Fatalf("testnet/wallet.json was unexpectedly created when explicit -file was specified")
	}
}

// TestCLI_ExplicitWalletFlagAlias verifies that deprecated alias -wallet functions
// identically to -file and overrides the network default path.
func TestCLI_ExplicitWalletFlagAlias(t *testing.T) {
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "alias_named_wallet.json")

	cmd := exec.Command("go", "run", "main.go", "-network", "testnet", "-wallet", customPath, "-create")
	cmd.Env = append(os.Environ(),
		"ARMX_DATA_DIR="+tempDir,
		"ARMX_WALLET_PASSPHRASE=testpassword123",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -wallet failed: %v, output: %s", err, string(out))
	}

	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Fatalf("expected wallet to be created at explicit alias path %s. Output:\n%s", customPath, string(out))
	}
}

// TestCLI_LocalMainnetUsesMainnetWallet verifies that -local-mainnet isolates into mainnet/wallet.json.
func TestCLI_LocalMainnetUsesMainnetWallet(t *testing.T) {
	tempDir := t.TempDir()
	cmd := exec.Command("go", "run", "main.go", "-local-mainnet", "-create")
	cmd.Env = append(os.Environ(),
		"ARMX_DATA_DIR="+tempDir,
		"ARMX_WALLET_PASSPHRASE=testpassword123",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -local-mainnet -create failed: %v, output: %s", err, string(out))
	}

	expectedWallet := filepath.Join(tempDir, "mainnet", "wallet.json")
	if _, err := os.Stat(expectedWallet); os.IsNotExist(err) {
		t.Fatalf("expected local-mainnet wallet at %s, but file does not exist. Output:\n%s", expectedWallet, string(out))
	}

	// Verify testnet wallet was NOT touched
	testnetWallet := filepath.Join(tempDir, "testnet", "wallet.json")
	if _, err := os.Stat(testnetWallet); err == nil {
		t.Fatalf("testnet wallet was unexpectedly created during local-mainnet wallet creation")
	}
}

// TestCLI_LegacyWalletNoticePreserved verifies that a legacy wallet in root data dir
// triggers a notice and is never modified, moved, or deleted by CLI.
func TestCLI_LegacyWalletNoticePreserved(t *testing.T) {
	tempDir := t.TempDir()
	legacyWallet := filepath.Join(tempDir, "wallet.json")
	legacyContent := []byte("SIMULATED_ROOT_LEGACY_WALLET_PRESERVE_UNTOUCHED")
	if err := os.WriteFile(legacyWallet, legacyContent, 0600); err != nil {
		t.Fatalf("failed to write simulated legacy wallet: %v", err)
	}
	initialHash := sha256.Sum256(legacyContent)

	cmd := exec.Command("go", "run", "main.go", "-network", "testnet", "-create")
	cmd.Env = append(os.Environ(),
		"ARMX_DATA_DIR="+tempDir,
		"ARMX_WALLET_PASSPHRASE=testpassword123",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -create failed: %v, output: %s", err, string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "[!] Notice: Found legacy wallet at") {
		t.Fatalf("expected legacy notice in output, got:\n%s", outStr)
	}

	// Verify legacy file was NOT altered
	afterData, err := os.ReadFile(legacyWallet)
	if err != nil {
		t.Fatalf("legacy wallet file was removed or moved: %v", err)
	}
	if sha256.Sum256(afterData) != initialHash {
		t.Fatalf("legacy wallet file content was modified by CLI run!")
	}
}
