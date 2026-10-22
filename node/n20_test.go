//go:build testhooks

package node

import (
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN20_ProfileBasedSeedPeers verifies [N-20]:
// Seed peers are profile-aware (mainnet vs testnet) and configured from network profiles.
func TestN20_ProfileBasedSeedPeers(t *testing.T) {
	// 1. Test mainnet seeds
	_ = core.SetNetwork("mainnet")
	cfgMain := DefaultConfig()
	if len(cfgMain.SeedPeers) < 3 {
		t.Fatalf("[N-20] Expected at least 3 independent seeds on mainnet, got %d", len(cfgMain.SeedPeers))
	}
	if cfgMain.P2PPort != core.MainnetP2PPort {
		t.Fatalf("[N-20] Expected P2PPort %s on mainnet, got %s", core.MainnetP2PPort, cfgMain.P2PPort)
	}

	// 2. Test testnet seeds
	_ = core.SetNetwork("testnet")
	cfgTest := DefaultConfig()
	if len(cfgTest.SeedPeers) < 3 {
		t.Fatalf("[N-20] Expected at least 3 independent seeds on testnet, got %d", len(cfgTest.SeedPeers))
	}
	if cfgTest.P2PPort != core.TestnetP2PPort {
		t.Fatalf("[N-20] Expected P2PPort %s on testnet, got %s", core.TestnetP2PPort, cfgTest.P2PPort)
	}

	// Restore mainnet
	_ = core.SetNetwork("mainnet")
}
