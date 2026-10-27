package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestFetchBlockTemplateSendsAuthAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getblocktemplate" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("X-Mine-Key") != "k" {
			t.Errorf("missing X-Mine-Key header")
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "p" {
			t.Errorf("missing basic auth")
		}
		_ = json.NewEncoder(w).Encode(BlockTemplate{CoinbaseValue: 42})
	}))
	defer srv.Close()

	tmpl, err := fetchBlockTemplate(srv.Client(), srv.URL, "k", "u", "p")
	if err != nil {
		t.Fatalf("fetchBlockTemplate: %v", err)
	}
	if tmpl.CoinbaseValue != 42 {
		t.Fatalf("CoinbaseValue = %d, want 42", tmpl.CoinbaseValue)
	}
}

func TestFetchBlockTemplateRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := fetchBlockTemplate(srv.Client(), srv.URL, "", "", ""); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}
}

func TestFetchBlockTemplateRejectsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := fetchBlockTemplate(srv.Client(), srv.URL, "", "", ""); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestSubmitSolvedBlockAcceptedAndRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/block/submit" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var b core.Block
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Errorf("body is not a block: %v", err)
		}
		if b.Index == 7 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(SubmitBlockResponse{Status: "rejected", Message: "stale tip"})
			return
		}
		_ = json.NewEncoder(w).Encode(SubmitBlockResponse{Status: "accepted", Height: b.Index})
	}))
	defer srv.Close()

	res, err := submitSolvedBlock(srv.Client(), srv.URL, core.Block{Index: 3}, "", "", "")
	if err != nil || res.Status != "accepted" || res.Height != 3 {
		t.Fatalf("accepted case: res=%+v err=%v", res, err)
	}

	res, err = submitSolvedBlock(srv.Client(), srv.URL, core.Block{Index: 7}, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "stale tip") || res == nil {
		t.Fatalf("rejected case: res=%+v err=%v", res, err)
	}
}
