package wallet

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/acxxcxx/armax-coin/core"
)

const (
	ArmaxcoinAddressVersion byte   = 0x17
	Base58Alphabet          string = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
)

// Base58Encode encodes raw bytes into a Base58 string
func Base58Encode(input []byte) string {
	x := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	zero := big.NewInt(0)
	mod := new(big.Int)

	var result []byte
	for x.Cmp(zero) > 0 {
		x.DivMod(x, base, mod)
		result = append(result, Base58Alphabet[mod.Int64()])
	}

	for _, b := range input {
		if b == 0x00 {
			result = append(result, Base58Alphabet[0])
		} else {
			break
		}
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

// Base58Decode decodes a Base58 string into raw bytes
func Base58Decode(input string) ([]byte, error) {
	base := big.NewInt(58)
	x := new(big.Int)

	for _, c := range input {
		idx := strings.IndexRune(Base58Alphabet, c)
		if idx == -1 {
			return nil, fmt.Errorf("invalid base58 character: %c", c)
		}
		x.Mul(x, base)
		x.Add(x, big.NewInt(int64(idx)))
	}

	decoded := x.Bytes()
	for _, c := range input {
		if c == rune(Base58Alphabet[0]) {
			decoded = append([]byte{0x00}, decoded...)
		} else {
			break
		}
	}
	return decoded, nil
}

// Base58CheckEncode encodes a 20-byte payload with version byte and 4-byte double SHA-256 checksum
func Base58CheckEncode(version byte, payload []byte) string {
	versionedPayload := append([]byte{version}, payload...)
	h1 := sha256.Sum256(versionedPayload)
	h2 := sha256.Sum256(h1[:])
	full := append(versionedPayload, h2[:4]...)
	return Base58Encode(full)
}

// Base58CheckDecode decodes and verifies the 4-byte double-SHA256 checksum of a Base58Check address
func Base58CheckDecode(address string) (byte, []byte, error) {
	decoded, err := Base58Decode(address)
	if err != nil {
		return 0, nil, err
	}

	if len(decoded) != 25 {
		return 0, nil, fmt.Errorf("invalid address length: expected 25 bytes, got %d", len(decoded))
	}

	version := decoded[0]
	payload := decoded[1:21]
	checksum := decoded[21:25]

	h1 := sha256.Sum256(decoded[:21])
	h2 := sha256.Sum256(h1[:])

	for i := 0; i < 4; i++ {
		if h2[i] != checksum[i] {
			return 0, nil, errors.New("address checksum mismatch (typo detected)")
		}
	}

	return version, payload, nil
}

// Re-export core address functions for convenience
var (
	ValidateAddress    = core.ValidateAddress
	HexToBase58Address = core.HexToAddress
	Base58ToHexAddress = core.AddressToHex
)
