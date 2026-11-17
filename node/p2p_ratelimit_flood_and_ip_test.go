package node

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	ps.BlockRangeBurst = 5.0 // Test token bucket rate limiting with capacity of 5
	ps.BlockRangeRefillRate = 0.001

	// Simulate node having connected peers
	ps.Peers = []string{"198.51.100.1:19334", "198.51.100.2:19334"}
	if ps.GetPeersKnown() == 0 {
		t.Fatalf("Expected GetPeersKnown() > 0, got 0")
	}

	defer ps.Stop()
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

// TestAF06_IPRateLimiterCapAndExpiredEviction deterministically verifies that:
//  1. maxRateLimiterIPs (10,000) cap is enforced for both general RPC requests and mining requests.
//  2. Expired entries in requests (>1m) and lastMine (>10m) are evicted when the map reaches capacity.
func TestAF06_IPRateLimiterCapAndExpiredEviction(t *testing.T) {
	limiter := newIPRateLimiter()
	defer limiter.Close()

	now := time.Now()
	limiter.mu.Lock()
	for i := 0; i < maxRateLimiterIPs; i++ {
		ip := fmt.Sprintf("198.18.%d.%d", i/256, i%256)
		if i < 50 {
			// 50 expired entries (> 1 minute old)
			limiter.requests[ip] = []time.Time{now.Add(-2 * time.Minute)}
		} else {
			limiter.requests[ip] = []time.Time{now}
		}
		if i < 25 {
			// 25 expired mining entries (> 10 minutes old)
			limiter.lastMine[ip] = now.Add(-11 * time.Minute)
		} else {
			limiter.lastMine[ip] = now
		}
	}
	limiter.mu.Unlock()

	// 1. Trigger allow() on a new IP: should evict the 50 expired entries and admit the new IP
	if !limiter.allow("203.0.113.250", 120) {
		t.Fatalf("expected limiter.allow to evict expired entries and admit new IP")
	}
	limiter.mu.Lock()
	reqLenAfterEvict := len(limiter.requests)
	// Refill back to maxRateLimiterIPs with fresh timestamps
	for i := 0; len(limiter.requests) < maxRateLimiterIPs; i++ {
		ip := fmt.Sprintf("198.19.%d.%d", i/256, i%256)
		limiter.requests[ip] = []time.Time{time.Now()}
	}
	limiter.mu.Unlock()

	if reqLenAfterEvict != maxRateLimiterIPs-50+1 {
		t.Fatalf("expected %d entries after evicting 50 expired and adding 1, got %d", maxRateLimiterIPs-49, reqLenAfterEvict)
	}

	// 2. With all 10,000 entries active (unexpired), a new IP must be rejected by the cap
	if limiter.allow("203.0.113.251", 120) {
		t.Fatalf("expected limiter.allow to reject new IP when all %d slots are active", maxRateLimiterIPs)
	}

	// 3. Trigger allowMine() on a new IP: should evict the 25 expired entries and admit the new IP
	if !limiter.allowMine("203.0.113.250", 2*time.Second) {
		t.Fatalf("expected limiter.allowMine to evict expired entries and admit new IP")
	}
	limiter.mu.Lock()
	mineLenAfterEvict := len(limiter.lastMine)
	for i := 0; len(limiter.lastMine) < maxRateLimiterIPs; i++ {
		ip := fmt.Sprintf("198.19.%d.%d", i/256, i%256)
		limiter.lastMine[ip] = time.Now()
	}
	limiter.mu.Unlock()

	if mineLenAfterEvict != maxRateLimiterIPs-25+1 {
		t.Fatalf("expected %d lastMine entries after evicting 25 expired and adding 1, got %d", maxRateLimiterIPs-24, mineLenAfterEvict)
	}

	// 4. With all 10,000 lastMine slots active, a new IP must be rejected
	if limiter.allowMine("203.0.113.251", 2*time.Second) {
		t.Fatalf("expected limiter.allowMine to reject new IP when all %d slots are active", maxRateLimiterIPs)
	}
}

// TestAF06_IPv4AndIPv6LoopbackHandling verifies host validation and loopback rate-limiting
// behavior across IPv4 (127.0.0.1:port) and IPv6 ([::1]:port) addresses.
func TestAF06_IPv4AndIPv6LoopbackHandling(t *testing.T) {
	for _, validHost := range []string{"127.0.0.1:19333", "[::1]:19333", "::1", "localhost:19333"} {
		if !isAllowedHost(validHost, "") {
			t.Fatalf("expected isAllowedHost(%q) == true", validHost)
		}
	}
	if isAllowedHost("attacker.example.com:19333", "") {
		t.Fatalf("expected isAllowedHost to reject external DNS host")
	}

	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	limiter := newIPRateLimiter()
	defer limiter.Close()

	InitRPCAuth("testuser", "testpass-strong-123")
	cfg := &ServerConfig{
		NetworkID:   "ARMX-TESTNET-1",
		RPCBind:     "127.0.0.1",
		RPCUser:     "testuser",
		RPCPassword: "testpass-strong-123",
	}
	handler := NewRPCHandler(bc, nil, limiter, cfg)

	// Back-to-back /mine requests from IPv4 loopback (127.0.0.1:54321) and IPv6 loopback ([::1]:54321)
	// must bypass the 2-second remote mining cooldown and reach parameter validation (HTTP 400).
	for _, loopAddr := range []string{"127.0.0.1:54321", "[::1]:54321"} {
		for attempt := 1; attempt <= 2; attempt++ {
			req := httptest.NewRequest(http.MethodPost, "/mine", strings.NewReader(`{}`))
			req.Host = "[::1]:19333"
			req.RemoteAddr = loopAddr
			req.SetBasicAuth("testuser", "testpass-strong-123")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("loopback %s attempt #%d: got HTTP %d, want 400 BadRequest (body=%s)", loopAddr, attempt, rec.Code, rec.Body.String())
			}
		}
	}

	// Non-loopback IPv6 ([2001:db8::9]:54321) and IPv4 (203.0.113.9:54321) must hit the 2s cooldown (HTTP 429) on the 2nd request
	for _, remoteAddr := range []string{"203.0.113.9:54321", "[2001:db8::9]:54321"} {
		req1 := httptest.NewRequest(http.MethodPost, "/mine", strings.NewReader(`{}`))
		req1.Host = "127.0.0.1:19333"
		req1.RemoteAddr = remoteAddr
		req1.SetBasicAuth("testuser", "testpass-strong-123")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusBadRequest {
			t.Fatalf("remote %s first attempt: got HTTP %d, want 400", remoteAddr, rec1.Code)
		}

		req2 := httptest.NewRequest(http.MethodPost, "/mine", strings.NewReader(`{}`))
		req2.Host = "127.0.0.1:19333"
		req2.RemoteAddr = remoteAddr
		req2.SetBasicAuth("testuser", "testpass-strong-123")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Fatalf("remote %s second immediate attempt: got HTTP %d, want 429 TooManyRequests", remoteAddr, rec2.Code)
		}
	}
}

// TestAF06_LocalMainnetPostPeersRejectsNonLoopbackBeforeDNS verifies that in Local Mainnet mode,
// POST /peers rejects DNS hostnames and non-loopback IPs without adding them or resolving DNS,
// while accepting valid IPv4 and IPv6 loopback peer addresses.
func TestAF06_LocalMainnetPostPeersRejectsNonLoopbackBeforeDNS(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()

	ps := NewPeerServer("19334", bc, dir, "ARMX-MAINNET-1")
	ps.AllowPrivatePeers = true

	limiter := newIPRateLimiter()
	defer limiter.Close()

	InitRPCAuth("admin", "localmainnet-pass-123")
	cfg := &ServerConfig{
		NetworkID:   "ARMX-MAINNET-1",
		RPCBind:     "127.0.0.1",
		RPCUser:     "admin",
		RPCPassword: "localmainnet-pass-123",
	}
	handler := NewRPCHandler(bc, ps, limiter, cfg)

	rejectedPeers := []string{
		"seed.external-domain.invalid:19334",
		"203.0.113.44:19334",
		"[2001:db8::44]:19334",
		"192.168.1.50:19334",
	}
	for _, badPeer := range rejectedPeers {
		body := fmt.Sprintf(`{"peer":%q}`, badPeer)
		req := httptest.NewRequest(http.MethodPost, "/peers", bytes.NewBufferString(body))
		req.Host = "127.0.0.1:19332"
		req.RemoteAddr = "127.0.0.1:55111"
		req.SetBasicAuth("admin", "localmainnet-pass-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /peers for %q returned HTTP %d", badPeer, rec.Code)
		}
		if ps.HasPeer(badPeer) || ps.GetPeersKnown() != 0 {
			t.Fatalf("Local Mainnet POST /peers accepted non-loopback peer %q (known=%d)", badPeer, ps.GetPeersKnown())
		}
	}

	acceptedLoopbacks := []string{
		"127.0.0.1:29334",
		"[::1]:29336",
	}
	for _, goodPeer := range acceptedLoopbacks {
		body := fmt.Sprintf(`{"peer":%q}`, goodPeer)
		req := httptest.NewRequest(http.MethodPost, "/peers", bytes.NewBufferString(body))
		req.Host = "127.0.0.1:19332"
		req.RemoteAddr = "127.0.0.1:55111"
		req.SetBasicAuth("admin", "localmainnet-pass-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /peers for %q returned HTTP %d", goodPeer, rec.Code)
		}
		if !ps.HasPeer(goodPeer) {
			t.Fatalf("Local Mainnet POST /peers failed to accept valid loopback peer %q", goodPeer)
		}
	}
}
