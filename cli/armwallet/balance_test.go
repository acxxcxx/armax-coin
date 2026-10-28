package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestCheckBalanceRendersAccountDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/balance" || r.URL.Query().Get("address") != "armx1test" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"address":"armx1test","balance":150000000,"pending_balance":150000000,"nonce":2,"pending_nonce":2,"next_nonce":3}`))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { checkBalance("armx1test", srv.URL, http.Get) })
	for _, want := range []string{"armx1test", "1.50000000 tARMX", "Next TX Nonce: #3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckBalanceShowsImmatureRewards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"address":"a","balance":300,"immature_balance":200,"spendable_balance":100,"coinbase_maturity":10}`))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { checkBalance("a", srv.URL, http.Get) })
	if !strings.Contains(out, "Immature Rewards") || !strings.Contains(out, "< 10 block confirmations") {
		t.Fatalf("immature section missing:\n%s", out)
	}
}

func TestCheckBalanceFallsBackToRawOnInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain text"))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { checkBalance("a", srv.URL, http.Get) })
	if !strings.Contains(out, "Raw response") || !strings.Contains(out, "plain text") {
		t.Fatalf("raw fallback missing:\n%s", out)
	}
}

func TestRescanWalletParsesHistoryArray(t *testing.T) {
	ksPath := t.TempDir() + "/wallet.json"
	addr := "armx17nqa4uyewe8elsn4w8qfygjv7aafkkzj25r8antzdmecwnhpd28sayl2yl"
	if err := os.WriteFile(ksPath, []byte(`{"version":2,"address":"`+addr+`"}`), 0600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/history" || r.URL.Query().Get("address") != addr {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`[
			{"block_index":1,"timestamp":1700000000,"status":"confirmed","tx":{"sender":"SYSTEM","recipient":"` + addr + `","amount":6000000000,"fee":0,"nonce":1}},
			{"block_index":2,"timestamp":1700000120,"status":"confirmed","tx":{"sender":"` + addr + `","recipient":"armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd","amount":100000000,"fee":1000,"nonce":1}}
		]`))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { rescanWallet(ksPath, srv.URL, http.Get) })
	for _, want := range []string{"(2 txs found)", "[1] Mined: 60.00000000", "[2] Sent: 1.00000000"} {
		if !strings.Contains(out, want) {
			t.Errorf("rescan output missing %q:\n%s", want, out)
		}
	}
}
