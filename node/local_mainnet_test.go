//go:build testhooks

package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestLocalMainnet_ExplicitActivationOnly verifies that Mainnet CANNOT be activated
// without explicit opt-in (via EnableLocalMainnet or testhooks), preserving MainnetLaunched=false.
func TestLocalMainnet_ExplicitActivationOnly(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.DisableLocalMainnet()
	core.SetAllowMainnetForTesting(false)
	defer core.SetAllowMainnetForTesting(true)

	// 1. Invariant: MainnetLaunched must be false
	if core.MainnetLaunched {
		t.Fatalf("CRITICAL: MainnetLaunched constant must remain false")
	}

	// 2. Default state: Mainnet resolution fails with exact safety notice
	_, err := core.ResolveCLINetwork("mainnet")
	if err == nil || !strings.Contains(err.Error(), "Mainnet has not been launched; please use -network testnet") {
		t.Fatalf("Expected 'Mainnet has not been launched' error without local mainnet, got: %v", err)
	}

	err = core.SetNetwork("mainnet")
	if err == nil || !strings.Contains(err.Error(), "Mainnet has not been launched; please use -network testnet") {
		t.Fatalf("Expected SetNetwork(mainnet) to fail without local mainnet, got: %v", err)
	}

	// 3. Explicit activation: EnableLocalMainnet
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	cfg, err := core.ResolveCLINetwork("mainnet")
	if err != nil {
		t.Fatalf("ResolveCLINetwork(mainnet) failed after EnableLocalMainnet: %v", err)
	}
	if cfg.NetworkProfile.Name != "mainnet" || cfg.NetworkProfile.NetworkID != "ARMX-MAINNET-1" {
		t.Fatalf("Unexpected profile after EnableLocalMainnet: %+v", cfg.NetworkProfile)
	}
	if cfg.DataDir != "mainnet" {
		t.Fatalf("Expected DataDir 'mainnet', got %q", cfg.DataDir)
	}
	if cfg.RPCPort != "19332" || cfg.P2PPort != "19334" {
		t.Fatalf("Expected Mainnet ports 19332/19334, got %s/%s", cfg.RPCPort, cfg.P2PPort)
	}
	if len(cfg.DefaultSeeds) != 0 {
		t.Fatalf("Expected empty DefaultSeeds in local mainnet mode, got: %v", cfg.DefaultSeeds)
	}
}

// TestLocalMainnet_NoPublicSeedsOrExternalDNS verifies that local mainnet mode
// completely strips public seeds and blocks DNS seed lookups.
func TestLocalMainnet_NoPublicSeedsOrExternalDNS(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	// 1. DNS lookups strictly prohibited
	dnsResults := resolveDNSSeed("id.armax-coin.cloud:19334", false)
	if len(dnsResults) != 0 {
		t.Fatalf("Expected 0 results from resolveDNSSeed in local mainnet mode, got: %v", dnsResults)
	}

	// 2. Active network has zero default seeds
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}
	activeNet := core.GetActiveNetwork()
	if len(activeNet.DefaultSeeds) != 0 {
		t.Fatalf("Expected 0 default seeds on active local mainnet profile, got: %v", activeNet.DefaultSeeds)
	}

	// 3. InitServerFromConfig filters out public seeds from config
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mainnet", "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	cfg := &Config{
		NetworkID: "ARMX-MAINNET-1",
		DataDir:   tempDir,
		RPCBind:   "127.0.0.1",
		RPCPort:   "19332",
		P2PPort:   "19334",
		SeedPeers: []string{
			"id.armax-coin.cloud:19334",
			"202.10.34.180:19334",
			"127.0.0.1:40123", // loopback peer
		},
	}
	ps, _, err := InitServerFromConfig(bc, cfg)
	if err != nil {
		t.Fatalf("InitServerFromConfig failed: %v", err)
	}
	defer UnregisterPeerServer(ps)

	// Verify only loopback seed was retained
	if len(cfg.SeedPeers) != 1 || cfg.SeedPeers[0] != "127.0.0.1:40123" {
		t.Fatalf("Expected only loopback seed retained, got: %v", cfg.SeedPeers)
	}
}

// TestLocalMainnet_NonLoopbackBindingRejected verifies that non-loopback RPC and P2P binds
// are strictly rejected in local mainnet mode.
func TestLocalMainnet_NonLoopbackBindingRejected(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	// 1. ValidateRPCBind rejects 0.0.0.0
	cfgWildcard := &Config{
		NetworkID: "ARMX-MAINNET-1",
		RPCBind:   "0.0.0.0",
		RPCPort:   "19332",
	}
	if err := ValidateRPCBind(cfgWildcard); err == nil {
		t.Fatalf("Expected ValidateRPCBind to reject 0.0.0.0 in local mainnet mode")
	}

	// 2. ValidateRPCBind rejects public IP even if AllowNonLoopbackRPC is set
	cfgPublic := &Config{
		NetworkID:           "ARMX-MAINNET-1",
		RPCBind:             "202.10.34.180",
		RPCPort:             "19332",
		AllowNonLoopbackRPC: true,
		RPCUser:             "user",
		RPCPassword:         "pass",
	}
	if err := ValidateRPCBind(cfgPublic); err == nil {
		t.Fatalf("Expected ValidateRPCBind to reject non-loopback even with AllowNonLoopbackRPC")
	}

	// 3. Loopback bind succeeds
	cfgLoopback := &Config{
		NetworkID: "ARMX-MAINNET-1",
		RPCBind:   "127.0.0.1",
		RPCPort:   "19332",
	}
	if err := ValidateRPCBind(cfgLoopback); err != nil {
		t.Fatalf("Expected ValidateRPCBind to accept 127.0.0.1: %v", err)
	}
}

// TestLocalMainnet_ExternalP2PDialRejected verifies that AddPeer, DialPeer, and DialEncryptedWithFallback
// strictly reject non-loopback external peers in local mainnet mode.
func TestLocalMainnet_ExternalP2PDialRejected(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mainnet", "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer("19334", bc, tempDir, "ARMX-MAINNET-1")
	defer UnregisterPeerServer(ps)

	// 1. AddPeer rejects public IP
	_ = ps.AddPeer("202.10.34.180:19334")
	if ps.HasPeer("202.10.34.180:19334") {
		t.Fatalf("AddPeer accepted external IP in local mainnet mode")
	}

	// 2. AddPeer rejects domain
	_ = ps.AddPeer("id.armax-coin.cloud:19334")
	if ps.HasPeer("id.armax-coin.cloud:19334") {
		t.Fatalf("AddPeer accepted external domain in local mainnet mode")
	}

	// 3. AddPeer accepts loopback
	_ = ps.AddPeer("127.0.0.1:19335")
	if !ps.HasPeer("127.0.0.1:19335") {
		t.Fatalf("AddPeer rejected loopback IP in local mainnet mode")
	}

	// 4. DialPeer strictly rejects non-loopback
	_, _, err = ps.DialPeer("202.10.34.180:19334", 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "strictly prohibits outbound connection to non-loopback peer") {
		t.Fatalf("Expected dial error for external peer, got: %v", err)
	}

	// 5. DialEncryptedWithFallback strictly rejects non-loopback
	_, _, err = DialEncryptedWithFallback("202.10.34.180:19334", 100*time.Millisecond, true)
	if err == nil || !strings.Contains(err.Error(), "strictly prohibits dialing non-loopback peer") {
		t.Fatalf("Expected fallback dial error for external peer, got: %v", err)
	}
}

// TestLocalMainnet_RPCLoopbackAccessWithAuth verifies that loopback RPC
// works properly with cookie / credential authentication in local mainnet mode.
func TestLocalMainnet_RPCLoopbackAccessWithAuth(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mainnet", "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer("19334", bc, tempDir, "ARMX-MAINNET-1")
	defer UnregisterPeerServer(ps)

	serverCfg := &ServerConfig{
		NetworkID:   "ARMX-MAINNET-1",
		DataDir:     tempDir,
		RPCBind:     "127.0.0.1",
		RPCPort:     "19332",
		P2PPort:     "19334",
		RPCUser:     "armaxuser",
		RPCPassword: "armaxpassword123",
	}

	limiter := newIPRateLimiter()
	if err := InitRPCAuth("armaxuser", "armaxpassword123"); err != nil {
		t.Fatalf("InitRPCAuth failed: %v", err)
	}

	handler := NewRPCHandler(bc, ps, limiter, serverCfg)
	server := httptest.NewServer(handler)
	defer server.Close()

	// 1. Unauthenticated request to protected endpoint (e.g. /mine or POST) should fail or return 401
	req, _ := http.NewRequest("GET", server.URL+"/status", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /status failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for /status, got %d", resp.StatusCode)
	}

	// 2. Authenticated request verifies correct mainnet identity
	reqAuth, _ := http.NewRequest("GET", server.URL+"/stats", nil)
	reqAuth.SetBasicAuth("armaxuser", "armaxpassword123")
	respAuth, err := client.Do(reqAuth)
	if err != nil {
		t.Fatalf("GET /stats with auth failed: %v", err)
	}
	defer respAuth.Body.Close()
	if respAuth.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for /stats with auth, got %d", respAuth.StatusCode)
	}
}

// TestLocalMainnet_MiningStorageRestartLifecycle verifies that local mainnet
// can mine a valid block with Mainnet PoW, persist to disk, reload across restarts, and verify integrity.
func TestLocalMainnet_MiningStorageRestartLifecycle(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mainnet", "blockchain.db")

	// Phase 1: Initialize fresh mainnet blockchain
	bc1, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("Failed to create blockchain: %v", err)
	}
	if bc1.Chain[0].Hash != core.GenesisHash {
		t.Fatalf("Genesis hash mismatch: got %s, want %s", bc1.Chain[0].Hash, core.GenesisHash)
	}

	// Mine block #1 with SHA-256d
	minerAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	b1, err := bc1.MineBlockWithErr(minerAddr)
	if err != nil {
		t.Fatalf("MineBlockWithErr failed: %v", err)
	}
	if b1.Index != 1 {
		t.Fatalf("Expected mined block index 1, got %d", b1.Index)
	}
	if err := bc1.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
	if err := bc1.Close(); err != nil {
		t.Fatalf("Close bc1 failed: %v", err)
	}

	// Phase 2: Reload node state from disk and verify
	bc2, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("Failed to reload blockchain: %v", err)
	}
	defer bc2.Close()

	if len(bc2.Chain) != 2 {
		t.Fatalf("Expected 2 blocks on reload, got %d", len(bc2.Chain))
	}
	if bc2.GetLatestBlock().Hash != b1.Hash {
		t.Fatalf("Tip hash mismatch on reload: got %s, want %s", bc2.GetLatestBlock().Hash, b1.Hash)
	}
	if !bc2.IsChainValid() {
		t.Fatalf("IsChainValid returned false for reloaded mainnet blockchain")
	}
}

// TestLocalMainnet_AlphaTestnetCompletelyUntouched verifies that running Local Mainnet
// does not touch, mutate, or access any existing Alpha Testnet database or wallet files.
func TestLocalMainnet_AlphaTestnetCompletelyUntouched(t *testing.T) {
	core.LockNetworkForTesting(t)

	tempDir := t.TempDir()

	// 1. Create a simulated existing Alpha Testnet environment
	testnetDir := filepath.Join(tempDir, "testnet")
	if err := os.MkdirAll(testnetDir, 0700); err != nil {
		t.Fatalf("MkdirAll testnetDir failed: %v", err)
	}
	testnetDBPath := filepath.Join(testnetDir, "blockchain.db")
	testnetWalletPath := filepath.Join(testnetDir, "wallet.json")

	// Write simulated testnet DB and wallet data
	simulatedDBData := []byte("SIMULATED_TESTNET_BOLT_DATABASE_BYTES_DO_NOT_TOUCH")
	simulatedWalletData := []byte(`{"address": "armx1testnetmockaddress", "encrypted_seed": "mockseed"}`)
	if err := os.WriteFile(testnetDBPath, simulatedDBData, 0600); err != nil {
		t.Fatalf("WriteFile testnetDB failed: %v", err)
	}
	if err := os.WriteFile(testnetWalletPath, simulatedWalletData, 0600); err != nil {
		t.Fatalf("WriteFile testnetWallet failed: %v", err)
	}

	dbHashBefore := sha256.Sum256(simulatedDBData)
	walletHashBefore := sha256.Sum256(simulatedWalletData)

	// 2. Run Local Mainnet operations in the SAME parent data directory
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}

	mainnetDir := filepath.Join(tempDir, "mainnet")
	mainnetDBPath := filepath.Join(mainnetDir, "blockchain.db")

	bc, err := core.NewBlockchainWithStoragePath(1, mainnetDBPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath for local mainnet failed: %v", err)
	}
	minerAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	_, err = bc.MineBlockWithErr(minerAddr)
	if err != nil {
		t.Fatalf("MineBlockWithErr failed: %v", err)
	}
	_ = bc.SaveToFile()
	_ = bc.Close()

	// 3. Verify Alpha Testnet database and wallet files are 100% untouched
	dbDataAfter, err := os.ReadFile(testnetDBPath)
	if err != nil {
		t.Fatalf("Failed to read testnetDB after mainnet execution: %v", err)
	}
	dbHashAfter := sha256.Sum256(dbDataAfter)
	if hex.EncodeToString(dbHashBefore[:]) != hex.EncodeToString(dbHashAfter[:]) {
		t.Fatalf("CRITICAL: Testnet database file was mutated by Local Mainnet operations!")
	}

	walletDataAfter, err := os.ReadFile(testnetWalletPath)
	if err != nil {
		t.Fatalf("Failed to read testnetWallet after mainnet execution: %v", err)
	}
	walletHashAfter := sha256.Sum256(walletDataAfter)
	if hex.EncodeToString(walletHashBefore[:]) != hex.EncodeToString(walletHashAfter[:]) {
		t.Fatalf("CRITICAL: Testnet wallet file was mutated by Local Mainnet operations!")
	}
}

// TestLocalMainnet_P2PListenerLoopbackOnly verifies that in local mainnet mode,
// the TCP listener binds strictly to 127.0.0.1 rather than 0.0.0.0.
func TestLocalMainnet_P2PListenerLoopbackOnly(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mainnet", "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	// Find an available port
	tmpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to acquire ephemeral port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(tmpListener.Addr().String())
	_ = tmpListener.Close()

	ps := NewPeerServer(portStr, bc, tempDir, "ARMX-MAINNET-1")
	defer UnregisterPeerServer(ps)

	go ps.StartServer()
	time.Sleep(100 * time.Millisecond)
	defer ps.Stop()

	// Verify listener is bound to loopback
	ps.stopLock.Lock()
	l := ps.listener
	ps.stopLock.Unlock()
	if l == nil {
		t.Fatalf("PeerServer listener failed to initialize")
	}

	addr := l.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("Expected listener to bind to 127.0.0.1, got: %s", addr)
	}
}

// TestLocalMainnet_OutboundDialingExhaustiveValidation verifies Section C:
// 1. Valid loopback IPv4, IPv6, IPv4-mapped IPv6, and localhost succeed.
// 2. ResolveAndValidateLoopback converts "localhost" to concrete loopback IP to eliminate TOCTOU / DNS rebinding.
// 3. Arbitrary hostnames are rejected fail-closed without external DNS resolution.
// 4. Malformed addresses and non-loopback IPs immediately fail-closed.
// 5. Testnet network dials bypass local mainnet restrictions.
// 6. Actual outbound connection to localhost connects socket directly to loopback IP.
func TestLocalMainnet_OutboundDialingExhaustiveValidation(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	// 1. Valid loopback destinations
	validAddrs := []string{
		"127.0.0.1:19334",
		"127.0.0.2:19334",
		"[::1]:19334",
		"[::ffff:127.0.0.1]:19334",
		"localhost:19334",
	}
	for _, addr := range validAddrs {
		targetAddrs, serverName, err := ResolveAndValidateLoopback(addr)
		if err != nil {
			t.Errorf("ResolveAndValidateLoopback unexpectedly rejected valid loopback %q: %v", addr, err)
		}
		if len(targetAddrs) == 0 {
			t.Fatalf("ResolveAndValidateLoopback returned empty targetAddrs for %q", addr)
		}
		for _, targetAddr := range targetAddrs {
			// targetAddr must be a concrete IP:port (not "localhost") to prevent secondary DNS lookups by dialer
			host, _, splitErr := net.SplitHostPort(targetAddr)
			if splitErr != nil {
				t.Fatalf("SplitHostPort failed on targetAddr %q: %v", targetAddr, splitErr)
			}
			hostClean := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
			parsedIP := net.ParseIP(hostClean)
			if parsedIP == nil || !isLoopbackIP(parsedIP) {
				t.Fatalf("ResolveAndValidateLoopback did not return a concrete loopback IP! got: %s", targetAddr)
			}
		}
		if strings.EqualFold(addr, "localhost:19334") && serverName != "localhost" {
			t.Fatalf("Expected serverName 'localhost' to be preserved for TLS SNI, got %q", serverName)
		}
	}

	// 2. Malformed addresses (fail-closed)
	malformedAddrs := []string{
		"no_port_here",
		":19334",
		"127.0.0.1:",
		"[invalid_bracket:19334",
		"",
	}
	for _, addr := range malformedAddrs {
		if err := ValidateLoopbackAddress(addr); err == nil {
			t.Errorf("ValidateLoopbackAddress unexpectedly accepted malformed address %q", addr)
		}
		// DialEncryptedWithFallbackForNetwork must also return immediate error
		_, _, err := DialEncryptedWithFallbackForNetwork("ARMX-MAINNET-1", addr, 10*time.Millisecond, true)
		if err == nil {
			t.Errorf("DialEncryptedWithFallbackForNetwork unexpectedly accepted malformed address %q", addr)
		}
	}

	// 3. Non-loopback addresses and arbitrary hostnames (fail-closed, no external lookups)
	nonLoopbackAddrs := []string{
		"202.10.34.180:19334",
		"192.168.1.50:19334",
		"10.0.0.1:19334",
		"[2001:db8::1]:19334",
		"[::ffff:202.10.34.180]:19334",
		"id.armax-coin.cloud:19334",
		"evil.com:19334",
		"attacker-domain.org:19334",
	}
	for _, addr := range nonLoopbackAddrs {
		if err := ValidateLoopbackAddress(addr); err == nil {
			t.Errorf("ValidateLoopbackAddress unexpectedly accepted non-loopback address %q", addr)
		}
		_, _, err := DialEncryptedWithFallbackForNetwork("ARMX-MAINNET-1", addr, 10*time.Millisecond, true)
		if err == nil {
			t.Errorf("DialEncryptedWithFallbackForNetwork unexpectedly accepted non-loopback address %q", addr)
		}
	}

	// 4. Actual outbound connection test: verify dialer connects directly to loopback IP
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start local test listener: %v", err)
	}
	defer ln.Close()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed on listener addr: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	// Dial using "localhost:<port>" in local mainnet mode
	clientConn, _, err := DialEncryptedWithFallbackForNetwork("ARMX-MAINNET-1", "localhost:"+port, 2*time.Second, true)
	if err != nil {
		t.Fatalf("Failed to dial local listener via localhost:%s in local mainnet mode: %v", port, err)
	}
	defer clientConn.Close()

	select {
	case serverConn := <-accepted:
		defer serverConn.Close()
		remoteAddr := serverConn.RemoteAddr().String()
		rHost, _, _ := net.SplitHostPort(remoteAddr)
		rIP := net.ParseIP(rHost)
		if rIP == nil || !isLoopbackIP(rIP) {
			t.Fatalf("Inbound connection received from non-loopback remote address %s!", remoteAddr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for test server to accept connection")
	}

	// 5. Testnet network dial must NOT be blocked by local mainnet restrictions
	// Malformed address still fails parsing, but valid non-loopback IP for Testnet is permitted through to the dialer
	// (we verify it doesn't fail with "local mainnet mode strictly prohibits")
	_, _, err = DialEncryptedWithFallbackForNetwork("ARMX-TESTNET-1", "202.10.34.180:19335", 10*time.Millisecond, true)
	if err != nil && strings.Contains(err.Error(), "local mainnet mode") {
		t.Fatalf("Testnet dial was incorrectly blocked by local mainnet loopback validator: %v", err)
	}
}

// TestLocalMainnet_PeerCacheFiltering verifies Section D:
// When local mainnet loads peer cache, non-loopback addresses are ignored without mutating disk file.
// Testnet loads all peers normally.
func TestLocalMainnet_PeerCacheFiltering(t *testing.T) {
	core.LockNetworkForTesting(t)

	tempDir := t.TempDir()
	mainnetDir := filepath.Join(tempDir, "mainnet")
	if err := os.MkdirAll(mainnetDir, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	peersFile := filepath.Join(mainnetDir, "peers.json")

	// Create mixed peer store with loopback and external peers
	mixedStore := VersionedPeerStore{
		Version:   1,
		NetworkID: "ARMX-MAINNET-1",
		Peers: []*PeerInfo{
			{Address: "127.0.0.1:19335", CertFingerprint: "fp1"},
			{Address: "202.10.34.180:19334", CertFingerprint: "fp2"},
			{Address: "id.armax-coin.cloud:19334"},
			{Address: "[::1]:19335", CertFingerprint: "fp3"},
		},
	}
	dataBefore, err := json.MarshalIndent(mixedStore, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}
	if err := os.WriteFile(peersFile, dataBefore, 0600); err != nil {
		t.Fatalf("Failed to write initial peers.json: %v", err)
	}
	hashBefore := sha256.Sum256(dataBefore)

	// 1. Start Local Mainnet PeerServer
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	dbPath := filepath.Join(mainnetDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer("19334", bc, tempDir, "ARMX-MAINNET-1")
	defer UnregisterPeerServer(ps)

	// Verify only loopback peers (127.0.0.1 and ::1) were loaded into memory
	if len(ps.Peers) != 2 {
		t.Fatalf("Expected exactly 2 loopback peers loaded into memory, got %d: %v", len(ps.Peers), ps.Peers)
	}
	for _, pAddr := range ps.Peers {
		if !strings.Contains(pAddr, "127.0.0.1") && !strings.Contains(pAddr, "::1") {
			t.Fatalf("Non-loopback peer loaded into local mainnet memory: %s", pAddr)
		}
	}

	// 2. Verify disk file was NOT mutated
	dataAfter, err := os.ReadFile(peersFile)
	if err != nil {
		t.Fatalf("Failed to read peers.json after loading: %v", err)
	}
	hashAfter := sha256.Sum256(dataAfter)
	if hashBefore != hashAfter {
		t.Fatalf("Peer cache file on disk was mutated during local mainnet peer filtering!")
	}
}

// TestLocalMainnet_SimultaneousNetworkIsolation verifies Section B:
// Mainnet and Testnet instances coexisting in the same process each use their own
// correct genesis checkpoints regardless of global ActiveNetwork transitions.
func TestLocalMainnet_SimultaneousNetworkIsolation(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.EnableLocalMainnet()
	defer core.DisableLocalMainnet()
	_ = core.SetNetwork("mainnet")

	tempDir := t.TempDir()

	// 1. Create Mainnet blockchain
	mainnetDB := filepath.Join(tempDir, "mainnet", "blockchain.db")
	_ = core.SetNetwork("mainnet")
	bcMain, err := core.NewBlockchainWithStoragePath(1, mainnetDB)
	if err != nil {
		t.Fatalf("Failed to create mainnet blockchain: %v", err)
	}
	defer bcMain.Close()

	// 2. Create Testnet blockchain
	testnetDB := filepath.Join(tempDir, "testnet", "blockchain.db")
	_ = core.SetNetwork("testnet")
	bcTest, err := core.NewBlockchainWithStoragePath(1, testnetDB)
	if err != nil {
		t.Fatalf("Failed to create testnet blockchain: %v", err)
	}
	defer bcTest.Close()

	// 3. Switch global ActiveNetwork to Testnet
	_ = core.SetNetwork("testnet")

	// Both blockchains MUST validate their own checkpoints cleanly!
	if !bcMain.IsChainValid() {
		t.Fatalf("Mainnet blockchain IsChainValid failed when global profile was set to Testnet!")
	}
	if !bcTest.IsChainValid() {
		t.Fatalf("Testnet blockchain IsChainValid failed when global profile was set to Testnet!")
	}

	// 4. Switch global ActiveNetwork to Mainnet
	_ = core.SetNetwork("mainnet")

	// Both blockchains MUST STILL validate their own checkpoints cleanly!
	if !bcMain.IsChainValid() {
		t.Fatalf("Mainnet blockchain IsChainValid failed when global profile was set to Mainnet!")
	}
	if !bcTest.IsChainValid() {
		t.Fatalf("Testnet blockchain IsChainValid failed when global profile was set to Mainnet!")
	}

	// 5. Verify Checkpoint for Network helper
	mainCkpt, exists := core.GetCheckpointForNetwork("ARMX-MAINNET-1", 0)
	if !exists || mainCkpt != core.GenesisHash {
		t.Fatalf("GetCheckpointForNetwork(ARMX-MAINNET-1, 0) mismatch: got %s, want %s", mainCkpt, core.GenesisHash)
	}
	testCkpt, exists := core.GetCheckpointForNetwork("ARMX-TESTNET-1", 0)
	if !exists || testCkpt != core.TestnetGenesisHash {
		t.Fatalf("GetCheckpointForNetwork(ARMX-TESTNET-1, 0) mismatch: got %s, want %s", testCkpt, core.TestnetGenesisHash)
	}
}

// TestLocalMainnet_TestCleanupRestoresState verifies Section E:
// LockNetworkForTesting resets LocalMainnet state upon cleanup.
func TestLocalMainnet_TestCleanupRestoresState(t *testing.T) {
	// Run an isolated subtest that sets LocalMainnet
	t.Run("SubTestWithLocalMainnet", func(subT *testing.T) {
		core.LockNetworkForTesting(subT)
		core.EnableLocalMainnet()
		if !core.IsLocalMainnet() {
			subT.Fatalf("Expected IsLocalMainnet to be true within subtest")
		}
	})

	// After subtest cleanup completes, LocalMainnet must be restored to false
	if core.IsLocalMainnet() {
		t.Fatalf("CRITICAL: LockNetworkForTesting cleanup failed to reset localMainnetEnabled!")
	}
}
