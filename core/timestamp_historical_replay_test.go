package core

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestT13_HistoricalReplayIgnoresFutureWallClock verifies that historical blocks
// replayed during storage load or IBD are not rejected by the 2-hour wall-clock limit,
// while real-time tip blocks are strictly validated [T13].
func TestT13_HistoricalReplayIgnoresFutureWallClock(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	diff1Target := DifficultyToTarget(1)
	diff1Hex := fmt.Sprintf("%064x", diff1Target)
	bc.Chain[0].Target = diff1Hex

	genesis := bc.Chain[0]
	miner := GenesisMiner
	target := CalculateExpectedTarget(bc.Chain, 1)

	// Construct block #1 with timestamp 3 hours in the future relative to wall clock
	futureTime := time.Now().Unix() + 3*3600
	b1 := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, genesis.Hash, 1, miner, target)
	b1.Timestamp = futureTime
	b1.Target = fmt.Sprintf("%064x", target)
	b1.Difficulty = TargetToDifficulty(target)
	for {
		b1.Hash = b1.CalculateHash()
		if HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	// 1. Real-time tip validation must strictly reject the block for being too far in future
	errTip := validateBlockContext(&b1, &genesis, bc.Chain, false)
	if errTip == nil || !strings.Contains(errTip.Error(), "too far in future") {
		t.Fatalf("Expected tip validation to reject future block, got: %v", errTip)
	}

	// 2. Historical replay validation must bypass wall clock and accept the block
	errHist := validateBlockContext(&b1, &genesis, bc.Chain, true)
	if errHist != nil {
		t.Fatalf("Expected historical replay validation to pass, got: %v", errHist)
	}
}
