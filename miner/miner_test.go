package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

var (
	builtBinary string
	buildOnce   sync.Once
	buildErr    error
)

func getMinerBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		binName := "armminer_test.exe"
		if runtime.GOOS != "windows" {
			binName = "armminer_test"
		}
		builtBinary = filepath.Join(os.TempDir(), binName)
		cmd := exec.Command("go", "build", "-o", builtBinary, "miner.go")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("failed to build armminer binary: %v, output: %s", err, string(out))
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBinary
}

// TestMinerVersionFlag verifies that armminer CLI responds to -version
func TestMinerVersionFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -version failed: %v, output: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	expected := "armminer v" + core.Version
	if !strings.Contains(outStr, expected) {
		t.Fatalf("Expected output to contain '%s', got: '%s'", expected, outStr)
	}
}

// TestMinerResponseJSON verifies MineResponse parsing
func TestMinerResponseJSON(t *testing.T) {
	raw := `{"status":"success","message":"Block #1 mined successfully"}`
	var resp MineResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if resp.Status != "success" || !strings.Contains(resp.Message, "Block #1") {
		t.Fatalf("Unexpected MineResponse values: %+v", resp)
	}
}

// TestMinerThreadsFlag verifies that armminer CLI accepts the -threads flag
func TestMinerThreadsFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-miner", "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd", "-threads", "4", "-info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -threads flag rejected: %v, output: %s", err, string(out))
	}
	outStr := string(out)
	if strings.Contains(outStr, "flag provided but not defined: -threads") {
		t.Fatalf("armminer rejected -threads flag: %s", outStr)
	}
}

// TestMinerBenchmarkFlag verifies that armminer -benchmark runs and logs hash rate
func TestMinerBenchmarkFlag(t *testing.T) {
	bin := getMinerBinary(t)
	cmd := exec.Command(bin, "-benchmark")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("armminer -benchmark failed: %v, output: %s", err, string(out))
	}
	outStr := string(out)
	if !strings.Contains(outStr, "Aggregate Hashrate") || !strings.Contains(outStr, "hash/s") {
		t.Fatalf("Expected benchmark output containing 'Aggregate Hashrate', got: %s", outStr)
	}
}
