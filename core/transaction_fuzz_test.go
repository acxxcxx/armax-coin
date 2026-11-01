package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

// FuzzTransactionParser verifies that transaction unmarshaling, hashing, and verification
// never panic on arbitrary byte inputs [Security Audit]
func FuzzTransactionParser(f *testing.F) {
	validTx := Transaction{
		SigAlg:    1,
		Sender:    GenesisMiner,
		SenderPub: "abcd",
		Recipient: GenesisMiner,
		Amount:    1000,
		Fee:       1000,
		Nonce:     1,
		Signature: "beef",
	}
	validBytes, _ := json.Marshal(validTx)
	f.Add(validBytes)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"amount": -99999999999999999}`))
	f.Add([]byte(`{"sender": "invalid", "recipient": "invalid"}`))
	f.Add([]byte(`malformed json string`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var tx Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return
		}
		// Invariant: Calling Hash(), HashHex(), or Size() must never panic
		_ = tx.Hash()
		_ = tx.HashHex()
		_ = tx.Size()
		_ = tx.VerifyWithError()
	})
}

// FuzzBlockDecode verifies that block JSON decoding, hashing, Merkle root calculation,
// and size accounting never panic on arbitrary malformed byte inputs [Security Audit].
func FuzzBlockDecode(f *testing.F) {
	gen := GetGenesisBlock()
	genBytes, _ := json.Marshal(gen)
	f.Add(genBytes)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"index":-1,"timestamp":-999,"transactions":[{"sender":"SYSTEM","amount":5000000000}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var blk Block
		if err := json.Unmarshal(data, &blk); err != nil {
			return
		}
		if len(blk.Transactions) > 1000 {
			return
		}
		_ = blk.CalculateMerkleRoot()
		_ = blk.CalculateHash()
		_ = blk.Size()
	})
}

// FuzzAmountAndTargetInvariants verifies that decimal parsing and difficulty/target conversion
// never panic or overflow on arbitrary fuzzed inputs [Security Audit].
func FuzzAmountAndTargetInvariants(f *testing.F) {
	f.Add("50.00000000", int(1))
	f.Add("0.00000001", int(1000))
	f.Add("21000000", int(0))
	f.Add("-1.5", int(-5))
	f.Add("999999999999999999999.99999999", int(999999999))

	f.Fuzz(func(t *testing.T, amountStr string, diff int) {
		if amt, err := ParseArmxDecimal(amountStr); err == nil {
			if amt < 0 || amt > MaxSupply {
				t.Fatalf("ParseArmxDecimal returned out-of-range amount %d for input %q", amt, amountStr)
			}
			formatted := FormatArmxDecimal(amt)
			reparsed, err2 := ParseArmxDecimal(formatted)
			if err2 != nil || reparsed != amt {
				t.Fatalf("Round-trip failure for %d -> %q -> %d (%v)", amt, formatted, reparsed, err2)
			}
		}
		legacyTarget := DifficultyToTarget(diff)
		if legacyTarget == nil {
			t.Fatalf("DifficultyToTarget(%d) returned nil", diff)
		}
		if diff <= 63 {
			if legacyTarget.Sign() <= 0 {
				t.Fatalf("DifficultyToTarget(%d) returned non-positive target %v", diff, legacyTarget)
			}
		} else {
			if legacyTarget.Sign() != 0 {
				t.Fatalf("DifficultyToTarget(%d) expected 0 target after >=256-bit right shift, got %v", diff, legacyTarget)
			}
		}
		rtDiff := TargetToDifficulty(legacyTarget)
		if rtDiff < 1 || rtDiff > 64 {
			t.Fatalf("TargetToDifficulty(%v) returned out-of-range difficulty %d", legacyTarget, rtDiff)
		}
	})
}

// TestDifficultyToTargetBoundariesAndConsensusSafety verifies exact behavior at difficulty
// boundaries 0, 1, 63, 64, and 1000 across DifficultyToTarget, TargetToDifficulty, NewBlock,
// CalculateChainwork, CalculateExpectedTarget, PreCheckBlock, and ValidateBlock.
func TestDifficultyToTargetBoundariesAndConsensusSafety(t *testing.T) {
	t0 := DifficultyToTarget(0)
	t1 := DifficultyToTarget(1)
	t63 := DifficultyToTarget(63)
	t64 := DifficultyToTarget(64)
	t1000 := DifficultyToTarget(1000)

	// 1. Difficulty 0 clamps to Difficulty 1 (positive 252-bit target)
	if t0.Sign() <= 0 || t0.Cmp(t1) != 0 {
		t.Fatalf("DifficultyToTarget(0) = %x, want equal to DifficultyToTarget(1) = %x", t0, t1)
	}
	if got := TargetToDifficulty(t0); got != 1 {
		t.Fatalf("TargetToDifficulty(DifficultyToTarget(0)) = %d, want 1", got)
	}

	// 2. Difficulty 1 has 252 bits (63 hex 'f' nibbles)
	if t1.BitLen() != 252 || TargetToDifficulty(t1) != 1 {
		t.Fatalf("DifficultyToTarget(1) bitlen=%d diff=%d, want bitlen=252 diff=1", t1.BitLen(), TargetToDifficulty(t1))
	}

	// 3. Difficulty 63 is the highest positive nibble-shift difficulty: (2^256 - 1) >> 252 == 0xf (15)
	if t63.Sign() <= 0 || t63.Int64() != 15 || TargetToDifficulty(t63) != 63 {
		t.Fatalf("DifficultyToTarget(63) = %v (diff=%d), want 15 (diff=63)", t63, TargetToDifficulty(t63))
	}

	// 4. Difficulty 64 shifts 256-bit MaxTarget256 right by 256 bits -> 0; TargetToDifficulty(0) -> 64
	if t64.Sign() != 0 || TargetToDifficulty(t64) != 64 {
		t.Fatalf("DifficultyToTarget(64) = %v (diff=%d), want 0 (diff=64)", t64, TargetToDifficulty(t64))
	}

	// 5. Difficulty 1000 clamps to 64 -> 0; TargetToDifficulty(0) -> 64
	if t1000.Sign() != 0 || t1000.Cmp(t64) != 0 || TargetToDifficulty(t1000) != 64 {
		t.Fatalf("DifficultyToTarget(1000) = %v (diff=%d), want 0 (diff=64)", t1000, TargetToDifficulty(t1000))
	}

	// 6. NewBlock safely replaces zero/non-positive targets (from diff 64 or 1000) with DifficultyToTarget(1)
	for _, d := range []int{64, 1000} {
		blk := NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Nonce:     1,
			Signature: "REWARD_SIG",
		}}, GenesisHash, 1, GenesisMiner, DifficultyToTarget(d))
		if blk.GetTarget().Cmp(t1) != 0 || blk.Difficulty != 1 {
			t.Fatalf("NewBlock with DifficultyToTarget(%d) did not clamp to diff 1: target=%x diff=%d", d, blk.GetTarget(), blk.Difficulty)
		}
	}

	// 7. CalculateChainwork safely guards against zero target (Target="", Difficulty=64 or 1000) without panic
	for _, d := range []int{0, 1, 63, 64, 1000} {
		cw := CalculateChainwork([]Block{{Index: 0, Difficulty: d, Target: ""}})
		if cw == nil || cw.Sign() <= 0 {
			t.Fatalf("CalculateChainwork with Difficulty=%d returned non-positive work %v", d, cw)
		}
	}

	// 8.Consensus PreCheckBlock and ValidateBlock strictly reject candidate blocks with zero target or forged difficulty 64/1000
	tempDir := t.TempDir()
	bc, err := NewBlockchainWithStoragePath(1, filepath.Join(tempDir, "diff_boundary.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	expectedTarget := CalculateExpectedTarget(bc.Chain, 1)
	if expectedTarget == nil || expectedTarget.Sign() <= 0 {
		t.Fatalf("CalculateExpectedTarget returned non-positive target: %v", expectedTarget)
	}

	prev := bc.Chain[0]
	for _, badDiff := range []int{64, 1000} {
		badBlk := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Nonce:     1,
			Signature: "REWARD_SIG",
		}}, prev.Hash, 1, GenesisMiner, t1)
		badBlk.Timestamp = prev.Timestamp + TargetBlockTimeSeconds
		badBlk.Target = fmt.Sprintf("%064x", DifficultyToTarget(badDiff)) // all-zero 64-hex string
		badBlk.Difficulty = badDiff
		badBlk.Hash = badBlk.CalculateHash()

		if err := bc.PreCheckBlock(&badBlk); err == nil {
			t.Fatalf("PreCheckBlock accepted block with zero target (difficulty=%d)", badDiff)
		}
		if err := bc.ValidateBlock(badBlk); err == nil {
			t.Fatalf("ValidateBlock accepted block with zero target (difficulty=%d)", badDiff)
		}
	}
}

// TestResolveForkRollbackOnPersistenceFailure verifies that if SaveToFile fails during ResolveFork,
// the active chain, state, supply, chainwork, and backing array are cleanly rolled back [Security Audit].
func TestResolveForkRollbackOnPersistenceFailure(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	miner := GenesisMiner
	target := DifficultyToTarget(1)
	bc.Chain[0].Target = fmt.Sprintf("%064x", target)
	prev := bc.Chain[0]

	mineChild := func(parent Block, index int, tsOffset int64) Block {
		blk := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    InitialReward,
			Nonce:     int64(index),
			Signature: "REWARD_SIG",
		}}, parent.Hash, index, miner, target)
		blk.Timestamp = parent.Timestamp + int64(TargetBlockTimeSeconds) + tsOffset
		for {
			blk.Hash = blk.CalculateHash()
			if HashMeetsTarget(blk.Hash, target) {
				break
			}
			blk.Nonce++
		}
		return blk
	}

	// Add block 1A to main chain
	b1A := mineChild(prev, 1, 0)
	if !bc.ValidateAndAddBlock(b1A) {
		t.Fatalf("Failed to add block 1A")
	}

	origHeight := len(bc.Chain)
	origTipHash := bc.Chain[origHeight-1].Hash
	origSupply := bc.cachedSupply
	origWork := bc.GetTotalChainwork().String()

	// Construct heavier fork (1B -> 2B)
	b1B := mineChild(prev, 1, 1)
	b2B := mineChild(b1B, 2, 1)

	// Force SaveToFile() to fail during ResolveFork by closing the underlying db handle temporarily
	origDB := bc.db
	bc.db = nil
	bc.StoragePath = filepath.Join(tempDir, "nonexistent_file_blocker\x00invalid", "blockchain.db")

	reorgErr := bc.ResolveFork(0, []Block{b1B, b2B})
	bc.db = origDB
	bc.StoragePath = dbPath

	if reorgErr == nil {
		t.Fatalf("Expected ResolveFork to fail when persistence fails, got nil")
	}

	// Verify in-memory chain and state rolled back cleanly
	if len(bc.Chain) != origHeight {
		t.Fatalf("Chain height not rolled back: got %d, want %d", len(bc.Chain), origHeight)
	}
	if bc.Chain[len(bc.Chain)-1].Hash != origTipHash {
		t.Fatalf("Chain tip hash corrupted after failed reorg: got %s, want %s", bc.Chain[len(bc.Chain)-1].Hash, origTipHash)
	}
	if bc.cachedSupply != origSupply {
		t.Fatalf("TotalSupply not rolled back: got %d, want %d", bc.cachedSupply, origSupply)
	}
	if bc.GetTotalChainwork().String() != origWork {
		t.Fatalf("TotalChainwork not rolled back: got %s, want %s", bc.GetTotalChainwork().String(), origWork)
	}
}
