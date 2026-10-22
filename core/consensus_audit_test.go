package core

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// Helper to create a test blockchain with funded account
func setupTestChainWithFunds(t *testing.T) (*Blockchain, *pqsig.PrivateKey, string, string) {
	gen := GetGenesisBlock()
	gen.Target = fmt.Sprintf("%064x", DifficultyToTarget(1))
	bc := &Blockchain{
		Chain:            []Block{gen},
		Difficulty:       1,
		Mempool:          []Transaction{},
		BlockReward:      InitialReward,
		State:            NewStateManager(),
		StoragePath:      filepath.Join(t.TempDir(), "blockchain.db"),
		CoinbaseMaturity: 1,
	}
	t.Cleanup(func() { _ = bc.Close() })
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], InitialReward)

	acc := newTestAccount(t)
	privKey, pubHex, addr := acc.PrivKey, acc.PubHex, acc.Address

	// Mine a block to fund addr
	target := bc.CalculateLWMA1()
	prev := bc.GetLatestBlock()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: addr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	b1 := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, addr, target)
	b1.Timestamp = prev.Timestamp + 1
	for {
		b1.Hash = b1.CalculateHash()
		if HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}
	if !bc.ValidateAndAddBlock(b1) {
		t.Fatalf("Failed to add funding block 1")
	}

	return bc, privKey, pubHex, addr
}

// [C-01] Integer Overflow test: Amount=MaxInt64 with Fee=1 wraps around to negative
func TestC01_IntegerOverflow(t *testing.T) {
	bc, privKey, pubHex, senderAddr := setupTestChainWithFunds(t)
	recipAddr := newTestAccount(t).Address

	// Malicious tx with Amount=MaxInt64, Fee=1
	badTx := Transaction{
		Sender:    senderAddr,
		SenderPub: pubHex,
		Recipient: recipAddr,
		Amount:    math.MaxInt64,
		Fee:       1,
		Nonce:     1,
	}
	_ = badTx.Sign(privKey)

	// tx.Verify must reject Amount > MaxSupply
	if badTx.Verify() {
		t.Errorf("[C-01] FAILED: tx.Verify() accepted transaction with Amount=MaxInt64")
	}

	// Block including this transaction must be rejected by consensus
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: senderAddr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	malBlock := *NewBlock([]Transaction{cbTx, badTx}, prev.Hash, prev.Index+1, senderAddr, target)
	malBlock.Timestamp = prev.Timestamp + 1
	for {
		malBlock.Hash = malBlock.CalculateHash()
		if HashMeetsTarget(malBlock.Hash, target) {
			break
		}
		malBlock.Nonce++
	}

	errMalBlock := bc.ValidateBlock(malBlock)
	if errMalBlock == nil {
		t.Fatalf("[C-01] CRITICAL VULNERABILITY: Block with integer overflow (Amount=MaxInt64, Fee=1) was ACCEPTED into blockchain!")
	}
	if !strings.Contains(errMalBlock.Error(), "invalid amount") && !strings.Contains(errMalBlock.Error(), "overflow") {
		t.Fatalf("Expected 'invalid amount' or 'overflow' rejection substring, got: %v", errMalBlock)
	}
}

// [C-02] Incomplete Coinbase Validation test
func TestC02_CoinbaseValidation(t *testing.T) {
	_, _, _, minerAddr := setupTestChainWithFunds(t)

	testCases := []struct {
		name string
		cb   Transaction
	}{
		{
			name: "Negative Amount",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    -1000,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Garbage Non-20-byte Recipient",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: "invalid_garbage_address_string_12345",
				Amount:    InitialReward,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Coinbase with Non-zero Fee",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    InitialReward,
				Fee:       500,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Coinbase with Non-matching Nonce",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    InitialReward,
				Nonce:     999,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Coinbase with Non-empty SenderPub",
			cb: Transaction{
				Sender:    "SYSTEM",
				SenderPub: "deadbeef",
				Recipient: minerAddr,
				Amount:    InitialReward,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Coinbase with Non-empty Type",
			cb: Transaction{
				Sender:    "SYSTEM",
				Type:      "HTLC_LOCK",
				Recipient: minerAddr,
				Amount:    InitialReward,
				Signature: "REWARD_SIG",
			},
		},
		{
			name: "Coinbase with Wrong Signature Sentinel",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: minerAddr,
				Amount:    InitialReward,
				Signature: "FORGED_SIG",
			},
		},
		{
			name: "Coinbase sending to SYSTEM",
			cb: Transaction{
				Sender:    "SYSTEM",
				Recipient: "SYSTEM",
				Amount:    InitialReward,
				Signature: "REWARD_SIG",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testBc, _, _, mAddr := setupTestChainWithFunds(t)
			p := testBc.GetLatestBlock()
			tgt := testBc.CalculateLWMA1()

			cb := tc.cb
			if cb.Recipient == minerAddr {
				cb.Recipient = mAddr
			}
			if tc.name != "Coinbase with Non-matching Nonce" {
				cb.Nonce = int64(p.Index + 1)
			}

			b := *NewBlock([]Transaction{cb}, p.Hash, p.Index+1, mAddr, tgt)
			b.Timestamp = p.Timestamp + 1
			for {
				b.Hash = b.CalculateHash()
				if HashMeetsTarget(b.Hash, tgt) {
					break
				}
				b.Nonce++
			}
			errCb := testBc.ValidateBlock(b)
			if errCb == nil {
				t.Fatalf("[C-02] FAILED: Malicious coinbase (%s) was accepted into blockchain!", tc.name)
			}
			if !strings.Contains(errCb.Error(), "coinbase") && !strings.Contains(errCb.Error(), "reward") {
				t.Fatalf("Expected coinbase/reward rejection substring, got: %v", errCb)
			}
		})
	}
}

// [C-03] Transaction field constraints & validation
func TestC03_TransactionFieldConstraints(t *testing.T) {
	_, privKey, pubHex, addr := setupTestChainWithFunds(t)
	recipAddr := newTestAccount(t).Address

	// Whitelisted types only
	t.Run("Disallowed Tx Type", func(t *testing.T) {
		tx := Transaction{
			Sender:    addr,
			SenderPub: pubHex,
			Recipient: recipAddr,
			Amount:    100,
			Nonce:     1,
			Type:      "UNKNOWN_MALICIOUS_TYPE",
		}
		_ = tx.Sign(privKey)
		if tx.Verify() {
			t.Errorf("[C-03] FAILED: tx.Verify() accepted disallowed type")
		}
	})

	t.Run("Non-printable characters or colon delimiter injection", func(t *testing.T) {
		tx := Transaction{
			Sender:    addr + ":malicious",
			SenderPub: pubHex,
			Recipient: recipAddr,
			Amount:    100,
			Nonce:     1,
		}
		_ = tx.Sign(privKey)
		if tx.Verify() {
			t.Errorf("[C-03] FAILED: tx.Verify() accepted address with colon delimiter injection")
		}
	})

	t.Run("Negative TimeLock", func(t *testing.T) {
		tx := Transaction{
			Sender:    addr,
			SenderPub: pubHex,
			Recipient: recipAddr,
			Amount:    100,
			Nonce:     1,
			Type:      TxTypeStandard,
			TimeLock:  -50,
		}
		_ = tx.Sign(privKey)
		if tx.Verify() {
			t.Errorf("[C-03] FAILED: tx.Verify() accepted negative TimeLock")
		}
	})
}

// [C-18] Explicit duplicate transaction rejection within the same block
func TestC18_DuplicateTxInBlock(t *testing.T) {
	bc, privKey, pubHex, addr := setupTestChainWithFunds(t)
	recipAddr := newTestAccount(t).Address

	tx1 := Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: recipAddr,
		Amount:    100,
		Nonce:     1,
	}
	_ = tx1.Sign(privKey)

	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: addr,
		Amount:    InitialReward,
		Signature: "REWARD_SIG",
	}

	// Include the exact same tx twice
	dupBlock := *NewBlock([]Transaction{cbTx, tx1, tx1}, prev.Hash, prev.Index+1, addr, target)
	dupBlock.Timestamp = prev.Timestamp + 1
	for {
		dupBlock.Hash = dupBlock.CalculateHash()
		if HashMeetsTarget(dupBlock.Hash, target) {
			break
		}
		dupBlock.Nonce++
	}

	errDup := bc.ValidateBlock(dupBlock)
	if errDup == nil {
		t.Fatalf("[C-18] FAILED: Block with duplicate transaction IDs was accepted!")
	}
	if !strings.Contains(errDup.Error(), "duplicate transaction") {
		t.Fatalf("Expected 'duplicate transaction' rejection substring, got: %v", errDup)
	}
}

// [C-05] & [C-06] Median Time Past (MTP-11) enforcement
func TestC05_C06_MedianTimePast(t *testing.T) {
	bc, _, _, minerAddr := setupTestChainWithFunds(t)

	// Mine 11 blocks with known advancing timestamps
	baseTime := bc.GetLatestBlock().Timestamp
	for i := 0; i < 11; i++ {
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
		b.Timestamp = baseTime + int64((i+1)*10)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Failed to add block %d in MTP setup", i+2)
		}
	}

	mtp := CalculateMedianTimePast(bc.Chain, len(bc.Chain))
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}

	// Try block with timestamp <= MTP
	bStale := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	bStale.Timestamp = mtp
	for {
		bStale.Hash = bStale.CalculateHash()
		if HashMeetsTarget(bStale.Hash, target) {
			break
		}
		bStale.Nonce++
	}

	errStale := bc.ValidateBlock(bStale)
	if errStale == nil {
		t.Fatalf("[C-06] FAILED: Block with timestamp <= MedianTimePast was accepted!")
	}
	if !strings.Contains(errStale.Error(), "MedianTimePast") && !strings.Contains(errStale.Error(), "timestamp") {
		t.Fatalf("Expected timestamp/MedianTimePast rejection substring, got: %v", errStale)
	}

	// Try block with timestamp > now + 2 hours
	bFuture := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
	bFuture.Timestamp = time.Now().Add(3 * time.Hour).Unix()
	for {
		bFuture.Hash = bFuture.CalculateHash()
		if HashMeetsTarget(bFuture.Hash, target) {
			break
		}
		bFuture.Nonce++
	}

	errFuture := bc.ValidateBlock(bFuture)
	if errFuture == nil {
		t.Fatalf("[C-05] FAILED: Block with timestamp > now+2h was accepted!")
	}
	if !strings.Contains(errFuture.Error(), "too far in the future") && !strings.Contains(errFuture.Error(), "future") {
		t.Fatalf("Expected future timestamp rejection substring, got: %v", errFuture)
	}
}

// [C-04] Block size and transaction size limits
func TestC04_BlockAndTxSizeLimits(t *testing.T) {
	bc, privKey, pubHex, addr := setupTestChainWithFunds(t)
	recipAddr := newTestAccount(t).Address

	// Create a huge tx with preimage/metadata stuffing that exceeds MaxTxSize
	hugePreimage := make([]byte, MaxTxSize+100)
	for i := range hugePreimage {
		hugePreimage[i] = 'a'
	}

	bigTx := Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: recipAddr,
		Amount:    100,
		Nonce:     1,
		Preimage:  string(hugePreimage),
	}
	_ = bigTx.Sign(privKey)

	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: addr,
		Amount:    InitialReward,
		Signature: "REWARD_SIG",
	}

	b := *NewBlock([]Transaction{cbTx, bigTx}, prev.Hash, prev.Index+1, addr, target)
	b.Timestamp = prev.Timestamp + 1
	for {
		b.Hash = b.CalculateHash()
		if HashMeetsTarget(b.Hash, target) {
			break
		}
		b.Nonce++
	}

	errSize := bc.ValidateBlock(b)
	if errSize == nil {
		t.Fatalf("[C-04] FAILED: Block with transaction exceeding MaxTxSize was accepted!")
	}
	if !strings.Contains(errSize.Error(), "exceeds MaxTxSize") && !strings.Contains(errSize.Error(), "exceeds maximum") {
		t.Fatalf("Expected size limit rejection substring, got: %v", errSize)
	}
}
