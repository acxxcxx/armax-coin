package core

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupC24EasyTarget(t *testing.T) {
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	t.Cleanup(func() {
		targetOverrideHook = oldHook
	})
}

// TestC24_ResolveForkHeavierChainWins tests that a candidate fork with higher cumulative chainwork
// successfully reorganizes the local chain.
func TestC24_ResolveForkHeavierChainWins(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	// Mine 2 local blocks on top of genesis
	miner := GenesisMiner
	_, err := bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 1 failed: %v", err)
	}
	_, err = bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 2 failed: %v", err)
	}

	ancestorIndex := 1 // fork from block 1
	ancestor := bc.Chain[ancestorIndex]

	// Mine 2 blocks on candidate fork (blocks 2 and 3)
	var candidateBlocks []Block
	prev := ancestor
	for i := 2; i <= 3; i++ {
		target := bc.CalculateLWMA1()
		b := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, prev.Hash, i, miner, target)
		b.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		candidateBlocks = append(candidateBlocks, b)
		prev = b
	}

	err = bc.ResolveFork(ancestorIndex, candidateBlocks)
	if err != nil {
		t.Fatalf("[C-24] ResolveFork failed for heavier candidate: %v", err)
	}

	if len(bc.Chain) != 4 {
		t.Fatalf("Expected chain length 4, got %d", len(bc.Chain))
	}
	if bc.GetLatestBlock().Hash != candidateBlocks[len(candidateBlocks)-1].Hash {
		t.Fatalf("Tip hash mismatch after reorg")
	}
}

// TestC24_ResolveForkLighterChainLoses tests that a candidate fork with lower or equal cumulative chainwork
// is strictly rejected.
func TestC24_ResolveForkLighterChainLoses(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	miner := GenesisMiner
	for i := 0; i < 3; i++ {
		if _, err := bc.MineBlockWithErr(miner); err != nil {
			t.Fatalf("MineBlock failed: %v", err)
		}
	}

	// Candidate has only 1 block forking from block 1 (lighter than local chain with 3 blocks)
	ancestorIndex := 1
	ancestor := bc.Chain[ancestorIndex]
	target := bc.CalculateLWMA1()
	b := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, ancestor.Hash, 2, miner, target)
	b.Timestamp = ancestor.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b.Hash = b.CalculateHash()
		if HashMeetsTarget(b.Hash, target) {
			break
		}
		b.Nonce++
	}

	err := bc.ResolveFork(ancestorIndex, []Block{b})
	if err == nil {
		t.Fatalf("[C-24] Expected ResolveFork to reject lighter candidate chain")
	}
	if !strings.Contains(err.Error(), "candidate chainwork is not strictly greater than active chainwork") {
		t.Fatalf("[C-24] Unexpected error for lighter chain: %v", err)
	}
}

// TestC24_ResolveForkExceedsMaxReorgDepth tests that reorg depths exceeding MaxReorganizationDepth (720)
// are strictly rejected.
func TestC24_ResolveForkExceedsMaxReorgDepth(t *testing.T) {
	bc := &Blockchain{
		Chain: make([]Block, 725),
	}
	// Try reorg from block 0 (depth = 724 > 720)
	err := bc.ResolveFork(0, []Block{})
	if err == nil {
		t.Fatalf("[C-24] Expected ResolveFork to reject fork depth > 720")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed reorg depth of 720 blocks") {
		t.Fatalf("[C-24] Unexpected error for deep reorg: %v", err)
	}
}

// TestC24_ResolveForkFutureTimestampRejected tests that candidate blocks with timestamps exceeding 2 hours
// into the future are rejected.
func TestC24_ResolveForkFutureTimestampRejected(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	miner := GenesisMiner
	if _, err := bc.MineBlockWithErr(miner); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}

	ancestorIndex := 0
	ancestor := bc.Chain[0]
	target := bc.CalculateLWMA1()

	// Candidate fork has 2 blocks so candidate work > local work (which has 1 block)
	// Block 1 with timestamp 3 hours into future
	b1 := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, ancestor.Hash, 1, miner, target)
	b1.Timestamp = time.Now().Unix() + 3*3600
	for {
		b1.Hash = b1.CalculateHash()
		if HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	b2 := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, b1.Hash, 2, miner, target)
	b2.Timestamp = b1.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b2.Hash = b2.CalculateHash()
		if HashMeetsTarget(b2.Hash, target) {
			break
		}
		b2.Nonce++
	}

	err := bc.ResolveFork(ancestorIndex, []Block{b1, b2})
	if err == nil {
		t.Fatalf("[C-24] Expected ResolveFork to reject block with future timestamp")
	}
	if !strings.Contains(err.Error(), "exceeds maximum drift") && !strings.Contains(err.Error(), "future") {
		t.Fatalf("[C-24] Unexpected error for future timestamp: %v", err)
	}
}

// TestC24_ResolveForkInvalidBlockPreservesLocalState verifies that an invalid block in the candidate segment
// aborts reorg atomically without corrupting or modifying local state.
func TestC24_ResolveForkInvalidBlockPreservesLocalState(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	miner := GenesisMiner
	_, err := bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 1 failed: %v", err)
	}
	_, err = bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 2 failed: %v", err)
	}

	initialTip := bc.GetLatestBlock().Hash
	initialChainLen := len(bc.Chain)

	ancestorIndex := 1
	ancestor := bc.Chain[ancestorIndex]
	target := bc.CalculateLWMA1()

	// Candidate block 2 is valid
	b2 := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, ancestor.Hash, 2, miner, target)
	b2.Timestamp = ancestor.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b2.Hash = b2.CalculateHash()
		if HashMeetsTarget(b2.Hash, target) {
			break
		}
		b2.Nonce++
	}

	// Candidate block 3 has invalid transaction (spending non-existent balance)
	accX := newTestAccount(t)
	privX, pubX, addrX := accX.PrivKey, accX.PubHex, accX.Address
	invalidTx := Transaction{
		Sender:    addrX,
		SenderPub: pubX,
		Recipient: miner,
		Amount:    500 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = invalidTx.Sign(privX)

	b3 := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    InitialReward,
		Nonce:     3,
		Signature: "REWARD_SIG",
	}, invalidTx}, b2.Hash, 3, miner, target)
	b3.Timestamp = b2.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b3.Hash = b3.CalculateHash()
		if HashMeetsTarget(b3.Hash, target) {
			break
		}
		b3.Nonce++
	}

	err = bc.ResolveFork(ancestorIndex, []Block{b2, b3})
	if err == nil {
		t.Fatalf("[C-24] Expected ResolveFork to fail on invalid candidate block")
	}

	// Assert local chain was NOT changed
	if len(bc.Chain) != initialChainLen {
		t.Fatalf("Local chain length changed after failed reorg: %d vs %d", len(bc.Chain), initialChainLen)
	}
	if bc.GetLatestBlock().Hash != initialTip {
		t.Fatalf("Local chain tip changed after failed reorg")
	}
}

// TestC24_ResolveForkMempoolPreserved verifies that orphaned transactions in the overridden branch
// that are not in the winning branch are restored back into the mempool.
func TestC24_ResolveForkMempoolPreserved(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.CoinbaseMaturity = 1
	bc.StoragePath = dbPath
	defer bc.Close()

	accSender := newTestAccount(t, "sender")
	privSender, pubSender, senderAddr := accSender.PrivKey, accSender.PubHex, accSender.Address
	recipientAddr := newTestAccount(t, "recipient").Address

	// Block 1 funds senderAddr
	b1, err := bc.MineBlockContext(context.Background(), senderAddr)
	if err != nil {
		t.Fatalf("MineBlock 1 failed: %v", err)
	}
	_ = b1

	// Add Tx from sender to recipient
	tx := Transaction{
		Sender:    senderAddr,
		SenderPub: pubSender,
		Recipient: recipientAddr,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(privSender); err != nil {
		t.Fatalf("sign tx failed: %v", err)
	}
	ok, msg := bc.AddTransaction(tx)
	if !ok {
		t.Fatalf("AddTransaction failed: %s", msg)
	}

	// Mine Block 2 including tx
	b2, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("MineBlock 2 failed: %v", err)
	}
	_ = b2

	// Mempool should now be empty
	if len(bc.Mempool) != 0 {
		t.Fatalf("Expected empty mempool after block confirmation")
	}

	// Now candidate fork branches off block 1 with 2 blocks (blocks 2 and 3)
	// without including tx
	ancestorIndex := 1
	ancestor := bc.Chain[ancestorIndex]
	target := bc.CalculateLWMA1()

	b2Fork := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: GenesisMiner,
		Amount:    InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, ancestor.Hash, 2, GenesisMiner, target)
	b2Fork.Timestamp = ancestor.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b2Fork.Hash = b2Fork.CalculateHash()
		if HashMeetsTarget(b2Fork.Hash, target) {
			break
		}
		b2Fork.Nonce++
	}

	b3Fork := *NewBlock([]Transaction{{
		Sender:    "SYSTEM",
		Recipient: GenesisMiner,
		Amount:    InitialReward,
		Nonce:     3,
		Signature: "REWARD_SIG",
	}}, b2Fork.Hash, 3, GenesisMiner, target)
	b3Fork.Timestamp = b2Fork.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		b3Fork.Hash = b3Fork.CalculateHash()
		if HashMeetsTarget(b3Fork.Hash, target) {
			break
		}
		b3Fork.Nonce++
	}

	err = bc.ResolveFork(ancestorIndex, []Block{b2Fork, b3Fork})
	if err != nil {
		t.Fatalf("ResolveFork failed: %v", err)
	}

	// Verify tx was restored into mempool!
	bc.Lock.RLock()
	mempoolLen := len(bc.Mempool)
	foundTx := false
	for _, mTx := range bc.Mempool {
		if mTx.HashHex() == tx.HashHex() {
			foundTx = true
			break
		}
	}
	bc.Lock.RUnlock()

	if mempoolLen != 1 || !foundTx {
		t.Fatalf("[C-24] Expected tx to be restored to mempool after reorg, found %d txs", mempoolLen)
	}
}

// TestC24_ResolveForkSpeed100Blocks verifies that reorganizing 100 blocks takes < 1 second.
func TestC24_ResolveForkSpeed100Blocks(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := filepath.Join(tempDir, "blockchain.db")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	miner := GenesisMiner
	target := DifficultyToTarget(1)
	bc.Chain[0].Target = fmt.Sprintf("%064x", target)

	// Build local chain of 100 blocks
	for i := 1; i <= 100; i++ {
		prev := bc.Chain[len(bc.Chain)-1]
		b := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, prev.Hash, i, miner, target)
		b.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Failed to add local block %d", i)
		}
	}

	// Build candidate branch of 101 blocks forking at genesis (ancestorIndex = 0)
	var candidateBlocks []Block
	prev := bc.Chain[0]
	// Use slightly lower target (higher difficulty) or more blocks so chainwork is higher
	for i := 1; i <= 101; i++ {
		b := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, prev.Hash, i, miner, target)
		b.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		candidateBlocks = append(candidateBlocks, b)
		prev = b
	}

	start := time.Now()
	err := bc.ResolveFork(0, candidateBlocks)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("[C-24] ResolveFork 100 blocks failed: %v", err)
	}

	if duration > 1*time.Second {
		t.Fatalf("[C-24] ResolveFork 100 blocks took %v (> 1s)", duration)
	}
	t.Logf("[C-24] 100-block reorg executed in %v", duration)
}
