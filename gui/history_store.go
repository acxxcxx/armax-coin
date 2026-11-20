package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

var syncMetaBucket = []byte("sync_meta")

// HistorySyncMeta captures the local bbolt cache watermark and delta-sync telemetry.
type HistorySyncMeta struct {
	SyncedHeight  int    `json:"synced_height"`
	SyncedTipHash string `json:"synced_tip_hash"`
	UpdatedAt     int64  `json:"updated_at"`
	CacheHit      bool   `json:"cache_hit"`
	DeltaBlocks   int    `json:"delta_blocks"`
	ReorgReset    bool   `json:"reorg_reset"`
}

// HistoryStore persists confirmed transaction history per (networkID, address) in bbolt
// so GUI navigation never re-scans genesis repeatedly while supporting O(delta) updates.
type HistoryStore struct {
	db     *bbolt.DB
	syncMu sync.Mutex
}

// OpenHistoryStore opens or creates the local bbolt transaction cache at dbPath.
func OpenHistoryStore(dbPath string) (*HistoryStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("create history store dir: %w", err)
		}
	}
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt history store: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(syncMetaBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &HistoryStore{db: db}, nil
}

// Close closes the underlying bbolt database handle.
func (s *HistoryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func historyAddrScope(netID, address string) string {
	return strings.TrimSpace(netID) + ":" + strings.ToLower(strings.TrimSpace(address))
}

func historyTxBucketName(netID, address string) []byte {
	return []byte("txs:" + historyAddrScope(netID, address))
}

func historyRecordKey(rec PagedHistoryRecord) []byte {
	h := rec.TxHash
	if h == "" {
		h = rec.Tx.HashHex()
	}
	return []byte(fmt.Sprintf("%012d:%s", rec.BlockIndex, h))
}

func sortHistoryRecordsDesc(records []PagedHistoryRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].BlockIndex != records[j].BlockIndex {
			return records[i].BlockIndex > records[j].BlockIndex
		}
		if records[i].Timestamp != records[j].Timestamp {
			return records[i].Timestamp > records[j].Timestamp
		}
		return records[i].TxHash < records[j].TxHash
	})
}

// LoadConfirmed reads all confirmed records and sync metadata cached for (netID, address).
func (s *HistoryStore) LoadConfirmed(netID, address string) ([]PagedHistoryRecord, HistorySyncMeta, error) {
	meta := HistorySyncMeta{SyncedHeight: -1}
	if s == nil || s.db == nil {
		return nil, meta, nil
	}
	scope := []byte(historyAddrScope(netID, address))
	bName := historyTxBucketName(netID, address)
	out := make([]PagedHistoryRecord, 0, 64)

	err := s.db.View(func(tx *bbolt.Tx) error {
		if mb := tx.Bucket(syncMetaBucket); mb != nil {
			if rawMeta := mb.Get(scope); len(rawMeta) > 0 {
				var loaded HistorySyncMeta
				if json.Unmarshal(rawMeta, &loaded) == nil {
					meta = loaded
				}
			}
		}
		tb := tx.Bucket(bName)
		if tb == nil {
			return nil
		}
		return tb.ForEach(func(k, v []byte) error {
			var rec PagedHistoryRecord
			if json.Unmarshal(v, &rec) == nil {
				if rec.TxHash == "" {
					rec.TxHash = rec.Tx.HashHex()
				}
				out = append(out, rec)
			}
			return nil
		})
	})
	if err != nil {
		return nil, meta, err
	}
	sortHistoryRecordsDesc(out)
	return out, meta, nil
}

// SaveDelta persists newly confirmed transactions and updates the height/hash watermark.
func (s *HistoryStore) SaveDelta(netID, address string, confirmed []PagedHistoryRecord, syncedHeight int, tipHash string) error {
	if s == nil || s.db == nil {
		return nil
	}
	scope := []byte(historyAddrScope(netID, address))
	bName := historyTxBucketName(netID, address)

	return s.db.Update(func(tx *bbolt.Tx) error {
		tb, err := tx.CreateBucketIfNotExists(bName)
		if err != nil {
			return err
		}
		for _, rec := range confirmed {
			if rec.BlockIndex < 0 {
				continue
			}
			if rec.TxHash == "" {
				rec.TxHash = rec.Tx.HashHex()
			}
			raw, mErr := json.Marshal(rec)
			if mErr != nil {
				return mErr
			}
			if pErr := tb.Put(historyRecordKey(rec), raw); pErr != nil {
				return pErr
			}
		}

		mb, err := tx.CreateBucketIfNotExists(syncMetaBucket)
		if err != nil {
			return err
		}
		meta := HistorySyncMeta{
			SyncedHeight:  syncedHeight,
			SyncedTipHash: strings.TrimSpace(tipHash),
			UpdatedAt:     time.Now().Unix(),
		}
		metaBytes, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return mb.Put(scope, metaBytes)
	})
}

// ResetAddress deletes cached confirmed records and sync metadata for (netID, address).
func (s *HistoryStore) ResetAddress(netID, address string) error {
	if s == nil || s.db == nil {
		return nil
	}
	scope := []byte(historyAddrScope(netID, address))
	bName := historyTxBucketName(netID, address)
	return s.db.Update(func(tx *bbolt.Tx) error {
		if mb := tx.Bucket(syncMetaBucket); mb != nil {
			_ = mb.Delete(scope)
		}
		if tx.Bucket(bName) != nil {
			return tx.DeleteBucket(bName)
		}
		return nil
	})
}

func verifyBlockHashMatches(ctx context.Context, client *http.Client, rpcBaseURL string, expectedNetID string, height int, expectedHash string) (matches bool, reachable bool) {
	if expectedHash == "" || height < 0 {
		return true, true
	}
	urlStr := fmt.Sprintf("%s/block?index=%d", strings.TrimRight(rpcBaseURL, "/"), height)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return true, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return true, false
	}
	defer resp.Body.Close()

	if expectedNetID != "" {
		if hdr := strings.TrimSpace(resp.Header.Get("X-Network-ID")); hdr != "" && !strings.EqualFold(hdr, expectedNetID) {
			return true, false
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, true
	}
	if resp.StatusCode != http.StatusOK {
		return true, false
	}
	var blk struct {
		Hash string `json:"hash"`
	}
	if json.NewDecoder(resp.Body).Decode(&blk) != nil {
		return true, false
	}
	return strings.EqualFold(strings.TrimSpace(blk.Hash), strings.TrimSpace(expectedHash)), true
}

func mergePendingAndConfirmed(pending []PagedHistoryRecord, confirmed []PagedHistoryRecord) []PagedHistoryRecord {
	seen := make(map[string]bool, len(pending)+len(confirmed))
	merged := make([]PagedHistoryRecord, 0, len(pending)+len(confirmed))

	add := func(r PagedHistoryRecord) {
		if r.TxHash == "" {
			r.TxHash = r.Tx.HashHex()
		}
		key := fmt.Sprintf("%d:%s", r.BlockIndex, r.TxHash)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, r)
		}
	}
	for _, p := range pending {
		add(p)
	}
	for _, c := range confirmed {
		add(c)
	}
	return merged
}

// SyncAddress incrementally synchronizes address history into bbolt.
// On repeat visits at the same tip it checks only live mempool txs and serves confirmed rows from disk.
func (s *HistoryStore) SyncAddress(
	ctx context.Context,
	client *http.Client,
	rpcBaseURL string,
	netID string,
	address string,
	tipHeight int,
	tipHash string,
	forceFull bool,
) ([]PagedHistoryRecord, HistorySyncMeta, error) {
	if strings.TrimSpace(address) == "" {
		return []PagedHistoryRecord{}, HistorySyncMeta{SyncedHeight: -1}, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if forceFull {
		_ = s.ResetAddress(netID, address)
	}

	cached, meta, err := s.LoadConfirmed(netID, address)
	if err != nil {
		meta = HistorySyncMeta{SyncedHeight: -1}
		cached = nil
	}

	reorgReset := false
	if !forceFull && meta.SyncedHeight >= 0 {
		if tipHeight >= 0 && meta.SyncedHeight > tipHeight {
			_ = s.ResetAddress(netID, address)
			meta = HistorySyncMeta{SyncedHeight: -1}
			cached = nil
			reorgReset = true
		} else if meta.SyncedHeight == tipHeight && tipHash != "" {
			if strings.EqualFold(strings.TrimSpace(meta.SyncedTipHash), strings.TrimSpace(tipHash)) {
				// Exact tip match: only check live pending mempool without re-scanning any confirmed blocks.
				memRecords, _, _, memErr := FetchHistoryRange(ctx, client, rpcBaseURL, address, netID, tipHeight+1, 200, nil)
				if memErr != nil {
					meta.CacheHit = true
					return cached, meta, memErr
				}
				var pending []PagedHistoryRecord
				for _, r := range memRecords {
					if r.BlockIndex < 0 {
						pending = append(pending, r)
					}
				}
				meta.CacheHit = true
				meta.DeltaBlocks = 0
				return mergePendingAndConfirmed(pending, cached), meta, nil
			}
			// Same-height hash change indicates a 1-block reorg.
			_ = s.ResetAddress(netID, address)
			meta = HistorySyncMeta{SyncedHeight: -1}
			cached = nil
			reorgReset = true
		} else if meta.SyncedTipHash != "" {
			okMatch, reachable := verifyBlockHashMatches(ctx, client, rpcBaseURL, netID, meta.SyncedHeight, meta.SyncedTipHash)
			if reachable && !okMatch {
				_ = s.ResetAddress(netID, address)
				meta = HistorySyncMeta{SyncedHeight: -1}
				cached = nil
				reorgReset = true
			}
		}
	}

	minHeight := 0
	if meta.SyncedHeight >= 0 {
		minHeight = meta.SyncedHeight + 1
	}

	deltaRecords, fetchedHeight, fetchedTipHash, fetchErr := FetchHistoryRange(ctx, client, rpcBaseURL, address, netID, minHeight, 2000, nil)
	if fetchErr != nil {
		if len(cached) > 0 && !forceFull {
			meta.CacheHit = true
			return cached, meta, fetchErr
		}
		return nil, meta, fetchErr
	}

	var pending []PagedHistoryRecord
	var confirmedDelta []PagedHistoryRecord
	for _, r := range deltaRecords {
		if r.BlockIndex < 0 {
			pending = append(pending, r)
		} else {
			confirmedDelta = append(confirmedDelta, r)
		}
	}

	newSyncedHeight := fetchedHeight
	if newSyncedHeight == 0 && tipHeight > 0 && fetchedTipHash == "" {
		newSyncedHeight = tipHeight
	}
	newTipHash := strings.TrimSpace(fetchedTipHash)
	if newTipHash == "" && newSyncedHeight == meta.SyncedHeight {
		newTipHash = meta.SyncedTipHash
	}

	deltaBlocks := 0
	if newSyncedHeight >= minHeight {
		deltaBlocks = newSyncedHeight - minHeight + 1
	}

	if err := s.SaveDelta(netID, address, confirmedDelta, newSyncedHeight, newTipHash); err != nil {
		// Still return freshly fetched merged records even if disk write fails.
		mergedFallback := mergePendingAndConfirmed(pending, append(confirmedDelta, cached...))
		return mergedFallback, HistorySyncMeta{
			SyncedHeight:  newSyncedHeight,
			SyncedTipHash: newTipHash,
			UpdatedAt:     time.Now().Unix(),
			DeltaBlocks:   deltaBlocks,
			ReorgReset:    reorgReset,
		}, nil
	}

	reloaded, savedMeta, loadErr := s.LoadConfirmed(netID, address)
	if loadErr != nil {
		reloaded = mergePendingAndConfirmed(confirmedDelta, cached)
		savedMeta = HistorySyncMeta{
			SyncedHeight:  newSyncedHeight,
			SyncedTipHash: newTipHash,
			UpdatedAt:     time.Now().Unix(),
		}
	}
	savedMeta.CacheHit = meta.SyncedHeight >= 0 && !reorgReset && !forceFull && deltaBlocks <= 0
	savedMeta.DeltaBlocks = deltaBlocks
	savedMeta.ReorgReset = reorgReset

	return mergePendingAndConfirmed(pending, reloaded), savedMeta, nil
}
