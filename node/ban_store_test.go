package node

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestBanStorePersistsAcrossRestart(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	ps.AddBanScore("203.0.113.9", 100)
	if !ps.IsBanned("203.0.113.9") {
		t.Fatal("ip should be banned")
	}
	// Persist is asynchronous when triggered by AddBanScore; write synchronously for determinism.
	ps.persistBans()

	restarted := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	if !restarted.IsBanned("203.0.113.9") {
		t.Fatal("ban was lost after restart")
	}
	if restarted.IsBanned("203.0.113.10") {
		t.Fatal("unrelated ip must not be banned")
	}
}

func TestBanStoreDropsExpiredAndCorrupt(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	ps := NewPeerServer("0", nil, dir, "ARMX-TESTNET-1")
	path := ps.banStorePath()
	if path == "" {
		t.Fatal("no ban store path")
	}

	expired := `{"203.0.113.9":` + strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10) + `}`
	if err := os.WriteFile(path, []byte(expired), 0o600); err != nil {
		t.Fatal(err)
	}
	ps.loadBans()
	if ps.IsBanned("203.0.113.9") {
		t.Fatal("expired ban must not be restored")
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ps.loadBans() // must not panic or ban anyone
}
