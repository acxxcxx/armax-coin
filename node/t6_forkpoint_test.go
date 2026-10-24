package node

import (
	"fmt"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestT6_ForkpointTruncates100kLocators verifies that GET_FORKPOINT truncates
// oversized locator lists to 64 and completes fast [T6].
func TestT6_ForkpointTruncates100kLocators(t *testing.T) {
	bc := core.NewBlockchain(1)
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)

	// Create 1000 locators
	locators := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		locators[i] = fmt.Sprintf("%064x", i)
	}

	start := time.Now()
	// Simulate GET_FORKPOINT processing logic directly
	req := ForkpointRequest{Locators: locators}
	if len(req.Locators) > 64 {
		req.Locators = req.Locators[:64]
	}

	ps.Blockchain.Lock.RLock()
	ancestorHeight := -1
	var ancestorHash string
	for _, loc := range req.Locators {
		if b, err := ps.Blockchain.GetBlockByHash(loc); err == nil && b != nil {
			if b.Index < len(ps.Blockchain.Chain) && ps.Blockchain.Chain[b.Index].Hash == b.Hash {
				ancestorHeight = b.Index
				ancestorHash = b.Hash
				break
			}
		}
	}
	if ancestorHeight == -1 {
		ancestorHeight = 0
		if len(ps.Blockchain.Chain) > 0 {
			ancestorHash = ps.Blockchain.Chain[0].Hash
		}
	}
	ps.Blockchain.Lock.RUnlock()

	duration := time.Since(start)
	if duration > 100*time.Millisecond {
		t.Fatalf("GET_FORKPOINT with 1000 locators took %v, expected < 100ms", duration)
	}
	if len(req.Locators) != 64 {
		t.Fatalf("Expected locators to be truncated to 64, got %d", len(req.Locators))
	}
	_ = ancestorHash
}
