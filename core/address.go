package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	// Bech32HRP is the canonical human-readable prefix for Armaxcoin addresses (BIP-173 standard)
	Bech32HRP = "armx"

	// AddressPayloadLen is the exact byte length of the address preimage hash (SHA-256 = 32 bytes) [P2-2]
	AddressPayloadLen = 32

	bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
)

var bech32Gen = []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

func bech32Polymod(values []int) int {
	chk := 1
	for _, v := range values {
		b := chk >> 25
		chk = ((chk & 0x1ffffff) << 5) ^ v
		for i := 0; i < 5; i++ {
			if (b>>i)&1 == 1 {
				chk ^= bech32Gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []int {
	var ret []int
	for _, c := range hrp {
		ret = append(ret, int(c>>5))
	}
	ret = append(ret, 0)
	for _, c := range hrp {
		ret = append(ret, int(c&31))
	}
	return ret
}

func bech32VerifyChecksum(hrp string, data []int) bool {
	return bech32Polymod(append(bech32HrpExpand(hrp), data...)) == 1
}

func bech32CreateChecksum(hrp string, data []int) []int {
	values := append(bech32HrpExpand(hrp), data...)
	values = append(values, []int{0, 0, 0, 0, 0, 0}...)
	mod := bech32Polymod(values) ^ 1
	var ret []int
	for i := 0; i < 6; i++ {
		ret = append(ret, (mod>>uint(5*(5-i)))&31)
	}
	return ret
}

func convertBits(data []byte, frombits, tobits uint, pad bool) ([]int, error) {
	acc := 0
	bits := uint(0)
	var ret []int
	maxv := (1 << tobits) - 1
	for _, value := range data {
		acc = (acc << frombits) | int(value)
		bits += frombits
		for bits >= tobits {
			bits -= tobits
			ret = append(ret, (acc>>bits)&maxv)
		}
	}
	if pad {
		if bits > 0 {
			ret = append(ret, (acc<<(tobits-bits))&maxv)
		}
	} else if bits >= frombits || ((acc<<(tobits-bits))&maxv) != 0 {
		return nil, fmt.Errorf("invalid bits padding")
	}
	return ret, nil
}

func convertBitsFrom5(data []int, frombits, tobits uint, pad bool) ([]byte, error) {
	acc := 0
	bits := uint(0)
	var ret []byte
	maxv := (1 << tobits) - 1
	for _, value := range data {
		if value < 0 || (value>>frombits) != 0 {
			return nil, fmt.Errorf("invalid data value")
		}
		acc = (acc << frombits) | value
		bits += frombits
		for bits >= tobits {
			bits -= tobits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}
	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(tobits-bits))&maxv))
		}
	} else if bits >= frombits || ((acc<<(tobits-bits))&maxv) != 0 {
		return nil, fmt.Errorf("invalid bits padding")
	}
	return ret, nil
}

// Bech32Encode encodes a byte payload into a BIP-173 Bech32 address with the specified HRP
func Bech32Encode(hrp string, payload []byte) (string, error) {
	data, err := convertBits(payload, 8, 5, true)
	if err != nil {
		return "", err
	}
	combined := append(data, bech32CreateChecksum(hrp, data)...)
	var sb strings.Builder
	sb.WriteString(strings.ToLower(hrp))
	sb.WriteString("1")
	for _, d := range combined {
		sb.WriteByte(bech32Charset[d])
	}
	return sb.String(), nil
}

// Bech32Decode decodes and validates the checksum of a BIP-173 Bech32 address.
// Rejects uppercase and mixed-case strings [C-20].
func Bech32Decode(bech string) (string, []byte, error) {
	if bech != strings.ToLower(bech) {
		return "", nil, fmt.Errorf("bech32 address must be strictly lowercase")
	}
	pos := strings.LastIndex(bech, "1")
	if pos < 1 || pos+7 > len(bech) {
		return "", nil, fmt.Errorf("invalid bech32 separator or length")
	}

	hrp := bech[:pos]
	dataStr := bech[pos+1:]

	var data []int
	for _, c := range dataStr {
		d := strings.IndexRune(bech32Charset, c)
		if d == -1 {
			return "", nil, fmt.Errorf("invalid bech32 character: %c", c)
		}
		data = append(data, d)
	}

	if !bech32VerifyChecksum(hrp, data) {
		return "", nil, fmt.Errorf("invalid bech32 checksum (typo detected)")
	}

	payload5 := data[:len(data)-6]
	decoded, err := convertBitsFrom5(payload5, 5, 8, false)
	if err != nil {
		return "", nil, err
	}

	return hrp, decoded, nil
}

// ValidateAddress strictly verifies that an address is a canonical lowercase Bech32 address
// with HRP 'armx' and an AddressPayloadLen (32 bytes) payload [P2-2].
func ValidateAddress(address string) bool {
	if address == "" || strings.TrimSpace(address) != address || address != strings.ToLower(address) {
		return false
	}
	if !strings.HasPrefix(address, Bech32HRP+"1") || len(address) > 90 {
		return false
	}
	hrp, payload, err := Bech32Decode(address)
	return err == nil && hrp == Bech32HRP && len(payload) == AddressPayloadLen
}

// PubToAddress derives the canonical Bech32 address (starting with "armx1") from SigAlg and raw public key bytes:
// payload = SHA-256(SigAlg || publicKey) [P2-2].
func PubToAddress(sigAlg byte, pubBytes []byte) (string, error) {
	if len(pubBytes) == 0 {
		return "", errors.New("public key cannot be empty")
	}
	preimage := append([]byte{sigAlg}, pubBytes...)
	hash := sha256.Sum256(preimage)
	return Bech32Encode(Bech32HRP, hash[:])
}

// PubHexToAddress derives the canonical Bech32 address from a hex-encoded public key.
// Backward compatibility bridge for wallet migration [P2-2/P2-3].
func PubHexToAddress(pubHex string) (string, error) {
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil {
		return "", err
	}
	// By default uses ML-DSA-65 (0x01)
	return PubToAddress(0x01, pubBytes)
}

// HexToAddress converts a 64-character hex pubkey hash to a canonical Bech32 address (armx1...)
func HexToAddress(hexAddr string) (string, error) {
	rawBytes, err := hex.DecodeString(hexAddr)
	if err != nil || len(rawBytes) != AddressPayloadLen {
		return "", fmt.Errorf("invalid hex address: must be exactly %d hex characters (%d bytes)", 2*AddressPayloadLen, AddressPayloadLen)
	}
	return Bech32Encode(Bech32HRP, rawBytes)
}

// AddressToHex converts a canonical Bech32 address back to its 64-character lowercase hex pubkey hash.
// Strictly requires exact lowercase and AddressPayloadLen (32 bytes) payload [P2-2].
func AddressToHex(addr string) (string, error) {
	if addr == "" || strings.TrimSpace(addr) != addr || addr != strings.ToLower(addr) {
		return "", errors.New("address must be strictly lowercase bech32 without leading or trailing spaces")
	}
	if !strings.HasPrefix(addr, Bech32HRP+"1") || len(addr) > 90 {
		return "", errors.New("address must begin with canonical hrp 'armx1' and not exceed 90 characters")
	}
	hrp, payload, err := Bech32Decode(addr)
	if err != nil {
		return "", fmt.Errorf("invalid bech32 address: %w", err)
	}
	if hrp != Bech32HRP {
		return "", fmt.Errorf("invalid hrp: expected %s, got %s", Bech32HRP, hrp)
	}
	if len(payload) != AddressPayloadLen {
		return "", fmt.Errorf("invalid payload length: expected %d bytes, got %d", AddressPayloadLen, len(payload))
	}
	return hex.EncodeToString(payload), nil
}

var (
	// addrHexCache caches validated address string to lowercase hex conversions [C-15]
	addrHexCache sync.Map
)

// AddressToHexCached provides thread-safe O(1) cached lookup of canonical 40-character hex addresses [C-15]
func AddressToHexCached(addr string) (string, error) {
	if addr == "" || addr != strings.ToLower(addr) {
		return "", errors.New("address must be strictly lowercase bech32")
	}
	if val, ok := addrHexCache.Load(addr); ok {
		return val.(string), nil
	}
	hexStr, err := AddressToHex(addr)
	if err == nil {
		addrHexCache.Store(addr, hexStr)
	}
	return hexStr, err
}

// MatchAddress compares two address strings for equality strictly under canonical Bech32 rules [C-20]
func MatchAddress(a, b string) bool {
	if a == "" || b == "" || a != strings.ToLower(a) || b != strings.ToLower(b) {
		return false
	}
	hexA, errA := AddressToHexCached(a)
	hexB, errB := AddressToHexCached(b)
	if errA == nil && errB == nil {
		return hexA == hexB
	}
	return false
}
