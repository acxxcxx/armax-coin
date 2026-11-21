package core

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func TestCodeQL_G2_MaxAllowedMiningThreadsAndClamp(t *testing.T) {
	maxThreads := MaxAllowedMiningThreads()
	if maxThreads < 1 || maxThreads > MaxMiningThreadsCap {
		t.Fatalf("expected MaxAllowedMiningThreads() in [1, %d], got %d", MaxMiningThreadsCap, maxThreads)
	}

	cpuCases := []struct {
		cpus int
		want int
	}{
		{-10, 1},
		{0, 1},
		{1, 4},
		{4, 16},
		{64, MaxMiningThreadsCap},
		{65, MaxMiningThreadsCap},
		{1024, MaxMiningThreadsCap},
		{math.MaxInt, MaxMiningThreadsCap},
	}
	for _, tc := range cpuCases {
		got := maxAllowedMiningThreadsForCPU(tc.cpus)
		if got != tc.want {
			t.Fatalf("maxAllowedMiningThreadsForCPU(%d) = %d, want %d", tc.cpus, got, tc.want)
		}
		if got < 1 || got > MaxMiningThreadsCap {
			t.Fatalf("maxAllowedMiningThreadsForCPU(%d) exceeded bounds [1, %d]: got %d", tc.cpus, MaxMiningThreadsCap, got)
		}
	}

	bc, err := NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "bc.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath: %v", err)
	}
	defer bc.Close()

	acc := newTestAccount(t, "miner")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Must clamp excessive thread counts safely instead of panicking or allocating 1e9 worker slots.
	if _, err := bc.MineBlockContext(ctx, acc.Address, 1_000_000_000); err == nil {
		t.Fatalf("expected cancelled context error from MineBlockContext")
	}
}

func TestCodeQL_G3_ActivationHeightOverflowGuards(t *testing.T) {
	SetASERTActivationHeight(math.MaxInt64)
	t.Cleanup(ResetASERTActivationHeightOverride)

	bc, err := NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "bc.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath: %v", err)
	}
	defer bc.Close()

	target := CalculateArmaxASERTTarget(bc.Chain, 1, bc.Chain[0].Timestamp+120)
	if target == nil || target.Sign() <= 0 {
		t.Fatalf("expected valid fallback target when ASERT height exceeds MaxInt32, got %v", target)
	}

	if drift, ok := GetASERTScheduleDrift(bc.Chain, 0); ok || drift != 0 {
		t.Fatalf("expected inactive drift (0, false) for out-of-range ASERT height, got (%d, %v)", drift, ok)
	}
}

func TestCodeQL_G7_ResolveForkBounds(t *testing.T) {
	bc, err := NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "bc.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath: %v", err)
	}
	defer bc.Close()

	gen := bc.GetLatestBlock()
	if err := bc.ResolveFork(-1, []Block{gen}); err == nil {
		t.Fatalf("expected ResolveFork to reject negative ancestorIndex")
	}
	if err := bc.ResolveFork(10, []Block{gen}); err == nil {
		t.Fatalf("expected ResolveFork to reject out-of-bounds ancestorIndex")
	}
	if err := bc.ResolveFork(0, nil); err == nil {
		t.Fatalf("expected ResolveFork to reject empty candidate blocks")
	}
}
