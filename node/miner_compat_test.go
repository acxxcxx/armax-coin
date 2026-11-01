//go:build testhooks

package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestMinerHTTPFlowCompatibility replays the request sequence used by the armminer
// HTTP client (/getmininginfo, /getblocktemplate, POST /block/submit with X-Mine-Key
// or Basic auth) against the current RPC handler. It uses only in-process httptest.
func TestMinerHTTPFlowCompatibility(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)
	ResetRPCAuthForTesting()
	defer ResetRPCAuthForTesting()

	bc := core.NewBlockchain(1)
	defer bc.Close()

	cfg := &ServerConfig{RPCBind: "127.0.0.1", MineKey: "miner-secret"}
	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), cfg)

	do := func(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:19333"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{"/getmininginfo", "/getblocktemplate"} {
		w := do(http.MethodGet, path, "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		if w.Header().Get("X-Network-ID") == "" {
			t.Fatalf("%s: missing additive X-Network-ID header", path)
		}
	}

	w := do(http.MethodGet, "/getblocktemplate", "", nil)
	var tmpl map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tmpl); err != nil {
		t.Fatalf("template is not JSON: %v", err)
	}
	for _, f := range []string{"previousblockhash", "height", "target", "coinbasevalue", "transactions", "merkleroot", "pow_algo", "curtime", "mintime"} {
		if _, ok := tmpl[f]; !ok {
			t.Fatalf("template lost field %q required by miners", f)
		}
	}

	if w := do(http.MethodPost, "/block/submit", "{}", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated submit: expected 401, got %d", w.Code)
	}
	if w := do(http.MethodPost, "/block/submit", "{}", map[string]string{"X-Mine-Key": "wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: expected 401, got %d", w.Code)
	}
	// Authorized requests must reach block validation (400 rejected), not 401/403.
	if w := do(http.MethodPost, "/block/submit", "{}", map[string]string{"X-Mine-Key": "miner-secret"}); w.Code != http.StatusBadRequest {
		t.Fatalf("X-Mine-Key submit: expected 400 from validation, got %d: %s", w.Code, w.Body.String())
	}

	if err := InitRPCAuth("minerrpc", "minerpass"); err != nil {
		t.Fatalf("InitRPCAuth: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/block/submit", strings.NewReader("{}"))
	req.Host = "127.0.0.1:19333"
	req.SetBasicAuth("minerrpc", "minerpass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Basic auth submit: expected 400 from validation, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRPCCookiePathUnderDataDir pins the cookie location the miner must search.
func TestRPCCookiePathUnderDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	got := GetRPCAuthCookiePath()
	net := core.GetActiveNetwork().Name
	if net == "" {
		net = "mainnet"
	}
	want := filepath.Join(dir, net, "armax.cookie")
	if got != want {
		t.Fatalf("cookie path = %q, want %q", got, want)
	}
}
