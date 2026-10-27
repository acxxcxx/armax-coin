package core

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Canonical Port Constants Table [TASK P1]
// Standard allocation table for Armaxcoin network services (Range: 19332-19343).
// RPC default bind is 127.0.0.1. Shared services (19340-19343) will also bind strictly to 127.0.0.1 by default when implemented.
const (
	// Mainnet Ports
	MainnetRPCPort     = "19332"
	MainnetP2PPort     = "19334"
	MainnetZMQPort     = "19336" // (reserved constant; service not yet instantiated)
	MainnetStratumPort = "19338" // (reserved constant; service not yet instantiated)

	// Testnet Ports
	TestnetRPCPort     = "19333"
	TestnetP2PPort     = "19335"
	TestnetZMQPort     = "19337" // (reserved constant; service not yet instantiated)
	TestnetStratumPort = "19339" // (reserved constant; service not yet instantiated)

	// Shared Services (Common across profiles; default bind to 127.0.0.1)
	APIExplorerPort = "19340"
	WebExplorerPort = "19341"
	WalletAPIPort   = "19342"
	MetricsPort     = "19343"
)

// NetworkProfile encapsulates network-specific parameters and ports [C-33, TASK P1, L3-4]
type NetworkProfile struct {
	Name                        string   `json:"name"`
	NetworkID                   string   `json:"network_id"`
	P2PPort                     string   `json:"p2p_port"`
	RPCPort                     string   `json:"rpc_port"`
	ZMQPort                     string   `json:"zmq_port"`
	StratumPort                 string   `json:"stratum_port"`
	P2PMagic                    string   `json:"p2p_magic"`
	PowLimitHex                 string   `json:"pow_limit_hex"`           // Minimum difficulty ceiling / maximum allowed target [L3-4]
	ArmaxHashPowLimitHex        string   `json:"armaxhash_pow_limit_hex"` // Initial bootstrap target ceiling when ArmaxHash activates
	DefaultSeeds                []string `json:"default_seeds"`
	CoinbaseMaturity            int      `json:"coinbase_maturity"`
	IsMainnet                   bool     `json:"is_mainnet"`                     // [F3] Stable immutable network identity: strictly true only for Mainnet
	ArmaxHashActivationHeight   int64    `json:"armaxhash_activation_height"`    // Block height where PoW switches to ArmaxHash v1 (-1 = disabled / SHA-256d)
	ArmaxHashV2ActivationHeight int64    `json:"armaxhash_v2_activation_height"` // Block height where PoW switches to ArmaxHash v2 Stream-Fast (-1 = disabled)
	// TimestampRulesActivationHeight is the first block height validated with the C1/C2 timestamp
	// rules (MTP-55 and non-decreasing timestamps). A negative value disables the rules.
	TimestampRulesActivationHeight int64 `json:"timestamp_rules_activation_height"`
}

var (
	// MainnetProfile defines production network settings (requiring >= 3 independent seed operators) [N-20, TASK P1]
	MainnetProfile = NetworkProfile{
		Name:                        "mainnet",
		NetworkID:                   "ARMX-MAINNET-1",
		IsMainnet:                   true, // [F3]
		P2PPort:                     MainnetP2PPort,
		RPCPort:                     MainnetRPCPort,
		ZMQPort:                     MainnetZMQPort,
		StratumPort:                 MainnetStratumPort,
		P2PMagic:                    "ARMX_MAIN",
		PowLimitHex:                 GenesisTargetHex,
		ArmaxHashPowLimitHex:        "00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // Difficulty 5 bootstrap
		CoinbaseMaturity:            100,
		ArmaxHashActivationHeight:   -1,
		ArmaxHashV2ActivationHeight: -1,
		// Mainnet has not launched, so the C1/C2 timestamp rules apply from the first block.
		TimestampRulesActivationHeight: 0,
		DefaultSeeds: []string{
			"id.armax-coin.cloud:" + MainnetP2PPort,
			"armax-coin.cloud:" + MainnetP2PPort,
			"202.10.34.180:" + MainnetP2PPort,
		},
	}

	// TestnetProfile defines public test network settings (requiring >= 3 independent seed operators) [N-20, TASK P1]
	TestnetProfile = NetworkProfile{
		Name:                        "testnet",
		NetworkID:                   "ARMX-TESTNET-1",
		P2PPort:                     TestnetP2PPort,
		RPCPort:                     TestnetRPCPort,
		ZMQPort:                     TestnetZMQPort,
		StratumPort:                 TestnetStratumPort,
		P2PMagic:                    "ARMX_TEST",
		PowLimitHex:                 TestnetGenesisTargetHex,
		ArmaxHashPowLimitHex:        "000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // Difficulty 3 bootstrap (4,096 hashes)
		CoinbaseMaturity:            10,
		ArmaxHashActivationHeight:   520,  // Activated at block #520 on testnet
		ArmaxHashV2ActivationHeight: 2500, // Hard fork activation at block #2500 on testnet
		// Hard fork: C1/C2 timestamp rules (MTP-55, non-decreasing timestamps) from block #8000 on testnet.
		TimestampRulesActivationHeight: 8000,
		DefaultSeeds: []string{
			"testnet-seed1.armax-coin.cloud:" + TestnetP2PPort,
			"testnet-seed2.armax-coin.cloud:" + TestnetP2PPort,
			"testnet-seed3.armax-coin.cloud:" + TestnetP2PPort,
		},
	}

	// SoakProfile defines local test profile with 16-bit target (Difficulty 4) for soak/chaos testing [L3-5]
	SoakProfile = NetworkProfile{
		Name:                        "soak",
		NetworkID:                   "ARMX-SOAK-1",
		P2PPort:                     "29335",
		RPCPort:                     "29333",
		P2PMagic:                    "ARMX_SOAK",
		PowLimitHex:                 "0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		ArmaxHashPowLimitHex:        "0000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // Difficulty 4 bootstrap
		CoinbaseMaturity:            100,
		ArmaxHashActivationHeight:   -1,
		ArmaxHashV2ActivationHeight: -1,
		DefaultSeeds:                []string{},

		TimestampRulesActivationHeight: -1,
	}

	// RegtestProfile defines private local regression testing profile [L4-8]
	RegtestProfile = NetworkProfile{
		Name:                        "regtest",
		NetworkID:                   "ARMX-REGTEST-1",
		P2PPort:                     "29334",
		RPCPort:                     "29332",
		ZMQPort:                     "29333",
		StratumPort:                 "29335",
		P2PMagic:                    "ARMX_REGT",
		PowLimitHex:                 "00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // 8-bit low target [L4-8]
		ArmaxHashPowLimitHex:        "00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // Difficulty 2 bootstrap
		CoinbaseMaturity:            1,
		ArmaxHashActivationHeight:   -1,
		ArmaxHashV2ActivationHeight: -1,
		DefaultSeeds:                []string{},

		TimestampRulesActivationHeight: -1,
	}
)

// MainnetLaunched indicates whether production mainnet has officially launched [L4-1].
// While false, mainnet is locked and all tools default to testnet.
const MainnetLaunched = false

var (
	netMu                               sync.RWMutex
	ActiveNetwork                       = TestnetProfile // [L4-1] Testnet is default network profile
	networkInitialized                  bool
	allowNetworkResetHook               func() bool
	allowMainnetInTestsHook             func() bool
	networkIDOverrideHook               func() string
	armaxHashActivationHeightOverride   *int64 // nil means use network profile or env
	armaxHashV2ActivationHeightOverride *int64 // nil means use network profile or env
	timestampRulesHeightOverride        *int64 // nil means use network profile
	testGlobalNetworkLock               sync.Mutex
)

// NetworkID returns the active network profile's NetworkID dynamically [L1-2].
// It safely snapshots the hook and default network ID while holding netMu.RLock,
// and invokes the hook outside the lock to prevent re-entrant deadlocks.
// NOTE: Snapshotting the hook pointer ensures that accessing the hook variable is
// thread-safe and allows the hook itself to re-entrantly invoke functions that acquire
// netMu.Lock() without deadlock. However, any internal state, shared objects, or closures
// accessed by the hook function must be managed thread-safely by the caller.
func NetworkID() string {
	netMu.RLock()
	hook := networkIDOverrideHook
	defaultNetID := ActiveNetwork.NetworkID
	netMu.RUnlock()

	if hook != nil {
		return hook()
	}
	return defaultNetID
}

// AcquireNetworkTestLock acquires the global test mutex to serialize tests that mutate
// network profiles, activation heights, or network hooks.
// It returns an unlock function suitable for defer or t.Cleanup.
func AcquireNetworkTestLock() func() {
	testGlobalNetworkLock.Lock()
	return func() {
		testGlobalNetworkLock.Unlock()
	}
}

// SetNetworkIDOverrideForTesting installs a temporary hook to override NetworkID in tests.
// It returns a cleanup function that restores the previous hook state.
func SetNetworkIDOverrideForTesting(fn func() string) func() {
	netMu.Lock()
	prev := networkIDOverrideHook
	networkIDOverrideHook = fn
	netMu.Unlock()
	return func() {
		netMu.Lock()
		networkIDOverrideHook = prev
		netMu.Unlock()
	}
}

// GetArmaxHashActivationHeight returns the block height where ArmaxHash PoW activates.
// An activation height < 0 indicates ArmaxHash is disabled (using legacy SHA-256d).
func GetArmaxHashActivationHeight() int64 {
	netMu.RLock()
	defer netMu.RUnlock()
	if armaxHashActivationHeightOverride != nil {
		return *armaxHashActivationHeightOverride
	}
	if val := os.Getenv("ARMX_ARMAXHASH_HEIGHT"); val != "" {
		if h, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return h
		}
	}
	return ActiveNetwork.ArmaxHashActivationHeight
}

// SetArmaxHashActivationHeight dynamically sets or overrides the ArmaxHash activation height.
// Passing -1 disables ArmaxHash (reverting to SHA-256d only).
func SetArmaxHashActivationHeight(height int64) {
	netMu.Lock()
	defer netMu.Unlock()
	armaxHashActivationHeightOverride = &height
}

// ResetArmaxHashActivationHeightOverride clears any dynamic override, reverting to network profile defaults.
func ResetArmaxHashActivationHeightOverride() {
	netMu.Lock()
	defer netMu.Unlock()
	armaxHashActivationHeightOverride = nil
}

// IsArmaxHashActive returns true if the block at the specified index must be evaluated with ArmaxHash.
// ArmaxHash is strictly disabled when activation height <= 0 (e.g. default -1).
// Genesis block (index 0) is permanently locked to SHA-256d.
func IsArmaxHashActive(blockIndex int) bool {
	act := GetArmaxHashActivationHeight()
	if act <= 0 {
		return false
	}
	if blockIndex < 0 {
		return false
	}
	return int64(blockIndex) >= act
}

// GetArmaxHashV2ActivationHeight returns the block height where ArmaxHash v2 (Stream-Fast) activates.
// An activation height <= 0 indicates ArmaxHash v2 is disabled.
func GetArmaxHashV2ActivationHeight() int64 {
	netMu.RLock()
	defer netMu.RUnlock()
	if armaxHashV2ActivationHeightOverride != nil {
		return *armaxHashV2ActivationHeightOverride
	}
	if val := os.Getenv("ARMX_ARMAXHASH_V2_HEIGHT"); val != "" {
		if h, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return h
		}
	}
	return ActiveNetwork.ArmaxHashV2ActivationHeight
}

// SetArmaxHashV2ActivationHeight dynamically sets or overrides the ArmaxHash v2 activation height.
// Passing -1 disables ArmaxHash v2.
func SetArmaxHashV2ActivationHeight(height int64) {
	netMu.Lock()
	defer netMu.Unlock()
	armaxHashV2ActivationHeightOverride = &height
}

// ResetArmaxHashV2ActivationHeightOverride clears any dynamic override, reverting to network profile defaults.
func ResetArmaxHashV2ActivationHeightOverride() {
	netMu.Lock()
	defer netMu.Unlock()
	armaxHashV2ActivationHeightOverride = nil
}

// IsArmaxHashV2Active returns true if the block at the specified index must be evaluated with ArmaxHash v2.
// ArmaxHash v2 is strictly disabled when activation height <= 0 (e.g. default -1).
func IsArmaxHashV2Active(blockIndex int) bool {
	act := GetArmaxHashV2ActivationHeight()
	if act <= 0 {
		return false
	}
	if blockIndex < 0 {
		return false
	}
	return int64(blockIndex) >= act
}

// SetNetwork switches the active network profile thread-safely (e.g. "mainnet", "testnet", "soak") [C-33, L1-2, L3-5, L4-1]
// It must only be called ONCE at application startup; subsequent calls outside testing return an error.
func SetNetwork(name string) error {
	netMu.Lock()
	defer netMu.Unlock()

	allowReset := false
	if allowNetworkResetHook != nil {
		allowReset = allowNetworkResetHook()
	}

	if networkInitialized && !allowReset {
		return fmt.Errorf("network profile already initialized (cannot be changed after start)")
	}

	switch strings.ToLower(strings.TrimSpace(name)) {
	case "testnet", "armx-testnet-1", "":
		ActiveNetwork = TestnetProfile
	case "mainnet", "armx-mainnet-1":
		if !MainnetLaunched {
			allowTest := false
			if allowMainnetInTestsHook != nil {
				allowTest = allowMainnetInTestsHook()
			}
			if !allowTest {
				//lint:ignore ST1005 Exact user-specified error message required [L4-1]
				return fmt.Errorf("Mainnet has not been launched; please use -network testnet")
			}
		}
		ActiveNetwork = MainnetProfile
	case "soak", "armx-soak-1", "test":
		ActiveNetwork = SoakProfile
	case "regtest", "armx-regtest-1":
		ActiveNetwork = RegtestProfile
	default:
		return fmt.Errorf("unknown network profile: %s (supported: testnet, mainnet, regtest, soak)", name)
	}
	networkInitialized = true
	return nil
}

// GetActiveNetwork returns a copy of the currently active network profile [C-33]
func GetActiveNetwork() NetworkProfile {
	netMu.RLock()
	defer netMu.RUnlock()
	prof := ActiveNetwork
	if len(ActiveNetwork.DefaultSeeds) > 0 {
		prof.DefaultSeeds = make([]string, len(ActiveNetwork.DefaultSeeds))
		copy(prof.DefaultSeeds, ActiveNetwork.DefaultSeeds)
	}
	return prof
}

// CLINetworkConfig resolves effective network parameters for CLI and GUI tools [L3-8]
type CLINetworkConfig struct {
	NetworkProfile NetworkProfile
	RPCPort        string
	P2PPort        string
	DataDir        string
	DefaultSeeds   []string
	DaemonURL      string
}

// ResolveCLINetwork resolves the active profile and returns the derived ports, seeds, data dir, and default daemon URL [L3-8, L4-1]
func ResolveCLINetwork(networkName string) (CLINetworkConfig, error) {
	var prof NetworkProfile
	switch strings.ToLower(strings.TrimSpace(networkName)) {
	case "testnet", "armx-testnet-1", "":
		prof = TestnetProfile
	case "mainnet", "armx-mainnet-1":
		if !MainnetLaunched {
			allowTest := false
			if allowMainnetInTestsHook != nil {
				allowTest = allowMainnetInTestsHook()
			}
			if !allowTest {
				//lint:ignore ST1005 Exact user-specified error message required [L4-1]
				return CLINetworkConfig{}, fmt.Errorf("Mainnet has not been launched; please use -network testnet")
			}
		}
		prof = MainnetProfile
	case "soak", "armx-soak-1", "test":
		prof = SoakProfile
	case "regtest", "armx-regtest-1":
		prof = RegtestProfile
	default:
		return CLINetworkConfig{}, fmt.Errorf("unknown network profile: %s (supported: testnet, mainnet, regtest, soak)", networkName)
	}

	dataDir := "data"
	if prof.Name == "testnet" {
		dataDir = "testnet"
	} else if prof.Name == "regtest" {
		dataDir = "regtest"
	} else if prof.Name == "soak" {
		dataDir = "soak"
	}

	seeds := make([]string, len(prof.DefaultSeeds))
	copy(seeds, prof.DefaultSeeds)

	return CLINetworkConfig{
		NetworkProfile: prof,
		RPCPort:        prof.RPCPort,
		P2PPort:        prof.P2PPort,
		DataDir:        dataDir,
		DefaultSeeds:   seeds,
		DaemonURL:      "http://127.0.0.1:" + prof.RPCPort,
	}, nil
}
