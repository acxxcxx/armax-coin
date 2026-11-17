package node

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestForkpointLocatorCap_ServerTruncatesOversizedLocators verifies that PeerServer's
// GET_FORKPOINT handler truncates oversized locator lists to 64 entries and responds fast.
func TestForkpointLocatorCap_ServerTruncatesOversizedLocators(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatalf("failed to create server blockchain: %v", err)
	}
	defer bc.Close()

	// Mine block #1 so we can place its real hash past index 64 (at index 100).
	prev := bc.Chain[0]
	target := core.DifficultyToTarget(1)
	bc.Chain[0].Target = fmt.Sprintf("%064x", target)
	b1 := *core.NewBlock([]core.Transaction{{
		Sender:    "SYSTEM",
		Recipient: core.GenesisMiner,
		Amount:    core.InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}}, prev.Hash, 1, core.GenesisMiner, target)
	b1.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds)
	for {
		b1.Hash = b1.CalculateHash()
		if core.HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}
	if !bc.ValidateAndAddBlock(b1) {
		t.Fatal("failed to add mined block #1")
	}

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()
	defer ps.Stop()

	clientBC, err := core.NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatalf("failed to create client blockchain: %v", err)
	}
	defer clientBC.Close()
	clientPS := NewPeerServer(getFreePort(t), clientBC)

	time.Sleep(100 * time.Millisecond)

	// Build 1000 locators where real block #1 hash is placed at index 100 (beyond the 64 cap).
	locators := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		locators[i] = fmt.Sprintf("%064x", i+1)
	}
	locators[100] = b1.Hash

	forkMsg := Message{
		NetworkID: ps.GetNetworkID(),
		Type:      "GET_FORKPOINT",
		Payload:   ForkpointRequest{Locators: locators},
	}

	start := time.Now()
	forkResp, err := clientPS.QueryPeer("127.0.0.1:"+port, forkMsg, 5*time.Second)
	if err != nil {
		t.Fatalf("QueryPeer GET_FORKPOINT failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("GET_FORKPOINT took %v, expected < 500ms", elapsed)
	}
	if forkResp == nil || forkResp.Type != "FORKPOINT_RESPONSE" {
		t.Fatalf("expected FORKPOINT_RESPONSE, got %+v", forkResp)
	}

	raw, _ := json.Marshal(forkResp.Payload)
	var resp ForkpointResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("failed to decode ForkpointResponse: %v", err)
	}
	// Because p2p.go truncates locators to 64 entries, index 100 (b1.Hash) must NOT be reached,
	// forcing fallback to genesis ancestor height 0.
	if resp.AncestorHeight != 0 {
		t.Fatalf("expected AncestorHeight=0 due to 64-locator cap, got %d", resp.AncestorHeight)
	}
}
