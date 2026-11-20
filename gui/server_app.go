package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/node"
	"github.com/acxxcxx/armax-coin/wallet"
	"github.com/skip2/go-qrcode"
)

var (
	rpcURL        = "http://127.0.0.1:" + core.TestnetRPCPort
	listenPort    = 18088
	activeWallet  *wallet.Wallet
	activeAddress string

	// Multi-wallet & startup authentication state
	walletMgr            *wallet.WalletManager
	walletMgrMu          sync.Mutex
	activeWalletID       string
	unlockedWallets      = make(map[string]*wallet.Wallet)
	unlockedMu           sync.RWMutex
	autoLockMinutes      int   = 15
	lastActivityUnix     int64 = time.Now().Unix()
	startupAuthRequired  atomic.Bool
	startupAuthenticated atomic.Bool

	// Async embedded node supervisor status & lifecycle handle
	nodeBootstrapMu     sync.RWMutex
	nodeBootstrapStatus = NodeBootstrapStatus{State: "starting", Mode: "embedded", Detail: "Initializing local node..."}
	embeddedNodeMu      sync.Mutex
	embeddedNodeHandle  *node.EmbeddedNodeHandle
	lastNodeInitProfile core.NetworkProfile
	lastNodeInitRPCPort string
	lastNodeInitP2PPort string
	lastNodeInitDataDir string
	appExiting          atomic.Bool

	// Persistent local bbolt history cache
	historyStore   *HistoryStore
	historyStoreMu sync.Mutex

	// Mining state & live reconciliation tracker
	miningActive       bool
	miningLock         sync.Mutex
	miningStopChan     chan struct{}
	miningCancel       context.CancelFunc
	miningDone         chan struct{}
	miningGen          uint64
	miningWaitingSync  atomic.Bool
	miningStartTime    time.Time
	miningLiveHashrate uint64 // atomic bits of float64
	miningLogs         []string
	miningLogsLock     sync.Mutex
	miningTracker      = NewMiningSessionTracker()
	lastTipHeight      int64
	lastTipHash        atomic.Value // string
	lastPeersActive    int64
	lastNodeMaturity   int64
	lastMempoolSize    int64
	lastReconcileUnix  int64

	lastSyncMu     sync.RWMutex
	lastSyncStatus = guiNodeSyncStatus{
		Synced:       true,
		SyncProgress: 100.0,
	}

	// Desktop application lifecycle shutdown coordination
	appExitMu     sync.Mutex
	appExitChan   = make(chan struct{})
	appExitClosed bool
)

func setMiningHps(hps float64) {
	atomic.StoreUint64(&miningLiveHashrate, math.Float64bits(hps))
}

func getMiningHps() float64 {
	bits := atomic.LoadUint64(&miningLiveHashrate)
	return math.Float64frombits(bits)
}

func appendMiningLog(msg string) {
	miningLogsLock.Lock()
	defer miningLogsLock.Unlock()
	ts := time.Now().Format("15:04:05")
	miningLogs = append(miningLogs, fmt.Sprintf("[%s] %s", ts, msg))
	if len(miningLogs) > 100 {
		miningLogs = miningLogs[len(miningLogs)-100:]
	}
}

func getMiningLogs() []string {
	miningLogsLock.Lock()
	defer miningLogsLock.Unlock()
	copied := make([]string, len(miningLogs))
	copy(copied, miningLogs)
	return copied
}

type guiNodeSyncStatus struct {
	Synced         bool    `json:"synced"`
	Syncing        bool    `json:"syncing"`
	Height         int64   `json:"local_height"`
	BestPeerHeight int64   `json:"best_peer_height"`
	SyncProgress   float64 `json:"sync_progress"`
	PeersActive    int64   `json:"peers_active"`
	Reason         string  `json:"sync_reason"`
}

func optimalCPUThreads() int {
	n := runtime.NumCPU()
	if n >= 4 {
		return n / 2
	}
	if n < 1 {
		return 1
	}
	return n
}

func getCachedSyncStatus() guiNodeSyncStatus {
	lastSyncMu.RLock()
	defer lastSyncMu.RUnlock()
	return lastSyncStatus
}

func setCachedSyncStatus(st guiNodeSyncStatus) {
	lastSyncMu.Lock()
	lastSyncStatus = st
	lastSyncMu.Unlock()
}

func computeLegacyV1011SyncStatus(height int64, peersActive int64) guiNodeSyncStatus {
	netName := core.GetActiveNetwork().Name
	isPublic := (netName == "testnet" || netName == "mainnet") && !core.IsLocalMainnet()
	if isPublic && peersActive <= 0 {
		return guiNodeSyncStatus{
			Synced:         false,
			Syncing:        true,
			Height:         height,
			BestPeerHeight: height,
			SyncProgress:   0,
			PeersActive:    0,
			Reason:         "Waiting for P2P peers (legacy v1.0.11 daemon)...",
		}
	}
	return guiNodeSyncStatus{
		Synced:         true,
		Syncing:        false,
		Height:         height,
		BestPeerHeight: height,
		SyncProgress:   100.0,
		PeersActive:    peersActive,
	}
}

func queryNodeSyncStatus(ctx context.Context) guiNodeSyncStatus {
	reqCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(rpcURL, "/")+"/stats", nil)
	if err != nil {
		return guiNodeSyncStatus{Synced: false, Syncing: true, Reason: "Waiting for node RPC connection..."}
	}
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return guiNodeSyncStatus{Synced: false, Syncing: true, Reason: "Waiting for node RPC connection..."}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return guiNodeSyncStatus{Synced: false, Syncing: true, Reason: fmt.Sprintf("Node /stats returned HTTP %d", resp.StatusCode)}
	}
	var stats struct {
		Height         int64   `json:"height"`
		PeersActive    int64   `json:"peers_active"`
		Synced         *bool   `json:"synced"`
		Syncing        *bool   `json:"syncing"`
		BestPeerHeight int64   `json:"best_peer_height"`
		SyncProgress   float64 `json:"sync_progress"`
		SyncReason     string  `json:"sync_reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return guiNodeSyncStatus{Synced: false, Syncing: true, Reason: "Waiting for valid /stats response..."}
	}
	atomic.StoreInt64(&lastTipHeight, stats.Height)
	atomic.StoreInt64(&lastPeersActive, stats.PeersActive)

	var st guiNodeSyncStatus
	if stats.Synced != nil {
		isSyncing := false
		if stats.Syncing != nil {
			isSyncing = *stats.Syncing
		}
		progress := stats.SyncProgress
		if *stats.Synced && progress == 0 {
			progress = 100.0
		}
		st = guiNodeSyncStatus{
			Synced:         *stats.Synced,
			Syncing:        isSyncing,
			Height:         stats.Height,
			BestPeerHeight: stats.BestPeerHeight,
			SyncProgress:   progress,
			PeersActive:    stats.PeersActive,
			Reason:         stats.SyncReason,
		}
	} else {
		st = computeLegacyV1011SyncStatus(stats.Height, stats.PeersActive)
	}
	setCachedSyncStatus(st)
	return st
}

func triggerAppShutdown() {
	appExiting.Store(true)
	appExitMu.Lock()
	if appExitChan != nil && !appExitClosed {
		appExitClosed = true
		close(appExitChan)
	}
	appExitMu.Unlock()
}

func resetAppShutdownForTest() {
	appExiting.Store(false)
	appExitMu.Lock()
	appExitChan = make(chan struct{})
	appExitClosed = false
	appExitMu.Unlock()
}

func getAppExitChan() <-chan struct{} {
	appExitMu.Lock()
	defer appExitMu.Unlock()
	return appExitChan
}

// NodeBootstrapStatus reports embedded or external daemon startup state to the UI.
type NodeBootstrapStatus struct {
	State     string `json:"state"`
	Mode      string `json:"mode"`
	Detail    string `json:"detail"`
	Error     string `json:"error,omitempty"`
	RPCURL    string `json:"rpc_url"`
	NetworkID string `json:"network_id"`
}

func setNodeBootstrapStatus(st NodeBootstrapStatus) {
	nodeBootstrapMu.Lock()
	nodeBootstrapStatus = st
	nodeBootstrapMu.Unlock()
}

func getNodeBootstrapStatus() NodeBootstrapStatus {
	nodeBootstrapMu.RLock()
	defer nodeBootstrapMu.RUnlock()
	return nodeBootstrapStatus
}

func isDirWritable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, dirErr := os.Stat(dir)
	createdDir := os.IsNotExist(dirErr)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".armax-write-probe-*")
	if err != nil {
		if createdDir {
			_ = os.Remove(dir)
		}
		return false
	}
	name := f.Name()
	ok := true
	if runtime.GOOS != "windows" {
		// Peer store persistence (and keystores) require POSIX 0600 permissions;
		// non-POSIX mounts such as WSL /mnt/* without metadata report 0777 after chmod.
		if chmodErr := f.Chmod(0600); chmodErr != nil {
			ok = false
		} else if fi, statErr := f.Stat(); statErr != nil || fi.Mode().Perm() != 0600 {
			ok = false
		}
	}
	_ = f.Close()
	_ = os.Remove(name)
	if !ok && createdDir {
		_ = os.Remove(dir)
	}
	return ok
}

// resolveGUIDataDir picks a guaranteed writable directory (with strict 0600
// permission support on Unix) so 1-click GUI launches work whether opened from
// a portable folder, Start Menu shortcut, read-only install path, or WSL mount.
func resolveGUIDataDir(netName string, explicitDataDir string) string {
	return resolveGUIDataDirWithCheck(netName, explicitDataDir, isDirWritable)
}

func resolveGUIDataDirWithCheck(netName string, explicitDataDir string, usable func(string) bool) string {
	if usable == nil {
		usable = isDirWritable
	}
	if dir := strings.TrimSpace(explicitDataDir); dir != "" {
		return dir
	}
	if env := strings.TrimSpace(os.Getenv("ARMX_DATA_DIR")); env != "" {
		return env
	}

	subDir := strings.ToLower(strings.TrimSpace(netName))
	if subDir == "" {
		subDir = "mainnet"
	}

	hasNetworkData := func(root string) bool {
		sub := root
		if filepath.Base(root) != subDir {
			sub = filepath.Join(root, subDir)
		}
		for _, name := range []string{"blockchain.db", "wallet.json", "wallets.json"} {
			if _, err := os.Stat(filepath.Join(sub, name)); err == nil {
				return true
			}
		}
		return false
	}

	var execDir string
	if execPath, err := os.Executable(); err == nil {
		execDir = filepath.Dir(execPath)
		if hasNetworkData(execDir) && usable(filepath.Join(execDir, subDir)) {
			return execDir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if hasNetworkData(cwd) && usable(filepath.Join(cwd, subDir)) {
			return cwd
		}
	}
	if execDir != "" && usable(filepath.Join(execDir, subDir)) {
		return execDir
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		candidate := filepath.Join(cfgDir, "ArmaxCoin")
		if usable(filepath.Join(candidate, subDir)) {
			return candidate
		}
	}
	fallback := filepath.Join(os.TempDir(), "ArmaxCoin")
	_ = os.MkdirAll(filepath.Join(fallback, subDir), 0700)
	return fallback
}

func startEmbeddedNodeAsync(activeNet core.NetworkProfile, rpcPort, p2pPort, dataDir string) {
	embeddedNodeMu.Lock()
	lastNodeInitProfile = activeNet
	lastNodeInitRPCPort = rpcPort
	lastNodeInitP2PPort = p2pPort
	lastNodeInitDataDir = dataDir
	prev := embeddedNodeHandle
	embeddedNodeHandle = nil
	embeddedNodeMu.Unlock()

	if prev != nil {
		_, _ = prev.Shutdown(2 * time.Second)
	}

	setNodeBootstrapStatus(NodeBootstrapStatus{
		State:     "starting",
		Mode:      "embedded",
		Detail:    "Checking local node connection...",
		RPCURL:    rpcURL,
		NetworkID: activeNet.NetworkID,
	})

	go func() {
		nodeErr := verifyNodeNetworkErr(rpcURL, activeNet.NetworkID)
		if nodeErr == nil {
			fmt.Printf("[+] Connected to running node daemon at %s\n", rpcURL)
			setNodeBootstrapStatus(NodeBootstrapStatus{
				State:     "online",
				Mode:      "external",
				Detail:    fmt.Sprintf("Connected to running node daemon at %s", rpcURL),
				RPCURL:    rpcURL,
				NetworkID: activeNet.NetworkID,
			})
			return
		}
		if strings.Contains(nodeErr.Error(), "network mismatch") || strings.Contains(nodeErr.Error(), "empty network_id") {
			fmt.Fprintf(os.Stderr, "[-] Error: Daemon on %s returned network mismatch: %v\n", rpcURL, nodeErr)
			setNodeBootstrapStatus(NodeBootstrapStatus{
				State:     "error",
				Mode:      "external",
				Detail:    "Daemon network mismatch detected",
				Error:     nodeErr.Error(),
				RPCURL:    rpcURL,
				NetworkID: activeNet.NetworkID,
			})
			return
		}
		if !isLoopbackRPC(rpcURL) {
			setNodeBootstrapStatus(NodeBootstrapStatus{
				State:     "error",
				Mode:      "remote",
				Detail:    "Remote node RPC unreachable",
				Error:     nodeErr.Error(),
				RPCURL:    rpcURL,
				NetworkID: activeNet.NetworkID,
			})
			return
		}

		fmt.Printf("[*] No standalone daemon detected on %s. Starting embedded node...\n", rpcURL)
		setNodeBootstrapStatus(NodeBootstrapStatus{
			State:     "starting",
			Mode:      "embedded",
			Detail:    "Loading blockchain database...",
			RPCURL:    rpcURL,
			NetworkID: activeNet.NetworkID,
		})

		bc, openErr := core.OpenBlockchain(3)
		if openErr != nil {
			fmt.Fprintf(os.Stderr, "[-] Failed to open blockchain database: %v\n", openErr)
			setNodeBootstrapStatus(NodeBootstrapStatus{
				State:     "error",
				Mode:      "embedded",
				Detail:    "Failed to open blockchain database",
				Error:     openErr.Error(),
				RPCURL:    rpcURL,
				NetworkID: activeNet.NetworkID,
			})
			return
		}
		bc.CoinbaseMaturity = activeNet.CoinbaseMaturity

		cfg, err := node.LoadConfig("armax.conf")
		if err != nil {
			cfg = node.DefaultConfig()
		}
		cfg.RPCBind = "127.0.0.1"
		cfg.RPCPort = rpcPort
		cfg.P2PPort = p2pPort
		if activeNet.Name == "testnet" || activeNet.Name == "regtest" || activeNet.Name == "soak" {
			cfg.NetworkID = activeNet.NetworkID
			if len(cfg.SeedPeers) == 0 || (len(core.MainnetProfile.DefaultSeeds) > 0 && len(cfg.SeedPeers) > 0 && cfg.SeedPeers[0] == core.MainnetProfile.DefaultSeeds[0]) {
				cfg.SeedPeers = activeNet.DefaultSeeds
			}
		}
		if cfg.NetworkID == "" {
			cfg.NetworkID = activeNet.NetworkID
		}
		if cfg.StoreCompression {
			core.SetStoreCompression(true)
		}
		if dataDir != "" {
			cfg.DataDir = dataDir
		}

		setNodeBootstrapStatus(NodeBootstrapStatus{
			State:     "starting",
			Mode:      "embedded",
			Detail:    "Starting embedded P2P & RPC node...",
			RPCURL:    rpcURL,
			NetworkID: activeNet.NetworkID,
		})

		handle, startErr := node.StartEmbeddedServer(bc, cfg)
		if startErr != nil {
			_ = bc.Close()
			fmt.Fprintf(os.Stderr, "[-] Failed to start embedded node: %v\n", startErr)
			setNodeBootstrapStatus(NodeBootstrapStatus{
				State:     "error",
				Mode:      "embedded",
				Detail:    "Embedded node startup failed",
				Error:     startErr.Error(),
				RPCURL:    rpcURL,
				NetworkID: activeNet.NetworkID,
			})
			return
		}

		if appExiting.Load() {
			_, _ = handle.Shutdown(2 * time.Second)
			return
		}

		embeddedNodeMu.Lock()
		embeddedNodeHandle = handle
		embeddedNodeMu.Unlock()

		setNodeBootstrapStatus(NodeBootstrapStatus{
			State:     "online",
			Mode:      "embedded",
			Detail:    fmt.Sprintf("Embedded node online (RPC %s · P2P %s)", cfg.RPCPort, cfg.P2PPort),
			RPCURL:    rpcURL,
			NetworkID: activeNet.NetworkID,
		})

		go func(h *node.EmbeddedNodeHandle) {
			doneErr := <-h.Done()
			if doneErr != nil && !appExiting.Load() {
				setNodeBootstrapStatus(NodeBootstrapStatus{
					State:     "error",
					Mode:      "embedded",
					Detail:    "Embedded node stopped unexpectedly",
					Error:     doneErr.Error(),
					RPCURL:    rpcURL,
					NetworkID: activeNet.NetworkID,
				})
			}
		}(handle)
	}()
}

func shutdownEmbeddedNode(timeout time.Duration) {
	embeddedNodeMu.Lock()
	h := embeddedNodeHandle
	embeddedNodeHandle = nil
	embeddedNodeMu.Unlock()
	if h != nil {
		_, _ = h.Shutdown(timeout)
	}
}

func handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(getNodeBootstrapStatus())
}

func handleNodeRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := getNodeBootstrapStatus()
	if st.State == "starting" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
		return
	}
	embeddedNodeMu.Lock()
	netProf := lastNodeInitProfile
	rPort := lastNodeInitRPCPort
	pPort := lastNodeInitP2PPort
	dDir := lastNodeInitDataDir
	embeddedNodeMu.Unlock()

	if netProf.NetworkID == "" {
		netProf = core.GetActiveNetwork()
	}
	if rPort == "" {
		rPort = netProf.RPCPort
	}
	if pPort == "" {
		pPort = netProf.P2PPort
	}
	startEmbeddedNodeAsync(netProf, rPort, pPort, dDir)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "retrying"})
}

func runGUIServerMain() {
	networkPtr := flag.String("network", "testnet", "Network profile (testnet, mainnet) [L4-1]")
	portFlag := flag.Int("port", 18088, "HTTP GUI Wallet port")
	rpcFlag := flag.String("rpc", "", "Armaxcoin node RPC URL (overrides default)")
	rpcPortPtr := flag.String("rpcport", "", "RPC Daemon port (overrides default)")
	p2pPortPtr := flag.String("p2p", "", "P2P port (deprecated: use -p2pport)")
	p2pPortNewPtr := flag.String("p2pport", "", "P2P port (overrides default)")
	dataDirPtr := flag.String("datadir", "", "Custom data directory")
	infoPtr := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionPtr := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	noBrowserFlag := flag.Bool("no-browser", false, "Do not automatically launch desktop window")
	flag.Parse()

	if *versionPtr {
		fmt.Println(armguiVersionString())
		return
	}

	effRPCPort := *rpcPortPtr
	effP2PPort := *p2pPortNewPtr
	portExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portExplicit = true
		}
		if f.Name == "p2p" {
			fmt.Println("[!] Warning: -p2p is deprecated, please use -p2pport instead")
			if effP2PPort == "" {
				effP2PPort = *p2pPortPtr
			}
		}
	})

	origEnvDataDir := strings.TrimSpace(os.Getenv("ARMX_DATA_DIR"))
	if *dataDirPtr != "" {
		_ = os.Setenv("ARMX_DATA_DIR", *dataDirPtr)
	}

	netChoice := *networkPtr
	isExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "network" {
			isExplicit = true
		}
	})
	if !isExplicit {
		if envNet := os.Getenv("ARMX_NETWORK"); envNet != "" {
			if strings.EqualFold(envNet, "regtest") {
				fmt.Println("[!] Note: regtest profile cannot be selected via environment variable. Please specify '-network regtest' explicitly.")
			} else {
				netChoice = envNet
			}
		}
	}
	if strings.EqualFold(netChoice, "regtest") && !isExplicit {
		fmt.Println("[-] Error: regtest profile cannot be selected without explicit flag '-network regtest'")
		os.Exit(1)
	}
	cliNet, err := core.ResolveCLINetwork(netChoice)
	if err != nil {
		fmt.Printf("[-] Invalid network profile '%s': %v\n", netChoice, err)
		os.Exit(1)
	}
	if err := core.SetNetwork(netChoice); err != nil {
		fmt.Printf("[-] Failed to set network profile '%s': %v\n", netChoice, err)
		os.Exit(1)
	}
	activeNet := core.GetActiveNetwork()

	resolvedDataDir := resolveGUIDataDir(activeNet.Name, *dataDirPtr)
	if os.Getenv("ARMX_DATA_DIR") == "" && resolvedDataDir != "" {
		_ = os.Setenv("ARMX_DATA_DIR", resolvedDataDir)
	}

	fmt.Println("==================================================")
	fmt.Println(core.NetworkWarningLine())
	fmt.Printf("  ARMAXCOIN (%s) - DESKTOP WALLET\n", core.ActiveTicker())
	fmt.Println("==================================================")

	if runtime.GOOS != "windows" && resolvedDataDir != "" {
		subDir := strings.ToLower(strings.TrimSpace(activeNet.Name))
		if subDir == "" {
			subDir = "mainnet"
		}
		if *dataDirPtr != "" || origEnvDataDir != "" {
			if !isDirWritable(filepath.Join(resolvedDataDir, subDir)) {
				fmt.Printf("[!] Warning: Data directory %s does not enforce POSIX 0600 permissions (e.g. WSL /mnt/* mount); peer persistence requires a POSIX filesystem such as ~/.config/ArmaxCoin.\n", resolvedDataDir)
			}
		} else if execPath, execErr := os.Executable(); execErr == nil {
			execDir := filepath.Dir(execPath)
			if execDir != resolvedDataDir {
				fmt.Printf("[*] Using POSIX data directory %s (executable folder %s does not support strict 0600 file permissions or is read-only).\n", resolvedDataDir, execDir)
				execWallet := filepath.Join(execDir, subDir, "wallet.json")
				resolvedWallet := filepath.Join(resolvedDataDir, subDir, "wallet.json")
				if _, errExecW := os.Stat(execWallet); errExecW == nil {
					if _, errResW := os.Stat(resolvedWallet); os.IsNotExist(errResW) {
						fmt.Printf("[*] Note: Existing wallet keystore found at %s. Copy it to %s or import it via Manage Wallets.\n", execWallet, resolvedWallet)
					}
				}
			}
		}
	}

	if *infoPtr {
		fmt.Printf("[+] Target Network   : %s (%s, RPC: %s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID, cliNet.RPCPort)
		fmt.Printf("[+] Network Profile  : %s (%s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID)
		rpcP := cliNet.RPCPort
		if effRPCPort != "" {
			rpcP = effRPCPort
		}
		p2pP := cliNet.P2PPort
		if effP2PPort != "" {
			p2pP = effP2PPort
		}
		fmt.Printf("[+] RPC Port         : %s\n", rpcP)
		fmt.Printf("[+] P2P Port         : %s\n", p2pP)
		fmt.Printf("[+] Data Directory   : %s\n", resolvedDataDir)
		for _, seed := range cliNet.DefaultSeeds {
			fmt.Printf("[+] Seed Peer        : %s\n", seed)
		}
		return
	}

	rpcPort := cliNet.RPCPort
	if effRPCPort != "" {
		rpcPort = effRPCPort
	}
	p2pPort := cliNet.P2PPort
	if effP2PPort != "" {
		p2pPort = effP2PPort
	}

	listenPort = *portFlag
	if *rpcFlag != "" {
		rpcURL = strings.TrimRight(*rpcFlag, "/")
		if effRPCPort == "" && isLoopbackRPC(rpcURL) {
			if u, parseErr := url.Parse(rpcURL); parseErr == nil && u.Port() != "" {
				rpcPort = u.Port()
			}
		}
	} else if effRPCPort != "" {
		rpcURL = "http://127.0.0.1:" + effRPCPort
	} else {
		rpcURL = "http://127.0.0.1:" + cliNet.RPCPort
	}

	loadOrInitWallet()
	_ = ensureHistoryStore()
	defer zeroizeAllUnlockedWallets()
	defer closeHistoryStore()
	defer shutdownEmbeddedNode(5 * time.Second)
	defer stopMiningInternal()

	go startIdleAutoLockLoop()
	startEmbeddedNodeAsync(activeNet, rpcPort, p2pPort, resolvedDataDir)

	serverMux := buildGUIServeMux()
	bindTarget := fmt.Sprintf("127.0.0.1:%d", listenPort)
	if desktopUsesEmbeddedWindow() && !portExplicit {
		bindTarget = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", bindTarget)
	if err != nil && !portExplicit {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to bind GUI port %d: %v\n", listenPort, err)
		os.Exit(1)
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		listenPort = tcpAddr.Port
	}
	guiURL := fmt.Sprintf("http://127.0.0.1:%d", listenPort)

	sessionToken := initSessionToken()
	launchSecret := generateLaunchSecret()
	requireHeaderOnly := desktopUsesEmbeddedWindow() && !*noBrowserFlag
	handler := launchSessionGuard(secureGUIHandler(serverMux, sessionToken), sessionToken, launchSecret, requireHeaderOnly)

	fmt.Println("=================================================================")
	fmt.Printf("   ARMAXCOIN CORE - DESKTOP WALLET (v%s - %s)\n", core.Version, strings.ToUpper(runtime.GOOS))
	fmt.Println("=================================================================")
	fmt.Printf("[*] Data Directory:     %s\n", resolvedDataDir)
	fmt.Printf("[*] Target Node RPC:    %s\n", rpcURL)
	fmt.Printf("[*] GUI Wallet URL:     %s\n", guiURL)
	if *noBrowserFlag || !desktopUsesEmbeddedWindow() {
		fmt.Printf("[*] Launch URL:         %s/auth/launch?ticket=%s\n", guiURL, launchSecret)
	}
	fmt.Println("=================================================================")

	srv := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(listener)
	}()

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			triggerAppShutdown()
		case <-getAppExitChan():
		}
		signal.Stop(sigChan)
	}()

	go func() {
		if err := <-serveDone; err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "GUI Server Error: %v\n", err)
			triggerAppShutdown()
		}
	}()

	windowTitle := fmt.Sprintf("Armaxcoin Core - Desktop Wallet v%s [%s]", core.Version, strings.ToUpper(activeNet.Name))
	if !*noBrowserFlag {
		if winErr := runDesktopWindow(guiURL, launchSecret, windowTitle, getAppExitChan(), triggerAppShutdown); winErr != nil {
			fmt.Fprintf(os.Stderr, "[-] Desktop window error: %v\n", winErr)
			triggerAppShutdown()
		}
	} else {
		<-getAppExitChan()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
}

func buildGUIServeMux() *http.ServeMux {
	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", handleWebUI)
	serverMux.HandleFunc("/assets/armax_logo.png", handleAssetLogoPNG)
	serverMux.HandleFunc("/assets/armax_text.png", handleAssetTextPNG)
	serverMux.HandleFunc("/favicon.ico", handleAssetFaviconICO)
	serverMux.HandleFunc("/api/node/status", handleNodeStatus)
	serverMux.HandleFunc("/api/node/retry", handleNodeRetry)
	serverMux.HandleFunc("/api/stats", handleStats)
	serverMux.HandleFunc("/api/peers", handlePeers)
	serverMux.HandleFunc("/api/chain", handleChain)
	serverMux.HandleFunc("/api/mempool", handleMempool)
	serverMux.HandleFunc("/api/cli", handleCLI)
	serverMux.HandleFunc("/api/balance", handleBalance)
	serverMux.HandleFunc("/api/history", handleHistory)
	serverMux.HandleFunc("/api/fee-estimate", handleFeeEstimate)
	serverMux.HandleFunc("/api/send", handleSend)
	serverMux.HandleFunc("/api/mine/start", handleMineStart)
	serverMux.HandleFunc("/api/mine/stop", handleMineStop)
	serverMux.HandleFunc("/api/mine/status", handleMineStatus)
	serverMux.HandleFunc("/api/addressbook", handleAddressBook)
	serverMux.HandleFunc("/api/qr", handleQRCode)
	serverMux.HandleFunc("/api/wallet/info", handleWalletInfo)
	serverMux.HandleFunc("/api/wallet/create", handleWalletCreate)
	serverMux.HandleFunc("/api/wallet/restore", handleWalletRestore)
	serverMux.HandleFunc("/api/wallets", handleWalletsList)
	serverMux.HandleFunc("/api/wallets/select", handleWalletsSelect)
	serverMux.HandleFunc("/api/wallets/create", handleWalletsCreate)
	serverMux.HandleFunc("/api/wallets/restore", handleWalletsRestore)
	serverMux.HandleFunc("/api/wallets/import", handleWalletsImport)
	serverMux.HandleFunc("/api/wallets/rename", handleWalletsRename)
	serverMux.HandleFunc("/api/wallets/backup", handleWalletsBackup)
	serverMux.HandleFunc("/api/wallets/unlock", handleWalletsUnlock)
	serverMux.HandleFunc("/api/wallets/lock", handleWalletsLock)
	serverMux.HandleFunc("/api/wallets/autolock", handleWalletsAutoLock)
	serverMux.HandleFunc("/api/export-csv", handleExportCSV)
	serverMux.HandleFunc("/api/app/exit", handleAppExit)
	return serverMux
}

var (
	guiSessionToken string
	guiTokenOnce    sync.Once
)

func initSessionToken() string {
	guiTokenOnce.Do(func() {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			panic(fmt.Sprintf("crypto/rand initialization failure: %v", err))
		}
		guiSessionToken = hex.EncodeToString(tokenBytes)
	})
	return guiSessionToken
}

func generateLaunchSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand initialization failure: %v", err))
	}
	return hex.EncodeToString(b)
}

func constantTimeTokenEq(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func applyGUISecurityHeaders(w http.ResponseWriter, path string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if path == "/" || strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/auth/") {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	}
}

func isAllowedLoopbackOrigin(origin string) bool {
	if strings.EqualFold(origin, "null") {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func validateLocalRequestContext(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "" {
		http.Error(w, "Forbidden: Invalid Host header", http.StatusForbidden)
		return false
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if !isAllowedLoopbackOrigin(origin) {
			http.Error(w, "Forbidden: Invalid Origin header", http.StatusForbidden)
			return false
		}
	}
	if sfs := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); sfs != "" && strings.EqualFold(sfs, "cross-site") {
		http.Error(w, "Forbidden: Cross-site request rejected", http.StatusForbidden)
		return false
	}
	return true
}

func launchSessionGuard(next http.Handler, sessionToken string, launchSecret string, requireHeaderOnly bool) http.Handler {
	var ticketUsed atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyGUISecurityHeaders(w, r.URL.Path)
		if !validateLocalRequestContext(w, r) {
			return
		}

		if r.URL.Path == "/auth/launch" {
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// Already-authenticated browsers revisiting /auth/launch (e.g. Back button) redirect cleanly
			if cookie, err := r.Cookie("armax_gui_token"); err == nil && constantTimeTokenEq(cookie.Value, sessionToken) {
				w.Header().Set("Location", "/")
				w.WriteHeader(http.StatusFound)
				return
			}
			ticket := r.URL.Query().Get("ticket")
			if !constantTimeTokenEq(ticket, launchSecret) || !ticketUsed.CompareAndSwap(false, true) {
				http.Error(w, "Forbidden: Invalid or already-consumed launch ticket", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "armax_gui_token",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		validLaunchHeader := constantTimeTokenEq(r.Header.Get("X-Armx-Launch-Auth"), launchSecret)
		authorized := validLaunchHeader || constantTimeTokenEq(r.Header.Get("X-Armx-GUI-Token"), sessionToken)
		if !authorized {
			if cookie, err := r.Cookie("armax_gui_token"); err == nil && constantTimeTokenEq(cookie.Value, sessionToken) {
				authorized = true
			}
		}
		if !authorized && !requireHeaderOnly && r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/api/qr", "/api/export-csv", "/api/wallets/backup":
				if constantTimeTokenEq(r.URL.Query().Get("token"), sessionToken) {
					authorized = true
				}
			}
		}
		if !authorized {
			if requireHeaderOnly {
				http.Error(w, "Forbidden: Native GUI launch authentication required", http.StatusForbidden)
			} else {
				http.Error(w, "Forbidden: GUI session authorization required (open the Launch URL printed in the armgui terminal)", http.StatusForbidden)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isStartupAllowedAPI(path string) bool {
	switch path {
	case "/api/wallet/info",
		"/api/node/status",
		"/api/node/retry",
		"/api/wallets/unlock",
		"/api/wallet/create",
		"/api/wallet/restore",
		"/api/app/exit":
		return true
	default:
		return false
	}
}

func secureGUIHandler(mux *http.ServeMux, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyGUISecurityHeaders(w, r.URL.Path)

		// 1. Mitigate DNS Rebinding, CSRF Origin & cross-site fetches
		if !validateLocalRequestContext(w, r) {
			return
		}

		// 2. Guard all /api/* routes with session token
		if strings.HasPrefix(r.URL.Path, "/api/") {
			reqToken := r.Header.Get("X-Armx-GUI-Token")
			if reqToken == "" {
				if cookie, err := r.Cookie("armax_gui_token"); err == nil {
					reqToken = cookie.Value
				}
			}
			if reqToken == "" && r.Method == http.MethodGet {
				reqToken = r.URL.Query().Get("token")
			}
			if token != "" && (reqToken == "" || subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1) {
				http.Error(w, `{"error":"Unauthorized: Invalid or missing GUI session token"}`, http.StatusUnauthorized)
				return
			}

			// 3. Enforce strict startup authentication gate before exposing any wallet data or watch-only UI
			if startupAuthRequired.Load() && !startupAuthenticated.Load() && !isStartupAllowedAPI(r.URL.Path) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusLocked)
				_, _ = w.Write([]byte(`{"error":"Wallet locked: passphrase authentication required on startup","startup_locked":true}`))
				return
			}
		}

		mux.ServeHTTP(w, r)
	})
}

func handleAssetLogoPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(armaxLogoPNG)
}

func handleAssetTextPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(armaxTextPNG)
}

func handleAssetFaviconICO(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(armaxLogoICO)
}

func handleQRCode(w http.ResponseWriter, r *http.Request) {
	content := strings.TrimSpace(r.URL.Query().Get("content"))
	if content == "" {
		content = activeAddress
	}
	if content == "" || len(content) > 512 {
		http.Error(w, "Invalid QR content", http.StatusBadRequest)
		return
	}
	pngBytes, err := qrcode.Encode(content, qrcode.Medium, 220)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode QR: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pngBytes)
}

func ensureWalletManager() *wallet.WalletManager {
	walletMgrMu.Lock()
	defer walletMgrMu.Unlock()
	if walletMgr == nil {
		walletMgr = wallet.NewWalletManager()
	}
	return walletMgr
}

func ensureHistoryStore() *HistoryStore {
	historyStoreMu.Lock()
	defer historyStoreMu.Unlock()
	if historyStore == nil {
		dbPath := filepath.Join(filepath.Dir(node.GetRPCAuthCookiePath()), "gui_history.db")
		store, err := OpenHistoryStore(dbPath)
		if err == nil {
			historyStore = store
		}
	}
	return historyStore
}

func closeHistoryStore() {
	historyStoreMu.Lock()
	defer historyStoreMu.Unlock()
	if historyStore != nil {
		_ = historyStore.Close()
		historyStore = nil
	}
}

func touchWalletActivity() {
	atomic.StoreInt64(&lastActivityUnix, time.Now().Unix())
}

func zeroizeAllUnlockedWallets() {
	unlockedMu.Lock()
	for k, w := range unlockedWallets {
		if w != nil {
			w.Zeroize()
		}
		delete(unlockedWallets, k)
	}
	if activeWallet != nil {
		activeWallet.Zeroize()
		activeWallet = nil
	}
	unlockedMu.Unlock()
}

func startIdleAutoLockLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-getAppExitChan():
			return
		case <-ticker.C:
			unlockedMu.RLock()
			mins := autoLockMinutes
			unlockedMu.RUnlock()
			if mins <= 0 {
				continue
			}
			last := atomic.LoadInt64(&lastActivityUnix)
			if time.Since(time.Unix(last, 0)) >= time.Duration(mins)*time.Minute {
				zeroizeAllUnlockedWallets()
			}
		}
	}
}

func findWalletPath() string {
	if wm := ensureWalletManager(); wm != nil {
		return wm.ActiveKeystorePath()
	}
	return wallet.GetDefaultWalletPath()
}

func findAddressBookPath() string {
	if _, err := os.Stat("addressbook.json"); err == nil {
		return "addressbook.json"
	}
	return filepath.Join(filepath.Dir(node.GetRPCAuthCookiePath()), "addressbook.json")
}

func loadOrInitWallet() {
	wm := ensureWalletManager()
	if activeEntry, ok := wm.Active(); ok {
		activeWalletID = activeEntry.ID
		activeAddress = activeEntry.Address
		activeWallet = nil

		if envPass := os.Getenv("ARMX_WALLET_PASSPHRASE"); envPass != "" {
			if w, err := wm.DecryptWallet(activeEntry.ID, envPass); err == nil {
				unlockedMu.Lock()
				unlockedWallets[activeEntry.ID] = w
				activeWallet = w
				unlockedMu.Unlock()
				startupAuthRequired.Store(false)
				startupAuthenticated.Store(true)
				touchWalletActivity()
				return
			}
		}

		// Wallet exists on disk -> strictly enforce passphrase gate on startup
		startupAuthRequired.Store(true)
		startupAuthenticated.Store(false)
		return
	}

	walletFile := findWalletPath()
	if envPass := os.Getenv("ARMX_WALLET_PASSPHRASE"); len(envPass) >= 12 && !wallet.IsLegacyPassphrase(envPass) {
		entry, w, _, err := wm.Create("Default Wallet", envPass)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] Failed to create wallet: %v\n", err)
			return
		}
		unlockedMu.Lock()
		unlockedWallets[entry.ID] = w
		activeWallet = w
		activeWalletID = entry.ID
		activeAddress = entry.Address
		unlockedMu.Unlock()
		startupAuthRequired.Store(false)
		startupAuthenticated.Store(true)
		touchWalletActivity()
		fmt.Printf("[+] Initialized encrypted wallet keystore at %s\n", walletFile)
		return
	}

	startupAuthRequired.Store(false)
	startupAuthenticated.Store(false)
	fmt.Printf("[!] No wallet keystore found at %s. Onboarding required.\n", walletFile)
}

func getActiveUnlockedWalletSnapshot() *wallet.Wallet {
	unlockedMu.RLock()
	defer unlockedMu.RUnlock()
	if activeWalletID != "" {
		if w := unlockedWallets[activeWalletID]; w != nil && w.PrivateKey != nil {
			return w
		}
	}
	if activeWallet != nil && activeWallet.PrivateKey != nil {
		return activeWallet
	}
	return nil
}

func handleAppExit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stopMiningInternal()
	zeroizeAllUnlockedWallets()
	closeHistoryStore()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "exiting"})
	go func() {
		time.Sleep(120 * time.Millisecond)
		triggerAppShutdown()
	}()
}

func handleWalletInfo(w http.ResponseWriter, r *http.Request) {
	label := ""
	if activeWalletID != "" {
		if wm := ensureWalletManager(); wm != nil {
			if entry, ok := wm.Active(); ok && entry.ID == activeWalletID {
				label = entry.Label
			}
		}
	}

	startupLocked := startupAuthRequired.Load() && !startupAuthenticated.Load()
	exposedAddr := activeAddress
	if startupLocked {
		// Do not leak wallet address until the startup passphrase has been verified
		exposedAddr = ""
	}
	isUnlocked := getActiveUnlockedWalletSnapshot() != nil

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"address":             exposedAddr,
		"wallet_path":         findWalletPath(),
		"initialized":         activeAddress != "",
		"startup_locked":      startupLocked,
		"locked":              !isUnlocked,
		"unlocked":            isUnlocked,
		"active_wallet_id":    activeWalletID,
		"active_wallet_label": label,
		"version":             core.Version,
		"network":             core.ActiveNetwork.NetworkID,
		"ticker":              core.TickerForNetworkID(core.ActiveNetwork.NetworkID),
		"crypto":              "ML-DSA-65 (FIPS 204)",
	})
}

func handleWalletCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Label      string `json:"label"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.Passphrase) < 12 {
		http.Error(w, "Passphrase must be at least 12 characters", http.StatusBadRequest)
		return
	}
	if wallet.IsLegacyPassphrase(req.Passphrase) {
		http.Error(w, "Cannot use insecure legacy default passphrase", http.StatusBadRequest)
		return
	}

	wm := ensureWalletManager()
	entry, newW, mnemonic, err := wm.Create(req.Label, req.Passphrase)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate wallet: %v", err), http.StatusInternalServerError)
		return
	}

	unlockedMu.Lock()
	unlockedWallets[entry.ID] = newW
	activeWallet = newW
	activeWalletID = entry.ID
	activeAddress = entry.Address
	unlockedMu.Unlock()
	startupAuthenticated.Store(true)
	touchWalletActivity()

	w.Header().Set("Content-Type", "application/json")
	payload, _ := json.Marshal(map[string]interface{}{
		"status":   "success",
		"id":       entry.ID,
		"label":    entry.Label,
		"address":  activeAddress,
		"mnemonic": mnemonic,
	})
	newW.ClearMnemonic()
	_, _ = w.Write(payload)
}

func handleWalletRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Label      string `json:"label"`
		Mnemonic   string `json:"mnemonic"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	mnemonic := strings.TrimSpace(req.Mnemonic)
	if mnemonic == "" {
		http.Error(w, "Mnemonic cannot be empty", http.StatusBadRequest)
		return
	}
	if len(req.Passphrase) < 12 {
		http.Error(w, "Passphrase must be at least 12 characters", http.StatusBadRequest)
		return
	}
	if wallet.IsLegacyPassphrase(req.Passphrase) {
		http.Error(w, "Cannot use insecure legacy default passphrase", http.StatusBadRequest)
		return
	}

	wm := ensureWalletManager()
	entry, restored, err := wm.Restore(req.Label, mnemonic, req.Passphrase)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to restore: %v", err), http.StatusBadRequest)
		return
	}

	unlockedMu.Lock()
	unlockedWallets[entry.ID] = restored
	activeWallet = restored
	activeWalletID = entry.ID
	activeAddress = entry.Address
	unlockedMu.Unlock()
	startupAuthenticated.Store(true)
	touchWalletActivity()
	restored.ClearMnemonic()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"id":      entry.ID,
		"label":   entry.Label,
		"address": activeAddress,
	})
}

func handleWalletsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wm := ensureWalletManager()
	list := wm.List()
	if activeEntry, ok := wm.Active(); ok && activeWalletID == "" {
		activeWalletID = activeEntry.ID
		activeAddress = activeEntry.Address
	}

	type walletDTO struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Address   string `json:"address"`
		FilePath  string `json:"file_path"`
		CreatedAt string `json:"created_at"`
		IsActive  bool   `json:"is_active"`
		Unlocked  bool   `json:"unlocked"`
	}

	unlockedMu.RLock()
	mins := autoLockMinutes
	out := make([]walletDTO, 0, len(list))
	for _, e := range list {
		uw := unlockedWallets[e.ID]
		isUnl := uw != nil && uw.PrivateKey != nil
		out = append(out, walletDTO{
			ID:        e.ID,
			Label:     e.Label,
			Address:   e.Address,
			FilePath:  e.FilePath,
			CreatedAt: e.CreatedAt,
			IsActive:  e.ID == activeWalletID,
			Unlocked:  isUnl,
		})
	}
	unlockedMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active_id":         activeWalletID,
		"active_address":    activeAddress,
		"auto_lock_minutes": mins,
		"wallets":           out,
	})
}

func handleWalletsSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	wm := ensureWalletManager()
	entry, err := wm.Select(strings.TrimSpace(req.ID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	unlockedMu.Lock()
	activeWalletID = entry.ID
	activeAddress = entry.Address
	activeWallet = unlockedWallets[entry.ID]
	unlockedMu.Unlock()
	touchWalletActivity()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "selected",
		"id":       entry.ID,
		"label":    entry.Label,
		"address":  entry.Address,
		"unlocked": activeWallet != nil && activeWallet.PrivateKey != nil,
	})
}

func handleWalletsCreate(w http.ResponseWriter, r *http.Request) {
	handleWalletCreate(w, r)
}

func handleWalletsRestore(w http.ResponseWriter, r *http.Request) {
	handleWalletRestore(w, r)
}

func handleWalletsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, wallet.MaxKeystoreFileSize+4096)
	var req struct {
		Label    string          `json:"label"`
		Keystore json.RawMessage `json:"keystore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	rawKS := []byte(strings.TrimSpace(string(req.Keystore)))
	if len(rawKS) > 0 && rawKS[0] == '"' {
		var unquoted string
		if json.Unmarshal(rawKS, &unquoted) == nil {
			rawKS = []byte(unquoted)
		}
	}
	wm := ensureWalletManager()
	entry, err := wm.ImportKeystoreJSON(req.Label, rawKS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	unlockedMu.Lock()
	activeWalletID = entry.ID
	activeAddress = entry.Address
	activeWallet = nil
	unlockedMu.Unlock()
	touchWalletActivity()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "imported",
		"id":      entry.ID,
		"label":   entry.Label,
		"address": entry.Address,
	})
}

func handleWalletsRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	wm := ensureWalletManager()
	entry, err := wm.Rename(strings.TrimSpace(req.ID), req.Label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "renamed",
		"id":     entry.ID,
		"label":  entry.Label,
	})
}

func handleWalletsBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		id = activeWalletID
	}
	wm := ensureWalletManager()
	var path string
	var err error
	if id != "" {
		path, err = wm.ResolvePath(id)
	} else {
		path = findWalletPath()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read keystore: %v", err), http.StatusNotFound)
		return
	}
	filename := "armax_wallet_backup.json"
	if id != "" {
		filename = fmt.Sprintf("armax_wallet_%s.json", id)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=%s", filename))
	_, _ = w.Write(data)
}

func handleWalletsUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID         string `json:"id"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = activeWalletID
	}
	wm := ensureWalletManager()
	decW, err := wm.DecryptWallet(id, req.Passphrase)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unlock failed: %v", err), http.StatusUnauthorized)
		return
	}
	unlockedMu.Lock()
	unlockedWallets[id] = decW
	if id == activeWalletID || activeAddress == "" {
		activeWalletID = id
		activeAddress = decW.Address
		activeWallet = decW
	}
	unlockedMu.Unlock()
	startupAuthenticated.Store(true)
	touchWalletActivity()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "unlocked",
		"id":      id,
		"address": decW.Address,
	})
}

func handleWalletsLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	zeroizeAllUnlockedWallets()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "locked"})
}

func handleWalletsAutoLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Minutes < 0 || req.Minutes > 120 {
		http.Error(w, "Minutes must be between 0 and 120", http.StatusBadRequest)
		return
	}
	unlockedMu.Lock()
	autoLockMinutes = req.Minutes
	unlockedMu.Unlock()
	touchWalletActivity()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "updated",
		"auto_lock_minutes": req.Minutes,
	})
}

func handleCLI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("command")))
	arg := strings.TrimSpace(r.URL.Query().Get("arg"))
	path := ""
	switch command {
	case "getnetworkinfo", "getblockchaininfo":
		path = "/stats"
	case "getmempoolinfo", "mempool":
		path = "/mempool"
	case "getpeerinfo":
		path = "/peers"
	case "getbalance":
		if arg == "" {
			arg = activeAddress
		}
		if !core.ValidateAddress(arg) {
			http.Error(w, "Invalid address. Use getbalance [armx1... address]", http.StatusBadRequest)
			return
		}
		path = "/balance?address=" + url.QueryEscape(arg)
	case "gethistory":
		if arg == "" {
			arg = activeAddress
		}
		if !core.ValidateAddress(arg) {
			http.Error(w, "Invalid address. Use gethistory [armx1... address]", http.StatusBadRequest)
			return
		}
		path = "/history?address=" + url.QueryEscape(arg)
	default:
		http.Error(w, "Unknown command. Supported: help, getnetworkinfo, getblockchaininfo, getpeerinfo, getbalance [address], gethistory [address], getmempoolinfo", http.StatusBadRequest)
		return
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(rpcURL, "/") + path)
	if err != nil {
		http.Error(w, "Node query failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		http.Error(w, "Failed to read node response", http.StatusInternalServerError)
		return
	}
	if command == "getpeerinfo" {
		bodyBytes = filterSafePeersJSON(bodyBytes, core.GetActiveNetwork())
	} else if (command == "getmempoolinfo" || command == "mempool") && resp.StatusCode == http.StatusOK {
		bodyBytes = formatMempoolInfoJSON(bodyBytes, core.ActiveNetwork.NetworkID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(bodyBytes)
}

func verifyNodeNetworkErr(url string, expectedNetID string) error {
	client := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(url + "/stats")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned HTTP %d", resp.StatusCode)
	}
	hdrID := strings.TrimSpace(resp.Header.Get("X-Network-ID"))
	if hdrID == "" {
		return errors.New("missing X-Network-ID response header")
	}
	if hdrID != expectedNetID {
		return fmt.Errorf("network mismatch on response header: expected '%s', got '%s'", expectedNetID, hdrID)
	}
	var stats struct {
		NetworkID string `json:"network_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("malformed /stats response: %w", err)
	}
	if stats.NetworkID == "" {
		return errors.New("node returned empty network_id")
	}
	if stats.NetworkID != expectedNetID {
		return fmt.Errorf("network mismatch: expected '%s', got '%s'", expectedNetID, stats.NetworkID)
	}
	return nil
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/stats")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	expectedNetID := core.ActiveNetwork.NetworkID
	hdrNetID := strings.TrimSpace(resp.Header.Get("X-Network-ID"))
	if hdrNetID == "" {
		http.Error(w, "missing X-Network-ID response header", http.StatusBadGateway)
		return
	}
	if hdrNetID != expectedNetID {
		http.Error(w, fmt.Sprintf("Node network mismatch on header: expected '%s', got '%s'", expectedNetID, hdrNetID), http.StatusBadGateway)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read node /stats response: "+err.Error(), http.StatusBadGateway)
		return
	}

	var stats struct {
		NetworkID        string  `json:"network_id"`
		Height           int64   `json:"height"`
		LatestHash       string  `json:"latest_hash"`
		PeersActive      int64   `json:"peers_active"`
		MempoolSize      int64   `json:"mempool_size"`
		Mempool          int64   `json:"mempool"`
		CoinbaseMaturity int64   `json:"coinbase_maturity"`
		Synced           *bool   `json:"synced"`
		Syncing          *bool   `json:"syncing"`
		BestPeerHeight   int64   `json:"best_peer_height"`
		SyncProgress     float64 `json:"sync_progress"`
		SyncReason       string  `json:"sync_reason"`
	}
	if err := json.Unmarshal(bodyBytes, &stats); err != nil {
		http.Error(w, "Malformed /stats response from daemon", http.StatusBadGateway)
		return
	}
	if stats.NetworkID == "" {
		http.Error(w, "Node returned empty network_id", http.StatusBadGateway)
		return
	}
	if stats.NetworkID != expectedNetID {
		http.Error(w, fmt.Sprintf("Node network mismatch: expected '%s', got '%s'", expectedNetID, stats.NetworkID), http.StatusBadGateway)
		return
	}
	atomic.StoreInt64(&lastTipHeight, stats.Height)
	if stats.LatestHash != "" {
		lastTipHash.Store(stats.LatestHash)
	}
	atomic.StoreInt64(&lastPeersActive, stats.PeersActive)
	if stats.CoinbaseMaturity > 0 {
		atomic.StoreInt64(&lastNodeMaturity, stats.CoinbaseMaturity)
	}
	mp := stats.MempoolSize
	if mp == 0 && stats.Mempool > 0 {
		mp = stats.Mempool
	}
	atomic.StoreInt64(&lastMempoolSize, mp)

	if stats.Synced != nil {
		isSyncing := false
		if stats.Syncing != nil {
			isSyncing = *stats.Syncing
		}
		progress := stats.SyncProgress
		if *stats.Synced && progress == 0 {
			progress = 100.0
		}
		setCachedSyncStatus(guiNodeSyncStatus{
			Synced:         *stats.Synced,
			Syncing:        isSyncing,
			Height:         stats.Height,
			BestPeerHeight: stats.BestPeerHeight,
			SyncProgress:   progress,
			PeersActive:    stats.PeersActive,
			Reason:         stats.SyncReason,
		})
	} else {
		setCachedSyncStatus(computeLegacyV1011SyncStatus(stats.Height, stats.PeersActive))
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(bodyBytes)
}

func handlePeers(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	targetURL := rpcURL + "/peers"
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := filterSafePeersJSON(bodyBytes, core.GetActiveNetwork())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(filtered)
}

func isLoopbackRPC(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "127.0.0.1" || host == "localhost" || host == "::1" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isSafeP2PDisplayPeer(endpoint string, p2pPort string, defaultSeeds []string) bool {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false
	}
	for _, seed := range defaultSeeds {
		seedClean := strings.TrimSpace(seed)
		if strings.EqualFold(seedClean, endpoint) {
			return true
		}
		seedHost, _, _ := net.SplitHostPort(seedClean)
		if seedHost != "" && strings.EqualFold(seedHost, endpoint) {
			return true
		}
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
		port = ""
	}
	parsedIP := net.ParseIP(host)
	if parsedIP == nil {
		return true
	}
	if parsedIP.IsLoopback() {
		return true
	}
	if p2pPort != "" && port == p2pPort {
		return true
	}
	if p2pPort == "" && (port == core.TestnetP2PPort || port == core.MainnetP2PPort) {
		return true
	}
	return false
}

func filterSafePeersJSON(raw []byte, netProfile core.NetworkProfile) []byte {
	var strPeers []string
	if err := json.Unmarshal(raw, &strPeers); err == nil {
		var filtered []string
		for _, p := range strPeers {
			if isSafeP2PDisplayPeer(p, netProfile.P2PPort, netProfile.DefaultSeeds) {
				filtered = append(filtered, p)
			}
		}
		if filtered == nil {
			filtered = []string{}
		}
		out, err := json.Marshal(filtered)
		if err == nil {
			return out
		}
		return raw
	}
	var objPeers []map[string]interface{}
	if err := json.Unmarshal(raw, &objPeers); err == nil {
		var filtered []map[string]interface{}
		for _, obj := range objPeers {
			if isSeed, ok := obj["is_seed"].(bool); ok && isSeed {
				filtered = append(filtered, obj)
				continue
			}
			addr, _ := obj["display_address"].(string)
			if addr == "" {
				addr, _ = obj["endpoint"].(string)
			}
			if addr == "" {
				addr, _ = obj["addr"].(string)
			}
			if addr == "" {
				addr, _ = obj["address"].(string)
			}
			if addr == "" {
				addr, _ = obj["peer"].(string)
			}
			if isSafeP2PDisplayPeer(addr, netProfile.P2PPort, netProfile.DefaultSeeds) {
				filtered = append(filtered, obj)
			}
		}
		if filtered == nil {
			filtered = []map[string]interface{}{}
		}
		out, err := json.Marshal(filtered)
		if err == nil {
			return out
		}
		return raw
	}
	return raw
}

func handleChain(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/chain")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 2<<20))
}

func formatMempoolInfoJSON(raw []byte, netID string) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw
	}
	var txs []core.Transaction
	if err := json.Unmarshal(trimmed, &txs); err != nil {
		var wrapped struct {
			Transactions []core.Transaction `json:"transactions"`
		}
		if wErr := json.Unmarshal(trimmed, &wrapped); wErr != nil || wrapped.Transactions == nil {
			return raw
		}
		txs = wrapped.Transactions
	}

	type mempoolTxEntry struct {
		TXID          string `json:"txid"`
		Sender        string `json:"sender"`
		Recipient     string `json:"recipient"`
		AmountArmoids int64  `json:"amount_armoids"`
		Amount        string `json:"amount"`
		FeeArmoids    int64  `json:"fee_armoids"`
		Fee           string `json:"fee"`
		Nonce         int64  `json:"nonce"`
		SizeBytes     int    `json:"size_bytes"`
	}

	ticker := core.TickerForNetworkID(netID)
	totalBytes := 0
	var totalFees int64
	entries := make([]mempoolTxEntry, 0, len(txs))
	for _, tx := range txs {
		sz := tx.Size()
		totalBytes += sz
		totalFees += tx.Fee
		entries = append(entries, mempoolTxEntry{
			TXID:          tx.HashHex(),
			Sender:        tx.Sender,
			Recipient:     tx.Recipient,
			AmountArmoids: tx.Amount,
			Amount:        core.FormatArmxDecimal(tx.Amount) + " " + ticker,
			FeeArmoids:    tx.Fee,
			Fee:           core.FormatArmxDecimal(tx.Fee) + " " + ticker,
			Nonce:         tx.Nonce,
			SizeBytes:     sz,
		})
	}

	out, err := json.Marshal(map[string]interface{}{
		"network_id":           netID,
		"size":                 len(txs),
		"bytes":                totalBytes,
		"total_fees_armoids":   totalFees,
		"total_fees":           core.FormatArmxDecimal(totalFees) + " " + ticker,
		"min_relay_fee_per_kb": core.MinRelayFee,
		"transactions":         entries,
	})
	if err != nil {
		return raw
	}
	return out
}

func handleMempool(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/mempool")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if resp.StatusCode == http.StatusOK {
		bodyBytes = formatMempoolInfoJSON(bodyBytes, core.ActiveNetwork.NetworkID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(bodyBytes)
}

func handleBalance(w http.ResponseWriter, r *http.Request) {
	if err := verifyNodeNetworkErr(rpcURL, core.ActiveNetwork.NetworkID); err != nil {
		http.Error(w, fmt.Sprintf("Node network validation failed: %v", err), http.StatusBadGateway)
		return
	}
	addr := r.URL.Query().Get("address")
	if addr == "" {
		addr = activeAddress
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", rpcURL, url.QueryEscape(addr)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handleFeeEstimate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	recipient := strings.TrimSpace(r.URL.Query().Get("to"))
	var amtArmoids int64
	if aStr := strings.TrimSpace(r.URL.Query().Get("amount")); aStr != "" {
		if parsed, err := core.ParseArmxDecimal(aStr); err == nil && parsed > 0 {
			amtArmoids = parsed
		}
	}
	est := ComputeGUIFeeEstimate(
		getActiveUnlockedWalletSnapshot(),
		recipient,
		amtArmoids,
		int(atomic.LoadInt64(&lastMempoolSize)),
		core.ActiveNetwork.NetworkID,
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(est)
}

func enrichHistoryRecordsForJSON(records []PagedHistoryRecord) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(records))
	for _, rec := range records {
		txBytes, err := json.Marshal(rec.Tx)
		if err != nil {
			continue
		}
		var txMap map[string]interface{}
		if err := json.Unmarshal(txBytes, &txMap); err != nil {
			continue
		}
		if rec.TxHash != "" {
			txMap["hash"] = rec.TxHash
		} else {
			txMap["hash"] = rec.Tx.HashHex()
		}
		out = append(out, map[string]interface{}{
			"block_index": rec.BlockIndex,
			"timestamp":   rec.Timestamp,
			"status":      rec.Status,
			"tx":          txMap,
		})
	}
	return out
}

func currentKnownTipHeightAndHash() (int, string) {
	if v := lastTipHash.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return int(atomic.LoadInt64(&lastTipHeight)), s
		}
	}
	return -1, ""
}

func effectiveMaturityInt() int {
	mat := int(atomic.LoadInt64(&lastNodeMaturity))
	if mat <= 0 {
		mat = core.GetActiveNetwork().CoinbaseMaturity
	}
	if mat <= 0 {
		mat = 100
	}
	return mat
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	if err := verifyNodeNetworkErr(rpcURL, core.ActiveNetwork.NetworkID); err != nil {
		http.Error(w, fmt.Sprintf("Node network validation failed: %v", err), http.StatusBadGateway)
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		addr = activeAddress
	}

	pagedMode := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "paged")
	if pagedMode {
		targetURL := fmt.Sprintf("%s/history?address=%s&format=paged", rpcURL, url.QueryEscape(addr))
		if ch := strings.TrimSpace(r.URL.Query().Get("cursor_height")); ch != "" {
			targetURL += "&cursor_height=" + url.QueryEscape(ch)
		}
		if bb := strings.TrimSpace(r.URL.Query().Get("batch_blocks")); bb != "" {
			targetURL += "&batch_blocks=" + url.QueryEscape(bb)
		}
		if lm := strings.TrimSpace(r.URL.Query().Get("limit")); lm != "" {
			targetURL += "&limit=" + url.QueryEscape(lm)
		}
		if mh := strings.TrimSpace(r.URL.Query().Get("min_height")); mh != "" {
			targetURL += "&min_height=" + url.QueryEscape(mh)
		}
		client := http.Client{Timeout: 6 * time.Second}
		resp, err := client.Get(targetURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}
		var env PagedHistoryResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&env); err != nil {
			http.Error(w, "Invalid paged history response from node", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records":            enrichHistoryRecordsForJSON(env.Records),
			"chain_height":       env.ChainHeight,
			"tip_hash":           env.TipHash,
			"scanned_from":       env.ScannedFrom,
			"scanned_to":         env.ScannedTo,
			"next_cursor_height": env.NextCursorHeight,
			"has_more":           env.HasMore,
		})
		return
	}

	// Default & envelope=1 / full=1: serve through persistent bbolt store with incremental delta sync
	forceFull := r.URL.Query().Get("force") == "1"
	tipH, tipHash := currentKnownTipHeightAndHash()
	client := &http.Client{Timeout: 8 * time.Second}

	var records []PagedHistoryRecord
	var meta HistorySyncMeta
	var syncErr error

	if store := ensureHistoryStore(); store != nil {
		records, meta, syncErr = store.SyncAddress(r.Context(), client, rpcURL, core.ActiveNetwork.NetworkID, addr, tipH, tipHash, forceFull)
	} else {
		var ch int
		var th string
		records, ch, th, syncErr = FetchHistoryRange(r.Context(), client, rpcURL, addr, core.ActiveNetwork.NetworkID, 0, 2000, nil)
		meta = HistorySyncMeta{SyncedHeight: ch, SyncedTipHash: th}
	}

	if syncErr != nil && len(records) == 0 {
		http.Error(w, syncErr.Error(), http.StatusBadGateway)
		return
	}

	out := enrichHistoryRecordsForJSON(records)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("envelope") == "1" {
		chainH := meta.SyncedHeight
		if tipH > chainH {
			chainH = tipH
		}
		respObj := map[string]interface{}{
			"records":         out,
			"synced_height":   meta.SyncedHeight,
			"synced_tip_hash": meta.SyncedTipHash,
			"cache_hit":       meta.CacheHit,
			"delta_blocks":    meta.DeltaBlocks,
			"reorg_reset":     meta.ReorgReset,
			"chain_height":    chainH,
			"maturity":        effectiveMaturityInt(),
			"total_records":   len(out),
		}
		if syncErr != nil {
			respObj["warning"] = syncErr.Error()
		}
		_ = json.NewEncoder(w).Encode(respObj)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		To         string `json:"to"`
		Amount     string `json:"amount"`
		Fee        string `json:"fee"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	to := strings.TrimSpace(req.To)
	if !core.ValidateAddress(to) {
		http.Error(w, "Invalid recipient Bech32 address", http.StatusBadRequest)
		return
	}

	amtArmoids, err := core.ParseArmxDecimal(req.Amount)
	if err != nil || amtArmoids <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	var feeArmoids int64
	explicitFee := strings.TrimSpace(req.Fee) != ""
	if explicitFee {
		f, err := core.ParseArmxDecimal(req.Fee)
		if err != nil {
			http.Error(w, "Invalid fee", http.StatusBadRequest)
			return
		}
		feeArmoids = f
	}
	if amtArmoids > core.MaxSupply-feeArmoids {
		http.Error(w, "Amount plus fee exceeds maximum supply", http.StatusBadRequest)
		return
	}

	if err := verifyNodeNetworkErr(rpcURL, core.ActiveNetwork.NetworkID); err != nil {
		http.Error(w, fmt.Sprintf("Node network validation failed: %v", err), http.StatusBadGateway)
		return
	}

	var signingWallet *wallet.Wallet
	if req.Passphrase != "" {
		var loadedW *wallet.Wallet
		var err error
		if activeWalletID != "" {
			loadedW, err = ensureWalletManager().DecryptWallet(activeWalletID, req.Passphrase)
		} else {
			loadedW, err = wallet.DecryptAndLoadKeystore(req.Passphrase, findWalletPath())
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Keystore passphrase decryption error: %v", err), http.StatusUnauthorized)
			return
		}
		defer loadedW.Zeroize()
		signingWallet = loadedW
		touchWalletActivity()
	} else {
		unlockedMu.RLock()
		var cached *wallet.Wallet
		if activeWalletID != "" {
			cached = unlockedWallets[activeWalletID]
		}
		if cached == nil && activeWallet != nil && (activeAddress == "" || core.MatchAddress(activeWallet.Address, activeAddress)) {
			cached = activeWallet
		}
		unlockedMu.RUnlock()
		if cached != nil && cached.PrivateKey != nil {
			signingWallet = cached
			touchWalletActivity()
		} else {
			http.Error(w, "Wallet passphrase is required to sign transactions", http.StatusUnauthorized)
			return
		}
	}

	// Compute real minimum required fee from ML-DSA-65 tx size & core.MinRelayFee (1,000 Armoids/KB)
	minFee := wallet.EstimateMinMempoolFee(signingWallet, to, amtArmoids, 0)
	if explicitFee && feeArmoids < minFee {
		http.Error(w, fmt.Sprintf(
			"Transaction fee %s is below minimum required network relay fee %s (core.MinRelayFee = %d Armoids/KB)",
			core.FormatArmxDecimal(feeArmoids), core.FormatArmxDecimal(minFee), core.MinRelayFee),
			http.StatusBadRequest)
		return
	}
	if !explicitFee || feeArmoids == 0 {
		feeArmoids = minFee
	}
	if amtArmoids > core.MaxSupply-feeArmoids {
		http.Error(w, "Amount plus fee exceeds maximum supply", http.StatusBadRequest)
		return
	}

	client := http.Client{Timeout: 5 * time.Second}
	bResp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", rpcURL, signingWallet.Address))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to query nonce: %v", err), http.StatusBadGateway)
		return
	}
	defer bResp.Body.Close()
	if bResp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Failed to query balance (HTTP %d)", bResp.StatusCode), http.StatusBadGateway)
		return
	}
	if netHdr := strings.TrimSpace(bResp.Header.Get("X-Network-ID")); netHdr != "" && !strings.EqualFold(netHdr, core.ActiveNetwork.NetworkID) {
		http.Error(w, fmt.Sprintf("Network mismatch on /balance: daemon reported %s, expected %s", netHdr, core.ActiveNetwork.NetworkID), http.StatusBadGateway)
		return
	}
	var bal struct {
		Balance          int64 `json:"balance"`
		ConfirmedBalance int64 `json:"confirmed_balance"`
		PendingBalance   int64 `json:"pending_balance"`
		SpendableBalance int64 `json:"spendable_balance"`
		PendingNonce     int64 `json:"pending_nonce"`
		NextNonce        int64 `json:"next_nonce"`
	}
	if err := json.NewDecoder(bResp.Body).Decode(&bal); err != nil {
		http.Error(w, fmt.Sprintf("Invalid balance response: %v", err), http.StatusBadGateway)
		return
	}
	avail := bal.SpendableBalance
	if avail == 0 {
		avail = bal.PendingBalance
	}
	if avail == 0 {
		avail = bal.ConfirmedBalance
	}
	if avail == 0 {
		avail = bal.Balance
	}
	if avail < amtArmoids+feeArmoids {
		http.Error(w, "Insufficient balance", http.StatusBadRequest)
		return
	}

	txNonce := bal.NextNonce
	if txNonce <= 0 {
		txNonce = bal.PendingNonce + 1
	}

	tx := core.Transaction{
		Sender:    signingWallet.Address,
		SenderPub: signingWallet.PublicKey,
		Recipient: to,
		Amount:    amtArmoids,
		Fee:       feeArmoids,
		Nonce:     txNonce,
	}

	if err := tx.Sign(signingWallet.PrivateKey); err != nil {
		http.Error(w, fmt.Sprintf("Signature creation failed: %v", err), http.StatusInternalServerError)
		return
	}

	txData, _ := json.Marshal(tx)
	postResp, err := client.Post(rpcURL+"/tx/new", "application/json", bytes.NewBuffer(txData))
	if err != nil {
		http.Error(w, fmt.Sprintf("Broadcast failed: %v", err), http.StatusBadGateway)
		return
	}
	defer postResp.Body.Close()
	bodyBytes, _ := io.ReadAll(postResp.Body)

	if postResp.StatusCode != 200 {
		http.Error(w, string(bodyBytes), postResp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"txid":     tx.HashHex(),
		"nonce":    txNonce,
		"fee":      feeArmoids,
		"response": string(bodyBytes),
	})
}

func armguiVersionString() string {
	return fmt.Sprintf("armgui v%s", core.Version)
}

func writeMineErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": msg,
	})
}

func guiMinerStatusLabel(active bool) string {
	if !active {
		return "Idle"
	}
	if miningWaitingSync.Load() {
		return "Waiting for Sync"
	}
	return "CPU Mining"
}

func readLocalRPCCookie() (string, string) {
	data, err := os.ReadFile(node.GetRPCAuthCookiePath())
	if err != nil {
		return "", ""
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func startCPUMiningWorker(minerAddr string, threads int, delay int, mineCtx context.Context, stopCh <-chan struct{}, prevDone <-chan struct{}, doneCh chan struct{}, gen uint64) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		wg.Wait()
		close(doneCh)
		miningLock.Lock()
		if miningGen == gen {
			miningDone = nil
			miningCancel = nil
		}
		miningLock.Unlock()
	}()

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		tr := &http.Transport{}
		defer tr.CloseIdleConnections()
		client := http.Client{Transport: tr, Timeout: 800 * time.Millisecond}
		for {
			select {
			case <-mineCtx.Done():
				setMiningHps(0)
				return
			case <-stopCh:
				setMiningHps(0)
				return
			case <-ticker.C:
				if miningWaitingSync.Load() {
					setMiningHps(0)
					continue
				}
				var liveHps float64
				statsCtx, statsCancel := context.WithTimeout(mineCtx, 800*time.Millisecond)
				req, reqErr := http.NewRequestWithContext(statsCtx, http.MethodGet, rpcURL+"/stats", nil)
				if reqErr == nil {
					resp, err := client.Do(req)
					if err == nil {
						if resp.StatusCode == http.StatusOK {
							var stats struct {
								MiningHashrate float64 `json:"mining_hashrate"`
							}
							if json.NewDecoder(resp.Body).Decode(&stats) == nil {
								liveHps = stats.MiningHashrate
							}
						}
						resp.Body.Close()
					}
				}
				statsCancel()
				if mineCtx.Err() != nil {
					setMiningHps(0)
					return
				}
				setMiningHps(liveHps)
			}
		}
	}()

	authUser, authPass := readLocalRPCCookie()

	go func() {
		defer wg.Done()
		if prevDone != nil {
			select {
			case <-prevDone:
			case <-mineCtx.Done():
				return
			}
		}

		tr := &http.Transport{}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr}
		mineURL := fmt.Sprintf("%s/mine", rpcURL)

		for {
			select {
			case <-mineCtx.Done():
				return
			case <-stopCh:
				return
			default:
			}

			// Wait until blockchain synchronization completes before issuing /mine
			syncSt := queryNodeSyncStatus(mineCtx)
			var lastSyncLog time.Time
			wasWaiting := miningWaitingSync.Load()
			for !syncSt.Synced {
				wasWaiting = true
				miningWaitingSync.Store(true)
				setMiningHps(0)
				if lastSyncLog.IsZero() || time.Since(lastSyncLog) >= 15*time.Second {
					msg := syncSt.Reason
					if msg == "" {
						msg = fmt.Sprintf("Synchronizing blockchain (#%d / #%d, %.1f%%)", syncSt.Height, syncSt.BestPeerHeight, syncSt.SyncProgress)
					}
					appendMiningLog(fmt.Sprintf("[*] Waiting for Sync: %s", msg))
					lastSyncLog = time.Now()
				}
				select {
				case <-mineCtx.Done():
					return
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
				}
				syncSt = queryNodeSyncStatus(mineCtx)
			}
			if miningWaitingSync.Swap(false) || wasWaiting {
				appendMiningLog(fmt.Sprintf("[*] Blockchain synced (#%d). Starting CPU mining!", syncSt.Height))
			}

			if authUser == "" || authPass == "" {
				authUser, authPass = readLocalRPCCookie()
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"miner":   minerAddr,
				"threads": threads,
			})
			reqCtx, cancel := context.WithTimeout(mineCtx, 15*time.Minute)
			req, err := http.NewRequestWithContext(reqCtx, "POST", mineURL, bytes.NewBuffer(payload))
			if err != nil {
				cancel()
				if mineCtx.Err() != nil {
					return
				}
				select {
				case <-mineCtx.Done():
					return
				case <-stopCh:
					return
				case <-time.After(1 * time.Second):
					continue
				}
			}
			req.Header.Set("Content-Type", "application/json")
			if authUser != "" && authPass != "" {
				req.SetBasicAuth(authUser, authPass)
			}

			resp, err := client.Do(req)
			cancel()
			if err != nil {
				if mineCtx.Err() != nil {
					return
				}
				select {
				case <-mineCtx.Done():
					return
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			var mResult struct {
				Status      string      `json:"status"`
				Message     string      `json:"message"`
				Block       *core.Block `json:"block"`
				PeersActive *int        `json:"peers_active"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&mResult)
			resp.Body.Close()

			if resp.StatusCode == http.StatusServiceUnavailable {
				if !miningWaitingSync.Swap(true) {
					msg := mResult.Message
					if msg == "" {
						msg = "Node is still synchronizing blocks"
					}
					appendMiningLog(fmt.Sprintf("[*] Waiting for Sync: %s", msg))
				}
				setMiningHps(0)
				select {
				case <-mineCtx.Done():
					return
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			if resp.StatusCode == 200 && mResult.Status == "success" {
				var rewardAmt int64 = 60 * core.CoinUnit
				blkIdx := 0
				blkHash := ""
				if mResult.Block != nil {
					blkIdx = mResult.Block.Index
					blkHash = mResult.Block.Hash
					if len(mResult.Block.Transactions) > 0 {
						rewardAmt = mResult.Block.Transactions[0].Amount
					}
				}
				peersAtMine := int(atomic.LoadInt64(&lastPeersActive))
				if mResult.PeersActive != nil {
					peersAtMine = *mResult.PeersActive
					atomic.StoreInt64(&lastPeersActive, int64(peersAtMine))
				}
				miningTracker.RecordFound(blkIdx, blkHash, rewardAmt, peersAtMine, effectiveMaturityInt())
				hashPrefix := ""
				if len(blkHash) > 16 {
					hashPrefix = " | hash: " + blkHash[:16] + "..."
				}
				appendMiningLog(fmt.Sprintf("[RELAYED] Block found #%d%s | Est. Reward: %s | Peers: %d (awaiting network confirmation)",
					blkIdx, hashPrefix, core.FormatArmxDecimal(rewardAmt), peersAtMine))
			} else if mineCtx.Err() == nil && mResult.Message != "" {
				appendMiningLog(fmt.Sprintf("[-] Mining notice (HTTP %d): %s", resp.StatusCode, mResult.Message))
			}

			if mineCtx.Err() != nil {
				return
			}

			reconcileUnixMinedBlocks()
			select {
			case <-mineCtx.Done():
				return
			case <-stopCh:
				return
			case <-time.After(time.Duration(delay) * time.Millisecond):
			}
		}
	}()
}

func stopMiningInternal() bool {
	miningLock.Lock()
	if !miningActive {
		miningLock.Unlock()
		return false
	}
	miningActive = false
	miningWaitingSync.Store(false)
	cancel := miningCancel
	stopCh := miningStopChan
	doneCh := miningDone
	miningCancel = nil
	miningStopChan = nil
	miningLock.Unlock()

	if cancel != nil {
		cancel()
	}
	if stopCh != nil {
		close(stopCh)
	}

	embeddedNodeMu.Lock()
	embHandle := embeddedNodeHandle
	embeddedNodeMu.Unlock()
	var stopReqDone chan struct{}
	var stopReqCancel context.CancelFunc
	if embHandle != nil {
		embHandle.CancelActiveMining()
	} else {
		stopURL := strings.TrimRight(rpcURL, "/") + "/mine/stop"
		u, p := readLocalRPCCookie()
		stopReqDone = make(chan struct{})
		var stopReqCtx context.Context
		stopReqCtx, stopReqCancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
		go func() {
			defer close(stopReqDone)
			defer stopReqCancel()
			req, err := http.NewRequestWithContext(stopReqCtx, http.MethodPost, stopURL, nil)
			if err != nil {
				return
			}
			if u != "" && p != "" {
				req.SetBasicAuth(u, p)
			}
			tr := &http.Transport{}
			defer tr.CloseIdleConnections()
			c := &http.Client{Transport: tr, Timeout: 250 * time.Millisecond}
			if resp, err := c.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
		}
	}
	if stopReqDone != nil {
		select {
		case <-stopReqDone:
		case <-time.After(250 * time.Millisecond):
		}
	}
	if stopReqCancel != nil {
		stopReqCancel()
	}

	setMiningHps(0)
	appendMiningLog("[*] CPU Mining stopped by user.")
	return true
}

func handleMineStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Miner   string `json:"miner"`
		Threads int    `json:"threads"`
		Delay   int    `json:"delay"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	minerAddr := strings.TrimSpace(req.Miner)
	if minerAddr == "" {
		minerAddr = activeAddress
	}
	if !core.ValidateAddress(minerAddr) {
		writeMineErrorJSON(w, http.StatusBadRequest, "Invalid miner address")
		return
	}

	threads := req.Threads
	if threads < 1 {
		threads = optimalCPUThreads()
	}
	delay := req.Delay
	if delay < 100 {
		delay = 1000
	}

	syncProbeCtx, syncProbeCancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	syncSt := queryNodeSyncStatus(syncProbeCtx)
	syncProbeCancel()

	miningLock.Lock()
	if miningActive {
		miningLock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}
	prevDone := miningDone
	miningActive = true
	waitingSync := !syncSt.Synced
	miningWaitingSync.Store(waitingSync)
	miningGen++
	gen := miningGen
	stopCh := make(chan struct{})
	mineCtx, mineCancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	miningStopChan = stopCh
	miningCancel = mineCancel
	miningDone = doneCh
	miningStartTime = time.Now()
	setMiningHps(0)
	miningLock.Unlock()

	if waitingSync {
		appendMiningLog(fmt.Sprintf("[*] Mining queued (Waiting for Sync) | Threads: %d, Delay: %dms, Miner: %s", threads, delay, minerAddr))
	} else {
		appendMiningLog(fmt.Sprintf("[*] CPU Mining started! Threads: %d, Delay: %dms, Miner: %s", threads, delay, minerAddr))
	}
	startCPUMiningWorker(minerAddr, threads, delay, mineCtx, stopCh, prevDone, doneCh, gen)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "started",
		"waiting_for_sync": waitingSync,
	})
}

func reconcileUnixMinedBlocks() {
	nowUnix := time.Now().Unix()
	lastRec := atomic.LoadInt64(&lastReconcileUnix)
	if nowUnix-lastRec < 4 {
		return
	}
	if !atomic.CompareAndSwapInt64(&lastReconcileUnix, lastRec, nowUnix) {
		return
	}
	tipH := int(atomic.LoadInt64(&lastTipHeight))
	client := &http.Client{Timeout: 2 * time.Second}
	orphans, _ := miningTracker.Reconcile(context.Background(), client, rpcURL, tipH, effectiveMaturityInt())
	for _, orph := range orphans {
		shortHash := orph.Hash
		if len(shortHash) > 16 {
			shortHash = shortHash[:16] + "..."
		}
		appendMiningLog(fmt.Sprintf("[!] ORPHANED: Block #%d (%s) was replaced on the main chain -- session rewards adjusted.", orph.Height, shortHash))
	}
}

func handleMineStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !stopMiningInternal() {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_running"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func handleMineStatus(w http.ResponseWriter, r *http.Request) {
	reconcileUnixMinedBlocks()

	miningLock.Lock()
	active := miningActive
	st := miningStartTime
	miningLock.Unlock()

	waitingSync := active && miningWaitingSync.Load()
	syncSt := getCachedSyncStatus()

	elapsedSecs := 0
	var hps float64
	if active && !waitingSync {
		elapsedSecs = int(time.Since(st).Seconds())
		hps = getMiningHps()
	}

	validBlks, validRwd, orphanBlks, entries := miningTracker.Summary()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active":           active,
		"waiting_for_sync": waitingSync,
		"status_label":     guiMinerStatusLabel(active),
		"hashrate":         hps,
		"elapsed_secs":     elapsedSecs,
		"session_blocks":   validBlks,
		"session_reward":   validRwd,
		"orphaned_blocks":  orphanBlks,
		"mined_blocks":     entries,
		"peers_active":     atomic.LoadInt64(&lastPeersActive),
		"node_synced":      syncSt.Synced,
		"node_syncing":     syncSt.Syncing,
		"local_height":     syncSt.Height,
		"best_peer_height": syncSt.BestPeerHeight,
		"sync_progress":    syncSt.SyncProgress,
		"sync_reason":      syncSt.Reason,
		"optimal_threads":  optimalCPUThreads(),
		"logs":             getMiningLogs(),
		"cpu_cores":        runtime.NumCPU(),
	})
}

func handleAddressBook(w http.ResponseWriter, r *http.Request) {
	bookFile := findAddressBookPath()
	type ContactEntry struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		DateAdded string `json:"date_added"`
	}

	if r.Method == http.MethodGet {
		var contacts []ContactEntry
		data, err := os.ReadFile(bookFile)
		if err == nil {
			_ = json.Unmarshal(data, &contacts)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contacts)
		return
	}

	if r.Method == http.MethodPost {
		var contacts []ContactEntry
		if err := json.NewDecoder(r.Body).Decode(&contacts); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		data, _ := json.MarshalIndent(contacts, "", "  ")
		_ = os.MkdirAll(filepath.Dir(bookFile), 0700)
		_ = os.WriteFile(bookFile, data, 0600)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	if err := verifyNodeNetworkErr(rpcURL, core.ActiveNetwork.NetworkID); err != nil {
		http.Error(w, fmt.Sprintf("Node network validation failed: %v", err), http.StatusBadGateway)
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		addr = activeAddress
	}
	client := &http.Client{Timeout: 8 * time.Second}
	tipH, tipHash := currentKnownTipHeightAndHash()

	var records []PagedHistoryRecord
	var err error
	if store := ensureHistoryStore(); store != nil {
		records, _, err = store.SyncAddress(r.Context(), client, rpcURL, core.ActiveNetwork.NetworkID, addr, tipH, tipHash, false)
	} else {
		records, _, _, err = FetchHistoryRange(r.Context(), client, rpcURL, addr, core.ActiveNetwork.NetworkID, 0, 2000, nil)
	}
	if err != nil && len(records) == 0 {
		http.Error(w, fmt.Sprintf("Failed to query full history: %v", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment;filename=armax_transactions.csv")

	ticker := core.TickerForNetworkID(core.ActiveNetwork.NetworkID)
	_, _ = w.Write([]byte(fmt.Sprintf("Index,Type,Amount_%s,Height,TXID,Timestamp\n", ticker)))
	for i, rec := range records {
		txType := strings.ReplaceAll(ClassifyTxType(rec.Tx, addr), ",", " ")
		hStr := "Pending"
		if rec.BlockIndex >= 0 {
			hStr = fmt.Sprintf("%d", rec.BlockIndex)
		}
		tStr := time.Unix(rec.Timestamp, 0).Format("2006-01-02 15:04:05")
		txHash := rec.TxHash
		if txHash == "" {
			txHash = rec.Tx.HashHex()
		}
		line := fmt.Sprintf("%d,%s,%s,%s,%s,%s\n",
			i+1, txType, core.FormatArmxDecimal(rec.Tx.Amount), hStr, txHash, tStr)
		_, _ = w.Write([]byte(line))
	}
}

func handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	token := initSessionToken()
	http.SetCookie(w, &http.Cookie{
		Name:     "armax_gui_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ticker := core.TickerForNetworkID(core.ActiveNetwork.NetworkID)
	html := strings.ReplaceAll(webUIHTML, "{{TICKER}}", ticker)
	html = strings.ReplaceAll(html, "{{SESSION_TOKEN}}", token)
	_, _ = w.Write([]byte(html))
}
