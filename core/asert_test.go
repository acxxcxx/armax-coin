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
	if MainnetProfile.ASERTActivationHeight != 0 {
		t.Fatalf("expected MainnetProfile.ASERTActivationHeight == 0, got %d", MainnetProfile.ASERTActivationHeight)
	}
}

// TestASERT_UnboundedNegativeDrift proves programmatically that MaxASERTBlockInterval (14,400s)
// only caps per-block positive/negative interval deltas and NEVER caps cumulative difficulty growth
// across consecutive fast blocks (dt = 1s) relative to Anchor #13,334.
func TestASERT_UnboundedNegativeDrift(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()
	SetASERTActivationHeight(13335)
	defer ResetASERTActivationHeightOverride()
	SetAntiStallActivationHeight(-1)
	defer ResetAntiStallActivationHeightOverride()
	ResetASERTDriftCache()

	anchorTargetHex := "0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff" // Diff 7
	anchorTarget, _ := new(big.Int).SetString(anchorTargetHex, 16)
	chain := make([]Block, 13335)
	for i := 0; i < 13335; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  1700000000 + int64(i*120),
			Target:     anchorTargetHex,
			Difficulty: 7,
			Hash:       fmt.Sprintf("%064x", i+1),
		}
	}

	minDrift, maxDrift := ComputeDriftBounds(anchorTarget, GetArmaxHashPowLimit())
	effDrift := int64(0)
	prev := chain[13334]

	// Simulate 1,200 fast blocks with dt = 1s -> stepDrift = -119s per block -> -142,800s cumulative drift.
	for h := 13335; h < 13335+1200; h++ {
		ts := prev.Timestamp + 1
		tgt := CalculateArmaxASERTTarget(chain, h, ts)
		curr := Block{
			Index:      h,
			Timestamp:  ts,
			Target:     fmt.Sprintf("%064x", tgt),
			Difficulty: TargetToDifficulty(tgt),
			Hash:       fmt.Sprintf("%064x", h+1),
		}
		effDrift = StepEffectiveDrift(effDrift, &prev, &curr, minDrift, maxDrift)
		chain = append(chain, curr)
		prev = curr
	}

	const expectedCumulativeDrift = int64(1200 * (1 - TargetBlockTimeSeconds)) // -142,800s
	if effDrift != expectedCumulativeDrift {
		t.Fatalf("expected unbounded cumulative negative drift %d, got %d", expectedCumulativeDrift, effDrift)
	}

	// Verify target contracted past 2^30x (> 1,073,741,824x difficulty multiplier).
	finalTarget := CalculateArmaxASERTTarget(chain, len(chain), prev.Timestamp+1)
	diffMultiplier := new(big.Float).Quo(new(big.Float).SetInt(anchorTarget), new(big.Float).SetInt(finalTarget))
	multVal, _ := diffMultiplier.Float64()
	if multVal <= float64(uint64(1)<<30) {
		t.Fatalf("expected cumulative difficulty growth > 2^30x (~8.7e11x), got %.2fx", multVal)
	}

	// Verify minDrift bound reaches exact Target = 1 without premature clamping.
	extremeTarget := ComputeASERTFromDrift(anchorTarget, minDrift)
	if extremeTarget.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected minDrift (%d) to reach tightest target 1, got %s", minDrift, extremeTarget.String())
	}
}

// TestASERT_OutageRecoveryCap simulates a 10-day chain stall (dt = 864,000s) with TEDR disabled
// and verifies single-block target loosening is strictly capped at MaxASERTBlockInterval = 14,400s (~15.63x).
func TestASERT_OutageRecoveryCap(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()
	SetASERTActivationHeight(13335)
	defer ResetASERTActivationHeightOverride()
	SetAntiStallActivationHeight(-1) // Isolate pure ASERT 14,400s surge cap without TEDR
	defer ResetAntiStallActivationHeightOverride()
	ResetASERTDriftCache()

	anchorTargetHex := "0000000000ffffffffffffffffffffffffffffffffffffffffffffffffffffff" // Diff 10
	anchorTarget, _ := new(big.Int).SetString(anchorTargetHex, 16)
	chain := make([]Block, 13335)
	for i := 0; i < 13335; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  1700000000 + int64(i*120),
			Target:     anchorTargetHex,
			Difficulty: 10,
			Hash:       fmt.Sprintf("%064x", i+1),
		}
	}

	const tenDayStallSec = int64(10 * 24 * 3600) // 864,000s
	tip := &chain[13334]
	cand := &Block{Index: 13335, Timestamp: tip.Timestamp + tenDayStallSec}
	minDrift, maxDrift := ComputeDriftBounds(anchorTarget, GetArmaxHashPowLimit())

	stepDrift := StepEffectiveDrift(0, tip, cand, minDrift, maxDrift)
	const wantStepDrift = MaxASERTBlockInterval - TargetBlockTimeSeconds // 14,400 - 120 = 14,280s
	if stepDrift != wantStepDrift {
		t.Fatalf("expected 10-day stall drift clamped to %ds, got %ds", wantStepDrift, stepDrift)
	}

	targetAfterStall := CalculateArmaxASERTTarget(chain, 13335, tip.Timestamp+tenDayStallSec)
	ratio := new(big.Float).Quo(new(big.Float).SetInt(targetAfterStall), new(big.Float).SetInt(anchorTarget))
	rFloat, _ := ratio.Float64()
	if rFloat < 15.60 || rFloat > 15.66 {
		t.Fatalf("expected single-block relaxation capped at ~15.63x, got %.4fx", rFloat)
	}
}

// TestASERT_DriftCacheRebuild verifies bit-for-bit identical target and cumulative drift
// across cold restarts (cache cleared and rebuilt) for both Testnet Anchor #13,334 and Genesis Anchor #0.
func TestASERT_DriftCacheRebuild(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()
	defer ResetASERTActivationHeightOverride()
	defer ResetAntiStallActivationHeightOverride()

	modes := []struct {
		name      string
		actHeight int64
		tedrOn    bool
		chainLen  int
	}{
		{"Testnet_Anchor_13334", 13335, true, 13450},
		{"Mainnet_Genesis_Anchor_0", 0, false, 150},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			ResetASERTDriftCache()
			SetASERTActivationHeight(m.actHeight)
			if m.tedrOn {
				SetAntiStallActivationHeight(13293)
			} else {
				SetAntiStallActivationHeight(-1)
			}

			anchorTargetHex := "000000000fffffffffffffffffffffffffffffffffffffffffffffffffffffff" // Diff 9
			chain := make([]Block, m.chainLen)
			var ts int64 = 1700000000
			for i := 0; i < m.chainLen; i++ {
				dt := int64(120)
				tgtHex := anchorTargetHex
				diff := 9
				if i > int(m.actHeight)+10 {
					switch i % 7 {
					case 0:
						dt = 25000 // > 14,400s outage (or TEDR if powLimit target)
						if m.tedrOn {
							dt = 1300
							tgtHex = fmt.Sprintf("%064x", GetArmaxHashPowLimit())
							diff = TargetToDifficulty(GetArmaxHashPowLimit())
						}
					case 1, 2:
						dt = 15 // fast block burst
					default:
						dt = 120
					}
				}
				ts += dt
				chain[i] = Block{
					Index:      i,
					Timestamp:  ts,
					Target:     tgtHex,
					Difficulty: diff,
					Hash:       fmt.Sprintf("%064x", i+1),
				}
			}

			tip := chain[len(chain)-1]
			candTs := tip.Timestamp + 120

			targetBefore := CalculateArmaxASERTTarget(chain, len(chain), candTs)
			driftBefore, okBefore := GetCachedASERTDrift(tip.Hash)
			if !okBefore {
				t.Fatalf("expected tip drift to be populated in globalASERTDriftCache")
			}

			// Simulate cold node restart: wipe global ASERT drift cache completely.
			ResetASERTDriftCache()
			if _, okCleared := GetCachedASERTDrift(tip.Hash); okCleared {
				t.Fatalf("expected cache entry to be cleared after ResetASERTDriftCache")
			}

			// Recompute on cold cache -> triggers deterministic scan rebuild from anchor.
			targetAfter := CalculateArmaxASERTTarget(chain, len(chain), candTs)
			driftAfter, okAfter := GetCachedASERTDrift(tip.Hash)
			if !okAfter {
				t.Fatalf("expected tip drift to be rebuilt in globalASERTDriftCache")
			}
			if driftBefore != driftAfter {
				t.Fatalf("rebuilt cumulative drift mismatch: before=%d, after=%d", driftBefore, driftAfter)
			}
			if targetBefore.Cmp(targetAfter) != 0 {
				t.Fatalf("rebuilt target mismatch: before=%064x, after=%064x", targetBefore, targetAfter)
			}
		})
	}
}

// TestASERT_DriftCachePruning verifies that populating > 10,000 entries into globalASERTDriftCache
// safely triggers bounded window pruning (retaining the latest 2,048 entries) without affecting
// ASERT target calculation or cumulative drift consistency at the tip.
func TestASERT_DriftCachePruning(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()
	SetASERTActivationHeight(1)
	defer ResetASERTActivationHeightOverride()
	SetAntiStallActivationHeight(-1)
	defer ResetAntiStallActivationHeightOverride()
	ResetASERTDriftCache()
	defer ResetASERTDriftCache()

	const totalBlocks = 10505 // > maxASERTDriftCacheEntries (10,000)
	anchorTargetHex := "000000000fffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	chain := make([]Block, totalBlocks)
	var ts int64 = 1700000000
	for i := 0; i < totalBlocks; i++ {
		dt := int64(120)
		switch i % 5 {
		case 0:
			dt = 90
		case 2:
			dt = 150
		}
		ts += dt
		chain[i] = Block{
			Index:      i,
			Timestamp:  ts,
			Target:     anchorTargetHex,
			Difficulty: 9,
			Hash:       fmt.Sprintf("%064x", i+1),
		}
	}

	tip := chain[totalBlocks-1]
	candTs := tip.Timestamp + 120

	// 1. Trigger cache population across 10,505 blocks -> automatically prunes > 10,000 down to 2,048 entries.
	targetPruned := CalculateArmaxASERTTarget(chain, totalBlocks, candTs)
	cacheLen := ASERTDriftCacheLen()
	if cacheLen != prunedASERTDriftWindowSize {
		t.Fatalf("expected pruned globalASERTDriftCache length %d, got %d (max limit %d)",
			prunedASERTDriftWindowSize, cacheLen, maxASERTDriftCacheEntries)
	}

	// Early history entries must be evicted while tip & recent 2048 window remain O(1) cached.
	if _, earlyPresent := GetCachedASERTDrift(chain[10].Hash); earlyPresent {
		t.Fatalf("expected old block #10 to be evicted from pruned ASERT cache")
	}
	driftPruned, tipCached := GetCachedASERTDrift(tip.Hash)
	if !tipCached {
		t.Fatalf("expected chain tip to remain cached after pruning")
	}

	// 2. Extend by 1 block using pruned cache -> must match unpruned fresh computation bit-for-bit.
	nextBlk := Block{
		Index:      totalBlocks,
		Timestamp:  candTs,
		Target:     fmt.Sprintf("%064x", targetPruned),
		Difficulty: TargetToDifficulty(targetPruned),
		Hash:       fmt.Sprintf("%064x", totalBlocks+1),
	}
	extendedChain := append(chain, nextBlk)
	nextCandTs := nextBlk.Timestamp + 100

	targetFromPrunedCache := CalculateArmaxASERTTarget(extendedChain, totalBlocks+1, nextCandTs)

	// Recompute from empty cache on the same extendedChain to confirm identical result
	ResetASERTDriftCache()
	targetUnprunedBaseline := CalculateArmaxASERTTarget(extendedChain, totalBlocks+1, nextCandTs)
	if targetFromPrunedCache.Cmp(targetUnprunedBaseline) != 0 {
		t.Fatalf("pruned cache target mismatch: pruned=%064x, baseline=%064x", targetFromPrunedCache, targetUnprunedBaseline)
	}
	// Also verify tip drift computed under pruned cache equals baseline tip drift
	driftBaseline, okBase := GetCachedASERTDrift(tip.Hash)
	if !okBase || driftBaseline != driftPruned {
		t.Fatalf("pruned vs baseline drift mismatch at tip: pruned=%d, baseline=%d (ok=%v)", driftPruned, driftBaseline, okBase)
	}
}
