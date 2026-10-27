package node

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

const (
	MaxPeersDefault            = 50
	MaxAddressBookSize         = 1000              // Maximum addresses stored in address book [N-16]
	MaxPeersPerResponse        = 250               // Maximum peers returned in PEERS_LIST [N-16]
	MaxPeerFailures            = 3                 // Number of consecutive failures before evicting dead peer [N-16]
	MaxSyncConcurrency         = 8                 // Max parallel workers for SyncBestPeer [N-16]
	MaxPeersPerSubnet          = 3                 // Maximum concurrent peers/connections allowed per /24 subnet (Sybil/Eclipse defense)
	MaxIncomingPacketSize      = 4 * 1024 * 1024   // 4 Megabytes max per P2P packet [P2-4]
	MaxBlockRangeResponseBytes = core.MaxBlockSize // >= MaxBlockSize (2,000,000 B), ensures always returning at least 1 block [P2-4]
	SocketTimeoutDuration      = 20 * time.Second
	PeersFile                  = "peers.json"
	SyncChunkSize              = 250              // Maximum blocks per GET_BLOCK_RANGE request
	MaxOrphanBlocks            = 100              // Maximum orphan blocks in buffer [N-17]
	MaxOrphanBlocksPerPeer     = 10               // Maximum orphan blocks allowed per peer [N-17]
	MaxOrphanTxs               = 500              // Maximum pending out-of-order transactions [N-17]
	OrphanTxExpiry             = 10 * time.Minute // Out-of-order tx expiration window [N-17]
)

// extractSubnet derives the /24 IPv4 or /48 IPv6 subnet prefix for Sybil/Eclipse attack mitigation
func extractSubnet(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if ip.IsLoopback() {
		return "127.0.0.1"
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.Mask(net.CIDRMask(24, 32)).String() // /24 subnet (e.g. 192.168.1.0)
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() // /48 IPv6 prefix
}

// extractHost extracts only the IP / host part without port
func extractHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// parseNetworkID parses and validates a network identifier or alias against exact supported profiles [P1-A]
func parseNetworkID(idOrName string) (canonicalNetID string, subDir string, err error) {
	switch strings.ToUpper(strings.TrimSpace(idOrName)) {
	case "ARMX-MAINNET-1", "MAINNET":
		return "ARMX-MAINNET-1", "", nil
	case "ARMX-TESTNET-1", "TESTNET":
		return "ARMX-TESTNET-1", "testnet", nil
	case "ARMX-REGTEST-1", "REGTEST":
		return "ARMX-REGTEST-1", "regtest", nil
	case "ARMX-SOAK-1", "SOAK", "TEST":
		return "ARMX-SOAK-1", "soak", nil
	default:
		return "", "", fmt.Errorf("unsupported or unknown network ID: %q (supported: mainnet, testnet, regtest, soak)", idOrName)
	}
}

// networkSubDir returns the subdirectory name for a given network ID, or error if invalid [P1-A]
func networkSubDir(netID string) (string, error) {
	_, subDir, err := parseNetworkID(netID)
	return subDir, err
}

// isPrivatePeersAllowedForNetwork determines if private/loopback peers are allowed based solely on network identity and explicit environment variable [N-05, N-16, P1-A]
func isPrivatePeersAllowedForNetwork(idOrName string) bool {
	if os.Getenv("ARMX_ALLOW_PRIVATE_PEERS") == "1" {
		return true
	}
	canon, _, err := parseNetworkID(idOrName)
	if err != nil {
		return false
	}
	switch canon {
	case "ARMX-TESTNET-1", "ARMX-REGTEST-1", "ARMX-SOAK-1":
		return true
	default:
		return false
	}
}

// derivePeerStorePath computes an isolated, immutable peer store filesystem path for a given base data directory and network ID [P1-A]
func derivePeerStorePath(baseDataDir string, networkID string) (string, error) {
	canonicalNetID, subDir, err := parseNetworkID(networkID)
	if err != nil {
		return "", err
	}

	baseDir := baseDataDir
	if baseDir == "" {
		baseDir = os.Getenv("ARMX_DATA_DIR")
	}

	var targetDir string
	if baseDir != "" {
		cleanBase := filepath.Clean(baseDir)
		if subDir != "" {
			if strings.EqualFold(filepath.Base(cleanBase), subDir) {
				targetDir = cleanBase
			} else {
				targetDir = filepath.Join(cleanBase, subDir)
			}
		} else {
			targetDir = cleanBase
		}
	} else {
		// No data dir specified: check executable dir or local dir
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			if subDir != "" {
				execDir = filepath.Join(execDir, subDir)
			}
			candidate := filepath.Join(execDir, PeersFile)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		if subDir != "" {
			targetDir = subDir
		} else {
			targetDir = "."
		}
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create peer store directory %s: %w", targetDir, err)
	}

	targetPath := filepath.Join(targetDir, PeersFile)

	// Perform backward-compatible migration for legacy peer stores if needed
	if err := migrateLegacyPeerStore(targetPath, canonicalNetID, subDir); err != nil {
		return "", err
	}

	return targetPath, nil
}

var (
	migrateLegacyWriteFileHook func(filename string, data []byte, perm os.FileMode) error
)

// migrateLegacyPeerStore handles backward compatibility for existing mainnet and testnet peer stores [P1-A]
func migrateLegacyPeerStore(targetPath string, canonicalNetID string, subDir string) error {
	if fi, err := os.Stat(targetPath); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("peer store path %s is a directory, not a regular file", targetPath)
		}
		return nil // Target file already exists, no migration needed
	}

	writeFn := os.WriteFile
	if migrateLegacyWriteFileHook != nil {
		writeFn = migrateLegacyWriteFileHook
	}

	targetDir := filepath.Dir(targetPath)

	// Migration 1: Mainnet legacy path <dataDir>/mainnet/peers.json -> <dataDir>/peers.json
	if subDir == "" {
		legacyPath := filepath.Join(targetDir, "mainnet", PeersFile)
		if data, err := os.ReadFile(legacyPath); err == nil {
			var store VersionedPeerStore
			if err := json.Unmarshal(data, &store); err != nil {
				return fmt.Errorf("corrupt legacy peer store found at %s: %w", legacyPath, err)
			}
			if store.Version != 1 {
				return fmt.Errorf("unsupported legacy peer store version %d in %s", store.Version, legacyPath)
			}
			if store.NetworkID != "" && store.NetworkID != "ARMX-MAINNET-1" {
				return fmt.Errorf("legacy peer store network ID mismatch at %s: store has %q, expected %q", legacyPath, store.NetworkID, "ARMX-MAINNET-1")
			}
			if err := writeFn(targetPath, data, 0600); err != nil {
				return fmt.Errorf("failed to write migrated peer store to %s: %w", targetPath, err)
			}
			return nil
		}
	}

	// Migration 2: Non-mainnet (testnet/regtest/soak) where older version might have stored in <parent>/peers.json
	if subDir != "" {
		parentDir := filepath.Dir(targetDir)
		legacyPath := filepath.Join(parentDir, PeersFile)
		if data, err := os.ReadFile(legacyPath); err == nil {
			var store VersionedPeerStore
			if err := json.Unmarshal(data, &store); err != nil {
				return fmt.Errorf("corrupt legacy peer store found at %s: %w", legacyPath, err)
			}
			if store.Version != 1 {
				return fmt.Errorf("unsupported legacy peer store version %d in %s", store.Version, legacyPath)
			}
			if store.NetworkID == canonicalNetID {
				if err := writeFn(targetPath, data, 0600); err != nil {
					return fmt.Errorf("failed to write migrated peer store to %s: %w", targetPath, err)
				}
				return nil
			}
			// If parent store belongs to another network (e.g. mainnet store in shared root), it's not a legacy store for this sub-network
			if store.NetworkID != "ARMX-MAINNET-1" {
				return fmt.Errorf("legacy peer store network ID mismatch at %s: store has %q, expected %q", legacyPath, store.NetworkID, canonicalNetID)
			}
		}
	}
	return nil
}

func getPeersPath() string {
	path, _ := derivePeerStorePath("", core.NetworkID())
	return path
}

// PeerInfo holds address book metadata for a peer node [N-16]
type PeerInfo struct {
	Address         string    `json:"address"`
	LastSeen        time.Time `json:"last_seen"`
	Failures        int       `json:"failures"`
	Height          int       `json:"height"`
	NetworkID       string    `json:"network_id"`
	CertFingerprint string    `json:"cert_fingerprint,omitempty"` // [F1] SHA-256 certificate pubkey fingerprint for TOFU pinning
}

// ServerLifecycleState defines the operational lifecycle states of PeerServer.
type ServerLifecycleState int

const (
	// ServerStateStopped indicates the server is offline; configuration setters are allowed.
	ServerStateStopped ServerLifecycleState = iota
	// ServerStateStarting indicates the server is binding its listener and initializing.
	ServerStateStarting
	// ServerStateRunning indicates the server listener and loops are actively running.
	ServerStateRunning
	// ServerStateStopping indicates the server is actively tearing down resources.
	ServerStateStopping
)

func (s ServerLifecycleState) String() string {
	switch s {
	case ServerStateStopped:
		return "stopped"
	case ServerStateStarting:
		return "starting"
	case ServerStateRunning:
		return "running"
	case ServerStateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// serverCycle tracks the ownership, cancellation, workers, and active sockets for one server lifecycle generation [P1-A Hardening].
type serverCycle struct {
	id          uint64
	quitChan    chan struct{}
	doneChan    chan struct{}
	wg          sync.WaitGroup
	connsMu     sync.Mutex
	activeConns map[net.Conn]struct{}
	cancelOnce  sync.Once
	doneOnce    sync.Once
}

type PeerServer struct {
	Port                 string
	netIDVal             atomic.Pointer[string] // Single synchronized source of truth for runtime queries [C-33]
	DataDir              string                 // Custom instance data directory for isolated peer storage [P1-A]
	peerStorePath        string                 // [P1-A] Resolved immutable peer store path
	peerStoreInitErr     error                  // [P1-A] Peer store directory creation / path error
	Peers                []string
	newPeers             map[string]*PeerInfo // [N-16] Untried peer candidates
	triedPeers           map[string]*PeerInfo // [N-16] Handshake verified peers
	Lock                 sync.RWMutex
	Blockchain           *core.Blockchain
	MaxPeers             int
	ActiveConns          int32
	connLock             sync.Mutex
	subnetConns          map[string]int
	DHT                  *RoutingTable // Kademlia DHT routing table for decentralized peer discovery
	seenLock             sync.Mutex
	seenTxs              map[string]time.Time
	seenBlks             map[string]time.Time
	negativeBlks         map[string]time.Time // [T2] Bounded negative cache of invalid block digests
	syncCooldowns        map[string]time.Time // [T3] Peer IP -> last sync trigger time
	relayLock            sync.Mutex
	relayedTxs           map[string]map[string]time.Time
	syncMu               sync.Mutex
	syncingHosts         map[string]bool
	seedMu               sync.RWMutex
	seedPeers            []string             // configured seeds (host:port or DNS name), re-dialed after isolation
	seedAddrs            map[string]struct{}  // resolved seed addresses, never evicted from the peer store
	expectedChains       map[string]time.Time // [N-04] tracks requested chains to reject unsolicited CHAIN_RESPONSE
	orphanLock           sync.Mutex
	orphanBlocks         map[string]*orphanBlockEntry // [N-06, N-17] orphan block buffer keyed by Block.Hash
	orphanTxs            map[string]*orphanTxEntry    // [N-17] out-of-order pending transactions pool
	lastOrphanScan       time.Time                    // [T5] Timestamp of last top-level orphan tx scan
	DisableSubnetLimit   bool                         // [N-12] Allow multiple connections from same subnet (LAN/testnet)
	AllowPrivatePeers    bool                         // [N-05, N-16] Allow loopback and private IP addresses (LAN/testnet)
	RequireTLS           bool                         // [N-08] Require strict TLS 1.3
	P2PTOFU              bool                         // [F1] Enable Trust-On-First-Use (TOFU) TLS certificate fingerprint pinning
	tlsConfig            *tls.Config                  // Instance TLS config for concurrent safety
	peerStoreCorrupted   bool                         // [Phase 5, Follow-up Point 4] Fail-closed flag when peer store file is corrupt
	savePeersLock        sync.Mutex                   // [P1 Final Blocker] Serializes peer-store snapshot -> marshal -> write -> sync -> rename transactions
	tofuLock             sync.Mutex                   // [P1 Final Fix] Serializes first-contact TOFU pinning per peer address
	pendingTOFU          map[string]*pendingPin       // [P1 Final Fix] In-flight first-contact persistence attempts per peer
	failedPins           map[string]string            // [P1 Final Fix] peerAddr -> cert fingerprint that failed persistence; prevents silent repinning
	banLock              sync.RWMutex                 // [N-15] Ban and rate limit mutex
	banScores            map[string]int               // [N-15] Misbehavior ban scores per IP
	bannedIPs            map[string]time.Time         // [N-15] Active IP bans and expiry
	rateTrackers         map[string]*peerRateTracker  // [N-15] Token bucket and endpoint trackers
	BlockRangeBurst      float64                      // Configurable burst for GET_BLOCK_RANGE (token-bucket limiter; default 120.0)
	BlockRangeRefillRate float64                      // Configurable refill rate/sec for GET_BLOCK_RANGE (default 20.0)
	listener             net.Listener                 // [T-06] Active TCP listener
	quitChan             chan struct{}                // [T-06] Shutdown signal channel
	stopLock             sync.Mutex
	outboundMu           sync.Mutex
	outboundSessions     map[string]*outboundPeerSession
	inflightDials        map[string]*inflightPeerDial
	reachabilityMu       sync.Mutex
	unreachablePeers     map[string]time.Time
	probingPeers         map[string]bool
	announcedPeers       map[string]time.Time

	// lifecycleLock protects against store path/network transitions racing with active TOFU operations [P1-A Lifecycle Race Fix].
	//
	// Documented Lock Hierarchy (must strictly be acquired in this order to prevent deadlocks):
	//   Level 1: lifecycleLock (sync.RWMutex) - top-level lifecycle / store migration lock (read for TOFU, DialPeer, AddPeer, RecordPeerFailure, RecordPeerSuccess operations; write for SetDataDir/SetNetworkID/state transitions)
	//   Level 2: savePeersLock (sync.Mutex)   - serializes disk persistence transactions
	//   Level 3: tofuLock      (sync.Mutex)   - serializes in-memory pending TOFU registration
	//   Level 4: Lock          (sync.RWMutex) - protects in-memory peer maps and fields
	//   Level 5: stopLock      (sync.Mutex)   - listener shutdown synchronization
	lifecycleLock sync.RWMutex
	state         ServerLifecycleState
	serverStarted bool          // Preserved for backwards compatibility, mirrors state == ServerStateRunning
	serverDone    chan struct{} // Closed when StartServer accept loop and teardown fully exit
	currentCycle  uint64
	activeCycle   *serverCycle
}

type pendingPin struct {
	fp   string
	done chan struct{}
	err  error
}

type orphanBlockEntry struct {
	Block   core.Block
	PeerIP  string
	AddedAt time.Time
}

type orphanTxEntry struct {
	Tx      core.Transaction
	PeerIP  string
	AddedAt time.Time
}

type peerRateTracker struct {
	tokens               float64
	lastRefill           time.Time
	blockTokens          float64
	lastBlockRefill      time.Time
	blockRangeTokens     float64
	lastBlockRangeRefill time.Time
	forkpointTokens      float64
	lastForkpointRefill  time.Time
}

const (
	MaxSessionRespBytes      = 16 * 1024 * 1024
	AuthenticatedIdleTimeout = 90 * time.Second
	UnreachablePeerCooldown  = 30 * time.Minute
	EvictedPeerCooldown      = 15 * time.Minute
	SelfAnnounceInterval     = 10 * time.Minute
	InboundProbeTimeout      = 3 * time.Second
)

var errSessionClosed = errors.New("P2P outbound session was closed before write")

func isP2PDebugEnabled() bool {
	return os.Getenv("ARMX_P2P_DEBUG") == "1" || core.IsDebugEnabled(core.LogCategoryNet)
}

func debugLogf(format string, args ...any) {
	if isP2PDebugEnabled() {
		core.LogDebug(core.LogCategoryNet, format, args...)
	}
}

func isBenignDisconnectErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "closed network connection") ||
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "eof")
}

type inflightPeerDial struct {
	done    chan struct{}
	session *outboundPeerSession
	err     error
}

type outboundPeerSession struct {
	peerAddress string
	conn        net.Conn
	reader      *bufio.Reader
	cycle       *serverCycle
	release     func()

	// Serializes request-response exchanges on this connection.
	mu        sync.Mutex
	closed    atomic.Bool
	lastUsed  atomic.Int64
	closeOnce sync.Once
}

func (s *outboundPeerSession) close() {
	if s == nil {
		return
	}

	s.closed.Store(true)
	s.closeOnce.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		if s.cycle != nil {
			s.cycle.connsMu.Lock()
			if s.cycle.activeConns != nil {
				delete(s.cycle.activeConns, s.conn)
			}
			s.cycle.connsMu.Unlock()
		}
		if s.release != nil {
			s.release()
		}
	})
}

func (s *outboundPeerSession) isClosed() bool {
	if s == nil {
		return true
	}
	return s.closed.Load()
}

func (s *outboundPeerSession) touch() {
	if s != nil {
		s.lastUsed.Store(time.Now().UnixNano())
	}
}

func (s *outboundPeerSession) lastActivity() time.Time {
	if s == nil {
		return time.Time{}
	}
	ns := s.lastUsed.Load()
	if ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (p *PeerServer) isShuttingDown() bool {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()
	if p.state == ServerStateStopping {
		return true
	}
	if p.activeCycle != nil {
		select {
		case <-p.activeCycle.quitChan:
			return true
		default:
		}
	}
	return false
}

func isOwnPublicInterfaceIP(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || !ip.IsGlobalUnicast() {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		var ifaceIP net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ifaceIP = v.IP
		case *net.IPAddr:
			ifaceIP = v.IP
		}
		if ifaceIP != nil && ifaceIP.Equal(ip) {
			return true
		}
	}
	return false
}

func (p *PeerServer) isSelfPeerAddress(peerAddress string) bool {
	if peerAddress == "" || p.Port == "" {
		return false
	}
	host, portStr, err := net.SplitHostPort(peerAddress)
	if err != nil || portStr != p.Port {
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	if isOwnPublicInterfaceIP(host) {
		return true
	}
	return false
}

func isSamePeerOrHost(peerAddr, senderAddr string) bool {
	if peerAddr == "" || senderAddr == "" {
		return false
	}
	if peerAddr == senderAddr {
		return true
	}
	peerHost := extractHost(peerAddr)
	senderHost := extractHost(senderAddr)
	if peerHost == "" || senderHost == "" || peerHost != senderHost {
		return false
	}
	if peerHost == "127.0.0.1" || peerHost == "::1" || strings.EqualFold(peerHost, "localhost") {
		_, senderPort, err := net.SplitHostPort(senderAddr)
		if err == nil && senderPort != "" {
			return false
		}
	}
	return true
}

func (p *PeerServer) markPeerUnreachable(addr string, ttl time.Duration) {
	if addr == "" {
		return
	}
	p.reachabilityMu.Lock()
	defer p.reachabilityMu.Unlock()
	if p.unreachablePeers == nil {
		p.unreachablePeers = make(map[string]time.Time)
	}
	now := time.Now()
	if len(p.unreachablePeers) > MaxAddressBookSize {
		for k, exp := range p.unreachablePeers {
			if now.After(exp) {
				delete(p.unreachablePeers, k)
			}
		}
	}
	p.unreachablePeers[addr] = now.Add(ttl)
}

func (p *PeerServer) clearPeerUnreachable(addr string) {
	if addr == "" {
		return
	}
	p.reachabilityMu.Lock()
	defer p.reachabilityMu.Unlock()
	if p.unreachablePeers != nil {
		delete(p.unreachablePeers, addr)
	}
}

func (p *PeerServer) clearAllPeerUnreachable() {
	p.reachabilityMu.Lock()
	defer p.reachabilityMu.Unlock()
	p.unreachablePeers = nil
}

// SetSeedPeers records the configured seed list so the node can re-dial it after
// losing connectivity. Seeds are protected from eviction and are re-added on demand.
func (p *PeerServer) SetSeedPeers(seeds []string) {
	p.seedMu.Lock()
	p.seedPeers = append([]string(nil), seeds...)
	p.seedMu.Unlock()
	p.ReseedPeers()
}

func (p *PeerServer) isSeedPeer(addr string) bool {
	p.seedMu.RLock()
	defer p.seedMu.RUnlock()
	_, ok := p.seedAddrs[addr]
	return ok
}

// ReseedPeers (re)resolves the configured seeds and puts them back in the address book,
// clearing any unreachable cooldown on them.
func (p *PeerServer) ReseedPeers() {
	p.seedMu.RLock()
	seeds := append([]string(nil), p.seedPeers...)
	p.seedMu.RUnlock()
	if len(seeds) == 0 {
		return
	}
	for _, seed := range seeds {
		if p.isShuttingDown() {
			return
		}
		for _, addr := range ResolveDNSSeed(seed) {
			p.seedMu.Lock()
			if p.seedAddrs == nil {
				p.seedAddrs = make(map[string]struct{})
			}
			p.seedAddrs[addr] = struct{}{}
			p.seedMu.Unlock()
			p.clearPeerUnreachable(addr)
			_ = p.AddPeer(addr)
		}
	}
}

func (p *PeerServer) isPeerUnreachable(addr string) bool {
	if addr == "" {
		return false
	}
	p.reachabilityMu.Lock()
	defer p.reachabilityMu.Unlock()
	if p.unreachablePeers == nil {
		return false
	}
	exp, ok := p.unreachablePeers[addr]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(p.unreachablePeers, addr)
		return false
	}
	return true
}

func (p *PeerServer) shouldSelfAnnounceTo(target string) bool {
	if target == "" || p.Port == "" {
		return false
	}
	p.reachabilityMu.Lock()
	defer p.reachabilityMu.Unlock()
	if p.announcedPeers == nil {
		p.announcedPeers = make(map[string]time.Time)
	}
	now := time.Now()
	if last, ok := p.announcedPeers[target]; ok && now.Sub(last) < SelfAnnounceInterval {
		return false
	}
	p.announcedPeers[target] = now
	return true
}

// verifyAndAddInboundPeer asynchronously checks if a self-announced peer's P2P port is reachable
// from the outside before adding it to the address book and DHT. If unreachable (behind NAT/firewall),
// it caches the address in unreachablePeers so the node never wastes connections or spams logs.
func (p *PeerServer) verifyAndAddInboundPeer(candidateAddr string) {
	if candidateAddr == "" || p.isShuttingDown() || p.isSelfPeerAddress(candidateAddr) || p.HasPeer(candidateAddr) || p.isPeerUnreachable(candidateAddr) {
		return
	}

	host, portStr, err := net.SplitHostPort(candidateAddr)
	if err != nil || host == "" || portStr == "" {
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return
	}
	if ip := net.ParseIP(host); ip != nil {
		p.Lock.RLock()
		allowPrivate := p.AllowPrivatePeers
		p.Lock.RUnlock()
		isPrivateOrLoopback := ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
		if isPrivateOrLoopback && !allowPrivate {
			return
		}
	}

	p.reachabilityMu.Lock()
	if p.probingPeers == nil {
		p.probingPeers = make(map[string]bool)
	}
	if p.probingPeers[candidateAddr] || len(p.probingPeers) >= MaxSyncConcurrency {
		p.reachabilityMu.Unlock()
		return
	}
	p.probingPeers[candidateAddr] = true
	p.reachabilityMu.Unlock()

	go func() {
		defer func() {
			p.reachabilityMu.Lock()
			delete(p.probingPeers, candidateAddr)
			p.reachabilityMu.Unlock()
		}()

		if p.isShuttingDown() {
			return
		}

		resp, err := p.QueryPeer(candidateAddr, Message{
			NetworkID: p.GetNetworkID(),
			Type:      "GET_HEIGHT",
		}, InboundProbeTimeout)
		if err != nil || resp == nil || resp.Type != "HEIGHT_RESPONSE" {
			// Peer port is closed / behind NAT / unreachable; cache negative result to prevent retry spam.
			p.markPeerUnreachable(candidateAddr, UnreachablePeerCooldown)
			if sess := p.getExistingOutboundSession(candidateAddr); sess != nil {
				p.dropOutboundSession(candidateAddr, sess)
			}
			core.LogDebug(core.LogCategoryDiscovery, "Inbound peer %s is not reachable on P2P port (NAT/firewall); cooldown %v", candidateAddr, UnreachablePeerCooldown)
			return
		}

		var hr HeightResponse
		b, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(b, &hr)

		p.clearPeerUnreachable(candidateAddr)
		if err := p.AddPeer(candidateAddr); err == nil {
			_ = p.RecordPeerSuccess(candidateAddr, hr.Height, resp.NetworkID)
			p.DHT.Update(candidateAddr)
			core.LogInfo(core.LogCategoryDiscovery, "Verified reachable public peer %s (height #%d) and added as relay node", candidateAddr, hr.Height)
		}
	}()
}

func (p *PeerServer) registerOutboundConnSession(
	peerAddress string,
	conn net.Conn,
	release func(),
) (*outboundPeerSession, error) {
	// Acquire lifecycleLock (Level 1) BEFORE outboundMu to strictly respect the lock hierarchy.
	p.lifecycleLock.RLock()
	if p.state == ServerStateStopping {
		p.lifecycleLock.RUnlock()
		_ = conn.Close()
		if release != nil {
			release()
		}
		return nil, errors.New("P2P server is stopping")
	}
	cycle := p.activeCycle
	if cycle != nil {
		cycle.connsMu.Lock()
		select {
		case <-cycle.quitChan:
			cycle.connsMu.Unlock()
			p.lifecycleLock.RUnlock()
			_ = conn.Close()
			if release != nil {
				release()
			}
			return nil, errors.New("P2P server cycle is stopping")
		default:
			cycle.activeConns[conn] = struct{}{}
		}
		cycle.connsMu.Unlock()
	}
	p.lifecycleLock.RUnlock()

	session := &outboundPeerSession{
		peerAddress: peerAddress,
		conn:        conn,
		reader:      bufio.NewReader(conn),
		cycle:       cycle,
		release:     release,
	}
	session.touch()

	var oldSession *outboundPeerSession
	p.outboundMu.Lock()
	if p.outboundSessions == nil {
		p.outboundSessions = make(map[string]*outboundPeerSession)
	}
	if existing := p.outboundSessions[peerAddress]; existing != nil && existing != session {
		oldSession = existing
	}
	p.outboundSessions[peerAddress] = session
	p.outboundMu.Unlock()

	if oldSession != nil {
		oldSession.close()
		if oldSession.cycle != nil {
			oldSession.cycle.connsMu.Lock()
			delete(oldSession.cycle.activeConns, oldSession.conn)
			oldSession.cycle.connsMu.Unlock()
		}
	}

	return session, nil
}

func (p *PeerServer) getOrCreateOutboundSessionWithStatus(
	peerAddress string,
	timeout time.Duration,
) (*outboundPeerSession, bool, error) {
	p.outboundMu.Lock()
	if p.outboundSessions == nil {
		p.outboundSessions = make(map[string]*outboundPeerSession)
	}
	if session := p.outboundSessions[peerAddress]; session != nil {
		if !session.isClosed() {
			p.outboundMu.Unlock()
			return session, true, nil
		}
		delete(p.outboundSessions, peerAddress)
	}

	if p.inflightDials == nil {
		p.inflightDials = make(map[string]*inflightPeerDial)
	}
	if inflight := p.inflightDials[peerAddress]; inflight != nil {
		done := inflight.done
		p.outboundMu.Unlock()
		<-done
		if inflight.err != nil {
			return nil, false, inflight.err
		}
		if inflight.session != nil && !inflight.session.isClosed() {
			return inflight.session, true, nil
		}
		return nil, false, errSessionClosed
	}

	inflight := &inflightPeerDial{
		done: make(chan struct{}),
	}
	p.inflightDials[peerAddress] = inflight
	p.outboundMu.Unlock()

	defer func() {
		p.outboundMu.Lock()
		if p.inflightDials[peerAddress] == inflight {
			delete(p.inflightDials, peerAddress)
		}
		close(inflight.done)
		p.outboundMu.Unlock()
	}()

	conn, release, err := p.DialPeer(peerAddress, timeout)
	if err != nil {
		inflight.err = err
		return nil, false, err
	}

	session, err := p.registerOutboundConnSession(peerAddress, conn, release)
	if err != nil {
		inflight.err = err
		return nil, false, err
	}

	inflight.session = session
	return session, false, nil
}

func (p *PeerServer) getOrCreateOutboundSession(
	peerAddress string,
	timeout time.Duration,
) (*outboundPeerSession, error) {
	session, _, err := p.getOrCreateOutboundSessionWithStatus(peerAddress, timeout)
	return session, err
}

func (p *PeerServer) dropOutboundSession(
	peerAddress string,
	session *outboundPeerSession,
) {
	if session == nil {
		return
	}

	p.outboundMu.Lock()
	if p.outboundSessions[peerAddress] == session {
		delete(p.outboundSessions, peerAddress)
	}
	p.outboundMu.Unlock()

	session.close()

	if session.cycle != nil {
		session.cycle.connsMu.Lock()
		delete(session.cycle.activeConns, session.conn)
		session.cycle.connsMu.Unlock()
	}
}

func (p *PeerServer) closeOutboundSessions(cycle *serverCycle) {
	p.outboundMu.Lock()

	var sessions []*outboundPeerSession

	for address, session := range p.outboundSessions {
		if cycle == nil || session.cycle == nil || session.cycle == cycle {
			delete(p.outboundSessions, address)
			sessions = append(sessions, session)
		}
	}

	p.outboundMu.Unlock()

	for _, session := range sessions {
		session.close()

		if session.cycle != nil {
			session.cycle.connsMu.Lock()
			delete(session.cycle.activeConns, session.conn)
			session.cycle.connsMu.Unlock()
		}
	}
}

func (p *PeerServer) outboundSessionAddresses(
	cycle *serverCycle,
) []string {
	p.outboundMu.Lock()
	defer p.outboundMu.Unlock()

	addresses := make([]string, 0, len(p.outboundSessions))

	for address, session := range p.outboundSessions {
		if (cycle == nil || session.cycle == nil || session.cycle == cycle) && !session.isClosed() {
			addresses = append(addresses, address)
		}
	}

	return addresses
}

func (s *outboundPeerSession) roundTrip(
	msgData []byte,
	timeout time.Duration,
) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.closed.Load() || s.conn == nil || s.reader == nil {
		return "", errSessionClosed
	}

	if err := s.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		s.close()
		return "", err
	}
	defer func() {
		if !s.closed.Load() && s.conn != nil {
			_ = s.conn.SetDeadline(time.Time{})
		}
	}()

	if _, err := s.conn.Write(append(msgData, '\n')); err != nil {
		s.close()
		return "", err
	}

	line, err := readBoundedLine(s.reader, MaxSessionRespBytes)
	if err != nil {
		s.close()
		return "", err
	}

	s.touch()
	return line, nil
}

func (s *outboundPeerSession) readNextLine(timeout time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.closed.Load() || s.conn == nil || s.reader == nil {
		return "", errSessionClosed
	}

	if err := s.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		s.close()
		return "", err
	}
	defer func() {
		if !s.closed.Load() && s.conn != nil {
			_ = s.conn.SetReadDeadline(time.Time{})
		}
	}()

	line, err := readBoundedLine(s.reader, MaxSessionRespBytes)
	if err != nil {
		s.close()
		return "", err
	}

	s.touch()
	return line, nil
}

func (s *outboundPeerSession) send(
	msgData []byte,
	timeout time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.closed.Load() || s.conn == nil {
		return errSessionClosed
	}

	if err := s.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		s.close()
		return err
	}
	defer func() {
		if !s.closed.Load() && s.conn != nil {
			_ = s.conn.SetWriteDeadline(time.Time{})
		}
	}()

	if _, err := s.conn.Write(append(msgData, '\n')); err != nil {
		s.close()
		return err
	}

	s.touch()
	return nil
}

// isTxSeen checks if a transaction has already been seen or queued
func (p *PeerServer) isTxSeen(txHash string) bool {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.seenTxs == nil {
		return false
	}
	_, exists := p.seenTxs[txHash]
	return exists
}

// markTxSeen records a transaction hash after it has been successfully verified or queued [N-17]
func (p *PeerServer) markTxSeen(txHash string) {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.seenTxs == nil {
		p.seenTxs = make(map[string]time.Time)
	}
	now := time.Now()
	if len(p.seenTxs) > 10000 {
		for h, t := range p.seenTxs {
			if now.Sub(t) > 10*time.Minute {
				delete(p.seenTxs, h)
			}
		}
	}
	p.seenTxs[txHash] = now
}

const txRelayRetryTTL = 10 * time.Minute

// claimTxRelay atomically claims a relay attempt for one transaction and peer.
func (p *PeerServer) claimTxRelay(
	txHash string,
	peer string,
) (time.Time, bool) {
	p.relayLock.Lock()
	defer p.relayLock.Unlock()

	if p.relayedTxs == nil {
		p.relayedTxs = make(map[string]map[string]time.Time)
	}

	now := time.Now()

	// Remove expired claims and empty transaction entries.
	for hash, claims := range p.relayedTxs {
		for addr, claimedAt := range claims {
			if now.Sub(claimedAt) >= txRelayRetryTTL {
				delete(claims, addr)
			}
		}

		if len(claims) == 0 {
			delete(p.relayedTxs, hash)
		}
	}

	claims := p.relayedTxs[txHash]
	if claims == nil {
		claims = make(map[string]time.Time)
		p.relayedTxs[txHash] = claims
	}

	if _, exists := claims[peer]; exists {
		return time.Time{}, false
	}

	claims[peer] = now
	return now, true
}

// releaseTxRelay releases only the claim owned by this attempt.
func (p *PeerServer) releaseTxRelay(
	txHash string,
	peer string,
	claimTime time.Time,
) {
	p.relayLock.Lock()
	defer p.relayLock.Unlock()

	claims := p.relayedTxs[txHash]
	if claims == nil {
		return
	}

	// Do not delete a newer claim created after this attempt expired.
	if current, exists := claims[peer]; !exists || !current.Equal(claimTime) {
		return
	}

	delete(claims, peer)

	if len(claims) == 0 {
		delete(p.relayedTxs, txHash)
	}
}

// isBlockSeen checks if a block has already been processed or stored without marking
func (p *PeerServer) isBlockSeen(blkHash string) bool {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.seenBlks == nil {
		return false
	}
	_, exists := p.seenBlks[blkHash]
	return exists
}

// markBlockSeen records a block hash after it has been verified or queued in orphan buffer [N-17]
func (p *PeerServer) markBlockSeen(blkHash string) {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.seenBlks == nil {
		p.seenBlks = make(map[string]time.Time)
	}
	now := time.Now()
	if len(p.seenBlks) > 5000 {
		for h, t := range p.seenBlks {
			if now.Sub(t) > 30*time.Minute {
				delete(p.seenBlks, h)
			}
		}
	}
	p.seenBlks[blkHash] = now
}

// isBlockSeenOrMark checks if seen and marks (provided for backwards compatibility)
func (p *PeerServer) isBlockSeenOrMark(blkHash string) bool {
	if p.isBlockSeen(blkHash) {
		return true
	}
	p.markBlockSeen(blkHash)
	return false
}

// isBlockNegativeCached checks whether a block hash is known to be invalid [T2].
func (p *PeerServer) isBlockNegativeCached(blkHash string) bool {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.negativeBlks == nil {
		return false
	}
	exp, exists := p.negativeBlks[blkHash]
	if !exists {
		return false
	}
	if time.Now().After(exp) {
		delete(p.negativeBlks, blkHash)
		return false
	}
	return true
}

// markBlockNegative adds an invalid block hash to the bounded negative cache [T2].
func (p *PeerServer) markBlockNegative(blkHash string) {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.negativeBlks == nil {
		p.negativeBlks = make(map[string]time.Time)
	}
	now := time.Now()
	// Prune if cache exceeds 1024 entries
	if len(p.negativeBlks) >= 1024 {
		for h, exp := range p.negativeBlks {
			if now.After(exp) {
				delete(p.negativeBlks, h)
			}
		}
		// If still too large, drop half to bound memory
		if len(p.negativeBlks) >= 1024 {
			count := 0
			for h := range p.negativeBlks {
				delete(p.negativeBlks, h)
				count++
				if count >= 512 {
					break
				}
			}
		}
	}
	p.negativeBlks[blkHash] = now.Add(10 * time.Minute)
}

// canTriggerSync enforces a cooldown per peer IP (1 sync per 30 seconds) [T3].
func (p *PeerServer) canTriggerSync(peerHost string) bool {
	p.seenLock.Lock()
	defer p.seenLock.Unlock()
	if p.syncCooldowns == nil {
		p.syncCooldowns = make(map[string]time.Time)
	}
	now := time.Now()
	if last, exists := p.syncCooldowns[peerHost]; exists {
		if now.Sub(last) < 30*time.Second {
			return false
		}
	}
	p.syncCooldowns[peerHost] = now
	return true
}

// AddOrphanBlock validates header PoW, target bounds, and Merkle root before caching orphan block keyed by Block.Hash [N-17, T3]
func (p *PeerServer) AddOrphanBlock(b core.Block, peerIP string) bool {
	// 1. Header PoW and Merkle validation before storing [N-17]
	calcHash, err := b.CalculateHashWithError()
	if err != nil || b.Hash == "" || b.Hash != calcHash {
		p.AddBanScore(peerIP, 20)
		return false
	}

	// Maximum allowable target ceiling (minimum network difficulty) [T3]
	maxAllowedTarget := core.GetPowLimit()
	if core.IsArmaxHashActive(b.Index) {
		maxAllowedTarget = core.GetArmaxHashPowLimit()
	}
	if flag.Lookup("test.v") != nil && !core.GetActiveNetwork().IsMainnet {
		if maxAllowedTarget.Cmp(core.DifficultyToTarget(1)) < 0 {
			maxAllowedTarget = core.DifficultyToTarget(1)
		}
	}

	target := b.GetTarget()
	if target == nil || target.Sign() <= 0 || target.Cmp(maxAllowedTarget) > 0 || !core.HashMeetsTarget(calcHash, target) {
		p.AddBanScore(peerIP, 20)
		return false
	}

	// If parent block is already known in our local chain, enforce exact ExpectedTarget [T3]
	if p.Blockchain != nil {
		p.Blockchain.Lock.RLock()
		if parentBlock, err := p.Blockchain.GetBlockByHash(b.PrevHash); err == nil && parentBlock != nil {
			if b.Index == parentBlock.Index+1 {
				expectedTarget := core.CalculateExpectedTarget(p.Blockchain.Chain, b.Index)
				expectedTargetHex := fmt.Sprintf("%064x", expectedTarget)
				if b.Target != expectedTargetHex || !core.HashMeetsTarget(calcHash, expectedTarget) {
					p.Blockchain.Lock.RUnlock()
					p.AddBanScore(peerIP, 20)
					return false
				}
			}
		}
		p.Blockchain.Lock.RUnlock()
	}

	expectedMerkle := core.CalculateMerkleRoot(b.Transactions)
	if expectedMerkle != b.MerkleRoot {
		p.AddBanScore(peerIP, 20)
		return false
	}

	p.orphanLock.Lock()
	defer p.orphanLock.Unlock()
	if p.orphanBlocks == nil {
		p.orphanBlocks = make(map[string]*orphanBlockEntry)
	}

	// Per-peer limit check [N-17]
	peerCount := 0
	for _, entry := range p.orphanBlocks {
		if entry.PeerIP == peerIP {
			peerCount++
		}
	}
	if peerCount >= MaxOrphanBlocksPerPeer {
		return false
	}

	// Global LRU / FIFO limit [N-17]
	if len(p.orphanBlocks) >= MaxOrphanBlocks {
		var oldestHash string
		var oldestTime time.Time
		first := true
		for h, entry := range p.orphanBlocks {
			if first || entry.AddedAt.Before(oldestTime) {
				oldestHash = h
				oldestTime = entry.AddedAt
				first = false
			}
		}
		if oldestHash != "" {
			delete(p.orphanBlocks, oldestHash)
		}
	}

	p.orphanBlocks[b.Hash] = &orphanBlockEntry{
		Block:   b,
		PeerIP:  peerIP,
		AddedAt: time.Now(),
	}
	p.markBlockSeen(b.Hash)
	return true
}

// HasOrphanBlock checks if an orphan block is present in buffer [N-17]
func (p *PeerServer) HasOrphanBlock(blkHash string) bool {
	p.orphanLock.Lock()
	defer p.orphanLock.Unlock()
	if p.orphanBlocks == nil {
		return false
	}
	_, ok := p.orphanBlocks[blkHash]
	return ok
}

// processOrphans recursively attaches matching orphans whose PrevHash == parentHash [N-17]
func (p *PeerServer) processOrphans(parentHash string) {
	p.orphanLock.Lock()
	if p.orphanBlocks == nil {
		p.orphanLock.Unlock()
		return
	}
	var readyBlocks []core.Block
	for h, entry := range p.orphanBlocks {
		if entry.Block.PrevHash == parentHash {
			readyBlocks = append(readyBlocks, entry.Block)
			delete(p.orphanBlocks, h)
		}
	}
	p.orphanLock.Unlock()

	for _, orphan := range readyBlocks {
		if p.Blockchain.ValidateAndAddBlock(orphan) {
			p.markBlockSeen(orphan.Hash)
			go p.RelayBlock(orphan, "")
			p.processOrphans(orphan.Hash)
			p.processOrphanTxs()
		}
	}
}

// ProcessIncomingBlock validates and handles an incoming block from the network [T2, T3].
// Inexpensive structural and PoW checks are performed without write lock and without cloning state.
func (p *PeerServer) ProcessIncomingBlock(block core.Block, remoteIP string, remoteAddr string) bool {
	calcHash, err := block.CalculateHashWithError()
	if err != nil {
		p.AddBanScore(remoteIP, 20)
		return false
	}

	// Reject immediately if the block is already recorded in negative cache [T2]
	if p.isBlockNegativeCached(calcHash) {
		p.AddBanScore(remoteIP, 5)
		return false
	}

	if p.isBlockSeen(calcHash) {
		return false
	}

	if p.Blockchain == nil {
		return false
	}

	p.Blockchain.Lock.RLock()
	latest := p.Blockchain.GetLatestBlockUnsafe()
	p.Blockchain.Lock.RUnlock()

	if block.Index == latest.Index+1 && block.PrevHash == latest.Hash {
		// Stage 1 Pre-check: Inexpensive validation without write lock or state clone [T2]
		if err := p.Blockchain.PreCheckBlock(&block); err != nil {
			p.AddBanScore(remoteIP, 20)
			p.markBlockNegative(calcHash)
			debugLogf("[P2P] Pre-check rejected block #%d from %s: %v", block.Index, remoteAddr, err)
			return false
		}

		// Stage 2: Full consensus validation and state transition [T2]
		if p.Blockchain.ValidateAndAddBlock(block) {
			p.markBlockSeen(calcHash)
			hashPrefix := calcHash
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16] + "..."
			}
			dt := block.Timestamp - latest.Timestamp
			core.LogInfo(core.LogCategoryValidation, "Block #%d accepted from %s | hash: %s | txs: %d | diff: %d | dt: %ds",
				block.Index, remoteAddr, hashPrefix, len(block.Transactions), block.Difficulty, dt)
			go p.RelayBlock(block, remoteAddr)
			p.processOrphans(calcHash)
			p.processOrphanTxs()
			return true
		} else {
			// Failed full validation (e.g. invalid tx signature, state double-spend)
			p.AddBanScore(remoteIP, 20)
			p.markBlockNegative(calcHash)
			return false
		}
	} else if block.Index > latest.Index {
		// Orphan block or local chain is behind; save orphan and trigger sync [N-17, T3]
		if p.AddOrphanBlock(block, remoteIP) {
			p.markBlockSeen(calcHash)
			prevPrefix := block.PrevHash
			if len(prevPrefix) > 16 {
				prevPrefix = prevPrefix[:16] + "..."
			}
			core.LogInfo(core.LogCategoryValidation, "Valid orphan block #%d cached from %s | prev: %s. Initiating catch-up sync...",
				block.Index, remoteAddr, prevPrefix)

			host := remoteIP
			if h, _, err := net.SplitHostPort(remoteAddr); err == nil && h != "" {
				host = h
			}

			// Only trigger sync if peer cooldown has elapsed [T3]
			if p.canTriggerSync(host) {
				var matchingPeers []string
				for _, peer := range p.GetPeers() {
					if strings.HasPrefix(peer, host+":") {
						matchingPeers = append(matchingPeers, peer)
					}
				}
				if len(matchingPeers) == 1 {
					go p.RequestChainSync(matchingPeers[0])
				} else if len(matchingPeers) > 1 {
					go p.SyncBestPeer()
				} else if p.Port != "" {
					go p.RequestChainSync(net.JoinHostPort(host, p.Port))
				}
			}
			return true
		}
		// If orphan was rejected, do NOT trigger sync! [T3]
		return false
	} else if block.Index <= latest.Index && block.Index > 0 {
		// Competing block on an alternate fork at equal or lower height [N-14]
		if _, err := p.Blockchain.GetBlockByHash(calcHash); err != nil {
			if p.AddOrphanBlock(block, remoteIP) {
				p.markBlockSeen(calcHash)
				hashPrefix := calcHash
				if len(hashPrefix) > 16 {
					hashPrefix = hashPrefix[:16] + "..."
				}
				core.LogInfo(core.LogCategoryValidation, "Competing fork block #%d received from %s | hash: %s (local tip: #%d). Initiating fork sync...",
					block.Index, remoteAddr, hashPrefix, latest.Index)

				host := remoteIP
				if h, _, err := net.SplitHostPort(remoteAddr); err == nil && h != "" {
					host = h
				}

				if p.canTriggerSync(host) {
					var matchingPeers []string
					for _, peer := range p.GetPeers() {
						if strings.HasPrefix(peer, host+":") {
							matchingPeers = append(matchingPeers, peer)
						}
					}
					if len(matchingPeers) == 1 {
						go p.RequestChainSync(matchingPeers[0])
					} else if len(matchingPeers) > 1 {
						go p.SyncBestPeer()
					} else if p.Port != "" {
						go p.RequestChainSync(net.JoinHostPort(host, p.Port))
					}
				}
				return true
			}
		}
	}

	return false
}

// ProcessIncomingTx validates and adds tx to mempool, or holds out-of-order nonces in orphan pool [N-17]
func (p *PeerServer) ProcessIncomingTx(tx core.Transaction, peerIP string) bool {
	if p.Blockchain == nil {
		return false
	}
	txHash := tx.HashHex()
	if p.isTxSeen(txHash) {
		return false
	}

	success, msg := p.Blockchain.AddTransaction(tx)
	if success {
		core.LogInfo(core.LogCategoryMempool, "Accepted transaction %s from peer %s",
			txHash, peerIP)

		p.markTxSeen(txHash)
		go p.RelayTransaction(tx, peerIP)
		p.processOrphanTxs()
		return true
	}

	// Hold out-of-order nonces in orphan pool [N-17]
	if strings.Contains(strings.ToLower(msg), "nonce") {
		if tx.Verify() {
			p.orphanLock.Lock()
			if p.orphanTxs == nil {
				p.orphanTxs = make(map[string]*orphanTxEntry)
			}
			if len(p.orphanTxs) >= MaxOrphanTxs {
				var oldestHash string
				var oldestTime time.Time
				first := true
				for h, entry := range p.orphanTxs {
					if first || entry.AddedAt.Before(oldestTime) {
						oldestHash = h
						oldestTime = entry.AddedAt
						first = false
					}
				}
				if oldestHash != "" {
					delete(p.orphanTxs, oldestHash)
				}
			}
			p.orphanTxs[txHash] = &orphanTxEntry{
				Tx:      tx,
				PeerIP:  peerIP,
				AddedAt: time.Now(),
			}
			p.orphanLock.Unlock()
			p.markTxSeen(txHash)
			return true
		}
	}

	core.LogWarn(core.LogCategoryMempool, "Rejected transaction %s from peer %s: %s",
		txHash, peerIP, msg)
	return false
}

// HasOrphanTx checks if a transaction is in the orphan pool [N-17]
func (p *PeerServer) HasOrphanTx(txHash string) bool {
	p.orphanLock.Lock()
	defer p.orphanLock.Unlock()
	if p.orphanTxs == nil {
		return false
	}
	_, ok := p.orphanTxs[txHash]
	return ok
}

// processOrphanTxs checks cached orphan transactions to see if any can now be added to mempool [N-17, T5]
func (p *PeerServer) processOrphanTxs() {
	p.processOrphanTxsWithDepth(0)
}

func (p *PeerServer) processOrphanTxsWithDepth(depth int) {
	if depth > 5 {
		return
	}
	p.orphanLock.Lock()
	if len(p.orphanTxs) == 0 {
		p.orphanLock.Unlock()
		return
	}
	now := time.Now()
	// Debounce top-level scan if triggered less than 100ms ago [T5]
	if depth == 0 && now.Sub(p.lastOrphanScan) < 100*time.Millisecond {
		p.orphanLock.Unlock()
		return
	}
	p.lastOrphanScan = now

	var candidates []core.Transaction
	for h, entry := range p.orphanTxs {
		if now.Sub(entry.AddedAt) > OrphanTxExpiry {
			delete(p.orphanTxs, h)
			continue
		}
		candidates = append(candidates, entry.Tx)
	}
	p.orphanLock.Unlock()

	promotedAny := false
	for _, tx := range candidates {
		success, _ := p.Blockchain.AddTransaction(tx)
		if success {
			p.orphanLock.Lock()
			delete(p.orphanTxs, tx.HashHex())
			p.orphanLock.Unlock()
			p.markTxSeen(tx.HashHex())
			go p.RelayTransaction(tx, "")
			core.LogInfo(core.LogCategoryMempool, "Orphan transaction promoted: %s", tx.HashHex())
			promotedAny = true
		}
	}

	if promotedAny {
		p.processOrphanTxsWithDepth(depth + 1)
	}
}

// GetBanScore returns the current misbehavior score for an IP
func (p *PeerServer) GetBanScore(ip string) int {
	p.banLock.RLock()
	defer p.banLock.RUnlock()
	if p.banScores == nil {
		return 0
	}
	return p.banScores[ip]
}

// GetNetworkID returns the synchronized network protocol ID for this PeerServer instance [C-33].
// It reads netIDVal atomically as the single source of truth without acquiring locks,
// eliminating any recursive RLock deadlocks when callers already hold p.Lock.
func (p *PeerServer) GetNetworkID() string {
	if ptr := p.netIDVal.Load(); ptr != nil && *ptr != "" {
		return *ptr
	}
	// Fallback for uninitialized PeerServer structs (e.g. direct struct literals in unit tests)
	var fallback string
	if p.Blockchain != nil && p.Blockchain.GetNetworkID() != "" {
		fallback = p.Blockchain.GetNetworkID()
	} else {
		fallback = core.NetworkID()
	}
	canonical, _, err := parseNetworkID(fallback)
	if err == nil {
		fallback = canonical
	}
	p.netIDVal.CompareAndSwap(nil, &fallback)
	if ptr := p.netIDVal.Load(); ptr != nil && *ptr != "" {
		return *ptr
	}
	return fallback
}

// getNetworkIDLocked returns the network ID.
func (p *PeerServer) getNetworkIDLocked() string {
	return p.GetNetworkID()
}

// NewPeerServer initializes a new PeerServer with blockchain connection and peer persistence [P1-A]
func NewPeerServer(port string, bc *core.Blockchain, opts ...string) *PeerServer {
	var dataDir string
	var netID string
	if len(opts) > 0 {
		dataDir = opts[0]
	}
	if len(opts) > 1 {
		netID = opts[1]
	}
	if netID == "" {
		if bc != nil && bc.GetNetworkID() != "" {
			netID = bc.GetNetworkID()
		} else {
			netID = core.NetworkID()
		}
	}
	canonicalNetID, _, parseErr := parseNetworkID(netID)
	if parseErr != nil {
		canonicalNetID = netID // preserve original for error reporting
	}

	var path string
	var initErr error
	if parseErr != nil {
		initErr = parseErr
	} else {
		path, initErr = derivePeerStorePath(dataDir, canonicalNetID)
	}

	// Ensure persistent TLS identity is loaded or created for this data directory [T11]
	instTLS, _ := EnsureNodeTLSConfig(dataDir)

	ps := &PeerServer{
		Port:               port,
		DataDir:            dataDir,
		tlsConfig:          instTLS,
		peerStorePath:      path,
		peerStoreInitErr:   initErr,
		peerStoreCorrupted: initErr != nil,
		Peers:              []string{},
		newPeers:           make(map[string]*PeerInfo),
		triedPeers:         make(map[string]*PeerInfo),
		Blockchain:         bc,
		MaxPeers:           MaxPeersDefault,
		subnetConns:        make(map[string]int),
		AllowPrivatePeers:  isPrivatePeersAllowedForNetwork(canonicalNetID),
		DHT:                NewRoutingTable(":" + port),
		seenTxs:            make(map[string]time.Time),
		seenBlks:           make(map[string]time.Time),
		relayedTxs:         make(map[string]map[string]time.Time),
		syncingHosts:       make(map[string]bool),
		expectedChains:     make(map[string]time.Time),
		orphanBlocks:       make(map[string]*orphanBlockEntry),
		orphanTxs:          make(map[string]*orphanTxEntry),
		banScores:          make(map[string]int),
		bannedIPs:          make(map[string]time.Time),
		rateTrackers:       make(map[string]*peerRateTracker),
		pendingTOFU:        make(map[string]*pendingPin),
		failedPins:         make(map[string]string),
		quitChan:           make(chan struct{}),
	}
	ps.netIDVal.Store(&canonicalNetID)
	ps.loadPeers()

	// Seed DHT routing table with known peers
	for _, p := range ps.Peers {
		ps.DHT.Update(p)
	}

	return ps
}

// GetState returns the current lifecycle state of the server [P1-A Lifecycle Hardening].
func (p *PeerServer) GetState() ServerLifecycleState {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()
	return p.state
}

// Stop gracefully closes the listener, aborts active connections, waits for server goroutines, and terminates background tasks [T-06, P1-A].
func (p *PeerServer) Stop() {
	p.lifecycleLock.Lock()
	if p.state == ServerStateStopped {
		p.lifecycleLock.Unlock()
		return // Idempotent: already stopped
	}

	cycle := p.activeCycle
	if cycle == nil {
		p.state = ServerStateStopped
		p.serverStarted = false
		p.lifecycleLock.Unlock()
		return
	}

	if p.state == ServerStateStopping {
		// Another caller is already stopping this cycle.
		// Requirement 1.3: Wait for the same shutdown completion; do NOT return prematurely.
		doneChan := cycle.doneChan
		p.lifecycleLock.Unlock()

		select {
		case <-doneChan:
			return
		case <-time.After(5 * time.Second):
			fmt.Printf("[P2P] Concurrent Stop timed out waiting for cycle %d shutdown\n", cycle.id)
			return
		}
	}

	p.state = ServerStateStopping
	p.serverStarted = false
	doneChan := cycle.doneChan
	p.lifecycleLock.Unlock()

	if serverStoppingHook != nil {
		serverStoppingHook()
	}

	// Trigger cycle cancellation
	cycle.cancelOnce.Do(func() {
		close(cycle.quitChan)
	})

	// Close listener immediately so Accept unblocks
	p.stopLock.Lock()
	if p.listener != nil {
		_ = p.listener.Close()
		p.listener = nil
	}
	p.stopLock.Unlock()

	// Close all active connections to unblock in-flight HandleConnection readers
	cycle.connsMu.Lock()
	for c := range cycle.activeConns {
		_ = c.Close()
	}
	cycle.connsMu.Unlock()

	p.closeOutboundSessions(cycle)

	// Wait for StartServer accept loop, workers, and teardown to complete
	select {
	case <-doneChan:
		// Clean shutdown completed; state is safely ServerStateStopped.
		return
	case <-time.After(5 * time.Second):
		// Requirement 1.4: Do NOT change state to STOPPED just because timeout expired.
		// Keep state as ServerStateStopping and report timeout clearly.
		fmt.Printf("[P2P] Stop timed out waiting for cycle %d workers to drain\n", cycle.id)
		return
	}
}

var (
	mainnetRateLimitWarnOnce sync.Once
)

// isRateLimitDisabled returns true only if ARMX_DISABLE_RATE_LIMIT is set AND active profile is not Mainnet [F2-HARDENING]
func (p *PeerServer) isRateLimitDisabled() bool {
	if os.Getenv("ARMX_DISABLE_RATE_LIMIT") != "1" {
		return false
	}
	canon, _, err := parseNetworkID(p.GetNetworkID())
	if err == nil && canon == "ARMX-MAINNET-1" {
		mainnetRateLimitWarnOnce.Do(func() {
			fmt.Println("[P2P] [!] WARNING: ARMX_DISABLE_RATE_LIMIT is ignored on Mainnet profile. Rate limiting remains strictly enforced.")
		})
		return false
	}
	return true
}

// AddBanScore increments the misbehavior score of an IP and bans it for 24h if threshold >= 100 [N-15]
func (p *PeerServer) AddBanScore(ip string, score int) {
	if p.isRateLimitDisabled() {
		return
	}
	p.banLock.Lock()
	defer p.banLock.Unlock()
	if p.banScores == nil {
		p.banScores = make(map[string]int)
	}
	if p.bannedIPs == nil {
		p.bannedIPs = make(map[string]time.Time)
	}
	p.banScores[ip] += score
	if p.banScores[ip] >= 100 {
		p.bannedIPs[ip] = time.Now().Add(24 * time.Hour)
		fmt.Printf("[P2P] [!] Banned peer IP %s for 24h (ban score: %d)\n", ip, p.banScores[ip])
	}
}

// IsBanned checks if an IP is currently banned [N-15]
func (p *PeerServer) IsBanned(ip string) bool {
	if p.isRateLimitDisabled() {
		return false
	}
	p.banLock.RLock()
	defer p.banLock.RUnlock()
	if p.bannedIPs == nil {
		return false
	}
	exp, exists := p.bannedIPs[ip]
	if !exists {
		return false
	}
	if time.Now().Before(exp) {
		return true
	}
	return false
}

// AllowMessage checks token bucket (20 msgs/s) and specific endpoint limits (GET_BLOCK_RANGE 5/min, GET_FORKPOINT 5/min) [N-15]
func (p *PeerServer) AllowMessage(ip string, msgType string) bool {
	if p.isRateLimitDisabled() {
		return true
	}
	p.banLock.Lock()
	defer p.banLock.Unlock()

	if p.bannedIPs != nil {
		if exp, exists := p.bannedIPs[ip]; exists && time.Now().Before(exp) {
			return false
		}
	}

	if p.rateTrackers == nil {
		p.rateTrackers = make(map[string]*peerRateTracker)
	}
	tracker, exists := p.rateTrackers[ip]
	now := time.Now()
	if !exists {
		burst := p.BlockRangeBurst
		if burst <= 0 {
			burst = 120.0
		}
		tracker = &peerRateTracker{
			tokens:               30,
			lastRefill:           now,
			blockTokens:          200,
			lastBlockRefill:      now,
			blockRangeTokens:     burst,
			lastBlockRangeRefill: now,
			forkpointTokens:      30,
			lastForkpointRefill:  now,
		}
		p.rateTrackers[ip] = tracker
	}

	// Dedicated token bucket for NEW_BLOCK messages during block sync [T1]
	// Allows burst 200 and sustained refill of 80 blocks/sec so honest sync is not throttled
	if msgType == "NEW_BLOCK" {
		elapsed := now.Sub(tracker.lastBlockRefill).Seconds()
		if elapsed > 0 {
			tracker.blockTokens += elapsed * 80.0
			if tracker.blockTokens > 200.0 {
				tracker.blockTokens = 200.0
			}
			tracker.lastBlockRefill = now
		}
		if tracker.blockTokens < 1.0 {
			if p.banScores == nil {
				p.banScores = make(map[string]int)
			}
			p.banScores[ip] += 5
			if p.banScores[ip] >= 100 {
				if p.bannedIPs == nil {
					p.bannedIPs = make(map[string]time.Time)
				}
				p.bannedIPs[ip] = now.Add(24 * time.Hour)
			}
			return false
		}
		tracker.blockTokens -= 1.0
		return true
	}

	// Dedicated token bucket for GET_BLOCK_RANGE during continuous synchronization
	// Allows burst up to 120 chunks (30,000 blocks) and sustained refill of 20 chunks/sec (5,000 blocks/sec)
	if msgType == "GET_BLOCK_RANGE" {
		burst := p.BlockRangeBurst
		if burst <= 0 {
			burst = 120.0
		}
		refillRate := p.BlockRangeRefillRate
		if refillRate <= 0 {
			refillRate = 20.0
		}
		elapsed := now.Sub(tracker.lastBlockRangeRefill).Seconds()
		if elapsed > 0 {
			tracker.blockRangeTokens += elapsed * refillRate
			if tracker.blockRangeTokens > burst {
				tracker.blockRangeTokens = burst
			}
			tracker.lastBlockRangeRefill = now
		}
		if tracker.blockRangeTokens < 1.0 {
			// Throttle excess requests gracefully without penalizing honest synchronization peers
			return false
		}
		tracker.blockRangeTokens -= 1.0
		return true
	} else if msgType == "GET_FORKPOINT" {
		elapsed := now.Sub(tracker.lastForkpointRefill).Seconds()
		if elapsed > 0 {
			tracker.forkpointTokens += elapsed * 5.0
			if tracker.forkpointTokens > 30.0 {
				tracker.forkpointTokens = 30.0
			}
			tracker.lastForkpointRefill = now
		}
		if tracker.forkpointTokens < 1.0 {
			return false
		}
		tracker.forkpointTokens -= 1.0
		return true
	}

	// Refill token bucket: 20 tokens per second up to 30 burst for general messages
	elapsed := now.Sub(tracker.lastRefill).Seconds()
	tracker.tokens += elapsed * 20.0
	if tracker.tokens > 30.0 {
		tracker.tokens = 30.0
	}
	tracker.lastRefill = now

	if tracker.tokens < 1.0 {
		// Rate limited on global packet rate
		if p.banScores == nil {
			p.banScores = make(map[string]int)
		}
		p.banScores[ip] += 5
		if p.banScores[ip] >= 100 {
			if p.bannedIPs == nil {
				p.bannedIPs = make(map[string]time.Time)
			}
			p.bannedIPs[ip] = now.Add(24 * time.Hour)
		}
		return false
	}
	tracker.tokens -= 1.0

	return true
}

// rebuildPeersListLocked synchronizes p.Peers slice with tried and new peer tables
func (p *PeerServer) rebuildPeersListLocked() {
	var list []string
	for addr := range p.triedPeers {
		list = append(list, addr)
	}
	for addr := range p.newPeers {
		list = append(list, addr)
	}
	p.Peers = list
}

// IsNewPeer checks if address is in untried peer table [N-16]
func (p *PeerServer) IsNewPeer(addr string) bool {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	if p.newPeers == nil {
		return false
	}
	_, ok := p.newPeers[addr]
	return ok
}

// IsTriedPeer checks if address is in verified tried peer table [N-16]
func (p *PeerServer) IsTriedPeer(addr string) bool {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	if p.triedPeers == nil {
		return false
	}
	_, ok := p.triedPeers[addr]
	return ok
}

// HasPeer checks if address is in either tried or new tables [N-16]
func (p *PeerServer) HasPeer(addr string) bool {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	if p.triedPeers != nil {
		if _, ok := p.triedPeers[addr]; ok {
			return true
		}
	}
	if p.newPeers != nil {
		if _, ok := p.newPeers[addr]; ok {
			return true
		}
	}
	return false
}

// RecordPeerSuccess promotes peer from new to tried table upon valid handshake and persists to disk [N-16, F-01]
func (p *PeerServer) RecordPeerSuccess(addr string, height int, networkID string) error {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	p.Lock.Lock()

	// Capture snapshot of previous in-memory state for rollback on persistence failure.
	// Uses deep value copies so rollback snapshot is strictly immutable and cannot be mutated by other operations.
	var oldTriedCopy *PeerInfo
	if p.triedPeers != nil {
		if pi, ok := p.triedPeers[addr]; ok && pi != nil {
			cp := *pi
			oldTriedCopy = &cp
		}
	}
	var oldNewCopy *PeerInfo
	if p.newPeers != nil {
		if pi, ok := p.newPeers[addr]; ok && pi != nil {
			cp := *pi
			oldNewCopy = &cp
		}
	}

	// F-01: Preserve existing pinned certificate fingerprint if already recorded in tried or new peers
	existingFP := ""
	if oldTriedCopy != nil {
		existingFP = oldTriedCopy.CertFingerprint
	}
	if existingFP == "" && oldNewCopy != nil {
		existingFP = oldNewCopy.CertFingerprint
	}

	if p.newPeers != nil {
		delete(p.newPeers, addr)
	}
	if p.triedPeers == nil {
		p.triedPeers = make(map[string]*PeerInfo)
	}
	p.triedPeers[addr] = &PeerInfo{
		Address:         addr,
		LastSeen:        time.Now(),
		Failures:        0,
		Height:          height,
		NetworkID:       networkID,
		CertFingerprint: existingFP, // Preserve TOFU pinned identity
	}
	p.rebuildPeersListLocked()
	p.Lock.Unlock()

	if err := p.savePeersLocked(); err != nil {
		// Roll back in-memory state so persistence failure does not leave inconsistent state.
		// Restores deep-copied snapshot under p.Lock while still holding savePeersLock.
		p.Lock.Lock()
		if oldTriedCopy != nil {
			p.triedPeers[addr] = oldTriedCopy
		} else {
			delete(p.triedPeers, addr)
		}
		if oldNewCopy != nil {
			if p.newPeers == nil {
				p.newPeers = make(map[string]*PeerInfo)
			}
			p.newPeers[addr] = oldNewCopy
		} else {
			delete(p.newPeers, addr)
		}
		p.rebuildPeersListLocked()
		p.Lock.Unlock()
		return fmt.Errorf("failed to persist peer store on peer success for %s: %w", addr, err)
	}

	p.clearPeerUnreachable(addr)
	return nil
}

// RecordPeerFailure increments failure counter and evicts dead peers after MaxPeerFailures [N-16]
func (p *PeerServer) RecordPeerFailure(addr string) error {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	p.Lock.Lock()
	var info *PeerInfo
	inTried := false
	if p.triedPeers != nil {
		if pi, ok := p.triedPeers[addr]; ok {
			info = pi
			inTried = true
		}
	}
	if info == nil && p.newPeers != nil {
		if pi, ok := p.newPeers[addr]; ok {
			info = pi
		}
	}
	if info == nil {
		p.Lock.Unlock()
		return nil
	}

	// Capture deep-copy snapshot in case failure eviction/persistence fails
	var oldTriedCopy *PeerInfo
	if inTried && info != nil {
		cp := *info
		oldTriedCopy = &cp
	}
	var oldNewCopy *PeerInfo
	if !inTried && info != nil {
		cp := *info
		oldNewCopy = &cp
	}

	info.Failures++
	evicted := false
	if info.Failures >= MaxPeerFailures && p.isSeedPeer(addr) {
		// Seeds are the node's only way back after an outage: keep them in the address book.
		info.Failures = MaxPeerFailures - 1
	}
	if info.Failures >= MaxPeerFailures {
		// Evict dead peer [N-16]
		if inTried {
			delete(p.triedPeers, addr)
		} else {
			delete(p.newPeers, addr)
		}
		p.rebuildPeersListLocked()
		evicted = true
	}
	p.Lock.Unlock()

	if evicted {
		p.markPeerUnreachable(addr, EvictedPeerCooldown)
		if sess := p.getExistingOutboundSession(addr); sess != nil {
			p.dropOutboundSession(addr, sess)
		}
		if err := p.savePeersLocked(); err != nil {
			// Roll back eviction on persistence failure so memory matches disk
			p.Lock.Lock()
			if inTried && oldTriedCopy != nil {
				if p.triedPeers == nil {
					p.triedPeers = make(map[string]*PeerInfo)
				}
				p.triedPeers[addr] = oldTriedCopy
			} else if !inTried && oldNewCopy != nil {
				if p.newPeers == nil {
					p.newPeers = make(map[string]*PeerInfo)
				}
				p.newPeers[addr] = oldNewCopy
			}
			p.rebuildPeersListLocked()
			p.Lock.Unlock()
			return fmt.Errorf("failed to persist peer store on peer eviction for %s: %w", addr, err)
		}
	}
	return nil
}

// VersionedPeerStore represents the on-disk peer storage format [Phase 5]
type VersionedPeerStore struct {
	Version   int         `json:"version"`
	NetworkID string      `json:"network_id"`
	Peers     []*PeerInfo `json:"peers"`
}

// GetPeerStoreInitErr returns any error encountered while creating or deriving the peer store path [P1-A]
func (p *PeerServer) GetPeerStoreInitErr() error {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	return p.peerStoreInitErr
}

// GetPeersPath returns the filesystem path to the peer store for this PeerServer instance [P1-A]
func (p *PeerServer) GetPeersPath() string {
	p.Lock.RLock()
	if p.peerStorePath != "" {
		parentDir := filepath.Dir(p.peerStorePath)
		subDir, _ := networkSubDir(p.getNetworkIDLocked())
		expectedDir := p.DataDir
		if subDir != "" && expectedDir != "" && !strings.EqualFold(filepath.Base(filepath.Clean(expectedDir)), subDir) {
			expectedDir = filepath.Join(expectedDir, subDir)
		}
		if p.DataDir == "" || filepath.Clean(parentDir) == filepath.Clean(expectedDir) {
			path := p.peerStorePath
			p.Lock.RUnlock()
			return path
		}
	}
	p.Lock.RUnlock()

	p.Lock.Lock()
	defer p.Lock.Unlock()
	path, err := derivePeerStorePath(p.DataDir, p.getNetworkIDLocked())
	p.peerStorePath = path
	p.peerStoreInitErr = err
	if err != nil {
		p.peerStoreCorrupted = true
	}
	return path
}

// SetDataDir updates the instance data directory and refreshes the peer store path [P1-A]
func (p *PeerServer) SetDataDir(dir string) error {
	p.lifecycleLock.Lock()
	defer p.lifecycleLock.Unlock()

	if p.state != ServerStateStopped {
		return fmt.Errorf("cannot change data directory after server has started (state: %s, only allowed when stopped)", p.state)
	}

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	p.tofuLock.Lock()
	defer p.tofuLock.Unlock()

	if len(p.pendingTOFU) > 0 {
		return errors.New("cannot change data directory while TOFU pin is pending")
	}

	p.Lock.Lock()
	p.DataDir = dir
	path, err := derivePeerStorePath(dir, p.getNetworkIDLocked())
	p.peerStorePath = path
	p.peerStoreInitErr = err
	if err != nil {
		p.peerStoreCorrupted = true
		p.Lock.Unlock()
		return err
	}
	// Clear existing in-memory peers so missing or new store does not inherit pins from previous store!
	p.newPeers = make(map[string]*PeerInfo)
	p.triedPeers = make(map[string]*PeerInfo)
	p.Peers = []string{}
	p.failedPins = make(map[string]string)
	p.pendingTOFU = make(map[string]*pendingPin)
	p.peerStoreCorrupted = false
	p.Lock.Unlock()

	p.closeOutboundSessions(nil)
	p.loadPeers()
	return nil
}

// SetNetworkID updates the instance network ID and refreshes the peer store path [P1-A]
func (p *PeerServer) SetNetworkID(netID string) error {
	normID, _, err := parseNetworkID(netID)
	if err != nil {
		return err
	}

	p.lifecycleLock.Lock()
	defer p.lifecycleLock.Unlock()

	if p.state != ServerStateStopped {
		return fmt.Errorf("cannot change network ID after server has started (state: %s, only allowed when stopped)", p.state)
	}

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	p.tofuLock.Lock()
	defer p.tofuLock.Unlock()

	if len(p.pendingTOFU) > 0 {
		return errors.New("cannot change network ID while TOFU pin is pending")
	}

	// Remove stale registry mapping under old network ID
	UnregisterPeerServer(p)

	p.Lock.Lock()
	p.netIDVal.Store(&normID)
	p.AllowPrivatePeers = isPrivatePeersAllowedForNetwork(normID)
	path, err := derivePeerStorePath(p.DataDir, normID)
	p.peerStorePath = path
	p.peerStoreInitErr = err
	if err != nil {
		p.peerStoreCorrupted = true
		p.Lock.Unlock()
		return err
	}
	// Clear existing in-memory peers so missing or new store does not inherit pins from previous store!
	p.newPeers = make(map[string]*PeerInfo)
	p.triedPeers = make(map[string]*PeerInfo)
	p.Peers = []string{}
	p.failedPins = make(map[string]string)
	p.pendingTOFU = make(map[string]*pendingPin)
	p.peerStoreCorrupted = false
	p.Lock.Unlock()

	p.closeOutboundSessions(nil)
	p.loadPeers()
	return nil
}

// loadPeers loads known peer addresses and pinned TOFU fingerprints from disk [Phase 5, F-04, P1-A]
func (p *PeerServer) loadPeers() {
	p.Lock.RLock()
	initErr := p.peerStoreInitErr
	p.Lock.RUnlock()
	if initErr != nil {
		p.Lock.Lock()
		p.peerStoreCorrupted = true
		p.Lock.Unlock()
		fmt.Printf("[P2P] CRITICAL: Peer store path initialization failed (%v). P2P entering fail-closed mode.\n", initErr)
		return
	}
	path := p.GetPeersPath()
	if path == "" {
		p.Lock.Lock()
		p.peerStoreCorrupted = true
		p.Lock.Unlock()
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return // Clean first run on new installation
		}
		// F-04: Non-NotExist errors (permission denied, disk error) must enter fail-closed mode!
		p.Lock.Lock()
		p.peerStoreCorrupted = true
		p.Lock.Unlock()
		fmt.Printf("[P2P] CRITICAL: Failed to read peer store at %s (%v). P2P entering fail-closed mode.\n", path, err)
		return
	}

	p.Lock.Lock()
	defer p.Lock.Unlock()

	if p.newPeers == nil {
		p.newPeers = make(map[string]*PeerInfo)
	}
	if p.triedPeers == nil {
		p.triedPeers = make(map[string]*PeerInfo)
	}

	// 1. Attempt to unmarshal into VersionedPeerStore
	var store VersionedPeerStore
	if err := json.Unmarshal(data, &store); err == nil {
		// Strict schema and version validation [F-04]:
		// Version must be exactly 1 (current supported schema version)
		if store.Version != 1 {
			p.preserveCorruptPeerStore(path, data, fmt.Sprintf("Unsupported peer store version %d in %s", store.Version, path))
			return
		}
		// NetworkID must match active network
		if store.NetworkID == "" || store.NetworkID != p.getNetworkIDLocked() {
			p.preserveCorruptPeerStore(path, data, fmt.Sprintf("Peer store network ID mismatch or missing: store has '%s', expected '%s'", store.NetworkID, p.getNetworkIDLocked()))
			return
		}
		// Peers array must be non-nil
		if store.Peers == nil {
			p.preserveCorruptPeerStore(path, data, fmt.Sprintf("Peer store has invalid/missing peers list in %s", path))
			return
		}

		for _, info := range store.Peers {
			if info == nil || info.Address == "" || p.isSelfPeerAddress(info.Address) {
				continue
			}
			if info.CertFingerprint != "" {
				// Peer has pinned certificate fingerprint
				p.triedPeers[info.Address] = info
			} else {
				// Peer has no pinned fingerprint; untrusted/unpinned, requires explicit bootstrap
				p.newPeers[info.Address] = info
			}
		}
		p.rebuildPeersListLocked()
		p.peerStoreCorrupted = false
		fmt.Printf("[P2P] Loaded %d known peers from versioned store %s\n", len(p.Peers), path)
		return
	}

	// 2. Legacy fallback: JSON array of string addresses (untrusted/unpinned)
	var legacyList []string
	if err := json.Unmarshal(data, &legacyList); err == nil {
		for _, addr := range legacyList {
			if addr == "" || p.isSelfPeerAddress(addr) {
				continue
			}
			if len(p.newPeers) < MaxAddressBookSize {
				p.newPeers[addr] = &PeerInfo{
					Address:         addr,
					LastSeen:        time.Now(),
					NetworkID:       p.getNetworkIDLocked(),
					CertFingerprint: "", // Untrusted/unpinned: requires explicit TOFU bootstrap
				}
			}
		}
		p.rebuildPeersListLocked()
		p.peerStoreCorrupted = false
		fmt.Printf("[P2P] Loaded %d legacy unpinned peers from %s\n", len(p.Peers), path)
		return
	}

	// 3. Corrupt or malformed peer store: FAIL CLOSED [Phase 5, Follow-up Point 4, Point 9]
	// Never silently reset pins or fall back to unpinned trust. Preserve the file for diagnosis,
	// and disable TOFU bootstrapping/re-pinning to prevent Person-In-The-Middle attacks.
	p.preserveCorruptPeerStore(path, data, fmt.Sprintf("Peer store %s is corrupt or malformed", path))
}

func (p *PeerServer) preserveCorruptPeerStore(path string, data []byte, reason string) {
	p.peerStoreCorrupted = true
	corruptBackup := path + ".corrupt"
	if _, err := os.Stat(corruptBackup); os.IsNotExist(err) {
		_ = os.WriteFile(corruptBackup, data, 0600)
	} else {
		// Timestamped forensic snapshot to ensure earlier corrupt backups are never overwritten [Follow-up Point 9]
		timestampedBackup := fmt.Sprintf("%s.corrupt.%d", path, time.Now().UnixNano())
		_ = os.WriteFile(timestampedBackup, data, 0600)
	}
	fmt.Printf("[P2P] CRITICAL: %s! Preserved at %s. P2P entering fail-closed mode: TOFU bootstrapping and auto-pinning disabled to prevent Person-In-The-Middle attacks.\n", reason, corruptBackup)
}

// Failure injection hooks for deterministic unit testing of savePeers I/O stages [P1 Review]
var (
	savePeersMarshalHook    func(v any) ([]byte, error)
	savePeersMkdirAllHook   func(path string, perm os.FileMode) error
	savePeersCreateTempHook func(dir, pattern string) (*os.File, error)
	savePeersChmodTempHook  func(name string, mode os.FileMode) error
	savePeersWriteHook      func(f *os.File, b []byte) (int, error)
	savePeersSyncHook       func(f *os.File) error
	savePeersCloseHook      func(f *os.File) error
	savePeersRenameHook     func(oldpath, newpath string) error
	tofuPreSaveHook         func(peerAddr string)
	serverStartingHook      func()
	serverStoppingHook      func()
	dialPreTOFUHook         func(peerAddr string)
	peerStoreMutationHook   func(op string, addr string)
	peerStoreMutationHookMu sync.RWMutex
	serverCycleCleanupHook  func(cycleID uint64)
)

func getPeerStoreMutationHook() func(op string, addr string) {
	peerStoreMutationHookMu.RLock()
	defer peerStoreMutationHookMu.RUnlock()
	return peerStoreMutationHook
}

// savePeers persists known peer addresses and pinned TOFU fingerprints via atomic write with fsync [N-10, Phase 5].
//
// Lock Ordering Architecture [P1 Final Blocker]:
// 1. p.savePeersLock (exclusive mutex, serializes the entire snapshot -> marshal -> write -> sync -> rename transaction)
// 2. p.Lock (read-lock RLock, acquired briefly ONLY to snapshot and deep-copy peer maps)
//
// Deadlock Prevention Rule:
// NEVER acquire p.savePeersLock while holding p.Lock. p.savePeersLock is always acquired first, and
// p.Lock is released before any I/O or disk operations proceed.
func (p *PeerServer) savePeers() error {
	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()
	return p.savePeersLocked()
}

// savePeersLocked performs atomic peer store persistence while caller already holds p.savePeersLock.
func (p *PeerServer) savePeersLocked() error {
	p.Lock.RLock()
	if p.peerStoreInitErr != nil {
		p.Lock.RUnlock()
		return fmt.Errorf("peer store path initialization failed: %w", p.peerStoreInitErr)
	}
	if p.peerStoreCorrupted {
		// Fail-closed guard: NEVER overwrite a corrupted peer store with an empty table [Follow-up Point 4]
		p.Lock.RUnlock()
		return fmt.Errorf("refusing to save peers: peer store is in corrupt state")
	}

	store := VersionedPeerStore{
		Version:   1,
		NetworkID: p.getNetworkIDLocked(),
		Peers:     make([]*PeerInfo, 0, len(p.triedPeers)+len(p.newPeers)),
	}
	// Deep-copy every PeerInfo value while holding p.Lock.RLock() [P1 Final Blocker]
	for _, info := range p.triedPeers {
		if info != nil && info.Address != "" {
			cp := *info
			store.Peers = append(store.Peers, &cp)
		}
	}
	for addr, info := range p.newPeers {
		if info != nil && info.Address != "" {
			if _, exists := p.triedPeers[addr]; !exists {
				cp := *info
				store.Peers = append(store.Peers, &cp)
			}
		}
	}
	p.Lock.RUnlock()

	var data []byte
	var err error
	if savePeersMarshalHook != nil {
		data, err = savePeersMarshalHook(store)
	} else {
		data, err = json.MarshalIndent(store, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("failed to marshal peer store: %w", err)
	}

	path := p.GetPeersPath()
	dir := filepath.Dir(path)
	if savePeersMkdirAllHook != nil {
		err = savePeersMkdirAllHook(dir, 0700)
	} else {
		err = os.MkdirAll(dir, 0700)
	}
	if err != nil {
		return fmt.Errorf("failed to create peer store directory %s: %w", dir, err)
	}

	// Unique temporary file for concurrent-safe atomic persistence [Follow-up Point 6]
	var tmpFile *os.File
	if savePeersCreateTempHook != nil {
		tmpFile, err = savePeersCreateTempHook(dir, "peers-*.tmp")
	} else {
		tmpFile, err = os.CreateTemp(dir, "peers-*.tmp")
	}
	if err != nil {
		return fmt.Errorf("failed to create temporary peer store file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if savePeersChmodTempHook != nil {
		err = savePeersChmodTempHook(tmpPath, 0600)
	} else {
		err = os.Chmod(tmpPath, 0600)
	}
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to chmod temporary peer store file: %w", err)
	}

	// Verify 0600 permissions on temp file before rename
	if runtime.GOOS != "windows" {
		if fi, statErr := tmpFile.Stat(); statErr != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to stat temporary peer store file: %w", statErr)
		} else if fi.Mode().Perm() != 0600 {
			_ = tmpFile.Close()
			return fmt.Errorf("temporary peer store file permissions %o != expected 0600", fi.Mode().Perm())
		}
	}

	if savePeersWriteHook != nil {
		_, err = savePeersWriteHook(tmpFile, data)
	} else {
		_, err = tmpFile.Write(data)
	}
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temporary peer store file: %w", err)
	}

	if savePeersSyncHook != nil {
		err = savePeersSyncHook(tmpFile)
	} else {
		err = tmpFile.Sync()
	}
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temporary peer store file: %w", err)
	}

	var closeErr error
	if savePeersCloseHook != nil {
		closeErr = savePeersCloseHook(tmpFile)
		_ = tmpFile.Close()
	} else {
		closeErr = tmpFile.Close()
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close temporary peer store file: %w", closeErr)
	}

	if savePeersRenameHook != nil {
		err = savePeersRenameHook(tmpPath, path)
	} else {
		err = os.Rename(tmpPath, path)
		if err != nil && runtime.GOOS == "windows" {
			for retry := 0; retry < 5 && err != nil; retry++ {
				time.Sleep(10 * time.Millisecond)
				err = os.Rename(tmpPath, path)
			}
		}
	}
	if err != nil {
		// F-02: Do NOT speculatively delete the existing file. Preserve old destination and return.
		return fmt.Errorf("failed to rename temporary peer store file: %w", err)
	}
	tmpPath = "" // rename succeeded atomically
	return nil
}

// startPeerDiscoveryGossip periodically queries known peers for newly discovered network nodes
func (p *PeerServer) startPeerDiscoveryGossip(cycle *serverCycle) {
	defer cycle.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	firstRound := true

	for {
		if firstRound {
			firstRound = false
		} else {
			select {
			case <-ticker.C:
			case <-cycle.quitChan:
				return
			}
		}

		peers := p.GetPeers()
		if len(peers) == 0 {
			continue
		}

		// Bound concurrency: randomly sample at most 8 peers per round to prevent goroutine explosion
		if len(peers) > 8 {
			rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
			peers = peers[:8]
		}

		for _, peer := range peers {
			select {
			case <-cycle.quitChan:
				return
			default:
			}

			cycle.wg.Add(1)
			go func(target string) {
				defer cycle.wg.Done()

				select {
				case <-cycle.quitChan:
					return
				default:
				}

				// 0. Self-announce our listening P2P port so public nodes not in DNS seeds can be discovered and verified
				if p.shouldSelfAnnounceTo(target) {
					_ = p.SendMessage(target, Message{
						NetworkID: p.GetNetworkID(),
						Type:      "PEERS_LIST",
						Payload:   []string{":" + p.Port},
					})
				}

				// 1. Query peer for newly discovered network peers
				resp, err := p.QueryPeer(target, Message{
					NetworkID: p.GetNetworkID(),
					Type:      "GET_PEERS",
					Payload:   nil,
				}, 4*time.Second)

				if err == nil && resp.Type == "PEERS_LIST" {
					select {
					case <-cycle.quitChan:
						return
					default:
					}

					var peerList []string
					b, _ := json.Marshal(resp.Payload)
					if json.Unmarshal(b, &peerList) == nil {
						for _, newPeer := range peerList {
							select {
							case <-cycle.quitChan:
								return
							default:
							}
							if !p.isPeerUnreachable(newPeer) && !p.isSelfPeerAddress(newPeer) {
								_ = p.AddPeer(newPeer)
							}
						}
					}
				}

				// 2. Kademlia DHT neighborhood exploration
				select {
				case <-cycle.quitChan:
					return
				default:
				}

				randTarget := GenerateRandomTarget()
				dhtResp, err := p.QueryPeer(target, Message{
					NetworkID: p.GetNetworkID(),
					Type:      "DHT_FIND_NODE",
					Payload:   hex.EncodeToString(randTarget[:]),
				}, 4*time.Second)

				if err == nil && dhtResp.Type == "DHT_NODES" {
					select {
					case <-cycle.quitChan:
						return
					default:
					}

					var discovered []DHTNodeInfo
					b, _ := json.Marshal(dhtResp.Payload)
					if json.Unmarshal(b, &discovered) == nil {
						for _, node := range discovered {
							select {
							case <-cycle.quitChan:
								return
							default:
							}
							if !p.isPeerUnreachable(node.Address) && !p.isSelfPeerAddress(node.Address) {
								p.DHT.Update(node.Address)
								_ = p.AddPeer(node.Address)
							}
						}
					}
				}
			}(peer)
		}
	}
}

// startSyncWorker periodically syncs chainwork with known peers
func (p *PeerServer) startSyncWorker(cycle *serverCycle) {
	defer cycle.wg.Done()

	select {
	case <-time.After(2 * time.Second):
	case <-cycle.quitChan:
		return
	}

	select {
	case <-cycle.quitChan:
		return
	default:
		p.SyncBestPeer()
	}

	syncTicker := time.NewTicker(15 * time.Second)
	defer syncTicker.Stop()

	syncCycle := 0
	for {
		select {
		case <-syncTicker.C:
			// Isolated (empty address book) or periodic refresh: re-dial seeds so a node that lost
			// its internet link rejoins the network without a restart.
			if len(p.GetPeers()) == 0 {
				p.clearAllPeerUnreachable()
				p.ReseedPeers()
			} else if syncCycle%4 == 3 {
				p.ReseedPeers()
			}
			p.SyncBestPeer()
			syncCycle++
			if syncCycle%2 == 0 {
				p.SyncMempool()
				p.RebroadcastMempool()
			}
		case <-cycle.quitChan:
			return
		}
	}
}

// StartServer starts the hardened P2P TCP socket listener [P1-A Lifecycle Hardening].
func (p *PeerServer) StartServer() {
	p.lifecycleLock.Lock()
	if p.state != ServerStateStopped {
		p.lifecycleLock.Unlock()
		return // Idempotent: already starting, running, or stopping
	}

	p.Lock.RLock()
	initErr := p.peerStoreInitErr
	p.Lock.RUnlock()
	if initErr != nil {
		p.lifecycleLock.Unlock()
		fmt.Printf("[P2P] CRITICAL: Refusing to start server due to peer store initialization failure: %v\n", initErr)
		return
	}

	p.currentCycle++
	cycle := &serverCycle{
		id:          p.currentCycle,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	p.activeCycle = cycle

	p.stopLock.Lock()
	p.quitChan = cycle.quitChan
	p.serverDone = cycle.doneChan
	p.stopLock.Unlock()

	p.state = ServerStateStarting
	p.serverStarted = false
	p.lifecycleLock.Unlock()

	if serverStartingHook != nil {
		serverStartingHook()
	}

	listener, err := net.Listen("tcp", ":"+p.Port)
	if err != nil {
		p.lifecycleLock.Lock()
		if p.activeCycle == cycle {
			cycle.cancelOnce.Do(func() { close(cycle.quitChan) })
			p.state = ServerStateStopped
			p.serverStarted = false
			p.activeCycle = nil
			cycle.doneOnce.Do(func() { close(cycle.doneChan) })
		}
		p.lifecycleLock.Unlock()

		p.stopLock.Lock()
		p.listener = nil
		p.stopLock.Unlock()

		UnregisterPeerServer(p)
		fmt.Printf("[P2P] Failed to listen on port %s: %v\n", p.Port, err)
		return
	}

	p.lifecycleLock.Lock()
	// Check if Stop was called concurrently while binding
	select {
	case <-cycle.quitChan:
		p.lifecycleLock.Unlock()
		_ = listener.Close()
		p.teardownCycle(cycle, nil)
		return
	default:
	}

	if p.state != ServerStateStarting || p.activeCycle != cycle {
		p.lifecycleLock.Unlock()
		_ = listener.Close()
		p.teardownCycle(cycle, nil)
		return
	}

	p.stopLock.Lock()
	p.listener = listener
	p.stopLock.Unlock()

	p.state = ServerStateRunning
	p.serverStarted = true
	RegisterPeerServer(p)
	p.lifecycleLock.Unlock()

	p.runCycle(cycle, listener)
}

func (p *PeerServer) runCycle(cycle *serverCycle, listener net.Listener) {
	defer func() {
		p.teardownCycle(cycle, listener)
	}()

	cycle.wg.Add(1)
	go p.startPeerDiscoveryGossip(cycle)

	cycle.wg.Add(1)
	go p.startSyncWorker(cycle)

	// Keep persistent outbound peer sessions alive.
	cycle.wg.Add(1)
	go p.startPeerSessionHeartbeat(cycle)

	fmt.Printf("[P2P] Node actively listening on port %s (Network ID: %s | Max Peers: %d)...\n", p.Port, p.GetNetworkID(), p.MaxPeers)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-cycle.quitChan:
				return
			default:
			}
			return
		}

		// Register connection in cycle's activeConns before processing
		cycle.connsMu.Lock()
		select {
		case <-cycle.quitChan:
			cycle.connsMu.Unlock()
			_ = conn.Close()
			return
		default:
			cycle.activeConns[conn] = struct{}{}
			cycle.connsMu.Unlock()
		}

		remoteHost := extractHost(conn.RemoteAddr().String())
		// Regtest profile strictly rejects non-loopback peers [L4-8]
		if canon, _, err := parseNetworkID(p.GetNetworkID()); err == nil && canon == "ARMX-REGTEST-1" {
			if remoteHost != "127.0.0.1" && remoteHost != "::1" && remoteHost != "localhost" {
				ip := net.ParseIP(remoteHost)
				if ip == nil || !ip.IsLoopback() {
					cycle.connsMu.Lock()
					delete(cycle.activeConns, conn)
					cycle.connsMu.Unlock()
					_ = conn.Close()
					continue
				}
			}
		}

		if p.IsBanned(remoteHost) {
			_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
			_, _ = conn.Write([]byte("ERROR: Peer is banned\n"))
			cycle.connsMu.Lock()
			delete(cycle.activeConns, conn)
			cycle.connsMu.Unlock()
			_ = conn.Close()
			continue
		}

		p.Lock.RLock()
		maxPeers := p.MaxPeers
		disableSubnetLimit := p.DisableSubnetLimit
		requireTLS := p.RequireTLS
		p2pTOFU := p.P2PTOFU
		p.Lock.RUnlock()

		// Enforce maximum concurrent active connections to prevent socket exhaustion
		if atomic.LoadInt32(&p.ActiveConns) >= int32(maxPeers) {
			_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
			_, _ = conn.Write([]byte("ERROR: Connection limit exceeded\n"))
			cycle.connsMu.Lock()
			delete(cycle.activeConns, conn)
			cycle.connsMu.Unlock()
			_ = conn.Close()
			continue
		}

		// Sybil & Eclipse attack prevention: limit connections per subnet [N-12]
		remoteSubnet := extractSubnet(conn.RemoteAddr().String())
		p.connLock.Lock()
		if !disableSubnetLimit && remoteSubnet != "127.0.0.1" && p.subnetConns[remoteSubnet] >= MaxPeersPerSubnet {
			p.connLock.Unlock()
			_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
			_, _ = conn.Write([]byte("ERROR: Subnet connection limit reached\n"))
			cycle.connsMu.Lock()
			delete(cycle.activeConns, conn)
			cycle.connsMu.Unlock()
			_ = conn.Close()
			continue
		}
		p.subnetConns[remoteSubnet]++
		p.connLock.Unlock()

		atomic.AddInt32(&p.ActiveConns, 1)

		// Opportunistic or strict TLS 1.3 Transport Upgrade [N-08, F1]
		secureConn, isTLS := UpgradeToEncryptedTransportWithConfig(conn, p.tlsConfig)
		if secureConn != nil && (!(requireTLS || p2pTOFU) || isTLS) {
			debugLogf(
				"[P2P][CONN] inbound established remote=%s tls=%t active=%d",
				conn.RemoteAddr().String(),
				isTLS,
				atomic.LoadInt32(&p.ActiveConns),
			)
		}
		if secureConn == nil || ((requireTLS || p2pTOFU) && !isTLS) {
			if secureConn != nil {
				_ = secureConn.Close()
			} else {
				_ = conn.Close()
			}
			cycle.connsMu.Lock()
			delete(cycle.activeConns, conn)
			cycle.connsMu.Unlock()

			atomic.AddInt32(&p.ActiveConns, -1)
			p.connLock.Lock()
			p.subnetConns[remoteSubnet]--
			if p.subnetConns[remoteSubnet] <= 0 {
				delete(p.subnetConns, remoteSubnet)
			}
			p.connLock.Unlock()
			continue
		}

		// Update activeConns from raw conn to secureConn
		cycle.connsMu.Lock()
		delete(cycle.activeConns, conn)
		select {
		case <-cycle.quitChan:
			cycle.connsMu.Unlock()
			_ = secureConn.Close()
			atomic.AddInt32(&p.ActiveConns, -1)
			p.connLock.Lock()
			p.subnetConns[remoteSubnet]--
			if p.subnetConns[remoteSubnet] <= 0 {
				delete(p.subnetConns, remoteSubnet)
			}
			p.connLock.Unlock()
			return
		default:
			cycle.activeConns[secureConn] = struct{}{}
			cycle.connsMu.Unlock()
		}

		cycle.wg.Add(1)
		go func(sc net.Conn, sn string) {
			defer func() {
				cycle.connsMu.Lock()
				delete(cycle.activeConns, sc)
				cycle.connsMu.Unlock()
				cycle.wg.Done()
			}()
			p.HandleConnection(sc, sn)
		}(secureConn, remoteSubnet)
	}
}

func (p *PeerServer) teardownCycle(cycle *serverCycle, listener net.Listener) {
	// 1. Ensure cycle quitChan is closed
	cycle.cancelOnce.Do(func() {
		close(cycle.quitChan)
	})

	// 2. Ensure listener is closed (respecting Level 1 lifecycleLock -> Level 5 stopLock hierarchy)
	p.lifecycleLock.RLock()
	isActive := (p.activeCycle == cycle)
	p.stopLock.Lock()
	if listener != nil {
		_ = listener.Close()
	}
	if isActive && p.listener != nil {
		_ = p.listener.Close()
		p.listener = nil
	}
	p.stopLock.Unlock()
	p.lifecycleLock.RUnlock()

	// 3. Close all remaining active connections to unblock in-flight HandleConnection readers
	cycle.connsMu.Lock()
	for c := range cycle.activeConns {
		_ = c.Close()
	}
	cycle.connsMu.Unlock()

	// Close all outbound sessions
	p.closeOutboundSessions(cycle)

	// 4. Wait for all cycle workers to drain
	cycle.wg.Wait()

	// 5. Unregister from registry and transition state only if this cycle is still the active owner!
	p.lifecycleLock.Lock()
	if p.activeCycle == cycle {
		UnregisterPeerServer(p)
		p.state = ServerStateStopped
		p.serverStarted = false
		p.activeCycle = nil
	}
	p.lifecycleLock.Unlock()

	if serverCycleCleanupHook != nil {
		serverCycleCleanupHook(cycle.id)
	}

	// 6. Signal completion to any Stop() waiters
	cycle.doneOnce.Do(func() {
		close(cycle.doneChan)
	})
}

// rawInboundMessage avoids double JSON serialization/deserialization cycles [N-09]
type rawInboundMessage struct {
	NetworkID string          `json:"network_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// readBoundedLine reads a newline-terminated string up to maxBytes.
// If the stream exceeds maxBytes before encountering a newline, it immediately returns an error [N-13].
func readBoundedLine(r *bufio.Reader, maxBytes int) (string, error) {
	var buf strings.Builder
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return "", err
		}
		if buf.Len()+len(chunk) > maxBytes {
			return "", fmt.Errorf("message size exceeds hard limit of %d bytes", maxBytes)
		}
		buf.Write(chunk)
		if !isPrefix {
			break
		}
	}
	return buf.String(), nil
}

// HandleConnection processes incoming P2P messages in a persistent stream loop [N-02]
func (p *PeerServer) HandleConnection(conn net.Conn, subnet string) {
	defer func() {
		debugLogf(
			"[P2P][CONN] inbound closed remote=%s active_before=%d",
			conn.RemoteAddr().String(),
			atomic.LoadInt32(&p.ActiveConns),
		)
		_ = conn.Close()
		atomic.AddInt32(&p.ActiveConns, -1)
		p.connLock.Lock()
		p.subnetConns[subnet]--
		if p.subnetConns[subnet] <= 0 {
			delete(p.subnetConns, subnet)
		}
		p.connLock.Unlock()
	}()

	reader := bufio.NewReader(conn)
	remoteAddr := conn.RemoteAddr().String()
	remoteIP := extractHost(remoteAddr)
	if p.IsBanned(remoteIP) {
		return
	}

	readTimeout := SocketTimeoutDuration

	// Keep reading messages until client disconnects or times out [N-02]
	for {
		_ = conn.SetDeadline(time.Now().Add(readTimeout))
		messageStr, err := readBoundedLine(reader, MaxIncomingPacketSize)
		if err != nil {
			// Suppress expected EOF, timeout, and peer reset disconnects to prevent log spam.
			if !isBenignDisconnectErr(err) {
				debugLogf(
					"[P2P][CONN] inbound read failed remote=%s error=%v",
					conn.RemoteAddr().String(),
					err,
				)
			}
			return
		}

		trimmed := strings.TrimSpace(messageStr)
		if len(trimmed) == 0 {
			continue
		}

		var msg rawInboundMessage
		if err := json.Unmarshal([]byte(trimmed), &msg); err != nil {
			p.AddBanScore(remoteIP, 10)
			continue // Discard malformed packet
		}

		// Network protocol magic validation (drop cross-chain / alien packets) [C-33]
		if msg.NetworkID != p.GetNetworkID() {
			p.AddBanScore(remoteIP, 10)
			continue
		}

		// Rate limiting and abusive behavior ban score [N-15, T1].
		if p.IsBanned(remoteIP) {
			continue
		}
		if !p.AllowMessage(remoteIP, msg.Type) {
			continue
		}

		// Extend socket read deadline once peer is authenticated on matching network ID to prevent disconnect/reconnect churn.
		if readTimeout != AuthenticatedIdleTimeout {
			readTimeout = AuthenticatedIdleTimeout
			if remoteIP != "" && p.Port != "" {
				if ip := net.ParseIP(remoteIP); ip != nil && !ip.IsLoopback() {
					p.verifyAndAddInboundPeer(net.JoinHostPort(remoteIP, p.Port))
				}
			}
		}

		switch msg.Type {
		case "NEW_BLOCK":
			var block core.Block
			if len(msg.Payload) == 0 || json.Unmarshal(msg.Payload, &block) != nil {
				continue
			}
			p.ProcessIncomingBlock(block, remoteIP, remoteAddr)

		case "GET_HEIGHT":
			p.Blockchain.Lock.RLock()
			latest := p.Blockchain.GetLatestBlockUnsafe()
			cw := p.Blockchain.GetTotalChainworkUnsafe().String()
			p.Blockchain.Lock.RUnlock()

			respData, err := json.Marshal(Message{
				NetworkID: p.GetNetworkID(),
				Type:      "HEIGHT_RESPONSE",
				Payload:   HeightResponse{Height: latest.Index, LatestHash: latest.Hash, Chainwork: cw},
			})
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
				_, _ = conn.Write(append(respData, '\n'))
			}

		case "GET_BLOCK_RANGE":
			var req BlockRangeRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil && req.FromIndex >= 0 && req.ToIndex >= req.FromIndex {
				if req.ToIndex-req.FromIndex >= SyncChunkSize {
					req.ToIndex = req.FromIndex + SyncChunkSize - 1
				}

				var blocks []core.Block
				p.Blockchain.Lock.RLock()
				chainLen := len(p.Blockchain.Chain)
				end := req.ToIndex
				if end >= chainLen {
					end = chainLen - 1
				}

				totalBytes := 0
				for i := req.FromIndex; i <= end && i < chainLen; i++ {
					blk := p.Blockchain.Chain[i]
					blkSize := blk.Size()
					// Bounding total chunk size to MaxBlockRangeResponseBytes >= MaxBlockSize [P2-4]
					// Always return at least 1 block even if single block exceeds byte threshold
					if len(blocks) > 0 && totalBytes+blkSize > MaxBlockRangeResponseBytes {
						end = i - 1
						break
					}
					totalBytes += blkSize
					blocks = append(blocks, blk)
				}
				p.Blockchain.Lock.RUnlock()

				// Send response containing the real upper bound (end) processed
				respData, err := json.Marshal(Message{
					NetworkID: p.GetNetworkID(),
					Type:      "BLOCK_RANGE_RESPONSE",
					Payload:   BlockRangeResponse{FromIndex: req.FromIndex, ToIndex: end, Blocks: blocks},
				})
				if err == nil {
					_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
					_, _ = conn.Write(append(respData, '\n'))
				}
			}

		case "GET_FORKPOINT":
			var req ForkpointRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil && len(req.Locators) > 0 {
				// Bound max evaluated locators to 64 [T6]
				if len(req.Locators) > 64 {
					req.Locators = req.Locators[:64]
				}

				p.Blockchain.Lock.RLock()
				ancestorHeight := -1
				var ancestorHash string

				// Find highest matching locator in local chain using O(1) store index lookup [T6]
				for _, loc := range req.Locators {
					cleanLoc := strings.ToLower(strings.TrimSpace(loc))
					if len(cleanLoc) != 64 {
						continue
					}
					if b, err := p.Blockchain.GetBlockByHash(cleanLoc); err == nil && b != nil {
						if b.Index < len(p.Blockchain.Chain) && p.Blockchain.Chain[b.Index].Hash == b.Hash {
							ancestorHeight = b.Index
							ancestorHash = b.Hash
							break
						}
					}
				}
				if ancestorHeight == -1 {
					ancestorHeight = 0
					if len(p.Blockchain.Chain) > 0 {
						ancestorHash = p.Blockchain.Chain[0].Hash
					} else {
						ancestorHash = core.GenesisHash
					}
				}
				p.Blockchain.Lock.RUnlock()

				respData, err := json.Marshal(Message{
					NetworkID: p.GetNetworkID(),
					Type:      "FORKPOINT_RESPONSE",
					Payload: ForkpointResponse{
						AncestorHeight: ancestorHeight,
						AncestorHash:   ancestorHash,
					},
				})
				if err == nil {
					_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
					_, _ = conn.Write(append(respData, '\n'))
				}
			}

		case "NEW_TX":
			var tx core.Transaction
			if len(msg.Payload) == 0 || json.Unmarshal(msg.Payload, &tx) != nil {
				continue
			}
			p.ProcessIncomingTx(tx, remoteIP)

		case "GET_MEMPOOL":
			if p.Blockchain == nil {
				continue
			}
			p.Blockchain.Lock.RLock()
			totalMp := len(p.Blockchain.Mempool)
			mempoolTxs := make([]core.Transaction, totalMp)
			copy(mempoolTxs, p.Blockchain.Mempool)
			p.Blockchain.Lock.RUnlock()

			if len(mempoolTxs) > 100 {
				mempoolTxs = mempoolTxs[:100]
			}
			respData, err := json.Marshal(Message{
				NetworkID: p.GetNetworkID(),
				Type:      "MEMPOOL_RESPONSE",
				Payload:   mempoolTxs,
			})
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
				_, _ = conn.Write(append(respData, '\n'))
			}

		case "MEMPOOL_RESPONSE":
			var txList []core.Transaction
			if len(msg.Payload) > 0 && json.Unmarshal(msg.Payload, &txList) == nil {
				// Bound max incoming txs per MEMPOOL_RESPONSE to 100 [T5]
				if len(txList) > 100 {
					txList = txList[:100]
				}
				for _, tx := range txList {
					p.ProcessIncomingTx(tx, remoteIP)
				}
			}

		case "GET_PEERS":
			allPeers := p.GetPeers()
			peers := make([]string, 0, len(allPeers))
			for _, candidate := range allPeers {
				if p.isPeerUnreachable(candidate) || p.isSelfPeerAddress(candidate) {
					continue
				}
				if remoteIP != "" && remoteIP != "127.0.0.1" && remoteIP != "::1" && extractHost(candidate) == remoteIP {
					continue
				}
				peers = append(peers, candidate)
			}
			if len(peers) > MaxPeersPerResponse {
				peers = peers[:MaxPeersPerResponse]
			}
			respData, err := json.Marshal(Message{
				NetworkID: p.GetNetworkID(),
				Type:      "PEERS_LIST",
				Payload:   peers,
			})
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
				_, _ = conn.Write(append(respData, '\n'))
			}

		case "PEERS_LIST":
			var peerList []string
			if json.Unmarshal(msg.Payload, &peerList) == nil {
				for _, newPeer := range peerList {
					trimmedPeer := strings.TrimSpace(newPeer)
					if trimmedPeer == "" {
						continue
					}
					host, portStr, splitErr := net.SplitHostPort(trimmedPeer)
					if splitErr == nil && (host == "" || host == "0.0.0.0" || host == "::") && portStr != "" {
						// Self-announcement from remote peer: combine remoteIP with announced listening port
						// and verify reachability asynchronously before adding to address book.
						candidate := net.JoinHostPort(remoteIP, portStr)
						p.verifyAndAddInboundPeer(candidate)
						continue
					}
					if !p.isPeerUnreachable(trimmedPeer) && !p.isSelfPeerAddress(trimmedPeer) {
						_ = p.AddPeer(trimmedPeer)
					}
				}
			}

		case "DHT_PING":
			respData, err := json.Marshal(Message{
				NetworkID: p.GetNetworkID(),
				Type:      "DHT_PONG",
				Payload:   p.DHT.FindClosestNodes(p.DHT.LocalNode.ID, 8),
			})
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
				_, _ = conn.Write(append(respData, '\n'))
			}

		case "DHT_FIND_NODE":
			var targetHex string
			_ = json.Unmarshal(msg.Payload, &targetHex)
			var targetID [32]byte
			if tBytes, err := hex.DecodeString(targetHex); err == nil && len(tBytes) == 32 {
				copy(targetID[:], tBytes)
			} else {
				targetID = p.DHT.LocalNode.ID
			}
			closest := p.DHT.FindClosestNodes(targetID, KademliaK)
			respData, err := json.Marshal(Message{
				NetworkID: p.GetNetworkID(),
				Type:      "DHT_NODES",
				Payload:   closest,
			})
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(SocketTimeoutDuration))
				_, _ = conn.Write(append(respData, '\n'))
			}

		case "DHT_NODES":
			var discovered []DHTNodeInfo
			if err := json.Unmarshal(msg.Payload, &discovered); err == nil {
				for _, node := range discovered {
					if !p.isPeerUnreachable(node.Address) && !p.isSelfPeerAddress(node.Address) {
						p.DHT.Update(node.Address)
						_ = p.AddPeer(node.Address)
					}
				}
			}
		}
	}
}

// ResolveDNSSeed resolves a domain-based or IP-based seed address (host:port) with timeout [N-11]
func ResolveDNSSeed(seedAddr string) []string {
	seedAddr = strings.TrimSpace(seedAddr)
	if seedAddr == "" {
		return nil
	}
	host, portStr, err := net.SplitHostPort(seedAddr)
	if err != nil {
		return []string{seedAddr}
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{seedAddr}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return []string{seedAddr}
	}

	fmt.Printf("[DNS Seed] Resolved '%s' to %d peer address(es): %v\n", host, len(ips), ips)

	var results []string
	seen := make(map[string]bool)
	for _, ip := range ips {
		addr := net.JoinHostPort(ip, portStr)
		if !seen[addr] {
			seen[addr] = true
			results = append(results, addr)
		}
	}
	if len(results) == 0 && !seen[seedAddr] {
		results = append(results, seedAddr)
	}
	return results
}

// AddPeer registers a new peer address and updates the peer registry file [N-05, N-16, P1-A].
func (p *PeerServer) AddPeer(peerAddress string) error {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()

	if peerAddress == "" || p.isSelfPeerAddress(peerAddress) {
		return nil
	}
	host, portStr, err := net.SplitHostPort(peerAddress)
	if err != nil || host == "" || portStr == "" {
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil
	}

	// [N-05, N-16] Filter loopback and private IP addresses in public production mode
	ip := net.ParseIP(host)
	if ip != nil {
		p.Lock.RLock()
		allowPrivate := p.AllowPrivatePeers
		p.Lock.RUnlock()
		isPrivateOrLoopback := ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
		if isPrivateOrLoopback && !allowPrivate {
			return nil
		}
	}

	// Regtest profile strictly rejects non-loopback peers [L4-8]
	if canon, _, err := parseNetworkID(p.GetNetworkID()); err == nil && canon == "ARMX-REGTEST-1" {
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			if ip == nil || !ip.IsLoopback() {
				return nil
			}
		}
	}

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	p.Lock.Lock()

	if p.newPeers == nil {
		p.newPeers = make(map[string]*PeerInfo)
	}
	if p.triedPeers == nil {
		p.triedPeers = make(map[string]*PeerInfo)
	}

	// Do not duplicate
	if _, exists := p.triedPeers[peerAddress]; exists {
		p.Lock.Unlock()
		return nil
	}
	if _, exists := p.newPeers[peerAddress]; exists {
		p.Lock.Unlock()
		return nil
	}

	// [N-16] Address book capacity check (MaxAddressBookSize = 1000)
	if len(p.newPeers)+len(p.triedPeers) >= MaxAddressBookSize {
		p.Lock.Unlock()
		return nil
	}

	// Sybil / Eclipse attack defense: limit max peers from the same /24 subnet [N-12]
	subnet := extractSubnet(peerAddress)
	if !p.DisableSubnetLimit && subnet != "127.0.0.1" {
		count := 0
		for existing := range p.triedPeers {
			if extractSubnet(existing) == subnet {
				count++
			}
		}
		for existing := range p.newPeers {
			if extractSubnet(existing) == subnet {
				count++
			}
		}
		if count >= MaxPeersPerSubnet {
			p.Lock.Unlock()
			return nil
		}
	}

	p.newPeers[peerAddress] = &PeerInfo{
		Address:   peerAddress,
		LastSeen:  time.Now(),
		NetworkID: p.getNetworkIDLocked(),
	}
	p.rebuildPeersListLocked()
	p.DHT.Update(peerAddress)
	p.Lock.Unlock()

	if fn := getPeerStoreMutationHook(); fn != nil {
		fn("AddPeer", peerAddress)
	}

	if err := p.savePeersLocked(); err != nil {
		p.Lock.Lock()
		delete(p.newPeers, peerAddress)
		p.rebuildPeersListLocked()
		p.Lock.Unlock()
		return fmt.Errorf("failed to persist peer store on AddPeer for %s: %w", peerAddress, err)
	}

	return nil
}

// GetPeers returns a copy of known peer addresses
func (p *PeerServer) GetPeers() []string {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	peersCopy := make([]string, len(p.Peers))
	copy(peersCopy, p.Peers)
	return peersCopy
}

// GetPeersActive returns the number of active live inbound and outbound peer connections [L4-8]
func (p *PeerServer) GetPeersActive() int {
	active := atomic.LoadInt32(&p.ActiveConns)
	if active < 0 {
		return 0
	}
	return int(active)
}

// TrackActiveConn registers an active inbound or outbound connection and returns a cleanup func [L4-8]
func (p *PeerServer) TrackActiveConn() func() {
	atomic.AddInt32(&p.ActiveConns, 1)

	var releaseOnce sync.Once

	return func() {
		releaseOnce.Do(func() {
			atomic.AddInt32(&p.ActiveConns, -1)
		})
	}
}

// FingerprintProvider allows connection wrappers or test mocks to expose TLS pubkey fingerprints
type FingerprintProvider interface {
	GetCertFingerprint() string
}

// extractCertPubkeyFingerprint extracts the SHA-256 fingerprint of the remote TLS certificate's Subject Public Key Info [F1]
func extractCertPubkeyFingerprint(conn net.Conn) string {
	if fpProvider, ok := conn.(FingerprintProvider); ok {
		return fpProvider.GetCertFingerprint()
	}
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return ""
	}
	state := tlsConn.ConnectionState()
	if !state.HandshakeComplete {
		_ = tlsConn.Handshake()
		state = tlsConn.ConnectionState()
	}
	if len(state.PeerCertificates) == 0 {
		return ""
	}
	hash := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	return hex.EncodeToString(hash[:])
}

// VerifyOrRecordTOFU implements Trust-On-First-Use (TOFU) certificate fingerprint pinning [F1, Phase 5, P1 Final Fix].
// It acquires lifecycleLock in read mode and delegates to verifyOrRecordTOFULocked.
//
// Concurrency & Durability Guarantees:
//  1. Per-peer serialization: Concurrent first-contact attempts to the same peer wait for the initial
//     persistence transaction to complete. A concurrent caller never succeeds on an uncommitted in-memory pin.
//  2. Strict fingerprint matching: Conflicting fingerprints presented concurrently or after failure are rejected.
//  3. No silent repinning: If persistence fails for a fingerprint, alternate certificates are strictly rejected.
//  4. Fallback to unencrypted plaintext is strictly forbidden when P2PTOFU is enabled.
func (p *PeerServer) VerifyOrRecordTOFU(peerAddr string, conn net.Conn) error {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()

	return p.verifyOrRecordTOFULocked(peerAddr, conn)
}

// verifyOrRecordTOFULocked performs the core TOFU verification, in-memory staging, and durable save transaction.
// Precondition: Caller MUST already hold p.lifecycleLock (RLock or Lock). This avoids recursive RLock acquisitions
// on sync.RWMutex which could deadlock when a writer (e.g. SetDataDir/SetNetworkID) is waiting.
func (p *PeerServer) verifyOrRecordTOFULocked(peerAddr string, conn net.Conn) error {
	p.Lock.RLock()
	p2pTOFU := p.P2PTOFU
	p.Lock.RUnlock()
	if !p2pTOFU {
		return nil // Plaintext or unverified mode explicitly configured
	}

	fp := extractCertPubkeyFingerprint(conn)
	if fp == "" {
		return fmt.Errorf("P2P TOFU fail-closed: peer %s connection lacks valid TLS certificate fingerprint", peerAddr)
	}

	p.tofuLock.Lock()

	// 1. Never permit an alternate cert to be silently repinned after a failed save
	if failedFP, hasFailed := p.failedPins[peerAddr]; hasFailed {
		if fp != failedFP {
			p.tofuLock.Unlock()
			return fmt.Errorf("P2P TOFU fail-closed: peer %s previously failed persistence with fingerprint %s; certificate fingerprint mismatch (refusing alternate %s)", peerAddr, failedFP, fp)
		}
	}

	// 2. Wait for any concurrent in-flight first-contact persistence transaction
	for {
		pending, inFlight := p.pendingTOFU[peerAddr]
		if !inFlight {
			break
		}
		// Conflicting fingerprint while another is pending
		if pending.fp != fp {
			p.tofuLock.Unlock()
			return fmt.Errorf("P2P TOFU fail-closed: peer %s presented conflicting fingerprint %s while %s is pending (certificate fingerprint mismatch)", peerAddr, fp, pending.fp)
		}

		// Same fingerprint pending: wait for durable commit
		doneChan := pending.done
		p.tofuLock.Unlock()
		<-doneChan

		p.tofuLock.Lock()
		if pending.err != nil {
			p.tofuLock.Unlock()
			return fmt.Errorf("P2P TOFU fail-closed: first-contact persistence for peer %s failed: %w", peerAddr, pending.err)
		}
	}

	// 3. Check if peer is already durably committed in triedPeers
	p.Lock.RLock()
	var triedFP string
	if info, exists := p.triedPeers[peerAddr]; exists && info != nil && info.CertFingerprint != "" {
		triedFP = info.CertFingerprint
	}
	p.Lock.RUnlock()

	if triedFP != "" {
		p.tofuLock.Unlock()
		if triedFP != fp {
			return fmt.Errorf("P2P TOFU verification failed for peer %s: certificate fingerprint mismatch (pinned %s, got %s)", peerAddr, triedFP, fp)
		}
		p.Lock.Lock()
		if info := p.triedPeers[peerAddr]; info != nil {
			info.LastSeen = time.Now()
		}
		p.Lock.Unlock()
		return nil
	}

	// If peer exists in newPeers with a fingerprint, check for mismatch
	p.Lock.RLock()
	var newFP string
	if info, exists := p.newPeers[peerAddr]; exists && info != nil && info.CertFingerprint != "" {
		newFP = info.CertFingerprint
	}
	p.Lock.RUnlock()

	if newFP != "" && newFP != fp {
		p.tofuLock.Unlock()
		return fmt.Errorf("P2P TOFU verification failed for peer %s: certificate fingerprint mismatch (pinned %s, got %s)", peerAddr, newFP, fp)
	}

	// 4. Fail-closed gate: if peer store is corrupt, refuse to pin
	if p.IsPeerStoreCorrupted() {
		p.tofuLock.Unlock()
		return fmt.Errorf("P2P TOFU fail-closed: peer store is in corrupt state; refusing to bootstrap or re-pin certificate for %s until store is restored", peerAddr)
	}

	// 5. Register pending pin so concurrent callers wait on this durable persistence transaction
	currentPending := &pendingPin{
		fp:   fp,
		done: make(chan struct{}),
	}
	if p.pendingTOFU == nil {
		p.pendingTOFU = make(map[string]*pendingPin)
	}
	p.pendingTOFU[peerAddr] = currentPending
	p.tofuLock.Unlock()

	var saveErr error
	defer func() {
		p.tofuLock.Lock()
		currentPending.err = saveErr
		close(currentPending.done)
		delete(p.pendingTOFU, peerAddr)
		if saveErr != nil {
			if p.failedPins == nil {
				p.failedPins = make(map[string]string)
			}
			p.failedPins[peerAddr] = fp
		} else {
			if p.failedPins != nil {
				delete(p.failedPins, peerAddr)
			}
		}
		p.tofuLock.Unlock()
	}()

	p.savePeersLock.Lock()
	defer p.savePeersLock.Unlock()

	// Stage in memory under p.Lock
	p.Lock.Lock()
	var oldTriedCopy *PeerInfo
	if pi := p.triedPeers[peerAddr]; pi != nil {
		cp := *pi
		oldTriedCopy = &cp
	}
	var oldNewCopy *PeerInfo
	if p.newPeers != nil {
		if pi := p.newPeers[peerAddr]; pi != nil {
			cp := *pi
			oldNewCopy = &cp
		}
	}

	if p.triedPeers == nil {
		p.triedPeers = make(map[string]*PeerInfo)
	}
	p.triedPeers[peerAddr] = &PeerInfo{
		Address:         peerAddr,
		LastSeen:        time.Now(),
		NetworkID:       p.getNetworkIDLocked(),
		CertFingerprint: fp,
	}
	if p.newPeers != nil {
		delete(p.newPeers, peerAddr)
	}
	p.rebuildPeersListLocked()
	p.Lock.Unlock()

	if tofuPreSaveHook != nil {
		tofuPreSaveHook(peerAddr)
	}

	// Perform durable persistence
	saveErr = p.savePeersLocked()
	if saveErr != nil {
		// Roll back in-memory state on persistence failure
		p.Lock.Lock()
		if oldTriedCopy != nil {
			p.triedPeers[peerAddr] = oldTriedCopy
		} else {
			delete(p.triedPeers, peerAddr)
		}
		if oldNewCopy != nil {
			if p.newPeers == nil {
				p.newPeers = make(map[string]*PeerInfo)
			}
			p.newPeers[peerAddr] = oldNewCopy
		} else {
			delete(p.newPeers, peerAddr)
		}
		p.rebuildPeersListLocked()
		p.Lock.Unlock()

		return fmt.Errorf("P2P TOFU fail-closed: failed to persist certificate fingerprint for peer %s: %w", peerAddr, saveErr)
	}

	return nil
}

// IsPeerStoreCorrupted returns true if the peer store failed to load due to malformed data [Follow-up Point 4]
func (p *PeerServer) IsPeerStoreCorrupted() bool {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	return p.peerStoreCorrupted
}

// DialPeer establishes an outbound connection to a peer address and tracks it in ActiveConns [L4-8, Phase 5, P1 Final Fix].
// It holds lifecycleLock in read mode across the entire outbound transaction (configuration snapshot ->
// TLS handshake -> TOFU verification -> durable persistence) to ensure network identity and peer store
// remain consistent throughout the handshake and verification, and cannot be mutated by concurrent setters.
func (p *PeerServer) DialPeer(peerAddress string, timeout time.Duration) (net.Conn, func(), error) {
	p.lifecycleLock.RLock()
	defer p.lifecycleLock.RUnlock()

	p.Lock.RLock()
	if p.peerStoreInitErr != nil {
		err := p.peerStoreInitErr
		p.Lock.RUnlock()
		return nil, nil, fmt.Errorf("P2P TOFU fail-closed: peer store init error: %w", err)
	}
	if p.peerStoreCorrupted && p.P2PTOFU {
		p.Lock.RUnlock()
		return nil, nil, errors.New("P2P TOFU fail-closed: peer store is in corrupt state")
	}
	requireTLS := p.RequireTLS
	p2pTOFU := p.P2PTOFU
	p.Lock.RUnlock()

	allowPlaintext := !requireTLS && !p2pTOFU && !IsStrictTLS()
	conn, isTLS, err := DialEncryptedWithFallback(peerAddress, timeout, allowPlaintext)
	if err != nil {
		return nil, nil, err
	}
	if requireTLS || p2pTOFU {
		if !isTLS {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("P2P TOFU fail-closed: unencrypted plaintext connection to %s rejected when TOFU or TLS is active", peerAddress)
		}
	}

	if dialPreTOFUHook != nil {
		dialPreTOFUHook(peerAddress)
	}

	if p2pTOFU {
		// Call verifyOrRecordTOFULocked which assumes lifecycleLock is ALREADY held by caller.
		// This avoids nested/recursive RLock calls that could deadlock when a writer is waiting.
		if err := p.verifyOrRecordTOFULocked(peerAddress, conn); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}
	release := p.TrackActiveConn()
	var releaseOnce sync.Once
	safeRelease := func() {
		releaseOnce.Do(release)
	}

	return conn, safeRelease, nil
}

func expectedResponseType(reqType string) string {
	switch reqType {
	case "GET_HEIGHT":
		return "HEIGHT_RESPONSE"
	case "GET_BLOCK_RANGE":
		return "BLOCK_RANGE_RESPONSE"
	case "GET_FORKPOINT":
		return "FORKPOINT_RESPONSE"
	case "GET_PEERS":
		return "PEERS_LIST"
	case "DHT_PING":
		return "DHT_PONG"
	case "DHT_FIND_NODE":
		return "DHT_NODES"
	default:
		return ""
	}
}

func isStandardRPCResponseType(respType string) bool {
	switch respType {
	case "HEIGHT_RESPONSE", "BLOCK_RANGE_RESPONSE", "FORKPOINT_RESPONSE", "PEERS_LIST", "DHT_PONG", "DHT_NODES":
		return true
	default:
		return false
	}
}

// QueryPeer sends a message to a peer over a verified, TOFU-validated connection and waits for the response [P1 Final Fix]
func (p *PeerServer) QueryPeer(
	peerAddress string,
	msg Message,
	timeout time.Duration,
) (*Message, error) {
	expectedNetID := msg.NetworkID
	if expectedNetID == "" {
		expectedNetID = p.GetNetworkID()
		msg.NetworkID = expectedNetID
	}

	msgData, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	session, err := p.getOrCreateOutboundSession(peerAddress, timeout)
	if err != nil {
		return nil, err
	}

	line, err := session.roundTrip(msgData, timeout)
	if errors.Is(err, errSessionClosed) {
		// Session was closed by a prior concurrent caller before our request was ever written.
		p.dropOutboundSession(peerAddress, session)
		session, err = p.getOrCreateOutboundSession(peerAddress, timeout)
		if err != nil {
			return nil, err
		}
		line, err = session.roundTrip(msgData, timeout)
	}
	if err != nil {
		debugLogf(
			"[P2P][SESSION] roundTrip failed peer=%s err=%v active=%d",
			peerAddress,
			err,
			p.GetPeersActive(),
		)
		p.dropOutboundSession(peerAddress, session)

		if p.isShuttingDown() {
			return nil, err
		}

		// Reconnect only to verify TLS and the stored TOFU fingerprint.
		// Do not resend the original message because the peer may have processed it.
		probeConn, probeRelease, probeErr := p.DialPeer(peerAddress, timeout)
		if probeErr != nil {
			return nil, probeErr
		}

		// Reuse the verified probe connection for future requests instead of churning sockets,
		// while preserving the original transport error for this call.
		if probeConn != nil {
			if _, regErr := p.registerOutboundConnSession(peerAddress, probeConn, probeRelease); regErr != nil {
				_ = probeConn.Close()
				if probeRelease != nil {
					probeRelease()
				}
			}
		} else if probeRelease != nil {
			probeRelease()
		}

		// The peer's identity was valid; preserve the original transport error.
		return nil, err
	}

	var resp Message
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		debugLogf(
			"[P2P][SESSION] invalid response peer=%s err=%v active=%d",
			peerAddress,
			err,
			p.GetPeersActive(),
		)
		p.dropOutboundSession(peerAddress, session)
		return nil, err
	}

	// Drain stale buffered RPC responses if an earlier RPC left a known response type in the stream.
	expType := expectedResponseType(msg.Type)
	for attempt := 0; expType != "" && resp.Type != expType && isStandardRPCResponseType(resp.Type) && attempt < 2; attempt++ {
		nextLine, nextErr := session.readNextLine(timeout)
		if nextErr != nil {
			p.dropOutboundSession(peerAddress, session)
			break
		}
		var nextResp Message
		if json.Unmarshal([]byte(strings.TrimSpace(nextLine)), &nextResp) != nil {
			p.dropOutboundSession(peerAddress, session)
			break
		}
		resp = nextResp
	}

	if resp.NetworkID != expectedNetID {
		debugLogf(
			"[P2P][SESSION] network ID mismatch peer=%s expected=%s got=%s active=%d",
			peerAddress,
			expectedNetID,
			resp.NetworkID,
			p.GetPeersActive(),
		)
		p.dropOutboundSession(peerAddress, session)
		return nil, fmt.Errorf(
			"network ID mismatch: expected %s, got %s",
			expectedNetID,
			resp.NetworkID,
		)
	}

	return &resp, nil
}

// SendMessage sends a message to a peer over a verified, TOFU-validated connection [P1 Final Fix]
func (p *PeerServer) SendMessage(
	peerAddress string,
	msg Message,
) error {
	if msg.NetworkID == "" {
		msg.NetworkID = p.GetNetworkID()
	}

	// If msg.Type is a query type that causes HandleConnection to write back a response,
	// use QueryPeer so the response line is consumed and never desynchronizes the session stream.
	if expectedResponseType(msg.Type) != "" {
		_, err := p.QueryPeer(peerAddress, msg, 5*time.Second)
		return err
	}

	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	session, reused, err := p.getOrCreateOutboundSessionWithStatus(
		peerAddress,
		4*time.Second,
	)
	if err != nil {
		return err
	}

	if err := session.send(msgData, 5*time.Second); err != nil {
		p.dropOutboundSession(peerAddress, session)
		if (!reused && !errors.Is(err, errSessionClosed)) || p.isShuttingDown() {
			return err
		}
		freshSession, _, dialErr := p.getOrCreateOutboundSessionWithStatus(peerAddress, 4*time.Second)
		if dialErr != nil {
			return dialErr
		}
		if sendErr := freshSession.send(msgData, 5*time.Second); sendErr != nil {
			p.dropOutboundSession(peerAddress, freshSession)
			return sendErr
		}
	}

	return nil
}

func (p *PeerServer) startPeerSessionHeartbeat(cycle *serverCycle) {
	defer cycle.wg.Done()

	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()

	sem := make(chan struct{}, MaxSyncConcurrency)

	for {
		select {
		case <-cycle.quitChan:
			return

		case <-ticker.C:
			addresses := p.outboundSessionAddresses(cycle)

			for _, address := range addresses {
				select {
				case <-cycle.quitChan:
					return
				default:
				}

				sess := p.getExistingOutboundSession(address)
				if sess == nil || sess.isClosed() {
					continue
				}
				// Only ping sessions that have been idle for >= 10s to minimize heartbeat traffic & log noise.
				if time.Since(sess.lastActivity()) < 10*time.Second {
					continue
				}

				cycle.wg.Add(1)
				go func(targetAddr string) {
					defer cycle.wg.Done()
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-cycle.quitChan:
						return
					}

					response, err := p.QueryPeer(
						targetAddr,
						Message{
							NetworkID: p.GetNetworkID(),
							Type:      "DHT_PING",
						},
						5*time.Second,
					)
					if err != nil {
						return
					}
					if response.Type != "DHT_PONG" {
						p.dropOutboundSession(
							targetAddr,
							p.getExistingOutboundSession(targetAddr),
						)
					}
				}(address)
			}
		}
	}
}

func (p *PeerServer) getExistingOutboundSession(
	peerAddress string,
) *outboundPeerSession {
	p.outboundMu.Lock()
	defer p.outboundMu.Unlock()

	return p.outboundSessions[peerAddress]
}

// BroadcastBlock broadcasts a block to the specified peers using this PeerServer's TOFU transport [P1-A]
func (p *PeerServer) BroadcastBlock(peerAddresses []string, block core.Block) {
	msg := Message{
		NetworkID: p.GetNetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   block,
	}
	for _, peer := range peerAddresses {
		go func(target string) {
			if err := p.SendMessage(target, msg); err == nil {
				fmt.Printf("[P2P] Broadcasted block #%d to peer %s\n", block.Index, target)
			}
		}(peer)
	}
}

// BroadcastTransaction broadcasts a transaction to the specified peers using this PeerServer's TOFU transport [P1-A]
func (p *PeerServer) BroadcastTransaction(peerAddresses []string, tx core.Transaction) {
	msg := Message{
		NetworkID: p.GetNetworkID(),
		Type:      "NEW_TX",
		Payload:   tx,
	}
	for _, peer := range peerAddresses {
		go func(target string) {
			_ = p.SendMessage(target, msg)
		}(peer)
	}
}

// GetPeersKnown returns the number of known peers in the address book [L4-8]
func (p *PeerServer) GetPeersKnown() int {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	return len(p.Peers)
}

// RelayBlock relays a newly accepted block to all connected peers except sender
func (p *PeerServer) RelayBlock(block core.Block, senderAddr string) {
	p.isBlockSeenOrMark(block.Hash)
	peers := p.GetPeers()
	for _, peer := range peers {
		if !isSamePeerOrHost(peer, senderAddr) {
			go func(target string) {
				_ = p.SendMessage(target, Message{NetworkID: p.GetNetworkID(), Type: "NEW_BLOCK", Payload: block})
			}(peer)
		}
	}
}

// SyncMempool queries connected peers for their unconfirmed transactions to prevent mempool fragmentation [BIP-35]
func (p *PeerServer) SyncMempool() {
	if p.Blockchain == nil || p.isShuttingDown() {
		return
	}
	peers := p.GetPeers()
	if len(peers) == 0 {
		return
	}

	for _, peer := range peers {
		if p.isShuttingDown() {
			return
		}
		resp, err := p.QueryPeer(peer, Message{NetworkID: p.GetNetworkID(), Type: "GET_MEMPOOL"}, 3*time.Second)
		if err == nil && resp != nil && resp.Type == "MEMPOOL_RESPONSE" {
			var txList []core.Transaction
			b, marshalErr := json.Marshal(resp.Payload)
			if marshalErr == nil && json.Unmarshal(b, &txList) == nil {
				for _, tx := range txList {
					p.ProcessIncomingTx(tx, peer)
				}
			}
		}
	}
}

// RebroadcastMempool periodically redistributes unmined local mempool transactions to all peers
func (p *PeerServer) RebroadcastMempool() {
	if p.Blockchain == nil || p.isShuttingDown() {
		return
	}
	p.Blockchain.Lock.RLock()
	if len(p.Blockchain.Mempool) == 0 {
		p.Blockchain.Lock.RUnlock()
		return
	}
	mempoolCopy := make([]core.Transaction, len(p.Blockchain.Mempool))
	copy(mempoolCopy, p.Blockchain.Mempool)
	p.Blockchain.Lock.RUnlock()

	peers := p.GetPeers()
	if len(peers) == 0 {
		return
	}

	for _, tx := range mempoolCopy {
		p.BroadcastTransaction(peers, tx)
	}
}

// RelayTransaction relays a transaction asynchronously to eligible peers.
func (p *PeerServer) RelayTransaction(
	tx core.Transaction,
	senderAddr string,
) {
	txHash := tx.HashHex()

	for _, peer := range p.GetPeers() {
		if isSamePeerOrHost(peer, senderAddr) {
			continue
		}

		target := peer

		claimTime, claimed := p.claimTxRelay(txHash, target)
		if !claimed {
			continue
		}

		go func() {
			msg := Message{
				NetworkID: p.GetNetworkID(),
				Type:      "NEW_TX",
				Payload:   tx,
			}

			if err := p.SendMessage(target, msg); err != nil {
				core.LogDebug(
					core.LogCategoryNet,
					"Failed to relay transaction %s to peer %s: %v",
					txHash,
					target,
					err,
				)

				p.releaseTxRelay(txHash, target, claimTime)
				return
			}

			core.LogDebug(
				core.LogCategoryNet,
				"Relay sent: tx=%s peer=%s",
				txHash,
				target,
			)
		}()
	}
}

// lastDeepReorgWarn rate-limits the stale-branch error to once per 5 minutes (unix seconds).
var lastDeepReorgWarn int64

// SyncBestPeer discovers which peer has the highest chainwork and initiates sync only with that best peer [N-09, N-16]
func (p *PeerServer) SyncBestPeer() {
	if p.Blockchain == nil || p.isShuttingDown() {
		return
	}
	peers := p.GetPeers()
	if len(peers) == 0 {
		return
	}

	p.Blockchain.Lock.RLock()
	localLatest := p.Blockchain.GetLatestBlockUnsafe()
	localChainwork := p.Blockchain.GetTotalChainworkUnsafe()
	p.Blockchain.Lock.RUnlock()

	var mu sync.Mutex
	var failedPeers []string
	succeeded := 0
	markFailed := func(addr string) {
		mu.Lock()
		failedPeers = append(failedPeers, addr)
		mu.Unlock()
	}
	var bestPeer string
	bestChainwork := new(big.Int).Set(localChainwork)
	bestHeight := localLatest.Index

	sem := make(chan struct{}, MaxSyncConcurrency)
	var wg sync.WaitGroup

	for _, peer := range peers {
		sem <- struct{}{}
		wg.Add(1)
		go func(peerAddr string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			if p.isShuttingDown() {
				return
			}
			resp, err := p.QueryPeer(peerAddr, Message{NetworkID: p.GetNetworkID(), Type: "GET_HEIGHT"}, 3*time.Second)
			if err != nil {
				if p.isShuttingDown() {
					return
				}
				debugLogf("[SYNC] GET_HEIGHT failed peer=%s err=%v", peerAddr, err)
				markFailed(peerAddr)
				return
			}
			if resp == nil || resp.Type != "HEIGHT_RESPONSE" {
				core.LogWarn(core.LogCategorySync, "Unexpected height response peer=%s resp=%+v", peerAddr, resp)
				if rerr := p.RecordPeerFailure(peerAddr); rerr != nil {
					core.LogDebug(core.LogCategoryNet, "Failed to persist peer store on failure for %s: %v", peerAddr, rerr)
				}
				return
			}
			var hr HeightResponse
			b, _ := json.Marshal(resp.Payload)
			if json.Unmarshal(b, &hr) != nil {
				if rerr := p.RecordPeerFailure(peerAddr); rerr != nil {
					core.LogDebug(core.LogCategoryNet, "Failed to persist peer store on failure for %s: %v", peerAddr, rerr)
				}
				return
			}

			// Handshake / query success: promote peer to tried table and persist [N-16]
			if err := p.RecordPeerSuccess(peerAddr, hr.Height, resp.NetworkID); err != nil {
				core.LogDebug(core.LogCategoryNet, "Failed to persist peer store on success for %s: %v", peerAddr, err)
			}

			peerCW, ok := new(big.Int).SetString(hr.Chainwork, 10)
			if !ok {
				peerCW = big.NewInt(0)
			}

			mu.Lock()
			succeeded++
			if peerCW.Cmp(bestChainwork) > 0 || (peerCW.Cmp(bestChainwork) == 0 && hr.Height > bestHeight) {
				bestChainwork = peerCW
				bestHeight = hr.Height
				bestPeer = peerAddr
			}
			mu.Unlock()
		}(peer)
	}
	wg.Wait()

	// If every peer failed, our own connectivity is the likely culprit: do not punish the peers.
	if succeeded > 0 {
		for _, addr := range failedPeers {
			if rerr := p.RecordPeerFailure(addr); rerr != nil {
				core.LogDebug(core.LogCategoryNet, "Failed to persist peer store on failure for %s: %v", addr, rerr)
			}
		}
	} else if len(failedPeers) > 0 {
		core.LogWarn(core.LogCategorySync, "No peer answered (%d tried): network connectivity lost? Will keep retrying.", len(failedPeers))
	}

	if bestPeer != "" {
		p.RequestChainSync(bestPeer)
	}
	go p.SyncMempool()
}

// GenerateBlockLocators builds a sparse list of block hashes (exponential backoff)
// starting from the local tip down to genesis [N-14]
func (p *PeerServer) GenerateBlockLocators() []string {
	if p.Blockchain == nil {
		return []string{core.GenesisHash}
	}
	p.Blockchain.Lock.RLock()
	defer p.Blockchain.Lock.RUnlock()

	chain := p.Blockchain.Chain
	if len(chain) == 0 {
		return []string{core.GenesisHash}
	}

	var locators []string
	step := 1
	idx := len(chain) - 1
	for idx > 0 {
		locators = append(locators, chain[idx].Hash)
		if len(locators) >= 10 {
			step *= 2
		}
		idx -= step
	}
	locators = append(locators, chain[0].Hash)
	return locators
}

// RequestChainSync synchronizes blocks from a peer using GET_FORKPOINT, bounded GET_BLOCK_RANGE chunks,
// and atomic ResolveFork reorganization [N-14].
func (p *PeerServer) RequestChainSync(peerAddress string) {
	if p.Blockchain == nil || p.isShuttingDown() {
		return
	}
	host, _, _ := net.SplitHostPort(peerAddress)
	syncKey := peerAddress
	if host != "" && host != "127.0.0.1" && host != "::1" && host != "localhost" {
		syncKey = host
	}
	p.syncMu.Lock()
	if p.syncingHosts == nil {
		p.syncingHosts = make(map[string]bool)
	}
	if p.syncingHosts[syncKey] {
		p.syncMu.Unlock()
		return
	}
	p.syncingHosts[syncKey] = true
	p.syncMu.Unlock()

	defer func() {
		p.syncMu.Lock()
		delete(p.syncingHosts, syncKey)
		p.syncMu.Unlock()
	}()

	// 1. Query peer's height and cumulative chainwork in O(1) [N-01, N-14]
	heightResp, err := p.QueryPeer(peerAddress, Message{
		NetworkID: p.GetNetworkID(),
		Type:      "GET_HEIGHT",
		Payload:   nil,
	}, 5*time.Second)

	if err != nil {
		core.LogDebug(core.LogCategorySync, "GET_HEIGHT failed peer=%s err=%v", peerAddress, err)
		return
	}
	if heightResp == nil || heightResp.Type != "HEIGHT_RESPONSE" {
		core.LogWarn(core.LogCategorySync, "Unexpected height response peer=%s resp=%+v", peerAddress, heightResp)
		return
	}

	var hr HeightResponse
	b, err := json.Marshal(heightResp.Payload)
	if err != nil {
		core.LogWarn(core.LogCategorySync, "Failed to unmarshal height response payload peer=%s err=%v", peerAddress, err)
		return
	}
	if err := json.Unmarshal(b, &hr); err != nil {
		core.LogWarn(core.LogCategorySync, "Failed to unmarshal height response into struct peer=%s err=%v", peerAddress, err)
		return
	}

	p.Blockchain.Lock.RLock()
	localLatest := p.Blockchain.GetLatestBlockUnsafe()
	localChainwork := p.Blockchain.GetTotalChainworkUnsafe()
	p.Blockchain.Lock.RUnlock()

	peerChainwork, ok := new(big.Int).SetString(hr.Chainwork, 10)
	if !ok {
		peerChainwork = big.NewInt(0)
	}

	// Peer is ahead if peerChainwork > localChainwork, or if chainwork equal but height higher
	peerIsAhead := false
	if peerChainwork.Sign() > 0 && localChainwork.Sign() > 0 {
		cmp := peerChainwork.Cmp(localChainwork)
		peerIsAhead = cmp > 0 || (cmp == 0 && hr.Height > localLatest.Index)
	} else {
		peerIsAhead = hr.Height > localLatest.Index
	}

	if peerIsAhead {
		fmt.Printf("[P2P] Starting block sync from peer %s (Local: #%d CW:%s, Peer: #%d CW:%s)...\n",
			peerAddress, localLatest.Index, localChainwork.String(), hr.Height, peerChainwork.String())

		// 2. Discover common ancestor fork point using block locators [N-14]
		locators := p.GenerateBlockLocators()
		forkMsg := Message{
			NetworkID: p.GetNetworkID(),
			Type:      "GET_FORKPOINT",
			Payload:   ForkpointRequest{Locators: locators},
		}

		forkResp, err := p.QueryPeer(peerAddress, forkMsg, 5*time.Second)
		if err != nil {
			core.LogDebug(core.LogCategorySync, "GET_FORKPOINT failed peer=%s err=%v", peerAddress, err)
			return
		}
		if forkResp == nil || forkResp.Type != "FORKPOINT_RESPONSE" {
			core.LogWarn(core.LogCategorySync, "Unexpected forkpoint response peer=%s resp=%+v", peerAddress, forkResp)
			return
		}

		var fpResp ForkpointResponse
		fb, err := json.Marshal(forkResp.Payload)
		if err != nil {
			core.LogWarn(core.LogCategorySync, "Failed to unmarshal forkpoint response payload peer=%s err=%v", peerAddress, err)
			return
		}
		if err := json.Unmarshal(fb, &fpResp); err != nil {
			core.LogWarn(core.LogCategorySync, "Failed to unmarshal forkpoint response into struct peer=%s err=%v", peerAddress, err)
			return
		}

		ancestorHeight := fpResp.AncestorHeight
		if ancestorHeight < 0 || ancestorHeight > localLatest.Index {
			ancestorHeight = 0
		}

		// Reorg depth check before downloading blocks
		if (localLatest.Index - ancestorHeight) > core.MaxReorganizationDepth {
			if now := time.Now().Unix(); now-atomic.LoadInt64(&lastDeepReorgWarn) >= 300 {
				atomic.StoreInt64(&lastDeepReorgWarn, now)
				core.LogError(core.LogCategorySync,
					"Peer %s has more chainwork but forks %d blocks deep (limit %d): this node is on a stale branch and will NOT reorganize automatically. Stop the node and restart with -resync to rebuild from the network.",
					peerAddress, localLatest.Index-ancestorHeight, core.MaxReorganizationDepth)
			}
			fmt.Printf("[P2P] [!] Rejected deep reorganization from peer %s (depth %d > limit %d)\n",
				peerAddress, localLatest.Index-ancestorHeight, core.MaxReorganizationDepth)
			return
		}

		// 3. Fetch missing blocks in continuous bounded batches from ancestorHeight + 1 up to hr.Height
		currentAncestor := ancestorHeight
		syncPeer := peerAddress

		for currentAncestor < hr.Height {
			var batchBlocks []core.Block
			const MaxSyncBlocksPerRequest = 4 * SyncChunkSize // 1000 blocks per DB commit batch
			fetchFailed := false

			for from := currentAncestor + 1; from <= hr.Height; {
				// Safeguard: apply in bounded batches to commit progress safely to database
				if len(batchBlocks) >= MaxSyncBlocksPerRequest {
					break
				}

				to := from + SyncChunkSize - 1
				if to > hr.Height {
					to = hr.Height
				}

				rangeMsg := Message{
					NetworkID: p.GetNetworkID(),
					Type:      "GET_BLOCK_RANGE",
					Payload:   BlockRangeRequest{FromIndex: from, ToIndex: to},
				}

				rangeResp, err := p.QueryPeer(syncPeer, rangeMsg, 15*time.Second)
				if err != nil {
					core.LogWarn(
						core.LogCategorySync,
						"GET_BLOCK_RANGE failed peer=%s from=%d to=%d err=%v",
						syncPeer, from, to, err,
					)
					// Multi-peer failover: attempt alternative peers if primary fails
					failedPeer := syncPeer
					recovered := false
					for _, altPeer := range p.GetPeers() {
						if altPeer == failedPeer || altPeer == "" {
							continue
						}
						core.LogInfo(core.LogCategorySync, "Attempting failover to alternative peer %s for range %d-%d", altPeer, from, to)
						if altResp, altErr := p.QueryPeer(altPeer, rangeMsg, 15*time.Second); altErr == nil && altResp != nil && altResp.Type == "BLOCK_RANGE_RESPONSE" {
							rangeResp = altResp
							err = nil
							syncPeer = altPeer
							recovered = true
							break
						}
					}
					if !recovered {
						fetchFailed = true
						break
					}
				}

				if rangeResp == nil || rangeResp.Type != "BLOCK_RANGE_RESPONSE" {
					core.LogWarn(
						core.LogCategorySync,
						"Unexpected block range response peer=%s from=%d to=%d resp=%+v",
						syncPeer, from, to, rangeResp,
					)
					fetchFailed = true
					break
				}

				var br BlockRangeResponse
				chunkBytes, err := json.Marshal(rangeResp.Payload)
				if err != nil {
					core.LogWarn(
						core.LogCategorySync,
						"Failed to unmarshal block range payload peer=%s from=%d to=%d err=%v",
						syncPeer, from, to, err,
					)
					fetchFailed = true
					break
				}
				if err := json.Unmarshal(chunkBytes, &br); err != nil {
					core.LogWarn(
						core.LogCategorySync,
						"Failed to unmarshal block range response into struct peer=%s from=%d to=%d err=%v",
						syncPeer, from, to, err,
					)
					fetchFailed = true
					break
				}
				if len(br.Blocks) == 0 {
					core.LogWarn(
						core.LogCategorySync,
						"Peer %s returned empty block list for range %d-%d",
						syncPeer, from, to,
					)
					fetchFailed = true
					break
				}

				// Validate start index and contiguous linkage in received partial range
				if br.Blocks[0].Index != from {
					fmt.Printf("[P2P] [!] Received unexpected block range start index %d (expected %d) from %s\n", br.Blocks[0].Index, from, syncPeer)
					fetchFailed = true
					break
				}
				segmentContiguous := true
				for i := 1; i < len(br.Blocks); i++ {
					if br.Blocks[i].Index != br.Blocks[i-1].Index+1 {
						fmt.Printf("[P2P] [!] Received non-contiguous block range segment from %s\n", syncPeer)
						segmentContiguous = false
						break
					}
				}
				if !segmentContiguous {
					fetchFailed = true
					break
				}

				batchBlocks = append(batchBlocks, br.Blocks...)
				from += len(br.Blocks)
				time.Sleep(10 * time.Millisecond) // Fast pipeline pacing
			}

			if len(batchBlocks) == 0 {
				break
			}

			// 4. Resolve fork or catch-up atomically using ResolveFork for this batch [N-14]
			if err := p.Blockchain.ResolveFork(currentAncestor, batchBlocks); err != nil {
				fmt.Printf("[P2P] ResolveFork with peer %s failed: %v\n", syncPeer, err)
				return
			}

			for _, blk := range batchBlocks {
				p.markBlockSeen(blk.Hash)
			}

			p.Blockchain.Lock.RLock()
			newChainLen := len(p.Blockchain.Chain)
			latest := p.Blockchain.GetLatestBlockUnsafe()
			p.Blockchain.Lock.RUnlock()

			currentAncestor = latest.Index

			hashPrefix := latest.Hash
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16] + "..."
			}
			core.LogInfo(core.LogCategorySync, "Successfully synchronized %d blocks from peer %s! New height: #%d (%d blocks, tip: %s)",
				len(batchBlocks), syncPeer, newChainLen-1, newChainLen, hashPrefix)

			// Mark and relay new tip
			p.isBlockSeenOrMark(latest.Hash)
			p.processOrphans(latest.Hash)
			p.processOrphanTxs()

			if fetchFailed {
				break
			}
		}
		return
	} else if hr.Height < localLatest.Index {
		// Remote peer is behind; stream missing blocks over a persistent connection [N-02]
		fmt.Printf("[P2P] Remote peer %s is behind (Peer: #%d, Local: #%d). Streaming missing blocks...\n",
			peerAddress, hr.Height, localLatest.Index)

		conn, release, err := p.DialPeer(peerAddress, 15*time.Second)
		if err != nil {
			return
		}
		defer func() {
			_ = conn.Close()
			release()
		}()

		for from := hr.Height + 1; from <= localLatest.Index; from += SyncChunkSize {
			to := from + SyncChunkSize - 1
			if to > localLatest.Index {
				to = localLatest.Index
			}

			p.Blockchain.Lock.RLock()
			var chunk []core.Block
			if from < len(p.Blockchain.Chain) {
				end := to
				if end >= len(p.Blockchain.Chain) {
					end = len(p.Blockchain.Chain) - 1
				}
				chunk = make([]core.Block, end-from+1)
				copy(chunk, p.Blockchain.Chain[from:end+1])
			}
			p.Blockchain.Lock.RUnlock()

			for _, blk := range chunk {
				msgBytes, err := json.Marshal(Message{
					NetworkID: p.GetNetworkID(),
					Type:      "NEW_BLOCK",
					Payload:   blk,
				})
				if err == nil {
					_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if _, err := conn.Write(append(msgBytes, '\n')); err != nil {
						return
					}
				}
				time.Sleep(20 * time.Millisecond) // Pacing 50 msgs/s, well within NEW_BLOCK budget of 80/s [T1]
			}
		}
		return
	}
}
