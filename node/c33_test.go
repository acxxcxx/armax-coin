//go:build testhooks

package node

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestC33_NetworkProfiles verifies the configuration of mainnet and testnet network profiles [C-33].
func TestC33_NetworkProfiles(t *testing.T) {
	// 1. Mainnet profile verification
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("failed to set mainnet profile: %v", err)
	}
	mainnet := core.GetActiveNetwork()
	if mainnet.NetworkID != "ARMX-MAINNET-1" {
		t.Errorf("expected mainnet NetworkID 'ARMX-MAINNET-1', got '%s'", mainnet.NetworkID)
	}
	if mainnet.P2PPort != core.MainnetP2PPort {
		t.Errorf("expected mainnet P2PPort '%s', got '%s'", core.MainnetP2PPort, mainnet.P2PPort)
	}
	if mainnet.RPCPort != core.MainnetRPCPort {
		t.Errorf("expected mainnet RPCPort '%s', got '%s'", core.MainnetRPCPort, mainnet.RPCPort)
	}

	// 2. Testnet profile verification
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("failed to set testnet profile: %v", err)
	}
	testnet := core.GetActiveNetwork()
	if testnet.NetworkID != "ARMX-TESTNET-1" {
		t.Errorf("expected testnet NetworkID 'ARMX-TESTNET-1', got '%s'", testnet.NetworkID)
	}
	if testnet.P2PPort != core.TestnetP2PPort {
		t.Errorf("expected testnet P2PPort '%s', got '%s'", core.TestnetP2PPort, testnet.P2PPort)
	}
	if testnet.RPCPort != core.TestnetRPCPort {
		t.Errorf("expected testnet RPCPort '%s', got '%s'", core.TestnetRPCPort, testnet.RPCPort)
	}

	// Reset back to mainnet
	_ = core.SetNetwork("mainnet")
}

// TestC33_MainnetRejectsTestnetP2PConnection verifies that a mainnet node rejects
// incoming messages or queries sent from a testnet node/peer with mismatched NetworkID [C-33].
func TestC33_MainnetRejectsTestnetP2PConnection(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	tempDir := t.TempDir()
	core.SetDBPath(filepath.Join(tempDir, "test.db"))
	defer core.SetDBPath("")

	_ = core.SetNetwork("mainnet")

	bc := &core.Blockchain{
		Chain:       []core.Block{core.GetGenesisBlock()},
		Difficulty:  1,
		Mempool:     []core.Transaction{},
		BlockReward: core.InitialReward,
		State:       core.NewStateManager(),
	}
	_ = bc.State.VerifyAndApplyBlock(&bc.Chain[0], core.InitialReward)

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)
	target := "127.0.0.1:" + port

	// Connect as an alien / testnet peer
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.Close()

	// Send GET_HEIGHT message stamped with alien Testnet NetworkID
	testnetMsg := Message{
		NetworkID: "ARMX-TESTNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	data, _ := json.Marshal(testnetMsg)
	_, err = conn.Write(append(data, '\n'))
	if err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// Mainnet server must not respond with HEIGHT_RESPONSE and should disconnect/ignore alien message
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, readErr := conn.Read(buf)
	if readErr == nil && n > 0 {
		var resp Message
		if err := json.Unmarshal(buf[:n], &resp); err == nil && resp.Type == "HEIGHT_RESPONSE" {
			t.Fatalf("[C-33] FAILED: Mainnet server responded to testnet peer packet: %s", string(buf[:n]))
		}
	}
}
