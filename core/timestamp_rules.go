package core

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	// LegacyMTPWindow is the median-time-past window used before the C1/C2 activation height.
	LegacyMTPWindow = 11
	// ExtendedMTPWindow is the median-time-past window enforced from the C1/C2 activation height [C1].
	ExtendedMTPWindow = 55
)

// FutureTimestampLimitSeconds is the maximum a block timestamp may lead the validator's clock
// from FutureTimestampLimitActivationHeight (owner decision: 10 minutes).
const FutureTimestampLimitSeconds = 10 * 60

// LegacyFutureTimestampLimitSeconds is the original 2-hour allowance used below the activation height.
const LegacyFutureTimestampLimitSeconds = 2 * 60 * 60

// GetFutureTimestampLimitActivationHeight returns the first block height validated with the
// tighter future-timestamp limit. A negative value means the limit is disabled.
func GetFutureTimestampLimitActivationHeight() int64 {
	netMu.RLock()
	defer netMu.RUnlock()
	if futureLimitHeightOverride != nil {
		return *futureLimitHeightOverride
	}
	// Rehearsal hook: regtest only, so it can never alter testnet or mainnet consensus.
	if ActiveNetwork.Name == "regtest" {
		if v := os.Getenv("ARMX_FUTURE_LIMIT_HEIGHT"); v != "" {
			if h, err := strconv.ParseInt(v, 10, 64); err == nil {
				return h
			}
		}
	}
	return ActiveNetwork.FutureTimestampLimitActivationHeight
}

// SetFutureTimestampLimitActivationHeight overrides the activation height (intended for tests).
func SetFutureTimestampLimitActivationHeight(height int64) {
	netMu.Lock()
	defer netMu.Unlock()
	futureLimitHeightOverride = &height
}

// ResetFutureTimestampLimitActivationHeightOverride restores the network profile default.
func ResetFutureTimestampLimitActivationHeightOverride() {
	netMu.Lock()
	defer netMu.Unlock()
	futureLimitHeightOverride = nil
}

// minerNowUnix is the clock used to stamp mined blocks. The regtest-only env hook
// ARMX_MINER_CLOCK_OFFSET_SECONDS skews it so rehearsals can produce forward-stamped blocks.
func minerNowUnix() int64 {
	now := time.Now().Unix()
	if ActiveNetwork.Name == "regtest" {
		if v := os.Getenv("ARMX_MINER_CLOCK_OFFSET_SECONDS"); v != "" {
			if off, err := strconv.ParseInt(v, 10, 64); err == nil {
				now += off
			}
		}
	}
	return now
}

// MaxFutureTimestampSeconds returns how far ahead of the local clock the block at blockIndex may be.
func MaxFutureTimestampSeconds(blockIndex int) int64 {
	act := GetFutureTimestampLimitActivationHeight()
	if act >= 0 && blockIndex > 0 && int64(blockIndex) >= act {
		return FutureTimestampLimitSeconds
	}
	return LegacyFutureTimestampLimitSeconds
}

// FutureTimestampError indicates that a block's timestamp leads the node's local clock
// beyond the consensus future drift limit. This error is node-local and temporal:
// as local time advances, the same block will naturally become valid without PoW re-mining.
type FutureTimestampError struct {
	BlockHeight    int
	BlockTimestamp int64
	MaxAllowed     int64
	SkewSeconds    int64
}

func (e *FutureTimestampError) Error() string {
	return fmt.Sprintf("timestamp %d too far in future (max: %d, skew: %ds)", e.BlockTimestamp, e.MaxAllowed, e.SkewSeconds)
}

// IsFutureTimestampError reports whether err is or wraps a *FutureTimestampError.
func IsFutureTimestampError(err error) bool {
	if err == nil {
		return false
	}
	var fte *FutureTimestampError
	return errors.As(err, &fte)
}

// GetFutureTimestampSkew returns the positive clock skew in seconds if err is a *FutureTimestampError.
func GetFutureTimestampSkew(err error) int64 {
	var fte *FutureTimestampError
	if errors.As(err, &fte) {
		return fte.SkewSeconds
	}
	return 0
}

// validateFutureTimestamp rejects blocks whose timestamp is too far ahead of the wall clock.
// The check is node-local (not replayable), so callers skip it for historical replay.
func validateFutureTimestamp(b *Block) error {
	now := time.Now().Unix()
	limit := MaxFutureTimestampSeconds(b.Index)
	maxFuture := now + limit
	if b.Timestamp > maxFuture {
		return &FutureTimestampError{
			BlockHeight:    b.Index,
			BlockTimestamp: b.Timestamp,
			MaxAllowed:     maxFuture,
			SkewSeconds:    b.Timestamp - maxFuture,
		}
	}
	return nil
}

// GetTimestampRulesActivationHeight returns the first block height validated with the
// C1/C2 timestamp rules. A negative value means the rules are disabled.
func GetTimestampRulesActivationHeight() int64 {
	netMu.RLock()
	defer netMu.RUnlock()
	if timestampRulesHeightOverride != nil {
		return *timestampRulesHeightOverride
	}
	return ActiveNetwork.TimestampRulesActivationHeight
}

// SetTimestampRulesActivationHeight overrides the activation height (intended for tests).
func SetTimestampRulesActivationHeight(height int64) {
	netMu.Lock()
	defer netMu.Unlock()
	timestampRulesHeightOverride = &height
}

// ResetTimestampRulesActivationHeightOverride restores the network profile default.
func ResetTimestampRulesActivationHeightOverride() {
	netMu.Lock()
	defer netMu.Unlock()
	timestampRulesHeightOverride = nil
}

// IsTimestampRulesActive reports whether the block at blockIndex is validated with the C1/C2 rules.
func IsTimestampRulesActive(blockIndex int) bool {
	act := GetTimestampRulesActivationHeight()
	if act < 0 || blockIndex <= 0 {
		return false
	}
	return int64(blockIndex) >= act
}

// MTPWindowForHeight returns the median-time-past window that applies to the block at blockIndex.
func MTPWindowForHeight(blockIndex int) int {
	if IsTimestampRulesActive(blockIndex) {
		return ExtendedMTPWindow
	}
	return LegacyMTPWindow
}

// MinValidTimestamp returns the smallest timestamp a block at blockIndex may carry on top of
// prev and chain: MTP+1 before activation, and max(MTP-55+1, prev.Timestamp) from activation.
func MinValidTimestamp(chain []Block, prev *Block, blockIndex int) int64 {
	min := CalculateMedianTimePastN(chain, blockIndex, MTPWindowForHeight(blockIndex)) + 1
	if IsTimestampRulesActive(blockIndex) && prev != nil && prev.Timestamp > min {
		min = prev.Timestamp
	}
	return min
}

// validateBlockTimestampRules enforces the height-gated lower bound on a block timestamp.
// Before activation: timestamp > MTP-11. From activation [C1, C2]: timestamp > MTP-55 and
// timestamp >= parent timestamp.
func validateBlockTimestampRules(b *Block, prev *Block, chain []Block) error {
	window := MTPWindowForHeight(b.Index)
	mtp := CalculateMedianTimePastN(chain, b.Index, window)
	if b.Timestamp <= mtp {
		if window == LegacyMTPWindow {
			return fmt.Errorf("timestamp %d <= MedianTimePast %d", b.Timestamp, mtp)
		}
		return fmt.Errorf("timestamp %d <= MedianTimePast(MTP-%d) %d", b.Timestamp, window, mtp)
	}
	if IsTimestampRulesActive(b.Index) && prev != nil && b.Timestamp < prev.Timestamp {
		return fmt.Errorf("timestamp %d is earlier than parent timestamp %d (non-decreasing rule)", b.Timestamp, prev.Timestamp)
	}
	return nil
}
