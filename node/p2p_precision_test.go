package node

import (
	"encoding/json"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestP2PMessagePrecision verifies that 64-bit integer values > 2^53 (e.g. Stratum nonces,
// high satoshi balances, nanosecond timestamps) are preserved through P2P Message
// unmarshaling without IEEE-754 double precision float64 truncation.
func TestP2PMessagePrecision(t *testing.T) {
	// Block #4878 live testnet mined nonce: 1071342827189980452 (> 2^53 = 9007199254740992)
	const minedNonce = int64(1071342827189980452)

	origBlock := core.Block{
		Index: 4878,
		Nonce: minedNonce,
		Hash:  "0000002d1d17760018541593ca8f22578e269d147806e554a7e4899617fee2c4",
	}

	rangeResp := BlockRangeResponse{
		FromIndex: 4878,
		ToIndex:   4878,
		Blocks:    []core.Block{origBlock},
	}

	msg := Message{
		NetworkID: "ARMX-TESTNET-1",
		Type:      "BLOCK_RANGE_RESPONSE",
		Payload:   rangeResp,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal(msg) failed: %v", err)
	}

	// Unmarshal into generic Message (as done by QueryPeer and QueryP2P)
	var decodedMsg Message
	if err := json.Unmarshal(msgBytes, &decodedMsg); err != nil {
		t.Fatalf("json.Unmarshal(msg) failed: %v", err)
	}

	payloadBytes, err := decodedMsg.PayloadBytes()
	if err != nil {
		t.Fatalf("decodedMsg.PayloadBytes() failed: %v", err)
	}

	var decodedRange BlockRangeResponse
	if err := json.Unmarshal(payloadBytes, &decodedRange); err != nil {
		t.Fatalf("json.Unmarshal(payloadBytes) failed: %v", err)
	}

	if len(decodedRange.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(decodedRange.Blocks))
	}

	gotNonce := decodedRange.Blocks[0].Nonce
	if gotNonce != minedNonce {
		t.Fatalf("64-bit integer precision loss: expected %d, got %d (diff: %d)", minedNonce, gotNonce, gotNonce-minedNonce)
	}
}
