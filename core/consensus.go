package core

import (
	"fmt"
	"log"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	// Version defines the canonical software and consensus protocol release version [R-10]
	Version = "1.0.1"

	// MaxReorganizationDepth defines the maximum depth of reorganization allowed (prevents deep 51% history overwrites, 720 blocks = ~1 day at 120s) [D-05]
	MaxReorganizationDepth = 720
)

// Checkpoints allows locking critical historic block hashes to prevent deep reorganization
var Checkpoints = map[int]string{
	0: GenesisHash,
}

// CalculateChainwork computes the cumulative Proof-of-Work (Chainwork) of a block sequence
// based on Satoshi's 2^256 / (target + 1) work formula.
func CalculateChainwork(chain []Block) *big.Int {
	totalWork := big.NewInt(0)
	two256 := new(big.Int).Lsh(big.NewInt(1), 256)

	for _, b := range chain {
		target := b.GetTarget()
		if target == nil || target.Sign() <= 0 {
			target = DifficultyToTarget(1)
		}
		divisor := new(big.Int).Add(target, big.NewInt(1))
		work := new(big.Int).Div(two256, divisor)
		totalWork.Add(totalWork, work)
	}
	return totalWork
}

// CalculateMedianTimePast computes the median timestamp of up to 11 previous blocks (BIP-113 standard)
func CalculateMedianTimePast(chain []Block, upToIndex int) int64 {
	if upToIndex <= 0 || len(chain) == 0 {
		return 0
	}
	start := upToIndex - 11
	if start < 0 {
		start = 0
	}
	timestamps := make([]int64, 0, upToIndex-start)
	for i := start; i < upToIndex && i < len(chain); i++ {
		timestamps = append(timestamps, chain[i].Timestamp)
	}
	if len(timestamps) == 0 {
		return 0
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	return timestamps[len(timestamps)/2]
}

// validateBlockContext performs comprehensive header, contextual, PoW, MTP-11, and size validation
// shared identically between ValidateAndAddBlock and ResolveChain [C-04, C-05, C-06, C-18].
func validateBlockContext(b *Block, prev *Block, chainHistory []Block) error {
	// 0. Size constraints [C-04]
	if b.Size() > MaxBlockSize {
		return fmt.Errorf("block size %d exceeds MaxBlockSize (%d)", b.Size(), MaxBlockSize)
	}
	for idx, tx := range b.Transactions {
		if tx.Size() > MaxTxSize {
			return fmt.Errorf("tx #%d size %d exceeds MaxTxSize (%d)", idx, tx.Size(), MaxTxSize)
		}
	}

	// 1. Transaction count constraints
	if len(b.Transactions) == 0 || len(b.Transactions) > MaxBlockTransactions {
		return fmt.Errorf("invalid tx count (%d)", len(b.Transactions))
	}

	// [C-26] Block.Miner must pass ValidateAddress and match coinbase recipient
	if !ValidateAddress(b.Miner) {
		return fmt.Errorf("block #%d invalid miner address '%s'", b.Index, b.Miner)
	}
	coinbaseRecipient := b.Transactions[0].Recipient
	if !MatchAddress(b.Miner, coinbaseRecipient) {
		return fmt.Errorf("block #%d miner mismatch: header miner '%s' does not match coinbase recipient '%s'", b.Index, b.Miner, coinbaseRecipient)
	}

	// 2. Duplicate transaction check within the block [C-18]
	seenTxs := make(map[string]struct{}, len(b.Transactions))
	for idx, tx := range b.Transactions {
		h := tx.HashHex()
		if _, exists := seenTxs[h]; exists {
			return fmt.Errorf("duplicate transaction %s at index #%d", h, idx)
		}
		seenTxs[h] = struct{}{}
	}

	// 3. Sequential index linkage
	if b.Index != prev.Index+1 {
		return fmt.Errorf("non-sequential index (latest: #%d, got: #%d)", prev.Index, b.Index)
	}

	// 4. Previous hash linkage
	if b.PrevHash != prev.Hash {
		return fmt.Errorf("PrevHash mismatch (got %s, expected %s)", b.PrevHash, prev.Hash)
	}

	// 5. Binary Merkle tree root verification
	calculatedMerkle := CalculateMerkleRoot(b.Transactions)
	if b.MerkleRoot != calculatedMerkle {
		return fmt.Errorf("merkle root mismatch (got %s, calculated %s)", b.MerkleRoot, calculatedMerkle)
	}

	// 6. Block header hash verification
	calcHash, err := b.CalculateHashWithError()
	if err != nil {
		return fmt.Errorf("block hash computation failed: %w", err)
	}
	if b.Hash == "" || b.Hash != calcHash {
		return fmt.Errorf("calculated hash mismatch")
	}

	// 7. LWMA Difficulty and Numerical Target [C-09]
	expectedTarget := CalculateExpectedTarget(chainHistory, b.Index)
	expectedTargetHex := fmt.Sprintf("%064x", expectedTarget)
	if b.Target != expectedTargetHex {
		return fmt.Errorf("target mismatch (got %s, expected %s)", b.Target, expectedTargetHex)
	}
	expectedDifficulty := TargetToDifficulty(expectedTarget)
	if b.Difficulty != expectedDifficulty {
		return fmt.Errorf("difficulty field mismatch (got %d, expected %d)", b.Difficulty, expectedDifficulty)
	}
	if !HashMeetsTarget(b.Hash, expectedTarget) {
		return fmt.Errorf("hash does not meet target")
	}

	// 8. Timestamp sanity checks [C-05, C-06, C-22]:
	// No future blocks > 2h
	maxFutureTimestamp := time.Now().Add(2 * time.Hour).Unix()
	if b.Timestamp > maxFutureTimestamp {
		return fmt.Errorf("timestamp %d too far in future (max: %d)", b.Timestamp, maxFutureTimestamp)
	}
	// Median-Time-Past 11 blocks (MTP-11) [C-05, C-06, C-22]
	mtp := CalculateMedianTimePast(chainHistory, b.Index)
	if b.Timestamp <= mtp {
		return fmt.Errorf("timestamp %d <= MedianTimePast %d", b.Timestamp, mtp)
	}

	// 9. Checkpoint validation
	if expectedHash, exists := Checkpoints[b.Index]; exists && expectedHash != "" {
		if b.Hash != expectedHash {
			return fmt.Errorf("checkpoint mismatch for block #%d: expected %s, got %s", b.Index, expectedHash, b.Hash)
		}
	}

	return nil
}

// ValidateBlock validates a candidate against a disposable state copy.
// It must not modify the active blockchain state.
func (bc *Blockchain) ValidateBlock(newBlock Block) error {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()

	if bc.State == nil {
		return fmt.Errorf("blockchain state is not initialized")
	}

	candidateState := bc.State.Clone()
	return bc.validateBlockAgainstStateLocked(newBlock, candidateState)
}

// ValidateBlockLocked validates and applies a block to the supplied active
// state. The caller must hold bc.Lock for writing.
func (bc *Blockchain) ValidateBlockLocked(newBlock Block) error {
	return bc.validateBlockAgainstStateLocked(newBlock, bc.State)
}

// validateBlockAgainstStateLocked validates the block and applies its
// transactions only to the state passed to this helper.
func (bc *Blockchain) validateBlockAgainstStateLocked(
	newBlock Block,
	state *StateManager,
) error {
	if state == nil {
		return fmt.Errorf("blockchain state is not initialized")
	}

	latestBlock := bc.GetLatestBlockUnsafe()

	if err := validateBlockContext(
		&newBlock,
		&latestBlock,
		bc.Chain,
	); err != nil {
		return err
	}

	blockReward := bc.GetBlockReward(newBlock.Index)

	if err := state.VerifyAndApplyBlock(
		&newBlock,
		blockReward,
		bc.GetCoinbaseMaturity(),
	); err != nil {
		return fmt.Errorf("state validation failed: %w", err)
	}

	return nil
}

// PreCheckBlock performs inexpensive stateless and tip-contextual validation on an incoming block
// WITHOUT acquiring the global write lock and WITHOUT cloning the ledger state [T2].
// It returns an error if the block violates structural constraints, Merkle tree root, header hash,
// PoW target difficulty, timestamp sanity, or linkage to the current tip.
func (bc *Blockchain) PreCheckBlock(b *Block) error {
	if b == nil {
		return fmt.Errorf("block is nil")
	}

	// 0. Size constraints [C-04]
	if b.Size() > MaxBlockSize {
		return fmt.Errorf("block size %d exceeds MaxBlockSize (%d)", b.Size(), MaxBlockSize)
	}
	for idx, tx := range b.Transactions {
		if tx.Size() > MaxTxSize {
			return fmt.Errorf("tx #%d size %d exceeds MaxTxSize (%d)", idx, tx.Size(), MaxTxSize)
		}
	}

	// 1. Transaction count constraints
	if len(b.Transactions) == 0 || len(b.Transactions) > MaxBlockTransactions {
		return fmt.Errorf("invalid tx count (%d)", len(b.Transactions))
	}

	// 2. Miner address and coinbase recipient match [C-26]
	if !ValidateAddress(b.Miner) {
		return fmt.Errorf("block #%d invalid miner address '%s'", b.Index, b.Miner)
	}
	coinbaseRecipient := b.Transactions[0].Recipient
	if !MatchAddress(b.Miner, coinbaseRecipient) {
		return fmt.Errorf("block #%d miner mismatch: header miner '%s' does not match coinbase recipient '%s'", b.Index, b.Miner, coinbaseRecipient)
	}

	// 3. Duplicate transactions within the block [C-18]
	seenTxs := make(map[string]struct{}, len(b.Transactions))
	for idx, tx := range b.Transactions {
		h := tx.HashHex()
		if _, exists := seenTxs[h]; exists {
			return fmt.Errorf("duplicate transaction %s at index #%d", h, idx)
		}
		seenTxs[h] = struct{}{}
	}

	// 4. Binary Merkle tree root verification
	calculatedMerkle := CalculateMerkleRoot(b.Transactions)
	if b.MerkleRoot != calculatedMerkle {
		return fmt.Errorf("merkle root mismatch (got %s, calculated %s)", b.MerkleRoot, calculatedMerkle)
	}

	// 5. Header hash calculation verification
	calcHash, err := b.CalculateHashWithError()
	if err != nil {
		return fmt.Errorf("block hash computation failed: %w", err)
	}
	if b.Hash == "" || b.Hash != calcHash {
		return fmt.Errorf("calculated hash mismatch")
	}

	// 6. Chain tip linkage and expected target under RLock (no write lock, no state clone)
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()

	latest := bc.GetLatestBlockUnsafe()
	if b.Index != latest.Index+1 {
		return fmt.Errorf("non-sequential index (latest: #%d, got: #%d)", latest.Index, b.Index)
	}
	if b.PrevHash != latest.Hash {
		return fmt.Errorf("PrevHash mismatch (got %s, expected %s)", b.PrevHash, latest.Hash)
	}

	expectedTarget := CalculateExpectedTarget(bc.Chain, b.Index)
	expectedTargetHex := fmt.Sprintf("%064x", expectedTarget)
	if b.Target != expectedTargetHex {
		return fmt.Errorf("target mismatch (got %s, expected %s)", b.Target, expectedTargetHex)
	}
	expectedDifficulty := TargetToDifficulty(expectedTarget)
	if b.Difficulty != expectedDifficulty {
		return fmt.Errorf("difficulty field mismatch (got %d, expected %d)", b.Difficulty, expectedDifficulty)
	}
	if !HashMeetsTarget(calcHash, expectedTarget) {
		return fmt.Errorf("hash does not meet expected target")
	}

	// 7. Timestamp sanity checks [C-05, C-06, C-22]
	maxFutureTimestamp := time.Now().Add(2 * time.Hour).Unix()
	if b.Timestamp > maxFutureTimestamp {
		return fmt.Errorf("timestamp %d too far in future (max: %d)", b.Timestamp, maxFutureTimestamp)
	}
	mtp := CalculateMedianTimePast(bc.Chain, b.Index)
	if b.Timestamp <= mtp {
		return fmt.Errorf("timestamp %d <= MedianTimePast %d", b.Timestamp, mtp)
	}

	return nil
}

// ValidateAndAddBlock validates an incoming block from the P2P network and appends it if valid
func (bc *Blockchain) ValidateAndAddBlock(newBlock Block) bool {
	bc.Lock.Lock()
	defer bc.Lock.Unlock()

	// Capture undo record of accounts before block application [C-24, C-25]
	undo := &BlockUndoRecord{
		Height:   newBlock.Index,
		PrevAccs: make(map[string]AccountState),
	}
	if len(newBlock.Transactions) > 0 && bc.State != nil {
		minerHex, err := AddressToHex(newBlock.Transactions[0].Recipient)
		if err == nil {
			if acc, ok := bc.State.Accounts[minerHex]; ok {
				undo.PrevAccs[minerHex] = cloneAccountState(acc)
			} else {
				undo.PrevAccs[minerHex] = AccountState{Balance: 0, Nonce: 0}
			}
		}
		for _, tx := range newBlock.Transactions[1:] {
			sHex, err := AddressToHex(tx.Sender)
			if err == nil {
				if _, exists := undo.PrevAccs[sHex]; !exists {
					if acc, ok := bc.State.Accounts[sHex]; ok {
						undo.PrevAccs[sHex] = cloneAccountState(acc)
					} else {
						undo.PrevAccs[sHex] = AccountState{Balance: 0, Nonce: 0}
					}
				}
			}
			rHex, err := AddressToHex(tx.Recipient)
			if err == nil {
				if _, exists := undo.PrevAccs[rHex]; !exists {
					if acc, ok := bc.State.Accounts[rHex]; ok {
						undo.PrevAccs[rHex] = cloneAccountState(acc)
					} else {
						undo.PrevAccs[rHex] = AccountState{Balance: 0, Nonce: 0}
					}
				}
			}
		}
	}

	// Snapshot state before validation because validation applies transactions
	// to the active state.
	oldState := bc.State.Clone()

	oldChainLen := len(bc.Chain)
	oldDifficulty := bc.Difficulty
	oldBlockReward := bc.BlockReward
	oldCachedSupply := bc.cachedSupply
	oldCachedTotalTx := bc.cachedTotalTx

	var oldCachedWork *big.Int
	if bc.cachedWork != nil {
		oldCachedWork = new(big.Int).Set(bc.cachedWork)
	}

	if err := bc.ValidateBlockLocked(newBlock); err != nil {
		// Validation may have partially changed state; restore it.
		bc.State = oldState
		bc.Chain = bc.Chain[:oldChainLen]
		bc.Difficulty = oldDifficulty
		bc.BlockReward = oldBlockReward
		bc.cachedSupply = oldCachedSupply
		bc.cachedTotalTx = oldCachedTotalTx
		bc.cachedWork = oldCachedWork

		fmt.Printf(
			"[Consensus] [!] Rejected block #%d: %v\n",
			newBlock.Index,
			err,
		)
		return false
	}

	// Block is 100% valid! Append to chain

	if bc.cachedSupply <= 0 && oldChainLen > 0 {
		bc.cachedSupply = CalculateCirculatingSupply(bc.Chain)
	}
	blockReward := bc.GetBlockReward(newBlock.Index)
	bc.Chain = append(bc.Chain, newBlock)
	bc.AdjustDifficulty()
	bc.cachedSupply += blockReward
	if bc.cachedSupply > MaxSupply {
		bc.cachedSupply = MaxSupply
	}
	bc.BlockReward = bc.GetBlockReward(newBlock.Index + 1)
	bc.cachedTotalTx += int64(len(newBlock.Transactions))

	// Incrementally update cachedWork
	if bc.cachedWork != nil {
		target := newBlock.GetTarget()
		if target == nil || target.Sign() <= 0 {
			target = DifficultyToTarget(1)
		}
		div := new(big.Int).Add(target, big.NewInt(1))
		bw := new(big.Int).Div(Two256, div)
		bc.cachedWork = new(big.Int).Add(bc.cachedWork, bw)
	} else {
		bc.cachedWork = CalculateChainwork(bc.Chain)
	}

	// Persist the block before cleaning the mempool or reporting success.
	// SaveBlockToStore must persist cachedWork without incrementing it again.
	if !bc.storeFailed && bc.StoragePath != "IN_MEMORY" && bc.StoragePath != ":memory:" {
		if err := bc.SaveBlockToStore(&newBlock, undo); err != nil {
			bc.State = oldState
			bc.Chain = bc.Chain[:oldChainLen]
			bc.Difficulty = oldDifficulty
			bc.BlockReward = oldBlockReward
			bc.cachedSupply = oldCachedSupply
			bc.cachedTotalTx = oldCachedTotalTx
			bc.cachedWork = oldCachedWork

			fmt.Printf(
				"[Consensus] failed to persist block #%d; state rolled back: %v\n",
				newBlock.Index,
				err,
			)
			return false
		}
	} else {
		if bc.undoHistory == nil {
			bc.undoHistory = make(map[int]*BlockUndoRecord)
		}
		bc.undoHistory[newBlock.Index] = undo
		if newBlock.Index > UndoRecordRetention {
			delete(bc.undoHistory, newBlock.Index-UndoRecordRetention)
		}
	}

	// Only clean the mempool after persistence succeeds.
	if len(bc.Mempool) > 0 {
		var remainingMempool []Transaction

		for _, mTx := range bc.Mempool {
			isConfirmed := false
			mHash := mTx.HashHex()

			for _, bTx := range newBlock.Transactions {
				if bTx.HashHex() == mHash {
					isConfirmed = true
					break
				}
			}
			if !isConfirmed {
				remainingMempool = append(remainingMempool, mTx)
			}
		}
		bc.Mempool = remainingMempool
		bc.CleanMempoolLocked()
	}

	return true
}

// ResolveChain resolves blockchain forks using the Satoshi Cumulative Proof-of-Work (Chainwork) rule,
// ResolveFork reorganizes the local blockchain to a candidate fork branch starting at ancestorIndex [C-24].
// It rolls back the state using undo records, validates ONLY the candidate blocks in newBlocks,
// enforces the Satoshi cumulative chainwork rule, bounded reorg depth (<= 720), and atomic state updates.
func (bc *Blockchain) ResolveFork(ancestorIndex int, newBlocks []Block) error {
	bc.Lock.RLock()
	localLen := len(bc.Chain)
	if ancestorIndex < 0 || ancestorIndex >= localLen {
		bc.Lock.RUnlock()
		return fmt.Errorf("invalid ancestor index: %d (local chain length: %d)", ancestorIndex, localLen)
	}

	// [C-24] Reorganization depth check
	reorgDepth := (localLen - 1) - ancestorIndex
	if reorgDepth > MaxReorganizationDepth {
		bc.Lock.RUnlock()
		return fmt.Errorf("fork depth %d exceeds maximum allowed reorg depth of %d blocks", reorgDepth, MaxReorganizationDepth)
	}

	if len(newBlocks) == 0 {
		bc.Lock.RUnlock()
		return fmt.Errorf("candidate block segment is empty")
	}

	// Verify linkage of newBlocks to ancestor
	ancestor := bc.Chain[ancestorIndex]
	if newBlocks[0].Index != ancestor.Index+1 {
		bc.Lock.RUnlock()
		return fmt.Errorf("first candidate block index #%d does not match expected #%d", newBlocks[0].Index, ancestor.Index+1)
	}
	if newBlocks[0].PrevHash != ancestor.Hash {
		bc.Lock.RUnlock()
		return fmt.Errorf("first candidate block #%d PrevHash mismatch (got %s, expected %s)", newBlocks[0].Index, newBlocks[0].PrevHash, ancestor.Hash)
	}
	for i := 1; i < len(newBlocks); i++ {
		if newBlocks[i].Index != newBlocks[i-1].Index+1 {
			bc.Lock.RUnlock()
			return fmt.Errorf("candidate blocks are not contiguous: block #%d follows #%d", newBlocks[i].Index, newBlocks[i-1].Index)
		}
		if newBlocks[i].PrevHash != newBlocks[i-1].Hash {
			bc.Lock.RUnlock()
			return fmt.Errorf("candidate block #%d PrevHash mismatch", newBlocks[i].Index)
		}
	}

	// Calculate cumulative chainwork
	localWork := CalculateChainwork(bc.Chain)
	candidateWork := CalculateChainwork(bc.Chain[:ancestorIndex+1])
	for _, b := range newBlocks {
		target := b.GetTarget()
		if target == nil || target.Sign() <= 0 {
			target = DifficultyToTarget(1)
		}
		div := new(big.Int).Add(target, big.NewInt(1))
		bw := new(big.Int).Div(Two256, div)
		candidateWork = new(big.Int).Add(candidateWork, bw)
	}

	if candidateWork.Cmp(localWork) <= 0 {
		bc.Lock.RUnlock()
		return fmt.Errorf("candidate chainwork is not strictly greater than active chainwork")
	}

	// Snapshot state for tentative rollback and candidate validation
	var clonedState *StateManager
	if bc.State != nil {
		clonedState = bc.State.Clone()
	} else {
		clonedState = NewStateManager()
	}

	prefixChain := make([]Block, ancestorIndex+1, ancestorIndex+1+len(newBlocks))
	copy(prefixChain, bc.Chain[:ancestorIndex+1])
	bc.Lock.RUnlock()

	// Rollback state from localLen-1 down to ancestorIndex+1 using undo records [C-24]
	for h := localLen - 1; h > ancestorIndex; h-- {
		undo, err := bc.GetBlockUndoRecord(h)
		if err == nil && undo != nil {
			for hexAddr, prevAcc := range undo.PrevAccs {
				if prevAcc.Balance == 0 && prevAcc.Nonce == 0 && len(prevAcc.Immature) == 0 {
					delete(clonedState.Accounts, hexAddr)
				} else {
					clonedState.Accounts[hexAddr] = cloneAccountState(prevAcc)
				}
			}
		} else {
			// Fallback: Rebuild state up to ancestorIndex if undo records unavailable
			clonedState = NewStateManager()
			clonedState.CoinbaseMaturity = bc.GetCoinbaseMaturity()
			var fallbackSupply int64 = 0
			for i := 0; i <= ancestorIndex; i++ {
				r := CalculateBlockRewardGivenSupply(i, fallbackSupply)
				if err := clonedState.VerifyAndApplyBlock(&prefixChain[i], r, bc.GetCoinbaseMaturity()); err != nil {
					return fmt.Errorf("failed to reconstruct state at ancestor: %w", err)
				}
				fallbackSupply += r
			}
			break
		}
	}

	// Compute running supply at ancestorIndex
	var runningSupply int64 = 0
	for i := 0; i <= ancestorIndex; i++ {
		r := CalculateBlockRewardGivenSupply(i, runningSupply)
		runningSupply += r
	}

	// Validate and execute candidate newBlocks sequentially against clonedState while recording undo entries
	simulatedChain := append(prefixChain, newBlocks...)
	newUndos := make(map[int]*BlockUndoRecord, len(newBlocks))

	for i := 0; i < len(newBlocks); i++ {
		curr := &newBlocks[i]
		idxInSim := ancestorIndex + 1 + i
		prev := &simulatedChain[idxInSim-1]

		if err := validateBlockContext(curr, prev, simulatedChain[:idxInSim]); err != nil {
			return fmt.Errorf("candidate block #%d validation failed: %w", curr.Index, err)
		}

		undo := &BlockUndoRecord{
			Height:   curr.Index,
			PrevAccs: make(map[string]AccountState),
		}
		if len(curr.Transactions) > 0 {
			if minerHex, err := AddressToHex(curr.Transactions[0].Recipient); err == nil {
				if acc, ok := clonedState.Accounts[minerHex]; ok {
					undo.PrevAccs[minerHex] = cloneAccountState(acc)
				} else {
					undo.PrevAccs[minerHex] = AccountState{}
				}
			}
			for _, tx := range curr.Transactions[1:] {
				if sHex, err := AddressToHex(tx.Sender); err == nil {
					if _, exists := undo.PrevAccs[sHex]; !exists {
						if acc, ok := clonedState.Accounts[sHex]; ok {
							undo.PrevAccs[sHex] = cloneAccountState(acc)
						} else {
							undo.PrevAccs[sHex] = AccountState{}
						}
					}
				}
				if rHex, err := AddressToHex(tx.Recipient); err == nil {
					if _, exists := undo.PrevAccs[rHex]; !exists {
						if acc, ok := clonedState.Accounts[rHex]; ok {
							undo.PrevAccs[rHex] = cloneAccountState(acc)
						} else {
							undo.PrevAccs[rHex] = AccountState{}
						}
					}
				}
			}
		}

		reward := CalculateBlockRewardGivenSupply(curr.Index, runningSupply)
		if err := clonedState.VerifyAndApplyBlock(curr, reward, bc.GetCoinbaseMaturity()); err != nil {
			return fmt.Errorf("candidate block #%d state execution failed: %w", curr.Index, err)
		}
		newUndos[curr.Index] = undo
		runningSupply += reward
	}

	// Step 4: Acquire write lock for the atomic switch
	bc.Lock.Lock()
	defer bc.Lock.Unlock()

	// Re-verify that local chainwork hasn't surpassed candidate chainwork while validating
	if candidateWork.Cmp(CalculateChainwork(bc.Chain)) <= 0 {
		return fmt.Errorf("candidate chainwork is no longer greater than active chainwork")
	}

	// Re-verify reorg depth against current chain under write lock
	currentLocalLen := len(bc.Chain)
	if ancestorIndex >= currentLocalLen || bc.Chain[ancestorIndex].Hash != ancestor.Hash {
		return fmt.Errorf("local chain reorganized concurrently during fork resolution")
	}
	currentReorgDepth := (currentLocalLen - 1) - ancestorIndex
	if currentReorgDepth > MaxReorganizationDepth {
		return fmt.Errorf("fork depth %d exceeds maximum allowed reorg depth of %d blocks", currentReorgDepth, MaxReorganizationDepth)
	}

	// Collect orphaned transactions from overridden local blocks
	var orphanedTxs []Transaction
	for i := ancestorIndex + 1; i < currentLocalLen; i++ {
		for _, tx := range bc.Chain[i].Transactions {
			if !strings.EqualFold(tx.Sender, "SYSTEM") {
				orphanedTxs = append(orphanedTxs, tx)
			}
		}
	}

	// Reschedule valid unconfirmed transactions back into mempool
	candidateTxMap := make(map[string]struct{})
	for _, b := range newBlocks {
		for _, tx := range b.Transactions {
			candidateTxMap[tx.HashHex()] = struct{}{}
		}
	}

	for _, oTx := range orphanedTxs {
		if _, confirmed := candidateTxMap[oTx.HashHex()]; !confirmed {
			// [F6-PERF] Cryptographic signature verification for orphaned transactions re-entering mempool
			if oTx.Verify() {
				bc.Mempool = append(bc.Mempool, oTx)
			}
		}
	}

	// Replace active chain and state atomically
	bc.Chain = append(bc.Chain[:ancestorIndex+1], newBlocks...)
	bc.State = clonedState
	bc.cachedSupply = runningSupply
	bc.cachedWork = candidateWork
	if bc.undoHistory == nil {
		bc.undoHistory = make(map[int]*BlockUndoRecord)
	}
	for h := ancestorIndex + 1; h < currentLocalLen; h++ {
		delete(bc.undoHistory, h)
	}
	for h, u := range newUndos {
		bc.undoHistory[h] = u
		if h > UndoRecordRetention {
			delete(bc.undoHistory, h-UndoRecordRetention)
		}
	}
	var reorgTotalTx int64 = 0
	for _, b := range bc.Chain {
		reorgTotalTx += int64(len(b.Transactions))
	}
	bc.cachedTotalTx = reorgTotalTx
	bc.AdjustDifficulty()
	bc.BlockReward = CalculateBlockRewardGivenSupply(len(bc.Chain), runningSupply)

	bc.CleanMempoolLocked()
	if !bc.storeFailed && bc.StoragePath != "IN_MEMORY" && (bc.db != nil || bc.StoragePath != "") {
		if err := bc.SaveToFile(); err != nil {
			return fmt.Errorf("failed to persist blockchain state to disk after reorg: %w", err)
		}
	}

	topHeight := len(bc.Chain) - 1
	totalBlocks := len(bc.Chain)
	latest := bc.Chain[topHeight]
	hashPrefix := latest.Hash
	if len(hashPrefix) > 16 {
		hashPrefix = hashPrefix[:16] + "..."
	}
	if ancestorIndex < currentLocalLen-1 {
		log.Printf("[Consensus] [OK] Successfully reorganized chain at ancestor #%d! New height: #%d (%d blocks, tip: %s)\n", ancestorIndex, topHeight, totalBlocks, hashPrefix)
	} else {
		log.Printf("[Consensus] [OK] Successfully synchronized %d blocks from ancestor #%d! New height: #%d (%d blocks, tip: %s)\n", len(newBlocks), ancestorIndex, topHeight, totalBlocks, hashPrefix)
	}
	return nil
}

// ResolveChain resolves blockchain forks using the Satoshi Cumulative Proof-of-Work (Chainwork) rule.
// It locates the common ancestor fork point and delegates to ResolveFork [C-24].
func (bc *Blockchain) ResolveChain(candidateChain []Block) bool {
	if len(candidateChain) == 0 {
		return false
	}

	// [C-23] Never trust peer block 0; validate strictly against Genesis specification
	if err := ValidateGenesisBlock(&candidateChain[0]); err != nil {
		fmt.Printf("[Consensus] Candidate chain rejected: invalid genesis block 0: %v\n", err)
		return false
	}
	candidateChain[0] = GetGenesisBlock()

	bc.Lock.RLock()
	localLen := len(bc.Chain)
	minLen := localLen
	if len(candidateChain) < minLen {
		minLen = len(candidateChain)
	}
	forkPoint := -1
	for i := 0; i < minLen; i++ {
		if bc.Chain[i].Hash == candidateChain[i].Hash {
			forkPoint = i
		} else {
			break
		}
	}
	bc.Lock.RUnlock()

	if forkPoint == -1 || forkPoint >= len(candidateChain)-1 {
		return false
	}

	err := bc.ResolveFork(forkPoint, candidateChain[forkPoint+1:])
	if err != nil {
		fmt.Printf("[Consensus] ResolveFork failed: %v\n", err)
		return false
	}
	return true
}

// GetTotalChainworkUnsafe returns the cumulative Proof-of-Work assuming bc.Lock is already held
func (bc *Blockchain) GetTotalChainworkUnsafe() *big.Int {
	if bc.cachedWork != nil {
		return new(big.Int).Set(bc.cachedWork)
	}
	return CalculateChainwork(bc.Chain)
}

// GetTotalChainwork returns the cumulative Proof-of-Work of the active blockchain in O(1) [C-24, N-14]
func (bc *Blockchain) GetTotalChainwork() *big.Int {
	bc.Lock.RLock()
	defer bc.Lock.RUnlock()
	return bc.GetTotalChainworkUnsafe()
}

// InvalidateChainworkCache resets the chainwork cache when a reorganization occurs
func (bc *Blockchain) InvalidateChainworkCache() {
	bc.cachedWork = nil
}
