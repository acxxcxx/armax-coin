package core

import (
	"fmt"
	"math/big"
	"sync"
	"testing"
)

// TestCanonicalASERTBitForBitVectors verifies all 17 canonical integer aserti3-2d
// test vectors for exact bit-for-bit equivalence.
func TestCanonicalASERTBitForBitVectors(t *testing.T) {
	anchorHex := "0000000000000000000000000000000000000000000000000000ffff00000000"
	anchorTarget, ok := new(big.Int).SetString(anchorHex, 16)
	if !ok {
		t.Fatalf("failed to parse anchor hex")
	}

	testCases := []struct {
		driftSec    int64
		expectedHex string
		description string
	}{
		{-10000, "000000000000000000000000000000000000000000000000000025547aab6000", "Deep negative extrapolation"},
		{-7200, "00000000000000000000000000000000000000000000000000003fffc0000000", "Exact -2tau (0.25x)"},
		{-3601, "00000000000000000000000000000000000000000000000000007ff900068000", "Lower octave boundary"},
		{-3600, "00000000000000000000000000000000000000000000000000007fff80000000", "Exact -1tau (0.50x)"},
		{-3599, "00000000000000000000000000000000000000000000000000008005fff98000", "Lower octave entry"},
		{-1201, "0000000000000000000000000000000000000000000000000000cb2534da0000", "-10 block schedule drift"},
		{-1200, "0000000000000000000000000000000000000000000000000000cb2f34d00000", "Exact -10 ideal intervals"},
		{-1, "0000000000000000000000000000000000000000000000000000fff2000d0000", "Negative fractional boundary"},
		{0, "0000000000000000000000000000000000000000000000000000ffff00000000", "Exact Anchor Target (1.00x)"},
		{1, "0000000000000000000000000000000000000000000000000001000bfff30000", "Positive fractional boundary"},
		{1200, "00000000000000000000000000000000000000000000000000014285bd790000", "+10 ideal intervals"},
		{1201, "00000000000000000000000000000000000000000000000000014294bd6a0000", "Testnet TEDR threshold"},
		{3599, "0000000000000000000000000000000000000000000000000001ffe4001a0000", "Upper octave boundary"},
		{3600, "0000000000000000000000000000000000000000000000000001fffe00000000", "Exact +1tau (2.00x)"},
		{3601, "00000000000000000000000000000000000000000000000000020017ffe60000", "Upper octave entry"},
		{7200, "0000000000000000000000000000000000000000000000000003fffc00000000", "Exact +2tau (4.00x)"},
		{10000, "0000000000000000000000000000000000000000000000000006dbc124380000", "Deep positive extrapolation"},
	}

	for _, tc := range testCases {
		actual := ComputeASERTFromDrift(anchorTarget, tc.driftSec)
		actualHex := fmt.Sprintf("%064x", actual)
		if actualHex != tc.expectedHex {
			t.Fatalf("Vector %s (drift=%d) mismatch:\n  got:  %s\n  want: %s",
				tc.description, tc.driftSec, actualHex, tc.expectedHex)
		}
	}
}

// TestFloorDivEdgeCases verifies mathematical floor division across negative and positive numerators.
func TestFloorDivEdgeCases(t *testing.T) {
	cases := []struct {
		a, b, expected int64
	}{
		{0, 3600, 0},
		{1, 3600, 0},
		{3599, 3600, 0},
		{3600, 3600, 1},
		{-1, 3600, -1},
		{-3599, 3600, -1},
		{-3600, 3600, -1},
		{-3601, 3600, -2},
		{-7200, 3600, -2},
		{-7201, 3600, -3},
	}
	for _, c := range cases {
		got := FloorDiv(c.a, c.b)
		if got != c.expected {
			t.Fatalf("FloorDiv(%d, %d): got %d, want %d", c.a, c.b, got, c.expected)
		}
	}
}

// TestASERTActivationAndAnchorResolution verifies Block #16,000 seamless activation,
// anchor evaluation at Block #15,999, and anti-poisoning guard when #15,999 is an emergency block.
func TestASERTActivationAndAnchorResolution(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()

	if !IsASERTActive(16000) {
		t.Fatalf("expected Block #16,000 to be active under ASERT")
	}
	if IsASERTActive(15999) {
		t.Fatalf("expected Block #15,999 to be pre-activation (LWMA)")
	}

	// 1. Construct mock chain up to #15,999 with normal target
	chain := make([]Block, 16000)
	normalTargetHex := "0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	for i := 0; i < 16000; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  1700000000 + int64(i*120),
			Target:     normalTargetHex,
			Difficulty: 7,
			Hash:       fmt.Sprintf("%064x", i),
		}
	}

	anchorBlk, anchorTgt, err := ResolveASERTAnchor(chain, 16000)
	if err != nil {
		t.Fatalf("ResolveASERTAnchor failed: %v", err)
	}
	if anchorBlk.Index != 15999 {
		t.Fatalf("expected anchor index 15999, got %d", anchorBlk.Index)
	}
	if fmt.Sprintf("%064x", anchorTgt) != normalTargetHex {
		t.Fatalf("expected anchor target %s, got %064x", normalTargetHex, anchorTgt)
	}

	// First candidate block at height #16,000 with ideal timestamp must evaluate with zero drift
	target16000Ideal := CalculateArmaxASERTTarget(chain, 16000, chain[15999].Timestamp+120)
	if fmt.Sprintf("%064x", target16000Ideal) != normalTargetHex {
		t.Fatalf("expected seamless activation target %s, got %064x", normalTargetHex, target16000Ideal)
	}

	// Slower candidate block at height #16,000 (+120s slow) must expand target (lower difficulty)
	target16000Slow := CalculateArmaxASERTTarget(chain, 16000, chain[15999].Timestamp+240)
	if target16000Slow.Cmp(anchorTgt) <= 0 {
		t.Fatalf("expected slow block 16000 target to be larger than anchor target: got %s, anchor %s",
			target16000Slow.String(), anchorTgt.String())
	}

	// Faster candidate block at height #16,000 (-60s fast) must contract target (higher difficulty)
	target16000Fast := CalculateArmaxASERTTarget(chain, 16000, chain[15999].Timestamp+60)
	if target16000Fast.Cmp(anchorTgt) >= 0 {
		t.Fatalf("expected fast block 16000 target to be smaller than anchor target: got %s, anchor %s",
			target16000Fast.String(), anchorTgt.String())
	}

	// 2. Test Anchor Poisoning Guard (DA-03):
	// Simulate Block #15,999 as an emergency block
	emHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	chain[15999].Target = emHex
	chain[15999].Difficulty = 3
	chain[15999].Timestamp = chain[15998].Timestamp + 1205 // > 1200s TEDR condition

	_, safeTarget, err := ResolveASERTAnchor(chain, 16000)
	if err != nil {
		t.Fatalf("ResolveASERTAnchor with emergency anchor failed: %v", err)
	}
	if fmt.Sprintf("%064x", safeTarget) != normalTargetHex {
		t.Fatalf("Anti-poisoning guard failed: expected unpoisoned target %s, got %064x", normalTargetHex, safeTarget)
	}
}

// TestASERT200xCrashRecoveryAndTEDRBoost verifies the 4-Hour Surge Cap and TEDR Boost
// when hashrate collapses by 200x.
func TestASERT200xCrashRecoveryAndTEDRBoost(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()
	ResetASERTDriftCache()

	normalTargetHex := "0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff" // Diff 7
	chain := make([]Block, 16000)
	for i := 0; i < 16000; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  1700000000 + int64(i*120),
			Target:     normalTargetHex,
			Difficulty: 7,
			Hash:       fmt.Sprintf("%064x", i),
		}
	}

	// 1. Pure ASERT 4-Hour Surge Cap recovery (14,400s)
	// Single block with 24,000s solve time (6.67 hours)
	cappedDt := int64(24000)
	prev := &chain[15999]
	cand1 := &Block{Index: 16000, Timestamp: prev.Timestamp + cappedDt}
	minDrift, maxDrift := ComputeDriftBounds(prev.GetTarget(), GetArmaxHashPowLimit())

	effDrift := StepEffectiveDrift(0, prev, cand1, minDrift, maxDrift)
	// 14400 - 120 = 14280s drift injected
	if effDrift != 14280 {
		t.Fatalf("expected 4h-capped drift 14280s, got %d", effDrift)
	}
	target1 := ComputeASERTFromDrift(prev.GetTarget(), effDrift)

	ratio := new(big.Float).Quo(new(big.Float).SetInt(target1), new(big.Float).SetInt(prev.GetTarget()))
	rFloat, _ := ratio.Float64()
	if rFloat < 15.0 || rFloat > 16.0 {
		t.Fatalf("expected ~15.63x target relaxation per 4-hour capped block, got %.2fx", rFloat)
	}

	// 2. TEDR Boost Recovery (3 emergency blocks at 20m interval)
	// On testnet with TEDR, miner only waits 1,201s per emergency block
	emDrift := int64(0)
	emPrev := &chain[15999]
	for b := 1; b <= 3; b++ {
		emBlock := &Block{
			Index:      15999 + b,
			Timestamp:  emPrev.Timestamp + 1205, // emergency trigger
			Target:     fmt.Sprintf("%064x", GetArmaxHashPowLimit()),
			Difficulty: 3,
			Hash:       fmt.Sprintf("em_block_%d", b),
		}
		emDrift = StepEffectiveDrift(emDrift, emPrev, emBlock, minDrift, maxDrift)
		emPrev = emBlock
	}

	// 3 emergency blocks * (7200 - 120) = 3 * 7080 = 21240s drift
	if emDrift != 21240 {
		t.Fatalf("expected 21240s drift after 3 TEDR boosts, got %d", emDrift)
	}

	targetAfterTEDR := ComputeASERTFromDrift(prev.GetTarget(), emDrift)
	boostRatio := new(big.Float).Quo(new(big.Float).SetInt(targetAfterTEDR), new(big.Float).SetInt(prev.GetTarget()))
	bFloat, _ := boostRatio.Float64()
	// 2^(21240/3600) = 2^5.9 = ~59.7x target relaxation!
	if bFloat < 58.0 || bFloat > 62.0 {
		t.Fatalf("expected ~59.7x relaxation after 3 TEDR blocks, got %.2fx", bFloat)
	}
}

// TestASERTDriftCacheConcurrency verifies that O(1) incremental drift caching is
// thread-safe under concurrent access with zero data races.
func TestASERTDriftCacheConcurrency(t *testing.T) {
	SetASERTActivationHeight(16000)
	defer ResetASERTActivationHeightOverride()
	ResetASERTDriftCache()

	normalTargetHex := "0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	chain := make([]Block, 16005)
	for i := 0; i < 16005; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  1700000000 + int64(i*120),
			Target:     normalTargetHex,
			Difficulty: 7,
			Hash:       fmt.Sprintf("%064x", i),
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for iter := 0; iter < 100; iter++ {
				candTs := chain[16004].Timestamp + int64(120+workerID)
				_ = CalculateArmaxASERTTarget(chain, 16005, candTs)
			}
		}(w)
	}
	wg.Wait()
}

// TestTestnetProfileASERTActivationHeight13335 verifies that the testnet profile specifies
// Block #13,335 as the canonical activation height for the Armax-ASERT-AW hard fork.
func TestTestnetProfileASERTActivationHeight13335(t *testing.T) {
	ResetASERTActivationHeightOverride()
	if TestnetProfile.ASERTActivationHeight != 13335 {
		t.Fatalf("expected TestnetProfile.ASERTActivationHeight == 13335, got %d", TestnetProfile.ASERTActivationHeight)
	}
	if MainnetProfile.ASERTActivationHeight != -1 {
		t.Fatalf("expected MainnetProfile.ASERTActivationHeight == -1, got %d", MainnetProfile.ASERTActivationHeight)
	}
}

