package armaxhash

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
)

func TestValidation(t *testing.T) {
	header := []byte("armax-genesis-block-header-00000001")
	netID := "armax-mainnet-1"

	if _, err := Hash(nil, netID); !errors.Is(err, ErrEmptyHeader) {
		t.Fatalf("nil header: expected ErrEmptyHeader, got %v", err)
	}
	if _, err := Hash([]byte{}, netID); !errors.Is(err, ErrEmptyHeader) {
		t.Fatalf("empty header: expected ErrEmptyHeader, got %v", err)
	}
	if _, err := Hash(header, ""); !errors.Is(err, ErrInvalidNet) {
		t.Fatalf("empty network ID: expected ErrInvalidNet, got %v", err)
	}
	if _, err := Hash(header, strings.Repeat("a", 256)); !errors.Is(err, ErrInvalidNet) {
		t.Fatalf("long network ID: expected ErrInvalidNet, got %v", err)
	}
	if _, err := HashVersion(header, netID, 0); !errors.Is(err, ErrInvalidVer) {
		t.Fatalf("version 0: expected ErrInvalidVer, got %v", err)
	}
	if _, err := HashVersion(header, netID, VersionV2+1); !errors.Is(err, ErrInvalidVer) {
		t.Fatalf("unsupported version: expected ErrInvalidVer, got %v", err)
	}
	if _, err := Hash(bytes.Repeat([]byte{0}, MaxHeaderSize+1), netID); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("oversized header: expected ErrHeaderTooLarge, got %v", err)
	}
}

func TestHashDeterministic(t *testing.T) {
	header := []byte("block-header-height-100-nonce-1")
	a, err := Hash(header, "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash(header, "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("same input produced different digests: %x != %x", a, b)
	}
}

func TestContextMatchesPackageHash(t *testing.T) {
	header := []byte("context-api-compatibility-header")
	want, err := Hash(header, "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewContext().Hash(header, "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Context.Hash differs from Hash: %x != %x", got, want)
	}
}

func TestDomainSeparation(t *testing.T) {
	header := []byte("same-header-on-different-networks")
	a, err := Hash(header, "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash(header, "armax-testnet")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different network IDs produced identical digests")
	}
}

func TestHeaderSensitivity(t *testing.T) {
	a, err := Hash([]byte("header-nonce-1"), "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash([]byte("header-nonce-2"), "armax-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different headers produced identical digests")
	}
}

func TestDynamicSaltAndPrehashBindHeader(t *testing.T) {
	preA, saltA := derivePrehashAndSalt([]byte("header-nonce-1"), "armax-mainnet", Version)
	preB, saltB := derivePrehashAndSalt([]byte("header-nonce-2"), "armax-mainnet", Version)
	if bytes.Equal(preA, preB) {
		t.Fatal("prehash did not change with header")
	}
	if bytes.Equal(saltA, saltB) {
		t.Fatal("salt did not change with header")
	}
	if len(preA) != OutputSize || len(saltA) != SaltSize {
		t.Fatalf("unexpected prehash/salt lengths: %d/%d", len(preA), len(saltA))
	}
}

func TestArgon2idMatchesLibraryConstruction(t *testing.T) {
	header := []byte("argon2-cross-check-header")
	netID := "armax-testnet"
	prehash, salt := derivePrehashAndSalt(header, netID, Version)

	argonDigest := argon2.IDKey(prehash, salt, TimeCost, MemoryKiB, Threads, OutputSize)
	var finalInput [OutputSize * 2]byte
	copy(finalInput[:OutputSize], prehash)
	copy(finalInput[OutputSize:], argonDigest)
	want := blake2b.Sum256(finalInput[:])

	got, err := Hash(header, netID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest does not match standard Argon2id construction: got %x want %x", got, want)
	}
}

func TestConsensusVector(t *testing.T) {
	header := []byte("armax-stdlib-parity-verification-header-999")
	netID := "armax-mainnet"
	got, err := Hash(header, netID)
	if err != nil {
		t.Fatal(err)
	}
	const expectedHex = "a6449126c335ddb0e9919a3bf4c8a025e5a25b205f08f7d8fed9bae41d423cf4"
	if hex.EncodeToString(got[:]) != expectedHex {
		t.Fatalf("consensus vector mismatch: got %x want %s", got, expectedHex)
	}
}

func TestMeetsTarget(t *testing.T) {
	var hash, target [OutputSize]byte
	for i := 2; i < OutputSize; i++ {
		target[i] = 0xff
	}

	copy(hash[:], target[:])
	hash[2] = 0x7f
	ok, err := MeetsTarget(hash[:], target[:])
	if err != nil || !ok {
		t.Fatalf("hash below target should pass: ok=%v err=%v", ok, err)
	}

	copy(hash[:], target[:])
	ok, err = MeetsTarget(hash[:], target[:])
	if err != nil || !ok {
		t.Fatalf("hash equal to target should pass: ok=%v err=%v", ok, err)
	}

	copy(hash[:], target[:])
	hash[1] = 1
	ok, err = MeetsTarget(hash[:], target[:])
	if err != nil || ok {
		t.Fatalf("hash above target should fail: ok=%v err=%v", ok, err)
	}

	if _, err := MeetsTarget(hash[:31], target[:]); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("expected ErrInvalidHash, got %v", err)
	}
	if _, err := MeetsTarget(hash[:], target[:31]); !errors.Is(err, ErrInvalidTgt) {
		t.Fatalf("expected ErrInvalidTgt, got %v", err)
	}
}

func TestEqualDigest(t *testing.T) {
	a := bytes.Repeat([]byte{0x42}, OutputSize)
	b := bytes.Repeat([]byte{0x42}, OutputSize)
	if !EqualDigest(a, b) {
		t.Fatal("equal digests should compare equal")
	}
	b[0] ^= 1
	if EqualDigest(a, b) {
		t.Fatal("different digests should not compare equal")
	}
	if EqualDigest(a[:31], b) {
		t.Fatal("wrong-sized digest must not compare equal")
	}
}

func TestMeetsBigIntTarget(t *testing.T) {
	// Nil target
	if _, err := MeetsBigIntTarget(make([]byte, 32), nil); err == nil {
		t.Fatal("expected error for nil target")
	}

	// Negative target
	neg := big.NewInt(-1)
	if _, err := MeetsBigIntTarget(make([]byte, 32), neg); err == nil {
		t.Fatal("expected error for negative target")
	}

	// Oversized target (> 256 bits)
	over := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := MeetsBigIntTarget(make([]byte, 32), over); err == nil {
		t.Fatal("expected error for target > 256 bits")
	}

	// Target with leading zeros (e.g. 0x0000ffff... or small integer)
	smallTarget := big.NewInt(1000)
	tgtBytes, err := BigIntToTarget(smallTarget)
	if err != nil {
		t.Fatal(err)
	}
	if len(tgtBytes) != OutputSize {
		t.Fatalf("expected 32 bytes, got %d", len(tgtBytes))
	}
	if tgtBytes[31] != byte(1000&0xff) || tgtBytes[30] != byte(1000>>8) {
		t.Fatalf("unexpected big int conversion bytes: %x", tgtBytes)
	}

	// Hash equal to 500 should pass target 1000
	var hash500 [OutputSize]byte
	hash500[31] = byte(500 & 0xff)
	hash500[30] = byte(500 >> 8)
	ok, err := MeetsBigIntTarget(hash500[:], smallTarget)
	if err != nil || !ok {
		t.Fatalf("hash 500 should meet target 1000: ok=%v err=%v", ok, err)
	}

	// Hash equal to 1500 should fail target 1000
	var hash1500 [OutputSize]byte
	hash1500[31] = byte(1500 & 0xff)
	hash1500[30] = byte(1500 >> 8)
	ok, err = MeetsBigIntTarget(hash1500[:], smallTarget)
	if err != nil || ok {
		t.Fatalf("hash 1500 should not meet target 1000: ok=%v err=%v", ok, err)
	}
}

func TestConcurrentHashing(t *testing.T) {
	const goroutines = 8
	const iterations = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				hdr := []byte(fmt.Sprintf("concurrent-test-block-%d-%d", id, i))
				digest, err := Hash(hdr, "armax-mainnet")
				if err != nil {
					t.Errorf("goroutine %d iteration %d failed: %v", id, i, err)
					return
				}
				if digest == [OutputSize]byte{} {
					t.Errorf("goroutine %d iteration %d returned zero digest", id, i)
				}
			}
		}(g)
	}

	wg.Wait()
}

func BenchmarkHashSingleCore(b *testing.B) {
	header := make([]byte, 80)
	copy(header, []byte("armax-benchmark-block-header-data"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(header[72:], uint64(i))
		if _, err := Hash(header, "armax-mainnet"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashParallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		header := make([]byte, 80)
		copy(header, []byte("armax-benchmark-parallel-header"))
		var counter uint64
		for pb.Next() {
			counter++
			binary.LittleEndian.PutUint64(header[72:], counter)
			if _, err := Hash(header, "armax-mainnet"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestHashV2Deterministic(t *testing.T) {
	header := []byte("armax-v2-test-block-header-data-12345")
	h1, err := HashV2(header, "ARMX-TESTNET-1")
	if err != nil {
		t.Fatalf("HashV2 failed: %v", err)
	}
	h2, err := HashV2(header, "ARMX-TESTNET-1")
	if err != nil {
		t.Fatalf("HashV2 repeat failed: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("HashV2 non-deterministic: %x != %x", h1, h2)
	}

	// Domain separation test: different network ID must produce different hash
	hMain, err := HashV2(header, "ARMX-MAINNET-1")
	if err != nil {
		t.Fatalf("HashV2 mainnet failed: %v", err)
	}
	if h1 == hMain {
		t.Fatalf("HashV2 domain separation failed between testnet and mainnet")
	}

	// Verify HashVersion with VersionV2 matches HashV2
	hVer, err := HashVersion(header, "ARMX-TESTNET-1", VersionV2)
	if err != nil {
		t.Fatalf("HashVersion(VersionV2) failed: %v", err)
	}
	if h1 != hVer {
		t.Fatalf("HashVersion mismatch: %x != %x", h1, hVer)
	}
}

func BenchmarkHashV2Parallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		header := make([]byte, 80)
		copy(header, []byte("armax-benchmark-v2-parallel-header"))
		var counter uint64
		for pb.Next() {
			counter++
			binary.LittleEndian.PutUint64(header[72:], counter)
			if _, err := HashV2(header, "ARMX-TESTNET-1"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestMultiGoldenVectors(t *testing.T) {
	tests := []struct {
		name        string
		header      []byte
		netID       string
		expectedHex string
	}{
		{
			name:        "Standard Consensus Vector 999",
			header:      []byte("armax-stdlib-parity-verification-header-999"),
			netID:       "armax-mainnet",
			expectedHex: "a6449126c335ddb0e9919a3bf4c8a025e5a25b205f08f7d8fed9bae41d423cf4",
		},
		{
			name:        "Mainnet Canonical Header 1",
			header:      []byte("1:1710000100:0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b:808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d:armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd:12345:0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			netID:       "ARMX-MAINNET-1",
			expectedHex: "67a90bb986f7a6e1bf32c4f178956aea138aef97a55654364caef74d33151355",
		},
		{
			name:        "Testnet Canonical Header 1",
			header:      []byte("1:1710000100:0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b:808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d:armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd:12345:0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			netID:       "ARMX-TESTNET-1",
			expectedHex: "53db64a3ae40c2ca99a4ba88c6eaa16a59b00bcee67ab8938282ec5038b50b85",
		},
		{
			name:        "Regtest Canonical Header 1",
			header:      []byte("1:1710000100:0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b:808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d:armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd:12345:0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			netID:       "ARMX-REGTEST-1",
			expectedHex: "f4dd187e00eb9e199b08dfaeaf0402bec6cf3c6ecbb0d36275abcbfb464222f3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Hash(tc.header, tc.netID)
			if err != nil {
				t.Fatalf("unexpected hash error: %v", err)
			}
			gotHex := hex.EncodeToString(got[:])
			if tc.expectedHex == "" {
				t.Logf("Computed vector for %s: %s", tc.name, gotHex)
			} else if gotHex != tc.expectedHex {
				t.Fatalf("vector mismatch:\n got:  %s\n want: %s", gotHex, tc.expectedHex)
			}
		})
	}
}

func TestDifferentialTargetComparison(t *testing.T) {
	// Differential testing comparing big.Int comparison vs bytes.Compare
	targets := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(0xffff),
		new(big.Int).Lsh(big.NewInt(1), 128),
		new(big.Int).Lsh(big.NewInt(1), 240),
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
	}

	for i, tgt := range targets {
		tgtBytes, err := BigIntToTarget(tgt)
		if err != nil {
			t.Fatalf("target %d BigIntToTarget failed: %v", i, err)
		}

		// Generate 5 test hashes
		for h := 0; h < 5; h++ {
			header := []byte(fmt.Sprintf("diff-target-header-%d-%d", i, h))
			digest, err := Hash(header, "armax-testnet")
			if err != nil {
				t.Fatal(err)
			}

			// Reference comparison: new(big.Int).SetBytes(digest[:]).Cmp(tgt) <= 0
			hashNum := new(big.Int).SetBytes(digest[:])
			refMeets := hashNum.Cmp(tgt) <= 0

			// Candidate comparison: MeetsTarget
			candMeets, err := MeetsTarget(digest[:], tgtBytes[:])
			if err != nil {
				t.Fatalf("MeetsTarget error: %v", err)
			}

			// Candidate comparison: MeetsBigIntTarget
			bigMeets, err := MeetsBigIntTarget(digest[:], tgt)
			if err != nil {
				t.Fatalf("MeetsBigIntTarget error: %v", err)
			}

			if refMeets != candMeets {
				t.Fatalf("differential mismatch between big.Int and MeetsTarget for target %d hash %d: ref=%v cand=%v", i, h, refMeets, candMeets)
			}
			if refMeets != bigMeets {
				t.Fatalf("differential mismatch between big.Int and MeetsBigIntTarget for target %d hash %d: ref=%v big=%v", i, h, refMeets, bigMeets)
			}
		}
	}
}
