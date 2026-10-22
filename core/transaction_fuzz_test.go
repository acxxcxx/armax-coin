package core

import (
	"encoding/json"
	"testing"
)

// FuzzTransactionParser verifies that transaction unmarshaling, hashing, and verification
// never panic on arbitrary byte inputs [Security Audit]
func FuzzTransactionParser(f *testing.F) {
	validTx := Transaction{
		SigAlg:    1,
		Sender:    GenesisMiner,
		SenderPub: "abcd",
		Recipient: GenesisMiner,
		Amount:    1000,
		Fee:       1000,
		Nonce:     1,
		Signature: "beef",
	}
	validBytes, _ := json.Marshal(validTx)
	f.Add(validBytes)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"amount": -99999999999999999}`))
	f.Add([]byte(`{"sender": "invalid", "recipient": "invalid"}`))
	f.Add([]byte(`malformed json string`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var tx Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return
		}
		// Invariant: Calling Hash(), HashHex(), or Size() must never panic
		_ = tx.Hash()
		_ = tx.HashHex()
		_ = tx.Size()
		_ = tx.VerifyWithError()
	})
}
