package core

import (
	"fmt"
	"sync"
)

const (
	// GenesisTimestamp is the deterministic immutable timestamp for the Armaxcoin Mainnet Genesis block
	GenesisTimestamp = int64(1710000000) // 2024-03-09T18:40:00Z

	// GenesisMiner is the canonical burn address receiving the initial Genesis reward (32-byte 0x00 payload) [P2-2]
	GenesisMiner = "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"

	// GenesisNonce is the Proof of Work nonce solving the Mainnet Genesis block
	GenesisNonce = int64(7)

	// GenesisTargetHex is the Proof of Work target ceiling for the Mainnet Genesis block (Difficulty 1) [C-31]
	GenesisTargetHex = "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	// GenesisMerkleRoot is the Merkle root of the Mainnet Genesis coinbase transaction [C-07]
	GenesisMerkleRoot = "ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76"

	// GenesisHash is the canonical double-SHA256 (SHA-256d) hash of the Mainnet Genesis block
	GenesisHash = "0820d42962071e01c8a7d2e6a3aa5640a17f8a60e9006bf59b6e76ac0001535e"

	// GenesisPrevHash is the 64-zero parent hash of block 0
	GenesisPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

	// Testnet Genesis Constants [P2-6, L1-1, L1-2, L1-6, L4-8]
	TestnetGenesisTimestamp  = int64(1710000001)
	TestnetGenesisNonce      = int64(40272237)
	TestnetGenesisTargetHex  = "0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	TestnetGenesisMerkleRoot = "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d"
	TestnetGenesisHash       = "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b"
)

// GetMainnetGenesisBlock returns the hardcoded Mainnet Genesis block [P2-6]
func GetMainnetGenesisBlock() Block {
	coinbase := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: GenesisMiner,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     0,
		Signature: "REWARD_SIG",
	}

	return Block{
		Index:        0,
		Timestamp:    GenesisTimestamp,
		Transactions: []Transaction{coinbase},
		PrevHash:     GenesisPrevHash,
		MerkleRoot:   GenesisMerkleRoot,
		Hash:         GenesisHash,
		Nonce:        GenesisNonce,
		Miner:        GenesisMiner,
		Difficulty:   1,
		Target:       GenesisTargetHex,
	}
}

// GetTestnetGenesisBlock returns the hardcoded Testnet Genesis block [P2-6, L1-1, L1-6]
func GetTestnetGenesisBlock() Block {
	coinbase := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: GenesisMiner,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     0,
		Signature: "REWARD_SIG",
	}

	return Block{
		Index:        0,
		Timestamp:    TestnetGenesisTimestamp,
		Transactions: []Transaction{coinbase},
		PrevHash:     GenesisPrevHash,
		MerkleRoot:   TestnetGenesisMerkleRoot,
		Hash:         TestnetGenesisHash,
		Nonce:        TestnetGenesisNonce,
		Miner:        GenesisMiner,
		Difficulty:   7,
		Target:       TestnetGenesisTargetHex,
	}
}

var (
	soakGenesisOnce    sync.Once
	soakGenesisBlock   Block
	regtestGenesisOnce sync.Once
	regtestGenesisBlk  Block
)

// GetSoakGenesisBlock returns the deterministic 16-bit target Genesis block for soak testing [L3-5]
func GetSoakGenesisBlock() Block {
	soakGenesisOnce.Do(func() {
		coinbase := Transaction{
			Sender:    "SYSTEM",
			SenderPub: "",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Fee:       0,
			Nonce:     0,
			Signature: "REWARD_SIG",
		}

		b := Block{
			Index:        0,
			Timestamp:    1710000002,
			Transactions: []Transaction{coinbase},
			PrevHash:     GenesisPrevHash,
			MerkleRoot:   CalculateMerkleRoot([]Transaction{coinbase}),
			Miner:        GenesisMiner,
			Difficulty:   4,
			Target:       SoakProfile.PowLimitHex,
		}
		target := DifficultyToTarget(4)
		for {
			var err error
			b.Hash, err = b.CalculateHashWithError()
			if err == nil && HashMeetsTarget(b.Hash, target) {
				break
			}
			b.Nonce++
		}
		soakGenesisBlock = b
	})
	cp := soakGenesisBlock
	cp.Transactions = append([]Transaction(nil), soakGenesisBlock.Transactions...)
	return cp
}

// GetRegtestGenesisBlock returns the deterministic 8-bit target Genesis block for local regression testing [L4-8]
func GetRegtestGenesisBlock() Block {
	regtestGenesisOnce.Do(func() {
		coinbase := Transaction{
			Sender:    "SYSTEM",
			SenderPub: "",
			Recipient: GenesisMiner,
			Amount:    InitialReward,
			Fee:       0,
			Nonce:     0,
			Signature: "REWARD_SIG",
		}

		b := Block{
			Index:        0,
			Timestamp:    1710000003,
			Transactions: []Transaction{coinbase},
			PrevHash:     GenesisPrevHash,
			MerkleRoot:   CalculateMerkleRoot([]Transaction{coinbase}),
			Miner:        GenesisMiner,
			Difficulty:   2,
			Target:       RegtestProfile.PowLimitHex,
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
		regtestGenesisBlk = b
	})
	cp := regtestGenesisBlk
	cp.Transactions = append([]Transaction(nil), regtestGenesisBlk.Transactions...)
	return cp
}

// GetGenesisBlock returns the Genesis block for the active network profile [C-33, P2-6, L3-5, L4-8]
func GetGenesisBlock() Block {
	if GetActiveNetwork().Name == "testnet" {
		return GetTestnetGenesisBlock()
	}
	if GetActiveNetwork().Name == "regtest" {
		return GetRegtestGenesisBlock()
	}
	if GetActiveNetwork().Name == "soak" {
		return GetSoakGenesisBlock()
	}
	return GetMainnetGenesisBlock()
}

// ValidateGenesisBlock strictly verifies that a block 0 matches the active network's protocol Genesis specification [C-23, P2-6]
func ValidateGenesisBlock(b *Block) error {
	if b == nil {
		return fmt.Errorf("genesis block is nil")
	}
	gen := GetGenesisBlock()

	if b.Index != 0 {
		return fmt.Errorf("genesis block index must be 0, got %d", b.Index)
	}
	if b.Hash != gen.Hash {
		return fmt.Errorf("genesis block hash mismatch: got %s, expected %s", b.Hash, gen.Hash)
	}
	if b.PrevHash != GenesisPrevHash {
		return fmt.Errorf("genesis block prevhash mismatch")
	}
	if b.Timestamp != gen.Timestamp {
		return fmt.Errorf("genesis block timestamp mismatch: got %d, expected %d", b.Timestamp, gen.Timestamp)
	}
	if b.MerkleRoot != gen.MerkleRoot {
		return fmt.Errorf("genesis block merkle root mismatch: got %s, expected %s", b.MerkleRoot, gen.MerkleRoot)
	}
	if b.Nonce != gen.Nonce {
		return fmt.Errorf("genesis block nonce mismatch")
	}
	if b.Miner != gen.Miner {
		return fmt.Errorf("genesis block miner mismatch: got %s, expected %s", b.Miner, gen.Miner)
	}
	calcHash, err := b.CalculateHashWithError()
	if err != nil {
		return fmt.Errorf("genesis block calculated hash error: %w", err)
	}
	if b.Hash == "" || calcHash != gen.Hash {
		return fmt.Errorf("genesis block calculated hash mismatch")
	}
	if b.CalculateMerkleRoot() != gen.MerkleRoot {
		return fmt.Errorf("genesis block calculated merkle root mismatch")
	}
	if len(b.Transactions) != len(gen.Transactions) {
		return fmt.Errorf("genesis block transaction count mismatch: got %d, expected %d", len(b.Transactions), len(gen.Transactions))
	}
	for i := range b.Transactions {
		t1 := b.Transactions[i]
		t2 := gen.Transactions[i]
		if t1.Sender != t2.Sender || t1.Recipient != t2.Recipient || t1.Amount != t2.Amount ||
			t1.Fee != t2.Fee || t1.Nonce != t2.Nonce || t1.Signature != t2.Signature ||
			t1.Type != t2.Type || t1.SenderPub != t2.SenderPub {
			return fmt.Errorf("genesis transaction %d payload mismatch", i)
		}
		if t1.WitnessHashHex() != t2.WitnessHashHex() {
			return fmt.Errorf("genesis transaction %d hash mismatch", i)
		}
	}
	return nil
}
