//go:build testhooks

package node

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

func TestSyncGate_BlocksMineAndGetBlockTemplateUntilSynced(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet): %v", err)
	}

	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	t.Setenv("ARMX_MINE_KEY", "sync-gate-test-key")

	// 1. Public peer node ahead at height #3
	pubDir := t.TempDir()
	bcPub, err := core.NewBlockchainWithStoragePath(1, filepath.Join(pubDir, "pub.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath pub: %v", err)
	}
	defer bcPub.Close()

	wPub, _ := wallet.NewWallet()
	for i := 0; i < 3; i++ {
		if _, err := bcPub.MineBlockWithErr(wPub.Address); err != nil {
			t.Fatalf("pub mine #%d: %v", i+1, err)
		}
	}

	portPub := getFreePort(t)
	psPub := NewPeerServer(portPub, bcPub, pubDir, "ARMX-TESTNET-1")
	psPub.AllowPrivatePeers = true
	psPub.RequireTLS = false
	RegisterPeerServer(psPub)
	defer func() {
		UnregisterPeerServer(psPub)
		psPub.Stop()
	}()
	go psPub.StartServer()
	pubAddr := "127.0.0.1:" + portPub

	// 2. Fresh syncing node at height #0 with public seed configured
	freshDir := t.TempDir()
	bcFresh, err := core.NewBlockchainWithStoragePath(1, filepath.Join(freshDir, "fresh.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath fresh: %v", err)
	}
	defer bcFresh.Close()

	portFresh := getFreePort(t)
	psFresh := NewPeerServer(portFresh, bcFresh, freshDir, "ARMX-TESTNET-1")
	psFresh.AllowPrivatePeers = true
	psFresh.RequireTLS = false
	psFresh.MarkConfiguredSeeds([]string{pubAddr})
	psFresh.SetSeedPeers([]string{pubAddr})
	RegisterPeerServer(psFresh)
	defer func() {
		UnregisterPeerServer(psFresh)
		psFresh.Stop()
	}()

	limiter := newIPRateLimiter()
	defer limiter.Close()
	rpcFresh := NewRPCHandler(bcFresh, psFresh, limiter, &ServerConfig{
		NetworkID: "ARMX-TESTNET-1",
		RPCBind:   "127.0.0.1",
		MineKey:   "sync-gate-test-key",
	})

	// Before sync completes: GetSyncStatus, /stats, /getblocktemplate, and POST /mine must report syncing / 503
	if st := psFresh.GetSyncStatus(); st.Synced || !st.Syncing {
		t.Fatalf("expected unsynced initial status, got: %+v", st)
	}

	reqStats1 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	recStats1 := httptest.NewRecorder()
	rpcFresh.ServeHTTP(recStats1, reqStats1)
	if recStats1.Code != http.StatusOK {
		t.Fatalf("/stats returned %d", recStats1.Code)
	}
	var stBody map[string]interface{}
	if err := json.Unmarshal(recStats1.Body.Bytes(), &stBody); err != nil {
		t.Fatalf("unmarshal /stats: %v", err)
	}
	if stBody["synced"] != false || stBody["syncing"] != true {
		t.Fatalf("expected /stats synced=false syncing=true, got synced=%v syncing=%v", stBody["synced"], stBody["syncing"])
	}

	reqGBT1 := httptest.NewRequest(http.MethodGet, "/getblocktemplate", nil)
	recGBT1 := httptest.NewRecorder()
	rpcFresh.ServeHTTP(recGBT1, reqGBT1)
	if recGBT1.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 from /getblocktemplate before sync, got %d (%s)", recGBT1.Code, recGBT1.Body.String())
	}

	wMin, _ := wallet.NewWallet()
	minePayload, _ := json.Marshal(map[string]interface{}{"miner": wMin.Address, "threads": 1})
	reqMine1 := httptest.NewRequest(http.MethodPost, "/mine", bytes.NewReader(minePayload))
	reqMine1.Host = "127.0.0.1"
	reqMine1.RemoteAddr = "127.0.0.1:50001"
	reqMine1.Header.Set("Content-Type", "application/json")
	reqMine1.Header.Set("X-Mine-Key", "sync-gate-test-key")
	recMine1 := httptest.NewRecorder()
	rpcFresh.ServeHTTP(recMine1, reqMine1)
	if recMine1.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 from POST /mine before sync, got %d (%s)", recMine1.Code, recMine1.Body.String())
	}
	if len(bcFresh.Chain) != 1 {
		t.Fatalf("expected chain unchanged at genesis before sync, got len=%d", len(bcFresh.Chain))
	}

	// Wait briefly until pub listener is accepting and run SyncBestPeer
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		psFresh.SyncBestPeer()
		if psFresh.GetSyncStatus().Synced && len(bcFresh.Chain) == 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	finalSync := psFresh.GetSyncStatus()
	if !finalSync.Synced || finalSync.Syncing || finalSync.LocalHeight != 3 || !finalSync.InitialSyncDone {
		t.Fatalf("expected full sync completion at height #3, got: %+v", finalSync)
	}
	if psPub.GetBanScore("127.0.0.1") != 0 {
		t.Fatalf("expected 0 ban score on public node after clean sync, got %d", psPub.GetBanScore("127.0.0.1"))
	}

	// Now /getblocktemplate and POST /mine must unlock and return 200 OK
	reqGBT2 := httptest.NewRequest(http.MethodGet, "/getblocktemplate", nil)
	recGBT2 := httptest.NewRecorder()
	rpcFresh.ServeHTTP(recGBT2, reqGBT2)
	if recGBT2.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from /getblocktemplate after sync, got %d (%s)", recGBT2.Code, recGBT2.Body.String())
	}

	reqMine2 := httptest.NewRequest(http.MethodPost, "/mine", bytes.NewReader(minePayload))
	reqMine2.Host = "127.0.0.1"
	reqMine2.RemoteAddr = "127.0.0.1:50002"
	reqMine2.Header.Set("Content-Type", "application/json")
	reqMine2.Header.Set("X-Mine-Key", "sync-gate-test-key")
	recMine2 := httptest.NewRecorder()
	rpcFresh.ServeHTTP(recMine2, reqMine2)
	body2, _ := io.ReadAll(recMine2.Body)
	if recMine2.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from POST /mine after sync, got %d (%s)", recMine2.Code, string(body2))
	}
	if len(bcFresh.Chain) != 5 {
		t.Fatalf("expected mined block #4 committed on synced chain (len 5), got len=%d", len(bcFresh.Chain))
	}
}

func TestSyncGate_DoesNotBlockRegtestOrPeerlessTests(t *testing.T) {
	core.LockNetworkForTesting(t)
	core.SetNetworkForTesting(true)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	// 1. Testnet peer server with no configured seeds and 0 peers stays unblocked for isolated tests
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet): %v", err)
	}
	bcTest := core.NewBlockchain(1)
	defer bcTest.Close()
	psTest := NewPeerServer(getFreePort(t), bcTest, t.TempDir(), "ARMX-TESTNET-1")
	if st := psTest.GetSyncStatus(); !st.Synced || st.Syncing {
		t.Fatalf("expected peerless/seedless isolated testnet server to report Synced=true, got %+v", st)
	}

	// 2. Regtest with seeds still stays unblocked unless actively downloading
	if err := core.SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest): %v", err)
	}
	bcReg := core.NewBlockchain(1)
	defer bcReg.Close()
	psReg := NewPeerServer(getFreePort(t), bcReg, t.TempDir(), "ARMX-REGTEST-1")
	psReg.MarkConfiguredSeeds([]string{"127.0.0.1:29999"})
	if st := psReg.GetSyncStatus(); !st.Synced {
		t.Fatalf("expected regtest to remain mineable without waiting for seeds, got %+v", st)
	}
}
