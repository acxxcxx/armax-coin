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

	// TestnetStallThresholdSeconds is the elapsed solve time threshold (20 minutes) beyond which
	// an anti-stall emergency target is permitted on testnet post-activation [TEDR Draft v2].
	TestnetStallThresholdSeconds = int64(1200)
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

// TargetToHashesPerBlock calculates the expected number of hash trials required to mine a block:
// floor((2^256 - 1) / target). If target is nil or non-positive, returns MaxTarget256.
func TargetToHashesPerBlock(target *big.Int) *big.Int {
	if target == nil || target.Sign() <= 0 {
		return new(big.Int).Set(MaxTarget256)
	}
	return new(big.Int).Div(MaxTarget256, target)
}

// FormatHashes formats a large hash count into standard SI metric notation (e.g. "15.48 M", "1.25 G").
func FormatHashes(hashes *big.Int) string {
	if hashes == nil || hashes.Sign() <= 0 {
		return "1 H"
	}
	hFloat := new(big.Float).SetInt(hashes)
	val, _ := hFloat.Float64()

	switch {
	case val >= 1e12:
		return fmt.Sprintf("%.2f T", val/1e12)
	case val >= 1e9:
		return fmt.Sprintf("%.2f G", val/1e9)
	case val >= 1e6:
		return fmt.Sprintf("%.2f M", val/1e6)
	case val >= 1e3:
		return fmt.Sprintf("%.2f K", val/1e3)
	default:
		return fmt.Sprintf("%d H", hashes.Int64())
	}
}

// TargetToDifficultyRatio calculates the continuous difficulty multiplier relative to the network's
// baseline bootstrap limit (GetArmaxHashPowLimit for ArmaxHash era, or GetPowLimit for legacy era).
func TargetToDifficultyRatio(target *big.Int, blockIndex int) float64 {
	if target == nil || target.Sign() <= 0 {
		return 1.0
	}
	baseTarget := GetPowLimit()
	if IsArmaxHashActive(blockIndex) {
		baseTarget = GetArmaxHashPowLimit()
	}
	if baseTarget == nil || baseTarget.Sign() <= 0 {
		baseTarget = DifficultyToTarget(1)
	}

	ratio := new(big.Float).Quo(new(big.Float).SetInt(baseTarget), new(big.Float).SetInt(target))
	res, _ := ratio.Float64()
	if res < 0.0001 {
		return 0.0001
	}
	return res
}

// TargetToEstimatedNetworkHashrate calculates estimated global network hashrate in H/s:
// hashrate = ExpectedHashes / 120s.
func TargetToEstimatedNetworkHashrate(target *big.Int) *big.Int {
	hashes := TargetToHashesPerBlock(target)
	return new(big.Int).Div(hashes, big.NewInt(TargetBlockTimeSeconds))
}

// FormatHashrate formats a hash-per-second value into standard SI metric rate notation (e.g. "128.50 kH/s", "1.05 MH/s").
func FormatHashrate(hps *big.Int) string {
	if hps == nil || hps.Sign() <= 0 {
		return "0.00 H/s"
	}
	hFloat := new(big.Float).SetInt(hps)
	val, _ := hFloat.Float64()

	switch {
	case val >= 1e12:
		return fmt.Sprintf("%.2f TH/s", val/1e12)
	case val >= 1e9:
		return fmt.Sprintf("%.2f GH/s", val/1e9)
	case val >= 1e6:
		return fmt.Sprintf("%.2f MH/s", val/1e6)
	case val >= 1e3:
		return fmt.Sprintf("%.2f kH/s", val/1e3)
	default:
		return fmt.Sprintf("%.2f H/s", val)
	}
}

// GetCurrentDAAEngineName returns the formal name of the difficulty adjustment algorithm active at blockIndex.
func GetCurrentDAAEngineName(blockIndex int) string {
	if IsASERTActive(blockIndex) {
		return "Armax-ASERT-AW"
	}
	return "LWMA-1 (N=60)"
}

// GetASERTScheduleDrift calculates the active ASERT schedule drift in seconds relative to the anchor block.
// Returns driftSeconds, isActive.
func GetASERTScheduleDrift(chain []Block, tipIndex int) (int64, bool) {
	if !IsASERTActive(tipIndex) || len(chain) == 0 {
		return 0, false
	}
	actHeight := int(GetASERTActivationHeight())
	anchorBlock, _, err := ResolveASERTAnchor(chain, actHeight)
	if err != nil {
		return 0, false
	}
	tip := chain[len(chain)-1]
	drift := (tip.Timestamp - anchorBlock.Timestamp) - TargetBlockTimeSeconds*int64(tip.Index-anchorBlock.Index)
	return drift, true
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

// IsEmergencyBlockCandidate checks if a candidate block qualifies for the anti-stall emergency target:
// candidate timestamp - parent timestamp > 1200s, within active testnet post-activation [TEDR Draft v2].
func IsEmergencyBlockCandidate(prevTimestamp, candidateTimestamp int64, blockIndex int) bool {
	if !IsAntiStallActive(blockIndex) {
		return false
	}
	return (candidateTimestamp - prevTimestamp) > TestnetStallThresholdSeconds
}

// IsBlockEmergencyRecord checks if a historical block in the chain was an emergency block:
// it was mined at or above activation height, solve interval > 1200s, and target equals ArmaxHashPowLimit.
func IsBlockEmergencyRecord(curr *Block, prev *Block) bool {
	if curr == nil || prev == nil {
		return false
	}
	if !IsAntiStallActive(curr.Index) {
		return false
	}
	if (curr.Timestamp - prev.Timestamp) <= TestnetStallThresholdSeconds {
		return false
	}
	emTgtHex := fmt.Sprintf("%064x", GetArmaxHashPowLimit())
	return curr.Target == emTgtHex
}

// CalculateExpectedTarget computes the deterministic 256-bit target expected for a specific block index
// based on the preceding chain history using zawy's LWMA-1 algorithm [C-31] with TEDR Model 3 Synthetic Normalization.
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

	// TEDR Model 3: Locate the last non-emergency target before or at the start of the window
	var lastNormalTarget *big.Int
	startSearch := nextIndex - (LWMAWindowSize + 1)
	if startSearch < 0 {
		startSearch = 0
	}
	for k := startSearch; k >= 0 && k < len(chain); k-- {
		var prevK *Block
		if k > 0 {
			prevK = &chain[k-1]
		}
		if !IsBlockEmergencyRecord(&chain[k], prevK) {
			lastNormalTarget = chain[k].GetTarget()
			break
		}
	}
	if lastNormalTarget == nil || lastNormalTarget.Sign() <= 0 {
		lastNormalTarget = GetArmaxHashPowLimit()
	}

	for i := 1; i < subWindowLen; i++ {
		prev := window[i-1]
		curr := window[i]

		isEmergency := IsBlockEmergencyRecord(&curr, &prev)

		solveTime := curr.Timestamp - prev.Timestamp
		if isEmergency {
			// TEDR Model 3: Normalize emergency solve time to max allowable loosening interval (720s)
			solveTime = maxSolveTime
		} else if solveTime > maxSolveTime {
			solveTime = maxSolveTime
		} else if solveTime < -maxSolveTime {
			solveTime = -maxSolveTime
		}

		weight := int64(i)
		weightedSolveTimes += weight * solveTime

		var currTarget *big.Int
		if isEmergency {
			// TEDR Model 3: Synthetic Target Normalization.
			// Do not leak the low-difficulty ArmaxHashPowLimit into sumTargets!
			// Instead, inherit the last legitimate non-emergency target.
			currTarget = new(big.Int).Set(lastNormalTarget)
		} else {
			currTarget = curr.GetTarget()
			lastNormalTarget = currTarget
		}

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

// CalculateExpectedTargetWithParent computes the expected 256-bit target for a candidate block
// given its explicit parent block context, supporting both branch evaluation and active tip evaluation.
func CalculateExpectedTargetWithParent(chain []Block, parent *Block, nextIndex int, candidateTimestamp int64) *big.Int {
	if IsASERTActive(nextIndex) {
		normalTarget := CalculateArmaxASERTTarget(chain, nextIndex, candidateTimestamp)
		if parent != nil && IsEmergencyBlockCandidate(parent.Timestamp, candidateTimestamp, nextIndex) {
			emLimit := GetArmaxHashPowLimit()
			// Emergency Target Inversion Guard (DA-04): never tighten difficulty during emergency
			if normalTarget.Cmp(emLimit) > 0 {
				return normalTarget
			}
			return emLimit
		}
		return normalTarget
	}

	if parent != nil && IsEmergencyBlockCandidate(parent.Timestamp, candidateTimestamp, nextIndex) {
		return GetArmaxHashPowLimit()
	}
	return CalculateExpectedTarget(chain, nextIndex)
}

// CalculateExpectedTargetForCandidate computes the expected 256-bit target for a candidate block,
// taking into account candidate solve interval for TEDR anti-stall emergency fallback.
func CalculateExpectedTargetForCandidate(chain []Block, nextIndex int, candidateTimestamp int64) *big.Int {
	var parent *Block
	if len(chain) > 0 {
		p := chain[len(chain)-1]
		parent = &p
	}
	return CalculateExpectedTargetWithParent(chain, parent, nextIndex, candidateTimestamp)
}

// CalculateLWMA1 computes the next PoW 256-bit target for the current tip of the blockchain.
// When Armax-ASERT-AW is active at current tip height, it calculates the nominal ASERT target
// under the ideal block schedule (parent.Timestamp + 120s).
func (bc *Blockchain) CalculateLWMA1() *big.Int {
	if IsASERTActive(len(bc.Chain)) && len(bc.Chain) > 0 {
		latest := bc.Chain[len(bc.Chain)-1]
		return CalculateExpectedTargetWithParent(bc.Chain, &latest, len(bc.Chain), latest.Timestamp+TargetBlockTimeSeconds)
	}
	return CalculateExpectedTarget(bc.Chain, len(bc.Chain))
}
