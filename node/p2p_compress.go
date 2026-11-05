package node

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/acxxcxx/armax-coin/core"
)

const (
	encDeflate = "deflate"
	// maxInflatedRangeBytes bounds decompressed block-range payloads (zip-bomb guard).
	maxInflatedRangeBytes = 32 << 20
	// minCompressSavings: compressed (base64) form must be at least 10% smaller to be used.
	minCompressSavingsDiv = 10
)

// p2pCompressionEnabled controls whether this node asks peers for compressed block ranges.
// Set ARMX_P2P_COMPRESSION=0 to disable; serving compressed ranges always works when asked.
func p2pCompressionEnabled() bool {
	return os.Getenv("ARMX_P2P_COMPRESSION") != "0"
}

func acceptsDeflate(accept []string) bool {
	for _, a := range accept {
		if a == encDeflate {
			return true
		}
	}
	return false
}

// compressBlocks returns base64(DEFLATE(JSON(blocks))), or "" if compression is not worthwhile.
// HuffmanOnly is tried as well because hex hashes and signatures give LZ77 no matches.
func compressBlocks(blocks []core.Block) string {
	plain, err := json.Marshal(blocks)
	if err != nil {
		return ""
	}
	var best []byte
	for _, level := range []int{flate.BestSpeed, flate.HuffmanOnly} {
		var buf bytes.Buffer
		w, err := flate.NewWriter(&buf, level)
		if err != nil {
			continue
		}
		if _, err := w.Write(plain); err != nil {
			continue
		}
		if err := w.Close(); err != nil {
			continue
		}
		if best == nil || buf.Len() < len(best) {
			best = buf.Bytes()
		}
	}
	if best == nil {
		return ""
	}
	z := base64.StdEncoding.EncodeToString(best)
	if len(z) >= len(plain)-len(plain)/minCompressSavingsDiv {
		return ""
	}
	return z
}

// expandBlocks fills r.Blocks from a compressed response; plain responses are left untouched.
func (r *BlockRangeResponse) expandBlocks() error {
	if r.Z == "" && r.Enc == "" {
		return nil
	}
	if r.Enc != encDeflate {
		return fmt.Errorf("unsupported block range encoding %q", r.Enc)
	}
	raw, err := base64.StdEncoding.DecodeString(r.Z)
	if err != nil {
		return fmt.Errorf("invalid compressed block range: %w", err)
	}
	fr := flate.NewReader(bytes.NewReader(raw))
	defer fr.Close()
	plain, err := io.ReadAll(io.LimitReader(fr, maxInflatedRangeBytes+1))
	if err != nil {
		return fmt.Errorf("inflate block range: %w", err)
	}
	if len(plain) > maxInflatedRangeBytes {
		return fmt.Errorf("compressed block range exceeds %d bytes inflated", maxInflatedRangeBytes)
	}
	var blocks []core.Block
	if err := json.Unmarshal(plain, &blocks); err != nil {
		return fmt.Errorf("decode compressed block range: %w", err)
	}
	r.Blocks, r.Enc, r.Z = blocks, "", ""
	return nil
}
