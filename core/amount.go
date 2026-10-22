package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseArmxDecimal parses an ARMX decimal string into Armoid base units (1 ARMX = 100,000,000 Armoids)
// without using float64 to completely eliminate IEEE 754 floating-point inaccuracies [L3-1].
// Maximum supported decimal places is 8. Rejects negative values, >8 decimals, signs, empty, or invalid formatting.
func ParseArmxDecimal(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("amount cannot be empty")
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, errors.New("signs (+/-) not permitted in amount string")
	}

	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, errors.New("multiple decimal separators in amount")
	}

	intPartStr := parts[0]
	if intPartStr == "" {
		intPartStr = "0"
	}
	for _, ch := range intPartStr {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid character '%c' in amount", ch)
		}
	}
	intVal, err := strconv.ParseInt(intPartStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("integer part exceeds 64-bit integer range: %w", err)
	}

	// Maximum integer part that can be scaled by CoinUnit without overflow
	const maxIntPart = (1<<63 - 1) / 100_000_000
	if intVal > maxIntPart {
		return 0, errors.New("amount exceeds maximum supported integer range")
	}
	armoids := intVal * CoinUnit

	if len(parts) == 2 {
		fracPartStr := parts[1]
		if len(fracPartStr) > 8 {
			return 0, fmt.Errorf("exceeds maximum 8 decimal places: got %d digits", len(fracPartStr))
		}
		for _, ch := range fracPartStr {
			if ch < '0' || ch > '9' {
				return 0, fmt.Errorf("invalid character '%c' in fractional amount", ch)
			}
		}
		// Pad to exactly 8 decimal digits
		padded := fracPartStr + strings.Repeat("0", 8-len(fracPartStr))
		fracVal, err := strconv.ParseInt(padded, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse fractional digits: %w", err)
		}
		if (1<<63-1)-armoids < fracVal {
			return 0, errors.New("arithmetic overflow when combining integer and fractional amounts")
		}
		armoids += fracVal
	}

	if armoids > MaxSupply {
		return 0, fmt.Errorf("amount %d Armoids exceeds maximum supply cap (%d Armoids)", armoids, MaxSupply)
	}

	return armoids, nil
}

// FormatArmxDecimal formats an Armoid integer into an exact 8-decimal ARMX string without float64 [L3-1].
func FormatArmxDecimal(armoids int64) string {
	sign := ""
	if armoids < 0 {
		sign = "-"
		armoids = -armoids
	}
	intPart := armoids / CoinUnit
	fracPart := armoids % CoinUnit
	return fmt.Sprintf("%s%d.%08d", sign, intPart, fracPart)
}
