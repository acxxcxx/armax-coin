package node

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// NetworkID returns the active network profile's NetworkID dynamically [L1-2]
func NetworkID() string {
	return core.NetworkID()
}

// Message represents a P2P broadcast network message structure with protocol magic
type Message struct {
	NetworkID string      `json:"network_id"`
	Type      string      `json:"type"` // "NEW_BLOCK", "REQUEST_CHAIN", "CHAIN_RESPONSE", "NEW_TX", "GET_PEERS", "PEERS_LIST", "GET_HEIGHT", "HEIGHT_RESPONSE", "GET_BLOCK_RANGE", "BLOCK_RANGE_RESPONSE"
	Payload   interface{} `json:"payload"`
}

// BlockRangeRequest represents a query for a range of blocks [FromIndex, ToIndex]
type BlockRangeRequest struct {
	FromIndex int `json:"from_index"`
	ToIndex   int `json:"to_index"`
}

// BlockRangeResponse carries a chunk of requested blocks
type BlockRangeResponse struct {
	FromIndex int          `json:"from_index"`
	ToIndex   int          `json:"to_index"`
	Blocks    []core.Block `json:"blocks"`
}

// HeightResponse carries the peer's current height, tip hash, and cumulative chainwork
type HeightResponse struct {
	Height     int    `json:"height"`
	LatestHash string `json:"latest_hash"`
	Chainwork  string `json:"chainwork"`
}

// ForkpointRequest contains block locator hashes sent by a syncing node [N-14]
type ForkpointRequest struct {
	Locators []string `json:"locators"`
}

// ForkpointResponse contains the highest common ancestor block between peers [N-14]
type ForkpointResponse struct {
	AncestorHeight int    `json:"ancestor_height"`
	AncestorHash   string `json:"ancestor_hash"`
}

var (
	peerServersMu sync.RWMutex
	peerServers   = make(map[string]*PeerServer)
)

// RegisterPeerServer registers a finalized PeerServer in the network-scoped registry [P1-A].
func RegisterPeerServer(ps *PeerServer) {
	if ps == nil {
		return
	}
	peerServersMu.Lock()
	defer peerServersMu.Unlock()

	// Clean up any stale entries associated with this instance
	for k, v := range peerServers {
		if v == ps {
			delete(peerServers, k)
		}
	}
	netID := ps.GetNetworkID()
	if netID != "" {
		peerServers[netID] = ps
	}
}

// UnregisterPeerServer removes a PeerServer from the network-scoped registry [P1-A].
func UnregisterPeerServer(ps *PeerServer) {
	if ps == nil {
		return
	}
	peerServersMu.Lock()
	defer peerServersMu.Unlock()
	for k, v := range peerServers {
		if v == ps {
			delete(peerServers, k)
		}
	}
}

// ClearPeerServers clears all registered PeerServers (for test cleanup) [P1-A].
func ClearPeerServers() {
	peerServersMu.Lock()
	defer peerServersMu.Unlock()
	peerServers = make(map[string]*PeerServer)
}

// GetPeerServerForNetwork returns the registered PeerServer for a specific network ID, or nil [P1-A].
func GetPeerServerForNetwork(netID string) *PeerServer {
	peerServersMu.RLock()
	defer peerServersMu.RUnlock()
	return peerServers[netID]
}

// SetActivePeerServer registers the active PeerServer instance for global P2P message routing (backward compatibility).
func SetActivePeerServer(ps *PeerServer) {
	RegisterPeerServer(ps)
}

// GetActivePeerServer returns the registered PeerServer for the active network, or nil (backward compatibility).
func GetActivePeerServer() *PeerServer {
	peerServersMu.RLock()
	defer peerServersMu.RUnlock()
	netID := core.GetActiveNetwork().NetworkID
	if ps, ok := peerServers[netID]; ok {
		return ps
	}
	if len(peerServers) == 1 {
		for _, ps := range peerServers {
			return ps
		}
	}
	return nil
}

// resolvePeerServer routes P2P traffic to the appropriate PeerServer for a network ID.
// It strictly enforces network-scoped isolation, rejects missing or ambiguous network IDs,
// and forbids raw-dial fallback whenever TOFU or strict TLS is required [P1-A].
func resolvePeerServer(netID string) (*PeerServer, error) {
	peerServersMu.RLock()
	defer peerServersMu.RUnlock()

	if netID == "" {
		if len(peerServers) == 1 {
			for _, ps := range peerServers {
				return ps, nil
			}
		}
		if len(peerServers) > 1 {
			return nil, errors.New("cannot route P2P traffic: network ID is missing or ambiguous in multi-server registry (unauthenticated dial refused)")
		}
		netID = core.GetActiveNetwork().NetworkID
	}
	if netID == "" {
		return nil, errors.New("cannot route P2P traffic: network ID is missing or ambiguous (unauthenticated dial refused)")
	}

	if ps, ok := peerServers[netID]; ok && ps != nil {
		return ps, nil
	}

	// Network ID not found in registry.
	// If any registered server has TOFU or RequireTLS enabled, or if strict TLS is globally enforced,
	// raw unauthenticated dialing is strictly forbidden to prevent bypassing security policies.
	for _, ps := range peerServers {
		if ps.P2PTOFU || ps.RequireTLS {
			return nil, fmt.Errorf("cannot route P2P traffic for network %q: no server registered and active servers enforce strict TLS/TOFU (unauthenticated dial refused)", netID)
		}
	}
	if IsStrictTLS() {
		return nil, fmt.Errorf("cannot route P2P traffic for network %q: strict TLS is enforced globally (unauthenticated dial refused)", netID)
	}

	// In a multi-server environment where other networks are registered,
	// routing to an unregistered network is ambiguous and forbidden.
	if len(peerServers) > 0 {
		return nil, fmt.Errorf("cannot route P2P traffic for network %q: unregistered network in multi-server registry (unauthenticated dial refused)", netID)
	}

	// 0 servers registered and no strict security configured
	return nil, nil
}

// QueryP2P sends a message to a peer and synchronously waits for and decodes the response.
// Routes through active PeerServer if available to strictly enforce TOFU pinning and TLS policies [F1, P1-A].
func QueryP2P(peerAddress string, msg Message, timeout time.Duration) (*Message, error) {
	ps, err := resolvePeerServer(msg.NetworkID)
	if err != nil {
		return nil, err
	}
	if ps != nil {
		return ps.QueryPeer(peerAddress, msg, timeout)
	}

	expectedNetID := msg.NetworkID
	if expectedNetID == "" {
		expectedNetID = core.GetActiveNetwork().NetworkID
		msg.NetworkID = expectedNetID
	}
	if expectedNetID == "" {
		return nil, errors.New("cannot route P2P query: network ID is missing or ambiguous")
	}
	msgData, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	conn, _, err := DialEncryptedWithFallback(peerAddress, timeout, !IsStrictTLS())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(append(msgData, '\n')); err != nil {
		return nil, err
	}

	limitedReader := io.LimitReader(conn, 5*1024*1024)
	reader := bufio.NewReader(limitedReader)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	var resp Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		return nil, err
	}

	if resp.NetworkID != expectedNetID {
		return nil, fmt.Errorf("network ID mismatch: expected %s, got %s", expectedNetID, resp.NetworkID)
	}

	return &resp, nil
}

// SendP2PMessage sends a message to a specific peer with protocol network ID.
// Routes through active PeerServer if available to strictly enforce TOFU pinning and TLS policies [F1, P1-A].
func SendP2PMessage(peerAddress string, msg Message) error {
	ps, err := resolvePeerServer(msg.NetworkID)
	if err != nil {
		return err
	}
	if ps != nil {
		return ps.SendMessage(peerAddress, msg)
	}

	if msg.NetworkID == "" {
		msg.NetworkID = core.GetActiveNetwork().NetworkID
	}
	if msg.NetworkID == "" {
		return errors.New("cannot send P2P message: network ID is missing or ambiguous")
	}

	conn, _, err := DialEncryptedWithFallback(peerAddress, 4*time.Second, !IsStrictTLS())
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(msgData, '\n'))
	return err
}

// BroadcastBlock broadcasts a newly mined block to all active peers
func BroadcastBlock(peerAddresses []string, block core.Block) {
	netID := core.GetActiveNetwork().NetworkID
	if ps, _ := resolvePeerServer(netID); ps != nil {
		ps.BroadcastBlock(peerAddresses, block)
		return
	}

	msg := Message{
		NetworkID: NetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   block,
	}

	for _, peer := range peerAddresses {
		go func(target string) {
			if err := SendP2PMessage(target, msg); err == nil {
				fmt.Printf("[P2P] Broadcasted block #%d to peer %s\n", block.Index, target)
			}
		}(peer)
	}
}

// BroadcastTransaction broadcasts a new transaction across the P2P network
func BroadcastTransaction(peerAddresses []string, tx core.Transaction) {
	netID := core.GetActiveNetwork().NetworkID
	if ps, _ := resolvePeerServer(netID); ps != nil {
		ps.BroadcastTransaction(peerAddresses, tx)
		return
	}

	msg := Message{
		NetworkID: NetworkID(),
		Type:      "NEW_TX",
		Payload:   tx,
	}

	for _, peer := range peerAddresses {
		go func(target string) {
			_ = SendP2PMessage(target, msg)
		}(peer)
	}
}
