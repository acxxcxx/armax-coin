//go:build testhooks

package node

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func setupTestServer(t *testing.T) (*core.Blockchain, http.Handler) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	bc := core.NewBlockchain(3)
	t.Cleanup(func() {
		_ = bc.Close()
		core.SetTargetOverrideForTesting(nil)
	})
	limiter := newIPRateLimiter()
	cfg := &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.MainnetRPCPort,
	}
	handler := NewRPCHandler(bc, nil, limiter, cfg)
	return bc, handler
}

// TestPrunedWalletEndpoints verifies that private key endpoints return 404 on the daemon
func TestPrunedWalletEndpoints(t *testing.T) {
	_, handler := setupTestServer(t)

	prunedPaths := []string{
		"/wallet/new",
		"/wallet/restore",
		"/wallet/from_key",
		"/tx/send",
	}

	for _, p := range prunedPaths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("Path %s expected 404 Not Found on daemon, got status %d", p, w.Code)
		}
	}
}

// TestCopyBeforeWriteLocking ensures RLock is not held across network/JSON writes
func TestCopyBeforeWriteLocking(t *testing.T) {
	bc, handler := setupTestServer(t)

	// In a separate goroutine, execute a writer lock periodically
	stop := make(chan bool)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				bc.Lock.Lock()
				// Simulate internal blockchain state update
				time.Sleep(1 * time.Millisecond)
				bc.Lock.Unlock()
			}
		}
	}()

	// Query /chain and /stats multiple times; should never deadlock or block indefinitely
	for i := 0; i < 15; i++ {
		req := httptest.NewRequest("GET", "/chain", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Iteration %d: /chain returned code %d", i, w.Code)
		}

		reqStats := httptest.NewRequest("GET", "/stats", nil)
		wStats := httptest.NewRecorder()
		handler.ServeHTTP(wStats, reqStats)
		if wStats.Code != http.StatusOK {
			t.Fatalf("Iteration %d: /stats returned code %d", i, wStats.Code)
		}
	}
	close(stop)
}

// TestConstantTimeAuthentication verifies constant-time auth credentials enforcement
func TestConstantTimeAuthentication(t *testing.T) {
	defer ResetRPCAuthForTesting()

	InitRPCAuth("rpcadmin", "SuperSecretPassword123!")

	// 1. Valid basic auth credentials
	validAuth := base64.StdEncoding.EncodeToString([]byte("rpcadmin:SuperSecretPassword123!"))
	reqValid := httptest.NewRequest("POST", "/peers", bytes.NewBufferString(`{"peer":"127.0.0.1:9001"}`))
	reqValid.Header.Set("Authorization", "Basic "+validAuth)
	reqValid.RemoteAddr = "10.0.0.5:12345"

	if !Authenticate(reqValid) {
		t.Errorf("Expected valid credentials to pass Authenticate")
	}

	// 2. Invalid password
	badPass := base64.StdEncoding.EncodeToString([]byte("rpcadmin:WrongPassword!"))
	reqBadPass := httptest.NewRequest("POST", "/peers", nil)
	reqBadPass.Header.Set("Authorization", "Basic "+badPass)
	reqBadPass.RemoteAddr = "10.0.0.5:12345"

	if Authenticate(reqBadPass) {
		t.Errorf("Expected invalid password to fail Authenticate")
	}

	// 3. Invalid user
	badUser := base64.StdEncoding.EncodeToString([]byte("wronguser:SuperSecretPassword123!"))
	reqBadUser := httptest.NewRequest("POST", "/peers", nil)
	reqBadUser.Header.Set("Authorization", "Basic "+badUser)
	reqBadUser.RemoteAddr = "10.0.0.5:12345"

	if Authenticate(reqBadUser) {
		t.Errorf("Expected invalid username to fail Authenticate")
	}

	// 4. Valid cookie token
	GlobalAuth.lock.RLock()
	cookieVal := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	cookieAuth := base64.StdEncoding.EncodeToString([]byte("__cookie__:" + cookieVal))
	reqCookie := httptest.NewRequest("POST", "/peers", nil)
	reqCookie.Header.Set("Authorization", "Basic "+cookieAuth)
	reqCookie.RemoteAddr = "10.0.0.5:12345"

	if !Authenticate(reqCookie) {
		t.Errorf("Expected valid cookie token to pass Authenticate")
	}
}

// TestProxyLocalhostBypassPrevention verifies that reverse proxies cannot bypass auth
func TestProxyLocalhostBypassPrevention(t *testing.T) {
	defer ResetRPCAuthForTesting()

	// When AllowInsecureLocalhost is set, direct localhost is allowed, but proxied is rejected [N-19]
	AllowInsecureLocalhost = true
	defer func() { AllowInsecureLocalhost = false }()
	InitRPCAuth("", "")

	// 1. Direct local request without proxy headers
	reqDirect := httptest.NewRequest("GET", "/peers", nil)
	reqDirect.RemoteAddr = "127.0.0.1:54321"

	if !Authenticate(reqDirect) {
		t.Errorf("Expected direct localhost without password to be allowed when AllowInsecureLocalhost=true")
	}

	// 2. Proxied request through Nginx with X-Forwarded-For
	reqProxied := httptest.NewRequest("GET", "/peers", nil)
	reqProxied.RemoteAddr = "127.0.0.1:54321"
	reqProxied.Header.Set("X-Forwarded-For", "203.0.113.195")

	if Authenticate(reqProxied) {
		t.Errorf("Expected proxied request with X-Forwarded-For to be rejected without credentials")
	}
}

// TestMineRequiresPOST verifies [R-01]: GET /mine is rejected with 405 Method Not Allowed
func TestMineRequiresPOST(t *testing.T) {
	_, handler := setupTestServer(t)

	req := httptest.NewRequest("GET", "/mine?miner="+core.GenesisMiner, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405 Method Not Allowed on GET /mine, got %d", w.Code)
	}
}

// TestMineCSRFProtection verifies [R-01]: Strict Origin & Referer hostname checks
func TestMineCSRFProtection(t *testing.T) {
	AllowInsecureLocalhost = true
	defer func() { AllowInsecureLocalhost = false }()

	_, handler := setupTestServer(t)

	// 1. Cross-origin request from malicious third-party website
	reqEvil := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqEvil.Header.Set("Origin", "http://malicious-crypto-stealer.xyz")
	wEvil := httptest.NewRecorder()
	handler.ServeHTTP(wEvil, reqEvil)

	if wEvil.Code != http.StatusForbidden {
		t.Errorf("Expected cross-origin /mine request to return 403 Forbidden, got %d", wEvil.Code)
	}

	// 2. Subdomain prefix bypass attempt (e.g. 127.0.0.1.attacker.com)
	reqPrefixBypass := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqPrefixBypass.Header.Set("Origin", "http://127.0.0.1.attacker.com")
	wPrefixBypass := httptest.NewRecorder()
	handler.ServeHTTP(wPrefixBypass, reqPrefixBypass)

	if wPrefixBypass.Code != http.StatusForbidden {
		t.Errorf("Expected prefix bypass attempt to return 403 Forbidden, got %d", wPrefixBypass.Code)
	}

	// 3. Referer prefix bypass attempt
	reqRefBypass := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqRefBypass.Header.Set("Referer", "http://127.0.0.1.evil.org/steal")
	wRefBypass := httptest.NewRecorder()
	handler.ServeHTTP(wRefBypass, reqRefBypass)

	if wRefBypass.Code != http.StatusForbidden {
		t.Errorf("Expected referer prefix bypass to return 403 Forbidden, got %d", wRefBypass.Code)
	}

	// 4. Legitimate local web-wallet request with localhost Origin
	reqLocal := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqLocal.RemoteAddr = "127.0.0.1:50000"
	reqLocal.Header.Set("Origin", "http://127.0.0.1:3000")
	wLocal := httptest.NewRecorder()
	handler.ServeHTTP(wLocal, reqLocal)

	if wLocal.Code != http.StatusOK {
		t.Errorf("Expected legitimate localhost Origin to return 200 OK, got %d: %s", wLocal.Code, wLocal.Body.String())
	}
}

// TestMutatingEndpointsNoWildcardCORS verifies [R-04]: Wildcard CORS '*' is prohibited on mutating endpoints
func TestMutatingEndpointsNoWildcardCORS(t *testing.T) {
	_, handler := setupTestServer(t)

	// 1. GET /stats should have wildcard CORS (public read-only)
	reqGet := httptest.NewRequest("GET", "/stats", nil)
	wGet := httptest.NewRecorder()
	handler.ServeHTTP(wGet, reqGet)
	if cors := wGet.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("Expected GET /stats to have wildcard CORS '*', got '%s'", cors)
	}

	// 2. POST /mine without Origin should not echo wildcard '*'
	reqMine := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqMine.RemoteAddr = "127.0.0.1:50000"
	wMine := httptest.NewRecorder()
	handler.ServeHTTP(wMine, reqMine)
	if cors := wMine.Header().Get("Access-Control-Allow-Origin"); cors == "*" {
		t.Errorf("POST /mine must NEVER return wildcard CORS '*'")
	}

	// 3. POST /block/submit should not echo wildcard '*'
	reqSubmit := httptest.NewRequest("POST", "/block/submit", strings.NewReader(`{}`))
	reqSubmit.RemoteAddr = "127.0.0.1:50000"
	wSubmit := httptest.NewRecorder()
	handler.ServeHTTP(wSubmit, reqSubmit)
	if cors := wSubmit.Header().Get("Access-Control-Allow-Origin"); cors == "*" {
		t.Errorf("POST /block/submit must NEVER return wildcard CORS '*'")
	}
}

// TestBlockSubmitBodyLimit verifies [R-05]: Body size bounding on block submissions
func TestBlockSubmitBodyLimit(t *testing.T) {
	AllowInsecureLocalhost = true
	defer func() { AllowInsecureLocalhost = false }()

	_, handler := setupTestServer(t)

	// Create oversized payload exceeding MaxBlockSize + 64KB
	hugePayload := strings.Repeat("A", int(core.MaxBlockSize+128*1024))
	req := httptest.NewRequest("POST", "/block/submit", strings.NewReader(hugePayload))
	req.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// MaxBytesReader should cause decoding to fail and return 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for oversized payload, got %d", w.Code)
	}
}

// TestMineSecretKeyHeader verifies [R-01]: Optional X-Mine-Key enforcement
func TestMineSecretKeyHeader(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)
	bc := core.NewBlockchain(3)
	defer bc.Close()
	limiter := newIPRateLimiter()
	cfg := &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.MainnetRPCPort,
		MineKey: "supersecretminekey",
	}
	handler := NewRPCHandler(bc, nil, limiter, cfg)

	// 1. Request without X-Mine-Key
	reqNoKey := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqNoKey.RemoteAddr = "127.0.0.1:50000"
	wNoKey := httptest.NewRecorder()
	handler.ServeHTTP(wNoKey, reqNoKey)
	if wNoKey.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized without X-Mine-Key, got %d", wNoKey.Code)
	}

	// 2. Request with wrong X-Mine-Key
	reqBadKey := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqBadKey.RemoteAddr = "127.0.0.1:50000"
	reqBadKey.Header.Set("X-Mine-Key", "wrongkey")
	wBadKey := httptest.NewRecorder()
	handler.ServeHTTP(wBadKey, reqBadKey)
	if wBadKey.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized with wrong X-Mine-Key, got %d", wBadKey.Code)
	}

	// 3. Request with valid X-Mine-Key
	reqValidKey := httptest.NewRequest("POST", "/mine?miner="+core.GenesisMiner, nil)
	reqValidKey.RemoteAddr = "127.0.0.1:50000"
	reqValidKey.Header.Set("X-Mine-Key", "supersecretminekey")
	wValidKey := httptest.NewRecorder()
	handler.ServeHTTP(wValidKey, reqValidKey)
	if wValidKey.Code != http.StatusOK {
		t.Errorf("Expected 200 OK with valid X-Mine-Key, got %d: %s", wValidKey.Code, wValidKey.Body.String())
	}
}

// TestBalanceNextNonceField verifies [R-11]: /balance returns next_nonce
func TestBalanceNextNonceField(t *testing.T) {
	_, handler := setupTestServer(t)

	req := httptest.NewRequest("GET", "/balance?address="+core.GenesisMiner, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "next_nonce") {
		t.Fatalf("Expected response to contain next_nonce, got: %s", w.Body.String())
	}
}

// TestGetBlockTemplateBounded verifies [R-07, T-06]: /getblocktemplate strictly bounds transactions
func TestGetBlockTemplateBounded(t *testing.T) {
	bc, handler := setupTestServer(t)

	// Populate mempool with 1050 transactions (exceeding MaxBlockTransactions = 1000)
	bc.Lock.Lock()
	for i := 1; i <= 1050; i++ {
		dummyTx := core.Transaction{
			Sender:    core.GenesisMiner,
			Recipient: "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqz8c4y7",
			Amount:    100,
			Fee:       core.MinRelayFee,
			Nonce:     int64(i),
		}
		bc.Mempool = append(bc.Mempool, dummyTx)
	}
	bc.Lock.Unlock()

	req := httptest.NewRequest("GET", "/getblocktemplate", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", w.Code)
	}

	var template struct {
		CoinbaseValue int64              `json:"coinbasevalue"`
		Target        string             `json:"target"`
		Transactions  []core.Transaction `json:"transactions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &template); err != nil {
		t.Fatalf("Failed to parse blocktemplate: %v", err)
	}

	if template.CoinbaseValue <= 0 || template.Target == "" {
		t.Fatalf("Expected valid coinbasevalue and target, got: %+v", template)
	}

	// Must be bounded to MaxBlockTransactions - 1 (leaving room for coinbase)
	maxExpected := core.MaxBlockTransactions - 1
	if len(template.Transactions) > maxExpected {
		t.Fatalf("[T-06, R-07] Block template returned %d transactions, exceeded max limit %d", len(template.Transactions), maxExpected)
	}
	if len(template.Transactions) != maxExpected {
		t.Fatalf("[T-06, R-07] Expected block template to clamp to %d transactions, got %d", maxExpected, len(template.Transactions))
	}
}

// TestMiningStandardEndpoints verifies Bitcoin/Litecoin standard endpoints: /getmininginfo, /getnetworkhashps, /submitblock
func TestMiningStandardEndpoints(t *testing.T) {
	_, handler := setupTestServer(t)

	// 1. /getmininginfo
	reqInfo := httptest.NewRequest("GET", "/getmininginfo", nil)
	wInfo := httptest.NewRecorder()
	handler.ServeHTTP(wInfo, reqInfo)

	if wInfo.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /getmininginfo, got %d", wInfo.Code)
	}

	var info struct {
		Blocks        int     `json:"blocks"`
		Difficulty    float64 `json:"difficulty"`
		Target        string  `json:"target"`
		NetworkHashPS int64   `json:"networkhashps"`
		PooledTx      int     `json:"pooledtx"`
		Chain         string  `json:"chain"`
	}
	if err := json.Unmarshal(wInfo.Body.Bytes(), &info); err != nil {
		t.Fatalf("Failed to parse /getmininginfo JSON: %v", err)
	}
	if info.Blocks <= 0 || info.Target == "" || info.Chain == "" {
		t.Fatalf("Unexpected /getmininginfo payload: %+v", info)
	}

	// 2. /getnetworkhashps
	reqHashPS := httptest.NewRequest("GET", "/getnetworkhashps", nil)
	wHashPS := httptest.NewRecorder()
	handler.ServeHTTP(wHashPS, reqHashPS)

	if wHashPS.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /getnetworkhashps, got %d", wHashPS.Code)
	}
	var hashPS struct {
		NetworkHashPS int64 `json:"networkhashps"`
	}
	if err := json.Unmarshal(wHashPS.Body.Bytes(), &hashPS); err != nil {
		t.Fatalf("Failed to parse /getnetworkhashps JSON: %v", err)
	}

	// 3. /submitblock without auth should be 401
	reqSubmitNoAuth := httptest.NewRequest("POST", "/submitblock", strings.NewReader(`{}`))
	wSubmitNoAuth := httptest.NewRecorder()
	handler.ServeHTTP(wSubmitNoAuth, reqSubmitNoAuth)
	if wSubmitNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized from /submitblock without auth, got %d", wSubmitNoAuth.Code)
	}
}
