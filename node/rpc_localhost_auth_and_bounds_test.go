package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN19_LocalhostMutationsRequireAuthByDefault verifies [N-19]:
// Mutation endpoints (/mine, /block/submit, POST /peers) strictly require auth
// by default even from localhost when no credentials are configured.
func TestN19_LocalhostMutationsRequireAuthByDefault(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	peerServer := NewPeerServer(getFreePort(t), bc)
	limiter := newIPRateLimiter()

	// Clear credentials and disable insecure localhost
	InitRPCAuth("", "")
	AllowInsecureLocalhost = false

	handler := NewRPCHandler(bc, peerServer, limiter, &ServerConfig{})

	// 1. POST /block/submit from localhost without auth -> must be 401 Unauthorized
	reqBlock := httptest.NewRequest(http.MethodPost, "/block/submit", bytes.NewBufferString("{}"))
	reqBlock.RemoteAddr = "127.0.0.1:5555"
	recBlock := httptest.NewRecorder()
	handler.ServeHTTP(recBlock, reqBlock)
	if recBlock.Code != http.StatusUnauthorized {
		t.Fatalf("[N-19] /block/submit allowed unauthenticated localhost request: got code %d", recBlock.Code)
	}

	// 2. POST /peers from localhost without auth -> must be 401 Unauthorized
	reqPeers := httptest.NewRequest(http.MethodPost, "/peers", bytes.NewBufferString(fmt.Sprintf(`{"peer":"10.0.0.1:%s"}`, core.MainnetP2PPort)))
	reqPeers.RemoteAddr = "127.0.0.1:5555"
	recPeers := httptest.NewRecorder()
	handler.ServeHTTP(recPeers, reqPeers)
	if recPeers.Code != http.StatusUnauthorized {
		t.Fatalf("[N-19] POST /peers allowed unauthenticated localhost request: got code %d", recPeers.Code)
	}

	// 3. POST /mine from localhost without auth or mine key -> must be 401 Unauthorized
	reqMine := httptest.NewRequest(http.MethodPost, "/mine", bytes.NewBufferString(`{"miner":"armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqx6r7we"}`))
	reqMine.RemoteAddr = "127.0.0.1:5555"
	recMine := httptest.NewRecorder()
	handler.ServeHTTP(recMine, reqMine)
	if recMine.Code != http.StatusUnauthorized {
		t.Fatalf("[N-19] POST /mine allowed unauthenticated localhost request: got code %d", recMine.Code)
	}
}

// TestN19_RemoteOrProxyWithoutXForwardedForRejected verifies remote request without credentials is rejected
func TestN19_RemoteOrProxyWithoutXForwardedForRejected(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	peerServer := NewPeerServer(getFreePort(t), bc)
	limiter := newIPRateLimiter()
	InitRPCAuth("", "")
	AllowInsecureLocalhost = false

	handler := NewRPCHandler(bc, peerServer, limiter, &ServerConfig{})

	// Remote IP without any proxy headers attempting POST /block/submit
	req := httptest.NewRequest(http.MethodPost, "/block/submit", bytes.NewBufferString("{}"))
	req.RemoteAddr = "198.51.100.55:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("[N-19] Remote unauthenticated request was not rejected with 401: got %d", rec.Code)
	}
}

// TestN19_GetPeersAndMempoolBoundedTo100 verifies GET /peers and GET /mempool are clamped to max 100 entries [N-19]
func TestN19_GetPeersAndMempoolBoundedTo100(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	peerServer := NewPeerServer(getFreePort(t), bc)
	limiter := newIPRateLimiter()
	handler := NewRPCHandler(bc, peerServer, limiter, &ServerConfig{})

	// 1. Populate 150 peers
	for i := 1; i <= 150; i++ {
		addr := fmt.Sprintf("198.51.100.%d:%s", i%10, core.MainnetP2PPort)
		peerServer.AddPeer(addr)
	}

	// 2. Populate 150 mempool dummy txs
	bc.Lock.Lock()
	for i := 1; i <= 150; i++ {
		bc.Mempool = append(bc.Mempool, core.Transaction{
			Sender:    "armx1test",
			Recipient: "armx1test",
			Amount:    1,
			Nonce:     int64(i),
		})
	}
	bc.Lock.Unlock()

	// Query GET /peers
	reqPeers := httptest.NewRequest(http.MethodGet, "/peers", nil)
	recPeers := httptest.NewRecorder()
	handler.ServeHTTP(recPeers, reqPeers)
	var returnedPeers []string
	_ = json.NewDecoder(recPeers.Body).Decode(&returnedPeers)
	if len(returnedPeers) > 100 {
		t.Fatalf("[N-19] GET /peers returned more than 100 entries: got %d", len(returnedPeers))
	}

	// Query GET /mempool
	reqMempool := httptest.NewRequest(http.MethodGet, "/mempool", nil)
	recMempool := httptest.NewRecorder()
	handler.ServeHTTP(recMempool, reqMempool)
	var returnedMempool []core.Transaction
	_ = json.NewDecoder(recMempool.Body).Decode(&returnedMempool)
	if len(returnedMempool) > 100 {
		t.Fatalf("[N-19] GET /mempool returned more than 100 entries: got %d", len(returnedMempool))
	}
}
