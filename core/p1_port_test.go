package core

import (
	"strconv"
	"testing"
)

// TestP1_PortConstants verifies TASK P1 requirements:
// (a) Exact port mapping table, all unique, within 19332-19343.
// (b) MainnetProfile and TestnetProfile match expected ports.
func TestP1_PortConstants(t *testing.T) {
	expectedPorts := map[string]string{
		"MainnetRPC":     "19332",
		"TestnetRPC":     "19333",
		"MainnetP2P":     "19334",
		"TestnetP2P":     "19335",
		"MainnetZMQ":     "19336",
		"TestnetZMQ":     "19337",
		"MainnetStratum": "19338",
		"TestnetStratum": "19339",
		"APIExplorer":    "19340",
		"WebExplorer":    "19341",
		"WalletAPI":      "19342",
		"Metrics":        "19343",
	}

	actualPorts := map[string]string{
		"MainnetRPC":     MainnetRPCPort,
		"TestnetRPC":     TestnetRPCPort,
		"MainnetP2P":     MainnetP2PPort,
		"TestnetP2P":     TestnetP2PPort,
		"MainnetZMQ":     MainnetZMQPort,
		"TestnetZMQ":     TestnetZMQPort,
		"MainnetStratum": MainnetStratumPort,
		"TestnetStratum": TestnetStratumPort,
		"APIExplorer":    APIExplorerPort,
		"WebExplorer":    WebExplorerPort,
		"WalletAPI":      WalletAPIPort,
		"Metrics":        MetricsPort,
	}

	seenPorts := make(map[string]string)
	for name, exp := range expectedPorts {
		act, ok := actualPorts[name]
		if !ok {
			t.Fatalf("Missing port definition for %s", name)
		}
		if act != exp {
			t.Errorf("[%s] Expected port %s, got %s", name, exp, act)
		}

		pNum, err := strconv.Atoi(act)
		if err != nil || pNum < 19332 || pNum > 19343 {
			t.Errorf("[%s] Port %s is outside range 19332-19343", name, act)
		}

		if prev, exists := seenPorts[act]; exists {
			t.Errorf("Duplicate port %s detected between %s and %s", act, prev, name)
		}
		seenPorts[act] = name
	}

	// Verify MainnetProfile
	if MainnetProfile.RPCPort != "19332" {
		t.Errorf("Expected MainnetProfile.RPCPort 19332, got %s", MainnetProfile.RPCPort)
	}
	if MainnetProfile.P2PPort != "19334" {
		t.Errorf("Expected MainnetProfile.P2PPort 19334, got %s", MainnetProfile.P2PPort)
	}
	if MainnetProfile.ZMQPort != "19336" {
		t.Errorf("Expected MainnetProfile.ZMQPort 19336, got %s", MainnetProfile.ZMQPort)
	}
	if MainnetProfile.StratumPort != "19338" {
		t.Errorf("Expected MainnetProfile.StratumPort 19338, got %s", MainnetProfile.StratumPort)
	}

	// Verify TestnetProfile
	if TestnetProfile.RPCPort != "19333" {
		t.Errorf("Expected TestnetProfile.RPCPort 19333, got %s", TestnetProfile.RPCPort)
	}
	if TestnetProfile.P2PPort != "19335" {
		t.Errorf("Expected TestnetProfile.P2PPort 19335, got %s", TestnetProfile.P2PPort)
	}
	if TestnetProfile.ZMQPort != "19337" {
		t.Errorf("Expected TestnetProfile.ZMQPort 19337, got %s", TestnetProfile.ZMQPort)
	}
	if TestnetProfile.StratumPort != "19339" {
		t.Errorf("Expected TestnetProfile.StratumPort 19339, got %s", TestnetProfile.StratumPort)
	}
}
