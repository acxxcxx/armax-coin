package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

const (
	TxTypeStandard = "STANDARD"

	// TxBinaryVersionV1 identifies version 1 of the compact TxBinaryV1 wire serialization format.
	TxBinaryVersionV1 byte = 0x01

	txFlagIsCoinbase           byte = 1 << 0
	txFlagHasExtended          byte = 1 << 1
	txFlagExplicitStandardType byte = 1 << 2
	txFlagCustomSender         byte = 1 << 3
	txFlagRawRecipient         byte = 1 << 4
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
	Type             string `json:"type,omitempty"`      // Transaction type: STANDARD (default), HTLC_LOCK, HTLC_CLAIM, HTLC_REFUND
	HashLock         string `json:"hash_lock,omitempty"` // 64 hex chars (SHA-256 hash lock for HTLC/Lightning channel)
	TimeLock         int64  `json:"time_lock,omitempty"` // Expiration timestamp or block height for refund
	Preimage         string `json:"preimage,omitempty"`  // Secret preimage revealed to claim HTLC funds
	cachedSize       int32  `json:"-"`                   // Cached JSON serialized byte size [C-28, C-34, v1.0.6]
	cachedBinarySize int32  `json:"-"`                   // Cached TxBinaryV1 serialized byte size
}

// Hash produces a canonical double-SHA256 (SHA-256d) digest of the transaction data
// committing to NetworkID, SigAlg, canonical addresses, amounts, nonces, and script parameters [P2-2].
func (tx *Transaction) Hash() []byte {
	return tx.HashForNetwork(NetworkID())
}

// HashForNetwork computes the canonical transaction hash bound to a specific network ID [P2-2, C-33].
func (tx *Transaction) HashForNetwork(netID string) []byte {
	canonicalSender := strings.ToLower(tx.Sender)
	canonicalRecipient := strings.ToLower(tx.Recipient)
	if netID == "" {
		netID = NetworkID()
	}
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

// HashHexForNetwork returns the 64-character hexadecimal representation of the transaction hash for a specific network ID
func (tx *Transaction) HashHexForNetwork(netID string) string {
	return hex.EncodeToString(tx.HashForNetwork(netID))
}

// WitnessHash produces a double-SHA256 digest committing to both the transaction data and its signature [C-07]
func (tx *Transaction) WitnessHash() []byte {
	return tx.WitnessHashForNetwork(NetworkID())
}

// WitnessHashForNetwork produces a double-SHA256 digest committing to both transaction data and signature for a specific network ID [C-07, C-33]
func (tx *Transaction) WitnessHashForNetwork(netID string) []byte {
	record := fmt.Sprintf("%s:%s", tx.HashHexForNetwork(netID), tx.Signature)
	h1 := sha256.Sum256([]byte(record))
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// WitnessHashHex returns the 64-character hexadecimal representation of the witness hash
func (tx *Transaction) WitnessHashHex() string {
	return hex.EncodeToString(tx.WitnessHash())
}

// WitnessHashHexForNetwork returns the 64-character hexadecimal representation of the witness hash for a specific network ID
func (tx *Transaction) WitnessHashHexForNetwork(netID string) string {
	return hex.EncodeToString(tx.WitnessHashForNetwork(netID))
}

type sigVerifyCache struct {
	sync.RWMutex
	entries map[string]struct{}
	maxSize int
}

var globalSigVerifyCache = &sigVerifyCache{
	entries: make(map[string]struct{}),
	maxSize: 10000,
}

// IsSignatureVerified returns true if this transaction's witness hash has already been verified [T5].
func (tx *Transaction) IsSignatureVerified() bool {
	wHash := tx.WitnessHashHex()
	globalSigVerifyCache.RLock()
	_, exists := globalSigVerifyCache.entries[wHash]
	globalSigVerifyCache.RUnlock()
	return exists
}

// MarkSignatureVerified caches the transaction witness hash as cryptographically valid [T5].
func (tx *Transaction) MarkSignatureVerified() {
	wHash := tx.WitnessHashHex()
	globalSigVerifyCache.Lock()
	if len(globalSigVerifyCache.entries) >= globalSigVerifyCache.maxSize {
		count := 0
		for k := range globalSigVerifyCache.entries {
			delete(globalSigVerifyCache.entries, k)
			count++
			if count >= globalSigVerifyCache.maxSize/2 {
				break
			}
		}
	}
	globalSigVerifyCache.entries[wHash] = struct{}{}
	globalSigVerifyCache.Unlock()
}

// Size returns the approximate serialized byte size of the transaction, caching the result [C-28, C-34, v1.0.6]
func (tx *Transaction) Size() int {
	if cached := atomic.LoadInt32(&tx.cachedSize); cached > 0 {
		return int(cached)
	}
	data, err := json.Marshal(tx)
	if err != nil {
		return 0
	}
	sz := int32(len(data))
	atomic.StoreInt32(&tx.cachedSize, sz)
	return int(sz)
}

// InvalidateCache resets the cached serialized byte sizes of the transaction [v1.0.6]
func (tx *Transaction) InvalidateCache() {
	atomic.StoreInt32(&tx.cachedSize, 0)
	atomic.StoreInt32(&tx.cachedBinarySize, 0)
}

// IsCoinbase reports whether this transaction is a protocol block reward (Coinbase) transaction.
func (tx *Transaction) IsCoinbase() bool {
	return tx != nil && strings.EqualFold(tx.Sender, "SYSTEM")
}

// EffectiveTxSize returns the consensus-effective serialized byte size of tx at blockIndex
// (or height 0 if omitted). Before BlockBinarySizeActivationHeight it returns legacy JSON tx.Size();
// from activation onward it returns tx.BinarySize() (falling back to tx.Size() if tx is malformed).
func EffectiveTxSize(tx *Transaction, blockIndex ...int) int {
	if tx == nil {
		return 0
	}
	h := 0
	if len(blockIndex) > 0 {
		h = blockIndex[0]
	}
	if IsBlockBinarySizeActive(h) {
		if binSz := tx.BinarySize(); binSz > 0 {
			return binSz
		}
	}
	return tx.Size()
}

// BinarySize returns the deterministic byte length of the TxBinaryV1 encoding, caching the result thread-safely.
func (tx *Transaction) BinarySize() int {
	if cached := atomic.LoadInt32(&tx.cachedBinarySize); cached > 0 {
		return int(cached)
	}
	data, err := tx.MarshalBinary()
	if err != nil {
		return 0
	}
	sz := int32(len(data))
	atomic.StoreInt32(&tx.cachedBinarySize, sz)
	return int(sz)
}

func appendU16Bytes(buf []byte, b []byte) ([]byte, error) {
	if len(b) > 0xFFFF {
		return nil, fmt.Errorf("field exceeds uint16 max length: %d", len(b))
	}
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(b)))
	buf = append(buf, lenBuf[:]...)
	return append(buf, b...), nil
}

func readU16Bytes(data []byte, off *int) ([]byte, error) {
	if *off+2 > len(data) {
		return nil, errors.New("unexpected EOF reading length prefix")
	}
	n := int(binary.LittleEndian.Uint16(data[*off : *off+2]))
	*off += 2
	if *off+n > len(data) {
		return nil, errors.New("unexpected EOF reading payload bytes")
	}
	out := make([]byte, n)
	copy(out, data[*off:*off+n])
	*off += n
	return out, nil
}

// MarshalBinary encodes the transaction into the compact TxBinaryV1 wire format.
// Regular ML-DSA-65 transactions serialize in ~5,320 bytes; Coinbase transactions omit
// ML-DSA-65 keys/signatures and serialize in ~60 bytes.
func (tx *Transaction) MarshalBinary() ([]byte, error) {
	if tx == nil {
		return nil, errors.New("cannot marshal nil transaction")
	}

	isCoinbase := tx.IsCoinbase()
	hasExtended := (tx.Type != "" && tx.Type != TxTypeStandard) || tx.HashLock != "" || tx.TimeLock != 0 || tx.Preimage != ""

	var flags byte
	if isCoinbase {
		flags |= txFlagIsCoinbase
	}
	if hasExtended {
		flags |= txFlagHasExtended
	} else if tx.Type == TxTypeStandard {
		flags |= txFlagExplicitStandardType
	}

	var pubBytes, sigBytes []byte
	if !isCoinbase {
		if len(tx.SenderPub) != 2*pqsig.PublicKeySize {
			return nil, fmt.Errorf("invalid sender public key hex length: got %d, want %d", len(tx.SenderPub), 2*pqsig.PublicKeySize)
		}
		var err error
		pubBytes, err = hex.DecodeString(tx.SenderPub)
		if err != nil || len(pubBytes) != pqsig.PublicKeySize {
			return nil, errors.New("failed to decode ML-DSA-65 sender public key hex bytes")
		}
		if tx.SigAlg == 0 {
			return nil, errors.New("missing sig_alg for non-coinbase transaction")
		}
		derivedSender, err := PubToAddress(tx.SigAlg, pubBytes)
		if err != nil || tx.Sender != derivedSender {
			flags |= txFlagCustomSender
		}

		if len(tx.Signature) != 2*pqsig.SignatureSize {
			return nil, fmt.Errorf("invalid signature hex length: got %d, want %d", len(tx.Signature), 2*pqsig.SignatureSize)
		}
		sigBytes, err = hex.DecodeString(tx.Signature)
		if err != nil || len(sigBytes) != pqsig.SignatureSize {
			return nil, errors.New("failed to decode ML-DSA-65 signature hex bytes")
		}
	} else if tx.Sender != "SYSTEM" {
		// Preserve exact non-canonical case if any ("system")
		flags |= txFlagCustomSender
	}

	var recipPayload []byte
	if hrp, p, err := Bech32Decode(tx.Recipient); err == nil && hrp == Bech32HRP && len(p) == AddressPayloadLen {
		recipPayload = p
	} else {
		flags |= txFlagRawRecipient
	}

	estCap := 128
	if !isCoinbase {
		estCap = 5400
	}
	buf := make([]byte, 0, estCap)
	buf = append(buf, TxBinaryVersionV1, flags, tx.SigAlg)

	if !isCoinbase {
		buf = append(buf, pubBytes...)
	}

	var err error
	if (flags & txFlagCustomSender) != 0 {
		buf, err = appendU16Bytes(buf, []byte(tx.Sender))
		if err != nil {
			return nil, err
		}
	}

	if (flags & txFlagRawRecipient) == 0 {
		buf = append(buf, recipPayload...)
	} else {
		buf, err = appendU16Bytes(buf, []byte(tx.Recipient))
		if err != nil {
			return nil, err
		}
	}

	var numBuf [8]byte
	binary.LittleEndian.PutUint64(numBuf[:], uint64(tx.Amount))
	buf = append(buf, numBuf[:]...)
	binary.LittleEndian.PutUint64(numBuf[:], uint64(tx.Fee))
	buf = append(buf, numBuf[:]...)
	binary.LittleEndian.PutUint64(numBuf[:], uint64(tx.Nonce))
	buf = append(buf, numBuf[:]...)

	if isCoinbase {
		if tx.Signature == "REWARD_SIG" {
			buf = append(buf, 0x01)
		} else {
			buf = append(buf, 0x02)
			buf, err = appendU16Bytes(buf, []byte(tx.Signature))
			if err != nil {
				return nil, err
			}
		}
	} else {
		buf = append(buf, sigBytes...)
	}

	if hasExtended {
		buf, err = appendU16Bytes(buf, []byte(tx.Type))
		if err != nil {
			return nil, err
		}
		if tx.HashLock == "" {
			buf = append(buf, 0x00)
		} else if len(tx.HashLock) == 64 {
			if rawHL, decErr := hex.DecodeString(tx.HashLock); decErr == nil && hex.EncodeToString(rawHL) == tx.HashLock {
				buf = append(buf, 0x01)
				buf = append(buf, rawHL...)
			} else {
				buf = append(buf, 0x02)
				buf, err = appendU16Bytes(buf, []byte(tx.HashLock))
				if err != nil {
					return nil, err
				}
			}
		} else {
			buf = append(buf, 0x02)
			buf, err = appendU16Bytes(buf, []byte(tx.HashLock))
			if err != nil {
				return nil, err
			}
		}

		binary.LittleEndian.PutUint64(numBuf[:], uint64(tx.TimeLock))
		buf = append(buf, numBuf[:]...)

		if tx.Preimage == "" {
			buf = append(buf, 0x00)
		} else if len(tx.Preimage) == 64 {
			if rawPre, decErr := hex.DecodeString(tx.Preimage); decErr == nil && hex.EncodeToString(rawPre) == tx.Preimage {
				buf = append(buf, 0x01)
				buf, err = appendU16Bytes(buf, rawPre)
				if err != nil {
					return nil, err
				}
			} else {
				buf = append(buf, 0x02)
				buf, err = appendU16Bytes(buf, []byte(tx.Preimage))
				if err != nil {
					return nil, err
				}
			}
		} else {
			buf = append(buf, 0x02)
			buf, err = appendU16Bytes(buf, []byte(tx.Preimage))
			if err != nil {
				return nil, err
			}
		}
	}

	return buf, nil
}

// UnmarshalBinary decodes a TxBinaryV1 byte buffer back into the Transaction struct.
func (tx *Transaction) UnmarshalBinary(data []byte) error {
	if tx == nil {
		return errors.New("cannot unmarshal into nil transaction")
	}
	if len(data) < 3 {
		return errors.New("tx binary buffer too short")
	}
	ver := data[0]
	if ver != TxBinaryVersionV1 {
		return fmt.Errorf("unsupported tx binary version: 0x%02x", ver)
	}
	flags := data[1]
	sigAlg := data[2]
	off := 3

	isCoinbase := (flags & txFlagIsCoinbase) != 0
	var decoded Transaction
	decoded.SigAlg = sigAlg

	var pubBytes []byte
	if !isCoinbase {
		if off+pqsig.PublicKeySize > len(data) {
			return errors.New("unexpected EOF reading sender public key")
		}
		pubBytes = data[off : off+pqsig.PublicKeySize]
		off += pqsig.PublicKeySize
		decoded.SenderPub = hex.EncodeToString(pubBytes)
	}

	if (flags & txFlagCustomSender) != 0 {
		sBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading custom sender: %w", err)
		}
		decoded.Sender = string(sBytes)
	} else if isCoinbase {
		decoded.Sender = "SYSTEM"
	} else {
		addr, err := PubToAddress(sigAlg, pubBytes)
		if err != nil {
			return fmt.Errorf("failed deriving sender address: %w", err)
		}
		decoded.Sender = addr
	}

	if (flags & txFlagRawRecipient) == 0 {
		if off+AddressPayloadLen > len(data) {
			return errors.New("unexpected EOF reading recipient payload")
		}
		addr, err := Bech32Encode(Bech32HRP, data[off:off+AddressPayloadLen])
		if err != nil {
			return fmt.Errorf("failed encoding recipient address: %w", err)
		}
		decoded.Recipient = addr
		off += AddressPayloadLen
	} else {
		rBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading raw recipient: %w", err)
		}
		decoded.Recipient = string(rBytes)
	}

	if off+24 > len(data) {
		return errors.New("unexpected EOF reading amount/fee/nonce")
	}
	decoded.Amount = int64(binary.LittleEndian.Uint64(data[off : off+8]))
	off += 8
	decoded.Fee = int64(binary.LittleEndian.Uint64(data[off : off+8]))
	off += 8
	decoded.Nonce = int64(binary.LittleEndian.Uint64(data[off : off+8]))
	off += 8

	if isCoinbase {
		if off >= len(data) {
			return errors.New("unexpected EOF reading coinbase signature mode")
		}
		cbMode := data[off]
		off++
		switch cbMode {
		case 0x01:
			decoded.Signature = "REWARD_SIG"
		case 0x02:
			cbSig, err := readU16Bytes(data, &off)
			if err != nil {
				return fmt.Errorf("failed reading coinbase signature: %w", err)
			}
			decoded.Signature = string(cbSig)
		default:
			return fmt.Errorf("invalid coinbase signature mode: 0x%02x", cbMode)
		}
	} else {
		if off+pqsig.SignatureSize > len(data) {
			return errors.New("unexpected EOF reading ML-DSA-65 signature")
		}
		decoded.Signature = hex.EncodeToString(data[off : off+pqsig.SignatureSize])
		off += pqsig.SignatureSize
	}

	if (flags & txFlagHasExtended) != 0 {
		tBytes, err := readU16Bytes(data, &off)
		if err != nil {
			return fmt.Errorf("failed reading tx type: %w", err)
		}
		decoded.Type = string(tBytes)

		if off >= len(data) {
			return errors.New("unexpected EOF reading hash_lock mode")
		}
		hlMode := data[off]
		off++
		switch hlMode {
		case 0x00:
			decoded.HashLock = ""
		case 0x01:
			if off+32 > len(data) {
				return errors.New("unexpected EOF reading 32-byte hash_lock")
			}
			decoded.HashLock = hex.EncodeToString(data[off : off+32])
			off += 32
		case 0x02:
			hlBytes, err := readU16Bytes(data, &off)
			if err != nil {
				return fmt.Errorf("failed reading hash_lock string: %w", err)
			}
			decoded.HashLock = string(hlBytes)
		default:
			return fmt.Errorf("invalid hash_lock mode: 0x%02x", hlMode)
		}

		if off+8 > len(data) {
			return errors.New("unexpected EOF reading time_lock")
		}
		decoded.TimeLock = int64(binary.LittleEndian.Uint64(data[off : off+8]))
		off += 8

		if off >= len(data) {
			return errors.New("unexpected EOF reading preimage mode")
		}
		preMode := data[off]
		off++
		switch preMode {
		case 0x00:
			decoded.Preimage = ""
		case 0x01:
			preBytes, err := readU16Bytes(data, &off)
			if err != nil {
				return fmt.Errorf("failed reading binary preimage: %w", err)
			}
			decoded.Preimage = hex.EncodeToString(preBytes)
		case 0x02:
			preBytes, err := readU16Bytes(data, &off)
			if err != nil {
				return fmt.Errorf("failed reading string preimage: %w", err)
			}
			decoded.Preimage = string(preBytes)
		default:
			return fmt.Errorf("invalid preimage mode: 0x%02x", preMode)
		}
	} else if (flags & txFlagExplicitStandardType) != 0 {
		decoded.Type = TxTypeStandard
	}

	if off != len(data) {
		return fmt.Errorf("trailing bytes in tx binary payload: consumed %d of %d", off, len(data))
	}

	atomic.StoreInt32(&decoded.cachedBinarySize, int32(len(data)))
	*tx = decoded
	return nil
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
	atomic.StoreInt32(&tx.cachedSize, 0)
	atomic.StoreInt32(&tx.cachedBinarySize, 0)
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
	return tx.validateBasic()
}

// VerifyWithError validates the transaction and returns a descriptive error upon failure [L1-2, T5]
func (tx *Transaction) VerifyWithError() error {
	if err := tx.validateBasic(); err != nil {
		return err
	}

	if tx.IsSignatureVerified() {
		return nil
	}

	pubKeyBytes, err := hex.DecodeString(tx.SenderPub)
	if err != nil {
		return errors.New("failed to decode sender public key hex bytes")
	}
	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return errors.New("failed to decode signature hex bytes")
	}

	if !pqsig.Verify(pubKeyBytes, tx.Hash(), sigBytes) {
		return errors.New("signature verification failed (invalid signature for network/message)")
	}
	tx.MarkSignatureVerified()
	return nil
}

func (tx *Transaction) validateBasic() error {
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

	return nil
}
