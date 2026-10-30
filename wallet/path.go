package wallet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/acxxcxx/armax-coin/core"
)

const WalletFileName = "wallet.json"

var (
	walletPathMu     sync.RWMutex
	customWalletPath string
	noticedLegacyMu  sync.Mutex
	noticedLegacy    = make(map[string]bool)
)

// SetWalletPath explicitly sets a custom wallet file location.
func SetWalletPath(path string) {
	walletPathMu.Lock()
	customWalletPath = path
	walletPathMu.Unlock()
}

// ResetWalletPath clears any custom wallet path override.
func ResetWalletPath() {
	walletPathMu.Lock()
	customWalletPath = ""
	walletPathMu.Unlock()
}

// ResetLegacyNoticesForTesting resets the recorded notices so tests can verify output.
func ResetLegacyNoticesForTesting() {
	noticedLegacyMu.Lock()
	noticedLegacy = make(map[string]bool)
	noticedLegacyMu.Unlock()
}

func notifyLegacyWallet(legacyFile, netName, targetFile string) {
	key := legacyFile + "->" + targetFile
	noticedLegacyMu.Lock()
	defer noticedLegacyMu.Unlock()
	if !noticedLegacy[key] {
		noticedLegacy[key] = true
		fmt.Printf("[!] Notice: Found legacy wallet at %s. Network '%s' now uses isolated path %s. The legacy file was left intact. Please copy or move it manually if you wish to reuse it.\n", legacyFile, netName, targetFile)
	}
}

// GetDefaultWalletPath determines the wallet.json location for the active network profile.
func GetDefaultWalletPath() string {
	return GetDefaultWalletPathForNetwork(core.GetActiveNetwork().Name)
}

// GetDefaultWalletPathForNetwork determines the wallet.json location for a specific network name.
// Network profiles are strictly isolated into respective subfolders:
// mainnet -> <datadir>/mainnet/wallet.json
// testnet -> <datadir>/testnet/wallet.json
// regtest -> <datadir>/regtest/wallet.json
// soak    -> <datadir>/soak/wallet.json
func GetDefaultWalletPathForNetwork(netName string) string {
	walletPathMu.RLock()
	cPath := customWalletPath
	walletPathMu.RUnlock()
	if cPath != "" {
		return cPath
	}

	// Environment variable override for a specific wallet file path
	if envFile := strings.TrimSpace(os.Getenv("ARMX_WALLET_FILE")); envFile != "" {
		return envFile
	}

	subDir := strings.ToLower(strings.TrimSpace(netName))
	if subDir == "" {
		subDir = "mainnet"
	}

	// 1. Check environment variable ARMX_DATA_DIR override
	if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
		targetDir := envDir
		if filepath.Base(envDir) != subDir {
			targetDir = filepath.Join(envDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0700)
		targetFile := filepath.Join(targetDir, WalletFileName)

		// Check for legacy unisolated wallet in root data directory without mutating it
		if targetDir != envDir {
			legacyFile := filepath.Join(envDir, WalletFileName)
			if _, err := os.Stat(legacyFile); err == nil {
				if _, errT := os.Stat(targetFile); os.IsNotExist(errT) {
					notifyLegacyWallet(legacyFile, netName, targetFile)
				}
			}
		}
		return targetFile
	}

	// 2. Check if wallet.json already exists in network subfolder next to executable
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		targetDir := filepath.Join(execDir, subDir)
		_ = os.MkdirAll(targetDir, 0700)
		candidate := filepath.Join(targetDir, WalletFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		legacyCandidate := filepath.Join(execDir, WalletFileName)
		if _, err := os.Stat(legacyCandidate); err == nil {
			notifyLegacyWallet(legacyCandidate, netName, candidate)
		}
	}

	// 3. Check current working directory under subDir
	baseDir := subDir
	_ = os.MkdirAll(baseDir, 0700)
	candidate := filepath.Join(baseDir, WalletFileName)
	if _, err := os.Stat(candidate); err == nil {
		if abs, err := filepath.Abs(candidate); err == nil {
			return abs
		}
		return candidate
	}
	if _, err := os.Stat(WalletFileName); err == nil {
		notifyLegacyWallet("./"+WalletFileName, netName, candidate)
	}
	return candidate
}
