package wallet

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// TestBIP39_OfficialTestVectors verifies MnemonicToSeed against standard SatoshiLabs BIP-39 test vectors [Phase 4].
func TestBIP39_OfficialTestVectors(t *testing.T) {
	testVectors := []struct {
		name       string
		mnemonic   string
		passphrase string
		expected   string
	}{
		{
			name:       "12 words empty passphrase",
			mnemonic:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			passphrase: "",
			expected:   "5eb00bbddcf069084889a8ab9155568165f5c453ccb85e70811aaed6f6da5fc19a5ac40b389cd370d086206dec8aa6c43daea6690f20ad3d8d48b2d2ce9e38e4",
		},
		{
			name:       "12 words TREZOR passphrase",
			mnemonic:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			passphrase: "TREZOR",
			expected:   "c55257c360c07c72029aebc1b53c05ed0362ada38ead3e3e9efa3708e53495531f09a6987599d18264c1e1c92f2cf141630c7a3c4ab7c81b2f001698e7463b04",
		},
		{
			name:       "24 words TREZOR passphrase",
			mnemonic:   "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title",
			passphrase: "TREZOR",
			expected:   "bc09fca1804f7e69da93c2f2028eb238c227f2e9dda30cd63699232578480a4021b146ad717fbb7e451ce9eb835f43620bf5c514db0f8add49f5d121449d3e87",
		},
		{
			name:       "24 words with unicode Japanese characters",
			mnemonic:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art",
			passphrase: "TREZOR\u4e9c\u7f8e\u308c\u3057",
			expected:   "55570f3f542927288b0f84070917698b517a64cbd6ccb38c7af724e14cb9673f81c1f4755dc772856492c13faeec1baf31d4c9213ad32d0ea3cd7548b696bbc3",
		},
	}

	for _, tv := range testVectors {
		t.Run(tv.name, func(t *testing.T) {
			seed := MnemonicToSeed(tv.mnemonic, tv.passphrase)
			seedHex := hex.EncodeToString(seed)
			if seedHex != tv.expected {
				t.Fatalf("Vector %q failed: expected %s, got %s", tv.name, tv.expected, seedHex)
			}
		})
	}
}

// TestBIP39_PassphraseCaseAndWhitespaceSensitivity proves that passphrase casing and whitespace
// are strictly preserved and NOT mangled during seed derivation [Follow-up Point 1].
func TestBIP39_PassphraseCaseAndWhitespaceSensitivity(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	// 1. Passphrase casing sensitivity
	seedUpper := MnemonicToSeed(mnemonic, "TREZOR")
	seedLower := MnemonicToSeed(mnemonic, "trezor")
	seedMixed := MnemonicToSeed(mnemonic, "Trezor")

	if bytes.Equal(seedUpper, seedLower) {
		t.Fatalf("MnemonicToSeed must NOT fold passphrase case! Upper and lower seeds matched.")
	}
	if bytes.Equal(seedUpper, seedMixed) || bytes.Equal(seedLower, seedMixed) {
		t.Fatalf("MnemonicToSeed must be strictly case sensitive for passphrases.")
	}

	// 2. Passphrase whitespace sensitivity
	seedClean := MnemonicToSeed(mnemonic, "my passphrase")
	seedLeadingTrailing := MnemonicToSeed(mnemonic, "  my passphrase  ")
	seedMultiSpace := MnemonicToSeed(mnemonic, "my   passphrase")

	if bytes.Equal(seedClean, seedLeadingTrailing) {
		t.Fatalf("MnemonicToSeed must NOT trim passphrase whitespace! Seeds matched unexpectedly.")
	}
	if bytes.Equal(seedClean, seedMultiSpace) {
		t.Fatalf("MnemonicToSeed must NOT compact passphrase inner whitespace! Seeds matched unexpectedly.")
	}
}

// TestBIP39_PassphraseUnicodeNFKDNormalization proves that Unicode passphrases
// normalized with NFC vs NFD produce identical seeds per BIP-39 specification [Follow-up Point 1].
func TestBIP39_PassphraseUnicodeNFKDNormalization(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	// 'é' in NFC (precomposed: \u00e9) vs NFD (decomposed: e + \u0301)
	passNFC := "caf\u00e9"
	passNFD := "cafe\u0301"

	seedNFC := MnemonicToSeed(mnemonic, passNFC)
	seedNFD := MnemonicToSeed(mnemonic, passNFD)

	if !bytes.Equal(seedNFC, seedNFD) {
		t.Fatalf("BIP-39 NFKD normalization failed: NFC and NFD passphrases must produce identical seeds!")
	}
}

// TestBIP39_ConvenienceMnemonicInputSanitization proves that user input convenience normalization
// properly sanitizes user input (whitespace, tabs, casing) for WalletFromMnemonic [Follow-up Point 1].
func TestBIP39_ConvenienceMnemonicInputSanitization(t *testing.T) {
	canonicalMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	passphrase := "MySecretPassphrase"

	rawInputs := []string{
		canonicalMnemonic,
		"  abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about  ",
		"abandon   abandon   abandon  abandon abandon abandon abandon abandon abandon abandon abandon about",
		"ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABANDON ABOUT",
		"Abandon Abandon Abandon Abandon Abandon Abandon Abandon Abandon Abandon Abandon Abandon About",
		"abandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabandon\tabout",
		"abandon\nabandon\rabandon\tabandon abandon abandon abandon abandon abandon abandon abandon about",
	}

	expectedWallet, err := WalletFromMnemonic(canonicalMnemonic, passphrase, 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic failed on canonical input: %v", err)
	}

	for idx, raw := range rawInputs {
		cleaned := NormalizeMnemonicInput(raw)
		if cleaned != canonicalMnemonic {
			t.Fatalf("Input %d NormalizeMnemonicInput mismatch: expected %q, got %q", idx, canonicalMnemonic, cleaned)
		}

		w, err := WalletFromMnemonic(raw, passphrase, 0)
		if err != nil {
			t.Fatalf("Input %d WalletFromMnemonic failed: %v", idx, err)
		}
		if w.Address != expectedWallet.Address {
			t.Fatalf("Input %d derived different address: expected %s, got %s", idx, expectedWallet.Address, w.Address)
		}
		if w.PublicKey != expectedWallet.PublicKey {
			t.Fatalf("Input %d derived different public key", idx)
		}
	}
}

// TestBIP39_ExistingWalletDerivationFixture verifies backward compatibility against pinned
// seed, address, and public-key values derived from the canonical v3.2.0 release [P2 Review].
//
// Migration and Compatibility Notice:
//   - In v3.2.0, passphrases were processed as raw byte slices without Unicode NFKD normalization.
//   - In v3.2.1-alpha, MnemonicToSeed strictly conforms to standard BIP-39 using UTF-8 NFKD normalization.
//   - For standard ASCII mnemonics with empty or alphanumeric/ASCII passphrases, the derivation is 100%
//     byte-for-byte identical to v3.2.0 as demonstrated by the pinned fixtures below.
//   - If a legacy wallet used a non-ASCII Unicode passphrase with precomposed characters, users should
//     preserve their wallet.json keystore or import using the exact NFKD-normalized passphrase.
func TestBIP39_ExistingWalletDerivationFixture(t *testing.T) {
	// 1. Pinned 24-word v3.2.0 testnet reference wallet (empty passphrase)
	mnemonic24 := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	const (
		expectedV320Seed24     = "408b285c123836004f4b8842c89324c1f01382450c0d439af345ba7fc49acf705489c6fc77dbd4e3dc1dd8cc6bc9f043db8ada1e243c4a0eafb290d399480840"
		expectedV320Addr24Acc0 = "armx164m9zmj93tsnwzymuuap5krzen8cxlpl9dmgzh8v8vs9l0rnd3pqdwtak2"
		expectedV320Pub24Acc0  = "b91780d93264157bce558788109fd515e1ff84003f41e7ab396ae5d6ae81617b"
		expectedV320Addr24Acc1 = "armx1fggvz88ucst3u7kv7680c4pjf42f4t6jk9x0kgw42xfla8du0hnqk0gtkv"
		expectedV320Pub24Acc1  = "156558b1121fe40233bc965fc3ee9524bb45fdcbc630cdcda377521a26f1285d"
	)

	seed24 := MnemonicToSeed(mnemonic24, "")
	if hex.EncodeToString(seed24) != expectedV320Seed24 {
		t.Fatalf("24-word seed mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Seed24, hex.EncodeToString(seed24))
	}

	w24Acc0, err := WalletFromMnemonic(mnemonic24, "", 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic (24-word acc 0) failed: %v", err)
	}
	if w24Acc0.Address != expectedV320Addr24Acc0 {
		t.Fatalf("24-word Acc0 address mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Addr24Acc0, w24Acc0.Address)
	}
	if !strings.HasPrefix(w24Acc0.PublicKey, expectedV320Pub24Acc0) {
		t.Fatalf("24-word Acc0 public key prefix mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Pub24Acc0, w24Acc0.PublicKey[:64])
	}

	w24Acc1, err := WalletFromMnemonic(mnemonic24, "", 1)
	if err != nil {
		t.Fatalf("WalletFromMnemonic (24-word acc 1) failed: %v", err)
	}
	if w24Acc1.Address != expectedV320Addr24Acc1 {
		t.Fatalf("24-word Acc1 address mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Addr24Acc1, w24Acc1.Address)
	}
	if !strings.HasPrefix(w24Acc1.PublicKey, expectedV320Pub24Acc1) {
		t.Fatalf("24-word Acc1 public key prefix mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Pub24Acc1, w24Acc1.PublicKey[:64])
	}

	// 2. Pinned 12-word v3.2.0 testnet reference wallet (empty passphrase)
	mnemonic12 := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	const (
		expectedV320Seed12     = "5eb00bbddcf069084889a8ab9155568165f5c453ccb85e70811aaed6f6da5fc19a5ac40b389cd370d086206dec8aa6c43daea6690f20ad3d8d48b2d2ce9e38e4"
		expectedV320Addr12Acc0 = "armx1slsg5rfks8v2z7jy00s9gz8wqseyrel7v4p84nkma99pf23p79asc0jfmf"
		expectedV320Pub12Acc0  = "9920032354d46efafcf95a4b784a9c343cc4db4d463e5091b723ca8e6b2174d2"
	)

	seed12 := MnemonicToSeed(mnemonic12, "")
	if hex.EncodeToString(seed12) != expectedV320Seed12 {
		t.Fatalf("12-word seed mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Seed12, hex.EncodeToString(seed12))
	}

	w12Acc0, err := WalletFromMnemonic(mnemonic12, "", 0)
	if err != nil {
		t.Fatalf("WalletFromMnemonic (12-word acc 0) failed: %v", err)
	}
	if w12Acc0.Address != expectedV320Addr12Acc0 {
		t.Fatalf("12-word Acc0 address mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Addr12Acc0, w12Acc0.Address)
	}
	if !strings.HasPrefix(w12Acc0.PublicKey, expectedV320Pub12Acc0) {
		t.Fatalf("12-word Acc0 public key prefix mismatch against v3.2.0 fixture:\nexpected: %s\ngot:      %s", expectedV320Pub12Acc0, w12Acc0.PublicKey[:64])
	}
}
