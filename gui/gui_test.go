//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/armaxcoincore/armax-coin/core"
	"github.com/armaxcoincore/armax-coin/wallet"
)

// TestGUIBalanceResponseGetConfirmed verifies GetConfirmed logic
func TestGUIBalanceResponseGetConfirmed(t *testing.T) {
	// 1. Confirmed balance present
	b1 := BalanceResponse{
		Balance:          1000,
		ConfirmedBalance: 2000,
	}
	if b1.GetConfirmed() != 2000 {
		t.Fatalf("Expected 2000, got %d", b1.GetConfirmed())
	}

	// 2. Confirmed balance zero -> fall back to Balance
	b2 := BalanceResponse{
		Balance:          1500,
		ConfirmedBalance: 0,
	}
	if b2.GetConfirmed() != 1500 {
		t.Fatalf("Expected 1500, got %d", b2.GetConfirmed())
	}
}

// TestGUIStatsResponseJSON verifies deserialization of node stats JSON
func TestGUIStatsResponseJSON(t *testing.T) {
	raw := `{"height":22,"blocks":23,"peers_active":1,"peers_known":3,"difficulty":7,"network_id":"ARMX-TESTNET-1"}`
	var s StatsResponse
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if s.Height != 22 || s.Blocks != 23 || s.PeersActive != 1 || s.NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("Unexpected stats response values: %+v", s)
	}
}

// TestGUIFormatArmx verifies decimal formatting
func TestGUIFormatArmx(t *testing.T) {
	prev := activeNetwork
	defer func() { activeNetwork = prev }()

	activeNetwork = core.MainnetProfile
	str := formatArmx(6000000000)
	if str != "60.00000000 ARMX" {
		t.Fatalf("Expected '60.00000000 ARMX', got '%s'", str)
	}

	activeNetwork = core.TestnetProfile
	str = formatArmx(6000000000)
	if str != "60.00000000 tARMX" {
		t.Fatalf("Expected '60.00000000 tARMX' on testnet, got '%s'", str)
	}
}

// TestGUIFormatHashrate verifies hashrate formatting across scales
func TestGUIFormatHashrate(t *testing.T) {
	cases := []struct {
		hps      float64
		expected string
	}{
		{500, "500 H/s"},
		{1500, "1.50 kH/s"},
		{2_500_000, "2.50 MH/s"},
		{3_000_000_000, "3.00 GH/s"},
	}

	for _, c := range cases {
		res := formatHashrate(c.hps)
		if res != c.expected {
			t.Errorf("formatHashrate(%f) = %s, want %s", c.hps, res, c.expected)
		}
	}
}

// TestGUIAddressBookModel verifies the address book table model
func TestGUIAddressBookModel(t *testing.T) {
	model := &AddressBookModel{
		items: []*Contact{
			{Name: "Alice Mining", Address: "armx1alice", DateAdded: "2026-10-17"},
			{Name: "Bob Cold", Address: "armx1bob", DateAdded: "2026-10-17"},
		},
	}

	if model.RowCount() != 2 {
		t.Fatalf("Expected 2 rows, got %d", model.RowCount())
	}
	if model.Value(0, 0) != "Alice Mining" {
		t.Fatalf("Expected Alice Mining, got %v", model.Value(0, 0))
	}
	if model.Value(1, 1) != "armx1bob" {
		t.Fatalf("Expected armx1bob, got %v", model.Value(1, 1))
	}
}

// TestGUIStatsResponseMiningHashrate verifies mining_hashrate json unmarshaling
func TestGUIStatsResponseMiningHashrate(t *testing.T) {
	raw := `{"height":50,"mining_hashrate":1250000.5,"network_id":"ARMX-TESTNET-1"}`
	var s StatsResponse
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if s.MiningHashrate != 1250000.5 {
		t.Fatalf("Expected MiningHashrate 1250000.5, got %f", s.MiningHashrate)
	}
}

// TestGUITxModel verifies TxModel row count, column retrieval, and bounds safety
func TestGUITxModel(t *testing.T) {
	model := &TxModel{
		items: []*TxItem{
			{
				Index:     0,
				Type:      "INCOMING",
				Amount:    "+10.00000000 ARMX",
				Height:    "42",
				TxID:      "txhash123456789",
				Timestamp: "2026-10-21 12:00:00",
			},
			{
				Index:     1,
				Type:      "OUTGOING",
				Amount:    "-2.50000000 ARMX",
				Height:    "43",
				TxID:      "txhash987654321",
				Timestamp: "2026-10-21 12:05:00",
			},
		},
	}

	if model.RowCount() != 2 {
		t.Fatalf("Expected RowCount 2, got %d", model.RowCount())
	}

	// Verify all column values for row 0
	if model.Value(0, 0) != "INCOMING" {
		t.Fatalf("Col 0: expected INCOMING, got %v", model.Value(0, 0))
	}
	if model.Value(0, 1) != "+10.00000000 ARMX" {
		t.Fatalf("Col 1: expected +10.00000000 ARMX, got %v", model.Value(0, 1))
	}
	if model.Value(0, 2) != "42" {
		t.Fatalf("Col 2: expected 42, got %v", model.Value(0, 2))
	}
	if model.Value(0, 3) != "txhash123456789" {
		t.Fatalf("Col 3: expected txhash123456789, got %v", model.Value(0, 3))
	}
	if model.Value(0, 4) != "2026-10-21 12:00:00" {
		t.Fatalf("Col 4: expected 2026-10-21 12:00:00, got %v", model.Value(0, 4))
	}

	// Verify out-of-bounds safety
	if model.Value(-1, 0) != "" {
		t.Fatalf("Expected empty string for row -1, got %v", model.Value(-1, 0))
	}
	if model.Value(2, 0) != "" {
		t.Fatalf("Expected empty string for row 2, got %v", model.Value(2, 0))
	}
	if model.Value(0, -1) != "" {
		t.Fatalf("Expected empty string for col -1, got %v", model.Value(0, -1))
	}
	if model.Value(0, 5) != "" {
		t.Fatalf("Expected empty string for col 5, got %v", model.Value(0, 5))
	}
}

// TestGUIAddressBookModel_OutOfBounds verifies AddressBookModel boundary protections
func TestGUIAddressBookModel_OutOfBounds(t *testing.T) {
	model := &AddressBookModel{
		items: []*Contact{
			{Name: "Alice", Address: "armx1alice", DateAdded: "2026-10-21"},
		},
	}

	if model.Value(-1, 0) != "" {
		t.Fatalf("Expected empty string for row -1, got %v", model.Value(-1, 0))
	}
	if model.Value(5, 0) != "" {
		t.Fatalf("Expected empty string for row 5, got %v", model.Value(5, 0))
	}
	if model.Value(0, -1) != "" {
		t.Fatalf("Expected empty string for col -1, got %v", model.Value(0, -1))
	}
	if model.Value(0, 3) != "" {
		t.Fatalf("Expected empty string for col 3, got %v", model.Value(0, 3))
	}
}

// TestGUIFormatArmx_EdgeCases verifies decimal precision across minimum and zero amounts
func TestGUIFormatArmx_EdgeCases(t *testing.T) {
	prev := activeNetwork
	defer func() { activeNetwork = prev }()
	activeNetwork = core.MainnetProfile

	cases := []struct {
		armoids  int64
		expected string
	}{
		{0, "0.00000000 ARMX"},
		{1, "0.00000001 ARMX"},
		{10, "0.00000010 ARMX"},
		{100_000_000, "1.00000000 ARMX"},
		{6_000_000_000, "60.00000000 ARMX"},
	}

	for _, c := range cases {
		res := formatArmx(c.armoids)
		if res != c.expected {
			t.Errorf("formatArmx(%d) = %q, want %q", c.armoids, res, c.expected)
		}
	}
}

// TestGUIVerifyNodeNetwork_MatchingNetworkAccepted proves that a daemon reporting the exact
// expected NetworkID is accepted.
func TestGUIVerifyNodeNetwork_MatchingNetworkAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			_, _ = w.Write([]byte(`{"height":100,"blocks":101,"network_id":"ARMX-TESTNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	status := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if !status.Online {
		t.Fatalf("expected node to be online, got error: %v", status.Err)
	}
	if !status.Match {
		t.Fatalf("expected network to match, got error: %v", status.Err)
	}
	if status.NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("expected NetworkID 'ARMX-TESTNET-1', got '%s'", status.NetworkID)
	}
	if status.Err != nil {
		t.Fatalf("expected nil Err, got %v", status.Err)
	}
}

// TestGUIVerifyNodeNetwork_MismatchedNetworkRejected proves that a daemon reporting a different
// NetworkID is strictly rejected (fail-closed).
func TestGUIVerifyNodeNetwork_MismatchedNetworkRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
			_, _ = w.Write([]byte(`{"height":50,"blocks":51,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	status := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if !status.Online {
		t.Fatalf("expected node to be detected online, got: %v", status.Err)
	}
	if status.Match {
		t.Fatalf("security violation: expected network mismatch rejection, but connection was accepted!")
	}
	if status.NetworkID != "ARMX-MAINNET-1" {
		t.Fatalf("expected NetworkID 'ARMX-MAINNET-1', got '%s'", status.NetworkID)
	}
	if status.Err == nil || !strings.Contains(status.Err.Error(), "network mismatch") {
		t.Fatalf("expected error containing 'network mismatch', got: %v", status.Err)
	}
}

// TestGUIVerifyNodeNetwork_EmptyNetworkIDRejected proves that a daemon with empty or missing
// network_id is rejected.
func TestGUIVerifyNodeNetwork_EmptyNetworkIDRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			_, _ = w.Write([]byte(`{"height":10,"network_id":""}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	status := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if !status.Online {
		t.Fatalf("expected node online")
	}
	if status.Match {
		t.Fatalf("security violation: empty network_id must be rejected")
	}
	if status.Err == nil || !strings.Contains(status.Err.Error(), "empty network_id") {
		t.Fatalf("expected empty network_id error, got: %v", status.Err)
	}
}

// TestGUIVerifyNodeNetwork_MalformedStatsRejected proves that non-JSON or invalid /stats
// responses cause verification to fail closed.
func TestGUIVerifyNodeNetwork_MalformedStatsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body>502 Bad Gateway</body></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	status := verifyNodeNetwork(srv.URL, "ARMX-TESTNET-1")
	if !status.Online {
		t.Fatalf("expected node online")
	}
	if status.Match {
		t.Fatalf("security violation: malformed response must be rejected")
	}
	if status.Err == nil || !strings.Contains(status.Err.Error(), "malformed") {
		t.Fatalf("expected malformed error, got: %v", status.Err)
	}
}

// TestGUIVerifyNodeNetwork_DaemonOffline proves that when daemon is unreachable,
// status.Online is false.
func TestGUIVerifyNodeNetwork_DaemonOffline(t *testing.T) {
	status := verifyNodeNetwork("http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if status.Online {
		t.Fatalf("expected node to be offline on unreachable port")
	}
	if status.Match {
		t.Fatalf("expected match to be false on unreachable port")
	}
}

// TestGUIIsNodeOnline_NetworkAware proves that isNodeOnline rejects daemons from
// another network and only returns true when network_id matches activeNetwork.
func TestGUIIsNodeOnline_NetworkAware(t *testing.T) {
	prev := activeNetwork
	defer func() { activeNetwork = prev }()
	activeNetwork = core.TestnetProfile

	testnetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		_, _ = w.Write([]byte(`{"height":10,"network_id":"ARMX-TESTNET-1"}`))
	}))
	defer testnetSrv.Close()

	mainnetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
		_, _ = w.Write([]byte(`{"height":10,"network_id":"ARMX-MAINNET-1"}`))
	}))
	defer mainnetSrv.Close()

	// Matching network -> true
	if !isNodeOnline(testnetSrv.URL) {
		t.Fatalf("expected isNodeOnline to return true for matching testnet node")
	}

	// Foreign network -> false (fail-closed!)
	if isNodeOnline(mainnetSrv.URL) {
		t.Fatalf("security violation: isNodeOnline returned true for foreign mainnet node!")
	}
}

// TestGUIVerifyNodeNetwork_LocalMainnetProtectionPreserved verifies that MainnetLaunched
// remains false and local mainnet checks are intact.
func TestGUIVerifyNodeNetwork_LocalMainnetProtectionPreserved(t *testing.T) {
	if core.MainnetLaunched {
		t.Fatalf("security violation: core.MainnetLaunched is unexpectedly true!")
	}
}

// TestGUIExecuteConsoleCommand_NetworkMatchAccepted verifies that valid commands on a matching
// daemon execute properly and return HTTP 200 and data.
func TestGUIExecuteConsoleCommand_NetworkMatchAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		switch r.URL.Path {
		case "/stats":
			_, _ = w.Write([]byte(`{"height":105,"blocks":106,"network_id":"ARMX-TESTNET-1"}`))
		case "/peers":
			_, _ = w.Write([]byte(`[{"addr":"127.0.0.1:8333"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// 1. getblockchaininfo -> queries /stats
	out := executeConsoleCommand("getblockchaininfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "HTTP 200") || !strings.Contains(out, `"height":105`) {
		t.Fatalf("expected successful HTTP 200 output for matching node, got: %s", out)
	}

	// 2. getpeerinfo -> queries /peers after verifying /stats
	outPeers := executeConsoleCommand("getpeerinfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(outPeers, "HTTP 200") || !strings.Contains(outPeers, "127.0.0.1:8333") {
		t.Fatalf("expected successful peer info for matching node, got: %s", outPeers)
	}
}

// TestGUIExecuteConsoleCommand_NetworkMismatchRejected verifies that RPC console commands
// are strictly rejected when daemon network does not match.
func TestGUIExecuteConsoleCommand_NetworkMismatchRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
		if r.URL.Path == "/stats" {
			_, _ = w.Write([]byte(`{"height":200,"network_id":"ARMX-MAINNET-1"}`))
			return
		}
		if r.URL.Path == "/peers" {
			_, _ = w.Write([]byte(`[{"addr":"mainnet.peer:8333"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	out := executeConsoleCommand("getblockchaininfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "CLI error: network validation failed") || !strings.Contains(out, "network mismatch") {
		t.Fatalf("expected network validation failed error, got: %s", out)
	}
	if strings.Contains(out, "HTTP 200") {
		t.Fatalf("security violation: mismatched network request should not have executed HTTP query, got: %s", out)
	}

	outPeers := executeConsoleCommand("getpeerinfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(outPeers, "CLI error: network validation failed") {
		t.Fatalf("expected network validation failed for getpeerinfo on mismatched daemon, got: %s", outPeers)
	}
}

// TestGUIExecuteConsoleCommand_EmptyNetworkIDRejected verifies that RPC console commands
// fail closed when daemon returns an empty network_id.
func TestGUIExecuteConsoleCommand_EmptyNetworkIDRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		_, _ = w.Write([]byte(`{"height":5,"network_id":""}`))
	}))
	defer srv.Close()

	out := executeConsoleCommand("getblockchaininfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "CLI error: network validation failed") || !strings.Contains(out, "empty network_id") {
		t.Fatalf("expected empty network_id error, got: %s", out)
	}
}

// TestGUIExecuteConsoleCommand_DaemonOfflineRejected verifies that RPC console commands
// fail cleanly when daemon is unreachable without panic.
func TestGUIExecuteConsoleCommand_DaemonOfflineRejected(t *testing.T) {
	out := executeConsoleCommand("getblockchaininfo", "http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if !strings.Contains(out, "CLI error: daemon offline or unreachable") {
		t.Fatalf("expected daemon offline error, got: %s", out)
	}
}

// TestGUIExecuteConsoleCommand_MalformedStatsRejected verifies that RPC console commands
// fail closed when daemon returns malformed HTML/JSON.
func TestGUIExecuteConsoleCommand_MalformedStatsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		_, _ = w.Write([]byte(`<html>Error 500</html>`))
	}))
	defer srv.Close()

	out := executeConsoleCommand("getblockchaininfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "CLI error: network validation failed") || !strings.Contains(out, "malformed") {
		t.Fatalf("expected malformed stats rejection, got: %s", out)
	}
}

// TestGUIExecuteConsoleCommand_LocalCommands verifies local non-network commands.
func TestGUIExecuteConsoleCommand_LocalCommands(t *testing.T) {
	// 1. help
	helpOut := executeConsoleCommand("help", "http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if !strings.Contains(helpOut, "ARMAX GUI CLI (read-only commands)") {
		t.Fatalf("expected help output, got: %s", helpOut)
	}

	// 2. clear
	clearOut := executeConsoleCommand("clear", "http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if clearOut != "" {
		t.Fatalf("expected empty output for clear, got: %q", clearOut)
	}

	// 3. unknown command
	unknownOut := executeConsoleCommand("unknown_cmd", "http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if !strings.Contains(unknownOut, "Unknown command") {
		t.Fatalf("expected Unknown command, got: %s", unknownOut)
	}

	// 4. invalid address for getbalance
	badAddrOut := executeConsoleCommand("getbalance not_an_address", "http://127.0.0.1:59999", "ARMX-TESTNET-1")
	if !strings.Contains(badAddrOut, "Invalid address") {
		t.Fatalf("expected Invalid address error, got: %s", badAddrOut)
	}
}

// TestGUIQueryValidatedNodeEndpoint_MissingHeaderRejected proves that any endpoint response
// lacking the mandatory X-Network-ID header is rejected immediately with "missing X-Network-ID response header".
func TestGUIQueryValidatedNodeEndpoint_MissingHeaderRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Intentionally omit X-Network-ID header
		_, _ = w.Write([]byte(`[{"addr":"1.2.3.4:8333"}]`))
	}))
	defer srv.Close()

	_, _, err := queryValidatedNodeEndpoint(srv.URL, "/peers", "ARMX-TESTNET-1", 2*time.Second)
	if err == nil {
		t.Fatalf("security violation: expected error on missing X-Network-ID header, got nil")
	}
	if !strings.Contains(err.Error(), "missing X-Network-ID response header") {
		t.Fatalf("expected 'missing X-Network-ID response header', got: %v", err)
	}

	out := executeConsoleCommand("getpeerinfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "missing X-Network-ID response header") || strings.Contains(out, "1.2.3.4") {
		t.Fatalf("security violation: leaked data without mandatory header: %s", out)
	}
}

// TestGUIQueryValidatedNodeEndpoint_WrongHeaderRejected proves that any endpoint response
// whose X-Network-ID header does not match expectedNetID is rejected immediately and body discarded.
func TestGUIQueryValidatedNodeEndpoint_WrongHeaderRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
		_, _ = w.Write([]byte(`[{"addr":"secret.mainnet.peer:8333"}]`))
	}))
	defer srv.Close()

	data, _, err := queryValidatedNodeEndpoint(srv.URL, "/peers", "ARMX-TESTNET-1", 2*time.Second)
	if err == nil {
		t.Fatalf("security violation: expected error on X-Network-ID header mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "network mismatch on response header") {
		t.Fatalf("expected 'network mismatch on response header', got: %v", err)
	}
	if data != nil {
		t.Fatalf("security violation: body was not discarded on header mismatch")
	}

	out := executeConsoleCommand("getpeerinfo", srv.URL, "ARMX-TESTNET-1")
	if !strings.Contains(out, "network validation failed") || strings.Contains(out, "secret.mainnet.peer") {
		t.Fatalf("security violation: executeConsoleCommand leaked mismatched peer data: %s", out)
	}
}

// TestGUIQueryValidatedNodeEndpoint_CorrectHeaderAccepted proves that responses with
// valid matching X-Network-ID headers are accepted.
func TestGUIQueryValidatedNodeEndpoint_CorrectHeaderAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		_, _ = w.Write([]byte(`[{"addr":"127.0.0.1:8333"}]`))
	}))
	defer srv.Close()

	data, code, err := queryValidatedNodeEndpoint(srv.URL, "/peers", "ARMX-TESTNET-1", 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error on valid header: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", code)
	}
	if !strings.Contains(string(data), "127.0.0.1:8333") {
		t.Fatalf("unexpected body: %s", string(data))
	}
}

// TestGUIQueryValidatedNodeEndpoint_DaemonSwitchedMidSession simulates a daemon process
// switching networks on the same port between requests, proving TOCTOU cannot leak data.
func TestGUIQueryValidatedNodeEndpoint_DaemonSwitchedMidSession(t *testing.T) {
	reqCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		if reqCount == 1 {
			// First call (/stats) is from legitimate Testnet node
			w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
			_, _ = w.Write([]byte(`{"height":50,"network_id":"ARMX-TESTNET-1"}`))
			return
		}
		// Subsequent call is from an attacker or foreign daemon replacing the port with Mainnet
		w.Header().Set("X-Network-ID", "ARMX-MAINNET-1")
		_, _ = w.Write([]byte(`[{"addr":"secret.mainnet.peer:18333"}]`))
	}))
	defer srv.Close()

	// Call 1: valid testnet node
	statsData, _, sErr := queryValidatedNodeEndpoint(srv.URL, "/stats", "ARMX-TESTNET-1", 2*time.Second)
	if sErr != nil {
		t.Fatalf("unexpected error on valid stats call: %v", sErr)
	}
	if !strings.Contains(string(statsData), "ARMX-TESTNET-1") {
		t.Fatalf("expected valid stats response, got: %s", string(statsData))
	}

	// Call 2: port now occupied by mainnet node -> MUST be rejected on the response itself!
	peersData, _, pErr := queryValidatedNodeEndpoint(srv.URL, "/peers", "ARMX-TESTNET-1", 2*time.Second)
	if pErr == nil {
		t.Fatalf("CRITICAL SECURITY VIOLATION: daemon switch went undetected, got data: %s", string(peersData))
	}
	if !strings.Contains(pErr.Error(), "network mismatch") {
		t.Fatalf("expected network mismatch error on switched daemon, got: %v", pErr)
	}
}

// TestGUIQueryValidatedNodeEndpoint_NoSecondVerificationRequestOnMissingHeader proves
// that when a response lacks X-Network-ID, it is rejected immediately with NO second
// verification request to /stats (preventing TOCTOU fallback exploits).
func TestGUIQueryValidatedNodeEndpoint_NoSecondVerificationRequestOnMissingHeader(t *testing.T) {
	var requestedPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// Omit X-Network-ID header
		_, _ = w.Write([]byte(`[{"addr":"unverified.peer:8333"}]`))
	}))
	defer srv.Close()

	_, _, err := queryValidatedNodeEndpoint(srv.URL, "/peers", "ARMX-TESTNET-1", 2*time.Second)
	if err == nil {
		t.Fatalf("expected error on missing header")
	}
	if !strings.Contains(err.Error(), "missing X-Network-ID response header") {
		t.Fatalf("expected 'missing X-Network-ID response header', got: %v", err)
	}

	// Invariant: exactly 1 request was made to /peers; /stats was NEVER queried as fallback
	if len(requestedPaths) != 1 || requestedPaths[0] != "/peers" {
		t.Fatalf("security violation: expected exactly 1 request to /peers, but got requests: %v", requestedPaths)
	}
}

// TestGUIQueryValidatedNodeEndpoint_ResponseBodyMismatchRejected proves that when /stats
// body contains a conflicting NetworkID, the response is rejected even if header matched.
func TestGUIQueryValidatedNodeEndpoint_ResponseBodyMismatchRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		_, _ = w.Write([]byte(`{"height":50,"network_id":"ARMX-MAINNET-1"}`))
	}))
	defer srv.Close()

	_, _, err := queryValidatedNodeEndpoint(srv.URL, "/stats", "ARMX-TESTNET-1", 2*time.Second)
	if err == nil {
		t.Fatalf("security violation: expected error on conflicting body network_id")
	}
	if !strings.Contains(err.Error(), "network mismatch on response body") {
		t.Fatalf("expected 'network mismatch on response body', got: %v", err)
	}
}

// TestAF05_GUIPreSignValidationAndSigningFlow verifies that GUI send preparation:
//  1. Rejects amount and fee strings over permitted MaxSupply bounds before querying RPC or signing.
//  2. Aborts before signing when /balance returns HTTP non-200.
//  3. Aborts before signing when /stats or /balance returns an incorrect X-Network-ID.
//  4. Aborts before signing when available balance is insufficient.
//  5. Signs a valid ML-DSA-65 transaction when network identity and balance are valid.
func TestAF05_GUIPreSignValidationAndSigningFlow(t *testing.T) {
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

	// 1. Validate amount/fee input string bounds (validateGUISendAmounts)
	if _, _, _, _, ok := validateGUISendAmounts("126000001.00000000", "0.00011000"); ok {
		t.Fatalf("expected amount > MaxSupply to be rejected by validateGUISendAmounts")
	}
	if _, _, _, _, ok := validateGUISendAmounts("1.00000000", "126000001.00000000"); ok {
		t.Fatalf("expected fee > MaxSupply to be rejected by validateGUISendAmounts")
	}
	if _, _, _, _, ok := validateGUISendAmounts("126000000.00000000", "0.00011000"); ok {
		t.Fatalf("expected amount + fee > MaxSupply to be rejected by validateGUISendAmounts")
	}
	amt, fee, _, _, ok := validateGUISendAmounts("2.50000000", "0.00011000")
	if !ok || amt != 250_000_000 || fee != 11000 {
		t.Fatalf("expected valid amount/fee to pass validateGUISendAmounts, got ok=%v amt=%d fee=%d", ok, amt, fee)
	}

	var statsCalls int32
	var balanceCalls int32
	var statsNetID = "ARMX-TESTNET-1"
	var balanceNetID = "ARMX-TESTNET-1"
	var balanceStatus int32 = http.StatusOK
	var availBal int64 = 50 * core.CoinUnit
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stats":
			atomic.AddInt32(&statsCalls, 1)
			mu.Lock()
			sNet := statsNetID
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Network-ID", sNet)
			_, _ = fmt.Fprintf(w, `{"height":100,"blocks":101,"network_id":%q}`, sNet)
		case "/balance":
			atomic.AddInt32(&balanceCalls, 1)
			mu.Lock()
			bNet := balanceNetID
			status := int(atomic.LoadInt32(&balanceStatus))
			bal := availBal
			mu.Unlock()
			if bNet != "" {
				w.Header().Set("X-Network-ID", bNet)
			}
			if status != http.StatusOK {
				http.Error(w, "balance failure", status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"address":%q,"balance":%d,"confirmed_balance":%d,"pending_balance":%d,"nonce":3,"pending_nonce":3,"next_nonce":4}`,
				senderWallet.Address, bal, bal, bal)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Use a wallet copy with nil PrivateKey for negative tests to prove tx.Sign is never reached
	noKeyWallet := &wallet.Wallet{
		Address:   senderWallet.Address,
		PublicKey: senderWallet.PublicKey,
	}

	// 2. Out-of-bounds amount/fee in queryNonceAndSignGUITransaction aborts before any RPC call
	atomic.StoreInt32(&statsCalls, 0)
	atomic.StoreInt32(&balanceCalls, 0)
	if tx, _, _, _, err := queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", noKeyWallet, recipientWallet.Address, core.MaxSupply, 11000); err == nil || tx != nil {
		t.Fatalf("expected amount + fee > MaxSupply to abort before RPC, got tx=%v err=%v", tx, err)
	}
	if atomic.LoadInt32(&statsCalls) != 0 || atomic.LoadInt32(&balanceCalls) != 0 {
		t.Fatalf("expected 0 RPC calls on out-of-bounds amount+fee, got stats=%d balance=%d", statsCalls, balanceCalls)
	}

	// 3. /balance HTTP non-200 aborts before signing
	atomic.StoreInt32(&balanceStatus, http.StatusBadGateway)
	tx, _, title, msg, err := queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", noKeyWallet, recipientWallet.Address, 1*core.CoinUnit, 11000)
	if err == nil || tx != nil || title != "RPC Error" || !strings.Contains(msg, "HTTP 502") {
		t.Fatalf("expected HTTP 502 on /balance to abort with RPC Error, got tx=%v title=%q msg=%q err=%v", tx, title, msg, err)
	}

	// 4. Incorrect X-Network-ID on /stats or /balance aborts before signing
	atomic.StoreInt32(&balanceStatus, http.StatusOK)
	mu.Lock()
	balanceNetID = "ARMX-MAINNET-1"
	mu.Unlock()
	tx, _, title, _, err = queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", noKeyWallet, recipientWallet.Address, 1*core.CoinUnit, 11000)
	if err == nil || tx != nil || title != "Network Mismatch" {
		t.Fatalf("expected X-Network-ID mismatch on /balance to abort with Network Mismatch, got tx=%v title=%q err=%v", tx, title, err)
	}

	mu.Lock()
	statsNetID = "ARMX-MAINNET-1"
	balanceNetID = "ARMX-TESTNET-1"
	mu.Unlock()
	atomic.StoreInt32(&balanceCalls, 0)
	tx, _, title, _, err = queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", noKeyWallet, recipientWallet.Address, 1*core.CoinUnit, 11000)
	if err == nil || tx != nil || title != "Network Mismatch" || atomic.LoadInt32(&balanceCalls) != 0 {
		t.Fatalf("expected X-Network-ID mismatch on /stats to abort before /balance, got tx=%v title=%q balanceCalls=%d err=%v", tx, title, balanceCalls, err)
	}

	// 5. Insufficient balance aborts before signing
	mu.Lock()
	statsNetID = "ARMX-TESTNET-1"
	balanceNetID = "ARMX-TESTNET-1"
	availBal = 1000 // less than 1 ARMX + 11000 fee
	mu.Unlock()
	tx, _, title, _, err = queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", noKeyWallet, recipientWallet.Address, 1*core.CoinUnit, 11000)
	if err == nil || tx != nil || title != "Insufficient Balance" {
		t.Fatalf("expected insufficient balance to abort before signing, got tx=%v title=%q err=%v", tx, title, err)
	}

	// 6. Sufficient balance and matching network signs a valid ML-DSA-65 transaction
	mu.Lock()
	availBal = 50 * core.CoinUnit
	mu.Unlock()
	signedTx, nonce, title, msg, err := queryNonceAndSignGUITransaction(srv.URL, "ARMX-TESTNET-1", senderWallet, recipientWallet.Address, 250_000_000, 11000)
	if err != nil || signedTx == nil {
		t.Fatalf("expected valid GUI pre-sign + sign to succeed, got title=%q msg=%q err=%v", title, msg, err)
	}
	if nonce != 4 || signedTx.Nonce != 4 || signedTx.Amount != 250_000_000 || signedTx.Fee != 11000 {
		t.Fatalf("unexpected signed transaction fields: %+v (nonce=%d)", signedTx, nonce)
	}
	if err := signedTx.VerifyWithError(); err != nil {
		t.Fatalf("GUI signed transaction failed ML-DSA-65 verification: %v", err)
	}
}

// TestGUICreateAndSignTransaction_BoundsErrorFormat verifies that queryNonceAndSignGUITransaction
// rejects out-of-bounds amounts and returns standard lowercased error strings [v1.0.6].
func TestGUICreateAndSignTransaction_BoundsErrorFormat(t *testing.T) {
	w, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	defer w.Zeroize()

	testCases := []struct {
		name string
		amt  int64
		fee  int64
	}{
		{"ZeroAmount", 0, 11000},
		{"NegativeAmount", -1, 11000},
		{"NegativeFee", 100000, -1},
		{"AmountExceedsMaxSupply", core.MaxSupply + 1, 11000},
		{"FeeExceedsMaxSupply", 100000, core.MaxSupply + 1},
		{"SumExceedsMaxSupply", core.MaxSupply, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, nonce, title, msg, err := queryNonceAndSignGUITransaction("http://127.0.0.1:19333", "ARMX-TESTNET-1", w, w.Address, tc.amt, tc.fee)
			if err == nil || tx != nil || nonce != 0 {
				t.Fatalf("expected error on bounds violation, got tx=%v nonce=%d err=%v", tx, nonce, err)
			}
			if title != "Invalid Amount" {
				t.Errorf("expected title 'Invalid Amount', got %q", title)
			}
			if msg != "Transaction amount or fee exceeds permitted bounds" {
				t.Errorf("expected UI msg 'Transaction amount or fee exceeds permitted bounds', got %q", msg)
			}
			if err.Error() != "transaction amount or fee exceeds permitted bounds" {
				t.Errorf("expected Go error 'transaction amount or fee exceeds permitted bounds', got %q", err.Error())
			}
		})
	}
}

// TestSafeP2PDisplayFilter verifies that home residential nodes with ephemeral client ports
// are excluded from GUI active peer display, while DNS seeds and public nodes with open listening P2P ports are preserved.
func TestSafeP2PDisplayFilter(t *testing.T) {
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

	// 5. filterSafePeersJSON on object array
	rawObjArray := []byte(`[
		{"endpoint":"198.51.100.25:58539", "display_address":"198.51.100.25:58539", "is_active":true},
		{"endpoint":"testnet-seed1.armax-coin.cloud:19335", "display_address":"testnet-seed1.armax-coin.cloud:19335", "is_seed":true},
		{"endpoint":"198.51.100.30:19335", "display_address":"198.51.100.30:19335", "is_active":true}
	]`)
	filteredObjRaw := filterSafePeersJSON(rawObjArray, netProfile)
	var filteredObjs []map[string]interface{}
	if err := json.Unmarshal(filteredObjRaw, &filteredObjs); err != nil {
		t.Fatalf("failed to unmarshal filtered obj JSON: %v", err)
	}
	if len(filteredObjs) != 2 {
		t.Fatalf("expected 2 filtered objects, got %d: %v", len(filteredObjs), filteredObjs)
	}
	for _, obj := range filteredObjs {
		if ep, _ := obj["endpoint"].(string); strings.Contains(ep, "58539") {
			t.Fatalf("found ephemeral peer in filtered obj output: %s", ep)
		}
	}
}

func TestGUIStatsResponseMaturityAndTicker(t *testing.T) {
	payload := `{"height":19576,"blocks":19577,"coinbase_maturity":10,"network_id":"ARMX-TESTNET-1","ticker":"tARMX","peers_active":4}`
	var st StatsResponse
	if err := json.Unmarshal([]byte(payload), &st); err != nil {
		t.Fatalf("failed to unmarshal stats: %v", err)
	}
	if st.CoinbaseMaturity != 10 || st.Ticker != "tARMX" || st.PeersActive != 4 || st.Height != 19576 {
		t.Fatalf("unexpected decoded StatsResponse fields: %+v", st)
	}
}

func TestGUISessionMinedModel(t *testing.T) {
	m := &SessionMinedModel{
		items: []SessionMinedBlock{
			{
				Height:        501,
				Hash:          "0000000018cacfd0123456789abcdef0",
				Reward:        60 * core.CoinUnit,
				PeersAtMine:   5,
				Confirmations: 7,
				Maturity:      10,
				Status:        MinedStatusConfirming,
			},
			{
				Height:        502,
				Hash:          "0000aaaa",
				Reward:        60 * core.CoinUnit,
				PeersAtMine:   0,
				Confirmations: 0,
				Maturity:      10,
				Status:        MinedStatusOrphaned,
			},
		},
	}

	if m.RowCount() != 2 {
		t.Fatalf("expected RowCount 2, got %d", m.RowCount())
	}
	if got := m.Value(0, 0); got != "#501" {
		t.Errorf("col 0 = %v, want #501", got)
	}
	if got := m.Value(0, 1); got != "0000000018cacfd0..." {
		t.Errorf("col 1 = %v", got)
	}
	if got := m.Value(0, 2); !strings.Contains(fmt.Sprint(got), "60.00000000") {
		t.Errorf("col 2 = %v", got)
	}
	if got := m.Value(0, 3); got != "5" {
		t.Errorf("col 3 = %v, want 5", got)
	}
	if got := m.Value(0, 4); got != "7/10" {
		t.Errorf("col 4 = %v, want 7/10", got)
	}
	if got := m.Value(0, 5); got != string(MinedStatusConfirming) {
		t.Errorf("col 5 = %v, want CONFIRMING", got)
	}
	if got := m.Value(1, 5); got != string(MinedStatusOrphaned) {
		t.Errorf("row 1 col 5 = %v, want ORPHANED", got)
	}
	if got := m.Value(-1, 0); got != "" {
		t.Errorf("out-of-bounds low expected empty, got %v", got)
	}
	if got := m.Value(9, 0); got != "" {
		t.Errorf("out-of-bounds high expected empty, got %v", got)
	}
}

func TestGUIFilterTxItems_TypeDateAndQuery(t *testing.T) {
	items := []*TxItem{
		{
			Index:     1,
			Type:      "Mined (Coinbase)",
			Amount:    "60.00000000 tARMX",
			Height:    "#100 (Mature)",
			TxID:      "tx-coinbase-001",
			Timestamp: "2026-11-15 10:00:00",
			Record:    &TxHistoryRecord{BlockIndex: 100, Tx: core.Transaction{Sender: "SYSTEM"}},
		},
		{
			Index:     2,
			Type:      "Sent",
			Amount:    "5.00000000 tARMX",
			Height:    "Pending",
			TxID:      "tx-sent-002",
			Timestamp: "2026-11-17 12:30:00",
			Record:    &TxHistoryRecord{BlockIndex: -1, Tx: core.Transaction{Sender: "armx1alice", Recipient: "armx1bob"}},
		},
		{
			Index:     3,
			Type:      "Received",
			Amount:    "12.00000000 tARMX",
			Height:    "#120 (Finalized)",
			TxID:      "tx-recv-003",
			Timestamp: "2026-11-19 08:15:00",
			Record:    &TxHistoryRecord{BlockIndex: 120, Tx: core.Transaction{Sender: "armx1charlie", Recipient: "armx1alice"}},
		},
	}

	if got := filterTxItems(items, "", "All", "", ""); len(got) != 3 {
		t.Fatalf("All filter expected 3, got %d", len(got))
	}
	if got := filterTxItems(items, "", "Mined (Coinbase)", "", ""); len(got) != 1 || got[0].TxID != "tx-coinbase-001" {
		t.Fatalf("Mined filter failed: %+v", got)
	}
	if got := filterTxItems(items, "", "Pending", "", ""); len(got) != 1 || got[0].TxID != "tx-sent-002" {
		t.Fatalf("Pending filter failed: %+v", got)
	}
	if got := filterTxItems(items, "", "All", "2026-11-16", "2026-11-18"); len(got) != 1 || got[0].TxID != "tx-sent-002" {
		t.Fatalf("Date range filter failed: %+v", got)
	}
	if got := filterTxItems(items, "charlie", "Received", "", ""); len(got) != 1 || got[0].TxID != "tx-recv-003" {
		t.Fatalf("Combined text+type filter failed: %+v", got)
	}
}

func TestGUIPeerInspector_FiltersEphemeralPeers(t *testing.T) {
	netProfile := core.NetworkProfile{
		P2PPort:      "19335",
		DefaultSeeds: []string{"testnet-seed1.armax-coin.cloud:19335"},
	}
	raw := []byte(`[
		{"endpoint":"198.51.100.25:58539", "is_active":true},
		{"endpoint":"testnet-seed1.armax-coin.cloud:19335", "is_seed":true, "is_active":true},
		{"endpoint":"198.51.100.30:19335", "is_active":true}
	]`)

	rows := parseSafePeerInspectorRows(raw, netProfile)
	if len(rows) != 2 {
		t.Fatalf("expected 2 safe peer rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].Role != "DNS Seed" || rows[1].Role != "Public Node" {
		t.Errorf("unexpected peer roles: %+v", rows)
	}

	m := &PeerInspectorModel{items: rows}
	if m.RowCount() != 2 || m.Value(0, 1) != "DNS Seed" || m.Value(1, 0) != "198.51.100.30:19335" {
		t.Errorf("unexpected PeerInspectorModel values")
	}
}

func TestGUIBIP21AndAddressBlocks(t *testing.T) {
	addr := "armx1cdj242tzwrp5uutzyktj0p4jsyl0s06ahxfk3xzv0vqgnn438e7s2ps308"
	uriNoAmt := buildBIP21PaymentURI(addr, "", "DesktopWallet")
	if uriNoAmt != "armax:"+addr+"?label=DesktopWallet" {
		t.Errorf("unexpected URI without amount: %s", uriNoAmt)
	}
	uriWithAmt := buildBIP21PaymentURI(addr, "12.5", "DesktopWallet")
	if uriWithAmt != "armax:"+addr+"?amount=12.5&label=DesktopWallet" {
		t.Errorf("unexpected URI with amount: %s", uriWithAmt)
	}
	blocks := formatAddress4CharBlocks("armx12345678")
	if blocks != "armx  1234  5678" {
		t.Errorf("unexpected 4-char blocks formatting: %q", blocks)
	}
}

func TestWindowsICOIconParser(t *testing.T) {
	h32 := createIconFromICOBytes(armaxLogoICO, 32, 32)
	if h32 == 0 {
		t.Fatalf("expected non-zero 32x32 HICON from embedded armax_logo.ico")
	}
	destroyHIcon(h32)

	h16 := createIconFromICOBytes(armaxLogoICO, 16, 16)
	if h16 == 0 {
		t.Fatalf("expected non-zero 16x16 HICON from embedded armax_logo.ico")
	}
	destroyHIcon(h16)

	if bad := createIconFromICOBytes([]byte{1, 2, 3}, 32, 32); bad != 0 {
		t.Errorf("expected 0 HICON for truncated ICO bytes, got %v", bad)
	}
}

func TestGUIExecuteConsoleCommand_GetMempoolInfoQueriesMempool(t *testing.T) {
	mempoolHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network-ID", "ARMX-TESTNET-1")
		switch r.URL.Path {
		case "/stats":
			_, _ = w.Write([]byte(`{"height":105,"network_id":"ARMX-TESTNET-1"}`))
		case "/mempool":
			mempoolHits++
			_ = json.NewEncoder(w).Encode([]core.Transaction{{
				Sender:    core.GenesisMiner,
				Recipient: "armx1recipient000",
				Amount:    100_000_000,
				Fee:       11_000,
				Nonce:     1,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out := executeConsoleCommand("getmempoolinfo", srv.URL, "ARMX-TESTNET-1")
	if mempoolHits != 1 {
		t.Fatalf("expected executeConsoleCommand(getmempoolinfo) to query /mempool once, got %d (out=%s)", mempoolHits, out)
	}
	if !strings.Contains(out, "HTTP 200") || !strings.Contains(out, `"size":1`) || !strings.Contains(out, `"transactions"`) {
		t.Fatalf("unexpected getmempoolinfo console output: %s", out)
	}
}

func TestWindowsNotifyIconDataSize(t *testing.T) {
	gotSize := unsafe.Sizeof(winNotifyIconDataW{})
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if gotSize != 976 {
			t.Fatalf("expected 64-bit winNotifyIconDataW size 976, got %d", gotSize)
		}
		var zero winNotifyIconDataW
		checks := []struct {
			name string
			got  uintptr
			want uintptr
		}{
			{"HWnd", unsafe.Offsetof(zero.HWnd), 8},
			{"HIcon", unsafe.Offsetof(zero.HIcon), 32},
			{"SzTip", unsafe.Offsetof(zero.SzTip), 40},
			{"SzInfo", unsafe.Offsetof(zero.SzInfo), 304},
			{"SzInfoTitle", unsafe.Offsetof(zero.SzInfoTitle), 820},
			{"GuidItem", unsafe.Offsetof(zero.GuidItem), 952},
			{"HBalloonIcon", unsafe.Offsetof(zero.HBalloonIcon), 968},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("64-bit winNotifyIconDataW.%s offset = %d, want %d", c.name, c.got, c.want)
			}
		}
	} else if gotSize != 956 {
		t.Fatalf("expected 32-bit winNotifyIconDataW size 956, got %d", gotSize)
	}
}

func TestWindowsTrayHelpers_UTF16CopyAndStatusText(t *testing.T) {
	// 1. Empty slice safety
	copyUTF16Fixed(nil, "ignored")

	// 2. Normal UTF-8 with em-dash + NUL termination
	var tip [128]uint16
	tip[0] = 0x1234
	tip[127] = 0x5678
	msg := "Armaxcoin Core — Desktop Wallet"
	copyUTF16Fixed(tip[:], msg)
	if tip[127] != 0 {
		t.Fatalf("expected trailing elements cleared, got %x", tip[127])
	}
	end := 0
	for end < len(tip) && tip[end] != 0 {
		end++
	}
	decoded := string(utf16.Decode(tip[:end]))
	if decoded != msg {
		t.Fatalf("decoded UTF-16 = %q, want %q", decoded, msg)
	}

	// 3. Exact truncation with guaranteed NUL terminator
	var small [6]uint16
	copyUTF16Fixed(small[:], "Armaxcoin")
	if small[5] != 0 {
		t.Fatalf("expected NUL terminator at last slot, got %d", small[5])
	}
	if got := string(utf16.Decode(small[:5])); got != "Armax" {
		t.Fatalf("truncated UTF-16 = %q, want %q", got, "Armax")
	}

	// 4. Tray status formatting (zero network I/O, no secret exposure)
	origNode := getNodeBootstrapStatus()
	origTip := atomic.LoadInt64(&lastTipHeight)
	miningLock.Lock()
	origActive := miningActive
	miningLock.Unlock()
	t.Cleanup(func() {
		setNodeBootstrapStatus(origNode)
		atomic.StoreInt64(&lastTipHeight, origTip)
		miningLock.Lock()
		miningActive = origActive
		miningLock.Unlock()
	})

	setNodeBootstrapStatus(NodeBootstrapStatus{State: "starting"})
	miningLock.Lock()
	miningActive = false
	miningLock.Unlock()
	if got := buildTrayStatusText(); got != "Node: Starting | Miner: Idle" {
		t.Errorf("unexpected status (starting/idle): %q", got)
	}

	setNodeBootstrapStatus(NodeBootstrapStatus{State: "online"})
	miningLock.Lock()
	miningActive = true
	miningLock.Unlock()
	if got := buildTrayStatusText(); got != "Node: Online | Miner: CPU Mining" {
		t.Errorf("unexpected status (online/cpu): %q", got)
	}

	setNodeBootstrapStatus(NodeBootstrapStatus{State: ""})
	atomic.StoreInt64(&lastTipHeight, 42)
	miningLock.Lock()
	miningActive = true
	miningLock.Unlock()
	if got := buildTrayStatusText(); got != "Node: Online | Miner: CPU Mining" {
		t.Errorf("unexpected status (tip fallback/cpu): %q", got)
	}

	atomic.StoreInt64(&lastTipHeight, 0)
	miningLock.Lock()
	miningActive = false
	miningLock.Unlock()
	if got := buildTrayStatusText(); got != "Node: Offline | Miner: Idle" {
		t.Errorf("unexpected status (offline/idle): %q", got)
	}
}
