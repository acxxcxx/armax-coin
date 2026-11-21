package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
	"github.com/armaxcoincore/armax-coin/wallet"
)

func TestEmbeddedBrandAssetsPresent(t *testing.T) {
	if len(armaxLogoPNG) == 0 {
		t.Fatalf("expected embedded armax_logo.png to be non-empty")
	}
	if len(armaxTextPNG) == 0 {
		t.Fatalf("expected embedded armax_text.png to be non-empty")
	}
	if len(armaxLogoICO) == 0 {
		t.Fatalf("expected embedded armax_logo.ico to be non-empty")
	}
}

func TestMiningSessionTracker_ConfirmingMatureAndOrphan(t *testing.T) {
	tracker := NewMiningSessionTracker()

	hash10 := "00000000aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff666677778888"
	hash11 := "00000000bbbb1111cccc2222dddd3333eeee4444ffff5555aaaa666677779999"

	tracker.RecordFound(10, hash10, 60*core.CoinUnit, 2, 10)
	tracker.RecordFound(11, hash11, 60*core.CoinUnit, 2, 10)

	blks, rwd, orph, entries := tracker.Summary()
	if blks != 2 || rwd != 120*core.CoinUnit || orph != 0 || len(entries) != 2 {
		t.Fatalf("unexpected initial summary: blks=%d rwd=%d orph=%d len=%d", blks, rwd, orph, len(entries))
	}
	if entries[0].Status != MinedStatusRelayed {
		t.Fatalf("expected initial status RELAYED, got %s", entries[0].Status)
	}

	// Mock node where block #10 survived cleanly (now at height 19 => 10 confs = MATURE on testnet),
	// while block #11 was orphaned by a competing hash.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/block" {
			http.NotFound(w, r)
			return
		}
		idx, _ := strconv.Atoi(r.URL.Query().Get("index"))
		w.Header().Set("Content-Type", "application/json")
		switch idx {
		case 10:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"index": 10, "hash": hash10})
		case 11:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"index": 11, "hash": "00000000competingwinnerhash"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	orphans, changed := tracker.Reconcile(context.Background(), srv.Client(), srv.URL, 19, 10)
	if !changed {
		t.Fatalf("expected reconciliation to report state changes")
	}
	if len(orphans) != 1 || orphans[0].Height != 11 {
		t.Fatalf("expected block #11 orphan event, got %+v", orphans)
	}

	blks, rwd, orph, entries = tracker.Summary()
	if blks != 1 || rwd != 60*core.CoinUnit || orph != 1 {
		t.Fatalf("expected adjusted summary (1 valid, 60 ARMX, 1 orphan), got blks=%d rwd=%d orph=%d", blks, rwd, orph)
	}
	if entries[0].Status != MinedStatusOrphaned || entries[1].Status != MinedStatusMature {
		t.Fatalf("unexpected reconciled statuses: #11=%s, #10=%s", entries[0].Status, entries[1].Status)
	}
}

func TestFetchAllHistory_MultiPageAndClassification(t *testing.T) {
	acc := "armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308"

	// Simulate 3 pages from block #5000 down to #0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		w.Header().Set("Content-Type", "application/json")
		cursor := r.URL.Query().Get("cursor_height")
		switch cursor {
		case "":
			_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
				Records: []PagedHistoryRecord{
					{BlockIndex: -1, Timestamp: 1700000100, Status: "pending", Tx: core.Transaction{Sender: acc, Recipient: core.GenesisMiner, Amount: 100, Nonce: 2}},
					{BlockIndex: 4990, Timestamp: 1700000090, Status: "confirmed", Tx: core.Transaction{Sender: "SYSTEM", Recipient: acc, Amount: 60 * core.CoinUnit, Nonce: 4990}},
				},
				ChainHeight:      5000,
				ScannedFrom:      5000,
				ScannedTo:        3001,
				NextCursorHeight: 3000,
				HasMore:          true,
			})
		case "3000":
			_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
				Records: []PagedHistoryRecord{
					// Deliberate duplicate to verify deduplication
					{BlockIndex: 4990, Timestamp: 1700000090, Status: "confirmed", Tx: core.Transaction{Sender: "SYSTEM", Recipient: acc, Amount: 60 * core.CoinUnit, Nonce: 4990}},
					{BlockIndex: 1500, Timestamp: 1700000050, Status: "confirmed", Tx: core.Transaction{Sender: "COINBASE", Recipient: acc, Amount: 60 * core.CoinUnit, Nonce: 1500}},
				},
				ChainHeight:      5000,
				ScannedFrom:      3000,
				ScannedTo:        1001,
				NextCursorHeight: 1000,
				HasMore:          true,
			})
		case "1000":
			_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
				Records: []PagedHistoryRecord{
					{BlockIndex: 5, Timestamp: 1700000005, Status: "confirmed", Tx: core.Transaction{Sender: core.GenesisMiner, Recipient: acc, Amount: 5000, Nonce: 1}},
				},
				ChainHeight:      5000,
				ScannedFrom:      1000,
				ScannedTo:        0,
				NextCursorHeight: -1,
				HasMore:          false,
			})
		default:
			t.Fatalf("unexpected cursor %q", cursor)
		}
	}))
	defer srv.Close()

	var progressCalls int
	records, err := FetchAllHistory(context.Background(), srv.Client(), srv.URL, acc, "ARMX-TESTNET-1", 2000, func(recs []PagedHistoryRecord, prog HistoryFetchProgress) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("FetchAllHistory failed: %v", err)
	}
	if progressCalls != 3 {
		t.Fatalf("expected 3 progress callbacks, got %d", progressCalls)
	}
	if len(records) != 4 {
		t.Fatalf("expected 4 deduplicated records across all pages, got %d", len(records))
	}

	// Verify classification of SYSTEM vs COINBASE vs Sent vs Received
	if got := ClassifyTxType(records[0].Tx, acc); got != "Sent" {
		t.Fatalf("expected Sent, got %s", got)
	}
	if got := ClassifyTxType(records[1].Tx, acc); got != "Mined (Coinbase)" {
		t.Fatalf("expected Mined (Coinbase) for SYSTEM, got %s", got)
	}
	if got := ClassifyTxType(records[2].Tx, acc); got != "Mined (Coinbase)" {
		t.Fatalf("expected Mined (Coinbase) for COINBASE, got %s", got)
	}
	if got := ClassifyTxType(records[3].Tx, acc); got != "Received" {
		t.Fatalf("expected Received, got %s", got)
	}

	// Dynamic maturity formatting check (Testnet 10 blocks vs Mainnet 100 blocks)
	if got := FormatTxHeightStatus(4990, "Mined (Coinbase)", 4995, 10); got != "#4990 (6/10 blks)" {
		t.Fatalf("unexpected testnet maturity format: %s", got)
	}
	if FormatTxHeightStatus(4990, "Mined (Coinbase)", 4999, 10) != "#4990 (Mature)" {
		t.Fatalf("expected Mature at 10 confs on testnet")
	}
}

func TestHistoryStore_BboltPersistenceDeltaAndReorg(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui_history.db")
	acc := "armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308"

	var tipHeight = 5
	var tipHash = "hash-block-5"
	var block7Hash = "hash-block-7"
	var minHeightsSeen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/block" {
			idx, _ := strconv.Atoi(r.URL.Query().Get("index"))
			h := "hash-block-" + strconv.Itoa(idx)
			if idx == 7 {
				h = block7Hash
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"index": idx, "hash": h})
			return
		}
		if r.URL.Path == "/history" {
			mhStr := r.URL.Query().Get("min_height")
			minHeightsSeen = append(minHeightsSeen, mhStr)
			mh, _ := strconv.Atoi(mhStr)

			var recs []PagedHistoryRecord
			recs = append(recs, PagedHistoryRecord{
				BlockIndex: -1, Timestamp: 1700000999, Status: "pending",
				Tx: core.Transaction{Sender: acc, Recipient: core.GenesisMiner, Amount: 123, Nonce: 9},
			})
			if mh <= 5 {
				recs = append(recs, PagedHistoryRecord{
					BlockIndex: 5, Timestamp: 1700000500, Status: "confirmed",
					Tx: core.Transaction{Sender: "SYSTEM", Recipient: acc, Amount: 60 * core.CoinUnit, Nonce: 5},
				})
			}
			if tipHeight >= 7 && mh <= 7 {
				recs = append(recs, PagedHistoryRecord{
					BlockIndex: 7, Timestamp: 1700000700, Status: "confirmed",
					Tx: core.Transaction{Sender: "SYSTEM", Recipient: acc, Amount: 60 * core.CoinUnit, Nonce: 7},
				})
			}
			if mh <= 2 {
				recs = append(recs, PagedHistoryRecord{
					BlockIndex: 2, Timestamp: 1700000200, Status: "confirmed",
					Tx: core.Transaction{Sender: core.GenesisMiner, Recipient: acc, Amount: 5000, Nonce: 2},
				})
			}
			_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
				Records:          recs,
				ChainHeight:      tipHeight,
				TipHash:          tipHash,
				ScannedFrom:      tipHeight,
				ScannedTo:        mh,
				NextCursorHeight: -1,
				HasMore:          false,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// 1. Initial full sync at height 5 persists #5 and #2 into bbolt
	store, err := OpenHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("OpenHistoryStore failed: %v", err)
	}
	recs1, meta1, err := store.SyncAddress(context.Background(), srv.Client(), srv.URL, "ARMX-TESTNET-1", acc, 5, "hash-block-5", false)
	if err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	if len(recs1) != 3 || meta1.SyncedHeight != 5 || meta1.SyncedTipHash != "hash-block-5" {
		t.Fatalf("unexpected initial sync result: len=%d meta=%+v", len(recs1), meta1)
	}
	_ = store.Close()

	// 2. Reopen DB on restart -> exact same tip hits bbolt cache with mempool-only check (min_height=6)
	store2, err := OpenHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("reopen OpenHistoryStore failed: %v", err)
	}
	defer store2.Close()

	minHeightsSeen = nil
	recs2, meta2, err := store2.SyncAddress(context.Background(), srv.Client(), srv.URL, "ARMX-TESTNET-1", acc, 5, "hash-block-5", false)
	if err != nil {
		t.Fatalf("cached sync failed: %v", err)
	}
	if !meta2.CacheHit || meta2.DeltaBlocks != 0 || len(recs2) != 3 {
		t.Fatalf("expected instant bbolt cache hit with 3 records, got meta=%+v len=%d", meta2, len(recs2))
	}
	if len(minHeightsSeen) != 1 || minHeightsSeen[0] != "6" {
		t.Fatalf("expected mempool-only query with min_height=6, got %v", minHeightsSeen)
	}

	// 3. New blocks mined (#6..#7) -> delta sync requests min_height=6 only and merges with cached #5, #2
	tipHeight = 7
	tipHash = "hash-block-7"
	minHeightsSeen = nil
	recs3, meta3, err := store2.SyncAddress(context.Background(), srv.Client(), srv.URL, "ARMX-TESTNET-1", acc, 7, "hash-block-7", false)
	if err != nil {
		t.Fatalf("delta sync failed: %v", err)
	}
	if meta3.SyncedHeight != 7 || meta3.DeltaBlocks != 2 || len(recs3) != 4 {
		t.Fatalf("expected delta sync to height 7 with 4 merged txs, got meta=%+v len=%d", meta3, len(recs3))
	}
	if len(minHeightsSeen) != 1 || minHeightsSeen[0] != "6" {
		t.Fatalf("expected delta scan starting at min_height=6, got %v", minHeightsSeen)
	}

	// 4. Chain reorg invalidates hash at block #7 -> cache resets automatically and re-syncs from genesis
	block7Hash = "competing-reorg-hash-7"
	tipHeight = 8
	tipHash = "hash-block-8"
	minHeightsSeen = nil
	_, meta4, err := store2.SyncAddress(context.Background(), srv.Client(), srv.URL, "ARMX-TESTNET-1", acc, 8, "hash-block-8", false)
	if err != nil {
		t.Fatalf("reorg resync failed: %v", err)
	}
	if !meta4.ReorgReset || meta4.SyncedHeight != 8 {
		t.Fatalf("expected automatic reorg reset to height 8, got %+v", meta4)
	}
}

func TestFeeEstimator_MatchesCoreMempoolRule(t *testing.T) {
	// 1. Locked/fallback estimate matches 11,000 Armoids (0.00011000) and scales cleanly
	fallback := ComputeGUIFeeEstimate(nil, "", 0, 0, "ARMX-TESTNET-1")
	if fallback.MinRelayFeePerKB != core.MinRelayFee {
		t.Fatalf("MinRelayFeePerKB = %d, want %d", fallback.MinRelayFeePerKB, core.MinRelayFee)
	}
	if fallback.MinFeeArmoids != 11000 || fallback.EconomyArmoids != 11000 {
		t.Fatalf("expected 11000 minimum/economy fee, got min=%d econ=%d", fallback.MinFeeArmoids, fallback.EconomyArmoids)
	}
	if fallback.StandardArmoids < fallback.EconomyArmoids || fallback.PriorityArmoids < fallback.StandardArmoids {
		t.Fatalf("invalid tier ordering: %+v", fallback)
	}

	// 2. Unlocked ML-DSA-65 wallet estimate produces signed tx meeting core.CalculateMinMempoolFee(tx.Size())
	w, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	defer w.Zeroize()
	rec, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("recipient wallet failed: %v", err)
	}
	defer rec.Zeroize()

	est := ComputeGUIFeeEstimate(w, rec.Address, 100_000_000, 0, "ARMX-TESTNET-1")
	for _, tierFee := range []int64{est.EconomyArmoids, est.StandardArmoids, est.PriorityArmoids} {
		tx := core.Transaction{
			Sender:    w.Address,
			SenderPub: w.PublicKey,
			Recipient: rec.Address,
			Amount:    100_000_000,
			Fee:       tierFee,
			Nonce:     1,
		}
		if sErr := tx.Sign(w.PrivateKey); sErr != nil {
			t.Fatalf("tx.Sign failed: %v", sErr)
		}
		reqMin := core.CalculateMinMempoolFee(tx.Size())
		if tierFee < reqMin {
			t.Fatalf("estimated fee %d < required core.CalculateMinMempoolFee(%d bytes) = %d", tierFee, tx.Size(), reqMin)
		}
	}

	// 3. Mempool congestion elevates Standard & Priority above baseline
	congested := ComputeGUIFeeEstimate(w, rec.Address, 100_000_000, 120, "ARMX-TESTNET-1")
	if congested.StandardArmoids <= est.StandardArmoids || congested.PriorityArmoids <= est.PriorityArmoids {
		t.Fatalf("expected congested fee tiers > idle tiers: idle=%+v congested=%+v", est, congested)
	}
}

func TestStartupAuthGate_BlocksDataAPIsUntilUnlocked(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	closeHistoryStore()

	origMgr := walletMgr
	origID := activeWalletID
	origAddr := activeAddress
	origW := activeWallet
	origAuthReq := startupAuthRequired.Load()
	origAuthOK := startupAuthenticated.Load()
	unlockedMu.Lock()
	origUnlocked := unlockedWallets
	unlockedWallets = make(map[string]*wallet.Wallet)
	unlockedMu.Unlock()
	t.Cleanup(func() {
		closeHistoryStore()
		resetAppShutdownForTest()
		walletMgrMu.Lock()
		walletMgr = origMgr
		walletMgrMu.Unlock()
		unlockedMu.Lock()
		activeWalletID = origID
		activeAddress = origAddr
		activeWallet = origW
		unlockedWallets = origUnlocked
		unlockedMu.Unlock()
		startupAuthRequired.Store(origAuthReq)
		startupAuthenticated.Store(origAuthOK)
	})

	walletMgrMu.Lock()
	walletMgr = wallet.NewWalletManager()
	walletMgrMu.Unlock()

	pass := "Str0ng!StartupGate2026"
	entry, _, _, err := walletMgr.Create("Gate Test Wallet", pass)
	if err != nil {
		t.Fatalf("wallet create failed: %v", err)
	}

	// Simulate fresh app restart where keystore exists on disk and no env passphrase is supplied
	zeroizeAllUnlockedWallets()
	loadOrInitWallet()

	if !startupAuthRequired.Load() || startupAuthenticated.Load() {
		t.Fatalf("expected startupAuthRequired=true and startupAuthenticated=false, got req=%v ok=%v",
			startupAuthRequired.Load(), startupAuthenticated.Load())
	}

	tok := "gate-test-token"
	handler := secureGUIHandler(buildGUIServeMux(), tok)

	doReq := func(method, path, body string) *httptest.ResponseRecorder {
		var rBody *strings.Reader
		if body != "" {
			rBody = strings.NewReader(body)
		} else {
			rBody = strings.NewReader("")
		}
		req := httptest.NewRequest(method, "http://127.0.0.1:18088"+path, rBody)
		req.Header.Set("X-Armx-GUI-Token", tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. /api/wallet/info allowed, reports startup_locked=true and hides address
	infoRec := doReq(http.MethodGet, "/api/wallet/info", "")
	if infoRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/wallet/info, got %d", infoRec.Code)
	}
	if !strings.Contains(infoRec.Body.String(), `"startup_locked":true`) || !strings.Contains(infoRec.Body.String(), `"address":""`) {
		t.Fatalf("expected startup_locked:true and redacted address, got: %s", infoRec.Body.String())
	}

	// 2. All data / watch-only endpoints MUST return 423 Locked while gate is locked
	for _, blocked := range []struct{ m, p string }{
		{http.MethodGet, "/api/stats"},
		{http.MethodGet, "/api/balance"},
		{http.MethodGet, "/api/history"},
		{http.MethodGet, "/api/wallets"},
		{http.MethodGet, "/api/fee-estimate"},
		{http.MethodPost, "/api/send"},
	} {
		rec := doReq(blocked.m, blocked.p, "")
		if rec.Code != http.StatusLocked {
			t.Fatalf("expected 423 Locked on %s %s while startup-locked, got %d: %s", blocked.m, blocked.p, rec.Code, rec.Body.String())
		}
	}

	// 3. Wrong passphrase stays locked
	badUnl := doReq(http.MethodPost, "/api/wallets/unlock", `{"passphrase":"wrong-pass"}`)
	if badUnl.Code != http.StatusUnauthorized || startupAuthenticated.Load() {
		t.Fatalf("wrong passphrase should return 401 and keep gate locked, got %d", badUnl.Code)
	}

	// 4. Valid passphrase unlocks gate and exposes active wallet
	goodUnl := doReq(http.MethodPost, "/api/wallets/unlock", `{"id":"`+entry.ID+`","passphrase":"`+pass+`"}`)
	if goodUnl.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid unlock, got %d: %s", goodUnl.Code, goodUnl.Body.String())
	}
	if !startupAuthenticated.Load() {
		t.Fatalf("expected startupAuthenticated=true after valid unlock")
	}

	walletsRec := doReq(http.MethodGet, "/api/wallets", "")
	if walletsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/wallets after unlock, got %d", walletsRec.Code)
	}

	// 5. /api/app/exit works cleanly while locked or unlocked
	exitRec := doReq(http.MethodPost, "/api/app/exit", "")
	if exitRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/app/exit, got %d", exitRec.Code)
	}
	waitExit := time.NewTimer(2 * time.Second)
	defer waitExit.Stop()
	select {
	case <-getAppExitChan():
	case <-waitExit.C:
		t.Fatalf("timed out waiting for /api/app/exit to trigger shutdown channel")
	}
}

func TestWebUI_LightDarkThemeAndDiagnosticsConsole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	rec := httptest.NewRecorder()
	handleWebUI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleWebUI returned %d", rec.Code)
	}
	body := rec.Body.String()
	for _, needle := range []string{
		`[data-theme="light"]`,
		`[data-theme="dark"]`,
		`#0b0f14`,
		`#121922`,
		`#0ea5e9`,
		`#f59e0b`,
		`id="startupLockScreen"`,
		`unlockStartupWallet`,
		`exitDesktopApp`,
		`syncHistoryFromDB`,
		`loadFeeEstimate`,
		`pillFeeEconomy`,
		`pillFeeStandard`,
		`pillFeePriority`,
		`min-height: 460px`,
		`toggleConsoleExpand`,
		`setupWalletModal`,
		`restoreWalletModal`,
		`noWalletCard`,
		`walletManagerModal`,
		`tab-convert`,
		`executeModalCreateWallet`,
		`executeModalRestoreWallet`,
		`GUI_SESSION_TOKEN`,
		`armax_logo.png`,
		`armax_text.png`,
		`btn-sidebar`,
		`CPU Miner Configuration`,
		`lblMinerHps`,
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("Web UI missing expected element/token %q", needle)
		}
	}
	for _, removed := range []string{
		`detectGPUHardware`,
		`/api/mine/gpu/detect`,
		`selMinerMode`,
		`selGpuDevice`,
		`lblMinerBreakdown`,
	} {
		if strings.Contains(body, removed) {
			t.Errorf("Web UI unexpectedly retains removed GPU token %q", removed)
		}
	}
}

func TestLaunchSessionGuard_WindowsHeaderModeBlocksUnauthorizedRoot(t *testing.T) {
	sessionTok := "test-session-token-win"
	launchSec := "test-launch-secret-win"
	handler := launchSessionGuard(secureGUIHandler(buildGUIServeMux(), sessionTok), sessionTok, launchSec, true)

	// 1. Unauthorized local process trying GET / must be blocked with 403 Forbidden
	reqNoAuth := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	recNoAuth := httptest.NewRecorder()
	handler.ServeHTTP(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 on unauthorized GET / in native window mode, got %d", recNoAuth.Code)
	}
	if strings.Contains(recNoAuth.Body.String(), sessionTok) {
		t.Fatalf("session token leaked in unauthorized response")
	}

	// 2. Native WebView2 request carrying X-Armx-Launch-Auth succeeds with hardened security headers
	reqOK := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	reqOK.Header.Set("X-Armx-Launch-Auth", launchSec)
	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on authorized native GET /, got %d: %s", recOK.Code, recOK.Body.String())
	}
	if got := recOK.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options=DENY, got %q", got)
	}
	if got := recOK.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options=nosniff, got %q", got)
	}
	if got := recOK.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("expected strict Content-Security-Policy, got %q", got)
	}
	if got := recOK.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("expected Cache-Control containing no-store, got %q", got)
	}
}

func TestLaunchSessionGuard_UnixSingleUseTicketConsumedOnce(t *testing.T) {
	sessionTok := "test-session-token-unix"
	launchSec := "test-launch-ticket-unix"
	handler := launchSessionGuard(secureGUIHandler(buildGUIServeMux(), sessionTok), sessionTok, launchSec, false)

	// 1. Direct GET / without ticket or cookie must fail 403
	reqDirect := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	recDirect := httptest.NewRecorder()
	handler.ServeHTTP(recDirect, reqDirect)
	if recDirect.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 on direct GET / without cookie/ticket, got %d", recDirect.Code)
	}

	// 2. First GET /auth/launch?ticket=<secret> exchanges once for HttpOnly SameSite=Strict cookie + 302 /
	reqTicket := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/auth/launch?ticket="+launchSec, nil)
	recTicket := httptest.NewRecorder()
	handler.ServeHTTP(recTicket, reqTicket)
	if recTicket.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302 Found on first launch ticket, got %d: %s", recTicket.Code, recTicket.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range recTicket.Result().Cookies() {
		if c.Name == "armax_gui_token" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value != sessionTok || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected HttpOnly SameSite=Strict armax_gui_token cookie matching session token, got %+v", sessionCookie)
	}

	// 3. Replaying the single-use ticket must return 403 Forbidden
	reqReplay := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/auth/launch?ticket="+launchSec, nil)
	recReplay := httptest.NewRecorder()
	handler.ServeHTTP(recReplay, reqReplay)
	if recReplay.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden on launch ticket replay, got %d", recReplay.Code)
	}

	// 4. Subsequent GET / carrying the issued session cookie succeeds
	reqAuthed := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	reqAuthed.AddCookie(sessionCookie)
	recAuthed := httptest.NewRecorder()
	handler.ServeHTTP(recAuthed, reqAuthed)
	if recAuthed.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK on GET / with valid cookie, got %d", recAuthed.Code)
	}

	// 5. Already-authenticated browser revisiting /auth/launch (e.g. Back button) redirects 302 -> /,
	// while cookie-less replay remains blocked with 403.
	reqRevisit := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/auth/launch?ticket="+launchSec, nil)
	reqRevisit.AddCookie(sessionCookie)
	recRevisit := httptest.NewRecorder()
	handler.ServeHTTP(recRevisit, reqRevisit)
	if recRevisit.Code != http.StatusFound || recRevisit.Header().Get("Location") != "/" {
		t.Fatalf("expected 302 -> / on revisit with valid session cookie, got %d Location=%q", recRevisit.Code, recRevisit.Header().Get("Location"))
	}

	reqReplayStillBlocked := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/auth/launch?ticket="+launchSec, nil)
	recReplayStillBlocked := httptest.NewRecorder()
	handler.ServeHTTP(recReplayStillBlocked, reqReplayStillBlocked)
	if recReplayStillBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden on cookie-less replay after revisit, got %d", recReplayStillBlocked.Code)
	}
}

func TestResolveGUIDataDirWithCheck_FallsBackWhenExecDirRejectsStrictPerms(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", "")
	cfgDir, err := os.UserConfigDir()
	if err != nil || cfgDir == "" {
		t.Skipf("os.UserConfigDir unavailable: %v", err)
	}
	expectedCfg := filepath.Join(cfgDir, "ArmaxCoin")
	expectedSub := filepath.Clean(filepath.Join(expectedCfg, "testnet"))

	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	fakeCwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fakeCwd, "testnet"), 0700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeCwd, "testnet", "wallet.json"), []byte("{}"), 0600); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origCwd)
	})

	// Reject execDir and cwd (simulating WSL /mnt/* DrvFs 0777 mode), accept only UserConfigDir/ArmaxCoin
	got := resolveGUIDataDirWithCheck("testnet", "", func(dir string) bool {
		return filepath.Clean(dir) == expectedSub
	})
	if filepath.Clean(got) != filepath.Clean(expectedCfg) {
		t.Fatalf("expected fallback to UserConfigDir %q, got %q", expectedCfg, got)
	}
}

func TestSecureGUIHandler_RejectsCrossOriginAndPostQueryToken(t *testing.T) {
	tok := "secure-handler-token"
	handler := secureGUIHandler(buildGUIServeMux(), tok)

	// 1. Cross-origin Origin header must fail 403
	reqOrigin := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/node/status", nil)
	reqOrigin.Header.Set("X-Armx-GUI-Token", tok)
	reqOrigin.Header.Set("Origin", "https://evil.example")
	recOrigin := httptest.NewRecorder()
	handler.ServeHTTP(recOrigin, reqOrigin)
	if recOrigin.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 on cross-origin Origin header, got %d", recOrigin.Code)
	}

	// 2. Cross-site Sec-Fetch-Site must fail 403
	reqSFS := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/node/status", nil)
	reqSFS.Header.Set("X-Armx-GUI-Token", tok)
	reqSFS.Header.Set("Sec-Fetch-Site", "cross-site")
	recSFS := httptest.NewRecorder()
	handler.ServeHTTP(recSFS, reqSFS)
	if recSFS.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 on cross-site Sec-Fetch-Site header, got %d", recSFS.Code)
	}

	// 3. Query ?token= must NEVER authorize POST mutations
	reqPostQuery := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18088/api/wallets/lock?token="+tok, nil)
	recPostQuery := httptest.NewRecorder()
	handler.ServeHTTP(recPostQuery, reqPostQuery)
	if recPostQuery.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized on POST with query ?token=, got %d", recPostQuery.Code)
	}
}

func TestResolveGUIDataDir_WritableFallbackAndEmbeddedNodeStatusAPI(t *testing.T) {
	tmpExplicit := filepath.Join(t.TempDir(), "custom-data")
	if got := resolveGUIDataDir("testnet", tmpExplicit); got != tmpExplicit {
		t.Fatalf("expected explicit data dir %q, got %q", tmpExplicit, got)
	}

	tmpEnv := filepath.Join(t.TempDir(), "env-data")
	t.Setenv("ARMX_DATA_DIR", tmpEnv)
	if got := resolveGUIDataDir("testnet", ""); got != tmpEnv {
		t.Fatalf("expected ARMX_DATA_DIR %q, got %q", tmpEnv, got)
	}
	if !isDirWritable(filepath.Join(tmpEnv, "testnet")) {
		t.Fatalf("expected resolved testnet dir to be writable")
	}
	if _, err := os.Stat(filepath.Join(tmpEnv, "testnet")); err != nil {
		t.Fatalf("expected writable check to ensure dir exists: %v", err)
	}

	origReq := startupAuthRequired.Load()
	origAuth := startupAuthenticated.Load()
	origAddr := activeAddress
	origStatus := getNodeBootstrapStatus()
	t.Cleanup(func() {
		startupAuthRequired.Store(origReq)
		startupAuthenticated.Store(origAuth)
		activeAddress = origAddr
		setNodeBootstrapStatus(origStatus)
	})

	startupAuthRequired.Store(true)
	startupAuthenticated.Store(false)
	activeAddress = "armx1secretshouldnotleak"
	setNodeBootstrapStatus(NodeBootstrapStatus{
		State:     "starting",
		Mode:      "embedded",
		Detail:    "Loading blockchain database...",
		RPCURL:    "http://127.0.0.1:19333",
		NetworkID: "ARMX-TESTNET-1",
	})

	tok := "node-status-tok"
	handler := secureGUIHandler(buildGUIServeMux(), tok)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/node/status", nil)
	req.Header.Set("X-Armx-GUI-Token", tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on /api/node/status during startup lock, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"state":"starting"`) {
		t.Fatalf("expected starting state in /api/node/status, got: %s", body)
	}
	if strings.Contains(body, "armx1secretshouldnotleak") {
		t.Fatalf("unexpected wallet address leaked in /api/node/status: %s", body)
	}
}

func TestHandleCLI_GetMempoolInfoQueriesMempoolNotStats(t *testing.T) {
	origRPC := rpcURL
	origNet := core.ActiveNetwork
	t.Cleanup(func() {
		rpcURL = origRPC
		core.ActiveNetwork = origNet
	})
	core.ActiveNetwork = core.TestnetProfile

	var mempoolPayload []core.Transaction
	statsHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		switch r.URL.Path {
		case "/stats":
			statsHits++
			_, _ = w.Write([]byte(`{"height":100,"asert_drift_seconds":12,"network_id":"ARMX-TESTNET-1"}`))
		case "/mempool":
			_ = json.NewEncoder(w).Encode(mempoolPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rpcURL = srv.URL

	// 1. Populated mempool
	mempoolPayload = []core.Transaction{{
		Sender:    core.GenesisMiner,
		Recipient: "armx1recipienttestaddress000000000000000000000000000000000",
		Amount:    500_000_000,
		Fee:       11_000,
		Nonce:     3,
	}}

	for _, testCase := range []struct {
		name string
		hFn  http.HandlerFunc
		url  string
	}{
		{"CLI getmempoolinfo", handleCLI, "http://127.0.0.1:18088/api/cli?command=getmempoolinfo"},
		{"Direct /api/mempool", handleMempool, "http://127.0.0.1:18088/api/mempool"},
	} {
		req := httptest.NewRequest(http.MethodGet, testCase.url, nil)
		rec := httptest.NewRecorder()
		testCase.hFn(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s failed (%d): %s", testCase.name, rec.Code, rec.Body.String())
		}
		if statsHits != 0 {
			t.Fatalf("%s MUST query /mempool, not /stats (statsHits=%d)", testCase.name, statsHits)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s invalid JSON: %v", testCase.name, err)
		}
		if _, hasStats := out["asert_drift_seconds"]; hasStats {
			t.Fatalf("%s returned /stats payload instead of mempool info: %s", testCase.name, rec.Body.String())
		}
		if int(out["size"].(float64)) != 1 || int64(out["total_fees_armoids"].(float64)) != 11000 {
			t.Fatalf("%s unexpected size/fees: %v", testCase.name, out)
		}
		if int64(out["min_relay_fee_per_kb"].(float64)) != core.MinRelayFee {
			t.Fatalf("%s unexpected min_relay_fee_per_kb: %v", testCase.name, out["min_relay_fee_per_kb"])
		}
		txs, ok := out["transactions"].([]interface{})
		if !ok || len(txs) != 1 {
			t.Fatalf("%s expected 1 transaction entry, got: %v", testCase.name, out["transactions"])
		}
	}

	// 2. Empty mempool
	mempoolPayload = []core.Transaction{}
	reqEmpty := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/cli?command=getmempoolinfo", nil)
	recEmpty := httptest.NewRecorder()
	handleCLI(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("empty getmempoolinfo failed (%d): %s", recEmpty.Code, recEmpty.Body.String())
	}
	var outEmpty map[string]interface{}
	if err := json.Unmarshal(recEmpty.Body.Bytes(), &outEmpty); err != nil {
		t.Fatalf("empty getmempoolinfo invalid JSON: %v", err)
	}
	if int(outEmpty["size"].(float64)) != 0 || int(outEmpty["bytes"].(float64)) != 0 {
		t.Fatalf("expected size=0, bytes=0 on empty mempool, got: %v", outEmpty)
	}
	if txs, ok := outEmpty["transactions"].([]interface{}); !ok || len(txs) != 0 {
		t.Fatalf("expected non-nil empty transactions array, got: %T (%v)", outEmpty["transactions"], outEmpty["transactions"])
	}
}

func TestCPUMiner_StatusAndRemovedGPUEndpoint(t *testing.T) {
	origAddr := activeAddress
	t.Cleanup(func() {
		stopMiningInternal()
		activeAddress = origAddr
	})
	stopMiningInternal()
	activeAddress = ""

	mux := buildGUIServeMux()

	// 1. Removed GPU detection endpoint must return HTTP 404 (not fallback 200 HTML).
	reqGPU := httptest.NewRequest(http.MethodGet, "/api/mine/gpu/detect", nil)
	recGPU := httptest.NewRecorder()
	mux.ServeHTTP(recGPU, reqGPU)
	if recGPU.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404 for removed /api/mine/gpu/detect, got %d", recGPU.Code)
	}

	// 2. Invalid miner address must return HTTP 400 and leave miningActive false.
	reqBad := httptest.NewRequest(http.MethodPost, "/api/mine/start", strings.NewReader(`{"miner":"not-a-valid-address","threads":2,"delay":1000}`))
	recBad := httptest.NewRecorder()
	handleMineStart(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for invalid miner address, got %d (%s)", recBad.Code, recBad.Body.String())
	}
	miningLock.Lock()
	activeAfterBad := miningActive
	miningLock.Unlock()
	if activeAfterBad {
		t.Fatalf("expected miningActive=false after HTTP 400 rejection")
	}

	// 3. CPU-only /api/mine/status exposes standard keys and omits GPU fields.
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/mine/status", nil)
	recStatus := httptest.NewRecorder()
	handleMineStatus(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for /api/mine/status, got %d", recStatus.Code)
	}
	var statusMap map[string]interface{}
	if err := json.Unmarshal(recStatus.Body.Bytes(), &statusMap); err != nil {
		t.Fatalf("decode /api/mine/status: %v", err)
	}
	if statusMap["active"] != false || statusMap["status_label"] != "Idle" {
		t.Fatalf("expected active=false and status_label='Idle', got active=%v label=%v", statusMap["active"], statusMap["status_label"])
	}
	for _, requiredKey := range []string{"active", "status_label", "hashrate", "elapsed_secs", "session_blocks", "session_reward", "orphaned_blocks", "mined_blocks", "peers_active", "logs", "cpu_cores"} {
		if _, ok := statusMap[requiredKey]; !ok {
			t.Errorf("/api/mine/status missing expected key %q", requiredKey)
		}
	}
	for _, removedKey := range []string{"mode", "cpu_hashrate", "gpu_active", "gpu_hashrate", "gpu_device", "gpu_device_name"} {
		if _, ok := statusMap[removedKey]; ok {
			t.Errorf("/api/mine/status unexpectedly still exposes GPU key %q", removedKey)
		}
	}
}

func TestArmGUIVersionString(t *testing.T) {
	if core.Version != "1.0.12" {
		t.Fatalf("expected core.Version 1.0.12, got %q", core.Version)
	}
	want := "armgui v" + core.Version
	if got := armguiVersionString(); got != want {
		t.Fatalf("armguiVersionString() = %q, want %q", got, want)
	}
}

func TestCPUMiner_StopCancelsInFlightRPCAndRestartsCleanly(t *testing.T) {
	origRPC := rpcURL
	origAddr := activeAddress
	origLogs := getMiningLogs()
	origSync := getCachedSyncStatus()
	t.Cleanup(func() {
		stopMiningInternal()
		rpcURL = origRPC
		activeAddress = origAddr
		setCachedSyncStatus(origSync)
		miningLogsLock.Lock()
		miningLogs = origLogs
		miningLogsLock.Unlock()
	})

	stopMiningInternal()
	miningLogsLock.Lock()
	miningLogs = nil
	miningLogsLock.Unlock()

	var jobActive atomic.Int32
	var blockMode atomic.Bool
	blockMode.Store(true)
	enteredMine := make(chan struct{}, 4)
	ctxCanceled := make(chan struct{}, 4)
	stopCalled := make(chan struct{}, 4)
	var minedHeight atomic.Int32
	minedHeight.Store(20558)

	mockRPC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stats":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":20558,"peers_active":1,"mining_hashrate":12500,"synced":true,"syncing":false,"best_peer_height":20558,"sync_progress":100}`))
		case "/mine/stop":
			select {
			case stopCalled <- struct{}{}:
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"stopped","active":true}`))
		case "/mine":
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			if !jobActive.CompareAndSwap(0, 1) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"status":"error","message":"Another mining job is currently active"}`))
				return
			}
			defer jobActive.Store(0)

			if blockMode.Load() {
				select {
				case enteredMine <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				select {
				case ctxCanceled <- struct{}{}:
				default:
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"status":"error","message":"Mining aborted: context canceled"}`))
				return
			}

			h := int(minedHeight.Add(1))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"block": map[string]interface{}{
					"index": h,
					"hash":  "00000008bc1c5835000000000000000000000000000000000000000000000000",
					"transactions": []map[string]interface{}{
						{"amount": 60 * core.CoinUnit},
					},
				},
				"peers_active": 0,
			})
		case "/chain":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"chain":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockRPC.Close()

	rpcURL = mockRPC.URL
	minerAddr := core.GenesisMiner

	// 1. Start mining while /mine blocks until cancelled
	startBody := strings.NewReader(`{"miner":"` + minerAddr + `","threads":2,"delay":100}`)
	reqStart := httptest.NewRequest(http.MethodPost, "/api/mine/start", startBody)
	recStart := httptest.NewRecorder()
	handleMineStart(recStart, reqStart)
	if recStart.Code != http.StatusOK {
		t.Fatalf("handleMineStart failed: %d %s", recStart.Code, recStart.Body.String())
	}

	select {
	case <-enteredMine:
	case <-time.After(2 * time.Second):
		t.Fatalf("worker did not invoke POST /mine in time")
	}

	// 2. Stop mining; must cancel in-flight POST /mine and wait for worker exit
	reqStop := httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil)
	recStop := httptest.NewRecorder()
	handleMineStop(recStop, reqStop)
	if recStop.Code != http.StatusOK || !strings.Contains(recStop.Body.String(), `"stopped"`) {
		t.Fatalf("handleMineStop unexpected response: %d %s", recStop.Code, recStop.Body.String())
	}

	select {
	case <-ctxCanceled:
	case <-time.After(2 * time.Second):
		t.Fatalf("in-flight /mine r.Context() was not cancelled upon stop")
	}
	select {
	case <-stopCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected /mine/stop notification for external RPC daemon")
	}

	miningLock.Lock()
	activeAfterStop := miningActive
	miningLock.Unlock()
	if activeAfterStop || getMiningHps() != 0 {
		t.Fatalf("expected stopped state (active=%v, hps=%v)", activeAfterStop, getMiningHps())
	}

	// Drain any logs right after stop and ensure no post-stop block or 409 appears
	time.Sleep(150 * time.Millisecond)
	logsAfterStop := getMiningLogs()
	if len(logsAfterStop) == 0 || !strings.Contains(logsAfterStop[len(logsAfterStop)-1], "CPU Mining stopped by user.") {
		t.Fatalf("expected last log to be stop notice, got: %v", logsAfterStop)
	}

	// 3. Restart immediately with fast mining enabled: must NOT hit HTTP 409
	blockMode.Store(false)
	reqRestart := httptest.NewRequest(http.MethodPost, "/api/mine/start", strings.NewReader(`{"miner":"`+minerAddr+`","threads":2,"delay":100}`))
	recRestart := httptest.NewRecorder()
	handleMineStart(recRestart, reqRestart)
	if recRestart.Code != http.StatusOK || !strings.Contains(recRestart.Body.String(), `"started"`) {
		t.Fatalf("restart handleMineStart failed: %d %s", recRestart.Code, recRestart.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range getMiningLogs() {
			if strings.Contains(line, "[RELAYED] Block found #20559") {
				goto minedOK
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("restarted miner did not record block #20559 cleanly; logs=%v", getMiningLogs())

minedOK:
	handleMineStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil))
	for _, line := range getMiningLogs() {
		if strings.Contains(line, "HTTP 409") || strings.Contains(line, "Another mining job is currently active") {
			t.Fatalf("unexpected HTTP 409 overlap notice in mining logs: %v", getMiningLogs())
		}
	}
}

func TestCPUMiner_WaitingForSyncAutoStartAndLegacyV1011Fallback(t *testing.T) {
	origRPC := rpcURL
	origAddr := activeAddress
	origCoreNet := core.ActiveNetwork
	origSync := getCachedSyncStatus()
	origPeers := atomic.LoadInt64(&lastPeersActive)
	t.Cleanup(func() {
		handleMineStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil))
		time.Sleep(50 * time.Millisecond)
		rpcURL = origRPC
		activeAddress = origAddr
		core.ActiveNetwork = origCoreNet
		setCachedSyncStatus(origSync)
		atomic.StoreInt64(&lastPeersActive, origPeers)
	})

	handleMineStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil))

	var synced atomic.Bool
	var mineHits atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		switch r.URL.Path {
		case "/stats":
			isSynced := synced.Load()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"height":            10,
				"blocks":            11,
				"peers_active":      1,
				"best_peer_height":  20558,
				"syncing":           !isSynced,
				"synced":            isSynced,
				"initial_sync_done": isSynced,
				"sync_progress": func() float64 {
					if isSynced {
						return 100.0
					}
					return 5.0
				}(),
				"sync_reason": func() string {
					if isSynced {
						return ""
					}
					return "syncing from peer tip 20558"
				}(),
				"network_id": "ARMX-TESTNET-1",
			})
		case "/mine":
			mineHits.Add(1)
			if !synced.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"node is syncing","syncing":true,"synced":false}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"block": map[string]interface{}{
					"index": 11,
					"hash":  "0000000abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123",
					"transactions": []map[string]interface{}{
						{"amount": 60 * core.CoinUnit},
					},
				},
				"peers_active": 1,
			})
		case "/chain":
			_, _ = w.Write([]byte(`{"chain":[]}`))
		case "/mine/stop":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"stopped"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rpcURL = srv.URL
	core.ActiveNetwork = core.TestnetProfile
	activeAddress = "armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308"

	startBody := `{"miner":"armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308","threads":1,"delay":100}`

	// (a) Clicking Start while unsynced enters "Waiting for Sync" without calling POST /mine.
	startReq := httptest.NewRequest(http.MethodPost, "/api/mine/start", strings.NewReader(startBody))
	startRec := httptest.NewRecorder()
	handleMineStart(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("handleMineStart failed (%d): %s", startRec.Code, startRec.Body.String())
	}
	var startResp map[string]interface{}
	if err := json.Unmarshal(startRec.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("invalid start JSON: %v", err)
	}
	if waitFlag, _ := startResp["waiting_for_sync"].(bool); !waitFlag {
		t.Fatalf("expected waiting_for_sync=true on unsynced start, got: %v", startResp)
	}

	time.Sleep(150 * time.Millisecond)
	stReq := httptest.NewRequest(http.MethodGet, "/api/mine/status", nil)
	stRec := httptest.NewRecorder()
	handleMineStatus(stRec, stReq)
	if stRec.Code != http.StatusOK {
		t.Fatalf("handleMineStatus failed (%d): %s", stRec.Code, stRec.Body.String())
	}
	var stResp map[string]interface{}
	if err := json.Unmarshal(stRec.Body.Bytes(), &stResp); err != nil {
		t.Fatalf("invalid status JSON: %v", err)
	}
	if active, _ := stResp["active"].(bool); !active {
		t.Fatalf("expected active=true in waiting-for-sync mode, got: %v", stResp)
	}
	if waitFlag, _ := stResp["waiting_for_sync"].(bool); !waitFlag {
		t.Fatalf("expected status waiting_for_sync=true, got: %v", stResp)
	}
	if label, _ := stResp["status_label"].(string); label != "Waiting for Sync" {
		t.Fatalf("expected status_label=\"Waiting for Sync\", got %q (%v)", label, stResp)
	}
	if nodeSynced, _ := stResp["node_synced"].(bool); nodeSynced {
		t.Fatalf("expected node_synced=false while unsynced, got: %v", stResp)
	}
	if hps, _ := stResp["hashrate"].(float64); hps != 0 {
		t.Fatalf("expected hashrate=0 while waiting for sync, got %v", hps)
	}
	if hits := mineHits.Load(); hits != 0 {
		t.Fatalf("expected zero POST /mine calls while unsynced, got %d", hits)
	}

	// (b) Flipping mock /stats to synced=true transitions automatically into mining and relays block.
	synced.Store(true)
	deadline := time.Now().Add(5 * time.Second)
	transitioned := false
	relayedLogged := false
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		handleMineStatus(rec, httptest.NewRequest(http.MethodGet, "/api/mine/status", nil))
		var cur map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &cur); err == nil {
			waitFlag, _ := cur["waiting_for_sync"].(bool)
			if !waitFlag && mineHits.Load() > 0 {
				transitioned = true
			}
		}
		for _, line := range getMiningLogs() {
			if strings.Contains(line, "[RELAYED] Block found #11") {
				relayedLogged = true
			}
		}
		if transitioned && relayedLogged {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !transitioned || !relayedLogged {
		t.Fatalf("did not auto-start mining and relay after sync (transitioned=%v, relayedLogged=%v, mineHits=%d, logs=%v)",
			transitioned, relayedLogged, mineHits.Load(), getMiningLogs())
	}

	handleMineStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil))
	stopWait := time.Now().Add(2 * time.Second)
	for time.Now().Before(stopWait) {
		rec := httptest.NewRecorder()
		handleMineStatus(rec, httptest.NewRequest(http.MethodGet, "/api/mine/status", nil))
		var cur map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &cur); err == nil {
			if active, _ := cur["active"].(bool); !active {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	// (c) Stopping while in "Waiting for Sync" cancels cleanly without calling /mine.
	synced.Store(false)
	mineHits.Store(0)
	startRec2 := httptest.NewRecorder()
	handleMineStart(startRec2, httptest.NewRequest(http.MethodPost, "/api/mine/start", strings.NewReader(startBody)))
	if startRec2.Code != http.StatusOK {
		t.Fatalf("second handleMineStart failed (%d): %s", startRec2.Code, startRec2.Body.String())
	}
	time.Sleep(100 * time.Millisecond)
	handleMineStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mine/stop", nil))
	cancelDeadline := time.Now().Add(2 * time.Second)
	stoppedCleanly := false
	for time.Now().Before(cancelDeadline) {
		rec := httptest.NewRecorder()
		handleMineStatus(rec, httptest.NewRequest(http.MethodGet, "/api/mine/status", nil))
		var cur map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &cur); err == nil {
			active, _ := cur["active"].(bool)
			waitFlag, _ := cur["waiting_for_sync"].(bool)
			if !active && !waitFlag {
				stoppedCleanly = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !stoppedCleanly {
		t.Fatalf("expected clean stop during Waiting for Sync")
	}
	if hits := mineHits.Load(); hits != 0 {
		t.Fatalf("expected zero POST /mine calls when stopped during Waiting for Sync, got %d", hits)
	}

	// (d) Legacy v1.0.11 /stats fallback (no "synced" field): testnet requires peers_active > 0, regtest exempt.
	core.ActiveNetwork = core.TestnetProfile
	st0 := computeLegacyV1011SyncStatus(100, 0)
	if st0.Synced || !st0.Syncing {
		t.Fatalf("legacy v1.0.11 fallback on testnet with 0 peers must wait for sync, got: %+v", st0)
	}
	st1 := computeLegacyV1011SyncStatus(100, 1)
	if !st1.Synced || st1.Syncing {
		t.Fatalf("legacy v1.0.11 fallback on testnet with >=1 peers must unlock, got: %+v", st1)
	}

	core.ActiveNetwork = core.RegtestProfile
	stReg := computeLegacyV1011SyncStatus(0, 0)
	if !stReg.Synced || stReg.Syncing {
		t.Fatalf("legacy v1.0.11 fallback on regtest must be synced even with 0 peers, got: %+v", stReg)
	}
}
