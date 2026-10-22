package core

import (
	"testing"
)

// TestC32_TotalEmissionCalculation proves the total emission of Armaxcoin from block 0
// until block reward reaches 0 [C-32].
// Due to integer bit-shift truncation (similar to Bitcoin's 20,999,999.9769 BTC),
// the exact total minted supply is 12,599,999,987,400,000 Armoids (125,999,999.874 ARMX),
// which strictly never exceeds MaxSupply (126,000,000 ARMX).
func TestC32_TotalEmissionCalculation(t *testing.T) {
	var totalArmoids int64 = 0
	var currentSupply int64 = 0

	var epochSummaries []struct {
		epoch  int
		reward int64
		blocks int64
		total  int64
	}

	for epoch := 0; epoch < 64; epoch++ {
		rewardPerBlock := InitialReward >> epoch
		if rewardPerBlock == 0 {
			break
		}

		epochTotal := int64(0)
		for b := 0; b < HalvingInterval; b++ {
			blockIdx := epoch*HalvingInterval + b
			r := CalculateBlockRewardGivenSupply(blockIdx, currentSupply)
			if r <= 0 {
				break
			}
			epochTotal += r
			currentSupply += r
			totalArmoids += r
		}

		epochSummaries = append(epochSummaries, struct {
			epoch  int
			reward int64
			blocks int64
			total  int64
		}{
			epoch:  epoch,
			reward: rewardPerBlock,
			blocks: int64(HalvingInterval),
			total:  epochTotal,
		})
	}

	totalARMX := float64(totalArmoids) / float64(CoinUnit)
	t.Logf("Active Halving Epochs: %d", len(epochSummaries))
	t.Logf("Total Calculated Emission: %d Armoids (%.8f ARMX)", totalArmoids, totalARMX)

	// Exact satoshi/armoid integer calculation: 125,999,999.87400000 ARMX
	expectedTotalArmoids := int64(12_599_999_987_400_000)
	if totalArmoids != expectedTotalArmoids {
		t.Fatalf("[C-32] Total emission mismatch: expected %d Armoids, got %d Armoids",
			expectedTotalArmoids, totalArmoids)
	}

	if totalArmoids > MaxSupply {
		t.Fatalf("[C-32] Total emission %d exceeds MaxSupply %d", totalArmoids, MaxSupply)
	}
}
