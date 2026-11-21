//go:build testhooks

package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
)

// TestRPCHistory_PagedTraversalAndBlockBoundary verifies that ?format=paged traverses all blocks
// across multiple pages, keeps pending mempool txs only on page 1, completes all intra-block txs
// before cutting off, and preserves the default JSON array response when format != paged.
func TestRPCHistory_PagedTraversalAndBlockBoundary(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bc := core.NewBlockchain(1)
	defer bc.Close()

	payload := make([]byte, 32)
	payload[0] = 0x11
	minerAddr, err := core.Bech32Encode(core.Bech32HRP, payload)
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}

	// Mine 6 blocks to build a multi-batch chain (#0..#6)
	for i := 1; i <= 6; i++ {
		if _, err := bc.MineBlockWithErr(minerAddr); err != nil {
			t.Fatalf("MineBlockWithErr %d failed: %v", i, err)
		}
	}

	// Inject a synthetic pending mempool tx targeting minerAddr
	bc.Lock.Lock()
	bc.Mempool = append(bc.Mempool, core.Transaction{
		Sender:    core.GenesisMiner,
		Recipient: minerAddr,
		Amount:    12345,
		Fee:       core.MinRelayFee,
		Nonce:     999,
	})
	bc.Lock.Unlock()

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.TestnetRPCPort,
	})

	// 1. Backward-compatible unpaged request must still return a JSON array
	reqUnpaged := httptest.NewRequest(http.MethodGet, "/history?address="+minerAddr, nil)
	wUnpaged := httptest.NewRecorder()
	handler.ServeHTTP(wUnpaged, reqUnpaged)
	if wUnpaged.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for unpaged /history, got %d: %s", wUnpaged.Code, wUnpaged.Body.String())
	}
	var unpagedArr []map[string]interface{}
	if err := json.Unmarshal(wUnpaged.Body.Bytes(), &unpagedArr); err != nil {
		t.Fatalf("expected unpaged /history to return JSON array, got error: %v (%s)", err, wUnpaged.Body.String())
	}
	if len(unpagedArr) != 7 { // 1 mempool + 6 mined blocks
		t.Fatalf("expected 7 records in unpaged array, got %d", len(unpagedArr))
	}

	// 2. Paged traversal with batch_blocks=2 and limit=2 must yield mempool once + all 6 mined blocks
	type pagedEnvelope struct {
		Records          []map[string]interface{} `json:"records"`
		ChainHeight      int                      `json:"chain_height"`
		TipHash          string                   `json:"tip_hash"`
		ScannedFrom      int                      `json:"scanned_from"`
		ScannedTo        int                      `json:"scanned_to"`
		NextCursorHeight int                      `json:"next_cursor_height"`
		HasMore          bool                     `json:"has_more"`
	}

	var allRecords []map[string]interface{}
	pendingCount := 0
	cursorQuery := ""
	pages := 0
	for {
		pages++
		url := fmt.Sprintf("/history?address=%s&format=paged&batch_blocks=2&limit=2%s", minerAddr, cursorQuery)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("page %d expected 200 OK, got %d: %s", pages, w.Code, w.Body.String())
		}
		var env pagedEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("page %d decode failed: %v", pages, err)
		}
		if env.ChainHeight != 6 {
			t.Fatalf("expected chain_height 6, got %d", env.ChainHeight)
		}
		for _, rec := range env.Records {
			if status, _ := rec["status"].(string); status == "pending" {
				pendingCount++
				if pages != 1 {
					t.Fatalf("pending tx emitted on non-first page %d", pages)
				}
			}
		}
		allRecords = append(allRecords, env.Records...)
		if !env.HasMore {
			if env.ScannedTo != 0 || env.NextCursorHeight != -1 {
				t.Fatalf("expected final page to reach block 0 with next_cursor_height=-1, got scanned_to=%d next=%d", env.ScannedTo, env.NextCursorHeight)
			}
			break
		}
		cursorQuery = fmt.Sprintf("&cursor_height=%d", env.NextCursorHeight)
		if pages > 10 {
			t.Fatalf("pagination loop did not terminate")
		}
	}

	if pendingCount != 1 {
		t.Fatalf("expected exactly 1 pending tx across all pages, got %d", pendingCount)
	}
	if len(allRecords) != 7 {
		t.Fatalf("expected 7 total records (1 pending + 6 blocks) across paged traversal, got %d", len(allRecords))
	}
}

// TestRPCStatsAndMine_DynamicMaturityAndPeersActive verifies /stats includes coinbase_maturity & ticker
// and /mine includes peers_active in its JSON response.
func TestRPCStatsAndMine_DynamicMaturityAndPeersActive(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bc := core.NewBlockchain(1)
	defer bc.Close()

	payload := make([]byte, 32)
	payload[0] = 0x11
	minerAddr, err := core.Bech32Encode(core.Bech32HRP, payload)
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.TestnetRPCPort,
		MineKey: "test-mine-key",
	})

	// 1. /stats check
	reqStats := httptest.NewRequest(http.MethodGet, "/stats", nil)
	wStats := httptest.NewRecorder()
	handler.ServeHTTP(wStats, reqStats)
	if wStats.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /stats, got %d", wStats.Code)
	}
	var stats map[string]interface{}
	if err := json.Unmarshal(wStats.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to parse /stats JSON: %v", err)
	}
	if mat, ok := stats["coinbase_maturity"].(float64); !ok || int(mat) != core.TestnetProfile.CoinbaseMaturity {
		t.Fatalf("expected coinbase_maturity=%d, got %v", core.TestnetProfile.CoinbaseMaturity, stats["coinbase_maturity"])
	}
	if ticker, _ := stats["ticker"].(string); ticker != "tARMX" {
		t.Fatalf("expected ticker=tARMX on testnet, got %q", ticker)
	}

	// 2. /mine check
	reqMine := httptest.NewRequest(http.MethodPost, "/mine?miner="+minerAddr+"&threads=1", nil)
	reqMine.Host = "127.0.0.1"
	reqMine.Header.Set("X-Mine-Key", "test-mine-key")
	wMine := httptest.NewRecorder()
	handler.ServeHTTP(wMine, reqMine)
	if wMine.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /mine, got %d: %s", wMine.Code, wMine.Body.String())
	}
	var mineResp map[string]interface{}
	if err := json.Unmarshal(wMine.Body.Bytes(), &mineResp); err != nil {
		t.Fatalf("failed to parse /mine JSON: %v", err)
	}
	if _, ok := mineResp["peers_active"]; !ok {
		t.Fatalf("expected peers_active field in /mine response, got keys: %v", mineResp)
	}
}

func TestRPCHistory_PagedMinHeightDeltaSync(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bc := core.NewBlockchain(1)
	defer bc.Close()

	payload := make([]byte, 32)
	payload[0] = 0x22
	minerAddr, err := core.Bech32Encode(core.Bech32HRP, payload)
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}

	for i := 1; i <= 6; i++ {
		if _, err := bc.MineBlockWithErr(minerAddr); err != nil {
			t.Fatalf("MineBlockWithErr %d failed: %v", i, err)
		}
	}

	bc.Lock.Lock()
	bc.Mempool = append(bc.Mempool, core.Transaction{
		Sender:    core.GenesisMiner,
		Recipient: minerAddr,
		Amount:    5555,
		Fee:       core.MinRelayFee,
		Nonce:     77,
	})
	bc.Lock.Unlock()

	handler := NewRPCHandler(bc, nil, newIPRateLimiter(), &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.TestnetRPCPort,
	})

	type pagedEnvelope struct {
		Records []struct {
			BlockIndex int    `json:"block_index"`
			Status     string `json:"status"`
		} `json:"records"`
		ChainHeight      int  `json:"chain_height"`
		ScannedFrom      int  `json:"scanned_from"`
		ScannedTo        int  `json:"scanned_to"`
		NextCursorHeight int  `json:"next_cursor_height"`
		HasMore          bool `json:"has_more"`
	}

	// 1. min_height=4 must scan ONLY #6..#4 plus 1 pending mempool tx
	reqDelta := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/history?address=%s&format=paged&min_height=4", minerAddr), nil)
	wDelta := httptest.NewRecorder()
	handler.ServeHTTP(wDelta, reqDelta)
	if wDelta.Code != http.StatusOK {
		t.Fatalf("delta query failed (%d): %s", wDelta.Code, wDelta.Body.String())
	}
	var envDelta pagedEnvelope
	if err := json.Unmarshal(wDelta.Body.Bytes(), &envDelta); err != nil {
		t.Fatalf("decode delta failed: %v", err)
	}
	if envDelta.HasMore || envDelta.NextCursorHeight != -1 {
		t.Fatalf("expected has_more=false and next_cursor_height=-1, got has_more=%v next=%d", envDelta.HasMore, envDelta.NextCursorHeight)
	}
	if envDelta.ScannedTo != 4 || len(envDelta.Records) != 4 { // 1 pending + blocks 6, 5, 4
		t.Fatalf("expected scanned_to=4 and 4 records (1 pending + #6,#5,#4), got scanned_to=%d len=%d", envDelta.ScannedTo, len(envDelta.Records))
	}

	// 2. min_height=7 (ahead of tip #6) must return ONLY pending mempool tx with has_more=false
	reqTip := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/history?address=%s&format=paged&min_height=7", minerAddr), nil)
	wTip := httptest.NewRecorder()
	handler.ServeHTTP(wTip, reqTip)
	if wTip.Code != http.StatusOK {
		t.Fatalf("tip delta query failed (%d): %s", wTip.Code, wTip.Body.String())
	}
	var envTip pagedEnvelope
	if err := json.Unmarshal(wTip.Body.Bytes(), &envTip); err != nil {
		t.Fatalf("decode tip delta failed: %v", err)
	}
	if envTip.HasMore || len(envTip.Records) != 1 || envTip.Records[0].BlockIndex != -1 {
		t.Fatalf("expected only 1 pending record with has_more=false, got has_more=%v records=%+v", envTip.HasMore, envTip.Records)
	}

	// 3. Invalid negative min_height must return 400 Bad Request
	reqBad := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/history?address=%s&format=paged&min_height=-2", minerAddr), nil)
	wBad := httptest.NewRecorder()
	handler.ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for negative min_height, got %d", wBad.Code)
	}
}

func TestStartEmbeddedServer_ImmediateRPCAndGracefulShutdown(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain failed: %v", err)
	}
	defer bc.Close()

	p2pLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve ephemeral P2P port: %v", err)
	}
	_, p2pPort, _ := net.SplitHostPort(p2pLn.Addr().String())
	_ = p2pLn.Close()

	cfg := DefaultConfig()
	cfg.NetworkID = core.TestnetProfile.NetworkID
	cfg.RPCBind = "127.0.0.1"
	cfg.RPCPort = "0"
	cfg.P2PBind = "127.0.0.1"
	cfg.P2PPort = p2pPort
	cfg.DataDir = tmpDir
	cfg.SeedPeers = []string{"127.0.0.1:19999"}

	handle, err := StartEmbeddedServer(bc, cfg)
	if err != nil {
		t.Fatalf("StartEmbeddedServer failed: %v", err)
	}
	// Give peerServer goroutine a brief moment to enter StartServer lifecycle before immediate test shutdown.
	defer func() {
		_, _ = handle.Shutdown(3 * time.Second)
	}()

	rpcAddr := handle.RPCAddr()
	if rpcAddr == "" {
		t.Fatalf("expected non-empty bound RPCAddr from StartEmbeddedServer")
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + rpcAddr + "/stats")
	if err != nil {
		t.Fatalf("expected immediate GET /stats to succeed on %s: %v", rpcAddr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from /stats, got %d", resp.StatusCode)
	}
	if gotNet := resp.Header.Get("X-Network-ID"); gotNet != core.TestnetProfile.NetworkID {
		t.Fatalf("expected X-Network-ID %q, got %q", core.TestnetProfile.NetworkID, gotNet)
	}

	// Ensure PeerServer has entered StartServer before triggering clean shutdown
	time.Sleep(50 * time.Millisecond)
	drained, shErr := handle.Shutdown(3 * time.Second)
	if !drained || shErr != nil {
		t.Fatalf("expected clean embedded shutdown (drained=true, err=nil), got drained=%v err=%v", drained, shErr)
	}

	select {
	case doneErr := <-handle.Done():
		if doneErr != nil {
			t.Fatalf("expected nil Done() error after Shutdown, got %v", doneErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for embedded server Done() channel")
	}

	if _, err := client.Get("http://" + rpcAddr + "/stats"); err == nil {
		t.Fatalf("expected connection failure after embedded server shutdown")
	}
}
