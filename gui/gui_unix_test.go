//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestUnixGUIVerifyNodeNetwork_MatchingNetworkAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":100,"blocks":101,"network_id":"ARMX-TESTNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if err != nil {
		t.Fatalf("expected nil error on matching network, got: %v", err)
	}
}

func TestUnixGUIVerifyNodeNetwork_MissingHeaderRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":100,"blocks":101,"network_id":"ARMX-TESTNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if err == nil {
		t.Fatalf("expected error on missing X-Network-ID header, got nil")
	}
	if !strings.Contains(err.Error(), "missing X-Network-ID response header") {
		t.Fatalf("expected error containing 'missing X-Network-ID response header', got: %v", err)
	}
}

func TestUnixGUIVerifyNodeNetwork_MismatchedNetworkRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":50,"blocks":51,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if err == nil {
		t.Fatalf("expected error on mismatched network, but got nil")
	}
	if !strings.Contains(err.Error(), "network mismatch") {
		t.Fatalf("expected error containing 'network mismatch', got: %v", err)
	}
}

func TestUnixGUIVerifyNodeNetwork_EmptyNetworkIDRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":10,"network_id":""}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if err == nil {
		t.Fatalf("expected error on empty network_id, got nil")
	}
	if !strings.Contains(err.Error(), "empty network_id") {
		t.Fatalf("expected error containing 'empty network_id', got: %v", err)
	}
}

func TestUnixGUIVerifyNodeNetwork_MalformedStatsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if err == nil {
		t.Fatalf("expected error on malformed stats, got nil")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected error containing 'malformed', got: %v", err)
	}
}

func TestUnixGUIHandleStats_NetworkMismatchRejected(t *testing.T) {
	foreignSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":100,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer foreignSrv.Close()

	origRPC := rpcURL
	defer func() { rpcURL = origRPC }()
	rpcURL = foreignSrv.URL

	origNet := core.ActiveNetwork
	defer func() { core.ActiveNetwork = origNet }()
	core.ActiveNetwork = core.TestnetProfile

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()

	handleStats(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected HTTP 502 Bad Gateway on foreign network stats, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Node network mismatch") {
		t.Fatalf("expected body containing 'Node network mismatch', got: %s", w.Body.String())
	}
}

func TestUnixGUIHandleBalance_NetworkMismatchRejected(t *testing.T) {
	foreignSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":100,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer foreignSrv.Close()

	origRPC := rpcURL
	defer func() { rpcURL = origRPC }()
	rpcURL = foreignSrv.URL

	origNet := core.ActiveNetwork
	defer func() { core.ActiveNetwork = origNet }()
	core.ActiveNetwork = core.TestnetProfile

	req := httptest.NewRequest("GET", "/api/balance", nil)
	w := httptest.NewRecorder()

	handleBalance(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected HTTP 502 Bad Gateway on foreign balance, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Node network validation failed") {
		t.Fatalf("expected body containing 'Node network validation failed', got: %s", w.Body.String())
	}
}

func TestUnixGUIHandleSend_NetworkMismatchRejected(t *testing.T) {
	foreignSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"height":100,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer foreignSrv.Close()

	origRPC := rpcURL
	defer func() { rpcURL = origRPC }()
	rpcURL = foreignSrv.URL

	origNet := core.ActiveNetwork
	defer func() { core.ActiveNetwork = origNet }()
	core.ActiveNetwork = core.TestnetProfile

	payload := fmt.Sprintf(`{"to":"%s","amount":"1.0","passphrase":"test"}`, core.GenesisMiner)
	req := httptest.NewRequest("POST", "/api/send", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()

	handleSend(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected HTTP 502 Bad Gateway on foreign send, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Node network validation failed") {
		t.Fatalf("expected body containing 'Node network validation failed', got: %s", w.Body.String())
	}
}
