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

// TestP2P_MempoolSync_BIP35 verifies that when a node comes online later or requests mempool sync,
// it receives unmined transactions from connected peers (BIP-35 standard)
func TestP2P_MempoolSync_BIP35(t *testing.T) {
	tempDirA := t.TempDir()
	tempDirB := t.TempDir()

	dbA := filepath.Join(tempDirA, "chainA.db")
	bcA, err := core.NewBlockchainWithStoragePath(1, dbA)
	if err != nil {
		t.Fatalf("Failed to init bcA: %v", err)
	}
	defer bcA.Close()
	bcA.CoinbaseMaturity = 1
	if bcA.State != nil {
		bcA.State.CoinbaseMaturity = 1
	}

	dbB := filepath.Join(tempDirB, "chainB.db")
	bcB, err := core.NewBlockchainWithStoragePath(1, dbB)
	if err != nil {
		t.Fatalf("Failed to init bcB: %v", err)
	}
	defer bcB.Close()
	bcB.CoinbaseMaturity = 1
	if bcB.State != nil {
		bcB.State.CoinbaseMaturity = 1
	}

	minerWallet, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet error: %v", err)
	}

	core.SetTargetOverrideForTesting(core.DifficultyToTarget(1))
	defer core.SetTargetOverrideForTesting(nil)

	// Mine 10 blocks to mature coinbase reward
	for i := 0; i < 10; i++ {
		blk, err := bcA.MineBlockContext(context.Background(), minerWallet.Address)
		if err != nil {
			t.Fatalf("bcA mine error: %v", err)
		}
		if !bcB.ValidateAndAddBlock(blk) {
			t.Fatalf("bcB failed to add blk #%d", blk.Index)
		}
	}

	portA := getFreePort(t)
	portB := getFreePort(t)

	psA := NewPeerServer(portA, bcA, tempDirA, "ARMX-TESTNET-1")
	psA.AllowPrivatePeers = true
	psA.RequireTLS = false
	RegisterPeerServer(psA)
	defer UnregisterPeerServer(psA)
	defer psA.Stop()
	go psA.StartServer()

	psB := NewPeerServer(portB, bcB, tempDirB, "ARMX-TESTNET-1")
	psB.AllowPrivatePeers = true
	psB.RequireTLS = false
	RegisterPeerServer(psB)
	defer UnregisterPeerServer(psB)
	defer psB.Stop()
	go psB.StartServer()

	time.Sleep(300 * time.Millisecond)

	addrA := "127.0.0.1:" + portA
	addrB := "127.0.0.1:" + portB

	_ = psA.AddPeer(addrB)
	_ = psB.AddPeer(addrA)

	// Create valid user transaction on Node A
	recipWallet, err := wallet.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet error: %v", err)
	}

	tx := &core.Transaction{
		Sender:    minerWallet.Address,
		Recipient: recipWallet.Address,
		Amount:    1000,
		Fee:       11000,
		Nonce:     1,
	}
	if err := tx.Sign(minerWallet.PrivateKey); err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	// Add tx ONLY to Node A
	added, msg := bcA.AddTransaction(*tx)
	if !added {
		t.Fatalf("Failed to add tx to bcA: %s", msg)
	}

	// Verify Node B initially has empty mempool
	bcB.Lock.RLock()
	if len(bcB.Mempool) != 0 {
		t.Fatalf("Expected Node B mempool to be empty initially")
	}
	bcB.Lock.RUnlock()

	// Node B calls SyncMempool() to sync unconfirmed transactions from Node A
	psB.SyncMempool()

	// Wait up to 3 seconds for transaction to be processed on Node B
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		bcB.Lock.RLock()
		if len(bcB.Mempool) == 1 && bcB.Mempool[0].HashHex() == tx.HashHex() {
			found = true
			bcB.Lock.RUnlock()
			break
		}
		bcB.Lock.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}

	if !found {
		bcB.Lock.RLock()
		defer bcB.Lock.RUnlock()
		t.Fatalf("Node B failed to synchronize mempool from Node A! Node B mempool size: %d", len(bcB.Mempool))
	}

	// Now simulate Node C (e.g. miner that was offline and reconnects)
	tempDirC := t.TempDir()
	dbC := filepath.Join(tempDirC, "chainC.db")
	bcC, err := core.NewBlockchainWithStoragePath(1, dbC)
	if err != nil {
		t.Fatalf("Failed to init bcC: %v", err)
	}
	defer bcC.Close()
	bcC.CoinbaseMaturity = 1
	if bcC.State != nil {
		bcC.State.CoinbaseMaturity = 1
	}

	// Sync blocks to C
	bcA.Lock.RLock()
	for _, b := range bcA.Chain {
		bcC.ValidateAndAddBlock(b)
	}
	bcA.Lock.RUnlock()

	portC := getFreePort(t)
	psC := NewPeerServer(portC, bcC, tempDirC, "ARMX-TESTNET-1")
	psC.AllowPrivatePeers = true
	psC.RequireTLS = false
	RegisterPeerServer(psC)
	defer UnregisterPeerServer(psC)
	defer psC.Stop()
	go psC.StartServer()

	time.Sleep(300 * time.Millisecond)

	addrC := "127.0.0.1:" + portC
	_ = psA.AddPeer(addrC)
	_ = psC.AddPeer(addrA)

	// Node A periodically rebroadcasts mempool to all peers (including newly connected Node C)
	psA.RebroadcastMempool()

	deadlineC := time.Now().Add(3 * time.Second)
	foundC := false
	for time.Now().Before(deadlineC) {
		bcC.Lock.RLock()
		if len(bcC.Mempool) == 1 && bcC.Mempool[0].HashHex() == tx.HashHex() {
			foundC = true
			bcC.Lock.RUnlock()
			break
		}
		bcC.Lock.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}

	if !foundC {
		bcC.Lock.RLock()
		defer bcC.Lock.RUnlock()
		t.Fatalf("Node C failed to receive rebroadcasted mempool from Node A! Node C mempool size: %d", len(bcC.Mempool))
	}
}
