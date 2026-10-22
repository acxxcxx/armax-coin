package core

import (
	"math/big"
	"strings"
	"testing"
)

// TestC26_CoinbaseNonceEqualsHeightDifferentTxids verifies that two coinbase transactions
// for the same miner and reward at different block heights have different TXIDs [C-26].
func TestC26_CoinbaseNonceEqualsHeightDifferentTxids(t *testing.T) {
	minerAddr := newTestAccount(t).Address

	cb1 := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    60 * CoinUnit,
		Fee:       0,
		Nonce:     1, // Height 1
		Signature: "REWARD_SIG",
	}

	cb2 := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    60 * CoinUnit,
		Fee:       0,
		Nonce:     2, // Height 2
		Signature: "REWARD_SIG",
	}

	if cb1.HashHex() == cb2.HashHex() {
		t.Errorf("coinbase transactions at different heights (nonces) must have different txids, but got identical: %s", cb1.HashHex())
	}
}

func newCleanC26Blockchain(t *testing.T) *Blockchain {
	tempDir := t.TempDir()
	dbFile := tempDir + "/test_c26.db"
	SetDBPath(dbFile)
	t.Cleanup(func() {
		SetDBPath("")
	})
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	t.Cleanup(func() {
		targetOverrideHook = oldHook
	})
	bc := &Blockchain{
		Chain:       []Block{GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward)
	return bc
}

// TestC26_CoinbaseNonceMustMatchBlockHeight verifies that a block with coinbase.Nonce != b.Index is rejected.
func TestC26_CoinbaseNonceMustMatchBlockHeight(t *testing.T) {
	bc := newCleanC26Blockchain(t)
	minerAddr := newTestAccount(t, "miner").Address
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()

	// Coinbase with Nonce = 0 for block #1 (should be 1)
	badCb := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    60 * CoinUnit,
		Fee:       0,
		Nonce:     0, // Wrong: must equal block height 1
		Signature: "REWARD_SIG",
	}

	b := *NewBlock([]Transaction{badCb}, prev.Hash, 1, minerAddr, target)
	b.Timestamp = prev.Timestamp + 1
	for {
		b.Hash = b.CalculateHash()
		if HashMeetsTarget(b.Hash, target) {
			break
		}
		b.Nonce++
	}

	err := bc.ValidateBlock(b)
	if err == nil {
		t.Fatalf("expected block with coinbase.Nonce != b.Index to be rejected, but got nil")
	}
	if !strings.Contains(err.Error(), "coinbase nonce") && !strings.Contains(err.Error(), "height") {
		t.Errorf("expected coinbase nonce/height error substring, got: %v", err)
	}
}

// TestC26_BlockMinerValidation verifies that b.Miner must pass ValidateAddress and match coinbase.Recipient.
func TestC26_BlockMinerValidation(t *testing.T) {
	bc := newCleanC26Blockchain(t)
	minerAddr := newTestAccount(t, "miner").Address
	otherAddr := newTestAccount(t, "other").Address
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()

	cb := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    60 * CoinUnit,
		Fee:       0,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}

	// Case 1: b.Miner is an invalid address
	bInvalidMiner := *NewBlock([]Transaction{cb}, prev.Hash, 1, "invalid_miner_addr", target)
	bInvalidMiner.Timestamp = prev.Timestamp + 1
	for {
		bInvalidMiner.Hash = bInvalidMiner.CalculateHash()
		if HashMeetsTarget(bInvalidMiner.Hash, target) {
			break
		}
		bInvalidMiner.Nonce++
	}
	err1 := bc.ValidateBlock(bInvalidMiner)
	if err1 == nil {
		t.Fatalf("expected block with invalid miner address to be rejected, got nil")
	}
	if !strings.Contains(err1.Error(), "miner address") {
		t.Errorf("expected 'miner address' error, got: %v", err1)
	}

	// Case 2: b.Miner differs from coinbase.Recipient
	bMismatchMiner := *NewBlock([]Transaction{cb}, prev.Hash, 1, otherAddr, target)
	bMismatchMiner.Timestamp = prev.Timestamp + 1
	for {
		bMismatchMiner.Hash = bMismatchMiner.CalculateHash()
		if HashMeetsTarget(bMismatchMiner.Hash, target) {
			break
		}
		bMismatchMiner.Nonce++
	}
	err2 := bc.ValidateBlock(bMismatchMiner)
	if err2 == nil {
		t.Fatalf("expected block with miner != coinbase recipient to be rejected, got nil")
	}
	if !strings.Contains(err2.Error(), "miner mismatch") && !strings.Contains(err2.Error(), "coinbase recipient") {
		t.Errorf("expected miner mismatch/coinbase recipient error, got: %v", err2)
	}
}
