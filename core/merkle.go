package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// CalculateMerkleRoot computes the 32-byte (64 hex characters) binary Merkle tree root of transactions
func CalculateMerkleRoot(txs []Transaction) string {
	if len(txs) == 0 {
		h := sha256.Sum256([]byte{})
		return hex.EncodeToString(h[:])
	}

	var currentLevel []string
	for _, tx := range txs {
		currentLevel = append(currentLevel, tx.WitnessHashHex())
	}

	for len(currentLevel) > 1 {
		// If odd number of hashes, duplicate the last one (Satoshi Bitcoin standard)
		if len(currentLevel)%2 != 0 {
			currentLevel = append(currentLevel, currentLevel[len(currentLevel)-1])
		}

		var nextLevel []string
		for i := 0; i < len(currentLevel); i += 2 {
			left, _ := hex.DecodeString(currentLevel[i])
			right, _ := hex.DecodeString(currentLevel[i+1])
			concat := append(left, right...)
			h := sha256.Sum256(concat)
			nextLevel = append(nextLevel, hex.EncodeToString(h[:]))
		}
		currentLevel = nextLevel
	}

	return currentLevel[0]
}

// GenerateMerkleProof creates a cryptographic audit trail (Merkle Path) proving a transaction is inside a block
func GenerateMerkleProof(txs []Transaction, txIndex int) ([]string, error) {
	if txIndex < 0 || txIndex >= len(txs) {
		return nil, errors.New("transaction index out of bounds")
	}

	var currentLevel []string
	for _, tx := range txs {
		currentLevel = append(currentLevel, tx.WitnessHashHex())
	}

	var proof []string
	idx := txIndex

	for len(currentLevel) > 1 {
		if len(currentLevel)%2 != 0 {
			currentLevel = append(currentLevel, currentLevel[len(currentLevel)-1])
		}

		// Find sibling
		siblingIdx := idx ^ 1 // flip last bit to find pair
		proof = append(proof, currentLevel[siblingIdx])

		// Compute next level
		var nextLevel []string
		for i := 0; i < len(currentLevel); i += 2 {
			left, _ := hex.DecodeString(currentLevel[i])
			right, _ := hex.DecodeString(currentLevel[i+1])
			concat := append(left, right...)
			h := sha256.Sum256(concat)
			nextLevel = append(nextLevel, hex.EncodeToString(h[:]))
		}

		idx /= 2
		currentLevel = nextLevel
	}

	return proof, nil
}

// VerifyMerkleProof validates an SPV proof against a block's Merkle Root
func VerifyMerkleProof(txHash string, proof []string, txIndex int, merkleRoot string) bool {
	currHash := txHash
	idx := txIndex

	for _, sibling := range proof {
		currBytes, err := hex.DecodeString(currHash)
		if err != nil {
			return false
		}
		sibBytes, err := hex.DecodeString(sibling)
		if err != nil {
			return false
		}

		var concat []byte
		if idx%2 == 0 {
			concat = append(currBytes, sibBytes...)
		} else {
			concat = append(sibBytes, currBytes...)
		}

		h := sha256.Sum256(concat)
		currHash = hex.EncodeToString(h[:])
		idx /= 2
	}

	return currHash == merkleRoot
}
