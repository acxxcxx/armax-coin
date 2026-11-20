package main

import (
	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// FeeEstimateResponse exposes protocol-accurate fee tiers computed from core.MinRelayFee and tx size.
type FeeEstimateResponse struct {
	MinRelayFeePerKB  int64  `json:"min_relay_fee_per_kb"`
	EstimatedTxBytes  int    `json:"estimated_tx_bytes"`
	MinFeeArmoids     int64  `json:"min_fee_armoids"`
	MinFeeFormatted   string `json:"min_fee_formatted"`
	EconomyArmoids    int64  `json:"economy_armoids"`
	EconomyFormatted  string `json:"economy_formatted"`
	StandardArmoids   int64  `json:"standard_armoids"`
	StandardFormatted string `json:"standard_formatted"`
	PriorityArmoids   int64  `json:"priority_armoids"`
	PriorityFormatted string `json:"priority_formatted"`
	MempoolTxCount    int    `json:"mempool_tx_count"`
	Ticker            string `json:"ticker"`
}

func roundUpFeeStep(val, step int64) int64 {
	if step <= 0 {
		return val
	}
	return ((val + step - 1) / step) * step
}

// ComputeGUIFeeEstimate calculates guaranteed-valid Economy/Standard/Priority fees using
// core.MinRelayFee (1,000 Armoids/KB) and wallet.EstimateMinMempoolFee for ML-DSA-65 tx sizes.
func ComputeGUIFeeEstimate(w *wallet.Wallet, recipient string, amount int64, mempoolCount int, netID string) FeeEstimateResponse {
	minFee := wallet.EstimateMinMempoolFee(w, recipient, amount, 0)
	estBytes := 10800

	if w != nil && w.PrivateKey != nil {
		dummy := core.Transaction{
			Sender:    w.Address,
			SenderPub: w.PublicKey,
			Recipient: recipient,
			Amount:    amount,
			Fee:       minFee,
			Nonce:     0,
		}
		if dummy.Sign(w.PrivateKey) == nil {
			estBytes = dummy.Size()
			if exactMin := core.CalculateMinMempoolFee(estBytes); exactMin > minFee {
				minFee = exactMin
			}
		}
	}

	step := core.MinRelayFee
	if step <= 0 {
		step = 1000
	}
	if mempoolCount < 0 {
		mempoolCount = 0
	}
	congestionSteps := int64(mempoolCount / 50)

	economy := minFee
	standard := roundUpFeeStep(minFee*3/2, step) + congestionSteps*step
	if standard <= economy {
		standard = economy + step
	}
	priority := minFee*2 + congestionSteps*2*step
	if priority <= standard {
		priority = standard + step
	}

	return FeeEstimateResponse{
		MinRelayFeePerKB:  step,
		EstimatedTxBytes:  estBytes,
		MinFeeArmoids:     minFee,
		MinFeeFormatted:   core.FormatArmxDecimal(minFee),
		EconomyArmoids:    economy,
		EconomyFormatted:  core.FormatArmxDecimal(economy),
		StandardArmoids:   standard,
		StandardFormatted: core.FormatArmxDecimal(standard),
		PriorityArmoids:   priority,
		PriorityFormatted: core.FormatArmxDecimal(priority),
		MempoolTxCount:    mempoolCount,
		Ticker:            core.TickerForNetworkID(netID),
	}
}
