package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
		hasErr   bool
	}{
		{"error", LogLevelError, false},
		{"ERR", LogLevelError, false},
		{"warn", LogLevelWarn, false},
		{"warning", LogLevelWarn, false},
		{"info", LogLevelInfo, false},
		{"debug", LogLevelDebug, false},
		{"trace", LogLevelTrace, false},
		{"unknown_level", LogLevelInfo, true},
	}

	for _, tt := range tests {
		got, err := ParseLogLevel(tt.input)
		if tt.hasErr && err == nil {
			t.Errorf("ParseLogLevel(%q) expected error, got nil", tt.input)
		}
		if !tt.hasErr && err != nil {
			t.Errorf("ParseLogLevel(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.expected {
			t.Errorf("ParseLogLevel(%q) = %v; want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLoggerFiltering(t *testing.T) {
	logger := NewLogger()
	var buf bytes.Buffer
	logger.SetCustomWriter(&buf)
	logger.SetPrintToConsole(false)

	// Default level is INFO
	logger.SetLevel(LogLevelInfo)

	// INFO should be emitted
	logger.Log(LogLevelInfo, "validation", "Block accepted: #100")
	if !strings.Contains(buf.String(), "[INFO] [validation] Block accepted: #100") {
		t.Fatalf("Expected INFO log, got: %q", buf.String())
	}
	buf.Reset()

	// DEBUG should NOT be emitted by default when level is INFO
	logger.Log(LogLevelDebug, "net", "Ping peer 127.0.0.1")
	if buf.Len() > 0 {
		t.Fatalf("Expected DEBUG log to be filtered out at INFO level, got: %q", buf.String())
	}

	// Enable debug for category "net" specifically
	logger.SetDebugCategories("net")
	logger.Log(LogLevelDebug, "net", "Ping peer 127.0.0.1")
	if !strings.Contains(buf.String(), "[DEBUG] [net] Ping peer 127.0.0.1") {
		t.Fatalf("Expected category-enabled DEBUG log, got: %q", buf.String())
	}
	buf.Reset()

	// DEBUG for "sync" should still be filtered
	logger.Log(LogLevelDebug, "sync", "Querying headers")
	if buf.Len() > 0 {
		t.Fatalf("Expected non-enabled category DEBUG to be filtered, got: %q", buf.String())
	}

	// Enable all debug via "all"
	logger.SetDebugCategories("all")
	logger.Log(LogLevelDebug, "sync", "Querying headers")
	if !strings.Contains(buf.String(), "[DEBUG] [sync] Querying headers") {
		t.Fatalf("Expected global DEBUG log, got: %q", buf.String())
	}
	buf.Reset()

	// WARN and ERROR should always be emitted at INFO level
	logger.Log(LogLevelWarn, "net", "Peer timeout")
	logger.Log(LogLevelError, "storage", "Disk full")
	out := buf.String()
	if !strings.Contains(out, "[WARN] [net] Peer timeout") || !strings.Contains(out, "[ERROR] [storage] Disk full") {
		t.Fatalf("Expected WARN and ERROR logs, got: %q", out)
	}
}

func TestLoggerFileOutput(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test_debug.log")

	logger := NewLogger()
	logger.SetPrintToConsole(false)
	if err := logger.SetLogFile(logPath); err != nil {
		t.Fatalf("SetLogFile failed: %v", err)
	}

	logger.Log(LogLevelInfo, "test", "Persistent message written to disk")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	if !strings.Contains(string(data), "[INFO] [test] Persistent message written to disk") {
		t.Fatalf("Expected log content in file, got: %s", string(data))
	}
}
