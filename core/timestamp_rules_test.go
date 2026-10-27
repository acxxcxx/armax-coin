package core

import (
	"strings"
	"testing"
)

// buildTimestampChain builds a synthetic chain whose block i has timestamp ts(i).
func buildTimestampChain(n int, ts func(i int) int64) []Block {
	chain := make([]Block, n)
	for i := 0; i < n; i++ {
		chain[i] = Block{Index: i, Timestamp: ts(i)}
	}
	return chain
}

func withTimestampRulesHeight(t *testing.T, h int64) {
	t.Helper()
	unlock := AcquireNetworkTestLock()
	SetTimestampRulesActivationHeight(h)
	t.Cleanup(func() {
		ResetTimestampRulesActivationHeightOverride()
		unlock()
	})
}

func TestTimestampRules_ProfileDefaults(t *testing.T) {
	if TestnetProfile.TimestampRulesActivationHeight != 8000 {
		t.Fatalf("testnet activation height: got %d, want 8000", TestnetProfile.TimestampRulesActivationHeight)
	}
	if MainnetProfile.TimestampRulesActivationHeight != 0 {
		t.Fatalf("mainnet activation height: got %d, want 0", MainnetProfile.TimestampRulesActivationHeight)
	}
	if SoakProfile.TimestampRulesActivationHeight >= 0 || RegtestProfile.TimestampRulesActivationHeight >= 0 {
		t.Fatalf("soak/regtest must keep the legacy rules (negative height)")
	}
}

func TestTimestampRules_ActivationBoundary(t *testing.T) {
	withTimestampRulesHeight(t, 100)

	if IsTimestampRulesActive(99) || !IsTimestampRulesActive(100) || !IsTimestampRulesActive(101) {
		t.Fatalf("activation boundary wrong around height 100")
	}
	if MTPWindowForHeight(99) != LegacyMTPWindow || MTPWindowForHeight(100) != ExtendedMTPWindow {
		t.Fatalf("MTP window not height-gated")
	}
	if IsTimestampRulesActive(0) {
		t.Fatalf("genesis must never use the timestamp rules")
	}
}

func TestTimestampRules_DisabledKeepsLegacyBehavior(t *testing.T) {
	withTimestampRulesHeight(t, -1)
	if IsTimestampRulesActive(1_000_000) {
		t.Fatalf("negative activation height must disable the rules")
	}
}

// A timestamp that regresses below the parent but stays above MTP-11 is valid before activation
// and rejected from activation [C2]. Old blocks must stay valid.
func TestTimestampRules_RegressionAcceptedBeforeRejectedAfter(t *testing.T) {
	withTimestampRulesHeight(t, 100)

	// Blocks 0..99 have increasing timestamps; block 99 is a spike.
	chain := buildTimestampChain(100, func(i int) int64 { return int64(1000 + i*120) })
	chain[99].Timestamp = chain[98].Timestamp + 5000

	// Candidate at index 99 would be validated against chain[:99] with prev = chain[98].
	// Use a legacy candidate (index 99 < 100): timestamp below the parent but above MTP-11.
	legacy := &Block{Index: 99, Timestamp: chain[98].Timestamp - 1}
	mtp11 := CalculateMedianTimePastN(chain, 99, LegacyMTPWindow)
	if legacy.Timestamp <= mtp11 {
		t.Fatalf("test setup: candidate must exceed MTP-11 (%d <= %d)", legacy.Timestamp, mtp11)
	}
	if err := validateBlockTimestampRules(legacy, &chain[98], chain); err != nil {
		t.Fatalf("pre-activation block with regressing timestamp must stay valid: %v", err)
	}

	// Candidate at index 100 (activation height) with the same relation to its parent.
	parent := &chain[99]
	cand := &Block{Index: 100, Timestamp: parent.Timestamp - 1}
	mtp55 := CalculateMedianTimePastN(chain, 100, ExtendedMTPWindow)
	if cand.Timestamp <= mtp55 {
		t.Fatalf("test setup: candidate must exceed MTP-55 (%d <= %d)", cand.Timestamp, mtp55)
	}
	err := validateBlockTimestampRules(cand, parent, chain)
	if err == nil || !strings.Contains(err.Error(), "non-decreasing") {
		t.Fatalf("post-activation regression must be rejected, got: %v", err)
	}

	// Equal to parent is allowed (non-decreasing), provided it exceeds MTP-55.
	cand.Timestamp = parent.Timestamp
	if err := validateBlockTimestampRules(cand, parent, chain); err != nil {
		t.Fatalf("timestamp equal to parent must be accepted: %v", err)
	}
}

// MTP-55 is stricter than MTP-11 when old timestamps are low: a timestamp valid under MTP-11 can
// fall at or below MTP-55 [C1].
func TestTimestampRules_MTP55StricterThanMTP11(t *testing.T) {
	withTimestampRulesHeight(t, 100)

	// 44 old blocks at a low time, then 56 blocks at a high time (so MTP-11 is high, MTP-55 is also high),
	// but arrange: last 11 blocks high, preceding 44 blocks low => MTP-11 high, MTP-55 low.
	chain := buildTimestampChain(100, func(i int) int64 {
		if i >= 89 {
			return 100000 + int64(i)
		}
		return 1000 + int64(i)
	})
	mtp11 := CalculateMedianTimePastN(chain, 100, LegacyMTPWindow)
	mtp55 := CalculateMedianTimePastN(chain, 100, ExtendedMTPWindow)
	if !(mtp11 > mtp55) {
		t.Fatalf("test setup: expected MTP-11 (%d) > MTP-55 (%d)", mtp11, mtp55)
	}

	// Candidate above MTP-55 but below MTP-11 and below parent: only C1/C2 differences matter here;
	// use parent with a low timestamp to isolate the MTP window.
	parent := &Block{Index: 99, Timestamp: mtp55}
	cand := &Block{Index: 100, Timestamp: mtp55 + 1}
	if err := validateBlockTimestampRules(cand, parent, chain); err != nil {
		t.Fatalf("timestamp just above MTP-55 must be accepted post-activation: %v", err)
	}
	cand.Timestamp = mtp55
	if err := validateBlockTimestampRules(cand, parent, chain); err == nil || !strings.Contains(err.Error(), "MTP-55") {
		t.Fatalf("timestamp equal to MTP-55 must be rejected, got: %v", err)
	}
}

func TestTimestampRules_ShortChainWindow(t *testing.T) {
	withTimestampRulesHeight(t, 3)

	chain := buildTimestampChain(4, func(i int) int64 { return int64(1000 + i*10) })
	got := CalculateMedianTimePastN(chain, 4, ExtendedMTPWindow)
	// timestamps 1000,1010,1020,1030 -> index len/2 = 2 -> 1020
	if got != 1020 {
		t.Fatalf("median over a short chain: got %d, want 1020", got)
	}
	if CalculateMedianTimePastN(chain, 0, ExtendedMTPWindow) != 0 || CalculateMedianTimePastN(nil, 5, ExtendedMTPWindow) != 0 {
		t.Fatalf("empty input must yield 0")
	}
	if CalculateMedianTimePastN(chain, 4, 0) != 0 {
		t.Fatalf("non-positive window must yield 0")
	}
}

func TestTimestampRules_MinValidTimestamp(t *testing.T) {
	withTimestampRulesHeight(t, 100)

	chain := buildTimestampChain(100, func(i int) int64 { return int64(1000 + i*120) })

	pre := MinValidTimestamp(chain, &chain[98], 99)
	if want := CalculateMedianTimePastN(chain, 99, LegacyMTPWindow) + 1; pre != want {
		t.Fatalf("pre-activation minimum: got %d, want MTP-11+1 = %d", pre, want)
	}

	chain[99].Timestamp += 10_000
	post := MinValidTimestamp(chain, &chain[99], 100)
	if post != chain[99].Timestamp {
		t.Fatalf("post-activation minimum must be raised to the parent timestamp: got %d, want %d", post, chain[99].Timestamp)
	}

	// The minimum itself must be accepted by the validator.
	cand := &Block{Index: 100, Timestamp: post}
	if err := validateBlockTimestampRules(cand, &chain[99], chain); err != nil {
		t.Fatalf("MinValidTimestamp must be a valid timestamp: %v", err)
	}
	cand.Timestamp = post - 1
	if err := validateBlockTimestampRules(cand, &chain[99], chain); err == nil {
		t.Fatalf("one second below MinValidTimestamp must be rejected")
	}
}

// With C1/C2 active, solve times fed to LWMA from post-activation blocks are never negative.
func TestTimestampRules_PostActivationSolveTimesNonNegative(t *testing.T) {
	withTimestampRulesHeight(t, 10)

	chain := buildTimestampChain(10, func(i int) int64 { return int64(1000 + i*120) })
	r := uint64(12345)
	next := func() int64 { r = r*6364136223846793005 + 1442695040888963407; return int64(r >> 33) }

	for i := 10; i < 400; i++ {
		prev := &chain[len(chain)-1]
		min := MinValidTimestamp(chain, prev, i)
		ts := min + next()%300
		cand := Block{Index: i, Timestamp: ts}
		if err := validateBlockTimestampRules(&cand, prev, chain); err != nil {
			t.Fatalf("block %d: generated timestamp rejected: %v", i, err)
		}
		if ts < prev.Timestamp {
			t.Fatalf("block %d: negative solve time %d", i, ts-prev.Timestamp)
		}
		chain = append(chain, cand)
	}
}

// End-to-end through ValidateBlock: the same regressing-timestamp block is valid one height before
// activation and rejected at the activation height, so old chains keep validating.
func TestTimestampRules_RealBlockBoundary(t *testing.T) {
	bc, _, _, minerAddr := setupTestChainWithFunds(t)

	baseTime := bc.GetLatestBlock().Timestamp
	for i := 1; i <= 12; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		cbTx := Transaction{Sender: "SYSTEM", Recipient: minerAddr, Amount: InitialReward, Nonce: int64(prev.Index + 1), Signature: "REWARD_SIG"}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = baseTime + int64(i*100)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("failed to add block #%d", b.Index)
		}
	}

	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cbTx := Transaction{Sender: "SYSTEM", Recipient: minerAddr, Amount: InitialReward, Nonce: int64(prev.Index + 1), Signature: "REWARD_SIG"}
	cand := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	cand.Timestamp = prev.Timestamp - 10
	for {
		cand.Hash = cand.CalculateHash()
		if HashMeetsTarget(cand.Hash, target) {
			break
		}
		cand.Nonce++
	}

	unlock := AcquireNetworkTestLock()
	defer unlock()
	defer ResetTimestampRulesActivationHeightOverride()

	SetTimestampRulesActivationHeight(int64(cand.Index) + 1)
	if err := bc.ValidateBlock(cand); err != nil {
		t.Fatalf("block at H-1 with regressing timestamp must be accepted: %v", err)
	}

	SetTimestampRulesActivationHeight(int64(cand.Index))
	err := bc.ValidateBlock(cand)
	if err == nil || !strings.Contains(err.Error(), "non-decreasing") {
		t.Fatalf("block at H with regressing timestamp must be rejected, got: %v", err)
	}

	cand.Timestamp = prev.Timestamp
	for cand.Nonce = 0; ; cand.Nonce++ {
		cand.Hash = cand.CalculateHash()
		if HashMeetsTarget(cand.Hash, target) {
			break
		}
	}
	if err := bc.ValidateBlock(cand); err != nil {
		t.Fatalf("block at H with timestamp equal to parent must be accepted: %v", err)
	}
}
func FuzzMedianTimePastN(f *testing.F) {
	f.Add(int64(1000), 5, 55)
	f.Add(int64(0), 0, 11)
	f.Add(int64(-5), 100, 0)
	f.Fuzz(func(t *testing.T, base int64, upTo int, window int) {
		chain := buildTimestampChain(30, func(i int) int64 { return base + int64(i*7%13) })
		got := CalculateMedianTimePastN(chain, upTo, window)
		if upTo <= 0 || window <= 0 {
			if got != 0 {
				t.Fatalf("expected 0 for non-positive input, got %d", got)
			}
		}
	})
}
