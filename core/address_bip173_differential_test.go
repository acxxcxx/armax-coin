package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
)

// TestF5_BIP173_DifferentialValidation performs differential fuzz and vector testing
// of Armaxcoin address encoding/decoding against the reference BIP-173 bech32 implementation [F5].
func TestF5_BIP173_DifferentialValidation(t *testing.T) {
	// 1. Generate at least 25 diverse 32-byte test vectors (edge cases, bit patterns, hashes)
	var testVectors [][]byte

	// Vector 1: All zeros (32 bytes)
	testVectors = append(testVectors, make([]byte, AddressPayloadLen))

	// Vector 2: All 0xFF (32 bytes)
	allOnes := make([]byte, AddressPayloadLen)
	for i := range allOnes {
		allOnes[i] = 0xFF
	}
	testVectors = append(testVectors, allOnes)

	// Vector 3: Alternating 0xAA (32 bytes)
	altAA := make([]byte, AddressPayloadLen)
	for i := range altAA {
		altAA[i] = 0xAA
	}
	testVectors = append(testVectors, altAA)

	// Vector 4: Alternating 0x55 (32 bytes)
	alt55 := make([]byte, AddressPayloadLen)
	for i := range alt55 {
		alt55[i] = 0x55
	}
	testVectors = append(testVectors, alt55)

	// Vector 5: Ascending sequence (0, 1, 2, ... 31)
	ascSeq := make([]byte, AddressPayloadLen)
	for i := range ascSeq {
		ascSeq[i] = byte(i)
	}
	testVectors = append(testVectors, ascSeq)

	// Vectors 6..25: 20 deterministic SHA-256 hashes
	for i := 0; i < 20; i++ {
		seed := make([]byte, 8)
		binary.BigEndian.PutUint64(seed, uint64(i*10007+42))
		h := sha256.Sum256(seed)
		testVectors = append(testVectors, h[:])
	}

	if len(testVectors) < 20 {
		t.Fatalf("Insufficient test vectors: got %d, expected at least 20", len(testVectors))
	}

	// 2. Differential round-trip tests against btcutil/bech32
	for idx, payload := range testVectors {
		t.Run(fmt.Sprintf("Differential_Vector_%d", idx), func(t *testing.T) {
			// A. Encode with Armaxcoin core implementation
			coreAddr, err := Bech32Encode(Bech32HRP, payload)
			if err != nil {
				t.Fatalf("Vector %d: core.Bech32Encode failed: %v", idx, err)
			}

			// Validate with core.ValidateAddress
			if !ValidateAddress(coreAddr) {
				t.Fatalf("Vector %d: core.ValidateAddress rejected its own generated address: %s", idx, coreAddr)
			}

			// B. Decode with reference btcutil/bech32
			refHRP, refData5, err := bech32.Decode(coreAddr)
			if err != nil {
				t.Fatalf("Vector %d: reference bech32.Decode rejected core address %s: %v", idx, coreAddr, err)
			}
			if refHRP != Bech32HRP {
				t.Fatalf("Vector %d: reference bech32 HRP mismatch: got %s, expected %s", idx, refHRP, Bech32HRP)
			}
			refPayload, err := bech32.ConvertBits(refData5, 5, 8, false)
			if err != nil {
				t.Fatalf("Vector %d: reference bech32.ConvertBits failed: %v", idx, err)
			}
			if !bytes.Equal(refPayload, payload) {
				t.Fatalf("Vector %d: reference decoded payload does not match original payload", idx)
			}

			// C. Encode with reference btcutil/bech32
			refDataConv5, err := bech32.ConvertBits(payload, 8, 5, true)
			if err != nil {
				t.Fatalf("Vector %d: reference ConvertBits (8->5) failed: %v", idx, err)
			}
			refAddr, err := bech32.Encode(Bech32HRP, refDataConv5)
			if err != nil {
				t.Fatalf("Vector %d: reference bech32.Encode failed: %v", idx, err)
			}

			// D. Verify that core and reference produce EXACT character-for-character identical strings
			if coreAddr != refAddr {
				t.Fatalf("Vector %d: string mismatch between core and reference:\n  core: %s\n   ref: %s", idx, coreAddr, refAddr)
			}

			// E. Decode reference address using core.Bech32Decode
			coreDecodedHRP, coreDecodedPayload, err := Bech32Decode(refAddr)
			if err != nil {
				t.Fatalf("Vector %d: core.Bech32Decode failed on reference address %s: %v", idx, refAddr, err)
			}
			if coreDecodedHRP != Bech32HRP {
				t.Fatalf("Vector %d: coreDecodedHRP mismatch: got %s, expected %s", idx, coreDecodedHRP, Bech32HRP)
			}
			if !bytes.Equal(coreDecodedPayload, payload) {
				t.Fatalf("Vector %d: coreDecodedPayload mismatch with original payload", idx)
			}
		})
	}
}

// TestF5_RejectMixedCaseAndUppercase verifies that uppercase and mixed-case addresses are rejected [F5].
func TestF5_RejectMixedCaseAndUppercase(t *testing.T) {
	dummyPayload := sha256.Sum256([]byte("armax_mixed_case_test"))
	validAddr, err := Bech32Encode(Bech32HRP, dummyPayload[:])
	if err != nil {
		t.Fatalf("Failed to encode valid address: %v", err)
	}

	// 1. Fully uppercase
	upperAddr := strings.ToUpper(validAddr)
	if ValidateAddress(upperAddr) {
		t.Fatalf("ValidateAddress accepted fully uppercase address: %s", upperAddr)
	}
	if _, _, err := Bech32Decode(upperAddr); err == nil {
		t.Fatalf("Bech32Decode accepted fully uppercase address: %s", upperAddr)
	}

	// 2. Mixed-case HRP ("Armx1...")
	mixedHRPAddr := "Armx" + validAddr[4:]
	if ValidateAddress(mixedHRPAddr) {
		t.Fatalf("ValidateAddress accepted mixed-case HRP address: %s", mixedHRPAddr)
	}
	if _, _, err := Bech32Decode(mixedHRPAddr); err == nil {
		t.Fatalf("Bech32Decode accepted mixed-case HRP address: %s", mixedHRPAddr)
	}

	// 3. Mixed-case payload ("armx1...A...")
	mid := len(validAddr) / 2
	mixedPayloadAddr := validAddr[:mid] + strings.ToUpper(string(validAddr[mid])) + validAddr[mid+1:]
	if ValidateAddress(mixedPayloadAddr) {
		t.Fatalf("ValidateAddress accepted mixed-case payload address: %s", mixedPayloadAddr)
	}
	if _, _, err := Bech32Decode(mixedPayloadAddr); err == nil {
		t.Fatalf("Bech32Decode accepted mixed-case payload address: %s", mixedPayloadAddr)
	}
}

// TestF5_RejectWrongHRP verifies that addresses with non-armx HRP prefixes are strictly rejected [F5].
func TestF5_RejectWrongHRP(t *testing.T) {
	dummyPayload := sha256.Sum256([]byte("armax_wrong_hrp_test"))
	invalidHRPs := []string{"bc", "tb", "btc", "eth", "arm", "armxx", "cosmos", "sol", "doge"}

	for _, badHRP := range invalidHRPs {
		badAddr, err := Bech32Encode(badHRP, dummyPayload[:])
		if err != nil {
			continue
		}
		if ValidateAddress(badAddr) {
			t.Fatalf("ValidateAddress accepted address with wrong HRP '%s': %s", badHRP, badAddr)
		}
		if _, err := AddressToHex(badAddr); err == nil {
			t.Fatalf("AddressToHex accepted address with wrong HRP '%s': %s", badHRP, badAddr)
		}
	}
}

// TestF5_ChecksumCorruptionDetection verifies that single-character and multiple-character typos
// are caught by the BIP-173 checksum verification [F5].
func TestF5_ChecksumCorruptionDetection(t *testing.T) {
	dummyPayload := sha256.Sum256([]byte("armax_checksum_test"))
	validAddr, err := Bech32Encode(Bech32HRP, dummyPayload[:])
	if err != nil {
		t.Fatalf("Failed to encode valid address: %v", err)
	}

	// Corrupt single character at various positions (start of payload, middle, end)
	testPositions := []int{5, 15, 30, len(validAddr) - 1, len(validAddr) - 2}
	for _, pos := range testPositions {
		corruptedBytes := []byte(validAddr)
		// Change character to an alternate character in bech32 charset
		if corruptedBytes[pos] == 'q' {
			corruptedBytes[pos] = 'p'
		} else {
			corruptedBytes[pos] = 'q'
		}
		corruptedAddr := string(corruptedBytes)

		if ValidateAddress(corruptedAddr) {
			t.Fatalf("ValidateAddress accepted corrupted address at pos %d: %s", pos, corruptedAddr)
		}
		if _, _, err := Bech32Decode(corruptedAddr); err == nil {
			t.Fatalf("Bech32Decode accepted corrupted address at pos %d: %s", pos, corruptedAddr)
		}
	}
}
