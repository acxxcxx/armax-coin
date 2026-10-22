package node

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestN13_BoundedMessageReaderDropsOversizedStream verifies [N-13]:
// HandleConnection drops incoming streams exceeding 2 MB without newline immediately.
func TestN13_BoundedMessageReaderDropsOversizedStream(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	bc := core.NewBlockchain(1)
	defer bc.Close()

	port := getFreePort(t)
	ps := NewPeerServer(port, bc)
	go ps.StartServer()

	time.Sleep(100 * time.Millisecond)

	conn, _, err := DialEncrypted("127.0.0.1:"+port, 3*time.Second)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer conn.Close()

	// Send a 5 MB stream with no newlines (exceeding the 4 MB hard limit)
	chunk := bytes.Repeat([]byte("A"), 64*1024)

	totalSent := 0
	droppedEarly := false

	// Attempt to stream 5 MB (80 * 64 KB = 5 MB > 4 MB)
	for i := 0; i < 80; i++ {
		_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Write(chunk)
		if err != nil {
			droppedEarly = true
			break
		}
		totalSent += n
	}

	if !droppedEarly {
		// Verify connection was severed by server (EOF, not read timeout)
		buf := make([]byte, 10)
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buf)
		if err == nil {
			t.Fatalf("[N-13] Server did NOT disconnect client sending > 2 MB stream without newline! Read %d bytes", n)
		}
		if strings.Contains(err.Error(), "timeout") {
			t.Fatalf("[N-13] Server hung on oversized stream without disconnecting (timed out instead of EOF): %v", err)
		}
	}
}
