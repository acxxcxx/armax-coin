package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	bboltErrors "go.etcd.io/bbolt/errors"
)

const DBFileName = "blockchain.db"

var (
	dbPathMu     sync.RWMutex
	customDBPath string
)

// SetDBPath explicitly sets a custom database file location
func SetDBPath(path string) {
	dbPathMu.Lock()
	customDBPath = path
	dbPathMu.Unlock()
}

// GetDBPath determines the absolute path for blockchain.db.
// Testnet profiles are strictly isolated into a <datadir>/testnet/ subfolder [L1-3].
func GetDBPath() string {
	dbPathMu.RLock()
	cPath := customDBPath
	dbPathMu.RUnlock()
	if cPath != "" {
		return cPath
	}

	subDir := ""
	if netName := GetActiveNetwork().Name; netName == "testnet" || netName == "regtest" || netName == "soak" {
		subDir = netName
	}

	// 1. Check environment variable override
	if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
		targetDir := envDir
		if subDir != "" && filepath.Base(envDir) != subDir {
			targetDir = filepath.Join(envDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0755)
		return filepath.Join(targetDir, DBFileName)
	}

	// 2. Check if blockchain.db already exists next to the running executable
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		if subDir != "" {
			execDir = filepath.Join(execDir, subDir)
			_ = os.MkdirAll(execDir, 0755)
		}
		candidate := filepath.Join(execDir, DBFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 3. Check if blockchain.db exists in current working directory
	baseDir := "."
	if subDir != "" {
		baseDir = subDir
		_ = os.MkdirAll(baseDir, 0755)
	}
	candidate := filepath.Join(baseDir, DBFileName)
	if _, err := os.Stat(candidate); err == nil {
		if abs, err := filepath.Abs(candidate); err == nil {
			return abs
		}
		return candidate
	}

	// 4. Default: Save next to executable
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		if subDir != "" {
			execDir = filepath.Join(execDir, subDir)
			_ = os.MkdirAll(execDir, 0755)
		}
		return filepath.Join(execDir, DBFileName)
	}

	return candidate
}

// BackupAndResetDatabase safely archives the active blockchain database file into a timestamped backup
// and removes the active file, allowing clean resynchronization from Genesis [Phase 4].
func BackupAndResetDatabase() (string, error) {
	dbPath := GetDBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", nil // No database file exists
	}
	backupPath := fmt.Sprintf("%s.bak.%d", dbPath, time.Now().Unix())
	if err := os.Rename(dbPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to backup existing database file '%s' to '%s': %w", dbPath, backupPath, err)
	}
	return backupPath, nil
}

// SaveToFile safely persists the blockchain to bbolt store [C-25]
func (bc *Blockchain) SaveToFile() error {
	db, err := bc.getStoreDB()
	if err != nil {
		return fmt.Errorf("failed to get bbolt store: %w", err)
	}

	return db.Update(func(tx *bolt.Tx) error {
		bktB := tx.Bucket(bktBlocks)
		bktH := tx.Bucket(bktHashIndex)
		bktT := tx.Bucket(bktTxIndex)
		bktW := tx.Bucket(bktChainwork)
		bktS := tx.Bucket(bktSnapshots)
		bktU := tx.Bucket(bktUndo)
		bktM := tx.Bucket(bktMeta)

		runningWork := big.NewInt(0)
		two256 := new(big.Int).Lsh(big.NewInt(1), 256)
		reorgStartHeight := -1

		for i := 0; i < len(bc.Chain); i++ {
			b := &bc.Chain[i]
			hBytes := uint64ToBytes(uint64(b.Index))

			target := b.GetTarget()
			if target == nil || target.Sign() <= 0 {
				target = DifficultyToTarget(1)
			}
			divisor := new(big.Int).Add(target, big.NewInt(1))
			work := new(big.Int).Div(two256, divisor)
			runningWork.Add(runningWork, work)

			needWrite := false
			existing := bktB.Get(hBytes)
			if existing == nil {
				needWrite = true
			} else {
				var oldB Block
				if err := json.Unmarshal(existing, &oldB); err != nil || oldB.Hash != b.Hash {
					needWrite = true
					if reorgStartHeight == -1 {
						reorgStartHeight = b.Index
					}
					if oldB.Hash != "" {
						_ = bktH.Delete([]byte(oldB.Hash))
					}
					for _, oldTx := range oldB.Transactions {
						_ = bktT.Delete([]byte(oldTx.HashHex()))
					}
					if bktS != nil {
						_ = bktS.Delete(hBytes)
					}
					if bktU != nil {
						_ = bktU.Delete(hBytes)
					}
				}
			}

			if needWrite {
				bData, err := json.Marshal(b)
				if err != nil {
					return err
				}
				if err := bktB.Put(hBytes, bData); err != nil {
					return err
				}
				if err := bktH.Put([]byte(b.Hash), hBytes); err != nil {
					return err
				}
				for txIdx, t := range b.Transactions {
					loc := encodeTxLocation(uint64(b.Index), uint64(txIdx))
					if err := bktT.Put([]byte(t.HashHex()), loc); err != nil {
						return err
					}
				}
				if err := bktW.Put(hBytes, append([]byte(nil), runningWork.Bytes()...)); err != nil {
					return err
				}
				if bktU != nil && bc.undoHistory != nil {
					if u, ok := bc.undoHistory[b.Index]; ok && u != nil {
						if uData, err := json.Marshal(u); err == nil {
							_ = bktU.Put(hBytes, uData)
						}
					}
				}
			}

			// Only persist bc.State snapshot at the exact chain tip height (bc.State represents tip state)
			if i == len(bc.Chain)-1 && b.Index%SnapshotInterval == 0 && bc.State != nil && bktS != nil {
				sData, err := json.Marshal(bc.State)
				if err != nil {
					return fmt.Errorf("failed to marshal state snapshot: %w", err)
				}
				if err := bktS.Put(hBytes, sData); err != nil {
					return err
				}
			} else if reorgStartHeight != -1 && b.Index >= reorgStartHeight && i != len(bc.Chain)-1 && bktS != nil {
				_ = bktS.Delete(hBytes)
			}
		}

		if bktM != nil {
			// Prune any orphaned heights above the current chain tip
			if len(bc.Chain) > 0 {
				newTip := uint64(bc.Chain[len(bc.Chain)-1].Index)
				if oldTipBytes := bktM.Get(keyTipHeight); oldTipBytes != nil {
					oldTip := bytesToUint64(oldTipBytes)
					for h := newTip + 1; h <= oldTip; h++ {
						hBytes := uint64ToBytes(h)
						if oldData := bktB.Get(hBytes); oldData != nil {
							var oldB Block
							if json.Unmarshal(oldData, &oldB) == nil {
								if oldB.Hash != "" {
									_ = bktH.Delete([]byte(oldB.Hash))
								}
								for _, oldTx := range oldB.Transactions {
									_ = bktT.Delete([]byte(oldTx.HashHex()))
								}
							}
							_ = bktB.Delete(hBytes)
						}
						_ = bktW.Delete(hBytes)
						if bktS != nil {
							_ = bktS.Delete(hBytes)
						}
						if bktU != nil {
							_ = bktU.Delete(hBytes)
						}
					}
				}
				tipBytes := uint64ToBytes(newTip)
				_ = bktM.Put(keyTipHeight, tipBytes)
			}
			_ = bktM.Put(keySchemaVersion, uint64ToBytes(CurrentSchemaVersion))
		}
		return nil
	})
}

// LoadFromFile loads the blockchain from the persistent bbolt store and validates integrity [C-14, C-23, C-25, P2-6]
func LoadFromFile() (*Blockchain, bool) {
	bc, found, err := loadBlockchainFromPath(GetDBPath())
	if err != nil {
		fmt.Printf("[-] WARNING: Could not load blockchain: %v\n", err)
	}
	return bc, found
}

func loadBlockchainFromPath(dbPath string) (*Blockchain, bool, error) {

	// If DB file doesn't exist, return false to trigger genesis initialization
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat database %q: %w", dbPath, err)
	}

	db, err := OpenStore(dbPath)
	if err != nil {
		// Quarantine only when bbolt identifies the file as an invalid database.
		// Other errors (for example, permissions or a lock timeout) should not
		// cause an otherwise valid database to be renamed.
		if errors.Is(err, bboltErrors.ErrInvalid) {
			if quarantineErr := MarkDBInvalid(dbPath); quarantineErr != nil {
				return nil, false, fmt.Errorf(
					"open database %q: %w (failed to quarantine database: %v)",
					dbPath, err, quarantineErr,
				)
			}
		}

		return nil, false, fmt.Errorf("open database %q: %w", dbPath, err)
	}

	var tipHeight uint64
	var blocks []Block

	err = db.View(func(tx *bolt.Tx) error {
		bktM := tx.Bucket(bktMeta)
		if bktM == nil {
			return fmt.Errorf("meta bucket missing")
		}

		// Enforce schema version: reject legacy ECDSA or outdated databases [P2-6]
		verBytes := bktM.Get(keySchemaVersion)
		if verBytes == nil || bytesToUint64(verBytes) < CurrentSchemaVersion {
			var oldVer uint64
			if verBytes != nil {
				oldVer = bytesToUint64(verBytes)
			}
			return fmt.Errorf("incompatible database schema version: got v%d, required v%d (legacy database rejected)", oldVer, CurrentSchemaVersion)
		}

		tipBytes := bktM.Get(keyTipHeight)
		if tipBytes == nil {
			return fmt.Errorf("tip height missing")
		}
		tipHeight = bytesToUint64(tipBytes)

		bktB := tx.Bucket(bktBlocks)
		if bktB == nil {
			return fmt.Errorf("blocks bucket missing")
		}

		blocks = make([]Block, 0, tipHeight+1)
		for h := uint64(0); h <= tipHeight; h++ {
			hBytes := uint64ToBytes(h)
			bData := bktB.Get(hBytes)
			if bData == nil {
				return fmt.Errorf("block at height %d missing", h)
			}
			var b Block
			if err := json.Unmarshal(bData, &b); err != nil {
				return fmt.Errorf("failed to unmarshal block %d: %w", h, err)
			}
			blocks = append(blocks, b)
		}
		return nil
	})

	if err != nil || len(blocks) == 0 {
		_ = db.Close()

		if err != nil {
			if strings.Contains(err.Error(), "incompatible database schema version") {
				if quarantineErr := MarkDBInvalid(dbPath); quarantineErr != nil {
					return nil, false, fmt.Errorf(
						"read blockchain from %q: %w; quarantine failed: %v",
						dbPath,
						err,
						quarantineErr,
					)
				}
			}

			return nil, false, fmt.Errorf(
				"read blockchain from %q: %w",
				dbPath,
				err,
			)
		}

		return nil, false, fmt.Errorf(
			"blockchain database %q contains no blocks",
			dbPath,
		)
	}

	// [C-23] Strictly validate Genesis block
	if err := ValidateGenesisBlock(&blocks[0]); err != nil {
		_ = db.Close()
		MarkDBInvalid(dbPath)
		return nil, false, fmt.Errorf("stored database genesis mismatch: %w", err)
	}
	blocks[0] = GetGenesisBlock()

	// Locate nearest state snapshot <= tipHeight [C-25]
	var snapshotHeight uint64 = 0
	var stateMgr *StateManager

	_ = db.View(func(tx *bolt.Tx) error {
		bktS := tx.Bucket(bktSnapshots)
		if bktS == nil {
			return nil
		}
		// Check from highest potential snapshot down
		nearestSnap := (tipHeight / SnapshotInterval) * SnapshotInterval
		for s := nearestSnap; ; s -= SnapshotInterval {
			sBytes := uint64ToBytes(s)
			sData := bktS.Get(sBytes)
			if sData != nil {
				var sm StateManager
				if err := json.Unmarshal(sData, &sm); err == nil {
					snapshotHeight = s
					stateMgr = &sm
					break
				}
			}
			if s < SnapshotInterval {
				break
			}
		}
		return nil
	})
	_ = db.Close()

	mat := GetActiveNetwork().CoinbaseMaturity
	if mat <= 0 {
		mat = CoinbaseMaturity
	}

	// Initialize state
	if stateMgr == nil {
		stateMgr = NewStateManager()
		stateMgr.CoinbaseMaturity = mat
		if err := stateMgr.VerifyAndApplyBlock(&blocks[0], InitialReward, mat); err != nil {
			_ = MarkDBInvalid(dbPath)
			return nil, false, fmt.Errorf("genesis state application failed: %w", err)
		}
		snapshotHeight = 0
	} else {
		if stateMgr.Accounts == nil {
			stateMgr.Accounts = make(map[string]AccountState)
		}
		stateMgr.CoinbaseMaturity = mat
	}

	// Calculate running supply up to snapshotHeight
	var runningSupply int64 = 0
	for h := uint64(0); h <= snapshotHeight; h++ {
		r := CalculateBlockRewardGivenSupply(int(h), runningSupply)
		runningSupply += r
	}

	// Replay and verify blocks from snapshotHeight + 1 up to tipHeight [C-25]
	for h := snapshotHeight + 1; h <= tipHeight; h++ {
		curr := &blocks[h]
		prev := &blocks[h-1]

		if err := validateBlockContext(curr, prev, blocks[:h]); err != nil {
			MarkDBInvalid(dbPath)
			return nil, false, fmt.Errorf("block #%d linkage validation failed: %w", h, err)
		}

		reward := CalculateBlockRewardGivenSupply(curr.Index, runningSupply)
		if err := stateMgr.VerifyAndApplyBlock(curr, reward, mat); err != nil {
			MarkDBInvalid(dbPath)
			return nil, false, fmt.Errorf("block #%d state execution failed: %w", h, err)
		}
		runningSupply += reward
	}

	var totalLoadedTx int64 = 0
	for _, b := range blocks {
		totalLoadedTx += int64(len(b.Transactions))
	}

	bc := &Blockchain{
		Chain:            blocks,
		Difficulty:       TargetToDifficulty(blocks[len(blocks)-1].GetTarget()),
		Mempool:          []Transaction{},
		BlockReward:      CalculateBlockRewardGivenSupply(int(tipHeight+1), runningSupply),
		State:            stateMgr,
		StoragePath:      dbPath,
		undoHistory:      make(map[int]*BlockUndoRecord),
		cachedSupply:     runningSupply,
		cachedTotalTx:    totalLoadedTx,
		cachedWork:       CalculateChainwork(blocks),
		CoinbaseMaturity: mat,
	}

	fmt.Printf("[OK] Blockchain database loaded and verified from: %s (%d blocks)\n", dbPath, len(bc.Chain))
	return bc, true, nil
}
