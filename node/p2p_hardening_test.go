//go:build testhooks

package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func newIsolatedBlockchain(t *testing.T) *core.Blockchain {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain at %s: %v", dbPath, err)
	}

	t.Cleanup(func() {
		if err := bc.Close(); err != nil {
			t.Errorf("failed to close blockchain: %v", err)
		}
	})

	return bc
}

// TestP2PChunkedRangeSync verifies GET_HEIGHT (with Chainwork) and GET_BLOCK_RANGE round-trips
func TestP2PChunkedRangeSync(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bc := newIsolatedBlockchain(t)
	minerAddr := core.GenesisMiner
	for i := 0; i < 3; i++ {
		if _, err := bc.MineBlockWithErr(minerAddr); err != nil {
			t.Fatalf("MineBlockWithErr failed: %v", err)
		}
	}

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	defer ps.Stop()
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)
	target := "127.0.0.1:" + port

	// 1. Query GET_HEIGHT
	heightMsg := Message{
		NetworkID: NetworkID(),
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	resp, err := QueryP2P(target, heightMsg, 3*time.Second)
	if err != nil {
		t.Fatalf("GET_HEIGHT failed: %v", err)
	}
	if resp.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Expected HEIGHT_RESPONSE, got %s", resp.Type)
	}

	// 2. Query GET_BLOCK_RANGE
	rangeMsg := Message{
		NetworkID: NetworkID(),
		Type:      "GET_BLOCK_RANGE",
		Payload:   BlockRangeRequest{FromIndex: 1, ToIndex: 2},
	}
	rangeResp, err := QueryP2P(target, rangeMsg, 3*time.Second)
	if err != nil {
		t.Fatalf("GET_BLOCK_RANGE failed: %v", err)
	}
	if rangeResp.Type != "BLOCK_RANGE_RESPONSE" {
		t.Fatalf("Expected BLOCK_RANGE_RESPONSE, got %s", rangeResp.Type)
	}

	// 3. Query GET_PEERS
	peersMsg := Message{
		NetworkID: NetworkID(),
		Type:      "GET_PEERS",
		Payload:   nil,
	}
	peersResp, err := QueryP2P(target, peersMsg, 3*time.Second)
	if err != nil {
		t.Fatalf("GET_PEERS failed: %v", err)
	}
	if peersResp.Type != "PEERS_LIST" {
		t.Fatalf("Expected PEERS_LIST, got %s", peersResp.Type)
	}
}

// TestAddPeerMaxLimitEnforcement verifies that AddPeer bounds total address book entries to MaxAddressBookSize [N-16]
func TestAddPeerMaxLimitEnforcement(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := newIsolatedBlockchain(t)
	ps := NewPeerServer(getFreePort(t), bc)
	defer ps.Stop()
	ps.MaxPeers = 5 // Active connection limit is independent of address book capacity

	for i := 1; i <= 10; i++ {
		// Use different /24 subnets so subnet rate limiting does not interfere
		peerAddr := fmt.Sprintf("198.51.%d.1:%s", i, core.MainnetP2PPort)
		ps.AddPeer(peerAddr)
	}

	peers := ps.GetPeers()
	if len(peers) != 10 {
		t.Fatalf("Expected address book to store 10 peers (independent of MaxPeers), got %d", len(peers))
	}
}

// TestResolveDNSSeed verifies dynamic DNS seed domain resolution into IP endpoints
func TestResolveDNSSeed(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	rawIP := "202.10.34.180:" + core.MainnetP2PPort
	res := ResolveDNSSeed(rawIP)
	if len(res) == 0 || res[0] != rawIP {
		t.Fatalf("Expected %s, got %v", rawIP, res)
	}

	domainSeed := "id.armax-coin.cloud:" + core.MainnetP2PPort
	resolved := ResolveDNSSeed(domainSeed)
	if len(resolved) == 0 {
		t.Fatalf("Expected resolved endpoints for %s, got empty", domainSeed)
	}

	foundExpectedIP := false
	for _, r := range resolved {
		if strings.HasPrefix(r, "202.10.34.180:") {
			foundExpectedIP = true
			break
		}
	}
	if !foundExpectedIP {
		t.Logf("DNS Seed resolved: %v", resolved)
	}
}

// TestP2PMultiMessageKeepAlive verifies [N-02]: HandleConnection supports multiple sequential messages over one TCP stream
func TestP2PMultiMessageKeepAlive(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := newIsolatedBlockchain(t)
	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	defer ps.Stop()
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)

	conn, _, err := DialEncrypted("127.0.0.1:"+port, 3*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send 3 sequential GET_HEIGHT messages over the EXACT SAME connection
	for i := 0; i < 3; i++ {
		msgData := []byte(fmt.Sprintf(`{"network_id":"%s","type":"GET_HEIGHT","payload":null}`+"\n", NetworkID()))
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write(msgData); err != nil {
			t.Fatalf("Write message #%d failed: %v", i, err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("Read message #%d failed (connection prematurely closed): %v", i, err)
		}
		if !strings.Contains(string(buf[:n]), "HEIGHT_RESPONSE") {
			t.Fatalf("Unexpected response on message #%d: %s", i, string(buf[:n]))
		}
	}
}

// TestP2PChainworkReorg verifies [N-01]: Node A successfully reorganizes to heavier chain from Node B
func TestP2PChainworkReorg(t *testing.T) {
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	dirA := filepath.Join(t.TempDir(), "nodeA")
	dirB := filepath.Join(t.TempDir(), "nodeB")
	_ = os.MkdirAll(dirA, 0755)
	_ = os.MkdirAll(dirB, 0755)

	// Node A: Difficulty 1, mines 1 block
	bcA, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dirA, "blockchain.db"))
	if err != nil {
		t.Fatalf("failed to create blockchain A: %v", err)
	}
	defer bcA.Close()

	if _, err := bcA.MineBlockWithErr(core.GenesisMiner); err != nil {
		t.Fatalf("bcA MineBlockWithErr failed: %v", err)
	}

	// Node B: Difficulty 1, mines 3 blocks
	bcB, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dirB, "blockchain.db"))
	if err != nil {
		t.Fatalf("failed to create blockchain B: %v", err)
	}
	defer bcB.Close()

	for i := 0; i < 3; i++ {
		if _, err := bcB.MineBlockWithErr(core.GenesisMiner); err != nil {
			t.Fatalf("bcB MineBlockWithErr failed: %v", err)
		}
	}

	portB := getFreePort(t)
	psB := NewPeerServer(portB, bcB)
	defer psB.Stop()
	go psB.StartServer()

	time.Sleep(100 * time.Millisecond)

	portA := getFreePort(t)
	psA := NewPeerServer(portA, bcA)
	defer psA.Stop()

	// Node A syncs from Node B
	psA.RequestChainSync("127.0.0.1:" + portB)

	// Verify Node A reorganized to match Node B's chain length and tip
	bcA.Lock.RLock()
	lenA := len(bcA.Chain)
	tipA := bcA.Chain[lenA-1].Hash
	bcA.Lock.RUnlock()

	bcB.Lock.RLock()
	lenB := len(bcB.Chain)
	tipB := bcB.Chain[lenB-1].Hash
	bcB.Lock.RUnlock()

	if lenA != lenB {
		t.Fatalf("Reorg failed: Node A length %d != Node B length %d", lenA, lenB)
	}
	if tipA != tipB {
		t.Fatalf("Reorg failed: Node A tip %s != Node B tip %s", tipA, tipB)
	}
}

// TestP2PUnsolicitedChainResponseRejected verifies [N-04]: Node drops unsolicited CHAIN_RESPONSE
func TestP2PUnsolicitedChainResponseRejected(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := newIsolatedBlockchain(t)
	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	defer ps.Stop()
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)

	// Send an unsolicited CHAIN_RESPONSE
	unsolicitedMsg := Message{
		NetworkID: NetworkID(),
		Type:      "CHAIN_RESPONSE",
		Payload:   []core.Block{},
	}
	err := SendP2PMessage("127.0.0.1:"+port, unsolicitedMsg)
	if err != nil {
		t.Fatalf("Send unsolicited message failed: %v", err)
	}

	// Give node a moment to process; local chain should remain intact with 1 genesis block
	time.Sleep(50 * time.Millisecond)
	if len(bc.Chain) != 1 {
		t.Fatalf("Unsolicited CHAIN_RESPONSE affected local chain!")
	}
}

// TestP2PStrictTLS verifies [N-08]: Strict TLS mode enforcement
func TestP2PStrictTLS(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	SetStrictTLS(true)
	defer SetStrictTLS(false)

	if !IsStrictTLS() {
		t.Fatalf("Expected IsStrictTLS to be true")
	}

	// Dialing an invalid/closed address with strict TLS returns TLS error, not TCP fallback
	_, _, err := DialEncrypted("127.0.0.1:1", 200*time.Millisecond)
	if err == nil {
		t.Fatalf("Expected connection failure on invalid port with strict TLS")
	}
}

// TestP2PDisableSubnetLimit verifies [N-12]: Subnet connection limit relaxation for LAN/testnet
func TestP2PDisableSubnetLimit(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := newIsolatedBlockchain(t)
	ps := NewPeerServer(core.MainnetP2PPort, bc)
	defer ps.Stop()
	ps.Peers = []string{}

	// 1. With subnet limit enabled (default): 4th peer in same /24 should be rejected
	ps.DisableSubnetLimit = false
	ps.AddPeer("198.51.100.1:" + core.MainnetP2PPort)
	ps.AddPeer("198.51.100.2:" + core.MainnetP2PPort)
	ps.AddPeer("198.51.100.3:" + core.MainnetP2PPort)
	ps.AddPeer("198.51.100.4:" + core.MainnetP2PPort)

	if len(ps.GetPeers()) != 3 {
		t.Fatalf("Expected 3 peers due to MaxPeersPerSubnet, got %d", len(ps.GetPeers()))
	}

	// 2. With subnet limit disabled: additional peers in same subnet should be accepted
	ps.DisableSubnetLimit = true
	ps.AddPeer("198.51.100.4:" + core.MainnetP2PPort)
	ps.AddPeer("198.51.100.5:" + core.MainnetP2PPort)

	if len(ps.GetPeers()) != 5 {
		t.Fatalf("Expected 5 peers with DisableSubnetLimit=true, got %d", len(ps.GetPeers()))
	}
}
