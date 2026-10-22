package node

import (
	"net"
	"testing"
)

func TestOutboundSessionCloseUnregistersActiveConnection(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()

	cycle := &serverCycle{activeConns: map[net.Conn]struct{}{local: {}}}
	session := &outboundPeerSession{conn: local, cycle: cycle}

	session.close()

	cycle.connsMu.Lock()
	_, registered := cycle.activeConns[local]
	cycle.connsMu.Unlock()
	if registered {
		t.Fatal("closed outbound session remained registered in activeConns")
	}
}
