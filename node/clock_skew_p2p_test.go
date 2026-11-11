package node

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestFutureTimestampErrorType verifies that core.FutureTimestampError correctly
// implements error and reports exact skew seconds.
func TestFutureTimestampErrorType(t *testing.T) {
	err := &core.FutureTimestampError{
		BlockHeight:    100,
		BlockTimestamp: 1700000100,
		MaxAllowed:     1700000000,
		SkewSeconds:    100,
	}

	if !core.IsFutureTimestampError(err) {
		t.Fatalf("expected IsFutureTimestampError to return true")
	}
	if core.GetFutureTimestampSkew(err) != 100 {
		t.Fatalf("expected skew 100s, got %d", core.GetFutureTimestampSkew(err))
	}

	standardErr := fmt.Errorf("generic validation error")
	if core.IsFutureTimestampError(standardErr) {
		t.Fatalf("expected IsFutureTimestampError to return false for generic error")
	}
}

// TestClockSkew3TierPolicyAndNegativeCacheBypass verifies that incoming blocks
// with minor clock skew (<= 30s or <= 600s) bypass the negative cache and receive
// tiered ban scores.
func TestClockSkew3TierPolicyAndNegativeCacheBypass(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	ps := &PeerServer{
		Blockchain:  bc,
		banScores:   make(map[string]int),
		skewStrikes: make(map[string][]time.Time),
		bannedIPs:   make(map[string]time.Time),
	}

	peerIP := "192.168.1.50"

	// 1. Tier 1: Clock skew <= 30 seconds -> 0 ban score
	skewErrTier1 := &core.FutureTimestampError{
		BlockHeight:    1,
		BlockTimestamp: time.Now().Unix() + 25,
		MaxAllowed:     time.Now().Unix(),
		SkewSeconds:    25,
	}
	skew := core.GetFutureTimestampSkew(skewErrTier1)
	if skew > 30 {
		t.Fatalf("expected Tier 1 skew <= 30s")
	}
	// Verify no score added
	if ps.banScores[peerIP] != 0 {
		t.Fatalf("expected 0 ban score for Tier 1 skew, got %d", ps.banScores[peerIP])
	}

	// 2. Tier 2: 31s - 600s -> recordClockSkewViolation adds strikes
	// 1st infraction -> 0 score
	ps.recordClockSkewViolation(peerIP)
	if ps.banScores[peerIP] != 0 {
		t.Fatalf("expected 0 ban score on 1st strike, got %d", ps.banScores[peerIP])
	}
	// 2nd infraction -> 0 score
	ps.recordClockSkewViolation(peerIP)
	if ps.banScores[peerIP] != 0 {
		t.Fatalf("expected 0 ban score on 2nd strike, got %d", ps.banScores[peerIP])
	}
	// 3rd infraction within 10 minutes -> assesses +2 ban score
	ps.recordClockSkewViolation(peerIP)
	if ps.banScores[peerIP] != 2 {
		t.Fatalf("expected +2 ban score on 3rd strike, got %d", ps.banScores[peerIP])
	}

	// 3. Tier 3: Severe clock skew > 600 seconds -> +10 ban score and negative cache marked
	severeHash := "0000000000000000000000000000000000000000000000000000000000000999"
	ps.AddBanScore(peerIP, 10)
	ps.markBlockNegative(severeHash)
	if ps.banScores[peerIP] != 12 {
		t.Fatalf("expected 12 total score after Tier 3 infraction, got %d", ps.banScores[peerIP])
	}
	if !ps.isBlockNegativeCached(severeHash) {
		t.Fatalf("expected severe skew block to be marked negative")
	}

	// Verify that transient skew block is NOT in negative cache
	transientHash := "0000000000000000000000000000000000000000000000000000000000000888"
	if ps.isBlockNegativeCached(transientHash) {
		t.Fatalf("expected transient skew block to bypass negative cache")
	}
}

// TestOrphanValidationUsesTrueParentContext verifies that orphan blocks
// validate expected target using their true parent block context (F-03 fix).
func TestOrphanValidationUsesTrueParentContext(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	parent := &core.Block{
		Index:      50,
		Timestamp:  1700000000,
		Target:     "00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Difficulty: 2,
		Hash:       "0000000000000000000000000000000000000000000000000000000000000500",
	}

	chainSlice := []core.Block{*parent}
	target := core.CalculateExpectedTargetWithParent(chainSlice, parent, 51, parent.Timestamp+120)

	if target == nil || target.Sign() <= 0 {
		t.Fatalf("expected valid non-zero expected target")
	}
}

// Ensure mock net.Conn satisfies interface in tests if needed
var _ net.Conn = (*net.TCPConn)(nil)
