package node

import (
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT9_FastFailNonexistentTxAndBlockHashes verifies that queries for nonexistent
// or malformed hashes fail immediately without linear database/mempool scans [T9].
func TestT9_FastFailNonexistentTxAndBlockHashes(t *testing.T) {
	bc := core.NewBlockchain(1)
	defer bc.Close()

	// 1. Malformed block hash fails immediately
	_, err := bc.GetBlockByHash("short-invalid-hash")
	if err == nil || !strings.Contains(err.Error(), "invalid block hash format") {
		t.Fatalf("Expected invalid format error for short hash, got %v", err)
	}

	// 2. Malformed tx hash fails immediately
	_, _, _, _, err = bc.GetTransactionByHash("not-a-valid-hex")
	if err == nil || !strings.Contains(err.Error(), "invalid transaction hash format") {
		t.Fatalf("Expected invalid format error for non-hex hash, got %v", err)
	}

	// 3. Valid hex but nonexistent hash fast-fails in O(1) without iterating entire chain
	start := time.Now()
	nonExistent := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	_, err = bc.GetBlockByHash(nonExistent)
	if err == nil {
		t.Fatal("Expected error for non-existent block hash")
	}
	if duration := time.Since(start); duration > 50*time.Millisecond {
		t.Fatalf("Non-existent block hash lookup took %v, expected < 50ms", duration)
	}

	startTx := time.Now()
	_, _, _, _, err = bc.GetTransactionByHash(nonExistent)
	if err == nil {
		t.Fatal("Expected error for non-existent transaction hash")
	}
	if duration := time.Since(startTx); duration > 50*time.Millisecond {
		t.Fatalf("Non-existent tx hash lookup took %v, expected < 50ms", duration)
	}
}
