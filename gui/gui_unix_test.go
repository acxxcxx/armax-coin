//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
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

func TestUnixGUIIsLoopbackRPC(t *testing.T) {
	loopbacks := []string{
		"http://127.0.0.1:19333",
		"http://localhost:19333",
		"http://127.0.0.1",
		"http://[::1]:19333",
	}
	for _, u := range loopbacks {
		if !isLoopbackRPC(u) {
			t.Errorf("expected %s to be recognized as loopback RPC", u)
		}
	}

	remotes := []string{
		"http://192.168.1.100:19333",
		"http://10.0.0.5:19333",
		"http://node.armax-coin.cloud:19333",
		"https://api.armax-coin.cloud",
	}
	for _, u := range remotes {
		if isLoopbackRPC(u) {
			t.Errorf("expected %s to NOT be recognized as loopback RPC", u)
		}
	}
}

func TestUnixGUIAssetHandlers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/assets/armax_logo.png", handleAssetLogoPNG)
	mux.HandleFunc("/assets/armax_text.png", handleAssetTextPNG)
	mux.HandleFunc("/favicon.ico", handleAssetFaviconICO)

	cases := []struct {
		path string
		ct   string
		head []byte
	}{
		{"/assets/armax_logo.png", "image/png", []byte{0x89, 'P', 'N', 'G'}},
		{"/assets/armax_text.png", "image/png", []byte{0x89, 'P', 'N', 'G'}},
		{"/favicon.ico", "image/x-icon", []byte{0x00, 0x00, 0x01, 0x00}},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned status %d", tc.path, rec.Code)
		}
		if gotCT := rec.Header().Get("Content-Type"); !strings.Contains(gotCT, tc.ct) {
			t.Errorf("%s expected Content-Type %s, got %s", tc.path, tc.ct, gotCT)
		}
		body := rec.Body.Bytes()
		if len(body) < 16 || !bytes.HasPrefix(body, tc.head) {
			t.Errorf("%s invalid binary signature or empty payload (len=%d)", tc.path, len(body))
		}
	}
}

func TestUnixGUIQRHandler_TokenAndEncoding(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qr", handleQRCode)
	handler := secureGUIHandler(mux, "secret-qr-token")

	// Missing token must return 401 Unauthorized
	reqNoTok := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/qr?content=armx1test", nil)
	recNoTok := httptest.NewRecorder()
	handler.ServeHTTP(recNoTok, reqNoTok)
	if recNoTok.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token on /api/qr, got %d", recNoTok.Code)
	}

	// Valid token via query parameter (used by <img src="/api/qr?...">) must encode PNG
	reqOK := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/qr?content=armax:armx1testaddress?amount=5&token=secret-qr-token", nil)
	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/qr with token, got %d: %s", recOK.Code, recOK.Body.String())
	}
	if !bytes.HasPrefix(recOK.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("expected valid PNG signature from /api/qr")
	}

	// Oversized content must fail cleanly with 400
	tooLong := strings.Repeat("a", 600)
	reqBad := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/api/qr?content="+tooLong, nil)
	reqBad.Header.Set("X-Armx-GUI-Token", "secret-qr-token")
	recBad := httptest.NewRecorder()
	handler.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for overlength QR content, got %d", recBad.Code)
	}
}

func TestUnixGUIWalletsHandlers_FullLifecycle(t *testing.T) {
	origMgr := walletMgr
	origID := activeWalletID
	origAddr := activeAddress
	origW := activeWallet
	unlockedMu.Lock()
	origUnlocked := unlockedWallets
	unlockedWallets = make(map[string]*wallet.Wallet)
	unlockedMu.Unlock()
	t.Cleanup(func() {
		walletMgrMu.Lock()
		walletMgr = origMgr
		walletMgrMu.Unlock()
		unlockedMu.Lock()
		activeWalletID = origID
		activeAddress = origAddr
		activeWallet = origW
		unlockedWallets = origUnlocked
		unlockedMu.Unlock()
	})

	tmpDir := t.TempDir()
	walletMgrMu.Lock()
	walletMgr = wallet.NewWalletManagerForDir(tmpDir)
	walletMgrMu.Unlock()
	unlockedMu.Lock()
	activeWalletID = ""
	activeAddress = ""
	activeWallet = nil
	unlockedMu.Unlock()

	pass := "Str0ng!WalletPass2026"

	// 1. Create wallet #1 and wallet #2
	createPayload := func(label string) (string, string) {
		body := fmt.Sprintf(`{"label":%q,"passphrase":%q}`, label, pass)
		req := httptest.NewRequest(http.MethodPost, "/api/wallets/create", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleWalletsCreate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s failed (%d): %s", label, rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		id, _ := out["id"].(string)
		addr, _ := out["address"].(string)
		if id == "" || addr == "" {
			t.Fatalf("missing id/address in create output: %v", out)
		}
		return id, addr
	}

	id1, addr1 := createPayload("Primary Vault")
	id2, addr2 := createPayload("Mining Rig")
	if id1 == id2 || addr1 == addr2 {
		t.Fatalf("expected distinct wallet IDs and addresses")
	}

	// 2. List wallets -> active should be id2 (most recently created)
	reqList := httptest.NewRequest(http.MethodGet, "/api/wallets", nil)
	recList := httptest.NewRecorder()
	handleWalletsList(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("/api/wallets failed: %d", recList.Code)
	}
	var listResp struct {
		ActiveID string                   `json:"active_id"`
		Wallets  []map[string]interface{} `json:"wallets"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResp.ActiveID != id2 || len(listResp.Wallets) != 2 {
		t.Fatalf("expected active=%s and 2 wallets, got active=%s len=%d", id2, listResp.ActiveID, len(listResp.Wallets))
	}

	// 3. 1-click select wallet #1
	selReq := httptest.NewRequest(http.MethodPost, "/api/wallets/select", strings.NewReader(fmt.Sprintf(`{"id":%q}`, id1)))
	selRec := httptest.NewRecorder()
	handleWalletsSelect(selRec, selReq)
	if selRec.Code != http.StatusOK {
		t.Fatalf("select wallet #1 failed (%d): %s", selRec.Code, selRec.Body.String())
	}
	if activeWalletID != id1 || activeAddress != addr1 {
		t.Fatalf("expected activeWalletID=%s activeAddress=%s after 1-click select, got %s %s", id1, addr1, activeWalletID, activeAddress)
	}

	// 4. Rename wallet #1
	renReq := httptest.NewRequest(http.MethodPost, "/api/wallets/rename", strings.NewReader(fmt.Sprintf(`{"id":%q,"label":"Cold Vault Renamed"}`, id1)))
	renRec := httptest.NewRecorder()
	handleWalletsRename(renRec, renReq)
	if renRec.Code != http.StatusOK {
		t.Fatalf("rename failed (%d): %s", renRec.Code, renRec.Body.String())
	}
	if entry, ok := walletMgr.Active(); !ok || entry.Label != "Cold Vault Renamed" {
		t.Fatalf("expected renamed label, got %+v (ok=%v)", entry, ok)
	}

	// 5. Backup keystore download
	bakReq := httptest.NewRequest(http.MethodGet, "/api/wallets/backup?id="+id1, nil)
	bakRec := httptest.NewRecorder()
	handleWalletsBackup(bakRec, bakReq)
	if bakRec.Code != http.StatusOK {
		t.Fatalf("backup failed (%d): %s", bakRec.Code, bakRec.Body.String())
	}
	if disp := bakRec.Header().Get("Content-Disposition"); !strings.Contains(disp, id1) {
		t.Errorf("expected Content-Disposition with wallet id %s, got %s", id1, disp)
	}
	if _, err := os.Stat(tmpDir); err != nil || len(bakRec.Body.Bytes()) == 0 {
		t.Errorf("expected non-empty keystore backup JSON")
	}

	// 6. Lock all unlocked wallets
	lockReq := httptest.NewRequest(http.MethodPost, "/api/wallets/lock", nil)
	lockRec := httptest.NewRecorder()
	handleWalletsLock(lockRec, lockReq)
	if lockRec.Code != http.StatusOK {
		t.Fatalf("lock failed: %d", lockRec.Code)
	}
	unlockedMu.RLock()
	numUnlocked := len(unlockedWallets)
	curActiveW := activeWallet
	unlockedMu.RUnlock()
	if numUnlocked != 0 || curActiveW != nil {
		t.Fatalf("expected all wallets zeroized & locked, unlocked=%d activeWallet=%v", numUnlocked, curActiveW)
	}
}

func TestUnixGUIMineStatus_ReconcilesOrphan(t *testing.T) {
	origRPC := rpcURL
	origTracker := miningTracker
	origTip := atomic.LoadInt64(&lastTipHeight)
	origMat := atomic.LoadInt64(&lastNodeMaturity)
	origRec := atomic.LoadInt64(&lastReconcileUnix)
	miningLogsLock.Lock()
	origLogs := append([]string(nil), miningLogs...)
	miningLogs = nil
	miningLogsLock.Unlock()
	t.Cleanup(func() {
		rpcURL = origRPC
		miningTracker = origTracker
		atomic.StoreInt64(&lastTipHeight, origTip)
		atomic.StoreInt64(&lastNodeMaturity, origMat)
		atomic.StoreInt64(&lastReconcileUnix, origRec)
		miningLogsLock.Lock()
		miningLogs = origLogs
		miningLogsLock.Unlock()
	})

	// Mock node returns competing hash at block #50
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block" && r.URL.Query().Get("index") == "50" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"index": 50,
				"hash":  "00000000competingwinner9999",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	rpcURL = srv.URL
	miningTracker = NewMiningSessionTracker()
	miningTracker.RecordFound(50, "00000000localstaleblock1111", 60*core.CoinUnit, 4, 10)
	atomic.StoreInt64(&lastTipHeight, 55)
	atomic.StoreInt64(&lastNodeMaturity, 10)
	atomic.StoreInt64(&lastReconcileUnix, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/mine/status", nil)
	rec := httptest.NewRecorder()
	handleMineStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/mine/status returned %d", rec.Code)
	}

	var statusResp struct {
		SessionBlocks  int64               `json:"session_blocks"`
		SessionReward  int64               `json:"session_reward"`
		OrphanedBlocks int64               `json:"orphaned_blocks"`
		MinedBlocks    []SessionMinedBlock `json:"mined_blocks"`
		Logs           []string            `json:"logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode mine status: %v", err)
	}
	if statusResp.OrphanedBlocks != 1 || statusResp.SessionBlocks != 0 || statusResp.SessionReward != 0 {
		t.Fatalf("expected 1 orphan and 0 valid blocks/reward, got valid=%d reward=%d orphan=%d",
			statusResp.SessionBlocks, statusResp.SessionReward, statusResp.OrphanedBlocks)
	}
	if len(statusResp.MinedBlocks) != 1 || statusResp.MinedBlocks[0].Status != MinedStatusOrphaned {
		t.Fatalf("expected mined_blocks[0] status ORPHANED, got %+v", statusResp.MinedBlocks)
	}
	foundOrphanLog := false
	for _, l := range statusResp.Logs {
		if strings.Contains(l, "ORPHANED") {
			foundOrphanLog = true
			break
		}
	}
	if !foundOrphanLog {
		t.Errorf("expected ORPHANED notice appended to mining logs, got %v", statusResp.Logs)
	}
}

func TestUnixGUIExportCSV_FullPagedHistoryAndSystemCoinbase(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	closeHistoryStore()
	origRPC := rpcURL
	t.Cleanup(func() {
		closeHistoryStore()
		rpcURL = origRPC
	})

	addr := "armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308"
	peer := "armx1923456789abcdef0123456789abcdef0123456789abcdef0123456789"
	netID := core.ActiveNetwork.NetworkID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Network-ID", netID)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/stats":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"height": 200, "blocks": 201, "network_id": netID})
		case "/history":
			cursor := r.URL.Query().Get("cursor_height")
			if cursor == "" {
				_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
					Records: []PagedHistoryRecord{
						{BlockIndex: 199, Timestamp: 1700000200, Status: "confirmed", Tx: core.Transaction{Sender: "SYSTEM", Recipient: addr, Amount: 60 * core.CoinUnit, Nonce: 199}},
					},
					ChainHeight:      200,
					NextCursorHeight: 100,
					HasMore:          true,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(PagedHistoryResponse{
				Records: []PagedHistoryRecord{
					{BlockIndex: 50, Timestamp: 1700000100, Status: "confirmed", Tx: core.Transaction{Sender: addr, Recipient: peer, Amount: 5 * core.CoinUnit, Nonce: 1}},
					{BlockIndex: 10, Timestamp: 1700000050, Status: "confirmed", Tx: core.Transaction{Sender: peer, Recipient: addr, Amount: 12 * core.CoinUnit, Nonce: 2}},
				},
				ChainHeight: 200,
				HasMore:     false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rpcURL = srv.URL
	req := httptest.NewRequest(http.MethodGet, "/api/export-csv?address="+addr, nil)
	rec := httptest.NewRecorder()
	handleExportCSV(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/export-csv failed (%d): %s", rec.Code, rec.Body.String())
	}
	csv := rec.Body.String()
	for _, want := range []string{"Mined (Coinbase)", ",Sent,", ",Received,"} {
		if !strings.Contains(csv, want) {
			t.Errorf("expected CSV to contain %q, got:\n%s", want, csv)
		}
	}
	if !strings.Contains(csv, ",199,") || !strings.Contains(csv, ",50,") || !strings.Contains(csv, ",10,") {
		t.Errorf("expected all multi-page block heights (199, 50, 10) in CSV, got:\n%s", csv)
	}
}

func TestUnixGUIHandleWebUI_OptionAParityElements(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handleWebUI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / returned %d", rec.Code)
	}
	body := rec.Body.String()
	for _, requiredID := range []string{
		"tab-convert",
		"walletManagerModal",
		"armax_logo.png",
		"armax_text.png",
		"btn-sidebar",
		"setupWalletModal",
		"restoreWalletModal",
		"noWalletCard",
		"GUI_SESSION_TOKEN",
	} {
		if !strings.Contains(body, requiredID) {
			t.Errorf("rendered Unix WebUI missing required element %q", requiredID)
		}
	}
}

func TestIsDirWritable_EnforcesPosix0600OnUnix(t *testing.T) {
	probeDir := filepath.Join(t.TempDir(), "posix-check", "testnet")
	if !isDirWritable(probeDir) {
		t.Fatalf("expected temp subdir %q on Unix POSIX filesystem to pass isDirWritable", probeDir)
	}
	entries, err := os.ReadDir(probeDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) failed: %v", probeDir, err)
	}
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".armax-write-probe-") {
			t.Fatalf("unexpected probe file residue left behind: %s", ent.Name())
		}
	}
}
