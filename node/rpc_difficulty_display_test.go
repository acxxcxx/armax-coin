package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestStatsEndpointReturnsDualDifficultyFields verifies that GET /stats payload includes
// the work-domain (hashes) and ratio-domain difficulty fields, network hashrate, and DAA engine name.
func TestStatsEndpointReturnsDualDifficultyFields(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	srv := httptest.NewServer(createTestServerMux(bc))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("failed to GET /stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode /stats JSON: %v", err)
	}

	// Verify required fields
	requiredKeys := []string{
		"difficulty",
		"difficulty_ratio",
		"difficulty_hashes",
		"difficulty_formatted",
		"daa_engine",
		"network_hashrate_estimated",
		"network_hashrate_formatted",
	}

	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("missing key %q in /stats payload", key)
		}
	}

	// Verify DAA engine is LWMA-1 for genesis chain
	if engine, ok := stats["daa_engine"].(string); !ok || engine != "LWMA-1 (N=60)" {
		t.Errorf("expected daa_engine 'LWMA-1 (N=60)', got %v", stats["daa_engine"])
	}
}

// TestGetMiningInfoReturnsDualDifficultyFields verifies that GET /getmininginfo payload
// includes the new difficulty fields.
func TestGetMiningInfoReturnsDualDifficultyFields(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	srv := httptest.NewServer(createTestServerMux(bc))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/getmininginfo")
	if err != nil {
		t.Fatalf("failed to GET /getmininginfo: %v", err)
	}
	defer resp.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("failed to decode /getmininginfo JSON: %v", err)
	}

	requiredKeys := []string{
		"difficulty",
		"difficulty_ratio",
		"difficulty_hashes",
		"difficulty_formatted",
		"networkhashps_formatted",
		"daa_engine",
	}

	for _, key := range requiredKeys {
		if _, ok := info[key]; !ok {
			t.Errorf("missing key %q in /getmininginfo payload", key)
		}
	}
}

// TestGetBlockTemplateReturnsDualDifficultyFields verifies that GET /getblocktemplate payload
// includes the new difficulty fields.
func TestGetBlockTemplateReturnsDualDifficultyFields(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	srv := httptest.NewServer(createTestServerMux(bc))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/getblocktemplate")
	if err != nil {
		t.Fatalf("failed to GET /getblocktemplate: %v", err)
	}
	defer resp.Body.Close()

	var tmpl map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tmpl); err != nil {
		t.Fatalf("failed to decode /getblocktemplate JSON: %v", err)
	}

	requiredKeys := []string{
		"difficulty",
		"difficulty_ratio",
		"difficulty_hashes",
		"difficulty_formatted",
		"daa_engine",
		"maxtime",
	}

	for _, key := range requiredKeys {
		if _, ok := tmpl[key]; !ok {
			t.Errorf("missing key %q in /getblocktemplate payload", key)
		}
	}
}

// TestASERTTransitionAtBlock16000InStats verifies that when chain height reaches the ASERT
// activation boundary, /stats dynamically returns "Armax-ASERT-AW", drift metrics, and formatted labels.
func TestASERTTransitionAtBlock16000InStats(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	// Configure ASERT activation height at height 2 for unit testing
	core.SetASERTActivationHeight(2)
	defer core.ResetASERTActivationHeightOverride()

	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	// Initially at height 0 -> LWMA-1
	srv := httptest.NewServer(createTestServerMux(bc))
	defer srv.Close()

	resp1, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("failed to GET /stats: %v", err)
	}
	var stats1 map[string]interface{}
	_ = json.NewDecoder(resp1.Body).Decode(&stats1)
	resp1.Body.Close()

	if stats1["daa_engine"] != "LWMA-1 (N=60)" {
		t.Errorf("expected height 0 daa_engine 'LWMA-1 (N=60)', got %v", stats1["daa_engine"])
	}

	// Add Block 1 (anchor) and Block 2 (ASERT activation block)
	bc.Lock.Lock()
	anchorBlock := core.Block{
		Index:      1,
		Timestamp:  bc.Chain[0].Timestamp + 120,
		Target:     bc.Chain[0].Target,
		Difficulty: bc.Chain[0].Difficulty,
		Hash:       "0000000000000000000000000000000000000000000000000000000000000001",
	}
	actBlock := core.Block{
		Index:      2,
		Timestamp:  anchorBlock.Timestamp + 130, // 10 seconds slower than 120s ideal spacing
		Target:     anchorBlock.Target,
		Difficulty: anchorBlock.Difficulty,
		Hash:       "0000000000000000000000000000000000000000000000000000000000000002",
	}
	bc.Chain = append(bc.Chain, anchorBlock, actBlock)
	bc.Difficulty = actBlock.Difficulty
	bc.Lock.Unlock()

	// Query /stats again at height 2
	resp2, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("failed to GET /stats: %v", err)
	}
	var stats2 map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&stats2)
	resp2.Body.Close()

	if stats2["daa_engine"] != "Armax-ASERT-AW" {
		t.Errorf("expected height 2 daa_engine 'Armax-ASERT-AW', got %v", stats2["daa_engine"])
	}

	driftSec, ok := stats2["asert_drift_seconds"].(float64)
	if !ok || int64(driftSec) != 10 {
		t.Errorf("expected asert_drift_seconds == 10, got %v", stats2["asert_drift_seconds"])
	}

	driftFmt, ok := stats2["asert_drift_formatted"].(string)
	if !ok || driftFmt != "+10s (Behind)" {
		t.Errorf("expected asert_drift_formatted '+10s (Behind)', got %v", stats2["asert_drift_formatted"])
	}
}

// Helper to create test server handler with the actual server handlers
func createTestServerMux(bc *core.Blockchain) http.Handler {
	return NewRPCHandler(bc, nil, nil, nil)
}
