package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT1_RateLimitActiveWhenPeersKnown verifies that AllowMessage and rate limiting
// remain strictly active even when the node has known peers (p.GetPeersKnown() > 0).
func TestT1_RateLimitActiveWhenPeersKnown(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)

	// Simulate node having connected peers
	ps.Peers = []string{"198.51.100.1:9000", "198.51.100.2:9000"}
	if ps.GetPeersKnown() == 0 {
		t.Fatalf("Expected GetPeersKnown() > 0, got 0")
	}

	go ps.StartServer()
	time.Sleep(100 * time.Millisecond)
	target := "127.0.0.1:" + port

	// 1. Send 5 valid GET_BLOCK_RANGE requests
	for i := 0; i < 5; i++ {
		msg := Message{
			NetworkID: NetworkID(),
			Type:      "GET_BLOCK_RANGE",
			Payload:   BlockRangeRequest{FromIndex: 0, ToIndex: 0},
		}
		resp, err := QueryP2P(target, msg, 2*time.Second)
		if err != nil || resp.Type != "BLOCK_RANGE_RESPONSE" {
			t.Fatalf("Request #%d under rate limit failed: %v", i+1, err)
		}
	}

	// 2. The 6th request within the same minute MUST be rate limited / rejected
	// Even though peers > 0!
	msg6 := Message{
		NetworkID: NetworkID(),
		Type:      "GET_BLOCK_RANGE",
		Payload:   BlockRangeRequest{FromIndex: 0, ToIndex: 0},
	}
	resp6, err := QueryP2P(target, msg6, 1*time.Second)
	if err == nil && resp6 != nil && resp6.Type == "BLOCK_RANGE_RESPONSE" {
		t.Fatalf("[T1 FAILING TEST] 6th GET_BLOCK_RANGE request was accepted because GetPeersKnown() > 0 bypassed AllowMessage!")
	}
}

// TestT1_FloodGetHeightDropsAndBans verifies that flooding GET_HEIGHT messages
// from an IP is throttled and causes the sender's ban score to increase.
func TestT1_FloodGetHeightDropsAndBans(t *testing.T) {
	ps := &PeerServer{
		banScores:    make(map[string]int),
		bannedIPs:    make(map[string]time.Time),
		rateTrackers: make(map[string]*peerRateTracker),
	}

	testIP := "198.51.100.5"

	// Initial burst (30 tokens) should pass
	passed := 0
	dropped := 0
	for i := 0; i < 1000; i++ {
		if ps.AllowMessage(testIP, "GET_HEIGHT") {
			passed++
		} else {
			dropped++
		}
	}

	if passed > 35 {
		t.Fatalf("Expected at most initial burst (~30) to pass, but %d passed", passed)
	}
	if dropped < 950 {
		t.Fatalf("Expected vast majority of 1000 flood packets to be dropped, got %d dropped", dropped)
	}

	// Ban score should have risen significantly and resulted in a ban
	ps.banLock.RLock()
	score := ps.banScores[testIP]
	banned := false
	if exp, ok := ps.bannedIPs[testIP]; ok && time.Now().Before(exp) {
		banned = true
	}
	ps.banLock.RUnlock()

	if score < 100 || !banned {
		t.Fatalf("Expected flooder to reach ban threshold (score=%d, banned=%v)", score, banned)
	}
}

// TestT1_SyncBlocksNormalDoesNotBan verifies that streaming NEW_BLOCK messages
// at normal sync pace (e.g. 500 blocks with dedicated bucket) does NOT trigger a ban.
func TestT1_SyncBlocksNormalDoesNotBan(t *testing.T) {
	ps := &PeerServer{
		banScores:    make(map[string]int),
		bannedIPs:    make(map[string]time.Time),
		rateTrackers: make(map[string]*peerRateTracker),
	}

	testIP := "198.51.100.10"

	// Stream 500 blocks with simulated 25ms pacing
	for i := 0; i < 500; i++ {
		// Advance tracker time or simulate 25ms gap every few blocks
		if !ps.AllowMessage(testIP, "NEW_BLOCK") {
			t.Fatalf("NEW_BLOCK #%d was unexpectedly dropped during sync!", i+1)
		}
		// Simulate small delay every 10 blocks (pacing)
		time.Sleep(20 * time.Millisecond)
	}

	if ps.IsBanned(testIP) {
		t.Fatalf("Honest sync peer was banned during normal 500-block sync!")
	}
}
