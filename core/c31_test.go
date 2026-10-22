package core

import (
	"fmt"
	"testing"
)

// TestC31_NoDifficulty1BypassForEarlyBlocks verifies that CalculateExpectedTarget
// does not blindly bypass difficulty to MaxTarget (Difficulty 1) for blocks 1..60 [C-31].
// It must inherit the genesis block's target (or PowLimit), preserving real PoW requirements.
func TestC31_NoDifficulty1BypassForEarlyBlocks(t *testing.T) {
	// Create a genesis block with custom realistic target (e.g. 4 leading zeros / difficulty 4)
	customDiff := 4
	customTarget := DifficultyToTarget(customDiff)
	customTargetHex := fmt.Sprintf("%064x", customTarget)

	customGenesis := GetGenesisBlock()
	customGenesis.Target = customTargetHex
	customGenesis.Difficulty = customDiff

	chain := []Block{customGenesis}

	// For heights 1 through 60, CalculateExpectedTarget must return customGenesis target, NOT DifficultyToTarget(1)
	for h := 1; h <= 60; h++ {
		expected := CalculateExpectedTarget(chain, h)
		if expected.Cmp(customTarget) != 0 {
			t.Fatalf("[C-31] FAILED at height %d: expected genesis target %s (diff %d), got %x (diff %d)",
				h, customTargetHex, customDiff, expected, TargetToDifficulty(expected))
		}
	}
}
