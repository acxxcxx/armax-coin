package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

var (
	sendBinary string
	sendOnce   sync.Once
	sendErr    error
)

func armsendBinary(t *testing.T) string {
	t.Helper()
	sendOnce.Do(func() {
		name := "armsend_cli_test"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		sendBinary = filepath.Join(os.TempDir(), name)
		if out, err := exec.Command("go", "build", "-o", sendBinary, "main.go").CombinedOutput(); err != nil {
			sendErr = &buildError{err: err, out: string(out)}
		}
	})
	if sendErr != nil {
		t.Fatal(sendErr)
	}
	return sendBinary
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return "build armsend: " + e.err.Error() + ": " + e.out }

func runArmsend(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(armsendBinary(t), args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run failed: %v", err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestArmsendRejectsInvalidNetwork(t *testing.T) {
	out, code := runArmsend(t, "-network", "bogus", "-info")
	if code == 0 || !strings.Contains(out, "Invalid network profile") {
		t.Fatalf("expected failure for invalid network, code=%d out=%s", code, out)
	}
}

func TestArmsendRegtestRequiresExplicitFlag(t *testing.T) {
	cmd := exec.Command(armsendBinary(t), "-info")
	cmd.Env = append(os.Environ(), "ARMX_NETWORK=regtest")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "REGTEST") {
		t.Fatalf("regtest must not be selectable via environment:\n%s", out)
	}
}

func TestArmsendInfoShowsTestnetProfile(t *testing.T) {
	out, code := runArmsend(t, "-network", "testnet", "-info")
	if code != 0 || !strings.Contains(out, "TESTNET") || !strings.Contains(out, "RPC Port") {
		t.Fatalf("unexpected -info output (code=%d):\n%s", code, out)
	}
}

func TestArmsendEstimateFeeJSON(t *testing.T) {
	out, code := runArmsend(t, "-estimatefee", "-json")
	if code != 0 || !strings.Contains(out, `"fee_armoids":11000`) || !strings.Contains(out, `"typical_size_bytes":10800`) {
		t.Fatalf("unexpected -estimatefee output (code=%d):\n%s", code, out)
	}
}

func TestArmsendMempoolAgainstFakeDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mempool" {
			http.NotFound(w, r)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "p" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	out, code := runArmsend(t, "-mempool", "-daemon", srv.URL, "-rpcuser", "u", "-rpcpassword", "p")
	if code != 0 || !strings.Contains(out, "0 unconfirmed transactions") {
		t.Fatalf("authorized mempool query failed (code=%d):\n%s", code, out)
	}

	out, code = runArmsend(t, "-mempool", "-daemon", srv.URL)
	if code == 0 || !strings.Contains(out, "HTTP 401") {
		t.Fatalf("unauthorized mempool query should fail with 401 (code=%d):\n%s", code, out)
	}
}

func TestArmsendConfirmationsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	out, code := runArmsend(t, "-confirmations", "deadbeef", "-daemon", srv.URL)
	if code == 0 || !strings.Contains(out, "not found") {
		t.Fatalf("expected not-found failure (code=%d):\n%s", code, out)
	}
}

func runArmsendWithEnv(t *testing.T, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(armsendBinary(t), args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run failed: %v", err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// TestAF05_ArmsendPreSignValidationAndBroadcastFlow verifies that armsend:
//  1. Aborts before signing/broadcast when /balance returns HTTP non-200.
//  2. Aborts before signing/broadcast when X-Network-ID mismatches active network.
//  3. Aborts before /balance and signing when amount or fee exceeds permitted bounds.
//  4. Continues through signing and broadcasts to /tx/new when balance and network ID are valid.
func TestAF05_ArmsendPreSignValidationAndBroadcastFlow(t *testing.T) {
	tempDir := t.TempDir()
	walletPath := filepath.Join(tempDir, "test_wallet.json")
	passphrase := "DisposableTestPass#2026"

	senderWallet, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet sender failed: %v", err)
	}
	defer senderWallet.Zeroize()

	recipientWallet, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet recipient failed: %v", err)
	}
	defer recipientWallet.Zeroize()

	if err := wallet.EncryptAndSaveKeystore(senderWallet, passphrase, walletPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore failed: %v", err)
	}

	var balanceCalls int32
	var broadcastCalls int32
	var balanceStatus int32 = http.StatusOK
	var balanceNetID = "ARMX-TESTNET-1"
	var spendableBal int64 = 50 * core.CoinUnit
	var lastBroadcastTx core.Transaction
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/balance":
			atomic.AddInt32(&balanceCalls, 1)
			mu.Lock()
			netID := balanceNetID
			status := int(atomic.LoadInt32(&balanceStatus))
			bal := spendableBal
			mu.Unlock()
			if netID != "" {
				w.Header().Set("X-Network-ID", netID)
			}
			if status != http.StatusOK {
				http.Error(w, "simulated balance failure", status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"address":%q,"balance":%d,"pending_balance":%d,"spendable_balance":%d,"nonce":0,"pending_nonce":0,"next_nonce":1}`,
				senderWallet.Address, bal, bal, bal)
		case "/tx/new":
			atomic.AddInt32(&broadcastCalls, 1)
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(raw, &lastBroadcastTx)
			mu.Unlock()
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	env := []string{"ARMX_WALLET_PASSPHRASE=" + passphrase}

	// Case 1: Amount and fee over permitted bounds abort before querying /balance or broadcasting
	atomic.StoreInt32(&balanceCalls, 0)
	atomic.StoreInt32(&broadcastCalls, 0)
	out, code := runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount-armoids", fmt.Sprintf("%d", core.MaxSupply+1),
	)
	if code == 0 || !strings.Contains(out, "exceeds maximum supply") {
		t.Fatalf("expected amount > MaxSupply to fail, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&balanceCalls) != 0 || atomic.LoadInt32(&broadcastCalls) != 0 {
		t.Fatalf("expected 0 RPC calls on out-of-bounds amount, got balance=%d broadcast=%d", balanceCalls, broadcastCalls)
	}

	out, code = runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount", "1.0",
		"-fee-armoids", fmt.Sprintf("%d", core.MaxSupply+1),
	)
	if code == 0 || !strings.Contains(out, "exceeds maximum supply") {
		t.Fatalf("expected fee > MaxSupply to fail, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&balanceCalls) != 0 || atomic.LoadInt32(&broadcastCalls) != 0 {
		t.Fatalf("expected 0 RPC calls on out-of-bounds fee, got balance=%d broadcast=%d", balanceCalls, broadcastCalls)
	}

	out, code = runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount-armoids", fmt.Sprintf("%d", core.MaxSupply),
	)
	if code == 0 || !strings.Contains(out, "exceeds maximum supply") {
		t.Fatalf("expected amount + fee > MaxSupply to fail before /balance, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&balanceCalls) != 0 || atomic.LoadInt32(&broadcastCalls) != 0 {
		t.Fatalf("expected 0 RPC calls on amount + fee > MaxSupply, got balance=%d broadcast=%d", balanceCalls, broadcastCalls)
	}

	// Case 2: /balance HTTP non-200 aborts before signing/broadcast
	atomic.StoreInt32(&balanceStatus, http.StatusInternalServerError)
	atomic.StoreInt32(&broadcastCalls, 0)
	out, code = runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount", "1.0",
	)
	if code == 0 || !strings.Contains(out, "HTTP 500") {
		t.Fatalf("expected HTTP 500 on /balance to abort, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&broadcastCalls) != 0 {
		t.Fatalf("expected 0 broadcast calls when /balance returns HTTP 500, got %d", broadcastCalls)
	}

	// Case 3: Incorrect X-Network-ID on /balance aborts before signing/broadcast
	atomic.StoreInt32(&balanceStatus, http.StatusOK)
	mu.Lock()
	balanceNetID = "ARMX-MAINNET-1"
	mu.Unlock()
	atomic.StoreInt32(&broadcastCalls, 0)
	out, code = runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount", "1.0",
	)
	if code == 0 || !strings.Contains(out, "Network mismatch") {
		t.Fatalf("expected Network mismatch error on wrong X-Network-ID, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&broadcastCalls) != 0 {
		t.Fatalf("expected 0 broadcast calls on X-Network-ID mismatch, got %d", broadcastCalls)
	}

	// Case 4: Valid matching network and sufficient balance signs and broadcasts successfully
	mu.Lock()
	balanceNetID = "ARMX-TESTNET-1"
	spendableBal = 50 * core.CoinUnit
	lastBroadcastTx = core.Transaction{}
	mu.Unlock()
	atomic.StoreInt32(&broadcastCalls, 0)

	out, code = runArmsendWithEnv(t, env,
		"-network", "testnet",
		"-daemon", srv.URL,
		"-wallet", walletPath,
		"-to", recipientWallet.Address,
		"-amount", "2.5",
	)
	if code != 0 {
		t.Fatalf("expected valid send to succeed, code=%d out=%s", code, out)
	}
	if atomic.LoadInt32(&broadcastCalls) != 1 {
		t.Fatalf("expected 1 broadcast call to /tx/new, got %d", broadcastCalls)
	}
	mu.Lock()
	tx := lastBroadcastTx
	mu.Unlock()
	if tx.Amount != 250_000_000 || tx.Fee != 11000 || tx.Nonce != 1 {
		t.Fatalf("unexpected broadcasted transaction fields: amount=%d fee=%d nonce=%d", tx.Amount, tx.Fee, tx.Nonce)
	}
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("broadcasted transaction failed ML-DSA-65 verification: %v", err)
	}
}
