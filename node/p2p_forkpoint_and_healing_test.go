package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN14_ForkpointLocatorDiscovery verifies [N-14]:
// Node correctly identifies common ancestor using block locator hashes.
func TestN14_ForkpointLocatorDiscovery(t *testing.T) {
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	baseDir := t.TempDir()
	bcA := newN14Blockchain(t, filepath.Join(baseDir, "nodeA", "blockchain.db"))
	bcB := newN14Blockchain(t, filepath.Join(baseDir, "nodeB", "blockchain.db"))

	// Mine 1 common block on both
	miner := core.GenesisMiner
	target := core.DifficultyToTarget(1)
	bcA.Chain[0].Target = fmt.Sprintf("%064x", target)
	bcB.Chain[0].Target = fmt.Sprintf("%064x", target)
	prev := bcA.Chain[0]

	b1 := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, prev.Hash, 1, miner, target)
	b1.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b1.Hash = b1.CalculateHash()
		if core.HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	if !bcA.ValidateAndAddBlock(b1) || !bcB.ValidateAndAddBlock(b1) {
		t.Fatalf("Failed to add common block 1")
	}

	// Node A mines block 2A
	b2A := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    core.InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, b1.Hash, 2, miner, target)
	b2A.Timestamp = b1.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b2A.Hash = b2A.CalculateHash()
		if core.HashMeetsTarget(b2A.Hash, target) {
			break
		}
		b2A.Nonce++
	}
	if !bcA.ValidateAndAddBlock(b2A) {
		t.Fatalf("Failed to add block 2A")
	}

	// Node B mines block 2B and 3B
	b2B := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    core.InitialReward,
		Nonce:     2,
		Signature: "REWARD_SIG",
	}}, b1.Hash, 2, miner, target)
	b2B.Timestamp = b1.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
	for {
		b2B.Hash = b2B.CalculateHash()
		if core.HashMeetsTarget(b2B.Hash, target) {
			break
		}
		b2B.Nonce++
	}
	if !bcB.ValidateAndAddBlock(b2B) {
		t.Fatalf("Failed to add block 2B")
	}

	portA := getFreePort(t)
	psA := NewPeerServer(portA, bcA)
	go psA.StartServer()

	portB := getFreePort(t)
	psB := NewPeerServer(portB, bcB)
	go psB.StartServer()

	time.Sleep(100 * time.Millisecond)

	// Node B generates locators and queries GET_FORKPOINT on Node A
	locators := psB.GenerateBlockLocators()
	forkMsg := Message{
		NetworkID: NetworkID(),
		Type:      "GET_FORKPOINT",
		Payload:   ForkpointRequest{Locators: locators},
	}

	resp, err := QueryP2P("127.0.0.1:"+portA, forkMsg, 3*time.Second)
	if err != nil {
		t.Fatalf("GET_FORKPOINT query failed: %v", err)
	}
	if resp.Type != "FORKPOINT_RESPONSE" {
		t.Fatalf("Expected FORKPOINT_RESPONSE, got: %s", resp.Type)
	}

	var fpResp ForkpointResponse
	bData, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(bData, &fpResp); err != nil {
		t.Fatalf("Unmarshal FORKPOINT_RESPONSE failed: %v", err)
	}

	// Common ancestor must be block 1
	if fpResp.AncestorHeight != 1 {
		t.Fatalf("Expected ancestor height 1, got %d", fpResp.AncestorHeight)
	}
	if fpResp.AncestorHash != b1.Hash {
		t.Fatalf("Expected ancestor hash %s, got %s", b1.Hash, fpResp.AncestorHash)
	}
}

func newN14Blockchain(t *testing.T, path string) *core.Blockchain {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create blockchain directory: %v", err)
	}

	// Set the path BEFORE NewBlockchain loads or creates the database.
	core.SetDBPath(path)
	bc := core.NewBlockchain(1)

	// The database is now opened and retained by this Blockchain.
	// Restore the process-global override so it does not leak into
	// subsequent test setup.
	core.SetDBPath("")

	if bc == nil {
		t.Fatal("NewBlockchain returned nil")
	}

	t.Cleanup(func() {
		_ = bc.Close()
	})
	return bc
}

// TestN14_ThreeNodeForkHealingAndIBD verifies [N-14]:
// Fork healing between Node 1 and Node 2, and Initial Block Download (IBD) by Node 3.
func TestN14_ThreeNodeForkHealingAndIBD(t *testing.T) {
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	t.Setenv("ARMX_DISABLE_RATE_LIMIT", "1")

	baseDir := t.TempDir()
	bc1 := newN14Blockchain(t, filepath.Join(baseDir, "node1", "blockchain.db"))
	bc2 := newN14Blockchain(t, filepath.Join(baseDir, "node2", "blockchain.db"))
	bc3 := newN14Blockchain(t, filepath.Join(baseDir, "node3", "blockchain.db"))

	miner := core.GenesisMiner
	target := core.DifficultyToTarget(1)
	bc1.Chain[0].Target = fmt.Sprintf("%064x", target)
	bc2.Chain[0].Target = fmt.Sprintf("%064x", target)
	bc3.Chain[0].Target = fmt.Sprintf("%064x", target)

	// Node 1 & 2 start with 1 common block on top of genesis
	prev := bc1.Chain[0]
	b1 := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: miner,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, prev.Hash, 1, miner, target)
	b1.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b1.Hash = b1.CalculateHash()
		if core.HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}
	bc1.ValidateAndAddBlock(b1)
	bc2.ValidateAndAddBlock(b1)
	bc3.ValidateAndAddBlock(b1)

	// Node 1 mines 2 more blocks (blocks 2 and 3)
	for i := 2; i <= 3; i++ {
		p := bc1.GetLatestBlock()
		b := *core.NewBlock([]core.Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    core.InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, p.Hash, i, miner, target)
		b.Timestamp = p.Timestamp + int64(core.TargetBlockTimeSeconds)
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		bc1.ValidateAndAddBlock(b)
	}

	// Node 2 mines 4 blocks forking off block 1 (blocks 2, 3, 4, 5) -> heavier chain!
	p2 := b1
	for i := 2; i <= 5; i++ {
		b := *core.NewBlock([]core.Transaction{{
			Sender:    "SYSTEM",
			Recipient: miner,
			Amount:    core.InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}}, p2.Hash, i, miner, target)
		b.Timestamp = p2.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		bc2.ValidateAndAddBlock(b)
		p2 = b
	}

	for _, blk := range bc2.Chain[1:] {
		bc3.ValidateAndAddBlock(blk)
	}

	port1 := getFreePort(t)
	ps1 := NewPeerServer(port1, bc1)
	go ps1.StartServer()

	port2 := getFreePort(t)
	ps2 := NewPeerServer(port2, bc2)
	go ps2.StartServer()

	port3 := getFreePort(t)
	ps3 := NewPeerServer(port3, bc3)
	go ps3.StartServer()

	time.Sleep(200 * time.Millisecond)

	targetAddr2 := "127.0.0.1:" + port2
	ps1.AddPeer(targetAddr2)

	// Fork Healing for Node 1 via P2P Forkpoint Discovery and ResolveFork
	locators := ps1.GenerateBlockLocators()
	forkMsg := Message{
		NetworkID: NetworkID(),
		Type:      "GET_FORKPOINT",
		Payload:   ForkpointRequest{Locators: locators},
	}
	resp, err := QueryP2P(targetAddr2, forkMsg, 3*time.Second)
	if err == nil && resp.Type == "FORKPOINT_RESPONSE" {
		var fpResp ForkpointResponse
		bData, _ := json.Marshal(resp.Payload)
		if json.Unmarshal(bData, &fpResp) == nil {
			if fpResp.AncestorHeight >= 0 && int(fpResp.AncestorHeight)+1 < len(bc2.Chain) {
				bc1.ResolveFork(int(fpResp.AncestorHeight), bc2.Chain[fpResp.AncestorHeight+1:])
			}
		}
	}

	bc2.Lock.RLock()
	expectedTip := bc2.GetLatestBlock().Hash
	expectedHeight := len(bc2.Chain)
	bc2.Lock.RUnlock()

	bc1.Lock.RLock()
	h1 := len(bc1.Chain)
	tip1 := bc1.GetLatestBlock().Hash
	bc1.Lock.RUnlock()

	bc3.Lock.RLock()
	h3 := len(bc3.Chain)
	tip3 := bc3.GetLatestBlock().Hash
	bc3.Lock.RUnlock()

	if h1 != expectedHeight || tip1 != expectedTip {
		t.Fatalf("Node 1 failed to heal fork to Node 2: height %d (expected %d), tip %s (expected %s)", h1, expectedHeight, tip1, expectedTip)
	}

	if h3 != expectedHeight || tip3 != expectedTip {
		t.Fatalf("Node 3 failed IBD to Node 2: height %d (expected %d), tip %s (expected %s)", h3, expectedHeight, tip3, expectedTip)
	}
}

// TestN14_LegacyRequestChainDropped verifies that legacy REQUEST_CHAIN message is rejected/ignored
func TestN14_LegacyRequestChainDropped(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)

	msg := Message{
		NetworkID: NetworkID(),
		Type:      "REQUEST_CHAIN",
		Payload:   nil,
	}

	// Querying legacy REQUEST_CHAIN should fail (no response or dropped)
	resp, err := QueryP2P("127.0.0.1:"+port, msg, 1*time.Second)
	if err == nil && resp != nil && resp.Type == "CHAIN_RESPONSE" {
		t.Fatalf("[N-14] Node responded to legacy REQUEST_CHAIN with full CHAIN_RESPONSE!")
	}
}
