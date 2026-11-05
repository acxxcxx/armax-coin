package node

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// An inbound connection that never delivers a first message must be dropped after
// InboundHandshakeTimeout, not after the longer SocketTimeoutDuration.
func TestInboundHandshakeTimeoutDropsSilentConnection(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	dir := filepath.Join(t.TempDir(), "node")
	bc := core.NewBlockchain(1)
	bc.StoragePath = filepath.Join(dir, "blockchain.db")
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()
	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(SocketTimeoutDuration))
	buf := make([]byte, 64)
	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}
	}
	elapsed := time.Since(start)

	if elapsed < InboundHandshakeTimeout-time.Second {
		t.Fatalf("connection dropped too early: %v", elapsed)
	}
	if elapsed > InboundHandshakeTimeout+5*time.Second {
		t.Fatalf("silent connection kept for %v; expected about %v", elapsed, InboundHandshakeTimeout)
	}
}
