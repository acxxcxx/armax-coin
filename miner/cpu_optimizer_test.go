package main

import (
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestWorkerSlotCachePadding verifies that WorkerSlot occupies at least 64 bytes
// to guarantee that adjacent thread counters never reside on the same CPU cache line.
func TestWorkerSlotCachePadding(t *testing.T) {
	slotSize := unsafe.Sizeof(WorkerSlot{})
	if slotSize < 64 {
		t.Fatalf("WorkerSlot size is %d bytes, expected >= 64 bytes for cache line padding", slotSize)
	}

	mgr := NewWorkerSlotManager(4)
	addr0 := uintptr(unsafe.Pointer(&mgr.slots[0]))
	addr1 := uintptr(unsafe.Pointer(&mgr.slots[1]))
	diff := addr1 - addr0

	if diff < 64 {
		t.Fatalf("Distance between consecutive worker slots is %d bytes, expected >= 64 bytes to prevent false sharing", diff)
	}
}

// TestWorkerSlotManagerConcurrentAccumulation verifies high-throughput lock-free
// atomic counter updates across multiple concurrent worker goroutines with zero race conditions.
func TestWorkerSlotManagerConcurrentAccumulation(t *testing.T) {
	const numWorkers = 8
	const batchesPerWorker = 50000
	const batchSize = 100

	mgr := NewWorkerSlotManager(numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for b := 0; b < batchesPerWorker; b++ {
				mgr.AddHashes(workerID, batchSize)
			}
		}(i)
	}

	wg.Wait()

	expectedTotal := uint64(numWorkers * batchesPerWorker * batchSize)
	actualTotal := mgr.TotalHashes()
	if actualTotal != expectedTotal {
		t.Fatalf("Concurrent accumulation mismatch: got %d, want %d", actualTotal, expectedTotal)
	}
}

// TestSlidingEMANoCumulativeDownwardDrag mathematically proves that SlidingEMA
// recovers immediately to true hardware speed after a pause, unlike cumulative averages
// which suffer from permanent downward drag as session duration increases.
func TestSlidingEMANoCumulativeDownwardDrag(t *testing.T) {
	ema := NewSlidingEMA(0.3)
	baseTime := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	// Step 1: Initialize baseline at t=0
	ema.Update(0, baseTime)

	// Step 2: 10 seconds of steady mining at 1,000,000 hashes/sec
	t1 := baseTime.Add(10 * time.Second)
	inst1, smooth1 := ema.Update(10_000_000, t1)
	if inst1 != 1_000_000.0 {
		t.Fatalf("expected instant H/s 1,000,000, got %.2f", inst1)
	}
	if smooth1 != 1_000_000.0 {
		t.Fatalf("expected initial smoothed H/s 1,000,000, got %.2f", smooth1)
	}

	// Step 3: Simulate a 30-second pause / job wait (hashes remain static at 10,000,000)
	t2 := t1.Add(30 * time.Second)
	instPause, smoothPause := ema.Update(10_000_000, t2)
	if instPause != 0.0 {
		t.Fatalf("expected instant H/s during pause to be 0, got %.2f", instPause)
	}
	if smoothPause >= 1_000_000.0 {
		t.Fatalf("expected smoothed H/s to decay during pause, got %.2f", smoothPause)
	}

	// Step 4: Resume mining at the exact same hardware speed (1,000,000 hashes/sec for 10s)
	t3 := t2.Add(10 * time.Second)
	instResume, _ := ema.Update(20_000_000, t3)
	if instResume != 1_000_000.0 {
		t.Fatalf("expected instant H/s after resumption to be exactly 1,000,000, got %.2f", instResume)
	}

	// Step 5: After a second 10s window at 1,000,000 H/s, EMA rapidly converges back
	t4 := t3.Add(10 * time.Second)
	_, smoothConverged := ema.Update(30_000_000, t4)
	if smoothConverged < 800_000.0 {
		t.Fatalf("expected EMA to rapidly recover to near hardware speed (>800k), got %.2f", smoothConverged)
	}

	// Comparison against cumulative average at t4 (total elapsed = 60s, total hashes = 30M):
	// Cumulative average is 30M / 60s = 500,000 H/s (severely distorted down by 50%!)
	cumulativeHps := 30_000_000.0 / 60.0
	if cumulativeHps != 500_000.0 {
		t.Fatalf("expected cumulative H/s to be 500k, got %.2f", cumulativeHps)
	}
	if smoothConverged <= cumulativeHps {
		t.Fatalf("Sliding EMA (%.2f) must be higher than distorted cumulative average (%.2f)", smoothConverged, cumulativeHps)
	}
}

// TestGetOptimalCPUWorkerThreads verifies thread allocation logic.
func TestGetOptimalCPUWorkerThreads(t *testing.T) {
	// Explicit threads must always be honored
	opt, isPhysical := GetOptimalCPUWorkerThreads(4)
	if opt != 4 || isPhysical {
		t.Fatalf("explicit 4 threads failed: got %d (physical=%v)", opt, isPhysical)
	}

	// Default thread count (explicit <= 0)
	defaultOpt, _ := GetOptimalCPUWorkerThreads(0)
	if defaultOpt < 1 {
		t.Fatalf("expected at least 1 optimal thread, got %d", defaultOpt)
	}
}
