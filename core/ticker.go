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
