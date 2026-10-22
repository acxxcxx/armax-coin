package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestDaemonVersionFlag verifies that armcoind returns correct release version
func TestDaemonVersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armcoind -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armcoind v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}
