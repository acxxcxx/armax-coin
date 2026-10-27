package core

import "fmt"

const (
	// LegacyMTPWindow is the median-time-past window used before the C1/C2 activation height.
	LegacyMTPWindow = 11
	// ExtendedMTPWindow is the median-time-past window enforced from the C1/C2 activation height [C1].
	ExtendedMTPWindow = 55
)

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
