package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
)

const (
	KeystoreVersion = 1

	// --- Keystore Generation Policy [Follow-up Point 1, Point 3] ---
	// Parameters used when creating NEW keystore files.
	// NOTE ON UNITS:
	// - The JSON field "memory" is specified in kibibytes (KiB, 1024 bytes).
	// - The argon2.IDKey(..., memory uint32, ...) function expects memory in KiB.
	// - Therefore, JSON memory is passed directly to argon2.IDKey without conversion.
	// Generation settings:
	// - 64 MiB (65,536 KiB), 3 iterations, 2 threads (~150ms execution).
	GenArgonMemoryKiB   uint32 = 64 * 1024 // 64 MiB (65,536 KiB)
	GenArgonMemory      uint32 = GenArgonMemoryKiB
	GenArgonIterations  uint32 = 3
	GenArgonParallelism uint8  = 2
	GenArgonKeyLen      uint32 = 32
	StandardSaltLen     int    = 16

	// --- Keystore Verification & Legacy-Read Policy [Follow-up Point 1, Point 3] ---
	// Strict boundaries evaluated BEFORE allocating memory or invoking argon2.IDKey.
	// Platform Resource Budget:
	// - Memory Budget: On a 1GB/2GB RAM SBC running Armbian/Linux, max allowable memory is capped
	//   at 256 MiB (262,144 KiB) to guarantee untrusted keystores cannot trigger the Linux OOM killer.
	// - CPU Budget: Iterations are capped at 30 (< 2.5s on Cortex-A72) to prevent CPU starvation DoS.
	// - Thread Budget: Parallelism is capped at 8 to prevent thread exhaustion.
	// - Concurrency Resource Policy [F-07]:
	//   While individual KDF operations are bounded to 256 MiB / 30 iters / 8 lanes, callers
	//   on memory-constrained systems (such as single-board computers / SBCs with 1-2 GB of RAM)
	//   must limit simultaneous concurrent decryptions using a semaphore (e.g. max 1-2 concurrent
	//   decryptions) to prevent aggregate memory exhaustion from parallel requests.
	MaxKeystoreFileSize = 1024 * 1024 // 1 MB file read limit

	MinVerificationIterations  uint32 = 1
	MaxVerificationIterations  uint32 = 30
	MinVerificationMemoryKiB   uint32 = 8 * 1024   // 8 MiB (8,192 KiB) minimum
	MaxVerificationMemoryKiB   uint32 = 256 * 1024 // 256 MiB (262,144 KiB) maximum verification budget
	MinVerificationMemory      uint32 = MinVerificationMemoryKiB
	MaxVerificationMemory      uint32 = MaxVerificationMemoryKiB
	MinVerificationParallelism uint8  = 1
	MaxVerificationParallelism uint8  = 8

	StandardNonceLen = 12 // Standard 96-bit nonce for AES-GCM
	MinCiphertextLen = 48 // 32-byte seed + 16-byte AES-GCM authentication tag
	MaxCiphertextLen = 1024
)

type KDFParams struct {
	Memory      uint32 `json:"memory"` // Memory usage in KiB (kibibytes)
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
}

type CryptoJSON struct {
	Cipher     string    `json:"cipher"`
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	KDF        string    `json:"kdf"`
	KDFParams  KDFParams `json:"kdf_params"`
}

type KeystoreFile struct {
	Version   int        `json:"version"`
	Address   string     `json:"address"`
	PublicKey string     `json:"public_key"`
	Crypto    CryptoJSON `json:"crypto"`
}

// EncryptAndSaveKeystore encrypts the 32-byte seed using Argon2id + AES-256-GCM and saves to disk
// atomically with strict 0600 permissions, ensuring safe permission enforcement even on overwrite [P2-3, Phase 3].
func EncryptAndSaveKeystore(w *Wallet, passphrase, filePath string) error {
	if w == nil {
		return errors.New("wallet is nil")
	}
	if passphrase == "" {
		return errors.New("passphrase cannot be empty")
	}
	if filePath == "" {
		return errors.New("file path cannot be empty")
	}

	salt := make([]byte, StandardSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("failed to generate random salt: %w", err)
	}

	key := argon2.IDKey([]byte(passphrase), salt, GenArgonIterations, GenArgonMemory, GenArgonParallelism, GenArgonKeyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("failed to generate random nonce: %w", err)
	}

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
				Memory:      GenArgonMemory,
				Iterations:  GenArgonIterations,
				Parallelism: GenArgonParallelism,
				Salt:        hex.EncodeToString(salt),
			},
		},
	}

	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keystore JSON: %w", err)
	}

	// Atomic write using temporary file in the same directory [Phase 3]
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create keystore directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "wallet-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary keystore file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to set 0600 permissions on temporary keystore: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write keystore data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync keystore to disk: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary keystore: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		// F-02: Do NOT speculatively delete the existing file on rename failure.
		// Retain the existing file intact and return the failure error.
		return fmt.Errorf("failed to atomically commit keystore file: %w", err)
	}
	tmpPath = "" // Rename succeeded, disarm defer cleanup

	// Ensure final permissions are strictly 0600 even if file already existed
	_ = os.Chmod(filePath, 0600)

	return nil
}

// CreateNewWalletKeystore creates or restores a wallet keystore at filePath.
// If the destination file already exists, it automatically creates a timestamped backup
// (<filePath>.backup.<timestamp>) before saving the new wallet, unless force is true [T15].
func CreateNewWalletKeystore(w *Wallet, passphrase, filePath string, force bool) (string, error) {
	var backupPath string
	if _, err := os.Stat(filePath); err == nil {
		if !force {
			backupPath = fmt.Sprintf("%s.backup.%d", filePath, time.Now().UnixNano())
			data, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("failed to read existing wallet for backup: %w", err)
			}
			if err := os.WriteFile(backupPath, data, 0600); err != nil {
				return "", fmt.Errorf("failed to create wallet backup at %s: %w", backupPath, err)
			}
		}
	}
	if err := EncryptAndSaveKeystore(w, passphrase, filePath); err != nil {
		return "", err
	}
	return backupPath, nil
}

// DecryptAndLoadKeystore loads an encrypted keystore file, enforces size/parameter bounds,
// and restores the Wallet with correct passphrase [P2-3, Phase 3].
func DecryptAndLoadKeystore(passphrase, filePath string) (*Wallet, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase cannot be empty")
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat keystore file: %w", err)
	}
	if fi.Size() > MaxKeystoreFileSize {
		return nil, fmt.Errorf("keystore file size (%d bytes) exceeds maximum allowable limit (%d bytes)", fi.Size(), MaxKeystoreFileSize)
	}
	if fi.Size() == 0 {
		return nil, errors.New("keystore file is empty")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open keystore file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxKeystoreFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read keystore file: %w", err)
	}
	if int64(len(data)) > MaxKeystoreFileSize {
		return nil, fmt.Errorf("keystore file exceeds maximum size limit of %d bytes", MaxKeystoreFileSize)
	}

	var ks KeystoreFile
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("invalid keystore JSON format: %w", err)
	}

	// 1. Version & suite validation
	if ks.Version != KeystoreVersion {
		return nil, fmt.Errorf("unsupported keystore version %d (expected %d)", ks.Version, KeystoreVersion)
	}
	if ks.Crypto.Cipher != "aes-256-gcm" || ks.Crypto.KDF != "argon2id" {
		return nil, fmt.Errorf("unsupported crypto suite: %s/%s", ks.Crypto.KDF, ks.Crypto.Cipher)
	}

	// 2. Strict KDF parameter range validation BEFORE calling argon2.IDKey [Phase 3, Follow-up Point 1, Point 3]
	kdf := ks.Crypto.KDFParams
	if err := ValidateKDFParameters(kdf); err != nil {
		return nil, err
	}

	// 3. Strict cryptographic artifact length validation BEFORE Argon2 allocation
	salt, err := hex.DecodeString(kdf.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt encoding: %w", err)
	}
	if len(salt) != StandardSaltLen {
		return nil, fmt.Errorf("invalid salt length %d bytes (expected %d)", len(salt), StandardSaltLen)
	}

	nonce, err := hex.DecodeString(ks.Crypto.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}
	if len(nonce) != StandardNonceLen {
		return nil, fmt.Errorf("invalid nonce length %d bytes (expected %d)", len(nonce), StandardNonceLen)
	}

	ciphertext, err := hex.DecodeString(ks.Crypto.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext encoding: %w", err)
	}
	if len(ciphertext) < MinCiphertextLen || len(ciphertext) > MaxCiphertextLen {
		return nil, fmt.Errorf("invalid ciphertext length %d bytes (must be between %d and %d)", len(ciphertext), MinCiphertextLen, MaxCiphertextLen)
	}

	// 4. Parameter validation passed: perform Argon2 key derivation
	key := argon2.IDKey([]byte(passphrase), salt, kdf.Iterations, kdf.Memory, kdf.Parallelism, GenArgonKeyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: invalid passphrase or corrupted keystore")
	}

	if len(plaintext) != 32 {
		return nil, fmt.Errorf("invalid decrypted seed length %d (expected 32)", len(plaintext))
	}

	var seed32 [32]byte
	copy(seed32[:], plaintext)

	return WalletFromSeed(seed32)
}

// ValidateKDFParameters verifies that Argon2id parameters fall strictly within platform resource limits [Follow-up Point 1].
// Note: kdf.Memory is in KiB (kibibytes = 1024 bytes). Min is 8,192 KiB (8 MiB); Max is 262,144 KiB (256 MiB).
func ValidateKDFParameters(kdf KDFParams) error {
	if kdf.Iterations < MinVerificationIterations || kdf.Iterations > MaxVerificationIterations {
		return fmt.Errorf("invalid Argon2 iterations %d: must be between %d and %d", kdf.Iterations, MinVerificationIterations, MaxVerificationIterations)
	}
	if kdf.Memory < MinVerificationMemoryKiB || kdf.Memory > MaxVerificationMemoryKiB {
		return fmt.Errorf("invalid Argon2 memory %d KiB: must be between %d KiB (8 MiB) and %d KiB (256 MiB)", kdf.Memory, MinVerificationMemoryKiB, MaxVerificationMemoryKiB)
	}
	if kdf.Parallelism < MinVerificationParallelism || kdf.Parallelism > MaxVerificationParallelism {
		return fmt.Errorf("invalid Argon2 parallelism %d: must be between %d and %d", kdf.Parallelism, MinVerificationParallelism, MaxVerificationParallelism)
	}
	return nil
}

// PromptPassphrase reads a passphrase from the specified environment variable,
// or prompts the user securely in terminal without echoing characters [P2-3].
func PromptPassphrase(envVar, promptMsg string) (string, error) {
	if envVar != "" {
		if pass := os.Getenv(envVar); pass != "" {
			return pass, nil
		}
	}

	fmt.Print(promptMsg)
	bytePass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password from terminal: %w", err)
	}

	pass := string(bytePass)
	if pass == "" {
		return "", errors.New("passphrase cannot be empty")
	}
	return pass, nil
}
