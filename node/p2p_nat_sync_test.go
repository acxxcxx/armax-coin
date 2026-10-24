//go:build testhooks

package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// TestP2P_NATNode_OutboundSync verifies that a node behind NAT without a public IP
// or open inbound listening port can successfully synchronize blocks and mempool
// from a public seed node, and relay new transactions and mined blocks over the established connection.
func TestP2P_NATNode_OutboundSync(t *testing.T) {
	tempDirPublic := t.TempDir()
	tempDirNAT := t.TempDir()

	// 1. Initialize Public Seed Node (Node A)
	dbPublic := filepath.Join(tempDirPublic, "chain_public.db")
	bcPublic, err := core.NewBlockchainWithStoragePath(1, dbPublic)
	if err != nil {
		t.Fatalf("Failed to init bcPublic: %v", err)
	}
	defer bcPublic.Close()
	bcPublic.CoinbaseMaturity = 1
	if bcPublic.State != nil {
		bcPublic.State.CoinbaseMaturity = 1
	}

	// 2. Initialize NAT Private Node (Node B)
	dbNAT := filepath.Join(tempDirNAT, "chain_nat.db")
	bcNAT, err := core.NewBlockchainWithStoragePath(1, dbNAT)
	if err != nil {
		t.Fatalf("Failed to init bcNAT: %v", err)
	}
	defer bcNAT.Close()
	bcNAT.CoinbaseMaturity = 1
	if bcNAT.State != nil {
		bcNAT.State.CoinbaseMaturity = 1
	}

	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	// Create wallets for mining and transactions
	minerPublic, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet error: %v", err)
	}

	minerNAT, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet error: %v", err)
	}

	// Mine 10 blocks on Public Node A so coinbase matures
	for i := 0; i < 10; i++ {
		_, err := bcPublic.MineBlockContext(context.Background(), minerPublic.Address)
		if err != nil {
			t.Fatalf("bcPublic mine error on block #%d: %v", i+1, err)
		}
	}

	// Add an unconfirmed transaction to Public Node A mempool
	txPublic := &core.Transaction{
		Sender:    minerPublic.Address,
		Recipient: minerNAT.Address,
		Amount:    5000,
		Fee:       11000,
		Nonce:     1,
	}
	if err := txPublic.Sign(minerPublic.PrivateKey); err != nil {
		t.Fatalf("Failed to sign txPublic: %v", err)
	}
	if added, msg := bcPublic.AddTransaction(*txPublic); !added {
		t.Fatalf("Failed to add txPublic to bcPublic: %s", msg)
	}

	// Start Public Seed Node A (listening on an open port)
	portPublic := getFreePort(t)
	psPublic := NewPeerServer(portPublic, bcPublic, tempDirPublic, "ARMX-TESTNET-1")
	psPublic.AllowPrivatePeers = true
	psPublic.RequireTLS = false
	RegisterPeerServer(psPublic)
	defer UnregisterPeerServer(psPublic)
	defer psPublic.Stop()
	go psPublic.StartServer()

	// Start NAT Node B (simulating a machine behind NAT with no inbound reachability:
	// it uses an unbound dummy port for incoming traffic, but dials outbound to Public Node A)
	dummyClosedPort := getFreePort(t)
	psNAT := NewPeerServer(dummyClosedPort, bcNAT, tempDirNAT, "ARMX-TESTNET-1")
	psNAT.AllowPrivatePeers = true
	psNAT.RequireTLS = false
	RegisterPeerServer(psNAT)
	defer UnregisterPeerServer(psNAT)
	// Notice: psNAT does NOT start a listening server (or listener is on closed port),
	// exactly simulating a node behind NAT without port forwarding.

	time.Sleep(200 * time.Millisecond)

	addrPublic := "127.0.0.1:" + portPublic
	_ = psNAT.AddPeer(addrPublic)

	// 3. NAT Node B initiates sync with Public Node A
	psNAT.SyncBestPeer()

	// Wait up to 5 seconds for block catch-up sync to complete
	deadlineBlocks := time.Now().Add(5 * time.Second)
	blocksSynced := false
	for time.Now().Before(deadlineBlocks) {
		bcNAT.Lock.RLock()
		h := bcNAT.GetLatestBlockUnsafe().Index
		bcNAT.Lock.RUnlock()
		if h >= 10 {
			blocksSynced = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !blocksSynced {
		bcNAT.Lock.RLock()
		defer bcNAT.Lock.RUnlock()
		t.Fatalf("NAT Node B failed to sync blocks from Public Node A! Height: %d (expected >= 10)", bcNAT.GetLatestBlockUnsafe().Index)
	}

	// 4. NAT Node B queries Mempool from Public Node A
	psNAT.SyncMempool()

	deadlineMempool := time.Now().Add(3 * time.Second)
	mempoolSynced := false
	for time.Now().Before(deadlineMempool) {
		bcNAT.Lock.RLock()
		if len(bcNAT.Mempool) == 1 && bcNAT.Mempool[0].HashHex() == txPublic.HashHex() {
			mempoolSynced = true
			bcNAT.Lock.RUnlock()
			break
		}
		bcNAT.Lock.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}

	if !mempoolSynced {
		bcNAT.Lock.RLock()
		defer bcNAT.Lock.RUnlock()
		t.Fatalf("NAT Node B failed to sync mempool transaction from Public Node A! Mempool size: %d", len(bcNAT.Mempool))
	}

	// 5. NAT Node B mines Block #11 including the mempool transaction
	// and relays it back to Public Node A via the established connection
	minedBlock, err := bcNAT.MineBlockContext(context.Background(), minerNAT.Address)
	if err != nil {
		t.Fatalf("NAT Node B failed to mine block #11: %v", err)
	}

	// Relay block from NAT Node B to Public Node A
	psNAT.RelayBlock(minedBlock, "")

	deadlineToPublic := time.Now().Add(3 * time.Second)
	publicReceivedBlock := false
	for time.Now().Before(deadlineToPublic) {
		bcPublic.Lock.RLock()
		latest := bcPublic.GetLatestBlockUnsafe()
		bcPublic.Lock.RUnlock()
		if latest.Index >= 11 && latest.Hash == minedBlock.Hash {
			publicReceivedBlock = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !publicReceivedBlock {
		bcPublic.Lock.RLock()
		defer bcPublic.Lock.RUnlock()
		t.Fatalf("Public Node A failed to accept mined block from NAT Node B! Height: %d (expected 11)", bcPublic.GetLatestBlockUnsafe().Index)
	}
}
