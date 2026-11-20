package main

import (
	"runtime"
	"sync/atomic"
	"time"
)

// ============================================================================
// CPU MINING HIGH-PERFORMANCE OPTIMIZATION ENGINE
// ============================================================================
// Solves multi-core throughput degradation, false sharing, cache-line bouncing,
// and cumulative measurement distortion.
// ============================================================================

// WorkerSlot holds a thread-local hash counter aligned to a distinct 64-byte
// CPU cache line. The 56-byte padding ensures that adjacent threads writing
// their hash counts never trigger MESI cache-line invalidation on neighboring cores.
type WorkerSlot struct {
	hashes uint64
	_      [56]byte // 64-byte CPU cache line padding (AMD Zen / Intel Core)
}

// WorkerSlotManager coordinates per-worker hash counters with zero false sharing.
type WorkerSlotManager struct {
	slots []WorkerSlot
}

// NewWorkerSlotManager initializes per-worker cache-aligned slots.
func NewWorkerSlotManager(numWorkers int) *WorkerSlotManager {
	if numWorkers < 1 {
		numWorkers = 1
	}
	return &WorkerSlotManager{
		slots: make([]WorkerSlot, numWorkers),
	}
}

// AddHashes adds local batch count to the worker's dedicated cache line.
func (m *WorkerSlotManager) AddHashes(workerID int, count uint64) {
	if workerID >= 0 && workerID < len(m.slots) {
		atomic.AddUint64(&m.slots[workerID].hashes, count)
	}
}

// TotalHashes aggregates total hashes across all worker slots without locking.
func (m *WorkerSlotManager) TotalHashes() uint64 {
	var sum uint64
	for i := range m.slots {
		sum += atomic.LoadUint64(&m.slots[i].hashes)
	}
	return sum
}

// SlidingEMA computes real-time instantaneous and exponential moving average (EMA)
// hashrate over a sliding window. This completely eliminates the cumulative lifetime
// average distortion where idle or job transition periods drag displayed hashrate down.
type SlidingEMA struct {
	alpha       float64 // Smoothing factor (typically 0.3 for ~10-second response)
	lastTotal   uint64
	lastTime    time.Time
	smoothedHps float64
	initialized bool
}

// NewSlidingEMA creates a new sliding window calculator.
func NewSlidingEMA(alpha float64) *SlidingEMA {
	if alpha <= 0 || alpha > 1.0 {
		alpha = 0.3
	}
	return &SlidingEMA{
		alpha:    alpha,
		lastTime: time.Now(),
	}
}

// Update computes the instantaneous hashrate and updates the smoothed EMA hashrate.
func (e *SlidingEMA) Update(currentTotal uint64, now time.Time) (instantHps, smoothedHps float64) {
	if !e.initialized {
		e.lastTotal = currentTotal
		e.lastTime = now
		e.initialized = true
		return 0, 0
	}

	dt := now.Sub(e.lastTime).Seconds()
	if dt <= 0 {
		return e.smoothedHps, e.smoothedHps
	}

	var delta uint64
	if currentTotal >= e.lastTotal {
		delta = currentTotal - e.lastTotal
	}
	instantHps = float64(delta) / dt

	if e.smoothedHps == 0 && instantHps > 0 {
		e.smoothedHps = instantHps
	} else {
		e.smoothedHps = e.alpha*instantHps + (1.0-e.alpha)*e.smoothedHps
	}

	e.lastTotal = currentTotal
	e.lastTime = now
	return instantHps, e.smoothedHps
}

// Reset re-anchors the baseline to now (e.g. after a new job or pause).
func (e *SlidingEMA) Reset(currentTotal uint64, now time.Time) {
	e.lastTotal = currentTotal
	e.lastTime = now
	e.initialized = true
}

// CurrentEMA returns the latest smoothed EMA hashrate.
func (e *SlidingEMA) CurrentEMA() float64 {
	return e.smoothedHps
}

// GetOptimalCPUWorkerThreads calculates the optimal thread count for compute-bound
// PoW hashing. On machines with SMT/HyperThreading (e.g. 8 cores / 16 threads),
// running on all logical threads saturates shared ALU execution ports and causes
// heavy thermal throttling. Defaulting to physical cores sustains maximum Turbo clock.
func GetOptimalCPUWorkerThreads(explicitThreads int) (optimalThreads int, isPhysicalOnly bool) {
	if explicitThreads > 0 {
		return explicitThreads, false
	}

	numLogical := runtime.NumCPU()
	if numLogical <= 1 {
		return 1, false
	}

	// For SMT/HT enabled systems with >= 4 logical processors,
	// physical cores = logical / 2.
	if numLogical >= 4 {
		return numLogical / 2, true
	}
	return numLogical, false
}
