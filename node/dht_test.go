package node

import (
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestRoutingTable(t *testing.T) {
	localAddr := "127.0.0.1:" + core.MainnetP2PPort
	rt := NewRoutingTable(localAddr)

	peer1 := "192.168.1.10:" + core.MainnetP2PPort
	peer2 := "10.0.0.5:" + core.MainnetP2PPort
	peer3 := "172.16.0.2:" + core.MainnetP2PPort

	rt.Update(peer1)
	rt.Update(peer2)
	rt.Update(peer3)

	addrs := rt.GetAllAddresses()
	if len(addrs) != 3 {
		t.Fatalf("Expected 3 peers in routing table, got %d", len(addrs))
	}

	target := GenerateRandomTarget()
	closest := rt.FindClosestNodes(target, 2)
	if len(closest) != 2 {
		t.Fatalf("Expected 2 closest nodes, got %d", len(closest))
	}

	for _, n := range closest {
		if len(n.IDHex) != 64 {
			t.Fatalf("Expected 64 hex char ID, got %s", n.IDHex)
		}
	}
}

func TestXORDistance(t *testing.T) {
	var a, b [32]byte
	a[0] = 0b10100000
	b[0] = 0b10010000

	dist := XOR(a, b)
	if dist[0] != 0b00110000 {
		t.Fatalf("Unexpected XOR distance: %08b", dist[0])
	}

	cpl := CommonPrefixLen(a, b)
	if cpl != 2 {
		t.Fatalf("Expected CommonPrefixLen 2, got %d", cpl)
	}
}
