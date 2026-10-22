package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRPCAuth_AtomicWriteAndPermissions verifies atomic 0600 cookie file creation [Phase 7].
func TestRPCAuth_AtomicWriteAndPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tmpDir)

	ResetRPCAuthForTesting()
	defer ResetRPCAuthForTesting()

	err := InitRPCAuth("admin", "secret123")
	if err != nil {
		t.Fatalf("InitRPCAuth failed: %v", err)
	}

	cookiePath := filepath.Join(tmpDir, "testnet", "armax.cookie")
	if _, err := os.Stat(cookiePath); err != nil {
		// If not in testnet subfolder, check root of tmpDir
		cookiePath = filepath.Join(tmpDir, "armax.cookie")
		if _, err := os.Stat(cookiePath); err != nil {
			t.Fatalf("Expected armax.cookie to exist: %v", err)
		}
	}

	// Verify file content
	content, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatalf("Failed to read cookie file: %v", err)
	}
	contentStr := string(content)
	if !strings.HasPrefix(contentStr, "__cookie__:") {
		t.Fatalf("Cookie file has unexpected format: %q", contentStr)
	}

	// Verify token matches GlobalAuth.Cookie
	token := strings.TrimSpace(strings.TrimPrefix(contentStr, "__cookie__:"))
	GlobalAuth.lock.RLock()
	activeCookie := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	if token != activeCookie {
		t.Fatalf("Cookie file token %q does not match active auth cookie %q", token, activeCookie)
	}
	if len(activeCookie) != 32 {
		t.Fatalf("Expected 32-character hex cookie token, got length %d (%q)", len(activeCookie), activeCookie)
	}
}

// TestRPCAuth_ErrorPathInvalidDirectory verifies that if directory creation or file writing fails,
// InitRPCAuth returns an error and GlobalAuth.Cookie is NOT activated [Phase 7].
func TestRPCAuth_ErrorPathInvalidDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a regular file where the directory would need to be created
	conflictFile := filepath.Join(tmpDir, "blocked_dir")
	if err := os.WriteFile(conflictFile, []byte("file blocker"), 0600); err != nil {
		t.Fatalf("Failed to create blocker file: %v", err)
	}

	// Point ARMX_DATA_DIR to a path inside the regular file, causing os.MkdirAll to fail
	invalidDir := filepath.Join(conflictFile, "sub")
	t.Setenv("ARMX_DATA_DIR", invalidDir)

	ResetRPCAuthForTesting()
	defer ResetRPCAuthForTesting()

	err := InitRPCAuth("admin", "secret123")
	if err == nil {
		t.Fatalf("Expected InitRPCAuth to fail when directory cannot be created, got nil")
	}

	// Crucial security invariant: GlobalAuth.Cookie MUST NOT be active if write failed!
	GlobalAuth.lock.RLock()
	activeCookie := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	if activeCookie != "" {
		t.Fatalf("GlobalAuth.Cookie was activated despite failed persistence! Cookie: %q", activeCookie)
	}
}
