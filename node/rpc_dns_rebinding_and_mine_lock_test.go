package node

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT10_JSONErrorEscapesQuotes verifies that writeJSONError properly escapes
// quotes and special characters rather than doing naive string concatenation [T10].
func TestT10_JSONErrorEscapesQuotes(t *testing.T) {
	w := httptest.NewRecorder()
	payload := `malicious"payload\with'special"chars`
	writeJSONError(w, payload, http.StatusBadRequest)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", res.StatusCode)
	}

	var parsed map[string]string
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("Failed to parse writeJSONError response as valid JSON: %v", err)
	}
	if parsed["error"] != payload {
		t.Fatalf("Expected escaped payload %q, got %q", payload, parsed["error"])
	}
}

// TestT10_DNSRebindingHostHeaderRejected verifies that mutating RPC endpoints reject
// unauthorized Host headers to prevent DNS rebinding attacks [T10].
func TestT10_DNSRebindingHostHeaderRejected(t *testing.T) {
	bc := core.NewBlockchain(1)
	defer bc.Close()

	handler := NewRPCHandler(bc, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/tx/new", bytes.NewReader([]byte("{}")))
	req.Host = "evil-domain.com"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected status 403 Forbidden for Host: evil-domain.com, got %d", res.StatusCode)
	}
}

// TestT10_ConcurrentMineReturns409 verifies that concurrent /mine calls serialize
// and return 409 Conflict for the second caller [T10].
func TestT10_ConcurrentMineReturns409(t *testing.T) {
	bc := core.NewBlockchain(1)
	defer bc.Close()

	minerAddr, err := core.Bech32Encode(core.Bech32HRP, make([]byte, 32))
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}

	cfg := &ServerConfig{
		MineKey: "test-mine-key",
	}
	handler := NewRPCHandler(bc, nil, nil, cfg)

	// Simulate one request holding activeMiningJob
	atomic.StoreInt32(&activeMiningJob, 1)
	defer atomic.StoreInt32(&activeMiningJob, 0)

	req := httptest.NewRequest(http.MethodPost, "/mine?miner="+minerAddr, nil)
	req.Host = "127.0.0.1"
	req.Header.Set("X-Mine-Key", "test-mine-key")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("Expected status 409 Conflict when mining is active, got %d", res.StatusCode)
	}
}
