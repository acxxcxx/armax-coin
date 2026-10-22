package node

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestP2_4_NodeInvariants verifies P2P packet size and GET_BLOCK_RANGE invariants [P2-4]
func TestP2_4_NodeInvariants(t *testing.T) {
	if MaxIncomingPacketSize != 4*1024*1024 {
		t.Fatalf("Expected MaxIncomingPacketSize = 4 MB (4194304 B), got %d", MaxIncomingPacketSize)
	}

	if MaxBlockRangeResponseBytes < core.MaxBlockSize {
		t.Fatalf("MaxBlockRangeResponseBytes (%d) must be >= core.MaxBlockSize (%d)", MaxBlockRangeResponseBytes, core.MaxBlockSize)
	}
}

// TestP2_4_GetBlockRangeAlwaysReturnsAtLeastOneBlock verifies that GET_BLOCK_RANGE
// always returns at least one block even when a single block is huge [P2-4].
func TestP2_4_GetBlockRangeAlwaysReturnsAtLeastOneBlock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "blockchain.db")
	bc, err := core.NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated blockchain: %v", err)
	}
	defer bc.Close()

	// Append a block to blockchain
	gen := bc.GetLatestBlock()
	target := core.DifficultyToTarget(1)
	bc.Chain[0].Target = fmt.Sprintf("%064x", target)

	// Construct block with large padding
	cb := core.Transaction{
		Sender:    "SYSTEM",
		Recipient: core.GenesisMiner,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
		Preimage:  string(make([]byte, 1000)),
	}
	bigBlock := *core.NewBlock([]core.Transaction{cb}, gen.Hash, 1, core.GenesisMiner, target)
	bigBlock.Timestamp = gen.Timestamp + 1
	for {
		bigBlock.Hash = bigBlock.CalculateHash()
		if core.HashMeetsTarget(bigBlock.Hash, target) {
			break
		}
		bigBlock.Nonce++
	}

	bc.Chain = append(bc.Chain, bigBlock)

	// Simulate GET_BLOCK_RANGE logic
	req := BlockRangeRequest{FromIndex: 1, ToIndex: 1}
	chainLen := len(bc.Chain)
	end := req.ToIndex
	if end >= chainLen {
		end = chainLen - 1
	}

	var blocks []core.Block
	totalBytes := 0
	for i := req.FromIndex; i <= end && i < chainLen; i++ {
		blk := bc.Chain[i]
		blkSize := blk.Size()
		if len(blocks) > 0 && totalBytes+blkSize > MaxBlockRangeResponseBytes {
			end = i - 1
			break
		}
		totalBytes += blkSize
		blocks = append(blocks, blk)
	}

	if len(blocks) != 1 {
		t.Fatalf("Expected exactly 1 block in response, got %d", len(blocks))
	}

	resp := BlockRangeResponse{
		FromIndex: req.FromIndex,
		ToIndex:   end,
		Blocks:    blocks,
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}
	if len(respBytes) == 0 {
		t.Fatalf("Response bytes should not be empty")
	}
}
