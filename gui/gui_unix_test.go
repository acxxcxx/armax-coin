//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
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

func TestUnixGUISecureHandler_UnauthorizedRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/info", handleWalletInfo)
	mux.HandleFunc("/api/wallet/create", handleWalletCreate)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	// 1. Missing token on /api/wallet/info
	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized for missing token, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Unauthorized: Invalid or missing GUI session token") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	// 2. Missing token on /api/wallet/create
	reqPost := httptest.NewRequest("POST", "http://127.0.0.1:18088/api/wallet/create", bytes.NewBufferString(`{"passphrase":"password12345"}`))
	wPost := httptest.NewRecorder()
	handler.ServeHTTP(wPost, reqPost)

	if wPost.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized on wallet create without token, got %d", wPost.Code)
	}
	if !strings.Contains(wPost.Body.String(), "Unauthorized: Invalid or missing GUI session token") {
		t.Fatalf("unexpected body: %s", wPost.Body.String())
	}
}

func TestUnixGUISecureHandler_InvalidTokenRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/info", handleWalletInfo)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info", nil)
	req.Header.Set("X-Armx-GUI-Token", "invalid_token_1234567890abcdef")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized for invalid token, got %d", w.Code)
	}
}

func TestUnixGUISecureHandler_HeaderTokenAccepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/info", handleWalletInfo)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info", nil)
	req.Header.Set("X-Armx-GUI-Token", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for valid header token, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"crypto":"ML-DSA-65 (FIPS 204)"`) {
		t.Fatalf("expected valid wallet info response, got: %s", w.Body.String())
	}
}

func TestUnixGUISecureHandler_CookieTokenAccepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/info", handleWalletInfo)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info", nil)
	req.AddCookie(&http.Cookie{
		Name:  "armax_gui_token",
		Value: token,
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for valid cookie token, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestUnixGUISecureHandler_QueryTokenAccepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/info", handleWalletInfo)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info?token="+token, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for valid query param token, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestUnixGUISecureHandler_RootPageAllowedWithoutToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebUI)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK on root path without token, got %d", w.Code)
	}
}

func TestUnixGUIHandleWebUI_SetsCookieAndInjectsToken(t *testing.T) {
	token := initSessionToken()

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/", nil)
	w := httptest.NewRecorder()
	handleWebUI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from handleWebUI, got %d", w.Code)
	}

	// 1. Verify Set-Cookie header
	cookies := w.Result().Cookies()
	var authCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "armax_gui_token" {
			authCookie = c
			break
		}
	}
	if authCookie == nil {
		t.Fatalf("expected 'armax_gui_token' cookie in response, but none was found")
	}
	if authCookie.Value != token {
		t.Fatalf("expected cookie value %q, got %q", token, authCookie.Value)
	}
	if !authCookie.HttpOnly {
		t.Fatalf("expected HttpOnly cookie")
	}
	if authCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected SameSite=Strict on auth cookie")
	}

	// 2. Verify Session Token injection into HTML/JavaScript
	body := w.Body.String()
	if strings.Contains(body, "{{SESSION_TOKEN}}") {
		t.Fatalf("placeholder {{SESSION_TOKEN}} was not replaced in HTML output")
	}
	expectedConst := fmt.Sprintf(`const GUI_SESSION_TOKEN = "%s";`, token)
	if !strings.Contains(body, expectedConst) {
		t.Fatalf("expected injected session token constant %q in HTML, but got: %s", expectedConst, body[:500])
	}
}

func TestUnixGUIWalletCreate_ValidationWithSession(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wallet/create", handleWalletCreate)

	token := initSessionToken()
	handler := secureGUIHandler(mux, token)

	// 1. Passphrase too short (< 12 chars)
	reqShort := httptest.NewRequest("POST", "http://127.0.0.1:18088/api/wallet/create", bytes.NewBufferString(`{"passphrase":"short"}`))
	reqShort.Header.Set("X-Armx-GUI-Token", token)
	wShort := httptest.NewRecorder()
	handler.ServeHTTP(wShort, reqShort)

	if wShort.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for short passphrase, got %d", wShort.Code)
	}
	if !strings.Contains(wShort.Body.String(), "Passphrase must be at least 12 characters") {
		t.Fatalf("expected error message about short passphrase, got: %s", wShort.Body.String())
	}
}

func TestUnixGUIHandleWebUI_ContainsOnboardingModalAndCard(t *testing.T) {
	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/", nil)
	w := httptest.NewRecorder()
	handleWebUI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from handleWebUI, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, `id="setupWalletModal"`) {
		t.Fatalf("expected setupWalletModal in Web UI HTML")
	}
	if !strings.Contains(body, `id="restoreWalletModal"`) {
		t.Fatalf("expected restoreWalletModal in Web UI HTML")
	}
	if !strings.Contains(body, `id="noWalletCard"`) {
		t.Fatalf("expected noWalletCard in Web UI HTML")
	}
	if !strings.Contains(body, `executeModalCreateWallet`) {
		t.Fatalf("expected executeModalCreateWallet in Web UI JavaScript")
	}
	if !strings.Contains(body, `executeModalRestoreWallet`) {
		t.Fatalf("expected executeModalRestoreWallet in Web UI JavaScript")
	}
}

func TestUnixGUIWalletInfo_MissingWalletReturnsEmptyAddress(t *testing.T) {
	origAddr := activeAddress
	defer func() { activeAddress = origAddr }()
	activeAddress = ""

	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/wallet/info", nil)
	w := httptest.NewRecorder()
	handleWalletInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from handleWalletInfo, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"address":""`) {
		t.Fatalf("expected empty address when no wallet is active, got: %s", body)
	}
	if !strings.Contains(body, `"initialized":false`) {
		t.Fatalf("expected initialized:false when no wallet is active, got: %s", body)
	}
}

// TestUnixSafeP2PDisplayFilter verifies that home residential nodes with ephemeral client ports
// are excluded from GUI active peer display, while DNS seeds and public nodes with open listening P2P ports are preserved.
func TestUnixSafeP2PDisplayFilter(t *testing.T) {
	testSeeds := []string{"testnet-seed1.armax-coin.cloud:19335", "testnet-seed2.armax-coin.cloud:19335"}
	p2pPort := "19335"

	// 1. Ephemeral residential client ports must be rejected to protect home nodes
	if isSafeP2PDisplayPeer("198.51.100.25:58539", p2pPort, testSeeds) {
		t.Errorf("expected 198.51.100.25:58539 to be rejected (ephemeral port)")
	}
	if isSafeP2PDisplayPeer("192.168.1.100:49152", p2pPort, testSeeds) {
		t.Errorf("expected 192.168.1.100:49152 to be rejected (ephemeral port)")
	}

	// 2. DNS seeds must be accepted
	if !isSafeP2PDisplayPeer("testnet-seed1.armax-coin.cloud:19335", p2pPort, testSeeds) {
		t.Errorf("expected testnet-seed1.armax-coin.cloud:19335 to be accepted (seed)")
	}
	if !isSafeP2PDisplayPeer("seed.custom-domain.org", p2pPort, testSeeds) {
		t.Errorf("expected seed.custom-domain.org to be accepted (domain)")
	}

	// 3. Public IP with open listening P2P port must be accepted
	if !isSafeP2PDisplayPeer("198.51.100.25:19335", p2pPort, testSeeds) {
		t.Errorf("expected 198.51.100.25:19335 to be accepted (standard listening port)")
	}

	// 4. filterSafePeersJSON on string array
	rawStringArray := []byte(`["198.51.100.25:58539", "testnet-seed1.armax-coin.cloud:19335", "198.51.100.30:19335"]`)
	netProfile := core.NetworkProfile{
		P2PPort:      "19335",
		DefaultSeeds: testSeeds,
	}
	filteredRaw := filterSafePeersJSON(rawStringArray, netProfile)
	var filteredStrings []string
	if err := json.Unmarshal(filteredRaw, &filteredStrings); err != nil {
		t.Fatalf("failed to unmarshal filtered JSON: %v", err)
	}
	if len(filteredStrings) != 2 {
		t.Fatalf("expected 2 filtered peers, got %d: %v", len(filteredStrings), filteredStrings)
	}
	for _, p := range filteredStrings {
		if strings.Contains(p, "58539") {
			t.Fatalf("found ephemeral peer in filtered output: %s", p)
		}
	}
}

func TestUnixGUIHandlePeers_FiltersEphemeralPort(t *testing.T) {
	origRPC := rpcURL
	defer func() { rpcURL = origRPC }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/peers") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["198.51.100.25:58539", "testnet-seed1.armax-coin.cloud:19335", "198.51.100.30:19335"]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	rpcURL = srv.URL

	// 1. Test /api/peers
	req := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/peers", nil)
	w := httptest.NewRecorder()
	handlePeers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from handlePeers, got %d", w.Code)
	}
	resBody := w.Body.String()
	if strings.Contains(resBody, "58539") {
		t.Fatalf("handlePeers output contains ephemeral port: %s", resBody)
	}
	if !strings.Contains(resBody, "testnet-seed1.armax-coin.cloud:19335") {
		t.Fatalf("handlePeers output missing seed: %s", resBody)
	}

	// 2. Test /api/cli?command=getpeerinfo
	reqCLI := httptest.NewRequest("GET", "http://127.0.0.1:18088/api/cli?command=getpeerinfo", nil)
	wCLI := httptest.NewRecorder()
	handleCLI(wCLI, reqCLI)

	if wCLI.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK from handleCLI getpeerinfo, got %d", wCLI.Code)
	}
	cliBody := wCLI.Body.String()
	if strings.Contains(cliBody, "58539") {
		t.Fatalf("handleCLI getpeerinfo output contains ephemeral port: %s", cliBody)
	}
	if !strings.Contains(cliBody, "testnet-seed1.armax-coin.cloud:19335") {
		t.Fatalf("handleCLI getpeerinfo output missing seed: %s", cliBody)
	}
}
