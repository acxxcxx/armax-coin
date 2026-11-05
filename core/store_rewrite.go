package core

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// RewriteStoreRecords rewrites every block, undo and snapshot record of the closed database at
// path, compressed (compress=true) or as plain JSON (compress=false). It is the documented
// downgrade step: run it with compress=false before starting a v1.0.6 binary on a database that
// was written with compression enabled. The node must not be running. Returns the number of
// records whose representation changed.
func RewriteStoreRecords(path string, compress bool) (int, error) {
	db, err := OpenStore(path)
	if err != nil {
		return 0, fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	prev := storeCompression.Load()
	storeCompression.Store(compress)
	defer storeCompression.Store(prev)

	changed := 0
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bktBlocks, bktUndo, bktSnapshots} {
			bkt := tx.Bucket(name)
			if bkt == nil {
				continue
			}
			type kv struct{ k, v []byte }
			var updates []kv
			if err := bkt.ForEach(func(k, v []byte) error {
				raw, err := decodeRecord(v)
				if err != nil {
					return fmt.Errorf("bucket %s key %x: %w", name, k, err)
				}
				enc := raw
				if compress {
					enc = encodeRecord(raw)
				}
				if string(enc) != string(v) {
					updates = append(updates, kv{append([]byte(nil), k...), append([]byte(nil), enc...)})
				}
				return nil
			}); err != nil {
				return err
			}
			for _, u := range updates {
				if err := bkt.Put(u.k, u.v); err != nil {
					return err
				}
			}
			changed += len(updates)
		}
		return nil
	})
	return changed, err
}
