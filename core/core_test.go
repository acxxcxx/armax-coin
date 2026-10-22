package core

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestAddressAndBech32(t *testing.T) {
	pubBytes := make([]byte, 64)
	_, _ = rand.Read(pubBytes)
	h := sha256.Sum256(pubBytes)

	bech32Addr, err := Bech32Encode(Bech32HRP, h[:AddressPayloadLen])
	if err != nil {
		t.Fatalf("Bech32Encode failed: %v", err)
	}
	if len(bech32Addr) < 5 || bech32Addr[:5] != "armx1" {
		t.Fatalf("Expected armx1 prefix, got: %s", bech32Addr)
	}

	decodedHrp, decodedData, err := Bech32Decode(bech32Addr)
	if err != nil || decodedHrp != Bech32HRP {
		t.Fatalf("Bech32Decode failed: %v", err)
	}
	if len(decodedData) != AddressPayloadLen {
		t.Fatalf("Expected %d decoded bytes, got %d", AddressPayloadLen, len(decodedData))
	}
}

func TestTransactionSignatureAndVerify(t *testing.T) {
	accSender := newTestAccount(t, "sender")
	privKey, pubHex, senderAddr := accSender.PrivKey, accSender.PubHex, accSender.Address
	recipientAddr := newTestAccount(t, "recip").Address

	tx := Transaction{
		Sender:    senderAddr,
		SenderPub: pubHex,
		Recipient: recipientAddr,
		Amount:    50,
		Fee:       1,
		Nonce:     0,
	}

	if err := tx.Sign(privKey); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if !tx.Verify() {
		t.Fatalf("Verify failed for valid transaction")
	}
}

func TestLWMAAdjustment(t *testing.T) {
	bc := &Blockchain{
		Chain:      []Block{},
		Difficulty: 3,
	}

	// 70 blocks with 5-second intervals (6x faster than target 30s)
	now := time.Now().Unix() - 70*5
	for i := 0; i < 70; i++ {
		b := Block{
			Index:      i,
			Timestamp:  now + int64(i*5),
			Difficulty: 3,
			Target:     fmt.Sprintf("%064x", DifficultyToTarget(3)),
		}
		bc.Chain = append(bc.Chain, b)
	}

	diff := bc.AdjustDifficulty()
	if diff <= 3 {
		t.Fatalf("Expected difficulty to increase above 3 for fast blocks, got: %d", diff)
	}
}
