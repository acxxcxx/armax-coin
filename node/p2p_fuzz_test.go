package node

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzP2PMessageParser verifies that inbound P2P packet parsing and bounded line readers
// safely handle arbitrary untrusted byte sequences without panics [Security Audit]
func FuzzP2PMessageParser(f *testing.F) {
	msg := Message{
		NetworkID: "ARMX-MAINNET-1",
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}
	msgData, _ := json.Marshal(msg)
	f.Add(msgData)
	f.Add([]byte(`{"network_id":"ARMX-TESTNET-1","type":"NEW_BLOCK","payload":{"index":1}}`))
	f.Add([]byte(`{"type":"UNKNOWN","payload":null}`))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("not json at all\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test rawInboundMessage unmarshaling
		var inbound rawInboundMessage
		_ = json.Unmarshal(data, &inbound)

		// Test readBoundedLine safety
		reader := bufio.NewReader(bytes.NewReader(data))
		_, _ = readBoundedLine(reader, 1024)
	})
}
