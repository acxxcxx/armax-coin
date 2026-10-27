package core

// Display tickers. These are presentation-only labels and are never hashed or
// signed, so changing them does not affect consensus, addresses, or genesis.
const (
	MainnetTicker = "ARMX"
	TestnetTicker = "tARMX"
)

// TickerForNetworkID returns the display ticker for a network ID.
// Only the mainnet network uses ARMX; every other network uses tARMX so that
// test coins are never mistaken for mainnet coins.
func TickerForNetworkID(netID string) string {
	if netID == MainnetProfile.NetworkID {
		return MainnetTicker
	}
	return TestnetTicker
}

// ActiveTicker returns the display ticker for the currently selected network profile.
func ActiveTicker() string {
	return TickerForNetworkID(GetActiveNetwork().NetworkID)
}

// NetworkWarningLine returns the banner warning that matches the active network.
func NetworkWarningLine() string {
	if ActiveTicker() == MainnetTicker {
		return "WARNING  ARMAXCOIN MAINNET - transactions use real economic value. Verify addresses and amounts."
	}
	return "WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited"
}
