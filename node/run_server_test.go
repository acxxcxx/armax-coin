package node

import (
	"errors"
	"strings"
	"testing"
)

func TestRunServerWithConfigReturnsErrorOnUnsafeBind(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RPCBind = "0.0.0.0:19333"
	cfg.AllowNonLoopbackRPC = false

	err := RunServerWithConfig(nil, cfg)
	if err == nil {
		t.Fatal("expected error for non-loopback bind without opt-in")
	}
	if !errors.Is(err, errFatalStartup) || !strings.Contains(err.Error(), "FATAL RPC SECURITY REFUSAL") {
		t.Fatalf("unexpected error: %v", err)
	}
}
