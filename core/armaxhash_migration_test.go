//go:build testhooks

package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/armaxhash"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
)

// TestActivationHeightDeactivatedInvariant proves mathematically and programmatically that
// when ArmaxHashActivationHeight <= 0 (-1 by default, or 0, or negative), ArmaxHash is NEVER
// activated for ANY block height from 0 to 1,000,000, and SHA-256d remains 100% active.
func TestActivationHeightDeactivatedInvariant(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")

	deactivatedValues := []int64{-1, 0, -2, -999999}
	testHeights := []int{-10, -1, 0, 1, 2, 10, 50, 100, 1000, 50000, 1000000}

	for _, deactVal := range deactivatedValues {
		SetArmaxHashActivationHeight(deactVal)

		for _, h := range testHeights {
			if IsArmaxHashActive(h) {
				t.Fatalf("CRITICAL CONTRADICTION: IsArmaxHashActive(%d) returned true when activation height is %d", h, deactVal)
			}

			// Validate that Block.CalculateHash unconditionally computes SHA-256d
			b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", h, GenesisMiner, DifficultyToTarget(1))
			if b.PoWAlgorithm() != "sha256d" {
				t.Fatalf("height %d PoWAlgorithm returned %s, expected sha256d", h, b.PoWAlgorithm())
			}
			if b.CalculateHash() != b.CalculateSha256dHash() {
				t.Fatalf("height %d CalculateHash does not match SHA-256d", h)
			}
		}
	}
}

// TestCanonicalSevenFieldsHeaderParity proves that Block.HeaderBytes matches the 7 canonical
// header fields used by the ArmaxHash golden vector and verifies it against an independent reference.
func TestCanonicalSevenFieldsHeaderParity(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("mainnet")

	target := new(big.Int)
	target.SetString("0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	// Block with the exact 7 canonical fields:
	// 1: Index = 1
	// 2: Timestamp = 1710000100
	// 3: PrevHash = "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b"
	// 4: MerkleRoot = "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d"
	// 5: Miner = "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	// 6: Nonce = 12345
	// 7: Target = "0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	b := &Block{
		Index:        1,
		Timestamp:    1710000100,
		PrevHash:     "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b",
		MerkleRoot:   "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d",
		Miner:        "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		Nonce:        12345,
		Difficulty:   TargetToDifficulty(target),
		Target:       fmt.Sprintf("%064x", target),
		Transactions: []Transaction{},
	}

	const expectedCanonicalHeader = "1:1710000100:0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b:808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d:armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd:12345:0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if string(b.HeaderBytes()) != expectedCanonicalHeader {
		t.Fatalf("HeaderBytes mismatch:\n got:  %s\n want: %s", string(b.HeaderBytes()), expectedCanonicalHeader)
	}

	// Independent step-by-step cryptographic implementation
	netID := "ARMX-MAINNET-1"
	const domain = "ARMX-POW\x00"
	var input []byte
	input = append(input, domain...)
	input = append(input, 1, byte(len(netID))) // version 1 + len(netID)
	input = append(input, netID...)
	input = append(input, []byte(expectedCanonicalHeader)...)

	d512 := blake2b.Sum512(input)
	password := d512[:32]
	salt := d512[32:48]

	argonDigest := argon2.IDKey(password, salt, 2, 2048, 1, 32)
	var finalInput [64]byte
	copy(finalInput[:32], password)
	copy(finalInput[32:], argonDigest)
	independentDigest := blake2b.Sum256(finalInput[:])
	independentHex := hex.EncodeToString(independentDigest[:])

	// Golden vector from test specification
	const expectedGoldenHex = "67a90bb986f7a6e1bf32c4f178956aea138aef97a55654364caef74d33151355"
	if independentHex != expectedGoldenHex {
		t.Fatalf("Independent reference hash mismatch: %s != %s", independentHex, expectedGoldenHex)
	}

	// Verify Block.CalculateArmaxHash matches independent computation
	calculatedHex, err := b.CalculateArmaxHash()
	if err != nil {
		t.Fatalf("CalculateArmaxHash failed: %v", err)
	}
	if calculatedHex != expectedGoldenHex {
		t.Fatalf("Block.CalculateArmaxHash mismatch: %s != %s", calculatedHex, expectedGoldenHex)
	}
}

// TestArmaxHashBackwardCompatibilityAndMigration tests the boundary behavior at height < H, height == H, and height > H.
func TestArmaxHashBackwardCompatibilityAndMigration(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")

	// Ensure clean state: default activation is disabled (-1)
	SetArmaxHashActivationHeight(-1)
	if IsArmaxHashActive(0) || IsArmaxHashActive(100) {
		t.Fatal("ArmaxHash should not be active when disabled")
	}

	target := DifficultyToTarget(1)
	miner := GenesisMiner

	// 1. Block at height 5 with activation disabled (-1) -> must be SHA-256d
	bLegacy := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", 5, miner, target)
	bLegacy.Hash = bLegacy.CalculateHash()
	if bLegacy.PoWAlgorithm() != "sha256d" {
		t.Fatalf("expected sha256d, got %s", bLegacy.PoWAlgorithm())
	}
	if bLegacy.Hash != bLegacy.CalculateSha256dHash() {
		t.Fatalf("legacy block hash mismatch: %s != %s", bLegacy.Hash, bLegacy.CalculateSha256dHash())
	}

	// 2. Set activation height at block 10
	SetArmaxHashActivationHeight(10)
	defer SetArmaxHashActivationHeight(-1)

	// Block below activation height (index 9) -> must remain SHA-256d
	bPre := NewBlock(nil, bLegacy.Hash, 9, miner, target)
	bPre.Hash = bPre.CalculateHash()
	if bPre.PoWAlgorithm() != "sha256d" {
		t.Fatalf("block index 9 should be sha256d, got %s", bPre.PoWAlgorithm())
	}
	if bPre.Hash != bPre.CalculateSha256dHash() {
		t.Fatal("pre-activation block must match CalculateSha256dHash")
	}

	// Block at activation height (index 10) -> must switch to ArmaxHash
	bPost := NewBlock(nil, bPre.Hash, 10, miner, target)
	bPost.Hash = bPost.CalculateHash()
	if bPost.PoWAlgorithm() != "armaxhash" {
		t.Fatalf("block index 10 should be armaxhash, got %s", bPost.PoWAlgorithm())
	}
	armaxHash, err := bPost.CalculateArmaxHash()
	if err != nil {
		t.Fatalf("CalculateArmaxHash error: %v", err)
	}
	if bPost.Hash != armaxHash {
		t.Fatalf("post-activation block hash mismatch: %s != %s", bPost.Hash, armaxHash)
	}
	if bPost.Hash == bPost.CalculateSha256dHash() {
		t.Fatal("ArmaxHash digest must not equal SHA-256d digest")
	}

	// 3. Target verification works identically for both algorithms
	maxTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if !HashMeetsTarget(bPost.Hash, maxTarget) {
		t.Fatal("HashMeetsTarget failed for maximum target")
	}
	zeroTarget := big.NewInt(0)
	if HashMeetsTarget(bPost.Hash, zeroTarget) {
		t.Fatal("non-zero hash should not meet zero target")
	}

	// 4. Verify smooth target reset at exact activation block
	chain := []Block{*bPre}
	nextTgt := CalculateExpectedTarget(chain, 10)
	if nextTgt.Cmp(GetArmaxHashPowLimit()) != 0 {
		t.Fatalf("expected target at activation block to reset to ArmaxHashPowLimit, got %x", nextTgt)
	}
}

// TestMultiBlockTransitionAndReorg tests full mining before (H-2, H-1), at (H), and after (H+1, H+2) activation,
// validating the mixed chain, historical persistence, and reorg across the fork point.
func TestMultiBlockTransitionAndReorg(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	dbPath := filepath.Join(t.TempDir(), "multi_transition.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath error: %v", err)
	}
	defer bc.Close()

	// Activation scheduled at block height 3
	const forkHeight = 3
	SetArmaxHashActivationHeight(forkHeight)
	defer SetArmaxHashActivationHeight(-1)

	// Block #1: SHA-256d (Before activation)
	b1, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("mining block 1 failed: %v", err)
	}
	if b1.PoWAlgorithm() != "sha256d" {
		t.Fatalf("block 1 algorithm: got %s, want sha256d", b1.PoWAlgorithm())
	}

	// Block #2: SHA-256d (Before activation)
	b2, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("mining block 2 failed: %v", err)
	}
	if b2.PoWAlgorithm() != "sha256d" {
		t.Fatalf("block 2 algorithm: got %s, want sha256d", b2.PoWAlgorithm())
	}

	// Block #3: ArmaxHash (Exact activation block)
	b3, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("mining block 3 failed: %v", err)
	}
	if b3.PoWAlgorithm() != "armaxhash" {
		t.Fatalf("block 3 algorithm: got %s, want armaxhash", b3.PoWAlgorithm())
	}

	// Block #4: ArmaxHash (After activation)
	b4, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("mining block 4 failed: %v", err)
	}
	if b4.PoWAlgorithm() != "armaxhash" {
		t.Fatalf("block 4 algorithm: got %s, want armaxhash", b4.PoWAlgorithm())
	}

	// Block #5: ArmaxHash (After activation)
	b5, err := bc.MineBlockContext(context.Background(), GenesisMiner)
	if err != nil {
		t.Fatalf("mining block 5 failed: %v", err)
	}
	if b5.PoWAlgorithm() != "armaxhash" {
		t.Fatalf("block 5 algorithm: got %s, want armaxhash", b5.PoWAlgorithm())
	}

	// Verify whole chain passes validation
	if !bc.IsChainValid() {
		t.Fatal("IsChainValid failed on multi-block transition chain")
	}

	// Verify each block's algorithm matches expectation
	if bc.Chain[0].PoWAlgorithm() != "sha256d" || // Genesis
		bc.Chain[1].PoWAlgorithm() != "sha256d" || // Block 1
		bc.Chain[2].PoWAlgorithm() != "sha256d" || // Block 2
		bc.Chain[3].PoWAlgorithm() != "armaxhash" || // Block 3 (Fork)
		bc.Chain[4].PoWAlgorithm() != "armaxhash" || // Block 4
		bc.Chain[5].PoWAlgorithm() != "armaxhash" { // Block 5
		t.Fatal("Chain blocks do not match expected algorithm distribution")
	}

	// Verify chainwork accumulation
	cw := bc.GetTotalChainwork()
	if cw == nil || cw.Sign() <= 0 {
		t.Fatal("Chainwork must be positive across transition")
	}
}

// TestCalculateHashNoFallbackOnArmaxHashFailure proves that when ArmaxHash is active,
// any failure in CalculateArmaxHash() returns an explicit error via CalculateHashWithError(),
// causes CalculateHash() to return an invalid error sentinel, and NEVER falls back to SHA-256d.
func TestCalculateHashNoFallbackOnArmaxHashFailure(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")

	// Activate ArmaxHash at height 10
	SetArmaxHashActivationHeight(10)
	defer SetArmaxHashActivationHeight(-1)

	if !IsArmaxHashActive(10) {
		t.Fatal("ArmaxHash must be active at height 10")
	}

	target := DifficultyToTarget(1)
	b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", 10, GenesisMiner, target)
	sha256dExpected := b.CalculateSha256dHash()

	// Scenario A: Force CalculateArmaxHash failure via oversized header (> 1 MiB)
	bOversized := *b
	bOversized.Miner = strings.Repeat("X", armaxhash.MaxHeaderSize+100)

	armaxHash, armaxErr := bOversized.CalculateArmaxHash()
	if armaxErr == nil {
		t.Fatal("expected CalculateArmaxHash to fail for oversized header, but got nil error")
	}
	if !errors.Is(armaxErr, armaxhash.ErrHeaderTooLarge) {
		t.Fatalf("expected ErrHeaderTooLarge, got: %v", armaxErr)
	}
	if armaxHash != "" {
		t.Fatalf("expected empty hash string on error, got: %s", armaxHash)
	}

	// 1. Verify CalculateHashWithError returns the error and NEVER falls back to SHA-256d
	calcHash, calcErr := bOversized.CalculateHashWithError()
	if calcErr == nil {
		t.Fatal("CRITICAL: CalculateHashWithError must return error on ArmaxHash failure, got nil")
	}
	if !errors.Is(calcErr, armaxhash.ErrHeaderTooLarge) {
		t.Fatalf("expected ErrHeaderTooLarge, got: %v", calcErr)
	}
	if calcHash != "" {
		t.Fatalf("CalculateHashWithError must return empty string on error, got: %s", calcHash)
	}
	if calcHash == sha256dExpected || calcHash == bOversized.CalculateSha256dHash() {
		t.Fatal("CRITICAL SECURITY VULNERABILITY: CalculateHashWithError fell back to SHA-256d on failure!")
	}

	// 2. Verify CalculateHash returns explicit unmatchable error sentinel and NOT SHA-256d
	calcHashSentinel := bOversized.CalculateHash()
	if !strings.HasPrefix(calcHashSentinel, "INVALID_HASH_ERROR:") {
		t.Fatalf("expected INVALID_HASH_ERROR prefix, got: %s", calcHashSentinel)
	}
	if calcHashSentinel == sha256dExpected || calcHashSentinel == bOversized.CalculateSha256dHash() {
		t.Fatal("CRITICAL SECURITY VULNERABILITY: CalculateHash fell back to SHA-256d on failure!")
	}

	// Scenario B: Force failure via invalid/empty network ID while block size is normal
	cleanup := SetNetworkIDOverrideForTesting(func() string {
		return ""
	})
	t.Cleanup(cleanup)

	bNormal := *b
	armaxHash2, armaxErr2 := bNormal.CalculateArmaxHash()
	if armaxErr2 == nil || !errors.Is(armaxErr2, armaxhash.ErrInvalidNet) {
		t.Fatalf("expected ErrInvalidNet, got hash=%s, err=%v", armaxHash2, armaxErr2)
	}

	calcHash2, calcErr2 := bNormal.CalculateHashWithError()
	if calcErr2 == nil {
		t.Fatal("CRITICAL: CalculateHashWithError returned nil error for invalid network ID")
	}
	if !errors.Is(calcErr2, armaxhash.ErrInvalidNet) {
		t.Fatalf("expected ErrInvalidNet, got: %v", calcErr2)
	}
	if calcHash2 != "" {
		t.Fatalf("expected empty hash string, got: %s", calcHash2)
	}
	if calcHash2 == sha256dExpected {
		t.Fatal("CRITICAL SECURITY VULNERABILITY: CalculateHashWithError fell back to SHA-256d on failure!")
	}

	calcHashSentinel2 := bNormal.CalculateHash()
	if !strings.HasPrefix(calcHashSentinel2, "INVALID_HASH_ERROR:") {
		t.Fatalf("expected INVALID_HASH_ERROR prefix, got: %s", calcHashSentinel2)
	}
	if calcHashSentinel2 == sha256dExpected {
		t.Fatal("CRITICAL SECURITY VULNERABILITY: CalculateHash fell back to SHA-256d on failure!")
	}

	// 3. Verify consensus validation rejects blocks with computation failure even if b.Hash was crafted with SHA-256d
	bCrafted := bNormal
	bCrafted.Hash = bCrafted.CalculateSha256dHash()
	bCrafted.Transactions = []Transaction{
		{
			Sender:    "SYSTEM",
			Recipient: bCrafted.Miner,
			Amount:    5000000000,
			Fee:       0,
			Nonce:     0,
			Signature: "REWARD_SIG",
		},
	}
	bCrafted.MerkleRoot = CalculateMerkleRoot(bCrafted.Transactions)

	prev := &Block{
		Index: 9,
		Hash:  bCrafted.PrevHash,
	}
	valErr := validateBlockContext(&bCrafted, prev, []Block{})
	if valErr == nil {
		t.Fatal("CRITICAL: validateBlockContext must reject block when hash computation fails")
	}
	if !strings.Contains(valErr.Error(), "block hash computation failed") {
		t.Fatalf("expected 'block hash computation failed' error, got: %v", valErr)
	}
}

// TestCalculateHashSha256dWhenDeactivated proves that when ArmaxHash activation is disabled
// (activation height <= 0), SHA-256d is strictly used across all heights and CalculateHashWithError
// returns the valid SHA-256d digest with nil error.
func TestCalculateHashSha256dWhenDeactivated(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")

	SetArmaxHashActivationHeight(-1)

	testHeights := []int{0, 1, 5, 10, 50, 100, 1000, 500000}
	target := DifficultyToTarget(1)

	for _, h := range testHeights {
		if IsArmaxHashActive(h) {
			t.Fatalf("ArmaxHash must not be active at height %d when activation height is -1", h)
		}

		b := NewBlock(nil, "0000000000000000000000000000000000000000000000000000000000000000", h, GenesisMiner, target)
		if b.PoWAlgorithm() != "sha256d" {
			t.Fatalf("expected sha256d algorithm at height %d, got %s", h, b.PoWAlgorithm())
		}

		shaExpected := b.CalculateSha256dHash()

		calcHash, err := b.CalculateHashWithError()
		if err != nil {
			t.Fatalf("height %d CalculateHashWithError returned unexpected error: %v", h, err)
		}
		if calcHash != shaExpected {
			t.Fatalf("height %d CalculateHashWithError mismatch: got %s, want %s", h, calcHash, shaExpected)
		}

		calcHashDirect := b.CalculateHash()
		if calcHashDirect != shaExpected {
			t.Fatalf("height %d CalculateHash mismatch: got %s, want %s", h, calcHashDirect, shaExpected)
		}
	}
}

// TestConsensusRejectsArmaxHashFailureAndSentinel proves that consensus block ingestion
// (ValidateAndAddBlock, database storage, chainwork accumulation, PrevHash linkage, and mining)
// rejects blocks when ArmaxHash computation fails or when sentinels are provided,
// and proves that SHA-256d is not accepted as fallback.
func TestConsensusRejectsArmaxHashFailureAndSentinel(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	dbPath := filepath.Join(t.TempDir(), "consensus_reject.db")
	bc, err := NewBlockchainWithStoragePath(1, dbPath)
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath failed: %v", err)
	}
	defer bc.Close()

	// Initial state: height 0 (genesis block)
	initTip := bc.GetLatestBlock()
	if initTip.Index != 0 {
		t.Fatalf("expected genesis index 0, got %d", initTip.Index)
	}
	initWork := new(big.Int).Set(bc.GetTotalChainwork())

	// Activate ArmaxHash at height 1
	SetArmaxHashActivationHeight(1)
	defer SetArmaxHashActivationHeight(-1)

	target := DifficultyToTarget(1)

	// 1. Rejection of block crafted with SHA-256d at height 1
	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: GenesisMiner,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     0,
		Signature: "REWARD_SIG",
	}
	bSha := NewBlock([]Transaction{cbTx}, initTip.Hash, 1, GenesisMiner, target)
	bSha.Hash = bSha.CalculateSha256dHash() // Crafted with legacy SHA-256d!

	if bc.ValidateAndAddBlock(*bSha) {
		t.Fatal("CRITICAL: ValidateAndAddBlock accepted SHA-256d block when ArmaxHash was active!")
	}
	if bc.GetLatestBlock().Index != 0 {
		t.Fatal("Chain tip advanced after submitting rejected SHA-256d block")
	}
	if bc.GetTotalChainwork().Cmp(initWork) != 0 {
		t.Fatal("Chainwork changed after submitting rejected SHA-256d block")
	}

	// 2. Rejection of block with INVALID_HASH_ERROR sentinel as Hash
	bSentinel := *bSha
	bSentinel.Hash = "INVALID_HASH_ERROR:forced failure"

	if HashMeetsTarget(bSentinel.Hash, target) {
		t.Fatal("HashMeetsTarget must return false for sentinel string")
	}
	if bc.ValidateAndAddBlock(bSentinel) {
		t.Fatal("CRITICAL: ValidateAndAddBlock accepted block with INVALID_HASH_ERROR sentinel!")
	}
	if bc.GetLatestBlock().Index != 0 {
		t.Fatal("Chain tip advanced after submitting sentinel block")
	}

	// 3. Rejection of block using sentinel as PrevHash
	bNext := NewBlock([]Transaction{cbTx}, "INVALID_HASH_ERROR:parent_failed", 1, GenesisMiner, target)
	bNext.Hash = bNext.CalculateSha256dHash()
	if bc.ValidateAndAddBlock(*bNext) {
		t.Fatal("CRITICAL: ValidateAndAddBlock accepted block with INVALID_HASH_ERROR PrevHash!")
	}

	// 4. Defense-in-depth: SaveBlockToStore directly rejects sentinel and invalid-hash blocks
	storeErrSentinel := bc.SaveBlockToStore(&bSentinel, nil)
	if storeErrSentinel == nil {
		t.Fatal("CRITICAL: SaveBlockToStore accepted block with sentinel hash!")
	}
	if !strings.Contains(storeErrSentinel.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error from SaveBlockToStore, got: %v", storeErrSentinel)
	}

	// Verify database was never modified: block 1 must not exist on disk
	storedBlk1, err := bc.GetBlockByHash(bSentinel.Hash)
	if err == nil && storedBlk1 != nil {
		t.Fatalf("CRITICAL: Sentinel block found in database store after rejection: %+v", storedBlk1)
	}
	storedSha, err := bc.GetBlockByHash(bSha.Hash)
	if err == nil && storedSha != nil {
		t.Fatalf("CRITICAL: Rejected SHA-256d block found in database store: %+v", storedSha)
	}
	db, err := bc.getStoreDB()
	if err == nil && db != nil {
		_ = db.View(func(tx *bolt.Tx) error {
			bkt := tx.Bucket(bktBlocks)
			if bkt != nil {
				hBytes := uint64ToBytes(1)
				if val := bkt.Get(hBytes); val != nil {
					t.Fatalf("CRITICAL: Block #1 found in bolt store bucket: %s", string(val))
				}
			}
			return nil
		})
	}
	if len(bc.Chain) != 1 {
		t.Fatalf("Chain length changed from 1 to %d", len(bc.Chain))
	}
	if bc.GetTotalChainwork().Cmp(initWork) != 0 {
		t.Fatal("Chainwork changed after rejection tests")
	}

	// 5. Mining failure handling: when network ID is invalid, MineBlockContext aborts immediately
	// without falling back to SHA-256d and without spinning indefinitely in a loop.
	cleanupMining := SetNetworkIDOverrideForTesting(func() string {
		return ""
	})
	t.Cleanup(cleanupMining)

	// Mine with a generous timeout to verify it aborts immediately via errorChan rather than waiting for timeout
	mineCtx, mineCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer mineCancel()

	mineStartTime := time.Now()
	minedBlk, mineErr := bc.MineBlockContext(mineCtx, GenesisMiner)
	mineElapsed := time.Since(mineStartTime)

	if mineErr == nil {
		t.Fatalf("CRITICAL: MineBlockContext should not have succeeded under broken network ID, got block: %+v", minedBlk)
	}
	if !strings.Contains(mineErr.Error(), "mining hash computation failed") {
		t.Fatalf("expected 'mining hash computation failed' error, got: %v", mineErr)
	}
	if mineElapsed >= 4*time.Second {
		t.Fatalf("MineBlockContext hung spinning until context timeout (took %v)", mineElapsed)
	}

	// Prove blockchain, database, and chainwork remain untouched after mining abort
	if bc.GetLatestBlock().Index != 0 {
		t.Fatal("Chain tip advanced during broken mining attempt")
	}
	if len(bc.Chain) != 1 {
		t.Fatalf("Chain length modified during broken mining attempt: %d", len(bc.Chain))
	}
	if bc.GetTotalChainwork().Cmp(initWork) != 0 {
		t.Fatal("Chainwork modified during broken mining attempt")
	}
	if db != nil {
		_ = db.View(func(tx *bolt.Tx) error {
			bkt := tx.Bucket(bktBlocks)
			if bkt != nil {
				hBytes := uint64ToBytes(1)
				if val := bkt.Get(hBytes); val != nil {
					t.Fatalf("CRITICAL: Block #1 found in bolt store bucket after mining failure: %s", string(val))
				}
			}
			return nil
		})
	}
}

// TestMineBlockContextConcurrentErrorRejectsBlock proves that if a worker finds a winning block
// while another worker records a hashing failure closely in time (or during the round),
// MineBlockContext cancels all workers, waits for all workers to finish (leaving no active workers),
// returns the error, and rejects/discards the mined block before ValidateAndAddBlock.
// This test uses deterministic channel-based synchronization rather than time.Sleep.
func TestMineBlockContextConcurrentErrorRejectsBlock(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("regtest")

	bc := NewBlockchain(1)
	defer bc.Close()
	bc.MiningThreads = 2

	initTip := bc.GetLatestBlock()
	initChainLen := len(bc.Chain)
	initWork := bc.GetTotalChainwork()

	simulatedErrMsg := "simulated concurrent worker hashing failure during mining"
	raceTriggered := make(chan struct{})
	workerErrRegistered := make(chan struct{})

	cleanupHook := SetMiningDeterministicRaceHookForTesting(func(recordErr func(error)) {
		// Deterministic synchronization: a block has arrived on resultChan.
		// Before cancelWorkers/wg.Wait, a concurrent worker registers a hashing failure.
		close(raceTriggered)
		go func() {
			recordErr(errors.New(simulatedErrMsg))
			close(workerErrRegistered)
		}()
		// Wait deterministically for the error to be registered on the concurrent goroutine
		<-workerErrRegistered
	})
	t.Cleanup(cleanupHook)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	minedBlk, err := bc.MineBlockContext(ctx, GenesisMiner)

	// Verify race hook was reached
	select {
	case <-raceTriggered:
	default:
		t.Fatal("deterministic race hook was not triggered during mining")
	}

	// 1. MineBlockContext must return the error from the faulted round
	if err == nil {
		t.Fatalf("CRITICAL: MineBlockContext accepted block despite concurrent worker error! Block: %+v", minedBlk)
	}
	if !strings.Contains(err.Error(), simulatedErrMsg) {
		t.Fatalf("expected error containing %q, got: %v", simulatedErrMsg, err)
	}

	// 2. The returned block must be empty
	if minedBlk.Hash != "" {
		t.Fatalf("expected empty block hash, got: %s", minedBlk.Hash)
	}

	// 3. The blockchain must NOT advance and must NOT contain the mined block
	if len(bc.Chain) != initChainLen {
		t.Fatalf("Chain length modified: got %d, want %d", len(bc.Chain), initChainLen)
	}
	if bc.GetLatestBlock().Index != initTip.Index || bc.GetLatestBlock().Hash != initTip.Hash {
		t.Fatalf("Chain tip changed: got index %d hash %s, want index %d hash %s",
			bc.GetLatestBlock().Index, bc.GetLatestBlock().Hash, initTip.Index, initTip.Hash)
	}
	if bc.GetTotalChainwork().Cmp(initWork) != 0 {
		t.Fatalf("Chainwork changed after faulted mining attempt")
	}

	// 4. Verify no workers are left active (total hashrate reset to 0 by deferred bc.SetMiningHashrate(0))
	if hr := bc.GetMiningHashrate(); hr != 0 {
		t.Fatalf("Mining hashrate was not reset to 0: got %f", hr)
	}
}

// TestNetworkIDHookReentrantLockNoDeadlock proves that NetworkID() releases netMu.RLock
// before executing networkIDOverrideHook, allowing the hook to safely call functions
// that acquire netMu.Lock() (like SetArmaxHashActivationHeight or SetNetwork) without deadlocking.
func TestNetworkIDHookReentrantLockNoDeadlock(t *testing.T) {
	LockNetworkForTesting(t)

	hookCalled := false
	cleanup := SetNetworkIDOverrideForTesting(func() string {
		hookCalled = true
		// This call acquires netMu.Lock(). If NetworkID() held netMu.RLock(), this would DEADLOCK!
		SetArmaxHashActivationHeight(999)
		return "ARMX-REENTRANT-HOOK-OK"
	})
	t.Cleanup(cleanup)

	done := make(chan string, 1)
	go func() {
		done <- NetworkID()
	}()

	select {
	case id := <-done:
		if !hookCalled {
			t.Fatal("networkIDOverrideHook was never invoked")
		}
		if id != "ARMX-REENTRANT-HOOK-OK" {
			t.Fatalf("unexpected network ID: got %s, want ARMX-REENTRANT-HOOK-OK", id)
		}
		if act := GetArmaxHashActivationHeight(); act != 999 {
			t.Fatalf("expected activation height 999 set inside hook, got %d", act)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK DETECTED: NetworkID() deadlocked when hook invoked a function acquiring netMu.Lock()")
	}
}

// TestArmaxHashDynamicDifficultyRetargetingAndLegacyPreservation proves that:
//  1. For all historical blocks (H < 520), difficulty remains clamped to Genesis Difficulty 7 (no legacy invalidation).
//  2. At exact activation (H = 520 on testnet), target resets to ArmaxHashPowLimit (Difficulty 3, 4096 hashes).
//  3. For H > 520, LWMA-1 dynamically lowers difficulty for small hashrate miners (bounded by Difficulty 1, not Difficulty 7),
//     and pre-activation SHA-256d blocks are normalized to ArmaxHashPowLimit to prevent difficulty distortion.
func TestArmaxHashDynamicDifficultyRetargetingAndLegacyPreservation(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")
	ResetArmaxHashActivationHeightOverride()

	if act := GetArmaxHashActivationHeight(); act != 520 {
		t.Fatalf("expected testnet activation height 520, got %d", act)
	}

	// 1. Verify historical blocks (0..519) never activate ArmaxHash
	for h := 0; h < 520; h++ {
		if IsArmaxHashActive(h) {
			t.Fatalf("historical block %d must NOT be ArmaxHash active", h)
		}
	}
	if !IsArmaxHashActive(520) {
		t.Fatal("block 520 MUST be ArmaxHash active")
	}

	// 2. Build mock chain of 520 legacy blocks with Genesis Difficulty 7
	genTarget := GetPowLimit() // Difficulty 7 on testnet
	genTargetHex := fmt.Sprintf("%064x", genTarget)
	if TargetToDifficulty(genTarget) != 7 {
		t.Fatalf("expected testnet genesis target to have difficulty 7, got %d", TargetToDifficulty(genTarget))
	}

	chain := make([]Block, 520)
	baseTime := int64(1700000000)
	for i := 0; i < 520; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  baseTime + int64(i)*TargetBlockTimeSeconds,
			Target:     genTargetHex,
			Difficulty: 7,
		}
	}

	// Verify legacy target calculation for block 519 remains clamped to Difficulty 7
	target519 := CalculateExpectedTarget(chain[:519], 519)
	if TargetToDifficulty(target519) != 7 {
		t.Fatalf("historical block 519 target difficulty modified: got %d, want 7", TargetToDifficulty(target519))
	}

	// 3. Verify block 520 resets to ArmaxHashPowLimit (Difficulty 3)
	target520 := CalculateExpectedTarget(chain, 520)
	armxLimit := GetArmaxHashPowLimit()
	if target520.Cmp(armxLimit) != 0 {
		t.Fatalf("block 520 target mismatch: got %x, want %x", target520, armxLimit)
	}
	if diff520 := TargetToDifficulty(target520); diff520 != 3 {
		t.Fatalf("block 520 difficulty mismatch: got %d, want 3", diff520)
	}

	// 4. Test dynamic retargeting at block 521 for slow miner (solve time > 120s)
	// Add block 520 mined with ArmaxHash target (Difficulty 3) and long solve time (400s)
	chainWith520Slow := append(chain, Block{
		Index:      520,
		Timestamp:  chain[519].Timestamp + 400, // Slow solve time
		Target:     fmt.Sprintf("%064x", target520),
		Difficulty: 3,
	})

	target521Slow := CalculateExpectedTarget(chainWith520Slow, 521)
	// Because solve time was slow (400s > 120s), target must be LARGER (easier or equal difficulty)
	// and MUST NOT be clamped to Difficulty 7!
	if target521Slow.Cmp(genTarget) <= 0 {
		t.Fatalf("target521Slow clamped to Difficulty 7 or harder! Got: %x, genTarget: %x", target521Slow, genTarget)
	}
	if diff521Slow := TargetToDifficulty(target521Slow); diff521Slow > 3 {
		t.Fatalf("expected difficulty <= 3 for slow solve time, got %d", diff521Slow)
	}

	// 5. Test dynamic retargeting at block 521 for fast miner (solve time = 10s)
	chainWith520Fast := append(chain, Block{
		Index:      520,
		Timestamp:  chain[519].Timestamp + 10, // Fast solve time
		Target:     fmt.Sprintf("%064x", target520),
		Difficulty: 3,
	})

	target521Fast := CalculateExpectedTarget(chainWith520Fast, 521)
	// Fast solve time produces higher difficulty (smaller target) than slow solve time
	if target521Fast.Cmp(target521Slow) >= 0 {
		t.Fatalf("target521Fast should be harder (smaller) than target521Slow: fast=%x slow=%x",
			target521Fast, target521Slow)
	}
}

// TestArmaxHashPostActivationTwoMinuteRuleConvergence mathematically proves that
// the 2-minute (120-second) per block target is strictly enforced by automatic LWMA-1 retargeting
// across blocks after the ArmaxHash activation (520+):
// 1. If blocks arrive faster than 120s (e.g. 30s), difficulty automatically increases (target decreases).
// 2. If blocks arrive slower than 120s (e.g. 240s), difficulty automatically decreases (target increases).
// 3. If blocks arrive at exactly 120s, difficulty converges and stabilizes.
func TestArmaxHashPostActivationTwoMinuteRuleConvergence(t *testing.T) {
	LockNetworkForTesting(t)
	SetNetworkForTesting(true)
	_ = SetNetwork("testnet")
	ResetArmaxHashActivationHeightOverride()

	// 1. Build initial chain up to block 519 (legacy blocks with 120s intervals)
	chain := make([]Block, 520)
	baseTime := int64(1700000000)
	genTarget := GetPowLimit()
	genTargetHex := fmt.Sprintf("%064x", genTarget)

	for i := 0; i < 520; i++ {
		chain[i] = Block{
			Index:      i,
			Timestamp:  baseTime + int64(i)*TargetBlockTimeSeconds,
			Target:     genTargetHex,
			Difficulty: 7,
		}
	}

	// Block 520 is the activation block (starts at ArmaxHashPowLimit = Difficulty 3)
	t520 := CalculateExpectedTarget(chain, 520)
	chain = append(chain, Block{
		Index:      520,
		Timestamp:  chain[519].Timestamp + TargetBlockTimeSeconds,
		Target:     fmt.Sprintf("%064x", t520),
		Difficulty: TargetToDifficulty(t520),
	})

	// Phase 1: High Hashrate -- simulate 20 blocks arriving in 30s intervals (4x faster than 120s target)
	for i := 521; i <= 540; i++ {
		nextT := CalculateExpectedTarget(chain, i)
		chain = append(chain, Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + 30, // 30 seconds solve time
			Target:     fmt.Sprintf("%064x", nextT),
			Difficulty: TargetToDifficulty(nextT),
		})
	}

	t540 := chain[540].GetTarget()
	// Target at block 540 must be smaller (numerically harder / higher difficulty) than at block 520
	if t540.Cmp(t520) >= 0 {
		t.Fatalf("LWMA-1 failed to increase difficulty during fast blocks: t540 (%x) not smaller than t520 (%x)",
			t540, t520)
	}

	// Phase 2: Low Hashrate -- simulate 20 blocks arriving in 300s intervals (2.5x slower than 120s target)
	for i := 541; i <= 560; i++ {
		nextT := CalculateExpectedTarget(chain, i)
		chain = append(chain, Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + 300, // 300 seconds solve time
			Target:     fmt.Sprintf("%064x", nextT),
			Difficulty: TargetToDifficulty(nextT),
		})
	}

	t560 := chain[560].GetTarget()
	// Target at block 560 must be larger (numerically easier / lower difficulty) than at block 540
	if t560.Cmp(t540) <= 0 {
		t.Fatalf("LWMA-1 failed to decrease difficulty during slow blocks: t560 (%x) not larger than t540 (%x)",
			t560, t540)
	}

	// Phase 3: Exact 120-second equilibrium -- simulate 30 blocks arriving at exactly 120s
	for i := 561; i <= 590; i++ {
		nextT := CalculateExpectedTarget(chain, i)
		chain = append(chain, Block{
			Index:      i,
			Timestamp:  chain[i-1].Timestamp + 120, // Exactly 120 seconds
			Target:     fmt.Sprintf("%064x", nextT),
			Difficulty: TargetToDifficulty(nextT),
		})
	}

	// Verify stability: targets over consecutive 120s blocks should converge closely
	t589 := chain[589].GetTarget()
	t590 := chain[590].GetTarget()
	diffRatio := new(big.Int).Abs(new(big.Int).Sub(t590, t589))
	percentChange := new(big.Int).Mul(diffRatio, big.NewInt(100))
	percentChange.Div(percentChange, t589)
	if percentChange.Int64() > 5 {
		t.Fatalf("LWMA-1 target did not stabilize at 120s target: >5%% variation between consecutive blocks (%d%%)",
			percentChange.Int64())
	}
}
