package core

import (
	"testing"
)

func TestParseArmxDecimal_Vectors(t *testing.T) {
	// 1. Exact 0.29 ARMX test
	a029, err := ParseArmxDecimal("0.29")
	if err != nil {
		t.Fatalf("ParseArmxDecimal('0.29') failed: %v", err)
	}
	if a029 != 29_000_000 {
		t.Fatalf("Expected 29,000,000 Armoids, got %d", a029)
	}

	// 2. Exact 0.1 + 0.2 == 0.3 addition test (IEEE 754 float failure test)
	a01, err := ParseArmxDecimal("0.1")
	if err != nil {
		t.Fatalf("Parse 0.1 failed: %v", err)
	}
	a02, err := ParseArmxDecimal("0.2")
	if err != nil {
		t.Fatalf("Parse 0.2 failed: %v", err)
	}
	a03, err := ParseArmxDecimal("0.3")
	if err != nil {
		t.Fatalf("Parse 0.3 failed: %v", err)
	}
	if a01+a02 != a03 {
		t.Fatalf("0.1 + 0.2 != 0.3: %d + %d != %d", a01, a02, a03)
	}

	// 3. Exactly 8 decimal places accepted
	a8, err := ParseArmxDecimal("0.12345678")
	if err != nil {
		t.Fatalf("Parse 8 decimals failed: %v", err)
	}
	if a8 != 12_345_678 {
		t.Fatalf("Expected 12345678, got %d", a8)
	}

	// 4. 9 decimal places rejected
	_, err = ParseArmxDecimal("0.123456789")
	if err == nil {
		t.Fatalf("Expected error for 9 decimal places, got nil")
	}

	// 5. Large value test: MaxSupply (126,000,000 ARMX)
	maxVal, err := ParseArmxDecimal("126000000")
	if err != nil {
		t.Fatalf("Parse MaxSupply failed: %v", err)
	}
	if maxVal != MaxSupply {
		t.Fatalf("Expected MaxSupply %d, got %d", MaxSupply, maxVal)
	}

	// 6. Exceeding MaxSupply rejected
	_, err = ParseArmxDecimal("126000000.00000001")
	if err == nil {
		t.Fatalf("Expected error for amount exceeding MaxSupply, got nil")
	}

	// 7. Negative values rejected
	_, err = ParseArmxDecimal("-10.5")
	if err == nil {
		t.Fatalf("Expected error for negative amount, got nil")
	}

	// 8. Formatting test
	formatted := FormatArmxDecimal(29_000_000)
	if formatted != "0.29000000" {
		t.Fatalf("FormatArmxDecimal failed: expected '0.29000000', got '%s'", formatted)
	}
}
