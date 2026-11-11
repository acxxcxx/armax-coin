package node

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/bits"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	KademliaK          = 20               // Standard Kademlia bucket size
	KademliaIDBits     = 256              // 256-bit Node IDs (SHA-256)
	DHTRefreshInterval = 90 * time.Second // Periodic random target lookup interval
)

// DHTNode represents a peer in the Kademlia routing table
type DHTNode struct {
	ID       [32]byte  `json:"id"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
}

// DHTNodeInfo is a lightweight serializable representation for P2P wire transmission
type DHTNodeInfo struct {
	IDHex   string `json:"id"`
	Address string `json:"address"`
}

// XOR calculates bitwise XOR distance between two 32-byte IDs
func XOR(a, b [32]byte) [32]byte {
	var res [32]byte
	for i := 0; i < 32; i++ {
		res[i] = a[i] ^ b[i]
	}
	return res
}

// CommonPrefixLen returns the number of leading identical bits between two IDs
func CommonPrefixLen(a, b [32]byte) int {
	dist := XOR(a, b)
	for i, bVal := range dist {
		if bVal != 0 {
			return i*8 + bits.LeadingZeros8(bVal)
		}
	}
	return KademliaIDBits
}

// RoutingTable manages Kademlia k-buckets for decentralized node discovery
type RoutingTable struct {
	LocalNode DHTNode
	Buckets   [KademliaIDBits][]DHTNode
	mu        sync.RWMutex
}

// NewRoutingTable creates an active Kademlia routing table with a cryptographically unique node ID
func NewRoutingTable(address string) *RoutingTable {
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		h := sha256.Sum256([]byte(address + time.Now().String()))
		copy(id[:], h[:])
	}

	return &RoutingTable{
		LocalNode: DHTNode{
			ID:       id,
			Address:  address,
			LastSeen: time.Now(),
		},
	}
}

// Update adds or refreshes a peer in the appropriate k-bucket
func (rt *RoutingTable) Update(address string) {
	if address == "" || address == rt.LocalNode.Address {
		return
	}

	// Validate host:port format and valid port range [N-07]
	host, portStr, err := net.SplitHostPort(address)
	if err != nil || host == "" || portStr == "" {
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return
	}

	h := sha256.Sum256([]byte(address))
	var nodeID [32]byte
	copy(nodeID[:], h[:])

	cpl := CommonPrefixLen(rt.LocalNode.ID, nodeID)
	if cpl >= KademliaIDBits {
		cpl = KademliaIDBits - 1
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	bucket := rt.Buckets[cpl]
	// Check if already in bucket
	for i, n := range bucket {
		if n.Address == address {
			// Move to end (most recently seen)
			bucket = append(append(bucket[:i], bucket[i+1:]...), DHTNode{
				ID:       nodeID,
				Address:  address,
				LastSeen: time.Now(),
			})
			rt.Buckets[cpl] = bucket
			return
		}
	}

	// Insert if bucket has space
	if len(bucket) < KademliaK {
		rt.Buckets[cpl] = append(bucket, DHTNode{
			ID:       nodeID,
			Address:  address,
			LastSeen: time.Now(),
		})
	}
}

// FindClosestNodes returns the closest known nodes to a target ID by XOR metric.
// Nodes that have not been seen for >= 2 hours are excluded to avoid routing dead peers [v1.0.10].
func (rt *RoutingTable) FindClosestNodes(targetID [32]byte, count int) []DHTNodeInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	now := time.Now()
	var allNodes []DHTNode
	for _, b := range rt.Buckets {
		for _, n := range b {
			if !n.LastSeen.IsZero() && now.Sub(n.LastSeen) >= 2*time.Hour {
				continue
			}
			allNodes = append(allNodes, n)
		}
	}

	// Sort nodes by XOR distance to targetID
	type distNode struct {
		node DHTNode
		dist [32]byte
	}
	dnList := make([]distNode, len(allNodes))
	for i, n := range allNodes {
		dnList[i] = distNode{node: n, dist: XOR(n.ID, targetID)}
	}

	sort.Slice(dnList, func(i, j int) bool {
		return bytes.Compare(dnList[i].dist[:], dnList[j].dist[:]) < 0
	})

	limit := count
	if limit > len(dnList) {
		limit = len(dnList)
	}

	result := make([]DHTNodeInfo, limit)
	for i := 0; i < limit; i++ {
		result[i] = DHTNodeInfo{
			IDHex:   hex.EncodeToString(dnList[i].node.ID[:]),
			Address: dnList[i].node.Address,
		}
	}
	return result
}

// Remove evicts a dead or unreachable peer address from the DHT routing table [v1.0.10]
func (rt *RoutingTable) Remove(address string) {
	if address == "" || rt == nil {
		return
	}
	h := sha256.Sum256([]byte(address))
	var nodeID [32]byte
	copy(nodeID[:], h[:])

	cpl := CommonPrefixLen(rt.LocalNode.ID, nodeID)
	if cpl >= KademliaIDBits {
		cpl = KademliaIDBits - 1
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	bucket := rt.Buckets[cpl]
	for i, n := range bucket {
		if n.Address == address {
			rt.Buckets[cpl] = append(bucket[:i], bucket[i+1:]...)
			return
		}
	}
}

// PruneStale removes all nodes that have not been seen for >= maxAge [v1.0.10]
func (rt *RoutingTable) PruneStale(maxAge time.Duration) int {
	if rt == nil || maxAge <= 0 {
		return 0
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	pruned := 0
	for cpl := 0; cpl < KademliaIDBits; cpl++ {
		var active []DHTNode
		for _, n := range rt.Buckets[cpl] {
			if !n.LastSeen.IsZero() && now.Sub(n.LastSeen) >= maxAge {
				pruned++
				continue
			}
			active = append(active, n)
		}
		rt.Buckets[cpl] = active
	}
	return pruned
}

// GetAllAddresses returns all active peer addresses in the routing table
func (rt *RoutingTable) GetAllAddresses() []string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var addrs []string
	for _, b := range rt.Buckets {
		for _, n := range b {
			addrs = append(addrs, n.Address)
		}
	}
	return addrs
}

// GenerateRandomTarget generates a random 32-byte target for neighborhood crawling
func GenerateRandomTarget() [32]byte {
	var target [32]byte
	_, _ = rand.Read(target[:])
	return target
}
