package core

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// Store record compression (v1.0.7, optional, default off).
//
// A stored record is either plain JSON (what v1.0.6 writes, always starts with '{') or a
// compressed record: recordMarkerFlate followed by a raw DEFLATE stream of the same JSON.
// Readers accept both forms at all times, so enabling or disabling compression never makes an
// existing database unreadable by v1.0.7. v1.0.6 cannot read compressed records; see
// DecompressStore for the downgrade path.
const (
	recordMarkerFlate byte = 0x01
	// Records below this size are left plain: the saving is negligible and small records are
	// the hot read path.
	recordCompressMinSize = 512
)

var storeCompression atomic.Bool

func init() {
	if os.Getenv("ARMX_STORE_COMPRESSION") == "1" {
		storeCompression.Store(true)
	}
}

// SetStoreCompression turns compression of newly written records on or off.
func SetStoreCompression(on bool) { storeCompression.Store(on) }

// StoreCompressionEnabled reports whether new records are written compressed.
func StoreCompressionEnabled() bool { return storeCompression.Load() }

// encodeRecord returns the bytes to store for a JSON record.
func encodeRecord(jsonData []byte) []byte {
	if !storeCompression.Load() || len(jsonData) < recordCompressMinSize {
		return jsonData
	}
	best := jsonData
	// LZ77+Huffman suits structured JSON (state snapshots, undo records); Huffman-only suits the
	// hex-encoded signatures and keys, where LZ77 finds no matches and can expand the data.
	for _, level := range []int{flate.BestSpeed, flate.HuffmanOnly} {
		var buf bytes.Buffer
		buf.WriteByte(recordMarkerFlate)
		w, err := flate.NewWriter(&buf, level)
		if err != nil {
			continue
		}
		if _, err := w.Write(jsonData); err != nil {
			continue
		}
		if err := w.Close(); err != nil {
			continue
		}
		if buf.Len() < len(best) {
			best = buf.Bytes()
		}
	}
	// Keep the compressed form only if it saves at least 10 percent.
	if len(best) >= len(jsonData)-len(jsonData)/10 {
		return jsonData
	}
	return best
}

// unmarshalRecord decodes a stored record (plain or compressed JSON) into v.
func unmarshalRecord(data []byte, v interface{}) error {
	raw, err := decodeRecord(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// maxDecodedRecordSize bounds decompression so a corrupt or hostile file cannot exhaust memory.
const maxDecodedRecordSize = 256 << 20

// decodeRecord returns the JSON bytes for a stored record, whether plain or compressed.
// The returned slice must not be modified and is only valid inside the bbolt transaction when
// the record is plain.
func decodeRecord(data []byte) ([]byte, error) {
	if len(data) == 0 || data[0] != recordMarkerFlate {
		return data, nil
	}
	r := flate.NewReader(bytes.NewReader(data[1:]))
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxDecodedRecordSize+1))
	if err != nil {
		return nil, fmt.Errorf("decompress stored record: %w", err)
	}
	if len(out) > maxDecodedRecordSize {
		return nil, fmt.Errorf("stored record exceeds %d bytes after decompression", maxDecodedRecordSize)
	}
	return out, nil
}
