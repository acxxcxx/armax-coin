package core

import "testing"

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
