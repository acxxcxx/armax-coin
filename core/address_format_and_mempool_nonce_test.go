package core

import (
	"context"
	"math/big"
	"strings"
	"testing"
)

// TestC20_UppercaseAndMixedCaseAddressRejected tests that uppercase/mixed-case bech32 is strictly rejected
func TestC20_UppercaseAndMixedCaseAddressRejected(t *testing.T) {
	addr := newTestAccount(t).Address

	upper := strings.ToUpper(addr)
	if ValidateAddress(upper) {
		t.Errorf("ValidateAddress must reject uppercase address %s", upper)
	}
	if _, err := AddressToHex(upper); err == nil {
		t.Errorf("AddressToHex must return error for uppercase address %s", upper)
	}

	// Mixed case: capitalize first 4 letters
	mixed := "ARMX" + addr[4:]
	if ValidateAddress(mixed) {
		t.Errorf("ValidateAddress must reject mixed-case address %s", mixed)
	}
	if _, err := AddressToHex(mixed); err == nil {
		t.Errorf("AddressToHex must return error for mixed-case address %s", mixed)
	}
}

// TestC20_Base58AndHexRejectedInConsensus tests that Base58Check and legacy 40-char hex are rejected
func TestC20_Base58AndHexRejectedInConsensus(t *testing.T) {
	base58Addr := "A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	if ValidateAddress(base58Addr) {
		t.Errorf("ValidateAddress must reject Base58Check address: %s", base58Addr)
	}
	if _, err := AddressToHex(base58Addr); err == nil {
		t.Errorf("AddressToHex must reject Base58Check address: %s", base58Addr)
	}

	hex40 := "1111111111111111111111111111111111111111"
	if ValidateAddress(hex40) {
		t.Errorf("ValidateAddress must reject 40-character legacy hex: %s", hex40)
	}
	if _, err := AddressToHex(hex40); err == nil {
		t.Errorf("AddressToHex must reject 40-character legacy hex: %s", hex40)
	}
}

// TestC20_DuplicateNonceMempoolRejected tests that two txs with the same nonce cannot both enter mempool
func TestC20_DuplicateNonceMempoolRejected(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	accA := newTestAccount(t, "userA")
	privA, pubA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	addrB := newTestAccount(t, "userB").Address

	// Pre-fund account A
	hexA, _ := AddressToHex(addrA)
	bc.State.Lock.Lock()
	bc.State.Accounts[hexA] = AccountState{
		Balance: 100 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	// Tx 1 with Nonce 1
	tx1 := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrB,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = tx1.Sign(privA)

	ok, msg := bc.AddTransaction(tx1)
	if !ok {
		t.Fatalf("first transaction with nonce 1 should be accepted, got: %s", msg)
	}

	// Tx 2 with same Nonce 1 (different amount or recipient)
	tx2 := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrB,
		Amount:    15 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = tx2.Sign(privA)

	ok2, _ := bc.AddTransaction(tx2)
	if ok2 {
		t.Errorf("second transaction with duplicate nonce 1 should be REJECTED from mempool")
	}
}

// TestC20_MineBlockContextSurvivesCorruptMempool tests that MineBlockContext purges invalid mempool txs
// and produces a block that cleanly passes ValidateAndAddBlock.
func TestC20_MineBlockContextSurvivesCorruptMempool(t *testing.T) {
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	defer func() {
		targetOverrideHook = oldHook
	}()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()
	accA := newTestAccount(t, "userA")
	privA, pubA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	addrB := newTestAccount(t, "userB").Address
	minerAddr := newTestAccount(t, "miner").Address

	hexA, _ := AddressToHex(addrA)
	bc.State.Lock.Lock()
	bc.State.Accounts[hexA] = AccountState{
		Balance: 50 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	// Valid Tx: 10 ARMX, Nonce 1
	validTx := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrB,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = validTx.Sign(privA)

	// Corrupt Tx: attempts to spend 100 ARMX (more than balance 50 ARMX)
	corruptTx := Transaction{
		Sender:    addrA,
		SenderPub: pubA,
		Recipient: addrB,
		Amount:    100 * CoinUnit,
		Fee:       MinRelayFee * 12,
		Nonce:     2,
		Type:      TxTypeStandard,
	}
	_ = corruptTx.Sign(privA)

	bc.Lock.Lock()
	bc.Mempool = append(bc.Mempool, validTx, corruptTx)
	bc.Lock.Unlock()

	// MineBlockContext should simulate transactions against state overlay, discard corruptTx,
	// mine the valid block, and pass ValidateAndAddBlock without error!
	block, err := bc.MineBlockContext(context.Background(), minerAddr)
	if err != nil {
		t.Fatalf("MineBlockContext failed: %v", err)
	}

	if block.Index != 1 {
		t.Errorf("expected mined block index 1, got %d", block.Index)
	}

	// Verify that corruptTx was NOT included in the block
	for _, tx := range block.Transactions {
		if tx.Amount == 100*CoinUnit {
			t.Errorf("corrupt transaction was illegally included in mined block!")
		}
	}
}
