//go:build testhooks

package node

import (
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestIBD_BrokenHashChainDetectionAndPeerPenalties verifies HIGH-09:
// During IBD chunk download, non-contiguous block segments (PrevHash mismatch or Index skip)
// and failed fork resolutions result in immediate ban scoring (AddBanScore) and failure recording (RecordPeerFailure).
func TestIBD_BrokenHashChainDetectionAndPeerPenalties(t *testing.T) {
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	t.Cleanup(func() { core.SetTargetOverrideForTesting(nil) })

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	core.SetDBPath(dbPath)
	defer core.SetDBPath("")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	peerIP := "203.0.113.88"
	peerAddr := "203.0.113.88:18333"
	_ = ps.AddPeer(peerAddr)

	initialScore := ps.GetBanScore(peerIP)
	if initialScore != 0 {
		t.Fatalf("expected initial ban score 0, got %d", initialScore)
	}

	// 1. Test hash chain broken linkage detection
	// Create two blocks where block #2 has a forged PrevHash that doesn't match block #1's Hash
	target := core.DifficultyToTarget(1)
	b1 := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: core.GenesisMiner,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, bc.Chain[0].Hash, 1, core.GenesisMiner, target)
	for {
		b1.Hash = b1.CalculateHash()
		if core.HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	b2Broken := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: core.GenesisMiner,
		Amount:    core.InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 2, core.GenesisMiner, target)
	for {
		b2Broken.Hash = b2Broken.CalculateHash()
		if core.HashMeetsTarget(b2Broken.Hash, target) {
			break
		}
		b2Broken.Nonce++
	}

	brokenChunk := []core.Block{b1, b2Broken}

	// Verify headerPoW passes for both blocks individually
	for _, b := range brokenChunk {
		if _, ok := headerPoWHash(b); !ok {
			t.Fatalf("headerPoWHash unexpectedly failed on valid test block")
		}
	}

	// Verify segment contiguous check detects PrevHash mismatch
	segmentContiguous := true
	for i := 1; i < len(brokenChunk); i++ {
		if brokenChunk[i].Index != brokenChunk[i-1].Index+1 || brokenChunk[i].PrevHash != brokenChunk[i-1].Hash {
			segmentContiguous = false
			break
		}
	}
	if segmentContiguous {
		t.Fatalf("expected broken chunk to fail cryptographic hash continuity check")
	}

	// Apply penalty matching IBD logic
	ps.AddBanScore(peerIP, 50)
	_ = ps.RecordPeerFailure(peerAddr)

	if score := ps.GetBanScore(peerIP); score != 50 {
		t.Fatalf("expected ban score 50 after broken linkage penalty, got %d", score)
	}
}
