package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// Hash Time-Locked Contracts (HTLC) - EXPERIMENTAL ONLY [C-29]
//
// NOTICE: This file contains experimental Layer-2 smart contract extensions.
// These transaction types are strictly NOT active or validated in the mainnet
// consensus rules. Standard transactions must only use type "" or "STANDARD".
// ============================================================================

// Experimental HTLC Transaction Types
const (
	TxTypeHTLCLock   = "HTLC_LOCK"
	TxTypeHTLCClaim  = "HTLC_CLAIM"
	TxTypeHTLCRefund = "HTLC_REFUND"
)

// GenerateHTLCSecret creates a cryptographically secure 32-byte secret preimage
// and its corresponding SHA-256 hash lock for Layer-2 Lightning/State channels.
func GenerateHTLCSecret() (preimageHex string, hashLockHex string, err error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", err
	}
	preimageHex = hex.EncodeToString(secret)
	hash := sha256.Sum256(secret)
	hashLockHex = hex.EncodeToString(hash[:])
	return preimageHex, hashLockHex, nil
}

// VerifyHTLCPreimage validates that the secret preimage matches the target hash lock
func VerifyHTLCPreimage(preimageHex, hashLockHex string) bool {
	if len(preimageHex) != 64 || len(hashLockHex) != 64 {
		return false
	}
	preimageBytes, err := hex.DecodeString(preimageHex)
	if err != nil {
		return false
	}
	expectedHashBytes, err := hex.DecodeString(hashLockHex)
	if err != nil {
		return false
	}

	actualHash := sha256.Sum256(preimageBytes)
	for i := 0; i < 32; i++ {
		if actualHash[i] != expectedHashBytes[i] {
			return false
		}
	}
	return true
}

// ValidateHTLC verifies HTLC conditions based on transaction type (experimental)
func (tx *Transaction) ValidateHTLC(currentBlockHeight int, currentTime int64) error {
	if currentTime <= 0 {
		currentTime = time.Now().Unix()
	}
	switch tx.Type {
	case "", TxTypeStandard:
		return nil

	case TxTypeHTLCLock:
		if len(tx.HashLock) != 64 {
			return errors.New("HTLC_LOCK requires a valid 32-byte (64 hex char) SHA-256 hash lock")
		}
		if tx.TimeLock <= 0 {
			return errors.New("HTLC_LOCK requires a positive time lock parameter")
		}
		// TimeLock can be either a future block height or Unix timestamp in seconds
		if tx.TimeLock < int64(currentBlockHeight) && tx.TimeLock < currentTime {
			return errors.New("HTLC_LOCK expiration must be set in the future")
		}
		return nil

	case TxTypeHTLCClaim:
		if len(tx.HashLock) != 64 {
			return errors.New("HTLC_CLAIM requires the target hash lock")
		}
		if !VerifyHTLCPreimage(tx.Preimage, tx.HashLock) {
			return errors.New("HTLC_CLAIM preimage does not match hash lock")
		}
		return nil

	case TxTypeHTLCRefund:
		if len(tx.HashLock) != 64 {
			return errors.New("HTLC_REFUND requires the target hash lock")
		}
		// Refund can only occur after the time lock expiration
		isHeightLocked := tx.TimeLock < 1000000000 // Block height threshold
		if isHeightLocked {
			if int64(currentBlockHeight) <= tx.TimeLock {
				return fmt.Errorf("HTLC_REFUND cannot be executed before block height %d (current: %d)", tx.TimeLock, currentBlockHeight)
			}
		} else {
			if currentTime <= tx.TimeLock {
				return fmt.Errorf("HTLC_REFUND cannot be executed before timestamp %d (current: %d)", tx.TimeLock, currentTime)
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported transaction type: %s", tx.Type)
	}
}
