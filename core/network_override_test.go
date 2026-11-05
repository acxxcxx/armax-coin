//go:build testhooks

package core

import (
	"os"
	"testing"
)

// TestArmaxHashActivationEnvOverrideLockedToRegtest verifies HIGH-02:
// Environment variable overrides ARMX_ARMAXHASH_HEIGHT and ARMX_ARMAXHASH_V2_HEIGHT
// are strictly ignored on Testnet and Mainnet to prevent accidental or malicious
// consensus divergence, and are only honored when running on regtest.
func TestArmaxHashActivationEnvOverrideLockedToRegtest(t *testing.T) {
	LockNetworkForTesting(t)

	// Ensure overrides are reset
	ResetArmaxHashActivationHeightOverride()
	ResetArmaxHashV2ActivationHeightOverride()

	// Set environment variables to non-standard values
	origV1 := os.Getenv("ARMX_ARMAXHASH_HEIGHT")
	origV2 := os.Getenv("ARMX_ARMAXHASH_V2_HEIGHT")
	t.Cleanup(func() {
		if origV1 != "" {
			_ = os.Setenv("ARMX_ARMAXHASH_HEIGHT", origV1)
		} else {
			_ = os.Unsetenv("ARMX_ARMAXHASH_HEIGHT")
		}
		if origV2 != "" {
			_ = os.Setenv("ARMX_ARMAXHASH_V2_HEIGHT", origV2)
		} else {
			_ = os.Unsetenv("ARMX_ARMAXHASH_V2_HEIGHT")
		}
	})

	_ = os.Setenv("ARMX_ARMAXHASH_HEIGHT", "123")
	_ = os.Setenv("ARMX_ARMAXHASH_V2_HEIGHT", "456")

	// 1. Verify Testnet ignores env vars
	SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("failed to set testnet: %v", err)
	}

	if h := GetArmaxHashActivationHeight(); h == 123 {
		t.Fatalf("HIGH-02 violation: testnet honored ARMX_ARMAXHASH_HEIGHT env override: got %d, want default %d", h, TestnetProfile.ArmaxHashActivationHeight)
	} else if h != TestnetProfile.ArmaxHashActivationHeight {
		t.Fatalf("expected testnet default %d, got %d", TestnetProfile.ArmaxHashActivationHeight, h)
	}

	if h2 := GetArmaxHashV2ActivationHeight(); h2 == 456 {
		t.Fatalf("HIGH-02 violation: testnet honored ARMX_ARMAXHASH_V2_HEIGHT env override: got %d, want default %d", h2, TestnetProfile.ArmaxHashV2ActivationHeight)
	} else if h2 != TestnetProfile.ArmaxHashV2ActivationHeight {
		t.Fatalf("expected testnet v2 default %d, got %d", TestnetProfile.ArmaxHashV2ActivationHeight, h2)
	}

	// 2. Verify Mainnet ignores env vars
	SetAllowMainnetForTesting(true)
	EnableLocalMainnet()
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("failed to set mainnet: %v", err)
	}

	if h := GetArmaxHashActivationHeight(); h == 123 {
		t.Fatalf("HIGH-02 violation: mainnet honored ARMX_ARMAXHASH_HEIGHT env override: got %d, want default %d", h, MainnetProfile.ArmaxHashActivationHeight)
	} else if h != MainnetProfile.ArmaxHashActivationHeight {
		t.Fatalf("expected mainnet default %d, got %d", MainnetProfile.ArmaxHashActivationHeight, h)
	}

	if h2 := GetArmaxHashV2ActivationHeight(); h2 == 456 {
		t.Fatalf("HIGH-02 violation: mainnet honored ARMX_ARMAXHASH_V2_HEIGHT env override: got %d, want default %d", h2, MainnetProfile.ArmaxHashV2ActivationHeight)
	} else if h2 != MainnetProfile.ArmaxHashV2ActivationHeight {
		t.Fatalf("expected mainnet v2 default %d, got %d", MainnetProfile.ArmaxHashV2ActivationHeight, h2)
	}

	// 3. Verify Regtest DOES honor env vars for development/testing
	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("failed to set regtest: %v", err)
	}

	if h := GetArmaxHashActivationHeight(); h != 123 {
		t.Fatalf("regtest should honor ARMX_ARMAXHASH_HEIGHT env override: got %d, want 123", h)
	}
	if h2 := GetArmaxHashV2ActivationHeight(); h2 != 456 {
		t.Fatalf("regtest should honor ARMX_ARMAXHASH_V2_HEIGHT env override: got %d, want 456", h2)
	}
}
