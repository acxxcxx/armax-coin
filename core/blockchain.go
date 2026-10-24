package core

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acxxcxx/armax-coin/armaxhash"
	bolt "go.etcd.io/bbolt"
)

const (
	MaxMempoolSize       = 5000
	MaxBlockTransactions = 1000                          // Maximum transactions allowed in a single block (including coinbase)
	MaxBlockSize         = 2_000_000                     // 2 MB maximum serialized block size [P2-4]
	MaxTxSize            = 20_000                        // 20 KB maximum serialized transaction size [P2-4]
	CoinUnit             = int64(100_000_000)            // 1 ARMX = 100,000,000 Armoids (8 decimal places)
	MaxSupply            = int64(126_000_000) * CoinUnit // 126 Million ARMX Cap [D-01]
	HalvingInterval      = 1_050_000                     // Reward cuts in half every 1,050,000 blocks (~4 years on 120s blocks) [D-01]
	InitialReward        = int64(60) * CoinUnit          // Starting block reward: 60 ARMX (6,000,000,000 Armoids) [D-01]
	MinRelayFee          = int64(1000)                   // Minimum relay fee: 1,000 Armoids (0.00001000 ARMX) [C-16]
	MaxTxsPerSender      = 25                            // Maximum unconfirmed pending mempool txs per sender [C-16]
	CoinbaseMaturity     = 100                           // Standard Coinbase maturity: 100 block confirmations required before coinbase rewards can be spent
)

type Blockchain struct {
	Chain            []Block                  `json:"chain"`
	Difficulty       int                      `json:"difficulty"`
	Mempool          []Transaction            `json:"mempool"`
	BlockReward      int64                    `json:"block_reward"`
	StoragePath      string                   `json:"-"`
	State            *StateManager            `json:"-"`
	cachedSupply     int64                    `json:"-"`
	cachedTotalTx    int64                    `json:"-"` // [N-21] O(1) cached cumulative transaction count
	cachedWork       *big.Int                 `json:"-"`
	db               *bolt.DB                 `json:"-"`
	undoHistory      map[int]*BlockUndoRecord `json:"-"`
	storeFailed      bool                     `json:"-"`
	pendingLock      sync.RWMutex             `json:"-"`
	pendingSpent     map[string]int64         `json:"-"`
	pendingCount     map[string]int64         `json:"-"`
	txsPerSender     map[string]int           `json:"-"`
	CoinbaseMaturity int                      `json:"-"` // Required confirmations for coinbase maturity
	MiningThreads    int                      `json:"-"`
	MiningHashrate   uint64                   `json:"-"` // atomic float64 bits of hashes per second
	Lock             sync.RWMutex             `json:"-"`
}

// SetMiningHashrate atomically updates current hashrate in hashes per second
func (bc *Blockchain) SetMiningHashrate(hps float64) {
	atomic.StoreUint64(&bc.MiningHashrate, math.Float64bits(hps))
}

// GetMiningHashrate atomically retrieves current hashrate in hashes per second
func (bc *Blockchain) GetMiningHashrate() float64 {
	bits := atomic.LoadUint64(&bc.MiningHashrate)
	return math.Float64frombits(bits)
}

// Close closes the underlying persistent bbolt store handle if opened [C-25]
func (bc *Blockchain) Close() error {
	bc.Lock.Lock()
	defer bc.Lock.Unlock()
	if bc.db != nil {
		err := bc.db.Close()
		bc.db = nil
		return err
	}
	return nil
}

// NewBlockchainWithError initializes a blockchain using the configured DB path.
func NewBlockchainWithError(difficulty int) (*Blockchain, error) {
	return NewBlockchainWithStoragePath(difficulty, "")
}

// NewBlockchainWithStoragePath initializes or loads a blockchain from an
// explicit database path. An empty path uses the configured default path.
func NewBlockchainWithStoragePath(difficulty int, storagePath string) (*Blockchain, error) {
	if storagePath == "" {
		storagePath = GetDBPath()
	}

	loaded, found, err := loadBlockchainFromPath(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load blockchain from %q: %w", storagePath, err)
	}
	if found {
		loaded.AdjustDifficulty()
		loaded.BlockReward = loaded.GetBlockReward(len(loaded.Chain))
		return loaded, nil
	}

	mat := GetActiveNetwork().CoinbaseMaturity
	if mat <= 0 {
		mat = CoinbaseMaturity
	}

	bc := &Blockchain{
		Chain:            []Block{},
		Difficulty:       difficulty,
		Mempool:          []Transaction{},
		BlockReward:      InitialReward,
		StoragePath:      storagePath,
		State:            NewStateManager(),
		undoHistory:      make(map[int]*BlockUndoRecord),
		pendingSpent:     make(map[string]int64),
		pendingCount:     make(map[string]int64),
		txsPerSender:     make(map[string]int),
		CoinbaseMaturity: mat,
	}
	bc.State.CoinbaseMaturity = mat

	if err := bc.CreateGenesisBlock(); err != nil {
		return nil, fmt.Errorf("failed to initialize genesis block: %w", err)
	}
	if err := bc.SaveToFile(); err != nil {
		_ = bc.Close()
		return nil, fmt.Errorf("failed to persist initial blockchain to disk: %w", err)
	}
	return bc, nil
}

// NewBlockchain initializes a new blockchain or loads an existing chain from local storage [F4]
// NewBlockchain initializes a new blockchain or loads an existing chain from local storage [F4]
func NewBlockchain(difficulty int) *Blockchain {
	var bc *Blockchain
	var err error

	// Retry loop with exponential backoff to handle bbolt lock contention during concurrent tests
	for i := 0; i < 20; i++ {
		bc, err = NewBlockchainWithError(difficulty)
		if err == nil {
			return bc
		}
		if strings.Contains(err.Error(), "timeout") {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		break
	}

	// In-memory fallback is strictly restricted to test environments or explicit regtest/ARMX_IN_MEMORY [T12]
	isExplicitInMemory := os.Getenv("ARMX_IN_MEMORY") == "1" || GetActiveNetwork().Name == "regtest"
	isTest := flag.Lookup("test.v") != nil
	if err != nil && strings.Contains(err.Error(), "timeout") && (isExplicitInMemory || isTest) {
		fmt.Printf("[!] Warning: bbolt store timeout encountered. Falling back to in-memory mode for test safety.\n")
		mat := GetActiveNetwork().CoinbaseMaturity
		if mat <= 0 {
			mat = CoinbaseMaturity
		}
		bc = &Blockchain{
			Chain:            []Block{},
			Difficulty:       difficulty,
			Mempool:          []Transaction{},
			BlockReward:      InitialReward,
			State:            NewStateManager(),
			undoHistory:      make(map[int]*BlockUndoRecord),
			pendingSpent:     make(map[string]int64),
			pendingCount:     make(map[string]int64),
			txsPerSender:     make(map[string]int),
			CoinbaseMaturity: mat,
			StoragePath:      "IN_MEMORY",
			storeFailed:      true,
		}
		bc.State.CoinbaseMaturity = mat
		_ = bc.CreateGenesisBlock()
		return bc
	}

	if err != nil {
		panic(fmt.Sprintf("FATAL: failed to initialize blockchain database: %v", err))
	}
	return bc
}

// MustNewBlockchain initializes a new blockchain or panics on error (intended for test fixtures) [F4]
func MustNewBlockchain(difficulty int) *Blockchain {
	return NewBlockchain(difficulty)
}

// CreateGenesisBlock initializes the chain with the canonical, hardcoded Genesis block
func (bc *Blockchain) CreateGenesisBlock() error {
	genesisBlock := GetGenesisBlock()
	bc.Chain = []Block{genesisBlock}
	mat := bc.GetCoinbaseMaturity()
	bc.State = NewStateManager()
	bc.State.CoinbaseMaturity = mat
	if bc.undoHistory == nil {
		bc.undoHistory = make(map[int]*BlockUndoRecord)
	}
	if err := bc.State.VerifyAndApplyBlock(&genesisBlock, InitialReward, mat); err != nil {
		return fmt.Errorf("genesis state application failed: %w", err)
	}
	bc.cachedSupply = InitialReward
	bc.cachedTotalTx = int64(len(genesisBlock.Transactions))
	bc.cachedWork = CalculateChainwork(bc.Chain)
	bc.pendingLock.Lock()
	bc.pendingSpent = make(map[string]int64)
	bc.pendingCount = make(map[string]int64)
	bc.txsPerSender = make(map[string]int)
	bc.pendingLock.Unlock()
	return nil
}

// GetNetworkID returns the immutable network identifier determined by the Genesis block of the chain [C-33]
func (bc *Blockchain) GetNetworkID() string {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	if len(bc.Chain) > 0 {
		h := bc.Chain[0].Hash
		t := bc.Chain[0].Target
		if h == TestnetGenesisHash {
			return TestnetProfile.NetworkID
		}
		if h == GenesisHash {
			return MainnetProfile.NetworkID
		}
		if t == RegtestProfile.PowLimitHex || h == GetRegtestGenesisBlock().Hash {
			return RegtestProfile.NetworkID
		}
		if t == SoakProfile.PowLimitHex || h == GetSoakGenesisBlock().Hash {
			return SoakProfile.NetworkID
		}
	}
	return NetworkID()
}

// GetTotalTransactions returns the cumulative transaction count in O(1) [N-21]
func (bc *Blockchain) GetTotalTransactions() int64 {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	return bc.cachedTotalTx
}

// RebuildState recalculates the complete account state from the Genesis block in O(n) with proper thread locking
func (bc *Blockchain) RebuildState() error {
	bc.Lock.Lock()
	defer bc.Lock.Unlock()

	mat := bc.GetCoinbaseMaturity()
	bc.State = NewStateManager()
	bc.State.CoinbaseMaturity = mat
	var runningSupply int64 = 0
	var totalTxs int64 = 0
	for i := 0; i < len(bc.Chain); i++ {
		b := &bc.Chain[i]
		reward := CalculateBlockRewardGivenSupply(b.Index, runningSupply)
		if err := bc.State.VerifyAndApplyBlock(b, reward, mat); err != nil {
			return fmt.Errorf("block #%d state failure: %w", b.Index, err)
		}
		runningSupply += reward
		totalTxs += int64(len(b.Transactions))
	}
	bc.cachedSupply = runningSupply
	bc.cachedTotalTx = totalTxs
	bc.cachedWork = CalculateChainwork(bc.Chain)
	bc.CleanMempoolLocked()
	return nil
}

// CalculateBlockRewardGivenSupply computes block reward based on block index and current circulating supply in O(1)
func CalculateBlockRewardGivenSupply(blockIndex int, currentSupply int64) int64 {
	halvings := blockIndex / HalvingInterval
	if halvings >= 64 {
		return 0
	}
	reward := InitialReward >> halvings
	if currentSupply >= MaxSupply {
		return 0
	}
	if currentSupply+reward > MaxSupply {
		return MaxSupply - currentSupply
	}
	return reward
}

// GetBlockReward calculates the deterministic block reward with halving schedule and supply cap in O(1) [C-28, C-34]
func (bc *Blockchain) GetBlockReward(blockIndex int) int64 {
	return CalculateBlockRewardGivenSupply(blockIndex, bc.cachedSupply)
}

// GetBlockRewardForChain calculates the block reward for a specific chain at blockIndex
func GetBlockRewardForChain(chain []Block, blockIndex int) int64 {
	circulating := CalculateCirculatingSupply(chain)
	return CalculateBlockRewardGivenSupply(blockIndex, circulating)
}

// GetCirculatingSupply returns the cached total minted ARMX base units currently in existence in O(1) [C-28, C-34]
func (bc *Blockchain) GetCirculatingSupply() int64 {
	return bc.cachedSupply
}

// CalculateCirculatingSupply deterministically sums the block rewards of a chain without counting recycled fees
func CalculateCirculatingSupply(chain []Block) int64 {
	var total int64 = 0
	for _, block := range chain {
		halvings := block.Index / HalvingInterval
		if halvings < 64 {
			reward := InitialReward >> halvings
			if total+reward > MaxSupply {
				total = MaxSupply
				break
			}
			total += reward
		}
	}
	return total
}

// GetLatestBlock returns the most recent block in the chain thread-safely
func (bc *Blockchain) GetLatestBlock() Block {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	return bc.GetLatestBlockUnsafe()
}

// GetLatestBlockUnsafe returns the latest block assuming lock is already held
func (bc *Blockchain) GetLatestBlockUnsafe() Block {
	if len(bc.Chain) == 0 {
		return Block{}
	}
	return bc.Chain[len(bc.Chain)-1]
}

// GetHeight returns the top block index thread-safely
func (bc *Blockchain) GetHeight() int {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	return bc.GetHeightUnsafe()
}

// GetHeightUnsafe returns the top block index assuming lock is already held
func (bc *Blockchain) GetHeightUnsafe() int {
	if len(bc.Chain) == 0 {
		return 0
	}
	return len(bc.Chain) - 1
}

// GetBalance returns the confirmed balance on the blockchain for a given address
func (bc *Blockchain) GetBalance(address string) int64 {
	if bc.State == nil {
		return 0
	}
	return bc.State.GetBalance(address)
}

// GetAccountNonce returns the confirmed transaction nonce for an address
func (bc *Blockchain) GetAccountNonce(address string) int64 {
	if bc.State == nil {
		return 0
	}
	return bc.State.GetNonce(address)
}

// GetPendingBalance calculates the available balance after deducting pending mempool transactions in O(1)
func (bc *Blockchain) GetPendingBalance(address string) int64 {
	balance := bc.GetBalance(address)
	sHex, err := AddressToHex(address)
	if err == nil {
		bc.pendingLock.RLock()
		if bc.pendingSpent != nil {
			balance -= bc.pendingSpent[sHex]
		}
		bc.pendingLock.RUnlock()
	}
	return balance
}

// GetPendingNonce calculates the current pending nonce count for an account in O(1)
func (bc *Blockchain) GetPendingNonce(address string) int64 {
	nonce := bc.GetAccountNonce(address)
	sHex, err := AddressToHex(address)
	if err == nil {
		bc.pendingLock.RLock()
		if bc.pendingCount != nil {
			nonce += bc.pendingCount[sHex]
		}
		bc.pendingLock.RUnlock()
	}
	return nonce
}

// GetCoinbaseMaturity returns the required confirmations before coinbase rewards can be spent [L4-10-a]
func (bc *Blockchain) GetCoinbaseMaturity() int {
	if bc.CoinbaseMaturity > 0 {
		return bc.CoinbaseMaturity
	}
	if net := GetActiveNetwork(); net.CoinbaseMaturity > 0 {
		return net.CoinbaseMaturity
	}
	return CoinbaseMaturity
}

// GetImmatureBalance calculates total coinbase rewards with confirmations < CoinbaseMaturity [L4-10-a]
func (bc *Blockchain) GetImmatureBalance(address string) int64 {
	maturity := bc.GetCoinbaseMaturity()
	if maturity <= 0 {
		return 0
	}
	chainLen := len(bc.Chain)
	if chainLen == 0 {
		return 0
	}
	currentHeight := chainLen - 1
	if bc.State != nil {
		return bc.State.GetImmatureBalance(address, currentHeight, maturity)
	}
	var immature int64 = 0
	startScan := currentHeight
	endScan := currentHeight - maturity + 1
	if endScan < 0 {
		endScan = 0
	}
	for i := startScan; i >= endScan; i-- {
		b := bc.Chain[i]
		if len(b.Transactions) > 0 {
			cb := b.Transactions[0]
			if (cb.Sender == "SYSTEM" || cb.Sender == "COINBASE") && MatchAddress(cb.Recipient, address) {
				confs := currentHeight - b.Index + 1
				if confs < maturity {
					immature += cb.Amount
				}
			}
		}
	}
	return immature
}

// GetSpendableBalance calculates the available balance after deducting pending mempool transactions and immature coinbase rewards
func (bc *Blockchain) GetSpendableBalance(address string) int64 {
	pending := bc.GetPendingBalance(address)
	immature := bc.GetImmatureBalance(address)
	spendable := pending - immature
	if spendable < 0 {
		return 0
	}
	return spendable
}

// CalculateMinMempoolFee computes the dynamic mempool minimum relay fee based on transaction byte size:
// fee minimum = MinRelayFee * ceil(tx.Size() / 1000) [P2-4]
func CalculateMinMempoolFee(txSize int) int64 {
	kb := (txSize + 999) / 1000
	if kb < 1 {
		kb = 1
	}
	return MinRelayFee * int64(kb)
}

// AddTransaction enforces security validation, mempool size limits, and appends valid transactions
func (bc *Blockchain) AddTransaction(tx Transaction) (bool, string) {
	// 1. Block external SYSTEM transaction spoofing
	if strings.EqualFold(tx.Sender, "SYSTEM") {
		return false, "Rejected: SYSTEM transactions can only be generated internally by the protocol"
	}

	// 2. Validate transfer amount and minimum relay fee [C-16, P2-4]
	if tx.Amount <= 0 {
		return false, "Rejected: Transaction amount must be greater than 0"
	}

	minMempoolFee := CalculateMinMempoolFee(tx.Size())
	if tx.Fee < minMempoolFee {
		return false, fmt.Sprintf("Rejected: Transaction fee %d is below required relay fee (%d Armoids for %d bytes)", tx.Fee, minMempoolFee, tx.Size())
	}

	if tx.Amount > MaxSupply {
		return false, fmt.Sprintf("Rejected: Amount exceeds maximum supply cap (%d Armoids)", MaxSupply)
	}
	if tx.Fee > MaxSupply {
		return false, fmt.Sprintf("Rejected: Fee value is unreasonably large (max %d Armoids)", MaxSupply)
	}

	senderHex, err := AddressToHex(tx.Sender)
	if err != nil {
		return false, fmt.Sprintf("Rejected: Invalid sender address format: %v", err)
	}

	recipHex, err := AddressToHex(tx.Recipient)
	if err != nil {
		return false, fmt.Sprintf("Rejected: Invalid recipient address format: %v", err)
	}
	if senderHex == recipHex {
		return false, "Rejected: Self-transfer transactions are prohibited"
	}

	// 3. Cryptographic Signature verification (ML-DSA-65 NIST FIPS 204) before taking write lock [T5]
	if !tx.Verify() {
		return false, "Rejected: Digital signature is invalid or does not match public key"
	}

	// Stateful validation and mempool modifications under global write lock [T5]
	bc.Lock.Lock()
	defer bc.Lock.Unlock()

	// 4. Rate-limit unconfirmed pending transactions per sender [C-16, C-20]
	bc.pendingLock.RLock()
	senderPendingCount := 0
	if bc.txsPerSender != nil {
		senderPendingCount = bc.txsPerSender[senderHex]
	}
	bc.pendingLock.RUnlock()
	if senderPendingCount >= MaxTxsPerSender {
		return false, fmt.Sprintf("Rejected: Sender has exceeded maximum unconfirmed pending transactions (%d)", MaxTxsPerSender)
	}

	// 5. Balance validation including fee and Coinbase maturity (prevent double-spending and immature spend)
	totalCost := tx.Amount + tx.Fee
	spendable := bc.GetSpendableBalance(tx.Sender)
	if spendable < totalCost {
		immature := bc.GetImmatureBalance(tx.Sender)
		if immature > 0 {
			return false, fmt.Sprintf("Rejected: Insufficient spendable balance (Spendable: %d, Immature: %d [< %d confirmations], Required: %d with fee)",
				spendable, immature, bc.GetCoinbaseMaturity(), totalCost)
		}
		return false, fmt.Sprintf("Rejected: Insufficient balance (Available: %d, Required: %d with fee)", spendable, totalCost)
	}

	// 6. Nonce sequence validation (strictly sequential: next tx must be currentPendingNonce + 1)
	expectedNonce := bc.GetPendingNonce(tx.Sender) + 1
	if tx.Nonce != expectedNonce {
		return false, fmt.Sprintf("Rejected: Invalid transaction nonce (Expected: %d, Got: %d)", expectedNonce, tx.Nonce)
	}

	// 7. Mempool capacity protection with tail-only fee eviction [C-16, C-21]
	// Eviction occurs strictly AFTER full validation to ensure invalid txs cannot evict honest ones.
	// Only tail transactions (highest nonce per sender) are eligible for eviction to preserve nonce continuity.
	if len(bc.Mempool) >= MaxMempoolSize {
		tailIndices := make(map[string]int) // senderHex -> index in Mempool
		for i, mTx := range bc.Mempool {
			sHex, err := AddressToHex(mTx.Sender)
			if err != nil {
				continue
			}
			if prevIdx, ok := tailIndices[sHex]; !ok {
				tailIndices[sHex] = i
			} else if mTx.Nonce > bc.Mempool[prevIdx].Nonce {
				tailIndices[sHex] = i
			}
		}

		minTailIdx := -1
		var minTailFee int64 = tx.Fee
		for _, idx := range tailIndices {
			mTx := bc.Mempool[idx]
			if mTx.Fee < minTailFee {
				minTailFee = mTx.Fee
				minTailIdx = idx
			}
		}

		if minTailIdx == -1 {
			return false, "Rejected: Mempool is full and no lower-fee tail transaction can be evicted"
		}

		// Evict the tail transaction and re-sync tracking maps
		bc.Mempool = append(bc.Mempool[:minTailIdx], bc.Mempool[minTailIdx+1:]...)
		bc.CleanMempoolLocked()
	}

	bc.Mempool = append(bc.Mempool, tx)
	bc.pendingLock.Lock()
	if bc.pendingSpent == nil {
		bc.pendingSpent = make(map[string]int64)
		bc.pendingCount = make(map[string]int64)
		bc.txsPerSender = make(map[string]int)
	}
	bc.pendingSpent[senderHex] += totalCost
	bc.pendingCount[senderHex]++
	bc.txsPerSender[senderHex]++
	bc.pendingLock.Unlock()

	return true, "Transaction is valid and added to mempool"
}

// CleanMempoolLocked re-validates and prunes invalid/exhausted transactions from the mempool.
// Callers must hold bc.Lock.
func (bc *Blockchain) CleanMempoolLocked() int {
	var validMempool []Transaction
	purgedCount := 0

	runningBalances := make(map[string]int64)
	expectedNonces := make(map[string]int64)

	seenTx := make(map[string]struct{})

	for _, tx := range bc.Mempool {
		// 1. Transaction hash deduplication [F6]
		txHash := tx.HashHex()
		if _, seen := seenTx[txHash]; seen {
			purgedCount++
			continue
		}

		// 2. Structural sanity checks [F6]
		if tx.Amount <= 0 || tx.Fee < 0 {
			purgedCount++
			continue
		}

		// 3. Address format and canonical self-transfer validation [F6-PERF]
		senderHex, err := AddressToHex(tx.Sender)
		if err != nil {
			purgedCount++
			continue
		}
		recipHex, err := AddressToHex(tx.Recipient)
		if err != nil {
			purgedCount++
			continue
		}
		if senderHex == recipHex {
			purgedCount++
			continue
		}

		// 4. Address format and post-reorg account state validation [F6]
		if _, ok := runningBalances[senderHex]; !ok {
			immature := bc.GetImmatureBalance(tx.Sender)
			spendable := bc.GetBalance(tx.Sender) - immature
			if spendable < 0 {
				spendable = 0
			}
			runningBalances[senderHex] = spendable
			expectedNonces[senderHex] = bc.GetAccountNonce(tx.Sender) + 1
		}

		totalCost := tx.Amount + tx.Fee
		if tx.Nonce != expectedNonces[senderHex] || runningBalances[senderHex] < totalCost {
			purgedCount++
			continue
		}

		runningBalances[senderHex] -= totalCost
		expectedNonces[senderHex]++
		seenTx[txHash] = struct{}{}
		validMempool = append(validMempool, tx)
	}

	bc.Mempool = validMempool

	// Rebuild O(1) mempool tracking maps keyed by canonical hex [C-15, C-16, C-20]
	bc.pendingLock.Lock()
	bc.pendingSpent = make(map[string]int64)
	bc.pendingCount = make(map[string]int64)
	bc.txsPerSender = make(map[string]int)
	for _, tx := range validMempool {
		sHex, err := AddressToHex(tx.Sender)
		if err == nil {
			bc.pendingSpent[sHex] += (tx.Amount + tx.Fee)
			bc.pendingCount[sHex]++
			bc.txsPerSender[sHex]++
		}
	}
	bc.pendingLock.Unlock()

	return purgedCount
}

// CleanMempool thread-safely prunes invalid transactions from the mempool
func (bc *Blockchain) CleanMempool() int {
	bc.Lock.Lock()
	defer bc.Lock.Unlock()
	return bc.CleanMempoolLocked()
}

var (
	miningDeterministicRaceHookMu sync.Mutex
	miningDeterministicRaceHook   func(recordErr func(error))
)

// SetMiningDeterministicRaceHookForTesting installs a deterministic synchronization hook for MineBlockContext tests.
// The hook is called when a winning block is received on resultChan before worker cancellation and barrier wait.
func SetMiningDeterministicRaceHookForTesting(hook func(recordErr func(error))) func() {
	miningDeterministicRaceHookMu.Lock()
	prev := miningDeterministicRaceHook
	miningDeterministicRaceHook = hook
	miningDeterministicRaceHookMu.Unlock()
	return func() {
		miningDeterministicRaceHookMu.Lock()
		miningDeterministicRaceHook = prev
		miningDeterministicRaceHookMu.Unlock()
	}
}

// MineBlockContext mines a new block including miner reward, transaction fees, and mempool transactions
// with context cancellation and stale-block detection support.
func (bc *Blockchain) MineBlockContext(ctx context.Context, minerAddress string, threadCount ...int) (Block, error) {
	bc.Lock.Lock()

	// 1. Purge stale/invalid transactions from mempool before assembly [C-20]
	bc.CleanMempoolLocked()

	target := bc.CalculateLWMA1()
	prevBlock := bc.GetLatestBlockUnsafe()
	currentBlockReward := bc.GetBlockReward(prevBlock.Index + 1)
	bc.BlockReward = currentBlockReward

	// 2. Simulate transactions against state overlay to discard invalid ones [C-20]
	maxMempoolTxs := MaxBlockTransactions - 1
	var validTxsToMine []Transaction
	var totalFees int64 = 0

	simBalances := make(map[string]int64)
	simNonces := make(map[string]int64)

	for _, tx := range bc.Mempool {
		if len(validTxsToMine) >= maxMempoolTxs {
			break
		}
		if !tx.Verify() {
			continue
		}
		senderHex, err := AddressToHex(tx.Sender)
		if err != nil {
			continue
		}
		recipHex, err := AddressToHex(tx.Recipient)
		if err != nil || strings.EqualFold(senderHex, recipHex) {
			continue
		}

		if _, ok := simBalances[senderHex]; !ok {
			imm := bc.GetImmatureBalance(tx.Sender)
			spend := bc.GetBalance(tx.Sender) - imm
			if spend < 0 {
				spend = 0
			}
			simBalances[senderHex] = spend
			simNonces[senderHex] = bc.GetAccountNonce(tx.Sender)
		}

		totalCost := tx.Amount + tx.Fee
		expectedNonce := simNonces[senderHex] + 1
		if tx.Nonce != expectedNonce || simBalances[senderHex] < totalCost {
			continue
		}

		simBalances[senderHex] -= totalCost
		simNonces[senderHex] = tx.Nonce

		if _, ok := simBalances[recipHex]; !ok {
			simBalances[recipHex] = bc.GetBalance(tx.Recipient)
		}
		simBalances[recipHex] += tx.Amount

		totalFees += tx.Fee
		validTxsToMine = append(validTxsToMine, tx)
	}

	rewardTx := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: minerAddress,
		Amount:    currentBlockReward + totalFees,
		Fee:       0,
		Nonce:     int64(prevBlock.Index + 1), // [C-26] Coinbase nonce equals block height
		Signature: "REWARD_SIG",
	}

	txsToMine := append([]Transaction{rewardTx}, validTxsToMine...)
	newBlock := *NewBlock(txsToMine, prevBlock.Hash, prevBlock.Index+1, minerAddress, target)

	// [C-22] Miner sets timestamp = max(now, MTP + 1) to ensure valid block
	mtp := CalculateMedianTimePast(bc.Chain, prevBlock.Index+1)
	now := time.Now().Unix()
	if now <= mtp {
		now = mtp + 1
	}
	newBlock.Timestamp = now
	// Randomize starting nonce (32-bit safe integer) so concurrent mining workers explore non-overlapping partitions
	var startNonce int64
	var nonceBytes [4]byte
	if _, err := io.ReadFull(cryptoRand.Reader, nonceBytes[:]); err == nil {
		startNonce = int64(binary.BigEndian.Uint32(nonceBytes[:]))
	}
	threads := bc.MiningThreads
	if len(threadCount) > 0 && threadCount[0] > 0 {
		threads = threadCount[0]
	}
	if threads <= 0 {
		if envStr := os.Getenv("ARMX_MINING_THREADS"); envStr != "" {
			if n, err := strconv.Atoi(envStr); err == nil && n > 0 {
				threads = n
			}
		}
	}
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	if threads < 1 {
		threads = 1
	}
	bc.Lock.Unlock()

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	defer bc.SetMiningHashrate(0)

	resultChan := make(chan Block, 1)
	errChan := make(chan error, threads)
	var wg sync.WaitGroup
	var totalHashes int64
	var firstErr error
	var errOnce sync.Once

	recordError := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			select {
			case errChan <- err:
			default:
			}
			cancelWorkers()
		})
	}

	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				hashes := atomic.LoadInt64(&totalHashes)
				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 {
					hps := float64(hashes) / elapsed
					bc.SetMiningHashrate(hps)
					if int(elapsed)%10 == 0 {
						fmt.Printf("[Mining] In progress for Block #%d: %d hashes (%.2f MH/s, %ds elapsed, %d threads)...\n",
							newBlock.Index, hashes, hps/1_000_000.0, int(elapsed), threads)
					}
				}
			}
		}
	}()

	var targetBytes [32]byte
	tBytes := target.Bytes()
	if len(tBytes) > 32 {
		tBytes = tBytes[len(tBytes)-32:]
	}
	copy(targetBytes[32-len(tBytes):], tBytes)

	stride := int64(threads)
	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			b := newBlock
			b.Nonce = startNonce + int64(workerID)

			targetStr := b.Target
			if targetStr == "" {
				targetStr = fmt.Sprintf("%064x", DifficultyToTarget(b.Difficulty))
			}
			suffix := ":" + targetStr
			suffixBytes := []byte(suffix)

			prefix := fmt.Sprintf("%d:%d:%s:%s:%s:", b.Index, b.Timestamp, b.PrevHash, b.MerkleRoot, b.Miner)
			prefixBytes := []byte(prefix)
			buf := make([]byte, 0, len(prefixBytes)+32+len(suffixBytes))

			useArmaxHashV2 := IsArmaxHashV2Active(b.Index)
			useArmaxHash := IsArmaxHashActive(b.Index)
			netID := NetworkID()
			batchLimit := int64(10000)
			if !useArmaxHashV2 && useArmaxHash {
				batchLimit = 20
			}

			var localBatch int64

			for {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				buf = buf[:0]
				buf = append(buf, prefixBytes...)
				buf = strconv.AppendInt(buf, b.Nonce, 10)
				buf = append(buf, suffixBytes...)

				var hashBytes [32]byte
				if useArmaxHashV2 {
					digest, err := armaxhash.HashV2(buf, netID)
					if err != nil {
						recordError(fmt.Errorf("mining hash computation failed: %w", err))
						return
					}
					hashBytes = digest
				} else if useArmaxHash {
					digest, err := armaxhash.Hash(buf, netID)
					if err != nil {
						recordError(fmt.Errorf("mining hash computation failed: %w", err))
						return
					}
					hashBytes = digest
				} else {
					h1 := sha256.Sum256(buf)
					hashBytes = sha256.Sum256(h1[:])
				}
				localBatch++

				if bytes.Compare(hashBytes[:], targetBytes[:]) <= 0 {
					atomic.AddInt64(&totalHashes, localBatch)
					b.Hash = hex.EncodeToString(hashBytes[:])
					select {
					case resultChan <- b:
						cancelWorkers()
					default:
					}
					return
				}

				b.Nonce += stride

				if localBatch >= batchLimit {
					atomic.AddInt64(&totalHashes, localBatch)
					localBatch = 0

					bc.Lock.RLock()
					tip := bc.GetLatestBlockUnsafe()
					bc.Lock.RUnlock()
					if tip.Hash != prevBlock.Hash {
						recordError(fmt.Errorf("stale block: chain tip updated during mining"))
						return
					}
					curTime := time.Now().Unix()
					if curTime > b.Timestamp {
						b.Timestamp = curTime
						prefix = fmt.Sprintf("%d:%d:%s:%s:%s:", b.Index, b.Timestamp, b.PrevHash, b.MerkleRoot, b.Miner)
						prefixBytes = []byte(prefix)
					}
				}
			}
		}(w)
	}

	var winningBlock Block
	select {
	case err := <-errChan:
		cancelWorkers()
		wg.Wait()
		return Block{}, err
	case winningBlock = <-resultChan:
		miningDeterministicRaceHookMu.Lock()
		raceHook := miningDeterministicRaceHook
		miningDeterministicRaceHookMu.Unlock()
		if raceHook != nil {
			raceHook(recordError)
		}
		cancelWorkers()
		wg.Wait()
	case <-ctx.Done():
		cancelWorkers()
		wg.Wait()
		if firstErr != nil {
			return Block{}, firstErr
		}
		return Block{}, ctx.Err()
	}

	// Check error after all workers have stopped and before the block is processed.
	// Do not accept or validate the mined block if the mining round registered an error.
	if firstErr != nil {
		return Block{}, firstErr
	}

	if !bc.ValidateAndAddBlock(winningBlock) {
		bc.Lock.Lock()
		bc.CleanMempoolLocked()
		bc.Lock.Unlock()
		return Block{}, fmt.Errorf("validation failed for newly mined block #%d", winningBlock.Index)
	}
	fmt.Printf("[Mining] Successfully mined block #%d! Nonce: %d, Hash: %s\n", winningBlock.Index, winningBlock.Nonce, winningBlock.Hash)
	return winningBlock, nil
}

// MineBlockWithErr mines a new block synchronously and returns any error encountered [C-13]
func (bc *Blockchain) MineBlockWithErr(minerAddress string) (Block, error) {
	return bc.MineBlockContext(context.Background(), minerAddress)
}

// IsChainValid verifies the cryptographic and PoW integrity of the entire blockchain
func (bc *Blockchain) IsChainValid() bool {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()

	if len(bc.Chain) == 0 {
		return false
	}
	// [C-23] Strictly validate Genesis block against protocol specification
	if err := ValidateGenesisBlock(&bc.Chain[0]); err != nil {
		return false
	}

	for i := 1; i < len(bc.Chain); i++ {
		currentBlock := bc.Chain[i]
		prevBlock := bc.Chain[i-1]

		if currentBlock.Index != prevBlock.Index+1 {
			return false
		}
		if currentBlock.PrevHash != prevBlock.Hash {
			return false
		}
		if len(currentBlock.Transactions) == 0 || len(currentBlock.Transactions) > MaxBlockTransactions {
			return false
		}
		if currentBlock.MerkleRoot != CalculateMerkleRoot(currentBlock.Transactions) {
			return false
		}
		calcHash, err := currentBlock.CalculateHashWithError()
		if err != nil || currentBlock.Hash == "" || currentBlock.Hash != calcHash {
			return false
		}

		expectedTarget := CalculateExpectedTarget(bc.Chain[:i], currentBlock.Index)
		expectedTargetHex := fmt.Sprintf("%064x", expectedTarget)
		if currentBlock.Target != "" && currentBlock.Target != expectedTargetHex {
			return false
		}
		if !HashMeetsTarget(currentBlock.Hash, expectedTarget) {
			return false
		}
	}
	return true
}

// AdjustDifficulty updates the display difficulty based on current chain tip target
func (bc *Blockchain) AdjustDifficulty() int {
	target := bc.CalculateLWMA1()
	diff := TargetToDifficulty(target)
	bc.Difficulty = diff
	return diff
}
