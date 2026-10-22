//go:build testhooks

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTesthooksExcludedFromProductionBuild verifies that SetTargetOverrideForTesting and SetNetworkForTesting
// are completely omitted from production builds compiled without the 'testhooks' build tag [L3-3].
func TestTesthooksExcludedFromProductionBuild(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "armcoind_prod.exe")

	// Compile production binary WITHOUT -tags testhooks
	cmd := exec.Command("go", "build", "-o", binPath, "main.go")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Production compilation failed: %v\nOutput: %s", err, string(out))
	}
	defer func() { _ = os.Remove(binPath) }()

	// Inspect compiled symbols using 'go tool nm'
	nmCmd := exec.Command("go", "tool", "nm", binPath)
	out, err := nmCmd.Output()
	if err != nil {
		t.Fatalf("go tool nm execution failed: %v", err)
	}

	symbols := string(out)
	if strings.Contains(symbols, "core.SetTargetOverrideForTesting") {
		t.Errorf("SECURITY LEAK: Production binary contains forbidden symbol 'core.SetTargetOverrideForTesting'!")
	}
	if strings.Contains(symbols, "core.SetNetworkForTesting") {
		t.Errorf("SECURITY LEAK: Production binary contains forbidden symbol 'core.SetNetworkForTesting'!")
	}
}
