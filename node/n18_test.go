package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN18_LoadConfigEnvOverridesWithoutFile verifies [N-18]:
// LoadConfig applies env overrides (ARMX_RPC_USER, ARMX_RPC_PASSWORD, ARMX_MINE_KEY)
// even when the config file does not exist on disk.
func TestN18_LoadConfigEnvOverridesWithoutFile(t *testing.T) {
	t.Setenv("ARMX_RPC_USER", "env_user_test")
	t.Setenv("ARMX_RPC_PASSWORD", "env_pass_test")
	t.Setenv("ARMX_MINE_KEY", "env_key_test")

	nonExistent := filepath.Join(t.TempDir(), "does_not_exist.json")
	cfg, err := LoadConfig(nonExistent)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.RPCUser != "env_user_test" {
		t.Fatalf("[N-18] Expected RPCUser from env 'env_user_test', got %q", cfg.RPCUser)
	}
	if cfg.RPCPassword != "env_pass_test" {
		t.Fatalf("[N-18] Expected RPCPassword from env 'env_pass_test', got %q", cfg.RPCPassword)
	}
	if cfg.MineKey != "env_key_test" {
		t.Fatalf("[N-18] Expected MineKey from env 'env_key_test', got %q", cfg.MineKey)
	}
}

func TestN18_SaveConfigIsAtomicAndRestricted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "armax.conf")
	cfg := DefaultConfig()
	cfg.RPCPassword = "not-for-logs"
	if err := cfg.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || string(data) == "not-for-logs" {
		t.Fatal("config was not serialized correctly")
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".armax-conf-*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary config files were left behind: %v, %v", matches, err)
	}
}

// TestN18_ApplyConfigToPeerServer verifies that [N-18] options
func TestN18_ApplyConfigToPeerServer(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	_ = core.SetNetwork("testnet")
	bc := core.NewBlockchain(1)
	defer bc.Close()

	cfg := &Config{
		NetworkID:          "ARMX-TESTNET-1",
		RPCBind:            "127.0.0.1",
		RPCPort:            "8085",
		P2PPort:            getFreePort(t),
		RPCUser:            "user",
		RPCPassword:        "pass",
		MineKey:            "secret_key_123",
		MaxPeers:           88,
		RequireTLS:         true,
		DisableSubnetLimit: true,
	}

	peerServer, serverCfg, err := InitServerFromConfig(bc, cfg)
	if err != nil {
		t.Fatalf("InitServerFromConfig failed: %v", err)
	}

	if !peerServer.RequireTLS {
		t.Fatalf("[N-18] peerServer.RequireTLS was not set to true")
	}
	if !peerServer.DisableSubnetLimit {
		t.Fatalf("[N-18] peerServer.DisableSubnetLimit was not set to true")
	}
	if peerServer.MaxPeers != 88 {
		t.Fatalf("[N-18] peerServer.MaxPeers expected 88, got %d", peerServer.MaxPeers)
	}
	if serverCfg.MineKey != "secret_key_123" {
		t.Fatalf("[N-18] serverCfg.MineKey expected 'secret_key_123', got %q", serverCfg.MineKey)
	}
	if core.GetActiveNetwork().NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("[N-18] Active network expected 'ARMX-TESTNET-1', got %q", core.GetActiveNetwork().NetworkID)
	}
}
