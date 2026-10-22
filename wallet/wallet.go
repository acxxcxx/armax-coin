package wallet

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/core/pqsig"
)

// Wallet represents an Armaxcoin quantum-safe account backed by NIST FIPS 204 ML-DSA-65 [P2-3]
type Wallet struct {
	PrivateKey *pqsig.PrivateKey `json:"-"`
	Seed32     [32]byte          `json:"-"` // Kept in memory, encrypted at rest in keystore
	PublicKey  string            `json:"public_key"`
	Address    string            `json:"address"`
	Mnemonic   string            `json:"-"` // Displayed once upon creation, NEVER stored in plaintext
}

// NewWallet generates a new post-quantum wallet with a deterministic 24-word BIP-39 mnemonic [P2-3, F4]
func NewWallet() (*Wallet, error) {
	mnemonic, err := GenerateMnemonic(24)
	if err != nil {
		return nil, fmt.Errorf("failed to generate 24-word mnemonic: %w", err)
	}

	w, err := WalletFromMnemonic(mnemonic, "", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize wallet from mnemonic: %w", err)
	}
	return w, nil
}

// MustNewWallet generates a new wallet or panics on initialization failure (for tests and quick fixtures)
func MustNewWallet() *Wallet {
	w, err := NewWallet()
	if err != nil {
		panic(fmt.Sprintf("MustNewWallet failed: %v", err))
	}
	return w
}

// WalletFromSeed initializes an Armaxcoin post-quantum wallet from an existing 32-byte ML-DSA-65 seed [P2-3]
func WalletFromSeed(seed32 [32]byte) (*Wallet, error) {
	sk, err := pqsig.GenerateFromSeed(seed32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ML-DSA-65 key from seed: %w", err)
	}

	pubBytes := sk.PublicKeyBytes()
	pubHex := hex.EncodeToString(pubBytes)
	address, err := core.PubToAddress(pqsig.SigAlgMLDSA65, pubBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to derive address: %w", err)
	}

	return &Wallet{
		PrivateKey: sk,
		Seed32:     seed32,
		PublicKey:  pubHex,
		Address:    address,
	}, nil
}

// AddressFromPubKeyHex derives the canonical Bech32 address from a public key hex string
func AddressFromPubKeyHex(pubHex string) (string, error) {
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != pqsig.PublicKeySize {
		return "", errors.New("invalid ML-DSA-65 public key length")
	}
	return core.PubToAddress(pqsig.SigAlgMLDSA65, pubBytes)
}
