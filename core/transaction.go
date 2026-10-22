package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

const (
	TxTypeStandard = "STANDARD"
)

type Transaction struct {
	Sender    string `json:"sender"`     // Sender wallet address (Bech32, BIP-173)
	SenderPub string `json:"sender_pub"` // Sender public key (hex encoded ML-DSA-65 public key, 3,904 hex chars / 1,952 bytes)
	Recipient string `json:"recipient"`  // Recipient wallet address
	Amount    int64  `json:"amount"`     // Amount of ARMX base units (Armoids) transferred (> 0)
	Fee       int64  `json:"fee"`        // Transaction fee awarded to miner (>= 0)
	Nonce     int64  `json:"nonce"`      // Sender transaction nonce (anti-replay and double-spending protection)
	Signature string `json:"signature"`  // ML-DSA-65 post-quantum digital signature (6,618 hex chars / 3,309 bytes)
	SigAlg    byte   `json:"sig_alg"`    // Signature algorithm identifier (0x01 = ML-DSA-65 FIPS 204) [P2-2]

	// Strategic Roadmap L2 & Smart Contract Extensions
	Type       string `json:"type,omitempty"`      // Transaction type: STANDARD (default), HTLC_LOCK, HTLC_CLAIM, HTLC_REFUND
	HashLock   string `json:"hash_lock,omitempty"` // 64 hex chars (SHA-256 hash lock for HTLC/Lightning channel)
	TimeLock   int64  `json:"time_lock,omitempty"` // Expiration timestamp or block height for refund
	Preimage   string `json:"preimage,omitempty"`  // Secret preimage revealed to claim HTLC funds
	cachedSize int    `json:"-"`                   // Cached serialized byte size [C-28, C-34]
}

// Hash produces a canonical double-SHA256 (SHA-256d) digest of the transaction data
// committing to NetworkID, SigAlg, canonical addresses, amounts, nonces, and script parameters [P2-2].
func (tx *Transaction) Hash() []byte {
	canonicalSender := strings.ToLower(tx.Sender)
	canonicalRecipient := strings.ToLower(tx.Recipient)
	netID := NetworkID()
	var record string
	if (tx.Type == "" || tx.Type == TxTypeStandard) && tx.HashLock == "" {
		record = fmt.Sprintf("%s:%d:%s:%s:%s:%d:%d:%d", netID, tx.SigAlg, canonicalSender, tx.SenderPub, canonicalRecipient, tx.Amount, tx.Fee, tx.Nonce)
	} else {
		record = fmt.Sprintf("%s:%d:%s:%s:%s:%d:%d:%d:%s:%s:%d:%s", netID, tx.SigAlg, canonicalSender, tx.SenderPub, canonicalRecipient, tx.Amount, tx.Fee, tx.Nonce, tx.Type, tx.HashLock, tx.TimeLock, tx.Preimage)
	}
	h1 := sha256.Sum256([]byte(record))
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// HashHex returns the 64-character hexadecimal representation of the transaction hash
func (tx *Transaction) HashHex() string {
	return hex.EncodeToString(tx.Hash())
}

// WitnessHash produces a double-SHA256 digest committing to both the transaction data and its signature [C-07]
func (tx *Transaction) WitnessHash() []byte {
	record := fmt.Sprintf("%s:%s", tx.HashHex(), tx.Signature)
	h1 := sha256.Sum256([]byte(record))
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// WitnessHashHex returns the 64-character hexadecimal representation of the witness hash
func (tx *Transaction) WitnessHashHex() string {
	return hex.EncodeToString(tx.WitnessHash())
}

// Size returns the approximate serialized byte size of the transaction, caching the result [C-28, C-34]
func (tx *Transaction) Size() int {
	if tx.cachedSize > 0 {
		return tx.cachedSize
	}
	data, err := json.Marshal(tx)
	if err != nil {
		return 0
	}
	tx.cachedSize = len(data)
	return tx.cachedSize
}

// Sign signs the transaction using an ML-DSA-65 PrivateKey [P2-2]
func (tx *Transaction) Sign(privKey *pqsig.PrivateKey) error {
	if privKey == nil {
		return errors.New("private key is nil")
	}
	tx.SigAlg = pqsig.SigAlgMLDSA65
	pubBytes := privKey.PublicKeyBytes()
	tx.SenderPub = hex.EncodeToString(pubBytes)

	txHash := tx.Hash()
	sig, err := privKey.Sign(txHash)
	if err != nil {
		return err
	}
	tx.Signature = hex.EncodeToString(sig)
	tx.cachedSize = 0
	return nil
}

func isCleanPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 32 || c > 126 || c == ':' {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Verify validates the transaction signature and cryptographic address binding.
// Note: Protocol coinbase transactions (Sender: "SYSTEM") are NOT validated here;
// they are strictly validated by consensus rules only at block transaction index 0.
func (tx *Transaction) Verify() bool {
	return tx.VerifyWithError() == nil
}

// ValidateBasic performs structural and consensus bounds validation on the transaction [Phase 6]
func (tx *Transaction) ValidateBasic() error {
	return tx.VerifyWithError()
}

// VerifyWithError validates the transaction and returns a descriptive error upon failure [L1-2]
func (tx *Transaction) VerifyWithError() error {
	// [C-29] Whitelist only standard transaction types for consensus
	if tx.Type != "" && tx.Type != TxTypeStandard {
		return fmt.Errorf("invalid transaction type: %s", tx.Type)
	}

	// Regular transactions must NEVER have Sender == "SYSTEM"
	if strings.EqualFold(tx.Sender, "SYSTEM") || tx.Sender == "" {
		return errors.New("sender cannot be SYSTEM or empty")
	}

	// Recipient must not be SYSTEM
	if strings.EqualFold(tx.Recipient, "SYSTEM") || tx.Recipient == "" {
		return errors.New("recipient cannot be SYSTEM or empty")
	}

	// [C-01] Range bounds on Amount and Fee
	if tx.Amount <= 0 || tx.Amount > MaxSupply {
		return fmt.Errorf("amount %d out of bounds (1..%d)", tx.Amount, MaxSupply)
	}
	if tx.Fee < 0 || tx.Fee > MaxSupply {
		return fmt.Errorf("fee %d out of bounds (0..%d)", tx.Fee, MaxSupply)
	}
	if tx.Amount > math.MaxInt64-tx.Fee {
		return errors.New("transaction amount + fee arithmetic overflow")
	}
	if tx.Nonce < 0 {
		return fmt.Errorf("nonce %d cannot be negative", tx.Nonce)
	}

	// [C-03] Sanitize fields against delimiter injection and non-printable characters
	if !isCleanPrintable(tx.Sender) || !isCleanPrintable(tx.Recipient) || !isCleanPrintable(tx.Type) {
		return errors.New("transaction fields contain non-printable or forbidden delimiter characters")
	}

	// Length bounds on addresses (BIP-173 max Bech32 length is 90)
	if len(tx.Sender) > 90 || len(tx.Recipient) > 90 {
		return errors.New("address exceeds maximum length of 90 characters")
	}

	// [Phase 6] As long as consensus only activates/accepts STANDARD transactions,
	// unsupported HTLC metadata fields (HashLock, TimeLock, Preimage) must be explicitly rejected
	// to ensure every field accepted by consensus is bound by the cryptographic hash and signature commitment.
	if tx.Type == "" || tx.Type == TxTypeStandard {
		if tx.HashLock != "" {
			return errors.New("unsupported field hash_lock in STANDARD transaction")
		}
		if tx.TimeLock != 0 {
			return errors.New("unsupported field time_lock in STANDARD transaction")
		}
		if tx.Preimage != "" {
			return errors.New("unsupported field preimage in STANDARD transaction")
		}
	} else {
		// Non-standard transaction types (e.g. HTLC) are not yet activated in consensus
		return fmt.Errorf("transaction type %q is not activated in consensus (only STANDARD supported)", tx.Type)
	}

	if tx.SenderPub == "" || tx.Signature == "" {
		return errors.New("sender_pub and signature must not be empty")
	}

	// Recipient address must be valid: Bech32, Base58Check, or legacy 40 hex characters
	if !ValidateAddress(tx.Recipient) {
		return fmt.Errorf("invalid recipient address: %s", tx.Recipient)
	}
	if !ValidateAddress(tx.Sender) {
		return fmt.Errorf("invalid sender address: %s", tx.Sender)
	}

	// [C-19] Disallow self-transfer (Sender == Recipient canonical hex)
	senderHex, errSender := AddressToHex(tx.Sender)
	recipHex, errRecip := AddressToHex(tx.Recipient)
	if errSender != nil || errRecip != nil || strings.EqualFold(senderHex, recipHex) {
		return errors.New("self-transfers are disallowed (sender equals recipient)")
	}

	// [P2-2] SigAlg validation: strictly ML-DSA-65 (0x01)
	if tx.SigAlg != pqsig.SigAlgMLDSA65 {
		return fmt.Errorf("unsupported signature algorithm: 0x%02x (expected ML-DSA-65 0x01)", tx.SigAlg)
	}

	// 1. Verify SenderPub is exactly 2 * PublicKeySize hex chars (1,952 bytes = 3,904 hex chars)
	if len(tx.SenderPub) != 2*pqsig.PublicKeySize || !isHex(tx.SenderPub) {
		return fmt.Errorf("sender public key hex length must be exactly %d, got %d", 2*pqsig.PublicKeySize, len(tx.SenderPub))
	}
	pubKeyBytes, err := hex.DecodeString(tx.SenderPub)
	if err != nil || len(pubKeyBytes) != pqsig.PublicKeySize {
		return errors.New("failed to decode sender public key hex bytes")
	}

	derivedBech32Address, err := PubToAddress(tx.SigAlg, pubKeyBytes)
	if err != nil || !MatchAddress(tx.Sender, derivedBech32Address) {
		return fmt.Errorf("sender address %s does not match derived public key address %s", tx.Sender, derivedBech32Address)
	}

	// 2. Verify Signature is exactly 2 * SignatureSize hex chars (3,309 bytes = 6,618 hex chars)
	if len(tx.Signature) != 2*pqsig.SignatureSize || !isHex(tx.Signature) {
		return fmt.Errorf("signature hex length must be exactly %d, got %d", 2*pqsig.SignatureSize, len(tx.Signature))
	}
	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil || len(sigBytes) != pqsig.SignatureSize {
		return errors.New("failed to decode signature hex bytes")
	}

	// 3. ML-DSA-65 signature verification [P2-2, L1-2]
	if !pqsig.Verify(pubKeyBytes, tx.Hash(), sigBytes) {
		return errors.New("signature verification failed (invalid signature for network/message)")
	}
	return nil
}
