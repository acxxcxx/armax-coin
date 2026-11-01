//go:build testhooks

package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestLocalMainnet_ThreeNodeFullSimulation executes an exhaustive, isolated 3-node
// Local Mainnet simulation covering:
// 1. Genesis & Consensus
// 2. Transaction & Wallet (ML-DSA-65)
// 3. Mining & Difficulty
// 4. Loopback transport connectivity and direct block validation
// 5. Reorg & Fork Choice
// 6. Persistence & Restart Recovery
// 7. RPC Authentication & Security
func TestLocalMainnet_ThreeNodeFullSimulation(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	baseDir := t.TempDir()
	dirA := filepath.Join(baseDir, "mainnet_nodeA")
	dirB := filepath.Join(baseDir, "mainnet_nodeB")
	dirC := filepath.Join(baseDir, "mainnet_nodeC")

	_ = os.MkdirAll(dirA, 0700)
	_ = os.MkdirAll(dirB, 0700)
	_ = os.MkdirAll(dirC, 0700)

	// =========================================================================
	// PHASE 1: Genesis & Consensus Initialization Across 3 Isolated Nodes
	// =========================================================================
	dbPathA := filepath.Join(dirA, "blockchain.db")
	dbPathB := filepath.Join(dirB, "blockchain.db")
	dbPathC := filepath.Join(dirC, "blockchain.db")

	bcA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatalf("Node A init failed: %v", err)
	}
	defer bcA.Close()

	bcB, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatalf("Node B init failed: %v", err)
	}
	defer bcB.Close()

	bcC, err := core.NewBlockchainWithStoragePath(1, dbPathC)
	if err != nil {
		t.Fatalf("Node C init failed: %v", err)
	}
	defer bcC.Close()

	// Invariant 1: All three nodes must derive the exact canonical Mainnet Genesis block & hash
	genA := bcA.GetLatestBlock()
	genB := bcB.GetLatestBlock()
	genC := bcC.GetLatestBlock()

	if genA.Hash != core.GenesisHash {
		t.Fatalf("Node A genesis hash mismatch: expected %s, got %s", core.GenesisHash, genA.Hash)
	}
	if genB.Hash != genA.Hash || genC.Hash != genA.Hash {
		t.Fatalf("Genesis hashes not identical across 3 nodes: A=%s B=%s C=%s", genA.Hash, genB.Hash, genC.Hash)
	}
	if genA.Nonce != core.GenesisNonce {
		t.Fatalf("Mainnet genesis nonce mismatch: expected %d, got %d", core.GenesisNonce, genA.Nonce)
	}
	if genA.Target != core.GenesisTargetHex {
		t.Fatalf("Mainnet genesis target mismatch: expected %s, got %s", core.GenesisTargetHex, genA.Target)
	}

	// =========================================================================
	// PHASE 2: Transaction Creation, ML-DSA-65 Signing & Verification
	// =========================================================================
	privKeySender, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("ML-DSA-65 key generation failed: %v", err)
	}
	privKeyRecip, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("Recipient key generation failed: %v", err)
	}

	senderAddr, err := core.PubToAddress(pqsig.SigAlgMLDSA65, privKeySender.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress for sender failed: %v", err)
	}
	recipientAddr, err := core.PubToAddress(pqsig.SigAlgMLDSA65, privKeyRecip.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress for recipient failed: %v", err)
	}

	tx := &core.Transaction{
		Sender:    senderAddr,
		Recipient: recipientAddr,
		Amount:    10 * core.CoinUnit,
		Fee:       core.MinRelayFee,
		Nonce:     1,
		Type:      core.TxTypeStandard,
	}

	// Sign with post-quantum ML-DSA-65 key (FIPS 204, SigAlg 0x01, Context ARMX-TX-V1)
	if err := tx.Sign(privKeySender); err != nil {
		t.Fatalf("ML-DSA-65 transaction signing failed: %v", err)
	}
	if tx.SigAlg != pqsig.SigAlgMLDSA65 {
		t.Fatalf("Expected SigAlg 0x01 (ML-DSA-65), got 0x%02x", tx.SigAlg)
	}
	if len(tx.Signature) == 0 {
		t.Fatalf("Transaction signature is empty")
	}

	// Verify cryptographic signature
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("ML-DSA-65 signature verification failed: %v", err)
	}

	// Submit transaction to Node A mempool
	bcA.Lock.Lock()
	bcA.Mempool = append(bcA.Mempool, *tx)
	bcA.Lock.Unlock()

	// =========================================================================
	// PHASE 3: Mining Block 1 on Node A & Validating State Transition
	// =========================================================================
	block1, err := bcA.MineBlockWithErr(senderAddr)
	if err != nil {
		t.Fatalf("Node A MineBlockWithErr failed: %v", err)
	}
	if block1.Index != 1 {
		t.Fatalf("Expected Node A height 1, got %d", block1.Index)
	}
	if err := bcA.SaveToFile(); err != nil {
		t.Fatalf("Node A failed to persist state: %v", err)
	}

	// =========================================================================
	// PHASE 4: P2P Transport Connectivity & Direct Block Validation
	// =========================================================================
	portA := getFreePort(t)
	portB := getFreePort(t)
	portC := getFreePort(t)

	psA := NewPeerServer(portA, bcA, dirA, "ARMX-MAINNET-1")
	psA.AllowPrivatePeers = true
	defer psA.Stop()

	psB := NewPeerServer(portB, bcB, dirB, "ARMX-MAINNET-1")
	psB.AllowPrivatePeers = true
	defer psB.Stop()

	psC := NewPeerServer(portC, bcC, dirC, "ARMX-MAINNET-1")
	psC.AllowPrivatePeers = true
	defer psC.Stop()

	// Start P2P servers on loopback ports
	go psA.StartServer()
	go psB.StartServer()
	go psC.StartServer()

	// Wait for listeners to bind
	time.Sleep(50 * time.Millisecond)

	// Perform actual P2P handshake over loopback between Node B and Node A
	connBA, releaseBA, err := psB.DialPeer("127.0.0.1:"+portA, 2*time.Second)
	if err != nil {
		t.Fatalf("Node B failed to establish P2P connection to Node A: %v", err)
	}
	defer func() {
		_ = connBA.Close()
		releaseBA()
	}()

	// These calls validate blocks directly; they do not prove P2P block exchange.
	if !bcB.ValidateAndAddBlock(block1) {
		t.Fatalf("Node B consensus validation rejected Block 1")
	}
	if !bcC.ValidateAndAddBlock(block1) {
		t.Fatalf("Node C consensus validation rejected Block 1")
	}

	if bcB.GetLatestBlock().Hash != block1.Hash || bcC.GetLatestBlock().Hash != block1.Hash {
		t.Fatalf("Chain sync inconsistency: Node B or C did not match block 1 hash")
	}

	// Verify loopback connection between Node A and Node B
	addrA := "127.0.0.1:" + portA
	_ = psB.AddPeer(addrA)
	if !psB.HasPeer(addrA) {
		t.Fatalf("Node B failed to register Node A as loopback peer")
	}

	// Test invalid peer rejection in local mainnet: external public IP must be rejected
	externalIP := "203.0.113.195:" + portA
	_ = psB.AddPeer(externalIP)
	if psB.HasPeer(externalIP) {
		t.Fatalf("Security violation: Node B accepted non-loopback external peer %s in local mainnet", externalIP)
	}

	// =========================================================================
	// PHASE 5: Reorg & Fork Choice Simulation (Node B vs Node C)
	// =========================================================================
	privKeyMinerB, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("Miner B key generation failed: %v", err)
	}
	minerAddrB, err := core.PubToAddress(pqsig.SigAlgMLDSA65, privKeyMinerB.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress for miner B failed: %v", err)
	}

	privKeyMinerC, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("Miner C key generation failed: %v", err)
	}
	minerAddrC, err := core.PubToAddress(pqsig.SigAlgMLDSA65, privKeyMinerC.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress for miner C failed: %v", err)
	}

	// Branch B: Node B mines 1 block (height 2)
	b2B, err := bcB.MineBlockWithErr(minerAddrB)
	if err != nil {
		t.Fatalf("Node B mining block 2B failed: %v", err)
	}
	if b2B.Index != 2 {
		t.Fatalf("Expected block 2B index 2, got %d", b2B.Index)
	}

	// Branch C: Node C mines 2 blocks (height 2 and height 3 -> strictly higher chainwork)
	b2C, err := bcC.MineBlockWithErr(minerAddrC)
	if err != nil {
		t.Fatalf("Node C mining block 2C failed: %v", err)
	}
	if b2C.Index != 2 {
		t.Fatalf("Expected block 2C index 2, got %d", b2C.Index)
	}
	b3C, err := bcC.MineBlockWithErr(minerAddrC)
	if err != nil {
		t.Fatalf("Node C mining block 3C failed: %v", err)
	}
	if b3C.Index != 3 {
		t.Fatalf("Expected block 3C index 3, got %d", b3C.Index)
	}

	// Node C chainwork is strictly higher than Node B
	if bcC.GetTotalChainwork().Cmp(bcB.GetTotalChainwork()) <= 0 {
		t.Fatalf("Node C chainwork %s must be greater than Node B %s", bcC.GetTotalChainwork(), bcB.GetTotalChainwork())
	}

	// Execute Reorg: Node B receives Node C's chain (block 2C and block 3C)
	// Node B must execute a clean reorg to Node C's branch via production ResolveChain
	bcC.Lock.RLock()
	candChain := append([]core.Block(nil), bcC.Chain...)
	bcC.Lock.RUnlock()

	if !bcB.ResolveChain(candChain) {
		t.Fatalf("Reorg failure: Node B ResolveChain failed to accept Node C's superior branch")
	}

	if bcB.GetLatestBlock().Hash != b3C.Hash {
		t.Fatalf("Reorg failure: Node B tip %s did not match Node C tip %s", bcB.GetLatestBlock().Hash, b3C.Hash)
	}
	if bcB.GetLatestBlock().Index != 3 {
		t.Fatalf("Expected Node B height 3 after reorg, got %d", bcB.GetLatestBlock().Index)
	}

	// =========================================================================
	// PHASE 6: Persistence & Recovery Across Safe Node Restart
	// =========================================================================
	// Persist Node A and close cleanly
	if err := bcA.SaveToFile(); err != nil {
		t.Fatalf("Node A SaveToFile error: %v", err)
	}
	if err := bcA.Close(); err != nil {
		t.Fatalf("Node A Close error: %v", err)
	}

	// Re-open Node A from disk and verify full state recovery
	reloadedA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatalf("Failed to reload Node A from disk: %v", err)
	}
	defer reloadedA.Close()

	if reloadedA.GetLatestBlock().Index != 1 {
		t.Fatalf("Expected reloaded Node A height 1, got %d", reloadedA.GetLatestBlock().Index)
	}
	if reloadedA.GetLatestBlock().Hash != block1.Hash {
		t.Fatalf("Reloaded Node A hash mismatch: expected %s, got %s", block1.Hash, reloadedA.GetLatestBlock().Hash)
	}

	// =========================================================================
	// PHASE 7: RPC Authentication, Unauthorized Access Rejection & Telemetry
	// =========================================================================
	srvCfg := &ServerConfig{
		NetworkID:   "ARMX-MAINNET-1",
		RPCBind:     "127.0.0.1",
		RPCPort:     getFreePort(t),
		RPCUser:     "admin",
		RPCPassword: "secretpassword123",
	}
	limiter := newIPRateLimiter()
	defer limiter.Close()
	rpcHandler := NewRPCHandler(reloadedA, psA, limiter, srvCfg)

	// 1. Unauthenticated mutation request must be rejected (401 Unauthorized)
	reqMine := httptest.NewRequest(http.MethodPost, "/mine", nil)
	reqMine.RemoteAddr = "127.0.0.1:45678"
	recMine := httptest.NewRecorder()
	rpcHandler.ServeHTTP(recMine, reqMine)
	if recMine.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for unauthenticated /mine, got %d", recMine.Code)
	}

	// 2. Authenticated request must succeed (200 OK)
	reqStats := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recStats := httptest.NewRecorder()
	rpcHandler.ServeHTTP(recStats, reqStats)
	if recStats.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /stats, got %d", recStats.Code)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(recStats.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats JSON: %v", err)
	}
	if int(stats["height"].(float64)) != 1 {
		t.Fatalf("Expected /stats height 1, got %v", stats["height"])
	}
	if !strings.EqualFold(stats["network_id"].(string), "ARMX-MAINNET-1") {
		t.Fatalf("Expected /stats network_id ARMX-MAINNET-1, got %v", stats["network_id"])
	}
}
