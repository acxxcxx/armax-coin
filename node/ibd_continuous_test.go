//go:build testhooks

package node

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestIBD_ContinuousSyncPastOldLimit verifies that Initial Block Download (IBD)
// can continuously synchronize past the old 1250-block / 5-request limit without
// getting throttled, timed out, or banned (BTC/LTC standard).
func TestIBD_ContinuousSyncPastOldLimit(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}
	defer func() {
		_ = core.SetNetwork("mainnet")
	}()

	// Use easy target for fast test execution
	easyTarget, _ := new(big.Int).SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
	core.SetTargetOverrideForTesting(easyTarget)
	defer core.SetTargetOverrideForTesting(nil)

	dirPrimary := filepath.Join(t.TempDir(), "primary_dir")
	if err := os.MkdirAll(dirPrimary, 0755); err != nil {
		t.Fatalf("failed to create primary dir: %v", err)
	}
	dbPrimary := filepath.Join(dirPrimary, "primary.db")
	bcPrimary, err := core.NewBlockchainWithStoragePath(1, dbPrimary)
	if err != nil {
		t.Fatalf("failed to create primary blockchain: %v", err)
	}
	defer bcPrimary.Close()

	bcPrimary.Chain[0].Target = fmt.Sprintf("%064x", easyTarget)

	// Synthesize 1,500 valid sequential blocks (requires 6 GET_BLOCK_RANGE chunks)
	const targetHeight = 1500
	bcPrimary.Lock.Lock()
	prev := bcPrimary.Chain[0]
	for i := 1; i <= targetHeight; i++ {
		cb := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: core.GenesisMiner,
			Amount:    core.InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}
		blk := core.NewBlock([]core.Transaction{cb}, prev.Hash, i, core.GenesisMiner, easyTarget)
		blk.Timestamp = prev.Timestamp + 1
		blk.Hash = blk.CalculateHash()
		bcPrimary.Chain = append(bcPrimary.Chain, *blk)
		prev = *blk
	}
	bcPrimary.InvalidateChainworkCache()
	bcPrimary.Lock.Unlock()

	portPrimary := getFreePort(t)
	psPrimary := NewPeerServer(portPrimary, bcPrimary, dirPrimary)
	defer psPrimary.Stop()
	go psPrimary.StartServer()

	time.Sleep(100 * time.Millisecond)

	// 2. Fresh node starting from Genesis (block 0)
	dirFresh := filepath.Join(t.TempDir(), "fresh_dir")
	if err := os.MkdirAll(dirFresh, 0755); err != nil {
		t.Fatalf("failed to create fresh dir: %v", err)
	}
	dbFresh := filepath.Join(dirFresh, "fresh.db")
	bcFresh, err := core.NewBlockchainWithStoragePath(1, dbFresh)
	if err != nil {
		t.Fatalf("failed to create fresh blockchain: %v", err)
	}
	defer bcFresh.Close()

	bcFresh.Chain[0].Target = fmt.Sprintf("%064x", easyTarget)
	bcFresh.InvalidateChainworkCache()

	portFresh := getFreePort(t)
	psFresh := NewPeerServer(portFresh, bcFresh, dirFresh)
	defer psFresh.Stop()
	go psFresh.StartServer()

	time.Sleep(100 * time.Millisecond)

	// 3. Initiate RequestChainSync from fresh node to primary
	resp, qErr := psFresh.QueryPeer("127.0.0.1:"+portPrimary, Message{
		NetworkID: psFresh.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 5*time.Second)
	t.Logf("Diagnostic GET_HEIGHT resp: %+v, err: %v", resp, qErr)
	psFresh.RequestChainSync("127.0.0.1:" + portPrimary)

	// 4. Verify fresh node successfully reached tip #1500 in continuous sync
	bcFresh.Lock.RLock()
	freshHeight := bcFresh.GetLatestBlockUnsafe().Index
	freshCW := new(big.Int).Set(bcFresh.GetTotalChainworkUnsafe())
	bcFresh.Lock.RUnlock()

	if freshHeight != targetHeight {
		t.Fatalf("Expected fresh node to reach height %d, got %d", targetHeight, freshHeight)
	}

	bcPrimary.Lock.RLock()
	primaryCW := new(big.Int).Set(bcPrimary.GetTotalChainworkUnsafe())
	bcPrimary.Lock.RUnlock()

	if freshCW.Cmp(primaryCW) != 0 {
		t.Fatalf("Chainwork mismatch: fresh %s != primary %s", freshCW.String(), primaryCW.String())
	}

	// 5. Verify fresh node was never banned
	if psPrimary.IsBanned("127.0.0.1") {
		t.Fatalf("Primary node banned the honest syncing node!")
	}
}
