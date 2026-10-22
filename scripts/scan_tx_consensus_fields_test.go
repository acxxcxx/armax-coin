package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	bolt "go.etcd.io/bbolt"
)

// helper to build a test bbolt database with controlled blocks and metadata
func createTestDB(t *testing.T, dir string, tipHeight uint64, mutate func(tx *bolt.Tx) error) string {
	t.Helper()
	dbPath := filepath.Join(dir, "blockchain.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("Failed to create test db: %v", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		bktM, err := tx.CreateBucketIfNotExists(bktMeta)
		if err != nil {
			return err
		}
		if err := bktM.Put([]byte("tip_height"), uint64ToBytes(tipHeight)); err != nil {
			return err
		}

		bktB, err := tx.CreateBucketIfNotExists(bktBlocks)
		if err != nil {
			return err
		}

		gen := core.GetGenesisBlock()
		prevHash := gen.Hash
		genData, err := json.Marshal(gen)
		if err != nil {
			return err
		}
		if err := bktB.Put(uint64ToBytes(0), genData); err != nil {
			return err
		}

		for h := uint64(1); h <= tipHeight; h++ {
			b := core.Block{
				Index:      int(h),
				Timestamp:  time.Now().Unix(),
				PrevHash:   prevHash,
				Hash:       fmt.Sprintf("blockhash_%d_%s", h, prevHash[:8]),
				Miner:      "test_miner",
				Difficulty: 1,
				Transactions: []core.Transaction{
					{
						Sender:    core.GenesisMiner,
						Recipient: "armx1testrecipient",
						Amount:    10 * core.CoinUnit,
						Fee:       1000,
						Type:      core.TxTypeStandard,
					},
				},
			}
			prevHash = b.Hash
			bData, err := json.Marshal(b)
			if err != nil {
				return err
			}
			if err := bktB.Put(uint64ToBytes(h), bData); err != nil {
				return err
			}
		}

		if mutate != nil {
			return mutate(tx)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}
	return dbPath
}

func runScanner(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmdArgs := append([]string{"run", "scan_tx_consensus_fields.go"}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = "."

	outBytes, err := cmd.CombinedOutput()
	output := string(outBytes)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return output, "", exitCode
}

// TestConsensusScanner_GenesisOnly verifies that scanning without a DB reports GENESIS_ONLY
// and explicitly states full-chain compatibility is not verified.
func TestConsensusScanner_GenesisOnly(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDB := filepath.Join(tmpDir, "missing.db")

	out, _, code := runScanner(t, "-db", nonExistentDB)
	if code != 0 {
		t.Fatalf("Expected exit code 0 for default genesis-only scan, got %d. Output:\n%s", code, out)
	}

	if !strings.Contains(out, "GENESIS ONLY -- FULL-CHAIN COMPATIBILITY NOT VERIFIED") {
		t.Fatalf("Output missing explicit genesis-only disclaimer. Output:\n%s", out)
	}
	if strings.Contains(out, "100% backward-compatible with all existing canonical blocks") {
		t.Fatalf("Genesis-only scan must NOT claim full chain backward-compatibility! Output:\n%s", out)
	}
	if !strings.Contains(out, "Scan Mode            : GENESIS_ONLY") {
		t.Fatalf("Output missing Scan Mode: GENESIS_ONLY. Output:\n%s", out)
	}
}

// TestConsensusScanner_RequireFullChain_NoDB verifies that --require-full-chain exits nonzero
// when no database exists.
func TestConsensusScanner_RequireFullChain_NoDB(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDB := filepath.Join(tmpDir, "missing.db")

	out, _, code := runScanner(t, "-db", nonExistentDB, "--require-full-chain")
	if code == 0 {
		t.Fatalf("Expected nonzero exit code when --require-full-chain is specified without DB. Output:\n%s", out)
	}
	if !strings.Contains(out, "--require-full-chain specified, but no database exists") {
		t.Fatalf("Expected error message about missing database under --require-full-chain. Output:\n%s", out)
	}
}

// TestConsensusScanner_FullValidHistory verifies that scanning a valid database reports FULL_CHAIN
// with 0 violations and exit code 0.
func TestConsensusScanner_FullValidHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createTestDB(t, tmpDir, 3, nil)

	out, _, code := runScanner(t, "-db", dbPath, "--require-full-chain")
	if code != 0 {
		t.Fatalf("Expected exit code 0 for valid chain history, got %d. Output:\n%s", code, out)
	}
	if !strings.Contains(out, "Scan Mode            : FULL_CHAIN") {
		t.Fatalf("Expected Scan Mode: FULL_CHAIN. Output:\n%s", out)
	}
	if !strings.Contains(out, "0 violations found across all canonical blocks") {
		t.Fatalf("Expected 0 violations summary. Output:\n%s", out)
	}
	if !strings.Contains(out, "Total Blocks Scanned : 4") {
		t.Fatalf("Expected 4 blocks scanned (0 to 3). Output:\n%s", out)
	}
}

// TestConsensusScanner_ViolationPresent verifies that a transaction violating consensus rules
// causes the scanner to report the violation and exit nonzero.
func TestConsensusScanner_ViolationPresent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createTestDB(t, tmpDir, 2, func(tx *bolt.Tx) error {
		// Mutate block 2 to inject an HTLC field violation into a standard transaction
		bktB := tx.Bucket(bktBlocks)
		b2Data := bktB.Get(uint64ToBytes(2))
		var b core.Block
		if err := json.Unmarshal(b2Data, &b); err != nil {
			return err
		}
		b.Transactions[0].HashLock = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		mutatedData, err := json.Marshal(b)
		if err != nil {
			return err
		}
		return bktB.Put(uint64ToBytes(2), mutatedData)
	})

	out, _, code := runScanner(t, "-db", dbPath)
	if code == 0 {
		t.Fatalf("Expected nonzero exit code when consensus violation is present! Output:\n%s", out)
	}
	if !strings.Contains(out, "non-standard consensus violations detected") {
		t.Fatalf("Expected violations header in output. Output:\n%s", out)
	}
	if !strings.Contains(out, "HashLock=") {
		t.Fatalf("Expected HashLock violation details in output. Output:\n%s", out)
	}
}

// TestConsensusScanner_CorruptDB verifies that opening a corrupt/invalid database file fails closed.
func TestConsensusScanner_CorruptDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupt.db")
	if err := os.WriteFile(dbPath, []byte("NOT_A_VALID_BOLT_DATABASE_HEADER"), 0600); err != nil {
		t.Fatalf("Failed to write corrupt DB: %v", err)
	}

	out, _, code := runScanner(t, "-db", dbPath)
	if code == 0 {
		t.Fatalf("Expected nonzero exit code for corrupt database. Output:\n%s", out)
	}
	if !strings.Contains(out, "Failed to open database") {
		t.Fatalf("Expected database open error. Output:\n%s", out)
	}
}

// TestConsensusScanner_MissingHeight verifies that a gap in block height continuity is caught and fails closed.
func TestConsensusScanner_MissingHeight(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createTestDB(t, tmpDir, 3, func(tx *bolt.Tx) error {
		// Delete block at height 2, leaving a gap between 1 and 3
		bktB := tx.Bucket(bktBlocks)
		return bktB.Delete(uint64ToBytes(2))
	})

	out, _, code := runScanner(t, "-db", dbPath)
	if code == 0 {
		t.Fatalf("Expected nonzero exit code for missing block height. Output:\n%s", out)
	}
	if !strings.Contains(out, "block continuity error: missing block at height 2") {
		t.Fatalf("Expected block continuity error for missing height 2. Output:\n%s", out)
	}
}

// TestConsensusScanner_MissingMetaBucket verifies that a database missing the meta bucket fails closed.
func TestConsensusScanner_MissingMetaBucket(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createTestDB(t, tmpDir, 1, func(tx *bolt.Tx) error {
		return tx.DeleteBucket(bktMeta)
	})

	out, _, code := runScanner(t, "-db", dbPath)
	if code == 0 {
		t.Fatalf("Expected nonzero exit code when meta bucket is missing. Output:\n%s", out)
	}
	if !strings.Contains(out, "required metadata bucket 'meta' not found") {
		t.Fatalf("Expected missing meta bucket error. Output:\n%s", out)
	}
}
