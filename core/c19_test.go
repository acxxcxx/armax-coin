package core

import (
	"crypto/rand"
	"math/big"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestC19_SelfSendRejectedInTxVerify tests that a transaction where Sender == Recipient is rejected by Verify()
func TestC19_SelfSendRejectedInTxVerify(t *testing.T) {
	acc := newTestAccount(t)
	priv, pubHex, addr := acc.PrivKey, acc.PubHex, acc.Address

	tx := Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: addr, // Self-send
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}

	if tx.Verify() {
		t.Errorf("expected tx.Verify() to reject self-send (Sender == Recipient), but it returned true")
	}
}

// TestC19_SelfSendRejectedInState tests that VerifyAndApplyBlock rejects self-send with explicit error
func TestC19_SelfSendRejectedInState(t *testing.T) {
	sm := NewStateManager()
	acc := newTestAccount(t)
	priv, pubHex, addr := acc.PrivKey, acc.PubHex, acc.Address
	addrHex, _ := AddressToHex(addr)

	// Pre-fund sender account
	sm.Accounts[addrHex] = AccountState{
		Balance: 100 * CoinUnit,
		Nonce:   0,
	}

	tx := Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: addr, // Self-send
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = tx.Sign(priv)

	minerAddr := newTestAccount(t).Address

	b := &Block{
		Index: 1,
		Transactions: []Transaction{
			{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    60*CoinUnit + MinRelayFee,
				Fee:       0,
				Nonce:     0,
				Signature: "REWARD_SIG",
			},
			tx,
		},
	}

	err := sm.VerifyAndApplyBlock(b, 60*CoinUnit)
	if err == nil {
		t.Fatalf("expected VerifyAndApplyBlock to fail on self-send, but got nil")
	}
	if !strings.Contains(err.Error(), "self-transfer rejected") && !strings.Contains(err.Error(), "cannot send to self") {
		t.Errorf("expected error message to contain 'self-transfer rejected' or 'cannot send to self', got: %v", err)
	}
}

// TestC19_MultipleTxsToSameRecipientInOneBlock tests multiple txs to same recipient in one block
func TestC19_MultipleTxsToSameRecipientInOneBlock(t *testing.T) {
	sm := NewStateManager()
	accA := newTestAccount(t)
	privA, pubHexA, addrA := accA.PrivKey, accA.PubHex, accA.Address
	addrB := newTestAccount(t).Address
	minerAddr := newTestAccount(t).Address

	hexA, _ := AddressToHex(addrA)
	hexB, _ := AddressToHex(addrB)

	sm.Accounts[hexA] = AccountState{
		Balance: 100 * CoinUnit,
		Nonce:   0,
	}

	// Two transfers from A to B in the same block:
	// Tx 1: 10 ARMX, Nonce 1, Fee 1000
	// Tx 2: 20 ARMX, Nonce 2, Fee 1000
	tx1 := Transaction{
		Sender:    addrA,
		SenderPub: pubHexA,
		Recipient: addrB,
		Amount:    10 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	_ = tx1.Sign(privA)

	tx2 := Transaction{
		Sender:    addrA,
		SenderPub: pubHexA,
		Recipient: addrB,
		Amount:    20 * CoinUnit,
		Fee:       MinRelayFee,
		Nonce:     2,
		Type:      TxTypeStandard,
	}
	_ = tx2.Sign(privA)

	b := &Block{
		Index: 1,
		Transactions: []Transaction{
			{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    60*CoinUnit + 2*MinRelayFee,
				Fee:       0,
				Nonce:     1,
				Signature: "REWARD_SIG",
			},
			tx1,
			tx2,
		},
	}

	err := sm.VerifyAndApplyBlock(b, 60*CoinUnit)
	if err != nil {
		t.Fatalf("VerifyAndApplyBlock failed unexpectedly: %v", err)
	}

	stateA := sm.Accounts[hexA]
	stateB := sm.Accounts[hexB]

	expectedBalanceA := 100*CoinUnit - (30*CoinUnit + 2*MinRelayFee)
	expectedBalanceB := 30 * CoinUnit

	if stateA.Balance != expectedBalanceA {
		t.Errorf("account A balance mismatch: expected %d, got %d", expectedBalanceA, stateA.Balance)
	}
	if stateA.Nonce != 2 {
		t.Errorf("account A nonce mismatch: expected 2, got %d", stateA.Nonce)
	}
	if stateB.Balance != expectedBalanceB {
		t.Errorf("account B balance mismatch: expected %d, got %d", expectedBalanceB, stateB.Balance)
	}
}

// TestC19_ConservationOfSupplyInvariantFuzz verifies that after 1000 blocks, total sum of all balances
// matches the cumulative sum of (coinbase.Amount - block fee) exactly.
func TestC19_ConservationOfSupplyInvariantFuzz(t *testing.T) {
	sm := NewStateManager()

	const numUsers = 5
	privKeys := make([]*pqsig.PrivateKey, numUsers)
	pubKeys := make([]string, numUsers)
	addrs := make([]string, numUsers)
	hexAddrs := make([]string, numUsers)

	for i := 0; i < numUsers; i++ {
		acc := newTestAccount(t)
		privKeys[i], pubKeys[i], addrs[i] = acc.PrivKey, acc.PubHex, acc.Address
		hexAddrs[i], _ = AddressToHex(addrs[i])
	}

	var cumulativeExpectedSupply int64 = 0

	for blockIdx := 1; blockIdx <= 1000; blockIdx++ {
		minerIdx := blockIdx % numUsers
		minerAddr := addrs[minerIdx]

		var blockTxs []Transaction
		var totalFees int64 = 0

		// Generate 0-3 random valid transactions between users
		numTxsBig, _ := rand.Int(rand.Reader, big.NewInt(4))
		numTxs := int(numTxsBig.Int64())

		for txCount := 0; txCount < numTxs; txCount++ {
			senderIdxBig, _ := rand.Int(rand.Reader, big.NewInt(int64(numUsers)))
			senderIdx := int(senderIdxBig.Int64())
			recipIdxBig, _ := rand.Int(rand.Reader, big.NewInt(int64(numUsers)))
			recipIdx := int(recipIdxBig.Int64())

			if senderIdx == recipIdx {
				continue // Skip self-send in valid generator
			}

			senderHex := hexAddrs[senderIdx]
			senderAcc := sm.Accounts[senderHex]

			// Determine pending state if sender already sent in this block
			pendingNonce := senderAcc.Nonce
			pendingBal := senderAcc.Balance
			for _, prevTx := range blockTxs {
				if prevTx.Sender == addrs[senderIdx] {
					pendingNonce = prevTx.Nonce
					pendingBal -= (prevTx.Amount + prevTx.Fee)
				}
			}

			cost := int64(1*CoinUnit + MinRelayFee)
			if pendingBal < cost {
				continue // not enough funds to transfer
			}

			tx := Transaction{
				Sender:    addrs[senderIdx],
				SenderPub: pubKeys[senderIdx],
				Recipient: addrs[recipIdx],
				Amount:    1 * CoinUnit,
				Fee:       MinRelayFee,
				Nonce:     pendingNonce + 1,
				Type:      TxTypeStandard,
			}
			_ = tx.Sign(privKeys[senderIdx])
			blockTxs = append(blockTxs, tx)
			totalFees += MinRelayFee
		}

		reward := int64(60 * CoinUnit)
		coinbaseAmount := reward + totalFees
		coinbase := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    coinbaseAmount,
			Fee:       0,
			Nonce:     int64(blockIdx),
			Signature: "REWARD_SIG",
		}

		fullTxs := append([]Transaction{coinbase}, blockTxs...)
		b := &Block{
			Index:        blockIdx,
			Transactions: fullTxs,
		}

		err := sm.VerifyAndApplyBlock(b, reward, 1)
		if err != nil {
			t.Fatalf("block #%d failed in fuzz test: %v", blockIdx, err)
		}

		cumulativeExpectedSupply += (coinbaseAmount - totalFees)

		// Check invariant
		var totalBalanceSum int64 = 0
		for _, acc := range sm.Accounts {
			totalBalanceSum += acc.Balance
		}

		if totalBalanceSum != cumulativeExpectedSupply {
			t.Fatalf("block #%d: invariant violated! totalBalanceSum=%d != cumulativeExpectedSupply=%d",
				blockIdx, totalBalanceSum, cumulativeExpectedSupply)
		}
	}
}
