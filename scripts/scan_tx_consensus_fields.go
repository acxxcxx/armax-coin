package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	bolt "go.etcd.io/bbolt"
)

// buckets in bbolt
var (
	bktBlocks = []byte("blocks")
	bktMeta   = []byte("meta")
)

const (
	ScanModeGenesisOnly = "GENESIS_ONLY"
	ScanModeFullChain   = "FULL_CHAIN"
)

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

func main() {
	dbPathFlag := flag.String("db", "", "Path to blockchain.db (defaults to canonical active DB path)")
	networkFlag := flag.String("network", "testnet", "Network profile (testnet, mainnet, regtest)")
	requireFullChainFlag := flag.Bool("require-full-chain", false, "Require full chain database to exist; fail closed if absent")
	flag.Parse()

	// 1. Initialize active network profile
	if err := core.SetNetwork(*networkFlag); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Failed to set active network: %v\n", err)
		os.Exit(1)
	}
	activeNet := core.GetActiveNetwork()

	// 2. Resolve database path
	dbPath := *dbPathFlag
	if dbPath == "" {
		dbPath = core.GetDBPath()
	}

	// 3. Obtain git commit / tree SHA running the scan
	gitCommit := "unknown"
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if out, err := cmd.Output(); err == nil {
		gitCommit = strings.TrimSpace(string(out))
	}

	fmt.Println("================================================================================")
	fmt.Println(" ARMAXCOIN CANONICAL CHAIN CONSENSUS SCANNER")
	fmt.Println("================================================================================")
	fmt.Printf("Network Name       : %s (ID: %s)\n", activeNet.Name, activeNet.NetworkID)
	fmt.Printf("Git Commit SHA     : %s\n", gitCommit)
	fmt.Printf("Target Database    : %s\n", dbPath)
	fmt.Printf("Require Full Chain : %t\n", *requireFullChainFlag)
	fmt.Println("--------------------------------------------------------------------------------")

	// If DB does not exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if *requireFullChainFlag {
			fmt.Fprintf(os.Stderr, "[-] ERROR: --require-full-chain specified, but no database exists at %s\n", dbPath)
			os.Exit(1)
		}
		fmt.Printf("[i] No persistent database found at %s.\n", dbPath)
		fmt.Println("[+] Running in GENESIS_ONLY mode against hardcoded canonical Genesis Block...")
		genesis := core.GetGenesisBlock()
		scanGenesisOnly(&genesis, activeNet.NetworkID, gitCommit)
		return
	}

	// Open database in Read-Only mode with strict timeout to avoid locking active nodes
	opts := &bolt.Options{
		ReadOnly: true,
		Timeout:  5 * time.Second,
	}
	db, err := bolt.Open(dbPath, 0600, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var totalBlocks uint64
	var totalTxs uint64
	var violations []string
	var tipHash string
	var prevHash string

	err = db.View(func(tx *bolt.Tx) error {
		// Required bucket validation [P1 Review]
		bktM := tx.Bucket(bktMeta)
		if bktM == nil {
			return fmt.Errorf("required metadata bucket 'meta' not found in database")
		}
		bktB := tx.Bucket(bktBlocks)
		if bktB == nil {
			return fmt.Errorf("required blocks bucket 'blocks' not found in database")
		}

		// Tip height validation [P1 Review]
		tipBytes := bktM.Get([]byte("tip_height"))
		if tipBytes == nil {
			return fmt.Errorf("tip_height key not found in meta bucket")
		}
		if len(tipBytes) < 8 {
			return fmt.Errorf("tip_height key in meta bucket is malformed (expected 8 bytes, got %d)", len(tipBytes))
		}
		tipHeight := bytesToUint64(tipBytes)
		totalBlocks = tipHeight + 1

		// Network/profile context verification if stored in meta [P1 Review]
		if netBytes := bktM.Get([]byte("network_id")); netBytes != nil {
			storedNetID := string(netBytes)
			if storedNetID != "" && storedNetID != activeNet.NetworkID {
				return fmt.Errorf("network ID mismatch: database contains data for network %q, but scanner configured for %q", storedNetID, activeNet.NetworkID)
			}
		}

		// Block continuity and consensus field inspection [P1 Review]
		for h := uint64(0); h <= tipHeight; h++ {
			blockData := bktB.Get(uint64ToBytes(h))
			if blockData == nil {
				return fmt.Errorf("block continuity error: missing block at height %d (tip height is %d)", h, tipHeight)
			}

			var b core.Block
			if err := json.Unmarshal(blockData, &b); err != nil {
				return fmt.Errorf("block at height %d is corrupted / failed to unmarshal: %w", h, err)
			}

			// Validate block height continuity
			if b.Index != int(h) {
				return fmt.Errorf("block height continuity error at index %d: block payload declares index %d", h, b.Index)
			}

			// Validate genesis hash against active network profile at height 0
			if h == 0 {
				expectedGen := core.GetGenesisBlock()
				if b.Hash != expectedGen.Hash {
					return fmt.Errorf("genesis block mismatch: database genesis hash %s does not match active network profile %s genesis %s", b.Hash, activeNet.Name, expectedGen.Hash)
				}
			} else {
				// Validate parent block continuity
				if b.PrevHash != prevHash {
					return fmt.Errorf("block continuity error at height %d: PrevHash %s does not match predecessor hash %s", h, b.PrevHash, prevHash)
				}
			}
			prevHash = b.Hash

			if h == tipHeight {
				tipHash = b.Hash
			}

			for txIdx, t := range b.Transactions {
				totalTxs++
				// Check for consensus violations:
				// STANDARD transaction with HashLock != "", TimeLock != 0, or Preimage != ""
				hasHTLCMetadata := false
				var issues []string

				if t.Type == "" || t.Type == core.TxTypeStandard {
					if t.HashLock != "" {
						hasHTLCMetadata = true
						issues = append(issues, fmt.Sprintf("HashLock=%s", t.HashLock))
					}
					if t.TimeLock != 0 {
						hasHTLCMetadata = true
						issues = append(issues, fmt.Sprintf("TimeLock=%d", t.TimeLock))
					}
					if t.Preimage != "" {
						hasHTLCMetadata = true
						issues = append(issues, fmt.Sprintf("Preimage=%s", t.Preimage))
					}
				} else {
					hasHTLCMetadata = true
					issues = append(issues, fmt.Sprintf("NonStandardType=%s", t.Type))
				}

				if hasHTLCMetadata {
					vMsg := fmt.Sprintf("Block #%d (Tx #%d: %s): %s", h, txIdx, t.HashHex(), strings.Join(issues, ", "))
					violations = append(violations, vMsg)
				}
			}
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Database scan error: %v\n", err)
		os.Exit(1)
	}

	exitCode := printSummary(ScanModeFullChain, activeNet.NetworkID, 0, int(totalBlocks-1), tipHash, int(totalBlocks), int(totalTxs), violations, gitCommit)
	os.Exit(exitCode)
}

func scanGenesisOnly(genesis *core.Block, netID, gitCommit string) {
	totalTxs := len(genesis.Transactions)
	var violations []string

	for txIdx, t := range genesis.Transactions {
		var issues []string
		if t.Type == "" || t.Type == core.TxTypeStandard {
			if t.HashLock != "" {
				issues = append(issues, "HashLock="+t.HashLock)
			}
			if t.TimeLock != 0 {
				issues = append(issues, fmt.Sprintf("TimeLock=%d", t.TimeLock))
			}
			if t.Preimage != "" {
				issues = append(issues, "Preimage="+t.Preimage)
			}
		} else {
			issues = append(issues, "NonStandardType="+t.Type)
		}
		if len(issues) > 0 {
			violations = append(violations, fmt.Sprintf("Genesis Tx #%d (%s): %s", txIdx, t.HashHex(), strings.Join(issues, ", ")))
		}
	}

	exitCode := printSummary(ScanModeGenesisOnly, netID, 0, 0, genesis.Hash, 1, totalTxs, violations, gitCommit)
	os.Exit(exitCode)
}

func printSummary(mode, netID string, minH, maxH int, tipHash string, totalBlocks, totalTxs int, violations []string, gitCommit string) int {
	fmt.Println("================================================================================")
	fmt.Println(" SCAN SUMMARY & CONSENSUS AUDIT VERIFICATION")
	fmt.Println("================================================================================")
	fmt.Printf("Scan Mode            : %s\n", mode)
	fmt.Printf("Network ID           : %s\n", netID)
	fmt.Printf("Canonical Block Range: Height %d to %d\n", minH, maxH)
	fmt.Printf("Tip Block Hash       : %s\n", tipHash)
	fmt.Printf("Total Blocks Scanned : %d\n", totalBlocks)
	fmt.Printf("Total Txs Scanned    : %d\n", totalTxs)
	fmt.Printf("Violations / HTLC Txs: %d\n", len(violations))
	fmt.Println("--------------------------------------------------------------------------------")

	if len(violations) > 0 {
		fmt.Printf("ERROR RESULT: %d non-standard consensus violations detected: \n", len(violations))
		for _, v := range violations {
			fmt.Printf("   [!] %s\n", v)
		}
		fmt.Println("================================================================================")
		return 1
	}

	if mode == ScanModeGenesisOnly {
		fmt.Println("WARNING  RESULT: GENESIS ONLY -- FULL-CHAIN COMPATIBILITY NOT VERIFIED")
		fmt.Println("   - 0 violations found in hardcoded Genesis Block.")
		fmt.Println("   - No historical database was scanned.")
		fmt.Println("   - Full-chain backward compatibility cannot be verified without a canonical DB.")
		fmt.Println("   - Pass --db=<path> to verify full historical chain database.")
		fmt.Println("================================================================================")
		return 0
	}

	fmt.Println(" RESULT: 0 violations found across all canonical blocks.")
	fmt.Println("   - Every transaction in the audited chain is strictly standard.")
	fmt.Println("   - No historical transaction populated HashLock, TimeLock, or Preimage.")
	fmt.Println("   - Full chain continuity (heights 0 to tip) and parent links verified.")
	fmt.Println("   - The v1.0.0 validity tightening is backward-compatible with")
	fmt.Println("     all audited canonical blocks and transaction history.")
	fmt.Println("================================================================================")
	return 0
}
