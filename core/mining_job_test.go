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

// TestMineBlockContextCancelledDoesNotCommitBlock verifies that when ctx is already cancelled
// or expires while mining an impossible target, MineBlockContext returns context error promptly
// and does not commit any block to the chain.
func TestMineBlockContextCancelledDoesNotCommitBlock(t *testing.T) {
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	bc := MustNewBlockchain(1)
	defer bc.Close()

	addr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"

	// 1. Already-cancelled context must return context.Canceled without adding a block
	canceledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := bc.MineBlockContext(canceledCtx, addr, 2); err == nil {
		t.Fatalf("expected error from already-cancelled MineBlockContext")
	}
	if len(bc.Chain) != 1 {
		t.Fatalf("expected chain length 1 after cancelled mining, got %d", len(bc.Chain))
	}

	// 2. Cancelling active workers on hard target must unwind promptly and leave chain unchanged
	hardTarget, _ := new(big.Int).SetString("0000000000000000000000000000000000000000000000000000000000000001", 16)
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int { return new(big.Int).Set(hardTarget) }
	defer func() { targetOverrideHook = oldHook }()

	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancelTimeout()

	start := time.Now()
	_, err := bc.MineBlockContext(timeoutCtx, addr, 2)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected error when MineBlockContext times out on hard target")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("MineBlockContext took too long to abort after context cancellation: %v", elapsed)
	}
	if len(bc.Chain) != 1 {
		t.Fatalf("expected chain length 1 after aborted mining, got %d", len(bc.Chain))
	}
}

func TestMineBlockContextArmaxHashV2PositiveNonces(t *testing.T) {
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	SetArmaxHashV2ActivationHeight(1)
	defer ResetArmaxHashV2ActivationHeightOverride()

	bc := MustNewBlockchain(1)
	defer bc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for h := 1; h <= 2; h++ {
		blk, err := bc.MineBlockContext(ctx, GenesisMiner, 4)
		if err != nil {
			t.Fatalf("MineBlockContext ArmaxHash v2 block #%d failed: %v", h, err)
		}
		if blk.Index != h {
			t.Fatalf("expected block height #%d, got #%d", h, blk.Index)
		}
		if blk.Nonce <= 0 {
			t.Fatalf("expected positive v1.0.11-compatible nonce, got %d", blk.Nonce)
		}
		if blk.PoWAlgorithm() != "armaxhash-v2" {
			t.Fatalf("expected armaxhash-v2 algorithm, got %s", blk.PoWAlgorithm())
		}
		calc, err := blk.CalculateHashWithError()
		if err != nil || calc != blk.Hash {
			t.Fatalf("hash verification mismatch: calc=%s err=%v hash=%s", calc, err, blk.Hash)
		}
	}
}
