package core

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bboltErrors "go.etcd.io/bbolt/errors"
)

const (
	// SnapshotInterval is the cadence in blocks for full state checkpoints [C-25]
	SnapshotInterval = 1000

	// UndoRecordRetention is the number of past blocks for which undo records are preserved [C-25]
	UndoRecordRetention = 2 * MaxReorganizationDepth // 1440 blocks

	// CurrentSchemaVersion is the bbolt database schema version [P2-6, L4-10-a]
	// Version 3: Consensus-enforced coinbase maturity queue in AccountState schema
	CurrentSchemaVersion = uint64(3)
)

var (
	bktBlocks    = []byte("blocks")
	bktHashIndex = []byte("hash_index")
	bktTxIndex   = []byte("tx_index")
	bktChainwork = []byte("chainwork")
	bktSnapshots = []byte("state_snapshots")
	bktUndo      = []byte("undo_records")
	bktMeta      = []byte("meta")

	keyTipHeight     = []byte("tip_height")
	keySchemaVersion = []byte("schema_version")
)

// BlockUndoRecord stores account states before a block was applied, allowing rapid rollback [C-24, C-25]
type BlockUndoRecord struct {
	Height   int                     `json:"height"`
	PrevAccs map[string]AccountState `json:"prev_accs"` // Accounts modified by this block, stored as they were before
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func bytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func encodeTxLocation(height uint64, txIdx uint64) []byte {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], height)
	binary.BigEndian.PutUint64(buf[8:16], txIdx)
	return buf
}

// OpenStore opens or initializes the bbolt database and creates required buckets [C-25]
func OpenStore(path string) (*bolt.DB, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{
		Timeout: 3 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		buckets := [][]byte{
			bktBlocks,
			bktHashIndex,
			bktTxIndex,
			bktChainwork,
			bktSnapshots,
			bktUndo,
			bktMeta,
		}

		for _, bkt := range buckets {
			if _, err := tx.CreateBucketIfNotExists(bkt); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize store buckets: %w", err)
	}

	return db, nil
}

// MarkDBInvalid renames an invalid database so it is preserved for analysis.
func MarkDBInvalid(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat database %q: %w", dbPath, err)
	}

	invalidName := fmt.Sprintf(
		"%s.invalid-%d",
		dbPath,
		time.Now().UnixNano(),
	)

	if err := os.Rename(dbPath, invalidName); err != nil {
		return fmt.Errorf(
			"failed to quarantine database %q as %q: %w",
			dbPath,
			invalidName,
			err,
		)
	}

	fmt.Printf(
		"[!] Renamed invalid database %s -> %s\n",
		dbPath,
		invalidName,
	)
	return nil
}

// getStoreDB returns the active bbolt database instance, opening and caching it on the blockchain if needed [C-25]
func (bc *Blockchain) getStoreDB() (*bolt.DB, error) {
	if bc.db != nil {
		return bc.db, nil
	}
	if bc.storeFailed {
		return nil, fmt.Errorf("persistent store is disabled or unavailable")
	}
	path := bc.StoragePath
	if path == ":memory:" || path == "IN_MEMORY" {
		bc.storeFailed = true
		return nil, fmt.Errorf("in-memory mode")
	}
	if path == "" {
		path = GetDBPath()
	}
	db, err := OpenStore(path)
	if err != nil {
		if errors.Is(err, bboltErrors.ErrInvalid) {
			if quarantineErr := MarkDBInvalid(path); quarantineErr != nil {
				return nil, fmt.Errorf(
					"open database %q: %w; quarantine failed: %v",
					path,
					err,
					quarantineErr,
				)
			}
		}

		return nil, fmt.Errorf(
			"open database %q: %w",
			path,
			err,
		)
	}
	bc.db = db
	return bc.db, nil
}

// SaveBlockToStore appends a single new block and its index metadata to the store atomically [C-25]
func (bc *Blockchain) SaveBlockToStore(b *Block, undo *BlockUndoRecord) error {
	if b == nil {
		return fmt.Errorf("cannot store nil block")
	}
	calcHash, err := b.CalculateHashWithError()
	if err != nil {
		return fmt.Errorf("cannot store block #%d: hash computation failed: %w", b.Index, err)
	}
	if b.Hash == "" || b.Hash != calcHash {
		return fmt.Errorf("cannot store block #%d: hash mismatch (got %s, calculated %s)", b.Index, b.Hash, calcHash)
	}

	db, err := bc.getStoreDB()
	if err != nil {
		return fmt.Errorf("failed to get store: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		hBytes := uint64ToBytes(uint64(b.Index))

		// 1. Serialize block
		bData, err := json.Marshal(b)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bktBlocks).Put(hBytes, bData); err != nil {
			return err
		}

		// 2. Hash index
		if err := tx.Bucket(bktHashIndex).Put([]byte(b.Hash), hBytes); err != nil {
			return err
		}

		// 3. Tx index
		for txIdx, t := range b.Transactions {
			loc := encodeTxLocation(uint64(b.Index), uint64(txIdx))
			if err := tx.Bucket(bktTxIndex).Put([]byte(t.HashHex()), loc); err != nil {
				return err
			}
		}

		// 4. Persist the already-computed cumulative chainwork.
		// The caller owns the cache update; storage must not increment it again.
		work := bc.cachedWork
		if work == nil {
			work = CalculateChainwork(bc.Chain)
		}
		if work == nil || work.Sign() <= 0 {
			return fmt.Errorf("invalid cumulative chainwork at block #%d", b.Index)
		}

		workBytes := append([]byte(nil), work.Bytes()...)
		if err := tx.Bucket(bktChainwork).Put(hBytes, workBytes); err != nil {
			return err
		}

		// 5. Serialize undo record in the transaction.
		// Update the in-memory undo cache only after the transaction commits.
		var undoData []byte
		if undo != nil {
			undoData, err = json.Marshal(undo)
			if err != nil {
				return fmt.Errorf("marshal undo record for block #%d: %w", b.Index, err)
			}
			if err := tx.Bucket(bktUndo).Put(hBytes, undoData); err != nil {
				return fmt.Errorf("persist undo record for block #%d: %w", b.Index, err)
			}
		}

		// 6. State snapshot cadence
		if b.Index%SnapshotInterval == 0 && bc.State != nil {
			sData, err := json.Marshal(bc.State)
			if err != nil {
				return fmt.Errorf("marshal state snapshot for block #%d: %w", b.Index, err)
			}
			if err := tx.Bucket(bktSnapshots).Put(hBytes, sData); err != nil {
				return fmt.Errorf("persist state snapshot for block #%d: %w", b.Index, err)
			}
		}

		// 7. Update tip height
		if err := tx.Bucket(bktMeta).Put(keyTipHeight, hBytes); err != nil {
			return err
		}

		// 8. Prune old undo records past retention horizon
		if b.Index > UndoRecordRetention {
			pruneHeight := b.Index - UndoRecordRetention
			pBytes := uint64ToBytes(uint64(pruneHeight))
			_ = tx.Bucket(bktUndo).Delete(pBytes)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if undo != nil {
		if bc.undoHistory == nil {
			bc.undoHistory = make(map[int]*BlockUndoRecord)
		}
		bc.undoHistory[b.Index] = undo
		if b.Index > UndoRecordRetention {
			delete(bc.undoHistory, b.Index-UndoRecordRetention)
		}
	}

	return nil
}

// GetBlockUndoRecord retrieves an undo record from in-memory cache or persistent bbolt store [C-24]
func (bc *Blockchain) GetBlockUndoRecord(height int) (*BlockUndoRecord, error) {
	if bc.undoHistory != nil {
		if u, ok := bc.undoHistory[height]; ok && u != nil {
			return u, nil
		}
	}
	if bc.db != nil {
		var undo *BlockUndoRecord
		err := bc.db.View(func(tx *bolt.Tx) error {
			bkt := tx.Bucket(bktUndo)
			if bkt == nil {
				return fmt.Errorf("undo bucket missing")
			}
			data := bkt.Get(uint64ToBytes(uint64(height)))
			if data == nil {
				return fmt.Errorf("undo record for block %d not found", height)
			}
			var u BlockUndoRecord
			if err := json.Unmarshal(data, &u); err != nil {
				return err
			}
			undo = &u
			return nil
		})
		if err == nil && undo != nil {
			return undo, nil
		}
	}
	return nil, fmt.Errorf("undo record for block %d not found", height)
}

func decodeTxLocation(buf []byte) (uint64, uint64, error) {
	if len(buf) < 16 {
		return 0, 0, fmt.Errorf("invalid tx location length: %d", len(buf))
	}
	h := binary.BigEndian.Uint64(buf[0:8])
	idx := binary.BigEndian.Uint64(buf[8:16])
	return h, idx, nil
}

// GetBlockByHash retrieves a block by its block hash using bktHashIndex in O(1) [N-21, C-25]
func (bc *Blockchain) GetBlockByHash(hash string) (*Block, error) {
	cleanHash := strings.ToLower(strings.TrimSpace(hash))

	// 1. Check in-memory chain under RLock
	bc.Lock.RLock()
	for i := len(bc.Chain) - 1; i >= 0; i-- {
		if strings.EqualFold(bc.Chain[i].Hash, cleanHash) {
			b := bc.Chain[i]
			bc.Lock.RUnlock()
			return &b, nil
		}
	}
	bc.Lock.RUnlock()

	// 2. Query persistent store bktHashIndex
	db, err := bc.getStoreDB()
	if err != nil {
		return nil, fmt.Errorf("block with hash %s not found", hash)
	}

	var found *Block
	err = db.View(func(tx *bolt.Tx) error {
		bktH := tx.Bucket(bktHashIndex)
		if bktH == nil {
			return fmt.Errorf("hash index bucket missing")
		}
		hBytes := bktH.Get([]byte(cleanHash))
		if hBytes == nil {
			hBytes = bktH.Get([]byte(hash))
		}
		if hBytes == nil {
			return fmt.Errorf("block hash not found")
		}

		bktB := tx.Bucket(bktBlocks)
		if bktB == nil {
			return fmt.Errorf("blocks bucket missing")
		}
		bData := bktB.Get(hBytes)
		if bData == nil {
			return fmt.Errorf("block data missing for height")
		}
		var b Block
		if err := json.Unmarshal(bData, &b); err != nil {
			return err
		}
		found = &b
		return nil
	})

	if err == nil && found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("block with hash %s not found", hash)
}

// GetTransactionByHash retrieves a transaction, its block index, timestamp, and block hash in O(1) using bktTxIndex [N-21, C-25]
func (bc *Blockchain) GetTransactionByHash(txHash string) (*Transaction, int, int64, string, error) {
	cleanHash := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(txHash), "0x"))

	// 1. Check mempool first under RLock
	bc.Lock.RLock()
	for _, tx := range bc.Mempool {
		if strings.EqualFold(tx.HashHex(), cleanHash) {
			tCopy := tx
			bc.Lock.RUnlock()
			return &tCopy, -1, time.Now().Unix(), "", nil
		}
	}
	bc.Lock.RUnlock()

	// 2. Query persistent store bktTxIndex
	db, err := bc.getStoreDB()
	if err == nil {
		var foundTx *Transaction
		var blockIdx int = -1
		var blockTimestamp int64 = 0
		var blockHash string

		viewErr := db.View(func(tx *bolt.Tx) error {
			bktT := tx.Bucket(bktTxIndex)
			if bktT == nil {
				return fmt.Errorf("tx index missing")
			}
			locBytes := bktT.Get([]byte(cleanHash))
			if locBytes == nil {
				return fmt.Errorf("tx not found in index")
			}
			h, txIdx, err := decodeTxLocation(locBytes)
			if err != nil {
				return err
			}

			bktB := tx.Bucket(bktBlocks)
			if bktB == nil {
				return fmt.Errorf("blocks bucket missing")
			}
			bData := bktB.Get(uint64ToBytes(h))
			if bData == nil {
				return fmt.Errorf("block not found for height %d", h)
			}
			var b Block
			if err := json.Unmarshal(bData, &b); err != nil {
				return err
			}
			if int(txIdx) >= len(b.Transactions) {
				return fmt.Errorf("transaction index out of bounds")
			}
			tCopy := b.Transactions[txIdx]
			foundTx = &tCopy
			blockIdx = b.Index
			blockTimestamp = b.Timestamp
			blockHash = b.Hash
			return nil
		})

		if viewErr == nil && foundTx != nil {
			return foundTx, blockIdx, blockTimestamp, blockHash, nil
		}
	}

	// 3. Fallback: Search in-memory chain under RLock
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	for i := len(bc.Chain) - 1; i >= 0; i-- {
		b := bc.Chain[i]
		for _, t := range b.Transactions {
			if strings.EqualFold(t.HashHex(), cleanHash) {
				tCopy := t
				return &tCopy, b.Index, b.Timestamp, b.Hash, nil
			}
		}
	}

	return nil, -1, 0, "", fmt.Errorf("transaction %s not found", txHash)
}
