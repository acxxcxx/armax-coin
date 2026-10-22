//go:build windows

package main

import (
	"encoding/json"
	"testing"
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
	str := formatArmx(6000000000)
	if str != "60.00000000 ARMX" {
		t.Fatalf("Expected '60.00000000 ARMX', got '%s'", str)
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
