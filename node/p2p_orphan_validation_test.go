package node

import (
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN17_OrphanBlockHeaderPoWValidation verifies [N-17]:
// An orphan block must pass header PoW and Merkle validation before being stored in orphan buffer.
// Fake orphan blocks are rejected and penalize the peer.
func TestN17_OrphanBlockHeaderPoWValidation(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(getFreePort(t), bc)

	peerIP := "198.51.100.1"

	// 1. Create fake orphan block with invalid Merkle root
	target := core.DifficultyToTarget(1)
	fakeBlock := *core.NewBlock([]core.Transaction{}, "00000000prev", 100, core.GenesisMiner, target)
	fakeBlock.MerkleRoot = "bad_merkle"
	fakeBlock.Hash = fakeBlock.CalculateHash()

	stored := ps.AddOrphanBlock(fakeBlock, peerIP)
	if stored {
		t.Fatalf("[N-17] Fake orphan block with invalid Merkle root was stored!")
	}
	if ps.GetBanScore(peerIP) == 0 {
		t.Fatalf("[N-17] Peer was not penalized for sending invalid PoW orphan block")
	}

	// 2. Create valid PoW orphan block
	validOrphan := *core.NewBlock([]core.Transaction{}, "00000000prev", 100, core.GenesisMiner, target)
	for {
		validOrphan.Hash = validOrphan.CalculateHash()
		if core.HashMeetsTarget(validOrphan.Hash, target) {
			break
		}
		validOrphan.Nonce++
	}

	storedValid := ps.AddOrphanBlock(validOrphan, peerIP)
	if !storedValid {
		t.Fatalf("[N-17] Valid PoW orphan block was rejected from orphan buffer")
	}

	// Must be keyed by its own Hash, not PrevHash
	if !ps.HasOrphanBlock(validOrphan.Hash) {
		t.Fatalf("[N-17] Orphan block was not stored keyed by block.Hash")
	}
}

// TestN17_OrphanBlocksKeyedByBlockHashNoOverwrite verifies that two orphan blocks sharing
// the same parent (PrevHash) do not overwrite each other [N-17].
func TestN17_OrphanBlocksKeyedByBlockHashNoOverwrite(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(getFreePort(t), bc)

	target := core.DifficultyToTarget(1)
	parentHash := "00000000shared_parent"

	// Orphan branch A
	bA := *core.NewBlock([]core.Transaction{}, parentHash, 10, "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqx6r7we", target)
	bA.Timestamp = 1000
	for {
		bA.Hash = bA.CalculateHash()
		if core.HashMeetsTarget(bA.Hash, target) {
			break
		}
		bA.Nonce++
	}

	// Orphan branch B
	bB := *core.NewBlock([]core.Transaction{}, parentHash, 10, "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqx6r7we", target)
	bB.Timestamp = 2000
	for {
		bB.Hash = bB.CalculateHash()
		if core.HashMeetsTarget(bB.Hash, target) {
			break
		}
		bB.Nonce++
	}

	ps.AddOrphanBlock(bA, "10.0.0.1")
	ps.AddOrphanBlock(bB, "10.0.0.2")

	if !ps.HasOrphanBlock(bA.Hash) || !ps.HasOrphanBlock(bB.Hash) {
		t.Fatalf("[N-17] Orphan blocks sharing same PrevHash overwritten! (Has bA: %v, Has bB: %v)",
			ps.HasOrphanBlock(bA.Hash), ps.HasOrphanBlock(bB.Hash))
	}
}

// TestN17_OrphanTransactionPool verifies out-of-order transactions are held in orphan pool
// and promoted to mempool when the predecessor transaction arrives [N-17].
func TestN17_OrphanTransactionPool(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	ps := NewPeerServer(getFreePort(t), bc)

	accSender := newTestAccount(t, "sender")
	privKey, pubHex, addr := accSender.PrivKey, accSender.PubHex, accSender.Address
	addrHex, _ := core.AddressToHex(addr)

	// Pre-fund account with balance and nonce 0
	bc.State.Accounts[addrHex] = core.AccountState{
		Balance: 100 * core.CoinUnit,
		Nonce:   0,
	}

	recip := newTestAccount(t, "recip").Address

	// 1. Create tx2 with Nonce = 2 (out of order, expects Nonce = 1 first)
	tx2 := core.Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: recip,
		Amount:    10 * core.CoinUnit,
		Fee:       core.MinRelayFee * 12,
		Nonce:     2,
		Type:      core.TxTypeStandard,
	}
	_ = tx2.Sign(privKey)

	// 2. Submit tx2: must be accepted into orphan pool
	accepted2 := ps.ProcessIncomingTx(tx2, "10.0.0.1")
	if !accepted2 {
		t.Fatalf("[N-17] Out-of-order tx2 was rejected instead of stored in orphan pool")
	}
	if !ps.HasOrphanTx(tx2.HashHex()) {
		t.Fatalf("[N-17] tx2 missing from orphan tx pool")
	}

	// 3. Create tx1 with Nonce = 1 (the missing predecessor)
	tx1 := core.Transaction{
		Sender:    addr,
		SenderPub: pubHex,
		Recipient: recip,
		Amount:    10 * core.CoinUnit,
		Fee:       core.MinRelayFee * 12,
		Nonce:     1,
		Type:      core.TxTypeStandard,
	}
	_ = tx1.Sign(privKey)

	// 4. Submit tx1: must enter mempool and automatically promote tx2 from orphan pool!
	accepted1 := ps.ProcessIncomingTx(tx1, "10.0.0.1")
	if !accepted1 {
		t.Fatalf("[N-17] Predecessor tx1 was rejected")
	}

	// Now both tx1 and tx2 should be confirmed in mempool
	bc.Lock.RLock()
	mempoolLen := len(bc.Mempool)
	bc.Lock.RUnlock()

	if mempoolLen != 2 {
		t.Fatalf("[N-17] Expected 2 transactions in mempool after orphan promotion, got %d", mempoolLen)
	}
	if ps.HasOrphanTx(tx2.HashHex()) {
		t.Fatalf("[N-17] tx2 should be removed from orphan pool once promoted to mempool")
	}
}

// TestOrphanBlockArmaxHashFailureAndSentinelRejection verifies that AddOrphanBlock
// rejects blocks with INVALID_HASH_ERROR sentinels, blocks failing ArmaxHash computation,
// and blocks attempting SHA-256d fallback when ArmaxHash is active.
func TestOrphanBlockArmaxHashFailureAndSentinelRejection(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()
	ps := NewPeerServer(getFreePort(t), bc)

	peerIP := "198.51.100.22"

	// Enable ArmaxHash at height 10
	core.SetArmaxHashActivationHeight(10)
	defer core.SetArmaxHashActivationHeight(-1)

	target := core.DifficultyToTarget(1)

	// 1. Sentinel hash rejection
	bSentinel := *core.NewBlock([]core.Transaction{}, "00000000prev", 100, core.GenesisMiner, target)
	bSentinel.Hash = "INVALID_HASH_ERROR:header too large"

	stored := ps.AddOrphanBlock(bSentinel, peerIP)
	if stored {
		t.Fatal("AddOrphanBlock accepted block with INVALID_HASH_ERROR sentinel!")
	}
	if ps.GetBanScore(peerIP) == 0 {
		t.Fatal("Peer was not penalized for sending sentinel block hash")
	}

	// 2. SHA-256d fallback attempt when ArmaxHash is active
	bShaFallback := *core.NewBlock([]core.Transaction{}, "00000000prev", 100, core.GenesisMiner, target)
	bShaFallback.Hash = bShaFallback.CalculateSha256dHash()

	storedSha := ps.AddOrphanBlock(bShaFallback, peerIP)
	if storedSha {
		t.Fatal("AddOrphanBlock accepted SHA-256d hash when ArmaxHash is active!")
	}
}
