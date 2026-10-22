//go:build testhooks

package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestL1_1_NetworkLifecycle runs full lifecycle verification for both mainnet and testnet:
// 1. Create chain
// 2. Mine 5 blocks
// 3. Close chain and reopen from bbolt (verify no MarkDBInvalid)
// 4. RebuildState must succeed without error
// 5. Fresh node performs IBD via GET_FORKPOINT / GET_BLOCK_RANGE to match tip
func TestL1_1_NetworkLifecycle(t *testing.T) {
	networks := []string{"mainnet", "testnet"}

	for _, netName := range networks {
		t.Run("Network_"+netName, func(t *testing.T) {
			if err := core.SetNetwork(netName); err != nil {
				t.Fatalf("Failed to set network %s: %v", netName, err)
			}
			defer func() {
				_ = core.SetNetwork("mainnet")
			}()

			t.Setenv("ARMX_DATA_DIR", t.TempDir())
			t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

			nodeDir := filepath.Join(t.TempDir(), "node_primary")
			if err := os.MkdirAll(nodeDir, 0755); err != nil {
				t.Fatalf("Failed to create dir: %v", err)
			}
			dbPath := filepath.Join(nodeDir, "blockchain.db")

			core.SetDBPath(dbPath)
			defer core.SetDBPath("")
			bc := core.NewBlockchain(1)
			bc.StoragePath = dbPath

			core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
			defer core.SetTargetOverrideForTesting(nil)

			miner := core.GenesisMiner
			target := core.DifficultyToTarget(1)

			// Mine 5 blocks
			for i := 1; i <= 5; i++ {
				prev := bc.GetLatestBlock()
				tx := core.Transaction{
					Sender:    "SYSTEM",
					Recipient: miner,
					Amount:    core.InitialReward,
					Nonce:     int64(i),
					Signature: "REWARD_SIG",
				}
				b := *core.NewBlock([]core.Transaction{tx}, prev.Hash, i, miner, target)
				b.Timestamp = prev.Timestamp + int64(core.TargetBlockTimeSeconds) + 1
				for {
					b.Hash = b.CalculateHash()
					if core.HashMeetsTarget(b.Hash, target) {
						break
					}
					b.Nonce++
				}
				if !bc.ValidateAndAddBlock(b) {
					t.Fatalf("[%s] Failed to add block #%d", netName, i)
				}
			}

			if err := bc.SaveToFile(); err != nil {
				t.Fatalf("[%s] Failed to save blockchain to file: %v", netName, err)
			}
			tipHashBeforeClose := bc.GetLatestBlock().Hash
			tipHeightBeforeClose := len(bc.Chain)

			// Close blockchain bbolt handle
			bc.Close()

			// Check that no .invalid- files were created
			entries, err := os.ReadDir(nodeDir)
			if err != nil {
				t.Fatalf("[%s] Failed to read nodeDir: %v", netName, err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".invalid-") {
					t.Fatalf("[%s] Found invalid DB file on restart: %s", netName, e.Name())
				}
			}

			// Reopen from bbolt using LoadFromFile
			bcReopened, ok := core.LoadFromFile()
			if !ok {
				t.Fatalf("[%s] LoadFromFile failed; DB marked invalid or corrupt", netName)
			}
			defer bcReopened.Close()
			if len(bcReopened.Chain) != tipHeightBeforeClose {
				t.Fatalf("[%s] Loaded %d blocks, expected %d", netName, len(bcReopened.Chain), tipHeightBeforeClose)
			}
			if err := bcReopened.RebuildState(); err != nil {
				t.Fatalf("[%s] RebuildState failed after reopen: %v", netName, err)
			}
			if bcReopened.GetLatestBlock().Hash != tipHashBeforeClose {
				t.Fatalf("[%s] Reopened chain tip mismatch: got %s, want %s", netName, bcReopened.GetLatestBlock().Hash, tipHashBeforeClose)
			}

			// Start P2P server on reopened primary node
			portPrimary := getFreePort(t)
			psPrimary := NewPeerServer(portPrimary, bcReopened)
			go psPrimary.StartServer()

			// Create fresh node (IBD target)
			freshDir := filepath.Join(t.TempDir(), "node_fresh")
			if err := os.MkdirAll(freshDir, 0755); err != nil {
				t.Fatalf("Failed to create freshDir: %v", err)
			}
			bcFresh := core.NewBlockchain(1)
			bcFresh.StoragePath = filepath.Join(freshDir, "blockchain.db")
			defer bcFresh.Close()

			portFresh := getFreePort(t)
			psFresh := NewPeerServer(portFresh, bcFresh)
			go psFresh.StartServer()

			time.Sleep(100 * time.Millisecond)

			// Perform IBD from primary node
			psFresh.RequestChainSync("127.0.0.1:" + portPrimary)

			bcFresh.Lock.RLock()
			freshHeight := len(bcFresh.Chain)
			freshTip := bcFresh.GetLatestBlock().Hash
			bcFresh.Lock.RUnlock()

			if freshHeight != tipHeightBeforeClose || freshTip != tipHashBeforeClose {
				t.Fatalf("[%s] Fresh node IBD failed: got height %d (want %d), tip %s (want %s)",
					netName, freshHeight, tipHeightBeforeClose, freshTip, tipHashBeforeClose)
			}
		})
	}
}
