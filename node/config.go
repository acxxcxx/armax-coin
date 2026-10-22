package node

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/acxxcxx/armax-coin/core"
)

type Config struct {
	NetworkID           string   `json:"network_id"`
	DataDir             string   `json:"data_dir,omitempty"` // [P1-A] Instance base data directory
	RPCBind             string   `json:"rpc_bind"`
	RPCPort             string   `json:"rpc_port"`
	P2PPort             string   `json:"p2p_port"`
	ZMQPort             string   `json:"zmq_port,omitempty"`     // [TASK P1] Reserved ZMQ port
	StratumPort         string   `json:"stratum_port,omitempty"` // [TASK P1] Reserved Stratum port
	RPCUser             string   `json:"rpc_user"`
	RPCPassword         string   `json:"rpc_password"`
	RPCTLSCert          string   `json:"rpc_tls_cert,omitempty"` // PEM certificate; mandatory for non-loopback RPC
	RPCTLSKey           string   `json:"rpc_tls_key,omitempty"`  // PEM private key; mandatory for non-loopback RPC
	MineKey             string   `json:"mine_key"`               // [N-18] Secret header key for /mine endpoint
	MinerAddress        string   `json:"miner_address"`
	EnableMining        bool     `json:"enable_mining"`
	MiningThreads       int      `json:"mining_threads,omitempty"`
	MiningDelayMS       int      `json:"mining_delay_ms"`
	SeedPeers           []string `json:"seed_peers"`
	MaxPeers            int      `json:"max_peers"`
	RequireTLS          bool     `json:"require_tls"`            // [N-08] Require strict TLS 1.3 without unencrypted fallback
	DisableSubnetLimit  bool     `json:"disable_subnet_limit"`   // [N-12] Allow multiple connections from same subnet (LAN/testnet)
	P2PTOFU             bool     `json:"p2p_tofu"`               // [F1] Enable Trust-On-First-Use (TOFU) TLS certificate fingerprint pinning
	AllowNonLoopbackRPC bool     `json:"allow_non_loopback_rpc"` // [F8] Explicit opt-in required to bind RPC to non-loopback addresses
}

// DefaultConfig returns production default settings for active network profile [N-20, TASK P1]
func DefaultConfig() *Config {
	net := core.GetActiveNetwork()
	p2pPort := net.P2PPort
	if p2pPort == "" {
		p2pPort = core.MainnetP2PPort
	}
	rpcPort := net.RPCPort
	if rpcPort == "" {
		rpcPort = core.MainnetRPCPort
	}
	zmqPort := net.ZMQPort
	if zmqPort == "" {
		zmqPort = core.MainnetZMQPort
	}
	stratumPort := net.StratumPort
	if stratumPort == "" {
		stratumPort = core.MainnetStratumPort
	}

	seeds := make([]string, len(net.DefaultSeeds))
	copy(seeds, net.DefaultSeeds)
	if len(seeds) == 0 {
		if net.Name == "testnet" {
			seeds = []string{
				"testnet-seed1.armax-coin.cloud:" + core.TestnetP2PPort,
				"testnet-seed2.armax-coin.cloud:" + core.TestnetP2PPort,
				"testnet-seed3.armax-coin.cloud:" + core.TestnetP2PPort,
			}
		} else {
			seeds = []string{
				"id.armax-coin.cloud:" + core.MainnetP2PPort,
				"armax-coin.cloud:" + core.MainnetP2PPort,
				"202.10.34.180:" + core.MainnetP2PPort,
			}
		}
	}

	return &Config{
		NetworkID:     net.NetworkID,
		RPCBind:       "127.0.0.1",
		RPCPort:       rpcPort,
		P2PPort:       p2pPort,
		ZMQPort:       zmqPort,
		StratumPort:   stratumPort,
		RPCUser:       "",
		RPCPassword:   "",
		MineKey:       "",
		MinerAddress:  "",
		EnableMining:  false,
		MiningDelayMS: 2000,
		SeedPeers:     seeds,
		MaxPeers:      50,
		// Production nodes must not silently downgrade P2P traffic to plaintext.
		RequireTLS: true,
		P2PTOFU:    true,
	}
}

// LoadConfig loads configuration from a JSON file, or returns default with env overrides if file does not exist [N-18]
func LoadConfig(filePath string) (*Config, error) {
	target := filePath
	if _, err := os.Stat(target); os.IsNotExist(err) {
		if execPath, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(execPath), filePath)
			if _, err := os.Stat(candidate); err == nil {
				target = candidate
			}
		}
	}

	cfg := DefaultConfig()

	if _, err := os.Stat(target); err == nil {
		data, err := os.ReadFile(target)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Environment variable overrides for sensitive RPC credentials and keys [R-09, N-18]
	if envUser := os.Getenv("ARMX_RPC_USER"); envUser != "" {
		cfg.RPCUser = envUser
	}
	if envPass := os.Getenv("ARMX_RPC_PASSWORD"); envPass != "" {
		cfg.RPCPassword = envPass
	}
	if envMineKey := os.Getenv("ARMX_MINE_KEY"); envMineKey != "" {
		cfg.MineKey = envMineKey
	}
	if cert := os.Getenv("ARMX_RPC_TLS_CERT"); cert != "" {
		cfg.RPCTLSCert = cert
	}
	if key := os.Getenv("ARMX_RPC_TLS_KEY"); key != "" {
		cfg.RPCTLSKey = key
	}

	return cfg, nil
}

// SaveConfig writes the configuration atomically with restrictive permissions. RPC
// credentials and private-key paths must never be left in a partially-written file.
func (c *Config) SaveConfig(filePath string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".armax-conf-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return err
	}
	return os.Chmod(filePath, 0600)
}
