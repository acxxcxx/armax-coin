package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

// TestArmwalletVersionFlag verifies that armwallet CLI responds to -version
func TestArmwalletVersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armwallet -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armwallet v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}

// TestArmwalletShowAddress verifies that showAddress correctly inspects a keystore file
func TestArmwalletShowAddress(t *testing.T) {
	tempDir := t.TempDir()
	ksPath := filepath.Join(tempDir, "test_wallet.json")

	dummyKS := wallet.KeystoreFile{
		Version:   2,
		Address:   "armx17nqa4uyewe8elsn4w8qfygjv7aafkkzj25r8antzdmecwnhpd28sayl2yl",
		PublicKey: strings.Repeat("a", 3904),
	}

	data, err := json.Marshal(dummyKS)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(ksPath, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify showAddress execution does not panic
	showAddress(ksPath)
}
