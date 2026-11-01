//go:build testhooks

package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestTestnet_LocalGenesisMiningAndP2PHandshake verifies the complete local Testnet lifecycle [v1.0.6]:
// 1. Clean Testnet database initialization from Genesis (Index 0, TestnetGenesisHash).
// 2. Reading genesis and verifying chain state.
// 3. Mining block #1 on Testnet (SHA-256d PoW) and appending to chain.
// 4. Verifying active peer counting through handshake-verified P2P sessions only.
// 5. Querying actual HTTP endpoints (/stats, /status, /peers?filter=active).
func TestTestnet_LocalGenesisMiningAndP2PHandshake(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	tempDir := t.TempDir()
	dbDirA := filepath.Join(tempDir, "testnet_nodeA")
	dbDirB := filepath.Join(tempDir, "testnet_nodeB")
	dbPathA := filepath.Join(dbDirA, "blockchain.db")
	dbPathB := filepath.Join(dbDirB, "blockchain.db")

	// 1. Initialize Node A blockchain from Genesis
	bcA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatalf("Failed to initialize Testnet Node A: %v", err)
	}
	defer bcA.Close()

	// 2. Verify Genesis block parameters
	genA := bcA.GetLatestBlock()
	if genA.Index != 0 {
		t.Fatalf("Expected Genesis index 0, got %d", genA.Index)
	}
	if genA.Hash != core.TestnetGenesisHash {
		t.Fatalf("Expected TestnetGenesisHash %s, got %s", core.TestnetGenesisHash, genA.Hash)
	}
	if genA.Nonce != core.TestnetGenesisNonce {
		t.Fatalf("Expected TestnetGenesisNonce %d, got %d", core.TestnetGenesisNonce, genA.Nonce)
	}
	if genA.Target != core.TestnetGenesisTargetHex {
		t.Fatalf("Expected TestnetGenesisTargetHex %s, got %s", core.TestnetGenesisTargetHex, genA.Target)
	}

	// Verify initial chain state
	if bcA.GetBalance(core.GenesisMiner) != core.InitialReward {
		t.Fatalf("Expected GenesisMiner balance %d, got %d", core.InitialReward, bcA.GetBalance(core.GenesisMiner))
	}

	// 3. Mine Block #1 on Testnet using valid post-quantum ML-DSA-65 miner address
	privMiner, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("Miner keygen failed: %v", err)
	}
	minerAddr, err := core.PubToAddress(pqsig.SigAlgMLDSA65, privMiner.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	b1, err := bcA.MineBlockWithErr(minerAddr)
	if err != nil {
		t.Fatalf("MineBlockWithErr failed on Testnet: %v", err)
	}
	if b1.Index != 1 {
		t.Fatalf("Expected height 1, got %d", b1.Index)
	}
	if b1.PoWAlgorithm() != "sha256d" {
		t.Fatalf("Expected PoW algorithm sha256d for Testnet block 1, got %s", b1.PoWAlgorithm())
	}
	if err := bcA.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// 4. Initialize Node B and validate Block #1 via consensus
	bcB, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatalf("Failed to initialize Testnet Node B: %v", err)
	}
	defer bcB.Close()

	if !bcB.ValidateAndAddBlock(b1) {
		t.Fatalf("Node B consensus rejected Block #1 mined by Node A")
	}
	if bcB.GetLatestBlock().Hash != b1.Hash {
		t.Fatalf("Node B tip %s does not match Block 1 %s", bcB.GetLatestBlock().Hash, b1.Hash)
	}

	// 5. Start P2P servers on loopback ports
	portA := getFreePort(t)
	portB := getFreePort(t)

	psA := NewPeerServer(portA, bcA, dbDirA, "ARMX-TESTNET-1")
	psA.AllowPrivatePeers = true
	psA.DisableSubnetLimit = true
	psA.RequireTLS = true
	psA.P2PTOFU = true
	go psA.StartServer()
	defer psA.Stop()

	psB := NewPeerServer(portB, bcB, dbDirB, "ARMX-TESTNET-1")
	psB.AllowPrivatePeers = true
	psB.DisableSubnetLimit = true
	psB.RequireTLS = true
	psB.P2PTOFU = true
	go psB.StartServer()
	defer psB.Stop()

	time.Sleep(50 * time.Millisecond)

	// Setup RPC handler on Node A
	srvCfg := &ServerConfig{
		NetworkID: "ARMX-TESTNET-1",
		RPCBind:   "127.0.0.1",
		RPCPort:   getFreePort(t),
	}
	limiter := newIPRateLimiter()
	defer limiter.Close()
	rpcA := NewRPCHandler(bcA, psA, limiter, srvCfg)

	// 6. Verify endpoints report 0 active peers BEFORE connection
	reqStats := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recStats := httptest.NewRecorder()
	rpcA.ServeHTTP(recStats, reqStats)
	if recStats.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /stats, got %d", recStats.Code)
	}

	var statsBefore map[string]interface{}
	if err := json.Unmarshal(recStats.Body.Bytes(), &statsBefore); err != nil {
		t.Fatalf("Failed to parse /stats: %v", err)
	}
	if int(statsBefore["peers_active"].(float64)) != 0 {
		t.Fatalf("Expected peers_active == 0 before connection, got %v", statsBefore["peers_active"])
	}

	reqPeersActive := httptest.NewRequest(http.MethodGet, "/peers?filter=active", nil)
	recPeersActive := httptest.NewRecorder()
	rpcA.ServeHTTP(recPeersActive, reqPeersActive)
	var activePeersBefore []string
	if err := json.Unmarshal(recPeersActive.Body.Bytes(), &activePeersBefore); err != nil {
		t.Fatalf("Failed to parse /peers?filter=active: %v", err)
	}
	if len(activePeersBefore) != 0 {
		t.Fatalf("Expected 0 active peers before connection, got %d", len(activePeersBefore))
	}

	// 7. Establish live P2P handshake from Node B to Node A
	response, err := psB.QueryPeer("127.0.0.1:"+portA, Message{
		NetworkID: psB.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("P2P application handshake from Node B to Node A failed: %v", err)
	}
	if response == nil || response.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Expected HEIGHT_RESPONSE handshake, got %#v", response)
	}
	time.Sleep(100 * time.Millisecond)

	// 8. Verify Node A reports exactly 1 active peer on /stats and /status
	recStatsAfter := httptest.NewRecorder()
	rpcA.ServeHTTP(recStatsAfter, reqStats)
	var statsAfter map[string]interface{}
	if err := json.Unmarshal(recStatsAfter.Body.Bytes(), &statsAfter); err != nil {
		t.Fatalf("Failed to parse /stats after connect: %v", err)
	}
	if int(statsAfter["peers_active"].(float64)) < 1 {
		t.Fatalf("Expected peers_active >= 1 after connect, got %v", statsAfter["peers_active"])
	}

	// Verify /status endpoint
	reqStatus := httptest.NewRequest(http.MethodGet, "/status", nil)
	recStatus := httptest.NewRecorder()
	rpcA.ServeHTTP(recStatus, reqStatus)
	var statusAfter map[string]interface{}
	if err := json.Unmarshal(recStatus.Body.Bytes(), &statusAfter); err != nil {
		t.Fatalf("Failed to parse /status: %v", err)
	}
	if int(statusAfter["peers_active"].(float64)) < 1 {
		t.Fatalf("Expected /status peers_active >= 1, got %v", statusAfter["peers_active"])
	}

	// 9. Close P2P connection and verify peers_active returns to 0
	psB.outboundMu.Lock()
	session := psB.outboundSessions["127.0.0.1:"+portA]
	psB.outboundMu.Unlock()
	psB.dropOutboundSession("127.0.0.1:"+portA, session)

	var remainingActive int
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		remainingActive = psA.GetPeersActive()
		if remainingActive == 0 {
			break
		}
	}
	if remainingActive != 0 {
		t.Fatalf("Expected peers_active == 0 after disconnect, got %d", remainingActive)
	}

	// Re-verify /stats after disconnect
	recStatsFinal := httptest.NewRecorder()
	rpcA.ServeHTTP(recStatsFinal, reqStats)
	var statsFinal map[string]interface{}
	if err := json.Unmarshal(recStatsFinal.Body.Bytes(), &statsFinal); err != nil {
		t.Fatalf("Failed to parse /stats final: %v", err)
	}
	if int(statsFinal["peers_active"].(float64)) != 0 {
		t.Fatalf("Expected peers_active == 0 after disconnect, got %v", statsFinal["peers_active"])
	}
}
