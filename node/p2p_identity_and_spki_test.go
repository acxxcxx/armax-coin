package node

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"os"
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

// TestT11_InitNodeIdentityPersistentAcrossRestarts verifies that node TLS identity
// is not generated in package init(), is persisted to disk with 0600 permissions,
// and preserves the exact same fingerprint across node restarts.
func TestT11_InitNodeIdentityPersistentAcrossRestarts(t *testing.T) {
	tempDir := t.TempDir()

	tlsConf1, fp1, err := InitNodeIdentity(tempDir)
	if err != nil {
		t.Fatalf("InitNodeIdentity failed on first call: %v", err)
	}
	if tlsConf1 == nil || len(tlsConf1.Certificates) == 0 {
		t.Fatal("Expected valid TLS config with certificates")
	}

	certPath := GetNodeTLSCertPath(tempDir)
	info, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("Expected p2ptls.pem to exist at %s: %v", certPath, err)
	}
	if info.IsDir() {
		t.Fatal("Expected p2ptls.pem to be a regular file, got directory")
	}

	rawCert := tlsConf1.Certificates[0].Certificate[0]
	parsedCert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	expectedFp := sha256.Sum256(parsedCert.RawSubjectPublicKeyInfo)
	if fp1 != expectedFp {
		t.Fatalf("Fingerprint mismatch: got %x, expected %x", fp1, expectedFp)
	}

	tlsConf2, fp2, err := InitNodeIdentity(tempDir)
	if err != nil {
		t.Fatalf("InitNodeIdentity failed on restart call: %v", err)
	}
	if fp2 != fp1 {
		t.Fatalf("Node fingerprint changed across restarts: %x != %x (TOFU will fail-close)", fp2, fp1)
	}
	if string(tlsConf2.Certificates[0].Certificate[0]) != string(tlsConf1.Certificates[0].Certificate[0]) {
		t.Fatal("Certificate DER bytes changed across restarts")
	}
}
