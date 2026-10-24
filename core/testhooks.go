//go:build testhooks

package core

import (
	"math/big"
	"sync"
	"testing"
)

var (
	targetOverrideMu  sync.RWMutex
	targetOverrideVal *big.Int

	netResetMu    sync.RWMutex
	allowResetVal = true

	allowMainnetMu  sync.RWMutex
	allowMainnetVal = true
)

func init() {
	targetOverrideHook = func() *big.Int {
		targetOverrideMu.RLock()
		defer targetOverrideMu.RUnlock()
		if targetOverrideVal == nil {
			return nil
		}
		return new(big.Int).Set(targetOverrideVal)
	}

	allowNetworkResetHook = func() bool {
		netResetMu.RLock()
		defer netResetMu.RUnlock()
		return allowResetVal
	}

	allowMainnetInTestsHook = func() bool {
		allowMainnetMu.RLock()
		defer allowMainnetMu.RUnlock()
		return allowMainnetVal
	}
}

// SetTargetOverrideForTesting sets a temporary target override for unit/integration tests [L1-6, L3-3].
// Thread-safe via sync.RWMutex. Only present when built with '-tags testhooks'.
func SetTargetOverrideForTesting(target *big.Int) {
	targetOverrideMu.Lock()
	defer targetOverrideMu.Unlock()
	if target == nil {
		targetOverrideVal = nil
	} else {
		targetOverrideVal = new(big.Int).Set(target)
	}
}

// SetNetworkForTesting controls whether SetNetwork can be called repeatedly during tests [L1-2, L3-3].
// Thread-safe via sync.RWMutex. Only present when built with '-tags testhooks'.
func SetNetworkForTesting(allow bool) {
	netResetMu.Lock()
	defer netResetMu.Unlock()
	allowResetVal = allow
	if allow {
		netMu.Lock()
		networkInitialized = false
		netMu.Unlock()
	}
}

// SetAllowMainnetForTesting enables or disables mainnet access in unit/integration tests [L4-1].
// Thread-safe via sync.RWMutex. Only present when built with '-tags testhooks'.
func SetAllowMainnetForTesting(allow bool) {
	allowMainnetMu.Lock()
	defer allowMainnetMu.Unlock()
	allowMainnetVal = allow
}

// LockNetworkForTesting serializes unit tests that mutate global network profiles,
// activation heights, or network hooks. It registers a t.Cleanup callback that
// automatically restores default network state and releases the lock, guaranteeing
// cleanup even if a test fails or panics.
func LockNetworkForTesting(t testing.TB) {
	unlock := AcquireNetworkTestLock()
	SetArmaxHashActivationHeight(-1)
	SetArmaxHashV2ActivationHeight(-1)
	t.Cleanup(func() {
		ResetArmaxHashActivationHeightOverride()
		ResetArmaxHashV2ActivationHeightOverride()
		SetNetworkForTesting(true)
		_ = SetNetwork("testnet")
		unlock()
	})
}
