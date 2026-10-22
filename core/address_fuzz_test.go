package core

import (
	"testing"
)

// FuzzBech32Decode verifies that Bech32 decoding never panics on arbitrary malformed inputs [Security Audit]
func FuzzBech32Decode(f *testing.F) {
	// Seed with valid and invalid addresses
	f.Add("armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd")
	f.Add("armx1")
	f.Add("armx1qqqq")
	f.Add("")
	f.Add("ARMX1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQPLWCDD")
	f.Add("armx1invalidchecksum1234567890")

	f.Fuzz(func(t *testing.T, input string) {
		// Neither Bech32Decode nor ValidateAddress should ever panic
		_, _, _ = Bech32Decode(input)
		_ = ValidateAddress(input)
	})
}
