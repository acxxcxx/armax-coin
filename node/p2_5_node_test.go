//go:build testhooks

package node

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// fundAccount funds an account in the blockchain state
func fundAccount(bc *core.Blockchain, addr string, balance int64) {
	addrHex, _ := core.AddressToHex(addr)
	bc.State.Lock.Lock()
	bc.State.Accounts[addrHex] = core.AccountState{
		Balance: balance,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()
}

// createLargeBlock creates a block with specified number of ML-DSA-65 transactions
func createLargeBlock(t *testing.T, bc *core.Blockchain, senderAcc *testAccount, recipAcc *testAccount, numTxs int) core.Block {
	t.Helper()
	minerAcc := newTestAccount(t, "large_miner")

	prev := bc.GetLatestBlock()
	target := core.DifficultyToTarget(1)

	txs := make([]core.Transaction, 0, numTxs+1)
	coinbase := core.Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAcc.Address,
		Amount:    core.InitialReward,
		Fee:       0,
		Nonce:     int64(prev.Index + 1),
		Signature: "REWARD_SIG",
	}
	txs = append(txs, coinbase)

	var totalFees int64
	for i := 1; i <= numTxs; i++ {
		tx := core.Transaction{
			Sender:    senderAcc.Address,
			SenderPub: senderAcc.PubHex,
			Recipient: recipAcc.Address,
			Amount:    1 * core.CoinUnit,
			Fee:       core.MinRelayFee * 10,
			Nonce:     int64(i),
			Type:      core.TxTypeStandard,
		}
		if err := tx.Sign(senderAcc.PrivKey); err != nil {
			t.Fatalf("Sign tx %d failed: %v", i, err)
		}
		txs = append(txs, tx)
		totalFees += tx.Fee
	}
	txs[0].Amount += totalFees

	block := *core.NewBlock(txs, prev.Hash, prev.Index+1, minerAcc.Address, target)
	block.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		block.Hash = block.CalculateHash()
		if core.HashMeetsTarget(block.Hash, target) {
			break
		}
		block.Nonce++
	}

	return block
}

// TestP2_5_LargeBlockOverNewBlockAndGetBlockRange tests that a large block (~550 KB with ML-DSA-65 txs)
// can be transferred via NEW_BLOCK and retrieved via GET_BLOCK_RANGE without hitting rate limits.
func TestP2_5_LargeBlockOverNewBlockAndGetBlockRange(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	defer ps.Stop()
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)
	targetAddr := "127.0.0.1:" + port

	senderAcc := newTestAccount(t, "user_sender")
	recipAcc := newTestAccount(t, "user_recip")
	fundAccount(bc, senderAcc.Address, 10_000_000*core.CoinUnit)

	// Create a large block with 100 PQC transactions
	largeBlock := createLargeBlock(t, bc, senderAcc, recipAcc, 100)
	blockSize := largeBlock.Size()
	t.Logf("[P2-5] Generated large block size: %d bytes (~%.2f KB)", blockSize, float64(blockSize)/1024.0)

	// 1. Send large block via NEW_BLOCK
	msgNewBlock := Message{
		NetworkID: NetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   largeBlock,
	}

	// Dial and write message
	conn, _, err := DialEncrypted(targetAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("DialEncrypted failed: %v", err)
	}
	defer conn.Close()

	data, err := json.Marshal(msgNewBlock)
	if err != nil {
		t.Fatalf("Marshal NEW_BLOCK failed: %v", err)
	}
	data = append(data, '\n')

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Failed to write NEW_BLOCK: %v", err)
	}

	// Wait for node to process block
	deadline := time.Now().Add(5 * time.Second)
	var latest core.Block
	for time.Now().Before(deadline) {
		bc.Lock.RLock()
		latest = bc.GetLatestBlock()
		bc.Lock.RUnlock()
		if latest.Hash == largeBlock.Hash {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if latest.Hash != largeBlock.Hash {
		t.Fatalf("Large block was not added via NEW_BLOCK: expected %s, got %s", largeBlock.Hash, latest.Hash)
	}

	// 2. Retrieve the large block via GET_BLOCK_RANGE
	msgRange := Message{
		NetworkID: NetworkID(),
		Type:      "GET_BLOCK_RANGE",
		Payload:   BlockRangeRequest{FromIndex: 1, ToIndex: 1},
	}
	resp, err := QueryP2P(targetAddr, msgRange, 5*time.Second)
	if err != nil {
		t.Fatalf("GET_BLOCK_RANGE for large block failed: %v", err)
	}
	if resp.Type != "BLOCK_RANGE_RESPONSE" {
		t.Fatalf("Expected BLOCK_RANGE_RESPONSE, got: %s", resp.Type)
	}

	var rangeResp BlockRangeResponse
	respBytes, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(respBytes, &rangeResp); err != nil {
		t.Fatalf("Unmarshal BlockRangeResponse failed: %v", err)
	}

	if len(rangeResp.Blocks) != 1 {
		t.Fatalf("Expected 1 block in range response, got %d", len(rangeResp.Blocks))
	}
	if rangeResp.Blocks[0].Hash != largeBlock.Hash {
		t.Fatalf("Retrieved block hash mismatch: expected %s, got %s", largeBlock.Hash, rangeResp.Blocks[0].Hash)
	}
}

// TestP2_5_ForkResolutionWithLargeBlocks verifies that fork resolution works smoothly
// when competing chain contains large ML-DSA-65 blocks.
func TestP2_5_ForkResolutionWithLargeBlocks(t *testing.T) {
	core.SetNetworkForTesting(true)
	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	dirA := filepath.Join(t.TempDir(), "nodeA")
	dirB := filepath.Join(t.TempDir(), "nodeB")

	t.Setenv("ARMX_DATA_DIR", dirA)
	bcA := core.NewBlockchain(1)
	bcA.CoinbaseMaturity = 1
	defer bcA.Close()

	t.Setenv("ARMX_DATA_DIR", dirB)
	bcB := core.NewBlockchain(1)
	bcB.CoinbaseMaturity = 1
	defer bcB.Close()

	senderAcc := newTestAccount(t, "fork_sender")
	recipAcc := newTestAccount(t, "fork_recip")

	target := bcA.CalculateLWMA1()

	// Both share common block 1 where senderAcc receives the block reward on-chain
	prev0 := bcA.Chain[0]
	commonBlock := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: senderAcc.Address,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, prev0.Hash, 1, senderAcc.Address, target)
	commonBlock.Timestamp = prev0.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		commonBlock.Hash = commonBlock.CalculateHash()
		if core.HashMeetsTarget(commonBlock.Hash, target) {
			break
		}
		commonBlock.Nonce++
	}

	if !bcA.ValidateAndAddBlock(commonBlock) || !bcB.ValidateAndAddBlock(commonBlock) {
		t.Fatalf("Failed to add common block")
	}

	// Node A mines short branch (block 2A, coinbase only)
	minerA := newTestAccount(t, "minerA")
	b2A := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: minerA.Address,
		Amount:    core.InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, commonBlock.Hash, 2, minerA.Address, target)
	b2A.Timestamp = commonBlock.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b2A.Hash = b2A.CalculateHash()
		if core.HashMeetsTarget(b2A.Hash, target) {
			break
		}
		b2A.Nonce++
	}
	if !bcA.ValidateAndAddBlock(b2A) {
		t.Fatalf("Failed to add block 2A to Node A")
	}

	// Node B mines heavier branch with large blocks (block 2B with PQC txs from senderAcc to recipAcc, and block 3B)
	b2B := createLargeBlock(t, bcB, senderAcc, recipAcc, 5)
	if !bcB.ValidateAndAddBlock(b2B) {
		t.Fatalf("Failed to add block 2B to Node B")
	}

	b3B := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: minerA.Address,
		Amount:    core.InitialReward,
		Nonce:     3,
		Signature: "REWARD_SIG",
	}}, b2B.Hash, 3, minerA.Address, target)
	b3B.Timestamp = b2B.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b3B.Hash = b3B.CalculateHash()
		if core.HashMeetsTarget(b3B.Hash, target) {
			break
		}
		b3B.Nonce++
	}
	if !bcB.ValidateAndAddBlock(b3B) {
		t.Fatalf("Failed to add block 3B to Node B")
	}

	err := bcA.ResolveFork(1, bcB.Chain[2:])
	if err != nil {
		t.Fatalf("Fork resolution with large blocks failed: %v", err)
	}

	bcB.Lock.RLock()
	tipB := bcB.GetLatestBlock().Hash
	lenB := len(bcB.Chain)
	bcB.Lock.RUnlock()

	bcA.Lock.RLock()
	lenA := len(bcA.Chain)
	tipA := bcA.GetLatestBlock().Hash
	bcA.Lock.RUnlock()

	if lenA != lenB || tipA != tipB {
		t.Fatalf("Fork resolution mismatch: Node A height %d tip %s, Node B height %d tip %s",
			lenA, tipA, lenB, tipB)
	}
}
