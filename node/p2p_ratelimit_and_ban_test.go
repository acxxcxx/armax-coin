package node

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN15_BlockRangeRateLimiterAndBanScore verifies [N-15]:
// Requests to GET_BLOCK_RANGE are strictly rate limited (max 5/min), and abusive peers are banned.
func TestN15_BlockRangeRateLimiterAndBanScore(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	ps.BlockRangeBurst = 5.0 // Test token bucket rate limiting with capacity of 5
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)
	target := "127.0.0.1:" + port

	// 1. Send 5 valid GET_BLOCK_RANGE requests (should succeed)
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

	// 2. The 6th request within the same minute must be rate limited / rejected
	msg6 := Message{
		NetworkID: NetworkID(),
		Type:      "GET_BLOCK_RANGE",
		Payload:   BlockRangeRequest{FromIndex: 0, ToIndex: 0},
	}
	resp6, err := QueryP2P(target, msg6, 1*time.Second)
	if err == nil && resp6 != nil && resp6.Type == "BLOCK_RANGE_RESPONSE" {
		t.Fatalf("[N-15] 6th GET_BLOCK_RANGE request was accepted despite 5/min rate limit!")
	}
}

// TestN15_PeerBanningOnHighBanScore verifies that peer reaching ban score >= 100 is banned for 24h
func TestN15_PeerBanningOnHighBanScore(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)
	target := "127.0.0.1:" + port

	// Manually add 100 ban score to 127.0.0.1
	ps.AddBanScore("127.0.0.1", 100)

	if !ps.IsBanned("127.0.0.1") {
		t.Fatalf("Expected 127.0.0.1 to be marked as banned")
	}

	// Connecting to server from banned IP must immediately receive ban message and disconnect
	conn, err := net.DialTimeout("tcp", target, 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 128)
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, rErr := conn.Read(buf)
	if rErr != nil {
		t.Fatalf("Expected ban error response from server, got error: %v", rErr)
	}
	out := string(buf[:n])
	if out != "ERROR: Peer is banned\n" {
		t.Fatalf("[N-15] Expected 'ERROR: Peer is banned\\n', got: %q", out)
	}
}

// TestN15_TokenBucketPacketFlooding verifies that global packet rate exceeding token capacity (20/s, burst 30) is dropped
func TestN15_TokenBucketPacketFlooding(t *testing.T) {
	ps := &PeerServer{
		banScores:    make(map[string]int),
		bannedIPs:    make(map[string]time.Time),
		rateTrackers: make(map[string]*peerRateTracker),
	}

	testIP := "10.0.0.1"

	// Exhaust initial 30 burst tokens
	for i := 0; i < 30; i++ {
		if !ps.AllowMessage(testIP, "GET_HEIGHT") {
			t.Fatalf("Expected token %d to be permitted within burst limit", i+1)
		}
	}

	// 31st immediate message must be rejected by token bucket
	if ps.AllowMessage(testIP, "GET_HEIGHT") {
		t.Fatalf("[N-15] Message permitted when token bucket is exhausted!")
	}
}
