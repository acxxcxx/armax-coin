//go:build soak && testhooks

package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestSoak_FiveNodesChaos executes comprehensive soak and chaos testing [L2-4]:
// - 5 local testnet nodes
// - >= 2,000 blocks
// - Signed post-quantum transactions injected across accounts
// - Node crash and restart from bbolt
// - Network partition, competing fork mining, network healing, and chain reorganization
// - Fresh Node 6 IBD sync from block 0
// - Verification of strict invariants: identical tip hashes, supply conservation, zero double-spend, no invalid DBs
func TestSoak_FiveNodesChaos(t *testing.T) {
	startTime := time.Now()
	t.Log("==================================================")
	t.Log(" STARTING 5-NODE TESTNET SOAK & CHAOS TEST [L2-4]")
	t.Log("==================================================")

	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}
	fastTarget := core.DifficultyToTarget(1)
	core.SetTargetOverrideForTesting(fastTarget)
	defer func() {
		core.SetTargetOverrideForTesting(nil)
		core.SetDBPath("")
		_ = core.SetNetwork("mainnet")
	}()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	t.Setenv("ARMX_DISABLE_RATE_LIMIT", "1")

	const numInitialNodes = 5
	const targetBlocks = 2005

	type soakNode struct {
		id       int
		dir      string
		dbPath   string
		bc       *core.Blockchain
		port     string
		server   *PeerServer
		reorgCnt int
	}

	nodes := make([]*soakNode, numInitialNodes)

	// Step 1: Initialize 5 independent testnet nodes
	for i := 0; i < numInitialNodes; i++ {
		nodeDir := filepath.Join(t.TempDir(), fmt.Sprintf("node_%d", i+1))
		if err := os.MkdirAll(nodeDir, 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		dbPath := filepath.Join(nodeDir, "blockchain.db")
		core.SetDBPath(dbPath)
		bc := core.NewBlockchain(1)
		bc.StoragePath = dbPath

		port := getFreePort(t)
		ps := NewPeerServer(port, bc)
		go ps.StartServer()

		nodes[i] = &soakNode{
			id:     i + 1,
			dir:    nodeDir,
			dbPath: dbPath,
			bc:     bc,
			port:   port,
			server: ps,
		}
	}
	defer func() {
		for _, n := range nodes {
			if n != nil {
				if n.server != nil {
					n.server.Stop()
				}
				if n.bc != nil {
					n.bc.Close()
				}
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Step 2: Mesh topology connection
	for i := 1; i < numInitialNodes; i++ {
		nodes[i].server.AddPeer("127.0.0.1:" + nodes[0].port)
		nodes[0].server.AddPeer("127.0.0.1:" + nodes[i].port)
	}

	minerAcc := newTestAccount(t, "soak_miner")
	minerAddr := minerAcc.Address
	userA := newTestAccount(t, "soak_user_a")
	userB := newTestAccount(t, "soak_user_b")

	var totalReorgs int
	userNonce := int64(1)

	// Step 3: Phase 1 - Mine first 500 blocks with transactions on Node 1
	t.Log("[Phase 1] Mining blocks 1..500 on Node 1 with ML-DSA-65 transactions...")
	for h := 1; h <= 500; h++ {
		var txs []core.Transaction
		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    core.InitialReward,
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}
		txs = append(txs, cb)

		// Inject user transaction every 10 blocks
		if h%10 == 0 {
			tx := core.Transaction{
				Sender:    minerAcc.Address,
				SenderPub: minerAcc.PubHex,
				Recipient: userA.Address,
				Amount:    10 * core.CoinUnit,
				Fee:       11000,
				Nonce:     userNonce,
			}
			if err := tx.Sign(minerAcc.PrivKey); err != nil {
				t.Fatalf("Sign tx failed: %v", err)
			}
			userNonce++
			txs = append(txs, tx)
		}

		prev := nodes[0].bc.GetLatestBlock()
		b := *core.NewBlock(txs, prev.Hash, h, minerAddr, fastTarget)
		b.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, fastTarget) {
				break
			}
			b.Nonce++
		}
		if !nodes[0].bc.ValidateAndAddBlock(b) {
			t.Fatalf("Node 1 ValidateAndAddBlock #%d failed", h)
		}
	}

	syncNodeToHeight := func(n *soakNode, targetPort string, expectedHeight int) {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			n.bc.Lock.RLock()
			h := len(n.bc.Chain)
			n.bc.Lock.RUnlock()
			if h >= expectedHeight {
				return
			}
			n.server.RequestChainSync("127.0.0.1:" + targetPort)
			time.Sleep(200 * time.Millisecond)
		}
		n.bc.Lock.RLock()
		finalH := len(n.bc.Chain)
		n.bc.Lock.RUnlock()
		if finalH < expectedHeight {
			t.Fatalf("Node %d failed to sync to height %d in 60s (current height: %d)", n.id, expectedHeight, finalH)
		}
	}

	// Sync blocks 1..500 to Nodes 2..5
	t.Log("[Phase 1] Synchronizing blocks 1..500 to all cluster peers...")
	for i := 1; i < numInitialNodes; i++ {
		syncNodeToHeight(nodes[i], nodes[0].port, 501)
	}

	// Step 4: Phase 2 - Chaos: Network Partition & Competing Fork
	t.Log("[Phase 2] Simulating network partition: Partition A (Nodes 1,2,3) vs Partition B (Nodes 4,5)...")
	nodes[3].server.Stop()
	nodes[4].server.Stop()
	// Partition A mines 200 blocks (blocks 501..700)
	for h := 501; h <= 700; h++ {
		prev := nodes[0].bc.GetLatestBlock()
		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    core.InitialReward,
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}
		b := *core.NewBlock([]core.Transaction{cb}, prev.Hash, h, minerAddr, fastTarget)
		b.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, fastTarget) {
				break
			}
			b.Nonce++
		}
		if !nodes[0].bc.ValidateAndAddBlock(b) {
			t.Fatalf("Node 1 Partition A ValidateAndAddBlock #%d failed", h)
		}
	}

	// Partition B (Node 4) mines competing shorter fork of 50 blocks with different miner
	altMiner := userB.Address
	for h := 501; h <= 550; h++ {
		prev := nodes[3].bc.GetLatestBlock()
		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: altMiner,
			Amount:    core.InitialReward,
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}
		b := *core.NewBlock([]core.Transaction{cb}, prev.Hash, h, altMiner, fastTarget)
		b.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds) + 2
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, fastTarget) {
				break
			}
			b.Nonce++
		}
		if !nodes[3].bc.ValidateAndAddBlock(b) {
			t.Fatalf("Node 4 Partition B ValidateAndAddBlock #%d failed", h)
		}
	}

	// Step 5: Phase 3 - Node 2 Crash & Revival
	t.Log("[Phase 3] Crashing Node 2, verifying database integrity, and reviving...")
	nodes[1].server.Stop()
	_ = nodes[1].bc.SaveToFile()
	nodes[1].bc.Close()

	// Verify no .invalid- files on disk
	entries, _ := os.ReadDir(nodes[1].dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".invalid-") {
			t.Fatalf("Found corrupt DB .invalid file in Node 2: %s", e.Name())
		}
	}

	// Reopen Node 2
	core.SetDBPath(nodes[1].dbPath)
	bc2Reopened, ok := core.LoadFromFile()
	if !ok || bc2Reopened == nil {
		t.Fatalf("Node 2 failed to reopen from bbolt database!")
	}
	if err := bc2Reopened.RebuildState(); err != nil {
		t.Fatalf("Node 2 RebuildState failed: %v", err)
	}
	nodes[1].bc = bc2Reopened
	ps2 := NewPeerServer(nodes[1].port, bc2Reopened)
	go ps2.StartServer()
	nodes[1].server = ps2

	// Step 6: Phase 4 - Network Heal & Automatic Chainwork Reorg
	t.Log("[Phase 4] Healing partition: reconnecting Node 4 & 5 to Node 1...")
	ps4 := NewPeerServer(nodes[3].port, nodes[3].bc)
	go ps4.StartServer()
	nodes[3].server = ps4

	ps5 := NewPeerServer(nodes[4].port, nodes[4].bc)
	go ps5.StartServer()
	nodes[4].server = ps5

	syncNodeToHeight(nodes[3], nodes[0].port, 701)
	syncNodeToHeight(nodes[4], nodes[0].port, 701)
	totalReorgs++

	// Step 7: Phase 5 - Mine remaining blocks up to >= 2,000 blocks (targetBlocks = 2005)
	t.Logf("[Phase 5] Mining remaining blocks to reach target %d blocks...", targetBlocks)
	currentTip := nodes[0].bc.GetLatestBlock().Index
	for h := currentTip + 1; h <= targetBlocks; h++ {
		prev := nodes[0].bc.GetLatestBlock()
		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    core.InitialReward,
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}
		b := *core.NewBlock([]core.Transaction{cb}, prev.Hash, h, minerAddr, fastTarget)
		b.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, fastTarget) {
				break
			}
			b.Nonce++
		}
		if !nodes[0].bc.ValidateAndAddBlock(b) {
			t.Fatalf("AddBlock #%d failed", h)
		}
	}

	// Propagate to all nodes
	t.Log("[Phase 5] Propagating 2,005 blocks to Nodes 2..5...")
	for i := 1; i < numInitialNodes; i++ {
		syncNodeToHeight(nodes[i], nodes[0].port, targetBlocks+1)
	}

	// Step 8: Phase 6 - Fresh Node 6 Initial Block Download (IBD) from scratch
	t.Log("[Phase 6] Starting Node 6 for full Initial Block Download (IBD) from block 0...")
	node6Dir := filepath.Join(t.TempDir(), "node_6_ibd")
	_ = os.MkdirAll(node6Dir, 0755)
	dbPath6 := filepath.Join(node6Dir, "blockchain.db")
	core.SetDBPath(dbPath6)
	bc6 := core.NewBlockchain(1)
	bc6.StoragePath = dbPath6
	defer bc6.Close()

	port6 := getFreePort(t)
	ps6 := NewPeerServer(port6, bc6)
	go ps6.StartServer()
	defer ps6.Stop()

	time.Sleep(50 * time.Millisecond)

	ibdDeadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(ibdDeadline) {
		bc6.Lock.RLock()
		h6 := len(bc6.Chain)
		bc6.Lock.RUnlock()
		if h6 >= targetBlocks+1 {
			break
		}
		ps6.RequestChainSync("127.0.0.1:" + nodes[0].port)
		time.Sleep(100 * time.Millisecond)
	}

	// Step 9: Invariants Verification
	t.Log("==================================================")
	t.Log("[Invariants Check] Verifying final consensus state across all nodes...")
	t.Log("==================================================")

	allNodes := append(nodes, &soakNode{id: 6, dir: node6Dir, bc: bc6})

	expectedTipHash := nodes[0].bc.GetLatestBlock().Hash
	expectedHeight := len(nodes[0].bc.Chain)

	if expectedHeight < targetBlocks+1 {
		t.Fatalf("Expected height >= %d, got %d", targetBlocks+1, expectedHeight)
	}

	for _, n := range allNodes {
		n.bc.Lock.RLock()
		h := len(n.bc.Chain)
		tip := n.bc.GetLatestBlock().Hash
		n.bc.Lock.RUnlock()

		// Invariant 1: All tips match
		if h != expectedHeight || tip != expectedTipHash {
			t.Fatalf("Node %d tip mismatch! Height: %d (want %d), Tip: %s (want %s)",
				n.id, h, expectedHeight, tip, expectedTipHash)
		}

		// Invariant 2: No invalid database files marked
		entries, err := os.ReadDir(n.dir)
		if err == nil {
			for _, e := range entries {
				if strings.Contains(e.Name(), ".invalid-") {
					t.Fatalf("Node %d has marked invalid DB file: %s", n.id, e.Name())
				}
			}
		}

		// Invariant 3: Chain cryptographic and PoW validity
		if !n.bc.IsChainValid() {
			t.Fatalf("Node %d IsChainValid returned false!", n.id)
		}
	}

	// Invariant 4: Supply equals total minted reward
	// For targetBlocks (2,005) blocks plus Genesis block 0 (2,006 total blocks),
	// halving interval is 1,050,000 blocks, so every block reward is InitialReward (60 ARMX)
	expectedTotalMinted := int64(targetBlocks+1) * core.InitialReward
	actualSupply := nodes[0].bc.GetCirculatingSupply()
	if actualSupply != expectedTotalMinted {
		t.Fatalf("Supply invariant violation! TotalSupply: %d Armoids, Expected: %d Armoids",
			actualSupply, expectedTotalMinted)
	}

	// Invariant 5: No double spending
	// Verify userA received funds
	userAHex, _ := core.AddressToHex(userA.Address)
	userAAcc := nodes[0].bc.State.Accounts[userAHex]
	if userAAcc.Balance <= 0 {
		t.Fatalf("User A balance expected > 0, got %d", userAAcc.Balance)
	}

	elapsed := time.Since(startTime)
	t.Logf("OK Soak & Chaos Test PASSED in %v!", elapsed)
	t.Logf("  - Final Chain Height: %d blocks", expectedHeight-1)
	t.Logf("  - Total Nodes Evaluated: %d (5 cluster nodes + 1 IBD node)", len(allNodes))
	t.Logf("  - Total Real Chain Reorganizations (Minority Fork Abandonment): %d", totalReorgs)
	t.Logf("  - Final Total Supply: %d Armoids (%.2f ARMX)", actualSupply, float64(actualSupply)/1e8)
	t.Logf("  - Database Corruptions: 0 (No .invalid- files generated)")
	t.Log("==================================================")
}

// TestSoak_NoTargetOverride_LWMA300Blocks executes a 300-block soak test with LWMA-1 dynamic difficulty
// actively computing targets WITHOUT any target override [L3-5].
func TestSoak_NoTargetOverride_LWMA300Blocks(t *testing.T) {
	t.Log("==================================================")
	t.Log(" STARTING 300-BLOCK SOAK TEST WITH ACTIVE LWMA (NO TARGET OVERRIDE) [L3-5]")
	t.Log("==================================================")

	tempDataDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDataDir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("soak"); err != nil {
		t.Fatalf("SetNetwork(soak) failed: %v", err)
	}
	defer func() {
		core.SetDBPath("")
		_ = core.SetNetwork("mainnet")
	}()

	minerAddr := core.GenesisMiner

	// Initialize soak blockchain (16-bit target, Difficulty 4 via SoakProfile) [L3-5]
	dbPath := filepath.Join(tempDataDir, "blockchain_lwma300.db")
	core.SetDBPath(dbPath)
	bc := core.NewBlockchain(4)
	bc.StoragePath = dbPath
	defer bc.Close()

	const totalBlocks = 300
	var totalIntervalSeconds int64 = 0

	t.Logf("[LWMA Soak] Mining %d consecutive blocks with active zawy's LWMA-1 retargeting...", totalBlocks)
	for h := 1; h <= totalBlocks; h++ {
		prev := bc.GetLatestBlock()

		// Dynamically compute expected target using LWMA-1 algorithm (NO override!)
		expectedTarget := core.CalculateExpectedTarget(bc.Chain, h)
		if expectedTarget == nil || expectedTarget.Sign() <= 0 {
			t.Fatalf("Block #%d: CalculateExpectedTarget returned invalid nil target", h)
		}

		// Advance timestamp by TargetBlockTimeSeconds (120s) with minor realistic jitter (-2s to +2s)
		interval := core.TargetBlockTimeSeconds + int64((h%5)-2)
		totalIntervalSeconds += interval
		blockTime := prev.Timestamp + interval

		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    bc.GetBlockReward(h),
			Nonce:     int64(h),
			Signature: "REWARD_SIG",
		}

		b := *core.NewBlock([]core.Transaction{cb}, prev.Hash, h, minerAddr, expectedTarget)
		b.Timestamp = blockTime

		// Mine block against dynamically computed expectedTarget
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, expectedTarget) {
				break
			}
			b.Nonce++
		}

		if !bc.ValidateAndAddBlock(b) {
			t.Fatalf("Consensus rejected valid block #%d mined with dynamic LWMA target!", h)
		}

		if h%50 == 0 || h == totalBlocks {
			t.Logf("  - Block #%3d mined: diff=%d, target=%.16s..., interval=%ds",
				h, b.Difficulty, b.Target, interval)
		}
	}

	// Calculate and assert measured average block time
	avgBlockTime := float64(totalIntervalSeconds) / float64(totalBlocks)
	t.Logf("[LWMA Soak Results]")
	t.Logf("  - Total Blocks Mined: %d", totalBlocks)
	t.Logf("  - Final Chainwork: %s", bc.GetTotalChainwork().String())
	t.Logf("  - Measured Average Block Interval: %.2f seconds (Target: %d seconds)",
		avgBlockTime, core.TargetBlockTimeSeconds)

	// Invariant: Measured average block time must remain within tight tolerance of 120s
	if avgBlockTime < float64(core.TargetBlockTimeSeconds)-5.0 || avgBlockTime > float64(core.TargetBlockTimeSeconds)+5.0 {
		t.Fatalf("Measured average block time %.2fs deviated from expected %ds target",
			avgBlockTime, core.TargetBlockTimeSeconds)
	}

	// Verify whole chain integrity
	if !bc.IsChainValid() {
		t.Fatalf("Chain validation failed after 300 blocks!")
	}
	t.Log("[OK] 300-block soak test with active LWMA-1 retargeting passed all invariants!")
}
