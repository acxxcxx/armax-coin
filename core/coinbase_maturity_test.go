//go:build testhooks

package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoinbaseMaturityEnforcement(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	bc := NewBlockchain(1)
	defer bc.Close()
	// Test with 5-block maturity for fast execution
	bc.CoinbaseMaturity = 5

	accMiner := newTestAccount(t, "miner")
	minerPriv, minerPub, minerAddr := accMiner.PrivKey, accMiner.PubHex, accMiner.Address
	accRecipient := newTestAccount(t, "recipient")
	recipAddr := accRecipient.Address

	// 1. Mine block #1 to minerAddr
	b1, err := bc.MineBlockContext(context.Background(), minerAddr)
	if err != nil {
		t.Fatalf("Failed to mine block 1: %v", err)
	}
	_ = b1

	// Height is 1, b1 confirmations = 1 (< 5)
	immature := bc.GetImmatureBalance(minerAddr)
	if immature != InitialReward {
		t.Fatalf("Expected immature balance %d, got %d", InitialReward, immature)
	}

	spendable := bc.GetSpendableBalance(minerAddr)
	if spendable != 0 {
		t.Fatalf("Expected spendable balance 0 before maturity, got %d", spendable)
	}

	// 2. Attempt to spend immature coinbase reward
	tx := Transaction{
		Sender:    minerAddr,
		SenderPub: minerPub,
		Recipient: recipAddr,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(minerPriv); err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	accepted, rejectReason := bc.AddTransaction(tx)
	if accepted {
		t.Fatalf("Expected transaction spending immature coinbase to be REJECTED, but was accepted!")
	}
	if !strings.Contains(rejectReason, "Insufficient spendable balance") {
		t.Fatalf("Expected reject reason to mention 'Insufficient spendable balance', got: %s", rejectReason)
	}

	// 3. Append dummy blocks directly with advancing timestamp so confirmations reach 5
	// b1 is at index 1.
	for i := len(bc.Chain); i <= 5; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		bDummy := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, prev.Hash, i, GenesisMiner, target)
		bDummy.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			bDummy.Hash = bDummy.CalculateHash()
			if HashMeetsTarget(bDummy.Hash, target) {
				break
			}
			bDummy.Nonce++
		}
		if !bc.ValidateAndAddBlock(bDummy) {
			t.Fatalf("Failed to add dummy block %d", i)
		}
	}

	// Height is 5. b1 confirmations = 5 - 1 + 1 = 5 (>= 5) -> mature!
	immatureAfter := bc.GetImmatureBalance(minerAddr)
	if immatureAfter != 0 {
		t.Fatalf("Expected immature balance 0 after maturity, got %d", immatureAfter)
	}

	spendableAfter := bc.GetSpendableBalance(minerAddr)
	if spendableAfter < 10*CoinUnit+MinRelayFee*12 {
		t.Fatalf("Expected spendable balance >= required, got %d", spendableAfter)
	}

	// 4. Spending should now SUCCEED!
	acceptedNow, msgNow := bc.AddTransaction(tx)
	if !acceptedNow {
		t.Fatalf("Expected transaction to be accepted after coinbase maturity, got: %s", msgNow)
	}
}

// TestCoinbaseMaturityConsensusRejection proves that a block containing an immature coinbase spend is REJECTED by consensus [L4-10-a]
func TestCoinbaseMaturityConsensusRejection(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	bc := NewBlockchain(1)
	defer bc.Close()
	bc.CoinbaseMaturity = 5
	bc.State.CoinbaseMaturity = 5

	accMiner := newTestAccount(t, "miner")
	minerPriv, minerPub, minerAddr := accMiner.PrivKey, accMiner.PubHex, accMiner.Address
	accRecipient := newTestAccount(t, "recipient")
	recipAddr := accRecipient.Address

	// 1. Mine block 1 with reward credited to minerAddr
	_, err := bc.MineBlockContext(context.Background(), minerAddr)
	if err != nil {
		t.Fatalf("Failed to mine block 1: %v", err)
	}

	// 2. Construct transaction spending immature coinbase reward in block 2
	txImmature := Transaction{
		Sender:    minerAddr,
		SenderPub: minerPub,
		Recipient: recipAddr,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := txImmature.Sign(minerPriv); err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cb2 := Transaction{
		Sender:    "SYSTEM",
		Recipient: GenesisMiner,
		Amount:    InitialReward + txImmature.Fee,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}
	bBad := *NewBlock([]Transaction{cb2, txImmature}, prev.Hash, 2, GenesisMiner, target)
	bBad.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		bBad.Hash = bBad.CalculateHash()
		if HashMeetsTarget(bBad.Hash, target) {
			break
		}
		bBad.Nonce++
	}

	// CONSENSUS VALIDATION: ValidateBlock must reject the block with specific error!
	errBlock := bc.ValidateBlock(bBad)
	if errBlock == nil {
		t.Fatalf("[L4-10-a] Consensus FAILED: Block spending immature coinbase reward was accepted!")
	}
	if !strings.Contains(errBlock.Error(), "insufficient spendable balance") {
		t.Fatalf("[L4-10-a] Expected error to contain 'insufficient spendable balance', got: %v", errBlock)
	}
	if !strings.Contains(errBlock.Error(), "immature coinbase rewards") {
		t.Fatalf("[L4-10-a] Expected error to contain 'immature coinbase rewards', got: %v", errBlock)
	}

	// ValidateAndAddBlock must return false
	if bc.ValidateAndAddBlock(bBad) {
		t.Fatalf("[L4-10-a] ValidateAndAddBlock unexpectedly accepted block with immature spend")
	}

	// 3. Mine valid blocks 2..5 so block 1 reward matures (confirmations reach 5)
	for i := 2; i <= 5; i++ {
		p := bc.GetLatestBlock()
		tgt := bc.CalculateLWMA1()
		bDummy := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, p.Hash, i, GenesisMiner, tgt)
		bDummy.Timestamp = p.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			bDummy.Hash = bDummy.CalculateHash()
			if HashMeetsTarget(bDummy.Hash, tgt) {
				break
			}
			bDummy.Nonce++
		}
		if !bc.ValidateAndAddBlock(bDummy) {
			t.Fatalf("Failed to add dummy block %d", i)
		}
	}

	// 4. In block 6 (height 6, block 1 has 6 - 1 = 5 confirmations >= 5), the spend MUST be accepted!
	prev6 := bc.GetLatestBlock()
	target6 := bc.CalculateLWMA1()
	cb6 := Transaction{
		Sender:    "SYSTEM",
		Recipient: GenesisMiner,
		Amount:    InitialReward + txImmature.Fee,
		Nonce:     6,
		Signature: "REWARD_SIG",
	}
	bValid := *NewBlock([]Transaction{cb6, txImmature}, prev6.Hash, 6, GenesisMiner, target6)
	bValid.Timestamp = prev6.Timestamp + int64(TargetBlockTimeSeconds)
	for {
		bValid.Hash = bValid.CalculateHash()
		if HashMeetsTarget(bValid.Hash, target6) {
			break
		}
		bValid.Nonce++
	}

	if !bc.ValidateAndAddBlock(bValid) {
		t.Fatalf("[L4-10-a] ValidateAndAddBlock rejected mature spend block 6")
	}
}

// TestCoinbaseMaturityReorgRestoresQueue proves that chain reorg properly rolls back and restores immature queues [L4-10-a]
func TestCoinbaseMaturityReorgRestoresQueue(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	bc := NewBlockchain(1)
	defer bc.Close()
	bc.CoinbaseMaturity = 10

	minerA := newTestAccount(t, "minerA").Address
	minerB := newTestAccount(t, "minerB").Address

	// Mine blocks 1..3 on chain A to minerA
	for i := 1; i <= 3; i++ {
		_, err := bc.MineBlockContext(context.Background(), minerA)
		if err != nil {
			t.Fatalf("Failed to mine block %d on chain A: %v", i, err)
		}
	}

	// Verify minerA has 3 immature rewards
	immA := bc.GetImmatureBalance(minerA)
	if immA != 3*InitialReward {
		t.Fatalf("Expected minerA immature %d, got %d", 3*InitialReward, immA)
	}

	// Build competing chain B with 4 blocks to minerB from genesis
	target := bc.CalculateLWMA1()
	prev := bc.Chain[0]
	var chainB []Block
	for i := 1; i <= 4; i++ {
		cb := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerB,
			Amount:    InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cb}, prev.Hash, i, minerB, target)
		b.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		chainB = append(chainB, b)
		prev = b
	}

	// Trigger reorg to chain B
	if err := bc.ResolveFork(0, chainB); err != nil {
		t.Fatalf("Reorganization to chain B failed: %v", err)
	}

	// After reorg, minerA's immature queue must be 0, and minerB must have 4 immature rewards!
	if immAAfter := bc.GetImmatureBalance(minerA); immAAfter != 0 {
		t.Fatalf("Expected minerA immature 0 after reorg, got %d", immAAfter)
	}
	if immBAfter := bc.GetImmatureBalance(minerB); immBAfter != 4*InitialReward {
		t.Fatalf("Expected minerB immature %d after reorg, got %d", 4*InitialReward, immBAfter)
	}
}

// TestCoinbaseMaturitySnapshotRestart verifies that state snapshot and restart preserves immature queue [L4-10-a]
func TestCoinbaseMaturitySnapshotRestart(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")
	SetTargetOverrideForTesting(DifficultyToTarget(1))
	defer SetTargetOverrideForTesting(nil)

	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc := NewBlockchain(1)
	bc.CoinbaseMaturity = 10
	miner := newTestAccount(t, "snapshotMiner").Address

	for i := 1; i <= 3; i++ {
		_, err := bc.MineBlockContext(context.Background(), miner)
		if err != nil {
			t.Fatalf("Failed to mine block %d: %v", i, err)
		}
	}
	immBefore := bc.GetImmatureBalance(miner)
	spendBefore := bc.GetSpendableBalance(miner)

	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
	_ = bc.Close()

	// Reload from DB
	loadedBC, ok := LoadFromFile()
	if !ok || loadedBC == nil {
		t.Fatalf("LoadFromFile failed")
	}
	defer loadedBC.Close()
	loadedBC.CoinbaseMaturity = 10

	immAfter := loadedBC.GetImmatureBalance(miner)
	spendAfter := loadedBC.GetSpendableBalance(miner)

	if immAfter != immBefore {
		t.Fatalf("Immature balance mismatch after restart: before %d, after %d", immBefore, immAfter)
	}
	if spendAfter != spendBefore {
		t.Fatalf("Spendable balance mismatch after restart: before %d, after %d", spendBefore, spendAfter)
	}
}

// TestCoinbaseMaturitySupplyInvariantFuzz fuzzes transactions, blocks, and reorgs checking supply invariant [L4-10-a]
func TestCoinbaseMaturitySupplyInvariantFuzz(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")
	defer func() { _ = SetNetwork("testnet") }()

	bc := NewBlockchain(1)
	defer bc.Close()
	bc.CoinbaseMaturity = 3

	miners := make([]*testAccount, 3)
	for i := 0; i < 3; i++ {
		miners[i] = newTestAccount(t, fmt.Sprintf("fuzzMiner%d", i))
	}

	for blockNum := 1; blockNum <= 15; blockNum++ {
		m := miners[blockNum%len(miners)]
		_, err := bc.MineBlockContext(context.Background(), m.Address)
		if err != nil {
			t.Fatalf("Fuzz mining failed at block %d: %v", blockNum, err)
		}

		// Verify invariant: Sum of all accounts balance == circulating supply
		bc.Lock.RLock()
		var totalStateBal int64
		for _, acc := range bc.State.Accounts {
			totalStateBal += acc.Balance
			spendable := acc.Spendable(len(bc.Chain)-1, bc.GetCoinbaseMaturity())
			imm := acc.ImmatureTotal(len(bc.Chain)-1, bc.GetCoinbaseMaturity())
			if spendable+imm != acc.Balance {
				bc.Lock.RUnlock()
				t.Fatalf("Account balance invariant broken: spendable %d + immature %d != balance %d",
					spendable, imm, acc.Balance)
			}
		}
		circ := bc.cachedSupply
		bc.Lock.RUnlock()

		if totalStateBal != circ {
			t.Fatalf("Circulating supply invariant broken at block %d: state sum %d != circulating %d",
				blockNum, totalStateBal, circ)
		}
	}
}
