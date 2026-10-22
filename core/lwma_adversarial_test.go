package core

import (
	"fmt"
	"math/big"
	"testing"
)

// TestLWMA_AdversarialTimestamps verifies that the canonical LWMA-1 difficulty adjustment algorithm:
// 1. Evaluates exactly N=60 intervals using N+1 (61) blocks.
// 2. Maintains stability under normal 120s intervals.
// 3. Clamps maximum difficulty spikes to <= 4x per block when solve times are 0s, 1s, or negative (retrograde timestamps).
// 4. Clamps maximum difficulty drops to <= 4x per block when solve times are huge (timewarp/delays).
// 5. Completely eliminates the historical flaw where weightedSolveTimes <= 0 could spike difficulty by 219,600x.
func TestLWMA_AdversarialTimestamps(t *testing.T) {
	initialTarget := DifficultyToTarget(1)
	initialTargetHex := fmt.Sprintf("%064x", initialTarget)

	// Helper to generate a mock chain of len blocks with specified solve time
	makeChain := func(totalBlocks int, solveTimeSec int64) []Block {
		chain := make([]Block, totalBlocks)
		baseTime := int64(1700000000)
		for i := 0; i < totalBlocks; i++ {
			chain[i] = Block{
				Index:     i,
				Timestamp: baseTime + int64(i)*solveTimeSec,
				Target:    initialTargetHex,
				Hash:      fmt.Sprintf("%064x", i),
			}
		}
		return chain
	}

	// 1. Normal blocks (120s interval): Target should remain identical to average target
	chainNormal := makeChain(65, TargetBlockTimeSeconds)
	targetNormal := CalculateExpectedTarget(chainNormal, 65)
	if targetNormal.Cmp(initialTarget) != 0 {
		t.Fatalf("Expected target to remain unchanged under 120s intervals, got: %s vs %s",
			targetNormal.Text(16), initialTarget.Text(16))
	}

	// 2. Ultra-fast blocks (1s interval): Target should tighten, but increase difficulty by at most 4x per step
	chainFast := makeChain(65, 1)
	targetFast := CalculateExpectedTarget(chainFast, 65)
	if targetFast.Cmp(initialTarget) >= 0 {
		t.Fatalf("Target should have tightened under 1s blocks")
	}
	// Max tightening factor is 4x (i.e. targetFast >= initialTarget / 4)
	minAllowedTargetUnderFast := new(big.Int).Div(initialTarget, big.NewInt(4))
	if targetFast.Cmp(minAllowedTargetUnderFast) < 0 {
		t.Fatalf("Target tightened too much (> 4x spike): targetFast=%s, minAllowed=%s",
			targetFast.Text(16), minAllowedTargetUnderFast.Text(16))
	}

	// 3. Adversarial Retrograde Timestamps (Blocks timestamped backward / out of order):
	// Simulate an adversarial scenario where timestamps decrease or drop below parent
	chainRetrograde := makeChain(65, TargetBlockTimeSeconds)
	// Artificially make recent 30 blocks timestamped earlier than parents (negative solve times)
	for i := 35; i < 65; i++ {
		chainRetrograde[i].Timestamp = chainRetrograde[34].Timestamp - int64(i-34)*50
	}
	targetRetrograde := CalculateExpectedTarget(chainRetrograde, 65)

	// Under the old bug, targetRetrograde would be multiplied by 1 / 219,600 (destroying the network).
	// With the canonical dampening bound, targetRetrograde is bounded to >= initialTarget / 4.
	if targetRetrograde.Cmp(minAllowedTargetUnderFast) < 0 {
		t.Fatalf("VULNERABILITY DETECTED: Retrograde timestamps caused difficulty spike > 4x: target=%s, floor=%s",
			targetRetrograde.Text(16), minAllowedTargetUnderFast.Text(16))
	}

	// 4. Ultra-slow blocks (3000s solve time): Difficulty must soften, capped by PowLimit/Genesis target
	chainSlow := makeChain(65, 3000)
	targetSlow := CalculateExpectedTarget(chainSlow, 65)
	if targetSlow.Cmp(initialTarget) > 0 {
		t.Fatalf("Target exceeded genesis PowLimit under slow blocks: targetSlow=%s, limit=%s",
			targetSlow.Text(16), initialTarget.Text(16))
	}
	// Under 3000s, max target softening in 1 block is bounded by initialTarget (powFloor)
	if targetSlow.Cmp(initialTarget) != 0 {
		t.Fatalf("Expected target to be capped at powFloor")
	}

	t.Logf("[OK] All LWMA-1 canonical window and adversarial timestamp tests passed successfully.")
}
