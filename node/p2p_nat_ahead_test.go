//go:build testhooks

package node

import (
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// natAheadSetup builds a public node and a NAT node (no listener) sharing `common` blocks, then lets
// the public node mine `pubExtra` and the NAT node mine `natExtra` blocks while disconnected.
func natAheadSetup(t *testing.T, common, pubExtra, natExtra int) (bcPub, bcNAT *core.Blockchain, psPub, psNAT *PeerServer, pubAddr string) {
	t.Helper()
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	t.Cleanup(func() { core.SetTargetOverrideForTesting(nil) })

	bcPub, dirPub := newReconnectChain(t, "pub")
	bcNAT, dirNAT := newReconnectChain(t, "nat")
	wPub, _ := wallet.NewWallet()
	wNAT, _ := wallet.NewWallet()

	// Shared history: mine on pub and copy to NAT through a one-off sync.
	mineN(t, bcPub, wPub.Address, common)
	portPub := getFreePort(t)
	psPub = startTestPeerServer(bcPub, dirPub, portPub)
	t.Cleanup(func() { UnregisterPeerServer(psPub); psPub.Stop() })
	pubAddr = "127.0.0.1:" + portPub

	psNAT = NewPeerServer(getFreePort(t), bcNAT, dirNAT, "ARMX-TESTNET-1")
	psNAT.AllowPrivatePeers = true
	psNAT.RequireTLS = false
	RegisterPeerServer(psNAT)
	t.Cleanup(func() { UnregisterPeerServer(psNAT) })
	psNAT.SetSeedPeers([]string{pubAddr})
	psNAT.SyncBestPeer()
	if !waitTipEqual(bcPub, bcNAT, 5*time.Second) {
		t.Fatalf("initial shared history sync failed")
	}

	// Offline period: drop the link and mine independently.
	psPub.Stop()
	UnregisterPeerServer(psPub)
	time.Sleep(200 * time.Millisecond)
	mineN(t, bcPub, wPub.Address, pubExtra)
	mineN(t, bcNAT, wNAT.Address, natExtra)

	psPub = startTestPeerServer(bcPub, dirPub, portPub)
	t.Cleanup(func() { UnregisterPeerServer(psPub); psPub.Stop() })
	return
}

// A lighter branch pushed to a heavier node must be ignored, and blocks with a forged hash must not
// be applied.
func TestPushBlocks_LighterBranchAndForgedBlocksRejected(t *testing.T) {
	bcPub, bcNAT, psPub, _, _ := natAheadSetup(t, 3, 20, 4)

	_, tipBefore := tipHash(bcPub)
	bcNAT.Lock.RLock()
	branch := append([]core.Block(nil), bcNAT.Chain[4:8]...)
	bcNAT.Lock.RUnlock()

	psPub.handlePushedBlocks(branch, "203.0.113.7", "203.0.113.7:1")
	if _, after := tipHash(bcPub); after != tipBefore {
		t.Fatal("lighter pushed branch must not replace the heavier chain")
	}

	forged := append([]core.Block(nil), branch...)
	forged[0].Hash = forged[1].Hash
	psPub.handlePushedBlocks(forged, "203.0.113.8", "203.0.113.8:1")
	if _, after := tipHash(bcPub); after != tipBefore {
		t.Fatal("forged block must not be applied")
	}
	psPub.banLock.RLock()
	score := psPub.banScores["203.0.113.8"]
	psPub.banLock.RUnlock()
	if score == 0 {
		t.Fatal("forged push must be penalized")
	}
}

// The NAT node is simply ahead (no fork). The public node cannot dial it, so it has to learn about
// the longer chain over the connection the NAT node opened.
func TestNATAhead_LinearExtension(t *testing.T) {
	bcPub, bcNAT, _, psNAT, _ := natAheadSetup(t, 3, 0, 20)

	psNAT.SyncBestPeer() // NAT dials out; nothing else is triggered by hand.
	pi0, _ := tipHash(bcPub)
	ni, _ := tipHash(bcNAT)
	t.Logf("after connect: pub=#%d nat=#%d", pi0, ni)

	if !waitTipEqual(bcPub, bcNAT, 15*time.Second) {
		pi, _ := tipHash(bcPub)
		t.Fatalf("public node did not catch up from NAT peer: pub=#%d nat=#%d", pi, ni)
	}
}

// The NAT node holds a heavier fork. The public node is on a shorter private branch.
func TestNATAhead_HeavierForkReorgsPublic(t *testing.T) {
	bcPub, bcNAT, _, psNAT, _ := natAheadSetup(t, 3, 4, 20)

	psNAT.SyncBestPeer()
	ni, nh := tipHash(bcNAT)
	_, ph0 := tipHash(bcPub)
	t.Logf("fork: pub tip=%s nat tip=#%d %s", ph0[:12], ni, nh[:12])

	if !waitTipEqual(bcPub, bcNAT, 15*time.Second) {
		pi, _ := tipHash(bcPub)
		t.Fatalf("public node did not reorg onto NAT fork: pub=#%d nat=#%d", pi, ni)
	}
}

// Same forked situation, but the NAT node announces one fresh block, which is what happens when it
// mines in the field.
func TestNATAhead_NewBlockRelayHealsPublic(t *testing.T) {
	bcPub, bcNAT, _, psNAT, _ := natAheadSetup(t, 3, 4, 20)
	wNAT, _ := wallet.NewWallet()

	psNAT.SyncBestPeer()
	blk, err := bcNAT.MineBlockContext(t.Context(), wNAT.Address)
	if err != nil {
		t.Fatalf("mine: %v", err)
	}
	psNAT.RelayBlock(blk, "")

	if !waitTipEqual(bcPub, bcNAT, 15*time.Second) {
		pi, _ := tipHash(bcPub)
		ni, _ := tipHash(bcNAT)
		t.Fatalf("public node did not heal after relay: pub=#%d nat=#%d", pi, ni)
	}
}
