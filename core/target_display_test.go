package core

import (
	"fmt"
	"math/big"
	"testing"
)

// TestTargetToHashesPerBlock verifies expected hash trials computation across canonical targets.
func TestTargetToHashesPerBlock(t *testing.T) {
	// 1. Target = MaxTarget256 -> Expected Hashes = 1
	h1 := TargetToHashesPerBlock(MaxTarget256)
	if h1.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1 hash for MaxTarget256, got %s", h1.String())
	}

	// 2. Target = 1 -> Expected Hashes = MaxTarget256
	hMax := TargetToHashesPerBlock(big.NewInt(1))
	if hMax.Cmp(MaxTarget256) != 0 {
		t.Fatalf("expected MaxTarget256 hashes for Target 1, got %s", hMax.String())
	}

	// 3. Target = nil or 0 -> Expected Hashes = MaxTarget256
	hNil := TargetToHashesPerBlock(nil)
	if hNil.Cmp(MaxTarget256) != 0 {
		t.Fatalf("expected MaxTarget256 for nil target, got %s", hNil.String())
	}
	hZero := TargetToHashesPerBlock(big.NewInt(0))
	if hZero.Cmp(MaxTarget256) != 0 {
		t.Fatalf("expected MaxTarget256 for 0 target, got %s", hZero.String())
	}

	// 4. Half Target -> Expected Hashes = 2
	halfTarget := new(big.Int).Rsh(MaxTarget256, 1)
	hHalf := TargetToHashesPerBlock(halfTarget)
	if hHalf.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected 2 hashes for half target, got %s", hHalf.String())
	}
}

// TestFormatHashes verifies SI metric string formatting.
func TestFormatHashes(t *testing.T) {
	cases := []struct {
		hashes   int64
		expected string
	}{
		{500, "500 H"},
		{1000, "1.00 K"},
		{1250, "1.25 K"},
		{999900, "999.90 K"},
		{1000000, "1.00 M"},
		{15480000, "15.48 M"},
		{1250000000, "1.25 G"},
		{4800000000000, "4.80 T"},
	}

	for _, tc := range cases {
		actual := FormatHashes(big.NewInt(tc.hashes))
		if actual != tc.expected {
			t.Errorf("FormatHashes(%d) = %s, want %s", tc.hashes, actual, tc.expected)
		}
	}

	// Nil / zero check
	if got := FormatHashes(nil); got != "1 H" {
		t.Errorf("FormatHashes(nil) = %s, want '1 H'", got)
	}
}

// TestTargetToDifficultyRatio verifies the continuous floating-point difficulty ratio.
func TestTargetToDifficultyRatio(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()

	// In ArmaxHash era (height 1000), base target is ArmaxHashPowLimit
	baseTarget := GetArmaxHashPowLimit()

	// 1. Target == BaseTarget -> Ratio == 1.0000
	r1 := TargetToDifficultyRatio(baseTarget, 1000)
	if r1 < 0.9999 || r1 > 1.0001 {
		t.Fatalf("expected ratio ~1.0 for base target, got %.4f", r1)
	}

	// 2. Target == BaseTarget / 2 -> Ratio == 2.0000
	halfTarget := new(big.Int).Rsh(baseTarget, 1)
	r2 := TargetToDifficultyRatio(halfTarget, 1000)
	if r2 < 1.9999 || r2 > 2.0001 {
		t.Fatalf("expected ratio ~2.0 for half target, got %.4f", r2)
	}

	// 3. Target == BaseTarget / 16 -> Ratio == 16.0000
	sixteenthTarget := new(big.Int).Rsh(baseTarget, 4)
	r16 := TargetToDifficultyRatio(sixteenthTarget, 1000)
	if r16 < 15.9999 || r16 > 16.0001 {
		t.Fatalf("expected ratio ~16.0 for 1/16th target, got %.4f", r16)
	}
}

// TestFormatHashrate verifies SI metric rate formatting.
func TestFormatHashrate(t *testing.T) {
	cases := []struct {
		hps      int64
		expected string
	}{
		{50, "50.00 H/s"},
		{120000, "120.00 kH/s"},
		{1500000, "1.50 MH/s"},
		{2500000000, "2.50 GH/s"},
		{4000000000000, "4.00 TH/s"},
	}

	for _, tc := range cases {
		actual := FormatHashrate(big.NewInt(tc.hps))
		if actual != tc.expected {
			t.Errorf("FormatHashrate(%d) = %s, want %s", tc.hps, actual, tc.expected)
		}
	}
}

// TestGetCurrentDAAEngineName verifies dynamic algorithm naming across height 16,000.
func TestGetCurrentDAAEngineName(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()

	if name := GetCurrentDAAEngineName(100); name != "LWMA-1 (N=60)" {
		t.Fatalf("expected LWMA-1 at height 100, got %s", name)
	}
	if name := GetCurrentDAAEngineName(15999); name != "LWMA-1 (N=60)" {
		t.Fatalf("expected LWMA-1 at height 15999, got %s", name)
	}
	if name := GetCurrentDAAEngineName(16000); name != "Armax-ASERT-AW" {
		t.Fatalf("expected Armax-ASERT-AW at height 16000, got %s", name)
	}
	if name := GetCurrentDAAEngineName(17500); name != "Armax-ASERT-AW" {
		t.Fatalf("expected Armax-ASERT-AW at height 17500, got %s", name)
	}
}

// TestGetASERTScheduleDrift verifies drift calculation against Anchor #15,999.
func TestGetASERTScheduleDrift(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()

	// 1. Build chain up to 16,002
	chain := make([]Block, 16003)
	anchorTs := int64(1700000000)
	for i := 0; i <= 16002; i++ {
		ts := anchorTs
		if i < 15999 {
			ts = anchorTs - int64(15999-i)*120
		} else if i > 15999 {
			// Add 125 seconds per block (5s slower than 120s ideal schedule)
			ts = anchorTs + int64(i-15999)*125
		}
		chain[i] = Block{
			Index:      i,
			Timestamp:  ts,
			Target:     fmt.Sprintf("%064x", GetArmaxHashPowLimit()),
			Difficulty: 3,
			Hash:       fmt.Sprintf("block_%d", i),
		}
	}

	// 2. Pre-activation check at height 15,999 -> inactive
	_, active := GetASERTScheduleDrift(chain[:16000], 15999)
	if active {
		t.Fatalf("expected GetASERTScheduleDrift to be inactive below 16000")
	}

	// 3. At height 16,000 -> active, drift = (anchorTs + 125) - anchorTs - 120*1 = +5s
	drift, active := GetASERTScheduleDrift(chain[:16001], 16000)
	if !active {
		t.Fatalf("expected GetASERTScheduleDrift to be active at 16000")
	}
	if drift != 5 {
		t.Fatalf("expected drift +5s at block 16000, got %d", drift)
	}

	// 4. At height 16,002 -> active, drift = (anchorTs + 3*125) - anchorTs - 120*3 = +15s
	drift2, active2 := GetASERTScheduleDrift(chain, 16002)
	if !active2 {
		t.Fatalf("expected GetASERTScheduleDrift to be active at 16002")
	}
	if drift2 != 15 {
		t.Fatalf("expected drift +15s at block 16002, got %d", drift2)
	}
}
