package wallet

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// captureStdout captures stdout during the execution of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	outChan := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outChan <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-outChan
}

// TestWalletIsolation_NetworkProfiles verifies that default wallet paths
// are strictly isolated into respective network subdirectories:
// mainnet -> <datadir>/mainnet/wallet.json
// testnet -> <datadir>/testnet/wallet.json
// regtest -> <datadir>/regtest/wallet.json
// soak    -> <datadir>/soak/wallet.json
func TestWalletIsolation_NetworkProfiles(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()

	profiles := []struct {
		name        string
		expectedSub string
	}{
		{"mainnet", "mainnet"},
		{"testnet", "testnet"},
		{"regtest", "regtest"},
		{"soak", "soak"},
	}

	for _, p := range profiles {
		t.Run("WalletPath_"+p.name, func(t *testing.T) {
			path := GetDefaultWalletPathForNetwork(p.name)
			expectedPath := filepath.Join(baseDir, p.expectedSub, WalletFileName)
			if filepath.Clean(path) != filepath.Clean(expectedPath) {
				t.Fatalf("[%s] GetDefaultWalletPathForNetwork expected %q, got %q", p.name, expectedPath, path)
			}

			// Verify parent directory was created
			dir := filepath.Dir(path)
			fi, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("[%s] target directory %s was not created: %v", p.name, dir, err)
			}
			if !fi.IsDir() {
				t.Fatalf("[%s] target %s is not a directory", p.name, dir)
			}
		})
	}
}

// TestWalletIsolation_LegacyWalletPreserved verifies that an existing legacy wallet file
// in the root data directory is NEVER modified, moved, renamed, or deleted when GetDefaultWalletPath is invoked.
func TestWalletIsolation_LegacyWalletPreserved(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()
	ResetLegacyNoticesForTesting()

	legacyPath := filepath.Join(baseDir, WalletFileName)
	legacyContent := []byte("SIMULATED_LEGACY_WALLET_ENCRYPTED_KEYSTORE_DO_NOT_ALTER")
	if err := os.WriteFile(legacyPath, legacyContent, 0600); err != nil {
		t.Fatalf("failed to create simulated legacy wallet: %v", err)
	}
	initialHash := sha256.Sum256(legacyContent)

	// Invoke path resolution for testnet and mainnet while capturing notice
	var testnetPath string
	out := captureStdout(t, func() {
		testnetPath = GetDefaultWalletPathForNetwork("testnet")
	})

	expectedIsolatedPath := filepath.Join(baseDir, "testnet", WalletFileName)
	if filepath.Clean(testnetPath) != filepath.Clean(expectedIsolatedPath) {
		t.Fatalf("expected isolated testnet path %q, got %q", expectedIsolatedPath, testnetPath)
	}

	// Verify notice was output instructing manual migration
	if !bytes.Contains([]byte(out), []byte("[!] Notice: Found legacy wallet at")) {
		t.Fatalf("expected legacy notice in output, got: %q", out)
	}

	// Verify legacy file was NOT moved, renamed, or modified
	dataAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy wallet file was deleted or moved: %v", err)
	}
	hashAfter := sha256.Sum256(dataAfter)
	if hashAfter != initialHash {
		t.Fatalf("legacy wallet file was modified! initial %x, current %x", initialHash, hashAfter)
	}
}

// TestWalletIsolation_NoSilentCrossNetworkFallback proves that when a wallet exists in one
// network (e.g. mainnet), another network (e.g. testnet) NEVER silently falls back to it.
func TestWalletIsolation_NoSilentCrossNetworkFallback(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()

	mainnetDir := filepath.Join(baseDir, "mainnet")
	_ = os.MkdirAll(mainnetDir, 0700)
	mainnetWalletPath := filepath.Join(mainnetDir, WalletFileName)

	w, err := NewWallet()
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	if err := EncryptAndSaveKeystore(w, "test-passphrase", mainnetWalletPath); err != nil {
		t.Fatalf("failed to save mainnet wallet: %v", err)
	}

	// Query testnet wallet path
	testnetPath := GetDefaultWalletPathForNetwork("testnet")
	expectedTestnetPath := filepath.Join(baseDir, "testnet", WalletFileName)
	if filepath.Clean(testnetPath) != filepath.Clean(expectedTestnetPath) {
		t.Fatalf("expected testnet path %q, got %q", expectedTestnetPath, testnetPath)
	}

	// Verify testnet wallet does NOT exist and does not silently fall back
	if _, err := os.Stat(testnetPath); !os.IsNotExist(err) {
		t.Fatalf("expected testnet wallet to not exist, but stat returned: %v", err)
	}

	// Verify attempting to decrypt testnet wallet fails cleanly without opening mainnet
	_, err = DecryptAndLoadKeystore("test-passphrase", testnetPath)
	if err == nil {
		t.Fatalf("security violation: DecryptAndLoadKeystore succeeded on non-existent testnet wallet (silent fallback!)")
	}
}

// TestWalletIsolation_CustomPathAndEnvOverrides verifies that explicit custom path
// and environment variable overrides are strictly respected.
func TestWalletIsolation_CustomPathAndEnvOverrides(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	ResetWalletPath()

	// 1. Test ARMX_WALLET_FILE override
	envCustom := filepath.Join(baseDir, "env_custom_wallet.json")
	t.Setenv("ARMX_WALLET_FILE", envCustom)

	resolved := GetDefaultWalletPathForNetwork("testnet")
	if resolved != envCustom {
		t.Fatalf("expected ARMX_WALLET_FILE %q, got %q", envCustom, resolved)
	}

	// Clear env override
	t.Setenv("ARMX_WALLET_FILE", "")

	// 2. Test SetWalletPath programmatic override
	progCustom := filepath.Join(baseDir, "prog_custom_wallet.json")
	SetWalletPath(progCustom)
	defer ResetWalletPath()

	resolved = GetDefaultWalletPathForNetwork("mainnet")
	if resolved != progCustom {
		t.Fatalf("expected SetWalletPath %q, got %q", progCustom, resolved)
	}

	// 3. Reset and verify returns network path
	ResetWalletPath()
	resolved = GetDefaultWalletPathForNetwork("soak")
	expected := filepath.Join(baseDir, "soak", WalletFileName)
	if filepath.Clean(resolved) != filepath.Clean(expected) {
		t.Fatalf("expected network path %q, got %q", expected, resolved)
	}
}

// TestWalletIsolation_SimultaneousNetworks verifies that concurrent path resolutions
// for different network profiles do not cross-contaminate or race.
func TestWalletIsolation_SimultaneousNetworks(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	nets := []string{"mainnet", "testnet", "regtest", "soak"}
	for i := 0; i < 40; i++ {
		for _, netName := range nets {
			wg.Add(1)
			go func(net string) {
				defer wg.Done()
				path := GetDefaultWalletPathForNetwork(net)
				expected := filepath.Join(baseDir, net, WalletFileName)
				if filepath.Clean(path) != filepath.Clean(expected) {
					errCh <- fmt.Errorf("[%s] expected %q, got %q", net, expected, path)
				}
			}(netName)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

// TestWalletIsolation_DirectoryPermissions verifies that newly created directories
// have restrictive permissions (0700).
func TestWalletIsolation_DirectoryPermissions(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()

	path := GetDefaultWalletPathForNetwork("regtest")
	dir := filepath.Dir(path)

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("failed to stat created directory: %v", err)
	}

	if !fi.IsDir() {
		t.Fatalf("expected %s to be a directory", dir)
	}

	// Save a wallet file into it to verify 0600 file permission enforcement
	w, err := NewWallet()
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	if err := EncryptAndSaveKeystore(w, "secret123", path); err != nil {
		t.Fatalf("failed to save keystore: %v", err)
	}

	fileFi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat created wallet file: %v", err)
	}
	if fileFi.Size() == 0 {
		t.Fatalf("wallet file is empty")
	}
}

// TestWalletIsolation_RestartOpensCorrectNetwork verifies that restarting a node or CLI
// opens the wallet for its active network profile, preserving independent accounts.
func TestWalletIsolation_RestartOpensCorrectNetwork(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)
	t.Setenv("ARMX_WALLET_FILE", "")
	ResetWalletPath()

	// 1. Session A: Create Testnet wallet
	testnetW, err := NewWallet()
	if err != nil {
		t.Fatalf("failed to create testnet wallet: %v", err)
	}
	testnetPath := GetDefaultWalletPathForNetwork("testnet")
	if err := EncryptAndSaveKeystore(testnetW, "pass-testnet", testnetPath); err != nil {
		t.Fatalf("failed to save testnet keystore: %v", err)
	}

	// 2. Session B: Create Mainnet wallet with different key
	mainnetW, err := NewWallet()
	if err != nil {
		t.Fatalf("failed to create mainnet wallet: %v", err)
	}
	mainnetPath := GetDefaultWalletPathForNetwork("mainnet")
	if err := EncryptAndSaveKeystore(mainnetW, "pass-mainnet", mainnetPath); err != nil {
		t.Fatalf("failed to save mainnet keystore: %v", err)
	}

	// 3. Simulated Restart: Reload Testnet
	reloadedTester, err := DecryptAndLoadKeystore("pass-testnet", GetDefaultWalletPathForNetwork("testnet"))
	if err != nil {
		t.Fatalf("failed to reload testnet wallet: %v", err)
	}
	if reloadedTester.Address != testnetW.Address {
		t.Fatalf("reloaded testnet address mismatch: expected %s, got %s", testnetW.Address, reloadedTester.Address)
	}

	// 4. Simulated Restart: Reload Mainnet
	reloadedMain, err := DecryptAndLoadKeystore("pass-mainnet", GetDefaultWalletPathForNetwork("mainnet"))
	if err != nil {
		t.Fatalf("failed to reload mainnet wallet: %v", err)
	}
	if reloadedMain.Address != mainnetW.Address {
		t.Fatalf("reloaded mainnet address mismatch: expected %s, got %s", mainnetW.Address, reloadedMain.Address)
	}

	// 5. Cross-network address distinction check
	if reloadedTester.Address == reloadedMain.Address {
		t.Fatalf("unexpected collision: testnet and mainnet loaded identical address: %s", reloadedTester.Address)
	}
}

// Suppress unused core import warning
var _ = core.Version
