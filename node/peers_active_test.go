package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	dbPath := filepath.Join(t.TempDir(), "blockchain.db")

	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(port, bc)
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

	// Wait for connection handler to register active connection
	time.Sleep(100 * time.Millisecond)

	active := ps.GetPeersActive()
	if active < 1 {
		t.Fatalf("Expected peers_active >= 1 on node receiving inbound connection, got %d", active)
	}

	// Verify /stats HTTP RPC endpoint returns peers_active >= 1 and peers_known
	handler := NewRPCHandler(bc, ps, nil, nil)
	req := httptest.NewRequest("GET", "/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected /stats code 200, got %d", rec.Code)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats json: %v", err)
	}

	activeFromStats, ok := stats["peers_active"].(float64)
	if !ok || int(activeFromStats) < 1 {
		t.Fatalf("Expected /stats peers_active >= 1, got %v", stats["peers_active"])
	}

	if _, ok := stats["peers_known"]; !ok {
		t.Fatalf("Expected /stats to contain peers_known")
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

	psB := NewPeerServer(portB, bcB)
	psB.Peers = []string{}
	psB.AllowPrivatePeers = true
	psB.DisableSubnetLimit = true
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

	psA := NewPeerServer(portA, bcA)
	psA.Peers = []string{}
	psA.AllowPrivatePeers = true
	psA.DisableSubnetLimit = true
	go psA.StartServer()
	defer psA.Stop()

	time.Sleep(100 * time.Millisecond)

	// 3. Node A initiates outbound connection to Node B
	connOut, release, err := psA.DialPeer("127.0.0.1:"+portB, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to dial peer B: %v", err)
	}
	defer func() {
		_ = connOut.Close()
		release()
	}()

	active := psA.GetPeersActive()
	if active < 1 {
		t.Fatalf("Expected peers_active >= 1 on node initiating outbound connection, got %d", active)
	}

	// 4. Verify /stats reflects outbound peer in peers_active
	handlerA := NewRPCHandler(bcA, psA, nil, nil)
	req := httptest.NewRequest("GET", "/stats", nil)
	rec := httptest.NewRecorder()
	handlerA.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected /stats code 200, got %d", rec.Code)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats json: %v", err)
	}

	activeFromStats, ok := stats["peers_active"].(float64)
	if !ok || int(activeFromStats) < 1 {
		t.Fatalf("Expected /stats peers_active >= 1, got %v", stats["peers_active"])
	}
}
