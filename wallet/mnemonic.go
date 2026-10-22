package wallet

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/acxxcxx/armax-coin/core/pqsig"
	"golang.org/x/text/unicode/norm"
)

// GenerateMnemonic generates a cryptographically secure BIP-39 mnemonic phrase.
// Default is 24 words (256-bit entropy). If 12 is specified, generates 12 words (128-bit entropy).
func GenerateMnemonic(numWords ...int) (string, error) {
	wordsCount := 24
	if len(numWords) > 0 && numWords[0] == 12 {
		wordsCount = 12
	}

	entropyLen := 32 // 256 bits for 24 words
	checksumBits := 8
	if wordsCount == 12 {
		entropyLen = 16 // 128 bits for 12 words
		checksumBits = 4
	}

	entropy := make([]byte, entropyLen)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("failed to generate random entropy: %v", err)
	}

	hash := sha256.Sum256(entropy)
	checksumByte := hash[0]

	entBig := new(big.Int).SetBytes(entropy)
	entBig.Lsh(entBig, uint(checksumBits))
	entBig.Or(entBig, big.NewInt(int64(checksumByte>>(8-checksumBits))))

	mask := big.NewInt(0x7FF) // 11 bits mask (2047)
	indices := make([]int, wordsCount)

	for i := wordsCount - 1; i >= 0; i-- {
		temp := new(big.Int).And(entBig, mask)
		indices[i] = int(temp.Int64())
		entBig.Rsh(entBig, 11)
	}

	var words []string
	for _, idx := range indices {
		if idx >= 0 && idx < len(BIP39EnglishWordList) {
			words = append(words, BIP39EnglishWordList[idx])
		}
	}

	return strings.Join(words, " "), nil
}

// NormalizeMnemonicInput applies user-input convenience canonicalization:
// 1. Applies Unicode NFKD normalization.
// 2. Converts English wordlist characters to lowercase.
// 3. Compacts arbitrary whitespace (spaces, tabs, newlines) into single ASCII spaces between words.
// Use this function when accepting raw user input from CLI or GUI text boxes.
func NormalizeMnemonicInput(raw string) string {
	nfkd := norm.NFKD.String(raw)
	words := strings.Fields(strings.ToLower(nfkd))
	return strings.Join(words, " ")
}

// NormalizeMnemonic is an alias for NormalizeMnemonicInput for backward compatibility.
func NormalizeMnemonic(raw string) string {
	return NormalizeMnemonicInput(raw)
}

// NormalizePassphrase canonicalizes a BIP-39 passphrase using Unicode NFKD normalization [Phase 4].
// Passphrases are strictly case-sensitive and preserve exact whitespace per the BIP-39 specification.
func NormalizePassphrase(passphrase string) string {
	return norm.NFKD.String(passphrase)
}

// ValidateMnemonic verifies if a 24-word or 12-word mnemonic phrase is valid according to BIP-39 checksum
func ValidateMnemonic(mnemonic string) (int, bool) {
	clean := NormalizeMnemonic(mnemonic)
	if clean == "" {
		return 0, false
	}
	words := strings.Split(clean, " ")
	wordsCount := len(words)
	if wordsCount != 24 && wordsCount != 12 {
		return wordsCount, false
	}

	wordMap := make(map[string]int, len(BIP39EnglishWordList))
	for idx, w := range BIP39EnglishWordList {
		wordMap[w] = idx
	}

	total := new(big.Int)
	for _, w := range words {
		idx, found := wordMap[w]
		if !found {
			return wordsCount, false // Word not in BIP-39 dictionary
		}
		total.Lsh(total, 11)
		total.Or(total, big.NewInt(int64(idx)))
	}

	var entropyLen int
	var checksum int
	var expectedChecksum int

	if wordsCount == 24 {
		// 8-bit checksum and 256-bit entropy
		checksum = int(new(big.Int).And(total, big.NewInt(0xFF)).Int64())
		total.Rsh(total, 8)
		entropyLen = 32
	} else {
		// 4-bit checksum and 128-bit entropy
		checksum = int(new(big.Int).And(total, big.NewInt(0x0F)).Int64())
		total.Rsh(total, 4)
		entropyLen = 16
	}

	entropyBytes := total.Bytes()
	if len(entropyBytes) < entropyLen {
		padded := make([]byte, entropyLen)
		copy(padded[entropyLen-len(entropyBytes):], entropyBytes)
		entropyBytes = padded
	} else if len(entropyBytes) > entropyLen {
		entropyBytes = entropyBytes[len(entropyBytes)-entropyLen:]
	}

	hash := sha256.Sum256(entropyBytes)
	if wordsCount == 24 {
		expectedChecksum = int(hash[0])
	} else {
		expectedChecksum = int(hash[0] >> 4)
	}

	return wordsCount, checksum == expectedChecksum
}

// pbkdf2SHA512 derives a cryptographic key using PBKDF2 with HMAC-SHA512
func pbkdf2SHA512(password, salt []byte, iterations, keyLen int) []byte {
	numBlocks := (keyLen + sha512.Size - 1) / sha512.Size
	var result []byte

	for block := 1; block <= numBlocks; block++ {
		var blockBytes [4]byte
		binary.BigEndian.PutUint32(blockBytes[:], uint32(block))

		mac := hmac.New(sha512.New, password)
		mac.Write(salt)
		mac.Write(blockBytes[:])
		u := mac.Sum(nil)
		ti := make([]byte, len(u))
		copy(ti, u)

		for iter := 1; iter < iterations; iter++ {
			mac.Reset()
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range ti {
				ti[j] ^= u[j]
			}
		}
		result = append(result, ti...)
	}

	return result[:keyLen]
}

// MnemonicToSeed derives a 64-byte binary seed from a BIP-39 mnemonic sentence per the BIP-39 standard [Phase 4].
// In accordance with standard BIP-39:
// - Password is the UTF-8 NFKD-normalized mnemonic sentence.
// - Salt is "mnemonic" concatenated with the UTF-8 NFKD-normalized passphrase.
// Passphrase casing and whitespace are strictly preserved.
func MnemonicToSeed(mnemonic, passphrase string) []byte {
	normMnemonic := norm.NFKD.String(mnemonic)
	normPassphrase := NormalizePassphrase(passphrase)
	password := []byte(normMnemonic)
	salt := []byte("mnemonic" + normPassphrase)
	return pbkdf2SHA512(password, salt, 2048, 64)
}

// WalletFromMnemonic deterministically restores an ARMX wallet from a BIP-39 mnemonic
// using HKDF-SHA256 account derivation for ML-DSA-65 [P2-3, Phase 4].
// It applies input convenience normalization (whitespace compaction, lowercase) to user-typed mnemonics,
// validates the BIP-39 checksum, derives the seed, and derives the account keypair.
func WalletFromMnemonic(mnemonic, passphrase string, accountIndex uint32) (*Wallet, error) {
	cleanMnemonic := NormalizeMnemonicInput(mnemonic)
	wordsCount, ok := ValidateMnemonic(cleanMnemonic)
	if !ok {
		return nil, errors.New("invalid mnemonic phrase (word unrecognized or checksum mismatch)")
	}
	if wordsCount == 12 {
		// 12 words accepted with warning as per spec
		fmt.Println("[!] Warning: 12-word mnemonic provides 128-bit entropy. 24 words (256-bit) is recommended for post-quantum security.")
	}

	bip39Seed := MnemonicToSeed(cleanMnemonic, passphrase)
	seed32, err := pqsig.DeriveSeedFromBIP39(bip39Seed, accountIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive account seed: %w", err)
	}

	w, err := WalletFromSeed(seed32)
	if err != nil {
		return nil, err
	}
	w.Mnemonic = cleanMnemonic
	return w, nil
}
