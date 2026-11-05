package node

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sampleBlocks(n int, withTx bool) []core.Block {
	blocks := make([]core.Block, n)
	for i := range blocks {
		b := core.Block{
			Index: i + 1, Timestamp: 1790000000 + int64(i)*60, PrevHash: randHex(32), MerkleRoot: randHex(32),
			Hash: "0000000" + randHex(28), Nonce: int64(i) * 7919, Miner: "armx1" + randHex(29), Difficulty: 7,
			Target: "0000000f" + strings.Repeat("f", 56),
			Transactions: []core.Transaction{{
				Sender: "SYSTEM", Recipient: "armx1" + randHex(29), Amount: 6000000000,
				Signature: "REWARD_SIG", Type: "COINBASE", Nonce: int64(i + 1),
			}},
		}
		if withTx {
			b.Transactions = append(b.Transactions, core.Transaction{
				Sender: "armx1" + randHex(29), Recipient: "armx1" + randHex(29), Amount: 100000000,
				SenderPub: randHex(1952), Signature: randHex(3309), SigAlg: 1, Nonce: 5,
			})
		}
		blocks[i] = b
	}
	return blocks
}

func TestBlockRangeCompressionRoundTrip(t *testing.T) {
	for _, withTx := range []bool{false, true} {
		blocks := sampleBlocks(50, withTx)
		z := compressBlocks(blocks)
		if z == "" {
			t.Fatalf("withTx=%v: expected compression to be worthwhile", withTx)
		}
		plain, _ := json.Marshal(blocks)
		t.Logf("withTx=%v plain=%d B wire(base64 deflate)=%d B (%.0f%%)", withTx, len(plain), len(z), 100*float64(len(z))/float64(len(plain)))

		resp := BlockRangeResponse{FromIndex: 1, ToIndex: 50, Enc: encDeflate, Z: z}
		wire, _ := json.Marshal(resp)
		var got BlockRangeResponse
		if err := json.Unmarshal(wire, &got); err != nil {
			t.Fatal(err)
		}
		if err := got.expandBlocks(); err != nil {
			t.Fatal(err)
		}
		again, _ := json.Marshal(got.Blocks)
		if !bytes.Equal(plain, again) {
			t.Fatal("blocks differ after compress/expand")
		}
	}
}

func TestBlockRangeCompressionSkippedWhenNotWorthwhile(t *testing.T) {
	if z := compressBlocks(nil); z != "" {
		t.Fatalf("empty range must stay plain, got %d bytes", len(z))
	}
}

func TestBlockRangeCompatibility(t *testing.T) {
	// A v1.0.6 requester sends no "accept" and must get plain blocks; a v1.0.7 requester talking to
	// a v1.0.6 responder gets plain blocks, which expandBlocks leaves untouched.
	var req BlockRangeRequest
	if err := json.Unmarshal([]byte(`{"from_index":1,"to_index":5}`), &req); err != nil || acceptsDeflate(req.Accept) {
		t.Fatalf("legacy request must not accept deflate: %v %+v", err, req)
	}
	var plain BlockRangeResponse
	if err := json.Unmarshal([]byte(`{"from_index":1,"to_index":1,"blocks":[{"index":1}]}`), &plain); err != nil {
		t.Fatal(err)
	}
	if err := plain.expandBlocks(); err != nil || len(plain.Blocks) != 1 {
		t.Fatalf("plain response must pass through: %v %+v", err, plain)
	}
	// A v1.0.6 decoder (no enc/z fields) must tolerate the new fields in a request/response.
	type legacyReq struct {
		FromIndex int `json:"from_index"`
		ToIndex   int `json:"to_index"`
	}
	data, _ := json.Marshal(BlockRangeRequest{FromIndex: 1, ToIndex: 2, Accept: []string{encDeflate}})
	var lr legacyReq
	if err := json.Unmarshal(data, &lr); err != nil || lr.ToIndex != 2 {
		t.Fatalf("legacy decoder failed: %v %+v", err, lr)
	}
}

func TestBlockRangeInflateBounds(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write([]byte("["))
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	for i := 0; i < maxInflatedRangeBytes>>20+2; i++ {
		_, _ = w.Write(chunk)
	}
	_, _ = w.Write([]byte("]"))
	_ = w.Close()
	r := BlockRangeResponse{Enc: encDeflate, Z: base64.StdEncoding.EncodeToString(buf.Bytes())}
	if err := r.expandBlocks(); err == nil {
		t.Fatal("oversized inflate must be rejected")
	}
	for _, bad := range []BlockRangeResponse{
		{Enc: "gzip", Z: "AAAA"},
		{Enc: encDeflate, Z: "!!!notbase64"},
		{Enc: encDeflate, Z: base64.StdEncoding.EncodeToString([]byte("garbage"))},
	} {
		if err := bad.expandBlocks(); err == nil {
			t.Fatalf("bad payload %+v accepted", bad.Enc)
		}
	}
}

func TestPushBlocksCompressedPayloadExpansion(t *testing.T) {
	blocks := sampleBlocks(10, false)
	z := compressBlocks(blocks)
	if z == "" {
		t.Fatal("expected compression to succeed")
	}

	pr := BlockRangeResponse{
		FromIndex: 1,
		ToIndex:   10,
		Enc:       encDeflate,
		Z:         z,
	}

	payload, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var receivedPR BlockRangeResponse
	if err := json.Unmarshal(payload, &receivedPR); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(receivedPR.Blocks) != 0 {
		t.Fatalf("expected wire blocks slice to be empty before expansion, got %d", len(receivedPR.Blocks))
	}

	if err := receivedPR.expandBlocks(); err != nil {
		t.Fatalf("expandBlocks failed: %v", err)
	}
	if len(receivedPR.Blocks) != 10 {
		t.Fatalf("expected 10 blocks after expansion, got %d", len(receivedPR.Blocks))
	}
	if receivedPR.Blocks[0].Index != 1 || receivedPR.Blocks[9].Index != 10 {
		t.Fatalf("block indices mismatch: first=%d, last=%d", receivedPR.Blocks[0].Index, receivedPR.Blocks[9].Index)
	}
}
