package core

import (
	"encoding/json"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// TestC23_ResolveChainRejectsModifiedGenesisPayload tests that ResolveChain rejects
// candidate chains where block 0 has modified payload (e.g. modified reward or recipient)
func TestC23_ResolveChainRejectsModifiedGenesisPayload(t *testing.T) {
	bc, _, _, minerAddr := setupTestChainWithFunds(t)

	// Build a valid candidate fork chain with 2 blocks mined on top of genesis
	candidateChain := []Block{bc.Chain[0]}
	baseTime := candidateChain[0].Timestamp

	for i := 1; i <= 3; i++ {
		prev := candidateChain[len(candidateChain)-1]
		target := CalculateExpectedTarget(candidateChain, prev.Index+1)
		cbTx := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := *NewBlock([]Transaction{cbTx}, prev.Hash, prev.Index+1, minerAddr, target)
		b.Timestamp = baseTime + int64(i*10)
		for {
			b.Hash = b.CalculateHash()
			if HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		candidateChain = append(candidateChain, b)
	}

	// Case 1: Tamper block 0 coinbase reward amount while leaving Hash forged to GenesisHash
	tamperedCandidate := make([]Block, len(candidateChain))
	copy(tamperedCandidate, candidateChain)

	tamperedGenesis := GetGenesisBlock()
	tamperedGenesis.Transactions[0].Amount = InitialReward * 10 // Malicious 10x genesis mint
	// Attacker leaves Hash as GenesisHash
	tamperedGenesis.Hash = GenesisHash
	tamperedCandidate[0] = tamperedGenesis

	if bc.ResolveChain(tamperedCandidate) {
		t.Fatalf("[C-23] FAILED: ResolveChain accepted candidate chain with tampered genesis transaction amount!")
	}

	// Case 2: Tamper block 0 recipient
	tamperedCandidate2 := make([]Block, len(candidateChain))
	copy(tamperedCandidate2, candidateChain)
	tamperedGenesis2 := GetGenesisBlock()
	tamperedGenesis2.Transactions[0].Recipient = minerAddr
	tamperedGenesis2.Hash = GenesisHash
	tamperedCandidate2[0] = tamperedGenesis2

	if bc.ResolveChain(tamperedCandidate2) {
		t.Fatalf("[C-23] FAILED: ResolveChain accepted candidate chain with tampered genesis recipient!")
	}
}

// TestC23_LoadFromFileRejectsTamperedGenesisDB tests that LoadFromFile rejects database files
// where block 0 has tampered transaction payload
func TestC23_LoadFromFileRejectsTamperedGenesisDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "blockchain.db")
	SetDBPath(dbPath)
	defer SetDBPath("")

	bc, _, _, minerAddr := setupTestChainWithFunds(t)
	bc.StoragePath = dbPath
	if err := bc.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
	_ = bc.Close()

	// Tamper genesis block 0 payload in bbolt store directly while leaving Hash & MerkleRoot forged
	tamperedGen := bc.Chain[0]
	tamperedGen.Transactions[0].Recipient = minerAddr
	tamperedGen.Hash = GenesisHash
	tamperedGen.MerkleRoot = GenesisMerkleRoot
	tBytes, err := json.Marshal(&tamperedGen)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	db, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bktBlocks).Put(uint64ToBytes(0), tBytes)
	})
	_ = db.Close()
	if err != nil {
		t.Fatalf("Failed to write tampered genesis block: %v", err)
	}

	loadedBc, ok := LoadFromFile()
	if ok && loadedBc != nil {
		_ = loadedBc.Close()
		t.Fatalf("[C-23] FAILED: LoadFromFile loaded a database with tampered genesis block payload!")
	}
}
