//go:build testhooks

package core

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"
)

// TestL1_5_RealSizes measures exact JSON sizes for ML-DSA-65 transactions and blocks [L1-5]
func TestL1_5_RealSizes(t *testing.T) {
	SetNetworkForTesting(true)
	_ = SetNetwork("mainnet")

	minerAcc := newTestAccount(t, "miner")
	senderAcc := newTestAccount(t, "perf_sender")
	recipAcc := newTestAccount(t, "perf_recip")

	// 1. Measure 1 signed ML-DSA-65 user transaction
	tx := Transaction{
		Sender:    senderAcc.Address,
		SenderPub: senderAcc.PubHex,
		Recipient: recipAcc.Address,
		Amount:    10 * CoinUnit,
		Fee:       10000,
		Nonce:     1,
	}
	if err := tx.Sign(senderAcc.PrivKey); err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}

	txJSON, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}
	txSize := len(txJSON)
	t.Logf("=== [L1-5] ML-DSA-65 TRANSACTION SIZE ===")
	t.Logf("1 Signed ML-DSA-65 Transaction JSON Size: %d bytes (tx.Size() = %d)", txSize, tx.Size())

	// Helper to create a realistic valid block with coinbase + N signed user transactions
	makeBlock := func(nTx int) *Block {
		coinbase := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAcc.Address,
			Amount:    50 * CoinUnit,
			Nonce:     1,
			Signature: "REWARD_SIG",
		}
		txs := make([]Transaction, 0, nTx+1)
		txs = append(txs, coinbase)

		for i := 0; i < nTx; i++ {
			sAcc := newTestAccount(t, fmt.Sprintf("sender_%d", i))
			rAcc := newTestAccount(t, fmt.Sprintf("recip_%d", i))
			txi := Transaction{
				Sender:    sAcc.Address,
				SenderPub: sAcc.PubHex,
				Recipient: rAcc.Address,
				Amount:    int64(1000 + i),
				Fee:       10000,
				Nonce:     1,
			}
			if err := txi.Sign(sAcc.PrivKey); err != nil {
				t.Fatalf("Sign tx %d failed: %v", i, err)
			}
			txs = append(txs, txi)
		}

		target, _ := new(big.Int).SetString("00000000ffff0000000000000000000000000000000000000000000000000000", 16)
		blk := NewBlock(txs, "0000000000000000000000000000000000000000000000000000000000000000", 1, minerAcc.Address, target)
		blk.Nonce = 12345
		blk.Timestamp = time.Now().Unix()
		blk.Hash = blk.CalculateHash()
		return blk
	}

	// 2. Measure blocks with 100, 150, 180 user transactions
	counts := []int{100, 150, 180}
	t.Logf("=== [L1-5] BLOCK SIZES ===")
	for _, count := range counts {
		blk := makeBlock(count)
		blkJSON, err := json.Marshal(blk)
		if err != nil {
			t.Fatalf("Marshal block failed: %v", err)
		}
		t.Logf("Block with %d user txs (+1 coinbase = %d total): %d bytes (%.2f KB / %.4f MB)",
			count, count+1, len(blkJSON), float64(len(blkJSON))/1024, float64(len(blkJSON))/1024/1024)
	}

	// Also measure exact 100, 150, 180 total txs (99, 149, 179 user txs)
	t.Logf("=== [L1-5] BLOCK SIZES (TOTAL TXS INCLUDING COINBASE) ===")
	for _, count := range counts {
		blk := makeBlock(count - 1)
		blkJSON, _ := json.Marshal(blk)
		t.Logf("Block with %d total txs (1 coinbase + %d user txs): %d bytes (%.2f KB / %.4f MB)",
			count, count-1, len(blkJSON), float64(len(blkJSON))/1024, float64(len(blkJSON))/1024/1024)
	}

	// 3. Compute maximum transactions per block under MaxBlockSize (2,000,000 B)
	// Base block overhead (header fields + empty transactions array)
	emptyBlk := makeBlock(0)
	emptyJSON, _ := json.Marshal(emptyBlk)
	coinbaseSize := len(emptyJSON)
	t.Logf("Empty block (coinbase only) size: %d bytes", coinbaseSize)

	// In JSON array of transactions: each transaction adds len(txJSON) + 1 (for comma delimiter)
	// Average signed tx JSON size
	avgTxSize := txSize + 1 // including comma delimiter in JSON array
	remainingCapacity := MaxBlockSize - coinbaseSize
	maxUserTx := remainingCapacity / avgTxSize
	maxTotalTx := maxUserTx + 1

	// Verify with actual block of that size
	t.Logf("=== [L1-5] MAX BLOCK CAPACITY ===")
	t.Logf("MaxBlockSize: %d bytes (2 MB)", MaxBlockSize)
	t.Logf("Calculated Max User Transactions per block: %d", maxUserTx)
	t.Logf("Calculated Max Total Transactions (with coinbase): %d", maxTotalTx)

	// Construct block with maxUserTx to verify size <= MaxBlockSize
	maxBlk := makeBlock(maxUserTx)
	maxBlkSize := maxBlk.Size()
	t.Logf("Actual verified size of block with %d user txs: %d bytes (fits in MaxBlockSize: %v, remaining headroom: %d bytes)",
		maxUserTx, maxBlkSize, maxBlkSize <= MaxBlockSize, MaxBlockSize-maxBlkSize)

	if maxBlkSize > MaxBlockSize {
		t.Fatalf("maxBlkSize %d exceeded MaxBlockSize %d", maxBlkSize, MaxBlockSize)
	}
}
