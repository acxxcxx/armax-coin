//go:build testhooks

package core

import (
	"math"
	"strconv"
	"testing"
)

func TestCodeQL_G3_RegtestEnvHeightBoundaries(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest): %v", err)
	}
	t.Cleanup(func() {
		ResetASERTActivationHeightOverride()
		ResetArmaxHashV2ActivationHeightOverride()
		_ = SetNetwork("testnet")
	})

	ResetASERTActivationHeightOverride()
	ResetArmaxHashV2ActivationHeightOverride()

	// Valid in-range values [-1, math.MaxInt32] must apply cleanly.
	t.Setenv("ARMX_ASERT_HEIGHT", "100")
	if got := GetASERTActivationHeight(); got != 100 {
		t.Fatalf("expected ASERT regtest height 100, got %d", got)
	}
	t.Setenv("ARMX_ASERT_HEIGHT", strconv.Itoa(math.MaxInt32))
	if got := GetASERTActivationHeight(); got != math.MaxInt32 {
		t.Fatalf("expected ASERT regtest height MaxInt32 (%d), got %d", math.MaxInt32, got)
	}
	t.Setenv("ARMX_ARMAXHASH_V2_HEIGHT", "250")
	if got := GetArmaxHashV2ActivationHeight(); got != 250 {
		t.Fatalf("expected ArmaxHashV2 regtest height 250, got %d", got)
	}
	t.Setenv("ARMX_ARMAXHASH_V2_HEIGHT", strconv.Itoa(math.MaxInt32))
	if got := GetArmaxHashV2ActivationHeight(); got != math.MaxInt32 {
		t.Fatalf("expected ArmaxHashV2 regtest height MaxInt32 (%d), got %d", math.MaxInt32, got)
	}

	// Out-of-range values (> math.MaxInt32 or < -1) must be safely ignored to prevent 32-bit int truncation.
	for _, bad := range []string{"-2", "2147483648", strconv.FormatInt(math.MaxInt64, 10)} {
		t.Setenv("ARMX_ASERT_HEIGHT", bad)
		if got := GetASERTActivationHeight(); got != RegtestProfile.ASERTActivationHeight {
			t.Fatalf("ARMX_ASERT_HEIGHT=%q must fall back to RegtestProfile default (%d), got %d",
				bad, RegtestProfile.ASERTActivationHeight, got)
		}
		t.Setenv("ARMX_ARMAXHASH_V2_HEIGHT", bad)
		if got := GetArmaxHashV2ActivationHeight(); got != RegtestProfile.ArmaxHashV2ActivationHeight {
			t.Fatalf("ARMX_ARMAXHASH_V2_HEIGHT=%q must fall back to RegtestProfile default (%d), got %d",
				bad, RegtestProfile.ArmaxHashV2ActivationHeight, got)
		}
	}
}
