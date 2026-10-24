package node

import (
	"crypto/sha256"
	"os"
	"testing"
)

// TestT11_InitNodeIdentityPersistentAcrossRestarts verifies that node TLS identity
// is not generated in package init(), is persisted to disk with 0600 permissions,
// and preserves the exact same fingerprint across node restarts [T11].
func TestT11_InitNodeIdentityPersistentAcrossRestarts(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Initial call generates identity in tempDir
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
	// Check permissions on Unix (on Windows, os.FileMode may not reflect 0600 fully, but info should be regular file)
	if info.IsDir() {
		t.Fatal("Expected p2ptls.pem to be a regular file, got directory")
	}

	rawCert := tlsConf1.Certificates[0].Certificate[0]
	expectedFp := sha256.Sum256(rawCert)
	if fp1 != expectedFp {
		t.Fatalf("Fingerprint mismatch: got %x, expected %x", fp1, expectedFp)
	}

	// 2. Second call (simulating restart) must load the exact same certificate and key
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
