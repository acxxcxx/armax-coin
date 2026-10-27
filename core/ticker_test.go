package core

import (
	"strings"
	"testing"
)

func TestTickerForNetworkID(t *testing.T) {
	cases := []struct {
		netID string
		want  string
	}{
		{MainnetProfile.NetworkID, "ARMX"},
		{TestnetProfile.NetworkID, "tARMX"},
		{RegtestProfile.NetworkID, "tARMX"},
		{SoakProfile.NetworkID, "tARMX"},
		{"", "tARMX"},
	}
	for _, c := range cases {
		if got := TickerForNetworkID(c.netID); got != c.want {
			t.Errorf("TickerForNetworkID(%q) = %q, want %q", c.netID, got, c.want)
		}
	}
}

// Ticker is display-only: it must not change the consensus network identity.
func TestTickerDoesNotChangeConsensusIdentity(t *testing.T) {
	if TestnetProfile.NetworkID != "ARMX-TESTNET-1" {
		t.Fatalf("testnet network ID changed: %s", TestnetProfile.NetworkID)
	}
	if MainnetProfile.NetworkID != "ARMX-MAINNET-1" {
		t.Fatalf("mainnet network ID changed: %s", MainnetProfile.NetworkID)
	}
	if Bech32HRP != "armx" {
		t.Fatalf("address HRP changed: %s", Bech32HRP)
	}
}

// Mainnet cannot be selected yet (SetNetwork rejects it), so mainnet mapping is
// covered by TestTickerForNetworkID; this test checks the active-profile path.
func TestActiveTickerFollowsSelectedNetwork(t *testing.T) {
	defer func() { _ = SetNetwork("testnet") }()

	if err := SetNetwork("testnet"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveTicker(); got != "tARMX" {
		t.Fatalf("testnet ticker = %q, want tARMX", got)
	}
	if line := NetworkWarningLine(); !strings.Contains(line, "ALPHA TESTNET") {
		t.Fatalf("testnet warning line = %q", line)
	}
}
