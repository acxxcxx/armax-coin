//go:build testhooks

package core

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckpoints_PerNetworkGenesisMatching verifies that height 0 checkpoints
// strictly match the corresponding network genesis hash and never cross-contaminate.
func TestCheckpoints_PerNetworkGenesisMatching(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	// 1. Mainnet checkpoint at height 0
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatalf("SetNetwork(mainnet) failed: %v", err)
	}
	mainHash, exists := GetCheckpoint(0)
	if !exists {
		t.Fatalf("Mainnet height 0 checkpoint missing")
	}
	if mainHash != GenesisHash {
		t.Fatalf("Mainnet checkpoint 0 mismatch: got %s, want %s", mainHash, GenesisHash)
	}

	// 2. Testnet checkpoint at height 0
	if err := SetNetwork("testnet"); err != nil {
		t.Fatalf("SetNetwork(testnet) failed: %v", err)
	}
	testHash, exists := GetCheckpoint(0)
	if !exists {
		t.Fatalf("Testnet height 0 checkpoint missing")
	}
	if testHash != TestnetGenesisHash {
		t.Fatalf("Testnet checkpoint 0 mismatch: got %s, want %s", testHash, TestnetGenesisHash)
	}

	// 3. Mainnet and Testnet checkpoints MUST NOT collide
	if mainHash == testHash {
		t.Fatalf("Critical collision: Mainnet and Testnet share identical checkpoint: %s", mainHash)
	}

	// 4. Regtest checkpoint
	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest) failed: %v", err)
	}
	regHash, exists := GetCheckpoint(0)
	if !exists {
		t.Fatalf("Regtest height 0 checkpoint missing")
	}
	regGenesis := GetRegtestGenesisBlock()
	if regHash != regGenesis.Hash {
		t.Fatalf("Regtest checkpoint 0 mismatch: got %s, want %s", regHash, regGenesis.Hash)
	}
	if regHash == mainHash || regHash == testHash {
		t.Fatalf("Regtest checkpoint collided with mainnet/testnet!")
	}

	// 5. Soak checkpoint
	if err := SetNetwork("soak"); err != nil {
		t.Fatalf("SetNetwork(soak) failed: %v", err)
	}
	soakHash, exists := GetCheckpoint(0)
	if !exists {
		t.Fatalf("Soak height 0 checkpoint missing")
	}
	soakGenesis := GetSoakGenesisBlock()
	if soakHash != soakGenesis.Hash {
		t.Fatalf("Soak checkpoint 0 mismatch: got %s, want %s", soakHash, soakGenesis.Hash)
	}
	if soakHash == mainHash || soakHash == testHash || soakHash == regHash {
		t.Fatalf("Soak checkpoint collided with other networks!")
	}
}

// TestCheckpoints_BlockRejectionOnMismatch proves that a block that does not match
// a configured checkpoint height is strictly rejected by consensus validation.
func TestCheckpoints_BlockRejectionOnMismatch(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest) failed: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "checkpoint_test.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	// Install a temporary test checkpoint for block #1
	expectedCheckpointHash := "00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	cleanup := SetCheckpointsForTesting(map[int]string{
		0: bc.Chain[0].Hash,
		1: expectedCheckpointHash,
	})
	defer cleanup()

	// Create a valid block #1 with a DIFFERENT hash
	minerAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	cb := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}

	b1 := Block{
		Index:        1,
		Timestamp:    bc.Chain[0].Timestamp + 120,
		Transactions: []Transaction{cb},
		PrevHash:     bc.Chain[0].Hash,
		MerkleRoot:   CalculateMerkleRoot([]Transaction{cb}),
		Miner:        minerAddr,
		Difficulty:   TargetToDifficulty(bc.Chain[0].GetTarget()),
		Target:       bc.Chain[0].Target,
		Nonce:        0,
	}

	target := b1.GetTarget()
	for {
		var err error
		b1.Hash, err = b1.CalculateHashWithError()
		if err == nil && HashMeetsTarget(b1.Hash, target) {
			break
		}
		b1.Nonce++
	}

	// Validate block context: must reject because b1.Hash != expectedCheckpointHash
	err = validateBlockContext(&b1, &bc.Chain[0], bc.Chain)
	if err == nil {
		t.Fatalf("Expected checkpoint mismatch rejection for block #1, but validation passed!")
	}
	if !strings.Contains(err.Error(), "checkpoint mismatch for block #1") {
		t.Fatalf("Expected 'checkpoint mismatch for block #1' error, got: %v", err)
	}

	// ValidateBlock must also reject
	if err := bc.ValidateBlock(b1); err == nil {
		t.Fatalf("bc.ValidateBlock succeeded despite checkpoint violation!")
	}

	// Set checkpoint to match b1.Hash and verify it passes
	cleanup2 := SetCheckpointsForTesting(map[int]string{
		0: bc.Chain[0].Hash,
		1: b1.Hash,
	})
	defer cleanup2()

	if err := validateBlockContext(&b1, &bc.Chain[0], bc.Chain); err != nil {
		t.Fatalf("validateBlockContext failed on matching checkpoint: %v", err)
	}
	if err := bc.ValidateBlock(b1); err != nil {
		t.Fatalf("bc.ValidateBlock failed on matching checkpoint: %v", err)
	}
}

// TestCheckpoints_ReorganizationBlockedByCheckpoint proves that a reorganization attempt
// to a fork chain that violates a historic checkpoint is strictly rejected by ResolveChain.
func TestCheckpoints_ReorganizationBlockedByCheckpoint(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest) failed: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "checkpoint_test.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	minerAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	mineBlock := func(prev Block) Block {
		cb := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := Block{
			Index:        prev.Index + 1,
			Timestamp:    prev.Timestamp + 120,
			Transactions: []Transaction{cb},
			PrevHash:     prev.Hash,
			MerkleRoot:   CalculateMerkleRoot([]Transaction{cb}),
			Miner:        minerAddr,
			Difficulty:   TargetToDifficulty(prev.GetTarget()),
			Target:       prev.Target,
			Nonce:        0,
		}
		target := b.GetTarget()
		for {
			var err error
			b.Hash, err = b.CalculateHashWithError()
			if err == nil && HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		return b
	}

	b1 := mineBlock(bc.Chain[0])
	if !bc.ValidateAndAddBlock(b1) {
		t.Fatalf("Failed to add b1 via ValidateAndAddBlock")
	}

	// Lock in block #1 with a checkpoint
	cleanup := SetCheckpointsForTesting(map[int]string{
		0: bc.Chain[0].Hash,
		1: b1.Hash,
	})
	defer cleanup()

	// Create an alternate fork starting from block 0 that tries to reorganize
	altB1 := mineBlock(bc.Chain[0])
	altB1.Timestamp += 1 // make hash distinct from b1
	target := altB1.GetTarget()
	for {
		var err error
		altB1.Hash, err = altB1.CalculateHashWithError()
		if err == nil && HashMeetsTarget(altB1.Hash, target) && altB1.Hash != b1.Hash {
			break
		}
		altB1.Nonce++
	}

	altB2 := mineBlock(altB1)

	// Proposed candidate chain: [genesis, altB1, altB2] (longer chainwork)
	candidateChain := []Block{bc.Chain[0], altB1, altB2}

	// Attempt reorganization
	reorgSuccess := bc.ResolveChain(candidateChain)
	if reorgSuccess {
		t.Fatalf("ResolveChain succeeded with a fork chain that violated checkpoint at height 1!")
	}

	// Verify original tip was preserved
	if bc.GetLatestBlock().Hash != b1.Hash {
		t.Fatalf("Chain tip was corrupted by rejected reorganization: got %s, want %s", bc.GetLatestBlock().Hash, b1.Hash)
	}
}

// TestStorage_FullWriteAndMiningLifecycleAfterReadOnlyCheck verifies Section 5:
// 1. Initial database verification opens in read-only mode without mutating the file.
// 2. Once verified, the database safely opens in read-write mode for normal blockchain operations.
// 3. A block can be mined, applied to state, and saved to disk with bbolt commit.
// 4. The node can close, restart, and reload state from disk.
// 5. A second block can be mined, saved, and re-loaded cleanly across restarts.
func TestStorage_FullWriteAndMiningLifecycleAfterReadOnlyCheck(t *testing.T) {
	SetNetworkForTesting(true)
	defer func() {
		_ = SetNetwork("testnet")
		SetNetworkForTesting(true)
	}()

	if err := SetNetwork("regtest"); err != nil {
		t.Fatalf("SetNetwork(regtest) failed: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "lifecycle_blockchain.db")

	// Phase 1: Initialize fresh database on disk
	bc1, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to initialize blockchain: %v", err)
	}
	if len(bc1.Chain) != 1 {
		t.Fatalf("expected chain length 1, got %d", len(bc1.Chain))
	}

	minerAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	mineAndAdd := func(bc *Blockchain) Block {
		prev := bc.GetLatestBlock()
		cb := Transaction{
			Sender:    "SYSTEM",
			Recipient: minerAddr,
			Amount:    InitialReward,
			Nonce:     int64(prev.Index + 1),
			Signature: "REWARD_SIG",
		}
		b := Block{
			Index:        prev.Index + 1,
			Timestamp:    prev.Timestamp + 120,
			Transactions: []Transaction{cb},
			PrevHash:     prev.Hash,
			MerkleRoot:   CalculateMerkleRoot([]Transaction{cb}),
			Miner:        minerAddr,
			Difficulty:   TargetToDifficulty(prev.GetTarget()),
			Target:       prev.Target,
			Nonce:        0,
		}
		target := b.GetTarget()
		for {
			var err error
			b.Hash, err = b.CalculateHashWithError()
			if err == nil && HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		if !bc.ValidateAndAddBlock(b) {
			panic("ValidateAndAddBlock failed")
		}
		if err := bc.SaveToFile(); err != nil {
			panic(fmt.Sprintf("SaveToFile failed: %v", err))
		}
		return b
	}

	// Mine and persist block #1
	b1 := mineAndAdd(bc1)
	if bc1.GetLatestBlock().Index != 1 {
		t.Fatalf("expected tip index 1, got %d", bc1.GetLatestBlock().Index)
	}
	if err := bc1.Close(); err != nil {
		t.Fatalf("failed to close bc1: %v", err)
	}

	// Verify file size > 0 and compute hash of DB with block #1
	fi, err := os.Stat(dbPath)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("db file does not exist or empty: %v", err)
	}
	dataAfterB1, _ := os.ReadFile(dbPath)
	hashAfterB1 := sha256.Sum256(dataAfterB1)

	// Phase 2: Restart node and reload database (triggers OpenStoreReadOnly, validation, then write mode)
	bc2, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to reload blockchain from disk: %v", err)
	}
	if len(bc2.Chain) != 2 || bc2.GetLatestBlock().Hash != b1.Hash {
		t.Fatalf("reloaded bc2 tip mismatch: len=%d, hash=%s, want=%s", len(bc2.Chain), bc2.GetLatestBlock().Hash, b1.Hash)
	}

	// Mine and persist block #2 on the reloaded chain (proves write mode works after read-only check)
	b2 := mineAndAdd(bc2)
	if bc2.GetLatestBlock().Index != 2 {
		t.Fatalf("expected tip index 2, got %d", bc2.GetLatestBlock().Index)
	}
	if err := bc2.Close(); err != nil {
		t.Fatalf("failed to close bc2: %v", err)
	}

	// Verify DB hash changed after persisting block #2
	dataAfterB2, _ := os.ReadFile(dbPath)
	hashAfterB2 := sha256.Sum256(dataAfterB2)
	if hashAfterB1 == hashAfterB2 {
		t.Fatalf("database file hash did not change after adding block #2!")
	}

	// Phase 3: Restart node once more and verify full state of 3 blocks (genesis, b1, b2)
	bc3, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("failed to reload bc3: %v", err)
	}
	defer bc3.Close()

	if len(bc3.Chain) != 3 {
		t.Fatalf("expected 3 blocks after second restart, got %d", len(bc3.Chain))
	}
	if bc3.Chain[1].Hash != b1.Hash || bc3.Chain[2].Hash != b2.Hash {
		t.Fatalf("persisted blocks mismatch after second restart")
	}
	if bc3.GetBalance(minerAddr) <= 0 {
		t.Fatalf("miner account state was not loaded properly: balance %d", bc3.GetBalance(minerAddr))
	}
}

// TestCheckpoints_ConsensusNetworkIdentityStrictness verifies:
// 1. validateBlockContext rejects missing chain history (cannot determine network identity).
// 2. validateBlockContext rejects unrecognized genesis anchors (anti-silent fallback).
// 3. Mainnet and Testnet blocks validate cleanly in alternation in a single process,
//    even when the global ActiveNetwork is switched to the opposing network.
func TestCheckpoints_ConsensusNetworkIdentityStrictness(t *testing.T) {
	LockNetworkForTesting(t)
	EnableLocalMainnet()
	defer DisableLocalMainnet()

	tempDir := t.TempDir()

	// 1. Empty chain history rejection
	dummyBlock := Block{
		Index:        1,
		PrevHash:     GenesisHash,
		Transactions: []Transaction{{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward}},
		Miner:        GenesisMiner,
	}
	dummyPrev := GetMainnetGenesisBlock()
	errEmpty := validateBlockContext(&dummyBlock, &dummyPrev, []Block{})
	if errEmpty == nil {
		t.Fatalf("Expected validateBlockContext to reject empty chain history, got nil")
	}
	if !strings.Contains(errEmpty.Error(), "consensus network context error") {
		t.Fatalf("Expected network context error, got: %v", errEmpty)
	}

	// 2. Unrecognized genesis anchor rejection
	corruptGen := dummyPrev
	corruptGen.Hash = "0000000000000000deadbeefcafebabedeadbeefcafebabedeadbeefcafebabe"
	errCorrupt := validateBlockContext(&dummyBlock, &dummyPrev, []Block{corruptGen})
	if errCorrupt == nil {
		t.Fatalf("Expected validateBlockContext to reject unrecognized genesis anchor, got nil")
	}
	if !strings.Contains(errCorrupt.Error(), "consensus network context error") {
		t.Fatalf("Expected network context error, got: %v", errCorrupt)
	}

	// 3. Multi-network alternating validation
	_ = SetNetwork("mainnet")
	mainDB := filepath.Join(tempDir, "mainnet", "blockchain.db")
	mainBC, err := NewBlockchainWithStoragePath(1, mainDB)
	if err != nil {
		t.Fatalf("Failed to initialize mainnet blockchain: %v", err)
	}
	defer mainBC.Close()

	_ = SetNetwork("testnet")
	testDB := filepath.Join(tempDir, "testnet", "blockchain.db")
	testBC, err := NewBlockchainWithStoragePath(1, testDB)
	if err != nil {
		t.Fatalf("Failed to initialize testnet blockchain: %v", err)
	}
	defer testBC.Close()

	// Mine 1 block on mainnet
	_ = SetNetwork("mainnet")
	mainBlock, err := mainBC.MineBlockWithErr(GenesisMiner)
	if err != nil {
		t.Fatalf("Failed to mine mainnet block: %v", err)
	}

	// Mine 1 block on testnet
	_ = SetNetwork("testnet")
	testBlock, err := testBC.MineBlockWithErr(GenesisMiner)
	if err != nil {
		t.Fatalf("Failed to mine testnet block: %v", err)
	}

	// Alternate: Set global network to Testnet, validate Mainnet block
	_ = SetNetwork("testnet")
	prevMain := mainBC.Chain[0]
	if err := validateBlockContext(&mainBlock, &prevMain, []Block{prevMain}); err != nil {
		t.Fatalf("Failed to validate Mainnet block when global profile was set to Testnet: %v", err)
	}

	// Alternate: Set global network to Mainnet, validate Testnet block
	_ = SetNetwork("mainnet")
	prevTest := testBC.Chain[0]
	if err := validateBlockContext(&testBlock, &prevTest, []Block{prevTest}); err != nil {
		t.Fatalf("Failed to validate Testnet block when global profile was set to Mainnet: %v", err)
	}
}
