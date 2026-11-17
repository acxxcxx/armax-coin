//go:build testhooks

package core

import (
	"flag"
	"math/big"
	"testing"
)

// TestF3_TargetOverrideHardGuard verifies that targetOverrideHook is strictly guarded
// by the stable IsMainnet boolean identity on NetworkProfile [F3].
func TestF3_TargetOverrideHardGuard(t *testing.T) {
	origNet := GetActiveNetwork()
	defer func() {
		netMu.Lock()
		ActiveNetwork = origNet
		netMu.Unlock()
	}()

	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	SetAllowMainnetForTesting(true)
	defer SetAllowMainnetForTesting(true)

	// 1. Initialize Mainnet
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet: %v", err)
	}

	// Deliberately mutate ActiveNetwork.Name to "ARMX-MAINNET-1" to simulate non-"mainnet" string
	netMu.Lock()
	ActiveNetwork.Name = "ARMX-MAINNET-1"
	netMu.Unlock()

	fakeOverride := big.NewInt(123456789)
	SetTargetOverrideForTesting(fakeOverride)
	defer SetTargetOverrideForTesting(nil)

	genesis := GetGenesisBlock()
	chain := []Block{genesis}

	// Verify that CalculateExpectedTarget STILL rejects the override because IsMainnet is true!
	expected := CalculateExpectedTarget(chain, 1)
	if expected.Cmp(fakeOverride) == 0 {
		t.Fatalf("[F3 VIOLATION] Target override was accepted on Mainnet profile despite IsMainnet=true (Name was 'ARMX-MAINNET-1')!")
	}

	// 2. Now switch to regtest (IsMainnet == false) where test fixture override is permitted
	SetNetworkForTesting(true)
	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("Failed to set regtest: %v", err)
	}

	if flag.Lookup("test.v") == nil {
		t.Fatalf("Expected test flag 'test.v' to be present during go test")
	}

	regtestExpected := CalculateExpectedTarget(chain, 1)
	if regtestExpected.Cmp(fakeOverride) != 0 {
		t.Fatalf("Expected CalculateExpectedTarget to honor override under regtest test harness, got %s", regtestExpected.String())
	}
}
