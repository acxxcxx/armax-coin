//go:build testhooks

package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// TestPushBlocks_MultiChunkStagingReorg verifies HIGH-07:
// When a competitor branch that reorganizes the active chain is pushed in multiple
// sequential chunks (where each partial chunk has less chainwork than the local tip),
// the receiver stages the chunks in pushSessions until the full branch with greater
// cumulative chainwork is assembled and applied cleanly.
func TestPushBlocks_MultiChunkStagingReorg(t *testing.T) {
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	t.Cleanup(func() { core.SetTargetOverrideForTesting(nil) })

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	core.SetDBPath(dbPath)
	defer core.SetDBPath("")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	w, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	// Local chain: mine 4 blocks on top of genesis (tip = #4, len = 5)
	for i := 1; i <= 4; i++ {
		if _, err := bc.MineBlockWithErr(w.Address); err != nil {
			t.Fatalf("MineBlock %d failed: %v", i, err)
		}
	}

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	// Competitor branch: forks from block #1 (ancestor = 1)
	// and builds 5 new blocks: #2, #3, #4, #5, #6 (tip = #6, heavier than #4)
	ancestor := bc.Chain[1]
	var competitorBlocks []core.Block
	prev := ancestor
	w2, _ := wallet.NewWallet()
	target := core.DifficultyToTarget(1)

	for h := 2; h <= 6; h++ {
		cbTx := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: w2.Address,
			Amount:    core.InitialReward,
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}
		b := *core.NewBlock([]core.Transaction{cbTx}, prev.Hash, h, w2.Address, target)
		b.Timestamp = prev.Timestamp + 10
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		competitorBlocks = append(competitorBlocks, b)
		prev = b
	}

	// Split competitor branch into two chunks:
	// Chunk 1: blocks #2, #3 (2 blocks). Total chainwork of ancestor + chunk1 is less than local tip #4.
	// Chunk 2: blocks #4, #5, #6 (3 blocks). Total chainwork exceeds local tip #4.
	chunk1 := competitorBlocks[:2]
	chunk2 := competitorBlocks[2:]

	remoteIP := "198.51.100.1"
	remoteAddr := "198.51.100.1:9999"

	// 1. Deliver Chunk 1
	ps.handlePushedBlocks(chunk1, remoteIP, remoteAddr)

	// Verify Chunk 1 was staged in pushSessions and local tip was NOT changed yet
	if tip := bc.GetLatestBlock().Index; tip != 4 {
		t.Fatalf("expected local tip to remain at #4 after incomplete chunk1, got #%d", tip)
	}

	pushSessionsMu.Lock()
	sess, ok := pushSessions[remoteAddr]
	stagedLen := 0
	if ok && sess != nil {
		stagedLen = len(sess.blocks)
	}
	pushSessionsMu.Unlock()

	if stagedLen != 2 {
		t.Fatalf("expected 2 blocks staged in pushSessions for %s, got %d", remoteAddr, stagedLen)
	}

	// 2. Deliver Chunk 2
	ps.handlePushedBlocks(chunk2, remoteIP, remoteAddr)

	// Verify the reorganization succeeded and local tip is now competitor tip (#6)
	if tip := bc.GetLatestBlock().Index; tip != 6 {
		t.Fatalf("expected reorganization to succeed after chunk2 with tip #6, got #%d", tip)
	}

	// Verify push session was cleared
	pushSessionsMu.Lock()
	_, stillExists := pushSessions[remoteAddr]
	pushSessionsMu.Unlock()
	if stillExists {
		t.Fatalf("expected pushSession to be cleaned up after successful reorganization")
	}

	// Ensure timeout expiration works
	pushSessionsMu.Lock()
	pushSessions["expired_peer:1234"] = &pushBufferSession{
		ancestor: 0,
		blocks:   chunk1,
		lastSeen: time.Now().Add(-35 * time.Second),
	}
	cleanupExpiredPushSessionsLocked()
	_, expiredFound := pushSessions["expired_peer:1234"]
	pushSessionsMu.Unlock()
	if expiredFound {
		t.Fatalf("expected expired pushSession to be cleaned up")
	}
}

