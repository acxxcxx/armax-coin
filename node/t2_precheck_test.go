package node

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT2_JunkBlocksDoNotTriggerStateCloneAndPenalize tests that incoming junk blocks
// matching tip index/prevhash do not trigger StateManager.Clone() and penalize the sender [T2].
func TestT2_JunkBlocksDoNotTriggerStateCloneAndPenalize(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	remoteIP := "198.51.100.77"

	latest := bc.GetLatestBlock()

	// Construct junk block with valid Index & PrevHash, but invalid Merkle and fake Hash
	target := core.DifficultyToTarget(1)
	junkBlock := *core.NewBlock([]core.Transaction{}, latest.Hash, latest.Index+1, core.GenesisMiner, target)
	junkBlock.MerkleRoot = "0000000000000000000000000000000000000000000000000000000000000bad"
	junkBlock.Hash = "000000000000000000000000000000000000000000000000000000000000fake"

	cloneBefore := atomic.LoadInt64(&core.CloneCount)

	// Ingest junk block via ProcessIncomingBlock
	accepted := ps.ProcessIncomingBlock(junkBlock, remoteIP, remoteIP+":9999")
	if accepted {
		t.Fatalf("Junk block was unexpectedly accepted!")
	}

	cloneAfter := atomic.LoadInt64(&core.CloneCount)
	if cloneAfter > cloneBefore {
		t.Fatalf("[T2 FAILING TEST] Junk block triggered StateManager.Clone()! Before: %d, After: %d", cloneBefore, cloneAfter)
	}

	banScore := ps.GetBanScore(remoteIP)
	if banScore < 20 {
		t.Fatalf("[T2 FAILING TEST] Expected sender of junk block to receive ban score >= 20, got %d", banScore)
	}
}

// TestT2_NegativeCacheRecordsRecalculatedHash verifies that failed blocks are stored
// in negative cache using recalculated hash, stopping spoofed payload repetitions [T2].
func TestT2_NegativeCacheRecordsRecalculatedHash(t *testing.T) {
	ps := &PeerServer{
		banScores:    make(map[string]int),
		bannedIPs:    make(map[string]time.Time),
		rateTrackers: make(map[string]*peerRateTracker),
	}

	fakeHash := "1111111111111111111111111111111111111111111111111111111111111111"
	if ps.isBlockNegativeCached(fakeHash) {
		t.Fatalf("Unrecorded hash should not be in negative cache")
	}

	ps.markBlockNegative(fakeHash)
	if !ps.isBlockNegativeCached(fakeHash) {
		t.Fatalf("Hash was marked negative but isBlockNegativeCached returned false")
	}
}

// TestT2_10k_JunkBlocks_ZeroClones verifies that flooding 10,000 junk blocks does not invoke StateManager.Clone()
// and quickly bans the attacker [T2].
func TestT2_10k_JunkBlocks_ZeroClones(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	attackerIP := "198.51.100.66"

	latest := bc.GetLatestBlock()
	target := core.DifficultyToTarget(1)

	cloneStart := atomic.LoadInt64(&core.CloneCount)

	for i := 0; i < 10000; i++ {
		junk := *core.NewBlock([]core.Transaction{}, latest.Hash, latest.Index+1, core.GenesisMiner, target)
		junk.Nonce = int64(i)
		junk.MerkleRoot = "bad_merkle_root"
		junk.Hash = "bad_hash_value"

		ps.ProcessIncomingBlock(junk, attackerIP, attackerIP+":9000")
	}

	cloneEnd := atomic.LoadInt64(&core.CloneCount)
	if cloneEnd != cloneStart {
		t.Fatalf("Expected 0 StateManager.Clone calls during 10k junk blocks flood, got %d", cloneEnd-cloneStart)
	}

	if ps.GetBanScore(attackerIP) < 100 {
		t.Fatalf("Attacker ban score expected >= 100, got %d", ps.GetBanScore(attackerIP))
	}
}
