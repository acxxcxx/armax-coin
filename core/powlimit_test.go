//go:build testhooks

package core

import (
	"encoding/hex"
	"math/big"
	"testing"
)

func TestPowLimitPerNetwork(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)

	// 1. Verify Mainnet PowLimit
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet: %v", err)
	}
	mainnetLimit := GetPowLimit()
	mainnetLimitBytes, _ := hex.DecodeString(GenesisTargetHex)
	expectedMainnetLimit := new(big.Int).SetBytes(mainnetLimitBytes)
	if mainnetLimit.Cmp(expectedMainnetLimit) != 0 {
		t.Fatalf("Mainnet PowLimit mismatch: expected %s, got %x", GenesisTargetHex, mainnetLimit)
	}
	if TargetToDifficulty(mainnetLimit) != 1 {
		t.Fatalf("Expected Mainnet PowLimit difficulty to be 1, got %d", TargetToDifficulty(mainnetLimit))
	}

	// 2. Verify Testnet PowLimit
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet: %v", err)
	}
	testnetLimit := GetPowLimit()
	testnetLimitBytes, _ := hex.DecodeString(TestnetGenesisTargetHex)
	expectedTestnetLimit := new(big.Int).SetBytes(testnetLimitBytes)
	if testnetLimit.Cmp(expectedTestnetLimit) != 0 {
		t.Fatalf("Testnet PowLimit mismatch: expected %s, got %x", TestnetGenesisTargetHex, testnetLimit)
	}
	if TargetToDifficulty(testnetLimit) != 7 {
		t.Fatalf("Expected Testnet PowLimit difficulty to be 7, got %d", TargetToDifficulty(testnetLimit))
	}

	// 3. Verify that the two networks' limits are distinct
	if mainnetLimit.Cmp(testnetLimit) == 0 {
		t.Fatalf("Mainnet and Testnet PowLimits must be distinct!")
	}
	if testnetLimit.Cmp(mainnetLimit) >= 0 {
		t.Fatalf("Testnet PowLimit must be strictly harder (lower target) than Mainnet: %x vs %x", testnetLimit, mainnetLimit)
	}

	// 4. Test that LWMA clamp prevents target from loosening beyond PowLimit [L3-4]
	// Create a chain with 65 blocks where timestamps are spaced extremely far apart (solve time >> TargetBlockTimeSeconds)
	genTestnet := GetTestnetGenesisBlock()
	chain := []Block{genTestnet}
	for i := 1; i <= 65; i++ {
		prev := chain[i-1]
		chain = append(chain, Block{
			Index:      i,
			Timestamp:  prev.Timestamp + 10000, // Very slow blocks -> algorithm attempts to loosen difficulty
			Target:     TestnetGenesisTargetHex,
			Difficulty: 7,
		})
	}

	calculatedTarget := CalculateExpectedTarget(chain, 66)
	if calculatedTarget.Cmp(testnetLimit) > 0 {
		t.Fatalf("LWMA target exceeded testnet PowLimit! Got %x, max allowed %x", calculatedTarget, testnetLimit)
	}
	if TargetToDifficulty(calculatedTarget) < 7 {
		t.Fatalf("Testnet difficulty dropped below limit 7! Got difficulty %d", TargetToDifficulty(calculatedTarget))
	}

	// Restore mainnet
	_ = SetNetwork("mainnet")
}
