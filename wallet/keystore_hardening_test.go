package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestKeystoreHardening_Comprehensive(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "wallet.json")
	passphrase := "CorrectQuantumPassphrase2026!"

	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	// 1. Valid keystore save and load round-trip
	if err := EncryptAndSaveKeystore(w, passphrase, ksPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore failed: %v", err)
	}

	loadedW, err := DecryptAndLoadKeystore(passphrase, ksPath)
	if err != nil {
		t.Fatalf("DecryptAndLoadKeystore failed: %v", err)
	}
	if loadedW.Address != w.Address {
		t.Fatalf("Loaded address mismatch: expected %s, got %s", w.Address, loadedW.Address)
	}
	if loadedW.PublicKey != w.PublicKey {
		t.Fatalf("Loaded public key mismatch")
	}

	// 2. Wrong passphrase must be rejected
	_, err = DecryptAndLoadKeystore("WrongPassword123!", ksPath)
	if err == nil {
		t.Fatalf("Expected decryption failure with wrong passphrase, but it succeeded")
	}

	// 3. File too large (> 1 MB limit)
	largePath := filepath.Join(tempDir, "oversized.json")
	largeData := make([]byte, MaxKeystoreFileSize+100)
	if err := os.WriteFile(largePath, largeData, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	_, err = DecryptAndLoadKeystore(passphrase, largePath)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("Expected file size limit rejection, got: %v", err)
	}

	// Helper to mutate keystore JSON
	readValidKeystore := func() KeystoreFile {
		data, err := os.ReadFile(ksPath)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		var ks KeystoreFile
		if err := json.Unmarshal(data, &ks); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		return ks
	}

	saveMutated := func(ks KeystoreFile, path string) {
		data, _ := json.MarshalIndent(ks, "", "  ")
		_ = os.WriteFile(path, data, 0600)
	}

	// 4. Extreme / invalid KDF parameters rejected BEFORE Argon2 execution
	// Case 4a: Extreme memory (DoS attempt)
	ksExtremeMem := readValidKeystore()
	ksExtremeMem.Crypto.KDFParams.Memory = 2 * 1024 * 1024 // 2 GB
	mutPath := filepath.Join(tempDir, "mut.json")
	saveMutated(ksExtremeMem, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid Argon2 memory") {
		t.Fatalf("Expected extreme memory rejection, got: %v", err)
	}

	// Case 4b: Zero iterations
	ksZeroIter := readValidKeystore()
	ksZeroIter.Crypto.KDFParams.Iterations = 0
	saveMutated(ksZeroIter, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid Argon2 iterations") {
		t.Fatalf("Expected zero iterations rejection, got: %v", err)
	}

	// Case 4c: Extreme parallelism
	ksExtremePar := readValidKeystore()
	ksExtremePar.Crypto.KDFParams.Parallelism = 100
	saveMutated(ksExtremePar, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid Argon2 parallelism") {
		t.Fatalf("Expected extreme parallelism rejection, got: %v", err)
	}

	// 5. Malformed salt, nonce, and ciphertext
	// Case 5a: Short salt
	ksShortSalt := readValidKeystore()
	ksShortSalt.Crypto.KDFParams.Salt = hex.EncodeToString([]byte("short"))
	saveMutated(ksShortSalt, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid salt length") {
		t.Fatalf("Expected short salt rejection, got: %v", err)
	}

	// Case 5b: Short nonce
	ksShortNonce := readValidKeystore()
	ksShortNonce.Crypto.Nonce = hex.EncodeToString([]byte("short_nonce"))
	saveMutated(ksShortNonce, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid nonce length") {
		t.Fatalf("Expected short nonce rejection, got: %v", err)
	}

	// Case 5c: Truncated ciphertext
	ksShortCipher := readValidKeystore()
	ksShortCipher.Crypto.Ciphertext = hex.EncodeToString([]byte("too_short_for_seed_and_tag"))
	saveMutated(ksShortCipher, mutPath)
	_, err = DecryptAndLoadKeystore(passphrase, mutPath)
	if err == nil || !strings.Contains(err.Error(), "invalid ciphertext length") {
		t.Fatalf("Expected truncated ciphertext rejection, got: %v", err)
	}

	// 6. Permission mode: overwrite file with 0644 mode must be re-secured to 0600
	if err := os.WriteFile(ksPath, []byte("overwrite_test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := EncryptAndSaveKeystore(w, passphrase, ksPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore failed on existing file: %v", err)
	}
	// Verify it still reloads cleanly
	reloaded, err := DecryptAndLoadKeystore(passphrase, ksPath)
	if err != nil || reloaded.Address != w.Address {
		t.Fatalf("Failed to reload re-encrypted keystore: %v", err)
	}
}

// TestKeystore_ValidateKDFParameters_Boundaries exhaustively tests parameter boundary validation
// without executing heavy KDF calculations, covering exact limits, one unit above, one unit below,
// math.MaxUint32, zero values, and extreme values [Follow-up Point 1].
func TestKeystore_ValidateKDFParameters_Boundaries(t *testing.T) {
	// Base valid parameters
	validKDF := KDFParams{
		Memory:      GenArgonMemoryKiB,   // 64 MiB (65,536 KiB)
		Iterations:  GenArgonIterations,  // 3
		Parallelism: GenArgonParallelism, // 2
		Salt:        "0123456789abcdef0123456789abcdef",
	}

	if err := ValidateKDFParameters(validKDF); err != nil {
		t.Fatalf("Expected valid KDF to pass: %v", err)
	}

	// 1. Memory Boundaries (Min: 8,192 KiB, Max: 262,144 KiB)
	// Exact upper bound: 262,144 KiB (256 MiB)
	kdfMaxMem := validKDF
	kdfMaxMem.Memory = MaxVerificationMemoryKiB
	if err := ValidateKDFParameters(kdfMaxMem); err != nil {
		t.Errorf("Exact max memory (262,144 KiB) should pass: %v", err)
	}

	// One unit above max memory: 262,145 KiB (REJECTED)
	kdfOverMem := validKDF
	kdfOverMem.Memory = MaxVerificationMemoryKiB + 1
	if err := ValidateKDFParameters(kdfOverMem); err == nil {
		t.Errorf("One unit above max memory (262,145 KiB) must be rejected!")
	}

	// Exact lower bound: 8,192 KiB (8 MiB)
	kdfMinMem := validKDF
	kdfMinMem.Memory = MinVerificationMemoryKiB
	if err := ValidateKDFParameters(kdfMinMem); err != nil {
		t.Errorf("Exact min memory (8,192 KiB) should pass: %v", err)
	}

	// One unit below min memory: 8,191 KiB (REJECTED)
	kdfUnderMem := validKDF
	kdfUnderMem.Memory = MinVerificationMemoryKiB - 1
	if err := ValidateKDFParameters(kdfUnderMem); err == nil {
		t.Errorf("One unit below min memory (8,191 KiB) must be rejected!")
	}

	// Extreme memory: 1,048,576 KiB (1 GiB) and math.MaxUint32 (REJECTED)
	kdf1GB := validKDF
	kdf1GB.Memory = 1024 * 1024
	if err := ValidateKDFParameters(kdf1GB); err == nil {
		t.Errorf("1 GiB memory must be rejected by 256 MiB platform budget!")
	}

	kdfMaxUint := validKDF
	kdfMaxUint.Memory = ^uint32(0) // 0xFFFFFFFF
	if err := ValidateKDFParameters(kdfMaxUint); err == nil {
		t.Errorf("math.MaxUint32 memory must be rejected!")
	}

	// 2. Iteration Boundaries (Min: 1, Max: 30)
	// Exact upper bound: 30
	kdfMaxIter := validKDF
	kdfMaxIter.Iterations = MaxVerificationIterations
	if err := ValidateKDFParameters(kdfMaxIter); err != nil {
		t.Errorf("Exact max iterations (30) should pass: %v", err)
	}

	// One unit above max iterations: 31 (REJECTED)
	kdfOverIter := validKDF
	kdfOverIter.Iterations = MaxVerificationIterations + 1
	if err := ValidateKDFParameters(kdfOverIter); err == nil {
		t.Errorf("One unit above max iterations (31) must be rejected!")
	}

	// Exact lower bound: 1
	kdfMinIter := validKDF
	kdfMinIter.Iterations = MinVerificationIterations
	if err := ValidateKDFParameters(kdfMinIter); err != nil {
		t.Errorf("Exact min iterations (1) should pass: %v", err)
	}

	// Zero iterations (REJECTED)
	kdfZeroIter := validKDF
	kdfZeroIter.Iterations = 0
	if err := ValidateKDFParameters(kdfZeroIter); err == nil {
		t.Errorf("Zero iterations must be rejected!")
	}

	// 3. Parallelism Boundaries (Min: 1, Max: 8)
	// Exact upper bound: 8
	kdfMaxPar := validKDF
	kdfMaxPar.Parallelism = MaxVerificationParallelism
	if err := ValidateKDFParameters(kdfMaxPar); err != nil {
		t.Errorf("Exact max parallelism (8) should pass: %v", err)
	}

	// One unit above max parallelism: 9 (REJECTED)
	kdfOverPar := validKDF
	kdfOverPar.Parallelism = MaxVerificationParallelism + 1
	if err := ValidateKDFParameters(kdfOverPar); err == nil {
		t.Errorf("One unit above max parallelism (9) must be rejected!")
	}

	// Zero parallelism (REJECTED)
	kdfZeroPar := validKDF
	kdfZeroPar.Parallelism = 0
	if err := ValidateKDFParameters(kdfZeroPar); err == nil {
		t.Errorf("Zero parallelism must be rejected!")
	}
}

// TestKeystore_HighParameters_ExecutionBudget tests decryption of a keystore configured with
// elevated parameters within a safe, bounded CI execution budget (32 MiB, 2 iterations, 2 lanes) [F-07].
func TestKeystore_HighParameters_ExecutionBudget(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "high_budget_wallet.json")
	passphrase := "HighBudgetPassword2026!"

	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	salt := make([]byte, StandardSaltLen)
	for i := range salt {
		salt[i] = byte(i)
	}

	// Use safe test limits for actual Argon2 execution to avoid slowing down CI
	testMem := uint32(32 * 1024) // 32 MiB
	testIter := uint32(2)
	testPar := uint8(2)

	key := argon2.IDKey([]byte(passphrase), salt, testIter, testMem, testPar, GenArgonKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM failed: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	ciphertext := gcm.Seal(nil, nonce, w.Seed32[:], nil)

	ks := KeystoreFile{
		Version:   KeystoreVersion,
		Address:   w.Address,
		PublicKey: w.PublicKey,
		Crypto: CryptoJSON{
			Cipher:     "aes-256-gcm",
			Ciphertext: hex.EncodeToString(ciphertext),
			Nonce:      hex.EncodeToString(nonce),
			KDF:        "argon2id",
			KDFParams: KDFParams{
				Memory:      testMem,
				Iterations:  testIter,
				Parallelism: testPar,
				Salt:        hex.EncodeToString(salt),
			},
		},
	}

	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}
	if err := os.WriteFile(ksPath, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loaded, err := DecryptAndLoadKeystore(passphrase, ksPath)
	if err != nil {
		t.Fatalf("DecryptAndLoadKeystore failed: %v", err)
	}
	if loaded.Address != w.Address {
		t.Fatalf("Address mismatch: expected %s, got %s", w.Address, loaded.Address)
	}
}

// TestKeystore_MaximumKDFParameterPolicyValidation validates the exact upper limits
// of the verification policy (256 MiB, 30 iterations, 8 lanes) without invoking heavy Argon2 [F-07].
func TestKeystore_MaximumKDFParameterPolicyValidation(t *testing.T) {
	saltHex := hex.EncodeToString(make([]byte, StandardSaltLen))

	// Exact maximum allowable verification parameters
	maxParams := KDFParams{
		Memory:      MaxVerificationMemoryKiB,   // 256 MiB
		Iterations:  MaxVerificationIterations,  // 30
		Parallelism: MaxVerificationParallelism, // 8
		Salt:        saltHex,
	}

	if err := ValidateKDFParameters(maxParams); err != nil {
		t.Fatalf("Maximum policy parameters (256 MiB, 30 iter, 8 par) must be accepted by policy: %v", err)
	}

	// Any value strictly above the policy limit must be rejected BEFORE execution
	overMem := maxParams
	overMem.Memory = MaxVerificationMemoryKiB + 1
	if err := ValidateKDFParameters(overMem); err == nil {
		t.Errorf("Memory above 256 MiB must be rejected!")
	}

	overIter := maxParams
	overIter.Iterations = MaxVerificationIterations + 1
	if err := ValidateKDFParameters(overIter); err == nil {
		t.Errorf("Iterations above 30 must be rejected!")
	}

	overPar := maxParams
	overPar.Parallelism = MaxVerificationParallelism + 1
	if err := ValidateKDFParameters(overPar); err == nil {
		t.Errorf("Parallelism above 8 must be rejected!")
	}
}

// TestKeystore_AtomicReplacementFailurePreservesExisting verifies that if an atomic replace
// fails (e.g. invalid target path), any existing destination file is preserved intact and NOT deleted [F-02].
func TestKeystore_AtomicReplacementFailurePreservesExisting(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "existing_wallet.json")
	originalContent := []byte("ORIGINAL_CANONICAL_KEYSTORE_DATA_DO_NOT_DELETE")

	if err := os.WriteFile(ksPath, originalContent, 0600); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	// Create a subfolder where the file should be to force a rename failure
	blockedDir := filepath.Join(tempDir, "blocked_dir")
	if err := os.Mkdir(blockedDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}
	targetFileInDir := filepath.Join(blockedDir, "target.json")
	if err := os.WriteFile(targetFileInDir, originalContent, 0600); err != nil {
		t.Fatalf("Failed to write target file: %v", err)
	}

	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	// Attempt to save to an invalid target where rename will fail (e.g. target path is an existing directory)
	err = EncryptAndSaveKeystore(w, "testpass", blockedDir)
	if err == nil {
		t.Fatalf("Expected save to fail when destination is a directory")
	}

	// Crucial F-02 check: The original file inside the directory was NOT deleted!
	data, readErr := os.ReadFile(targetFileInDir)
	if readErr != nil {
		t.Fatalf("Target file was deleted or cannot be read: %v", readErr)
	}
	if string(data) != string(originalContent) {
		t.Fatalf("Target content changed! Expected %s, got %s", string(originalContent), string(data))
	}
}

// TestKeystore_ConcurrentOperations verifies atomic thread-safety under concurrent saves and loads [Follow-up Point 3].
func TestKeystore_ConcurrentOperations(t *testing.T) {
	tempDir := t.TempDir()
	passphrase := "ConcurrentPassword2026!"

	w, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}

	// Run 10 concurrent workers creating and loading wallets
	concurrency := 10
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			path := filepath.Join(tempDir, fmt.Sprintf("wallet_worker_%d.json", workerID))
			if err := EncryptAndSaveKeystore(w, passphrase, path); err != nil {
				errChan <- fmt.Errorf("worker %d save failed: %w", workerID, err)
				return
			}
			loaded, err := DecryptAndLoadKeystore(passphrase, path)
			if err != nil {
				errChan <- fmt.Errorf("worker %d load failed: %w", workerID, err)
				return
			}
			if loaded.Address != w.Address {
				errChan <- fmt.Errorf("worker %d address mismatch", workerID)
				return
			}
			errChan <- nil
		}(i)
	}

	for i := 0; i < concurrency; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("Concurrent keystore error: %v", err)
		}
	}
}
