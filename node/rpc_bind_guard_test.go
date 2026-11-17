package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestF8_RPCBindGuard tests that non-loopback RPC binding requires both an explicit opt-in flag
// and active authentication credentials, else the node refuses to start [F8].
func TestF8_RPCBindGuard(t *testing.T) {
	tlsDir := t.TempDir()
	certPath := filepath.Join(tlsDir, "rpc-cert.pem")
	keyPath := filepath.Join(tlsDir, "rpc-key.pem")
	if err := os.WriteFile(certPath, []byte("test certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}
	// Case 1: Standard loopback binds (127.0.0.1, localhost, ::1, empty) must be permitted
	loopbackAddrs := []string{"127.0.0.1", "localhost", "::1", ""}
	for _, addr := range loopbackAddrs {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.RPCUser = ""
		cfg.RPCPassword = ""
		if err := ValidateRPCBind(cfg); err != nil {
			t.Fatalf("ValidateRPCBind rejected safe loopback address '%s': %v", addr, err)
		}
	}

	// Case 1b: Host:Port loopback binds must be permitted without credentials [F8-BLOCKER]
	loopbackHostPorts := []string{"127.0.0.1:19333", "[::1]:19333", "localhost:19333"}
	for _, addr := range loopbackHostPorts {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.RPCUser = ""
		cfg.RPCPassword = ""
		if err := ValidateRPCBind(cfg); err != nil {
			t.Fatalf("ValidateRPCBind rejected safe loopback host:port '%s': %v", addr, err)
		}
	}

	// Case 2: Non-loopback address without AllowNonLoopbackRPC flag must be refused
	nonLoopbackAddrs := []string{"0.0.0.0", "192.168.1.100", "10.0.0.1", "8.8.8.8"}
	for _, addr := range nonLoopbackAddrs {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.AllowNonLoopbackRPC = false
		cfg.RPCUser = "admin"
		cfg.RPCPassword = "secretpassword"

		err := ValidateRPCBind(cfg)
		if err == nil {
			t.Fatalf("ValidateRPCBind should have refused non-loopback address '%s' without AllowNonLoopbackRPC flag", addr)
		}
		if !strings.Contains(err.Error(), "requires explicit opt-in flag") {
			t.Fatalf("Unexpected error message for missing flag: %v", err)
		}
	}

	// Case 2b: Host:Port non-loopback binds without AllowNonLoopbackRPC must be refused [F8-BLOCKER]
	nonLoopbackHostPorts := []string{"0.0.0.0:19333", "192.168.1.5:19333"}
	for _, addr := range nonLoopbackHostPorts {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.AllowNonLoopbackRPC = false
		cfg.RPCUser = "admin"
		cfg.RPCPassword = "secretpassword"

		err := ValidateRPCBind(cfg)
		if err == nil {
			t.Fatalf("ValidateRPCBind should have refused non-loopback host:port '%s' without AllowNonLoopbackRPC flag", addr)
		}
		if !strings.Contains(err.Error(), "requires explicit opt-in flag") {
			t.Fatalf("Unexpected error message for missing flag: %v", err)
		}
	}

	// Case 3: Non-loopback with AllowNonLoopbackRPC = true but missing RPC user/password credentials must be refused
	for _, addr := range append(nonLoopbackAddrs, nonLoopbackHostPorts...) {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.AllowNonLoopbackRPC = true
		cfg.RPCUser = ""
		cfg.RPCPassword = ""

		err := ValidateRPCBind(cfg)
		if err == nil {
			t.Fatalf("ValidateRPCBind should have refused non-loopback address '%s' with unauthenticated credentials", addr)
		}
		if !strings.Contains(err.Error(), "mandatory") {
			t.Fatalf("Unexpected error message for missing auth: %v", err)
		}
	}

	// Case 4: Non-loopback with AllowNonLoopbackRPC = true AND valid credentials must succeed
	for _, addr := range append(nonLoopbackAddrs, nonLoopbackHostPorts...) {
		cfg := DefaultConfig()
		cfg.RPCBind = addr
		cfg.AllowNonLoopbackRPC = true
		cfg.RPCUser = "armaxadmin"
		cfg.RPCPassword = "StrongQuantumPassword123!"
		cfg.RPCTLSCert = certPath
		cfg.RPCTLSKey = keyPath

		err := ValidateRPCBind(cfg)
		if err != nil {
			t.Fatalf("ValidateRPCBind rejected valid non-loopback configuration for '%s': %v", addr, err)
		}
	}

	// Case 5: Verify that StartServerWithConfig panics if ValidateRPCBind fails
	t.Run("StartServerPanicsOnInvalidBind", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RPCBind = "0.0.0.0:19333"
		cfg.AllowNonLoopbackRPC = false

		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("Expected StartServerWithConfig to panic on invalid non-loopback bind, but it did not panic")
			}
			msg, ok := r.(string)
			if !ok || !strings.Contains(msg, "FATAL RPC SECURITY REFUSAL") {
				t.Fatalf("Unexpected panic message: %v", r)
			}
		}()

		StartServerWithConfig(nil, cfg)
	})
}
