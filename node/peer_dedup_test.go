package node

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// mockFingerprintConn implements net.Conn and FingerprintProvider for testing
type mockFingerprintConn struct {
	net.Conn
	fingerprint string
}

func (m *mockFingerprintConn) GetCertFingerprint() string {
	return m.fingerprint
}

// 1. Unit Test: DNS seed dan IP hasil resolusi yang sama.
// Memastikan bahwa jika DNS seed dan IP hasil resolusinya sama-sama ditambahkan / terhubung,
// DNS seed ditampilkan menggunakan hostname DNS, IP hasil resolusinya tidak ditampilkan lagi,
// dan peer count hanya menghitung 1 peer unik.
func TestDedup_DNSSeedAndResolvedIP(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "testnet-seed1.armax-coin.cloud:19335"
	ip := "202.10.34.180:19335"

	// Register mapping
	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[ip] = struct{}{}
	ps.seedHostToIPs[seed] = []string{ip}
	ps.seedHostToIPs[normalizePeerEndpoint(seed)] = []string{ip}
	ps.seedIPToHosts[ip] = []string{seed}
	ps.seedIPToHosts[normalizePeerEndpoint(ip)] = []string{seed}
	ps.seedMu.Unlock()

	// Add both seed hostname and resolved IP
	_ = ps.AddPeer(seed)
	_ = ps.AddPeer(ip)

	// 1. Known peers count must be strictly 1
	known := ps.GetPeersKnown()
	if known != 1 {
		t.Fatalf("[Dedup 1] Expected 1 deduplicated known peer, got %d (%v)", known, ps.GetPeers())
	}

	// 2. Display label must be the DNS seed hostname, NOT the IP
	peers := ps.GetPeers()
	if len(peers) != 1 || peers[0] != seed {
		t.Fatalf("[Dedup 1] Expected peer display '%s', got %v", seed, peers)
	}

	// 3. Connect to the IP address outbound; verify active display and count
	cli, srv := net.Pipe()
	defer cli.Close()
	defer srv.Close()

	sess := &outboundPeerSession{
		peerAddress: ip,
		conn:        cli,
	}
	sess.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions[ip] = sess
	ps.outboundMu.Unlock()

	activeCount := ps.GetPeersActive()
	if activeCount != 1 {
		t.Fatalf("[Dedup 1] Expected 1 active peer, got %d", activeCount)
	}

	activeEndpoints := ps.GetActivePeerEndpoints()
	if len(activeEndpoints) != 1 || activeEndpoints[0] != seed {
		t.Fatalf("[Dedup 1] Expected active endpoint display '%s', got %v", seed, activeEndpoints)
	}
}

// 2. Unit Test: Beberapa DNS seed yang menunjuk ke satu IP.
// Memastikan bahwa jika beberapa DNS seed (misal seed-a dan seed-b) menunjuk ke IP yang sama,
// keduanya dihitung sebagai 1 node fisik unik, display label memilih DNS seed utama,
// dan IP fisik tidak ditampilkan terpisah.
func TestDedup_MultipleDNSSeedsSameIP(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seedA := "testnet-seed-a.armax-coin.cloud:19335"
	seedB := "testnet-seed-b.armax-coin.cloud:19335"
	sharedIP := "198.51.100.50:19335"

	ps.seedMu.Lock()
	ps.seedPeers = []string{seedA, seedB}
	ps.seedAddrs[sharedIP] = struct{}{}
	ps.seedHostToIPs[seedA] = []string{sharedIP}
	ps.seedHostToIPs[normalizePeerEndpoint(seedA)] = []string{sharedIP}
	ps.seedHostToIPs[seedB] = []string{sharedIP}
	ps.seedHostToIPs[normalizePeerEndpoint(seedB)] = []string{sharedIP}
	ps.seedIPToHosts[sharedIP] = []string{seedA, seedB}
	ps.seedIPToHosts[normalizePeerEndpoint(sharedIP)] = []string{seedA, seedB}
	ps.seedMu.Unlock()

	_ = ps.AddPeer(seedA)
	_ = ps.AddPeer(seedB)
	_ = ps.AddPeer(sharedIP)

	known := ps.GetPeersKnown()
	if known != 1 {
		t.Fatalf("[Dedup 2] Expected 1 unique peer for seeds sharing same IP, got %d (%v)", known, ps.GetPeers())
	}

	peers := ps.GetPeers()
	if len(peers) != 1 || (peers[0] != seedA && peers[0] != seedB) {
		t.Fatalf("[Dedup 2] Expected display label to be one of the DNS seeds, got %v", peers)
	}
}

// 3. Unit Test: Peer publik non-seed.
// Memastikan peer publik independen yang bukan merupakan DNS seed tetap ditampilkan
// sebagai IP publiknya sendiri tanpa modifikasi.
func TestDedup_PublicNonSeedPeer(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "testnet-seed1.armax-coin.cloud:19335"
	seedIP := "202.10.34.180:19335"
	nonSeedIP := "198.51.100.99:19335"

	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[seedIP] = struct{}{}
	ps.seedHostToIPs[seed] = []string{seedIP}
	ps.seedIPToHosts[seedIP] = []string{seed}
	ps.seedMu.Unlock()

	_ = ps.AddPeer(seed)
	_ = ps.AddPeer(nonSeedIP)

	known := ps.GetPeersKnown()
	if known != 2 {
		t.Fatalf("[Dedup 3] Expected 2 known peers (1 seed + 1 public non-seed), got %d (%v)", known, ps.GetPeers())
	}

	peers := ps.GetPeers()
	hasSeed := false
	hasNonSeed := false
	for _, p := range peers {
		if p == seed {
			hasSeed = true
		}
		if p == nonSeedIP {
			hasNonSeed = true
		}
	}
	if !hasSeed || !hasNonSeed {
		t.Fatalf("[Dedup 3] Expected peers to contain '%s' and '%s', got %v", seed, nonSeedIP, peers)
	}
}

// 4. Unit Test: IPv4 dan IPv6.
// Memastikan normalisasi endpoint IPv4 dan IPv6 (format canonical dengan bracket dan compressed hex),
// serta memastikan variasi representasi IPv6 yang sama tidak dihitung ganda.
func TestDedup_IPv4AndIPv6Normalization(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	ipv4 := "198.51.100.10:19335"
	ipv6Long := "[2001:0db8:0000:0000:0000:0000:0000:0001]:19335"
	ipv6Short := "[2001:db8::1]:19335"

	// Verify normalization output directly
	normLong := normalizePeerEndpoint(ipv6Long)
	normShort := normalizePeerEndpoint(ipv6Short)
	if normLong != normShort {
		t.Fatalf("[Dedup 4] IPv6 normalization mismatch: long='%s' short='%s'", normLong, normShort)
	}

	_ = ps.AddPeer(ipv4)
	_ = ps.AddPeer(ipv6Long)
	_ = ps.AddPeer(ipv6Short) // Must be recognized as duplicate of ipv6Long

	known := ps.GetPeersKnown()
	if known != 2 {
		t.Fatalf("[Dedup 4] Expected exactly 2 peers (1 IPv4, 1 IPv6), got %d (%v)", known, ps.GetPeers())
	}
}

// 5. Unit Test: Port berbeda pada IP yang sama.
// Memastikan dua peer pada IP yang sama tetapi port berbeda tetap dihitung sebagai dua peer terpisah.
func TestDedup_DifferentPortsSameIP(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	peerPort1 := "198.51.100.25:19335"
	peerPort2 := "198.51.100.25:19336"

	_ = ps.AddPeer(peerPort1)
	_ = ps.AddPeer(peerPort2)

	known := ps.GetPeersKnown()
	if known != 2 {
		t.Fatalf("[Dedup 5] Expected 2 distinct peers for different ports on same IP, got %d (%v)", known, ps.GetPeers())
	}
}

// 6. Unit Test: Dua identitas peer berbeda pada IP yang sama (TOFU Cert Fingerprint).
// Memastikan bahwa jika dua koneksi berasal dari IP yang sama tetapi memiliki sertifikat TLS yang berbeda,
// kedua peer TIDAK digabungkan sembarangan, melainkan dihitung sebagai 2 identitas unik.
func TestDedup_TwoIdentitiesSameIP(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	sharedIP := "198.51.100.77:19335"
	fpAlpha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	fpBeta := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	cli1, srv1 := net.Pipe()
	defer cli1.Close()
	defer srv1.Close()

	cli2, srv2 := net.Pipe()
	defer cli2.Close()
	defer srv2.Close()

	mockConn1 := &mockFingerprintConn{Conn: cli1, fingerprint: fpAlpha}
	mockConn2 := &mockFingerprintConn{Conn: cli2, fingerprint: fpBeta}

	sess1 := &outboundPeerSession{
		peerAddress: sharedIP,
		conn:        mockConn1,
	}
	sess1.handshakeValidated.Store(true)

	sess2 := &outboundPeerSession{
		peerAddress: sharedIP,
		conn:        mockConn2,
	}
	sess2.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	// Simulate two sessions with distinct fingerprints
	ps.outboundSessions["198.51.100.77:19335"] = sess1
	ps.outboundSessions["198.51.100.77:19335#2"] = sess2
	ps.outboundMu.Unlock()

	active := ps.GetPeersActive()
	if active != 2 {
		t.Fatalf("[Dedup 6] Expected 2 distinct active peers for different cert fingerprints on same IP, got %d", active)
	}

	details := ps.GetActivePeerDetails()
	if len(details) != 2 {
		t.Fatalf("[Dedup 6] Expected 2 peer details, got %d", len(details))
	}
	if details[0].CanonicalID == details[1].CanonicalID {
		t.Fatalf("[Dedup 6] Expected distinct canonical IDs, both got: %s", details[0].CanonicalID)
	}
}

// 7. Unit Test: Peer disconnected dan reconnect.
// Memastikan siklus disconnect dan reconnect tidak meninggalkan residu atau menghitung ganda.
func TestDedup_PeerDisconnectAndReconnect(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	addr := "198.51.100.80:19335"

	// 1. Initial connection
	cli1, srv1 := net.Pipe()
	defer srv1.Close()
	sess1 := &outboundPeerSession{
		peerAddress: addr,
		conn:        cli1,
	}
	sess1.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions[addr] = sess1
	ps.outboundMu.Unlock()

	if ps.GetPeersActive() != 1 {
		t.Fatalf("[Dedup 7] Expected 1 active peer on initial connect, got %d", ps.GetPeersActive())
	}

	// 2. Disconnect
	ps.dropOutboundSession(addr, sess1)
	if ps.GetPeersActive() != 0 {
		t.Fatalf("[Dedup 7] Expected 0 active peers after disconnect, got %d", ps.GetPeersActive())
	}

	// 3. Reconnect
	cli2, srv2 := net.Pipe()
	defer srv2.Close()
	sess2 := &outboundPeerSession{
		peerAddress: addr,
		conn:        cli2,
	}
	sess2.handshakeValidated.Store(true)

	ps.outboundMu.Lock()
	ps.outboundSessions[addr] = sess2
	ps.outboundMu.Unlock()

	if ps.GetPeersActive() != 1 {
		t.Fatalf("[Dedup 7] Expected 1 active peer after reconnect, got %d", ps.GetPeersActive())
	}
}

// 8. Unit Test: DNS resolution gagal.
// Memastikan seed yang gagal resolusi DNS tidak menyebabkan panic/crash dan tetap tersimpan
// dengan identitas hostnamanya secara aman.
func TestDedup_DNSResolutionFailure(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	brokenSeed := "nonexistent-seed.armax-coin.cloud:19335"

	// Mock DNS resolver returning empty
	ps.SetSeedPeers([]string{brokenSeed})

	// Seed must be preserved in seedPeers list
	if ps.GetPeersSeeds() != 1 {
		t.Fatalf("[Dedup 8] Expected 1 configured seed peer, got %d", ps.GetPeersSeeds())
	}

	// Calling getDisplayAddress on the broken seed safely returns its hostname
	disp := ps.getDisplayAddress(brokenSeed)
	if disp != brokenSeed {
		t.Fatalf("[Dedup 8] Expected display address '%s', got '%s'", brokenSeed, disp)
	}

	// Canonical ID is safely derived
	canon := ps.getCanonicalPeerID(brokenSeed, "")
	if canon != "endpoint:"+brokenSeed {
		t.Fatalf("[Dedup 8] Expected canonical ID 'endpoint:%s', got '%s'", brokenSeed, canon)
	}
}

// 9. Unit Test: RPC /peers dan /stats deduplication & details.
// Memverifikasi bahwa endpoint /peers dan /stats memberikan respon yang konsisten
// baik untuk format string array default maupun structured details (?details=true).
func TestDedup_RPCServerPeersAndStatsEndpoints(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)
	ps.AllowPrivatePeers = true

	seed := "testnet-seed1.armax-coin.cloud:19335"
	resolvedIP := "202.10.34.180:19335"
	publicIP := "198.51.100.90:19335"

	ps.seedMu.Lock()
	ps.seedPeers = []string{seed}
	ps.seedAddrs[resolvedIP] = struct{}{}
	ps.seedHostToIPs[seed] = []string{resolvedIP}
	ps.seedIPToHosts[resolvedIP] = []string{seed}
	ps.seedMu.Unlock()

	_ = ps.AddPeer(seed)
	_ = ps.AddPeer(resolvedIP)
	_ = ps.AddPeer(publicIP)

	srvCfg := &ServerConfig{
		NetworkID: core.NetworkID(),
		RPCBind:   "127.0.0.1",
		RPCPort:   getFreePort(t),
	}
	handler := NewRPCHandler(bc, ps, newIPRateLimiter(), srvCfg)

	// 1. Query /stats
	reqStats := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recStats := httptest.NewRecorder()
	handler.ServeHTTP(recStats, reqStats)

	var stats map[string]interface{}
	if err := json.Unmarshal(recStats.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats: %v", err)
	}
	knownCount := int(stats["peers_known"].(float64))
	if knownCount != 2 {
		t.Fatalf("Expected /stats peers_known == 2 (1 seed + 1 public IP), got %d", knownCount)
	}

	// 2. Query /peers (default string array)
	reqPeers := httptest.NewRequest(http.MethodGet, "/peers", nil)
	recPeers := httptest.NewRecorder()
	handler.ServeHTTP(recPeers, reqPeers)

	var peers []string
	if err := json.Unmarshal(recPeers.Body.Bytes(), &peers); err != nil {
		t.Fatalf("Failed to parse /peers default: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("Expected /peers to return 2 items, got %d (%v)", len(peers), peers)
	}

	// 3. Query /peers?details=true (structured details)
	reqDetails := httptest.NewRequest(http.MethodGet, "/peers?details=true", nil)
	recDetails := httptest.NewRecorder()
	handler.ServeHTTP(recDetails, reqDetails)

	var peerEntries []PeerEntry
	if err := json.Unmarshal(recDetails.Body.Bytes(), &peerEntries); err != nil {
		t.Fatalf("Failed to parse /peers?details=true: %v", err)
	}
	if len(peerEntries) != 2 {
		t.Fatalf("Expected 2 PeerEntry items, got %d", len(peerEntries))
	}

	for _, entry := range peerEntries {
		if entry.DisplayAddress == seed {
			if !entry.IsSeed {
				t.Errorf("Expected IsSeed=true for %s", seed)
			}
		}
		if entry.DisplayAddress == publicIP {
			if entry.IsSeed {
				t.Errorf("Expected IsSeed=false for %s", publicIP)
			}
		}
	}
}
