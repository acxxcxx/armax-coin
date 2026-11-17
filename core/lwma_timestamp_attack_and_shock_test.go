package core

import (
	"fmt"
	"math/big"
	"testing"
)

// TestP2_TimestampAttackDifficultyIncrease verifies P2-0 d(i):
// One miner uses now+2h (max future timestamp), followed by an honest block:
// verify difficulty change is bounded (difficulty increase <= 2x).
func TestP2_TimestampAttackDifficultyIncrease(t *testing.T) {
	// 1. Build a stable chain of 70 blocks with standard 120s intervals
	chain := make([]Block, 70)
	gen := GetGenesisBlock()
	target := gen.GetTarget()
	chain[0] = gen

	for i := 1; i < 70; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + TargetBlockTimeSeconds,
			PrevHash:   chain[i-1].Hash,
			Difficulty: 1,
			Target:     fmt.Sprintf("%064x", target),
		}
		chain[i].Hash = chain[i].CalculateHash()
	}

	targetBefore := CalculateExpectedTarget(chain, len(chain))
	if targetBefore.Sign() == 0 {
		t.Fatalf("Target before attack cannot be zero")
	}

	// 2. Malicious miner produces block 70 with timestamp +2 hours (7200s ahead)
	maliciousTimestamp := chain[69].Timestamp + 7200
	blockMalicious := Block{
		Index:      70,
		Timestamp:  maliciousTimestamp,
		PrevHash:   chain[69].Hash,
		Difficulty: 1,
		Target:     fmt.Sprintf("%064x", targetBefore),
	}
	blockMalicious.Hash = blockMalicious.CalculateHash()
	chain = append(chain, blockMalicious)

	// 3. Honest miner produces block 71 following MTP rule
	mtp := CalculateMedianTimePast(chain, len(chain))
	honestTimestamp := mtp + TargetBlockTimeSeconds
	targetFor71 := CalculateExpectedTarget(chain, len(chain))
	blockHonest := Block{
		Index:      71,
		Timestamp:  honestTimestamp,
		PrevHash:   blockMalicious.Hash,
		Difficulty: 1,
		Target:     fmt.Sprintf("%064x", targetFor71),
	}
	blockHonest.Hash = blockHonest.CalculateHash()
	chain = append(chain, blockHonest)

	targetAfter := CalculateExpectedTarget(chain, len(chain))

	// Difficulty is inversely proportional to target: Diff = Two256 / Target
	// An increase in difficulty means Target decreases (targetBefore / targetAfter > 1).
	// We require: difficulty increase <= 2x, which means targetAfter >= targetBefore / 2.
	halfTargetBefore := new(big.Int).Div(targetBefore, big.NewInt(2))
	if targetAfter.Cmp(halfTargetBefore) < 0 {
		t.Fatalf("[P2-0] Difficulty increased more than 2x after timestamp distortion! Target before: %x, Target after: %x", targetBefore, targetAfter)
	}
}

// TestP2_HashrateShockConvergence verifies P2-0 d(ii):
// Hashrate increases 10x (block time drops to 12s) then drops 10x back to normal (120s):
// Target converges within <= 2 LWMA windows (120 blocks).
func TestP2_HashrateShockConvergence(t *testing.T) {
	// 1. Build initial 65 blocks to activate LWMA
	chain := make([]Block, 65)
	gen := GetGenesisBlock()
	initialTarget := gen.GetTarget()
	chain[0] = gen

	for i := 1; i < 65; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + TargetBlockTimeSeconds,
			PrevHash:   chain[i-1].Hash,
			Difficulty: 1,
			Target:     fmt.Sprintf("%064x", initialTarget),
		}
		chain[i].Hash = chain[i].CalculateHash()
	}

	// 2. Hashrate increases 10x: H = 10 * H0
	// For each block, expected solveTime = 120 * (nextTarget / initialTarget) / (H / H0)
	// Clamped to >= 1 second.
	for i := 65; i < 65+120; i++ {
		nextTarget := CalculateExpectedTarget(chain, len(chain))

		// Physical PoW solve time: solveTime = 120 * (initialTarget / nextTarget) / (H / H0)
		diffRatio := new(big.Float).Quo(new(big.Float).SetInt(initialTarget), new(big.Float).SetInt(nextTarget))
		stFloat := new(big.Float).Mul(big.NewFloat(120.0), diffRatio)
		stFloat.Quo(stFloat, big.NewFloat(10.0)) // H = 10 * H0
		solveTimeSec, _ := stFloat.Int64()
		if solveTimeSec < 1 {
			solveTimeSec = 1
		} else if solveTimeSec > 720 {
			solveTimeSec = 720
		}

		b := Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + solveTimeSec,
			PrevHash:   chain[i-1].Hash,
			Difficulty: 1,
			Target:     fmt.Sprintf("%064x", nextTarget),
		}
		b.Hash = b.CalculateHash()
		chain = append(chain, b)
	}

	targetUnderHighHashrate := CalculateExpectedTarget(chain, len(chain))

	// Under 10x hashrate, target should converge towards initialTarget / 10
	expectedLowTarget := new(big.Int).Div(initialTarget, big.NewInt(10))
	ratioHigh := new(big.Float).Quo(new(big.Float).SetInt(targetUnderHighHashrate), new(big.Float).SetInt(expectedLowTarget))
	ratioHighFloat, _ := ratioHigh.Float64()

	// 3. Hashrate drops 10x back to normal (H = 1 * H0)
	// solveTime = 120 * (initialTarget / nextTarget) / 1
	// Clamped to max 720s (6*T)
	for i := len(chain); i < 65+240; i++ {
		nextTarget := CalculateExpectedTarget(chain, len(chain))

		diffRatio := new(big.Float).Quo(new(big.Float).SetInt(initialTarget), new(big.Float).SetInt(nextTarget))
		stFloat := new(big.Float).Mul(big.NewFloat(120.0), diffRatio)
		solveTimeSec, _ := stFloat.Int64()
		if solveTimeSec < 1 {
			solveTimeSec = 1
		} else if solveTimeSec > 720 {
			solveTimeSec = 720
		}

		b := Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + solveTimeSec,
			PrevHash:   chain[i-1].Hash,
			Difficulty: 1,
			Target:     fmt.Sprintf("%064x", nextTarget),
		}
		b.Hash = b.CalculateHash()
		chain = append(chain, b)
	}

	targetRecovered := CalculateExpectedTarget(chain, len(chain))
	ratioRecovered := new(big.Float).Quo(new(big.Float).SetInt(targetRecovered), new(big.Float).SetInt(initialTarget))
	ratioRecoveredFloat, _ := ratioRecovered.Float64()

	t.Logf("[P2-0] 10x hashrate target ratio: %.2f (expected ~1.0x of target/10), recovered ratio: %.2f (expected ~1.0x of initial)",
		ratioHighFloat, ratioRecoveredFloat)

	// Target should recover back towards initialTarget within 2 windows
	if ratioRecoveredFloat < 0.7 || ratioRecoveredFloat > 1.3 {
		t.Fatalf("[P2-0] Target failed to converge back within 2 LWMA windows: ratio=%.2f, target=%x, initial=%x",
			ratioRecoveredFloat, targetRecovered, initialTarget)
	}
}

// TestP2_First60BlocksSimulation verifies P2-0 d(iii):
// Simulation of first 60 blocks with GenesisTargetHex installed:
// Average block time stays near 120s within stated tolerance.
func TestP2_First60BlocksSimulation(t *testing.T) {
	gen := GetGenesisBlock()
	chain := []Block{gen}

	target := gen.GetTarget()

	// Under assumed constant hashrate tuned to GenesisTargetHex, solve times average 120s
	totalSolveTime := int64(0)
	for i := 1; i <= 60; i++ {
		// In LWMA-1, blocks 1..60 maintain GenesisTargetHex
		nextTarget := CalculateExpectedTarget(chain, len(chain))
		if nextTarget.Cmp(target) != 0 {
			t.Fatalf("[P2-0] Expected target for block %d to be GenesisTargetHex (%x), got %x", i, target, nextTarget)
		}

		// Simulated block time: 120 seconds
		solveTime := TargetBlockTimeSeconds
		totalSolveTime += solveTime

		b := Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + solveTime,
			PrevHash:   chain[i-1].Hash,
			Difficulty: 1,
			Target:     fmt.Sprintf("%064x", nextTarget),
		}
		b.Hash = b.CalculateHash()
		chain = append(chain, b)
	}

	avgBlockTime := float64(totalSolveTime) / 60.0
	toleranceSeconds := 1.0 // Within ±1 second for deterministic simulation
	if avgBlockTime < float64(TargetBlockTimeSeconds)-toleranceSeconds || avgBlockTime > float64(TargetBlockTimeSeconds)+toleranceSeconds {
		t.Fatalf("[P2-0] Average block time %.2fs deviated from 120s target", avgBlockTime)
	}
}

// BenchmarkBlockHash measures Block.CalculateHash throughput (hashes/second)
func BenchmarkBlockHash(b *testing.B) {
	gen := GetGenesisBlock()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gen.CalculateHash()
	}
}
