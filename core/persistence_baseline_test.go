//go:build testhooks

package core

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Phase 0 baseline: cost of SaveToFile as chain length grows.
// Run: ARMAX_PERSIST_BENCH=1 go test ./core -run TestPersistenceBaseline -v -timeout 30m
// The chain is synthetic (random hashes, one small tx per block); no consensus code is touched.

func synthBlock(r *rand.Rand, idx int, prev string) Block {
	h := make([]byte, 32)
	r.Read(h)
	hash := fmt.Sprintf("%x", h)
	tx := Transaction{
		Sender: "COINBASE", Recipient: fmt.Sprintf("armx1synthetic%030d", idx%1000),
		Amount: 5000000000, Nonce: int64(idx), Signature: "", SigAlg: 1,
	}
	return Block{
		Index: idx, Timestamp: 1700000000 + int64(idx)*120, Transactions: []Transaction{tx},
		PrevHash: prev, MerkleRoot: hash, Hash: hash, Nonce: r.Int63(),
		Miner: "armx1synthetic", Difficulty: 8,
		Target: "0000000100000000000000000000000000000000000000000000000000000000",
	}
}

func TestPersistenceBaseline(t *testing.T) {
	if os.Getenv("ARMAX_PERSIST_BENCH") == "" {
		t.Skip("set ARMAX_PERSIST_BENCH=1 to run the persistence baseline")
	}
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	t.Logf("%8s | %12s | %16s | %14s | %10s", "blocks", "initial save", "save +1 block", "save (no-op)", "db size")
	for _, n := range []int{1000, 10000, 50000, 100000} {
		path := filepath.Join(t.TempDir(), "bench.db")
		bc, err := NewBlockchainWithStoragePath(1, path)
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewSource(1))
		prev := bc.Chain[len(bc.Chain)-1].Hash
		for i := len(bc.Chain); i < n; i++ {
			b := synthBlock(r, i, prev)
			prev = b.Hash
			bc.Chain = append(bc.Chain, b)
		}
		t0 := time.Now()
		if err := bc.SaveToFile(); err != nil {
			t.Fatal(err)
		}
		initial := time.Since(t0)

		var worst, total time.Duration
		const reps = 5
		for k := 0; k < reps; k++ {
			b := synthBlock(r, len(bc.Chain), prev)
			prev = b.Hash
			bc.Chain = append(bc.Chain, b)
			t1 := time.Now()
			if err := bc.SaveToFile(); err != nil {
				t.Fatal(err)
			}
			d := time.Since(t1)
			total += d
			if d > worst {
				worst = d
			}
		}
		t2 := time.Now()
		_ = bc.SaveToFile()
		noop := time.Since(t2)

		fi, _ := os.Stat(path)
		_ = bc.Close()
		t.Logf("%8d | %12s | %9s (max %s) | %14s | %7.1f MB",
			n, initial.Round(time.Millisecond), (total / reps).Round(time.Millisecond),
			worst.Round(time.Millisecond), noop.Round(time.Millisecond), float64(fi.Size())/1e6)
	}
}
