package core

import (
	"encoding/hex"
	"flag"
	"fmt"
	"math"
	"math/big"
	"sync"
)

const (
	// ASERT_TAU is the difficulty adjustment half-life in seconds (3600 seconds = 30 blocks).
	ASERT_TAU = 3600

	// MaxASERTBlockInterval is the 4-hour surge cap (14,400s) on single-block drift injection.
	// This expands the single-block relaxation to ~15.63x drop while bounding post-outage spam to 120 blocks.
	MaxASERTBlockInterval = 14400

	// TEDRBoostDriftSeconds is the synthetic solve time (7,200s / 2 hours) injected into the ASERT
	// drift accumulator whenever an emergency block is solved under TEDR.
	// 7,200s - 120s = 7,080s drift, which drops subsequent normal block difficulty by 2^(7080/3600) = ~3.91x.
	TEDRBoostDriftSeconds = 7200

	// asertRBits is the fractional precision bit length for the aserti3-2d integer approximation.
	asertRBits = 16

	// asertRadix is 2^16 (65536).
	asertRadix = int64(1) << asertRBits

	// maxASERTDriftCacheEntries caps the in-memory ASERT drift map to prevent unbounded growth.
	maxASERTDriftCacheEntries = 10000

	// prunedASERTDriftWindowSize retains the most recent 2048 blocks (well above MaxReorganizationDepth = 720)
	// whenever globalASERTDriftCache is pruned.
	prunedASERTDriftWindowSize = 2048
)

// FloorDiv performs exact mathematical floor division for integers: floor(a / b).
// Unlike Go's truncated integer division, FloorDiv(-1, 3600) yields -1 instead of 0.
func FloorDiv(a, b int64) int64 {
	if b <= 0 {
		panic("FloorDiv denominator must be > 0")
	}
	q := a / b
	if (a < 0) && (a%b != 0) {
		q--
	}
	return q
}

// ComputeASERTFromDrift calculates the 256-bit target using the exact canonical aserti3-2d
// fixed-point polynomial algorithm:
//
//	exponent = FloorDiv(driftSeconds * 65536, tau)
//	shifts = exponent >> 16
//	fraction = exponent & 0xffff
//	factor = 65536 + ((1957664074821 * fraction + 971821376 * fraction^2 + 5127 * fraction^3 + 2^47) >> 48)
//	target = (anchorTarget * factor) >> 16
//	if shifts >= 0: target <<= shifts
//	if shifts < 0:  target >>= (-shifts)
func ComputeASERTFromDrift(anchorTarget *big.Int, driftSeconds int64) *big.Int {
	if anchorTarget == nil || anchorTarget.Sign() <= 0 {
		return DifficultyToTarget(1)
	}

	exponent := FloorDiv(driftSeconds*asertRadix, ASERT_TAU)
	shifts := exponent >> asertRBits
	frac := uint64(exponent & (asertRadix - 1))

	// Canonical cubic polynomial approximation (Horner's form or expanded in uint64):
	poly := 195766423245049*frac + 971821376*frac*frac + 5127*frac*frac*frac + (uint64(1) << 47)
	factor := uint64(asertRadix) + (poly >> 48)

	nextTarget := new(big.Int).Mul(anchorTarget, new(big.Int).SetUint64(factor))
	nextTarget.Rsh(nextTarget, uint(asertRBits))

	if shifts >= 0 {
		nextTarget.Lsh(nextTarget, uint(shifts))
	} else {
		nextTarget.Rsh(nextTarget, uint(-shifts))
	}

	if nextTarget.Sign() <= 0 {
		nextTarget = big.NewInt(1)
	}

	return nextTarget
}

// ComputeDriftBounds calculates the minimum and maximum drift in seconds such that
// ComputeASERTFromDrift(anchorTarget, drift) stays within [1, powLimit].
func ComputeDriftBounds(anchorTarget *big.Int, powLimit *big.Int) (int64, int64) {
	if anchorTarget == nil || anchorTarget.Sign() <= 0 {
		return -72000, 72000
	}
	if powLimit == nil || powLimit.Sign() <= 0 {
		powLimit = DifficultyToTarget(1)
	}

	// 1. Minimum drift (tightest target = 1)
	minDrift := int64(-72000)
	for minDrift < 0 {
		tgt := ComputeASERTFromDrift(anchorTarget, minDrift)
		if tgt.Cmp(big.NewInt(1)) <= 0 {
			break
		}
		minDrift -= ASERT_TAU
	}

	// 2. Maximum drift (easiest target = powLimit)
	maxDrift := int64(72000)
	for maxDrift > 0 {
		tgt := ComputeASERTFromDrift(anchorTarget, maxDrift)
		if tgt.Cmp(powLimit) >= 0 {
			break
		}
		maxDrift += ASERT_TAU
	}

	return minDrift, maxDrift
}

// ResolveASERTAnchor inspects the chain at the activation boundary (Block activationHeight - 1)
// and returns the consensus anchor block and anchor target.
//
// Anti-Poisoning Guard (DA-03): If the boundary block was mined as a TEDR emergency block,
// the resolver walks backward through the chain to locate the last legitimate normal target,
// preventing emergency parameters from permanently corrupting the ASERT anchor.
func ResolveASERTAnchor(chain []Block, activationHeight int) (anchorBlock Block, anchorTarget *big.Int, err error) {
	anchorIdx := activationHeight - 1
	if anchorIdx < 0 {
		anchorIdx = 0
	}
	if len(chain) <= anchorIdx {
		return Block{}, nil, fmt.Errorf("insufficient chain depth: have %d blocks, need at least %d", len(chain), anchorIdx+1)
	}

	anchorBlock = chain[anchorIdx]

	// When ASERT activates at Genesis #0 alongside a PoW switch at Block #1 (e.g. Mainnet
	// immutable SHA-256d Genesis -> ArmaxHash v2), anchor to the ArmaxHash bootstrap target.
	if anchorIdx == 0 && !IsAnyArmaxHashActive(0) && IsAnyArmaxHashActive(1) && anchorBlock.Hash == GenesisHash {
		return anchorBlock, GetArmaxHashPowLimit(), nil
	}

	// Determine if anchor block was an emergency block
	isEmergency := false
	if anchorIdx > 0 {
		prevBlock := chain[anchorIdx-1]
		if IsBlockEmergencyRecord(&anchorBlock, &prevBlock) {
			isEmergency = true
		}
	}

	if !isEmergency {
		t := anchorBlock.GetTarget()
		if t != nil && t.Sign() > 0 {
			anchorTarget = new(big.Int).Set(t)
			return anchorBlock, anchorTarget, nil
		}
	}

	// Anchor Poisoning Guard: walk backward to find the last legitimate non-emergency target
	for i := anchorIdx - 1; i >= 0; i-- {
		b := chain[i]
		em := false
		if i > 0 {
			p := chain[i-1]
			if IsBlockEmergencyRecord(&b, &p) {
				em = true
			}
		}
		if !em {
			t := b.GetTarget()
			if t != nil && t.Sign() > 0 {
				anchorTarget = new(big.Int).Set(t)
				return anchorBlock, anchorTarget, nil
			}
		}
	}

	// Fallback to bootstrap limit if no normal block found
	anchorTarget = GetArmaxHashPowLimit()
	return anchorBlock, anchorTarget, nil
}

// StepEffectiveDrift accumulates effective drift between prevBlock and currBlock,
// applying the 4-Hour Surge Cap and TEDR Boost with anti-windup clamping [minDrift, maxDrift].
func StepEffectiveDrift(prevDrift int64, prevBlock, currBlock *Block, minDrift, maxDrift int64) int64 {
	var stepDrift int64
	if prevBlock != nil && IsBlockEmergencyRecord(currBlock, prevBlock) {
		// TEDR Boost: injects 7,200s (2 hours) synthetic drift
		stepDrift = TEDRBoostDriftSeconds - TargetBlockTimeSeconds
	} else if prevBlock != nil {
		dt := currBlock.Timestamp - prevBlock.Timestamp
		if dt > MaxASERTBlockInterval {
			dt = MaxASERTBlockInterval
		}
		if dt < -MaxASERTBlockInterval {
			dt = -MaxASERTBlockInterval
		}
		stepDrift = dt - TargetBlockTimeSeconds
	}

	nextDrift := prevDrift + stepDrift
	if nextDrift > maxDrift {
		nextDrift = maxDrift
	}
	if nextDrift < minDrift {
		nextDrift = minDrift
	}
	return nextDrift
}

// ASERTDriftCache provides thread-safe O(1) incremental caching of accumulated drift
// mapped by parent block hash.
type asertCache struct {
	mu     sync.RWMutex
	drifts map[string]int64
}

var globalASERTDriftCache = &asertCache{
	drifts: make(map[string]int64),
}

// ResetASERTDriftCache clears the drift cache (intended for tests or reorg resets).
func ResetASERTDriftCache() {
	globalASERTDriftCache.mu.Lock()
	defer globalASERTDriftCache.mu.Unlock()
	globalASERTDriftCache.drifts = make(map[string]int64)
}

// GetCachedASERTDrift returns the cached cumulative effective drift for a block hash if present.
func GetCachedASERTDrift(blockHash string) (int64, bool) {
	globalASERTDriftCache.mu.RLock()
	defer globalASERTDriftCache.mu.RUnlock()
	d, ok := globalASERTDriftCache.drifts[blockHash]
	return d, ok
}

// ASERTDriftCacheLen returns the current number of entries stored in globalASERTDriftCache.
func ASERTDriftCacheLen() int {
	globalASERTDriftCache.mu.RLock()
	defer globalASERTDriftCache.mu.RUnlock()
	return len(globalASERTDriftCache.drifts)
}

// CalculateArmaxASERTTarget evaluates the Armax-ASERT-AW consensus target for a candidate block
// at nextIndex with candidateTimestamp on top of chain.
func CalculateArmaxASERTTarget(chain []Block, nextIndex int, candidateTimestamp int64) *big.Int {
	rawAct := GetASERTActivationHeight()
	if rawAct < 0 || rawAct > math.MaxInt32 || nextIndex <= 0 {
		return CalculateExpectedTarget(chain, nextIndex)
	}
	actHeight := int(rawAct)

	_, anchorTarget, err := ResolveASERTAnchor(chain, actHeight)
	if err != nil {
		return CalculateExpectedTarget(chain, nextIndex)
	}

	powLimit := GetArmaxHashPowLimit()
	if !IsAnyArmaxHashActive(nextIndex) && anchorTarget.Cmp(powLimit) > 0 {
		powLimit = GetPowLimit()
	}
	minDrift, maxDrift := ComputeDriftBounds(anchorTarget, powLimit)

	if len(chain) == 0 {
		return new(big.Int).Set(anchorTarget)
	}
	tip := chain[len(chain)-1]

	startHeight := actHeight
	if startHeight < 1 {
		startHeight = 1
	}

	var effDrift int64
	if len(chain) <= startHeight {
		effDrift = 0
	} else {
		// Check O(1) cache for tip or nearest cached ancestor to minimize cold/reorg rebuilds.
		startIdx := startHeight
		globalASERTDriftCache.mu.RLock()
		if cachedDrift, found := globalASERTDriftCache.drifts[tip.Hash]; found {
			effDrift = cachedDrift
			startIdx = len(chain)
		} else {
			for i := len(chain) - 2; i >= startHeight; i-- {
				if cachedDrift, ok := globalASERTDriftCache.drifts[chain[i].Hash]; ok {
					effDrift = cachedDrift
					startIdx = i + 1
					break
				}
			}
		}
		globalASERTDriftCache.mu.RUnlock()

		if startIdx < len(chain) {
			type driftEntry struct {
				hash  string
				drift int64
			}
			newEntries := make([]driftEntry, 0, len(chain)-startIdx)
			for i := startIdx; i < len(chain); i++ {
				prev := &chain[i-1]
				curr := &chain[i]
				effDrift = StepEffectiveDrift(effDrift, prev, curr, minDrift, maxDrift)
				if curr.Hash != "" {
					newEntries = append(newEntries, driftEntry{hash: curr.Hash, drift: effDrift})
				}
			}
			if len(newEntries) > 0 {
				globalASERTDriftCache.mu.Lock()
				for _, entry := range newEntries {
					globalASERTDriftCache.drifts[entry.hash] = entry.drift
				}
				if len(globalASERTDriftCache.drifts) > maxASERTDriftCacheEntries {
					keepStart := len(chain) - prunedASERTDriftWindowSize
					if keepStart < startHeight {
						keepStart = startHeight
					}
					pruned := make(map[string]int64, prunedASERTDriftWindowSize)
					for i := keepStart; i < len(chain); i++ {
						h := chain[i].Hash
						if h == "" {
							continue
						}
						if d, ok := globalASERTDriftCache.drifts[h]; ok {
							pruned[h] = d
						}
					}
					globalASERTDriftCache.drifts = pruned
				}
				globalASERTDriftCache.mu.Unlock()
			}
		}
	}

	// Now step from tip to candidate
	candidateBlock := &Block{
		Index:     nextIndex,
		Timestamp: candidateTimestamp,
	}
	candDrift := StepEffectiveDrift(effDrift, &tip, candidateBlock, minDrift, maxDrift)

	target := ComputeASERTFromDrift(anchorTarget, candDrift)

	// Consensus clamps: [1, powLimit]
	if target.Sign() <= 0 {
		target = big.NewInt(1)
	}
	if target.Cmp(powLimit) > 0 {
		target.Set(powLimit)
	}

	return target
}

func parseTargetHexOrFallback(hexStr string, fallback *big.Int) *big.Int {
	if len(hexStr) == 64 {
		if raw, err := hex.DecodeString(hexStr); err == nil {
			return new(big.Int).SetBytes(raw)
		}
	}
	return new(big.Int).Set(fallback)
}

func isArmaxHashActiveForProfile(prof NetworkProfile, blockIndex int) bool {
	if blockIndex < 0 {
		return false
	}
	if prof.ArmaxHashActivationHeight > 0 && int64(blockIndex) >= prof.ArmaxHashActivationHeight {
		return true
	}
	return prof.ArmaxHashV2ActivationHeight > 0 && int64(blockIndex) >= prof.ArmaxHashV2ActivationHeight
}

func effectiveArmaxHashActForProfile(prof NetworkProfile) int64 {
	v1, v2 := prof.ArmaxHashActivationHeight, prof.ArmaxHashV2ActivationHeight
	switch {
	case v1 > 0 && v2 > 0:
		if v1 <= v2 {
			return v1
		}
		return v2
	case v1 > 0:
		return v1
	case v2 > 0:
		return v2
	default:
		return -1
	}
}

func isEmergencyForProfile(prof NetworkProfile, curr, prev *Block, armaxLimitHex string) bool {
	if prof.IsMainnet || curr == nil || prev == nil || prof.AntiStallActivationHeight <= 0 {
		return false
	}
	if curr.Index < 0 || int64(curr.Index) < prof.AntiStallActivationHeight {
		return false
	}
	return (curr.Timestamp-prev.Timestamp) > TestnetStallThresholdSeconds && curr.Target == armaxLimitHex
}

func stepEffectiveDriftForProfile(prof NetworkProfile, prevDrift int64, prevBlock, currBlock *Block, minDrift, maxDrift int64, armaxLimitHex string) int64 {
	var stepDrift int64
	if prevBlock != nil && isEmergencyForProfile(prof, currBlock, prevBlock, armaxLimitHex) {
		stepDrift = TEDRBoostDriftSeconds - TargetBlockTimeSeconds
	} else if prevBlock != nil {
		dt := currBlock.Timestamp - prevBlock.Timestamp
		if dt > MaxASERTBlockInterval {
			dt = MaxASERTBlockInterval
		}
		if dt < -MaxASERTBlockInterval {
			dt = -MaxASERTBlockInterval
		}
		stepDrift = dt - TargetBlockTimeSeconds
	}
	nextDrift := prevDrift + stepDrift
	if nextDrift > maxDrift {
		nextDrift = maxDrift
	}
	if nextDrift < minDrift {
		nextDrift = minDrift
	}
	return nextDrift
}

// calculateExpectedTargetForProfile computes the expected 256-bit target strictly against prof
// without reading or mutating global ActiveNetwork state. Used when validating blocks on a
// chain whose network identity differs from the caller's global ActiveNetwork.
func calculateExpectedTargetForProfile(prof NetworkProfile, chain []Block, nextIndex int, candidateTimestamp int64) *big.Int {
	basePowLimit := parseTargetHexOrFallback(prof.PowLimitHex, DifficultyToTarget(1))
	armaxLimit := parseTargetHexOrFallback(prof.ArmaxHashPowLimitHex, DifficultyToTarget(3))
	armaxLimitHex := fmt.Sprintf("%064x", armaxLimit)
	isEmergencyCand := func(prevTs, candTs int64, idx int) bool {
		return !prof.IsMainnet && prof.AntiStallActivationHeight > 0 && idx >= 0 &&
			int64(idx) >= prof.AntiStallActivationHeight && (candTs-prevTs) > TestnetStallThresholdSeconds
	}

	if prof.ASERTActivationHeight >= 0 && nextIndex > 0 && int64(nextIndex) >= prof.ASERTActivationHeight {
		actHeight := int(prof.ASERTActivationHeight)
		anchorIdx := actHeight - 1
		if anchorIdx < 0 {
			anchorIdx = 0
		}
		if len(chain) <= anchorIdx {
			return new(big.Int).Set(armaxLimit)
		}

		anchorBlock := chain[anchorIdx]
		var anchorTarget *big.Int
		if anchorIdx == 0 && !isArmaxHashActiveForProfile(prof, 0) && isArmaxHashActiveForProfile(prof, 1) && anchorBlock.Hash == GenesisHash {
			anchorTarget = new(big.Int).Set(armaxLimit)
		} else {
			for i := anchorIdx; i >= 0; i-- {
				var prevB *Block
				if i > 0 {
					prevB = &chain[i-1]
				}
				if !isEmergencyForProfile(prof, &chain[i], prevB, armaxLimitHex) {
					if t := chain[i].GetTarget(); t != nil && t.Sign() > 0 {
						anchorTarget = new(big.Int).Set(t)
						break
					}
				}
			}
			if anchorTarget == nil {
				anchorTarget = new(big.Int).Set(armaxLimit)
			}
		}

		powLimit := new(big.Int).Set(armaxLimit)
		if !isArmaxHashActiveForProfile(prof, nextIndex) && anchorTarget.Cmp(powLimit) > 0 {
			powLimit = new(big.Int).Set(basePowLimit)
		}
		minDrift, maxDrift := ComputeDriftBounds(anchorTarget, powLimit)

		tip := chain[len(chain)-1]
		startHeight := actHeight
		if startHeight < 1 {
			startHeight = 1
		}
		var effDrift int64
		if len(chain) > startHeight {
			for i := startHeight; i < len(chain); i++ {
				effDrift = stepEffectiveDriftForProfile(prof, effDrift, &chain[i-1], &chain[i], minDrift, maxDrift, armaxLimitHex)
			}
		}
		candBlock := &Block{Index: nextIndex, Timestamp: candidateTimestamp}
		candDrift := stepEffectiveDriftForProfile(prof, effDrift, &tip, candBlock, minDrift, maxDrift, armaxLimitHex)
		normalTarget := ComputeASERTFromDrift(anchorTarget, candDrift)
		if normalTarget.Sign() <= 0 {
			normalTarget = big.NewInt(1)
		}
		if normalTarget.Cmp(powLimit) > 0 {
			normalTarget.Set(powLimit)
		}
		if isEmergencyCand(tip.Timestamp, candidateTimestamp, nextIndex) && normalTarget.Cmp(armaxLimit) <= 0 {
			return new(big.Int).Set(armaxLimit)
		}
		return normalTarget
	}

	if targetOverrideHook != nil && !prof.IsMainnet && flag.Lookup("test.v") != nil {
		if override := targetOverrideHook(); override != nil {
			return override
		}
	}
	if len(chain) > 0 && isEmergencyCand(chain[len(chain)-1].Timestamp, candidateTimestamp, nextIndex) {
		return new(big.Int).Set(armaxLimit)
	}
	if nextIndex <= 0 {
		return new(big.Int).Set(basePowLimit)
	}
	actHeight := effectiveArmaxHashActForProfile(prof)
	if actHeight > 0 && int64(nextIndex) == actHeight {
		return new(big.Int).Set(armaxLimit)
	}
	if len(chain) < LWMAWindowSize+1 || nextIndex < LWMAWindowSize+1 {
		if isArmaxHashActiveForProfile(prof, nextIndex) {
			return new(big.Int).Set(armaxLimit)
		}
		if len(chain) > 0 {
			return chain[0].GetTarget()
		}
		return new(big.Int).Set(basePowLimit)
	}
	if len(chain) < nextIndex {
		nextIndex = len(chain)
	}
	window := chain[nextIndex-(LWMAWindowSize+1) : nextIndex]
	subWindowLen := len(window)
	var weightedSolveTimes int64
	sumTargets := big.NewInt(0)
	maxSolveTime := 6 * TargetBlockTimeSeconds
	isNextArmax := actHeight > 0 && int64(nextIndex) >= actHeight

	var lastNormalTarget *big.Int
	for k := nextIndex - (LWMAWindowSize + 1); k >= 0 && k < len(chain); k-- {
		var prevK *Block
		if k > 0 {
			prevK = &chain[k-1]
		}
		if !isEmergencyForProfile(prof, &chain[k], prevK, armaxLimitHex) {
			lastNormalTarget = chain[k].GetTarget()
			break
		}
	}
	if lastNormalTarget == nil || lastNormalTarget.Sign() <= 0 {
		lastNormalTarget = new(big.Int).Set(armaxLimit)
	}

	for i := 1; i < subWindowLen; i++ {
		prev := window[i-1]
		curr := window[i]
		isEm := isEmergencyForProfile(prof, &curr, &prev, armaxLimitHex)
		solveTime := curr.Timestamp - prev.Timestamp
		if isEm {
			solveTime = maxSolveTime
		} else if solveTime > maxSolveTime {
			solveTime = maxSolveTime
		} else if solveTime < -maxSolveTime {
			solveTime = -maxSolveTime
		}
		weightedSolveTimes += int64(i) * solveTime
		var currTarget *big.Int
		if isEm {
			currTarget = new(big.Int).Set(lastNormalTarget)
		} else {
			currTarget = curr.GetTarget()
			lastNormalTarget = currTarget
		}
		if isNextArmax && int64(curr.Index) < actHeight {
			currTarget = new(big.Int).Set(armaxLimit)
		}
		sumTargets.Add(sumTargets, currTarget)
	}

	n := int64(subWindowLen - 1)
	expectedWeight := (n * (n + 1) / 2) * TargetBlockTimeSeconds
	if weightedSolveTimes < expectedWeight/4 {
		weightedSolveTimes = expectedWeight / 4
	} else if weightedSolveTimes > expectedWeight*4 {
		weightedSolveTimes = expectedWeight * 4
	}
	avgTarget := new(big.Int).Div(sumTargets, big.NewInt(n))
	nextTarget := new(big.Int).Mul(avgTarget, big.NewInt(weightedSolveTimes))
	nextTarget.Div(nextTarget, big.NewInt(expectedWeight))

	powFloor := new(big.Int).Set(basePowLimit)
	if len(chain) > 0 {
		powFloor = chain[0].GetTarget()
	}
	if isArmaxHashActiveForProfile(prof, nextIndex) {
		powFloor = DifficultyToTarget(1)
	}
	if nextTarget.Cmp(big.NewInt(1)) < 0 {
		nextTarget.Set(big.NewInt(1))
	}
	if nextTarget.Cmp(powFloor) > 0 {
		nextTarget.Set(powFloor)
	}
	return nextTarget
}
