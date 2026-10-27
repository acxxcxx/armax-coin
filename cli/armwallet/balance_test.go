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
	for _, want := range []string{"armx1test", "1.50000000 ARMX", "Next TX Nonce: #3"} {
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
