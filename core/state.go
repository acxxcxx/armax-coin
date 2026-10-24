package core

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
)

// CloneCount tracks total StateManager.Clone() invocations (for telemetry and test verification).
var CloneCount int64

// ImmatureReward tracks an immature coinbase block reward entry [L4-10-a]
type ImmatureReward struct {
	Amount int64 `json:"amount"` // Reward amount in base units (Armoids)
	Height int   `json:"height"` // Block height at which reward was mined
}

// AccountState represents the on-chain confirmed balance, nonce, and immature coinbase queue of an address [L4-10-a]
type AccountState struct {
	Balance  int64            `json:"balance"`            // Confirmed total balance in base units (Armoids, 1 ARMX = 10^8 Armoids)
	Nonce    int64            `json:"nonce"`              // Strictly sequential transaction nonce (starts at 0)
	Immature []ImmatureReward `json:"immature,omitempty"` // Queue of immature coinbase rewards
}

// ImmatureTotal calculates total immature rewards for this account at blockHeight given maturity [L4-10-a]
func (acc AccountState) ImmatureTotal(blockHeight int, maturity int) int64 {
	if maturity <= 0 || len(acc.Immature) == 0 {
		return 0
	}
	var total int64
	for _, entry := range acc.Immature {
		confs := blockHeight - entry.Height + 1
		if confs < maturity {
			total += entry.Amount
		}
	}
	return total
}

// Spendable calculates the spendable balance at blockHeight given maturity [L4-10-a]
func (acc AccountState) Spendable(blockHeight int, maturity int) int64 {
	imm := acc.ImmatureTotal(blockHeight, maturity)
	spendable := acc.Balance - imm
	if spendable < 0 {
		return 0
	}
	return spendable
}

// PruneMatured returns a copy of AccountState with matured rewards pruned at blockHeight [L4-10-a]
func (acc AccountState) PruneMatured(blockHeight int, maturity int) AccountState {
	if maturity <= 0 || len(acc.Immature) == 0 {
		return acc
	}
	var active []ImmatureReward
	for _, entry := range acc.Immature {
		confs := blockHeight - entry.Height + 1
		if confs < maturity {
			active = append(active, entry)
		}
	}
	acc.Immature = active
	return acc
}

// StateManager maintains the authoritative account state ledger
func cloneAccountState(acc AccountState) AccountState {
	if len(acc.Immature) > 0 {
		immCopy := make([]ImmatureReward, len(acc.Immature))
		copy(immCopy, acc.Immature)
		acc.Immature = immCopy
	} else {
		acc.Immature = nil
	}
	return acc
}

type StateManager struct {
	Lock             sync.RWMutex            `json:"-"`
	Accounts         map[string]AccountState `json:"accounts"`          // Key: canonical 40-character lowercase hex address
	CoinbaseMaturity int                     `json:"coinbase_maturity"` // Maturity threshold for coinbase rewards [L4-10-a]
}

// NewStateManager initializes a fresh, empty state ledger
func NewStateManager() *StateManager {
	return &StateManager{
		Accounts: make(map[string]AccountState),
	}
}

// Clone creates a deep copy of the state ledger (used for atomic block validation)
func (sm *StateManager) Clone() *StateManager {
	atomic.AddInt64(&CloneCount, 1)
	sm.Lock.RLock()
	defer sm.Lock.RUnlock()

	clone := &StateManager{
		Accounts:         make(map[string]AccountState, len(sm.Accounts)),
		CoinbaseMaturity: sm.CoinbaseMaturity,
	}
	for k, v := range sm.Accounts {
		clone.Accounts[k] = cloneAccountState(v)
	}
	return clone
}

// GetBalance returns the confirmed balance for an address in base units
func (sm *StateManager) GetBalance(address string) int64 {
	sm.Lock.RLock()
	defer sm.Lock.RUnlock()

	hexAddr, err := AddressToHexCached(address)
	if err != nil {
		return 0
	}
	return sm.Accounts[hexAddr].Balance
}

// GetImmatureBalance returns total immature coinbase rewards for an address at the specified block height [L4-10-a]
func (sm *StateManager) GetImmatureBalance(address string, blockHeight int, maturity int) int64 {
	sm.Lock.RLock()
	defer sm.Lock.RUnlock()

	hexAddr, err := AddressToHexCached(address)
	if err != nil {
		return 0
	}
	return sm.Accounts[hexAddr].ImmatureTotal(blockHeight, maturity)
}

// GetSpendableBalance returns the spendable balance for an address at the specified block height [L4-10-a]
func (sm *StateManager) GetSpendableBalance(address string, blockHeight int, maturity int) int64 {
	sm.Lock.RLock()
	defer sm.Lock.RUnlock()

	hexAddr, err := AddressToHexCached(address)
	if err != nil {
		return 0
	}
	return sm.Accounts[hexAddr].Spendable(blockHeight, maturity)
}

// GetNonce returns the confirmed transaction nonce for an address
func (sm *StateManager) GetNonce(address string) int64 {
	sm.Lock.RLock()
	defer sm.Lock.RUnlock()

	hexAddr, err := AddressToHexCached(address)
	if err != nil {
		return 0
	}
	return sm.Accounts[hexAddr].Nonce
}

// VerifyAndApplyBlock validates all transaction balances and nonces, applying state updates atomically.
// If any transaction fails validation (insufficient balance, immature coinbase spend, nonce mismatch, unauthorized SYSTEM mint),
// the state remains untouched and an error is returned [C-15, L4-10-a].
// Uses an overlay delta map [C-15] to achieve O(transactions) performance without copying the entire ledger.
func (sm *StateManager) VerifyAndApplyBlock(b *Block, allowedBlockReward int64, maturityOpt ...int) error {
	sm.Lock.Lock()
	defer sm.Lock.Unlock()

	maturity := sm.CoinbaseMaturity
	if len(maturityOpt) > 0 && maturityOpt[0] >= 0 {
		maturity = maturityOpt[0]
	}
	if maturity <= 0 {
		if net := GetActiveNetwork(); net.CoinbaseMaturity > 0 {
			maturity = net.CoinbaseMaturity
		} else {
			maturity = CoinbaseMaturity
		}
	}

	if len(b.Transactions) == 0 {
		return fmt.Errorf("block #%d has no transactions", b.Index)
	}
	if sm.Accounts == nil {
		sm.Accounts = make(map[string]AccountState)
	}

	// [C-15] Delta overlay map: O(|Transactions|) instead of copying entire ledger O(|Accounts|)
	deltaAccounts := make(map[string]AccountState, len(b.Transactions)*2)
	getAccount := func(addrHex string) AccountState {
		if acc, ok := deltaAccounts[addrHex]; ok {
			return acc
		}
		orig := sm.Accounts[addrHex]
		var immCopy []ImmatureReward
		if len(orig.Immature) > 0 {
			immCopy = make([]ImmatureReward, len(orig.Immature))
			copy(immCopy, orig.Immature)
		}
		orig.Immature = immCopy
		return orig
	}

	// 1. First pass: Sum user fees and validate regular transactions (idx >= 1)
	var totalFees int64 = 0
	for idx := 1; idx < len(b.Transactions); idx++ {
		tx := b.Transactions[idx]

		// Consensus Rule: Only tx[0] is permitted to have Sender == "SYSTEM"
		if strings.EqualFold(tx.Sender, "SYSTEM") {
			return fmt.Errorf("block #%d tx #%d illegal SYSTEM sender (infinite minting rejected)", b.Index, idx)
		}

		// Prevent burning or transferring funds to SYSTEM
		if strings.EqualFold(tx.Recipient, "SYSTEM") {
			return fmt.Errorf("block #%d tx #%d cannot transfer funds to SYSTEM", b.Index, idx)
		}

		// [C-01] Range bounds and safe arithmetic
		if tx.Amount <= 0 || tx.Amount > MaxSupply {
			return fmt.Errorf("block #%d tx #%d invalid amount %d (must be between 1 and %d)", b.Index, idx, tx.Amount, MaxSupply)
		}
		if tx.Fee < 0 || tx.Fee > MaxSupply {
			return fmt.Errorf("block #%d tx #%d invalid fee %d (must be between 0 and %d)", b.Index, idx, tx.Fee, MaxSupply)
		}
		if tx.Amount > math.MaxInt64-tx.Fee {
			return fmt.Errorf("block #%d tx #%d totalCost arithmetic overflow", b.Index, idx)
		}

		senderHex, err := AddressToHexCached(tx.Sender)
		if err != nil {
			return fmt.Errorf("block #%d tx #%d invalid sender address format: %v", b.Index, idx, err)
		}

		recipHex, err := AddressToHexCached(tx.Recipient)
		if err != nil {
			return fmt.Errorf("block #%d tx #%d invalid recipient address format: %v", b.Index, idx, err)
		}

		// [C-19] Disallow self-transfer (Sender == Recipient canonical hex)
		if strings.EqualFold(senderHex, recipHex) {
			return fmt.Errorf("block #%d tx #%d self-transfer rejected (sender cannot be recipient)", b.Index, idx)
		}

		if !tx.Verify() {
			return fmt.Errorf("block #%d tx #%d cryptographic signature verification failed", b.Index, idx)
		}

		senderAcc := getAccount(senderHex)

		// Consensus Rule: Enforce strictly sequential nonce (anti-replay)
		expectedNonce := senderAcc.Nonce + 1
		if tx.Nonce != expectedNonce {
			return fmt.Errorf("block #%d tx #%d nonce mismatch for %s: expected %d, got %d",
				b.Index, idx, tx.Sender, expectedNonce, tx.Nonce)
		}

		// Consensus Rule: Enforce sufficient SPENDABLE balance (cannot spend immature coinbase rewards) [L4-10-a]
		totalCost := tx.Amount + tx.Fee
		spendable := senderAcc.Spendable(b.Index, maturity)
		if spendable < totalCost {
			immature := senderAcc.ImmatureTotal(b.Index, maturity)
			return fmt.Errorf("block #%d tx #%d insufficient balance: insufficient spendable balance for %s: requires %d, spendable %d (immature coinbase rewards %d [< %d confirmations])",
				b.Index, idx, tx.Sender, totalCost, spendable, immature, maturity)
		}

		// Accumulate fees safely
		if totalFees > math.MaxInt64-tx.Fee {
			return fmt.Errorf("block #%d total fees arithmetic overflow", b.Index)
		}
		totalFees += tx.Fee

		// Apply debit to sender in overlay
		senderAcc.Balance -= totalCost
		senderAcc.Nonce = tx.Nonce
		senderAcc = senderAcc.PruneMatured(b.Index, maturity)
		deltaAccounts[senderHex] = senderAcc

		// [C-19] Re-fetch recipient account from overlay after sender debit is written
		recipAcc := getAccount(recipHex)

		// Recipient balance overflow check
		if recipAcc.Balance > MaxSupply-tx.Amount {
			return fmt.Errorf("block #%d tx #%d recipient balance exceeds MaxSupply", b.Index, idx)
		}

		// Apply credit to recipient in overlay
		recipAcc.Balance += tx.Amount
		deltaAccounts[recipHex] = recipAcc
	}

	// 2. Second pass: Validate Coinbase transaction (idx == 0)
	// [C-02] Comprehensive Coinbase verification
	coinbaseTx := b.Transactions[0]
	if coinbaseTx.Sender != "SYSTEM" {
		return fmt.Errorf("block #%d tx #0 sender must be exact 'SYSTEM', got '%s'", b.Index, coinbaseTx.Sender)
	}
	if coinbaseTx.Fee != 0 {
		return fmt.Errorf("block #%d coinbase fee must be 0, got %d", b.Index, coinbaseTx.Fee)
	}
	// [C-26] Coinbase nonce must equal block height (height 0 for genesis)
	if coinbaseTx.Nonce != int64(b.Index) {
		return fmt.Errorf("block #%d coinbase nonce must match block height %d, got %d", b.Index, b.Index, coinbaseTx.Nonce)
	}
	if coinbaseTx.SenderPub != "" {
		return fmt.Errorf("block #%d coinbase sender_pub must be empty", b.Index)
	}
	if coinbaseTx.Type != "" && coinbaseTx.Type != TxTypeStandard {
		return fmt.Errorf("block #%d coinbase type must be empty or STANDARD, got '%s'", b.Index, coinbaseTx.Type)
	}
	if coinbaseTx.Signature != "REWARD_SIG" {
		return fmt.Errorf("block #%d coinbase signature sentinel mismatch", b.Index)
	}
	if coinbaseTx.Amount < 0 {
		return fmt.Errorf("block #%d coinbase amount cannot be negative (%d)", b.Index, coinbaseTx.Amount)
	}

	if allowedBlockReward < 0 || allowedBlockReward > math.MaxInt64-totalFees {
		return fmt.Errorf("block #%d coinbase reward arithmetic overflow", b.Index)
	}
	maxAllowedCoinbase := allowedBlockReward + totalFees
	if coinbaseTx.Amount > maxAllowedCoinbase {
		return fmt.Errorf("block #%d coinbase reward %d exceeds maximum allowed %d (reward %d + fees %d)",
			b.Index, coinbaseTx.Amount, maxAllowedCoinbase, allowedBlockReward, totalFees)
	}

	if strings.EqualFold(coinbaseTx.Recipient, "SYSTEM") {
		return fmt.Errorf("block #%d coinbase recipient cannot be SYSTEM", b.Index)
	}
	if !ValidateAddress(coinbaseTx.Recipient) {
		return fmt.Errorf("block #%d invalid coinbase recipient address: %s", b.Index, coinbaseTx.Recipient)
	}

	minerHex, err := AddressToHexCached(coinbaseTx.Recipient)
	if err != nil {
		return fmt.Errorf("block #%d invalid miner coinbase recipient address: %v", b.Index, err)
	}

	minerAcc := getAccount(minerHex)
	if minerAcc.Balance > MaxSupply-coinbaseTx.Amount {
		return fmt.Errorf("block #%d miner balance exceeds MaxSupply", b.Index)
	}
	minerAcc.Balance += coinbaseTx.Amount
	if maturity > 0 && coinbaseTx.Amount > 0 {
		minerAcc.Immature = append(minerAcc.Immature, ImmatureReward{
			Amount: coinbaseTx.Amount,
			Height: b.Index,
		})
	}
	minerAcc = minerAcc.PruneMatured(b.Index, maturity)
	deltaAccounts[minerHex] = minerAcc

	// Commit state updates atomically into authoritative ledger
	for addrHex, updatedAcc := range deltaAccounts {
		sm.Accounts[addrHex] = updatedAcc
	}
	return nil
}
