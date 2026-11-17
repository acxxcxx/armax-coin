package core

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// TestP2_AddressFormatAndPayloadLen verifies P2-2 address rules:
// Bech32 HRP 'armx', AddressPayloadLen = 32, SHA-256(SigAlg || pubKey).
func TestP2_AddressFormatAndPayloadLen(t *testing.T) {
	if AddressPayloadLen != 32 {
		t.Fatalf("Expected AddressPayloadLen to be 32, got %d", AddressPayloadLen)
	}

	sk, seed, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	_ = seed

	pubBytes := sk.PublicKeyBytes()
	addr, err := PubToAddress(pqsig.SigAlgMLDSA65, pubBytes)
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}

	if !ValidateAddress(addr) {
		t.Fatalf("ValidateAddress rejected valid ML-DSA address: %s", addr)
	}

	hrp, payload, err := Bech32Decode(addr)
	if err != nil || hrp != Bech32HRP {
		t.Fatalf("Bech32Decode failed: %v, hrp=%s", err, hrp)
	}
	if len(payload) != AddressPayloadLen {
		t.Fatalf("Expected payload len %d, got %d", AddressPayloadLen, len(payload))
	}

	// Verify preimage is SHA-256(SigAlg || pubKey)
	preimage := append([]byte{pqsig.SigAlgMLDSA65}, pubBytes...)
	expectedHash := sha256.Sum256(preimage)
	expectedAddr, err := Bech32Encode(Bech32HRP, expectedHash[:])
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}
	if addr != expectedAddr {
		t.Fatalf("Address mismatch: expected %s, got %s", expectedAddr, addr)
	}

	// Legacy 20-byte payload address must now be rejected
	legacy20ByteAddr, err := Bech32Encode(Bech32HRP, make([]byte, 20))
	if err == nil && ValidateAddress(legacy20ByteAddr) {
		t.Fatalf("ValidateAddress should reject legacy 20-byte payload address: %s", legacy20ByteAddr)
	}
}

// TestP2_TransactionSignAndVerifyMLDSA65 verifies transaction signing and verification with ML-DSA-65.
func TestP2_TransactionSignAndVerifyMLDSA65(t *testing.T) {
	sk, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, sk.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress sender failed: %v", err)
	}

	skRecip, _, _ := pqsig.GenerateKey()
	recipAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, skRecip.PublicKeyBytes())

	tx := Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    100000000,
		Fee:       1000,
		Nonce:     1,
	}

	if err := tx.Sign(sk); err != nil {
		t.Fatalf("tx.Sign failed: %v", err)
	}

	if tx.SigAlg != pqsig.SigAlgMLDSA65 {
		t.Fatalf("Expected tx.SigAlg == 1, got %d", tx.SigAlg)
	}
	if len(tx.SenderPub) != 2*pqsig.PublicKeySize {
		t.Fatalf("Expected SenderPub len %d, got %d", 2*pqsig.PublicKeySize, len(tx.SenderPub))
	}
	if len(tx.Signature) != 2*pqsig.SignatureSize {
		t.Fatalf("Expected Signature len %d, got %d", 2*pqsig.SignatureSize, len(tx.Signature))
	}

	if !tx.Verify() {
		t.Fatalf("tx.Verify failed on valid signed ML-DSA transaction")
	}

	// Negative tests:
	// 1. Wrong SigAlg
	txWrongAlg := tx
	txWrongAlg.SigAlg = 0x02
	if txWrongAlg.Verify() {
		t.Fatalf("tx.Verify accepted invalid SigAlg")
	}

	// 2. Corrupted Signature
	txCorruptSig := tx
	sigBytes, _ := hex.DecodeString(txCorruptSig.Signature)
	sigBytes[100] ^= 0x01
	txCorruptSig.Signature = hex.EncodeToString(sigBytes)
	if txCorruptSig.Verify() {
		t.Fatalf("tx.Verify accepted corrupted signature")
	}

	// 3. Signature size mismatch
	txTruncSig := tx
	txTruncSig.Signature = tx.Signature[:len(tx.Signature)-4]
	if txTruncSig.Verify() {
		t.Fatalf("tx.Verify accepted truncated signature")
	}

	// 4. Sender address does not match public key
	txMismatchSender := tx
	txMismatchSender.Sender = recipAddr
	if txMismatchSender.Verify() {
		t.Fatalf("tx.Verify accepted mismatched sender address")
	}
}
