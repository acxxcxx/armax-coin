//go:build testhooks

package core

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func TestGenesisBlockIntegrity(t *testing.T) {
	SetNetworkForTesting(true)
	_ = SetNetwork("mainnet")
	defer func() { _ = SetNetwork("mainnet") }()

	// 1. Verify Mainnet Genesis Block calculated directly from content [P2-0f, P2-6]
	genMain := GetMainnetGenesisBlock()
	calculatedMerkleMain := CalculateMerkleRoot(genMain.Transactions)
	if calculatedMerkleMain != GenesisMerkleRoot {
		t.Fatalf("Mainnet Merkle root computed from content mismatch: expected %s, got %s", GenesisMerkleRoot, calculatedMerkleMain)
	}
	calculatedHashMain := genMain.CalculateHash()
	if calculatedHashMain != GenesisHash {
		t.Fatalf("Mainnet Hash computed from content mismatch: expected %s, got %s", GenesisHash, calculatedHashMain)
	}
	if err := ValidateGenesisBlock(&genMain); err != nil {
		t.Fatalf("ValidateGenesisBlock failed on GetMainnetGenesisBlock: %v", err)
	}

	// 2. Verify Testnet Genesis Block calculated directly from content [P2-6, L1-2]
	_ = SetNetwork("testnet")
	genTest := GetTestnetGenesisBlock()
	calculatedMerkleTest := CalculateMerkleRoot(genTest.Transactions)
	if calculatedMerkleTest != TestnetGenesisMerkleRoot {
		_ = SetNetwork("mainnet")
		t.Fatalf("Testnet Merkle root computed from content mismatch: expected %s, got %s", TestnetGenesisMerkleRoot, calculatedMerkleTest)
	}
	calculatedHashTest := genTest.CalculateHash()
	if calculatedHashTest != TestnetGenesisHash {
		_ = SetNetwork("mainnet")
		t.Fatalf("Testnet Hash computed from content mismatch: expected %s, got %s", TestnetGenesisHash, calculatedHashTest)
	}
	_ = SetNetwork("mainnet")

	// 3. Mainnet and Testnet Genesis hashes MUST be distinct
	if calculatedHashMain == calculatedHashTest {
		t.Fatalf("Mainnet and Testnet Genesis hashes MUST be distinct, both got: %s", calculatedHashMain)
	}

	if len(genMain.Transactions) != 1 {
		t.Fatalf("Genesis must have exactly 1 coinbase transaction")
	}
	if genMain.Transactions[0].Amount != InitialReward {
		t.Fatalf("Genesis reward must be %d, got %d", InitialReward, genMain.Transactions[0].Amount)
	}
	if !ValidateAddress(genMain.Miner) {
		t.Fatalf("GenesisMiner address %s failed canonical ValidateAddress!", genMain.Miner)
	}
	if genMain.Transactions[0].Signature != "REWARD_SIG" {
		t.Fatalf("Genesis coinbase signature must be REWARD_SIG, got %s", genMain.Transactions[0].Signature)
	}
}

func TestInfiniteMintingPrevention(t *testing.T) {
	bc := &Blockchain{
		Chain:       []Block{GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward)

	minerAddr := newTestAccount(t, "miner").Address
	attackerAddr := newTestAccount(t, "attacker").Address

	// Attempt 1: Miner includes a SECOND SYSTEM transaction to mint coins out of thin air
	coinbaseTx := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     0,
		Signature: "REWARD_SIG",
	}

	fakeMintTx := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: attackerAddr,
		Amount:    1000 * CoinUnit,
		Fee:       0,
		Nonce:     0,
		Signature: "REWARD_SIG",
	}

	target := bc.CalculateLWMA1()
	prev := bc.GetLatestBlock()
	maliciousBlock := *NewBlock([]Transaction{coinbaseTx, fakeMintTx}, prev.Hash, prev.Index+1, minerAddr, target)

	// Mine PoW
	for {
		maliciousBlock.Hash = maliciousBlock.CalculateHash()
		if HashMeetsTarget(maliciousBlock.Hash, target) {
			break
		}
		maliciousBlock.Nonce++
	}

	// Must be REJECTED by consensus with specific error substring [T-07a]
	err := bc.ValidateBlock(maliciousBlock)
	if err == nil {
		t.Fatalf("Consensus FAILED: Infinite minting block with duplicate SYSTEM transaction was accepted!")
	}
	if !strings.Contains(err.Error(), "illegal SYSTEM sender") {
		t.Fatalf("Expected 'illegal SYSTEM sender' rejection substring, got: %v", err)
	}
}

func TestStateBalanceAndNonceEnforcement(t *testing.T) {
	bc := &Blockchain{
		Chain:            []Block{GetGenesisBlock()},
		Difficulty:       1,
		Mempool:          []Transaction{},
		BlockReward:      InitialReward,
		State:            NewStateManager(),
		CoinbaseMaturity: 1,
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward, 1)

	acc := newTestAccount(t, "miner")
	minerPriv, minerPub, minerAddr := acc.PrivKey, acc.PubHex, acc.Address
	recipientAddr := newTestAccount(t, "recipient").Address

	// 1. Mine a block to give minerAddr some funds
	target := bc.CalculateLWMA1()
	prev := bc.GetLatestBlock()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	b1 := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	for {
		b1.Hash = b1.CalculateHash()
		if HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}
	if !bc.ValidateAndAddBlock(b1) {
		t.Fatalf("Failed to add valid mined block 1")
	}

	if bc.GetBalance(minerAddr) != InitialReward {
		t.Fatalf("Expected balance %d for miner, got %d", InitialReward, bc.GetBalance(minerAddr))
	}

	// 2. Test Spending More Than Balance (Overspending)
	overspendTx := Transaction{
		Sender:    minerAddr,
		SenderPub: minerPub,
		Recipient: recipientAddr,
		Amount:    InitialReward + 1000,
		Fee:       0,
		Nonce:     1,
	}
	_ = overspendTx.Sign(minerPriv)

	prev = bc.GetLatestBlock()
	target = bc.CalculateLWMA1()
	cb2 := Transaction{Sender: "SYSTEM", Recipient: minerAddr, Amount: InitialReward, Nonce: int64(prev.Index + 1), Signature: "REWARD_SIG"}
	bOverspend := *NewBlock([]Transaction{cb2, overspendTx}, prev.Hash, prev.Index+1, minerAddr, target)
	bOverspend.Timestamp = prev.Timestamp + 1
	for {
		bOverspend.Hash = bOverspend.CalculateHash()
		if HashMeetsTarget(bOverspend.Hash, target) {
			break
		}
		bOverspend.Nonce++
	}

	errOverspend := bc.ValidateBlock(bOverspend)
	if errOverspend == nil {
		t.Fatalf("Consensus FAILED: Overspending block was accepted!")
	}
	if !strings.Contains(errOverspend.Error(), "insufficient balance") {
		t.Fatalf("Expected 'insufficient balance' rejection substring, got: %v", errOverspend)
	}

	// 3. Test Invalid Nonce Sequence (Nonce Replay / Skip)
	badNonceTx := Transaction{
		Sender:    minerAddr,
		SenderPub: minerPub,
		Recipient: recipientAddr,
		Amount:    10 * CoinUnit,
		Fee:       0,
		Nonce:     5, // Expected 1, but sent 5
	}
	_ = badNonceTx.Sign(minerPriv)

	bBadNonce := *NewBlock([]Transaction{cb2, badNonceTx}, prev.Hash, prev.Index+1, minerAddr, target)
	bBadNonce.Timestamp = prev.Timestamp + 1
	for {
		bBadNonce.Hash = bBadNonce.CalculateHash()
		if HashMeetsTarget(bBadNonce.Hash, target) {
			break
		}
		bBadNonce.Nonce++
	}

	errBadNonce := bc.ValidateBlock(bBadNonce)
	if errBadNonce == nil {
		t.Fatalf("Consensus FAILED: Out-of-sequence nonce block was accepted!")
	}
	if !strings.Contains(errBadNonce.Error(), "nonce mismatch") {
		t.Fatalf("Expected 'nonce mismatch' rejection substring, got: %v", errBadNonce)
	}

	// 4. Test Valid Transaction
	validTx := Transaction{
		Sender:    minerAddr,
		SenderPub: minerPub,
		Recipient: recipientAddr,
		Amount:    10 * CoinUnit,
		Fee:       1 * CoinUnit,
		Nonce:     1, // Correct sequential nonce
	}
	_ = validTx.Sign(minerPriv)

	cbValid := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward + validTx.Fee,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	prev = bc.GetLatestBlock()
	target = bc.CalculateLWMA1()
	bValid := *NewBlock([]Transaction{cbValid, validTx}, prev.Hash, prev.Index+1, minerAddr, target)
	bValid.Timestamp = prev.Timestamp + 1
	for {
		bValid.Hash = bValid.CalculateHash()
		if HashMeetsTarget(bValid.Hash, target) {
			break
		}
		bValid.Nonce++
	}

	if !bc.ValidateAndAddBlock(bValid) {
		t.Fatalf("Valid block with valid transaction was rejected!")
	}

	// Verify updated account state
	expectedMinerBal := InitialReward - (validTx.Amount + validTx.Fee) + cbValid.Amount
	if bc.GetBalance(minerAddr) != expectedMinerBal {
		t.Fatalf("Miner balance incorrect: expected %d, got %d", expectedMinerBal, bc.GetBalance(minerAddr))
	}
	if bc.GetBalance(recipientAddr) != validTx.Amount {
		t.Fatalf("Recipient balance incorrect: expected %d, got %d", validTx.Amount, bc.GetBalance(recipientAddr))
	}
	if bc.GetAccountNonce(minerAddr) != 1 {
		t.Fatalf("Miner nonce incorrect: expected 1, got %d", bc.GetAccountNonce(minerAddr))
	}
}

func mineTestBlock(t *testing.T, b *Block, target *big.Int) {
	t.Helper()

	const maxAttempts = uint64(5_000_000)

	for attempt := uint64(0); attempt < maxAttempts; attempt++ {
		b.Hash = b.CalculateHash()

		if HashMeetsTarget(b.Hash, target) {
			return
		}

		b.Nonce++
	}

	t.Fatalf(
		"PoW mining exceeded %d attempts: block=%d target=%s difficulty=%d",
		maxAttempts,
		b.Index,
		target.Text(16),
		b.Difficulty,
	)
}

func TestConsensusEnforcesLWMA(t *testing.T) {
	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest): %v", err)
	}
	t.Cleanup(func() {
		if err := SetNetwork("testnet"); err != nil {
			t.Errorf("restore testnet: %v", err)
		}
	})

	genesis := GetGenesisBlock()
	bc := &Blockchain{
		Chain:       []Block{genesis},
		Difficulty:  1,
		Mempool:     []Transaction{},
		BlockReward: InitialReward,
		State:       NewStateManager(),
	}

	if err := bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward); err != nil {
		t.Fatalf("apply genesis state: %v", err)
	}

	minerAddr := newTestAccount(t, "miner").Address
	baseTime := bc.GetLatestBlock().Timestamp

	// Build enough history for LWMA using the active regtest target.
	for i := 1; i <= 65; i++ {
		prev := bc.GetLatestBlock()
		target := CalculateExpectedTarget(bc.Chain, prev.Index+1)

		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}

		b := *NewBlock(
			[]Transaction{cbTx},
			prev.Hash,
			prev.Index+1,
			minerAddr,
			target,
		)
		b.Timestamp = baseTime + int64(i*5)
		mineTestBlock(t, &b, target)

		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("failed to add valid block #%d during LWMA setup", i)
		}
	}

	prev := bc.GetLatestBlock()
	expectedTarget := CalculateExpectedTarget(bc.Chain, prev.Index+1)

	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}

	// A forged, easier target must be rejected.
	forgedTarget := DifficultyToTarget(1)
	bForged := *NewBlock(
		[]Transaction{cbTx},
		prev.Hash,
		prev.Index+1,
		minerAddr,
		forgedTarget,
	)
	bForged.Timestamp = prev.Timestamp + 5
	bForged.Target = fmt.Sprintf("%064x", forgedTarget)
	bForged.Difficulty = 1
	mineTestBlock(t, &bForged, forgedTarget)

	errForged := bc.ValidateBlock(bForged)
	if errForged == nil {
		t.Fatal("consensus accepted a block with a forged target")
	}
	if !strings.Contains(errForged.Error(), "target mismatch") {
		t.Fatalf("expected target mismatch error, got: %v", errForged)
	}

	// The correctly targeted block must be accepted.
	bValid := *NewBlock(
		[]Transaction{cbTx},
		prev.Hash,
		prev.Index+1,
		minerAddr,
		expectedTarget,
	)
	bValid.Timestamp = prev.Timestamp + 5
	bValid.Target = fmt.Sprintf("%064x", expectedTarget)
	bValid.Difficulty = TargetToDifficulty(expectedTarget)
	mineTestBlock(t, &bValid, expectedTarget)

	if !bc.ValidateAndAddBlock(bValid) {
		t.Fatal("consensus rejected a block with the expected LWMA target")
	}
}
