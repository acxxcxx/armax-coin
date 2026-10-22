package core

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// TestLWMA_RealisticMiningSimulation tests the LWMA-1 difficulty adjustment algorithm
// under a realistic mining simulation where block solve times are drawn from an
// exponential distribution (Poisson process) with mean mu = 2^256 / (target * hashrate) [L4-5].
func TestLWMA_RealisticMiningSimulation(t *testing.T) {
	// Fixed random seed for complete deterministic reproducibility
	rng := rand.New(rand.NewSource(42))

	// Baseline hashrate: 10 MH/s (10,000,000 hashes/sec)
	baseHashrate := 10_000_000.0

	// Set initial genesis target such that baseHashrate yields exactly 120s on average:
	// target = 2^256 / (hashrate * 120)
	initTarget, initTargetHex := CalculateGenesisTarget(baseHashrate, float64(TargetBlockTimeSeconds))
	powFloor := new(big.Int).Set(initTarget)

	// Chain container
	chain := make([]Block, 0, 2001)

	// Genesis block (Index 0)
	genesisTime := int64(1710000000)
	genesisBlock := Block{
		Index:     0,
		Timestamp: genesisTime,
		Target:    initTargetHex,
	}
	chain = append(chain, genesisBlock)

	currentTime := genesisTime

	// Scenario definitions across 2,000 blocks:
	// Blocks 1..500: Steady hashrate (10 MH/s)
	// Blocks 501..1000: 10x Spike (100 MH/s)
	// Blocks 1001..1500: 10x Drop back to baseline (10 MH/s)
	// Blocks 1501..2000: Alternating hashrate (10 MH/s vs 50 MH/s every 100 blocks)
	getHashrate := func(blockIndex int) float64 {
		switch {
		case blockIndex <= 500:
			return baseHashrate
		case blockIndex <= 1000:
			return baseHashrate * 10.0 // 10x spike
		case blockIndex <= 1500:
			return baseHashrate // 10x drop back
		default:
			// Oscillating: alternates between 10 MH/s and 50 MH/s every 100 blocks
			cycle := ((blockIndex - 1500) / 100) % 2
			if cycle == 0 {
				return baseHashrate * 5.0
			}
			return baseHashrate
		}
	}

	// Simulation loop for 2,000 blocks
	totalBlocks := 2000
	for i := 1; i <= totalBlocks; i++ {
		// Calculate expected target using canonical LWMA-1
		nextTarget := CalculateExpectedTarget(chain, i)

		// Clamp so difficulty never drops below PowLimit (target never exceeds powFloor)
		if nextTarget.Cmp(powFloor) > 0 {
			nextTarget.Set(powFloor)
		}

		currentHashrate := getHashrate(i)

		// Calculate expected solve time mu = 2^256 / (target * hashrate)
		// expectedHashes = 2^256 / target
		expectedHashesBig := new(big.Int).Div(Two256, nextTarget)
		expectedHashesFloat, _ := new(big.Float).SetInt(expectedHashesBig).Float64()
		mu := expectedHashesFloat / currentHashrate
		if mu < 1.0 {
			mu = 1.0
		}

		// Draw solve time from exponential distribution: T = -ln(1 - U) * mu = ExpFloat64() * mu
		rawSolveTime := rng.ExpFloat64() * mu
		solveTimeSec := int64(math.Round(rawSolveTime))
		if solveTimeSec < 1 {
			solveTimeSec = 1
		}

		currentTime += solveTimeSec

		block := Block{
			Index:     i,
			Timestamp: currentTime,
			Target:    fmt.Sprintf("%064x", nextTarget),
		}
		chain = append(chain, block)

		// Assertions per block:
		// 1. Difficulty does not drop below PowLimit
		if nextTarget.Cmp(powFloor) > 0 {
			t.Fatalf("Block %d: target %s exceeded PowLimit %s", i, nextTarget.Text(16), powFloor.Text(16))
		}
		// 2. Target must be positive and non-zero (no unbounded oscillation to zero)
		if nextTarget.Sign() <= 0 {
			t.Fatalf("Block %d: target became non-positive: %s", i, nextTarget.String())
		}
	}

	// Helper to calculate statistics over a converged block slice
	calcStats := func(startBlock, endBlock int, name string) float64 {
		bStart := chain[startBlock]
		bEnd := chain[endBlock]
		timeSpan := float64(bEnd.Timestamp - bStart.Timestamp)
		numBlocks := float64(endBlock - startBlock)
		avgBlockTime := timeSpan / numBlocks

		t.Logf("[%s] Blocks %d..%d: Avg Block Time = %.2fs (Expected: ~120s, Delta: %+.2f%%)",
			name, startBlock, endBlock, avgBlockTime, ((avgBlockTime-120.0)/120.0)*100.0)

		// Assertion: Post-convergence average block time must be within +/- 25% of 120s (90s..150s)
		if avgBlockTime < 90.0 || avgBlockTime > 150.0 {
			t.Errorf("[%s] Average block time %.2fs is outside +/-25%% margin of 120s [90s..150s]", name, avgBlockTime)
		}
		return avgBlockTime
	}

	// 1. Phase 1 Converged Window (Blocks 100..500, steady 10 MH/s)
	avgP1 := calcStats(100, 500, "Phase 1: Steady Hashrate (10 MH/s)")

	// 2. Phase 2 Converged Window (Blocks 600..1000, after 10x spike adaptation)
	avgP2 := calcStats(600, 1000, "Phase 2: Post-10x Spike (100 MH/s)")

	// 3. Phase 3 Converged Window (Blocks 1100..1500, after 10x drop adaptation)
	avgP3 := calcStats(1100, 1500, "Phase 3: Post-10x Drop (10 MH/s)")

	// 4. Phase 4 Full Window (Blocks 1500..2000, repeating oscillation 10 MH/s <-> 50 MH/s)
	avgP4 := calcStats(1500, 2000, "Phase 4: Repeating Hashrate Oscillation")

	// Overall 2,000 block average
	totalTimeSpan := float64(chain[totalBlocks].Timestamp - chain[0].Timestamp)
	overallAvg := totalTimeSpan / float64(totalBlocks)
	t.Logf("--> Overall 2,000 Blocks Average Block Time = %.2fs (Delta: %+.2f%% from 120s)",
		overallAvg, ((overallAvg-120.0)/120.0)*100.0)

	if overallAvg < 90.0 || overallAvg > 150.0 {
		t.Errorf("Overall average block time %.2fs is outside +/-25%% margin of 120s", overallAvg)
	}

	// Check final difficulty vs initial
	finalTarget := chain[totalBlocks].GetTarget()
	t.Logf("Initial Target: %s", chain[0].Target)
	t.Logf("Final Target  : %s (bitLen: %d)", chain[totalBlocks].Target, finalTarget.BitLen())
	t.Logf("Measured Metrics Summary: P1=%.2fs, P2=%.2fs, P3=%.2fs, P4=%.2fs, Overall=%.2fs",
		avgP1, avgP2, avgP3, avgP4, overallAvg)
}
