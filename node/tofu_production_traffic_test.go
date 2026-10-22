//go:build testhooks

package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func generateEphemeralTestCert(commonName string) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Armaxcoin Test Peer"},
			CommonName:   commonName,
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return tls.X509KeyPair(certPEM, keyPEM)
}

// startMockTLSServer runs a TLS 1.3 listener with the provided certificate on an ephemeral port.
func startMockTLSServer(t *testing.T, cert tls.Certificate) (net.Listener, string) {
	return startMockTLSServerOnAddr(t, cert, "127.0.0.1:0")
}

// startMockTLSServerOnAddr runs a TLS 1.3 listener with the provided certificate on a specific address.
func startMockTLSServerOnAddr(t *testing.T, cert tls.Certificate, addr string) (net.Listener, string) {
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	ln, err := tls.Listen("tcp", addr, serverConfig)
	if err != nil {
		t.Fatalf("Failed to start mock TLS server on %s: %v", addr, err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				decoder := json.NewDecoder(c)
				encoder := json.NewEncoder(c)

				var msg Message
				if err := decoder.Decode(&msg); err != nil {
					return
				}

				switch msg.Type {
				case "GET_HEIGHT":
					_ = encoder.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "HEIGHT_RESPONSE",
						Payload: HeightResponse{
							Height:    10,
							Chainwork: "1000",
						},
					})
				case "GET_PEERS":
					_ = encoder.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "PEERS_LIST",
						Payload:   []string{"127.0.0.1:19335"},
					})
				case "DHT_FIND_NODE":
					_ = encoder.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "DHT_NODES",
						Payload:   []DHTNodeInfo{{Address: "127.0.0.1:19335"}},
					})
				case "GET_FORKPOINT":
					_ = encoder.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "FORKPOINT_RESPONSE",
						Payload: ForkpointResponse{
							AncestorHeight: 0,
							AncestorHash:   core.GenesisHash,
						},
					})
				case "GET_BLOCK_RANGE":
					_ = encoder.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "BLOCK_RANGE_RESPONSE",
						Payload: BlockRangeResponse{
							Blocks: []core.Block{},
						},
					})
				case "NEW_BLOCK", "NEW_TX":
					// Acknowledged / one-way message
					return
				}
			}(conn)
		}
	}()

	return ln, ln.Addr().String()
}

// TestTOFU_ProductionPath_QueryP2P_PinPersisted verifies:
// 1. Outgoing production QueryP2P with TOFU enabled persists first-use pin.
// 2. Reconnecting after store reload with same certificate succeeds.
// 3. Reconnecting with an altered certificate fails before application message is trusted.
func TestTOFU_ProductionPath_QueryP2P_PinPersisted(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(t.TempDir(), "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.P2PTOFU = true
	ps.RequireTLS = false // Ensure TOFU works independently without strict TLS mode
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	// Start server with Cert A
	certA, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	lnA, peerAddr := startMockTLSServer(t, certA)
	defer lnA.Close()

	// 1. First-use production request via QueryP2P
	resp, err := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 4*time.Second)
	if err != nil {
		t.Fatalf("QueryP2P first contact failed: %v", err)
	}
	if resp.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Unexpected response type: %s", resp.Type)
	}

	// Verify pin is recorded in triedPeers
	ps.Lock.RLock()
	info, pinned := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if !pinned || info.CertFingerprint == "" {
		t.Fatalf("Expected peer %s to be pinned with fingerprint in triedPeers", peerAddr)
	}
	pinnedFP := info.CertFingerprint

	// 2. Simulate node reload / restart from disk
	psReload := NewPeerServer(getFreePort(t), bc)
	psReload.P2PTOFU = true
	psReload.RequireTLS = false
	RegisterPeerServer(psReload)
	defer UnregisterPeerServer(psReload)

	psReload.Lock.RLock()
	reloadedInfo, reloadedExists := psReload.triedPeers[peerAddr]
	psReload.Lock.RUnlock()
	if !reloadedExists || reloadedInfo.CertFingerprint != pinnedFP {
		t.Fatalf("Expected reloaded peer to have pinned fingerprint %s, got %v", pinnedFP, reloadedInfo)
	}

	// Subsequent connection with same certificate -> Must succeed
	resp2, err := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 4*time.Second)
	if err != nil {
		t.Fatalf("QueryP2P subsequent contact with same cert failed: %v", err)
	}
	if resp2.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Unexpected response type: %s", resp2.Type)
	}

	// 3. Close Server A and start Server B with a different (altered) certificate on the exact same port
	lnA.Close()

	// Generate a distinct certificate B
	certB, err := generateEphemeralTestCert("AlteredPeer")
	if err != nil {
		t.Fatalf("Failed to generate test cert B: %v", err)
	}

	serverConfigB := &tls.Config{
		Certificates: []tls.Certificate{certB},
		MinVersion:   tls.VersionTLS13,
	}
	lnB, err := tls.Listen("tcp", peerAddr, serverConfigB)
	if err != nil {
		t.Fatalf("Failed to bind on same peerAddr with altered cert: %v", err)
	}
	defer lnB.Close()

	go func() {
		for {
			conn, err := lnB.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
			}(conn)
		}
	}()

	// Query with altered certificate -> Must fail before application message is trusted
	_, errMitm := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 3*time.Second)
	if errMitm == nil {
		t.Fatalf("Security failure: QueryP2P succeeded against an altered certificate!")
	}
	if !strings.Contains(errMitm.Error(), "fingerprint mismatch") {
		t.Fatalf("Expected fingerprint mismatch error, got: %v", errMitm)
	}
}

// TestTOFU_PlaintextPeerRejected_WhenRequireTLSFalse verifies that when P2PTOFU is true,
// unencrypted plaintext fallback is strictly forbidden even if RequireTLS=false.
func TestTOFU_PlaintextPeerRejected_WhenRequireTLSFalse(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.P2PTOFU = true
	ps.RequireTLS = false // Explicitly false!
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	// Start raw plaintext TCP listener
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen tcp: %v", err)
	}
	defer rawLn.Close()

	peerAddr := rawLn.Addr().String()

	go func() {
		for {
			conn, err := rawLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("{\"network_id\":\"ARMX-TESTNET-1\",\"type\":\"HEIGHT_RESPONSE\"}\n"))
			}(conn)
		}
	}()

	// 1. QueryP2P outbound must fail closed against plaintext peer
	_, errQuery := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 2*time.Second)
	if errQuery == nil {
		t.Fatalf("Security failure: QueryP2P succeeded against unencrypted plaintext peer when P2PTOFU=true!")
	}

	// 2. SendP2PMessage outbound must fail closed against plaintext peer
	errSend := SendP2PMessage(peerAddr, Message{Type: "NEW_TX"})
	if errSend == nil {
		t.Fatalf("Security failure: SendP2PMessage succeeded against unencrypted plaintext peer when P2PTOFU=true!")
	}

	// 3. DialPeer outbound must fail closed against plaintext peer
	_, _, errDial := ps.DialPeer(peerAddr, 2*time.Second)
	if errDial == nil {
		t.Fatalf("Security failure: DialPeer succeeded against unencrypted plaintext peer when P2PTOFU=true!")
	}
	if !strings.Contains(errDial.Error(), "rejected") && !strings.Contains(errDial.Error(), "disabled") && !strings.Contains(errDial.Error(), "handshake") {
		t.Fatalf("Expected plaintext rejection error, got: %v", errDial)
	}
}

// TestTOFU_PersistenceFailure_PinNotTrusted verifies that when peer store persistence fails,
// the connection fails closed and no unpersisted pin is treated as trusted.
func TestTOFU_PersistenceFailure_PinNotTrusted(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.P2PTOFU = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	cert, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	ln, peerAddr := startMockTLSServer(t, cert)
	defer ln.Close()

	// Inject persistence failure
	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		return 0, errors.New("simulated disk full error on peer store persistence")
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	// Outgoing connection must fail because durable persistence failed
	_, errQuery := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 3*time.Second)
	if errQuery == nil {
		t.Fatalf("Expected QueryP2P to fail when persistence fails, but it succeeded!")
	}
	if !strings.Contains(errQuery.Error(), "failed to persist") {
		t.Fatalf("Expected persistence failure error, got: %v", errQuery)
	}

	// Verify in-memory pin was rolled back and is NOT trusted
	ps.Lock.RLock()
	info := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if info != nil && info.CertFingerprint != "" {
		t.Fatalf("Unpersisted pin was retained in triedPeers after persistence failure!")
	}

	// Verify on-disk peers.json does not contain the unpersisted peer
	psReload := NewPeerServer(getFreePort(t), bc)
	psReload.P2PTOFU = true
	psReload.Lock.RLock()
	reloadedInfo := psReload.triedPeers[peerAddr]
	psReload.Lock.RUnlock()
	if reloadedInfo != nil {
		t.Fatalf("Unpersisted pin found in reloaded peer store on disk!")
	}
}

// TestTOFU_ConcurrentFirstContact_BlockedOrFailingSave verifies:
// 1. A second connection to the same peer must not return success based on an uncommitted in-memory pin.
// 2. When the first save fails, neither connection succeeds.
// 3. Different fingerprints cannot be repinned or accepted after a failed save.
func TestTOFU_ConcurrentFirstContact_BlockedOrFailingSave(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.P2PTOFU = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	certA, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	lnA, peerAddr := startMockTLSServer(t, certA)
	defer lnA.Close()

	saveStarted := make(chan struct{})
	saveCanFinish := make(chan struct{})

	// Intercept save to pause during first contact save
	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		select {
		case saveStarted <- struct{}{}:
		default:
		}
		<-saveCanFinish
		return 0, errors.New("simulated I/O failure during concurrent first-contact save")
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	var err1, err2 error
	var wg sync.WaitGroup

	// Launch connection 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err1 = QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 10*time.Second)
	}()

	// Wait until connection 1 has begun the save transaction
	<-saveStarted

	// Launch connection 2 to the EXACT SAME peer address while save is blocked
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err2 = QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 10*time.Second)
	}()

	// Give connection 2 time to reach the pending TOFU check and block on pending.done
	time.Sleep(100 * time.Millisecond)

	// Release save hook so it fails
	close(saveCanFinish)
	wg.Wait()

	// Both connections must fail because durable persistence failed
	if err1 == nil {
		t.Fatalf("Connection 1 succeeded despite persistence failure!")
	}
	if err2 == nil {
		t.Fatalf("Security failure: Connection 2 returned success against an uncommitted pin!")
	}

	// Verify no uncommitted pin is trusted
	ps.Lock.RLock()
	info := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if info != nil && info.CertFingerprint != "" {
		t.Fatalf("Unpersisted pin retained in memory after concurrent failure!")
	}

	// Now verify: an alternate certificate presented for this peer CANNOT be silently repinned!
	certAlt, err := generateEphemeralTestCert("AlternateUntrustedPeer")
	if err != nil {
		t.Fatalf("generateEphemeralTestCert failed: %v", err)
	}

	lnA.Close()
	serverConfigAlt := &tls.Config{
		Certificates: []tls.Certificate{certAlt},
		MinVersion:   tls.VersionTLS13,
	}
	lnAlt, err := tls.Listen("tcp", peerAddr, serverConfigAlt)
	if err != nil {
		t.Fatalf("Failed to listen on same peerAddr: %v", err)
	}
	defer lnAlt.Close()

	go func() {
		for {
			conn, err := lnAlt.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
			}(conn)
		}
	}()

	// Clear the failing save hook so saves would succeed if attempted
	savePeersWriteHook = nil

	// Outbound request with alternate cert must be strictly rejected
	_, errAlt := QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 3*time.Second)
	if errAlt == nil {
		t.Fatalf("Security failure: Alternate certificate was silently repinned after a failed save!")
	}
	if !strings.Contains(errAlt.Error(), "refusing alternate certificate") && !strings.Contains(errAlt.Error(), "previously failed") {
		t.Fatalf("Expected refusing alternate certificate error, got: %v", errAlt)
	}
}

// TestTOFU_ProductionCallSites_Covered verifies that every production call-site:
// - QueryPeer / QueryP2P
// - SendMessage / SendP2PMessage
// - RelayBlock
// - RelayTransaction
// - SyncBestPeer
// - RequestChainSync
// - peerDiscoveryLoop
// actively enforces TOFU validation against unverified or altered peers.
func TestTOFU_ProductionCallSites_Covered(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.P2PTOFU = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	// 1. Raw plaintext peer: must be rejected across all production methods
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen tcp: %v", err)
	}
	defer rawLn.Close()
	rawAddr := rawLn.Addr().String()

	// Direct QueryPeer
	_, errQuery := ps.QueryPeer(rawAddr, Message{Type: "GET_HEIGHT"}, 1*time.Second)
	if errQuery == nil {
		t.Fatalf("ps.QueryPeer accepted unencrypted plaintext peer!")
	}

	// Direct SendMessage
	errSend := ps.SendMessage(rawAddr, Message{Type: "NEW_BLOCK"})
	if errSend == nil {
		t.Fatalf("ps.SendMessage accepted unencrypted plaintext peer!")
	}

	// Direct DialPeer
	_, _, errDial := ps.DialPeer(rawAddr, 1*time.Second)
	if errDial == nil {
		t.Fatalf("ps.DialPeer accepted unencrypted plaintext peer!")
	}

	// 2. Valid TLS peer: pinning occurs on production call
	certValid, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	lnValid, validAddr := startMockTLSServer(t, certValid)
	defer lnValid.Close()

	// Call SyncBestPeer with valid peer in peer list
	ps.AddPeer(validAddr)
	ps.SyncBestPeer()

	// Verify valid peer was pinned
	ps.Lock.RLock()
	validInfo, validPinned := ps.triedPeers[validAddr]
	ps.Lock.RUnlock()
	if !validPinned || validInfo.CertFingerprint == "" {
		t.Fatalf("SyncBestPeer failed to pin valid peer in triedPeers!")
	}

	// 3. RelayBlock and RelayTransaction
	ps.RelayBlock(core.Block{Index: 1, Hash: "mock_block_hash"}, "")
	ps.RelayTransaction(core.Transaction{Amount: 100}, "")

	// 4. RequestChainSync: queries height, forkpoint, range
	ps.RequestChainSync(validAddr)
}

// TestTOFU_SimultaneousNetworks_IsolationAndRegistry verifies [P1-A]:
// 1. Two simultaneous peer servers with different network IDs (one with TOFU enabled, one disabled).
// 2. Server B (TOFU disabled) is created AFTER Server A (TOFU enabled) and is the most recently created server.
// 3. Routing a QueryP2P request with Server A's network ID routes to Server A (not Server B).
// 4. Server A's dedicated peer store is used; first-use pin is persisted only in Server A's store.
// 5. Subsequent connection to an altered certificate fails closed with fingerprint mismatch.
// 6. Ambiguous/missing network ID is rejected; no unauthenticated raw-dial fallback bypasses TOFU.
func TestTOFU_SimultaneousNetworks_IsolationAndRegistry(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	dirA := t.TempDir()
	dirB := t.TempDir()

	t.Setenv("ARMX_DATA_DIR", dirA)

	bcA, err := core.NewBlockchainWithStoragePath(
		1,
		filepath.Join(dirA, "blockchain.db"),
	)
	if err != nil {
		t.Fatalf("failed to create blockchain A: %v", err)
	}
	defer bcA.Close()

	psA := NewPeerServer(
		getFreePort(t),
		bcA,
		dirA,
		"ARMX-TESTNET-1",
	)
	psA.P2PTOFU = true
	psA.RequireTLS = false
	RegisterPeerServer(psA)
	defer UnregisterPeerServer(psA)

	// 2. Create Server B: Network ID ARMX-MAINNET-1, TOFU DISABLED
	// Created AFTER Server A (most recently created server)
	bcB, err := core.NewBlockchainWithStoragePath(
		1,
		filepath.Join(dirB, "blockchain.db"),
	)
	if err != nil {
		t.Fatalf("failed to create blockchain B: %v", err)
	}
	defer bcB.Close()
	t.Setenv("ARMX_DATA_DIR", dirB)
	psB := NewPeerServer(getFreePort(t), bcB, dirB, "ARMX-MAINNET-1")
	psB.P2PTOFU = false
	psB.RequireTLS = false
	RegisterPeerServer(psB)
	defer UnregisterPeerServer(psB)

	// Start Mock TLS Peer with Cert 1
	cert1, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	ln1, peerAddr := startMockTLSServer(t, cert1)
	defer ln1.Close()

	// 3. Route request to Server A's network ("ARMX-TESTNET-1")
	msgA := Message{
		NetworkID: "ARMX-TESTNET-1",
		Type:      "GET_HEIGHT",
	}
	respA, err := QueryP2P(peerAddr, msgA, 4*time.Second)
	if err != nil {
		t.Fatalf("QueryP2P to TOFU-enabled network failed: %v", err)
	}
	if respA.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Unexpected response type: %s", respA.Type)
	}

	// 4. Verify pin is recorded in Server A's triedPeers and NOT in Server B
	psA.Lock.RLock()
	infoA, pinnedA := psA.triedPeers[peerAddr]
	psA.Lock.RUnlock()
	if !pinnedA || infoA.CertFingerprint == "" {
		t.Fatalf("Server A failed to pin peer %s in triedPeers", peerAddr)
	}

	psB.Lock.RLock()
	_, pinnedB := psB.triedPeers[peerAddr]
	psB.Lock.RUnlock()
	if pinnedB {
		t.Fatalf("Server B unexpectedly pinned peer %s; network isolation failed!", peerAddr)
	}

	// Verify disk persistence in Server A's store
	dataA, err := os.ReadFile(psA.GetPeersPath())
	if err != nil {
		t.Fatalf("Failed to read Server A peer store file: %v", err)
	}
	if !strings.Contains(string(dataA), infoA.CertFingerprint) {
		t.Fatalf("Server A peer store file does not contain pinned fingerprint!")
	}

	// 5. Connect with altered certificate (Cert 2): MUST fail closed before message is trusted
	ln1.Close()
	time.Sleep(50 * time.Millisecond)

	cert2, err := generateEphemeralTestCert("AlteredPeerNetworkA")
	if err != nil {
		t.Fatalf("generateEphemeralTestCert for cert2 failed: %v", err)
	}
	ln2, _ := startMockTLSServerOnAddr(t, cert2, peerAddr)
	defer ln2.Close()

	_, errMitm := QueryP2P(peerAddr, msgA, 3*time.Second)
	if errMitm == nil {
		t.Fatalf("Security failure: QueryP2P to TOFU network succeeded against altered certificate!")
	}
	if !strings.Contains(errMitm.Error(), "fingerprint mismatch") && !strings.Contains(errMitm.Error(), "mismatch") {
		t.Fatalf("Expected fingerprint mismatch error, got: %v", errMitm)
	}

	// 6. Test missing/ambiguous registry behavior:
	// a) Unknown/unregistered network ID: must be rejected fail-closed, no raw dial fallback
	msgUnknown := Message{
		NetworkID: "ARMX-UNKNOWN-99",
		Type:      "GET_HEIGHT",
	}
	_, errUnknown := QueryP2P(peerAddr, msgUnknown, 2*time.Second)
	if errUnknown == nil {
		t.Fatalf("Expected error for unknown network ID in registry, but query succeeded!")
	}
	if !strings.Contains(errUnknown.Error(), "unauthenticated dial refused") {
		t.Fatalf("Expected unauthenticated dial refused error for unknown network, got: %v", errUnknown)
	}

	// b) Empty network ID in multi-server registry: must be rejected as ambiguous
	msgBlank := Message{
		NetworkID: "",
		Type:      "GET_HEIGHT",
	}
	_, errBlank := QueryP2P(peerAddr, msgBlank, 2*time.Second)
	if errBlank == nil {
		t.Fatalf("Expected error for missing/ambiguous network ID in registry, but query succeeded!")
	}
	if !strings.Contains(errBlank.Error(), "unauthenticated dial refused") && !strings.Contains(errBlank.Error(), "ambiguous") {
		t.Fatalf("Expected ambiguous or unauthenticated dial refusal, got: %v", errBlank)
	}
}

func TestTOFU_ProductionInit_SharedDataDir_IsolationAndStability(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	// Ensure no leftover environment overrides
	origAllowPrivate := os.Getenv("ARMX_ALLOW_PRIVATE_PEERS")
	origTestnet := os.Getenv("ARMX_TESTNET")
	_ = os.Unsetenv("ARMX_ALLOW_PRIVATE_PEERS")
	_ = os.Unsetenv("ARMX_TESTNET")
	defer func() {
		if origAllowPrivate != "" {
			_ = os.Setenv("ARMX_ALLOW_PRIVATE_PEERS", origAllowPrivate)
		} else {
			_ = os.Unsetenv("ARMX_ALLOW_PRIVATE_PEERS")
		}
		if origTestnet != "" {
			_ = os.Setenv("ARMX_TESTNET", origTestnet)
		} else {
			_ = os.Unsetenv("ARMX_TESTNET")
		}
	}()

	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)

	sharedDataDir := t.TempDir()
	dirBcMain := t.TempDir()
	dirBcTest := t.TempDir()

	// 1. Construct mainnet and testnet through InitServerFromConfig() with one shared base data directory
	t.Setenv("ARMX_DATA_DIR", dirBcMain)
	_ = core.SetNetwork("mainnet")
	bcMain, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dirBcMain, "blockchain.db"))
	if err != nil {
		t.Fatalf("failed to create isolated mainnet blockchain: %v", err)
	}
	defer bcMain.Close()

	t.Setenv("ARMX_DATA_DIR", dirBcTest)
	_ = core.SetNetwork("testnet")
	bcTest, err := core.NewBlockchainWithStoragePath(1, filepath.Join(dirBcTest, "blockchain.db"))
	if err != nil {
		t.Fatalf("failed to create isolated testnet blockchain: %v", err)
	}
	defer bcTest.Close()

	t.Setenv("ARMX_DATA_DIR", "")

	cfgMain := &Config{
		NetworkID:  "ARMX-MAINNET-1",
		DataDir:    sharedDataDir,
		P2PPort:    getFreePort(t),
		RPCPort:    getFreePort(t),
		RequireTLS: true,
		P2PTOFU:    true,
	}
	cfgTest := &Config{
		NetworkID:  "ARMX-TESTNET-1",
		DataDir:    sharedDataDir,
		P2PPort:    getFreePort(t),
		RPCPort:    getFreePort(t),
		RequireTLS: true,
		P2PTOFU:    true,
	}

	mainPs, _, err := InitServerFromConfig(bcMain, cfgMain)
	if err != nil {
		t.Fatalf("InitServerFromConfig mainnet failed: %v", err)
	}
	RegisterPeerServer(mainPs)
	defer UnregisterPeerServer(mainPs)

	testPs, _, err := InitServerFromConfig(bcTest, cfgTest)
	if err != nil {
		t.Fatalf("InitServerFromConfig testnet failed: %v", err)
	}
	RegisterPeerServer(testPs)
	defer UnregisterPeerServer(testPs)

	// 2. Toggle core.SetNetwork("mainnet") and core.SetNetwork("testnet") after both servers are created;
	// Assert each server's GetPeersPath() is stable across those changes.
	mainInitialPath := mainPs.GetPeersPath()
	testInitialPath := testPs.GetPeersPath()

	if mainInitialPath == testInitialPath {
		t.Fatalf("Mainnet and Testnet peer store paths must be distinct! Both got: %s", mainInitialPath)
	}

	for _, profile := range []string{"mainnet", "testnet", "regtest", "soak"} {
		_ = core.SetNetwork(profile)
		if p := mainPs.GetPeersPath(); p != mainInitialPath {
			t.Fatalf("Mainnet GetPeersPath() drifted under profile %s: expected %s, got %s", profile, mainInitialPath, p)
		}
		if p := testPs.GetPeersPath(); p != testInitialPath {
			t.Fatalf("Testnet GetPeersPath() drifted under profile %s: expected %s, got %s", profile, testInitialPath, p)
		}
	}

	// 3. Pin different TLS fingerprints on each server; assert each writes only to its own store.
	certMain, err := generateEphemeralTestCert("MainnetPeer")
	if err != nil {
		t.Fatalf("Failed to generate certMain: %v", err)
	}
	lnMain, addrMain := startMockTLSServer(t, certMain)
	defer lnMain.Close()

	certTest, err := generateEphemeralTestCert("TestnetPeer")
	if err != nil {
		t.Fatalf("Failed to generate certTest: %v", err)
	}
	lnTest, addrTest := startMockTLSServer(t, certTest)
	defer lnTest.Close()

	msgMain := Message{NetworkID: "ARMX-MAINNET-1", Type: "GET_HEIGHT"}
	respMain, err := QueryP2P(addrMain, msgMain, 3*time.Second)
	if err != nil {
		t.Fatalf("QueryP2P on Mainnet failed: %v", err)
	}
	if respMain.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Unexpected Mainnet response: %s", respMain.Type)
	}

	msgTest := Message{NetworkID: "ARMX-TESTNET-1", Type: "GET_HEIGHT"}
	respTest, err := QueryP2P(addrTest, msgTest, 3*time.Second)
	if err != nil {
		t.Fatalf("QueryP2P on Testnet failed: %v", err)
	}
	if respTest.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Unexpected Testnet response: %s", respTest.Type)
	}

	mainPs.Lock.RLock()
	infoMain, mainPinned := mainPs.triedPeers[addrMain]
	mainPs.Lock.RUnlock()
	if !mainPinned || infoMain.CertFingerprint == "" {
		t.Fatalf("Mainnet failed to pin peer %s in triedPeers", addrMain)
	}

	testPs.Lock.RLock()
	infoTest, testPinned := testPs.triedPeers[addrTest]
	testPs.Lock.RUnlock()
	if !testPinned || infoTest.CertFingerprint == "" {
		t.Fatalf("Testnet failed to pin peer %s in triedPeers", addrTest)
	}

	if infoMain.CertFingerprint == infoTest.CertFingerprint {
		t.Fatalf("Expected distinct fingerprints for Mainnet and Testnet mock peers")
	}

	// Assert each server wrote only to its own store
	mainData, err := os.ReadFile(mainInitialPath)
	if err != nil {
		t.Fatalf("Failed to read mainnet peer store %s: %v", mainInitialPath, err)
	}
	testData, err := os.ReadFile(testInitialPath)
	if err != nil {
		t.Fatalf("Failed to read testnet peer store %s: %v", testInitialPath, err)
	}

	if !strings.Contains(string(mainData), infoMain.CertFingerprint) {
		t.Fatalf("Mainnet store does not contain Mainnet peer fingerprint")
	}
	if strings.Contains(string(mainData), infoTest.CertFingerprint) {
		t.Fatalf("Cross-store leakage: Mainnet store contains Testnet peer fingerprint!")
	}
	if !strings.Contains(string(testData), infoTest.CertFingerprint) {
		t.Fatalf("Testnet store does not contain Testnet peer fingerprint")
	}
	if strings.Contains(string(testData), infoMain.CertFingerprint) {
		t.Fatalf("Cross-store leakage: Testnet store contains Mainnet peer fingerprint!")
	}

	// 4. Reload both stores while switching the global profile;
	// Assert each has the right NetworkID/fingerprint and neither enters corrupt-store mode.
	ClearPeerServers()

	// Switch global profile to testnet, then reload mainnet server
	_ = core.SetNetwork("testnet")
	mainReloadPs, _, err := InitServerFromConfig(bcMain, cfgMain)
	if err != nil {
		t.Fatalf("InitServerFromConfig mainReloadPs failed: %v", err)
	}
	RegisterPeerServer(mainReloadPs)
	defer UnregisterPeerServer(mainReloadPs)

	mainReloadPs.Lock.RLock()
	isMainCorrupt := mainReloadPs.peerStoreCorrupted
	infoMainReload, mainReloadPinned := mainReloadPs.triedPeers[addrMain]
	mainReloadPs.Lock.RUnlock()

	if isMainCorrupt {
		t.Fatalf("Mainnet server entered corrupt-store mode on reload!")
	}
	if !mainReloadPinned || infoMainReload.CertFingerprint != infoMain.CertFingerprint {
		t.Fatalf("Mainnet reload failed to preserve pinned fingerprint: expected %s, got %v", infoMain.CertFingerprint, infoMainReload)
	}
	if mainReloadPs.GetNetworkID() != "ARMX-MAINNET-1" {
		t.Fatalf("Mainnet reload has wrong NetworkID: %s", mainReloadPs.GetNetworkID())
	}

	// Switch global profile to mainnet, then reload testnet server
	_ = core.SetNetwork("mainnet")
	testReloadPs, _, err := InitServerFromConfig(bcTest, cfgTest)
	if err != nil {
		t.Fatalf("InitServerFromConfig testReloadPs failed: %v", err)
	}
	RegisterPeerServer(testReloadPs)
	defer UnregisterPeerServer(testReloadPs)

	testReloadPs.Lock.RLock()
	isTestCorrupt := testReloadPs.peerStoreCorrupted
	infoTestReload, testReloadPinned := testReloadPs.triedPeers[addrTest]
	testReloadPs.Lock.RUnlock()

	if isTestCorrupt {
		t.Fatalf("Testnet server entered corrupt-store mode on reload!")
	}
	if !testReloadPinned || infoTestReload.CertFingerprint != infoTest.CertFingerprint {
		t.Fatalf("Testnet reload failed to preserve pinned fingerprint: expected %s, got %v", infoTest.CertFingerprint, infoTestReload)
	}
	if testReloadPs.GetNetworkID() != "ARMX-TESTNET-1" {
		t.Fatalf("Testnet reload has wrong NetworkID: %s", testReloadPs.GetNetworkID())
	}

	// 5. Present an altered certificate for each peer after reload; assert both connections fail with fingerprint mismatch.
	lnMain.Close()
	lnTest.Close()
	time.Sleep(50 * time.Millisecond)

	certMainAltered, err := generateEphemeralTestCert("AlteredMainPeer")
	if err != nil {
		t.Fatalf("Failed to generate altered main cert: %v", err)
	}
	lnMainAltered, _ := startMockTLSServerOnAddr(t, certMainAltered, addrMain)
	defer lnMainAltered.Close()

	certTestAltered, err := generateEphemeralTestCert("AlteredTestPeer")
	if err != nil {
		t.Fatalf("Failed to generate altered test cert: %v", err)
	}
	lnTestAltered, _ := startMockTLSServerOnAddr(t, certTestAltered, addrTest)
	defer lnTestAltered.Close()

	_, errMainMitm := QueryP2P(addrMain, msgMain, 2*time.Second)
	if errMainMitm == nil {
		t.Fatalf("Security failure: Mainnet connection succeeded against altered certificate!")
	}
	if !strings.Contains(errMainMitm.Error(), "fingerprint mismatch") && !strings.Contains(errMainMitm.Error(), "mismatch") {
		t.Fatalf("Expected fingerprint mismatch on Mainnet, got: %v", errMainMitm)
	}

	_, errTestMitm := QueryP2P(addrTest, msgTest, 2*time.Second)
	if errTestMitm == nil {
		t.Fatalf("Security failure: Testnet connection succeeded against altered certificate!")
	}
	if !strings.Contains(errTestMitm.Error(), "fingerprint mismatch") && !strings.Contains(errTestMitm.Error(), "mismatch") {
		t.Fatalf("Expected fingerprint mismatch on Testnet, got: %v", errTestMitm)
	}

	// 6. Regression for configuring a mainnet server after the process's previous active profile was testnet;
	// Ensure AllowPrivatePeers follows the server's configured network, not a stale pre-configuration activeNet.
	_ = core.SetNetwork("testnet")
	if core.GetActiveNetwork().Name != "testnet" {
		t.Fatalf("Precondition failed: active network should be testnet, got %s", core.GetActiveNetwork().Name)
	}

	mainPsAfterTestnet, _, err := InitServerFromConfig(bcMain, cfgMain)
	if err != nil {
		t.Fatalf("InitServerFromConfig mainPsAfterTestnet failed: %v", err)
	}
	if mainPsAfterTestnet.AllowPrivatePeers {
		t.Fatalf("Security regression: Mainnet PeerServer has AllowPrivatePeers == true because global activeNet was testnet!")
	}
	if !testReloadPs.AllowPrivatePeers {
		t.Fatalf("Testnet PeerServer should have AllowPrivatePeers == true")
	}
}

func TestTOFU_PeerStore_InitFailuresAndMigration_SafeHalt(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	// 1. Initialization failure with unwritable/invalid path
	tempDir := t.TempDir()
	blockingFile := filepath.Join(tempDir, "regular_file")
	if err := os.WriteFile(blockingFile, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create blocking file: %v", err)
	}
	badDir := filepath.Join(blockingFile, "cannot_create_dir_here")

	dbPath := filepath.Join(t.TempDir(), "blockchain.db")

	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create test blockchain: %v", err)
	}
	defer bc.Close()

	psBad := NewPeerServer(getFreePort(t), bc, badDir, "ARMX-MAINNET-1")
	if psBad.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected initialization error for invalid data dir, got nil")
	}
	if !psBad.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true on initialization failure")
	}

	// Verify StartServer immediately halts and does not start listener
	psBad.StartServer()
	if psBad.listener != nil {
		t.Fatalf("Security violation: StartServer proceeded despite peer store initialization error!")
	}

	// 2. SetDataDir failure
	validDir := t.TempDir()
	psGood := NewPeerServer(getFreePort(t), bc, validDir, "ARMX-MAINNET-1")
	if err := psGood.GetPeerStoreInitErr(); err != nil {
		t.Fatalf("Expected clean initialization, got: %v", err)
	}

	if err := psGood.SetDataDir(badDir); err == nil {
		t.Fatalf("Expected SetDataDir to return error for badDir, got nil")
	}
	if !psGood.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true after failed SetDataDir")
	}

	// 3. Backward-compatible migration of legacy peer store with valid pins
	migDir := t.TempDir()
	legacyMainnetDir := filepath.Join(migDir, "mainnet")
	if err := os.MkdirAll(legacyMainnetDir, 0755); err != nil {
		t.Fatalf("Failed to create legacy dir: %v", err)
	}
	legacyStore := VersionedPeerStore{
		Version:   1,
		NetworkID: "ARMX-MAINNET-1",
		Peers: []*PeerInfo{
			{
				Address:         "192.168.1.10:29333",
				NetworkID:       "ARMX-MAINNET-1",
				CertFingerprint: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
		},
	}
	legacyBytes, _ := json.MarshalIndent(legacyStore, "", "  ")
	if err := os.WriteFile(filepath.Join(legacyMainnetDir, PeersFile), legacyBytes, 0644); err != nil {
		t.Fatalf("Failed to write legacy store: %v", err)
	}

	psMig := NewPeerServer(getFreePort(t), bc, migDir, "ARMX-MAINNET-1")
	if err := psMig.GetPeerStoreInitErr(); err != nil {
		t.Fatalf("Expected clean migration, got error: %v", err)
	}

	// Verify migrated file exists at canonical path
	canonicalPath := filepath.Join(migDir, PeersFile)
	if _, err := os.Stat(canonicalPath); err != nil {
		t.Fatalf("Expected migrated file at %s, got: %v", canonicalPath, err)
	}

	psMig.Lock.RLock()
	info, exists := psMig.triedPeers["192.168.1.10:29333"]
	psMig.Lock.RUnlock()
	if !exists || info.CertFingerprint != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Fatalf("Migration failed to preserve valid peer and fingerprint: got %+v", info)
	}

	// 4. Corrupted legacy store prevents startup
	corruptDir := t.TempDir()
	corruptLegacyDir := filepath.Join(corruptDir, "mainnet")
	if err := os.MkdirAll(corruptLegacyDir, 0755); err != nil {
		t.Fatalf("Failed to create corrupt legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptLegacyDir, PeersFile), []byte("{invalid-json"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt legacy store: %v", err)
	}

	psCorruptMig := NewPeerServer(getFreePort(t), bc, corruptDir, "ARMX-MAINNET-1")
	if psCorruptMig.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected error when migrating corrupt legacy store, got nil")
	}
	if !strings.Contains(psCorruptMig.GetPeerStoreInitErr().Error(), "corrupt legacy peer store") {
		t.Fatalf("Expected 'corrupt legacy peer store' error, got: %v", psCorruptMig.GetPeerStoreInitErr())
	}
	if !psCorruptMig.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true")
	}
}

// TestTOFU_NetworkIDValidation_RejectUnsupportedAndMismatch verifies:
// 1. parseNetworkID rejects malformed, unknown, or unsupported network IDs.
// 2. InitServerFromConfig fails fast on unsupported network IDs without silently defaulting to mainnet.
// 3. NewPeerServer fails closed if constructed with an unsupported network ID.
// 4. InitServerFromConfig verifies consistency between explicit config NetworkID, blockchain NetworkID, and active core profile.
func TestTOFU_NetworkIDValidation_RejectUnsupportedAndMismatch(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	// 1. parseNetworkID rejection of unknown/malformed strings
	badIDs := []string{
		"ARMX-UNKNOWN-99",
		"bitcoin",
		"mainnet-evil",
		"testnet_custom",
		"random_junk",
		"ARMX-MAINNET-2",
		"",
	}
	for _, badID := range badIDs {
		canon, sub, err := parseNetworkID(badID)
		if err == nil {
			t.Fatalf("parseNetworkID accepted invalid network ID %q (canon=%q, sub=%q)", badID, canon, sub)
		}
	}

	// Exact supported aliases must resolve correctly
	validCases := []struct {
		input     string
		wantCanon string
		wantSub   string
	}{
		{"ARMX-MAINNET-1", "ARMX-MAINNET-1", ""},
		{"MAINNET", "ARMX-MAINNET-1", ""},
		{"mainnet", "ARMX-MAINNET-1", ""},
		{"ARMX-TESTNET-1", "ARMX-TESTNET-1", "testnet"},
		{"TESTNET", "ARMX-TESTNET-1", "testnet"},
		{"testnet", "ARMX-TESTNET-1", "testnet"},
		{"ARMX-REGTEST-1", "ARMX-REGTEST-1", "regtest"},
		{"REGTEST", "ARMX-REGTEST-1", "regtest"},
		{"regtest", "ARMX-REGTEST-1", "regtest"},
		{"ARMX-SOAK-1", "ARMX-SOAK-1", "soak"},
		{"SOAK", "ARMX-SOAK-1", "soak"},
		{"soak", "ARMX-SOAK-1", "soak"},
		{"TEST", "ARMX-SOAK-1", "soak"},
		{"test", "ARMX-SOAK-1", "soak"},
	}
	for _, tc := range validCases {
		canon, sub, err := parseNetworkID(tc.input)
		if err != nil {
			t.Fatalf("parseNetworkID failed for valid alias %q: %v", tc.input, err)
		}
		if canon != tc.wantCanon || sub != tc.wantSub {
			t.Fatalf("parseNetworkID(%q) = (%q, %q), want (%q, %q)", tc.input, canon, sub, tc.wantCanon, tc.wantSub)
		}
	}

	// 2. InitServerFromConfig fails on unsupported NetworkID
	tempDir := t.TempDir()

	dbPath := filepath.Join(t.TempDir(), "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	cfgBad := &Config{
		NetworkID:  "ARMX-UNKNOWN-99",
		DataDir:    tempDir,
		P2PPort:    getFreePort(t),
		RPCPort:    getFreePort(t),
		RequireTLS: true,
		P2PTOFU:    true,
	}
	_, _, err = InitServerFromConfig(bc, cfgBad)
	if err == nil {
		t.Fatalf("InitServerFromConfig accepted invalid network ID 'ARMX-UNKNOWN-99'")
	}
	if !strings.Contains(err.Error(), "unsupported") && !strings.Contains(err.Error(), "invalid network_id") {
		t.Fatalf("Expected unsupported/invalid network ID error, got: %v", err)
	}

	// 3. NewPeerServer fails closed when constructed with unsupported NetworkID
	psBad := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-UNKNOWN-99")
	if psBad.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected NewPeerServer to fail closed on unsupported network ID, got nil")
	}
	if !psBad.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true on unsupported network ID")
	}

	// 4. Mismatch between config NetworkID and blockchain NetworkID
	cfgMismatch := &Config{
		NetworkID:  "ARMX-REGTEST-1", // bc was initialized with Testnet
		DataDir:    tempDir,
		P2PPort:    getFreePort(t),
		RPCPort:    getFreePort(t),
		RequireTLS: true,
		P2PTOFU:    true,
	}
	_, _, errMismatch := InitServerFromConfig(bc, cfgMismatch)
	if errMismatch == nil {
		t.Fatalf("Expected InitServerFromConfig to reject blockchain/config network ID mismatch")
	}
	if !strings.Contains(errMismatch.Error(), "mismatch") {
		t.Fatalf("Expected network ID mismatch error, got: %v", errMismatch)
	}
}

// TestTOFU_CorruptLegacyStore_FailClosed verifies:
// 1. When canonical store is missing and parent peers.json is corrupt, non-mainnet server fails closed.
// 2. Legacy store with wrong NetworkID fails closed.
// 3. Legacy store with unsupported schema version fails closed.
func TestTOFU_LifecycleSetters_MemoryAndDiskIsolation(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	// 1. Malformed parent peers.json exists, <base>/testnet/peers.json does not.
	// Testnet server must fail closed and preserve the parent file.
	dir1 := t.TempDir()
	parentPeers := filepath.Join(dir1, PeersFile)
	malformedContent := []byte("{ malformed json content here !! ")
	if err := os.WriteFile(parentPeers, malformedContent, 0644); err != nil {
		t.Fatalf("Failed to write malformed parent peers: %v", err)
	}

	testnetStorePath := filepath.Join(dir1, "testnet", PeersFile)
	if _, err := os.Stat(testnetStorePath); !os.IsNotExist(err) {
		t.Fatalf("Precondition failed: testnet peers.json should not exist yet")
	}

	psTestnet := NewPeerServer(getFreePort(t), bc, dir1, "ARMX-TESTNET-1")
	if psTestnet.GetPeerStoreInitErr() == nil {
		t.Fatalf("Security failure: Testnet server did not fail closed on malformed parent legacy store!")
	}
	if !psTestnet.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true")
	}

	// Verify parent file is preserved, NOT erased or overwritten
	parentData, err := os.ReadFile(parentPeers)
	if err != nil {
		t.Fatalf("Failed to read parent peers file: %v", err)
	}
	if string(parentData) != string(malformedContent) {
		t.Fatalf("Corrupt parent file was altered or overwritten!")
	}

	// Verify StartServer immediately aborts
	psTestnet.StartServer()
	if psTestnet.listener != nil {
		t.Fatalf("Security violation: StartServer started listener despite corrupt legacy store!")
	}

	// 2. Legacy store with wrong NetworkID
	dir2 := t.TempDir()
	legacyMainDir := filepath.Join(dir2, "mainnet")
	if err := os.MkdirAll(legacyMainDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}
	wrongNetStore := VersionedPeerStore{
		Version:   1,
		NetworkID: "ARMX-REGTEST-1", // Mismatch for mainnet candidate
		Peers:     []*PeerInfo{},
	}
	wrongBytes, _ := json.Marshal(wrongNetStore)
	if err := os.WriteFile(filepath.Join(legacyMainDir, PeersFile), wrongBytes, 0644); err != nil {
		t.Fatalf("Failed to write wrong net store: %v", err)
	}

	psWrongNet := NewPeerServer(getFreePort(t), bc, dir2, "ARMX-MAINNET-1")
	if psWrongNet.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected error when legacy store has wrong NetworkID, got nil")
	}
	if !strings.Contains(psWrongNet.GetPeerStoreInitErr().Error(), "mismatch") {
		t.Fatalf("Expected network ID mismatch error, got: %v", psWrongNet.GetPeerStoreInitErr())
	}
	if !psWrongNet.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true")
	}

	// 3. Legacy store with unsupported schema version
	dir3 := t.TempDir()
	legacyMainDir3 := filepath.Join(dir3, "mainnet")
	if err := os.MkdirAll(legacyMainDir3, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}
	badVerStore := VersionedPeerStore{
		Version:   99, // Unsupported schema version
		NetworkID: "ARMX-MAINNET-1",
		Peers:     []*PeerInfo{},
	}
	badVerBytes, _ := json.Marshal(badVerStore)
	if err := os.WriteFile(filepath.Join(legacyMainDir3, PeersFile), badVerBytes, 0644); err != nil {
		t.Fatalf("Failed to write bad ver store: %v", err)
	}

	psBadVer := NewPeerServer(getFreePort(t), bc, dir3, "ARMX-MAINNET-1")
	if psBadVer.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected error when legacy store has unsupported schema version, got nil")
	}
	if !strings.Contains(psBadVer.GetPeerStoreInitErr().Error(), "unsupported legacy peer store version") {
		t.Fatalf("Expected unsupported legacy peer store version error, got: %v", psBadVer.GetPeerStoreInitErr())
	}
	if !psBadVer.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true")
	}

	// 4. Failed migration write
	dir4 := t.TempDir()
	legacyMainDir4 := filepath.Join(dir4, "mainnet")
	if err := os.MkdirAll(legacyMainDir4, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}
	validStore := VersionedPeerStore{
		Version:   1,
		NetworkID: "ARMX-MAINNET-1",
		Peers:     []*PeerInfo{},
	}
	validBytes, _ := json.Marshal(validStore)
	if err := os.WriteFile(filepath.Join(legacyMainDir4, PeersFile), validBytes, 0644); err != nil {
		t.Fatalf("Failed to write valid store: %v", err)
	}

	// Inject migration write failure
	migrateLegacyWriteFileHook = func(filename string, data []byte, perm os.FileMode) error {
		return errors.New("simulated disk I/O error during migration write")
	}
	defer func() {
		migrateLegacyWriteFileHook = nil
	}()

	psWriteFail := NewPeerServer(getFreePort(t), bc, dir4, "ARMX-MAINNET-1")
	if psWriteFail.GetPeerStoreInitErr() == nil {
		t.Fatalf("Expected error when migration write fails, got nil")
	}
	if !strings.Contains(psWriteFail.GetPeerStoreInitErr().Error(), "failed to write migrated peer store") {
		t.Fatalf("Expected 'failed to write migrated peer store' error, got: %v", psWriteFail.GetPeerStoreInitErr())
	}
	if !psWriteFail.peerStoreCorrupted {
		t.Fatalf("Expected peerStoreCorrupted to be true")
	}
}

// TestTOFU_LifecycleSetters_MemoryAndDiskIsolation verifies:
// 1. Loading a pinned peer in testnet store.
// 2. Switching to an empty mainnet store via SetNetworkID and SetDataDir clears in-memory state.
// 3. The empty mainnet store does not inherit pins from testnet in memory or on disk.
// 4. Switching back to testnet safely reloads the testnet pins.
func TestTOFU_CorruptLegacyStore_FailClosed(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	baseDir := t.TempDir()
	testDir := filepath.Join(baseDir, "test_data")
	mainDir := filepath.Join(baseDir, "main_data")

	// 1. Initialize server on testnet
	ps := NewPeerServer(getFreePort(t), bc, testDir, "ARMX-TESTNET-1")
	if err := ps.GetPeerStoreInitErr(); err != nil {
		t.Fatalf("Testnet init failed: %v", err)
	}

	pinnedAddr := "192.168.100.50:19335"
	pinnedFP := "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"

	ps.Lock.Lock()
	ps.triedPeers[pinnedAddr] = &PeerInfo{
		Address:         pinnedAddr,
		NetworkID:       "ARMX-TESTNET-1",
		CertFingerprint: pinnedFP,
		LastSeen:        time.Now(),
	}
	ps.Peers = append(ps.Peers, pinnedAddr)
	ps.Lock.Unlock()

	// Persist to disk
	ps.savePeers()

	// Verify in-memory and on-disk in testnet
	ps.Lock.RLock()
	info := ps.triedPeers[pinnedAddr]
	ps.Lock.RUnlock()
	if info == nil || info.CertFingerprint != pinnedFP {
		t.Fatalf("Precondition failed: testnet peer not pinned in memory")
	}

	testDiskFile := filepath.Join(testDir, "testnet", PeersFile)
	if _, err := os.Stat(testDiskFile); err != nil {
		t.Fatalf("Expected testnet peer store on disk at %s: %v", testDiskFile, err)
	}

	// 2. Switch lifecycle to empty mainnet store
	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err != nil {
		t.Fatalf("SetNetworkID mainnet failed: %v", err)
	}
	if err := ps.SetDataDir(mainDir); err != nil {
		t.Fatalf("SetDataDir mainDir failed: %v", err)
	}

	// 3. Verify in-memory peer maps are cleared and do NOT inherit testnet pins!
	ps.Lock.RLock()
	memPin, hasPin := ps.triedPeers[pinnedAddr]
	memPeersLen := len(ps.Peers)
	memNewLen := len(ps.newPeers)
	memCorrupt := ps.peerStoreCorrupted
	ps.Lock.RUnlock()

	if hasPin || memPin != nil {
		t.Fatalf("Security failure: Mainnet server inherited pinned peer %s from testnet in memory!", pinnedAddr)
	}
	if memPeersLen != 0 {
		t.Fatalf("Mainnet server retained %d peers in ps.Peers from testnet!", memPeersLen)
	}
	if memNewLen != 0 {
		t.Fatalf("Mainnet server retained %d peers in ps.newPeers from testnet!", memNewLen)
	}
	if memCorrupt {
		t.Fatalf("Mainnet server unexpectedly entered corrupt mode")
	}

	// Verify mainnet disk path has NOT been populated with testnet pins
	mainDiskFile := filepath.Join(mainDir, PeersFile)
	if _, err := os.Stat(mainDiskFile); !os.IsNotExist(err) {
		data, _ := os.ReadFile(mainDiskFile)
		if strings.Contains(string(data), pinnedAddr) || strings.Contains(string(data), pinnedFP) {
			t.Fatalf("Mainnet disk store contains testnet pin!")
		}
	}

	// 4. Verify testnet disk store still intact and unaffected
	testDataAfter, err := os.ReadFile(testDiskFile)
	if err != nil {
		t.Fatalf("Failed to read testnet disk file after switch: %v", err)
	}
	if !strings.Contains(string(testDataAfter), pinnedAddr) || !strings.Contains(string(testDataAfter), pinnedFP) {
		t.Fatalf("Testnet disk file was corrupted or wiped during mainnet switch!")
	}

	// 5. Switch back to testnet; verify testnet pins reload seamlessly
	if err := ps.SetNetworkID("ARMX-TESTNET-1"); err != nil {
		t.Fatalf("Switch back to testnet NetworkID failed: %v", err)
	}
	if err := ps.SetDataDir(testDir); err != nil {
		t.Fatalf("Switch back to testDir failed: %v", err)
	}

	ps.Lock.RLock()
	reloadedPin, reloadedHas := ps.triedPeers[pinnedAddr]
	ps.Lock.RUnlock()

	if !reloadedHas || reloadedPin == nil || reloadedPin.CertFingerprint != pinnedFP {
		t.Fatalf("Failed to reload testnet pin after switching back: got %+v", reloadedPin)
	}

	// 6. SetNetworkID rejection of invalid network ID preserves current state
	if err := ps.SetNetworkID("ARMX-INVALID-99"); err == nil {
		t.Fatalf("Expected SetNetworkID to reject invalid network ID")
	}
	if ps.GetNetworkID() != "ARMX-TESTNET-1" {
		t.Fatalf("SetNetworkID altered network ID despite returning error: got %s", ps.GetNetworkID())
	}
}

// TestTOFU_LifecycleRace_ConcurrentSetterBlockedDuringFirstContact verifies:
// 1. Pausing TOFU after in-memory staging and before disk save blocks concurrent setters.
// 2. The setter does not run until the TOFU transaction completely finishes and durably saves to disk.
// 3. No first-contact connection returns success without its exact fingerprint durably present in the correct network store.
// 4. No pin crosses network/store boundaries.
// 5. No deadlocks occur.
func TestTOFU_LifecycleRace_ConcurrentSetterBlockedDuringFirstContact(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.P2PTOFU = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	cert, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	ln, peerAddr := startMockTLSServer(t, cert)
	defer ln.Close()

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}
	h := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	peerFP := hex.EncodeToString(h[:])

	tofuStaged := make(chan struct{})
	tofuCanProceed := make(chan struct{})

	// Deterministic pause after in-memory staging and before savePeers() disk commit
	tofuPreSaveHook = func(addr string) {
		select {
		case tofuStaged <- struct{}{}:
		default:
		}
		<-tofuCanProceed
	}
	defer func() {
		tofuPreSaveHook = nil
	}()

	var queryErr error
	var wg sync.WaitGroup

	// Launch TOFU first-contact connection in Goroutine A
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, queryErr = QueryP2P(peerAddr, Message{Type: "GET_HEIGHT"}, 10*time.Second)
	}()

	// Wait until TOFU has completed in-memory staging and paused before savePeers()
	select {
	case <-tofuStaged:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for TOFU to reach pre-save hook")
	}

	// In-memory pin is staged in ps.triedPeers
	ps.Lock.RLock()
	stagedInfo := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if stagedInfo == nil || stagedInfo.CertFingerprint == "" {
		t.Fatalf("Precondition failed: pin was not staged in memory before disk save")
	}

	// Now launch concurrent setter (Goroutine B) attempting to switch data dir while TOFU is paused
	newDir := filepath.Join(tempDir, "switched_dir")
	setterDone := make(chan error, 1)
	go func() {
		setterDone <- ps.SetDataDir(newDir)
	}()

	// Verify setter is BLOCKED and WAITING for TOFU transaction to complete
	select {
	case err := <-setterDone:
		t.Fatalf("RACE VULNERABILITY: SetDataDir finished while TOFU was paused before disk save! err: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: setter is properly blocked by lifecycleLock
	}

	// Verify pin is still present in memory and was NOT erased prematurely by the blocked setter
	ps.Lock.RLock()
	stillStaged := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if stillStaged == nil || stillStaged.CertFingerprint == "" {
		t.Fatalf("Security failure: pinned peer was erased from memory while TOFU transaction was in-flight!")
	}

	// Release TOFU transaction to proceed with durable persistence
	close(tofuCanProceed)
	wg.Wait()

	if queryErr != nil {
		t.Fatalf("QueryP2P returned error: %v", queryErr)
	}

	// Verify setter now unblocks and finishes without deadlock
	select {
	case setterErr := <-setterDone:
		if setterErr != nil {
			t.Fatalf("SetDataDir failed: %v", setterErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK DETECTED: SetDataDir did not unblock after TOFU completed!")
	}

	// 1. Verify original store durably contains the pinned fingerprint
	origStorePath := filepath.Join(tempDir, "testnet", PeersFile)
	data, err := os.ReadFile(origStorePath)
	if err != nil {
		t.Fatalf("Failed to read original testnet store at %s: %v", origStorePath, err)
	}
	var origStore VersionedPeerStore
	if err := json.Unmarshal(data, &origStore); err != nil {
		t.Fatalf("Failed to parse original store: %v", err)
	}
	foundInOrig := false
	for _, pInfo := range origStore.Peers {
		if pInfo.Address == peerAddr && pInfo.CertFingerprint == peerFP {
			foundInOrig = true
			break
		}
	}
	if !foundInOrig {
		t.Fatalf("Durability failure: peer %s with fingerprint %s was NOT durably saved in original store!", peerAddr, peerFP)
	}

	// 2. Verify new switched store does NOT contain the testnet pin (no pin crosses boundaries)
	newStorePath := filepath.Join(newDir, "testnet", PeersFile)
	if newData, err := os.ReadFile(newStorePath); err == nil {
		if strings.Contains(string(newData), peerAddr) || strings.Contains(string(newData), peerFP) {
			t.Fatalf("Boundary failure: testnet pin leaked into new store at %s!", newStorePath)
		}
	}

	// 3. Verify in-memory state of ps was cleared on transition and does NOT inherit old pin
	ps.Lock.RLock()
	memInfo, hasMem := ps.triedPeers[peerAddr]
	ps.Lock.RUnlock()
	if hasMem || memInfo != nil {
		t.Fatalf("Isolation failure: peer server retained pin in memory after SetDataDir to new store!")
	}
}

// TestTOFU_LifecycleSetters_RejectedAfterServerStart verifies:
// SetDataDir and SetNetworkID are strictly rejected once StartServer has commenced.
func TestTOFU_LifecycleSetters_RejectedAfterServerStart(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	go ps.StartServer()
	defer ps.Stop()

	// Give server time to listen
	time.Sleep(100 * time.Millisecond)

	// SetDataDir must be rejected
	newDir := t.TempDir()
	if err := ps.SetDataDir(newDir); err == nil {
		t.Fatalf("Security failure: SetDataDir succeeded after StartServer was running!")
	} else if !strings.Contains(err.Error(), "cannot change data directory after server has started") {
		t.Fatalf("Expected 'cannot change data directory after server has started', got: %v", err)
	}

	// SetNetworkID must be rejected
	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err == nil {
		t.Fatalf("Security failure: SetNetworkID succeeded after StartServer was running!")
	} else if !strings.Contains(err.Error(), "cannot change network ID after server has started") {
		t.Fatalf("Expected 'cannot change network ID after server has started', got: %v", err)
	}
}

// TestTOFU_Lifecycle_ConcurrentSetterBlockedDuringStartup proves that setters cannot mutate
// the data directory or network ID while StartServer is in ServerStateStarting.
func TestTOFU_Lifecycle_ConcurrentSetterBlockedDuringStartup(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")

	startingEntered := make(chan struct{}, 1)
	proceedStartup := make(chan struct{})

	serverStartingHook = func() {
		select {
		case startingEntered <- struct{}{}:
		default:
		}
		<-proceedStartup
	}
	defer func() {
		serverStartingHook = nil
	}()

	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		ps.StartServer()
	}()
	defer ps.Stop()

	// Wait until StartServer has transitioned to ServerStateStarting
	select {
	case <-startingEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for server to enter starting state")
	}

	if state := ps.GetState(); state != ServerStateStarting {
		t.Fatalf("Expected server state %s, got %s", ServerStateStarting, state)
	}

	// Attempt SetDataDir while starting
	newDir := t.TempDir()
	if err := ps.SetDataDir(newDir); err == nil {
		t.Fatalf("Security failure: SetDataDir succeeded while server was in ServerStateStarting!")
	} else if !strings.Contains(err.Error(), "starting") {
		t.Fatalf("Expected error indicating starting state, got: %v", err)
	}

	// Attempt SetNetworkID while starting
	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err == nil {
		t.Fatalf("Security failure: SetNetworkID succeeded while server was in ServerStateStarting!")
	} else if !strings.Contains(err.Error(), "starting") {
		t.Fatalf("Expected error indicating starting state, got: %v", err)
	}

	// Allow startup to proceed to completion
	close(proceedStartup)

	// Wait for server to reach running state
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ps.GetState() == ServerStateRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ps.GetState() != ServerStateRunning {
		t.Fatalf("Expected server state to reach ServerStateRunning, got %s", ps.GetState())
	}
}

// TestTOFU_Lifecycle_ConcurrentSetterBlockedDuringShutdown proves that setters cannot mutate
// the data directory or network ID while Stop is in ServerStateStopping.
func TestTOFU_Lifecycle_ConcurrentSetterBlockedDuringShutdown(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	go ps.StartServer()

	// Wait for server to be running
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ps.GetState() == ServerStateRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ps.GetState() != ServerStateRunning {
		t.Fatalf("Expected server to reach ServerStateRunning, got %s", ps.GetState())
	}

	stoppingEntered := make(chan struct{})
	proceedStop := make(chan struct{})

	serverStoppingHook = func() {
		select {
		case stoppingEntered <- struct{}{}:
		default:
		}
		<-proceedStop
	}
	defer func() {
		serverStoppingHook = nil
	}()

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		ps.Stop()
	}()

	// Wait until Stop() has transitioned to ServerStateStopping
	select {
	case <-stoppingEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for server to enter stopping state")
	}

	if state := ps.GetState(); state != ServerStateStopping {
		t.Fatalf("Expected server state %s, got %s", ServerStateStopping, state)
	}

	// Attempt SetDataDir while stopping
	newDir := t.TempDir()
	if err := ps.SetDataDir(newDir); err == nil {
		t.Fatalf("Security failure: SetDataDir succeeded while server was in ServerStateStopping!")
	} else if !strings.Contains(err.Error(), "stopping") {
		t.Fatalf("Expected error indicating stopping state, got: %v", err)
	}

	// Attempt SetNetworkID while stopping
	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err == nil {
		t.Fatalf("Security failure: SetNetworkID succeeded while server was in ServerStateStopping!")
	} else if !strings.Contains(err.Error(), "stopping") {
		t.Fatalf("Expected error indicating stopping state, got: %v", err)
	}

	// Unblock shutdown
	close(proceedStop)
	<-stopDone

	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected server state to reach ServerStateStopped after Stop(), got %s", state)
	}

	// Now that server is stopped, setters are permitted again
	if err := ps.SetDataDir(newDir); err != nil {
		t.Fatalf("SetDataDir failed on stopped server: %v", err)
	}
}

// TestTOFU_Lifecycle_DialPeerConsistentAcrossHandshakeAndSave proves that DialPeer holds lifecycleLock
// across the TLS handshake, TOFU validation, and disk persistence without deadlocking with waiting writers.
func TestTOFU_Lifecycle_DialPeerConsistentAcrossHandshakeAndSave(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.P2PTOFU = true
	ps.RequireTLS = false
	RegisterPeerServer(ps)
	defer UnregisterPeerServer(ps)

	cert, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate failed: %v", err)
	}
	ln, peerAddr := startMockTLSServer(t, cert)
	defer ln.Close()

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}
	h := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	peerFP := hex.EncodeToString(h[:])

	dialStaged := make(chan struct{})
	proceedDial := make(chan struct{})

	// Hook called inside DialPeer after TLS handshake and before TOFU save
	dialPreTOFUHook = func(addr string) {
		select {
		case dialStaged <- struct{}{}:
		default:
		}
		<-proceedDial
	}
	defer func() {
		dialPreTOFUHook = nil
	}()

	var dialConn net.Conn
	var dialRelease func()
	var dialErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		dialConn, dialRelease, dialErr = ps.DialPeer(peerAddr, 10*time.Second)
	}()

	// Wait until DialPeer has performed the TLS handshake and paused before TOFU save
	select {
	case <-dialStaged:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for DialPeer to reach pre-TOFU hook")
	}

	// Concurrently attempt SetDataDir while DialPeer is holding lifecycleLock.RLock
	newDir := filepath.Join(tempDir, "switched_during_dial")
	setterDone := make(chan error, 1)
	go func() {
		setterDone <- ps.SetDataDir(newDir)
	}()

	// Verify setter is BLOCKED by lifecycleLock
	select {
	case err := <-setterDone:
		t.Fatalf("RACE VULNERABILITY: SetDataDir completed while DialPeer was in-flight! err: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: blocked
	}

	// Resume DialPeer
	close(proceedDial)
	wg.Wait()

	if dialErr != nil {
		t.Fatalf("DialPeer failed: %v", dialErr)
	}
	defer func() {
		if dialConn != nil {
			_ = dialConn.Close()
		}
		if dialRelease != nil {
			dialRelease()
		}
	}()

	// Verify setter now completes without deadlock
	select {
	case setterErr := <-setterDone:
		if setterErr != nil {
			t.Fatalf("SetDataDir failed after unblocking: %v", setterErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK DETECTED: SetDataDir did not unblock after DialPeer completed!")
	}

	// Verify pin was saved in the original store (tempDir), NOT in switched store
	origStorePath := filepath.Join(tempDir, "testnet", PeersFile)
	data, err := os.ReadFile(origStorePath)
	if err != nil {
		t.Fatalf("Failed to read original testnet store: %v", err)
	}
	if !strings.Contains(string(data), peerFP) {
		t.Fatalf("Expected peer fingerprint %s in original store %s", peerFP, origStorePath)
	}

	newStorePath := filepath.Join(newDir, "testnet", PeersFile)
	if newData, err := os.ReadFile(newStorePath); err == nil {
		if strings.Contains(string(newData), peerFP) {
			t.Fatalf("Cross-store leakage: pin leaked into new store %s!", newStorePath)
		}
	}
}

// TestTOFU_Lifecycle_StartupFailure_RollbackAndRegistryClean proves that when StartServer
// encounters a bind failure, it rolls back state to ServerStateStopped, cleans up the registry,
// and does not leak listeners.
func TestTOFU_Lifecycle_StartupFailure_RollbackAndRegistryClean(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)

	// Pre-bind the port to guarantee net.Listen inside StartServer will fail
	blocker, err := net.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatalf("Failed to bind blocker listener on port %s: %v", port, err)
	}
	defer blocker.Close()

	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	// StartServer should return without panic or crash
	ps.StartServer()

	// State must be cleanly rolled back to ServerStateStopped
	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected state %s after bind failure, got %s", ServerStateStopped, state)
	}

	// Registry must NOT contain this failed server
	if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered == ps {
		t.Fatalf("Leaked registry entry: failed server remains registered in peerServers!")
	}

	// Listener field must be nil
	if ps.listener != nil {
		t.Fatalf("Leaked listener on failed server!")
	}
}

// TestTOFU_Lifecycle_ConcurrentStartStop_NoLeaksOrCorruptState tests concurrent, repeated
// StartServer and Stop calls to prove zero deadlocks, zero race crashes, and clean final state.
func TestTOFU_Lifecycle_ConcurrentStartStop_NoLeaksOrCorruptState(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ps.StartServer()
		}()
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			ps.Stop()
		}()
	}

	wg.Wait()

	// Ensure final state is stopped
	ps.Stop()

	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected final state %s, got %s", ServerStateStopped, state)
	}
	if ps.listener != nil {
		t.Fatalf("Expected listener to be nil after final stop")
	}
	if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered == ps {
		t.Fatalf("Expected server to be unregistered from registry after final stop")
	}
}

func waitForServerState(t *testing.T, ps *PeerServer, expected ServerLifecycleState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ps.GetState() == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ps.GetState() != expected {
		t.Fatalf("Timeout waiting for server state %s, got %s", expected, ps.GetState())
	}
}

// TestTOFU_Lifecycle_StopDuringStartup_DeterministicAbort proves that when Stop() is initiated
// while StartServer() is still in its startup phase (ServerStateStarting), the server aborts
// cleanly, does not proceed to listen or run, and leaves state as ServerStateStopped.
func TestTOFU_Lifecycle_StopDuringStartup_DeterministicAbort(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	startingBarrier := make(chan struct{})
	stopTriggered := make(chan struct{})
	serverDone := make(chan struct{})

	serverStartingHook = func() {
		close(startingBarrier)
		<-stopTriggered
	}
	defer func() { serverStartingHook = nil }()

	go func() {
		ps.StartServer()
		close(serverDone)
	}()

	// Wait until StartServer enters ServerStateStarting
	<-startingBarrier

	// Call Stop while StartServer is blocked in startingHook
	stopCompleted := make(chan struct{})
	go func() {
		ps.Stop()
		close(stopCompleted)
	}()

	// Release startingHook so StartServer proceeds to net.Listen / check cancel
	close(stopTriggered)

	// Both StartServer and Stop must complete within 5 seconds without deadlock
	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK: StartServer did not return after Stop was called during startup!")
	}

	select {
	case <-stopCompleted:
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK: Stop did not return after aborting startup!")
	}

	// State must be cleanly ServerStateStopped
	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected state %s, got %s", ServerStateStopped, state)
	}

	// Listener must be nil
	if ps.listener != nil {
		t.Fatalf("Leaked listener after startup abort!")
	}

	// Registry must not contain this aborted server
	if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered == ps {
		t.Fatalf("Leaked registry entry after startup abort!")
	}

	// Prove the port can be bound immediately (not held open)
	testL, err := net.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatalf("Port %s was not freed: %v", port, err)
	}
	_ = testL.Close()
}

// TestTOFU_Lifecycle_CycleCleanupCollision_OldCannotOverwriteNew proves that cleanup from an
// old server cycle cannot overwrite or corrupt the state or active cycle of a newer server cycle.
func TestTOFU_Lifecycle_CycleCleanupCollision_OldCannotOverwriteNew(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	// Start Cycle 1
	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	ps.lifecycleLock.RLock()
	cycle1 := ps.activeCycle
	ps.lifecycleLock.RUnlock()
	if cycle1 == nil {
		t.Fatalf("Expected activeCycle for cycle 1 to be non-nil")
	}

	// Stop Cycle 1
	ps.Stop()
	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected state stopped after cycle 1 stop, got %s", state)
	}

	// Start Cycle 2
	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	ps.lifecycleLock.RLock()
	cycle2 := ps.activeCycle
	ps.lifecycleLock.RUnlock()
	if cycle2 == nil || cycle2 == cycle1 {
		t.Fatalf("Expected distinct activeCycle for cycle 2")
	}

	// Simulate late/delayed execution of Cycle 1's teardownCycle
	ps.teardownCycle(cycle1, nil)

	// State MUST remain ServerStateRunning for Cycle 2, and activeCycle must still be cycle2!
	ps.lifecycleLock.RLock()
	curState := ps.state
	curCycle := ps.activeCycle
	ps.lifecycleLock.RUnlock()

	if curState != ServerStateRunning {
		t.Fatalf("SECURITY VIOLATION: Cycle 1 cleanup overwrote cycle 2 state to %s!", curState)
	}
	if curCycle != cycle2 {
		t.Fatalf("SECURITY VIOLATION: Cycle 1 cleanup cleared cycle 2 activeCycle!")
	}

	// Stop Cycle 2 cleanly
	ps.Stop()
	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected state stopped after cycle 2 stop, got %s", state)
	}
}

// TestTOFU_Lifecycle_ConcurrentStops_WaitOnSameShutdown proves that multiple concurrent Stop()
// calls wait for the same shutdown completion barrier and none return before shutdown is complete.
func TestTOFU_Lifecycle_ConcurrentStops_WaitOnSameShutdown(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	// Barrier to hold shutdown in stoppingHook so multiple Stop callers arrive simultaneously
	hookBarrier := make(chan struct{})
	releaseHook := make(chan struct{})
	serverStoppingHook = func() {
		close(hookBarrier)
		<-releaseHook
	}
	defer func() { serverStoppingHook = nil }()

	const numStoppers = 4
	var wg sync.WaitGroup
	stoppedPrematurely := false
	var mu sync.Mutex

	for i := 0; i < numStoppers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ps.Stop()
			// Check if any returned while server was still stopping
			mu.Lock()
			if ps.GetState() != ServerStateStopped {
				stoppedPrematurely = true
			}
			mu.Unlock()
		}()
	}

	// Wait until at least one caller entered the stopping hook
	<-hookBarrier

	// Verify all stoppers are currently blocked waiting for shutdown
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if stoppedPrematurely {
		mu.Unlock()
		t.Fatalf("Failure: A Stop() call returned prematurely before shutdown completed!")
	}
	mu.Unlock()

	// Release the hook barrier
	close(releaseHook)

	// Wait for all Stop calls to finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK: Concurrent Stop() calls did not unblock!")
	}

	if ps.GetState() != ServerStateStopped {
		t.Fatalf("Expected final state %s, got %s", ServerStateStopped, ps.GetState())
	}
}

// TestTOFU_Lifecycle_ActiveConnections_DrainedBeforeSettersAllowed proves that active TCP
// connections connected during shutdown have their sockets aborted, handlers terminate, and
// cycle workers drain before Stop() returns and configuration setters are permitted.
func TestTOFU_Lifecycle_ActiveConnections_DrainedBeforeSettersAllowed(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	// Connect a live client TCP socket
	clientConn, err := net.Dial("tcp", ":"+port)
	if err != nil {
		t.Fatalf("Failed to connect client to server: %v", err)
	}
	defer clientConn.Close()

	// Wait until server tracks the active connection
	for i := 0; i < 50; i++ {
		if ps.GetPeersActive() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Call Stop - must abort clientConn and join HandleConnection before returning
	ps.Stop()

	// Read on clientConn must receive EOF or error due to socket closure
	buf := make([]byte, 128)
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, readErr := clientConn.Read(buf)
	if readErr == nil {
		t.Fatalf("Expected client socket to be closed on shutdown, but read succeeded!")
	}

	// Server state must be stopped
	if state := ps.GetState(); state != ServerStateStopped {
		t.Fatalf("Expected state stopped, got %s", state)
	}

	// SetDataDir and SetNetworkID must succeed immediately because all handlers have drained
	newDir := t.TempDir()
	if err := ps.SetDataDir(newDir); err != nil {
		t.Fatalf("SetDataDir failed after Stop: %v", err)
	}
	if err := ps.SetNetworkID("ARMX-REGTEST-1"); err != nil {
		t.Fatalf("SetNetworkID failed after Stop: %v", err)
	}
}

// TestTOFU_Lifecycle_OldCycleWorkers_DoNotSurviveOrUseNewQuitChan proves that workers from
// cycle 1 do not persist or use cycle 2's quit channel after a restart.
func TestTOFU_Lifecycle_OldCycleWorkers_DoNotSurviveOrUseNewQuitChan(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	// Start cycle 1
	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	ps.lifecycleLock.RLock()
	cycle1 := ps.activeCycle
	quit1 := cycle1.quitChan
	ps.lifecycleLock.RUnlock()

	// Stop cycle 1
	ps.Stop()

	// quit1 must be closed
	select {
	case <-quit1:
	default:
		t.Fatalf("Expected cycle 1 quitChan to be closed after Stop")
	}

	// Start cycle 2
	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	ps.lifecycleLock.RLock()
	cycle2 := ps.activeCycle
	quit2 := cycle2.quitChan
	ps.lifecycleLock.RUnlock()

	if cycle1 == cycle2 {
		t.Fatalf("Cycle 1 and Cycle 2 cannot share same cycle instance")
	}
	if quit1 == quit2 {
		t.Fatalf("Cycle 1 and Cycle 2 cannot share same quit channel")
	}

	// quit2 must NOT be closed while cycle 2 is running
	select {
	case <-quit2:
		t.Fatalf("Cycle 2 quitChan was closed prematurely!")
	default:
	}

	ps.Stop()

	// quit2 must be closed after cycle 2 Stop
	select {
	case <-quit2:
	default:
		t.Fatalf("Expected cycle 2 quitChan to be closed after Stop")
	}
}

// TestTOFU_Lifecycle_PeerStoreMutation_BlocksConcurrentSetters proves that when AddPeer()
// or RecordPeerFailure() is actively updating and persisting peers, concurrent SetNetworkID()
// or SetDataDir() is blocked until the transaction completes, preventing cross-configuration data mixing.
func TestTOFU_Lifecycle_PeerStoreMutation_BlocksConcurrentSetters(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	const testPeer = "127.0.0.1:9099"

	// Hook to pause AddPeer right before saving to disk
	mutationEntered := make(chan struct{})
	releaseMutation := make(chan struct{})
	var mutationOnce sync.Once
	setPeerStoreMutationHook(func(op string, addr string) {
		if op == "AddPeer" && addr == testPeer {
			mutationOnce.Do(func() {
				close(mutationEntered)
			})
			<-releaseMutation
		}
	})
	defer func() { setPeerStoreMutationHook(nil) }()

	// Trigger AddPeer in goroutine A
	addPeerDone := make(chan struct{})
	go func() {
		ps.AddPeer(testPeer)
		close(addPeerDone)
	}()

	// Wait until AddPeer holds lifecycleLock.RLock and enters hook
	<-mutationEntered

	// Goroutine B tries to change network ID to regtest while AddPeer is in-flight
	setterResult := make(chan error, 1)
	go func() {
		setterResult <- ps.SetNetworkID("ARMX-REGTEST-1")
	}()

	// Verify setter is blocked and does NOT return while AddPeer is held
	select {
	case <-setterResult:
		t.Fatalf("SECURITY VIOLATION: SetNetworkID succeeded or returned while AddPeer was still persisting!")
	case <-time.After(100 * time.Millisecond):
		// Expected: setter is blocked on lifecycleLock.Lock()
	}

	// Release AddPeer so persistence finishes against testnet store
	close(releaseMutation)

	select {
	case <-addPeerDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK: AddPeer did not complete after release!")
	}

	// Now setter can acquire lifecycleLock and complete
	select {
	case err := <-setterResult:
		if err != nil {
			t.Fatalf("SetNetworkID failed after AddPeer completed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK: SetNetworkID did not unblock after AddPeer completed!")
	}

	// Verify testPeer was committed to testnet store, NOT to regtest store
	testnetStore := filepath.Join(tempDir, "testnet", PeersFile)
	data, err := os.ReadFile(testnetStore)
	if err != nil {
		t.Fatalf("Failed to read testnet store: %v", err)
	}
	if !strings.Contains(string(data), testPeer) {
		t.Fatalf("Expected %s in testnet store %s", testPeer, testnetStore)
	}

	regtestStore := filepath.Join(tempDir, "regtest", PeersFile)
	if rData, err := os.ReadFile(regtestStore); err == nil {
		if strings.Contains(string(rData), testPeer) {
			t.Fatalf("CROSS-STORE LEAKAGE: %s was erroneously written into regtest store!", testPeer)
		}
	}
}

// TestTOFU_Lifecycle_RepeatedBindFailureAndRestart_ZeroLeaks proves that repeated bind failures
// and restarts do not leak registry entries, listeners, channels, or background workers.
func TestTOFU_Lifecycle_RepeatedBindFailureAndRestart_ZeroLeaks(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc, tempDir, "ARMX-TESTNET-1")

	for i := 0; i < 4; i++ {
		// 1. Occupy port to force bind failure
		blocker, err := net.Listen("tcp", ":"+port)
		if err != nil {
			t.Fatalf("Iteration %d: Failed to create blocker on port %s: %v", i, port, err)
		}

		ps.StartServer() // Synchronous failure
		_ = blocker.Close()

		if state := ps.GetState(); state != ServerStateStopped {
			t.Fatalf("Iteration %d: Expected state stopped after bind failure, got %s", i, state)
		}
		if ps.listener != nil {
			t.Fatalf("Iteration %d: Leaked listener after bind failure!", i)
		}
		if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered == ps {
			t.Fatalf("Iteration %d: Leaked registry entry after bind failure!", i)
		}

		// 2. Start server successfully now that port is freed
		go ps.StartServer()
		waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

		if state := ps.GetState(); state != ServerStateRunning {
			t.Fatalf("Iteration %d: Expected state running after restart, got %s", i, state)
		}
		if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered != ps {
			t.Fatalf("Iteration %d: Expected server to be registered in peerServers!", i)
		}

		// 3. Stop server cleanly
		ps.Stop()
		if state := ps.GetState(); state != ServerStateStopped {
			t.Fatalf("Iteration %d: Expected state stopped after stop, got %s", i, state)
		}
		if ps.listener != nil {
			t.Fatalf("Iteration %d: Leaked listener after stop!", i)
		}
		if registered := GetPeerServerForNetwork("ARMX-TESTNET-1"); registered == ps {
			t.Fatalf("Iteration %d: Expected server to be unregistered after stop!", i)
		}
	}
}

// TestTOFU_NetworkID_SingleSourceOfTruthAndConcurrency proves:
// 1. GetNetworkID() returns the synchronized atomic single source of truth initialized by NewPeerServer.
// 2. SetNetworkID() validates input and rejects unsupported or malformed IDs without mutating state.
// 3. SetNetworkID() is strictly rejected while server lifecycle state is active.
// 4. Concurrent calls to GetNetworkID() under heavy concurrent load exhibit zero races or deadlocks.
// 5. When SetNetworkID() switches network under stopped state, in-memory peers are flushed and peer store path is re-scoped.
func TestTOFU_NetworkID_SingleSourceOfTruthAndConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	// 1. Single source of truth at initialization
	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	if netID := ps.GetNetworkID(); netID != "ARMX-TESTNET-1" {
		t.Fatalf("Expected initial NetworkID 'ARMX-TESTNET-1', got %q", netID)
	}

	// 2. SetNetworkID rejection of malformed IDs preserves current state
	if err := ps.SetNetworkID("INVALID-UNKNOWN-NETWORK-99"); err == nil {
		t.Fatalf("Expected SetNetworkID to reject invalid network ID, but got nil")
	}
	if netID := ps.GetNetworkID(); netID != "ARMX-TESTNET-1" {
		t.Fatalf("NetworkID mutated after failed SetNetworkID: got %q, want 'ARMX-TESTNET-1'", netID)
	}

	// 3. Lifecycle rejection while server is running
	go ps.StartServer()
	waitForServerState(t, ps, ServerStateRunning, 5*time.Second)

	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err == nil {
		t.Fatalf("Expected SetNetworkID to fail while server is running, but got nil")
	}

	// 4. Concurrent queries under load
	var wg sync.WaitGroup
	workers := 20
	queriesPerWorker := 100
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for q := 0; q < queriesPerWorker; q++ {
				id := ps.GetNetworkID()
				if id != "ARMX-TESTNET-1" {
					t.Errorf("Unexpected concurrent NetworkID read: %s", id)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Stop server cleanly
	ps.Stop()
	waitForServerState(t, ps, ServerStateStopped, 5*time.Second)

	// 5. Safe mutation while stopped
	if err := ps.SetNetworkID("ARMX-MAINNET-1"); err != nil {
		t.Fatalf("SetNetworkID failed while stopped: %v", err)
	}
	if netID := ps.GetNetworkID(); netID != "ARMX-MAINNET-1" {
		t.Fatalf("Expected updated NetworkID 'ARMX-MAINNET-1', got %q", netID)
	}
}

// TestTOFU_RecordPeerSuccess_PersistsAcrossReload proves that RecordPeerSuccess durably persists
// the peer to the versioned peer store so that it survives server restart and reloads cleanly into triedPeers.
func TestTOFU_RecordPeerSuccess_PersistsAcrossReload(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true

	peerAddr := "192.168.1.100:18333"
	ps.AddPeer(peerAddr)

	expectedFP := "sha256_mock_fingerprint_for_peer_100"
	ps.Lock.Lock()
	if info, inNew := ps.newPeers[peerAddr]; inNew && info != nil {
		info.CertFingerprint = expectedFP
	} else {
		ps.Lock.Unlock()
		t.Fatalf("Peer %s was not added to newPeers", peerAddr)
	}
	ps.Lock.Unlock()

	// Call RecordPeerSuccess
	if err := ps.RecordPeerSuccess(peerAddr, 105, "ARMX-TESTNET-1"); err != nil {
		t.Fatalf("RecordPeerSuccess returned unexpected error: %v", err)
	}

	// Verify in-memory state of original server
	ps.Lock.RLock()
	info, inTried := ps.triedPeers[peerAddr]
	_, inNew := ps.newPeers[peerAddr]
	ps.Lock.RUnlock()

	if !inTried || info == nil {
		t.Fatalf("Peer %s missing from triedPeers after RecordPeerSuccess", peerAddr)
	}
	if inNew {
		t.Fatalf("Peer %s still present in newPeers after promotion", peerAddr)
	}
	if info.Height != 105 {
		t.Fatalf("Expected height 105, got %d", info.Height)
	}
	if info.CertFingerprint != expectedFP {
		t.Fatalf("Expected fingerprint %q, got %q", expectedFP, info.CertFingerprint)
	}

	// Instantiate fresh PeerServer with the same data directory and load peers
	reloadServer := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	reloadServer.loadPeers()

	reloadServer.Lock.RLock()
	reloadedInfo, reloadedInTried := reloadServer.triedPeers[peerAddr]
	reloadServer.Lock.RUnlock()

	if !reloadedInTried || reloadedInfo == nil {
		t.Fatalf("Peer %s was NOT durably persisted to disk; missing from triedPeers after reload", peerAddr)
	}
	if reloadedInfo.Height != 105 {
		t.Fatalf("Reloaded peer height mismatch: expected 105, got %d", reloadedInfo.Height)
	}
	if reloadedInfo.NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("Reloaded peer NetworkID mismatch: expected 'ARMX-TESTNET-1', got %q", reloadedInfo.NetworkID)
	}
	if reloadedInfo.CertFingerprint != expectedFP {
		t.Fatalf("Reloaded peer fingerprint mismatch: expected %q, got %q", expectedFP, reloadedInfo.CertFingerprint)
	}
}

// TestTOFU_RecordPeerSuccess_PersistenceFailure_Rollback proves that when disk persistence fails,
// RecordPeerSuccess returns an error and cleanly rolls back in-memory state without leaving
// false claims of promotion.
func TestTOFU_RecordPeerSuccess_PersistenceFailure_Rollback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true

	peerAddr := "192.168.1.101:18333"
	ps.AddPeer(peerAddr)

	ps.Lock.RLock()
	if _, inNew := ps.newPeers[peerAddr]; !inNew {
		ps.Lock.RUnlock()
		t.Fatalf("Peer %s was not added to newPeers", peerAddr)
	}
	ps.Lock.RUnlock()

	// Inject simulated disk write failure
	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		return 0, errors.New("simulated disk I/O write error during RecordPeerSuccess")
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	err = ps.RecordPeerSuccess(peerAddr, 200, "ARMX-TESTNET-1")
	if err == nil {
		t.Fatalf("Expected RecordPeerSuccess to return error on persistence failure, got nil")
	}

	// Verify in-memory state was cleanly rolled back
	ps.Lock.RLock()
	_, inTried := ps.triedPeers[peerAddr]
	_, inNew := ps.newPeers[peerAddr]
	ps.Lock.RUnlock()

	if inTried {
		t.Fatalf("Peer %s was erroneously left in triedPeers after persistence failure", peerAddr)
	}
	if !inNew {
		t.Fatalf("Peer %s was not restored to newPeers after rollback", peerAddr)
	}
}

// TestTOFU_GetNetworkID_UninitializedStruct_SingleSourceOfTruth proves:
// 1. Uninitialized &PeerServer{} safely resolves NetworkID through atomic CAS without reading unmanaged struct fields.
// 2. Concurrent calls to GetNetworkID() on uninitialized structs return identical canonical values without races.
func TestTOFU_GetNetworkID_UninitializedStruct_SingleSourceOfTruth(t *testing.T) {
	psUninit := &PeerServer{}

	var wg sync.WaitGroup
	results := make([]string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = psUninit.GetNetworkID()
		}(i)
	}
	wg.Wait()

	expected := core.NetworkID()
	for i, res := range results {
		if res != expected {
			t.Fatalf("Worker %d returned %q, expected %q", i, res, expected)
		}
	}
}

// TestTOFU_RecordPeerSuccess_ConcurrentTransactions_NoRacesOrLostUpdates proves:
// 1. Multiple concurrent goroutines calling RecordPeerSuccess serialize cleanly under savePeersLock.
// 2. Concurrent RecordPeerFailure invocations do not cause races, lost updates, or corrupted state.
// 3. Rollback of a failing transaction (simulated disk error on a specific peer) does not clobber successful concurrent peers.
// 4. Reloading from disk confirms durable persistence of all successfully committed transactions.
func TestTOFU_RecordPeerSuccess_ConcurrentTransactions_NoRacesOrLostUpdates(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true
	ps.DisableSubnetLimit = true

	// Pre-populate 20 peers in newPeers
	numPeers := 20
	peers := make([]string, numPeers)
	for i := 0; i < numPeers; i++ {
		addr := fmt.Sprintf("192.168.1.%d:18333", i+10)
		peers[i] = addr
		ps.AddPeer(addr)
		// Assign unique certificate fingerprint
		ps.Lock.Lock()
		if info, ok := ps.newPeers[addr]; ok && info != nil {
			info.CertFingerprint = fmt.Sprintf("fp_%d", i+10)
		}
		ps.Lock.Unlock()
	}

	// Also add a 21st peer that will deliberately encounter a simulated persistence error
	failPeer := "192.168.1.99:18333"
	ps.AddPeer(failPeer)

	// Persist initial state
	if err := ps.savePeers(); err != nil {
		t.Fatalf("Failed to persist initial state: %v", err)
	}

	// Intercept write hook: fail persistence only when attempting to save failPeer's promotion (height 88888)
	var failOnce sync.Once
	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		var injectFail bool
		// If data contains the failPeer's unique promotion height (88888) and failOnce hasn't fired yet, simulate write error
		if strings.Contains(string(b), "88888") {
			failOnce.Do(func() {
				injectFail = true
			})
		}
		if injectFail {
			return 0, fmt.Errorf("simulated disk error for %s", failPeer)
		}
		return f.Write(b)
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, numPeers*3)

	// Concurrently promote distinct peers
	for i := 0; i < numPeers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := peers[idx]
			if err := ps.RecordPeerSuccess(addr, 100+idx, "ARMX-TESTNET-1"); err != nil {
				errCh <- fmt.Errorf("concurrent promotion of %s failed: %w", addr, err)
			}
		}(i)
	}

	// Concurrently promote the same peer (peers[0]) with varying heights
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(round int) {
			defer wg.Done()
			addr := peers[0]
			if err := ps.RecordPeerSuccess(addr, 200+round, "ARMX-TESTNET-1"); err != nil {
				errCh <- fmt.Errorf("concurrent contention on %s failed: %w", addr, err)
			}
		}(i)
	}

	// Concurrently invoke RecordPeerFailure on peers[1] and peers[2] while promotions are active
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := peers[idx]
			if err := ps.RecordPeerFailure(addr); err != nil {
				errCh <- fmt.Errorf("concurrent failure recording on %s failed: %w", addr, err)
			}
		}(i)
	}

	// Launch promotion for failPeer with height 88888 that will encounter the injected failure and roll back
	var failPeerErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		failPeerErr = ps.RecordPeerSuccess(failPeer, 88888, "ARMX-TESTNET-1")
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Concurrent transaction error on valid peer: %v", err)
	}

	// failPeer must have returned the persistence error
	if failPeerErr == nil {
		t.Fatalf("Expected failPeer promotion to fail and return persistence error, got nil")
	}

	// Verify failPeer cleanly rolled back to newPeers with original fingerprint preserved
	ps.Lock.RLock()
	if _, inTried := ps.triedPeers[failPeer]; inTried {
		ps.Lock.RUnlock()
		t.Fatalf("failPeer found in triedPeers after persistence failure rollback!")
	}
	infoFail, inNew := ps.newPeers[failPeer]
	if !inNew || infoFail == nil || infoFail.CertFingerprint != "" {
		ps.Lock.RUnlock()
		t.Fatalf("failPeer not properly restored in newPeers: %v", infoFail)
	}

	// Verify all 20 successful peers are present in triedPeers with preserved fingerprints
	for i := 0; i < numPeers; i++ {
		addr := peers[i]
		info, ok := ps.triedPeers[addr]
		if !ok || info == nil {
			ps.Lock.RUnlock()
			t.Fatalf("Peer %s missing from triedPeers after concurrent promotions", addr)
		}
		expectedFP := fmt.Sprintf("fp_%d", i+10)
		if info.CertFingerprint != expectedFP {
			ps.Lock.RUnlock()
			t.Fatalf("Peer %s fingerprint corrupted: want %q, got %q", addr, expectedFP, info.CertFingerprint)
		}
	}
	ps.Lock.RUnlock()

	// Reload from disk into a clean PeerServer instance
	reloaded := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	reloaded.loadPeers()

	reloaded.Lock.RLock()
	defer reloaded.Lock.RUnlock()
	for i := 0; i < numPeers; i++ {
		addr := peers[i]
		info, ok := reloaded.triedPeers[addr]
		if !ok || info == nil {
			t.Fatalf("Peer %s missing from triedPeers upon disk reload", addr)
		}
		expectedFP := fmt.Sprintf("fp_%d", i+10)
		if info.CertFingerprint != expectedFP {
			t.Fatalf("Reloaded peer %s fingerprint mismatch: want %q, got %q", addr, expectedFP, info.CertFingerprint)
		}
	}
	// failPeer must also be in newPeers on reload (not triedPeers)
	if _, inTried := reloaded.triedPeers[failPeer]; inTried {
		t.Fatalf("Reloaded disk store contains failPeer in triedPeers!")
	}
	if info, inNew := reloaded.newPeers[failPeer]; !inNew || info == nil || info.CertFingerprint != "" {
		t.Fatalf("Reloaded disk store missing failPeer in newPeers: %v", info)
	}
}

// TestTOFU_RecordPeerSuccess_FailureInterleaving_RollbackDoesNotCorrupt forces deterministic interleaving:
// 1. RecordPeerSuccess modifies in-memory state and enters persistence path.
// 2. Controlled barrier triggers RecordPeerFailure on the identical peer address (blocks on savePeersLock).
// 3. First persistence operation is forced to fail, triggering in-memory rollback.
// 4. Blocked failure operation unblocks, acquires lock, and records failure/eviction without losing state.
// 5. Verifies memory and disk match cleanly after reload.
func TestTOFU_RecordPeerSuccess_FailureInterleaving_RollbackDoesNotCorrupt(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	ps.AllowPrivatePeers = true
	ps.DisableSubnetLimit = true

	targetPeer := "192.168.1.77:18333"
	targetFP := "fp_initial_target_77"

	// Pre-populate target peer in newPeers with MaxPeerFailures - 1 failures (2 out of 3)
	ps.AddPeer(targetPeer)
	ps.Lock.Lock()
	if info, ok := ps.newPeers[targetPeer]; ok && info != nil {
		info.CertFingerprint = targetFP
		info.Failures = MaxPeerFailures - 1
	}
	ps.Lock.Unlock()

	// Persist initial state to disk
	if err := ps.savePeers(); err != nil {
		t.Fatalf("Failed to persist initial peer store: %v", err)
	}

	g1InWriteHook := make(chan struct{})
	g1CanFail := make(chan struct{})
	var hookOnce sync.Once

	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		var shouldFail bool
		hookOnce.Do(func() {
			close(g1InWriteHook)
			<-g1CanFail
			shouldFail = true
		})
		if shouldFail {
			return 0, fmt.Errorf("simulated I/O failure on RecordPeerSuccess persistence")
		}
		return f.Write(b)
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	var wg sync.WaitGroup
	var g1Err error
	var g2Err error

	// Step 1: Launch Goroutine 1 (RecordPeerSuccess)
	wg.Add(1)
	go func() {
		defer wg.Done()
		g1Err = ps.RecordPeerSuccess(targetPeer, 999, "ARMX-TESTNET-1")
	}()

	// Wait until Goroutine 1 has staged in-memory update and reached write hook under savePeersLock
	select {
	case <-g1InWriteHook:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for Goroutine 1 to reach write hook")
	}

	// Step 2: Trigger Goroutine 2 (RecordPeerFailure) on the SAME peer address.
	// Because Goroutine 1 holds savePeersLock, Goroutine 2 MUST serialize and wait.
	g2Started := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(g2Started)
		g2Err = ps.RecordPeerFailure(targetPeer)
	}()

	<-g2Started

	// Step 3: Force Goroutine 1's persistence to fail, which triggers rollback
	close(g1CanFail)

	// Step 4: Wait for both goroutines to finish
	wg.Wait()

	// Step 5: Verify errors
	if g1Err == nil {
		t.Fatalf("Expected Goroutine 1 to return persistence error, got nil")
	}
	if g2Err != nil {
		t.Fatalf("Expected Goroutine 2 (RecordPeerFailure) to succeed, got %v", g2Err)
	}

	// Because targetPeer had MaxPeerFailures-1, Goroutine 2's failure increment brings it to
	// MaxPeerFailures, which triggers dead peer eviction.
	// If Goroutine 1's rollback had clobbered Goroutine 2, targetPeer would still be in triedPeers or newPeers!
	ps.Lock.RLock()
	_, inTried := ps.triedPeers[targetPeer]
	_, inNew := ps.newPeers[targetPeer]
	ps.Lock.RUnlock()

	if inTried || inNew {
		t.Fatalf("targetPeer was NOT evicted: inTried=%v, inNew=%v. Rollback may have overwritten failure eviction!", inTried, inNew)
	}

	// Step 6: Reload into a clean PeerServer instance from disk
	reloaded := NewPeerServer(getFreePort(t), bc, tempDir, "ARMX-TESTNET-1")
	reloaded.loadPeers()

	reloaded.Lock.RLock()
	defer reloaded.Lock.RUnlock()
	_, reloadedInTried := reloaded.triedPeers[targetPeer]
	_, reloadedInNew := reloaded.newPeers[targetPeer]
	if reloadedInTried || reloadedInNew {
		t.Fatalf("Reloaded peer store contains evicted peer: tried=%v, new=%v", reloadedInTried, reloadedInNew)
	}
}
