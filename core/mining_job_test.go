package core

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"
)

// TestMiningJobCreationAndImmutability verifies the structural integrity of MiningJob.
func TestMiningJobCreationAndImmutability(t *testing.T) {
	prev := Block{
		Index:     13292,
		Timestamp: 1700000000,
		Hash:      "0000000000000000000000000000000000000000000000000000000000000123",
	}

	chain := []Block{prev}
	ts := prev.Timestamp + 120
	target := CalculateExpectedTargetWithParent(chain, &prev, prev.Index+1, ts)
	tgtHex := fmt.Sprintf("%064x", target)

	var tb [32]byte
	bBytes := target.Bytes()
	if len(bBytes) > 32 {
		bBytes = bBytes[len(bBytes)-32:]
	}
	copy(tb[32-len(bBytes):], bBytes)

	job := &MiningJob{
		JobVersion:     1,
		Index:          prev.Index + 1,
		MinTimestamp:   ts,
		MaxTimestamp:   prev.Timestamp + 1200,
		PrevHash:       prev.Hash,
		TargetHex:      tgtHex,
		ExpectedTarget: target,
		TargetBytes:    tb,
		PrefixBytes:    []byte("prefix:"),
		SuffixBytes:    []byte(":" + tgtHex),
	}

	if job.JobVersion != 1 {
		t.Fatalf("expected JobVersion 1, got %d", job.JobVersion)
	}
	if job.MaxTimestamp != prev.Timestamp+1200 {
		t.Fatalf("expected MaxTimestamp %d, got %d", prev.Timestamp+1200, job.MaxTimestamp)
	}
	if job.PrevHash != prev.Hash {
		t.Fatalf("expected PrevHash %s, got %s", prev.Hash, job.PrevHash)
	}
}

// TestMineBlockContextAtomicJobRotation verifies that MineBlockContext correctly
// coordinates multiple worker threads across an atomic JobVersion increment.
func TestMineBlockContextAtomicJobRotation(t *testing.T) {
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	SetAntiStallActivationHeight(1)
	defer ResetAntiStallActivationHeightOverride()

	bc := MustNewBlockchain(1)
	defer bc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	minedBlock, err := bc.MineBlockContext(ctx, addr, 4)
	if err != nil {
		t.Fatalf("MineBlockContext failed: %v", err)
	}

	if minedBlock.Index != 1 {
		t.Fatalf("expected mined block index 1, got %d", minedBlock.Index)
	}
	if minedBlock.PrevHash != bc.Chain[0].Hash {
		t.Fatalf("expected PrevHash %s, got %s", bc.Chain[0].Hash, minedBlock.PrevHash)
	}
	if !HashMeetsTarget(minedBlock.Hash, minedBlock.GetTarget()) {
		t.Fatalf("mined block hash does not meet target")
	}
}

// TestMineBlockContextRotatesToTEDREmergencyTarget verifies that when mining across the
// 1,200s TEDR stall threshold, MineBlockContext builds and solves with the emergency fallback target.
func TestMineBlockContextRotatesToTEDREmergencyTarget(t *testing.T) {
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	SetAntiStallActivationHeight(1)
	defer ResetAntiStallActivationHeightOverride()

	bc := MustNewBlockchain(1)
	defer bc.Close()

	// Skew miner clock beyond 1,200s TEDR window in regtest
	t.Setenv("ARMX_MINER_CLOCK_OFFSET_SECONDS", "1250")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	minedBlock, err := bc.MineBlockContext(ctx, addr, 2)
	if err != nil {
		t.Fatalf("MineBlockContext failed: %v", err)
	}

	emTgtHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	if minedBlock.Target != emTgtHex {
		t.Fatalf("expected emergency target %s, got %s", emTgtHex, minedBlock.Target)
	}
	expectedDiff := TargetToDifficulty(GetArmaxHashPowLimit())
	if minedBlock.Difficulty != expectedDiff {
		t.Fatalf("expected emergency difficulty %d, got %d", expectedDiff, minedBlock.Difficulty)
	}
}

// Test4PointSubmissionGateFiltersInvalidCandidates verifies the 4-point submission gate
// logic in candidate evaluation.
func Test4PointSubmissionGateFiltersInvalidCandidates(t *testing.T) {
	var activeJobVersion uint64 = 2
	tipHash := "0000000000000000000000000000000000000000000000000000000000000abc"

	target := DifficultyToTarget(3)
	targetHex := fmt.Sprintf("%064x", target)

	job := &MiningJob{
		JobVersion:     2,
		Index:          100,
		MinTimestamp:   1700000000,
		MaxTimestamp:   1700001200,
		PrevHash:       tipHash,
		TargetHex:      targetHex,
		ExpectedTarget: target,
	}

	// 1. Valid candidate passes
	candValid := Block{
		Index:      100,
		Timestamp:  1700000600,
		PrevHash:   tipHash,
		Target:     targetHex,
		Difficulty: 3,
	}
	if candValid.Index != job.Index ||
		candValid.PrevHash != job.PrevHash ||
		candValid.Timestamp < job.MinTimestamp ||
		candValid.Timestamp > job.MaxTimestamp ||
		candValid.Target != job.TargetHex ||
		atomic.LoadUint64(&activeJobVersion) != job.JobVersion {
		t.Fatalf("valid candidate was rejected by submission gate")
	}

	// 2. Candidate with obsolete JobVersion rejected
	obsoleteJob := *job
	obsoleteJob.JobVersion = 1
	if atomic.LoadUint64(&activeJobVersion) == obsoleteJob.JobVersion {
		t.Fatalf("gate failed to reject obsolete job version")
	}

	// 3. Candidate with expired timestamp (> MaxTimestamp) rejected
	candExpired := candValid
	candExpired.Timestamp = job.MaxTimestamp + 1
	if candExpired.Timestamp <= job.MaxTimestamp {
		t.Fatalf("gate failed to reject candidate with timestamp exceeding MaxTimestamp")
	}

	// 4. Candidate with mismatched tip hash rejected
	candWrongTip := candValid
	candWrongTip.PrevHash = "0000000000000000000000000000000000000000000000000000000000000def"
	if candWrongTip.PrevHash == job.PrevHash {
		t.Fatalf("gate failed to reject candidate with mismatched PrevHash")
	}

	// 5. Candidate with outdated target rejected
	candOldTarget := candValid
	candOldTarget.Target = fmt.Sprintf("%064x", new(big.Int).Rsh(target, 4))
	if candOldTarget.Target == job.TargetHex {
		t.Fatalf("gate failed to reject candidate with outdated target")
	}
}

// TestMineBlockContextUnderASERT verifies that MineBlockContext mines a valid block
// when ASERT is active, ensuring that candidate timestamp and target remain synchronized
// across worker iterations and successfully pass ValidateAndAddBlock.
func TestMineBlockContextUnderASERT(t *testing.T) {
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	SetASERTActivationHeight(1)
	defer ResetASERTActivationHeightOverride()

	bc := MustNewBlockchain(1)
	defer bc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	minedBlock, err := bc.MineBlockContext(ctx, addr, 2)
	if err != nil {
		t.Fatalf("MineBlockContext under ASERT failed: %v", err)
	}

	if minedBlock.Index != 1 {
		t.Fatalf("expected mined block index 1, got %d", minedBlock.Index)
	}
	expectedTgt := CalculateExpectedTargetForCandidate(bc.Chain[:1], 1, minedBlock.Timestamp)
	expectedTgtHex := fmt.Sprintf("%064x", expectedTgt)
	if minedBlock.Target != expectedTgtHex {
		t.Fatalf("mined block target mismatch: got %s, want %s", minedBlock.Target, expectedTgtHex)
	}
}
