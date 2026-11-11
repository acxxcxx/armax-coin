package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestBanDuration12Hours verifies that IP bans use a 12-hour expiration rather than 24 hours.
func TestBanDuration12Hours(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	testIP := "198.51.100.25"

	start := time.Now()
	ps.AddBanScore(testIP, 100)

	if !ps.IsBanned(testIP) {
		t.Fatalf("expected IP %s to be banned after ban score >= 100", testIP)
	}

	ps.banLock.RLock()
	exp, exists := ps.bannedIPs[testIP]
	ps.banLock.RUnlock()

	if !exists {
		t.Fatalf("expected ban expiration entry for %s", testIP)
	}

	// Verify ban expiration is approximately 12 hours from now
	remaining := exp.Sub(start)
	if remaining < 11*time.Hour+50*time.Minute || remaining > 12*time.Hour+10*time.Minute {
		t.Fatalf("expected ban duration of ~12h, got duration: %v (exp: %v, start: %v)", remaining, exp, start)
	}
}

// TestStalePeerEviction2Hours verifies that a non-seed peer that has not responded for >= 2 hours
// is pruned and evicted upon failure or periodic prune.
func TestStalePeerEviction2Hours(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	stalePeer := "203.0.113.88:19335"
	seedPeer := "testnet-seed1.armax-coin.cloud:19335"
	ps.SetSeedPeers([]string{seedPeer})

	// Add peer and simulate that it was last seen 3 hours ago with failures
	if err := ps.AddPeer(stalePeer); err != nil {
		t.Fatalf("AddPeer failed: %v", err)
	}

	ps.Lock.Lock()
	if info, ok := ps.newPeers[stalePeer]; ok {
		info.LastSeen = time.Now().Add(-3 * time.Hour)
		info.Failures = 1
	}
	ps.Lock.Unlock()

	// 1. isPeerStale must return true for stalePeer
	if !ps.isPeerStale(stalePeer) {
		t.Fatalf("expected isPeerStale to be true for peer last seen 3 hours ago with failure")
	}

	// Seed peer must never be stale
	if ps.isPeerStale(seedPeer) {
		t.Fatalf("seed peer must never be reported as stale")
	}

	// 2. PruneStalePeers should evict the stale peer
	prunedCount, err := ps.PruneStalePeers()
	if err != nil {
		t.Fatalf("PruneStalePeers failed: %v", err)
	}
	if prunedCount != 1 {
		t.Fatalf("expected 1 peer pruned, got %d", prunedCount)
	}

	// Peer must be removed from known peers
	if ps.HasPeer(stalePeer) {
		t.Fatalf("stale peer %s was not evicted from peer tables", stalePeer)
	}

	// Peer must be marked unreachable with 2h cooldown
	if !ps.isPeerUnreachable(stalePeer) {
		t.Fatalf("evicted stale peer %s should be marked unreachable", stalePeer)
	}

	// AddPeer must reject re-adding while in unreachable cooldown
	if err := ps.AddPeer(stalePeer); err != nil {
		t.Fatalf("AddPeer unexpected error: %v", err)
	}
	if ps.HasPeer(stalePeer) {
		t.Fatalf("stale peer %s was re-added despite being in unreachable cooldown", stalePeer)
	}
}

// TestDHTStaleNodeEviction verifies that RoutingTable removes dead peers and filters stale nodes.
func TestDHTStaleNodeEviction(t *testing.T) {
	rt := NewRoutingTable("127.0.0.1:19335")
	deadNode := "198.51.100.99:19335"
	activeNode := "198.51.100.100:19335"

	rt.Update(deadNode)
	rt.Update(activeNode)

	// Simulate deadNode last seen 3 hours ago
	rt.mu.Lock()
	for cpl := range rt.Buckets {
		for i := range rt.Buckets[cpl] {
			if rt.Buckets[cpl][i].Address == deadNode {
				rt.Buckets[cpl][i].LastSeen = time.Now().Add(-3 * time.Hour)
			}
		}
	}
	rt.mu.Unlock()

	// FindClosestNodes must exclude nodes older than 2 hours
	closest := rt.FindClosestNodes(rt.LocalNode.ID, 10)
	for _, node := range closest {
		if node.Address == deadNode {
			t.Fatalf("FindClosestNodes returned stale node %s last seen 3h ago", deadNode)
		}
	}

	// Test explicit Remove
	rt.Remove(activeNode)
	closestAfterRemove := rt.FindClosestNodes(rt.LocalNode.ID, 10)
	for _, node := range closestAfterRemove {
		if node.Address == activeNode {
			t.Fatalf("FindClosestNodes returned active node %s after Remove", activeNode)
		}
	}
}

// TestLoadPeersFiltersStaleOnStartup verifies that loadPeers skips non-seed peers
// with failures that were last seen >= 2 hours ago.
func TestLoadPeersFiltersStaleOnStartup(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	storePath := ps.GetPeersPath()
	if storePath == "" {
		t.Fatal("empty store path")
	}

	staleAddr := "203.0.113.77:19335"
	freshAddr := "203.0.113.78:19335"

	store := VersionedPeerStore{
		Version:   1,
		NetworkID: "ARMX-TESTNET-1",
		Peers: []*PeerInfo{
			{
				Address:         staleAddr,
				LastSeen:        time.Now().Add(-4 * time.Hour),
				Failures:        2,
				Height:          100,
				NetworkID:       "ARMX-TESTNET-1",
				CertFingerprint: "abcdef1234567890",
			},
			{
				Address:         freshAddr,
				LastSeen:        time.Now().Add(-5 * time.Minute),
				Failures:        0,
				Height:          100,
				NetworkID:       "ARMX-TESTNET-1",
				CertFingerprint: "1234567890abcdef",
			},
		},
	}

	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, data, 0600); err != nil {
		t.Fatal(err)
	}

	restarted := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	restarted.loadPeers()

	if restarted.HasPeer(staleAddr) {
		t.Fatalf("restarted node loaded stale peer %s from disk", staleAddr)
	}
	if !restarted.HasPeer(freshAddr) {
		t.Fatalf("restarted node failed to load fresh peer %s from disk", freshAddr)
	}
}

// TestSyncBestPeerSkipsUnreachableAndStale verifies that SyncBestPeer does not dial
// peers that are in unreachable cooldown or marked stale.
func TestSyncBestPeerSkipsUnreachableAndStale(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer("0", bc, dir, "ARMX-TESTNET-1")
	unreachablePeer := "203.0.113.55:19335"

	// Add peer and mark unreachable
	ps.Lock.Lock()
	ps.newPeers[unreachablePeer] = &PeerInfo{
		Address:   unreachablePeer,
		LastSeen:  time.Now(),
		Failures:  1,
		NetworkID: "ARMX-TESTNET-1",
	}
	ps.rebuildPeersListLocked()
	ps.Lock.Unlock()

	ps.markPeerUnreachable(unreachablePeer, 2*time.Hour)

	if !ps.isPeerUnreachable(unreachablePeer) {
		t.Fatalf("expected peer %s to be unreachable", unreachablePeer)
	}

	// Calling SyncBestPeer should not attempt to connect or hang on unreachable peer
	done := make(chan struct{})
	go func() {
		ps.SyncBestPeer()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded promptly without dialing blocked peer
	case <-time.After(2 * time.Second):
		t.Fatalf("SyncBestPeer hung while dealing with unreachable peer")
	}
}

