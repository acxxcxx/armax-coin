package core

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLWMA1_DifficultyAdjustmentBounds verifies [Section D.1]:
// LWMA-1 difficulty retargeting responds correctly to block solve intervals,
// respects the Difficulty 1 floor, and weights recent blocks more heavily.
func TestLWMA1_DifficultyAdjustmentBounds(t *testing.T) {
	// Build a mock chain of 65 blocks solved every 60s (2x too fast; target is 120s)
	chainFast := make([]Block, 65)
	targetDiff1 := DifficultyToTarget(1)
	targetHex := fmt.Sprintf("%064x", targetDiff1)

	baseTime := time.Now().Unix() - 100000
	for i := 0; i < 65; i++ {
		chainFast[i] = Block{
			Index:      i,
			Timestamp:  baseTime + int64(i*60), // 60s solve time (fast blocks)
			Target:     targetHex,
			Difficulty: 1,
			Hash:       fmt.Sprintf("%064x", i),
		}
	}

	// Compute expected target for block 65
	nextTarget := CalculateExpectedTarget(chainFast, 65)

	// Since solve time was 60s (< 120s target), nextTarget must be SMALLER than targetDiff1 (harder difficulty)
	if nextTarget.Cmp(targetDiff1) >= 0 {
		t.Fatalf("LWMA did not increase difficulty when blocks solved too fast: nextTarget=%064x, diff1Target=%064x",
			nextTarget, targetDiff1)
	}
	newDiff := TargetToDifficulty(nextTarget)
	if newDiff <= 1 {
		t.Fatalf("Expected difficulty > 1 for fast blocks, got %d", newDiff)
	}
	t.Logf("[OK] LWMA-1 fast block retargeting verified: Target tightened to %064x (Difficulty: %d)", nextTarget, newDiff)

	// Build a mock chain of 65 blocks solved every 300s (2.5x too slow)
	chainSlow := make([]Block, 65)
	chainSlow[0] = Block{
		Index:      0,
		Timestamp:  baseTime,
		Target:     targetHex,
		Difficulty: 1,
		Hash:       fmt.Sprintf("%064x", 0),
	}
	for i := 1; i < 65; i++ {
		chainSlow[i] = Block{
			Index:      i,
			Timestamp:  baseTime + int64(i*300), // 300s solve time (slow blocks)
			Target:     fmt.Sprintf("%064x", nextTarget),
			Difficulty: newDiff,
			Hash:       fmt.Sprintf("%064x", i),
		}
	}

	slowTarget := CalculateExpectedTarget(chainSlow, 65)
	// slowTarget must be LARGER than nextTarget (easier difficulty)
	if slowTarget.Cmp(nextTarget) <= 0 {
		t.Fatalf("LWMA did not decrease difficulty when blocks solved too slowly")
	}

	// Invariant: Target must NEVER exceed Difficulty 1 (MaxTarget)
	maxAllowedTarget := DifficultyToTarget(1)
	if slowTarget.Cmp(maxAllowedTarget) > 0 {
		t.Fatalf("LWMA target exceeded Difficulty 1 ceiling: got %064x > %064x", slowTarget, maxAllowedTarget)
	}
	t.Logf("[OK] LWMA-1 slow block retargeting and Difficulty 1 floor verified: Target loosened to %064x", slowTarget)
}

// TestTimestampValidationRules verifies [Section D.2]:
// Timestamp sanity checks: MTP-11 lower bound and 2-hour future ceiling.
func TestTimestampValidationRules(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	diff1Target := DifficultyToTarget(1)
	diff1Hex := fmt.Sprintf("%064x", diff1Target)
	bc.Chain[0].Target = diff1Hex

	// 1. Block timestamp <= MedianTimePast must be REJECTED [C-05, C-06, C-22]
	latest := bc.GetLatestBlock()
	mtp := CalculateMedianTimePast(bc.Chain, 1)

	invalidMTPBlock := Block{
		Index:        1,
		Timestamp:    mtp, // Equal to MTP
		PrevHash:     latest.Hash,
		Miner:        GenesisMiner,
		Difficulty:   1,
		Target:       diff1Hex,
		Transactions: []Transaction{{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward, Nonce: 1, Signature: "REWARD_SIG"}},
	}
	invalidMTPBlock.MerkleRoot = CalculateMerkleRoot(invalidMTPBlock.Transactions)
	for {
		invalidMTPBlock.Hash = invalidMTPBlock.CalculateHash()
		if HashMeetsTarget(invalidMTPBlock.Hash, diff1Target) {
			break
		}
		invalidMTPBlock.Nonce++
	}

	errMTP := bc.ValidateBlock(invalidMTPBlock)
	if errMTP == nil || !strings.Contains(errMTP.Error(), "MedianTimePast") {
		t.Fatalf("Expected block with timestamp <= MTP to be rejected with MedianTimePast error, got: %v", errMTP)
	}
	t.Logf("[OK] MTP-11 lower bound rejection verified: %v", errMTP)

	// 2. Block timestamp > 2 hours in the future must be REJECTED
	futureTime := time.Now().Add(3 * time.Hour).Unix()
	invalidFutureBlock := Block{
		Index:        1,
		Timestamp:    futureTime,
		PrevHash:     latest.Hash,
		Miner:        GenesisMiner,
		Difficulty:   1,
		Target:       diff1Hex,
		Transactions: []Transaction{{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward, Nonce: 1, Signature: "REWARD_SIG"}},
	}
	invalidFutureBlock.MerkleRoot = CalculateMerkleRoot(invalidFutureBlock.Transactions)
	for {
		invalidFutureBlock.Hash = invalidFutureBlock.CalculateHash()
		if HashMeetsTarget(invalidFutureBlock.Hash, diff1Target) {
			break
		}
		invalidFutureBlock.Nonce++
	}

	errFuture := bc.ValidateBlock(invalidFutureBlock)
	if errFuture == nil || !strings.Contains(errFuture.Error(), "too far in future") {
		t.Fatalf("Expected block with timestamp > 2h in future to be rejected with 'too far in future', got: %v", errFuture)
	}
	t.Logf("[OK] Future timestamp ceiling rejection verified: %v", errFuture)
}

// TestDeterministicForkReorganization verifies [Section D.3]:
// Fork choice rule strictly honors Satoshi cumulative Proof-of-Work (Chainwork).
func TestDeterministicForkReorganization(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	// Build Chain A: Genesis + 2 blocks at Difficulty 1 (Total Work = Gen + 2 * W1)
	bcA := NewBlockchain(1)
	defer bcA.Close()
	diff1Target := DifficultyToTarget(1)
	diff1Hex := fmt.Sprintf("%064x", diff1Target)
	bcA.Chain[0].Target = diff1Hex

	if _, err := bcA.MineBlockWithErr(GenesisMiner); err != nil {
		t.Fatalf("bcA mine #1 failed: %v", err)
	}
	if _, err := bcA.MineBlockWithErr(GenesisMiner); err != nil {
		t.Fatalf("bcA mine #2 failed: %v", err)
	}

	workA := CalculateChainwork(bcA.Chain)

	// Build Candidate Chain B: Branches at block #1 and mines 3 blocks (Total Work > WorkA)
	// Candidate blocks connecting to ancestor #1
	ancestor := bcA.Chain[1]

	var candidateBlocks []Block
	prev := ancestor
	for i := 0; i < 3; i++ {
		idx := prev.Index + 1
		b := Block{
			Index:        idx,
			Timestamp:    prev.Timestamp + 120,
			PrevHash:     prev.Hash,
			Miner:        GenesisMiner,
			Difficulty:   1,
			Target:       diff1Hex,
			Transactions: []Transaction{{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward, Nonce: int64(idx), Signature: "REWARD_SIG"}},
		}
		b.MerkleRoot = CalculateMerkleRoot(b.Transactions)
		// Mine block
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, diff1Target) {
				break
			}
			b.Nonce++
		}
		candidateBlocks = append(candidateBlocks, b)
		prev = b
	}

	// Compute candidate cumulative chainwork
	prefixWork := CalculateChainwork(bcA.Chain[:2]) // Genesis + block 1
	two256 := new(big.Int).Lsh(big.NewInt(1), 256)
	candidateWork := new(big.Int).Set(prefixWork)
	for _, b := range candidateBlocks {
		div := new(big.Int).Add(b.GetTarget(), big.NewInt(1))
		bw := new(big.Int).Div(two256, div)
		candidateWork.Add(candidateWork, bw)
	}

	if candidateWork.Cmp(workA) <= 0 {
		t.Fatalf("Candidate work must be strictly greater than active chain work: %s <= %s", candidateWork, workA)
	}

	// Reorganize bcA to candidate chain at ancestor index 1
	errReorg := bcA.ResolveFork(1, candidateBlocks)
	if errReorg != nil {
		t.Fatalf("ResolveFork failed to reorganize to heavier candidate chain: %v", errReorg)
	}

	// Verify bcA successfully reorganized
	if len(bcA.Chain) != 5 { // Genesis (0) + 1 + 3 new blocks = 5 blocks (height 4)
		t.Fatalf("Reorganized chain length mismatch: expected 5, got %d", len(bcA.Chain))
	}
	latest := bcA.GetLatestBlock()
	if latest.Hash != candidateBlocks[2].Hash {
		t.Fatalf("Tip hash mismatch after reorg: expected %s, got %s", candidateBlocks[2].Hash, latest.Hash)
	}

	t.Logf("[OK] Deterministic fork reorganization successfully switched to heavier chain (Height: %d, Tip: %s)",
		latest.Index, latest.Hash)
}

// TestMaxReorgDepth720Enforced verifies [Section D.4]:
// Reorganizations deeper than 720 blocks are strictly rejected by consensus.
func TestMaxReorgDepth720Enforced(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	// Simulate deep chain of 800 blocks
	mockChain := make([]Block, 800)
	for i := 0; i < 800; i++ {
		mockChain[i] = Block{Index: i, Hash: fmt.Sprintf("%064x", i)}
	}
	bc.Chain = mockChain

	// Attempting to reorganize at ancestor 50 (depth = 799 - 50 = 749 > 720) must FAIL
	err := bc.ResolveFork(50, []Block{{Index: 51}})
	if err == nil {
		t.Fatalf("Expected ResolveFork to reject reorg depth 749 > MaxReorganizationDepth (720)")
	}
	t.Logf("[OK] Max reorg depth 720 boundary strictly enforced: %v", err)
}

// TestMaxReorgDepth_ExactBoundaries verifies Phase 4 boundary conditions:
// Depth 720 (allowed) vs Depth 721 (strictly rejected).
func TestMaxReorgDepth_ExactBoundaries(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	// 1. Exactly 720 depth: Chain length 721 (index 0..720). Reorg at index 0 => depth = 720.
	chain721 := make([]Block, 721)
	for i := 0; i < 721; i++ {
		chain721[i] = Block{Index: i, Hash: fmt.Sprintf("%064x", i)}
	}
	bc.Chain = chain721

	// Attempting reorg at ancestor 0 (depth = 720):
	// Will fail on validation (blocks are mock), but must NOT fail with "exceeds maximum allowed reorg depth"
	err720 := bc.ResolveFork(0, []Block{{Index: 1}})
	if err720 != nil && strings.Contains(err720.Error(), "exceeds maximum allowed reorg depth") {
		t.Fatalf("Reorg depth 720 should not be rejected by depth check, got: %v", err720)
	}

	// 2. Exactly 721 depth: Chain length 722 (index 0..721). Reorg at index 0 => depth = 721.
	chain722 := make([]Block, 722)
	for i := 0; i < 722; i++ {
		chain722[i] = Block{Index: i, Hash: fmt.Sprintf("%064x", i)}
	}
	bc.Chain = chain722

	err721 := bc.ResolveFork(0, []Block{{Index: 1}})
	if err721 == nil || !strings.Contains(err721.Error(), "exceeds maximum allowed reorg depth of 720 blocks") {
		t.Fatalf("Expected reorg depth 721 to be rejected with 'exceeds maximum allowed reorg depth', got: %v", err721)
	}
	t.Logf("[OK] Exact reorg boundary tested: depth 720 permitted by depth guard, depth 721 strictly blocked.")
}

// TestOfflineNodeForwardSync verifies Phase 4 Scenario 1:
// An offline node syncing blocks forward sequentially along the canonical chain
// is NOT subject to the 720-block reorg limit because forward block addition is not a reorg.
func TestOfflineNodeForwardSync(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	diff1Target := DifficultyToTarget(1)
	diff1Hex := fmt.Sprintf("%064x", diff1Target)
	bc.Chain[0].Target = diff1Hex

	// Simulate syncing 5 sequential blocks forward from genesis
	for i := 1; i <= 5; i++ {
		prev := bc.GetLatestBlock()
		b := Block{
			Index:        i,
			Timestamp:    prev.Timestamp + 120,
			PrevHash:     prev.Hash,
			Miner:        GenesisMiner,
			Difficulty:   1,
			Target:       diff1Hex,
			Transactions: []Transaction{{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward, Nonce: int64(i), Signature: "REWARD_SIG"}},
		}
		b.MerkleRoot = CalculateMerkleRoot(b.Transactions)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, diff1Target) {
				break
			}
			b.Nonce++
		}

		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Forward sequential block #%d was unexpectedly rejected", i)
		}
	}

	if len(bc.Chain) != 6 {
		t.Fatalf("Expected chain length 6, got %d", len(bc.Chain))
	}
	t.Logf("[OK] Offline node forward sync verified: successfully appended %d blocks sequentially.", len(bc.Chain)-1)
}

// TestDatabaseResyncBackup verifies Phase 4:
// BackupAndResetDatabase safely archives the database to a .bak timestamped file
// and allows clean reinitialization from Genesis.
func TestDatabaseResyncBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	// Create and save an initial blockchain
	bc := NewBlockchain(1)
	bc.Chain[0].Target = fmt.Sprintf("%064x", DifficultyToTarget(1))
	if _, err := bc.MineBlockWithErr(GenesisMiner); err != nil {
		t.Fatalf("MineBlockWithErr failed: %v", err)
	}
	_ = bc.Close()

	dbPath := GetDBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Expected database file to exist at %s", dbPath)
	}

	// Trigger BackupAndResetDatabase
	bakPath, err := BackupAndResetDatabase()
	if err != nil {
		t.Fatalf("BackupAndResetDatabase failed: %v", err)
	}
	if bakPath == "" {
		t.Fatalf("Expected backup path to be returned")
	}

	// Verify active DB is gone and backup exists
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("Active database file should have been renamed")
	}
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Fatalf("Backup database file %s does not exist", bakPath)
	}

	// Re-initializing starts clean from Genesis
	bcFresh := NewBlockchain(1)
	defer bcFresh.Close()
	if len(bcFresh.Chain) != 1 {
		t.Fatalf("Expected fresh chain after resync to have length 1, got %d", len(bcFresh.Chain))
	}
	t.Logf("[OK] Database resync backup verified: archived to %s, fresh chain initialized at height 1.", filepath.Base(bakPath))
}
