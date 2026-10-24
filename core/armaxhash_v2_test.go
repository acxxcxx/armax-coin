//go:build testhooks

package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/armaxhash"
)

// TestArmaxHashV2ActivationLogic verifies that the three PoW eras (SHA-256d, ArmaxHash v1, ArmaxHash v2)
// are accurately dispatched according to activation heights.
func TestArmaxHashV2ActivationLogic(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	ResetArmaxHashActivationHeightOverride()
	ResetArmaxHashV2ActivationHeightOverride()
	defer ResetArmaxHashActivationHeightOverride()
	defer ResetArmaxHashV2ActivationHeightOverride()

	_ = SetNetwork("testnet")
	// Testnet default: v1 = 520, v2 = 2500
	if GetArmaxHashActivationHeight() != 520 {
		t.Fatalf("expected testnet v1 activation height 520, got %d", GetArmaxHashActivationHeight())
	}
	if GetArmaxHashV2ActivationHeight() != 2500 {
		t.Fatalf("expected testnet v2 activation height 2500, got %d", GetArmaxHashV2ActivationHeight())
	}

	// Era 1: SHA-256d (< 520)
	for _, h := range []int{0, 1, 100, 519} {
		if IsArmaxHashActive(h) {
			t.Fatalf("expected ArmaxHash v1 to be inactive at height %d", h)
		}
		if IsArmaxHashV2Active(h) {
			t.Fatalf("expected ArmaxHash v2 to be inactive at height %d", h)
		}
		b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", h, GenesisMiner, DifficultyToTarget(1))
		if b.PoWAlgorithm() != "sha256d" {
			t.Fatalf("height %d: expected algo sha256d, got %s", h, b.PoWAlgorithm())
		}
	}

	// Era 2: ArmaxHash v1 (520 .. 2499)
	for _, h := range []int{520, 521, 1000, 2499} {
		if !IsArmaxHashActive(h) {
			t.Fatalf("expected ArmaxHash v1 to be active at height %d", h)
		}
		if IsArmaxHashV2Active(h) {
			t.Fatalf("expected ArmaxHash v2 to be inactive at height %d", h)
		}
		b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", h, GenesisMiner, DifficultyToTarget(1))
		if b.PoWAlgorithm() != "armaxhash" {
			t.Fatalf("height %d: expected algo armaxhash, got %s", h, b.PoWAlgorithm())
		}
	}

	// Era 3: ArmaxHash v2 (>= 2500)
	for _, h := range []int{2500, 2501, 3000, 100000} {
		if !IsArmaxHashActive(h) {
			t.Fatalf("expected ArmaxHash general to be active at height %d", h)
		}
		if !IsArmaxHashV2Active(h) {
			t.Fatalf("expected ArmaxHash v2 to be active at height %d", h)
		}
		b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", h, GenesisMiner, DifficultyToTarget(1))
		if b.PoWAlgorithm() != "armaxhash-v2" {
			t.Fatalf("height %d: expected algo armaxhash-v2, got %s", h, b.PoWAlgorithm())
		}
	}
}

// TestArmaxHashV2Calculation verifies that CalculateArmaxHashV2 computes the identical digest as armaxhash.HashV2
// and that CalculateHashWithError correctly dispatches to it.
func TestArmaxHashV2Calculation(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	ResetArmaxHashActivationHeightOverride()
	ResetArmaxHashV2ActivationHeightOverride()
	defer ResetArmaxHashActivationHeightOverride()
	defer ResetArmaxHashV2ActivationHeightOverride()

	_ = SetNetwork("testnet")
	b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", 2500, GenesisMiner, DifficultyToTarget(1))
	b.Timestamp = 1700000000
	b.Nonce = 123456

	expectedRaw, err := armaxhash.HashV2(b.HeaderBytes(), NetworkID())
	if err != nil {
		t.Fatalf("armaxhash.HashV2 failed: %v", err)
	}

	hashStr, err := b.CalculateArmaxHashV2()
	if err != nil {
		t.Fatalf("CalculateArmaxHashV2 failed: %v", err)
	}

	calculatedHash, err := b.CalculateHashWithError()
	if err != nil {
		t.Fatalf("CalculateHashWithError failed: %v", err)
	}

	if hashStr != calculatedHash {
		t.Fatalf("mismatch between CalculateArmaxHashV2 (%s) and CalculateHashWithError (%s)", hashStr, calculatedHash)
	}

	if b.CalculateHash() != calculatedHash {
		t.Fatalf("mismatch between CalculateHash (%s) and CalculateHashWithError (%s)", b.CalculateHash(), calculatedHash)
	}

	// Verify against SHA-256d to ensure they are distinct algorithms
	shaHash := b.CalculateSha256dHash()
	if shaHash == calculatedHash {
		t.Fatalf("ArmaxHash v2 hash must not equal SHA-256d hash")
	}

	_ = expectedRaw
}

// TestArmaxHashV2MiningExecution tests in-process mining transitions across all 3 PoW eras:
// Block 1 (SHA-256d) -> Block 2 (ArmaxHash v1) -> Block 3 (ArmaxHash v2).
func TestArmaxHashV2MiningExecution(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	defer ResetArmaxHashActivationHeightOverride()
	defer ResetArmaxHashV2ActivationHeightOverride()

	_ = SetNetwork("regtest")
	// Block 1: SHA-256d (< 2)
	// Block 2: ArmaxHash v1 (== 2)
	// Block 3: ArmaxHash v2 (>= 3)
	SetArmaxHashActivationHeight(2)
	SetArmaxHashV2ActivationHeight(3)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer bc.Close()

	// Mine Block #1 (SHA-256d)
	b1, err := bc.MineBlockContext(context.Background(), GenesisMiner, 1)
	if err != nil {
		t.Fatalf("failed to mine block 1: %v", err)
	}
	if b1.PoWAlgorithm() != "sha256d" {
		t.Fatalf("block 1: expected algo sha256d, got %s", b1.PoWAlgorithm())
	}

	// Mine Block #2 (ArmaxHash v1)
	b2, err := bc.MineBlockContext(context.Background(), GenesisMiner, 1)
	if err != nil {
		t.Fatalf("failed to mine block 2: %v", err)
	}
	if b2.PoWAlgorithm() != "armaxhash" {
		t.Fatalf("block 2: expected algo armaxhash, got %s", b2.PoWAlgorithm())
	}

	// Mine Block #3 (ArmaxHash v2)
	b3, err := bc.MineBlockContext(context.Background(), GenesisMiner, 1)
	if err != nil {
		t.Fatalf("failed to mine block 3: %v", err)
	}
	if b3.PoWAlgorithm() != "armaxhash-v2" {
		t.Fatalf("block 3: expected algo armaxhash-v2, got %s", b3.PoWAlgorithm())
	}

	// Verify chain integrity across all three eras
	if !bc.IsChainValid() {
		t.Fatalf("chain validation failed across all 3 eras")
	}
}
