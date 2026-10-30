//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
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
