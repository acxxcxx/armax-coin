package node

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestAtomicPersistence_CookieConcurrentWrites verifies that concurrent calls to InitRPCAuth
// write atomically using unique temporary files without data corruption or temp file leaks [Follow-up Point 6].
func TestAtomicPersistence_CookieConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	concurrency := 10
	var wg sync.WaitGroup
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := InitRPCAuth("testuser", "testpass"); err != nil {
				errChan <- fmt.Errorf("worker %d InitRPCAuth failed: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)
	for err := range errChan {
		t.Fatalf("Concurrent InitRPCAuth error: %v", err)
	}

	// Verify no stray .tmp files remain anywhere in tempDir
	err := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".tmp" {
			return fmt.Errorf("leaked temporary file detected: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Temporary file leak check failed: %v", err)
	}
}

// TestAtomicPersistence_PeerStoreConcurrentWrites verifies that concurrent calls to savePeers
// write atomically using unique temporary files without data loss or corruption [P1 Final Blocker].
func TestAtomicPersistence_PeerStoreConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer("19335", bc)

	concurrency := 15
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var saveErrors []error

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			addr := fmt.Sprintf("192.168.1.%d:19335", id+10)
			ps.Lock.Lock()
			ps.newPeers[addr] = &PeerInfo{
				Address:         addr,
				LastSeen:        time.Now(),
				NetworkID:       ps.GetNetworkID(),
				CertFingerprint: fmt.Sprintf("fingerprint_%d", id),
			}
			ps.Lock.Unlock()
			if err := ps.savePeers(); err != nil {
				errMu.Lock()
				saveErrors = append(saveErrors, fmt.Errorf("writer %d failed: %w", id, err))
				errMu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if len(saveErrors) > 0 {
		t.Fatalf("Concurrent savePeers produced %d error(s): %v", len(saveErrors), saveErrors)
	}

	// Verify that the resulting peers.json can be reloaded cleanly
	psReload := NewPeerServer("19335", bc)
	psReload.loadPeers()

	if psReload.IsPeerStoreCorrupted() {
		t.Fatalf("Peer store became corrupted under concurrent writes!")
	}

	psReload.Lock.RLock()
	peerCount := len(psReload.newPeers) + len(psReload.triedPeers)
	psReload.Lock.RUnlock()

	// Assert reloaded peer count equals the number inserted [P1 Final Blocker]
	if peerCount != concurrency {
		t.Fatalf("Expected %d peers to be saved and reloaded, got %d", concurrency, peerCount)
	}

	// Assert every expected fingerprint exists [P1 Final Blocker]
	for i := 0; i < concurrency; i++ {
		expectedAddr := fmt.Sprintf("192.168.1.%d:19335", i+10)
		expectedFP := fmt.Sprintf("fingerprint_%d", i)

		psReload.Lock.RLock()
		info, exists := psReload.newPeers[expectedAddr]
		if !exists {
			info, exists = psReload.triedPeers[expectedAddr]
		}
		psReload.Lock.RUnlock()

		if !exists {
			t.Fatalf("Expected peer %s not found in reloaded peer store!", expectedAddr)
		}
		if info.CertFingerprint != expectedFP {
			t.Fatalf("Peer %s has mismatched fingerprint: expected %s, got %s", expectedAddr, expectedFP, info.CertFingerprint)
		}
	}

	// Verify no stray .tmp files in data dir
	dataDir := filepath.Join(tempDir, ps.GetNetworkID())
	entries, err := os.ReadDir(dataDir)
	if err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".tmp" {
				t.Fatalf("Leaked temporary file in peer store dir: %s", e.Name())
			}
		}
	}
}

// TestPeerStore_SerializedPersistence_NoStaleOverwrite is a deterministic concurrency regression test.
// It verifies that when an older writer is delayed mid-transaction, a subsequent mutation and save
// cannot be overwritten or lost by the delayed older writer [P1 Final Blocker].
func TestPeerStore_SerializedPersistence_NoStaleOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer("19335", bc)
	ps.P2PTOFU = true

	// Step 1: Insert initial peer A
	peerA := "192.168.1.50:19335"
	fpA := "fingerprint_peer_a_canonical"
	ps.Lock.Lock()
	ps.newPeers[peerA] = &PeerInfo{
		Address:         peerA,
		LastSeen:        time.Now(),
		NetworkID:       ps.GetNetworkID(),
		CertFingerprint: fpA,
	}
	ps.Lock.Unlock()

	var startOnce sync.Once
	writer1Started := make(chan struct{})
	writer1CanFinish := make(chan struct{})

	// Intercept write hook: pause Writer 1 after it has acquired savePeersLock and snapshotted peerA
	savePeersWriteHook = func(f *os.File, b []byte) (int, error) {
		startOnce.Do(func() {
			close(writer1Started)
			<-writer1CanFinish
		})
		return f.Write(b)
	}
	defer func() {
		savePeersWriteHook = nil
	}()

	var writer1Err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writer1Err = ps.savePeers()
	}()

	// Wait until Writer 1 is inside savePeers, holding savePeersLock with its snapshot of only Peer A
	select {
	case <-writer1Started:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for writer 1 to reach write hook")
	}

	// Step 2: Concurrently mutate peer store: insert Peer B with new TOFU fingerprint
	peerB := "192.168.1.60:19335"
	fpB := "fingerprint_peer_b_newer"
	ps.Lock.Lock()
	ps.newPeers[peerB] = &PeerInfo{
		Address:         peerB,
		LastSeen:        time.Now(),
		NetworkID:       ps.GetNetworkID(),
		CertFingerprint: fpB,
	}
	ps.Lock.Unlock()

	// Step 3: Launch Writer 2 to persist Peer B.
	// Because Writer 1 holds savePeersLock, Writer 2 must serialize and wait for Writer 1 to finish.
	var writer2Err error
	wg.Add(1)
	go func() {
		defer wg.Done()
		writer2Err = ps.savePeers()
	}()

	// Give Writer 2 a moment to attempt acquiring savePeersLock and block
	time.Sleep(50 * time.Millisecond)

	// Step 4: Allow Writer 1 to resume and finish its write and rename
	close(writer1CanFinish)
	wg.Wait()

	if writer1Err != nil {
		t.Fatalf("Writer 1 savePeers failed: %v", writer1Err)
	}
	if writer2Err != nil {
		t.Fatalf("Writer 2 savePeers failed: %v", writer2Err)
	}

	// Step 5: Reload peer store from disk; assert BOTH Peer A and Peer B are present with correct fingerprints
	psReload := NewPeerServer("19335", bc)
	psReload.P2PTOFU = true
	psReload.loadPeers()

	if psReload.IsPeerStoreCorrupted() {
		t.Fatalf("Peer store corrupted after serialized concurrent saves!")
	}

	psReload.Lock.RLock()
	infoA := psReload.triedPeers[peerA]
	if infoA == nil {
		infoA = psReload.newPeers[peerA]
	}
	infoB := psReload.triedPeers[peerB]
	if infoB == nil {
		infoB = psReload.newPeers[peerB]
	}
	totalCount := len(psReload.newPeers) + len(psReload.triedPeers)
	psReload.Lock.RUnlock()

	if totalCount != 2 {
		t.Fatalf("Expected exactly 2 peers in reloaded store, got %d", totalCount)
	}
	if infoA == nil || infoA.CertFingerprint != fpA {
		t.Fatalf("Peer A lost or corrupted: %v", infoA)
	}
	if infoB == nil || infoB.CertFingerprint != fpB {
		t.Fatalf("Peer B lost or corrupted by older writer! infoB: %v", infoB)
	}
}

// TestAtomicPersistence_P2PTLSUniqueTemp verifies that P2P TLS certificate persistence
// generates valid files and cleans up temporary files [Follow-up Point 6].
func TestAtomicPersistence_P2PTLSUniqueTemp(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	cert1, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate run 1 failed: %v", err)
	}

	cert2, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		t.Fatalf("loadOrGenerateNodeTLSCertificate run 2 failed: %v", err)
	}

	if cert1.Certificate[0] == nil || cert2.Certificate[0] == nil {
		t.Fatalf("Certificate DER bytes nil")
	}

	// Verify no stray .tmp files
	netDir := filepath.Join(tempDir, "testnet")
	entries, err := os.ReadDir(netDir)
	if err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".tmp" {
				t.Fatalf("Leaked temporary TLS file: %s", e.Name())
			}
		}
	}
}
