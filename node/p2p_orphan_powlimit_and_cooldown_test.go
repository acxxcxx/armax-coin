package node

import (
	"math/big"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT3_OrphanLooseTargetRejected verifies that an orphan block with an attacker-crafted
// loose target (above PowLimit) is strictly rejected and penalizes the peer [T3].
func TestT3_OrphanLooseTargetRejected(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	peerIP := "198.51.100.88"

	// Attacker crafts target with maximum allowable 256-bit integer (2^256 - 1)
	looseTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	b := *core.NewBlock([]core.Transaction{}, "00000000prevorphan", 50, core.GenesisMiner, looseTarget)
	for {
		calc, _ := b.CalculateHashWithError()
		b.Hash = calc
		if core.HashMeetsTarget(b.Hash, looseTarget) {
			break
		}
		b.Nonce++
	}

	stored := ps.AddOrphanBlock(b, peerIP)
	if stored {
		t.Fatalf("[T3 FAILING TEST] Orphan with loose target above PowLimit was accepted into orphan buffer!")
	}

	if ps.GetBanScore(peerIP) < 20 {
		t.Fatalf("[T3 FAILING TEST] Peer sending loose target orphan was not penalized (banScore: %d)", ps.GetBanScore(peerIP))
	}
}

// TestT3_SyncCooldownPerPeer verifies that sync requests triggered by orphans are rate-limited to 1 per 30s per peer [T3].
func TestT3_SyncCooldownPerPeer(t *testing.T) {
	ps := &PeerServer{}

	peerHost := "198.51.100.99"
	if !ps.canTriggerSync(peerHost) {
		t.Fatalf("Initial sync trigger should be allowed")
	}

	// Immediate second trigger must be cooled down
	if ps.canTriggerSync(peerHost) {
		t.Fatalf("Second sync trigger within 30s should be blocked by cooldown")
	}
}
