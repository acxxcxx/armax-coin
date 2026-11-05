//go:build testhooks

package node

import (
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// Rehearsal of the future-timestamp fork: the activation height is lowered so a NAT node mines
// across it and a public node must sync through the boundary, then blocks stamped ahead of the
// limit must be rejected after the fork and accepted before it.
func TestFutureTimestampFork_RehearsalAcrossActivation(t *testing.T) {
	const forkHeight = 8
	core.SetFutureTimestampLimitActivationHeight(forkHeight)
	t.Cleanup(core.ResetFutureTimestampLimitActivationHeightOverride)

	bcPub, bcNAT, _, psNAT, _ := natAheadSetup(t, 3, 0, 15)

	psNAT.SyncBestPeer()
	if !waitTipEqual(bcPub, bcNAT, 15*time.Second) {
		pi, _ := tipHash(bcPub)
		ni, _ := tipHash(bcNAT)
		t.Fatalf("sync across the fork height failed: pub=#%d nat=#%d", pi, ni)
	}
	if h, _ := tipHash(bcPub); h < forkHeight+3 {
		t.Fatalf("test did not cross the fork height: tip #%d", h)
	}

	w, _ := wallet.NewWallet()
	forged := func(bc *core.Blockchain, ahead int64) core.Block {
		bc.Lock.RLock()
		prev := bc.Chain[len(bc.Chain)-1]
		bc.Lock.RUnlock()
		target := core.DifficultyToTarget(1)
		idx := prev.Index + 1
		b := *core.NewBlock([]core.Transaction{{Sender: "SYSTEM", Recipient: w.Address, Amount: core.InitialReward, Nonce: int64(idx), Signature: "REWARD_SIG"}}, prev.Hash, idx, w.Address, target)
		b.Timestamp = time.Now().Unix() + ahead
		if b.Timestamp <= prev.Timestamp {
			b.Timestamp = prev.Timestamp + 1
		}
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, target) {
				return b
			}
			b.Nonce++
		}
	}

	// After the fork: +30 min is rejected, +5 min is accepted.
	hBefore, _ := tipHash(bcPub)
	if bcPub.ValidateAndAddBlock(forged(bcPub, 30*60)) {
		t.Fatal("block stamped +30m must be rejected after the fork height")
	}
	if h, _ := tipHash(bcPub); h != hBefore {
		t.Fatal("rejected block changed the tip")
	}
	if !bcPub.ValidateAndAddBlock(forged(bcPub, 5*60)) {
		t.Fatal("block stamped +5m must be accepted after the fork height")
	}

	// Before the fork the same +30 min block is still valid (2-hour rule).
	core.SetFutureTimestampLimitActivationHeight(1_000_000)
	if !bcPub.ValidateAndAddBlock(forged(bcPub, 30*60)) {
		t.Fatal("block stamped +30m must still be accepted under the legacy rule")
	}
}
