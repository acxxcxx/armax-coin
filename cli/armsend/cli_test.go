package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
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
	if code != 0 || !strings.Contains(out, `"fee_armoids"`) || !strings.Contains(out, `"typical_size_bytes":3500`) {
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
