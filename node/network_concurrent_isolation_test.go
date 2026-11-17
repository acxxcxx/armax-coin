//go:build testhooks

package node

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestL1_3_ConcurrentMainnetAndTestnetNodes verifies [L1-3]:
// Runs one mainnet node and one testnet node simultaneously with isolated data directories
// and distinct ports, ensuring no DB collisions or port conflicts.
func TestL1_3_ConcurrentMainnetAndTestnetNodes(t *testing.T) {
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	// Prepare distinct isolated data directories
	baseDataDir := t.TempDir()
	mainnetDir := filepath.Join(baseDataDir, "mainnet_data")
	testnetDir := filepath.Join(baseDataDir, "testnet_data")
	_ = os.MkdirAll(mainnetDir, 0755)
	_ = os.MkdirAll(testnetDir, 0755)

	// Step 1: Initialize Mainnet Node
	_ = core.SetNetwork("mainnet")
	defer func() {
		core.SetDBPath("")
		_ = core.SetNetwork("mainnet")
	}()
	mainnetDBPath := filepath.Join(mainnetDir, core.DBFileName)
	core.SetDBPath(mainnetDBPath)
	bcMainnet := core.NewBlockchain(1)
	defer bcMainnet.Close()

	portMainnetP2P := getFreePort(t)
	portMainnetRPC := getFreePort(t)

	cfgMainnet := &Config{
		NetworkID: core.MainnetProfile.NetworkID,
		RPCBind:   "127.0.0.1",
		RPCPort:   portMainnetRPC,
		P2PPort:   portMainnetP2P,
		MaxPeers:  5,
	}

	peerServerMain, srvCfgMain, err := InitServerFromConfig(bcMainnet, cfgMainnet)
	if err != nil {
		t.Fatalf("InitServerFromConfig mainnet failed: %v", err)
	}
	go peerServerMain.StartServer()
	defer peerServerMain.Stop()

	handlerMain := NewRPCHandler(bcMainnet, peerServerMain, newIPRateLimiter(), srvCfgMain)
	httpServerMain := &http.Server{Addr: "127.0.0.1:" + portMainnetRPC, Handler: handlerMain}
	go func() { _ = httpServerMain.ListenAndServe() }()
	defer httpServerMain.Close()

	// Step 2: Initialize Testnet Node
	_ = core.SetNetwork("testnet")
	core.SetDBPath("")
	t.Setenv("ARMX_DATA_DIR", testnetDir)
	testnetDBPath := core.GetDBPath()

	// Verify testnet DB is isolated into <testnetDir>/testnet/
	expectedTestnetSubdir := filepath.Join(testnetDir, "testnet", core.DBFileName)
	if testnetDBPath != expectedTestnetSubdir {
		t.Fatalf("Expected testnet DB path %s, got %s", expectedTestnetSubdir, testnetDBPath)
	}

	bcTestnet := core.NewBlockchain(1)
	bcTestnet.StoragePath = testnetDBPath
	defer bcTestnet.Close()

	portTestnetP2P := getFreePort(t)
	portTestnetRPC := getFreePort(t)

	cfgTestnet := &Config{
		NetworkID: core.TestnetProfile.NetworkID,
		RPCBind:   "127.0.0.1",
		RPCPort:   portTestnetRPC,
		P2PPort:   portTestnetP2P,
		MaxPeers:  5,
	}

	peerServerTest, srvCfgTest, err := InitServerFromConfig(bcTestnet, cfgTestnet)
	if err != nil {
		t.Fatalf("InitServerFromConfig testnet failed: %v", err)
	}
	go peerServerTest.StartServer()
	defer peerServerTest.Stop()

	handlerTest := NewRPCHandler(bcTestnet, peerServerTest, newIPRateLimiter(), srvCfgTest)
	httpServerTest := &http.Server{Addr: "127.0.0.1:" + portTestnetRPC, Handler: handlerTest}
	go func() { _ = httpServerTest.ListenAndServe() }()
	defer httpServerTest.Close()

	// Wait for both servers to be fully active
	time.Sleep(300 * time.Millisecond)

	// Step 3: Query Mainnet /stats
	respMain, err := http.Get("http://127.0.0.1:" + portMainnetRPC + "/stats")
	if err != nil {
		t.Fatalf("Failed to query mainnet RPC: %v", err)
	}
	defer respMain.Body.Close()

	var statsMain map[string]interface{}
	if err := json.NewDecoder(respMain.Body).Decode(&statsMain); err != nil {
		t.Fatalf("Failed to decode mainnet stats: %v", err)
	}
	if statsMain["network_id"] != core.MainnetProfile.NetworkID {
		t.Fatalf("Expected mainnet network_id %s, got %v", core.MainnetProfile.NetworkID, statsMain["network_id"])
	}

	// Step 4: Query Testnet /stats
	respTest, err := http.Get("http://127.0.0.1:" + portTestnetRPC + "/stats")
	if err != nil {
		t.Fatalf("Failed to query testnet RPC: %v", err)
	}
	defer respTest.Body.Close()

	var statsTest map[string]interface{}
	if err := json.NewDecoder(respTest.Body).Decode(&statsTest); err != nil {
		t.Fatalf("Failed to decode testnet stats: %v", err)
	}
	if statsTest["network_id"] != core.TestnetProfile.NetworkID {
		t.Fatalf("Expected testnet network_id %s, got %v", core.TestnetProfile.NetworkID, statsTest["network_id"])
	}

	// Step 5: Save both and verify DB files co-exist independently
	if err := bcMainnet.SaveToFile(); err != nil {
		t.Fatalf("Mainnet save failed: %v", err)
	}
	if err := bcTestnet.SaveToFile(); err != nil {
		t.Fatalf("Testnet save failed: %v", err)
	}

	if _, err := os.Stat(mainnetDBPath); os.IsNotExist(err) {
		t.Fatalf("Mainnet DB file does not exist: %s", mainnetDBPath)
	}
	if _, err := os.Stat(testnetDBPath); os.IsNotExist(err) {
		t.Fatalf("Testnet DB file does not exist: %s", testnetDBPath)
	}
}
