//go:build testhooks

package node

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestRegtest_ProfileAndGenesisIsolation verifies that regtest does not share NetworkID or Genesis with mainnet/testnet [L4-8]
func TestRegtest_ProfileAndGenesisIsolation(t *testing.T) {
	if core.RegtestProfile.NetworkID == core.MainnetProfile.NetworkID {
		t.Fatalf("regtest must not share NetworkID with mainnet, got: %s", core.RegtestProfile.NetworkID)
	}
	if core.RegtestProfile.NetworkID == core.TestnetProfile.NetworkID {
		t.Fatalf("regtest must not share NetworkID with testnet, got: %s", core.RegtestProfile.NetworkID)
	}
	if core.RegtestProfile.NetworkID != "ARMX-REGTEST-1" {
		t.Fatalf("Expected regtest NetworkID 'ARMX-REGTEST-1', got: %s", core.RegtestProfile.NetworkID)
	}

	regGen := core.GetRegtestGenesisBlock()
	mainGen := core.GetMainnetGenesisBlock()
	testGen := core.GetTestnetGenesisBlock()

	if regGen.Hash == mainGen.Hash {
		t.Fatalf("regtest genesis hash must not match mainnet genesis")
	}
	if regGen.Hash == testGen.Hash {
		t.Fatalf("regtest genesis hash must not match testnet genesis")
	}
	if regGen.Target != core.RegtestProfile.PowLimitHex {
		t.Fatalf("regtest genesis target mismatch: expected %s, got %s", core.RegtestProfile.PowLimitHex, regGen.Target)
	}
	if regGen.CalculateHash() != regGen.Hash {
		t.Fatalf("regtest genesis CalculatedHash mismatch: %s != %s", regGen.CalculateHash(), regGen.Hash)
	}
}

// TestRegtest_LWMAObservable150Blocks verifies that 150 blocks can be mined and LWMA observed in < 2 minutes [L4-8]
func TestRegtest_LWMAObservable150Blocks(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)

	if err := core.SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest) failed: %v", err)
	}
	defer func() { _ = core.SetNetwork("testnet") }()

	bc := core.NewBlockchain(2)
	defer bc.Close()

	start := time.Now()
	minerAddr := core.GenesisMiner
	baseTime := bc.GetLatestBlock().Timestamp

	// Mine 150 blocks sequentially on regtest (8-bit target)
	for i := 1; i <= 150; i++ {
		prev := bc.GetLatestBlock()
		target := bc.CalculateLWMA1()
		cbTx := core.Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    core.InitialReward,
			Nonce:     int64(i),
			Signature: "REWARD_SIG",
		}
		b := *core.NewBlock([]core.Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = baseTime + int64(i*120) // 120-second target block spacing
		for {
			b.Hash = b.CalculateHash()
			if core.HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if err := bc.State.VerifyAndApplyBlock(&b, core.InitialReward); err != nil {
			t.Fatalf("Failed to apply block #%d: %v", i, err)
		}
		bc.Lock.Lock()
		bc.Chain = append(bc.Chain, b)
		bc.Lock.Unlock()
	}

	elapsed := time.Since(start)
	t.Logf("[OK] Successfully mined 150 regtest blocks in %v (< 2 minutes)", elapsed)
	if elapsed > 2*time.Minute {
		t.Fatalf("Mined 150 regtest blocks took %v, exceeding 2 minute limit", elapsed)
	}

	// Verify LWMA target calculation over 150 blocks
	target := bc.CalculateLWMA1()
	if target == nil || target.Sign() <= 0 {
		t.Fatalf("Expected valid non-zero LWMA target after 150 blocks, got: %v", target)
	}
	t.Logf("[OK] Observed LWMA target after 150 blocks: %064x", target)
}

// TestRegtest_MessageRejection verifies that mainnet and testnet nodes reject regtest messages [L4-8]
func TestRegtest_MessageRejection(t *testing.T) {
	core.SetNetworkForTesting(true)
	defer core.SetNetworkForTesting(true)

	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	bc := core.NewBlockchain(1)
	defer bc.Close()

	testnetServer := NewPeerServer(port, bc)
	testnetServer.AllowPrivatePeers = true
	testnetServer.DisableSubnetLimit = true
	go testnetServer.StartServer()
	defer testnetServer.Stop()

	// Wait for testnet server to accept connections
	time.Sleep(50 * time.Millisecond)

	// Send message with regtest NetworkID to testnet node -> must be rejected
	regtestMsg := Message{
		NetworkID: core.RegtestProfile.NetworkID, // "ARMX-REGTEST-1"
		Type:      "GET_HEIGHT",
	}

	resp, err := QueryP2P("127.0.0.1:"+port, regtestMsg, 2*time.Second)
	if err == nil && resp != nil {
		t.Fatalf("Expected testnet node to reject regtest message, but got response: %+v", resp)
	}
	t.Logf("[OK] Testnet node successfully rejected message from regtest NetworkID: %v", err)

	// Test non-loopback peer rejection in regtest PeerServer [L4-8]
	regtestServer := NewPeerServer("29334", bc, t.TempDir(), core.RegtestProfile.NetworkID)
	regtestServer.AllowPrivatePeers = true
	regtestServer.AddPeer("203.0.113.50:29334") // Non-loopback public IP
	for _, p := range regtestServer.GetPeers() {
		if p == "203.0.113.50:29334" {
			t.Fatalf("regtest PeerServer must reject non-loopback peer address!")
		}
	}
	regtestServer.AddPeer("127.0.0.1:29334") // Loopback IP
	foundLoopback := false
	for _, p := range regtestServer.GetPeers() {
		if p == "127.0.0.1:29334" {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Fatalf("regtest PeerServer should have accepted loopback peer 127.0.0.1:29334")
	}
	t.Logf("[OK] Regtest PeerServer correctly rejected non-loopback peer and accepted loopback peer")
}
