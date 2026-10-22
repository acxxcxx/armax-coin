//go:build testhooks

package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN21_ChainUnpaginatedBoundedTo50 verifies [N-21]:
// Unpaginated GET /chain must return at most 50 blocks to prevent memory exhaustion and DoS.
func TestN21_ChainUnpaginatedBoundedTo50(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)
	bc := core.NewBlockchain(1)
	defer bc.Close()

	// Mine 60 blocks to exceed 50
	for i := 1; i <= 60; i++ {
		_, err := bc.MineBlockWithErr(core.GenesisMiner)
		if err != nil {
			t.Fatalf("Failed to mine block %d: %v", i, err)
		}
	}

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{RPCBind: "127.0.0.1", RPCPort: core.MainnetRPCPort})

	req := httptest.NewRequest("GET", "/chain", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /chain, got %d: %s", w.Code, w.Body.String())
	}

	var blocks []core.Block
	if err := json.Unmarshal(w.Body.Bytes(), &blocks); err != nil {
		t.Fatalf("Failed to parse /chain response: %v", err)
	}

	if len(blocks) != 50 {
		t.Fatalf("[N-21] Expected /chain unpaginated to be bounded to 50 blocks, got %d", len(blocks))
	}
}

// TestN21_StatsUsesCachedChainworkAndTotalTx verifies [N-21]:
// /stats retrieves cumulative chainwork and total tx from core O(1) cached metrics.
func TestN21_StatsUsesCachedChainworkAndTotalTx(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)
	bc := core.NewBlockchain(1)
	defer bc.Close()

	// Mine 5 blocks
	for i := 1; i <= 5; i++ {
		_, _ = bc.MineBlockWithErr(core.GenesisMiner)
	}

	if bc.GetTotalTransactions() < 6 {
		t.Fatalf("[N-21] Expected GetTotalTransactions >= 6, got %d", bc.GetTotalTransactions())
	}

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{RPCBind: "127.0.0.1", RPCPort: core.MainnetRPCPort})
	req := httptest.NewRequest("GET", "/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /stats, got %d", w.Code)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse /stats JSON: %v", err)
	}

	totalTxVal, ok := stats["total_tx"].(float64)
	if !ok || int64(totalTxVal) != bc.GetTotalTransactions() {
		t.Fatalf("[N-21] Expected total_tx in /stats to match core.GetTotalTransactions (%d), got %v", bc.GetTotalTransactions(), stats["total_tx"])
	}
}

// TestN21_StoreIndexedTxAndBlockLookups verifies [N-21]:
// /tx?hash= and /block?hash= use store indexes for O(1) retrieval.
func TestN21_StoreIndexedTxAndBlockLookups(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)
	bc := core.NewBlockchain(1)
	defer bc.Close()

	b1, err := bc.MineBlockWithErr(core.GenesisMiner)
	if err != nil {
		t.Fatalf("Failed to mine block: %v", err)
	}
	if len(b1.Transactions) == 0 {
		t.Fatalf("Block 1 has no coinbase transaction")
	}
	coinbaseTx := b1.Transactions[0]

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{RPCBind: "127.0.0.1", RPCPort: core.MainnetRPCPort})

	// 1. Query /block?hash=
	reqBlock := httptest.NewRequest("GET", fmt.Sprintf("/block?hash=%s", b1.Hash), nil)
	wBlock := httptest.NewRecorder()
	handler.ServeHTTP(wBlock, reqBlock)
	if wBlock.Code != http.StatusOK {
		t.Fatalf("[N-21] Expected 200 OK for /block?hash, got %d", wBlock.Code)
	}

	// 2. Query /tx?hash=
	reqTx := httptest.NewRequest("GET", fmt.Sprintf("/tx?hash=%s", coinbaseTx.HashHex()), nil)
	wTx := httptest.NewRecorder()
	handler.ServeHTTP(wTx, reqTx)
	if wTx.Code != http.StatusOK {
		t.Fatalf("[N-21] Expected 200 OK for /tx?hash, got %d: %s", wTx.Code, wTx.Body.String())
	}
}
