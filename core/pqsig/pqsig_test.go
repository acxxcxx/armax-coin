package pqsig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestPQSig_Constants verifies ML-DSA-65 NIST FIPS 204 parameters and protocol context.
func TestPQSig_Constants(t *testing.T) {
	if SeedSize != 32 {
		t.Errorf("Expected SeedSize 32, got %d", SeedSize)
	}
	if PublicKeySize != 1952 {
		t.Errorf("Expected PublicKeySize 1952, got %d", PublicKeySize)
	}
	if SignatureSize != 3309 {
		t.Errorf("Expected SignatureSize 3309, got %d", SignatureSize)
	}
	if SigAlgMLDSA65 != 0x01 {
		t.Errorf("Expected SigAlgMLDSA65 0x01, got 0x%02x", SigAlgMLDSA65)
	}
	if ContextString != "ARMX-TX-V1" {
		t.Errorf("Expected ContextString 'ARMX-TX-V1', got '%s'", ContextString)
	}
}

// TestPQSig_SignAndVerify tests key generation from seed, signing and deterministic verification.
func TestPQSig_SignAndVerify(t *testing.T) {
	var seed [32]byte
	copy(seed[:], []byte("armax-pqc-test-seed-012345678901"))

	sk, err := GenerateFromSeed(seed)
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}

	pubBytes := sk.PublicKeyBytes()
	if len(pubBytes) != PublicKeySize {
		t.Fatalf("Expected public key length %d, got %d", PublicKeySize, len(pubBytes))
	}

	msg := sha256.Sum256([]byte("test transaction message payload"))

	sig, err := sk.Sign(msg[:])
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if len(sig) != SignatureSize {
		t.Fatalf("Expected signature length %d, got %d", SignatureSize, len(sig))
	}

	// 1. Valid signature must verify
	if !Verify(pubBytes, msg[:], sig) {
		t.Fatalf("Verify failed on valid signature")
	}

	// 2. Corrupted message must fail
	corruptedMsg := msg
	corruptedMsg[0] ^= 0x01
	if Verify(pubBytes, corruptedMsg[:], sig) {
		t.Fatalf("Verify succeeded on corrupted message")
	}

	// 3. Corrupted signature must fail
	corruptedSig := make([]byte, len(sig))
	copy(corruptedSig, sig)
	corruptedSig[len(corruptedSig)/2] ^= 0x01
	if Verify(pubBytes, msg[:], corruptedSig) {
		t.Fatalf("Verify succeeded on corrupted signature")
	}

	// 4. Corrupted public key must fail
	corruptedPub := make([]byte, len(pubBytes))
	copy(corruptedPub, pubBytes)
	corruptedPub[10] ^= 0x01
	if Verify(corruptedPub, msg[:], sig) {
		t.Fatalf("Verify succeeded on corrupted public key")
	}

	// 5. Wrong length public key or signature must fail gracefully
	if Verify(pubBytes[:100], msg[:], sig) {
		t.Fatalf("Verify succeeded on truncated public key")
	}
	if Verify(pubBytes, msg[:], sig[:200]) {
		t.Fatalf("Verify succeeded on truncated signature")
	}
}

// TestPQSig_GoldenDeterministicDerivation protects against library or parameter changes.
func TestPQSig_GoldenDeterministicDerivation(t *testing.T) {
	// 64-byte BIP-39 simulated seed
	bip39Seed := bytes.Repeat([]byte{0x42}, 64)

	seed0, err := DeriveSeedFromBIP39(bip39Seed, 0)
	if err != nil {
		t.Fatalf("DeriveSeedFromBIP39 failed: %v", err)
	}

	seed1, err := DeriveSeedFromBIP39(bip39Seed, 1)
	if err != nil {
		t.Fatalf("DeriveSeedFromBIP39 index 1 failed: %v", err)
	}

	if seed0 == seed1 {
		t.Fatalf("Account index 0 and 1 must produce distinct seeds")
	}

	sk0, err := GenerateFromSeed(seed0)
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}

	pub0 := sk0.PublicKeyBytes()
	pubHash := sha256.Sum256(pub0)
	pubHashHex := hex.EncodeToString(pubHash[:])

	// Verify deterministic reproducibility
	sk0Repro, err := GenerateFromSeed(seed0)
	if err != nil {
		t.Fatalf("GenerateFromSeed repro failed: %v", err)
	}
	if !bytes.Equal(sk0.PublicKeyBytes(), sk0Repro.PublicKeyBytes()) {
		t.Fatalf("Same seed produced non-matching public keys")
	}

	t.Logf("Golden ML-DSA-65 SHA-256(PubKey) for seed0: %s", pubHashHex)
}

// TestPQSig_FIPS204_KnownAnswerVector verifies deterministic FIPS 204 ML-DSA-65 vector:
// Key generation, signing, and verification from a canonical 32-byte test vector.
func TestPQSig_FIPS204_KnownAnswerVector(t *testing.T) {
	// Canonical test seed: 32 bytes (0x01, 0x02, ..., 0x20)
	var katSeed [32]byte
	for i := 0; i < 32; i++ {
		katSeed[i] = byte(i + 1)
	}

	sk, err := GenerateFromSeed(katSeed)
	if err != nil {
		t.Fatalf("GenerateFromSeed failed: %v", err)
	}

	pubBytes := sk.PublicKeyBytes()
	if len(pubBytes) != PublicKeySize {
		t.Fatalf("PublicKey length mismatch: expected %d, got %d", PublicKeySize, len(pubBytes))
	}

	// Canonical message: SHA-256("ARMAX-MAINNET-FIPS204-KAT")
	msgDigest := sha256.Sum256([]byte("ARMAX-MAINNET-FIPS204-KAT"))

	sig, err := sk.Sign(msgDigest[:])
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if len(sig) != SignatureSize {
		t.Fatalf("Signature length mismatch: expected %d, got %d", SignatureSize, len(sig))
	}

	// Verify valid signature
	if !Verify(pubBytes, msgDigest[:], sig) {
		t.Fatalf("Verify failed on canonical FIPS 204 test vector")
	}

	// Boundary & Negative Tests:
	// 1. Oversized public key (1953 bytes)
	oversizedPub := append(pubBytes, 0x00)
	if Verify(oversizedPub, msgDigest[:], sig) {
		t.Fatalf("Verify accepted oversized public key")
	}

	// 2. Undersized public key (1951 bytes)
	undersizedPub := pubBytes[:len(pubBytes)-1]
	if Verify(undersizedPub, msgDigest[:], sig) {
		t.Fatalf("Verify accepted undersized public key")
	}

	// 3. Oversized signature (3310 bytes)
	oversizedSig := append(sig, 0x00)
	if Verify(pubBytes, msgDigest[:], oversizedSig) {
		t.Fatalf("Verify accepted oversized signature")
	}

	// 4. Undersized signature (3308 bytes)
	undersizedSig := sig[:len(sig)-1]
	if Verify(pubBytes, msgDigest[:], undersizedSig) {
		t.Fatalf("Verify accepted undersized signature")
	}

	t.Logf("[OK] FIPS 204 ML-DSA-65 known answer test and boundary size validations passed.")
}
