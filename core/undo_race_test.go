//go:build testhooks

package core

import (
	"sync"
	"testing"
	"time"
)

// TestUndoHistoryConcurrentReadWriteRace verifies HIGH-01:
// Concurrent reads via GetBlockUndoRecord and writes to undoHistory
// do not trigger a data race or "concurrent map read and map write" fatal error.
func TestUndoHistoryConcurrentReadWriteRace(t *testing.T) {
	bc := NewBlockchain(1)
	defer bc.Close()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers calling GetBlockUndoRecord concurrently
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := 0
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = bc.GetBlockUndoRecord(h)
					h = (h + 1) % 100
				}
			}
		}()
	}

	// Writers mutating undoHistory concurrently under undoLock
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				bc.undoLock.Lock()
				if bc.undoHistory == nil {
					bc.undoHistory = make(map[int]*BlockUndoRecord)
				}
				idx := (workerID*1000 + j) % 100
				bc.undoHistory[idx] = &BlockUndoRecord{
					Height: idx,
					PrevAccs: map[string]AccountState{
						"addr1": {Balance: 100, Nonce: int64(j)},
					},
				}
				if j%10 == 0 {
					delete(bc.undoHistory, (idx-10+100)%100)
				}
				bc.undoLock.Unlock()
			}
		}(i)
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

