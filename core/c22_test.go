package core

import (
	"math/rand"
	"testing"
	"time"
)

// TestC22_BlockWithTimestampLessThanParentAcceptedIfGreaterThanMTP verifies that
// a block with timestamp <= parent is accepted as long as it is > MedianTimePast (MTP-11) [C-22].
func TestC22_BlockWithTimestampLessThanParentAcceptedIfGreaterThanMTP(t *testing.T) {
	bc, _, _, minerAddr := setupTestChainWithFunds(t)

	// Mine 12 blocks with increasing timestamps (e.g. 100 seconds apart)
	baseTime := bc.GetLatestBlock().Timestamp
	for i := 1; i <= 12; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
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
			t.Fatalf("Failed to add block #%d", i+1)
		}
	}

	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	mtp := CalculateMedianTimePast(bc.Chain, prev.Index+1)

	// Verify that parent.Timestamp is strictly greater than mtp + 10
	if prev.Timestamp <= mtp+10 {
		t.Fatalf("expected parent timestamp > mtp + 10, got prev=%d, mtp=%d", prev.Timestamp, mtp)
	}

	// Create candidate block with timestamp LESS than parent, but GREATER than MTP
	// e.g. timestamp = prev.Timestamp - 10 (which is still > mtp)
	childTimestamp := prev.Timestamp - 10
	if childTimestamp <= mtp {
		t.Fatalf("child timestamp %d must be > mtp %d", childTimestamp, mtp)
	}

	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	childBlock := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	childBlock.Timestamp = childTimestamp
	for {
		childBlock.Hash = childBlock.CalculateHash()
		if HashMeetsTarget(childBlock.Hash, target) {
			break
		}
		childBlock.Nonce++
	}

	// Under C-22, this block MUST be valid because it satisfies child.Timestamp > MTP
	err := bc.ValidateBlock(childBlock)
	if err != nil {
		t.Fatalf("[C-22] FAILED: Block with timestamp <= parent (but > MTP) was rejected: %v", err)
	}
}

// TestC22_RandomTimestampSimulation20Blocks verifies that 20 consecutive blocks with
// random timestamps (valid according to MTP) compute LWMA target properly without overflow or crash [C-22].
func TestC22_RandomTimestampSimulation20Blocks(t *testing.T) {
	bc, _, _, minerAddr := setupTestChainWithFunds(t)

	r := rand.New(rand.NewSource(12345))

	for i := 1; i <= 20; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		mtp := CalculateMedianTimePast(bc.Chain, prev.Index+1)

		// Choose a random timestamp that is strictly > mtp and <= now + 1h
		// Allow it to be randomly greater or smaller than prev.Timestamp
		now := time.Now().Unix()
		delta := int64(r.Intn(100) + 1) // +1 to +100 above MTP
		ts := mtp + delta
		if ts > now+3600 {
			ts = now + 3600
		}

		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = ts
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}

		err := bc.ValidateBlock(b)
		if err != nil {
			t.Fatalf("[C-22] Simulation failed at block %d (ts=%d, prev=%d, mtp=%d): %v",
				i, ts, prev.Timestamp, mtp, err)
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("[C-22] Simulation failed to add block %d", i)
		}
	}
}
