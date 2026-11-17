//go:build testhooks

package core

import (
	"encoding/hex"
	"strings"
	"testing"
)

const c3Recipient = "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"

func c3SignedTx(t *testing.T) (*Transaction, *testAccount) {
	t.Helper()
	acc := newTestAccount(t)
	tx := &Transaction{
		SigAlg:    1,
		Sender:    acc.Address,
		SenderPub: acc.PubHex,
		Recipient: c3Recipient,
		Amount:    1000000,
		Fee:       12000,
		Nonce:     1,
	}
	if err := tx.Sign(acc.PrivKey); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tx, acc
}

// Every committed field must change the digest, so none can be altered after signing.
func TestC3_PreimageCommitsEveryStandardField(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)
	base := tx.HashHex()

	mutations := map[string]func(c *Transaction){
		"sig_alg":   func(c *Transaction) { c.SigAlg = 2 },
		"sender":    func(c *Transaction) { c.Sender = c.Sender[:len(c.Sender)-1] + "q" },
		"sender_pk": func(c *Transaction) { c.SenderPub = "00" + c.SenderPub[2:] },
		"recipient": func(c *Transaction) { c.Recipient = c.Recipient[:len(c.Recipient)-1] + "q" },
		"amount":    func(c *Transaction) { c.Amount++ },
		"fee":       func(c *Transaction) { c.Fee++ },
		"nonce":     func(c *Transaction) { c.Nonce++ },
	}
	for name, mutate := range mutations {
		c := *tx
		mutate(&c)
		if c.HashHex() == base {
			t.Errorf("field %s is not committed by the transaction hash", name)
		}
	}
}

// The signature and cached state are intentionally not part of the digest.
func TestC3_PreimageExcludesSignature(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)
	c := *tx
	c.Signature = strings.Repeat("00", len(tx.Signature)/2)
	if c.HashHex() != tx.HashHex() {
		t.Fatal("signature must not influence the signed digest")
	}
}

// Sender and recipient are lowercased before hashing, so case variants share one digest. The
// address validator decides whether such variants are accepted; this pins the current contract.
func TestC3_PreimageAddressCaseCanonicalization(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)
	up := *tx
	up.Sender = strings.ToUpper(tx.Sender)
	up.Recipient = strings.ToUpper(tx.Recipient)
	if up.HashHex() != tx.HashHex() {
		t.Fatal("expected address case to be canonicalized in the digest")
	}
}

// The delimiter and control characters are rejected before they can shift field boundaries.
func TestC3_ValidateBasicRejectsDelimiterAndControlChars(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)

	bad := []string{":", "\n", "\x00", "\t", "é"}
	for _, ch := range bad {
		c := *tx
		c.Recipient = c3Recipient + ch
		if err := c.ValidateBasic(); err == nil {
			t.Errorf("recipient with %q must be rejected", ch)
		}
		c = *tx
		c.Sender = tx.Sender + ch
		if err := c.ValidateBasic(); err == nil {
			t.Errorf("sender with %q must be rejected", ch)
		}
		c = *tx
		c.Type = ch
		if err := c.ValidateBasic(); err == nil {
			t.Errorf("type %q must be rejected", ch)
		}
	}
}

// Fields only the non-standard hash branch commits must be rejected on STANDARD transactions,
// otherwise they could be altered without invalidating the signature.
func TestC3_StandardTxRejectsUncommittedScriptFields(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)

	cases := map[string]func(c *Transaction){
		"hash_lock": func(c *Transaction) { c.HashLock = strings.Repeat("ab", 32) },
		"time_lock": func(c *Transaction) { c.TimeLock = 1 },
		"preimage":  func(c *Transaction) { c.Preimage = "ab" },
		"type":      func(c *Transaction) { c.Type = "HTLC_LOCK" },
	}
	for name, mutate := range cases {
		c := *tx
		mutate(&c)
		if err := c.VerifyWithError(); err == nil {
			t.Errorf("%s must be rejected on a STANDARD transaction", name)
		}
	}
}

// Verification recomputes the digest for the active network; a different network yields a different
// digest for identical fields and the signature no longer verifies.
func TestC3_VerifyUsesActiveNetworkDigest(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)
	if err := tx.VerifyWithError(); err != nil {
		t.Fatalf("testnet verify: %v", err)
	}
	if tx.HashHexForNetwork("ARMX-TESTNET-1") == tx.HashHexForNetwork("ARMX-MAINNET-1") {
		t.Fatal("digest must differ across network IDs")
	}

	// A fresh copy has no cached verification result.
	fresh := *tx
	fresh.InvalidateCache()
	if err := SetNetwork("mainnet"); err != nil {
		t.Fatal(err)
	}
	if fresh.Verify() {
		t.Fatal("testnet signature must not verify on mainnet")
	}
}

// Malformed signature or key encodings must be rejected without panicking.
func TestC3_MalformedSignatureAndKeyEncodings(t *testing.T) {
	SetNetworkForTesting(true)
	defer SetNetworkForTesting(true)
	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	tx, _ := c3SignedTx(t)
	sig, _ := hex.DecodeString(tx.Signature)

	check := func(name string, mutate func(c *Transaction)) {
		c := *tx
		c.InvalidateCache()
		mutate(&c)
		if c.VerifyWithError() == nil {
			t.Errorf("%s must be rejected", name)
		}
	}

	check("empty signature", func(c *Transaction) { c.Signature = "" })
	check("truncated signature", func(c *Transaction) { c.Signature = tx.Signature[:len(tx.Signature)-2] })
	check("oversized signature", func(c *Transaction) { c.Signature = tx.Signature + "00" })
	check("non-hex signature", func(c *Transaction) { c.Signature = "zz" + tx.Signature[2:] })
	check("truncated public key", func(c *Transaction) { c.SenderPub = tx.SenderPub[:len(tx.SenderPub)-2] })
	check("oversized public key", func(c *Transaction) { c.SenderPub = tx.SenderPub + "00" })
	check("non-hex public key", func(c *Transaction) { c.SenderPub = "zz" + tx.SenderPub[2:] })

	// Flip one bit in several regions of the packed signature.
	for _, pos := range []int{0, 31, 32, len(sig) / 2, len(sig) - 80, len(sig) - 1} {
		mut := append([]byte(nil), sig...)
		mut[pos] ^= 0x01
		check("bit flip at "+hex.EncodeToString([]byte{byte(pos)}), func(c *Transaction) { c.Signature = hex.EncodeToString(mut) })
	}

	// All-zero and all-one signatures.
	check("zero signature", func(c *Transaction) { c.Signature = strings.Repeat("00", len(sig)) })
	check("ff signature", func(c *Transaction) { c.Signature = strings.Repeat("ff", len(sig)) })
}
