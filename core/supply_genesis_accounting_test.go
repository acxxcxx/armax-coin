package core

import (
	"math"
	"testing"
)

// TestSupplyEmissionGeometricSeries verifies [Section B.4 & B.5]:
// Integer arithmetic emission calculation over all halving epochs.
func TestSupplyEmissionGeometricSeries(t *testing.T) {
	var totalCalculatedEmission int64 = 0
	lastNonZeroEpoch := -1

	for epoch := 0; epoch < 64; epoch++ {
		reward := InitialReward >> epoch
		if reward == 0 {
			break
		}
		lastNonZeroEpoch = epoch
		epochTotal := reward * int64(HalvingInterval)

		// Integer overflow check
		if epochTotal < 0 || (totalCalculatedEmission+epochTotal) < totalCalculatedEmission {
			t.Fatalf("Integer overflow detected at epoch %d", epoch)
		}
		totalCalculatedEmission += epochTotal
	}

	// Verify exact integer arithmetic results
	expectedBaseUnits := int64(12599999987400000) // 125,999,999.87400000 ARMX
	if totalCalculatedEmission != expectedBaseUnits {
		t.Fatalf("Emission calculation mismatch: got %d Armoids, expected %d", totalCalculatedEmission, expectedBaseUnits)
	}

	if totalCalculatedEmission > MaxSupply {
		t.Fatalf("Emission exceeds MaxSupply: %d > %d", totalCalculatedEmission, MaxSupply)
	}

	// Epoch 32 has reward = 1 Armoid (6,000,000,000 >> 32 = 1)
	// Epoch 33 has reward = 0 Armoids (6,000,000,000 >> 33 = 0)
	if lastNonZeroEpoch != 32 {
		t.Fatalf("Expected last non-zero epoch to be 32, got %d", lastNonZeroEpoch)
	}
	reward32 := InitialReward >> 32
	if reward32 != 1 {
		t.Fatalf("Epoch 32 reward should be 1 Armoid, got %d", reward32)
	}
	reward33 := InitialReward >> 33
	if reward33 != 0 {
		t.Fatalf("Epoch 33 reward should be 0 Armoids, got %d", reward33)
	}

	t.Logf("[OK] Geometric series emission: %d Armoids (%.8f ARMX) strictly within MaxSupply %d",
		totalCalculatedEmission, float64(totalCalculatedEmission)/1e8, MaxSupply/CoinUnit)
}

// TestGenesisRewardIncludedInSupply verifies [Section B.1 & B.2]:
// Genesis block allocation of 60 ARMX is accounted for in circulating supply.
func TestGenesisRewardIncludedInSupply(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	// Initial chain height is 1 (Genesis block at index 0)
	if len(bc.Chain) != 1 {
		t.Fatalf("Expected fresh chain to have length 1, got %d", len(bc.Chain))
	}

	genBlock := bc.Chain[0]
	if len(genBlock.Transactions) != 1 {
		t.Fatalf("Genesis block must have exactly 1 coinbase tx, got %d", len(genBlock.Transactions))
	}

	cbTx := genBlock.Transactions[0]
	if cbTx.Amount != InitialReward {
		t.Fatalf("Genesis coinbase amount must be %d (60 ARMX), got %d", InitialReward, cbTx.Amount)
	}
	if cbTx.Recipient != GenesisMiner {
		t.Fatalf("Genesis coinbase recipient must be GenesisMiner, got %s", cbTx.Recipient)
	}

	// Circulating supply must explicitly include Genesis 60 ARMX
	supply := bc.GetCirculatingSupply()
	if supply != InitialReward {
		t.Fatalf("Initial circulating supply must equal Genesis reward %d, got %d", InitialReward, supply)
	}

	// State ledger must credit GenesisMiner with exactly InitialReward
	minerBal := bc.GetBalance(GenesisMiner)
	if minerBal != InitialReward {
		t.Fatalf("GenesisMiner account balance in state must be %d, got %d", InitialReward, minerBal)
	}

	t.Logf("[OK] Genesis allocation of 60 ARMX (%d Armoids) is strictly credited to GenesisMiner and counted in supply", supply)
}

// TestBurnAddressUnspendability verifies [Section B.3]:
// The burn address (GenesisMiner) is mathematically and cryptographically unspendable.
func TestBurnAddressUnspendability(t *testing.T) {
	// 1. Verify burn address payload is all zeros
	hrp, payload, err := Bech32Decode(GenesisMiner)
	if err != nil {
		t.Fatalf("Failed to decode GenesisMiner address: %v", err)
	}
	if hrp != "armx" {
		t.Fatalf("Expected HRP 'armx', got '%s'", hrp)
	}
	if len(payload) != AddressPayloadLen {
		t.Fatalf("Expected payload length %d, got %d", AddressPayloadLen, len(payload))
	}
	for i, b := range payload {
		if b != 0x00 {
			t.Fatalf("GenesisMiner payload byte %d is not 0x00: got %02x", i, b)
		}
	}

	// 2. Prove unspendability: A valid spender MUST present public key P such that:
	// SHA256(0x01 || P) == 32 bytes of zeros.
	// Since SHA-256 is preimage resistant, finding such P requires ~2^256 hash trials.
	// Furthermore, spending requires a valid ML-DSA-65 signature matching P.
	testAcc := newTestAccount(t)

	// Attempt to spend from GenesisMiner using an authentic keypair
	fakeTx := Transaction{
		SigAlg:    1,
		Sender:    GenesisMiner,   // Claiming to spend burned genesis funds
		SenderPub: testAcc.PubHex, // Spender's real public key
		Recipient: testAcc.Address,
		Amount:    10 * CoinUnit,
		Fee:       1000,
		Nonce:     1,
	}
	_ = fakeTx.Sign(testAcc.PrivKey)

	// Invariant: VerifyWithError must REJECT fakeTx because SenderPub does not hash to GenesisMiner!
	errVerify := fakeTx.VerifyWithError()
	if errVerify == nil {
		t.Fatalf("[SECURITY FATAL] Fake transaction claiming GenesisMiner address passed verification!")
	}
	t.Logf("[OK] Cryptographic unspendability verified: %v", errVerify)

	// Invariant: StateManager must reject a block containing unauthorized spend from GenesisMiner
	sm := NewStateManager()
	minerHex, _ := AddressToHex(GenesisMiner)
	sm.Accounts[minerHex] = AccountState{Balance: 60 * CoinUnit, Nonce: 0}

	fakeBlock := Block{
		Index: 1,
		Transactions: []Transaction{
			{Sender: "SYSTEM", Recipient: testAcc.Address, Amount: 60 * CoinUnit, Signature: "REWARD_SIG"},
			fakeTx,
		},
	}
	errApply := sm.VerifyAndApplyBlock(&fakeBlock, 60*CoinUnit)
	if errApply == nil {
		t.Fatalf("[SECURITY FATAL] State manager applied block with invalid sender public key!")
	}
	t.Logf("[OK] State ledger unspendability verified: %v", errApply)
}

// TestHalvingBoundariesAndCapClipping verifies [Section B.5]:
func TestHalvingBoundariesAndCapClipping(t *testing.T) {
	// Epoch 0 boundary
	r0 := CalculateBlockRewardGivenSupply(0, 0)
	if r0 != 60*CoinUnit {
		t.Fatalf("Block 0 reward mismatch: got %d", r0)
	}
	rLastEpoch0 := CalculateBlockRewardGivenSupply(HalvingInterval-1, 100*CoinUnit)
	if rLastEpoch0 != 60*CoinUnit {
		t.Fatalf("Block %d reward mismatch: got %d", HalvingInterval-1, rLastEpoch0)
	}

	// Epoch 1 boundary (first halving block: before, at, and after)
	rFirstEpoch1 := CalculateBlockRewardGivenSupply(HalvingInterval, 100*CoinUnit)
	if rFirstEpoch1 != 30*CoinUnit {
		t.Fatalf("Block %d reward mismatch: expected 30 ARMX, got %d", HalvingInterval, rFirstEpoch1)
	}
	rAfterEpoch1 := CalculateBlockRewardGivenSupply(HalvingInterval+1, 100*CoinUnit)
	if rAfterEpoch1 != 30*CoinUnit {
		t.Fatalf("Block %d reward mismatch: expected 30 ARMX, got %d", HalvingInterval+1, rAfterEpoch1)
	}

	// Epoch 2 boundary (second halving block: before, at, and after)
	rBeforeEpoch2 := CalculateBlockRewardGivenSupply(2*HalvingInterval-1, 100*CoinUnit)
	if rBeforeEpoch2 != 30*CoinUnit {
		t.Fatalf("Block %d reward mismatch: expected 30 ARMX, got %d", 2*HalvingInterval-1, rBeforeEpoch2)
	}
	rFirstEpoch2 := CalculateBlockRewardGivenSupply(2*HalvingInterval, 100*CoinUnit)
	if rFirstEpoch2 != 15*CoinUnit {
		t.Fatalf("Block %d reward mismatch: expected 15 ARMX, got %d", 2*HalvingInterval, rFirstEpoch2)
	}
	rAfterEpoch2 := CalculateBlockRewardGivenSupply(2*HalvingInterval+1, 100*CoinUnit)
	if rAfterEpoch2 != 15*CoinUnit {
		t.Fatalf("Block %d reward mismatch: expected 15 ARMX, got %d", 2*HalvingInterval+1, rAfterEpoch2)
	}

	// Final Epoch boundaries: Epoch 32 (last non-zero reward) and Epoch 33 (reward zeroed)
	rEpoch32 := CalculateBlockRewardGivenSupply(32*HalvingInterval, 100*CoinUnit)
	if rEpoch32 != 1 { // 6000000000 / 2^32 = 1 Armoid (0.00000001 ARMX)
		t.Fatalf("Epoch 32 reward mismatch: expected 1 Armoid, got %d", rEpoch32)
	}
	rEpoch33 := CalculateBlockRewardGivenSupply(33*HalvingInterval, 100*CoinUnit)
	if rEpoch33 != 0 { // 6000000000 / 2^33 = 0 (integer division)
		t.Fatalf("Epoch 33 reward mismatch: expected 0, got %d", rEpoch33)
	}
	rEpoch34 := CalculateBlockRewardGivenSupply(34*HalvingInterval, 100*CoinUnit)
	if rEpoch34 != 0 {
		t.Fatalf("Epoch 34 reward mismatch: expected 0, got %d", rEpoch34)
	}

	// Supply cap clipping: when 5 ARMX remain before MaxSupply
	remainingCap := int64(5) * CoinUnit
	rClipped := CalculateBlockRewardGivenSupply(100, MaxSupply-remainingCap)
	if rClipped != remainingCap {
		t.Fatalf("Reward should clip to remaining supply %d, got %d", remainingCap, rClipped)
	}

	// Supply cap reached: reward must be exactly 0
	rZero := CalculateBlockRewardGivenSupply(100, MaxSupply)
	if rZero != 0 {
		t.Fatalf("Reward at MaxSupply must be 0, got %d", rZero)
	}
	rPastCap := CalculateBlockRewardGivenSupply(100, MaxSupply+1000)
	if rPastCap != 0 {
		t.Fatalf("Reward beyond MaxSupply must be 0, got %d", rPastCap)
	}

	// Integer overflow safety with math.MaxInt64
	rOverflow := CalculateBlockRewardGivenSupply(100, math.MaxInt64)
	if rOverflow != 0 {
		t.Fatalf("Reward with MaxInt64 supply must be 0, got %d", rOverflow)
	}

	t.Logf("[OK] Halving boundaries, supply cap clipping, and overflow safety verified.")
}
