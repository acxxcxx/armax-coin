//go:build testhooks

package node

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func TestP2PBlockExchangeAndSyncOverLoopback(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	dir := t.TempDir()
	dbPathA := filepath.Join(dir, "node-a", "blockchain.db")
	dbPathB := filepath.Join(dir, "node-b", "blockchain.db")
	bcA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatal(err)
	}
	defer bcA.Close()
	bcB, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatal(err)
	}

	portA := getFreePort(t)
	portB := getFreePort(t)
	addrA := net.JoinHostPort("127.0.0.1", portA)
	addrB := net.JoinHostPort("127.0.0.1", portB)
	psA := NewPeerServer(portA, bcA, filepath.Join(dir, "node-a"), "ARMX-TESTNET-1")
	psB := NewPeerServer(portB, bcB, filepath.Join(dir, "node-b"), "ARMX-TESTNET-1")
	for _, peer := range []*PeerServer{psA, psB} {
		peer.RequireTLS = true
		peer.P2PTOFU = true
		peer.AllowPrivatePeers = true
		peer.DisableSubnetLimit = true
	}
	go psA.StartServer()
	go psB.StartServer()
	defer psA.Stop()
	defer psB.Stop()
	waitForPeerServer(t, psA)
	waitForPeerServer(t, psB)

	// A valid TLS connection with an alien network ID must not become an active peer.
	conn, release, err := psA.DialPeer(addrB, 2*time.Second)
	if err != nil {
		t.Fatalf("TLS transport setup failed: %v", err)
	}
	if err := json.NewEncoder(conn).Encode(Message{
		NetworkID: "ARMX-WRONG-NETWORK",
		Type:      "GET_HEIGHT",
	}); err != nil {
		_ = conn.Close()
		release()
		t.Fatal(err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		psB.banLock.RLock()
		score := psB.banScores["127.0.0.1"]
		psB.banLock.RUnlock()
		return score >= 10
	}, "Node B did not process the invalid network ID")
	if psA.GetPeersActive() != 0 || psB.GetPeersActive() != 0 {
		_ = conn.Close()
		release()
		t.Fatal("invalid-network connection counted as active")
	}
	_ = conn.Close()
	release()

	response, err := psA.QueryPeer(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 3*time.Second)
	if err != nil || response == nil || response.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("application handshake failed: response=%#v error=%v", response, err)
	}
	waitForActivePeerCounts(t, psA, psB, 1)

	block1, err := bcA.MineBlockWithErr(core.GenesisMiner)
	if err != nil {
		t.Fatalf("Node A failed to mine block 1: %v", err)
	}
	if err := psA.SendMessage(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   block1,
	}); err != nil {
		t.Fatalf("Node A failed to send block 1 over P2P: %v", err)
	}
	waitForTipHash(t, bcB, block1.Hash)
	if _, err := bcB.GetBlockByHash(block1.Hash); err != nil {
		t.Fatalf("Node B did not store block 1 received over P2P: %v", err)
	}

	block2, err := bcA.MineBlockWithErr(core.GenesisMiner)
	if err != nil {
		t.Fatalf("Node A failed to mine block 2: %v", err)
	}
	invalidBlock := block2
	invalidBlock.Hash = strings.Repeat("0", 64)
	if invalidBlock.Hash == block2.Hash {
		invalidBlock.Hash = strings.Repeat("f", 64)
	}
	invalidDigest, err := invalidBlock.CalculateHashWithError()
	if err != nil {
		t.Fatalf("failed to calculate invalid block's header digest: %v", err)
	}
	if err := psA.SendMessage(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   invalidBlock,
	}); err != nil {
		t.Fatalf("failed to send invalid block over P2P: %v", err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		return psB.isBlockNegativeCached(invalidDigest)
	}, "Node B did not reject the invalid network block")
	if got := bcB.GetLatestBlock(); got.Index != 1 || got.Hash != block1.Hash {
		t.Fatalf("invalid block changed Node B's chain: height=%d hash=%s", got.Index, got.Hash)
	}
	if _, err := bcB.GetBlockByHash(block2.Hash); err == nil {
		t.Fatal("invalid block was stored before synchronization")
	}

	psA.outboundMu.Lock()
	session := psA.outboundSessions[addrB]
	psA.outboundMu.Unlock()
	psA.dropOutboundSession(addrB, session)
	waitForActivePeerCounts(t, psA, psB, 0)

	response, err = psA.QueryPeer(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 3*time.Second)
	if err != nil || response == nil || response.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("reconnect handshake failed: response=%#v error=%v", response, err)
	}
	waitForActivePeerCounts(t, psA, psB, 1)
	psA.outboundMu.Lock()
	outboundCount := len(psA.outboundSessions)
	session = psA.outboundSessions[addrB]
	psA.outboundMu.Unlock()
	psB.inboundSessionsMu.RLock()
	inboundCount := len(psB.activeInboundConns)
	psB.inboundSessionsMu.RUnlock()
	if outboundCount != 1 || inboundCount != 1 {
		t.Fatalf("reconnect left duplicate sessions: outbound=%d inbound=%d", outboundCount, inboundCount)
	}
	psA.dropOutboundSession(addrB, session)
	waitForActivePeerCounts(t, psA, psB, 0)

	// Node B catches up through the real GET_HEIGHT / GET_FORKPOINT / GET_BLOCK_RANGE
	// protocol instead of invoking block validation directly.
	psB.RequestChainSync(addrA)
	waitForTipHash(t, bcB, block2.Hash)
	if _, err := bcB.GetBlockByHash(block2.Hash); err != nil {
		t.Fatalf("Node B did not persist block 2 fetched over P2P sync: %v", err)
	}
	waitForActivePeerCounts(t, psA, psB, 1)
	if err := bcB.IsChainValid(); !err {
		t.Fatal("Node B chain failed validation after P2P block synchronization")
	}

	t.Logf("P2P exchange verified: block 1 height=%d hash=%s", block1.Index, block1.Hash)
	t.Logf("P2P synchronization verified: block 2 height=%d hash=%s", block2.Index, block2.Hash)

	psA.Stop()
	psB.Stop()
	if err := bcB.Close(); err != nil {
		t.Fatalf("failed to close Node B database before persistence check: %v", err)
	}
	reopened, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatalf("failed to reopen Node B database after P2P sync: %v", err)
	}
	defer reopened.Close()
	if tip := reopened.GetLatestBlock(); tip.Index != 2 || tip.Hash != block2.Hash {
		t.Fatalf("reopened Node B database has wrong tip: height=%d hash=%s", tip.Index, tip.Hash)
	}
}

func waitForActivePeerCounts(t *testing.T, a, b *PeerServer, want int) {
	t.Helper()
	waitForCondition(t, 3*time.Second, func() bool {
		return a.GetPeersActive() == want && b.GetPeersActive() == want
	}, "active peer counts did not converge")
}

func waitForTipHash(t *testing.T, bc *core.Blockchain, want string) {
	t.Helper()
	waitForCondition(t, 5*time.Second, func() bool {
		tip := bc.GetLatestBlock()
		return tip.Hash == want
	}, "blockchain tip did not converge to expected hash")
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
