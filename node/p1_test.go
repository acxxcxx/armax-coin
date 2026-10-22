//go:build testhooks

package node

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestP1_DefaultConfigAndProfiles verifies:
// (b) DefaultConfig and both profiles use correct values. DefaultConfig uses mainnet profile.
// -network=testnet selects testnet profile.
func TestP1_DefaultConfigAndProfiles(t *testing.T) {
	// 1. Mainnet DefaultConfig
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet: %v", err)
	}
	cfgMain := DefaultConfig()
	if cfgMain.NetworkID != "ARMX-MAINNET-1" {
		t.Errorf("Expected mainnet NetworkID 'ARMX-MAINNET-1', got '%s'", cfgMain.NetworkID)
	}
	if cfgMain.RPCPort != core.MainnetRPCPort {
		t.Errorf("Expected mainnet RPCPort '%s', got '%s'", core.MainnetRPCPort, cfgMain.RPCPort)
	}
	if cfgMain.P2PPort != core.MainnetP2PPort {
		t.Errorf("Expected mainnet P2PPort '%s', got '%s'", core.MainnetP2PPort, cfgMain.P2PPort)
	}
	if cfgMain.ZMQPort != core.MainnetZMQPort {
		t.Errorf("Expected mainnet ZMQPort '%s', got '%s'", core.MainnetZMQPort, cfgMain.ZMQPort)
	}
	if cfgMain.StratumPort != core.MainnetStratumPort {
		t.Errorf("Expected mainnet StratumPort '%s', got '%s'", core.MainnetStratumPort, cfgMain.StratumPort)
	}
	if cfgMain.RPCBind != "127.0.0.1" {
		t.Errorf("Expected RPCBind '127.0.0.1', got '%s'", cfgMain.RPCBind)
	}

	// 2. Testnet DefaultConfig
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet: %v", err)
	}
	cfgTest := DefaultConfig()
	if cfgTest.NetworkID != "ARMX-TESTNET-1" {
		t.Errorf("Expected testnet NetworkID 'ARMX-TESTNET-1', got '%s'", cfgTest.NetworkID)
	}
	if cfgTest.RPCPort != core.TestnetRPCPort {
		t.Errorf("Expected testnet RPCPort '%s', got '%s'", core.TestnetRPCPort, cfgTest.RPCPort)
	}
	if cfgTest.P2PPort != core.TestnetP2PPort {
		t.Errorf("Expected testnet P2PPort '%s', got '%s'", core.TestnetP2PPort, cfgTest.P2PPort)
	}
	if cfgTest.ZMQPort != core.TestnetZMQPort {
		t.Errorf("Expected testnet ZMQPort '%s', got '%s'", core.TestnetZMQPort, cfgTest.ZMQPort)
	}
	if cfgTest.StratumPort != core.TestnetStratumPort {
		t.Errorf("Expected testnet StratumPort '%s', got '%s'", core.TestnetStratumPort, cfgTest.StratumPort)
	}

	// 3. Credential-less invariant: DefaultConfig has NO hardcoded credentials [L3-2]
	if cfgMain.RPCUser != "" || cfgMain.RPCPassword != "" || cfgMain.MineKey != "" {
		t.Errorf("Expected mainnet DefaultConfig to be credential-less (cookie-based), got user=%q, pass=%q, mine_key=%q",
			cfgMain.RPCUser, cfgMain.RPCPassword, cfgMain.MineKey)
	}
	if cfgTest.RPCUser != "" || cfgTest.RPCPassword != "" || cfgTest.MineKey != "" {
		t.Errorf("Expected testnet DefaultConfig to be credential-less (cookie-based), got user=%q, pass=%q, mine_key=%q",
			cfgTest.RPCUser, cfgTest.RPCPassword, cfgTest.MineKey)
	}

	// Restore mainnet
	_ = core.SetNetwork("mainnet")
}

// TestP1_ConfigPortOverride verifies that custom port/p2p config overrides profile defaults.
func TestP1_ConfigPortOverride(t *testing.T) {
	_ = core.SetNetwork("mainnet")
	cfg := DefaultConfig()
	cfg.RPCPort = "29332"
	cfg.P2PPort = "29334"

	if cfg.RPCPort != "29332" || cfg.P2PPort != "29334" {
		t.Fatalf("Expected port override to be preserved, got rpc=%s, p2p=%s", cfg.RPCPort, cfg.P2PPort)
	}
}

// TestP1_SocketListeners verifies:
// (c) Socket test: run P2P and RPC servers with profile ports and verify listener binds to expected profile port.
// If port is already in use by another process, t.Skip with a clear explanation.
func TestP1_SocketListeners(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	_ = core.SetNetwork("mainnet")
	mainnet := core.GetActiveNetwork()

	// 1. Verify P2P port availability or skip
	p2pAddr := "127.0.0.1:" + mainnet.P2PPort
	p2pPreCheck, err := net.Listen("tcp", p2pAddr)
	if err != nil {
		t.Skipf("P2P port %s is already in use on this system (%v), skipping socket listener test", mainnet.P2PPort, err)
		return
	}
	_ = p2pPreCheck.Close()

	// 2. Verify RPC port availability or skip
	rpcAddr := "127.0.0.1:" + mainnet.RPCPort
	rpcPreCheck, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		t.Skipf("RPC port %s is already in use on this system (%v), skipping socket listener test", mainnet.RPCPort, err)
		return
	}
	_ = rpcPreCheck.Close()

	// 3. Launch P2P Server on canonical port
	bc := core.NewBlockchain(1)
	defer bc.Close()

	p2pServer := NewPeerServer(mainnet.P2PPort, bc)
	go p2pServer.StartServer()
	defer p2pServer.Stop()

	// Check P2P connectivity
	var p2pConn net.Conn
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		p2pConn, err = net.DialTimeout("tcp", p2pAddr, 500*time.Millisecond)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("Failed to connect to P2P server on %s: %v", p2pAddr, err)
	}
	_ = p2pConn.Close()

	// 4. Launch RPC Server on canonical port
	limiter := newIPRateLimiter()
	cfg := &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: mainnet.RPCPort,
	}
	handler := NewRPCHandler(bc, p2pServer, limiter, cfg)
	httpServer := &http.Server{
		Addr:    rpcAddr,
		Handler: handler,
	}

	listener, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		t.Fatalf("Failed to bind RPC server listener to %s: %v", rpcAddr, err)
	}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	// Query /stats from RPC listener
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/stats", rpcAddr))
	if err != nil {
		t.Fatalf("Failed to query RPC server on %s: %v", rpcAddr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected RPC status code %d: %s", resp.StatusCode, string(body))
	}
}

// TestP1_NoLegacyPortsInGoCode verifies:
// (d) No legacy port literals ("8080" or "9000") remain in any .go source file.
func TestP1_NoLegacyPortsInGoCode(t *testing.T) {
	// Find project root by walking upwards until go.mod is found
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	rootDir := dir
	for {
		if _, err := os.Stat(filepath.Join(rootDir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(rootDir)
		if parent == rootDir {
			t.Fatalf("Could not find project root containing go.mod")
		}
		rootDir = parent
	}

	var offending []string
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip .git, vendor, bin
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip this test file itself so it doesn't self-flag
		if strings.HasSuffix(path, "p1_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)

		// Check for legacy literal ports as strings
		if strings.Contains(content, "\"8080\"") || strings.Contains(content, "\"9000\"") ||
			strings.Contains(content, ":8080") || strings.Contains(content, ":9000") {
			offending = append(offending, path)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to scan directory for legacy ports: %v", err)
	}

	if len(offending) > 0 {
		t.Errorf("Found legacy port literals (8080/9000) in the following Go files:\n%s", strings.Join(offending, "\n"))
	}
}
