//go:build testhooks

package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestL1_2_CrossNetworkP2PRejection verifies [L1-2]:
// 1. A testnet node rejects incoming P2P messages with mainnet NetworkID
// 2. A mainnet node rejects incoming P2P messages with testnet NetworkID
func TestL1_2_CrossNetworkP2PRejection(t *testing.T) {
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	// Subtest 1: Mainnet node rejects alien testnet message
	t.Run("MainnetRejectsTestnetMessage", func(t *testing.T) {
		if err := core.SetNetwork("mainnet"); err != nil {
			t.Fatalf("SetNetwork mainnet failed: %v", err)
		}

		bc := core.NewBlockchain(1)
		bc.StoragePath = filepath.Join(t.TempDir(), "mainnet.db")
		defer bc.Close()

		port := getFreePort(t)
		ps := NewPeerServer(port, bc)
		defer ps.Stop()
		go ps.StartServer()
		time.Sleep(100 * time.Millisecond)

		// Send message with Testnet NetworkID to Mainnet node
		alienMsg := Message{
			NetworkID: "ARMX-TESTNET-1",
			Type:      "GET_HEIGHT",
			Payload:   nil,
		}

		resp, err := QueryP2P("127.0.0.1:"+port, alienMsg, 1*time.Second)
		if err == nil && resp != nil {
			t.Fatalf("Mainnet node MUST reject alien testnet message, but got response: %+v", resp)
		}
	})

	// Subtest 2: Testnet node rejects alien mainnet message
	t.Run("TestnetRejectsMainnetMessage", func(t *testing.T) {
		if err := core.SetNetwork("testnet"); err != nil {
			t.Fatalf("SetNetwork testnet failed: %v", err)
		}

		bc := core.NewBlockchain(1)
		bc.StoragePath = filepath.Join(t.TempDir(), "testnet.db")
		defer bc.Close()

		port := getFreePort(t)
		ps := NewPeerServer(port, bc)
		defer ps.Stop()
		go ps.StartServer()
		time.Sleep(100 * time.Millisecond)

		// Send message with Mainnet NetworkID to Testnet node
		alienMsg := Message{
			NetworkID: "ARMX-MAINNET-1",
			Type:      "GET_HEIGHT",
			Payload:   nil,
		}

		resp, err := QueryP2P("127.0.0.1:"+port, alienMsg, 1*time.Second)
		if err == nil && resp != nil {
			t.Fatalf("Testnet node MUST reject alien mainnet message, but got response: %+v", resp)
		}
	})
}
