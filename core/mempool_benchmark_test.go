//go:build testhooks

package core

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// BenchmarkCleanMempoolLocked_5000Tx benchmarks CleanMempoolLocked execution time
// on a mempool filled with 5,000 transactions [F6-PERF].
func BenchmarkCleanMempoolLocked_5000Tx(b *testing.B) {
	origNet := GetActiveNetwork()
	defer func() {
		netMu.Lock()
		ActiveNetwork = origNet
		netMu.Unlock()
	}()

	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	bc := MustNewBlockchain(1)

	// Create 100 senders with 50 txs each = 5,000 transactions
	numSenders := 100
	txsPerSender := 50
	totalTxs := numSenders * txsPerSender

	var templateMempool []Transaction
	recipientAddr := GenesisMiner

	for s := 0; s < numSenders; s++ {
		var seed [32]byte
		_, _ = rand.Read(seed[:])
		sk, err := pqsig.GenerateFromSeed(seed)
		if err != nil {
			b.Fatalf("GenerateFromSeed failed: %v", err)
		}
		senderAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
		sHex, _ := AddressToHex(senderAddr)

		// Fund account with sufficient balance for 50 txs
		bc.State.Accounts[sHex] = AccountState{
			Balance: int64(txsPerSender) * 10000000,
			Nonce:   0,
		}

		for n := 1; n <= txsPerSender; n++ {
			tx := Transaction{
				Sender:    senderAddr,
				Recipient: recipientAddr,
				Amount:    100000,
				Fee:       1000,
				Nonce:     int64(n),
			}
			templateMempool = append(templateMempool, tx)
		}
	}

	if len(templateMempool) != totalTxs {
		b.Fatalf("Expected %d transactions, got %d", totalTxs, len(templateMempool))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Re-initialize mempool and state balances for accurate benchmark iteration
		bc.Mempool = make([]Transaction, len(templateMempool))
		copy(bc.Mempool, templateMempool)
		b.StartTimer()

		purged := bc.CleanMempoolLocked()
		if purged != 0 {
			b.Fatalf("Unexpected purged count: %d", purged)
		}
	}
}

// TestF6_BenchmarkReport runs a single timed iteration of CleanMempoolLocked on 5000 transactions
// to print human-readable execution time [F6-PERF].
func TestF6_BenchmarkReport(t *testing.T) {
	origNet := GetActiveNetwork()
	defer func() {
		netMu.Lock()
		ActiveNetwork = origNet
		netMu.Unlock()
	}()

	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	bc := MustNewBlockchain(1)
	numSenders := 100
	txsPerSender := 50

	var templateMempool []Transaction
	recipientAddr := GenesisMiner

	for s := 0; s < numSenders; s++ {
		var seed [32]byte
		_, _ = rand.Read(seed[:])
		sk, _ := pqsig.GenerateFromSeed(seed)
		senderAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
		sHex, _ := AddressToHex(senderAddr)

		bc.State.Accounts[sHex] = AccountState{
			Balance: int64(txsPerSender) * 10000000,
			Nonce:   0,
		}

		for n := 1; n <= txsPerSender; n++ {
			tx := Transaction{
				Sender:    senderAddr,
				Recipient: recipientAddr,
				Amount:    100000,
				Fee:       1000,
				Nonce:     int64(n),
			}
			templateMempool = append(templateMempool, tx)
		}
	}

	bc.Mempool = make([]Transaction, len(templateMempool))
	copy(bc.Mempool, templateMempool)

	start := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			bc.Mempool = make([]Transaction, len(templateMempool))
			copy(bc.Mempool, templateMempool)
			b.StartTimer()

			_ = bc.CleanMempoolLocked()
		}
	})
	fmt.Printf("[F6-PERF REPORT] CleanMempoolLocked for 5,000 transactions: %s per operation (%d ns/op)\n",
		start.T, start.NsPerOp())
}
