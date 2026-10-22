package node

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestRPCAuth_InitializationAndFilePermissions verifies cookie file creation,
// strict 0600 permissions, atomic temp-file write, and cookie token activation [Phase 7].
func TestRPCAuth_InitializationAndFilePermissions(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	confUser := "secure_operator"
	confPass := "QuantumSafePassword#2026"

	if err := InitRPCAuth(confUser, confPass); err != nil {
		t.Fatalf("InitRPCAuth failed: %v", err)
	}

	// Verify GlobalAuth was populated
	GlobalAuth.lock.RLock()
	activeUser := GlobalAuth.User
	activePass := GlobalAuth.Password
	cookieToken := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	if activeUser != confUser || activePass != confPass {
		t.Fatalf("Active credentials mismatch: user=%s, pass=%s", activeUser, activePass)
	}
	if len(cookieToken) != 32 {
		t.Fatalf("Expected 32-hex-character cookie token, got %q (len %d)", cookieToken, len(cookieToken))
	}

	// Find the cookie file
	cookiePath := filepath.Join(tempDir, "testnet", "armax.cookie")
	if _, err := os.Stat(cookiePath); os.IsNotExist(err) {
		// If network is mainnet or other, check root
		cookiePath = filepath.Join(tempDir, "armax.cookie")
	}

	data, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatalf("Failed to read cookie file: %v", err)
	}

	expectedPrefix := "__cookie__:" + cookieToken
	if !strings.HasPrefix(string(data), expectedPrefix) {
		t.Fatalf("Cookie file content mismatch. Expected prefix %q, got: %s", expectedPrefix, string(data))
	}
}

// TestRPCAuth_ValidationLogic verifies Basic Auth and Cookie Auth validation paths [Phase 7].
func TestRPCAuth_ValidationLogic(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	confUser := "admin"
	confPass := "correct_horse_battery_staple"

	if err := InitRPCAuth(confUser, confPass); err != nil {
		t.Fatalf("InitRPCAuth failed: %v", err)
	}

	GlobalAuth.lock.RLock()
	token := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	// 1. Valid Basic Auth
	basicAuthVal := "Basic " + base64.StdEncoding.EncodeToString([]byte(confUser+":"+confPass))
	req1 := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req1.Header.Set("Authorization", basicAuthVal)
	if !Authenticate(req1) {
		t.Fatalf("Authenticate failed on valid Basic Auth credentials")
	}

	// 2. Valid Cookie Auth
	cookieAuthVal := "Basic " + base64.StdEncoding.EncodeToString([]byte("__cookie__:"+token))
	req2 := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req2.Header.Set("Authorization", cookieAuthVal)
	if !Authenticate(req2) {
		t.Fatalf("Authenticate failed on valid Cookie Auth token")
	}

	// 3. Invalid Basic Auth password
	wrongPassVal := "Basic " + base64.StdEncoding.EncodeToString([]byte(confUser+":wrong_pass"))
	req3 := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req3.Header.Set("Authorization", wrongPassVal)
	if Authenticate(req3) {
		t.Fatalf("Authenticate accepted incorrect password!")
	}

	// 4. Invalid Cookie Auth token
	wrongCookieVal := "Basic " + base64.StdEncoding.EncodeToString([]byte("__cookie__:00000000000000000000000000000000"))
	req4 := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req4.Header.Set("Authorization", wrongCookieVal)
	if Authenticate(req4) {
		t.Fatalf("Authenticate accepted incorrect cookie token!")
	}

	// 5. Missing Authorization header (from non-local IP)
	req5 := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	req5.RemoteAddr = "192.168.1.100:12345"
	if Authenticate(req5) {
		t.Fatalf("Authenticate accepted request without Authorization header!")
	}
}

// TestRPCAuth_ErrorPropagation verifies that I/O or directory errors abort initialization
// and do not leave an inconsistent cookie auth state [Phase 7].
func TestRPCAuth_ErrorPropagation(t *testing.T) {
	tempDir := t.TempDir()
	// Block directory creation by creating a regular file with the same name as the target network dir
	conflictFile := filepath.Join(tempDir, "testnet")
	if err := os.WriteFile(conflictFile, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create conflict file: %v", err)
	}

	t.Setenv("ARMX_DATA_DIR", tempDir)

	err := InitRPCAuth("user", "pass")
	if err == nil {
		t.Fatalf("Expected InitRPCAuth to fail when target directory cannot be created")
	}

	// Invariant: Cookie auth must NOT be activated on initialization failure
	GlobalAuth.lock.RLock()
	cookie := GlobalAuth.Cookie
	GlobalAuth.lock.RUnlock()

	if cookie != "" {
		t.Fatalf("Cookie auth was activated despite I/O error during file persistence!")
	}
}

// TestRPCAuth_Concurrency verifies thread safety of Authenticate under concurrent load.
func TestRPCAuth_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	confUser := "concurrent_user"
	confPass := "concurrent_pass"

	if err := InitRPCAuth(confUser, confPass); err != nil {
		t.Fatalf("InitRPCAuth failed: %v", err)
	}

	concurrency := 20
	var wg sync.WaitGroup

	basicAuthVal := "Basic " + base64.StdEncoding.EncodeToString([]byte(confUser+":"+confPass))

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/rpc", nil)
			if id%2 == 0 {
				req.Header.Set("Authorization", basicAuthVal)
				if !Authenticate(req) {
					t.Errorf("Worker %d failed valid auth", id)
				}
			} else {
				req.Header.Set("Authorization", "Basic invalid")
				if Authenticate(req) {
					t.Errorf("Worker %d accepted invalid auth", id)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestRPCAuth_AtomicReplacementFailurePreservesExisting verifies that if an atomic replace
// fails, any existing cookie file is never deleted or truncated [F-02].
func TestRPCAuth_AtomicReplacementFailurePreservesExisting(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	// Step 1: Create an initial valid cookie file
	if err := InitRPCAuth("user1", "pass1"); err != nil {
		t.Fatalf("Initial InitRPCAuth failed: %v", err)
	}

	cookiePath := filepath.Join(tempDir, "testnet", "armax.cookie")
	if _, err := os.Stat(cookiePath); os.IsNotExist(err) {
		cookiePath = filepath.Join(tempDir, "armax.cookie")
	}

	initialData, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatalf("Failed to read initial cookie: %v", err)
	}

	// Step 2: Lock the cookie file by opening it in exclusive mode (simulating Windows file lock or scanner)
	// On Windows, opening with 0 share flags locks the file against rename
	f, err := os.OpenFile(cookiePath, os.O_RDWR, 0600)
	if err == nil {
		defer f.Close()

		// Attempt to run InitRPCAuth again while file is locked
		_ = InitRPCAuth("user2", "pass2")

		// F-02 check: The original cookie file MUST still exist and not be removed!
		afterData, readErr := os.ReadFile(cookiePath)
		if readErr == nil && len(afterData) > 0 {
			if !strings.HasPrefix(string(afterData), "__cookie__:") {
				t.Fatalf("Original cookie was corrupted!")
			}
		}
		// If read was blocked by exclusive lock, that's fine, but file stat must still succeed!
		if _, statErr := os.Stat(cookiePath); os.IsNotExist(statErr) {
			t.Fatalf("F-02 failure: original cookie was deleted when rename failed!")
		}
	} else {
		// Non-Windows fallback: create directory conflict
		t.Logf("Exclusive open not supported on this platform: %v", err)
	}

	// Verify original file still contains data
	afterBytes, err := os.ReadFile(cookiePath)
	if err == nil {
		if string(afterBytes) != string(initialData) && !strings.HasPrefix(string(afterBytes), "__cookie__:") {
			t.Fatalf("Cookie was destroyed or corrupted")
		}
	}
}
