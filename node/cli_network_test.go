//go:build testhooks

package node

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

// TestCLI_NetworkFlagSelection tests CLI binary execution for -network flag [L3-8, L4-1]
func TestCLI_NetworkFlagSelection(t *testing.T) {
	// Locate repository root
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("Failed to resolve repo root: %v", err)
	}

	testCases := []struct {
		name         string
		pkgPath      string
		skipOnNonWin bool
	}{
		{
			name:    "armwallet",
			pkgPath: "./cli/armwallet",
		},
		{
			name:    "armsend",
			pkgPath: "./cli/armsend",
		},
		{
			name:    "armminer",
			pkgPath: "./miner",
		},
		{
			name:         "armgui",
			pkgPath:      "./gui",
			skipOnNonWin: true,
		},
	}

	for _, tc := range testCases {
		if tc.skipOnNonWin && runtime.GOOS != "windows" {
			t.Logf("Skipping %s test on non-windows platform", tc.name)
			continue
		}

		// 1. Default must select testnet and print testnet banner [L4-1]
		t.Run(tc.name+"_DefaultTestnet", func(t *testing.T) {
			out, err := runCLITestBinary(t, repoRoot, tc.pkgPath, tc.name, "testhooks", "-info")
			if err != nil {
				t.Fatalf("[%s default -info] execution failed: %v\nOutput: %s", tc.name, err, out)
			}
			if !strings.Contains(out, "ARMAX ALPHA TESTNET - no economic value, unaudited") {
				t.Errorf("[%s default] expected alpha testnet banner in output, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "TESTNET") {
				t.Errorf("[%s default] expected TESTNET in output, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "19333") {
				t.Errorf("[%s default] expected RPC port 19333, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "19335") {
				t.Errorf("[%s default] expected P2P port 19335, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "testnet") {
				t.Errorf("[%s default] expected Data Directory 'testnet', got:\n%s", tc.name, out)
			}
		})

		// 2. Explicit -network testnet
		t.Run(tc.name+"_ExplicitTestnet", func(t *testing.T) {
			out, err := runCLITestBinary(t, repoRoot, tc.pkgPath, tc.name, "testhooks", "-network", "testnet", "-info")
			if err != nil {
				t.Fatalf("[%s -network testnet -info] execution failed: %v\nOutput: %s", tc.name, err, out)
			}
			if !strings.Contains(out, "TESTNET") {
				t.Errorf("[%s testnet] expected TESTNET in output, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "19333") {
				t.Errorf("[%s testnet] expected RPC port 19333, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "19335") {
				t.Errorf("[%s testnet] expected P2P port 19335, got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "testnet") {
				t.Errorf("[%s testnet] expected Data Directory 'testnet', got:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "testnet-seed") {
				t.Errorf("[%s testnet] expected testnet seed in output, got:\n%s", tc.name, out)
			}
		})

		// 3. Mainnet rejected without testhooks tag [L4-1]
		t.Run(tc.name+"_MainnetLockedWithoutTesthooks", func(t *testing.T) {
			// Run WITHOUT -tags testhooks
			out, err := runCLITestBinary(t, repoRoot, tc.pkgPath, tc.name, "", "-network", "mainnet", "-info")
			if err == nil {
				t.Fatalf("[%s -network mainnet without testhooks] expected command to fail, but succeeded with output:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "Mainnet has not been launched; please use -network testnet") {
				t.Errorf("[%s -network mainnet] expected 'Mainnet has not been launched' rejection message, got:\n%s", tc.name, out)
			}
		})
	}
}

func runCLITestBinary(t *testing.T, repoRoot, pkgPath, name, tags string, args ...string) (string, error) {
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	tmpBin := filepath.Join(t.TempDir(), binName)
	buildArgs := []string{"build"}
	if tags != "" {
		buildArgs = append(buildArgs, "-tags", tags)
	}
	buildArgs = append(buildArgs, "-o", tmpBin, pkgPath)
	buildCmd := exec.Command("go", buildArgs...)
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return string(out), err
	}
	runCmd := exec.Command(tmpBin, args...)
	runCmd.Dir = repoRoot
	runCmd.Env = append(os.Environ(), "ARMX_NETWORK=")
	out, err := runCmd.CombinedOutput()
	return string(out), err
}

// TestCLI_DataDirAndSeedIsolation verifies that testnet selects testnet datadir and testnet seeds [L3-8, L4-1]
func TestCLI_DataDirAndSeedIsolation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	// 1. Default config must be testnet [L4-1]
	defCfg, err := core.ResolveCLINetwork("")
	if err != nil {
		t.Fatalf("Failed to resolve default CLI config: %v", err)
	}
	if defCfg.NetworkProfile.Name != "testnet" {
		t.Errorf("Expected default profile 'testnet', got '%s'", defCfg.NetworkProfile.Name)
	}
	if defCfg.DataDir != "testnet" {
		t.Errorf("Expected default DataDir 'testnet', got '%s'", defCfg.DataDir)
	}
	if defCfg.RPCPort != "19333" || defCfg.P2PPort != "19335" {
		t.Errorf("Expected default ports 19333/19335, got %s/%s", defCfg.RPCPort, defCfg.P2PPort)
	}

	// 2. Mainnet checks (under testhooks)
	mainCfg, err := core.ResolveCLINetwork("mainnet")
	if err != nil {
		t.Fatalf("Failed to resolve mainnet CLI config: %v", err)
	}
	if mainCfg.DataDir != "data" {
		t.Errorf("Expected mainnet DataDir 'data', got '%s'", mainCfg.DataDir)
	}
	if mainCfg.RPCPort != "19332" || mainCfg.P2PPort != "19334" {
		t.Errorf("Expected mainnet ports 19332/19334, got %s/%s", mainCfg.RPCPort, mainCfg.P2PPort)
	}

	// 3. Testnet checks
	testCfg, err := core.ResolveCLINetwork("testnet")
	if err != nil {
		t.Fatalf("Failed to resolve testnet CLI config: %v", err)
	}
	if testCfg.DataDir != "testnet" {
		t.Errorf("Expected testnet DataDir 'testnet', got '%s'", testCfg.DataDir)
	}
	if testCfg.RPCPort != "19333" || testCfg.P2PPort != "19335" {
		t.Errorf("Expected testnet ports 19333/19335, got %s/%s", testCfg.RPCPort, testCfg.P2PPort)
	}

	// 4. Verify seeds are mutually exclusive
	for _, mSeed := range mainCfg.DefaultSeeds {
		for _, tSeed := range testCfg.DefaultSeeds {
			if mSeed == tSeed {
				t.Errorf("Mainnet and testnet seeds must not overlap! Found shared seed: %s", mSeed)
			}
		}
	}

	// 5. Verify peers path and DB path isolation
	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("mainnet"); err != nil {
		t.Fatalf("Failed to set mainnet: %v", err)
	}
	mainDBPath := core.GetDBPath()
	mainPeersPath := getPeersPath()

	core.SetNetworkForTesting(true)
	if err := core.SetNetwork("testnet"); err != nil {
		t.Fatalf("Failed to set testnet: %v", err)
	}
	testDBPath := core.GetDBPath()
	testPeersPath := getPeersPath()

	if mainDBPath == testDBPath {
		t.Errorf("Mainnet and testnet DB path must be isolated! Both: %s", mainDBPath)
	}
	if !strings.Contains(testDBPath, "testnet") {
		t.Errorf("Testnet DB path must contain 'testnet' subfolder, got: %s", testDBPath)
	}
	if mainPeersPath == testPeersPath {
		t.Errorf("Mainnet and testnet peers path must be isolated! Both: %s", mainPeersPath)
	}
	if !strings.Contains(testPeersPath, "testnet") {
		t.Errorf("Testnet peers path must contain 'testnet' subfolder, got: %s", testPeersPath)
	}
}
