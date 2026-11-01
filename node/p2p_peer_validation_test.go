package node

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func TestInboundPeerRequiresIdentityAndNetworkHandshake(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	port := getFreePort(t)
	server := NewPeerServer(port, bc, filepath.Join(dir, "server"), "ARMX-TESTNET-1")
	server.RequireTLS = true
	server.P2PTOFU = true
	server.AllowPrivatePeers = true
	server.DisableSubnetLimit = true
	go server.StartServer()
	defer server.Stop()
	waitForPeerServer(t, server)
	address := net.JoinHostPort("127.0.0.1", port)

	bareConn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if server.GetPeersActive() != 0 {
		t.Fatalf("bare TCP connection counted as active: %d", server.GetPeersActive())
	}
	_ = bareConn.Close()

	clientCert, err := loadOrGenerateNodeTLSCertificate(filepath.Join(dir, "client"))
	if err != nil {
		t.Fatal(err)
	}
	clientTLS := clientTLSConf.Clone()
	clientTLS.Certificates = []tls.Certificate{clientCert}

	wrongNetworkConn, err := tls.Dial("tcp", address, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	_ = wrongNetworkConn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(wrongNetworkConn).Encode(Message{
		NetworkID: "ARMX-WRONG-NETWORK",
		Type:      "GET_HEIGHT",
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if server.GetPeersActive() != 0 {
		t.Fatalf("wrong network ID counted as active: %d", server.GetPeersActive())
	}
	_ = wrongNetworkConn.Close()

	noIdentityTLS := clientTLSConf.Clone()
	noIdentityConn, err := tls.Dial("tcp", address, noIdentityTLS)
	if err != nil {
		t.Fatal(err)
	}
	_ = noIdentityConn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(noIdentityConn).Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = noIdentityConn.Read(make([]byte, 1))
	if server.GetPeersActive() != 0 {
		t.Fatalf("connection without client identity counted as active: %d", server.GetPeersActive())
	}
	_ = noIdentityConn.Close()

	clientBC, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "client-blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer clientBC.Close()
	client := NewPeerServer(getFreePort(t), clientBC, filepath.Join(dir, "client"), "ARMX-TESTNET-1")
	client.RequireTLS = true
	client.P2PTOFU = true
	defer client.Stop()

	response, err := client.QueryPeer(address, Message{
		NetworkID: client.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("valid application handshake failed: %v", err)
	}
	if response == nil || response.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("unexpected handshake response: %#v", response)
	}
	waitForPeerActiveCount(t, server, 1)

	client.outboundMu.Lock()
	session := client.outboundSessions[address]
	client.outboundMu.Unlock()
	client.dropOutboundSession(address, session)
	waitForNoActivePeers(t, server)
}

func TestActivePeerRequiresHandshakeWhenTOFUIsDisabled(t *testing.T) {
	dir := t.TempDir()
	serverBC, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverBC.Close()

	server := NewPeerServer(getFreePort(t), serverBC, filepath.Join(dir, "server"), "ARMX-TESTNET-1")
	server.RequireTLS = true
	server.AllowPrivatePeers = true
	server.DisableSubnetLimit = true
	go server.StartServer()
	defer server.Stop()
	waitForPeerServer(t, server)
	address := net.JoinHostPort("127.0.0.1", server.Port)

	clientBC, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer clientBC.Close()

	client := NewPeerServer(getFreePort(t), clientBC, filepath.Join(dir, "client"), "ARMX-TESTNET-1")
	client.RequireTLS = true
	defer client.Stop()

	conn, release, err := client.DialPeer(address, time.Second)
	if err != nil {
		t.Fatalf("TLS transport setup failed: %v", err)
	}
	if server.GetPeersActive() != 0 || client.GetPeersActive() != 0 {
		t.Fatal("TLS transport without application handshake counted as active")
	}
	_ = conn.Close()
	release()

	response, err := client.QueryPeer(address, Message{
		NetworkID: client.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("valid application handshake failed without TOFU: %v", err)
	}
	if response == nil || response.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("unexpected handshake response: %#v", response)
	}
	waitForPeerActiveCount(t, server, 1)
	if client.GetPeersActive() != 1 {
		t.Fatalf("expected one protocol-validated outbound session, got %d", client.GetPeersActive())
	}

	client.outboundMu.Lock()
	session := client.outboundSessions[address]
	client.outboundMu.Unlock()
	client.dropOutboundSession(address, session)
	waitForNoActivePeers(t, server)
	if client.GetPeersActive() != 0 {
		t.Fatalf("outbound peer remained active after disconnect: %d", client.GetPeersActive())
	}
}

func TestDNSSeedRefreshReplacesAliasesAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	server := NewPeerServer(getFreePort(t), bc, dir, "ARMX-TESTNET-1")
	server.AllowPrivatePeers = true
	seed := "seed.example:1234"
	var resolverMu sync.RWMutex
	resolved := []string{"203.0.113.1:1234", "203.0.113.1:1234", "203.0.113.2:1234"}
	server.seedMu.Lock()
	server.resolveSeed = func(string, bool) []string {
		resolverMu.RLock()
		defer resolverMu.RUnlock()
		return append([]string(nil), resolved...)
	}
	server.seedMu.Unlock()

	secondSeed := "seed2.example:1234"
	server.SetSeedPeers([]string{seed, seed, secondSeed})
	if server.GetPeersSeeds() != 2 {
		t.Fatalf("duplicate configured seed was retained: %d seeds", server.GetPeersSeeds())
	}
	server.seedMu.RLock()
	firstAliases := len(server.seedHostToIPs[seed])
	sharedIPHosts := append([]string(nil), server.seedIPToHosts["203.0.113.2:1234"]...)
	server.seedMu.RUnlock()
	if firstAliases != 2 {
		t.Fatalf("expected duplicate resolved IP to be removed, got %d aliases", firstAliases)
	}
	if len(sharedIPHosts) != 2 {
		t.Fatalf("expected shared IP to map to both seed aliases, got %v", sharedIPHosts)
	}
	if server.GetPeersActive() != 0 {
		t.Fatal("DNS seed aliases must not count as active peers")
	}

	if err := server.AddPeer("203.0.113.1:1234"); err != nil {
		t.Fatal(err)
	}
	resolverMu.Lock()
	resolved = []string{"203.0.113.2:1234", "203.0.113.3:1234"}
	resolverMu.Unlock()
	server.ReseedPeers()

	server.seedMu.RLock()
	_, oldForwardExists := server.seedHostToIPs[seed]
	aliases := append([]string(nil), server.seedHostToIPs[seed]...)
	_, oldReverseExists := server.seedIPToHosts["203.0.113.1:1234"]
	server.seedMu.RUnlock()
	if !oldForwardExists || len(aliases) != 2 {
		t.Fatalf("expected refreshed seed to have two current aliases, got %v", aliases)
	}
	if oldReverseExists {
		t.Fatal("stale reverse DNS mapping was retained")
	}
	if server.areEndpointsEquivalent(seed, "203.0.113.1:1234") {
		t.Fatal("stale seed alias still compares equivalent")
	}
	if server.GetPeersCandidates() != 2 {
		t.Fatalf("expected stale candidate removed and two current candidates retained, got %d", server.GetPeersCandidates())
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = server.areEndpointsEquivalent(seed, "203.0.113.2:1234")
				server.ReseedPeers()
			}
		}()
	}
	wg.Wait()
}

func TestDNSSeedRefreshRejectsOutOfOrderResults(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	server := NewPeerServer(getFreePort(t), bc, dir, "ARMX-TESTNET-1")
	seed := "seed.example:1234"
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var callsMu sync.Mutex
	calls := 0
	server.seedMu.Lock()
	server.seedPeers = []string{seed}
	server.resolveSeed = func(string, bool) []string {
		callsMu.Lock()
		calls++
		call := calls
		callsMu.Unlock()
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
			return []string{"203.0.113.10:1234"}
		}
		return []string{"203.0.113.11:1234"}
	}
	server.seedMu.Unlock()

	var firstRefresh sync.WaitGroup
	firstRefresh.Add(1)
	go func() {
		defer firstRefresh.Done()
		server.ReseedPeers()
	}()
	<-firstStarted
	server.ReseedPeers()
	close(releaseFirst)
	firstRefresh.Wait()

	server.seedMu.RLock()
	aliases := append([]string(nil), server.seedHostToIPs[seed]...)
	server.seedMu.RUnlock()
	if len(aliases) != 1 || aliases[0] != "203.0.113.11:1234" {
		t.Fatalf("out-of-order DNS result replaced latest mapping: %v", aliases)
	}
}

func waitForPeerServer(t *testing.T, server *PeerServer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.GetState() == ServerStateRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("peer server did not start")
}

func waitForNoActivePeers(t *testing.T, server *PeerServer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.GetPeersActive() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("active peers did not drain: %d", server.GetPeersActive())
}

func waitForPeerActiveCount(t *testing.T, server *PeerServer, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.GetPeersActive() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d active peers, got %d", want, server.GetPeersActive())
}

// TestAF02_InboundTOFURejectsUnauthenticatedDiscoveryGossipAndPreservesAuthenticatedDiscovery
// verifies that in TOFU mode:
//  1. Unauthenticated inbound PEERS_LIST is rejected (peers not added).
//  2. Unauthenticated inbound DHT_NODES is rejected (nodes/peers not added).
//  3. Unverified peers cannot be promoted to authenticated/active status.
//  4. Valid authenticated peers presenting a client TLS certificate can still exchange PEERS_LIST and DHT_NODES.
func TestAF02_InboundTOFURejectsUnauthenticatedDiscoveryGossipAndPreservesAuthenticatedDiscovery(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	port := getFreePort(t)
	server := NewPeerServer(port, bc, filepath.Join(dir, "server"), "ARMX-TESTNET-1")
	server.RequireTLS = true
	server.P2PTOFU = true
	server.AllowPrivatePeers = true
	server.DisableSubnetLimit = true
	go server.StartServer()
	defer server.Stop()
	waitForPeerServer(t, server)
	address := net.JoinHostPort("127.0.0.1", port)

	// 1. Connect over TLS WITHOUT presenting a client certificate (pendingFingerprint == "")
	noIdentityTLS := clientTLSConf.Clone()
	noIdentityTLS.Certificates = nil
	unauthConn, err := tls.Dial("tcp", address, noIdentityTLS)
	if err != nil {
		t.Fatalf("tls.Dial without client cert failed: %v", err)
	}
	_ = unauthConn.SetDeadline(time.Now().Add(2 * time.Second))

	enc := json.NewEncoder(unauthConn)
	unauthPeerCandidate := "198.51.100.77:19335"
	unauthDHTCandidate := "198.51.100.88:19335"

	if err := enc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "PEERS_LIST",
		Payload:   []string{unauthPeerCandidate},
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "DHT_NODES",
		Payload:   []DHTNodeInfo{{Address: unauthDHTCandidate}},
	}); err != nil {
		t.Fatal(err)
	}
	// Send GET_HEIGHT after discovery gossip to synchronize server read loop and verify no promotion
	if err := enc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}); err != nil {
		t.Fatal(err)
	}
	var heightResp Message
	if err := json.NewDecoder(unauthConn).Decode(&heightResp); err != nil {
		t.Fatalf("expected HEIGHT_RESPONSE to synchronize read loop, got err: %v", err)
	}
	_ = unauthConn.Close()

	if server.GetPeersActive() != 0 {
		t.Fatalf("unauthenticated peer was promoted to active status: active=%d", server.GetPeersActive())
	}
	if server.HasPeer(unauthPeerCandidate) {
		t.Fatalf("unauthenticated PEERS_LIST injected %s into peer table", unauthPeerCandidate)
	}
	if server.HasPeer(unauthDHTCandidate) {
		t.Fatalf("unauthenticated DHT_NODES injected %s into peer table", unauthDHTCandidate)
	}

	// 2. Connect with a valid client TLS certificate and verify authenticated discovery works
	clientCert, err := loadOrGenerateNodeTLSCertificate(filepath.Join(dir, "auth-client"))
	if err != nil {
		t.Fatal(err)
	}
	authTLS := clientTLSConf.Clone()
	authTLS.Certificates = []tls.Certificate{clientCert}

	authConn, err := tls.Dial("tcp", address, authTLS)
	if err != nil {
		t.Fatalf("authenticated tls.Dial failed: %v", err)
	}
	defer authConn.Close()
	_ = authConn.SetDeadline(time.Now().Add(2 * time.Second))

	authEnc := json.NewEncoder(authConn)
	authDec := json.NewDecoder(authConn)

	// Complete authenticated GET_HEIGHT handshake first
	if err := authEnc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}); err != nil {
		t.Fatal(err)
	}
	if err := authDec.Decode(&heightResp); err != nil {
		t.Fatalf("authenticated GET_HEIGHT decode failed: %v", err)
	}
	waitForPeerActiveCount(t, server, 1)

	authPeerCandidate := "198.51.100.91:19335"
	authDHTCandidate := "198.51.100.92:19335"
	if err := authEnc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "PEERS_LIST",
		Payload:   []string{authPeerCandidate},
	}); err != nil {
		t.Fatal(err)
	}
	if err := authEnc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "DHT_NODES",
		Payload:   []DHTNodeInfo{{Address: authDHTCandidate}},
	}); err != nil {
		t.Fatal(err)
	}
	// Synchronize by sending another GET_HEIGHT
	if err := authEnc.Encode(Message{
		NetworkID: server.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}); err != nil {
		t.Fatal(err)
	}
	if err := authDec.Decode(&heightResp); err != nil {
		t.Fatalf("sync GET_HEIGHT decode failed: %v", err)
	}

	if !server.HasPeer(authPeerCandidate) {
		t.Fatalf("authenticated PEERS_LIST candidate %s was not accepted", authPeerCandidate)
	}
	if !server.HasPeer(authDHTCandidate) {
		t.Fatalf("authenticated DHT_NODES candidate %s was not accepted", authDHTCandidate)
	}
}

// TestAF07_IsSelfPeerAddressLoopbackVsRemoteSamePort verifies that:
//  1. 127.0.0.1:<ownPort> is rejected as self.
//  2. [::1]:<ownPort> is rejected as self.
//  3. localhost:<ownPort> is rejected as self.
//  4. A remote public IPv4 or IPv6 using the same port is NOT rejected as self.
func TestAF07_IsSelfPeerAddressLoopbackVsRemoteSamePort(t *testing.T) {
	dir := t.TempDir()
	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dir, "blockchain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Close()

	ownPort := "19335"
	ps := NewPeerServer(ownPort, bc, dir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true

	selfAddresses := []string{
		"127.0.0.1:" + ownPort,
		"[::1]:" + ownPort,
		"localhost:" + ownPort,
		"LOCALHOST:" + ownPort,
		"0.0.0.0:" + ownPort,
		"[::]:" + ownPort,
	}
	for _, addr := range selfAddresses {
		if !ps.isSelfPeerAddress(addr) {
			t.Fatalf("expected %q to be detected as self-peer on port %s", addr, ownPort)
		}
		if err := ps.AddPeer(addr); err != nil {
			t.Fatalf("AddPeer(%q) returned unexpected error: %v", addr, err)
		}
		if ps.HasPeer(addr) {
			t.Fatalf("self-peer %q was erroneously added to peer table", addr)
		}
	}

	nonSelfAddresses := []string{
		"203.0.113.50:" + ownPort,
		"[2001:db8::50]:" + ownPort,
		"127.0.0.1:29335",
		"[::1]:29335",
	}
	for _, addr := range nonSelfAddresses {
		if ps.isSelfPeerAddress(addr) {
			t.Fatalf("expected %q NOT to be detected as self-peer on port %s", addr, ownPort)
		}
		if err := ps.AddPeer(addr); err != nil {
			t.Fatalf("AddPeer(%q) failed: %v", addr, err)
		}
		if !ps.HasPeer(addr) {
			t.Fatalf("legitimate non-self peer %q was not added to peer table", addr)
		}
	}
}
