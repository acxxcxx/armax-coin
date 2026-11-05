package core

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestRecordCodecRoundTrip(t *testing.T) {
	SetStoreCompression(true)
	defer SetStoreCompression(false)

	big := bytes.Repeat([]byte(`{"a":"0123456789abcdef"}`), 200)
	enc := encodeRecord(big)
	if len(enc) == 0 || enc[0] != recordMarkerFlate || len(enc) >= len(big) {
		t.Fatalf("large compressible record should be compressed (got %d from %d bytes)", len(enc), len(big))
	}
	dec, err := decodeRecord(enc)
	if err != nil || !bytes.Equal(dec, big) {
		t.Fatalf("round trip failed: %v", err)
	}

	small := []byte(`{"a":1}`)
	if !bytes.Equal(encodeRecord(small), small) {
		t.Fatal("small records must stay plain JSON")
	}

	random := make([]byte, 4096)
	_, _ = rand.Read(random)
	random[0] = '{'
	if !bytes.Equal(encodeRecord(random), random) {
		t.Fatal("incompressible records must stay plain")
	}

	// Plain JSON (v1.0.6 form) is returned untouched.
	if out, err := decodeRecord(small); err != nil || !bytes.Equal(out, small) {
		t.Fatal("plain record must pass through decodeRecord")
	}
	if _, err := decodeRecord([]byte{recordMarkerFlate, 0xff, 0xff, 0xff}); err == nil {
		t.Fatal("corrupt compressed record must return an error")
	}
}

func recordForms(t *testing.T, path string) (plain, compressed int) {
	t.Helper()
	db, err := OpenStoreReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = db.View(func(tx *bolt.Tx) error {
		for _, n := range [][]byte{bktBlocks, bktUndo, bktSnapshots} {
			_ = tx.Bucket(n).ForEach(func(k, v []byte) error {
				if len(v) > 0 && v[0] == recordMarkerFlate {
					compressed++
				} else {
					plain++
				}
				return nil
			})
		}
		return nil
	})
	return
}

// TestStoreCompressionMixedAndDowngrade covers the compatibility promises: a plain database keeps
// loading after compression is switched on, compressed and plain records can coexist, and
// RewriteStoreRecords(false) restores a store identical to one that never used compression.
func TestStoreCompressionMixedAndDowngrade(t *testing.T) {
	setupC24EasyTarget(t)
	defer SetStoreCompression(false)

	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)
	SetStoreCompression(false)
	bc := NewBlockchain(1)
	path := GetDBPath()
	bc.StoragePath = path
	for i := 0; i < 12; i++ {
		if _, err := bc.MineBlockWithErr(GenesisMiner); err != nil {
			t.Fatal(err)
		}
	}
	if err := bc.SaveToFile(); err != nil {
		t.Fatal(err)
	}
	tipPlain := bc.GetLatestBlock().Hash
	_ = bc.Close()
	if _, c := recordForms(t, path); c != 0 {
		t.Fatalf("compression off must write no compressed records, found %d", c)
	}
	plainRef := dumpStoreFile(t, path)

	// Switch compression on: the old plain database still loads, and new blocks are compressed.
	SetStoreCompression(true)
	loaded, found, err := loadBlockchainFromPath(path)
	if err != nil || !found || loaded.GetLatestBlock().Hash != tipPlain {
		t.Fatalf("plain database must load with compression on: found=%v err=%v", found, err)
	}
	loaded.StoragePath = path
	for i := 0; i < 6; i++ {
		if _, err := loaded.MineBlockWithErr(GenesisMiner); err != nil {
			t.Fatal(err)
		}
	}
	tipMixed := loaded.GetLatestBlock().Hash
	_ = loaded.Close()
	p, c := recordForms(t, path)
	if c == 0 || p == 0 {
		t.Fatalf("expected a mix of plain and compressed records, got plain=%d compressed=%d", p, c)
	}

	reloaded, found, err := loadBlockchainFromPath(path)
	if err != nil || !found || reloaded.GetLatestBlock().Hash != tipMixed {
		t.Fatalf("mixed database must load: found=%v err=%v", found, err)
	}
	if tx := reloaded.GetLatestBlock().Transactions; len(tx) == 0 {
		t.Fatal("block content lost")
	}
	_ = reloaded.Close()

	// Downgrade path: everything back to plain JSON, still loads, no compressed record left.
	n, err := RewriteStoreRecords(path, false)
	if err != nil || n == 0 {
		t.Fatalf("RewriteStoreRecords(false): changed=%d err=%v", n, err)
	}
	if _, c := recordForms(t, path); c != 0 {
		t.Fatalf("downgrade left %d compressed records", c)
	}
	back, found, err := loadBlockchainFromPath(path)
	if err != nil || !found || back.GetLatestBlock().Hash != tipMixed {
		t.Fatalf("database must load after downgrade: found=%v err=%v", found, err)
	}
	_ = back.Close()

	// And the original blocks are byte-identical to the never-compressed store.
	now := dumpStoreFile(t, path)
	for k, v := range plainRef {
		if now[k] != v {
			t.Fatalf("record %x changed by the compress/downgrade cycle", k)
		}
	}

	// Full compression pass and back again is lossless.
	if _, err := RewriteStoreRecords(path, true); err != nil {
		t.Fatal(err)
	}
	if _, c := recordForms(t, path); c == 0 {
		t.Fatal("RewriteStoreRecords(true) compressed nothing")
	}
	again, found, err := loadBlockchainFromPath(path)
	if err != nil || !found || again.GetLatestBlock().Hash != tipMixed {
		t.Fatalf("fully compressed database must load: found=%v err=%v", found, err)
	}
	_ = again.Close()
}

func dumpStoreFile(t *testing.T, path string) map[string]string {
	t.Helper()
	db, err := OpenStoreReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out := map[string]string{}
	_ = db.View(func(tx *bolt.Tx) error {
		_ = tx.Bucket(bktBlocks).ForEach(func(k, v []byte) error {
			out[string(k)] = string(v)
			return nil
		})
		return nil
	})
	return out
}

// TestStoreCompressionRatio logs the saving on records shaped like real ML-DSA-65 transactions.
func TestStoreCompressionRatio(t *testing.T) {
	SetStoreCompression(true)
	defer SetStoreCompression(false)

	sig := make([]byte, 3309)
	pub := make([]byte, 1952)
	_, _ = rand.Read(sig)
	_, _ = rand.Read(pub)
	txs := []Transaction{{Sender: "armx1sender", Recipient: "armx1recipient", Amount: 1, Nonce: 1,
		Signature: hex.EncodeToString(sig), SenderPub: hex.EncodeToString(pub), SigAlg: 1}}
	withTx := Block{Index: 5, Transactions: txs, Miner: "armx1m", Hash: "00", PrevHash: "00"}
	coinbase := Block{Index: 6, Transactions: []Transaction{{Sender: "SYSTEM", Recipient: "armx1m", Amount: InitialReward, Signature: "REWARD_SIG"}},
		Miner: "armx1m", Hash: "00", PrevHash: "00", Target: "0000000100000000000000000000000000000000000000000000000000000000"}
	for name, b := range map[string]Block{"block with one signed tx": withTx, "coinbase-only block": coinbase} {
		raw, _ := json.Marshal(b)
		enc := encodeRecord(raw)
		t.Logf("%-26s plain=%5d B stored=%5d B (%.0f%%)", name, len(raw), len(enc), 100*float64(len(enc))/float64(len(raw)))
	}
}
