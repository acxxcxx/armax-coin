//go:build testhooks

package core

import (
	"strings"
	"testing"
)

// TestCLINetwork_Resolution verifies that -network flag resolver defaults to testnet and locks mainnet [L3-8, L4-1]
func TestCLINetwork_Resolution(t *testing.T) {
	// 1. Verify MainnetLaunched invariant
	if MainnetLaunched {
		t.Fatalf("MainnetLaunched must be false prior to official mainnet release")
	}

	// 2. Default (empty string) must resolve to testnet [L4-1]
	cfgDef, err := ResolveCLINetwork("")
	if err != nil {
		t.Fatalf("ResolveCLINetwork(\"\") failed: %v", err)
	}
	if cfgDef.NetworkProfile.Name != "testnet" {
		t.Errorf("Expected default profile 'testnet', got '%s'", cfgDef.NetworkProfile.Name)
	}
	if cfgDef.NetworkProfile.NetworkID != "ARMX-TESTNET-1" {
		t.Errorf("Expected default NetworkID 'ARMX-TESTNET-1', got '%s'", cfgDef.NetworkProfile.NetworkID)
	}
	if cfgDef.RPCPort != "19333" {
		t.Errorf("Expected default RPCPort '19333', got '%s'", cfgDef.RPCPort)
	}
	if cfgDef.P2PPort != "19335" {
		t.Errorf("Expected default P2PPort '19335', got '%s'", cfgDef.P2PPort)
	}
	if cfgDef.DataDir != "testnet" {
		t.Errorf("Expected default DataDir 'testnet', got '%s'", cfgDef.DataDir)
	}
	if cfgDef.DaemonURL != "http://127.0.0.1:19333" {
		t.Errorf("Expected default DaemonURL 'http://127.0.0.1:19333', got '%s'", cfgDef.DaemonURL)
	}
	if len(cfgDef.DefaultSeeds) < 2 {
		t.Errorf("Expected at least 2 testnet seeds, got %d", len(cfgDef.DefaultSeeds))
	}
	for _, seed := range cfgDef.DefaultSeeds {
		if !strings.Contains(seed, "19335") {
			t.Errorf("Expected testnet seed to use port 19335, got '%s'", seed)
		}
	}

	// 3. Explicit "testnet"
	cfgTest, err := ResolveCLINetwork("testnet")
	if err != nil {
		t.Fatalf("ResolveCLINetwork(\"testnet\") failed: %v", err)
	}
	if cfgTest.NetworkProfile.Name != "testnet" {
		t.Errorf("Expected profile 'testnet', got '%s'", cfgTest.NetworkProfile.Name)
	}

	// 4. Mainnet locked when testhooks does not allow or in production [L4-1]
	SetAllowMainnetForTesting(false)
	_, errLocked := ResolveCLINetwork("mainnet")
	if errLocked == nil || !strings.Contains(errLocked.Error(), "Mainnet has not been launched; please use -network testnet") {
		t.Fatalf("Expected 'Mainnet has not been launched' error when locked, got: %v", errLocked)
	}
	errSetLocked := SetNetwork("mainnet")
	if errSetLocked == nil || !strings.Contains(errSetLocked.Error(), "Mainnet has not been launched; please use -network testnet") {
		t.Fatalf("Expected SetNetwork to fail with 'Mainnet has not been launched', got: %v", errSetLocked)
	}

	// Restore testhooks mainnet allowance for test suite
	SetAllowMainnetForTesting(true)
	cfgMain, errMain := ResolveCLINetwork("mainnet")
	if errMain != nil {
		t.Fatalf("Expected ResolveCLINetwork(\"mainnet\") to succeed with testhooks tag, got: %v", errMain)
	}
	if cfgMain.RPCPort != "19332" || cfgMain.P2PPort != "19334" || cfgMain.DataDir != "data" {
		t.Errorf("Mainnet config mismatch: %+v", cfgMain)
	}

	// 5. Invalid profile must return descriptive error
	_, err = ResolveCLINetwork("unknown_net_xyz")
	if err == nil {
		t.Fatalf("Expected error for invalid network profile, got nil")
	}
}
