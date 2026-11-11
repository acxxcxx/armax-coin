package node

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWhitelistExemptFromBanScore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	defer ps.Stop()

	testIP := "198.51.100.5"

	// 1. Verify that before whitelist, 100 score results in a ban
	ps.AddBanScore(testIP, 100)
	if !ps.IsBanned(testIP) {
		t.Fatalf("expected IP %s to be banned before whitelisting", testIP)
	}

	// 2. Add to whitelist programmatically -> must unban immediately
	ps.AddWhitelistIP(testIP)
	if ps.IsBanned(testIP) {
		t.Fatalf("expected IP %s to be unbanned immediately after AddWhitelistIP", testIP)
	}

	// 3. Adding more ban scores to whitelisted IP must have zero effect
	ps.AddBanScore(testIP, 500)
	if ps.IsBanned(testIP) {
		t.Fatalf("whitelisted IP %s must never be banned by AddBanScore", testIP)
	}

	ps.banLock.RLock()
	score := ps.banScores[testIP]
	ps.banLock.RUnlock()
	if score != 0 {
		t.Fatalf("expected banScore 0 for whitelisted IP, got %d", score)
	}

	time.Sleep(50 * time.Millisecond)
}

func TestWhitelistFileLoadingAndUnbanning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	defer ps.Stop()

	ip1 := "192.0.2.11"
	ip2 := "192.0.2.12"

	// Ban ip1 first
	ps.AddBanScore(ip1, 100)
	if !ps.IsBanned(ip1) {
		t.Fatalf("IP %s must be banned initially", ip1)
	}

	// Write whitelist.json containing ip1 and ip2
	whitelistPath := ps.whitelistStorePath()
	if whitelistPath == "" {
		t.Fatal("expected non-empty whitelistStorePath")
	}

	data, err := json.Marshal([]string{ip1, ip2})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(whitelistPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Load whitelist from disk
	ps.loadWhitelist()

	if !ps.isWhitelisted(ip1) {
		t.Fatalf("expected %s to be whitelisted", ip1)
	}
	if !ps.isWhitelisted(ip2) {
		t.Fatalf("expected %s to be whitelisted", ip2)
	}
	if ps.IsBanned(ip1) {
		t.Fatalf("expected %s to be unbanned after loading whitelist.json", ip1)
	}

	time.Sleep(50 * time.Millisecond)
}

func TestWhitelistRateLimitBypass(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	defer ps.Stop()

	exemptIP := "203.0.113.99"
	ps.AddWhitelistIP(exemptIP)

	// Send 100 messages in rapid succession (far exceeding normal 20 msg/s limit)
	for i := 0; i < 100; i++ {
		if !ps.AllowMessage(exemptIP, "GET_BLOCKS") {
			t.Fatalf("whitelisted IP %s must never be rate limited", exemptIP)
		}
	}

	if ps.IsBanned(exemptIP) {
		t.Fatalf("whitelisted IP %s must not be banned from message flood", exemptIP)
	}
}

func TestWhitelistEnvVarSupport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")
	t.Setenv("ARMX_WHITELIST_IPS", "198.51.100.77, 198.51.100.88:19335")

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	defer ps.Stop()

	if !ps.isWhitelisted("198.51.100.77") {
		t.Fatal("expected 198.51.100.77 to be whitelisted via ARMX_WHITELIST_IPS")
	}
	if !ps.isWhitelisted("198.51.100.88") {
		t.Fatal("expected 198.51.100.88 (stripped of port) to be whitelisted via ARMX_WHITELIST_IPS")
	}
}

func TestWhitelistHotReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", dir)
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	defer ps.Stop()

	whitelistPath := ps.whitelistStorePath()
	if whitelistPath == "" {
		t.Fatal("whitelistStorePath is empty")
	}

	// 1. Write initial whitelist
	initData, _ := json.Marshal([]string{"198.51.100.10"})
	_ = os.WriteFile(whitelistPath, initData, 0o600)
	ps.loadWhitelist()

	if !ps.isWhitelisted("198.51.100.10") {
		t.Fatal("198.51.100.10 should be whitelisted")
	}
	if ps.isWhitelisted("198.51.100.20") {
		t.Fatal("198.51.100.20 should not be whitelisted yet")
	}

	// Wait briefly to ensure filesystem timestamp ticks
	time.Sleep(100 * time.Millisecond)

	// 2. Overwrite file with new IP added
	newData, _ := json.Marshal([]string{"198.51.100.10", "198.51.100.20"})
	_ = os.WriteFile(whitelistPath, newData, 0o600)
	futureTime := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(whitelistPath, futureTime, futureTime)

	// isWhitelisted triggers hot-reload check automatically
	if !ps.isWhitelisted("198.51.100.20") {
		t.Fatal("198.51.100.20 should be whitelisted dynamically after file change")
	}
}
