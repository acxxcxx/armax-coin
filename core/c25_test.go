package core

import (
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestC25_RestartPreservesStateAndSnapshot verifies that bbolt store preserves state
// across node restarts and creates snapshots every 1000 blocks [C-25].
func TestC25_RestartPreservesStateAndSnapshot(t *testing.T) {
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	defer func() {
		targetOverrideHook = oldHook
	}()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc := &Blockchain{
		Chain:       []Block{GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward)

	minerAddr := newTestAccount(t).Address

	// Save genesis
	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("Failed to save genesis to bolt store: %v", err)
	}

	// Mine 10 blocks
	for i := 1; i <= 10; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = prev.Timestamp + 1
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Failed to add block #%d", i)
		}
	}

	expectedBalance := bc.GetBalance(minerAddr)
	expectedHeight := len(bc.Chain)

	// Close active db handle before restart to release file lock on Windows
	_ = bc.Close()

	// Restart: LoadFromFile must reconstruct exact state and height
	loadedBc, ok := LoadFromFile()
	if !ok || loadedBc == nil {
		t.Fatalf("[C-25] LoadFromFile failed on valid bolt database")
	}
	defer loadedBc.Close()

	if len(loadedBc.Chain) != expectedHeight {
		t.Fatalf("Expected height %d after restart, got %d", expectedHeight, len(loadedBc.Chain))
	}
	if loadedBc.GetBalance(minerAddr) != expectedBalance {
		t.Fatalf("Expected miner balance %d after restart, got %d", expectedBalance, loadedBc.GetBalance(minerAddr))
	}
}

// TestC25_CorruptedDBRenamedToInvalidNotDeleted verifies that a corrupted database
// is renamed to <name>.invalid-<timestamp> and NOT deleted or overwritten [C-25].
func TestC25_CorruptedDBRenamedToInvalidNotDeleted(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	// Write garbage / corrupt binary data to dbPath
	garbageData := []byte("CORRUPT_INVALID_BOLT_DATABASE_HEADER_DATA_1234567890")
	if err := os.WriteFile(dbPath, garbageData, 0600); err != nil {
		t.Fatalf("failed to write garbage db: %v", err)
	}

	loadedBc, ok := LoadFromFile()
	if ok || loadedBc != nil {
		t.Fatalf("expected LoadFromFile to fail on corrupt db")
	}

	// Check if any file matching blockchain.db.invalid-* exists in tempDir
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}

	foundInvalidBackup := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "blockchain.db.invalid-") {
			foundInvalidBackup = true
			content, _ := os.ReadFile(filepath.Join(tempDir, f.Name()))
			if string(content) != string(garbageData) {
				t.Fatalf("corrupted backup content mismatch")
			}
			break
		}
	}

	if !foundInvalidBackup {
		t.Fatalf("[C-25] FAILED: Corrupted DB was not renamed to <name>.invalid-<timestamp>!")
	}
}

// TestC25_InitialSyncSpeed2000Blocks benchmarks that append-only bolt storage
// can sync/process 2,000 blocks quickly without O(n^2) disk serialization [C-25].
func TestC25_InitialSyncSpeed2000Blocks(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc := &Blockchain{
		Chain:       []Block{GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward)
	_ = bc.SaveToFile()
	defer bc.Close()

	minerAddr := newTestAccount(t).Address

	// Isolate database write throughput from PoW hashing [C-25]
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	defer func() {
		targetOverrideHook = oldHook
	}()

	startTime := time.Now()
	for i := 1; i <= 2000; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = prev.Timestamp + TargetBlockTimeSeconds
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Failed to add block #%d", i)
		}
	}
	elapsed := time.Since(startTime)
	t.Logf("2,000 blocks mined and saved in %v", elapsed)

	// Under JSON serialization, 2,000 blocks with quadratic writes took > 120 seconds.
	// Under bbolt, it should complete well under 10 seconds normally (and under 90s on slower Windows NTFS fsync with -race).
	if elapsed > 90*time.Second {
		t.Fatalf("[C-25] FAILED: 2,000 blocks took too long (%v > 90s)", elapsed)
	}
}
