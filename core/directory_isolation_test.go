//go:build testhooks

package core

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDirectoryIsolation_NetworkProfiles verifies that database paths
// are strictly isolated under their respective network subdirectories:
// mainnet -> <datadir>/mainnet/blockchain.db
// testnet -> <datadir>/testnet/blockchain.db
// regtest -> <datadir>/regtest/blockchain.db
// soak    -> <datadir>/soak/blockchain.db
func TestDirectoryIsolation_NetworkProfiles(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)

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
		t.Run("DBPath_"+p.name, func(t *testing.T) {
			if err := SetNetwork(p.name); err != nil {
				t.Fatalf("SetNetwork(%s) failed: %v", p.name, err)
			}
			dbPath := GetDBPath()
			expectedPath := filepath.Join(baseDir, p.expectedSub, DBFileName)
			if filepath.Clean(dbPath) != filepath.Clean(expectedPath) {
				t.Fatalf("[%s] GetDBPath() expected %q, got %q", p.name, expectedPath, dbPath)
			}

			// Verify directory was created
			dir := filepath.Dir(dbPath)
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

// TestDirectoryIsolation_LegacyDBPreserved verifies that an existing legacy database file
// in the root data directory is NEVER modified, moved, or deleted when GetDBPath is invoked.
func TestDirectoryIsolation_LegacyDBPreserved(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	baseDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDir)

	// Create legacy database file in root directory
	legacyPath := filepath.Join(baseDir, DBFileName)
	legacyContent := []byte("SIMULATED_LEGACY_BLOCKCHAIN_DATA_DO_NOT_ALTER")
	if err := os.WriteFile(legacyPath, legacyContent, 0600); err != nil {
		t.Fatalf("failed to create simulated legacy database: %v", err)
	}
	initialHash := sha256.Sum256(legacyContent)

	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	// Invoke GetDBPath for mainnet
	isolatedPath := GetDBPath()
	expectedIsolatedPath := filepath.Join(baseDir, "mainnet", DBFileName)
	if filepath.Clean(isolatedPath) != filepath.Clean(expectedIsolatedPath) {
		t.Fatalf("expected isolated mainnet path %q, got %q", expectedIsolatedPath, isolatedPath)
	}

	// Verify legacy database file was NOT moved, renamed, or modified
	dataAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy database file was deleted or moved: %v", err)
	}
	hashAfter := sha256.Sum256(dataAfter)
	if hashAfter != initialHash {
		t.Fatalf("legacy database file content was modified! original %x, current %x", initialHash, hashAfter)
	}
}

// TestDatabaseProtection_CrossNetworkGenesisMismatchUnmodified proves:
// 1. A valid Testnet database opened under Mainnet profile is rejected with a descriptive error.
// 2. The source Testnet database file is NOT renamed, deleted, or mutated.
// 3. The SHA-256 checksum of the database before and after failed startup is 100% identical.
// 4. Zero .invalid-* quarantine files are generated.
// 5. Switching back to Testnet allows the database to load cleanly and successfully.
func TestDatabaseProtection_CrossNetworkGenesisMismatchUnmodified(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	tempDir := t.TempDir()
	testnetDbPath := filepath.Join(tempDir, "testnet_blockchain.db")

	// 1. Initialize a valid Testnet blockchain in its own file
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}
	bcTest, err := NewBlockchainWithStoragePath(1, testnetDbPath)
	if err != nil {
		t.Fatalf("failed to create valid testnet blockchain: %v", err)
	}
	if err := bcTest.Close(); err != nil {
		t.Fatalf("failed to close testnet blockchain: %v", err)
	}

	// Capture initial exact checksum of testnet database
	initialBytes, err := os.ReadFile(testnetDbPath)
	if err != nil {
		t.Fatalf("failed to read testnet db: %v", err)
	}
	initialHash := sha256.Sum256(initialBytes)

	// 2. Switch active network profile to Mainnet
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	// 3. Attempt to load the Testnet database under Mainnet profile
	loadedMainBc, err := NewBlockchainWithStoragePath(1, testnetDbPath)
	if err == nil {
		_ = loadedMainBc.Close()
		t.Fatalf("Security failure: Mainnet node successfully opened Testnet database! Expected genesis mismatch error.")
	}

	// Verify descriptive error message mentions genesis mismatch and non-mutation
	errMsg := err.Error()
	if !strings.Contains(errMsg, "stored database genesis mismatch") {
		t.Fatalf("Expected 'stored database genesis mismatch' in error, got: %v", errMsg)
	}
	if !strings.Contains(errMsg, TestnetGenesisHash) {
		t.Fatalf("Expected error to mention database genesis hash %s, got: %v", TestnetGenesisHash, errMsg)
	}
	if !strings.Contains(errMsg, GenesisHash) {
		t.Fatalf("Expected error to mention expected genesis hash %s, got: %v", GenesisHash, errMsg)
	}

	// 4. Verify original database file STILL EXISTS at original location (NOT moved or renamed)
	if _, err := os.Stat(testnetDbPath); err != nil {
		t.Fatalf("Original database file %s was deleted or moved: %v", testnetDbPath, err)
	}

	// 5. Verify file checksum is 100% IDENTICAL (zero writes occurred)
	afterBytes, err := os.ReadFile(testnetDbPath)
	if err != nil {
		t.Fatalf("failed to re-read testnet db: %v", err)
	}
	afterHash := sha256.Sum256(afterBytes)
	if afterHash != initialHash {
		t.Fatalf("Database was mutated during rejected startup! Before: %x, After: %x", initialHash, afterHash)
	}

	// 6. Verify zero .invalid-* quarantine files were generated
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "testnet_blockchain.db.invalid-") || strings.Contains(entry.Name(), ".invalid-") {
			t.Fatalf("Unexpected quarantine file found on disk: %s", entry.Name())
		}
	}

	// 7. Switch back to Testnet and verify database loads cleanly without issues
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) restore failed: %v", err)
	}
	reloadedBc, err := NewBlockchainWithStoragePath(1, testnetDbPath)
	if err != nil {
		t.Fatalf("Failed to reload undamaged testnet database under testnet profile: %v", err)
	}
	defer reloadedBc.Close()

	if len(reloadedBc.Chain) != 1 || reloadedBc.Chain[0].Hash != TestnetGenesisHash {
		t.Fatalf("Reloaded blockchain has invalid state or genesis hash: got %s, want %s", reloadedBc.Chain[0].Hash, TestnetGenesisHash)
	}
}
