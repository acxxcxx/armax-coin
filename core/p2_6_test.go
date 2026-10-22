package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// TestP2_6_LegacySchemaVersionDatabaseRejected verifies that database with legacy schema
// (e.g. v1 or missing schema_version) is rejected and renamed to .invalid-<timestamp> [P2-6]
func TestP2_6_LegacySchemaVersionDatabaseRejected(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := GetDBPath()
	_ = os.MkdirAll(filepath.Dir(dbPath), 0755)

	// 1. Create a mock database with schema version 1 (legacy)
	db, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		bktM := tx.Bucket(bktMeta)
		// Set legacy schema version 1
		_ = bktM.Put(keySchemaVersion, uint64ToBytes(1))
		_ = bktM.Put(keyTipHeight, uint64ToBytes(0))

		bktB := tx.Bucket(bktBlocks)
		gen := GetGenesisBlock()
		genBytes, _ := json.Marshal(gen)
		return bktB.Put(uint64ToBytes(0), genBytes)
	})
	if err != nil {
		t.Fatalf("Failed to initialize mock legacy database: %v", err)
	}
	_ = db.Close()

	// 2. LoadFromFile must reject the database due to schema version mismatch
	loadedBc, ok := LoadFromFile()
	if ok || loadedBc != nil {
		t.Fatalf("LoadFromFile succeeded on legacy v1 database; expected rejection!")
	}

	// 3. Verify original database file is gone and moved to .invalid-<timestamp>
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatalf("Original database %s was NOT moved after schema rejection!", dbPath)
	}

	entries, err := os.ReadDir(filepath.Dir(dbPath))
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	foundInvalid := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "blockchain.db.invalid-") {
			foundInvalid = true
			break
		}
	}
	if !foundInvalid {
		t.Fatalf("No .invalid-<timestamp> file found in directory after schema rejection")
	}
}

// TestP2_6_LegacyGenesisDatabaseRejected verifies that database with mismatched/legacy genesis
// is rejected and preserved under .invalid-<timestamp> [P2-6]
func TestP2_6_LegacyGenesisDatabaseRejected(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	dbPath := GetDBPath()
	_ = os.MkdirAll(filepath.Dir(dbPath), 0755)

	// 1. Create a database with CurrentSchemaVersion but a fake/tampered genesis block
	db, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		bktM := tx.Bucket(bktMeta)
		_ = bktM.Put(keySchemaVersion, uint64ToBytes(CurrentSchemaVersion))
		_ = bktM.Put(keyTipHeight, uint64ToBytes(0))

		bktB := tx.Bucket(bktBlocks)
		fakeGenesis := GetGenesisBlock()
		fakeGenesis.Hash = "0000000000000000deadbeefcafebabedeadbeefcafebabedeadbeefcafebabe"
		genBytes, _ := json.Marshal(fakeGenesis)
		return bktB.Put(uint64ToBytes(0), genBytes)
	})
	if err != nil {
		t.Fatalf("Failed to initialize tampered genesis database: %v", err)
	}
	_ = db.Close()

	// 2. LoadFromFile must reject the database due to genesis mismatch
	loadedBc, ok := LoadFromFile()
	if ok || loadedBc != nil {
		t.Fatalf("LoadFromFile succeeded on tampered genesis database; expected rejection!")
	}

	// 3. Verify original database file is renamed to .invalid-<timestamp>
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatalf("Original database %s was NOT moved after genesis rejection!", dbPath)
	}

	entries, err := os.ReadDir(filepath.Dir(dbPath))
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	foundInvalid := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "blockchain.db.invalid-") {
			foundInvalid = true
			break
		}
	}
	if !foundInvalid {
		t.Fatalf("No .invalid-<timestamp> file found after genesis mismatch rejection")
	}
}

// TestP2_6_CurrentSchemaPersistenceAndReload verifies that a clean database saved with CurrentSchemaVersion
// is loaded and verified cleanly [P2-6]
func TestP2_6_CurrentSchemaPersistenceAndReload(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := NewBlockchain(1)
	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
	bc.Close() // Release exclusive file lock on Windows

	// Verify schema_version in bolt file directly
	dbPath := GetDBPath()
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	var ver uint64
	_ = db.View(func(tx *bolt.Tx) error {
		bktM := tx.Bucket(bktMeta)
		if bktM != nil {
			vBytes := bktM.Get(keySchemaVersion)
			if vBytes != nil {
				ver = bytesToUint64(vBytes)
			}
		}
		return nil
	})
	_ = db.Close()

	if ver != CurrentSchemaVersion {
		t.Fatalf("Expected stored schema version %d, got %d", CurrentSchemaVersion, ver)
	}

	// Now reload via LoadFromFile
	loadedBc, ok := LoadFromFile()
	if !ok || loadedBc == nil {
		t.Fatalf("LoadFromFile failed to reload valid v2 database")
	}
	defer loadedBc.Close()

	if loadedBc.GetLatestBlock().Hash != bc.GetLatestBlock().Hash {
		t.Fatalf("Reloaded genesis mismatch: expected %s, got %s", bc.GetLatestBlock().Hash, loadedBc.GetLatestBlock().Hash)
	}
}
