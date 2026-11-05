package node

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNodeIdentity_FingerprintMatchesSPKI(t *testing.T) {
	tempDir := t.TempDir()
	conf, fp, err := InitNodeIdentity(tempDir)
	if err != nil {
		t.Fatalf("InitNodeIdentity failed: %v", err)
	}

	if len(conf.Certificates) == 0 || len(conf.Certificates[0].Certificate) == 0 {
		t.Fatalf("expected certificates in tls.Config")
	}

	rawCert := conf.Certificates[0].Certificate[0]
	parsed, err := x509.ParseCertificate(rawCert)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	expectedSPKIHash := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	if fp != expectedSPKIHash {
		t.Fatalf("fingerprint mismatch: got %x, expected SPKI %x", fp, expectedSPKIHash)
	}

	fpHex := hex.EncodeToString(fp[:])
	server := &PeerServer{}
	err = server.SetSeedFingerprints(map[string]string{
		"127.0.0.1:19335": fpHex,
	})
	if err != nil {
		t.Fatalf("SetSeedFingerprints rejected matching fingerprint: %v", err)
	}

	if err := server.checkSeedPin("127.0.0.1:19335", fpHex); err != nil {
		t.Fatalf("checkSeedPin failed on verified SPKI fingerprint: %v", err)
	}

	// Verify mismatched pin rejection
	bogusFP := strings.Repeat("0", 64)
	if err := server.checkSeedPin("127.0.0.1:19335", bogusFP); err == nil {
		t.Fatalf("checkSeedPin should fail on bogus fingerprint")
	}
}

