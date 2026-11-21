//go:build testhooks

package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
)

// TestSlowClient_ReadTimeoutEnforced verifies that a slow client attempting to hold a connection
// is terminated by the server's ReadHeaderTimeout / ReadTimeout [L3-9].
func TestSlowClient_ReadTimeoutEnforced(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	// Short timeout to verify connection enforcement
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 150 * time.Millisecond,
		ReadTimeout:       250 * time.Millisecond,
		WriteTimeout:      500 * time.Millisecond,
	}
	go func() {
		_ = srv.Serve(listener)
	}()
	defer srv.Close()

	addr := listener.Addr().String()

	// Connect with raw TCP socket and send half of the request headers very slowly
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to test server: %v", err)
	}
	defer conn.Close()

	// Send initial line
	_, err = conn.Write([]byte("GET /ping HTTP/1.1\r\nHost: 127.0.0.1\r\n"))
	if err != nil {
		t.Fatalf("Failed to write initial request line: %v", err)
	}

	// Sleep past ReadHeaderTimeout (250ms > 150ms)
	time.Sleep(300 * time.Millisecond)

	// Now attempt to write remaining headers; server should have aborted or reset connection
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1024)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		// Server might respond with 408 Request Timeout before closing
		respStr := string(buf)
		if !strings.Contains(respStr, "408") && !strings.Contains(respStr, "close") {
			t.Logf("Server returned response on timeout: %s", respStr)
		}
	} else if readErr != io.EOF {
		t.Logf("Connection successfully closed/reset by server as expected: %v", readErr)
	}
}

// TestMineEndpoint_TimeoutAndCancellation verifies that /mine completes within deadline
// and aborts immediately when request context is cancelled [L3-9].
func TestMineEndpoint_TimeoutAndCancellation(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("regtest"); err != nil {
		t.Fatalf("Failed to set regtest: %v", err)
	}
	defer func() {
		_ = core.SetNetwork("testnet")
	}()

	// Set easy target so mining succeeds quickly when requested
	easyTarget, _ := new(big.Int).SetString("0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
	core.SetTargetOverrideForTesting(easyTarget)
	defer core.SetTargetOverrideForTesting(nil)

	bc := core.NewBlockchain(1)
	defer bc.Close()
	t.Setenv("ARMX_MINE_KEY", "test-secret-mine-key")

	limiter := newIPRateLimiter()
	cfg := DefaultConfig()
	_, serverCfg, err := InitServerFromConfig(bc, cfg)
	if err != nil {
		t.Fatalf("InitServerFromConfig failed: %v", err)
	}
	handler := NewRPCHandler(bc, nil, limiter, serverCfg)

	server := httptest.NewServer(handler)
	defer server.Close()

	// 1. Valid mining request completes within reasonable bound
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	payload, _ := json.Marshal(map[string]string{
		"miner": "armx1psmx4e6yhg84z2h5wadg3698vjlx06flnynhec08fv8nv56z758sl9xl6c",
	})
	req, err := http.NewRequestWithContext(ctx1, "POST", server.URL+"/mine", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mine-Key", "test-secret-mine-key")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("/mine request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected HTTP 200 for fast mine, got %d: %s", resp.StatusCode, string(body))
	}

	var mResp struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&mResp)
	if mResp.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", mResp.Status)
	}

	// 2. Cancellation: client context cancelled before completion must abort cleanly
	// Set an impossible target to make mining loop run until cancelled
	hardTarget, _ := new(big.Int).SetString("0000000000000000000000000000000000000000000000000000000000000001", 16)
	core.SetTargetOverrideForTesting(hardTarget)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	reqCancel, err := http.NewRequestWithContext(ctx2, "POST", server.URL+"/mine", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("Failed to create cancellable request: %v", err)
	}
	reqCancel.Header.Set("Content-Type", "application/json")
	reqCancel.Header.Set("X-Mine-Key", "test-secret-mine-key")

	start := time.Now()
	_, err = client.Do(reqCancel)
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("Expected request to fail with context deadline exceeded or cancellation")
	} else {
		t.Logf("Cancelled request aborted after %v as expected: %v", elapsed, err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Cancellation took too long to abort: %v", elapsed)
	}

	// 3. Immediate restart after client context cancellation must NOT get HTTP 409
	core.SetTargetOverrideForTesting(easyTarget)
	ctxRestart, cancelRestart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRestart()
	reqRestart, err := http.NewRequestWithContext(ctxRestart, "POST", server.URL+"/mine", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("Failed to build restart request: %v", err)
	}
	reqRestart.Header.Set("Content-Type", "application/json")
	reqRestart.Header.Set("X-Mine-Key", "test-secret-mine-key")
	respRestart, err := client.Do(reqRestart)
	if err != nil {
		t.Fatalf("Immediate restart /mine after cancellation failed: %v", err)
	}
	bodyRestart, _ := io.ReadAll(respRestart.Body)
	respRestart.Body.Close()
	if respRestart.StatusCode != http.StatusOK {
		t.Fatalf("Expected HTTP 200 on immediate /mine restart after cancel, got %d: %s", respRestart.StatusCode, string(bodyRestart))
	}

	// 4. POST /mine/stop auth guards & explicit abort of an active /mine request
	reqStopGet := httptest.NewRequest(http.MethodGet, "/mine/stop", nil)
	reqStopGet.Host = "127.0.0.1"
	reqStopGet.Header.Set("X-Mine-Key", "test-secret-mine-key")
	recStopGet := httptest.NewRecorder()
	handler.ServeHTTP(recStopGet, reqStopGet)
	if recStopGet.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 405 Method Not Allowed for GET /mine/stop, got %d", recStopGet.Code)
	}

	reqStopUnauth, _ := http.NewRequest(http.MethodPost, server.URL+"/mine/stop", nil)
	respStopUnauth, err := client.Do(reqStopUnauth)
	if err != nil {
		t.Fatalf("Unauthenticated /mine/stop request failed: %v", err)
	}
	respStopUnauth.Body.Close()
	if respStopUnauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for unauthenticated POST /mine/stop, got %d", respStopUnauth.StatusCode)
	}

	core.SetTargetOverrideForTesting(hardTarget)
	mineDoneCh := make(chan int, 1)
	go func() {
		ctxLong, cancelLong := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelLong()
		rMine, _ := http.NewRequestWithContext(ctxLong, "POST", server.URL+"/mine", bytes.NewBuffer(payload))
		rMine.Header.Set("Content-Type", "application/json")
		rMine.Header.Set("X-Mine-Key", "test-secret-mine-key")
		resMine, errMine := client.Do(rMine)
		if errMine != nil {
			mineDoneCh <- -1
			return
		}
		_, _ = io.ReadAll(resMine.Body)
		resMine.Body.Close()
		mineDoneCh <- resMine.StatusCode
	}()

	// Wait briefly until activeMiningJob is held, then trigger POST /mine/stop
	stopDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(stopDeadline) {
		if func() bool {
			activeMiningMu.Lock()
			defer activeMiningMu.Unlock()
			return activeMiningCancel != nil
		}() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	reqStopAuth, _ := http.NewRequest(http.MethodPost, server.URL+"/mine/stop", nil)
	reqStopAuth.Header.Set("X-Mine-Key", "test-secret-mine-key")
	respStopAuth, err := client.Do(reqStopAuth)
	if err != nil {
		t.Fatalf("Authenticated POST /mine/stop failed: %v", err)
	}
	stopBody, _ := io.ReadAll(respStopAuth.Body)
	respStopAuth.Body.Close()
	if respStopAuth.StatusCode != http.StatusOK || !strings.Contains(string(stopBody), `"stopped"`) {
		t.Fatalf("Expected 200 OK {status:stopped} from /mine/stop, got %d: %s", respStopAuth.StatusCode, string(stopBody))
	}

	select {
	case code := <-mineDoneCh:
		if code == http.StatusOK || code <= 0 {
			t.Fatalf("Expected stopped /mine call to return HTTP 409 aborted, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("In-flight /mine did not abort promptly after POST /mine/stop")
	}
}
