package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withFutureLimitHeight(t *testing.T, h int64) {
	t.Helper()
	unlock := AcquireNetworkTestLock()
	SetFutureTimestampLimitActivationHeight(h)
	t.Cleanup(func() {
		ResetFutureTimestampLimitActivationHeightOverride()
		unlock()
	})
}

func TestFutureTimestampLimit_ProfileDefaults(t *testing.T) {
	if TestnetProfile.FutureTimestampLimitActivationHeight != 20000 {
		t.Fatalf("testnet fork height: got %d, want 20000", TestnetProfile.FutureTimestampLimitActivationHeight)
	}
	for _, p := range []NetworkProfile{MainnetProfile, SoakProfile, RegtestProfile} {
		if p.FutureTimestampLimitActivationHeight >= 0 {
			t.Fatalf("%s must keep the 2-hour rule (negative height), got %d", p.Name, p.FutureTimestampLimitActivationHeight)
		}
	}
	if FutureTimestampLimitSeconds != 600 {
		t.Fatalf("limit must be 10 minutes, got %d s", FutureTimestampLimitSeconds)
	}
}

func TestFutureTimestampLimit_HeightGated(t *testing.T) {
	withFutureLimitHeight(t, 20000)
	if got := MaxFutureTimestampSeconds(19999); got != LegacyFutureTimestampLimitSeconds {
		t.Fatalf("below fork: got %d, want 2h", got)
	}
	if got := MaxFutureTimestampSeconds(20000); got != FutureTimestampLimitSeconds {
		t.Fatalf("at fork: got %d, want 10m", got)
	}
	if got := MaxFutureTimestampSeconds(25000); got != FutureTimestampLimitSeconds {
		t.Fatalf("above fork: got %d, want 10m", got)
	}
}

func TestFutureTimestampLimit_DisabledKeepsLegacy(t *testing.T) {
	withFutureLimitHeight(t, -1)
	if got := MaxFutureTimestampSeconds(1_000_000); got != LegacyFutureTimestampLimitSeconds {
		t.Fatalf("disabled: got %d, want 2h", got)
	}
}

func TestFutureTimestampLimit_Boundaries(t *testing.T) {
	withFutureLimitHeight(t, 100)
	// A few seconds of slack covers the clock advancing between building the block and validating it.
	const slack = 5
	now := time.Now().Unix()

	ok := &Block{Index: 100, Timestamp: now + FutureTimestampLimitSeconds - slack}
	if err := validateFutureTimestamp(ok); err != nil {
		t.Fatalf("block just inside 10m must pass: %v", err)
	}
	bad := &Block{Index: 100, Timestamp: now + FutureTimestampLimitSeconds + slack}
	if err := validateFutureTimestamp(bad); err == nil || !strings.Contains(err.Error(), "too far in future") {
		t.Fatalf("block just past 10m must be rejected at the fork height, got %v", err)
	}

	// The same 30-minute-ahead timestamp is accepted below the fork and rejected from it.
	ahead := now + 30*60
	if err := validateFutureTimestamp(&Block{Index: 99, Timestamp: ahead}); err != nil {
		t.Fatalf("below fork, +30m must still pass under the 2h rule: %v", err)
	}
	if err := validateFutureTimestamp(&Block{Index: 100, Timestamp: ahead}); err == nil {
		t.Fatal("at fork, +30m must be rejected")
	}

	// The legacy 2-hour ceiling is unchanged below the fork.
	if err := validateFutureTimestamp(&Block{Index: 99, Timestamp: now + LegacyFutureTimestampLimitSeconds + slack}); err == nil {
		t.Fatal("below fork, +2h must still be rejected")
	}
}

// forkAt30MinAhead builds a 2-block candidate whose blocks are 30 minutes ahead of the wall clock.
func resolveForkWithBlocksAhead(t *testing.T, forkHeight int64) error {
	t.Helper()
	setupC24EasyTarget(t)
	withFutureLimitHeight(t, forkHeight)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := NewBlockchain(1)
	bc.StoragePath = filepath.Join(tempDir, "blockchain.db")
	defer bc.Close()

	miner := GenesisMiner
	if _, err := bc.MineBlockWithErr(miner); err != nil {
		t.Fatalf("MineBlock failed: %v", err)
	}
	target := bc.CalculateLWMA1()
	prev := bc.Chain[0].Hash
	ts := time.Now().Unix() + 30*60
	var cand []Block
	for i := 1; i <= 2; i++ {
		b := *NewBlock([]Transaction{{Sender: "SYSTEM", Recipient: miner, Amount: InitialReward, Nonce: int64(i), Signature: "REWARD_SIG"}}, prev, i, miner, target)
		b.Timestamp = ts + int64(i)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		prev = b.Hash
		cand = append(cand, b)
	}
	return bc.ResolveFork(0, cand)
}

func TestFutureTimestampLimit_ResolveForkIntegration(t *testing.T) {
	t.Run("rejected_from_fork_height", func(t *testing.T) {
		err := resolveForkWithBlocksAhead(t, 1)
		if err == nil || !strings.Contains(err.Error(), "future") {
			t.Fatalf("expected future-timestamp rejection at fork height, got %v", err)
		}
	})
	t.Run("accepted_when_limit_disabled", func(t *testing.T) {
		if err := resolveForkWithBlocksAhead(t, -1); err != nil {
			t.Fatalf("+30m must be accepted under the legacy 2h rule: %v", err)
		}
	})
}
