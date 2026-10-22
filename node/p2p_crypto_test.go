package node

import (
	"os"
	"strings"
	"testing"
)

func TestP2PTLSIdentityCorruptionFailsClosed(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	path := GetNodeTLSCertPath()
	if err := os.WriteFile(path, []byte("not a PEM identity"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrGenerateNodeTLSCertificate()
	if err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("corrupt P2P TLS identity must fail closed, got %v", err)
	}
}
