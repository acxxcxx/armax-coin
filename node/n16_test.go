//go:build testhooks

package node

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN16_AddressBookSeparationAndLimit verifies [N-16]:
// Address book bounds total stored peers to MaxAddressBookSize (1000), independent of active MaxPeers (e.g. 50).
func TestN16_AddressBookSeparationAndLimit(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(getFreePort(t), bc)
	ps.MaxPeers = 20 // active conn limit is low

	// Add 1050 valid peer addresses across distinct subnets
	for i := 1; i <= 1050; i++ {
		b2 := (i / 250) + 1
		b3 := (i % 250) + 1
		addr := fmt.Sprintf("198.%d.%d.1:%s", b2, b3, core.MainnetP2PPort)
		ps.AddPeer(addr)
	}

	peers := ps.GetPeers()
	if len(peers) > 1000 {
		t.Fatalf("[N-16] Address book exceeded 1000 limit: got %d", len(peers))
	}
	if len(peers) <= ps.MaxPeers {
		t.Fatalf("[N-16] Address book was prematurely constrained by active MaxPeers (%d): got %d", ps.MaxPeers, len(peers))
	}
}

// TestN16_NewToTriedPromotionAndDeadPeerEviction verifies that:
// 1. New peers enter 'new' table.
// 2. Successful handshake moves them to 'tried'.
// 3. Repeated failures (>= 3) evict dead peers from the address book.
func TestN16_NewToTriedPromotionAndDeadPeerEviction(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(getFreePort(t), bc)

	peerAddr := "198.51.100.1:" + core.MainnetP2PPort
	ps.AddPeer(peerAddr)

	// Peer must initially be in newPeers
	if !ps.IsNewPeer(peerAddr) {
		t.Fatalf("[N-16] Expected newly added peer to be in 'new' table")
	}
	if ps.IsTriedPeer(peerAddr) {
		t.Fatalf("[N-16] Peer should not be in 'tried' before handshake")
	}

	// Record success -> promote to tried
	ps.RecordPeerSuccess(peerAddr, 10, NetworkID())
	if !ps.IsTriedPeer(peerAddr) {
		t.Fatalf("[N-16] Expected peer to be promoted to 'tried' after handshake")
	}
	if ps.IsNewPeer(peerAddr) {
		t.Fatalf("[N-16] Promoted peer should no longer be in 'new' table")
	}

	// Record 3 consecutive failures -> dead peer eviction
	ps.RecordPeerFailure(peerAddr)
	ps.RecordPeerFailure(peerAddr)
	ps.RecordPeerFailure(peerAddr)

	if ps.HasPeer(peerAddr) {
		t.Fatalf("[N-16] Dead peer was not evicted from address book after repeated failures")
	}
}

// TestN16_PrivateAndSelfAddressRejection verifies public production mode rejects private/loopback IPs
func TestN16_PrivateAndSelfAddressRejection(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "")
	t.Setenv("ARMX_TESTNET", "")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(core.MainnetP2PPort, bc)

	// Attempt to add private and loopback addresses
	ps.AddPeer("127.0.0.1:" + core.MainnetP2PPort)
	ps.AddPeer("10.0.0.1:" + core.MainnetP2PPort)
	ps.AddPeer("192.168.1.5:" + core.MainnetP2PPort)
	ps.AddPeer("172.16.0.1:" + core.MainnetP2PPort)

	if len(ps.GetPeers()) > 0 {
		t.Fatalf("[N-16] Private/loopback peer addresses were accepted in production mode: %v", ps.GetPeers())
	}
}

// TestN16_PeersListResponseBounded verifies PEERS_LIST responses are clamped to MaxPeersPerResponse
func TestN16_PeersListResponseBounded(t *testing.T) {
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

	// Populate server with 300 peers
	for i := 1; i <= 300; i++ {
		addr := fmt.Sprintf("198.%d.%d.1:%s", (i/250)+1, (i%250)+1, core.MainnetP2PPort)
		ps.AddPeer(addr)
	}

	// Query GET_PEERS
	target := "127.0.0.1:" + port
	resp, err := QueryP2P(target, Message{NetworkID: NetworkID(), Type: "GET_PEERS"}, 2*time.Second)
	if err != nil || resp.Type != "PEERS_LIST" {
		t.Fatalf("Failed to query GET_PEERS: %v", err)
	}

	var returned []string
	b, _ := json.Marshal(resp.Payload)
	_ = json.Unmarshal(b, &returned)

	if len(returned) > 250 {
		t.Fatalf("[N-16] PEERS_LIST response was not bounded: received %d peers (max 250)", len(returned))
	}
}
