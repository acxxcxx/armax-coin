package core

import (
	"math/big"
	"os"
	"runtime"
	"strings"
	"testing"
)

func setupPerfAuditEasyTarget(t *testing.T) {
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	t.Cleanup(func() {
		targetOverrideHook = oldHook
	})
}

// TestC12_CirculatingSupplyAndReward verifies O(1) reward calculation and eager supply caching
func TestC12_CirculatingSupplyAndReward(t *testing.T) {
	setupPerfAuditEasyTarget(t)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	bc := NewBlockchain(1)
	defer bc.Close()
	genesisReward := InitialReward

	if bc.GetCirculatingSupply() != genesisReward {
		t.Fatalf("Expected initial circulating supply %d, got %d", genesisReward, bc.GetCirculatingSupply())
	}

	// Mine 5 blocks
	for i := 1; i <= 5; i++ {
		_, err := bc.MineBlockWithErr(GenesisMiner)
		if err != nil {
			t.Fatalf("Mining block #%d failed: %v", i, err)
		}
	}

	expectedSupply := genesisReward + (5 * InitialReward)
	if bc.GetCirculatingSupply() != expectedSupply {
		t.Fatalf("Expected supply %d, got %d", expectedSupply, bc.GetCirculatingSupply())
	}

	// Reward at block 0, 1, and after halving
	r0 := CalculateBlockRewardGivenSupply(0, 0)
	if r0 != InitialReward {
		t.Fatalf("Expected initial reward %d, got %d", InitialReward, r0)
	}

	rHalving1 := CalculateBlockRewardGivenSupply(HalvingInterval, InitialReward*100)
	if rHalving1 != InitialReward/2 {
		t.Fatalf("Expected reward after halving %d, got %d", InitialReward/2, rHalving1)
	}
}

// TestC16_MempoolTrackingAndMinRelayFee verifies min fee, sender bounds, and O(1) pending lookup
func TestC16_MempoolTrackingAndMinRelayFee(t *testing.T) {
	setupPerfAuditEasyTarget(t)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	bc := NewBlockchain(1)
	bc.CoinbaseMaturity = 1
	defer bc.Close()

	// Generate keypair for miner/sender
	acc := newTestAccount(t)
	privKey, pubHex, senderAddr := acc.PrivKey, acc.PubHex, acc.Address

	// Mine a block so senderAddr has confirmed balance
	_, err := bc.MineBlockWithErr(senderAddr)
	if err != nil {
		t.Fatalf("Failed to mine block: %v", err)
	}

	// Create test transaction with 0 fee (below MinRelayFee)
	txZeroFee := Transaction{
		Sender:    senderAddr,
		SenderPub: pubHex,
		Recipient: GenesisMiner,
		Amount:    1000,
		Fee:       0, // Below MinRelayFee
		Nonce:     bc.GetPendingNonce(senderAddr) + 1,
	}
	_ = txZeroFee.Sign(privKey)

	accepted, reason := bc.AddTransaction(txZeroFee)
	if accepted {
		t.Fatalf("Expected transaction with 0 fee to be rejected below MinRelayFee")
	}
	if reason == "" {
		t.Fatalf("Expected reject reason for 0 fee transaction")
	}

	// Create transaction with valid fee >= MinRelayFee
	initialBal := bc.GetPendingBalance(senderAddr)
	initialNonce := bc.GetPendingNonce(senderAddr)

	txValid := Transaction{
		Sender:    senderAddr,
		SenderPub: pubHex,
		Recipient: GenesisMiner,
		Amount:    5000,
		Fee:       MinRelayFee * 12,
		Nonce:     initialNonce + 1,
	}
	if err := txValid.Sign(privKey); err != nil {
		t.Fatalf("Failed to sign valid tx: %v", err)
	}

	accepted, msg := bc.AddTransaction(txValid)
	if !accepted {
		t.Fatalf("Expected valid transaction to be accepted: %s", msg)
	}

	// Verify pending balance and nonce reflect the mempool transaction in O(1)
	expectedBal := initialBal - (5000 + MinRelayFee*12)
	if bc.GetPendingBalance(senderAddr) != expectedBal {
		t.Fatalf("Expected pending balance %d, got %d", expectedBal, bc.GetPendingBalance(senderAddr))
	}
	if bc.GetPendingNonce(senderAddr) != initialNonce+1 {
		t.Fatalf("Expected pending nonce %d, got %d", initialNonce+1, bc.GetPendingNonce(senderAddr))
	}
}

// TestC14_StorageSafePermissions verifies 0600 permissions on database files
func TestC14_StorageSafePermissions(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := NewBlockchain(1)
	defer bc.Close()
	err := bc.SaveToFile()
	if err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	dbPath := GetDBPath()
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Failed to stat db file: %v", err)
	}

	// On Unix/POSIX systems, file mode must be strictly 0600 [T-07a]
	if runtime.GOOS != "windows" {
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Fatalf("Expected database permissions 0600 on POSIX, got %o", perm)
		}
	}
}

// TestC15_DeltaOverlayRollbackAndAddressCache verifies delta overlay atomicity and AddressToHexCached [C-15, T-07a]
func TestC15_DeltaOverlayRollbackAndAddressCache(t *testing.T) {
	// 1. Verify AddressToHexCached
	testAddr := GenesisMiner
	hex1, err := AddressToHexCached(testAddr)
	if err != nil {
		t.Fatalf("AddressToHexCached failed: %v", err)
	}
	hex2, err := AddressToHexCached(testAddr)
	if err != nil || hex1 != hex2 {
		t.Fatalf("AddressToHexCached inconsistent: %s vs %s", hex1, hex2)
	}

	// 2. Verify Delta Overlay Rollback in StateManager
	sm := NewStateManager()

	accSender := newTestAccount(t, "sender")
	priv, pubHex, senderAddr := accSender.PrivKey, accSender.PubHex, accSender.Address
	senderHex, _ := AddressToHex(senderAddr)
	sm.Accounts[senderHex] = AccountState{Balance: 1000 * CoinUnit, Nonce: 0}

	recipAddr := newTestAccount(t, "recip").Address

	// Create a valid signed tx that passes Verify() but has insufficient balance [T-07a]
	overspendTx := Transaction{
		Sender:    senderAddr,
		SenderPub: pubHex,
		Recipient: recipAddr,
		Amount:    5000 * CoinUnit, // Exceeds balance (1000 * CoinUnit)
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := overspendTx.Sign(priv); err != nil {
		t.Fatalf("signing failed: %v", err)
	}
	if !overspendTx.Verify() {
		t.Fatalf("test precondition failed: overspendTx must pass cryptographic Verify()!")
	}

	invalidBlock := &Block{
		Index: 1,
		Transactions: []Transaction{
			{
				Sender:    "SYSTEM",
				Recipient: recipAddr,
				Amount:    InitialReward + MinRelayFee,
				Fee:       0,
				Nonce:     0,
				Signature: "REWARD_SIG",
			},
			overspendTx,
		},
	}

	initialBalance := sm.GetBalance(senderAddr)
	err = sm.VerifyAndApplyBlock(invalidBlock, InitialReward)
	if err == nil {
		t.Fatalf("Expected VerifyAndApplyBlock to fail on excessive amount")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("Expected 'insufficient balance' error, got: %v", err)
	}

	// Verify primary ledger was NOT touched
	if sm.GetBalance(senderAddr) != initialBalance {
		t.Fatalf("State was modified on failure! Expected %d, got %d", initialBalance, sm.GetBalance(senderAddr))
	}
}
