package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestArmsendBalanceResponseJSON verifies BalanceResponse struct JSON marshaling and unmarshaling
func TestArmsendBalanceResponseJSON(t *testing.T) {
	raw := `{"address":"armx1test","balance":6000000000,"pending_balance":6000000000,"nonce":1,"pending_nonce":1,"next_nonce":2}`
	var resp BalanceResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Failed to unmarshal BalanceResponse: %v", err)
	}

	if resp.Address != "armx1test" || resp.Balance != 6000000000 || resp.NextNonce != 2 {
		t.Fatalf("Unexpected deserialized values: %+v", resp)
	}
}

// TestArmsendVersionFlag verifies that armsend CLI responds to -version
func TestArmsendVersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armsend -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armsend v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}
