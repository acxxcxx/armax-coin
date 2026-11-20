package wallet

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalletManager_LegacyDefaultAutoRegisterAndPreserve(t *testing.T) {
	netDir := filepath.Join(t.TempDir(), "testnet")
	if err := os.MkdirAll(netDir, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	legacyW, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	legacyPath := filepath.Join(netDir, WalletFileName)
	if err := EncryptAndSaveKeystore(legacyW, "legacy-passphrase-1234", legacyPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore failed: %v", err)
	}

	wm := NewWalletManagerForDir(netDir)
	list := wm.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 auto-registered legacy wallet, got %d", len(list))
	}
	if list[0].ID != "default" || list[0].Address != legacyW.Address || list[0].FilePath != WalletFileName {
		t.Fatalf("unexpected auto-registered entry: %+v", list[0])
	}
	active, ok := wm.Active()
	if !ok || active.Address != legacyW.Address {
		t.Fatalf("expected default wallet to be active, got ok=%v active=%+v", ok, active)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected legacy wallet.json to remain intact in-place: %v", err)
	}
}

func TestWalletManager_MultiWalletCreateSelectRenameImportDecrypt(t *testing.T) {
	netDir := filepath.Join(t.TempDir(), "testnet")
	wm := NewWalletManagerForDir(netDir)

	// 1. Create first wallet ("Mining Main")
	e1, w1, mn1, err := wm.Create("Mining Main", "strong-passphrase-one")
	if err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	defer w1.Zeroize()
	if mn1 == "" || len(strings.Fields(mn1)) != 24 {
		t.Fatalf("expected 24-word mnemonic, got %q", mn1)
	}

	// 2. Create second wallet ("Treasury Cold")
	e2, w2, _, err := wm.Create("Treasury Cold", "strong-passphrase-two")
	if err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}
	defer w2.Zeroize()
	if e1.Address == e2.Address {
		t.Fatalf("expected distinct addresses for separate wallets")
	}

	// 3. One-click Select back to wallet 1
	sel, err := wm.Select(e1.ID)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if sel.Address != e1.Address {
		t.Fatalf("expected selected address %s, got %s", e1.Address, sel.Address)
	}
	active, ok := wm.Active()
	if !ok || active.ID != e1.ID {
		t.Fatalf("expected active ID %s, got %+v", e1.ID, active)
	}

	// 4. Rename wallet 2
	renamed, err := wm.Rename(e2.ID, "Long Term Vault")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if renamed.Label != "Long Term Vault" {
		t.Fatalf("expected renamed label, got %q", renamed.Label)
	}

	// 5. Reject path-traversal IDs
	if _, err := wm.Select("../escape"); err == nil {
		t.Fatalf("expected error on path-traversal ID")
	}
	if _, err := wm.ResolvePath("../escape"); err == nil {
		t.Fatalf("expected error on ResolvePath traversal")
	}

	// 6. Import external keystore JSON
	extDir := t.TempDir()
	extW, err := NewWallet()
	if err != nil {
		t.Fatalf("NewWallet ext failed: %v", err)
	}
	extPath := filepath.Join(extDir, "ext.json")
	if err := EncryptAndSaveKeystore(extW, "external-passphrase-123", extPath); err != nil {
		t.Fatalf("EncryptAndSaveKeystore ext failed: %v", err)
	}
	rawExt, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatalf("ReadFile ext failed: %v", err)
	}
	imported, err := wm.ImportKeystoreJSON("Imported Miner", rawExt)
	if err != nil {
		t.Fatalf("ImportKeystoreJSON failed: %v", err)
	}
	if imported.Address != extW.Address {
		t.Fatalf("expected imported address %s, got %s", extW.Address, imported.Address)
	}

	// 7. Reject oversized or malformed import JSON
	if _, err := wm.ImportKeystoreJSON("TooBig", bytes.Repeat([]byte("a"), MaxKeystoreFileSize+1)); err == nil {
		t.Fatalf("expected oversized import to fail")
	}
	badKS, _ := json.Marshal(KeystoreFile{Version: 1, Address: "invalid_addr"})
	if _, err := wm.ImportKeystoreJSON("BadAddr", badKS); err == nil {
		t.Fatalf("expected invalid address import to fail")
	}

	// 8. Decrypt imported wallet
	decW, err := wm.DecryptWallet(imported.ID, "external-passphrase-123")
	if err != nil {
		t.Fatalf("DecryptWallet failed: %v", err)
	}
	defer decW.Zeroize()
	if decW.Address != extW.Address {
		t.Fatalf("decrypted address mismatch: %s vs %s", decW.Address, extW.Address)
	}

	// 9. Restore wallet from mnemonic
	eRestored, wRestored, err := wm.Restore("Restored Test", mn1, "restore-passphrase-123")
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	defer wRestored.Zeroize()
	if eRestored.Address != e1.Address {
		t.Fatalf("restored address %s != original %s", eRestored.Address, e1.Address)
	}
}
