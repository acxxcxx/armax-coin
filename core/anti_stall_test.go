package core

import (
	"fmt"
	"testing"
)

func withAntiStallTestnet(t *testing.T, height int64) {
	t.Helper()
	unlock := AcquireNetworkTestLock()
	netMu.Lock()
	origNet := ActiveNetwork
	ActiveNetwork = TestnetProfile
	netMu.Unlock()
	SetAntiStallActivationHeight(height)
	t.Cleanup(func() {
		ResetAntiStallActivationHeightOverride()
		netMu.Lock()
		ActiveNetwork = origNet
		netMu.Unlock()
		unlock()
	})
}

// TestAntiStall_T01_T02_T03_ThresholdBoundaries verifies the strict 1200s boundary:
// - T-01: 1199s (sub-threshold) -> emergency is NOT eligible
// - T-02: 1200s (exact boundary) -> emergency is NOT eligible (strictly > 1200)
// - T-03: 1201s (super-threshold) -> emergency is ELIGIBLE
func TestAntiStall_T01_T02_T03_ThresholdBoundaries(t *testing.T) {
	if TestnetProfile.AntiStallActivationHeight != 13293 {
		t.Fatalf("Expected TestnetProfile.AntiStallActivationHeight == 13293, got %d", TestnetProfile.AntiStallActivationHeight)
	}
	withAntiStallTestnet(t, 13293)

	prevTs := int64(1710000000)

	// T-01: 1199s
	if IsEmergencyBlockCandidate(prevTs, prevTs+1199, 13293) {
		t.Fatalf("T-01 Failed: expected Delta t = 1199s to reject emergency eligibility")
	}

	// T-02: 1200s
	if IsEmergencyBlockCandidate(prevTs, prevTs+1200, 13293) {
		t.Fatalf("T-02 Failed: expected Delta t = 1200s to reject emergency eligibility")
	}

	// T-03: 1201s
	if !IsEmergencyBlockCandidate(prevTs, prevTs+1201, 13293) {
		t.Fatalf("T-03 Failed: expected Delta t = 1201s to accept emergency eligibility")
	}
}

// TestAntiStall_TargetCalculations verifies that CalculateExpectedTargetForCandidate
// returns ArmaxHashPowLimit for an emergency candidate, and regular LWMA target for normal candidate.
func TestAntiStall_TargetCalculations(t *testing.T) {
	withAntiStallTestnet(t, 100)

	diff10 := DifficultyToTarget(10)
	baseTs := int64(1710000000)

	var chain []Block
	for i := 0; i <= 60; i++ {
		chain = append(chain, Block{
			Index:     40 + i,
			Timestamp: baseTs + int64(i)*80,
			Target:    fmt.Sprintf("%064x", diff10),
			Hash:      fmt.Sprintf("%064x", 40+i),
		})
	}

	prev := chain[len(chain)-1]
	nextIdx := prev.Index + 1 // 101 >= 100

	// 1. Normal interval (120s): should return normal target (approx diff 10)
	normTarget := CalculateExpectedTargetForCandidate(chain, nextIdx, prev.Timestamp+120)
	if TargetToDifficulty(normTarget) != 10 {
		t.Fatalf("Expected normal target diff 10, got %d", TargetToDifficulty(normTarget))
	}

	// 2. Emergency interval (1201s): should return ArmaxHashPowLimit (diff 3)
	emTarget := CalculateExpectedTargetForCandidate(chain, nextIdx, prev.Timestamp+1201)
	expectedEm := GetArmaxHashPowLimit()
	if emTarget.Cmp(expectedEm) != 0 {
		t.Fatalf("Expected emergency target %064x, got %064x", expectedEm, emTarget)
	}
}

// TestAntiStall_ConsecutiveEmergencies verifies Model 3 Synthetic Normalization:
// Consecutive emergency blocks use synthetic target normalization (anchored to last normal target)
// and normalized solve time (720s), guaranteeing deterministic non-circular behavior.
func TestAntiStall_ConsecutiveEmergencies(t *testing.T) {
	withAntiStallTestnet(t, 100)

	diff10 := DifficultyToTarget(10)
	diff10Hex := fmt.Sprintf("%064x", diff10)
	emHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	baseTs := int64(1710000000)

	var chain []Block
	for i := 0; i <= 60; i++ {
		chain = append(chain, Block{
			Index:     40 + i,
			Timestamp: baseTs + int64(i)*80,
			Target:    diff10Hex,
			Hash:      fmt.Sprintf("%064x", 40+i),
		})
	}

	// Add 3 consecutive emergency blocks
	for k := 1; k <= 3; k++ {
		prev := chain[len(chain)-1]
		b := Block{
			Index:     prev.Index + 1,
			Timestamp: prev.Timestamp + 1201,
			Target:    emHex,
			Hash:      fmt.Sprintf("em_%d", k),
		}
		if !IsBlockEmergencyRecord(&b, &prev) {
			t.Fatalf("Block #%d should be recognized as emergency record", b.Index)
		}
		chain = append(chain, b)
	}

	// Verify next target for block 104 is computed deterministically without panics or circular recursion
	target1 := CalculateExpectedTarget(chain, len(chain))
	target2 := CalculateExpectedTarget(chain, len(chain))
	if target1.Cmp(target2) != 0 {
		t.Fatalf("CalculateExpectedTarget is non-deterministic: %s != %s", target1.String(), target2.String())
	}

	// Target should be valid and bounded
	if target1.Sign() <= 0 || target1.Cmp(GetPowLimit()) > 0 {
		t.Fatalf("Target out of bounds: %s", target1.String())
	}
}

// TestAntiStall_T06_MainnetStrictProtection verifies that TEDR is strictly disabled on Mainnet
// even if solve interval is extreme (e.g. 100,000s).
func TestAntiStall_T06_MainnetStrictProtection(t *testing.T) {
	SetAntiStallActivationHeight(1)
	defer ResetAntiStallActivationHeightOverride()

	origNet := GetActiveNetwork()
	netMu.Lock()
	ActiveNetwork = MainnetProfile
	netMu.Unlock()
	defer func() {
		netMu.Lock()
		ActiveNetwork = origNet
		netMu.Unlock()
	}()

	// 1. IsAntiStallActive must permanently return false on mainnet
	if IsAntiStallActive(100) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: IsAntiStallActive returned true on Mainnet!")
	}

	// 2. IsEmergencyBlockCandidate must permanently return false on mainnet
	if IsEmergencyBlockCandidate(1000, 100000, 100) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: IsEmergencyBlockCandidate returned true on Mainnet!")
	}
}

// TestAntiStall_T16_PreActivationRejection verifies that heights before activation height
// strictly reject the emergency target.
func TestAntiStall_T16_PreActivationRejection(t *testing.T) {
	withAntiStallTestnet(t, 13293)

	prevTs := int64(1710000000)

	// Block #13292 is below activation height 13293
	if IsAntiStallActive(13292) {
		t.Fatalf("T-16 Failed: height 13292 must not be active for anti-stall")
	}
	if IsEmergencyBlockCandidate(prevTs, prevTs+2000, 13292) {
		t.Fatalf("T-16 Failed: candidate 13292 must not be eligible for emergency target")
	}

	// Block #13293 is at activation height
	if !IsAntiStallActive(13293) {
		t.Fatalf("T-16 Failed: height 13293 must be active for anti-stall")
	}
	if !IsEmergencyBlockCandidate(prevTs, prevTs+2000, 13293) {
		t.Fatalf("T-16 Failed: candidate 13293 must be eligible for emergency target")
	}
}

// TestAntiStall_GPUReturnStability verifies that when fast blocks arrive after an emergency sequence,
// the difficulty safely ratchets back up without oscillation or breakdown.
func TestAntiStall_GPUReturnStability(t *testing.T) {
	withAntiStallTestnet(t, 100)

	diff10 := DifficultyToTarget(10)
	diff10Hex := fmt.Sprintf("%064x", diff10)
	emHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	baseTs := int64(1710000000)

	var chain []Block
	for i := 0; i <= 60; i++ {
		chain = append(chain, Block{
			Index:     40 + i,
			Timestamp: baseTs + int64(i)*80,
			Target:    diff10Hex,
			Hash:      fmt.Sprintf("%064x", 40+i),
		})
	}

	// 5 Emergency blocks
	for k := 1; k <= 5; k++ {
		prev := chain[len(chain)-1]
		chain = append(chain, Block{
			Index:     prev.Index + 1,
			Timestamp: prev.Timestamp + 1201,
			Target:    emHex,
			Hash:      fmt.Sprintf("em_%d", k),
		})
	}

	// GPU returns and mines 5 fast blocks (10s each)
	for j := 1; j <= 5; j++ {
		prev := chain[len(chain)-1]
		nextT := CalculateExpectedTarget(chain, len(chain))
		chain = append(chain, Block{
			Index:     prev.Index + 1,
			Timestamp: prev.Timestamp + 10,
			Target:    fmt.Sprintf("%064x", nextT),
			Hash:      fmt.Sprintf("gpu_%d", j),
		})
	}

	finalTarget := CalculateExpectedTarget(chain, len(chain))
	if finalTarget.Sign() <= 0 {
		t.Fatalf("Final target is invalid: %s", finalTarget.String())
	}
	// Difficulty should remain strong (at least diff 8 or higher)
	finalDiff := TargetToDifficulty(finalTarget)
	if finalDiff < 8 {
		t.Fatalf("Expected final difficulty >= 8 after GPU return, got %d", finalDiff)
	}
}

// TestAntiStall_T17_ReorgAcrossActivation verifies that blocks spanning across the activation height
// are evaluated with exact height-dependent rules during fork resolution and reorganization.
func TestAntiStall_T17_ReorgAcrossActivation(t *testing.T) {
	withAntiStallTestnet(t, 100)

	diff10 := DifficultyToTarget(10)
	diff10Hex := fmt.Sprintf("%064x", diff10)
	emHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	baseTs := int64(1710000000)

	var ancestorChain []Block
	for i := 0; i <= 60; i++ {
		ancestorChain = append(ancestorChain, Block{
			Index:     35 + i, // Block 35 to 95
			Timestamp: baseTs + int64(i)*80,
			Target:    diff10Hex,
			Hash:      fmt.Sprintf("%064x", 35+i),
		})
	}

	// Fork branch starting from Block 95 up to Block 102 (spanning across activation height 100)
	// Block 96..99 (Pre-activation): even if interval > 1200s, emergency target MUST be rejected!
	prev := ancestorChain[len(ancestorChain)-1]
	for h := 96; h <= 99; h++ {
		// Interval is 2000s (> 1200s)
		candTs := prev.Timestamp + 2000
		if IsEmergencyBlockCandidate(prev.Timestamp, candTs, h) {
			t.Fatalf("T-17 Failed: Block #%d (< 100) must NOT be eligible for emergency target", h)
		}
		// Target must remain normal target
		expectedT := CalculateExpectedTargetForCandidate(ancestorChain, h, candTs)
		if fmt.Sprintf("%064x", expectedT) == emHex {
			t.Fatalf("T-17 Failed: Block #%d (< 100) unexpectedly yielded emergency target", h)
		}
		b := Block{
			Index:     h,
			Timestamp: candTs,
			Target:    fmt.Sprintf("%064x", expectedT),
			Hash:      fmt.Sprintf("fork_%d", h),
		}
		ancestorChain = append(ancestorChain, b)
		prev = b
	}

	// Block 100..102 (Post-activation): interval > 1200s MUST be eligible for emergency target
	for h := 100; h <= 102; h++ {
		candTs := prev.Timestamp + 2000
		if !IsEmergencyBlockCandidate(prev.Timestamp, candTs, h) {
			t.Fatalf("T-17 Failed: Block #%d (>= 100) MUST be eligible for emergency target", h)
		}
		expectedT := CalculateExpectedTargetForCandidate(ancestorChain, h, candTs)
		if fmt.Sprintf("%064x", expectedT) != emHex {
			t.Fatalf("T-17 Failed: Block #%d (>= 100) expected emergency target %s, got %064x", h, emHex, expectedT)
		}
		b := Block{
			Index:     h,
			Timestamp: candTs,
			Target:    fmt.Sprintf("%064x", expectedT),
			Hash:      fmt.Sprintf("fork_%d", h),
		}
		ancestorChain = append(ancestorChain, b)
		prev = b
	}
}
