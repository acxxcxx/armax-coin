package node

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func waitForTxCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

// TestP2PTransactionRelayOverLoopback verifies end-to-end NEW_TX gossip across isolated P2P nodes:
// 1. Node A creates and admits valid transaction to its mempool.
// 2. Node A sends NEW_TX over TLS P2P to Node B.
// 3. Node B validates and admits transaction to its mempool.
// 4. Duplicate transaction submission does not create duplicate entries in mempool.
// 5. Invalid transaction (tampered signature / amount) is rejected and does not enter mempool.
// 6. Disconnect and reconnect functions cleanly without panics.
func TestP2PTransactionRelayOverLoopback(t *testing.T) {
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := t.TempDir()
	dbPathA := filepath.Join(dir, "node-a", "blockchain.db")
	dbPathB := filepath.Join(dir, "node-b", "blockchain.db")

	bcA, err := core.NewBlockchainWithStoragePath(1, dbPathA)
	if err != nil {
		t.Fatal(err)
	}
	defer bcA.Close()

	bcB, err := core.NewBlockchainWithStoragePath(1, dbPathB)
	if err != nil {
		t.Fatal(err)
	}
	defer bcB.Close()

	// Fund sender account in state of both nodes
	sender := newTestAccount(t, "sender")
	recipient := newTestAccount(t, "recipient")
	addrHex, _ := core.AddressToHex(sender.Address)

	for _, bc := range []*core.Blockchain{bcA, bcB} {
		bc.Lock.Lock()
		bc.State.Accounts[addrHex] = core.AccountState{
			Balance: 100 * core.CoinUnit,
			Nonce:   0,
		}
		bc.Lock.Unlock()
	}

	portA := getFreePort(t)
	portB := getFreePort(t)
	_ = net.JoinHostPort("127.0.0.1", portA)
	addrB := net.JoinHostPort("127.0.0.1", portB)

	psA := NewPeerServer(portA, bcA, filepath.Join(dir, "node-a"), "ARMX-TESTNET-1")
	psB := NewPeerServer(portB, bcB, filepath.Join(dir, "node-b"), "ARMX-TESTNET-1")

	for _, peer := range []*PeerServer{psA, psB} {
		peer.RequireTLS = true
		peer.P2PTOFU = true
		peer.AllowPrivatePeers = true
		peer.DisableSubnetLimit = true
	}

	go psA.StartServer()
	go psB.StartServer()
	defer psA.Stop()
	defer psB.Stop()
	waitForPeerServer(t, psA)
	waitForPeerServer(t, psB)

	// Establish bidirectional P2P TLS session
	resp, err := psA.QueryPeer(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 3*time.Second)
	if err != nil || resp == nil || resp.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Handshake failed: err=%v resp=%#v", err, resp)
	}

	// 1. Node A creates valid transaction
	tx1 := core.Transaction{
		Sender:    sender.Address,
		SenderPub: sender.PubHex,
		Recipient: recipient.Address,
		Amount:    5 * core.CoinUnit,
		Fee:       50000,
		Nonce:     1,
		Type:      core.TxTypeStandard,
	}
	if err := tx1.Sign(sender.PrivKey); err != nil {
		t.Fatalf("Failed to sign tx1: %v", err)
	}

	// 2. Transaction enters Mempool A
	okA, msgA := bcA.AddTransaction(tx1)
	if !okA {
		t.Fatalf("Node A rejected tx1: %s", msgA)
	}
	bcA.Lock.RLock()
	lenA := len(bcA.Mempool)
	bcA.Lock.RUnlock()
	if lenA != 1 {
		t.Fatalf("Expected Mempool A length 1, got %d", lenA)
	}

	// 3. Node A sends NEW_TX to Node B over P2P
	if err := psA.SendMessage(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "NEW_TX",
		Payload:   tx1,
	}); err != nil {
		t.Fatalf("Failed to send NEW_TX over P2P: %v", err)
	}

	// 4 & 5. Node B receives, validates, and admits transaction to Mempool B
	waitForTxCondition(t, 3*time.Second, func() bool {
		bcB.Lock.RLock()
		defer bcB.Lock.RUnlock()
		return len(bcB.Mempool) == 1 && bcB.Mempool[0].HashHex() == tx1.HashHex()
	}, "Node B did not receive or admit tx1 into its mempool via P2P relay")

	// 6. Duplicate transaction relay does not create duplicate entries in Mempool B
	if err := psA.SendMessage(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "NEW_TX",
		Payload:   tx1,
	}); err != nil {
		t.Fatalf("Failed to send duplicate NEW_TX: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	bcB.Lock.RLock()
	lenB := len(bcB.Mempool)
	bcB.Lock.RUnlock()
	if lenB != 1 {
		t.Fatalf("Duplicate NEW_TX created multiple mempool entries: count=%d", lenB)
	}

	// 7. Invalid transaction (tampered signature) is rejected by Node B
	badTx := tx1
	badTx.Nonce = 99 // nonce mismatch / broken signature
	if err := psA.SendMessage(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "NEW_TX",
		Payload:   badTx,
	}); err != nil {
		t.Fatalf("Failed to send badTx: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	bcB.Lock.RLock()
	lenBAfterBad := len(bcB.Mempool)
	bcB.Lock.RUnlock()
	if lenBAfterBad != 1 {
		t.Fatalf("Invalid transaction entered Mempool B: count=%d", lenBAfterBad)
	}

	// 8. Disconnect and reconnect functions cleanly
	psA.outboundMu.Lock()
	session := psA.outboundSessions[addrB]
	psA.outboundMu.Unlock()
	psA.dropOutboundSession(addrB, session)

	// Reconnect and query height
	resp2, err := psA.QueryPeer(addrB, Message{
		NetworkID: psA.GetNetworkID(),
		Type:      "GET_HEIGHT",
	}, 3*time.Second)
	if err != nil || resp2 == nil || resp2.Type != "HEIGHT_RESPONSE" {
		t.Fatalf("Reconnect failed: err=%v resp=%#v", err, resp2)
	}

	t.Logf("[PASS] NEW_TX relay verified: tx %s successfully propagated from Node A to Node B", tx1.HashHex())
}
