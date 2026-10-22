package core

import (
	"fmt"
	"testing"
)

// TestConsensusParametersFrozen locks in all final consensus, network, emission, and genesis constants.
// If any consensus parameter is inadvertently altered, this test will fail immediately.
func TestConsensusParametersFrozen(t *testing.T) {
	// 1. Emission & Supply Schedule Invariants
	if CoinUnit != 100_000_000 {
		t.Fatalf("CoinUnit mismatch: expected 100000000, got %d", CoinUnit)
	}
	expectedMaxSupply := int64(126_000_000) * CoinUnit
	if MaxSupply != expectedMaxSupply {
		t.Fatalf("MaxSupply mismatch: expected %d, got %d", expectedMaxSupply, MaxSupply)
	}
	expectedInitialReward := int64(60) * CoinUnit
	if InitialReward != expectedInitialReward {
		t.Fatalf("InitialReward mismatch: expected %d, got %d", expectedInitialReward, InitialReward)
	}
	if HalvingInterval != 1_050_000 {
		t.Fatalf("HalvingInterval mismatch: expected 1050000, got %d", HalvingInterval)
	}
	if MinRelayFee != 1000 {
		t.Fatalf("MinRelayFee mismatch: expected 1000, got %d", MinRelayFee)
	}

	// 2. Block & Transaction Size Boundaries
	if MaxBlockSize != 2_000_000 {
		t.Fatalf("MaxBlockSize mismatch: expected 2000000, got %d", MaxBlockSize)
	}
	if MaxTxSize != 20_000 {
		t.Fatalf("MaxTxSize mismatch: expected 20000, got %d", MaxTxSize)
	}
	if MaxBlockTransactions != 1000 {
		t.Fatalf("MaxBlockTransactions mismatch: expected 1000, got %d", MaxBlockTransactions)
	}
	if MaxReorganizationDepth != 720 {
		t.Fatalf("MaxReorganizationDepth mismatch: expected 720, got %d", MaxReorganizationDepth)
	}

	// 3. Cryptographic & Address Standards
	if Bech32HRP != "armx" {
		t.Fatalf("Bech32HRP mismatch: expected 'armx', got '%s'", Bech32HRP)
	}
	if AddressPayloadLen != 32 {
		t.Fatalf("AddressPayloadLen mismatch: expected 32, got %d", AddressPayloadLen)
	}

	// 4. Mainnet Network Profile & Ports
	if MainnetProfile.NetworkID != "ARMX-MAINNET-1" {
		t.Fatalf("Mainnet NetworkID mismatch: got %s", MainnetProfile.NetworkID)
	}
	if MainnetProfile.P2PPort != "19334" {
		t.Fatalf("Mainnet P2PPort mismatch: got %s", MainnetProfile.P2PPort)
	}
	if MainnetProfile.RPCPort != "19332" {
		t.Fatalf("Mainnet RPCPort mismatch: got %s", MainnetProfile.RPCPort)
	}
	if MainnetProfile.P2PMagic != "ARMX_MAIN" {
		t.Fatalf("Mainnet P2PMagic mismatch: got %s", MainnetProfile.P2PMagic)
	}

	// 5. Testnet Network Profile & Ports
	if TestnetProfile.NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("Testnet NetworkID mismatch: got %s", TestnetProfile.NetworkID)
	}
	if TestnetProfile.P2PPort != "19335" {
		t.Fatalf("Testnet P2PPort mismatch: got %s", TestnetProfile.P2PPort)
	}
	if TestnetProfile.RPCPort != "19333" {
		t.Fatalf("Testnet RPCPort mismatch: got %s", TestnetProfile.RPCPort)
	}
	if TestnetProfile.P2PMagic != "ARMX_TEST" {
		t.Fatalf("Testnet P2PMagic mismatch: got %s", TestnetProfile.P2PMagic)
	}

	// 6. Mainnet Genesis Block Specification
	mainnetGen := GetMainnetGenesisBlock()
	if mainnetGen.Index != 0 {
		t.Fatalf("Mainnet Genesis Index mismatch: got %d", mainnetGen.Index)
	}
	if mainnetGen.Timestamp != 1710000000 {
		t.Fatalf("Mainnet Genesis Timestamp mismatch: got %d", mainnetGen.Timestamp)
	}
	if mainnetGen.Nonce != 7 {
		t.Fatalf("Mainnet Genesis Nonce mismatch: got %d", mainnetGen.Nonce)
	}
	if mainnetGen.Hash != "0820d42962071e01c8a7d2e6a3aa5640a17f8a60e9006bf59b6e76ac0001535e" {
		t.Fatalf("Mainnet Genesis Hash mismatch: got %s", mainnetGen.Hash)
	}
	if mainnetGen.MerkleRoot != "ca91c25a9cb1c4d2f723a1849123ac18c71eac4958391b82fc51f56874f49d76" {
		t.Fatalf("Mainnet Genesis MerkleRoot mismatch: got %s", mainnetGen.MerkleRoot)
	}
	if mainnetGen.Miner != "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd" {
		t.Fatalf("Mainnet Genesis Miner mismatch: got %s", mainnetGen.Miner)
	}
	if len(mainnetGen.Transactions) != 1 || mainnetGen.Transactions[0].Amount != InitialReward {
		t.Fatalf("Mainnet Genesis Reward mismatch: got %d", mainnetGen.Transactions[0].Amount)
	}
	if mainnetGen.CalculateHash() != mainnetGen.Hash {
		t.Fatalf("Mainnet Genesis Calculated Hash mismatch: %s != %s", mainnetGen.CalculateHash(), mainnetGen.Hash)
	}

	// 7. Testnet Genesis Block Specification
	testnetGen := GetTestnetGenesisBlock()
	if testnetGen.Index != 0 {
		t.Fatalf("Testnet Genesis Index mismatch: got %d", testnetGen.Index)
	}
	if testnetGen.Timestamp != 1710000001 {
		t.Fatalf("Testnet Genesis Timestamp mismatch: got %d", testnetGen.Timestamp)
	}
	if testnetGen.Nonce != 40272237 {
		t.Fatalf("Testnet Genesis Nonce mismatch: got %d", testnetGen.Nonce)
	}
	if testnetGen.Hash != "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b" {
		t.Fatalf("Testnet Genesis Hash mismatch: got %s", testnetGen.Hash)
	}
	if testnetGen.MerkleRoot != "808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d" {
		t.Fatalf("Testnet Genesis MerkleRoot mismatch: got %s", testnetGen.MerkleRoot)
	}
	if testnetGen.Miner != "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd" {
		t.Fatalf("Testnet Genesis Miner mismatch: got %s", testnetGen.Miner)
	}
	if len(testnetGen.Transactions) != 1 || testnetGen.Transactions[0].Amount != InitialReward {
		t.Fatalf("Testnet Genesis Reward mismatch: got %d", testnetGen.Transactions[0].Amount)
	}
	if testnetGen.CalculateHash() != testnetGen.Hash {
		t.Fatalf("Testnet Genesis Calculated Hash mismatch: %s != %s", testnetGen.CalculateHash(), testnetGen.Hash)
	}

	// 8. Prove Total Halving Sum Mathematical Equivalence
	var totalCalculatedEmission int64 = 0
	for epoch := 0; epoch < 64; epoch++ {
		reward := InitialReward >> epoch
		if reward == 0 {
			break
		}
		epochTotal := reward * int64(HalvingInterval)
		totalCalculatedEmission += epochTotal
	}
	// With 60 ARMX * 1,050,000 blocks: sum = 63M + 31.5M + 15.75M + ... = 125,999,999.874 ARMX <= MaxSupply
	if totalCalculatedEmission > MaxSupply {
		t.Fatalf("Emission invariant breached: total calculated %d > MaxSupply %d", totalCalculatedEmission, MaxSupply)
	}
	fmt.Printf("[Consensus Freeze] Verified all consensus invariants. Total lifetime emission: %d Armoids (%.8f ARMX / Cap: %d ARMX)\n",
		totalCalculatedEmission, float64(totalCalculatedEmission)/1e8, MaxSupply/CoinUnit)
}
