package core

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestT12_ProductionRejectsSilentInMemoryFallback verifies that database open errors
// do not silently fall back to ephemeral in-memory storage in production/testnet mode [T12].
func TestT12_ProductionRejectsSilentInMemoryFallback(t *testing.T) {
	// Create an unwritable / directory conflict path
	tempDir := t.TempDir()
	invalidDbPath := filepath.Join(tempDir, "cannot_create_dir", "blockchain.db")

	// Directly calling NewBlockchainWithStoragePath on invalid path must return error, not fallback
	bc, err := NewBlockchainWithStoragePath(1, invalidDbPath)
	if err == nil {
		if bc != nil && bc.StoragePath == "IN_MEMORY" {
			t.Fatal("FATAL: NewBlockchainWithStoragePath silently fell back to in-memory mode on disk failure")
		}
		t.Fatal("Expected error when initializing blockchain on invalid storage path, got nil")
	}

	// Verify error message indicates storage failure
	if !strings.Contains(err.Error(), "stat database") && !strings.Contains(err.Error(), "failed to persist") && !strings.Contains(err.Error(), "open database") {
		t.Fatalf("Expected database open/stat error, got: %v", err)
	}
}
