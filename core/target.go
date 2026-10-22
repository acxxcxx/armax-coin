package core

import (
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
)

const (
	// LWMAWindowSize is the number of past blocks evaluated by the Linear Weighted Moving Average (LWMA-1) [D-04]
	LWMAWindowSize = 60

	// TargetBlockTimeSeconds is the ideal interval between blocks (120 seconds / 2 minutes) [D-02]
	TargetBlockTimeSeconds = int64(120)
)

// Two256 is 2^256 used for chainwork computation
var Two256 = new(big.Int).Lsh(big.NewInt(1), 256)

// MaxTarget256 is the absolute minimum difficulty ceiling (2^256 - 1)
var MaxTarget256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// CalculateGenesisTarget computes the 256-bit target ceiling and 64-character hex string for a given hashrate:
// target = 2^256 / (hashrate * blockTimeSec) [L1-6]
func CalculateGenesisTarget(hashrateHPS float64, blockTimeSec float64) (*big.Int, string) {
	if hashrateHPS <= 0 {
		hashrateHPS = 1.0
	}
	if blockTimeSec <= 0 {
		blockTimeSec = float64(TargetBlockTimeSeconds)
	}
	expectedHashes := hashrateHPS * blockTimeSec
	denom := new(big.Int)
	new(big.Float).SetFloat64(expectedHashes).Int(denom)
	if denom.Sign() <= 0 {
		denom = big.NewInt(1)
	}
	target := new(big.Int).Div(Two256, denom)
	if target.Cmp(MaxTarget256) > 0 {
		target = new(big.Int).Set(MaxTarget256)
	}
	targetHex := fmt.Sprintf("%064x", target)
	return target, targetHex
}

// DifficultyToTarget converts integer difficulty leading hex zeros into a 256-bit target
func DifficultyToTarget(difficulty int) *big.Int {
	if difficulty < 1 {
		difficulty = 1
	}
	if difficulty > 64 {
		difficulty = 64
	}
	// Target = (2^256 - 1) >> (4 * difficulty)
	shift := uint(4 * difficulty)
	return new(big.Int).Rsh(MaxTarget256, shift)
}

// TargetToDifficulty calculates the leading hex zero equivalent of a 256-bit target
// for display and RPC telemetry.
func TargetToDifficulty(target *big.Int) int {
	if target == nil || target.Sign() <= 0 {
		return 64
	}
	zeroBits := 256 - target.BitLen()
	diff := (zeroBits + 3) / 4
	if diff < 1 {
		diff = 1
	}
	if diff > 64 {
		diff = 64
	}
	return diff
}

// HashMeetsTarget verifies whether a double-SHA256 hash value numerically satisfies the target ceiling (hashNum <= target)
func HashMeetsTarget(hashHex string, target *big.Int) bool {
	hashBytes, err := hex.DecodeString(hashHex)
	if err != nil || len(hashBytes) != 32 {
		return false
	}
	hashNum := new(big.Int).SetBytes(hashBytes)
	return hashNum.Cmp(target) <= 0
}

// GetPowLimit returns the minimum difficulty ceiling (maximum allowable target) [C-31, L3-4].
// It is anchored to the active network profile's PowLimitHex.
func GetPowLimit() *big.Int {
	powHex := GetActiveNetwork().PowLimitHex
	if len(powHex) == 64 {
		tBytes, err := hex.DecodeString(powHex)
		if err == nil {
			return new(big.Int).SetBytes(tBytes)
		}
	}
	if len(GenesisTargetHex) == 64 {
		tBytes, err := hex.DecodeString(GenesisTargetHex)
		if err == nil {
			return new(big.Int).SetBytes(tBytes)
		}
	}
	return DifficultyToTarget(1)
}

// GetArmaxHashPowLimit returns the initial bootstrap target ceiling (minimum difficulty)
// for blocks mined under the ArmaxHash algorithm.
func GetArmaxHashPowLimit() *big.Int {
	ahHex := GetActiveNetwork().ArmaxHashPowLimitHex
	if len(ahHex) == 64 {
		tBytes, err := hex.DecodeString(ahHex)
		if err == nil {
			return new(big.Int).SetBytes(tBytes)
		}
	}
	return DifficultyToTarget(3)
}

var targetOverrideHook func() *big.Int

// CalculateExpectedTarget computes the deterministic 256-bit target expected for a specific block index
// based on the preceding chain history using zawy's LWMA-1 algorithm [C-31].
func CalculateExpectedTarget(chain []Block, nextIndex int) *big.Int {
	if targetOverrideHook != nil {
		// Hard-guard: target overrides are strictly prohibited on Mainnet profile identity [F3]
		if !GetActiveNetwork().IsMainnet {
			if flag.Lookup("test.v") != nil {
				if override := targetOverrideHook(); override != nil {
					return override
				}
			}
		}
	}
	if nextIndex <= 0 {
		return GetPowLimit()
	}
	// At the exact activation block of ArmaxHash, reset to ArmaxHashPowLimit to bootstrap smoothly into memory-hard mining
	if act := GetArmaxHashActivationHeight(); act > 0 && int64(nextIndex) == act {
		return GetArmaxHashPowLimit()
	}
	// Blocks 1..LWMAWindowSize inherit genesis target, avoiding bypass to MaxTarget (Difficulty 1) [C-31]
	if len(chain) < LWMAWindowSize+1 || nextIndex < LWMAWindowSize+1 {
		if IsArmaxHashActive(nextIndex) {
			return GetArmaxHashPowLimit()
		}
		if len(chain) > 0 {
			return chain[0].GetTarget()
		}
		return GetPowLimit()
	}

	if len(chain) < nextIndex {
		nextIndex = len(chain)
	}
	// Canonical LWMA-1 with N=60 intervals requires N+1 (61) blocks [B_0 ... B_N]
	// to compute exactly N solve time intervals: ST_i = Timestamp_i - Timestamp_{i-1} for i=1..N.
	window := chain[nextIndex-(LWMAWindowSize+1) : nextIndex]
	subWindowLen := len(window)

	var weightedSolveTimes int64
	sumTargets := big.NewInt(0)
	maxSolveTime := 6 * TargetBlockTimeSeconds // 720 seconds

	actHeight := GetArmaxHashActivationHeight()
	isNextArmax := actHeight > 0 && int64(nextIndex) >= actHeight

	for i := 1; i < subWindowLen; i++ {
		prev := window[i-1]
		curr := window[i]

		solveTime := curr.Timestamp - prev.Timestamp
		// Clamp solve time between -6*T and +6*T to prevent timestamp manipulation / timewarp
		if solveTime > maxSolveTime {
			solveTime = maxSolveTime
		} else if solveTime < -maxSolveTime {
			solveTime = -maxSolveTime
		}

		weight := int64(i)
		weightedSolveTimes += weight * solveTime

		currTarget := curr.GetTarget()
		// If we are currently evaluating an ArmaxHash block (nextIndex >= act),
		// any block in the historical window that preceded the ArmaxHash activation (curr.Index < act)
		// was mined under SHA-256d at an unrelated difficulty scale.
		// Substitute its target with the ArmaxHash initial bootstrap limit to prevent legacy targets
		// from artificially dragging down the target (spiking difficulty) during the 60-block transition.
		if isNextArmax && int64(curr.Index) < actHeight {
			currTarget = GetArmaxHashPowLimit()
		}

		sumTargets.Add(sumTargets, currTarget)
	}

	n := int64(subWindowLen - 1)                                 // Exactly N = 60 intervals
	expectedWeight := (n * (n + 1) / 2) * TargetBlockTimeSeconds // (60*61/2)*120 = 219,600

	// Canonical Zawy LWMA dampening bounds:
	// Bound weightedSolveTimes within [expectedWeight / 4, expectedWeight * 4]
	// to prevent extreme difficulty spikes from negative/near-zero solve times
	// or excessive difficulty drops from long intervals in a single block adjustment.
	minWeightedSolveTime := expectedWeight / 4
	maxWeightedSolveTime := expectedWeight * 4
	if weightedSolveTimes < minWeightedSolveTime {
		weightedSolveTimes = minWeightedSolveTime
	} else if weightedSolveTimes > maxWeightedSolveTime {
		weightedSolveTimes = maxWeightedSolveTime
	}

	avgTarget := new(big.Int).Div(sumTargets, big.NewInt(n))

	// NextTarget = avgTarget * weightedSolveTimes / expectedWeight
	nextTarget := new(big.Int).Mul(avgTarget, big.NewInt(weightedSolveTimes))
	nextTarget.Div(nextTarget, big.NewInt(expectedWeight))

	// Clamp boundaries: No artificial difficulty ceiling (Bitcoin model: difficulty scales indefinitely)
	minTarget := big.NewInt(1) // Absolute minimum target (highest conceivable difficulty)
	powFloor := GetPowLimit()
	if len(chain) > 0 {
		powFloor = chain[0].GetTarget()
	}
	if IsArmaxHashActive(nextIndex) {
		// When ArmaxHash is active, difficulty is allowed to drop to accommodate smaller miners
		// while bounded by Difficulty 1
		powFloor = DifficultyToTarget(1)
	}

	if nextTarget.Cmp(minTarget) < 0 {
		nextTarget.Set(minTarget)
	}
	if nextTarget.Cmp(powFloor) > 0 {
		nextTarget.Set(powFloor)
	}

	return nextTarget
}

// CalculateLWMA1 computes the next PoW 256-bit target for the current tip of the blockchain
func (bc *Blockchain) CalculateLWMA1() *big.Int {
	return CalculateExpectedTarget(bc.Chain, len(bc.Chain))
}
