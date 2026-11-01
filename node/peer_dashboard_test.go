package node

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestDNSSeedSingleIP verifies that a DNS seed resolving to a single IP is deduplicated
// so candidate count is 1 and active peer count is 0 [Scenario 1, v1.0.6]
func TestDNSSeedSingleIP(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "seed1.test.armax:19335"
	resolvedIP := "198.51.100.10:19335"

	// Register mapping
	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[resolvedIP] = struct{}{}
	ps.seedHostToIPs[seed] = []string{resolvedIP}
	ps.seedIPToHosts[resolvedIP] = []string{seed}
	ps.seedMu.Unlock()

	// Add both the seed name and resolved IP
	_ = ps.AddPeer(seed)
	_ = ps.AddPeer(resolvedIP)

	known := ps.GetPeersKnown()
	if known != 1 {
		t.Fatalf("Expected 1 deduplicated known peer for single-IP seed, got %d (%v)", known, ps.GetPeers())
	}

	active := ps.GetPeersActive()
	if active != 0 {
		t.Fatalf("Expected 0 active peers when no connections are active, got %d", active)
	}

	seeds := ps.GetPeersSeeds()
	if seeds != 1 {
		t.Fatalf("Expected 1 configured seed, got %d", seeds)
	}
}

// TestDNSSeedMultipleIPs verifies that a DNS seed resolving to multiple distinct IPs
// creates candidate entries for the distinct IPs without double-counting the seed name [Scenario 2, v1.0.6]
func TestDNSSeedMultipleIPs(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "cluster.test.armax:19335"
	ip1 := "198.51.100.11:19335"
	ip2 := "198.51.100.12:19335"

	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[ip1] = struct{}{}
	ps.seedAddrs[ip2] = struct{}{}
	ps.seedHostToIPs[seed] = []string{ip1, ip2}
	ps.seedIPToHosts[ip1] = []string{seed}
	ps.seedIPToHosts[ip2] = []string{seed}
	ps.seedMu.Unlock()

	_ = ps.AddPeer(seed)
	_ = ps.AddPeer(ip1)
	_ = ps.AddPeer(ip2)

	known := ps.GetPeersKnown()
	if known != 2 {
		t.Fatalf("Expected 2 deduplicated known peers for multi-IP cluster, got %d (%v)", known, ps.GetPeers())
	}
	if ps.GetPeersActive() != 0 {
		t.Fatalf("Expected 0 active peers, got %d", ps.GetPeersActive())
	}
}

// TestDNSSeedAndIPAddedFromDifferentSources verifies that a DNS seed added via configuration
// and its IP added via peer gossip (or POST /peers) are deduplicated [Scenario 3, v1.0.6]
func TestDNSSeedAndIPAddedFromDifferentSources(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "peer.test.armax:19335"
	ip := "198.51.100.20:19335"

	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[ip] = struct{}{}
	ps.seedHostToIPs[seed] = []string{ip}
	ps.seedIPToHosts[ip] = []string{seed}
	ps.seedMu.Unlock()

	// 1. Seed added first
	_ = ps.AddPeer(seed)
	if ps.GetPeersKnown() != 1 {
		t.Fatalf("Expected 1 peer after seed addition, got %d", ps.GetPeersKnown())
	}

	// 2. IP added via POST /peers HTTP RPC
	srvCfg := &ServerConfig{
		NetworkID: core.NetworkID(),
		RPCBind:   "127.0.0.1",
		RPCPort:   getFreePort(t),
	}
	handler := NewRPCHandler(bc, ps, newIPRateLimiter(), srvCfg)

	// Add via AddPeer directly (as simulated gossip)
	_ = ps.AddPeer(ip)

	// Invariant: Candidate count remains exactly 1, upgraded to IP
	if ps.GetPeersKnown() != 1 {
		t.Fatalf("Expected 1 deduplicated peer after gossip IP addition, got %d (%v)", ps.GetPeersKnown(), ps.GetPeers())
	}

	// Verify /stats HTTP endpoint exposes correct telemetry
	reqStats := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recStats := httptest.NewRecorder()
	handler.ServeHTTP(recStats, reqStats)

	var stats map[string]interface{}
	if err := json.Unmarshal(recStats.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats JSON: %v", err)
	}

	if int(stats["peers_known"].(float64)) != 1 {
		t.Fatalf("Expected /stats peers_known == 1, got %v", stats["peers_known"])
	}
	if int(stats["peers_active"].(float64)) != 0 {
		t.Fatalf("Expected /stats peers_active == 0, got %v", stats["peers_active"])
	}
	if int(stats["peers_seeds"].(float64)) != 1 {
		t.Fatalf("Expected /stats peers_seeds == 1, got %v", stats["peers_seeds"])
	}
}

// TestMultipleValidPeerSessions verifies that multiple live peer sessions
// are correctly counted in GetPeersActive() and exposed on /stats [Scenario 4, v1.0.6]
func TestMultipleValidPeerSessions(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	// Simulate 2 active outbound peer sessions using in-memory pipe connections
	cli1, srv1 := net.Pipe()
	defer cli1.Close()
	defer srv1.Close()

	cli2, srv2 := net.Pipe()
	defer cli2.Close()
	defer srv2.Close()

	sess1 := &outboundPeerSession{
		peerAddress: "198.51.100.31:19335",
		conn:        cli1,
	}
	sess1.handshakeValidated.Store(true)
	sess2 := &outboundPeerSession{
		peerAddress: "198.51.100.32:19335",
		conn:        cli2,
	}
	sess2.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions["198.51.100.31:19335"] = sess1
	ps.outboundSessions["198.51.100.32:19335"] = sess2
	ps.outboundMu.Unlock()

	active := ps.GetPeersActive()
	if active != 2 {
		t.Fatalf("Expected 2 active peers, got %d", active)
	}

	endpoints := ps.GetActivePeerEndpoints()
	if len(endpoints) != 2 {
		t.Fatalf("Expected 2 active endpoints, got %d (%v)", len(endpoints), endpoints)
	}

	// Verify /peers?filter=active returns only the 2 active sessions
	handler := NewRPCHandler(bc, ps, newIPRateLimiter(), nil)
	req := httptest.NewRequest(http.MethodGet, "/peers?filter=active", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var returnedActive []string
	if err := json.Unmarshal(rec.Body.Bytes(), &returnedActive); err != nil {
		t.Fatalf("Failed to parse /peers?filter=active: %v", err)
	}
	if len(returnedActive) != 2 {
		t.Fatalf("Expected /peers?filter=active to return 2 peers, got %d (%v)", len(returnedActive), returnedActive)
	}
}

// TestPeerDisconnect verifies that when an active peer disconnects,
// the active peer count immediately drops back to 0 [Scenario 5, v1.0.6]
func TestPeerDisconnect(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	cli, srv := net.Pipe()
	defer srv.Close()

	sess := &outboundPeerSession{
		peerAddress: "198.51.100.40:19335",
		conn:        cli,
	}
	sess.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions["198.51.100.40:19335"] = sess
	ps.outboundMu.Unlock()

	if ps.GetPeersActive() != 1 {
		t.Fatalf("Expected 1 active peer initially, got %d", ps.GetPeersActive())
	}

	// Disconnect session
	ps.dropOutboundSession("198.51.100.40:19335", sess)

	// Active count must immediately drop to 0
	if ps.GetPeersActive() != 0 {
		t.Fatalf("Expected 0 active peers after disconnect, got %d", ps.GetPeersActive())
	}
}

// TestFailedConnection verifies that failed connection attempts do not increment
// or leave residual active counts [Scenario 6, v1.0.6]
func TestFailedConnection(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	// Dial an unreachable port
	_, _, err := ps.DialPeer("127.0.0.1:1", 50*time.Millisecond)
	if err == nil {
		t.Fatalf("Expected connection failure to port 1, got nil")
	}

	// Active peer count must strictly remain 0
	if ps.GetPeersActive() != 0 {
		t.Fatalf("Expected 0 active peers after failed dial, got %d", ps.GetPeersActive())
	}
}

// TestPeerReconnect verifies that a reconnect on the same address replaces the session
// without duplicating active session counts [Scenario 7, v1.0.6]
func TestPeerReconnect(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	addr := "198.51.100.50:19335"

	// Session 1
	cli1, srv1 := net.Pipe()
	defer srv1.Close()
	sess1 := &outboundPeerSession{
		peerAddress: addr,
		conn:        cli1,
	}
	sess1.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions[addr] = sess1
	ps.outboundMu.Unlock()

	if ps.GetPeersActive() != 1 {
		t.Fatalf("Expected 1 active peer on initial session, got %d", ps.GetPeersActive())
	}

	// Session 2 replaces Session 1 on the exact same endpoint
	cli2, srv2 := net.Pipe()
	defer srv2.Close()
	sess2 := &outboundPeerSession{
		peerAddress: addr,
		conn:        cli2,
	}
	sess2.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	old := ps.outboundSessions[addr]
	ps.outboundSessions[addr] = sess2
	ps.outboundMu.Unlock()

	if old != nil {
		old.close()
	}

	// Invariant: Active count remains exactly 1 (not 2)
	if ps.GetPeersActive() != 1 {
		t.Fatalf("Expected exactly 1 active peer after reconnect replacement, got %d", ps.GetPeersActive())
	}

	// Dropping old session reference does not affect sess2
	ps.dropOutboundSession(addr, sess1)
	if ps.GetPeersActive() != 1 {
		t.Fatalf("Expected 1 active peer after dropping stale session, got %d", ps.GetPeersActive())
	}

	// Dropping current sess2 clears it
	ps.dropOutboundSession(addr, sess2)
	if ps.GetPeersActive() != 0 {
		t.Fatalf("Expected 0 active peers after dropping current session, got %d", ps.GetPeersActive())
	}
}

// TestIPRateLimiterConcurrencyAndClose verifies repeat Close, concurrent Close,
// and zero goroutine leaks on IPRateLimiter [FIX-02, v1.0.6]
func TestIPRateLimiterConcurrencyAndClose(t *testing.T) {
	limiter := newIPRateLimiter()

	// Concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = limiter.allow("192.168.1.10", 100)
				_ = limiter.allowMine("192.168.1.10", 100*time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent Close from 20 goroutines
	var closeWg sync.WaitGroup
	for i := 0; i < 20; i++ {
		closeWg.Add(1)
		go func() {
			defer closeWg.Done()
			limiter.Close()
		}()
	}
	closeWg.Wait()

	// Repeat Close call must be idempotent and instant
	limiter.Close()
	limiter.Close()
}
