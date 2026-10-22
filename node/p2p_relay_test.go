package node

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClaimTxRelay_DeduplicatesPerPeer(t *testing.T) {
	ps := &PeerServer{}

	firstClaim, ok := ps.claimTxRelay("tx-1", "peer-a")
	if !ok {
		t.Fatal("first claim should succeed")
	}

	if _, ok := ps.claimTxRelay("tx-1", "peer-a"); ok {
		t.Fatal("duplicate claim for the same peer should fail")
	}

	if _, ok := ps.claimTxRelay("tx-1", "peer-b"); !ok {
		t.Fatal("claim for a different peer should succeed")
	}

	ps.releaseTxRelay("tx-1", "peer-a", firstClaim)

	if _, ok := ps.claimTxRelay("tx-1", "peer-a"); !ok {
		t.Fatal("claim should succeed after release")
	}
}

func TestClaimTxRelay_ConcurrentClaims(t *testing.T) {
	ps := &PeerServer{}

	const goroutines = 32

	var wg sync.WaitGroup
	var successfulClaims atomic.Int32

	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			if _, ok := ps.claimTxRelay("tx-concurrent", "peer-a"); ok {
				successfulClaims.Add(1)
			}
		}()
	}

	wg.Wait()

	if got := successfulClaims.Load(); got != 1 {
		t.Fatalf("expected exactly one successful claim, got %d", got)
	}
}

func TestClaimTxRelay_ExpiredClaimCanBeRetried(t *testing.T) {
	ps := &PeerServer{
		relayedTxs: map[string]map[string]time.Time{
			"tx-expired": {
				"peer-a": time.Now().Add(-txRelayRetryTTL - time.Second),
			},
		},
	}

	if _, ok := ps.claimTxRelay("tx-expired", "peer-a"); !ok {
		t.Fatal("expired claim should be eligible for retry")
	}
}

func TestReleaseTxRelay_DoesNotDeleteNewerClaim(t *testing.T) {
	ps := &PeerServer{}

	oldClaim, ok := ps.claimTxRelay("tx-race", "peer-a")
	if !ok {
		t.Fatal("initial claim should succeed")
	}

	// Simulate an expired claim replaced by a newer attempt.
	newClaim := oldClaim.Add(txRelayRetryTTL + time.Second)

	ps.relayLock.Lock()
	ps.relayedTxs["tx-race"]["peer-a"] = newClaim
	ps.relayLock.Unlock()

	ps.releaseTxRelay("tx-race", "peer-a", oldClaim)

	ps.relayLock.Lock()
	got := ps.relayedTxs["tx-race"]["peer-a"]
	ps.relayLock.Unlock()

	if !got.Equal(newClaim) {
		t.Fatal("releasing an old claim must not delete the newer claim")
	}
}
