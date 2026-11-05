package node

import (
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestSeedFingerprintPin(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)
	p.P2PTOFU = true

	seed := "192.168.1.90:19335"
	other := "192.168.1.91:19335"
	good := "aabbccdd00112233445566778899aabbccdd00112233445566778899aabbccdd"
	evil := "0000000000000000000000000000000000000000000000000000000000000001"

	if err := p.SetSeedFingerprints(map[string]string{seed: strings.ToUpper(good)}); err != nil {
		t.Fatalf("SetSeedFingerprints: %v", err)
	}

	// An impostor on first contact is refused and leaves no pin behind.
	if err := p.VerifyOrRecordTOFU(seed, &mockTLSConn{fingerprint: evil}); err == nil {
		t.Fatal("impostor seed certificate must be rejected on first contact")
	}
	p.Lock.RLock()
	_, pinned := p.triedPeers[seed]
	p.Lock.RUnlock()
	if pinned {
		t.Fatal("rejected certificate must not be TOFU-pinned")
	}

	if err := p.VerifyOrRecordTOFU(seed, &mockTLSConn{fingerprint: good}); err != nil {
		t.Fatalf("genuine seed certificate rejected: %v", err)
	}

	// Peers without a configured pin keep plain TOFU behavior.
	if err := p.VerifyOrRecordTOFU(other, &mockTLSConn{fingerprint: evil}); err != nil {
		t.Fatalf("unpinned peer should use TOFU: %v", err)
	}
}

func TestSeedFingerprintValidation(t *testing.T) {
	bc := core.MustNewBlockchain(1)
	defer bc.Close()
	p := NewPeerServer("9999", bc)
	if err := p.SetSeedFingerprints(map[string]string{"1.2.3.4:19335": "short"}); err == nil {
		t.Fatal("malformed fingerprint must be rejected")
	}
	if err := p.SetSeedFingerprints(map[string]string{"1.2.3.4": strings.Repeat("a", 64)}); err == nil {
		t.Fatal("key without port must be rejected")
	}
}
