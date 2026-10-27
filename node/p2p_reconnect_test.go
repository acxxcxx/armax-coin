//go:build testhooks

package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

func newReconnectChain(t *testing.T, name string) (*core.Blockchain, string) {
	t.Helper()
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, name+".db"))
	if err != nil {
		t.Fatalf("init %s: %v", name, err)
	}
	t.Cleanup(func() { bc.Close() })
	return bc, dir
}

func mineN(t *testing.T, bc *core.Blockchain, addr string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := bc.MineBlockContext(context.Background(), addr); err != nil {
			t.Fatalf("mine: %v", err)
		}
	}
}

func tipHash(bc *core.Blockchain) (int, string) {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	b := bc.GetLatestBlockUnsafe()
	return b.Index, b.Hash
}

func waitTipEqual(a, b *core.Blockchain, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ai, ah := tipHash(a)
		bi, bh := tipHash(b)
		if ai == bi && ah == bh {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func startTestPeerServer(bc *core.Blockchain, dir, port string) *PeerServer {
	ps := NewPeerServer(port, bc, dir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	go ps.StartServer()
	time.Sleep(300 * time.Millisecond)
	return ps
}

// A miner node that loses its link to the public node (more than MaxPeerFailures sync rounds),
// mines a private branch meanwhile, then must rejoin and reorganize onto the heavier chain
// without a restart.
func TestReconnect_MinerRejoinsAfterOutageAndReorgs(t *testing.T) {
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bcPub, dirPub := newReconnectChain(t, "pub")
	bcMin, dirMin := newReconnectChain(t, "miner")
	wPub, _ := wallet.NewWallet()
	wMin, _ := wallet.NewWallet()

	mineN(t, bcPub, wPub.Address, 3)

	portPub := getFreePort(t)
	psPub := startTestPeerServer(bcPub, dirPub, portPub)
	pubAddr := "127.0.0.1:" + portPub

	psMin := NewPeerServer(getFreePort(t), bcMin, dirMin, "ARMX-TESTNET-1")
	psMin.AllowPrivatePeers = true
	psMin.RequireTLS = false
	RegisterPeerServer(psMin)
	defer UnregisterPeerServer(psMin)
	psMin.SetSeedPeers([]string{pubAddr})

	psMin.SyncBestPeer()
	if !waitTipEqual(bcPub, bcMin, 5*time.Second) {
		t.Fatalf("initial sync failed")
	}

	// Outage: the public node becomes unreachable.
	psPub.Stop()
	UnregisterPeerServer(psPub)
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < MaxPeerFailures+3; i++ {
		psMin.SyncBestPeer()
	}
	found := false
	for _, p := range psMin.GetPeers() {
		if p == pubAddr {
			found = true
		}
	}
	if !found {
		t.Fatalf("seed peer was evicted during outage; node would never reconnect")
	}

	// Both sides keep mining: miner a short private branch, public the heavier one.
	mineN(t, bcMin, wMin.Address, 2)
	mineN(t, bcPub, wPub.Address, 5)
	_, minTipBefore := tipHash(bcMin)

	psPub2 := startTestPeerServer(bcPub, dirPub, portPub)
	defer UnregisterPeerServer(psPub2)
	defer psPub2.Stop()

	psMin.SyncBestPeer()
	if !waitTipEqual(bcPub, bcMin, 8*time.Second) {
		pi, _ := tipHash(bcPub)
		mi, _ := tipHash(bcMin)
		t.Fatalf("miner did not rejoin/reorg: pub=#%d miner=#%d", pi, mi)
	}
	if _, after := tipHash(bcMin); after == minTipBefore {
		t.Fatalf("miner still on its private branch")
	}
}

// When every peer fails in the same round the local link is the likely culprit,
// so ordinary (non-seed) peers must not be evicted.
func TestReconnect_TotalOutageDoesNotEvictPeers(t *testing.T) {
	bc, dir := newReconnectChain(t, "n")
	ps := NewPeerServer(getFreePort(t), bc, dir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	dead := "127.0.0.1:" + getFreePort(t)
	if err := ps.AddPeer(dead); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxPeerFailures+3; i++ {
		ps.SyncBestPeer()
	}
	if len(ps.GetPeers()) != 1 {
		t.Fatalf("peer evicted during total outage: %v", ps.GetPeers())
	}
}

// If the address book ends up empty (e.g. after an older eviction), the seeds come back.
func TestReconnect_ReseedRestoresEmptyAddressBook(t *testing.T) {
	bc, dir := newReconnectChain(t, "n")
	ps := NewPeerServer(getFreePort(t), bc, dir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	seed := "127.0.0.1:" + getFreePort(t)
	ps.SetSeedPeers([]string{seed})
	if len(ps.GetPeers()) != 1 {
		t.Fatalf("seed not added: %v", ps.GetPeers())
	}

	ps.Lock.Lock()
	ps.newPeers = map[string]*PeerInfo{}
	ps.triedPeers = map[string]*PeerInfo{}
	ps.rebuildPeersListLocked()
	ps.Lock.Unlock()
	ps.markPeerUnreachable(seed, time.Hour)

	ps.clearAllPeerUnreachable()
	ps.ReseedPeers()
	if len(ps.GetPeers()) != 1 || ps.isPeerUnreachable(seed) {
		t.Fatalf("seed not restored: peers=%v", ps.GetPeers())
	}
}
