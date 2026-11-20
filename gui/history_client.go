package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// PagedHistoryRecord matches the JSON payload returned by node /history and /api/history.
type PagedHistoryRecord struct {
	BlockIndex int              `json:"block_index"`
	Timestamp  int64            `json:"timestamp"`
	Status     string           `json:"status"`
	Tx         core.Transaction `json:"tx"`
	TxHash     string           `json:"tx_hash,omitempty"`
}

// PagedHistoryResponse represents the envelope returned by GET /history?format=paged.
type PagedHistoryResponse struct {
	Records          []PagedHistoryRecord `json:"records"`
	ChainHeight      int                  `json:"chain_height"`
	TipHash          string               `json:"tip_hash"`
	ScannedFrom      int                  `json:"scanned_from"`
	ScannedTo        int                  `json:"scanned_to"`
	NextCursorHeight int                  `json:"next_cursor_height"`
	HasMore          bool                 `json:"has_more"`
}

// HistoryFetchProgress reports incremental full-chain scan progress to the UI.
type HistoryFetchProgress struct {
	LoadedCount   int
	ScannedBlocks int
	TotalBlocks   int
	Done          bool
}

// ClassifyTxType consistently classifies a transaction for display across Windows, Unix, and CSV exports.
// Recognizes both legacy "COINBASE" and consensus "SYSTEM" coinbase sender markers.
func ClassifyTxType(tx core.Transaction, activeAddr string) string {
	if tx.Sender == "SYSTEM" || tx.Sender == "COINBASE" {
		return "Mined (Coinbase)"
	}
	if activeAddr != "" && core.MatchAddress(tx.Sender, activeAddr) {
		return "Sent"
	}
	return "Received"
}

// FormatTxHeightStatus renders height and confirmation maturity consistently using dynamic network maturity.
func FormatTxHeightStatus(blockIndex int, txType string, tipHeight int, maturity int) string {
	if blockIndex < 0 {
		return "Pending"
	}
	if maturity <= 0 {
		maturity = 100
	}
	confs := 0
	if tipHeight >= blockIndex {
		confs = tipHeight - blockIndex + 1
	}
	if txType == "Mined (Coinbase)" {
		if confs < maturity {
			return fmt.Sprintf("#%d (%d/%d blks)", blockIndex, confs, maturity)
		}
		return fmt.Sprintf("#%d (Mature)", blockIndex)
	}
	if confs < 6 {
		return fmt.Sprintf("#%d (%d/6 confs)", blockIndex, confs)
	}
	if confs < 12 {
		return fmt.Sprintf("#%d (%d confs)", blockIndex, confs)
	}
	return fmt.Sprintf("#%d (Finalized)", blockIndex)
}

// FetchAllHistory retrieves 100% of historical transactions for address from tip down to genesis
// using paginated GET /history?format=paged calls, invoking onProgress after each batch.
// Falls back seamlessly to legacy array responses if the connected daemon does not support format=paged.
func FetchAllHistory(
	ctx context.Context,
	client *http.Client,
	rpcBaseURL string,
	address string,
	expectedNetID string,
	batchBlocks int,
	onProgress func(records []PagedHistoryRecord, prog HistoryFetchProgress),
) ([]PagedHistoryRecord, error) {
	records, _, _, err := FetchHistoryRange(ctx, client, rpcBaseURL, address, expectedNetID, 0, batchBlocks, onProgress)
	return records, err
}

// FetchHistoryRange retrieves transactions for address from the chain tip down to minHeight (inclusive)
// using paginated GET /history?format=paged calls, returning records, tip height, tip hash, and error.
// Passing minHeight > 0 performs fast incremental delta sync while still fetching current mempool txs.
func FetchHistoryRange(
	ctx context.Context,
	client *http.Client,
	rpcBaseURL string,
	address string,
	expectedNetID string,
	minHeight int,
	batchBlocks int,
	onProgress func(records []PagedHistoryRecord, prog HistoryFetchProgress),
) ([]PagedHistoryRecord, int, string, error) {
	if strings.TrimSpace(address) == "" {
		return []PagedHistoryRecord{}, 0, "", nil
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	if batchBlocks <= 0 {
		batchBlocks = 2000
	}
	if batchBlocks > 5000 {
		batchBlocks = 5000
	}
	if minHeight < 0 {
		minHeight = 0
	}

	baseURL := strings.TrimRight(rpcBaseURL, "/")
	seen := make(map[string]bool)
	all := make([]PagedHistoryRecord, 0, 64)

	cursorParam := ""
	minHeightParam := ""
	if minHeight > 0 {
		minHeightParam = fmt.Sprintf("&min_height=%d", minHeight)
	}
	totalChainBlocks := 0
	lowestScanned := -1
	chainHeight := 0
	tipHash := ""
	firstPage := true

	for {
		if ctx.Err() != nil {
			return all, chainHeight, tipHash, ctx.Err()
		}

		reqURL := fmt.Sprintf("%s/history?address=%s&format=paged&batch_blocks=%d&limit=500%s%s",
			baseURL, url.QueryEscape(address), batchBlocks, minHeightParam, cursorParam)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return all, chainHeight, tipHash, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return all, chainHeight, tipHash, err
		}

		if expectedNetID != "" {
			if netHdr := strings.TrimSpace(resp.Header.Get("X-Network-ID")); netHdr != "" && !strings.EqualFold(netHdr, expectedNetID) {
				_ = resp.Body.Close()
				return all, chainHeight, tipHash, fmt.Errorf("network mismatch on /history: expected %s, got %s", expectedNetID, netHdr)
			}
		}

		if resp.StatusCode != http.StatusOK {
			bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			return all, chainHeight, tipHash, fmt.Errorf("/history returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
		}

		raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if err != nil {
			return all, chainHeight, tipHash, err
		}

		trimmed := strings.TrimSpace(string(raw))
		if strings.HasPrefix(trimmed, "[") {
			// Legacy daemon array fallback
			var legacy []PagedHistoryRecord
			if err := json.Unmarshal(raw, &legacy); err != nil {
				return all, chainHeight, tipHash, err
			}
			for _, rec := range legacy {
				if rec.TxHash == "" {
					rec.TxHash = rec.Tx.HashHex()
				}
				key := fmt.Sprintf("%d:%s", rec.BlockIndex, rec.TxHash)
				if !seen[key] {
					seen[key] = true
					all = append(all, rec)
				}
			}
			if onProgress != nil {
				snapshot := make([]PagedHistoryRecord, len(all))
				copy(snapshot, all)
				onProgress(snapshot, HistoryFetchProgress{
					LoadedCount:   len(all),
					ScannedBlocks: len(all),
					TotalBlocks:   len(all),
					Done:          true,
				})
			}
			return all, chainHeight, tipHash, nil
		}

		var env PagedHistoryResponse
		if err := json.Unmarshal(raw, &env); err != nil {
			return all, chainHeight, tipHash, err
		}
		if firstPage {
			chainHeight = env.ChainHeight
			tipHash = env.TipHash
			firstPage = false
		}
		if env.ChainHeight+1 > totalChainBlocks {
			totalChainBlocks = env.ChainHeight + 1
		}
		if lowestScanned == -1 || env.ScannedTo < lowestScanned {
			lowestScanned = env.ScannedTo
		}

		for _, rec := range env.Records {
			if rec.TxHash == "" {
				rec.TxHash = rec.Tx.HashHex()
			}
			key := fmt.Sprintf("%d:%s", rec.BlockIndex, rec.TxHash)
			if !seen[key] {
				seen[key] = true
				all = append(all, rec)
			}
		}

		scanned := 0
		if totalChainBlocks > 0 && lowestScanned >= 0 {
			scanned = totalChainBlocks - lowestScanned
			if scanned < 0 {
				scanned = 0
			}
			if scanned > totalChainBlocks {
				scanned = totalChainBlocks
			}
		}

		done := !env.HasMore || env.NextCursorHeight < minHeight || env.NextCursorHeight < 0
		if onProgress != nil {
			snapshot := make([]PagedHistoryRecord, len(all))
			copy(snapshot, all)
			onProgress(snapshot, HistoryFetchProgress{
				LoadedCount:   len(all),
				ScannedBlocks: scanned,
				TotalBlocks:   totalChainBlocks,
				Done:          done,
			})
		}

		if done {
			break
		}
		cursorParam = fmt.Sprintf("&cursor_height=%d", env.NextCursorHeight)
	}

	return all, chainHeight, tipHash, nil
}
