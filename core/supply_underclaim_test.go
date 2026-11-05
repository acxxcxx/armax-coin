//go:build testhooks

package core

import (
	"math/big"
	"path/filepath"
	"testing"
)

// TestBlockMintedSubsidy_Unit verifies HIGH-04 unit logic for BlockMintedSubsidy:
// 1. Recycled fees must be deducted from coinbase amount so they are not double-counted.
// 2. Underclaimed rewards must only record what was actually minted into circulation.
// 3. Negative or zero net minted results are floored at 0.
// 4. Overclaim attempts are capped at maxAllowedReward.
func TestBlockMintedSubsidy_Unit(t *testing.T) {
	// 1. Nil or empty block
	if m := BlockMintedSubsidy(nil, 60*CoinUnit); m != 0 {
		t.Fatalf("expected 0 for nil block, got %d", m)
	}
	if m := BlockMintedSubsidy(&Block{}, 60*CoinUnit); m != 0 {
		t.Fatalf("expected 0 for block with no transactions, got %d", m)
	}

	// 2. Coinbase only (0 fees)
	// Full claim
	bFull := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 60 * CoinUnit},
		},
	}
	if m := BlockMintedSubsidy(bFull, 60*CoinUnit); m != 60*CoinUnit {
		t.Fatalf("expected 60 CoinUnit, got %d", m)
	}

	// Underclaim
	bUnder := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 40 * CoinUnit},
		},
	}
	if m := BlockMintedSubsidy(bUnder, 60*CoinUnit); m != 40*CoinUnit {
		t.Fatalf("expected 40 CoinUnit, got %d", m)
	}

	// Zero claim
	bZero := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 0},
		},
	}
	if m := BlockMintedSubsidy(bZero, 60*CoinUnit); m != 0 {
		t.Fatalf("expected 0 CoinUnit, got %d", m)
	}

	// Overclaim capped at max allowed
	bOver := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 999 * CoinUnit},
		},
	}
	if m := BlockMintedSubsidy(bOver, 60*CoinUnit); m != 60*CoinUnit {
		t.Fatalf("expected capped at 60 CoinUnit, got %d", m)
	}

	// 3. Transactions with recycled fees
	// Tx with fee = 5 CoinUnit
	feeTx := Transaction{
		Sender:    "sender",
		Recipient: "recip",
		Amount:    10 * CoinUnit,
		Fee:       5 * CoinUnit,
	}

	// Full claim: miner takes maxReward (60) + fee (5) = 65 total coinbase
	bWithFeesFull := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 65 * CoinUnit},
			feeTx,
		},
	}
	// Net minted must be 65 - 5 = 60 (recycled fee must not be counted as newly minted supply)
	if m := BlockMintedSubsidy(bWithFeesFull, 60*CoinUnit); m != 60*CoinUnit {
		t.Fatalf("expected 60 CoinUnit minted, got %d", m)
	}

	// Underclaim with fees: miner claims 45 total (5 fee + 40 minted subsidy)
	bWithFeesUnder := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 45 * CoinUnit},
			feeTx,
		},
	}
	if m := BlockMintedSubsidy(bWithFeesUnder, 60*CoinUnit); m != 40*CoinUnit {
		t.Fatalf("expected 40 CoinUnit minted, got %d", m)
	}

	// Miner claims only the fee (coinbase amount == fee): 0 new coins minted
	bOnlyFees := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 5 * CoinUnit},
			feeTx,
		},
	}
	if m := BlockMintedSubsidy(bOnlyFees, 60*CoinUnit); m != 0 {
		t.Fatalf("expected 0 CoinUnit minted when claiming only fees, got %d", m)
	}

	// Miner claims less than total fees: floored at 0
	bLessThanFees := &Block{
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: "addr1", Amount: 3 * CoinUnit},
			feeTx,
		},
	}
	if m := BlockMintedSubsidy(bLessThanFees, 60*CoinUnit); m != 0 {
		t.Fatalf("expected 0 CoinUnit minted when coinbase < fees, got %d", m)
	}
}

// TestCirculatingSupplyUnderclaimParityAcrossDiskReload verifies HIGH-04:
// When a block underclaims its coinbase subsidy, live node cachedSupply,
// disk-reloaded supply from loadBlockchainFromPath, and RebuildState() all
// produce the exact same circulating supply without diverging to theoretical emission.
func TestCirculatingSupplyUnderclaimParityAcrossDiskReload(t *testing.T) {
	LockNetworkForTesting(t)

	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	defer func() {
		targetOverrideHook = oldHook
	}()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc := &Blockchain{
		Chain:       []Block{GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}
	if err := bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward, bc.GetCoinbaseMaturity()); err != nil {
		t.Fatalf("failed to apply genesis block: %v", err)
	}
	bc.cachedSupply = InitialReward
	bc.cachedWork = CalculateChainwork(bc.Chain)

	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("failed to save genesis: %v", err)
	}

	minerAddr := newTestAccount(t).Address
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()

	// Mine Block #1 with intentional underclaim:
	// Allowed max subsidy is InitialReward (60 * CoinUnit).
	// Miner underclaims and mints only 35 * CoinUnit.
	underclaimedReward := 35 * CoinUnit
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    underclaimedReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	b1 := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	b1.Timestamp = prev.Timestamp + 1
	for {
		b1.Hash = b1.CalculateHash()
		if HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	// Add Block #1 to live chain
	if !bc.ValidateAndAddBlock(b1) {
		t.Fatalf("ValidateAndAddBlock failed on underclaimed block")
	}

	expectedLiveSupply := InitialReward + underclaimedReward
	if liveSupply := bc.GetCirculatingSupply(); liveSupply != expectedLiveSupply {
		t.Fatalf("live chain supply mismatch: got %d, want %d", liveSupply, expectedLiveSupply)
	}

	// Persist to disk
	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("failed to persist chain with underclaimed block: %v", err)
	}

	// Close active db handle before reload to release file lock on Windows
	_ = bc.Close()

	// Reload from disk via loadBlockchainFromPath
	bcLoaded, found, err := loadBlockchainFromPath(dbPath)
	if err != nil {
		t.Fatalf("failed to reload blockchain from disk: %v", err)
	}
	if !found {
		t.Fatalf("blockchain not found at %s", dbPath)
	}
	defer bcLoaded.Close()

	// Verify loaded supply matches live supply exactly
	loadedSupply := bcLoaded.GetCirculatingSupply()
	if loadedSupply != expectedLiveSupply {
		t.Fatalf("HIGH-04 failure: disk-reloaded supply diverged from live supply! got %d, want %d",
			loadedSupply, expectedLiveSupply)
	}

	// Verify RebuildState() also matches exactly
	if err := bcLoaded.RebuildState(); err != nil {
		t.Fatalf("RebuildState failed: %v", err)
	}
	rebuiltSupply := bcLoaded.GetCirculatingSupply()
	if rebuiltSupply != expectedLiveSupply {
		t.Fatalf("HIGH-04 failure: RebuildState() supply diverged from live supply! got %d, want %d",
			rebuiltSupply, expectedLiveSupply)
	}

	// Verify theoretical emission would have been wrong
	theoreticalSupply := InitialReward * 2
	if loadedSupply == theoreticalSupply {
		t.Fatalf("HIGH-04 failure: loaded supply equaled theoretical supply instead of actual minted subsidy!")
	}
}

// TestResolveForkUnderclaimSupplyParity verifies that when resolving a fork
// where candidate blocks underclaim subsidy, bc.cachedSupply and RebuildState
// accurately track the minted subsidy.
func TestResolveForkUnderclaimSupplyParity(t *testing.T) {
	LockNetworkForTesting(t)

	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	defer func() {
		targetOverrideHook = oldHook
	}()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc := NewBlockchain(1)
	bc.StoragePath = dbPath
	defer bc.Close()

	miner := GenesisMiner
	_, err := bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 1 failed: %v", err)
	}
	_, err = bc.MineBlockWithErr(miner)
	if err != nil {
		t.Fatalf("MineBlock 2 failed: %v", err)
	}

	ancestorIndex := 1 // fork from block 1
	ancestor := bc.Chain[ancestorIndex]

	// Mine 2 blocks on candidate fork (blocks 2 and 3) to create higher chainwork
	// Candidate block 2 underclaims subsidy: 30 * CoinUnit instead of 60 * CoinUnit
	// Candidate block 3 claims normal subsidy: 60 * CoinUnit
	var candidateBlocks []Block
	prev := ancestor
	for i := 2; i <= 3; i++ {
		target := bc.CalculateLWMA1()
		claimAmount := InitialReward
		if i == 2 {
			claimAmount = 30 * CoinUnit
		}
		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    claimAmount,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, miner, target)
		b.Timestamp = prev.Timestamp + 1
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		candidateBlocks = append(candidateBlocks, b)
		prev = b
	}

	if err := bc.ResolveFork(ancestorIndex, candidateBlocks); err != nil {
		t.Fatalf("ResolveFork failed: %v", err)
	}

	// Expected supply:
	// Genesis: InitialReward (60)
	// Block 1: InitialReward (60)
	// Cand #2: 30 * CoinUnit
	// Cand #3: InitialReward (60)
	expectedForkSupply := InitialReward*3 + 30*CoinUnit
	if s := bc.GetCirculatingSupply(); s != expectedForkSupply {
		t.Fatalf("HIGH-04 failure after ResolveFork: got %d, want %d", s, expectedForkSupply)
	}

	// Verify RebuildState matches
	if err := bc.RebuildState(); err != nil {
		t.Fatalf("RebuildState failed: %v", err)
	}
	if s := bc.GetCirculatingSupply(); s != expectedForkSupply {
		t.Fatalf("HIGH-04 failure after RebuildState: got %d, want %d", s, expectedForkSupply)
	}
}
