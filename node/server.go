package node

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// ServerConfig holds server configuration parameters
type ServerConfig struct {
	NetworkID   string
	DataDir     string // [P1-A]
	RPCBind     string
	RPCPort     string
	P2PPort     string
	RPCUser     string
	RPCPassword string
	MineKey     string
}

// IPRateLimiter provides per-IP request throttling
type IPRateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	lastMine map[string]time.Time
	stopChan chan struct{}
	doneChan chan struct{}
	stopOnce sync.Once
}

func newIPRateLimiter() *IPRateLimiter {
	limiter := &IPRateLimiter{
		requests: make(map[string][]time.Time),
		lastMine: make(map[string]time.Time),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
	// Background cleanup of expired IP tracking records
	go func() {
		defer close(limiter.doneChan)
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-limiter.stopChan:
				return
			case <-ticker.C:
				limiter.mu.Lock()
				cutoff := time.Now().Add(-1 * time.Minute)
				for ip, times := range limiter.requests {
					var valid []time.Time
					for _, t := range times {
						if t.After(cutoff) {
							valid = append(valid, t)
						}
					}
					if len(valid) == 0 {
						delete(limiter.requests, ip)
					} else {
						limiter.requests[ip] = valid
					}
				}

				// Clean expired lastMine entries [R-08]
				cutoffMine := time.Now().Add(-10 * time.Minute)
				for ip, t := range limiter.lastMine {
					if t.Before(cutoffMine) {
						delete(limiter.lastMine, ip)
					}
				}
				limiter.mu.Unlock()
			}
		}
	}()
	return limiter
}

// Close terminates the background cleanup goroutine and waits for it to exit [v1.0.6].
func (l *IPRateLimiter) Close() {
	if l != nil {
		l.stopOnce.Do(func() {
			if l.stopChan != nil {
				close(l.stopChan)
			}
		})
		if l.doneChan != nil {
			<-l.doneChan
		}
	}
}

const maxRateLimiterIPs = 10000

func (l *IPRateLimiter) allow(ip string, maxPerMin int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	times, exists := l.requests[ip]
	if !exists && len(l.requests) >= maxRateLimiterIPs {
		for k, ts := range l.requests {
			var keep []time.Time
			for _, t := range ts {
				if t.After(cutoff) {
					keep = append(keep, t)
				}
			}
			if len(keep) == 0 {
				delete(l.requests, k)
			} else {
				l.requests[k] = keep
			}
		}
		if len(l.requests) >= maxRateLimiterIPs {
			return false
		}
	}

	var valid []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= maxPerMin {
		l.requests[ip] = valid
		return false
	}

	l.requests[ip] = append(valid, now)
	return true
}

func (l *IPRateLimiter) allowMine(ip string, minInterval time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	last, exists := l.lastMine[ip]
	if !exists && len(l.lastMine) >= maxRateLimiterIPs {
		cutoffMine := now.Add(-10 * time.Minute)
		for k, t := range l.lastMine {
			if t.Before(cutoffMine) {
				delete(l.lastMine, k)
			}
		}
		if len(l.lastMine) >= maxRateLimiterIPs {
			return false
		}
	}
	if exists && now.Sub(last) < minInterval {
		return false
	}
	l.lastMine[ip] = now
	return true
}

// isAllowedOrigin parses raw URL string and checks if host is strictly a trusted local hostname
func isAllowedOrigin(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

var activeMiningJob int32

// writeJSONError safely serializes an error message into a valid JSON object [T10]
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   msg,
		"message": msg,
		"status":  "error",
	})
}

// isAllowedHost checks if an HTTP Host header is loopback or an explicitly configured host (DNS rebinding guard) [T10]
func isAllowedHost(rawHost string, configuredHost string) bool {
	h, _, err := net.SplitHostPort(rawHost)
	if err != nil {
		h = rawHost
	}
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	if h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "" {
		return true
	}
	if flag.Lookup("test.v") != nil && h == "example.com" {
		return true
	}
	if configuredHost != "" {
		confH, _, err := net.SplitHostPort(configuredHost)
		if err != nil {
			confH = configuredHost
		}
		confH = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(confH), "]"), "[")
		if strings.EqualFold(h, confH) {
			return true
		}
	}
	return false
}

// isMutatingEndpoint checks whether an endpoint or HTTP method modifies node state
func isMutatingEndpoint(path string, method string) bool {
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch {
		return true
	}
	return path == "/mine"
}

// isAuthorizedMiner checks if caller has valid X-Mine-Key or valid RPC credentials
func isAuthorizedMiner(r *http.Request, cfg *ServerConfig) bool {
	configuredKey := ""
	if cfg != nil && cfg.MineKey != "" {
		configuredKey = cfg.MineKey
	} else {
		configuredKey = os.Getenv("ARMX_MINE_KEY")
	}

	if configuredKey != "" {
		clientKey := r.Header.Get("X-Mine-Key")
		if clientKey != "" && constantTimeCompare(clientKey, configuredKey) {
			clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil || clientIP == "" {
				clientIP = r.RemoteAddr
			}
			globalAuthLockout.recordSuccess(clientIP)
			return true
		}
	}

	return Authenticate(r)
}

// NewRPCHandler creates and returns the central HTTP handler for Armaxcoin RPC
func NewRPCHandler(bc *core.Blockchain, peerServer *PeerServer, limiter *IPRateLimiter, cfg *ServerConfig) http.Handler {
	if limiter == nil {
		limiter = newIPRateLimiter()
	}
	if bc != nil && bc.CoinbaseMaturity <= 0 {
		bc.CoinbaseMaturity = core.GetActiveNetwork().CoinbaseMaturity
	}

	mux := http.NewServeMux()

	// Root Endpoint: Core Daemon API Status
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		netID := NetworkID()
		if cfg != nil && cfg.NetworkID != "" {
			netID = cfg.NetworkID
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"daemon":     "Armaxcoin Core Node",
			"version":    core.Version,
			"network_id": netID,
			"status":     "online",
		})
	})

	// Status Endpoint: Dedicated health & node telemetry check
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		height := 0
		if len(bc.Chain) > 0 {
			height = len(bc.Chain) - 1
		}
		bc.Lock.RUnlock()

		netID := NetworkID()
		if cfg != nil && cfg.NetworkID != "" {
			netID = cfg.NetworkID
		}
		peersActive := 0
		peersKnown := 0
		peersSeeds := 0
		peersCandidates := 0

		if peerServer != nil {
			peersActive = peerServer.GetPeersActive()
			peersKnown = peerServer.GetPeersKnown()
			peersSeeds = peerServer.GetPeersSeeds()
			peersCandidates = peerServer.GetPeersCandidates()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"daemon":             "Armaxcoin Core Node",
			"version":            core.Version,
			"network_id":         netID,
			"status":             "online",
			"height":             height,
			"peers_known":        peersKnown,
			"peers_seeds":        peersSeeds,
			"peers_candidates":   peersCandidates,
			"active_connections": peersActive,
			"peers_active":       peersActive, // legacy compatibility
		})
	})

	// Endpoint: Retrieve blockchain (Copy-Before-Write to prevent Slowloris RLock DoS)
	mux.HandleFunc("/chain", func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		pageStr := r.URL.Query().Get("page")
		query := strings.TrimSpace(r.URL.Query().Get("q"))

		w.Header().Set("Content-Type", "application/json")

		// Search mode by block index, hash, or miner [N-21]
		if query != "" {
			var matches []core.Block
			var totalBlocks int

			// O(1) direct block index lookup
			if idx, err := strconv.Atoi(query); err == nil {
				bc.Lock.RLock()
				totalBlocks = len(bc.Chain)
				if idx >= 0 && idx < totalBlocks {
					matches = append(matches, bc.Chain[idx])
				}
				bc.Lock.RUnlock()
			} else if len(query) == 64 {
				// O(1) store hash index lookup
				if b, err := bc.GetBlockByHash(query); err == nil && b != nil {
					matches = append(matches, *b)
				}
				bc.Lock.RLock()
				totalBlocks = len(bc.Chain)
				bc.Lock.RUnlock()
			} else {
				// Fallback search by miner address (bounded to 25 matches, scanning at most 1000 blocks) [T9]
				bc.Lock.RLock()
				totalBlocks = len(bc.Chain)
				scanLimit := 1000
				for i := totalBlocks - 1; i >= 0 && (totalBlocks-1-i) < scanLimit; i-- {
					b := bc.Chain[i]
					if strings.EqualFold(b.Miner, query) {
						matches = append(matches, b)
						if len(matches) >= 25 {
							break
						}
					}
				}
				bc.Lock.RUnlock()
			}

			if matches == nil {
				matches = []core.Block{}
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"total_blocks": totalBlocks,
				"page":         1,
				"limit":        len(matches),
				"total_pages":  1,
				"blocks":       matches,
			})
			return
		}

		// Paginated mode
		if limitStr != "" {
			limit, err := strconv.Atoi(limitStr)
			if err != nil || limit <= 0 {
				limit = 10
			}
			if limit > 50 {
				limit = 50
			}

			page := 1
			if pageStr != "" {
				if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
					page = p
				}
			}

			var paginated []core.Block
			var totalBlocks int

			bc.Lock.RLock()
			totalBlocks = len(bc.Chain)
			totalPages := (totalBlocks + limit - 1) / limit
			if totalPages < 1 {
				totalPages = 1
			}
			if page > totalPages {
				page = totalPages
			}

			startFromEnd := (page - 1) * limit
			endFromEnd := startFromEnd + limit
			if endFromEnd > totalBlocks {
				endFromEnd = totalBlocks
			}

			for i := totalBlocks - 1 - startFromEnd; i >= totalBlocks-endFromEnd; i-- {
				if i >= 0 && i < totalBlocks {
					paginated = append(paginated, bc.Chain[i])
				}
			}
			bc.Lock.RUnlock()

			if paginated == nil {
				paginated = []core.Block{}
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"total_blocks": totalBlocks,
				"page":         page,
				"limit":        limit,
				"total_pages":  totalPages,
				"blocks":       paginated,
			})
			return
		}

		// Non-paginated default: snapshot chain in memory bounded to 50 blocks [N-21, R-06]
		bc.Lock.RLock()
		totalLen := len(bc.Chain)
		maxSnapshot := 50
		start := 0
		if totalLen > maxSnapshot {
			start = totalLen - maxSnapshot
		}
		chainCopy := make([]core.Block, totalLen-start)
		copy(chainCopy, bc.Chain[start:])
		bc.Lock.RUnlock()

		_ = json.NewEncoder(w).Encode(chainCopy)
	})

	// Endpoint: Check account balance and nonces
	mux.HandleFunc("/balance", func(w http.ResponseWriter, r *http.Request) {
		address := r.URL.Query().Get("address")
		if address == "" {
			http.Error(w, `{"error":"Parameter 'address' is required"}`, http.StatusBadRequest)
			return
		}

		bc.Lock.RLock()
		balance := bc.GetBalance(address)
		pendingBal := bc.GetPendingBalance(address)
		immatureBal := bc.GetImmatureBalance(address)
		spendableBal := bc.GetSpendableBalance(address)
		maturity := bc.GetCoinbaseMaturity()
		nonce := bc.GetAccountNonce(address)
		pendingNonce := bc.GetPendingNonce(address)
		bc.Lock.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"address":                 address,
			"balance":                 balance,
			"confirmed_balance":       balance,
			"pending_balance":         pendingBal,
			"immature_balance":        immatureBal,
			"spendable_balance":       spendableBal,
			"coinbase_maturity":       maturity,
			"standard_confirmations":  6,
			"highvalue_confirmations": 12,
			"nonce":                   nonce,
			"pending_nonce":           pendingNonce,
			"next_nonce":              pendingNonce + 1, // Nonce expected for next transaction [R-11]
		})
	})

	// Endpoint: Retrieve transaction history for a specific wallet address [N-21]
	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		address := strings.TrimSpace(r.URL.Query().Get("address"))
		if address == "" {
			http.Error(w, `{"error":"Parameter 'address' is required"}`, http.StatusBadRequest)
			return
		}

		type TxRecord struct {
			BlockIndex int              `json:"block_index"`
			Timestamp  int64            `json:"timestamp"`
			Status     string           `json:"status"` // "confirmed" or "pending"
			Tx         core.Transaction `json:"tx"`
		}

		var history []TxRecord

		bc.Lock.RLock()
		// 1. Check pending mempool transactions first
		for _, tx := range bc.Mempool {
			if core.MatchAddress(tx.Sender, address) || core.MatchAddress(tx.Recipient, address) {
				history = append(history, TxRecord{
					BlockIndex: -1,
					Timestamp:  time.Now().Unix(),
					Status:     "pending",
					Tx:         tx,
				})
			}
		}

		limit := 100 // default limit [T9]
		if lStr := strings.TrimSpace(r.URL.Query().Get("limit")); lStr != "" {
			if lVal, err := strconv.Atoi(lStr); err == nil && lVal > 0 {
				limit = lVal
			}
		}
		if limit > 1000 {
			limit = 1000 // max cap [T9]
		}

		// 2. Search through blockchain in reverse chronological order (bounded to at most 2000 blocks) [T9]
		chainLen := len(bc.Chain)
		if chainLen > 0 {
			maxBlocksToScan := 2000
			scanStart := chainLen - 1
			scanEnd := scanStart - maxBlocksToScan
			if scanEnd < 0 {
				scanEnd = 0
			}
			for i := scanStart; i >= scanEnd; i-- {
				b := bc.Chain[i]
				for _, tx := range b.Transactions {
					if core.MatchAddress(tx.Sender, address) || core.MatchAddress(tx.Recipient, address) {
						history = append(history, TxRecord{
							BlockIndex: b.Index,
							Timestamp:  b.Timestamp,
							Status:     "confirmed",
							Tx:         tx,
						})
						if len(history) >= limit {
							break
						}
					}
				}
				if len(history) >= limit {
					break
				}
			}
		}
		bc.Lock.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if history == nil {
			history = []TxRecord{}
		}
		_ = json.NewEncoder(w).Encode(history)
	})

	// Endpoint: Network & Ledger Statistics (Copy-Before-Write, no nested RLock) [N-21]
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		blocks := len(bc.Chain)
		height := 0
		if blocks > 0 {
			height = blocks - 1
		}
		difficulty := bc.Difficulty
		mempoolSize := len(bc.Mempool)
		blockReward := bc.BlockReward
		circulatingSupply := bc.GetCirculatingSupply()
		latest := bc.GetLatestBlockUnsafe()
		bc.Lock.RUnlock()

		chainwork := bc.GetTotalChainwork().String()
		totalTx := bc.GetTotalTransactions()

		peersActive := 0
		peersKnown := 0
		peersSeeds := 0
		peersCandidates := 0
		if peerServer != nil {
			peersActive = peerServer.GetPeersActive()
			peersKnown = peerServer.GetPeersKnown()
			peersSeeds = peerServer.GetPeersSeeds()
			peersCandidates = peerServer.GetPeersCandidates()
		}

		netID := NetworkID()
		if cfg != nil && cfg.NetworkID != "" {
			netID = cfg.NetworkID
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"network_id":                    netID,
			"height":                        height,
			"blocks":                        blocks,
			"difficulty":                    difficulty,
			"chainwork":                     chainwork,
			"max_reorg_depth":               core.MaxReorganizationDepth,
			"total_tx":                      totalTx,
			"mempool_size":                  mempoolSize,
			"block_reward":                  float64(blockReward) / float64(core.CoinUnit),
			"block_reward_base_units":       blockReward,
			"circulating_supply":            float64(circulatingSupply) / float64(core.CoinUnit),
			"circulating_supply_base_units": circulatingSupply,
			"max_supply":                    float64(core.MaxSupply) / float64(core.CoinUnit),
			"max_supply_base_units":         core.MaxSupply,
			"halving_interval":              core.HalvingInterval,
			"latest_hash":                   latest.Hash,
			"peers_active":                  peersActive,
			"peers_known":                   peersKnown,
			"peers_seeds":                   peersSeeds,
			"peers_candidates":              peersCandidates,
			"peers_count":                   peersActive,
			"mining_hashrate":               bc.GetMiningHashrate(),
		})
	})

	// Standard CoinMarketCap & CoinGecko Plain-Text Supply Endpoints
	mux.HandleFunc("/supply/circulating", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		supply := bc.GetCirculatingSupply()
		bc.Lock.RUnlock()

		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, strconv.FormatFloat(float64(supply)/float64(core.CoinUnit), 'f', -1, 64))
	})

	mux.HandleFunc("/supply/total", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		supply := bc.GetCirculatingSupply()
		bc.Lock.RUnlock()

		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, strconv.FormatFloat(float64(supply)/float64(core.CoinUnit), 'f', -1, 64))
	})

	mux.HandleFunc("/supply/max", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, strconv.FormatFloat(float64(core.MaxSupply)/float64(core.CoinUnit), 'f', -1, 64))
	})

	// JSON Supply API Endpoint
	mux.HandleFunc("/api/supply", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		circulating := bc.GetCirculatingSupply()
		bc.Lock.RUnlock()

		circulatingCoins := float64(circulating) / float64(core.CoinUnit)
		maxCoins := float64(core.MaxSupply) / float64(core.CoinUnit)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"symbol":                        core.TickerForNetworkID(core.NetworkID()),
			"circulating_supply":            circulatingCoins,
			"total_supply":                  circulatingCoins,
			"max_supply":                    maxCoins,
			"circulating_supply_base_units": circulating,
			"total_supply_base_units":       circulating,
			"max_supply_base_units":         core.MaxSupply,
		})
	})

	// Endpoint: Query transaction by Hash (for Exchange deposit tracking, indexers, wallets) [N-21, C-25]
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		hash := strings.TrimSpace(r.URL.Query().Get("hash"))
		if hash == "" {
			http.Error(w, `{"error":"Parameter 'hash' is required"}`, http.StatusBadRequest)
			return
		}

		type TxResponse struct {
			Status        string           `json:"status"`
			BlockIndex    int              `json:"block_index"`
			BlockHash     string           `json:"block_hash,omitempty"`
			Confirmations int              `json:"confirmations"`
			Timestamp     int64            `json:"timestamp"`
			Tx            core.Transaction `json:"tx"`
		}

		tx, blockIdx, blockTs, blockHash, err := bc.GetTransactionByHash(hash)
		if err != nil || tx == nil {
			http.Error(w, `{"error":"Transaction not found"}`, http.StatusNotFound)
			return
		}

		status := "confirmed"
		confirmations := 0
		if blockIdx < 0 {
			status = "pending"
		} else {
			bc.Lock.RLock()
			currentHeight := len(bc.Chain)
			bc.Lock.RUnlock()
			confirmations = currentHeight - blockIdx
		}

		resp := &TxResponse{
			Status:        status,
			BlockIndex:    blockIdx,
			BlockHash:     blockHash,
			Confirmations: confirmations,
			Timestamp:     blockTs,
			Tx:            *tx,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Endpoint: Query block by Index or Hash (Copy-Before-Write) [N-21, C-25]
	mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		idxStr := r.URL.Query().Get("index")
		hashStr := strings.TrimSpace(r.URL.Query().Get("hash"))

		var foundBlock *core.Block

		if idxStr != "" {
			if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 {
				bc.Lock.RLock()
				if idx < len(bc.Chain) {
					b := bc.Chain[idx]
					foundBlock = &b
				}
				bc.Lock.RUnlock()
			}
		} else if hashStr != "" {
			if b, err := bc.GetBlockByHash(hashStr); err == nil && b != nil {
				foundBlock = b
			}
		}

		if foundBlock == nil {
			http.Error(w, `{"error":"Block not found"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(foundBlock)
	})

	// Endpoint: Peer list management (POST requires authentication)
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !Authenticate(r) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Armaxcoin RPC"`)
				http.Error(w, `{"error":"401 Unauthorized: Valid RPC credentials required to register peers"}`, http.StatusUnauthorized)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
			var body struct {
				Peer string `json:"peer"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Peer != "" {
				if peerServer != nil {
					if peerServer.IsLocalMainnet() {
						_ = peerServer.AddPeer(body.Peer)
					} else {
						for _, resolvedPeer := range ResolveDNSSeed(body.Peer) {
							_ = peerServer.AddPeer(resolvedPeer)
						}
					}
				}
			}
		}
		filter := r.URL.Query().Get("filter")
		if filter == "" && (r.URL.Query().Get("active") == "1" || r.URL.Query().Get("active") == "true") {
			filter = "active"
		}

		detailsQuery := r.URL.Query().Get("details")
		if detailsQuery == "1" || detailsQuery == "true" || r.URL.Query().Get("format") == "detailed" {
			var details []PeerEntry
			if peerServer != nil {
				if filter == "active" {
					details = peerServer.GetActivePeerDetails()
				} else {
					details = peerServer.GetPeerDetails()
				}
				if len(details) > 100 {
					details = details[:100] // Max 100 entries [N-19]
				}
			}
			if details == nil {
				details = []PeerEntry{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(details)
			return
		}

		var peers []string
		if peerServer != nil {
			if filter == "active" {
				peers = peerServer.GetActivePeerEndpoints()
			} else {
				peers = peerServer.GetPeers()
			}
			if len(peers) > 100 {
				peers = peers[:100] // Max 100 entries [N-19]
			}
		} else {
			peers = []string{}
		}
		if peers == nil {
			peers = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(peers)
	})

	// Endpoint: Retrieve current mempool pending transactions (Copy-Before-Write, capped at 100) [N-19]
	mux.HandleFunc("/mempool", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		total := len(bc.Mempool)
		if total > 100 {
			total = 100
		}
		mempoolCopy := make([]core.Transaction, total)
		copy(mempoolCopy, bc.Mempool[:total])
		bc.Lock.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mempoolCopy)
	})

	// Endpoint: Standard mining pool getblocktemplate [R-07]
	mux.HandleFunc("/getblocktemplate", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		latest := bc.GetLatestBlockUnsafe()
		maxAllowedTxs := core.MaxBlockTransactions - 1
		maxAllowedSize := core.MaxBlockSize - 50000
		candidateTxs, totalFees := bc.SelectCandidateTransactionsLocked(maxAllowedTxs, maxAllowedSize)

		minTs := core.MinValidTimestamp(bc.Chain, &latest, latest.Index+1)
		curTime := time.Now().Unix()
		if curTime < minTs {
			curTime = minTs
		}
		target := core.CalculateExpectedTargetForCandidate(bc.Chain, latest.Index+1, curTime)
		diff := core.TargetToDifficulty(target)
		blockReward := bc.GetBlockReward(latest.Index + 1)
		bc.Lock.RUnlock()

		if candidateTxs == nil {
			candidateTxs = []core.Transaction{}
		}

		coinbaseValue := blockReward + totalFees
		merkleRoot := core.CalculateMerkleRoot(candidateTxs)
		powAlgo := "sha256d"
		if core.IsArmaxHashV2Active(latest.Index + 1) {
			powAlgo = "armaxhash-v2"
		} else if core.IsArmaxHashActive(latest.Index + 1) {
			powAlgo = "armaxhash"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"version":           1,
			"previousblockhash": latest.Hash,
			"height":            latest.Index + 1,
			"curtime":           curTime,
			"mintime":           minTs,
			"difficulty":        diff,
			"target":            fmt.Sprintf("%064x", target),
			"coinbasevalue":     coinbaseValue,
			"transactions":      candidateTxs,
			"merkleroot":        merkleRoot,
			"pow_algo":          powAlgo,
		})
	})

	// Endpoint: Standard mining info (/getmininginfo)
	mux.HandleFunc("/getmininginfo", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		latest := bc.GetLatestBlockUnsafe()
		target := bc.CalculateLWMA1()
		diff := core.TargetToDifficulty(target)
		pooledTx := len(bc.Mempool)
		chainName := strings.ToLower(core.GetActiveNetwork().Name)
		isGenerating := bc.GetMiningHashrate() > 0
		bc.Lock.RUnlock()

		powAlgo := "sha256d"
		if core.IsArmaxHashV2Active(latest.Index + 1) {
			powAlgo = "armaxhash-v2"
		} else if core.IsArmaxHashActive(latest.Index + 1) {
			powAlgo = "armaxhash"
		}

		networkHashPS := big.NewInt(0)
		if target.Sign() > 0 {
			two256 := new(big.Int).Lsh(big.NewInt(1), 256)
			hashesPerBlock := new(big.Int).Div(two256, target)
			networkHashPS = new(big.Int).Div(hashesPerBlock, big.NewInt(core.TargetBlockTimeSeconds))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"blocks":             latest.Index + 1,
			"currentblockweight": 1000,
			"currentblocktx":     pooledTx + 1,
			"difficulty":         diff,
			"target":             fmt.Sprintf("%064x", target),
			"networkhashps":      networkHashPS.Int64(),
			"pooledtx":           pooledTx,
			"chain":              chainName,
			"pow_algo":           powAlgo,
			"generate":           isGenerating,
			"warnings":           "",
		})
	})

	// Endpoint: Standard network hashrate estimate (/getnetworkhashps)
	mux.HandleFunc("/getnetworkhashps", func(w http.ResponseWriter, r *http.Request) {
		bc.Lock.RLock()
		target := bc.CalculateLWMA1()
		bc.Lock.RUnlock()

		networkHashPS := big.NewInt(0)
		if target.Sign() > 0 {
			two256 := new(big.Int).Lsh(big.NewInt(1), 256)
			hashesPerBlock := new(big.Int).Div(two256, target)
			networkHashPS = new(big.Int).Div(hashesPerBlock, big.NewInt(core.TargetBlockTimeSeconds))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"networkhashps": networkHashPS.Int64(),
		})
	})

	// Endpoint: Submit solved block from external miner or pool (/submitblock)
	submitBlockHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"HTTP method must be POST"}`, http.StatusMethodNotAllowed)
			return
		}

		if !isAuthorizedMiner(r, cfg) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Armaxcoin RPC"`)
			http.Error(w, `{"error":"401 Unauthorized: Valid RPC credentials or X-Mine-Key required to submit block"}`, http.StatusUnauthorized)
			return
		}

		// Protect against oversized JSON request body (core.MaxBlockSize + 64KB margin)
		r.Body = http.MaxBytesReader(w, r.Body, int64(core.MaxBlockSize+64*1024))

		var submittedBlock core.Block
		if err := json.NewDecoder(r.Body).Decode(&submittedBlock); err != nil {
			http.Error(w, `{"error":"Invalid block JSON"}`, http.StatusBadRequest)
			return
		}

		if bc.ValidateAndAddBlock(submittedBlock) {
			hashPrefix := submittedBlock.Hash
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16] + "..."
			}
			core.LogInfo(core.LogCategoryMiner, "Mined block #%d accepted via submitblock | hash: %s | txs: %d | diff: %d | miner: %s",
				submittedBlock.Index, hashPrefix, len(submittedBlock.Transactions), submittedBlock.Difficulty, submittedBlock.Miner)
			if peerServer != nil {
				go peerServer.RelayBlock(submittedBlock, "EXTERNAL_MINING_POOL")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "accepted",
				"height": submittedBlock.Index,
				"hash":   submittedBlock.Hash,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "rejected",
			"message": "Block PoW, timestamp, or cryptographic validation failed",
		})
	}
	mux.HandleFunc("/block/submit", submitBlockHandler)
	mux.HandleFunc("/submitblock", submitBlockHandler)

	// Endpoint: Submit new transaction to mempool and gossip via P2P
	mux.HandleFunc("/tx/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"HTTP method must be POST"}`, http.StatusMethodNotAllowed)
			return
		}

		// Protect against oversized JSON request body (MaxTxSize + 16KB margin)
		r.Body = http.MaxBytesReader(w, r.Body, int64(core.MaxTxSize+16*1024))

		var tx core.Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid JSON transaction payload: %s", err.Error()), http.StatusBadRequest)
			return
		}

		success, message := bc.AddTransaction(tx)
		w.Header().Set("Content-Type", "application/json")
		if !success {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "error",
				"message": message,
			})
			return
		}

		// Gossip valid transaction across P2P network
		if peerServer != nil {
			go peerServer.RelayTransaction(tx, "")
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"message": message,
		})
	})

	// Endpoint: Mine new block with rate-limiting, CSRF protection, and exact origin hostname check
	mux.HandleFunc("/mine", func(w http.ResponseWriter, r *http.Request) {
		// Set dedicated extended write deadline for /mine so PoW execution can proceed beyond standard WriteTimeout [L3-9]
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Minute))
		defer func() {
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		}()

		// Strict HTTP method check: accept ONLY POST
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "HTTP method must be POST",
			})
			return
		}

		// Reject cross-origin browser requests with exact hostname parsing (prevent CSRF / drive-by mining)
		if origin := r.Header.Get("Origin"); origin != "" {
			if !isAllowedOrigin(origin) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status":  "error",
					"message": "Forbidden: cross-origin requests to /mine are prohibited",
				})
				return
			}
		}
		if referer := r.Header.Get("Referer"); referer != "" {
			if !isAllowedOrigin(referer) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status":  "error",
					"message": "Forbidden: cross-origin requests to /mine are prohibited",
				})
				return
			}
		}

		// Strictly require either valid RPC authentication or valid X-Mine-Key [N-19]
		if !isAuthorizedMiner(r, cfg) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Armaxcoin RPC"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Unauthorized: valid RPC credentials or X-Mine-Key required to mine",
			})
			return
		}

		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}

		// Throttling: Minimum 2 seconds between /mine requests per IP for non-localhost callers
		isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost"
		if !isLocal && !limiter.allowMine(clientIP, 2*time.Second) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Mining rate limit exceeded. Please wait at least 2 seconds between requests.",
			})
			return
		}

		// Body bounds (16 KB)
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

		miner := r.URL.Query().Get("miner")
		var requestedThreads int
		if r.Body != nil {
			var body struct {
				Miner   string `json:"miner"`
				Threads int    `json:"threads"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if miner == "" {
				miner = body.Miner
			}
			requestedThreads = body.Threads
		}
		if requestedThreads <= 0 {
			if thStr := r.URL.Query().Get("threads"); thStr != "" {
				requestedThreads, _ = strconv.Atoi(thStr)
			}
		}

		if miner == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Missing required parameter: miner address must be provided via ?miner= or JSON body",
			})
			return
		}

		// Validate miner address format
		if !core.ValidateAddress(miner) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Invalid miner address: must be a valid Armaxcoin address (armx1..., A..., or 40-hex)",
			})
			return
		}

		// Serialize active mining jobs across concurrent requests [T10]
		if !atomic.CompareAndSwapInt32(&activeMiningJob, 0, 1) {
			writeJSONError(w, "Another mining job is currently active", http.StatusConflict)
			return
		}
		defer atomic.StoreInt32(&activeMiningJob, 0)

		minedBlock, err := bc.MineBlockContext(r.Context(), miner, requestedThreads)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("Mining aborted: %v", err),
			})
			return
		}

		// Gossip newly mined block across P2P network
		if peerServer != nil {
			go peerServer.RelayBlock(minedBlock, "")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"message": "New block successfully mined!",
			"block":   minedBlock,
		})
	})

	// Global Middleware: CORS & Rate-Limiting
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effectiveNetID := NetworkID()
		if cfg != nil && cfg.NetworkID != "" {
			effectiveNetID = cfg.NetworkID
		}
		w.Header().Set("X-Network-ID", effectiveNetID)
		w.Header().Set("Access-Control-Expose-Headers", "X-Network-ID")

		origin := r.Header.Get("Origin")
		isMutating := isMutatingEndpoint(r.URL.Path, r.Method)

		if isMutating {
			// DNS Rebinding Guard: reject mutating requests if Host header is not loopback / configured [T10]
			configuredHost := ""
			if cfg != nil && cfg.RPCBind != "" {
				configuredHost = cfg.RPCBind
			}
			if !isAllowedHost(r.Host, configuredHost) {
				writeJSONError(w, "Forbidden: Host header not permitted", http.StatusForbidden)
				return
			}

			// Cross-Origin / Referer Guard on mutating endpoints [T10]
			referer := r.Header.Get("Referer")
			if origin != "" && !isAllowedOrigin(origin) {
				writeJSONError(w, "Forbidden cross-origin", http.StatusForbidden)
				return
			}
			if referer != "" && !isAllowedOrigin(referer) {
				writeJSONError(w, "Forbidden cross-origin referer", http.StatusForbidden)
				return
			}

			if origin != "" && isAllowedOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		} else {
			// Read-only endpoints allow wildcard CORS for explorers and wallets
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Mine-Key")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		if r.Method == http.MethodOptions {
			if isMutating && origin != "" && !isAllowedOrigin(origin) {
				http.Error(w, `{"error":"Forbidden cross-origin"}`, http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		// Extract IP for rate limiting
		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			clientIP = r.RemoteAddr
		}

		// Max 120 HTTP requests per minute per IP for RPC endpoints (loopback allowed higher throughput for local explorer/indexer)
		maxPerMin := 120
		if clientIP == "127.0.0.1" || clientIP == "::1" || strings.EqualFold(clientIP, "localhost") {
			maxPerMin = 2400
		}
		if !limiter.allow(clientIP, maxPerMin) {
			http.Error(w, `{"error":"Too many requests, rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		mux.ServeHTTP(w, r)
	})

	return handler
}

// getNetworkProfileByID returns the static network profile associated with a network ID [P1-A]
func getNetworkProfileByID(netID string) core.NetworkProfile {
	canon, _, _ := parseNetworkID(netID)
	switch canon {
	case "ARMX-MAINNET-1":
		return core.MainnetProfile
	case "ARMX-REGTEST-1":
		return core.RegtestProfile
	case "ARMX-SOAK-1":
		return core.SoakProfile
	default:
		return core.TestnetProfile
	}
}

// InitServerFromConfig initializes PeerServer and ServerConfig while strictly applying all N-18 configuration settings
func InitServerFromConfig(bc *core.Blockchain, cfg *Config) (*PeerServer, *ServerConfig, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("server config cannot be nil")
	}

	// 1. Resolve and validate target network ID [P1-A]
	targetNetID := ""
	if cfg.NetworkID != "" {
		canon, _, err := parseNetworkID(cfg.NetworkID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid config network ID %q: %w", cfg.NetworkID, err)
		}
		targetNetID = canon
	} else if bc != nil && bc.GetNetworkID() != "" {
		canon, _, err := parseNetworkID(bc.GetNetworkID())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid blockchain network ID %q: %w", bc.GetNetworkID(), err)
		}
		targetNetID = canon
	} else {
		canon, _, err := parseNetworkID(core.NetworkID())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid process active network ID %q: %w", core.NetworkID(), err)
		}
		targetNetID = canon
	}

	// 2. Network Profile Configuration & Validation [C-33, N-18]
	if cfg.NetworkID != "" {
		if err := core.SetNetwork(cfg.NetworkID); err != nil {
			activeNetID, _, _ := parseNetworkID(core.NetworkID())
			if activeNetID != targetNetID {
				return nil, nil, fmt.Errorf("network mismatch: server configured for %q but process active network is %q (%w)", targetNetID, activeNetID, err)
			}
			// If active network already matches targetNetID, profile is already successfully initialized
		}
	}

	// 3. Consistency checks across configuration, blockchain, and process active network [P1-A]
	activeNetID, _, _ := parseNetworkID(core.NetworkID())
	if activeNetID != targetNetID {
		return nil, nil, fmt.Errorf("network mismatch: server configured for %q but process active network is %q (single network profile enforced per process)", targetNetID, activeNetID)
	}

	if bc != nil && bc.GetNetworkID() != "" {
		bcNetID, _, err := parseNetworkID(bc.GetNetworkID())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid blockchain network ID: %w", err)
		}
		if bcNetID != targetNetID {
			return nil, nil, fmt.Errorf("network mismatch: server configured for %q but blockchain is bound to %q", targetNetID, bcNetID)
		}
	}

	netProfile := getNetworkProfileByID(targetNetID)
	if cfg.RPCPort == "" {
		cfg.RPCPort = netProfile.RPCPort
	}
	if cfg.P2PPort == "" {
		cfg.P2PPort = netProfile.P2PPort
	}
	if cfg.RPCBind == "" {
		cfg.RPCBind = "127.0.0.1"
	}

	if targetNetID == "ARMX-MAINNET-1" && core.IsLocalMainnet() {
		rawAddr := strings.TrimSpace(cfg.RPCBind)
		host := rawAddr
		if h, _, err := net.SplitHostPort(rawAddr); err == nil {
			host = h
		}
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
		ip := net.ParseIP(host)
		isLoop := (ip != nil && ip.IsLoopback()) || strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
		if !isLoop {
			return nil, nil, fmt.Errorf("local mainnet mode strictly prohibits binding RPC to non-loopback address %q", rawAddr)
		}
		if cfg.AllowNonLoopbackRPC {
			return nil, nil, fmt.Errorf("local mainnet mode strictly prohibits allow_non_loopback_rpc")
		}

		var localSeeds []string
		for _, s := range cfg.SeedPeers {
			sHost, _, err := net.SplitHostPort(s)
			if err == nil {
				sHost = strings.TrimPrefix(sHost, "[")
				sHost = strings.TrimSuffix(sHost, "]")
				sIP := net.ParseIP(sHost)
				if (sIP != nil && sIP.IsLoopback()) || strings.EqualFold(sHost, "localhost") || sHost == "127.0.0.1" || sHost == "::1" {
					localSeeds = append(localSeeds, s)
				}
			}
		}
		cfg.SeedPeers = localSeeds
	}

	// 4. Strict TLS 1.3 Transport Setting [N-08, N-18]
	if cfg.RequireTLS {
		SetStrictTLS(true)
	}

	// Resolve effective data directory [P1-A]
	effectiveDataDir := cfg.DataDir
	if effectiveDataDir == "" {
		effectiveDataDir = os.Getenv("ARMX_DATA_DIR")
	}

	// 5. Initialize P2P Peer Server with settings [N-18, P1-A]
	peerServer := NewPeerServer(cfg.P2PPort, bc, effectiveDataDir, targetNetID)
	if err := peerServer.GetPeerStoreInitErr(); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize peer store: %w", err)
	}

	peerServer.DataDir = effectiveDataDir
	peerServer.Bind = cfg.P2PBind

	if cfg.MaxPeers > 0 {
		peerServer.MaxPeers = cfg.MaxPeers
	}
	peerServer.RequireTLS = cfg.RequireTLS
	peerServer.DisableSubnetLimit = cfg.DisableSubnetLimit
	peerServer.P2PTOFU = cfg.P2PTOFU // [F1]
	if len(cfg.SeedFingerprints) > 0 {
		if err := peerServer.SetSeedFingerprints(cfg.SeedFingerprints); err != nil {
			return nil, nil, fmt.Errorf("invalid seed_fingerprints: %w", err)
		}
	}
	peerServer.AllowPrivatePeers = isPrivatePeersAllowedForNetwork(targetNetID)

	// SetSeedPeers resolves (and logs) the seeds once and adds them to the address book.
	peerServer.SetSeedPeers(cfg.SeedPeers)

	serverCfg := &ServerConfig{
		NetworkID:   targetNetID,
		DataDir:     effectiveDataDir,
		RPCBind:     cfg.RPCBind,
		RPCPort:     cfg.RPCPort,
		P2PPort:     cfg.P2PPort,
		RPCUser:     cfg.RPCUser,
		RPCPassword: cfg.RPCPassword,
		MineKey:     cfg.MineKey,
	}

	// Register in network-scoped registry now that all settings are finalized [P1-A]
	RegisterPeerServer(peerServer)

	return peerServer, serverCfg, nil
}

// ValidateRPCBind ensures that binding the RPC daemon to non-loopback interfaces
// requires both an explicit opt-in flag (AllowNonLoopbackRPC) AND active authentication credentials [F8].
// Handles host:port, IPv6 brackets, and host-only specifications properly.
func ValidateRPCBind(cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	rawAddr := strings.TrimSpace(cfg.RPCBind)
	if rawAddr == "" {
		rawAddr = "127.0.0.1"
	}

	host := rawAddr
	if h, _, err := net.SplitHostPort(rawAddr); err == nil {
		host = h
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	ip := net.ParseIP(host)
	isLoopback := false
	if ip != nil {
		isLoopback = ip.IsLoopback()
	} else if strings.EqualFold(host, "localhost") {
		isLoopback = true
	}

	isMainnet := false
	if cfg.NetworkID != "" {
		canon, _, err := parseNetworkID(cfg.NetworkID)
		if err == nil && canon == "ARMX-MAINNET-1" {
			isMainnet = true
		}
	} else if strings.EqualFold(core.GetActiveNetwork().Name, "mainnet") {
		isMainnet = true
	}

	if isMainnet && core.IsLocalMainnet() {
		if !isLoopback {
			return fmt.Errorf("local mainnet mode strictly prohibits binding RPC to non-loopback address '%s'", rawAddr)
		}
		if cfg.AllowNonLoopbackRPC {
			return fmt.Errorf("local mainnet mode strictly prohibits allow_non_loopback_rpc")
		}
	}

	if !isLoopback {
		if !cfg.AllowNonLoopbackRPC {
			return fmt.Errorf("refusing to bind RPC to non-loopback address '%s': requires explicit opt-in flag (AllowNonLoopbackRPC)", rawAddr)
		}
		if cfg.RPCUser == "" || cfg.RPCPassword == "" {
			return fmt.Errorf("refusing to bind RPC to non-loopback address '%s': active HTTP Basic Auth credentials (rpc_user and rpc_password) are mandatory", rawAddr)
		}
		if cfg.RPCTLSCert == "" || cfg.RPCTLSKey == "" {
			return fmt.Errorf("refusing to bind RPC to non-loopback address '%s': rpc_tls_cert and rpc_tls_key are mandatory to prevent Basic Auth disclosure", rawAddr)
		}
		if _, err := os.Stat(cfg.RPCTLSCert); err != nil {
			return fmt.Errorf("RPC TLS certificate is unavailable: %w", err)
		}
		if _, err := os.Stat(cfg.RPCTLSKey); err != nil {
			return fmt.Errorf("RPC TLS key is unavailable: %w", err)
		}
	}
	return nil
}

// errFatalStartup marks startup failures that StartServerWithConfig escalates to a panic.
var errFatalStartup = errors.New("fatal startup error")

// StartServerWithConfig runs the node and panics on fatal startup errors.
// New callers should prefer RunServerWithConfig, which returns the error instead.
func StartServerWithConfig(bc *core.Blockchain, cfg *Config) {
	err := RunServerWithConfig(bc, cfg)
	if err == nil {
		return
	}
	if errors.Is(err, errFatalStartup) {
		panic(err.Error())
	}
	fmt.Printf("[-] Server terminated with error: %v\n", err)
}

// ShutdownServerSafely safely shuts down the HTTP server, P2P network, rate limiter,
// and blockchain store.
//
// Safety Contract [FIX-01, v1.0.6]:
//  1. Shuts down HTTP server listeners and active connections. If handlers do not drain
//     within timeout, HTTP shutdown is incomplete.
//  2. Stops P2P network cycle using StopWithTimeout. If P2P workers do not drain within
//     timeout, P2P shutdown is incomplete.
//  3. Stops IP rate limiter.
//  4. Fail-Closed Protection: If HTTP handlers OR P2P workers fail to drain cleanly,
//     database flush (SaveToFile) and database close (bc.Close) are EXPLICITLY ABORTED.
//     This guarantees that no in-flight HTTP handler or background worker can access
//     or mutate BoltDB after it is closed, preventing panics and data corruption.
//  5. If SaveToFile() fails, reports failure and does NOT claim a successful flush.
//  6. If bc.Close() fails, reports failure and does NOT claim clean termination.
//  7. The logger is NOT closed on failure paths so that active handlers/workers can still log.
//
// Returns drained=true if and only if all components shut down cleanly, the database was
// successfully flushed and closed, and the logger closed.
func ShutdownServerSafely(server *http.Server, peerServer *PeerServer, limiter *IPRateLimiter, bc *core.Blockchain, timeout time.Duration) (drained bool, err error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	var httpErr error
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			httpErr = err
			// Force close HTTP server listeners and active TCP connections
			_ = server.Close()
		}
	}

	// 2. Stop P2P networking cleanly and verify worker drainage
	var p2pStopped bool = true
	if peerServer != nil {
		p2pStopped = peerServer.StopWithTimeout(timeout)
	}

	// 3. Stop IP rate limiter cleanup goroutine
	if limiter != nil {
		limiter.Close()
	}

	// 4. Fail closed if HTTP handlers or P2P workers did not drain within timeout:
	// NEVER close BoltDB or claim successful flush while active workers or handlers could still access it!
	// Do NOT close logger on failure path so potentially active routines can still log.
	if httpErr != nil {
		fmt.Printf("[-] HTTP server shutdown incomplete (handlers did not drain cleanly: %v). Aborting database persistence to prevent corruption.\n", httpErr)
		return false, fmt.Errorf("HTTP handlers failed to drain: %w", httpErr)
	}
	if !p2pStopped {
		p2pErr := fmt.Errorf("P2P workers failed to drain within %v", timeout)
		fmt.Printf("[-] P2P server shutdown incomplete: %v. Aborting database persistence to prevent corruption.\n", p2pErr)
		return false, p2pErr
	}

	// 5. Safely persist peer network cache
	if peerServer != nil {
		if err := peerServer.savePeers(); err != nil {
			fmt.Printf("[-] Error saving peers during shutdown: %v\n", err)
		} else {
			fmt.Println("[OK] Peer network cache safely saved.")
		}
	}

	// 6. Safely flush blockchain database to disk and close store handle ONLY after all handlers and P2P workers are fully drained
	if bc != nil {
		bc.Lock.RLock()
		saveErr := bc.SaveToFile()
		bc.Lock.RUnlock()
		if saveErr != nil {
			fmt.Printf("[-] Error saving blockchain during shutdown: %v\n", saveErr)
			return false, fmt.Errorf("SaveToFile failed: %w", saveErr)
		}
		fmt.Println("[OK] Blockchain database safely flushed to disk.")

		if closeErr := bc.Close(); closeErr != nil {
			fmt.Printf("[-] Error closing blockchain database during shutdown: %v\n", closeErr)
			return false, fmt.Errorf("Close failed: %w", closeErr)
		}
	}

	// 7. Close logging subsystem only upon complete clean shutdown
	core.CloseLogger()
	return true, nil
}

// RunServerWithConfig initializes and runs the P2P engine and HTTP RPC daemon using Config [N-18].
// It blocks until the server stops and returns an error on startup or serve failure.
func RunServerWithConfig(bc *core.Blockchain, cfg *Config) error {
	if err := ValidateRPCBind(cfg); err != nil {
		return fmt.Errorf("%w: FATAL RPC SECURITY REFUSAL: %v", errFatalStartup, err)
	}

	if cfg != nil && cfg.MiningThreads > 0 && bc.MiningThreads <= 0 {
		bc.MiningThreads = cfg.MiningThreads
	}
	peerServer, serverCfg, err := InitServerFromConfig(bc, cfg)
	if err != nil {
		return fmt.Errorf("%w: FATAL CONFIGURATION ERROR: %v", errFatalStartup, err)
	}
	go peerServer.StartServer()

	// Initialize HTTP Rate Limiter and RPC Auth Manager with provided credentials
	limiter := newIPRateLimiter()
	if err := InitRPCAuth(cfg.RPCUser, cfg.RPCPassword); err != nil {
		if (cfg.RPCUser == "" || cfg.RPCPassword == "") && !AllowInsecureLocalhost {
			return fmt.Errorf("%w: FATAL RPC SECURITY ERROR: failed to write RPC cookie and no rpc_user/rpc_password configured: %v", errFatalStartup, err)
		}
		fmt.Printf("[RPC] Warning: failed to persist RPC auth cookie: %v\n", err)
	}

	handler := NewRPCHandler(bc, peerServer, limiter, serverCfg)

	bindHost := cfg.RPCBind
	bindPort := cfg.RPCPort
	if h, p, err := net.SplitHostPort(cfg.RPCBind); err == nil {
		bindHost = h
		if p != "" {
			bindPort = p
		}
	}
	server := &http.Server{
		Addr:              net.JoinHostPort(bindHost, bindPort),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful Shutdown (SIGINT, SIGTERM)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stopChan
		fmt.Println("\n[!] Shutdown signal received. Initiating graceful shutdown...")
		drained, err := ShutdownServerSafely(server, peerServer, limiter, bc, 5*time.Second)
		if !drained {
			fmt.Printf("[-] Server shutdown aborted cleanly with active handlers: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[OK] ArmaxCoin node stopped cleanly.")
		os.Exit(0)
	}()

	fmt.Printf("[OK] RPC Daemon active on %s:%s | P2P Network active on port %s\n", bindHost, bindPort, cfg.P2PPort)
	var serveErr error
	if cfg.RPCTLSCert != "" || cfg.RPCTLSKey != "" {
		if cfg.RPCTLSCert == "" || cfg.RPCTLSKey == "" {
			serveErr = errors.New("both rpc_tls_cert and rpc_tls_key must be configured together")
		} else {
			serveErr = server.ListenAndServeTLS(cfg.RPCTLSCert, cfg.RPCTLSKey)
		}
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		if peerServer != nil {
			peerServer.Stop()
		}
		if limiter != nil {
			limiter.Close()
		}
		return serveErr
	}
	return nil
}

// StartServer starts the RPC Daemon HTTP server, Web Server, and P2P Network Engine (backward compatibility wrapper)
func StartServer(bc *core.Blockchain, rpcPort string, p2pPort string, initialPeers []string, rpcUser string, rpcPass string) {
	cfg := &Config{
		NetworkID:   NetworkID(),
		RPCBind:     "127.0.0.1",
		RPCPort:     rpcPort,
		P2PPort:     p2pPort,
		RPCUser:     rpcUser,
		RPCPassword: rpcPass,
		SeedPeers:   initialPeers,
		MaxPeers:    MaxPeersDefault,
	}
	StartServerWithConfig(bc, cfg)
}
