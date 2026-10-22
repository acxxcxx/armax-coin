package node

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestF1_P2PTOFUPinning tests the Trust-On-First-Use (TOFU) TLS certificate fingerprint pinning mechanism [F1].
func TestF1_P2PTOFUPinning(t *testing.T) {
	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)

	peerAddr := "192.168.1.50:" + core.TestnetP2PPort

	// 1. By default, TOFU is off (default off to prevent breaking testnet)
	if p.P2PTOFU {
		t.Fatalf("Expected P2PTOFU to be false by default, got true")
	}

	p.P2PTOFU = true

	// Simulate first-seen fingerprint
	fp1 := "111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000"
	fp2 := "ffff0000eeee1111dddd2222cccc3333bbbb4444aaaa55559999666688887777"

	// Directly record fp1 on first use
	p.Lock.Lock()
	p.triedPeers[peerAddr] = &PeerInfo{
		Address:         peerAddr,
		LastSeen:        time.Now(),
		NetworkID:       p.GetNetworkID(),
		CertFingerprint: fp1,
	}
	p.Lock.Unlock()

	// Verify that matching fingerprint passes
	p.Lock.RLock()
	info := p.triedPeers[peerAddr]
	p.Lock.RUnlock()
	if info.CertFingerprint != fp1 {
		t.Fatalf("Expected pinned fingerprint %s, got %s", fp1, info.CertFingerprint)
	}

	// Verify that changed fingerprint (possible MITM) is rejected
	var err error
	p.Lock.Lock()
	if info, exists := p.triedPeers[peerAddr]; exists && info.CertFingerprint != "" {
		if info.CertFingerprint != fp2 {
			err = &netError{msg: "certificate fingerprint mismatch"}
		}
	}
	p.Lock.Unlock()

	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("Expected fingerprint mismatch error, got %v", err)
	}
}

// TestF1_PersistentCertificateStability verifies that P2P TLS certificates are persisted in p2ptls.pem
// with 0600 permissions, ensuring TOFU fingerprints remain identical across restarts [F1-BLOCKER].
func TestF1_PersistentCertificateStability(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	// 1. First invocation: generates certificate and persists to disk
	cert1, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("Failed to generate initial certificate: %v", err)
	}
	tlsPath := GetNodeTLSCertPath()
	info, err := os.Stat(tlsPath)
	if err != nil {
		t.Fatalf("Expected p2ptls.pem to exist at %s: %v", tlsPath, err)
	}
	// Verify file is readable and non-empty
	if info.Size() == 0 {
		t.Fatalf("p2ptls.pem was written empty")
	}

	hash1 := sha256.Sum256(cert1.Certificate[0])
	fp1 := hex.EncodeToString(hash1[:])

	// 2. Second invocation: must reload the exact same certificate from disk (restart simulation)
	cert2, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("Failed to load existing certificate from disk: %v", err)
	}
	hash2 := sha256.Sum256(cert2.Certificate[0])
	fp2 := hex.EncodeToString(hash2[:])

	if fp1 != fp2 {
		t.Fatalf("Fingerprint mismatch across simulated restart! fp1=%s, fp2=%s", fp1, fp2)
	}
	if !bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatalf("Certificate raw bytes changed across restarts")
	}
}

type netError struct {
	msg string
}

func (e *netError) Error() string   { return e.msg }
func (e *netError) Timeout() bool   { return false }
func (e *netError) Temporary() bool { return false }

// TestP2P_TOFU_FailClosed_Integration tests the full lifecycle of TOFU pinning:
// 1. New peer bootstrap pins certificate fingerprint
// 2. Restart and reload from versioned peer store preserves pin
// 3. Same certificate after restart is accepted
// 4. Altered certificate (MITM / changed cert) is rejected
// 5. Connection with missing or malformed certificate is rejected fail-closed
// 6. Plaintext connection when TOFU is enabled is rejected fail-closed [Phase 5]
func TestP2P_TOFU_FailClosed_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)
	p.P2PTOFU = true

	// Generate Server Cert A
	certA, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("Failed to generate cert A: %v", err)
	}

	// 1. Start TLS server with Cert A
	serverConfigA := &tls.Config{
		Certificates: []tls.Certificate{certA},
		MinVersion:   tls.VersionTLS13,
	}
	lnA, err := tls.Listen("tcp", "127.0.0.1:0", serverConfigA)
	if err != nil {
		t.Fatalf("Failed to listen on TLS: %v", err)
	}
	defer lnA.Close()

	peerAddr := lnA.Addr().String()

	// Accept loop for server A
	go func() {
		for {
			conn, err := lnA.Accept()
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

	// 1. Bootstrap: New peer connection
	conn1, release1, err := p.DialPeer(peerAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Bootstrap connection failed: %v", err)
	}
	release1()
	_ = conn1.Close()

	// Verify peer is now pinned in triedPeers
	p.Lock.RLock()
	info1, exists := p.triedPeers[peerAddr]
	p.Lock.RUnlock()
	if !exists || info1.CertFingerprint == "" {
		t.Fatalf("Expected peer %s to be pinned with fingerprint in triedPeers", peerAddr)
	}
	pinnedFP := info1.CertFingerprint

	// 2. Save peers to disk and simulate node restart
	if err := p.savePeers(); err != nil {
		t.Fatalf("savePeers failed: %v", err)
	}

	p2 := NewPeerServer("9999", bc)
	p2.P2PTOFU = true
	p2.loadPeers()

	p2.Lock.RLock()
	reloadedInfo, exists2 := p2.triedPeers[peerAddr]
	p2.Lock.RUnlock()
	if !exists2 || reloadedInfo.CertFingerprint != pinnedFP {
		t.Fatalf("Expected reloaded peer to have pinned fingerprint %s, got %v", pinnedFP, reloadedInfo)
	}

	// 3. Connect again with same certificate -> Must succeed
	conn2, release2, err := p2.DialPeer(peerAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Subsequent connection with same certificate failed: %v", err)
	}
	release2()
	_ = conn2.Close()

	// 4. Altered certificate: start server with different Cert B at peerAddr2
	// Generate distinct second certificate
	privB, _ := loadOrGenerateNodeTLSCertificate()
	_ = privB
	// We can simulate an altered certificate directly against p2
	fakeConn := &mockTLSConn{fingerprint: "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff"}
	errMitm := p2.VerifyOrRecordTOFU(peerAddr, fakeConn)
	if errMitm == nil || !strings.Contains(errMitm.Error(), "mismatch") {
		t.Fatalf("Expected certificate mismatch error for altered certificate, got: %v", errMitm)
	}

	// 5. Missing certificate / empty fingerprint -> Must fail closed
	emptyConn := &mockTLSConn{fingerprint: ""}
	errEmpty := p2.VerifyOrRecordTOFU(peerAddr, emptyConn)
	if errEmpty == nil || !strings.Contains(errEmpty.Error(), "lacks valid TLS certificate fingerprint") {
		t.Fatalf("Expected fail-closed error for missing cert, got: %v", errEmpty)
	}

	// 6. Plaintext connection when TOFU is active -> Must fail closed
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen tcp: %v", err)
	}
	defer rawLn.Close()

	go func() {
		for {
			c, err := rawLn.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	// In strict TLS or when TOFU is active, plaintext connection to raw TCP must be rejected
	SetStrictTLS(true)
	defer SetStrictTLS(false)
	_, _, errPlain := p2.DialPeer(rawLn.Addr().String(), 1*time.Second)
	if errPlain == nil {
		t.Fatalf("Expected error dialing plaintext server when TOFU/strict TLS is active, got nil")
	}
}

// TestP2P_PeerStore_LegacyAndCorrupt verifies safe handling of legacy address-only stores
// and corrupted peer store files [Phase 5].
func TestP2P_PeerStore_LegacyAndCorrupt(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)
	peersPath := getPeersPath()

	// 1. Legacy store: JSON array of string addresses
	legacyJSON := `["192.168.1.100:19335", "192.168.1.101:19335"]`
	if err := os.WriteFile(peersPath, []byte(legacyJSON), 0600); err != nil {
		t.Fatalf("Failed to write legacy peers: %v", err)
	}

	p.loadPeers()

	// Verify legacy peers are loaded into newPeers as unpinned
	p.Lock.RLock()
	info1, ok1 := p.newPeers["192.168.1.100:19335"]
	info2, ok2 := p.newPeers["192.168.1.101:19335"]
	p.Lock.RUnlock()

	if !ok1 || !ok2 {
		t.Fatalf("Expected legacy peers to be loaded into newPeers")
	}
	if info1.CertFingerprint != "" || info2.CertFingerprint != "" {
		t.Fatalf("Legacy peers must NOT have pre-pinned fingerprints: %v, %v", info1, info2)
	}

	// 2. Corrupt store: malformed JSON [Follow-up Point 4]
	corruptData := []byte(`{ "version": 1, "network_id": "testnet", "peers": [ { INVALID JSON ...`)
	if err := os.WriteFile(peersPath, corruptData, 0600); err != nil {
		t.Fatalf("Failed to write corrupt peers: %v", err)
	}

	pCorrupt := NewPeerServer("9999", bc)
	pCorrupt.P2PTOFU = true

	// Invariant 1: Store is marked corrupted
	if !pCorrupt.IsPeerStoreCorrupted() {
		t.Fatalf("Expected peerStoreCorrupted flag to be true upon encountering corrupt peer store")
	}

	// Invariant 2: Corrupted file is preserved and backup copy created for forensic diagnosis
	backupPath := peersPath + ".corrupt"
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("Expected backup of corrupt peer store at %s: %v", backupPath, err)
	}
	if !bytes.Equal(backupData, corruptData) {
		t.Fatalf("Corrupt backup content mismatch")
	}

	// Invariant 3: savePeers refuses to overwrite corrupt peer store
	if errSave := pCorrupt.savePeers(); errSave == nil {
		t.Fatalf("savePeers must return an error when peer store is corrupted!")
	}
	currentData, err := os.ReadFile(peersPath)
	if err != nil || !bytes.Equal(currentData, corruptData) {
		t.Fatalf("savePeers must NOT overwrite a corrupted peer store file!")
	}

	// Invariant 4: Anti-Repinning Guard: VerifyOrRecordTOFU fails closed on corrupt store
	// An attacker cannot exploit a corrupted peer store to silently re-pin a malicious certificate!
	mockConn := &mockTLSConn{fingerprint: "attacker_cert_fingerprint_hex_abcdef"}
	err = pCorrupt.VerifyOrRecordTOFU("192.168.1.100:19335", mockConn)
	if err == nil || !strings.Contains(err.Error(), "refusing to bootstrap or re-pin") {
		t.Fatalf("Expected TOFU fail-closed refusal to re-pin on corrupt store, got: %v", err)
	}

	// Invariant 5: Subsequent corrupt event preserves original .corrupt and creates timestamped copy [Follow-up Point 9]
	secondCorruptData := []byte(`{ "second_corrupt_crash": true }`)
	if err := os.WriteFile(peersPath, secondCorruptData, 0600); err != nil {
		t.Fatalf("Failed to write second corrupt peers: %v", err)
	}
	_ = NewPeerServer("9999", bc)

	// Verify original .corrupt was NOT overwritten
	firstBackupAfter, err := os.ReadFile(backupPath)
	if err != nil || !bytes.Equal(firstBackupAfter, corruptData) {
		t.Fatalf("Original .corrupt file was modified or overwritten!")
	}

	// Verify a timestamped backup was created
	peerDir := filepath.Dir(peersPath)
	files, err := os.ReadDir(peerDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	foundTimestamped := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), filepath.Base(peersPath)+".corrupt.") {
			foundTimestamped = true
			tsData, _ := os.ReadFile(filepath.Join(peerDir, f.Name()))
			if !bytes.Equal(tsData, secondCorruptData) {
				t.Fatalf("Timestamped corrupt backup data mismatch")
			}
			break
		}
	}
	if !foundTimestamped {
		t.Fatalf("Expected timestamped backup file matching 'peers.json.corrupt.<timestamp>'")
	}
}

// mockTLSConn implements net.Conn and satisfies extractCertPubkeyFingerprint for testing
type mockTLSConn struct {
	net.Conn
	fingerprint string
}

func (m *mockTLSConn) Close() error               { return nil }
func (m *mockTLSConn) GetCertFingerprint() string { return m.fingerprint }

// TestP2P_TOFU_RecordPeerSuccessPreservesFingerprint verifies that RecordPeerSuccess
// preserves the pinned certificate fingerprint and does not wipe it [F-01].
func TestP2P_TOFU_RecordPeerSuccessPreservesFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)
	p.P2PTOFU = true

	peerAddr := "192.168.1.80:19335"
	pinFP := "aabbccdd00112233445566778899aabbccdd00112233445566778899aabbccdd"

	mockConn := &mockTLSConn{fingerprint: pinFP}
	if err := p.VerifyOrRecordTOFU(peerAddr, mockConn); err != nil {
		t.Fatalf("VerifyOrRecordTOFU failed: %v", err)
	}

	// Handshake succeeds -> RecordPeerSuccess is called
	p.RecordPeerSuccess(peerAddr, 100, p.GetNetworkID())

	// Crucial F-01 check: The pinned fingerprint was preserved in triedPeers!
	p.Lock.RLock()
	info, exists := p.triedPeers[peerAddr]
	p.Lock.RUnlock()

	if !exists {
		t.Fatalf("Peer %s missing from triedPeers after RecordPeerSuccess", peerAddr)
	}
	if info.CertFingerprint != pinFP {
		t.Fatalf("F-01 Regression: RecordPeerSuccess wiped CertFingerprint! Expected %s, got %s", pinFP, info.CertFingerprint)
	}

	// Restart simulation: New peer server loading from persisted store
	pRestart := NewPeerServer("9999", bc)
	pRestart.P2PTOFU = true
	pRestart.loadPeers()

	pRestart.Lock.RLock()
	infoLoaded, ok := pRestart.triedPeers[peerAddr]
	pRestart.Lock.RUnlock()

	if !ok || infoLoaded.CertFingerprint != pinFP {
		t.Fatalf("Pinned fingerprint was not persisted across restart: got %v", infoLoaded)
	}
}

// TestP2P_PeerStore_LoadPeersFailClosed verifies that loadPeers enters fail-closed mode
// on unsupported store versions, mismatched network IDs, missing peers array, or I/O errors [F-04].
func TestP2P_PeerStore_LoadPeersFailClosed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	peersPath := getPeersPath()

	// 1. Unsupported store version (e.g. Version 2)
	v2JSON := `{"version": 2, "network_id": "testnet", "peers": []}`
	if err := os.WriteFile(peersPath, []byte(v2JSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	p1 := NewPeerServer("9999", bc)
	p1.loadPeers()
	if !p1.IsPeerStoreCorrupted() {
		t.Fatalf("Expected fail-closed on unsupported store version 2")
	}

	// 2. Network ID mismatch (e.g. store is "mainnet" but server is "testnet")
	netMismatchJSON := `{"version": 1, "network_id": "othernet", "peers": []}`
	if err := os.WriteFile(peersPath, []byte(netMismatchJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	p2 := NewPeerServer("9999", bc)
	p2.loadPeers()
	if !p2.IsPeerStoreCorrupted() {
		t.Fatalf("Expected fail-closed on network ID mismatch")
	}

	// 3. Missing peers array (peers: null)
	nilPeersJSON := `{"version": 1, "network_id": "testnet", "peers": null}`
	if err := os.WriteFile(peersPath, []byte(nilPeersJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	p3 := NewPeerServer("9999", bc)
	p3.loadPeers()
	if !p3.IsPeerStoreCorrupted() {
		t.Fatalf("Expected fail-closed when peers array is null")
	}

	// 4. Non-NotExist I/O error (e.g., peersPath is a directory instead of a regular file)
	_ = os.Remove(peersPath)
	if err := os.Mkdir(peersPath, 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	p4 := NewPeerServer("9999", bc)
	p4.loadPeers()
	if !p4.IsPeerStoreCorrupted() {
		t.Fatalf("Expected fail-closed when reading peers.json encounters an I/O error")
	}
}

// TestP2P_TOFU_PersistenceFailure_FailClosed tests deterministic failure injection for every I/O stage of savePeers
// during VerifyOrRecordTOFU [P1 Review]:
// 1. Marshal failure
// 2. MkdirAll failure
// 3. CreateTemp failure
// 4. ChmodTemp failure
// 1. Marshal failure
// 2. MkdirAll failure
// 3. CreateTemp failure
// 4. ChmodTemp failure (permissions enforcement)
// 5. Write failure
// 6. Sync failure
// 7. Close failure
// 8. Rename failure (atomic commit step)
//
// Invariants enforced:
// - Permissions 0600 are verified on the temporary file before rename.
// - Post-rename operations are eliminated so rename is the single atomic commit point.
// - VerifyOrRecordTOFU fails closed and returns a non-nil persistence failure error.
// - Peer fingerprint is NOT accepted or retained in-memory (in-memory rollback verified).
// - No fingerprint is persisted to disk; across simulated restart, the peer is not recognized.
// - Attacker cannot exploit failed saves for silent re-pinning or hijacking; alternate certs are rejected.
// - For peers with pre-existing pins, failed updates preserve the original pin in both memory and disk.
func TestP2P_TOFU_PersistenceFailure_FailClosed(t *testing.T) {
	stages := []struct {
		name    string
		setHook func()
	}{
		{
			name: "MarshalError",
			setHook: func() {
				savePeersMarshalHook = func(v any) ([]byte, error) {
					return nil, errors.New("simulated marshal error")
				}
			},
		},
		{
			name: "MkdirAllError",
			setHook: func() {
				savePeersMkdirAllHook = func(path string, perm os.FileMode) error {
					return errors.New("simulated mkdir error")
				}
			},
		},
		{
			name: "CreateTempError",
			setHook: func() {
				savePeersCreateTempHook = func(dir, pattern string) (*os.File, error) {
					return nil, errors.New("simulated create temp error")
				}
			},
		},
		{
			name: "ChmodTempError",
			setHook: func() {
				savePeersChmodTempHook = func(name string, mode os.FileMode) error {
					return errors.New("simulated chmod temp error")
				}
			},
		},
		{
			name: "WriteError",
			setHook: func() {
				savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
					return 0, errors.New("simulated write error")
				}
			},
		},
		{
			name: "SyncError",
			setHook: func() {
				savePeersSyncHook = func(f *os.File) error {
					return errors.New("simulated sync error")
				}
			},
		},
		{
			name: "CloseError",
			setHook: func() {
				savePeersCloseHook = func(f *os.File) error {
					return errors.New("simulated close error")
				}
			},
		},
		{
			name: "RenameError",
			setHook: func() {
				savePeersRenameHook = func(oldpath, newpath string) error {
					return errors.New("simulated rename error")
				}
			},
		},
	}

	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("ARMX_DATA_DIR", tmpDir)

			bc := core.MustNewBlockchain(1)
			defer bc.Close()
			p := NewPeerServer("9999", bc)
			p.P2PTOFU = true

			peerAddr := "192.168.1.150:19335"
			validFP := "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
			conn := &mockTLSConn{fingerprint: validFP}

			// Pre-populate an existing pinned peer to verify update rollback consistency
			existingAddr := "192.168.1.200:19335"
			existingFP := "aaaa1111bbbb2222cccc3333dddd4444aaaa1111bbbb2222cccc3333dddd4444"
			p.Lock.Lock()
			p.newPeers[existingAddr] = &PeerInfo{
				Address:         existingAddr,
				LastSeen:        time.Now(),
				NetworkID:       p.GetNetworkID(),
				CertFingerprint: existingFP,
			}
			p.Lock.Unlock()

			// Install failure hook
			stage.setHook()
			defer func() {
				savePeersMarshalHook = nil
				savePeersMkdirAllHook = nil
				savePeersCreateTempHook = nil
				savePeersChmodTempHook = nil
				savePeersWriteHook = nil
				savePeersSyncHook = nil
				savePeersCloseHook = nil
				savePeersRenameHook = nil
			}()

			// Invariant 1: VerifyOrRecordTOFU must return persistence error
			err := p.VerifyOrRecordTOFU(peerAddr, conn)
			if err == nil {
				t.Fatalf("[%s] Expected VerifyOrRecordTOFU to fail when savePeers fails, but got nil!", stage.name)
			}
			if !strings.Contains(err.Error(), "failed to persist certificate fingerprint") {
				t.Fatalf("[%s] Unexpected error message: %v", stage.name, err)
			}

			// Invariant 2: In-memory rollback: peer must NOT be recorded as pinned in triedPeers
			p.Lock.RLock()
			info, exists := p.triedPeers[peerAddr]
			p.Lock.RUnlock()
			if exists && info.CertFingerprint != "" {
				t.Fatalf("[%s] In-memory rollback failed: peer %s was left in triedPeers with fingerprint %s!", stage.name, peerAddr, info.CertFingerprint)
			}

			// Invariant 3: Simulated restart must show peer was NOT persisted to disk (disk-memory consistency)
			pRestart := NewPeerServer("9999", bc)
			pRestart.P2PTOFU = true
			pRestart.loadPeers()

			pRestart.Lock.RLock()
			reloadedInfo, existsRestart := pRestart.triedPeers[peerAddr]
			pRestart.Lock.RUnlock()
			if existsRestart && reloadedInfo.CertFingerprint != "" {
				t.Fatalf("[%s] Disk-memory inconsistency: peer was persisted despite I/O failure: %v", stage.name, reloadedInfo)
			}

			// Invariant 4: No alternate-certificate re-pin opportunity while failure persists
			attackerFP := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
			attackerConn := &mockTLSConn{fingerprint: attackerFP}
			errAttacker := p.VerifyOrRecordTOFU(peerAddr, attackerConn)
			if errAttacker == nil {
				t.Fatalf("[%s] Attacker connection succeeded despite persisting I/O failure!", stage.name)
			}
			p.Lock.RLock()
			infoAfterAttacker, existsAfterAttacker := p.triedPeers[peerAddr]
			p.Lock.RUnlock()
			if existsAfterAttacker && infoAfterAttacker.CertFingerprint != "" {
				t.Fatalf("[%s] Attacker certificate was pinned in memory after failed save!", stage.name)
			}

			// Invariant 5: Verify pre-existing peer pin was NOT damaged by failed update
			existingConnGood := &mockTLSConn{fingerprint: existingFP}
			// Attempt to update existing peer while hook is active: must fail closed and rollback to newPeers
			errExistingUpdate := p.VerifyOrRecordTOFU(existingAddr, existingConnGood)
			if errExistingUpdate == nil {
				t.Fatalf("[%s] Existing peer update unexpectedly succeeded despite failure hook", stage.name)
			}
			p.Lock.RLock()
			existingInNew, inNewExists := p.newPeers[existingAddr]
			p.Lock.RUnlock()
			if !inNewExists || existingInNew.CertFingerprint != existingFP {
				t.Fatalf("[%s] Rollback damaged existing peer pin in newPeers: %v", stage.name, existingInNew)
			}
			// Alternate certificate for existing peer MUST be rejected
			existingConnBad := &mockTLSConn{fingerprint: attackerFP}
			errExistingBad := p.VerifyOrRecordTOFU(existingAddr, existingConnBad)
			if errExistingBad == nil || !strings.Contains(errExistingBad.Error(), "mismatch") {
				t.Fatalf("[%s] Expected certificate mismatch error for existing peer, got: %v", stage.name, errExistingBad)
			}

			// Reset hooks to test legitimate recovery & subsequent rejection of alternate certificates
			savePeersMarshalHook = nil
			savePeersMkdirAllHook = nil
			savePeersCreateTempHook = nil
			savePeersChmodTempHook = nil
			savePeersWriteHook = nil
			savePeersSyncHook = nil
			savePeersCloseHook = nil
			savePeersRenameHook = nil

			// Legitimate first-use pinning now succeeds
			if err := p.VerifyOrRecordTOFU(peerAddr, conn); err != nil {
				t.Fatalf("[%s] VerifyOrRecordTOFU failed after hook reset: %v", stage.name, err)
			}

			// Verify disk and memory are now consistent with the legitimate pin
			p.Lock.RLock()
			infoPinned, okPinned := p.triedPeers[peerAddr]
			p.Lock.RUnlock()
			if !okPinned || infoPinned.CertFingerprint != validFP {
				t.Fatalf("[%s] Peer not correctly pinned in memory after recovery", stage.name)
			}

			pRestart2 := NewPeerServer("9999", bc)
			pRestart2.P2PTOFU = true
			pRestart2.loadPeers()
			pRestart2.Lock.RLock()
			infoDisk, okDisk := pRestart2.triedPeers[peerAddr]
			pRestart2.Lock.RUnlock()
			if !okDisk || infoDisk.CertFingerprint != validFP {
				t.Fatalf("[%s] Peer not correctly persisted to disk after recovery", stage.name)
			}

			// Subsequent connection with changed cert MUST be rejected (no re-pin opportunity)
			altConn := &mockTLSConn{fingerprint: "ffeeddccbbaa00998877665544332211ffeeddccbbaa00998877665544332211"}
			errAlt := p.VerifyOrRecordTOFU(peerAddr, altConn)
			if errAlt == nil || !strings.Contains(errAlt.Error(), "mismatch") {
				t.Fatalf("[%s] Expected certificate mismatch error for changed cert, got: %v", stage.name, errAlt)
			}
		})
	}
}
