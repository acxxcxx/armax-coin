//go:build testhooks

package core

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

// TestL1_6_CalculateGenesisTarget verifies that CalculateGenesisTarget correctly implements:
// target = 2^256 / (hashrate * 120) [L1-6]
func TestL1_6_CalculateGenesisTarget(t *testing.T) {
	// Test 1: Calculate target for 10 MH/s hashrate (10,000,000 hash/s) over 120 seconds
	hashrate := 10_000_000.0 // 10 MH/s
	targetInt, targetHex := CalculateGenesisTarget(hashrate, 120.0)

	if len(targetHex) != 64 {
		t.Fatalf("TargetHex length must be exactly 64 characters, got %d", len(targetHex))
	}

	// Calculate manually: expectedHashes = 10,000,000 * 120 = 1,200,000,000
	expectedHashes := big.NewInt(1_200_000_000)
	manualTarget := new(big.Int).Div(Two256, expectedHashes)

	if targetInt.Cmp(manualTarget) != 0 {
		t.Fatalf("CalculateGenesisTarget mismatch: got %x, expected %x", targetInt, manualTarget)
	}

	// Test 2: Verify TestnetGenesisTargetHex specification (true 28 zero bits: "0000000" + 57 x "f") [L4-8]
	expectedTestnetTargetHex := "0000000" + strings.Repeat("f", 57)
	if TestnetGenesisTargetHex != expectedTestnetTargetHex {
		t.Fatalf("TestnetGenesisTargetHex mismatch: got %s, expected %s", TestnetGenesisTargetHex, expectedTestnetTargetHex)
	}

	testnetTargetBytes, err := hex.DecodeString(TestnetGenesisTargetHex)
	if err != nil {
		t.Fatalf("Failed to decode TestnetGenesisTargetHex: %v", err)
	}
	testnetTargetInt := new(big.Int).SetBytes(testnetTargetBytes)

	// Verify leading zero bits = 28 (7 hex characters of 0 = 28 bits)
	zeroBits := 256 - testnetTargetInt.BitLen()
	if zeroBits != 28 {
		t.Fatalf("Expected exactly 28 leading zero bits, got %d", zeroBits)
	}
}

// TestL1_6_TestnetGenesisHashContent verifies that Testnet Genesis hash is strictly derived
// from its contents and satisfies TestnetGenesisTargetHex [L1-6]
func TestL1_6_TestnetGenesisHashContent(t *testing.T) {
	SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	gen := GetTestnetGenesisBlock()

	// 1. Invariant: Network Profile must be testnet
	if NetworkID() != "ARMX-TESTNET-1" {
		t.Fatalf("Expected NetworkID ARMX-TESTNET-1, got %s", NetworkID())
	}

	// 2. Invariant: Coinbase signature must be REWARD_SIG [L1-1]
	if len(gen.Transactions) != 1 {
		t.Fatalf("Expected exactly 1 coinbase transaction in testnet genesis, got %d", len(gen.Transactions))
	}
	cb := gen.Transactions[0]
	if cb.Signature != "REWARD_SIG" {
		t.Fatalf("Expected coinbase signature REWARD_SIG, got %s", cb.Signature)
	}

	// 3. Invariant: Merkle root matches transactions
	calcMerkle := CalculateMerkleRoot(gen.Transactions)
	if gen.MerkleRoot != calcMerkle {
		t.Fatalf("Merkle root mismatch: block has %s, calculated %s", gen.MerkleRoot, calcMerkle)
	}
	if gen.MerkleRoot != TestnetGenesisMerkleRoot {
		t.Fatalf("Merkle root constant mismatch: got %s, expected %s", gen.MerkleRoot, TestnetGenesisMerkleRoot)
	}

	// 4. Invariant: Target matches TestnetGenesisTargetHex
	if gen.Target != TestnetGenesisTargetHex {
		t.Fatalf("Target mismatch: got %s, expected %s", gen.Target, TestnetGenesisTargetHex)
	}

	// 5. Invariant: Hash computed from content matches TestnetGenesisHash
	calcHash := gen.CalculateHash()
	if calcHash != gen.Hash {
		t.Fatalf("Calculated hash %s does not match gen.Hash %s", calcHash, gen.Hash)
	}
	if calcHash != TestnetGenesisHash {
		t.Fatalf("Calculated hash %s does not match TestnetGenesisHash %s", calcHash, TestnetGenesisHash)
	}

	// 6. Invariant: Hash satisfies Proof of Work target ceiling
	if !HashMeetsTarget(calcHash, gen.GetTarget()) {
		t.Fatalf("Testnet Genesis hash %s does NOT meet target %s", calcHash, gen.Target)
	}

	// 7. Invariant: ValidateGenesisBlock returns nil
	if err := ValidateGenesisBlock(&gen); err != nil {
		t.Fatalf("ValidateGenesisBlock failed: %v", err)
	}
}

// TestL1_6_MainnetGenesisUntouched verifies that Mainnet Genesis is 100% UNCHANGED [L1-6]
func TestL1_6_MainnetGenesisUntouched(t *testing.T) {
	SetNetworkForTesting(true)
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	gen := GetMainnetGenesisBlock()

	if gen.Timestamp != 1710000000 {
		t.Fatalf("Mainnet timestamp changed: %d", gen.Timestamp)
	}
	if gen.Nonce != 7 {
		t.Fatalf("Mainnet nonce changed: %d", gen.Nonce)
	}
	if gen.Target != "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" {
		t.Fatalf("Mainnet target changed: %s", gen.Target)
	}
	if gen.MerkleRoot != "ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76" {
		t.Fatalf("Mainnet Merkle root changed: %s", gen.MerkleRoot)
	}
	if gen.Hash != "0820d42962071e01c8a7d2e6a3aa5640a17f8a60e9006bf59b6e76ac0001535e" {
		t.Fatalf("Mainnet hash changed: %s", gen.Hash)
	}
	if err := ValidateGenesisBlock(&gen); err != nil {
		t.Fatalf("ValidateGenesisBlock on mainnet failed: %v", err)
	}
}
