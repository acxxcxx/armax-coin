package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MinedBlockStatus represents the lifecycle of a locally mined block relative to the network main chain.
type MinedBlockStatus string

const (
	MinedStatusRelayed    MinedBlockStatus = "RELAYED"    // Accepted by local tip & relayed, awaiting peer confirmation
	MinedStatusConfirming MinedBlockStatus = "CONFIRMING" // Verified on main chain with >= 2 confirmations, < maturity
	MinedStatusMature     MinedBlockStatus = "MATURE"     // Verified on main chain and reached coinbase maturity
	MinedStatusOrphaned   MinedBlockStatus = "ORPHANED"   // Replaced on main chain by another block (reorged out)
)

// SessionMinedBlock records a mined block candidate and its reconciled confirmation state.
type SessionMinedBlock struct {
	Height        int              `json:"height"`
	Hash          string           `json:"hash"`
	Reward        int64            `json:"reward"`
	FoundAt       int64            `json:"found_at"`
	PeersAtMine   int              `json:"peers_at_mine"`
	Confirmations int              `json:"confirmations"`
	Maturity      int              `json:"maturity"`
	Status        MinedBlockStatus `json:"status"`
}

// MiningSessionTracker reconciles blocks mined via POST /mine against the live main chain.
// It ensures blocks orphaned by faster P2P peers are not falsely shown as permanently credited.
type MiningSessionTracker struct {
	mu      sync.RWMutex
	entries []SessionMinedBlock
}

// NewMiningSessionTracker creates a new thread-safe mining block tracker.
func NewMiningSessionTracker() *MiningSessionTracker {
	return &MiningSessionTracker{
		entries: make([]SessionMinedBlock, 0, 16),
	}
}

// Reset clears all tracked session mined blocks.
func (t *MiningSessionTracker) Reset() {
	t.mu.Lock()
	t.entries = nil
	t.mu.Unlock()
}

// RecordFound records a newly accepted local block right after POST /mine succeeds.
func (t *MiningSessionTracker) RecordFound(height int, hash string, reward int64, peersAtMine int, maturity int) SessionMinedBlock {
	if maturity <= 0 {
		maturity = 100
	}
	entry := SessionMinedBlock{
		Height:        height,
		Hash:          strings.TrimSpace(hash),
		Reward:        reward,
		FoundAt:       time.Now().Unix(),
		PeersAtMine:   peersAtMine,
		Confirmations: 1,
		Maturity:      maturity,
		Status:        MinedStatusRelayed,
	}

	t.mu.Lock()
	// Prepend newest first and cap to 100 recent entries.
	t.entries = append([]SessionMinedBlock{entry}, t.entries...)
	if len(t.entries) > 100 {
		t.entries = t.entries[:100]
	}
	t.mu.Unlock()
	return entry
}

// Summary returns accurate session totals excluding ORPHANED blocks plus a snapshot of tracked entries.
func (t *MiningSessionTracker) Summary() (validBlocks int64, validReward int64, orphanedBlocks int64, entries []SessionMinedBlock) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entries = make([]SessionMinedBlock, len(t.entries))
	copy(entries, t.entries)

	for _, e := range t.entries {
		if e.Status == MinedStatusOrphaned {
			orphanedBlocks++
		} else {
			validBlocks++
			validReward += e.Reward
		}
	}
	return validBlocks, validReward, orphanedBlocks, entries
}

// Reconcile queries GET /block?index=<h> on rpcBaseURL for all non-final blocks and updates their state.
func (t *MiningSessionTracker) Reconcile(ctx context.Context, client *http.Client, rpcBaseURL string, tipHeight int, maturity int) (orphanEvents []SessionMinedBlock, changed bool) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	if maturity <= 0 {
		maturity = 100
	}
	baseURL := strings.TrimRight(rpcBaseURL, "/")

	t.mu.RLock()
	type pendingCheck struct {
		idx   int
		entry SessionMinedBlock
	}
	var toCheck []pendingCheck
	for i, e := range t.entries {
		if e.Status == MinedStatusRelayed || e.Status == MinedStatusConfirming {
			toCheck = append(toCheck, pendingCheck{idx: i, entry: e})
		}
	}
	t.mu.RUnlock()

	if len(toCheck) == 0 {
		return nil, false
	}

	type updateResult struct {
		hash   string
		height int
		status MinedBlockStatus
		confs  int
		mat    int
	}
	var updates []updateResult

	for _, item := range toCheck {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/block?index=%d", baseURL, item.entry.Height), nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		var blk struct {
			Index int    `json:"index"`
			Hash  string `json:"hash"`
		}
		decodeErr := error(nil)
		if resp.StatusCode == http.StatusOK {
			decodeErr = json.NewDecoder(resp.Body).Decode(&blk)
		}
		statusCode := resp.StatusCode
		_ = resp.Body.Close()

		newMat := maturity
		if item.entry.Maturity > 0 {
			newMat = item.entry.Maturity
		}

		if statusCode == http.StatusOK && decodeErr == nil {
			if strings.EqualFold(strings.TrimSpace(blk.Hash), item.entry.Hash) {
				confs := tipHeight - item.entry.Height + 1
				if confs < 1 {
					confs = 1
				}
				newStatus := MinedStatusRelayed
				if confs >= newMat {
					newStatus = MinedStatusMature
				} else if confs >= 2 {
					newStatus = MinedStatusConfirming
				}
				if newStatus != item.entry.Status || confs != item.entry.Confirmations || newMat != item.entry.Maturity {
					updates = append(updates, updateResult{
						hash:   item.entry.Hash,
						height: item.entry.Height,
						status: newStatus,
						confs:  confs,
						mat:    newMat,
					})
				}
			} else {
				// Another block owns this height on the main chain -> mined block was orphaned.
				updates = append(updates, updateResult{
					hash:   item.entry.Hash,
					height: item.entry.Height,
					status: MinedStatusOrphaned,
					confs:  0,
					mat:    newMat,
				})
			}
		} else if statusCode == http.StatusNotFound && tipHeight >= item.entry.Height {
			updates = append(updates, updateResult{
				hash:   item.entry.Hash,
				height: item.entry.Height,
				status: MinedStatusOrphaned,
				confs:  0,
				mat:    newMat,
			})
		}
	}

	if len(updates) == 0 {
		return nil, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, upd := range updates {
		for i := range t.entries {
			if t.entries[i].Height == upd.height && t.entries[i].Hash == upd.hash {
				prevStatus := t.entries[i].Status
				if prevStatus == MinedStatusOrphaned || prevStatus == MinedStatusMature {
					break
				}
				t.entries[i].Status = upd.status
				t.entries[i].Confirmations = upd.confs
				t.entries[i].Maturity = upd.mat
				changed = true
				if upd.status == MinedStatusOrphaned && prevStatus != MinedStatusOrphaned {
					orphanEvents = append(orphanEvents, t.entries[i])
				}
				break
			}
		}
	}
	return orphanEvents, changed
}
