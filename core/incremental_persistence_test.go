package core

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	altMinerA = "armx1psmx4e6yhg84z2h5wadg3698vjlx06flnynhec08fv8nv56z758sl9xl6c"
	altMinerB = GenesisMiner
)

func makeForkBlocks(bc *Blockchain, prev Block, n int, miner string) []Block {
	var out []Block
	for i := 0; i < n; i++ {
		idx := prev.Index + 1
		target := bc.CalculateLWMA1()
		b := *NewBlock([]Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    InitialReward,
			Nonce:     int64(idx),
			Signature: "REWARD_SIG",
		}}, prev.Hash, idx, miner, target)
		b.Timestamp = prev.Timestamp + int64(TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		out = append(out, b)
		prev = b
	}
	return out
}

func dumpStoreBuckets(t *testing.T, bc *Blockchain, names ...[]byte) map[string]map[string]string {
	t.Helper()
	db, err := bc.getStoreDB()
	if err != nil {
		t.Fatalf("getStoreDB: %v", err)
	}
	res := map[string]map[string]string{}
	err = db.View(func(tx *bolt.Tx) error {
		for _, name := range names {
			m := map[string]string{}
			bkt := tx.Bucket(name)
			if bkt == nil {
				t.Fatalf("bucket %s missing", name)
			}
			_ = bkt.ForEach(func(k, v []byte) error {
				m[string(k)] = string(v)
				return nil
			})
			res[string(name)] = m
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func compareStores(t *testing.T, label string, got, want *Blockchain) {
	t.Helper()
	names := [][]byte{bktBlocks, bktHashIndex, bktTxIndex, bktChainwork}
	g := dumpStoreBuckets(t, got, names...)
	w := dumpStoreBuckets(t, want, names...)
	for _, n := range names {
		gm, wm := g[string(n)], w[string(n)]
		if len(gm) != len(wm) {
			t.Errorf("%s: bucket %s has %d records, full save has %d", label, n, len(gm), len(wm))
		}
		var diff []string
		for k, v := range wm {
			if gv, ok := gm[k]; !ok || !bytes.Equal([]byte(gv), []byte(v)) {
				diff = append(diff, k)
			}
		}
		sort.Strings(diff)
		if len(diff) > 0 {
			t.Errorf("%s: bucket %s differs from full save on %d keys", label, n, len(diff))
		}
	}
	gTip := dumpStoreBuckets(t, got, bktMeta)[string(bktMeta)][string(keyTipHeight)]
	wTip := dumpStoreBuckets(t, want, bktMeta)[string(bktMeta)][string(keyTipHeight)]
	if gTip != wTip {
		t.Errorf("%s: tip height record differs", label)
	}
}

// fullSaveReference writes bc's in-memory chain into a brand-new store using the full pass.
func fullSaveReference(t *testing.T, bc *Blockchain, dir string) *Blockchain {
	t.Helper()
	refDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", refDir)
	ref := NewBlockchain(1)
	t.Setenv("ARMX_DATA_DIR", dir)
	ref.StoragePath = filepath.Join(refDir, "blockchain.db")
	ref.Chain = append([]Block(nil), bc.Chain...)
	if err := ref.SaveToFile(); err != nil {
		t.Fatalf("reference full save: %v", err)
	}
	return ref
}

// TestIncrementalReorgPersistenceMatchesFullSave checks that persisting only the heights above the
// fork ancestor leaves the same store a full pass would, across growth, reorg and restart.
func TestIncrementalReorgPersistenceMatchesFullSave(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := NewBlockchain(1)
	bc.StoragePath = filepath.Join(tempDir, "blockchain.db")
	defer bc.Close()

	for i := 0; i < 30; i++ {
		if _, err := bc.MineBlockWithErr(GenesisMiner); err != nil {
			t.Fatalf("mine %d: %v", i, err)
		}
	}

	// Sync-style catch-up: batches that extend the tip (ancestor == tip).
	ext := makeForkBlocks(bc, bc.Chain[len(bc.Chain)-1], 10, GenesisMiner)
	if err := bc.ResolveFork(len(bc.Chain)-1, ext); err != nil {
		t.Fatalf("extension: %v", err)
	}
	ref := fullSaveReference(t, bc, tempDir)
	compareStores(t, "after extension", bc, ref)
	_ = ref.Close()

	// Heavier fork replacing 8 local blocks with 12 new ones.
	ancestor := len(bc.Chain) - 1 - 8
	fork := makeForkBlocks(bc, bc.Chain[ancestor], 12, altMinerA)
	if err := bc.ResolveFork(ancestor, fork); err != nil {
		t.Fatalf("reorg: %v", err)
	}
	if len(bc.Chain) != ancestor+1+12 {
		t.Fatalf("unexpected chain length %d", len(bc.Chain))
	}
	ref = fullSaveReference(t, bc, tempDir)
	compareStores(t, "after reorg", bc, ref)
	_ = ref.Close()

	// A second reorg right after, then a full save must not change anything.
	ancestor2 := len(bc.Chain) - 1 - 3
	fork2 := makeForkBlocks(bc, bc.Chain[ancestor2], 5, altMinerB)
	if err := bc.ResolveFork(ancestor2, fork2); err != nil {
		t.Fatalf("second reorg: %v", err)
	}
	if err := bc.SaveToFile(); err != nil {
		t.Fatal(err)
	}
	ref = fullSaveReference(t, bc, tempDir)
	compareStores(t, "after second reorg + full save", bc, ref)
	_ = ref.Close()
}

// TestSyncPersistenceBenchmark (ARMAX_SYNC_BENCH=1) replays a batch-wise catch-up, the way
// RequestChainSync does, and compares it to the cost of the former full pass on the same chain.
func TestSyncPersistenceBenchmark(t *testing.T) {
	if os.Getenv("ARMAX_SYNC_BENCH") != "1" {
		t.Skip("set ARMAX_SYNC_BENCH=1 to run")
	}
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	bc := NewBlockchain(1)
	bc.StoragePath = filepath.Join(tempDir, "blockchain.db")
	defer bc.Close()

	const total, batch = 20000, 250
	start := time.Now()
	for done := 0; done < total; done += batch {
		blocks := makeForkBlocks(bc, bc.Chain[len(bc.Chain)-1], batch, GenesisMiner)
		if err := bc.ResolveFork(len(bc.Chain)-1, blocks); err != nil {
			t.Fatalf("batch at %d: %v", done, err)
		}
	}
	t.Logf("catch-up of %d blocks in %d-block batches (build + validate + incremental persist): %v", total, batch, time.Since(start))

	t0 := time.Now()
	if err := bc.SaveToFile(); err != nil {
		t.Fatal(err)
	}
	full := time.Since(t0)
	t.Logf("one full-pass save at %d blocks: %v; the former path paid this per batch (%d batches, ~half on average => ~%v)",
		len(bc.Chain), full, total/batch, full*time.Duration(total/batch)/2)
}

// TestSaveFromFallsBackWhenStoreLacksPredecessor makes sure a partial save never trusts a store
// that does not hold the block below the start height.
func TestSaveFromFallsBackWhenStoreLacksPredecessor(t *testing.T) {
	setupC24EasyTarget(t)
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc := NewBlockchain(1)
	bc.StoragePath = filepath.Join(tempDir, "blockchain.db")
	defer bc.Close()
	for i := 0; i < 10; i++ {
		if _, err := bc.MineBlockWithErr(GenesisMiner); err != nil {
			t.Fatal(err)
		}
	}

	db, err := bc.getStoreDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bktChainwork).Delete(uint64ToBytes(5)); err != nil {
			return err
		}
		for h := uint64(6); h <= 10; h++ {
			if err := tx.Bucket(bktBlocks).Delete(uint64ToBytes(h)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bc.saveToFileFrom(6); err != nil {
		t.Fatalf("partial save: %v", err)
	}
	ref := fullSaveReference(t, bc, tempDir)
	defer ref.Close()
	got := dumpStoreBuckets(t, bc, bktChainwork)[string(bktChainwork)]
	want := dumpStoreBuckets(t, ref, bktChainwork)[string(bktChainwork)]
	for h := uint64(6); h <= 10; h++ {
		key := uint64ToBytes(h)
		if got[string(key)] != want[string(key)] || got[string(key)] == "" {
			t.Fatalf("chainwork at height %d is wrong after fallback", h)
		}
	}
}
