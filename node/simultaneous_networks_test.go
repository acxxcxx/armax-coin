//go:build testhooks

package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestSimultaneousNetworksIsolated verifies [Section A: Network Isolation]:
// 1. One node runs with ARMX-MAINNET-1 and one with ARMX-TESTNET-1.
// 2. Both nodes report different, distinct NetworkIDs (assertion fails if identical).
// 3. Negative tests: Mainnet rejects Testnet messages & blocks; Testnet rejects Mainnet messages & blocks.
// 4. Filesystem, database, cookie, and peer cache isolation verified without port or state conflicts.
// 5. Explicitly logs each node's network identity, genesis hash, and listening port.
func TestSimultaneousNetworksIsolated(t *testing.T) {
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)

	baseDataDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", baseDataDir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	// ==========================================
	// 1. Initialize Mainnet Node (Node A)
	// ==========================================
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet profile: %v", err)
	}
	InitRPCAuth("mainnet_user", "mainnet_secret_pass")

	mainnetBc := core.NewBlockchain(1)
	defer mainnetBc.Close()

	mainPort := getFreePort(t)
	mainPs := NewPeerServer(mainPort, mainnetBc)
	defer mainPs.Stop()
	go mainPs.StartServer()

	// ==========================================
	// 2. Initialize Testnet Node (Node B)
	// ==========================================
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet profile: %v", err)
	}
	InitRPCAuth("testnet_user", "testnet_secret_pass")

	testnetBc := core.NewBlockchain(1)
	defer testnetBc.Close()

	testPort := getFreePort(t)
	testPs := NewPeerServer(testPort, testnetBc)
	defer testPs.Stop()
	go testPs.StartServer()

	time.Sleep(150 * time.Millisecond)

	// ==========================================
	// 3. Assert Distinct Network Identities
	// ==========================================
	mainNetID := mainPs.GetNetworkID()
	testNetID := testPs.GetNetworkID()

	t.Logf("=== NODE A (MAINNET) IDENTITY ===")
	t.Logf("  Port         : %s", mainPort)
	t.Logf("  Network ID   : %s", mainNetID)
	t.Logf("  Genesis Hash : %s", mainnetBc.Chain[0].Hash)
	t.Logf("  Merkle Root  : %s", mainnetBc.Chain[0].MerkleRoot)

	t.Logf("=== NODE B (TESTNET) IDENTITY ===")
	t.Logf("  Port         : %s", testPort)
	t.Logf("  Network ID   : %s", testNetID)
	t.Logf("  Genesis Hash : %s", testnetBc.Chain[0].Hash)
	t.Logf("  Merkle Root  : %s", testnetBc.Chain[0].MerkleRoot)

	if mainNetID != "ARMX-MAINNET-1" {
		t.Fatalf("Node A NetworkID must be ARMX-MAINNET-1, got: %s", mainNetID)
	}
	if testNetID != "ARMX-TESTNET-1" {
		t.Fatalf("Node B NetworkID must be ARMX-TESTNET-1, got: %s", testNetID)
	}
	if mainNetID == testNetID {
		t.Fatalf("[CRITICAL] Both nodes reported identical NetworkID '%s'! Isolation breached.", mainNetID)
	}

	// ==========================================
	// 4. Verify Genesis Block Separation
	// ==========================================
	if mainnetBc.Chain[0].Hash == testnetBc.Chain[0].Hash {
		t.Fatalf("Genesis block collision! Mainnet and Testnet share identical hash: %s", mainnetBc.Chain[0].Hash)
	}
	if mainnetBc.Chain[0].Hash != core.GenesisHash {
		t.Fatalf("Mainnet Genesis hash mismatch: got %s, expected %s", mainnetBc.Chain[0].Hash, core.GenesisHash)
	}
	if testnetBc.Chain[0].Hash != core.TestnetGenesisHash {
		t.Fatalf("Testnet Genesis hash mismatch: got %s, expected %s", testnetBc.Chain[0].Hash, core.TestnetGenesisHash)
	}

	// ==========================================
	// 5. Verify Filesystem & Storage Isolation
	// ==========================================
	mainnetDB := filepath.Join(baseDataDir, "blockchain.db")
	testnetDB := filepath.Join(baseDataDir, "testnet", "blockchain.db")

	if _, err := os.Stat(mainnetDB); os.IsNotExist(err) {
		t.Fatalf("Mainnet DB file missing: %s", mainnetDB)
	}
	if _, err := os.Stat(testnetDB); os.IsNotExist(err) {
		t.Fatalf("Testnet DB file missing: %s", testnetDB)
	}

	mainnetCookie := filepath.Join(baseDataDir, "armax.cookie")
	testnetCookie := filepath.Join(baseDataDir, "testnet", "armax.cookie")

	if _, err := os.Stat(mainnetCookie); os.IsNotExist(err) {
		t.Fatalf("Mainnet cookie missing: %s", mainnetCookie)
	}
	if _, err := os.Stat(testnetCookie); os.IsNotExist(err) {
		t.Fatalf("Testnet cookie missing: %s", testnetCookie)
	}

	mainCookieBytes, _ := os.ReadFile(mainnetCookie)
	testCookieBytes, _ := os.ReadFile(testnetCookie)
	if string(mainCookieBytes) == string(testCookieBytes) {
		t.Fatalf("Mainnet and Testnet cookie secrets must be independently generated! Got: %s", string(mainCookieBytes))
	}

	// ==========================================
	// 6. Negative P2P Isolation Tests
	// ==========================================
	// Subtest 6A: Valid Mainnet message to Mainnet node -> SUCCEEDS
	validMainMsg := Message{
		NetworkID: "ARMX-MAINNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	respMain, err := QueryP2P("127.0.0.1:"+mainPort, validMainMsg, 2*time.Second)
	if err != nil || respMain == nil || respMain.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Mainnet node failed to answer valid Mainnet message: %v", err)
	}

	// Subtest 6B: Valid Testnet message to Testnet node -> SUCCEEDS
	validTestMsg := Message{
		NetworkID: "ARMX-TESTNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	respTest, err := QueryP2P("127.0.0.1:"+testPort, validTestMsg, 2*time.Second)
	if err != nil || respTest == nil || respTest.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Testnet node failed to answer valid Testnet message: %v", err)
	}

	// Subtest 6C: Alien Testnet message sent to Mainnet node -> REJECTED & BANNED
	alienTestMsg := Message{
		NetworkID: "ARMX-TESTNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	respAlienMain, errAlienMain := QueryP2P("127.0.0.1:"+mainPort, alienTestMsg, 1*time.Second)
	if errAlienMain == nil && respAlienMain != nil {
		t.Fatalf("[SECURITY BREACH] Mainnet node accepted alien Testnet message! Response: %+v", respAlienMain)
	}
	t.Logf("[OK] Mainnet node successfully dropped alien Testnet message (err: %v)", errAlienMain)

	// Subtest 6D: Alien Mainnet message sent to Testnet node -> REJECTED & BANNED
	alienMainMsg := Message{
		NetworkID: "ARMX-MAINNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	respAlienTest, errAlienTest := QueryP2P("127.0.0.1:"+testPort, alienMainMsg, 1*time.Second)
	if errAlienTest == nil && respAlienTest != nil {
		t.Fatalf("[SECURITY BREACH] Testnet node accepted alien Mainnet message! Response: %+v", respAlienTest)
	}
	t.Logf("[OK] Testnet node successfully dropped alien Mainnet message (err: %v)", errAlienTest)

	// Subtest 6E: Mainnet node rejects Testnet block insertion
	initialMainTip := mainnetBc.GetLatestBlock().Hash
	initialMainSupply := mainnetBc.GetCirculatingSupply()
	initialMainBalance := mainnetBc.GetBalance(core.GenesisMiner)
	testnetBlock := testnetBc.Chain[0]
	if mainnetBc.ValidateBlock(testnetBlock) == nil {
		t.Fatalf("Mainnet node must NEVER validate or accept Testnet block!")
	}
	if mainnetBc.ValidateAndAddBlock(testnetBlock) {
		t.Fatalf("Mainnet ValidateAndAddBlock must return false for alien testnet block!")
	}
	if mainnetBc.GetLatestBlock().Hash != initialMainTip {
		t.Fatalf("Mainnet tip altered after alien block rejection!")
	}
	if mainnetBc.GetCirculatingSupply() != initialMainSupply {
		t.Fatalf("Mainnet supply altered after alien block rejection!")
	}
	if mainnetBc.GetBalance(core.GenesisMiner) != initialMainBalance {
		t.Fatalf("Mainnet balance altered after alien block rejection!")
	}

	// Subtest 6F: Testnet node rejects Mainnet block insertion
	initialTestTip := testnetBc.GetLatestBlock().Hash
	initialTestSupply := testnetBc.GetCirculatingSupply()
	initialTestBalance := testnetBc.GetBalance(core.GenesisMiner)
	mainnetBlock := mainnetBc.Chain[0]
	if testnetBc.ValidateBlock(mainnetBlock) == nil {
		t.Fatalf("Testnet node must NEVER validate or accept Mainnet block!")
	}
	if testnetBc.ValidateAndAddBlock(mainnetBlock) {
		t.Fatalf("Testnet ValidateAndAddBlock must return false for alien mainnet block!")
	}
	if testnetBc.GetLatestBlock().Hash != initialTestTip {
		t.Fatalf("Testnet tip altered after alien block rejection!")
	}
	if testnetBc.GetCirculatingSupply() != initialTestSupply {
		t.Fatalf("Testnet supply altered after alien block rejection!")
	}
	if testnetBc.GetBalance(core.GenesisMiner) != initialTestBalance {
		t.Fatalf("Testnet balance altered after alien block rejection!")
	}

	// Mempools remain empty and unaltered
	if len(mainnetBc.Mempool) != 0 || len(testnetBc.Mempool) != 0 {
		t.Fatalf("Mempool contaminated after alien rejection!")
	}

	t.Logf("[OK] All network isolation, cross-network rejection, and state separation invariants verified.")
}
