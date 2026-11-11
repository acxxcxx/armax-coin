package core

import (
	"fmt"
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
//   exponent = FloorDiv(driftSeconds * 65536, tau)
//   shifts = exponent >> 16
//   fraction = exponent & 0xffff
//   factor = 65536 + ((1957664074821 * fraction + 971821376 * fraction^2 + 5127 * fraction^3 + 2^47) >> 48)
//   target = (anchorTarget * factor) >> 16
//   if shifts >= 0: target <<= shifts
//   if shifts < 0:  target >>= (-shifts)
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
	if len(chain) <= anchorIdx || anchorIdx < 0 {
		return Block{}, nil, fmt.Errorf("insufficient chain depth: have %d blocks, need at least %d", len(chain), anchorIdx+1)
	}

	anchorBlock = chain[anchorIdx]

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

// CalculateArmaxASERTTarget evaluates the Armax-ASERT-AW consensus target for a candidate block
// at nextIndex with candidateTimestamp on top of chain.
func CalculateArmaxASERTTarget(chain []Block, nextIndex int, candidateTimestamp int64) *big.Int {
	actHeight := int(GetASERTActivationHeight())
	if actHeight <= 0 {
		return CalculateExpectedTarget(chain, nextIndex)
	}

	_, anchorTarget, err := ResolveASERTAnchor(chain, actHeight)
	if err != nil {
		return CalculateExpectedTarget(chain, nextIndex)
	}

	powLimit := GetArmaxHashPowLimit()
	minDrift, maxDrift := ComputeDriftBounds(anchorTarget, powLimit)

	if len(chain) == 0 {
		return new(big.Int).Set(anchorTarget)
	}
	tip := chain[len(chain)-1]

	var effDrift int64
	if len(chain) <= actHeight {
		effDrift = 0
	} else {
		// Check O(1) cache for tip
		globalASERTDriftCache.mu.RLock()
		cachedDrift, found := globalASERTDriftCache.drifts[tip.Hash]
		globalASERTDriftCache.mu.RUnlock()

		if found {
			effDrift = cachedDrift
		} else {
			// Linear scan from activation anchor
			effDrift = 0
			for i := actHeight; i < len(chain); i++ {
				prev := &chain[i-1]
				curr := &chain[i]
				effDrift = StepEffectiveDrift(effDrift, prev, curr, minDrift, maxDrift)
				globalASERTDriftCache.mu.Lock()
				globalASERTDriftCache.drifts[curr.Hash] = effDrift
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
