package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestInboundOnlyPeerCountsActive tests that a node that only receives incoming TCP connections
// correctly counts peers_active >= 1 in both PeerServer and /stats RPC [L4-8]
func TestInboundOnlyPeerCountsActive(t *testing.T) {
	// Pick a free local port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	baseDir := t.TempDir()
	dbPath := filepath.Join(baseDir, "blockchain.db")

	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(port, bc, filepath.Join(baseDir, "server"))
	ps.Peers = []string{}
	ps.AllowPrivatePeers = true
	ps.DisableSubnetLimit = true
	go ps.StartServer()
	defer ps.Stop()

	// Wait for server to be ready
	var conn net.Conn
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("Failed to establish inbound connection to PeerServer: %v", err)
	}
	defer conn.Close()

	// A bare TCP connection is not a validated peer session.
	time.Sleep(100 * time.Millisecond)

	active := ps.GetPeersActive()
	if active != 0 {
		t.Fatalf("Expected peers_active == 0 before TLS/application handshake, got %d", active)
	}

	for _, path := range []string{"/stats", "/status", "/peers?filter=active"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		NewRPCHandler(bc, ps, nil, nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected %s code 200, got %d", path, rec.Code)
		}
		if strings.HasPrefix(path, "/peers") {
			var peers []string
			if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
				t.Fatalf("Failed to parse %s: %v", path, err)
			}
			if len(peers) != 0 {
				t.Fatalf("Expected no active peers from %s before handshake, got %v", path, peers)
			}
			continue
		}
		var stats map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse %s: %v", path, err)
		}
		if int(stats["peers_active"].(float64)) != 0 {
			t.Fatalf("Expected %s peers_active == 0 before handshake, got %v", path, stats["peers_active"])
		}
	}

	// Close inbound connection and verify peers_active returns to 0
	_ = conn.Close()
	var remaining int
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(100 * time.Millisecond)
		remaining = ps.GetPeersActive()
		if remaining == 0 {
			break
		}
	}
	if remaining != 0 {
		t.Fatalf("Expected peers_active == 0 after disconnect, got %d", remaining)
	}
}

// TestOutboundOnlyPeerCountsActive verifies that outbound connections initiated by the node
// are also correctly registered in peers_active and reflected on /stats [L4-8]
func TestOutboundOnlyPeerCountsActive(t *testing.T) {
	// 1. Setup peer server B (target listening node)
	portB := getFreePort(t)
	dbPathB := filepath.Join(t.TempDir(), "blockchain.db")
	bcB, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain B: %v", err)
	}
	defer bcB.Close()

	psB := NewPeerServer(portB, bcB, filepath.Join(t.TempDir(), "peer-b"))
	psB.Peers = []string{}
	psB.AllowPrivatePeers = true
	psB.DisableSubnetLimit = true
	psB.RequireTLS = true
	psB.P2PTOFU = true
	go psB.StartServer()
	defer psB.Stop()

	time.Sleep(100 * time.Millisecond)

	// 2. Setup peer server A (the node initiating the outbound connection)
	portA := getFreePort(t)
	dbPathA := filepath.Join(t.TempDir(), "blockchain.db")
	bcA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain A: %v", err)
	}
	defer bcA.Close()

	psA := NewPeerServer(portA, bcA, filepath.Join(t.TempDir(), "peer-a"))
	psA.Peers = []string{}
	psA.AllowPrivatePeers = true
	psA.DisableSubnetLimit = true
	psA.RequireTLS = true
	psA.P2PTOFU = true
	go psA.StartServer()
	defer psA.Stop()

	time.Sleep(100 * time.Millisecond)

	// 3. Node A initiates outbound connection to Node B
	transportConn, releaseTransport, err := psA.DialPeer("127.0.0.1:"+portB, 2*time.Second)
	if err != nil {
		t.Fatalf("TLS/TOFU transport setup failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if psA.GetPeersActive() != 0 || psB.GetPeersActive() != 0 {
		t.Fatalf("TLS/TOFU without application handshake counted active: outbound=%d inbound=%d", psA.GetPeersActive(), psB.GetPeersActive())
	}
	_ = transportConn.Close()
	releaseTransport()

	result, err := psA.QueryPeer("127.0.0.1:"+portB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("GET_HEIGHT handshake failed: %v", err)
	}
	if result == nil || result.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Expected HEIGHT_RESPONSE handshake, got %#v", result)
	}
	waitForPeerActiveCount(t, psB, 1)

	active := psA.GetPeersActive()
	if active < 1 {
		t.Fatalf("Expected peers_active >= 1 on node initiating outbound connection, got %d", active)
	}

	// 4. Verify /stats reflects outbound peer in peers_active
	for _, path := range []string{"/stats", "/status", "/peers?filter=active"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		NewRPCHandler(bcA, psA, nil, nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected %s code 200, got %d", path, rec.Code)
		}
		if strings.HasPrefix(path, "/peers") {
			var peers []string
			if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil || len(peers) != 1 {
				t.Fatalf("Expected one validated active peer from %s, got %v (err %v)", path, peers, err)
			}
			continue
		}
		var stats map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse %s: %v", path, err)
		}
		if int(stats["peers_active"].(float64)) < 1 {
			t.Fatalf("Expected %s peers_active >= 1, got %v", path, stats["peers_active"])
		}
	}

	psA.outboundMu.Lock()
	session := psA.outboundSessions["127.0.0.1:"+portB]
	psA.outboundMu.Unlock()
	psA.dropOutboundSession("127.0.0.1:"+portB, session)
	for attempt := 0; attempt < 20 && psB.GetPeersActive() != 0; attempt++ {
		time.Sleep(50 * time.Millisecond)
	}
	if psB.GetPeersActive() != 0 || psA.GetPeersActive() != 0 {
		t.Fatalf("Expected both peers to become inactive after disconnect; outbound=%d inbound=%d", psA.GetPeersActive(), psB.GetPeersActive())
	}

	result, err = psA.QueryPeer("127.0.0.1:"+portB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 2*time.Second)
	if err != nil || result == nil || result.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Expected successful reconnect handshake, got response %#v and error %v", result, err)
	}
	waitForPeerActiveCount(t, psB, 1)
}
