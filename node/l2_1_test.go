package node

import (
	"os"
	"path/filepath"
	"testing"
)

// TestL2_1_TestIsolation verifies that running node tests does not drop armax.cookie
// or database files into repository source directories [L2-1]
func TestL2_1_TestIsolation(t *testing.T) {
	// 1. Verify working directory has no stray armax.cookie
	if _, err := os.Stat("armax.cookie"); err == nil {
		t.Fatalf("Found stray armax.cookie in working directory! Tests must isolate file writes.")
	}

	// 2. Verify root and node directories have no stray database files
	candidates := []string{
		"blockchain.db",
		filepath.Join("node", "armax.cookie"),
		filepath.Join("node", "blockchain.db"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("Found stray artifact %s! Tests must isolate file writes to t.TempDir().", path)
		}
	}
}
