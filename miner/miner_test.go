package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/armaxhash"
	"github.com/armaxcoincore/armax-coin/core"
)

var (
	builtBinary string
	buildOnce   sync.Once
	buildErr    error
)

func getMinerBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		binName := "armminer_test.exe"
		if runtime.GOOS != "windows" {
			binName = "armminer_test"
		}
		builtBinary = filepath.Join(os.TempDir(), binName)
		cmd := exec.Command("go", "build", "-o", builtBinary, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("failed to build armminer binary: %v, output: %s", err, string(out))
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBinary
}

// TestMinerVersionFlag verifies that armminer CLI responds to -version
func TestMinerVersionFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armminer v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}

// TestMinerResponseJSON verifies MineResponse parsing
func TestMinerResponseJSON(t *testing.T) {
	raw := `{"status":"success","message":"Block #1 mined successfully"}`
	var resp MineResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if resp.Status != "success" || !strings.Contains(resp.Message, "Block #1") {
		t.Fatalf("Unexpected MineResponse values: %+v", resp)
	}
}

// TestMinerThreadsFlag verifies that armminer CLI accepts the -threads flag
func TestMinerThreadsFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-miner", "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd", "-threads", "4", "-info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -threads flag rejected: %v, output: %s", err, string(out))
	}
	outStr := string(out)
	if strings.Contains(outStr, "flag provided but not defined: -threads") {
		t.Fatalf("armminer rejected -threads flag: %s", outStr)
	}
}

// TestMinerBenchmarkFlag verifies that armminer -benchmark runs and logs hash rate
func TestMinerBenchmarkFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-benchmark")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -benchmark failed: %v, output: %s", err, string(out))
	}
	outStr := string(out)
	if !strings.Contains(outStr, "Aggregate Hashrate") || !strings.Contains(outStr, "hash/s") {
		t.Fatalf("Expected benchmark output containing 'Aggregate Hashrate', got: %s", outStr)
	}
}

// TestDifferentialOptimizedMinerHashEquivalence verifies byte-for-byte identity between
// the optimized zero-allocation miner hot-path and the reference Block.HeaderBytes() +
// Block.CalculateHashWithError() + core.HashMeetsTarget() implementation across:
// - Varied block indexes (genesis, v1, v2 boundary, high heights)
// - Varied timestamps, PrevHashes, MerkleRoots, and Bech32 miner addresses
// - Boundary nonces (0, leading-zero binary bytes, 10^k digit transitions, MaxInt32, MaxInt64)
// - Randomized header inputs and repeated deterministic calls
// - Concurrent multi-worker execution
func TestDifferentialOptimizedMinerHashEquivalence(t *testing.T) {
	indexes := []int{0, 1, 7795, 7796, 13052, 13292, 13293, 999999}
	timestamps := []int64{1710000001, 1762668000, 2147483647, 9999999999}
	prevHashes := []string{
		"0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	merkleRoots := []string{
		"808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d",
		"ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76",
	}
	miners := []string{
		"armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		"armx1psmx4e6yhg84z2h5wadg3698vjlx06flnynhec08fv8nv56z758sl9xl6c",
		"armx17nqa4uyewe8elsn4w8qfygjv7aafkkzj25r8antzdmecwnhpd28sayl2yl",
	}
	boundaryNonces := []int64{
		0, 1, 9, 10, 99, 100, 255, 256, 65535, 65536,
		999999999, 1000000000,
		int64(math.MaxInt32) - 1, int64(math.MaxInt32), int64(math.MaxInt32) + 1,
		int64(math.MaxUint32) - 1, int64(math.MaxUint32), int64(math.MaxUint32) + 1,
		999999999999999, 1000000000000000, // 15 -> 16 digits
		9999999999999999, 10000000000000000, // 16 -> 17 digits (Block 1 / Block 2 boundary!)
		99999999999999999, 100000000000000000, // 17 -> 18 digits
		math.MaxInt64 - 1, math.MaxInt64,
	}

	netID := core.NetworkID()
	targets := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		core.DifficultyToTarget(7),
		core.DifficultyToTarget(5),
		core.DifficultyToTarget(1),
		new(big.Int).Set(core.MaxTarget256),
	}

	// 1. Exhaustive boundary & field matrix for ArmaxHash v2 (and height-dispatched modes)
	for _, idx := range indexes {
		// To keep Argon2id (v1 at idx=1, 7795) fast while testing full matrix on v2 and sha256d:
		nonceList := boundaryNonces
		if idx > 0 && idx < 7796 {
			nonceList = []int64{0, 1, 10, math.MaxInt32, math.MaxInt64}
		}
		powMode := resolveMinerPoWMode(idx, "")

		for _, ts := range timestamps[:2] {
			for _, prev := range prevHashes[:2] {
				for _, mr := range merkleRoots[:1] {
					for _, minerAddr := range miners[:2] {
						b := core.Block{
							Index:      idx,
							Timestamp:  ts,
							PrevHash:   prev,
							MerkleRoot: mr,
							Miner:      minerAddr,
							Difficulty: 5,
							Target:     "00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
						}
						prefixBytes := buildHeaderPrefixBytes(&b)
						suffixBytes := buildHeaderSuffixBytes(&b)
						buf := make([]byte, 0, len(prefixBytes)+32+len(suffixBytes))

						for _, nonce := range nonceList {
							b.Nonce = nonce
							refHeader := b.HeaderBytes()
							buf = appendCanonicalHeaderBytes(buf, prefixBytes, b.Nonce, suffixBytes)
							if !bytes.Equal(buf, refHeader) {
								t.Fatalf("header preimage mismatch at index=%d nonce=%d:\n got:  %q\n want: %q",
									idx, nonce, string(buf), string(refHeader))
							}

							refHashHex, refErr := b.CalculateHashWithError()
							optHashBytes, optErr := computeHeaderDigestBytes(buf, netID, powMode)
							if (refErr != nil) != (optErr != nil) {
								t.Fatalf("error mismatch at index=%d nonce=%d: refErr=%v optErr=%v", idx, nonce, refErr, optErr)
							}
							optHashHex := hex.EncodeToString(optHashBytes[:])
							if optHashHex != refHashHex {
								t.Fatalf("digest mismatch at index=%d nonce=%d:\n got:  %s\n want: %s",
									idx, nonce, optHashHex, refHashHex)
							}

							// Verify repeated hashing of identical input produces identical output
							optHashBytes2, err2 := computeHeaderDigestBytes(buf, netID, powMode)
							if err2 != nil || optHashBytes2 != optHashBytes {
								t.Fatalf("repeated hash mismatch at index=%d nonce=%d", idx, nonce)
							}

							// Verify target comparison equivalence across all targets
							for _, tgt := range targets {
								refMeets := core.HashMeetsTarget(refHashHex, tgt)
								tgtBytes, valid := bigIntToTargetBytes32(tgt)
								optMeets := valid && hashBytesMeetTarget32(&optHashBytes, &tgtBytes)
								if optMeets != refMeets {
									t.Fatalf("target comparison mismatch at index=%d nonce=%d target=%x: got=%v want=%v",
										idx, nonce, tgt, optMeets, refMeets)
								}
							}
						}
					}
				}
			}
		}
	}

	// 2. Randomized headers and nonces (500 iterations on ArmaxHash v2)
	rng := rand.New(rand.NewSource(20261109))
	for iter := 0; iter < 500; iter++ {
		var randPrev [32]byte
		var randMerkle [32]byte
		_, _ = rng.Read(randPrev[:])
		_, _ = rng.Read(randMerkle[:])

		b := core.Block{
			Index:      7796 + rng.Intn(500000),
			Timestamp:  1710000000 + int64(rng.Intn(100000000)),
			PrevHash:   hex.EncodeToString(randPrev[:]),
			MerkleRoot: hex.EncodeToString(randMerkle[:]),
			Miner:      miners[iter%len(miners)],
			Nonce:      rng.Int63(),
			Difficulty: 5,
			Target:     "00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		}
		prefixBytes := buildHeaderPrefixBytes(&b)
		suffixBytes := buildHeaderSuffixBytes(&b)
		buf := appendCanonicalHeaderBytes(nil, prefixBytes, b.Nonce, suffixBytes)

		if !bytes.Equal(buf, b.HeaderBytes()) {
			t.Fatalf("random iter %d header mismatch", iter)
		}
		refHex, err := b.CalculateArmaxHashV2()
		if err != nil {
			t.Fatalf("random iter %d ref hash failed: %v", iter, err)
		}
		optBytes, err := computeHeaderDigestBytes(buf, netID, powModeArmaxHashV2)
		if err != nil {
			t.Fatalf("random iter %d opt hash failed: %v", iter, err)
		}
		if hex.EncodeToString(optBytes[:]) != refHex {
			t.Fatalf("random iter %d digest mismatch: got %x want %s", iter, optBytes, refHex)
		}
	}

	// 3. Concurrent multi-worker hashing equivalence & solved block consensus validation
	var wg sync.WaitGroup
	const workers = 8
	const perWorker = 200
	easyTarget := core.DifficultyToTarget(1) // 1 leading hex zero (1/16 probability per hash)
	easyTargetHex := fmt.Sprintf("%064x", easyTarget)
	easyTargetBytes, _ := bigIntToTargetBytes32(easyTarget)

	var solvedCount int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			b := core.Block{
				Index:      13052,
				Timestamp:  1762668000,
				PrevHash:   prevHashes[0],
				MerkleRoot: merkleRoots[0],
				Miner:      miners[0],
				Difficulty: 1,
				Target:     easyTargetHex,
			}
			prefixBytes := buildHeaderPrefixBytes(&b)
			suffixBytes := buildHeaderSuffixBytes(&b)
			buf := make([]byte, 0, len(prefixBytes)+32+len(suffixBytes))

			for j := 0; j < perWorker; j++ {
				b.Nonce = int64(workerID)*1000000 + int64(j)
				buf = appendCanonicalHeaderBytes(buf, prefixBytes, b.Nonce, suffixBytes)
				optBytes, err := computeHeaderDigestBytes(buf, netID, powModeArmaxHashV2)
				if err != nil {
					t.Errorf("worker %d hash error: %v", workerID, err)
					return
				}
				refHex, err := b.CalculateHashWithError()
				if err != nil {
					t.Errorf("worker %d ref hash error: %v", workerID, err)
					return
				}
				optHex := hex.EncodeToString(optBytes[:])
				if optHex != refHex {
					t.Errorf("worker %d mismatch at nonce %d: got %s want %s", workerID, b.Nonce, optHex, refHex)
					return
				}

				if hashBytesMeetTarget32(&optBytes, &easyTargetBytes) {
					b.Hash = optHex
					// Verify existing consensus block validation accepts the solved block's PoW and Difficulty!
					calcHash, calcErr := b.CalculateHashWithError()
					if calcErr != nil || calcHash != b.Hash {
						t.Errorf("worker %d solved block rejected by b.CalculateHashWithError: err=%v calc=%s got=%s",
							workerID, calcErr, calcHash, b.Hash)
						return
					}
					if !core.HashMeetsTarget(b.Hash, easyTarget) {
						t.Errorf("worker %d solved block rejected by core.HashMeetsTarget", workerID)
						return
					}
					if b.Difficulty != core.TargetToDifficulty(easyTarget) {
						t.Errorf("worker %d solved block difficulty mismatch: got %d want %d",
							workerID, b.Difficulty, core.TargetToDifficulty(easyTarget))
						return
					}
					atomic.AddInt64(&solvedCount, 1)
				}
			}
		}(w)
	}
	wg.Wait()
	if solvedCount == 0 {
		t.Fatalf("expected at least 1 solved block across %d hashes at Difficulty 1, got 0", workers*perWorker)
	}
}

func newBenchmarkCandidateBlock() (core.Block, *big.Int) {
	targetInt := core.DifficultyToTarget(7)
	targetHex := fmt.Sprintf("%064x", targetInt)
	b := core.Block{
		Index:      13052,
		Timestamp:  1762668000,
		PrevHash:   "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b",
		MerkleRoot: "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d",
		Miner:      "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		Nonce:      10000000,
		Difficulty: 7,
		Target:     targetHex,
	}
	return b, targetInt
}

// BenchmarkMinerHotLoop_BaselineSingleCore measures the original unoptimized standalone miner loop
// (calling b.CalculateHashWithError() + core.HashMeetsTarget() on every nonce).
func BenchmarkMinerHotLoop_BaselineSingleCore(b *testing.B) {
	blk, targetInt := newBenchmarkCandidateBlock()
	b.ReportAllocs()
	b.SetBytes(int64(len(blk.HeaderBytes())))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		blk.Nonce = 10000000 + int64(i)
		hashHex, err := blk.CalculateHashWithError()
		if err != nil {
			b.Fatal(err)
		}
		_ = core.HashMeetsTarget(hashHex, targetInt)
	}
}

// BenchmarkMinerHotLoop_OptimizedSingleCore measures the optimized zero-allocation standalone miner loop
// on the exact same canonical Testnet block header workload.
func BenchmarkMinerHotLoop_OptimizedSingleCore(b *testing.B) {
	blk, targetInt := newBenchmarkCandidateBlock()
	prefixBytes := buildHeaderPrefixBytes(&blk)
	suffixBytes := buildHeaderSuffixBytes(&blk)
	buf := make([]byte, 0, len(prefixBytes)+32+len(suffixBytes))
	targetBytes, _ := bigIntToTargetBytes32(targetInt)
	powMode := resolveMinerPoWMode(blk.Index, "armaxhash-v2")
	netID := core.NetworkID()

	b.ReportAllocs()
	b.SetBytes(int64(len(blk.HeaderBytes())))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		nonce := 10000000 + int64(i)
		buf = appendCanonicalHeaderBytes(buf, prefixBytes, nonce, suffixBytes)
		hashBytes, err := computeHeaderDigestBytes(buf, netID, powMode)
		if err != nil {
			b.Fatal(err)
		}
		_ = hashBytesMeetTarget32(&hashBytes, &targetBytes)
	}
}

// BenchmarkMinerHotLoop_BaselineParallel measures the original unoptimized multi-threaded Stratum/GBT
// worker hot-path (with per-nonce stateMu.RLock(), CalculateHashWithError(), and HashMeetsTarget()).
func BenchmarkMinerHotLoop_BaselineParallel(b *testing.B) {
	baseBlk, targetInt := newBenchmarkCandidateBlock()
	var stateMu sync.RWMutex
	shareTarget := targetInt

	b.ReportAllocs()
	b.SetBytes(int64(len(baseBlk.HeaderBytes())))
	b.ResetTimer()

	var workerSeq int64
	b.RunParallel(func(pb *testing.PB) {
		wid := atomic.AddInt64(&workerSeq, 1)
		blk := baseBlk
		blk.Nonce = 10000000 + wid*100000000
		for pb.Next() {
			blk.Nonce++
			hashHex, err := calculateStratumBlockHash(&blk, "armaxhash-v2")
			if err != nil {
				b.Fatal(err)
			}
			stateMu.RLock()
			tgt := shareTarget
			stateMu.RUnlock()
			_ = core.HashMeetsTarget(hashHex, tgt)
		}
	})
}

// BenchmarkMinerHotLoop_OptimizedParallel measures the optimized zero-allocation multi-threaded
// worker hot-path (with preallocated header buffer, byte-array target check, and per-batch lock read).
func BenchmarkMinerHotLoop_OptimizedParallel(b *testing.B) {
	baseBlk, targetInt := newBenchmarkCandidateBlock()
	var stateMu sync.RWMutex
	shareTargetBytes, _ := bigIntToTargetBytes32(targetInt)
	powMode := resolveMinerPoWMode(baseBlk.Index, "armaxhash-v2")
	netID := core.NetworkID()

	b.ReportAllocs()
	b.SetBytes(int64(len(baseBlk.HeaderBytes())))
	b.ResetTimer()

	var workerSeq int64
	b.RunParallel(func(pb *testing.PB) {
		wid := atomic.AddInt64(&workerSeq, 1)
		blk := baseBlk
		blk.Nonce = 10000000 + wid*100000000
		prefixBytes := buildHeaderPrefixBytes(&blk)
		suffixBytes := buildHeaderSuffixBytes(&blk)
		buf := make([]byte, 0, len(prefixBytes)+32+len(suffixBytes))

		stateMu.RLock()
		localTargetBytes := shareTargetBytes
		stateMu.RUnlock()
		var localBatch int64

		for pb.Next() {
			if localBatch >= 256 {
				localBatch = 0
				stateMu.RLock()
				localTargetBytes = shareTargetBytes
				stateMu.RUnlock()
			}
			blk.Nonce++
			buf = appendCanonicalHeaderBytes(buf, prefixBytes, blk.Nonce, suffixBytes)
			hashBytes, err := computeHeaderDigestBytes(buf, netID, powMode)
			if err != nil {
				b.Fatal(err)
			}
			localBatch++
			_ = hashBytesMeetTarget32(&hashBytes, &localTargetBytes)
		}
	})
}

// TestMiner_FutureLimitWait explicitly verifies futureLimitWait behavior at height #20,000+
// across all three boundary zones: < 570s (no wait), 571s..600s (linear buffer wait 1..30s), and > 600s (30s cap).
func TestMiner_FutureLimitWait(t *testing.T) {
	core.SetFutureTimestampLimitActivationHeight(20000)
	defer core.ResetFutureTimestampLimitActivationHeightOverride()

	const nodeNow = int64(1_700_000_000)
	cases := []struct {
		name    string
		height  int
		blockTs int64
		want    int64
	}{
		{"skew 0s (< 570s)", 20000, nodeNow, 0},
		{"skew 120s (< 570s)", 20001, nodeNow + 120, 0},
		{"skew 569s (< 570s)", 20001, nodeNow + 569, 0},
		{"skew exact 570s safety boundary", 20001, nodeNow + 570, 0},
		{"skew 571s (inside 571..600)", 20001, nodeNow + 571, 1},
		{"skew 585s (inside 571..600)", 20001, nodeNow + 585, 15},
		{"skew 600s upper C6 limit (waits 30s)", 20001, nodeNow + 600, 30},
		{"skew 601s (> 600s capped at 30s)", 20001, nodeNow + 601, 30},
		{"skew 1200s (> 600s capped at 30s)", 20001, nodeNow + 1200, 30},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := futureLimitWait(c.height, c.blockTs, nodeNow); got != c.want {
				t.Fatalf("futureLimitWait(%d, skew=%ds): got %ds, want %ds",
					c.height, c.blockTs-nodeNow, got, c.want)
			}
		})
	}
}

// TestMiner_AdaptiveInheritedTipWait verifies that inheriting a forward-stamped parent/template
// timestamp (blockTs <= minTs) uses inheritedTipSafetyMargin (5s), while a locally advanced
// miner clock (blockTs > minTs) preserves futureLimitSafetyMargin (30s).
func TestMiner_AdaptiveInheritedTipWait(t *testing.T) {
	core.SetFutureTimestampLimitActivationHeight(20000)
	defer core.ResetFutureTimestampLimitActivationHeightOverride()

	const nodeNow = int64(1_700_000_000)
	cases := []struct {
		name    string
		blockTs int64
		minTs   int64
		want    int64
	}{
		// Inherited tip (blockTs == minTs): 5s margin => allowed skew up to 595s without pause, max pause 5s
		{"inherited skew 570s -> wait 0s", nodeNow + 570, nodeNow + 570, 0},
		{"inherited skew 595s (C6 5s margin) -> wait 0s", nodeNow + 595, nodeNow + 595, 0},
		{"inherited skew 596s -> wait 1s", nodeNow + 596, nodeNow + 596, 1},
		{"inherited skew 599s -> wait 4s", nodeNow + 599, nodeNow + 599, 4},
		{"inherited skew 600s (+600s sliding tip) -> wait 5s", nodeNow + 600, nodeNow + 600, 5},
		{"inherited skew 650s (capped at 5s)", nodeNow + 650, nodeNow + 650, 5},
		// Locally advanced clock (blockTs > minTs): preserves strict 30s safety margin
		{"local-clock skew 570s -> wait 0s", nodeNow + 570, nodeNow, 0},
		{"local-clock skew 571s -> wait 1s", nodeNow + 571, nodeNow, 1},
		{"local-clock skew 595s -> wait 25s", nodeNow + 595, nodeNow, 25},
		{"local-clock skew 600s -> wait 30s", nodeNow + 600, nodeNow, 30},
		{"local-clock skew 650s (capped at 30s)", nodeNow + 650, nodeNow, 30},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := futureLimitWaitWithMinTime(20001, c.blockTs, c.minTs, nodeNow); got != c.want {
				t.Fatalf("futureLimitWaitWithMinTime(20001, blockTs=%d, minTs=%d, now=%d): got %ds, want %ds",
					c.blockTs, c.minTs, nodeNow, got, c.want)
			}
		})
	}
}

func TestFetchBlockTemplate_Syncing503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getblocktemplate" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"syncing","message":"Synchronizing blockchain (#10 / #20558)...; mining paused until sync completes"}`))
	}))
	defer srv.Close()

	_, err := fetchBlockTemplate(&http.Client{Timeout: 2 * time.Second}, srv.URL, "", "", "")
	if !errors.Is(err, errNodeSyncing) {
		t.Fatalf("expected errNodeSyncing on HTTP 503 /getblocktemplate, got: %v", err)
	}
}

func TestMiner_V2JobHasherAndWorkerSlotsParity(t *testing.T) {
	b := core.Block{
		Index:      2501,
		Timestamp:  1762668000,
		PrevHash:   "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b",
		MerkleRoot: "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d",
		Miner:      core.GenesisMiner,
		Target:     "000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
	prefix := buildHeaderPrefixBytes(&b)
	suffix := buildHeaderSuffixBytes(&b)
	netID := core.NetworkID()

	jh, err := armaxhash.NewV2JobHasher(netID, prefix, suffix)
	if err != nil {
		t.Fatalf("NewV2JobHasher failed: %v", err)
	}
	for _, nonce := range []int64{1, 12345, (1 << 36) + 7} {
		hdr := appendCanonicalHeaderBytes(nil, prefix, nonce, suffix)
		want, err := computeHeaderDigestBytes(hdr, netID, powModeArmaxHashV2)
		if err != nil {
			t.Fatalf("computeHeaderDigestBytes failed: %v", err)
		}
		if got := jh.HashNonce(nonce); got != want {
			t.Fatalf("V2JobHasher parity mismatch nonce=%d", nonce)
		}
	}

	slots := NewWorkerSlotManager(4)
	slots.AddHashes(0, 8192)
	slots.AddHashes(3, 8192)
	if slots.TotalHashes() != 16384 {
		t.Fatalf("expected 16384 total hashes across padded slots, got %d", slots.TotalHashes())
	}
}
