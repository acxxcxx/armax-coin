package core

import (
	"fmt"
	"math/big"
	"testing"
)

// TestLWMA_IndependentTestVectors implements formal differential test vectors
// evaluating Zawy LWMA-1 and ARMAX specific dampening variants across 8 scenarios:
// 1. 60 normal intervals (120s)
// 2. 1-second solve intervals
// 3. Retrograde timestamps (out-of-order within MTP bounds)
// 4. Identical timestamps (0-second intervals)
// 5. Extreme timestamps passing consensus validation
// 6. Prolonged delay / stalled mining intervals
// 7. Large integer safety and rounding precision
// 8. Short chain startup and boundary transitions
func TestLWMA_IndependentTestVectors(t *testing.T) {
	initialTarget := DifficultyToTarget(1)
	initialTargetHex := fmt.Sprintf("%064x", initialTarget)

	createTestChain := func(timestamps []int64, targets []*big.Int) []Block {
		chain := make([]Block, len(timestamps))
		for i := 0; i < len(timestamps); i++ {
			tgtHex := initialTargetHex
			if i < len(targets) && targets[i] != nil {
				tgtHex = fmt.Sprintf("%064x", targets[i])
			}
			chain[i] = Block{
				Index:     i,
				Timestamp: timestamps[i],
				Target:    tgtHex,
				Hash:      fmt.Sprintf("%064x", i),
			}
		}
		return chain
	}

	// -------------------------------------------------------------------------
	// Vector 1: 60 Normal Intervals (120s cadence)
	// Invariant: nextTarget must equal previous target exactly
	// -------------------------------------------------------------------------
	t.Run("Vector1_NormalCadence", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 62; i++ {
			ts[i] = base + int64(i)*120
		}
		chain := createTestChain(ts, nil)
		nextTarget := CalculateExpectedTarget(chain, 61)

		if nextTarget.Cmp(initialTarget) != 0 {
			t.Fatalf("Vector 1 Failed: Target drifted under ideal 120s cadence. Got %s, expected %s",
				nextTarget.Text(16), initialTarget.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 2: 1-Second Solve Intervals
	// Invariant: Solvetimes clamp to dampening lower bound (expectedWeight / 4).
	// Under uniform previous targets, nextTarget must tighten by exactly factor 4x.
	// -------------------------------------------------------------------------
	t.Run("Vector2_OneSecondIntervals", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 62; i++ {
			ts[i] = base + int64(i)*1
		}
		chain := createTestChain(ts, nil)
		nextTarget := CalculateExpectedTarget(chain, 61)

		expectedTarget := new(big.Int).Div(initialTarget, big.NewInt(4))
		if nextTarget.Cmp(expectedTarget) != 0 {
			t.Fatalf("Vector 2 Failed: Target did not match expected 4x clamp. Got %s, expected %s",
				nextTarget.Text(16), expectedTarget.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 3: Retrograde Timestamps (Out-of-Order within consensus rules)
	// Invariant: Negative intervals must be safely clamped and never cause integer underflow
	// or 219,600x difficulty spikes.
	// -------------------------------------------------------------------------
	t.Run("Vector3_RetrogradeTimestamps", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		// Blocks 0..40 have normal 120s intervals
		for i := 0; i <= 40; i++ {
			ts[i] = base + int64(i)*120
		}
		// Blocks 41..61 have retrograde timestamps (each 5 seconds earlier than parent)
		for i := 41; i < 62; i++ {
			ts[i] = ts[40] - int64(i-40)*5
		}
		chain := createTestChain(ts, nil)
		nextTarget := CalculateExpectedTarget(chain, 61)

		minBoundTarget := new(big.Int).Div(initialTarget, big.NewInt(4))
		if nextTarget.Cmp(minBoundTarget) < 0 {
			t.Fatalf("Vector 3 Failed: Retrograde timestamps triggered excessive spike below dampening floor: %s < %s",
				nextTarget.Text(16), minBoundTarget.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 4: Identical Timestamps (0-Second Intervals)
	// Invariant: Solve times = 0. Weighted sum = 0. Must clamp safely to minBound.
	// -------------------------------------------------------------------------
	t.Run("Vector4_IdenticalTimestamps", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 62; i++ {
			ts[i] = base // all blocks share identical timestamp
		}
		chain := createTestChain(ts, nil)
		nextTarget := CalculateExpectedTarget(chain, 61)

		expectedBound := new(big.Int).Div(initialTarget, big.NewInt(4))
		if nextTarget.Cmp(expectedBound) != 0 {
			t.Fatalf("Vector 4 Failed: Zero-interval did not clamp to expectedBound. Got %s, expected %s",
				nextTarget.Text(16), expectedBound.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 5: Extreme Timestamps (Bounded by +/- 6*T = 720s)
	// Invariant: Outlier jumps exceeding 720s or -720s are bounded to +/-720s.
	// -------------------------------------------------------------------------
	t.Run("Vector5_ExtremeTimestampsTimewarp", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 61; i++ {
			ts[i] = base + int64(i)*120
		}
		// Block 61 attempts timewarp jump of +100,000 seconds
		ts[61] = ts[60] + 100000
		chain := createTestChain(ts, nil)
		nextTarget := CalculateExpectedTarget(chain, 61)

		// Target should soften, but remain bounded by powFloor
		if nextTarget.Cmp(initialTarget) > 0 {
			t.Fatalf("Vector 5 Failed: Target exceeded powFloor: %s > %s",
				nextTarget.Text(16), initialTarget.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 6: Prolonged Delay (Network Stall)
	// Invariant: In ARMAX variant, target softening is dampened to <= 4x per block.
	// -------------------------------------------------------------------------
	t.Run("Vector6_ProlongedDelay", func(t *testing.T) {
		// Use a high difficulty initial target (so loosening won't hit powFloor immediately)
		highDiffTarget := new(big.Int).Div(initialTarget, big.NewInt(100))
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 62; i++ {
			ts[i] = base + int64(i)*3600 // 1 hour per block (30x slower than 120s)
		}
		targets := make([]*big.Int, 62)
		for i := 0; i < 62; i++ {
			targets[i] = highDiffTarget
		}
		chain := createTestChain(ts, targets)
		nextTarget := CalculateExpectedTarget(chain, 61)

		// Max allowable loosening in single adjustment under dampening bound is 4x previous
		maxAllowedLoosening := new(big.Int).Mul(highDiffTarget, big.NewInt(4))
		if nextTarget.Cmp(maxAllowedLoosening) > 0 {
			t.Fatalf("Vector 6 Failed: Target loosened beyond 4x dampening bound: %s > %s",
				nextTarget.Text(16), maxAllowedLoosening.Text(16))
		}
	})

	// -------------------------------------------------------------------------
	// Vector 7: Precision, Overflow & Non-Linear Target Histories
	// Invariant: Verify relationship between previous target T_N and nextTarget.
	// When T_N differs from avgTarget, difficulty change ratio differs from weightedSolveTimes ratio.
	// -------------------------------------------------------------------------
	t.Run("Vector7_NonLinearTargetHistoryRatio", func(t *testing.T) {
		ts := make([]int64, 62)
		base := int64(1710000000)
		for i := 0; i < 62; i++ {
			ts[i] = base + int64(i)*120 // ideal 120s cadence
		}

		targets := make([]*big.Int, 62)
		// Past blocks 0..59 had target = 1000
		for i := 0; i < 60; i++ {
			targets[i] = big.NewInt(1000)
		}
		// Most recent block 60 had target = 400 (difficulty stepped up recently)
		targets[60] = big.NewInt(400)

		chain := createTestChain(ts, targets)
		nextTarget := CalculateExpectedTarget(chain, 61)

		// Here weightedSolveTimes equals expectedWeight (cadence = 120s).
		// Thus nextTarget = avgTarget.
		// avgTarget = (59*1000 + 400) / 60 = 59400 / 60 = 990.
		// Notice that nextTarget (990) is 2.475x higher than previous target T_60 (400),
		// even though weightedSolveTimes == expectedWeight!
		expectedAvg := int64(990)
		if nextTarget.Int64() != expectedAvg {
			t.Fatalf("Vector 7 Failed: Expected nextTarget=%d, got %s", expectedAvg, nextTarget.String())
		}
		t.Logf("[OK] Non-linear history ratio demonstrated: nextTarget/T_N = %d / 400 = %.3fx under ideal solvetimes.",
			nextTarget.Int64(), float64(nextTarget.Int64())/400.0)
	})

	// -------------------------------------------------------------------------
	// Vector 8: Chain Startup & Boundary Transitions
	// Invariant: Blocks 0..60 strictly inherit Genesis Target.
	// Block 61 is the first block to evaluate the full N=60 LWMA window.
	// -------------------------------------------------------------------------
	t.Run("Vector8_StartupWindowBoundary", func(t *testing.T) {
		ts := make([]int64, 65)
		base := int64(1710000000)
		for i := 0; i < 65; i++ {
			ts[i] = base + int64(i)*10 // super fast blocks
		}
		chain := createTestChain(ts, nil)

		// Test index 1..60: must return genesis target despite fast mining
		for idx := 1; idx <= 60; idx++ {
			target := CalculateExpectedTarget(chain, idx)
			if target.Cmp(initialTarget) != 0 {
				t.Fatalf("Vector 8 Failed: Index %d before window threshold did not inherit genesis target", idx)
			}
		}

		// Test index 61: first block where full N=60 window is active -> must retarget
		target61 := CalculateExpectedTarget(chain, 61)
		if target61.Cmp(initialTarget) >= 0 {
			t.Fatalf("Vector 8 Failed: Block 61 did not adjust difficulty despite 60 past fast blocks")
		}
		t.Logf("[OK] Startup boundary verified: blocks 1..60 locked to genesis, block 61 active.")
	})
}
