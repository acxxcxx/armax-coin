// Package armaxhash implements the ArmaxHash v1 proof-of-work digest.
//
// IMPORTANT: This is a candidate consensus implementation, not a claim of
// independent cryptographic audit or mainnet readiness. Every node and miner
// must use exactly the same header serialization, parameters, and target byte
// order. Changing any of these requires a coordinated consensus upgrade.
package armaxhash

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
)

const (
	// Version is the default ArmaxHash algorithm version.
	Version byte = 1

	// VersionV1 is the memory-hard ArmaxHash v1 consensus algorithm version (Argon2id 2 MB).
	VersionV1 byte = 1

	// VersionV2 is the stream-fast ArmaxHash v2 consensus algorithm version (BLAKE2b Chained).
	VersionV2 byte = 2

	OutputSize = 32
	SaltSize   = 16

	// Fixed consensus parameters for this candidate version.
	// These values must not be changed after deployment without a consensus upgrade.
	MemoryKiB uint32 = 2048 // 2 MiB
	TimeCost  uint32 = 2
	Threads   uint8  = 1

	// Header serialization should be much smaller than this. This is a defensive
	// bound against accidental/untrusted oversized inputs, not a block-size rule.
	MaxHeaderSize = 1 << 20 // 1 MiB
)

var (
	ErrEmptyHeader    = errors.New("armaxhash: empty block header")
	ErrHeaderTooLarge = errors.New("armaxhash: block header exceeds maximum size")
	ErrInvalidNet     = errors.New("armaxhash: invalid network ID (must be 1..255 bytes)")
	ErrInvalidVer     = errors.New("armaxhash: unsupported algorithm version")
	ErrInvalidHash    = errors.New("armaxhash: hash must be exactly 32 bytes")
	ErrInvalidTgt     = errors.New("armaxhash: target must be exactly 32 bytes")
)

// Context preserves the API used by the earlier arena implementation.
// It has no mutable state: each call delegates to the audited Argon2id
// implementation in golang.org/x/crypto. Concurrent calls are safe.
type Context struct{}

// NewContext creates a stateless ArmaxHash context.
func NewContext() *Context { return &Context{} }

// Hash computes an ArmaxHash v1 digest. header must be the canonical serialized
// block header, including the nonce. networkID is part of domain separation.
func Hash(header []byte, networkID string) ([OutputSize]byte, error) {
	return HashVersion(header, networkID, VersionV1)
}

// HashV2 computes an ArmaxHash v2 digest using high-throughput chained BLAKE2b.
// Construction:
//  1. D1 = BLAKE2b-512("ARMX-POW-V2\x00" || VersionV2 || len(networkID) || networkID || header)
//  2. Digest = BLAKE2b-256(D1)
func HashV2(header []byte, networkID string) ([OutputSize]byte, error) {
	return NewContext().HashV2(header, networkID)
}

// HashVersion computes the digest for an explicitly requested algorithm version.
// Consensus validation must reject versions not explicitly supported.
func HashVersion(header []byte, networkID string, version byte) ([OutputSize]byte, error) {
	return NewContext().HashVersion(header, networkID, version)
}

// Hash computes an ArmaxHash v1 digest.
func (ctx *Context) Hash(header []byte, networkID string) ([OutputSize]byte, error) {
	return ctx.HashVersion(header, networkID, VersionV1)
}

// HashV2 computes ArmaxHash v2 using domain-separated BLAKE2b-512 followed by BLAKE2b-256.
func (ctx *Context) HashV2(header []byte, networkID string) ([OutputSize]byte, error) {
	var result [OutputSize]byte

	if len(header) == 0 {
		return result, ErrEmptyHeader
	}
	if len(header) > MaxHeaderSize {
		return result, ErrHeaderTooLarge
	}
	if len(networkID) == 0 || len(networkID) > 255 {
		return result, ErrInvalidNet
	}

	const domainV2 = "ARMX-POW-V2\x00"
	netLen := len(networkID)
	if netLen > 255 {
		netLen = 255
	}

	needed := len(domainV2) + 2 + netLen + len(header)
	var stackBuf [512]byte
	var input []byte
	if needed <= len(stackBuf) {
		input = stackBuf[:0]
	} else {
		input = make([]byte, 0, needed)
	}
	input = append(input, domainV2...)
	input = append(input, VersionV2, byte(netLen))
	input = append(input, networkID[:netLen]...)
	input = append(input, header...)

	d1 := blake2b.Sum512(input)
	d2 := blake2b.Sum256(d1[:])
	copy(result[:], d2[:])
	return result, nil
}

// HashVersion computes ArmaxHash using BLAKE2b-512 domain-separated input,
// Argon2id v1.3, and BLAKE2b-256 finalization for v1, or Stream-Fast for v2.
func (ctx *Context) HashVersion(
	header []byte,
	networkID string,
	version byte,
) ([OutputSize]byte, error) {
	if version == VersionV2 {
		return ctx.HashV2(header, networkID)
	}
	if version != VersionV1 {
		return [OutputSize]byte{}, ErrInvalidVer
	}

	var result [OutputSize]byte

	if len(header) == 0 {
		return result, ErrEmptyHeader
	}
	if len(header) > MaxHeaderSize {
		return result, ErrHeaderTooLarge
	}
	if len(networkID) == 0 || len(networkID) > 255 {
		return result, ErrInvalidNet
	}

	prehash, salt := derivePrehashAndSalt(header, networkID, version)

	argonDigest := argon2.IDKey(
		prehash,
		salt,
		TimeCost,
		MemoryKiB,
		Threads,
		OutputSize,
	)

	var finalInput [OutputSize * 2]byte
	copy(finalInput[:OutputSize], prehash)
	copy(finalInput[OutputSize:], argonDigest)

	final := blake2b.Sum256(finalInput[:])
	copy(result[:], final[:])
	return result, nil
}

// derivePrehashAndSalt binds both the Argon2id password and salt to the network,
// algorithm version, and complete header.
func derivePrehashAndSalt(
	header []byte,
	networkID string,
	version byte,
) (prehash []byte, salt []byte) {
	const domain = "ARMX-POW\x00"

	netLen := len(networkID)
	if netLen > 255 {
		netLen = 255
	}

	needed := len(domain) + 2 + netLen + len(header)
	var stackBuf [512]byte
	var input []byte
	if needed <= len(stackBuf) {
		input = stackBuf[:0]
	} else {
		input = make([]byte, 0, needed)
	}
	input = append(input, domain...)
	input = append(input, version, byte(netLen))
	input = append(input, networkID[:netLen]...)
	input = append(input, header...)

	digest := blake2b.Sum512(input)
	return digest[:OutputSize], digest[OutputSize : OutputSize+SaltSize]
}

// MeetsTarget returns true if hash <= target when both are interpreted as
// unsigned big-endian 256-bit integers. Confirm that this byte order matches
// the existing ARMAXcoin consensus implementation before integration.
func MeetsTarget(hash, target []byte) (bool, error) {
	if len(hash) != OutputSize {
		return false, ErrInvalidHash
	}
	if len(target) != OutputSize {
		return false, ErrInvalidTgt
	}
	return bytes.Compare(hash, target) <= 0, nil
}

// BigIntToTarget converts a *big.Int target to a padded 32-byte big-endian slice.
// If target is nil, negative, or exceeds 256 bits (2^256-1), an error is returned.
func BigIntToTarget(target *big.Int) ([OutputSize]byte, error) {
	var out [OutputSize]byte
	if target == nil {
		return out, errors.New("armaxhash: target is nil")
	}
	if target.Sign() < 0 {
		return out, errors.New("armaxhash: target cannot be negative")
	}
	b := target.Bytes()
	if len(b) > OutputSize {
		return out, errors.New("armaxhash: target exceeds 256 bits")
	}
	copy(out[OutputSize-len(b):], b)
	return out, nil
}

// MeetsBigIntTarget verifies whether a 32-byte hash satisfies a *big.Int target ceiling.
// This bridges smoothly with core consensus target types without truncation bugs.
func MeetsBigIntTarget(hash []byte, target *big.Int) (bool, error) {
	tgtBytes, err := BigIntToTarget(target)
	if err != nil {
		return false, err
	}
	return MeetsTarget(hash, tgtBytes[:])
}

// EqualDigest compares two 32-byte digests in constant time.
func EqualDigest(a, b []byte) bool {
	if len(a) != OutputSize || len(b) != OutputSize {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// String returns the lowercase hexadecimal representation of a digest.
func String(digest [OutputSize]byte) string {
	return fmt.Sprintf("%x", digest[:])
}
