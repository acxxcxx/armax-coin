package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
)

const (
	walletIndexFileName = "wallets_index.json"
	walletsSubDirName   = "wallets"
)

var (
	validWalletIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	// Serialize Argon2id KDF decryption calls to bound concurrent memory usage on 1-2 GB SBCs [F-07].
	multiWalletKDFMu sync.Mutex
)

// ManagedWalletEntry stores non-sensitive metadata for a wallet managed in the UI.
type ManagedWalletEntry struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Address   string `json:"address"`
	FilePath  string `json:"file_path"` // relative to netDir (either "wallet.json" or "wallets/<id>.json")
	CreatedAt string `json:"created_at"`
}

// WalletIndex persists the list of managed wallets and active selection within a network directory.
type WalletIndex struct {
	Version  int                  `json:"version"`
	ActiveID string               `json:"active_id"`
	Wallets  []ManagedWalletEntry `json:"wallets"`
}

// WalletManager manages multiple encrypted keystores inside a single network directory
// while preserving backward compatibility with the legacy root wallet.json file.
type WalletManager struct {
	mu        sync.Mutex
	netDir    string
	indexPath string
	index     WalletIndex
}

// NewWalletManager creates a manager rooted in the active network's wallet directory.
func NewWalletManager() *WalletManager {
	defaultPath := GetDefaultWalletPath()
	return NewWalletManagerForDir(filepath.Dir(defaultPath))
}

// NewWalletManagerForDir creates a manager for the specified network data directory.
func NewWalletManagerForDir(netDir string) *WalletManager {
	if strings.TrimSpace(netDir) == "" {
		netDir = "."
	}
	_ = os.MkdirAll(netDir, 0700)
	wm := &WalletManager{
		netDir:    netDir,
		indexPath: filepath.Join(netDir, walletIndexFileName),
		index: WalletIndex{
			Version: 1,
			Wallets: []ManagedWalletEntry{},
		},
	}
	wm.mu.Lock()
	defer wm.mu.Unlock()
	_ = wm.loadLocked()
	wm.syncLegacyDefaultLocked()
	return wm
}

func (wm *WalletManager) loadLocked() error {
	data, err := os.ReadFile(wm.indexPath)
	if err != nil {
		return err
	}
	var idx WalletIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	if idx.Version <= 0 {
		idx.Version = 1
	}
	if idx.Wallets == nil {
		idx.Wallets = []ManagedWalletEntry{}
	}
	// Filter invalid entries (e.g. path-traversal or missing keystore file).
	valid := make([]ManagedWalletEntry, 0, len(idx.Wallets))
	seen := make(map[string]bool, len(idx.Wallets))
	for _, entry := range idx.Wallets {
		if !validWalletIDRegex.MatchString(entry.ID) || seen[entry.ID] {
			continue
		}
		absPath, err := wm.resolveSafePathLocked(entry.FilePath)
		if err != nil {
			continue
		}
		if _, statErr := os.Stat(absPath); statErr != nil {
			continue
		}
		seen[entry.ID] = true
		valid = append(valid, entry)
	}
	idx.Wallets = valid
	wm.index = idx
	return nil
}

func (wm *WalletManager) syncLegacyDefaultLocked() {
	legacyPath := filepath.Join(wm.netDir, WalletFileName)
	info, err := os.Stat(legacyPath)
	if err != nil || info.IsDir() {
		wm.ensureValidActiveLocked()
		return
	}

	ksData, err := os.ReadFile(ioLimitPath(legacyPath))
	if err != nil {
		wm.ensureValidActiveLocked()
		return
	}
	var ks KeystoreFile
	if json.Unmarshal(ksData, &ks) != nil || !core.ValidateAddress(ks.Address) {
		wm.ensureValidActiveLocked()
		return
	}

	alreadyTracked := false
	for i := range wm.index.Wallets {
		if filepath.Clean(wm.index.Wallets[i].FilePath) == WalletFileName {
			if wm.index.Wallets[i].Address != ks.Address {
				wm.index.Wallets[i].Address = ks.Address
				_ = wm.saveLocked()
			}
			alreadyTracked = true
			break
		}
	}
	if !alreadyTracked {
		entry := ManagedWalletEntry{
			ID:        "default",
			Label:     "Default Wallet",
			Address:   ks.Address,
			FilePath:  WalletFileName,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		for _, e := range wm.index.Wallets {
			if e.ID == entry.ID {
				entry.ID = "default-legacy"
				break
			}
		}
		wm.index.Wallets = append([]ManagedWalletEntry{entry}, wm.index.Wallets...)
		if wm.index.ActiveID == "" {
			wm.index.ActiveID = entry.ID
		}
		_ = wm.saveLocked()
	}
	wm.ensureValidActiveLocked()
}

func ioLimitPath(path string) string {
	return path
}

func (wm *WalletManager) ensureValidActiveLocked() {
	if len(wm.index.Wallets) == 0 {
		wm.index.ActiveID = ""
		return
	}
	for _, w := range wm.index.Wallets {
		if w.ID == wm.index.ActiveID {
			return
		}
	}
	wm.index.ActiveID = wm.index.Wallets[0].ID
	_ = wm.saveLocked()
}

func (wm *WalletManager) saveLocked() error {
	if err := os.MkdirAll(wm.netDir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(wm.index, "", "  ")
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(wm.netDir, "wallets_index.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, wm.indexPath); err != nil {
		return err
	}
	tmpPath = ""
	_ = os.Chmod(wm.indexPath, 0600)
	return nil
}

func (wm *WalletManager) resolveSafePathLocked(relPath string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(relPath))
	if cleaned == "" || cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", errors.New("invalid relative keystore path")
	}
	absBase, err := filepath.Abs(wm.netDir)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(filepath.Join(absBase, cleaned))
	if err != nil {
		return "", err
	}
	if absTarget != absBase && !strings.HasPrefix(absTarget, absBase+string(os.PathSeparator)) {
		return "", errors.New("keystore path escapes network directory")
	}
	return absTarget, nil
}

func (wm *WalletManager) nextWalletIDLocked() (string, error) {
	var b [4]byte
	for i := 0; i < 16; i++ {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		candidate := "w_" + hex.EncodeToString(b[:])
		exists := false
		for _, w := range wm.index.Wallets {
			if w.ID == candidate {
				exists = true
				break
			}
		}
		if !exists {
			return candidate, nil
		}
	}
	return fmt.Sprintf("w_%d", time.Now().UnixNano()), nil
}

func normalizeWalletLabel(label string, fallback string) string {
	clean := strings.TrimSpace(label)
	if clean == "" {
		return fallback
	}
	if len(clean) > 48 {
		clean = clean[:48]
	}
	return clean
}

func (wm *WalletManager) targetRelPathForNewWalletLocked(id string) (relPath string, absPath string, err error) {
	defaultAbs := filepath.Join(wm.netDir, WalletFileName)
	if len(wm.index.Wallets) == 0 {
		if _, statErr := os.Stat(defaultAbs); os.IsNotExist(statErr) {
			return WalletFileName, defaultAbs, nil
		}
	}
	subDir := filepath.Join(wm.netDir, walletsSubDirName)
	if err := os.MkdirAll(subDir, 0700); err != nil {
		return "", "", err
	}
	rel := filepath.Join(walletsSubDirName, id+".json")
	abs, err := wm.resolveSafePathLocked(rel)
	return rel, abs, err
}

// List returns a copy of all managed wallet entries.
func (wm *WalletManager) List() []ManagedWalletEntry {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.syncLegacyDefaultLocked()
	out := make([]ManagedWalletEntry, len(wm.index.Wallets))
	copy(out, wm.index.Wallets)
	return out
}

// Active returns the currently selected wallet entry, if any exists.
func (wm *WalletManager) Active() (ManagedWalletEntry, bool) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.syncLegacyDefaultLocked()
	for _, w := range wm.index.Wallets {
		if w.ID == wm.index.ActiveID {
			return w, true
		}
	}
	return ManagedWalletEntry{}, false
}

// ActiveKeystorePath returns the filesystem path of the active keystore, or the default path if none exist.
func (wm *WalletManager) ActiveKeystorePath() string {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.syncLegacyDefaultLocked()
	for _, w := range wm.index.Wallets {
		if w.ID == wm.index.ActiveID {
			if abs, err := wm.resolveSafePathLocked(w.FilePath); err == nil {
				return abs
			}
		}
	}
	return filepath.Join(wm.netDir, WalletFileName)
}

// ResolvePath safely resolves the filesystem path for a managed wallet ID.
func (wm *WalletManager) ResolvePath(id string) (string, error) {
	if !validWalletIDRegex.MatchString(id) {
		return "", errors.New("invalid wallet id")
	}
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for _, w := range wm.index.Wallets {
		if w.ID == id {
			return wm.resolveSafePathLocked(w.FilePath)
		}
	}
	return "", fmt.Errorf("wallet %q not found", id)
}

// Select switches the active wallet in O(1) without decrypting the keystore.
func (wm *WalletManager) Select(id string) (ManagedWalletEntry, error) {
	if !validWalletIDRegex.MatchString(id) {
		return ManagedWalletEntry{}, errors.New("invalid wallet id")
	}
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for _, w := range wm.index.Wallets {
		if w.ID == id {
			wm.index.ActiveID = id
			if err := wm.saveLocked(); err != nil {
				return ManagedWalletEntry{}, err
			}
			return w, nil
		}
	}
	return ManagedWalletEntry{}, fmt.Errorf("wallet %q not found", id)
}

// Create generates a new encrypted wallet keystore, registers it, and selects it as active.
func (wm *WalletManager) Create(label, passphrase string) (ManagedWalletEntry, *Wallet, string, error) {
	if len(passphrase) < 12 {
		return ManagedWalletEntry{}, nil, "", errors.New("passphrase must be at least 12 characters")
	}
	if IsLegacyPassphrase(passphrase) {
		return ManagedWalletEntry{}, nil, "", errors.New("cannot use insecure legacy default passphrase")
	}

	w, err := NewWallet()
	if err != nil {
		return ManagedWalletEntry{}, nil, "", err
	}
	mnemonic := w.Mnemonic

	wm.mu.Lock()
	defer wm.mu.Unlock()

	id, err := wm.nextWalletIDLocked()
	if err != nil {
		w.Zeroize()
		return ManagedWalletEntry{}, nil, "", err
	}
	relPath, absPath, err := wm.targetRelPathForNewWalletLocked(id)
	if err != nil {
		w.Zeroize()
		return ManagedWalletEntry{}, nil, "", err
	}
	if err := EncryptAndSaveKeystore(w, passphrase, absPath); err != nil {
		w.Zeroize()
		return ManagedWalletEntry{}, nil, "", err
	}

	fallbackLabel := fmt.Sprintf("Wallet %d", len(wm.index.Wallets)+1)
	if relPath == WalletFileName {
		id = "default"
		fallbackLabel = "Default Wallet"
	}
	entry := ManagedWalletEntry{
		ID:        id,
		Label:     normalizeWalletLabel(label, fallbackLabel),
		Address:   w.Address,
		FilePath:  relPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	wm.index.Wallets = append(wm.index.Wallets, entry)
	wm.index.ActiveID = entry.ID
	if err := wm.saveLocked(); err != nil {
		return ManagedWalletEntry{}, nil, "", err
	}
	w.ClearMnemonic()
	return entry, w, mnemonic, nil
}

// Restore recovers a wallet from a 24-word mnemonic, encrypts it, registers it, and selects it as active.
func (wm *WalletManager) Restore(label, mnemonic, passphrase string) (ManagedWalletEntry, *Wallet, error) {
	if strings.TrimSpace(mnemonic) == "" {
		return ManagedWalletEntry{}, nil, errors.New("mnemonic cannot be empty")
	}
	if len(passphrase) < 12 {
		return ManagedWalletEntry{}, nil, errors.New("passphrase must be at least 12 characters")
	}
	if IsLegacyPassphrase(passphrase) {
		return ManagedWalletEntry{}, nil, errors.New("cannot use insecure legacy default passphrase")
	}

	restored, err := WalletFromMnemonic(strings.TrimSpace(mnemonic), "", 0)
	if err != nil {
		return ManagedWalletEntry{}, nil, err
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	id, err := wm.nextWalletIDLocked()
	if err != nil {
		restored.Zeroize()
		return ManagedWalletEntry{}, nil, err
	}
	relPath, absPath, err := wm.targetRelPathForNewWalletLocked(id)
	if err != nil {
		restored.Zeroize()
		return ManagedWalletEntry{}, nil, err
	}
	if err := EncryptAndSaveKeystore(restored, passphrase, absPath); err != nil {
		restored.Zeroize()
		return ManagedWalletEntry{}, nil, err
	}

	fallbackLabel := fmt.Sprintf("Restored Wallet %d", len(wm.index.Wallets)+1)
	if relPath == WalletFileName {
		id = "default"
	}
	entry := ManagedWalletEntry{
		ID:        id,
		Label:     normalizeWalletLabel(label, fallbackLabel),
		Address:   restored.Address,
		FilePath:  relPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	wm.index.Wallets = append(wm.index.Wallets, entry)
	wm.index.ActiveID = entry.ID
	if err := wm.saveLocked(); err != nil {
		return ManagedWalletEntry{}, nil, err
	}
	restored.ClearMnemonic()
	return entry, restored, nil
}

// ImportKeystoreJSON validates and stores an external encrypted keystore JSON under the wallets directory.
func (wm *WalletManager) ImportKeystoreJSON(label string, rawJSON []byte) (ManagedWalletEntry, error) {
	if len(rawJSON) == 0 || len(rawJSON) > MaxKeystoreFileSize {
		return ManagedWalletEntry{}, fmt.Errorf("keystore file size must be between 1 byte and %d bytes", MaxKeystoreFileSize)
	}
	var ks KeystoreFile
	if err := json.Unmarshal(rawJSON, &ks); err != nil {
		return ManagedWalletEntry{}, fmt.Errorf("invalid keystore JSON: %w", err)
	}
	if ks.Version <= 0 {
		return ManagedWalletEntry{}, errors.New("unsupported keystore version")
	}
	if !core.ValidateAddress(ks.Address) {
		return ManagedWalletEntry{}, errors.New("keystore contains invalid Armaxcoin address")
	}
	if strings.TrimSpace(ks.Crypto.Ciphertext) == "" || strings.TrimSpace(ks.Crypto.Nonce) == "" || strings.TrimSpace(ks.Crypto.KDFParams.Salt) == "" {
		return ManagedWalletEntry{}, errors.New("keystore missing cryptographic fields")
	}
	if err := ValidateKDFParameters(ks.Crypto.KDFParams); err != nil {
		return ManagedWalletEntry{}, fmt.Errorf("invalid keystore KDF parameters: %w", err)
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	for _, existing := range wm.index.Wallets {
		if core.MatchAddress(existing.Address, ks.Address) {
			return existing, fmt.Errorf("wallet for address %s is already managed (%s)", ks.Address, existing.Label)
		}
	}

	id, err := wm.nextWalletIDLocked()
	if err != nil {
		return ManagedWalletEntry{}, err
	}
	relPath, absPath, err := wm.targetRelPathForNewWalletLocked(id)
	if err != nil {
		return ManagedWalletEntry{}, err
	}
	if err := os.WriteFile(absPath, rawJSON, 0600); err != nil {
		return ManagedWalletEntry{}, err
	}
	_ = os.Chmod(absPath, 0600)

	entry := ManagedWalletEntry{
		ID:        id,
		Label:     normalizeWalletLabel(label, fmt.Sprintf("Imported Wallet %d", len(wm.index.Wallets)+1)),
		Address:   ks.Address,
		FilePath:  relPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	wm.index.Wallets = append(wm.index.Wallets, entry)
	wm.index.ActiveID = entry.ID
	if err := wm.saveLocked(); err != nil {
		return ManagedWalletEntry{}, err
	}
	return entry, nil
}

// Rename updates the display label of a managed wallet.
func (wm *WalletManager) Rename(id, newLabel string) (ManagedWalletEntry, error) {
	if !validWalletIDRegex.MatchString(id) {
		return ManagedWalletEntry{}, errors.New("invalid wallet id")
	}
	cleanLabel := strings.TrimSpace(newLabel)
	if cleanLabel == "" {
		return ManagedWalletEntry{}, errors.New("wallet label cannot be empty")
	}
	if len(cleanLabel) > 48 {
		cleanLabel = cleanLabel[:48]
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()
	for i := range wm.index.Wallets {
		if wm.index.Wallets[i].ID == id {
			wm.index.Wallets[i].Label = cleanLabel
			if err := wm.saveLocked(); err != nil {
				return ManagedWalletEntry{}, err
			}
			return wm.index.Wallets[i], nil
		}
	}
	return ManagedWalletEntry{}, fmt.Errorf("wallet %q not found", id)
}

// DecryptWallet safely decrypts a managed wallet while serializing Argon2id memory usage [F-07].
func (wm *WalletManager) DecryptWallet(id, passphrase string) (*Wallet, error) {
	absPath, err := wm.ResolvePath(id)
	if err != nil {
		return nil, err
	}
	multiWalletKDFMu.Lock()
	defer multiWalletKDFMu.Unlock()
	return DecryptAndLoadKeystore(passphrase, absPath)
}
