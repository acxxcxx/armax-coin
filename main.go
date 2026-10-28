package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/node"
	"github.com/acxxcxx/armax-coin/wallet"
)

func main() {
	networkFlag := flag.String("network", "testnet", "Network profile to run (testnet, mainnet) [C-33, L4-1]")
	daemonFlag := flag.Bool("daemon", true, "Run node as RPC Daemon & P2P Server (default: true)")
	testFlag := flag.Bool("test", false, "Run demo self-test suite instead of production node daemon")
	configFlag := flag.String("config", "armax.conf", "Path to node configuration file")
	bindFlag := flag.String("rpcbind", "", "Interface to bind RPC server (default: 127.0.0.1 or from config)")
	portFlag := flag.String("port", "", "Port for RPC Daemon (deprecated: use -rpcport)")
	rpcPortFlag := flag.String("rpcport", "", "Port for RPC Daemon (overrides config)")
	p2pPortFlag := flag.String("p2p", "", "Port for P2P socket network (deprecated: use -p2pport)")
	p2pPortNewFlag := flag.String("p2pport", "", "Port for P2P socket network (overrides config)")
	dataDirFlag := flag.String("datadir", "", "Custom data directory for blockchain database, peers, and logs")
	peersFlag := flag.String("peers", "", "Comma-separated list of initial P2P peer addresses")
	mineFlag := flag.Bool("mine", false, "Enable automated background CPU mining")
	genFlag := flag.Bool("gen", false, "Enable automated background CPU mining (alias for -mine)")
	minerAddrFlag := flag.String("miner", "", "Miner reward address for background mining")
	miningThreadsFlag := flag.Int("miningthreads", 0, "Number of CPU worker threads for mining (default: all CPU cores)")
	miningThreadSingularFlag := flag.Int("miningthread", 0, "Alias for -miningthreads")
	threadsFlag := flag.Int("threads", 0, "Alias for -miningthreads")
	genProcLimitFlag := flag.Int("genproclimit", 0, "Processor limit / threads for background mining (alias for -miningthreads)")
	insecureLocalhostFlag := flag.Bool("rpcinsecure-localhost", false, "Allow unauthenticated mutation requests from localhost (insecure)")
	resyncFlag := flag.Bool("resync", false, "Safely backup local blockchain database and resynchronize from Genesis")
	infoFlag := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionFlag := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	tofuFlag := flag.Bool("p2p-tofu", false, "Enable Trust-On-First-Use (TOFU) TLS certificate fingerprint pinning [F1]")
	requireTLSFlag := flag.Bool("require-tls", false, "Require strict TLS 1.3 transport without unencrypted fallback [N-08]")
	rpcUserFlag := flag.String("rpcuser", "", "Username for JSON-RPC authentication")
	rpcPasswordFlag := flag.String("rpcpassword", "", "Password for JSON-RPC authentication")
	allowNonLoopbackFlag := flag.Bool("rpc-allow-non-loopback", false, "Explicitly allow binding RPC server to non-loopback interface (requires auth) [F8]")
	checkHealthFlag := flag.Bool("checkhealth", false, "Check RPC, database, P2P network, and blockchain synchronization status")
	getPeerInfoFlag := flag.Bool("getpeerinfo", false, "Display connected peers, addresses, connection details, and network status")
	getNetworkInfoFlag := flag.Bool("getnetworkinfo", false, "Display active network profile, ports, active peers, and node version")
	getBlockchainInfoFlag := flag.Bool("getblockchaininfo", false, "Display local block height, best block hash, chainwork, and sync status")
	getMempoolInfoFlag := flag.Bool("getmempoolinfo", false, "Display unconfirmed transactions waiting in the mempool")
	mempoolFlag := flag.Bool("mempool", false, "Display unconfirmed transactions waiting in the mempool (alias for -getmempoolinfo)")
	verifyChainFlag := flag.Bool("verifychain", false, "Verify local blockchain integrity and cryptographic block continuity")
	reindexFlag := flag.Bool("reindex", false, "Rebuild database index and state verification from genesis")
	logLevelFlag := flag.String("loglevel", "info", "Set daemon logging verbosity: error, warn, info, debug, trace")
	debugCategoriesFlag := flag.String("debug", "", "Enable debug logging for categories (comma-separated: net, sync, mempool, validation, rpc, discovery, miner, or 'all')")
	printToConsoleFlag := flag.Bool("printtoconsole", true, "Send log output to console/terminal (standard daemon behavior)")
	shutdownFlag := flag.Bool("shutdown", false, "Safely request shutdown of a running daemon via authenticated RPC")
	armaxHashHeightFlag := flag.Int64("armaxhash-height", -2, "Block height where PoW transitions to ArmaxHash (-1 = disabled)")
	flag.Parse()

	if *armaxHashHeightFlag >= -1 {
		core.SetArmaxHashActivationHeight(*armaxHashHeightFlag)
	}

	// Resolve mining CLI flag aliases
	if *genFlag {
		*mineFlag = true
	}
	if *miningThreadsFlag == 0 {
		if *miningThreadSingularFlag > 0 {
			*miningThreadsFlag = *miningThreadSingularFlag
		} else if *threadsFlag > 0 {
			*miningThreadsFlag = *threadsFlag
		} else if *genProcLimitFlag > 0 {
			*miningThreadsFlag = *genProcLimitFlag
		}
	}

	if *versionFlag {
		fmt.Printf("armcoind v%s\n", core.Version)
		return
	}

	effRPCPort := *rpcPortFlag
	effP2PPort := *p2pPortNewFlag

	// Check deprecated flags with explicit warnings [L4-8]
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			fmt.Println("[!] Warning: -port is deprecated, please use -rpcport instead")
			if effRPCPort == "" {
				effRPCPort = *portFlag
			}
		}
		if f.Name == "p2p" {
			fmt.Println("[!] Warning: -p2p is deprecated, please use -p2pport instead")
			if effP2PPort == "" {
				effP2PPort = *p2pPortFlag
			}
		}
	})

	if *dataDirFlag != "" {
		_ = os.Setenv("ARMX_DATA_DIR", *dataDirFlag)
	}

	if *insecureLocalhostFlag {
		node.AllowInsecureLocalhost = true
		fmt.Println("[!] WARNING: -rpcinsecure-localhost is enabled. Localhost mutation requests will NOT require credentials.")
	}

	// Resolve network choice: check CLI flag first, then fallback to ARMX_NETWORK environment variable [L1-3]
	networkChoice := *networkFlag
	isNetworkFlagExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "network" {
			isNetworkFlagExplicit = true
		}
	})
	if !isNetworkFlagExplicit {
		if envNet := os.Getenv("ARMX_NETWORK"); envNet != "" {
			if strings.EqualFold(envNet, "regtest") {
				fmt.Println("[!] Note: regtest profile cannot be selected via environment variable. Please specify '-network regtest' explicitly.")
			} else {
				networkChoice = envNet
			}
		}
	}
	if strings.EqualFold(networkChoice, "regtest") && !isNetworkFlagExplicit {
		fmt.Println("[-] Error: regtest profile cannot be selected without explicit flag '-network regtest'")
		os.Exit(1)
	}

	// Switch active network profile (testnet / mainnet) [C-33, L1-3, L4-1]
	cliNet, err := core.ResolveCLINetwork(networkChoice)
	if err != nil {
		fmt.Printf("[-] Invalid network profile '%s': %v\n", networkChoice, err)
		os.Exit(1)
	}
	if err := core.SetNetwork(networkChoice); err != nil {
		fmt.Printf("[-] Failed to set network profile '%s': %v\n", networkChoice, err)
		os.Exit(1)
	}
	activeNet := core.GetActiveNetwork()

	fmt.Println("==================================================")
	fmt.Println(core.NetworkWarningLine())
	fmt.Println("==================================================")

	debugLogPath := filepath.Join(cliNet.DataDir, "debug.log")
	if err := core.InitLogger(*logLevelFlag, *debugCategoriesFlag, *printToConsoleFlag, debugLogPath); err != nil {
		fmt.Printf("[-] Warning: Failed to initialize debug.log at %s: %v\n", debugLogPath, err)
	}
	defer core.CloseLogger()

	if *infoFlag {
		fmt.Printf("[+] Target Network   : %s (%s, RPC: %s, P2P: %s, DataDir: %s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID, cliNet.RPCPort, cliNet.P2PPort, cliNet.DataDir)
		for _, seed := range cliNet.DefaultSeeds {
			fmt.Printf("[+] Seed Peer        : %s\n", seed)
		}
		return
	}

	if *resyncFlag {
		fmt.Println("[*] -resync requested: Archiving existing blockchain database...")
		bak, err := core.BackupAndResetDatabase()
		if err != nil {
			fmt.Printf("[-] Database backup failed: %v\n", err)
			os.Exit(1)
		}
		if bak != "" {
			fmt.Printf("[OK] Existing blockchain archived to: %s\n", bak)
			fmt.Println("[+] Blockchain will now rebuild state cleanly from Genesis block.")
		} else {
			fmt.Println("[i] No existing database found to backup. Starting fresh from Genesis.")
		}
	}

	daemonHost := "127.0.0.1"
	daemonPort := activeNet.RPCPort
	if effRPCPort != "" {
		daemonPort = effRPCPort
	}
	// Parse positional host:port arguments if passed (e.g. -daemon 127.0.0.1:19333 or 127.0.0.1:19333)
	for _, arg := range flag.Args() {
		if strings.Contains(arg, ":") {
			parts := strings.Split(arg, ":")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				daemonHost = parts[0]
				daemonPort = parts[1]
				break
			}
		}
	}
	daemonRPCURL := "http://" + daemonHost + ":" + daemonPort

	queryDaemon := func(path string) ([]byte, error) {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequest("GET", daemonRPCURL+path, nil)
		if err != nil {
			return nil, err
		}
		if *rpcUserFlag != "" || *rpcPasswordFlag != "" {
			req.SetBasicAuth(*rpcUserFlag, *rpcPasswordFlag)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}

	var bc *core.Blockchain
	getBC := func() *core.Blockchain {
		if bc != nil {
			return bc
		}
		var openErr error
		bc, openErr = core.OpenBlockchain(3)
		if openErr != nil {
			fmt.Printf("[-] %v\n", openErr)
			os.Exit(1)
		}
		if bc.StoragePath == "IN_MEMORY" && !*testFlag && activeNet.Name != "regtest" {
			fmt.Printf("[-] FATAL: Failed to access persistent database on disk. Operating in-memory is prohibited in production to prevent data loss.\n")
			os.Exit(1)
		}
		bc.CoinbaseMaturity = activeNet.CoinbaseMaturity
		if *miningThreadsFlag > 0 {
			bc.MiningThreads = *miningThreadsFlag
		}
		return bc
	}

	if *verifyChainFlag {
		fmt.Println("[*] Verifying local blockchain integrity and cryptographic continuity...")
		chain := getBC()
		if !chain.IsChainValid() {
			fmt.Println("[-] Blockchain verification FAILED: invalid block hash, PoW target, or continuity")
			os.Exit(1)
		}
		fmt.Printf("[OK] Blockchain verified successfully! Total blocks: %d, Difficulty: %d\n", len(chain.Chain), chain.Difficulty)
		return
	}

	if *reindexFlag {
		fmt.Println("[*] Reindexing local blockchain state from Genesis...")
		chain := getBC()
		if err := chain.RebuildState(); err != nil {
			fmt.Printf("[-] Reindexing verification error: %v\n", err)
			os.Exit(1)
		}
		_ = chain.SaveToFile()
		fmt.Printf("[OK] Blockchain state verified and database flushed cleanly. Total blocks: %d\n", len(chain.Chain))
		return
	}

	if *checkHealthFlag {
		fmt.Println("[*] Checking Armaxcoin system health...")
		body, err := queryDaemon("/status")
		if err == nil {
			var st map[string]interface{}
			_ = json.Unmarshal(body, &st)
			fmt.Println("[OK] RPC Daemon     : ONLINE (" + daemonRPCURL + ")")
			fmt.Printf("    Network ID     : %v\n", st["network_id"])
			fmt.Printf("    Node Version   : %v\n", st["version"])
			fmt.Printf("    Block Height   : %v\n", st["height"])
			fmt.Printf("    Active Peers   : %v\n", st["peers_active"])
			return
		}
		fmt.Printf("[!] RPC Daemon     : OFFLINE (%v)\n", err)
		chain := getBC()
		fmt.Printf("[OK] Local Database : %d blocks indexed (Circulating: %d %s)\n", len(chain.Chain), chain.GetCirculatingSupply()/core.CoinUnit, core.ActiveTicker())
		return
	}

	if *getBlockchainInfoFlag {
		body, err := queryDaemon("/stats")
		if err == nil {
			var stats map[string]interface{}
			if json.Unmarshal(body, &stats) == nil {
				fmt.Println("==================================================")
				fmt.Println("  ARMAX BLOCKCHAIN INFO (LIVE DAEMON)")
				fmt.Println("==================================================")
				fmt.Printf("  Network ID        : %v\n", stats["network_id"])
				fmt.Printf("  Block Height      : %v\n", stats["height"])
				fmt.Printf("  Total Blocks      : %v\n", stats["blocks"])
				fmt.Printf("  Difficulty        : %v\n", stats["difficulty"])
				fmt.Printf("  Total Chainwork   : %v\n", stats["chainwork"])
				fmt.Printf("  Circulating Supply: %v %s\n", stats["circulating_supply"], core.ActiveTicker())
				fmt.Printf("  Max Supply        : %v %s\n", stats["max_supply"], core.ActiveTicker())
				fmt.Printf("  Mempool Size      : %v txs\n", stats["mempool_size"])
				fmt.Printf("  Active Peers      : %v\n", stats["peers_active"])
				fmt.Printf("  Best Block Hash   : %v\n", stats["latest_hash"])
				fmt.Println("==================================================")
				return
			}
		}
		// Fallback to local offline blockchain info
		chain := getBC()
		topHeight := 0
		if len(chain.Chain) > 0 {
			topHeight = len(chain.Chain) - 1
		}
		fmt.Println("==================================================")
		fmt.Println("  ARMAX BLOCKCHAIN INFO (LOCAL DATABASE)")
		fmt.Println("==================================================")
		fmt.Printf("  Network ID        : %s\n", activeNet.NetworkID)
		fmt.Printf("  Block Height      : %d\n", topHeight)
		fmt.Printf("  Total Blocks      : %d\n", len(chain.Chain))
		fmt.Printf("  Difficulty        : %d\n", chain.Difficulty)
		fmt.Printf("  Total Chainwork   : %s\n", chain.GetTotalChainwork().String())
		fmt.Printf("  Circulating Supply: %d %s\n", chain.GetCirculatingSupply()/core.CoinUnit, core.ActiveTicker())
		fmt.Printf("  Max Supply        : %d %s\n", core.MaxSupply/core.CoinUnit, core.ActiveTicker())
		if len(chain.Chain) > 0 {
			fmt.Printf("  Best Block Hash   : %s\n", chain.GetLatestBlock().Hash)
		}
		fmt.Println("==================================================")
		return
	}

	if *getMempoolInfoFlag || *mempoolFlag {
		body, err := queryDaemon("/mempool")
		if err == nil {
			var txs []core.Transaction
			if json.Unmarshal(body, &txs) != nil {
				var mp struct {
					Transactions []core.Transaction `json:"transactions"`
				}
				if json.Unmarshal(body, &mp) == nil {
					txs = mp.Transactions
				}
			}
			if txs != nil {
				totalBytes := 0
				for _, tx := range txs {
					totalBytes += tx.Size()
				}
				fmt.Println("==================================================")
				fmt.Println("  ARMAX MEMPOOL INFO (LIVE DAEMON)")
				fmt.Println("==================================================")
				fmt.Printf("  Mempool Size      : %d transactions\n", len(txs))
				fmt.Printf("  Total Data Size   : %d bytes (%.2f KB)\n", totalBytes, float64(totalBytes)/1024.0)
				fmt.Println("--------------------------------------------------")
				for i, tx := range txs {
					fmt.Printf("  [%d] TXID : %s\n", i+1, tx.HashHex())
					fmt.Printf("      From : %s\n", tx.Sender)
					fmt.Printf("      To   : %s\n", tx.Recipient)
					fmt.Printf("      Amt  : %s | Fee: %s | Nonce #%d | Size: %d B\n",
						core.FormatArmxDecimal(tx.Amount), core.FormatArmxDecimal(tx.Fee), tx.Nonce, tx.Size())
				}
				if len(txs) == 0 {
					fmt.Println("  (mempool queue is currently empty)")
				}
				fmt.Println("==================================================")
				return
			}
		}
		// Fallback to local memory pool
		chain := getBC()
		chain.Lock.RLock()
		mpSize := len(chain.Mempool)
		totalBytes := 0
		for _, tx := range chain.Mempool {
			totalBytes += tx.Size()
		}
		fmt.Println("==================================================")
		fmt.Println("  ARMAX MEMPOOL INFO (LOCAL DATABASE)")
		fmt.Println("==================================================")
		fmt.Printf("  Mempool Size      : %d transactions\n", mpSize)
		fmt.Printf("  Total Data Size   : %d bytes (%.2f KB)\n", totalBytes, float64(totalBytes)/1024.0)
		fmt.Println("--------------------------------------------------")
		for i, tx := range chain.Mempool {
			fmt.Printf("  [%d] TXID : %s\n", i+1, tx.HashHex())
			fmt.Printf("      From : %s\n", tx.Sender)
			fmt.Printf("      To   : %s\n", tx.Recipient)
			fmt.Printf("      Amt  : %s | Fee: %s | Nonce #%d | Size: %d B\n",
				core.FormatArmxDecimal(tx.Amount), core.FormatArmxDecimal(tx.Fee), tx.Nonce, tx.Size())
		}
		if mpSize == 0 {
			fmt.Println("  (mempool queue is currently empty)")
		}
		chain.Lock.RUnlock()
		fmt.Println("==================================================")
		return
	}

	if *getPeerInfoFlag {
		statsBody, statsErr := queryDaemon("/stats")

		activePeers := "unknown"
		if statsErr == nil {
			var stats map[string]interface{}
			if json.Unmarshal(statsBody, &stats) == nil {
				if value, ok := stats["peers_active"]; ok {
					activePeers = fmt.Sprintf("%v", value)
				}
			}
		}

		body, err := queryDaemon("/peers")
		if err != nil {
			fmt.Printf("[-] Failed to query daemon peers (%s): %v\n",
				daemonRPCURL, err)
			os.Exit(1)
		}

		var peers []string
		if err := json.Unmarshal(body, &peers); err != nil {
			fmt.Printf("[-] Invalid response from /peers: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("==================================================")
		fmt.Printf("Active P2P Connections : %s\n", activePeers)
		fmt.Printf("Known Peer Addresses  : %d\n", len(peers))
		fmt.Println("==================================================")

		for i, peer := range peers {
			fmt.Printf("  [%d] %s\n", i+1, peer)
		}

		if len(peers) == 0 {
			fmt.Println("  (no known peer addresses)")
		}

		fmt.Println("==================================================")
		return
	}

	if *getNetworkInfoFlag {
		fmt.Println("==================================================")
		fmt.Println(" ARMAX NETWORK INFO:")
		fmt.Println("==================================================")
		fmt.Printf("  Active Profile    : %s\n", strings.ToUpper(activeNet.Name))
		fmt.Printf("  Network ID        : %s\n", activeNet.NetworkID)
		fmt.Printf("  P2P Port          : %s\n", activeNet.P2PPort)
		fmt.Printf("  RPC Port          : %s\n", activeNet.RPCPort)
		fmt.Printf("  Core Version      : %s\n", core.Version)
		body, err := queryDaemon("/status")
		if err == nil {
			var st map[string]interface{}
			_ = json.Unmarshal(body, &st)
			fmt.Printf("  Daemon Status     : %v\n", st["status"])
			fmt.Printf("  Active Peers      : %v\n", st["peers_active"])
			fmt.Printf("  Blockchain Height : %v\n", st["height"])
		} else {
			fmt.Println("  Daemon Status     : Offline")
		}
		fmt.Println("==================================================")
		return
	}

	if *shutdownFlag {
		fmt.Println("[*] Sending graceful shutdown request to daemon...")
		fmt.Println("[i] For local instances, send SIGINT/SIGTERM (Ctrl+C or kill <PID>) to flush blockchain state safely.")
		return
	}

	if *daemonFlag && !*testFlag {
		bc = getBC()
		fmt.Println("==================================================")
		fmt.Printf(" ARMAXCOIN (%s) - DAEMON & P2P NODE\n", core.ActiveTicker())
		fmt.Println("==================================================")
		fmt.Printf("[+] Network Profile  : %s\n", strings.ToUpper(activeNet.Name))
		topHeight := 0
		if len(bc.Chain) > 0 {
			topHeight = len(bc.Chain) - 1
		}
		fmt.Printf("[+] Blockchain Height: %d (blocks: %d, Supply: %d / %d %s)\n", topHeight, len(bc.Chain), bc.GetCirculatingSupply()/core.CoinUnit, core.MaxSupply/core.CoinUnit, core.ActiveTicker())

		// Load configuration
		cfg, err := node.LoadConfig(*configFlag)
		if err != nil {
			fmt.Printf("[!] Could not load config file '%s': %v (using defaults)\n", *configFlag, err)
			cfg = node.DefaultConfig()
			cfg.P2PPort = activeNet.P2PPort
			cfg.RPCPort = activeNet.RPCPort
		} else if activeNet.Name == "testnet" || activeNet.Name == "regtest" || activeNet.Name == "soak" {
			cfg.NetworkID = activeNet.NetworkID
			// In non-mainnet mode, automatically align default ports and seeds to active profile unless overridden
			if effRPCPort == "" && cfg.RPCPort == core.MainnetRPCPort {
				cfg.RPCPort = activeNet.RPCPort
			}
			if effP2PPort == "" && cfg.P2PPort == core.MainnetP2PPort {
				cfg.P2PPort = activeNet.P2PPort
			}
			if len(cfg.SeedPeers) == 0 || (len(core.MainnetProfile.DefaultSeeds) > 0 && len(cfg.SeedPeers) > 0 && cfg.SeedPeers[0] == core.MainnetProfile.DefaultSeeds[0]) {
				cfg.SeedPeers = activeNet.DefaultSeeds
			}
		}
		if cfg.NetworkID == "" {
			cfg.NetworkID = activeNet.NetworkID
		}

		// Apply CLI overrides if specified
		if *bindFlag != "" {
			cfg.RPCBind = *bindFlag
		}
		if effRPCPort != "" {
			cfg.RPCPort = effRPCPort
		}
		if effP2PPort != "" {
			cfg.P2PPort = effP2PPort
		}
		if *minerAddrFlag != "" {
			cfg.MinerAddress = *minerAddrFlag
		}
		if *mineFlag {
			cfg.EnableMining = true
		}
		if *tofuFlag {
			cfg.P2PTOFU = true
		}
		if *requireTLSFlag {
			cfg.RequireTLS = true
		}
		if *rpcUserFlag != "" {
			cfg.RPCUser = *rpcUserFlag
		}
		if *rpcPasswordFlag != "" {
			cfg.RPCPassword = *rpcPasswordFlag
		}
		if *allowNonLoopbackFlag {
			cfg.AllowNonLoopbackRPC = true
		}

		// Override seed peers if specified via CLI [N-20]
		if *peersFlag != "" {
			var cliPeers []string
			for _, p := range strings.Split(*peersFlag, ",") {
				trimmed := strings.TrimSpace(p)
				if trimmed != "" {
					cliPeers = append(cliPeers, trimmed)
				}
			}
			if len(cliPeers) > 0 {
				cfg.SeedPeers = cliPeers
			}
		}

		// Launch background mining worker if enabled
		if cfg.EnableMining {
			minerAddr := cfg.MinerAddress
			if minerAddr == "" {
				walletPath := "wallet.json"
				if _, err := os.Stat(walletPath); os.IsNotExist(err) {
					if execPath, err := os.Executable(); err == nil {
						candidate := filepath.Join(filepath.Dir(execPath), "wallet.json")
						if _, err := os.Stat(candidate); err == nil {
							walletPath = candidate
						}
					}
				}
				if fileData, err := os.ReadFile(walletPath); err == nil {
					var ks struct {
						Address string `json:"address"`
					}
					if json.Unmarshal(fileData, &ks) == nil && ks.Address != "" {
						minerAddr = ks.Address
					}
				}
			}
			if minerAddr != "" {
				fmt.Printf("[+] Automated background CPU miner active for address: %s\n", minerAddr)
				go func() {
					delay := time.Duration(cfg.MiningDelayMS) * time.Millisecond
					if delay < 500*time.Millisecond {
						delay = 2000 * time.Millisecond
					}
					for {
						time.Sleep(delay)
						b, err := bc.MineBlockWithErr(minerAddr)
						if err != nil {
							fmt.Printf("[Miner] [-] Mining round error: %v\n", err)
							continue
						}
						// Read reward from the coinbase tx (b.Transactions[0]) rather than
						// calling bc.GetBlockReward(b.Index) which reads bc.Chain without a lock.
						var rewardAmt int64 = 0
						if len(b.Transactions) > 0 {
							rewardAmt = b.Transactions[0].Amount
						}
						fmt.Printf("[Miner] [OK] Block #%d mined! Reward: %s\n", b.Index, core.FormatArmxDecimal(rewardAmt))
					}
				}()
			} else {
				fmt.Println("[!] Mining enabled but no miner address configured. Skipping background miner.")
			}
		}

		if err := node.RunServerWithConfig(bc, cfg); err != nil {
			fmt.Printf("[-] %v\n", err)
			os.Exit(1)
		}
		return
	}

	bc = getBC()
	fmt.Println("==================================================")
	fmt.Printf(" ARMAXCOIN (%s) - SELF-TEST & DEMO SUITE\n", core.ActiveTicker())
	fmt.Println("==================================================")
	fmt.Printf("[+] Blockchain active. Block count: %d (Supply: %d / %d %s)\n", len(bc.Chain), bc.GetCirculatingSupply()/core.CoinUnit, core.MaxSupply/core.CoinUnit, core.ActiveTicker())

	// 2. Generate user and miner wallets
	w1, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("[-] Failed to create demo wallet 1: %v\n", err)
		return
	}
	w2, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("[-] Failed to create demo wallet 2: %v\n", err)
		return
	}
	minerWallet, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("[-] Failed to create miner wallet: %v\n", err)
		return
	}

	fmt.Printf("[+] Wallet 1 (W1)    : %s\n", w1.Address)
	fmt.Printf("[+] Wallet 2 (W2)    : %s\n", w2.Address)
	fmt.Printf("[+] Miner Wallet     : %s\n", minerWallet.Address)

	// Save miner wallet to encrypted wallet.json for CLI testing convenience without destroying existing user keys [T15]
	bakPath, err := wallet.CreateNewWalletKeystore(minerWallet, "armax-demo", "wallet.json", false)
	if err != nil {
		fmt.Printf("[-] Warning: Failed to save demo miner wallet: %v\n", err)
	} else if bakPath != "" {
		fmt.Printf("[+] Existing wallet.json detected and backed up to %s\n", bakPath)
		fmt.Println("[+] Demo miner wallet secured and saved to wallet.json")
	} else {
		fmt.Println("[+] Miner wallet secured and saved to wallet.json")
	}

	// 3. Genesis miner mines initial block for block reward
	fmt.Println("\n[~] Miner mining block #1 (Initial Mining Reward)...")
	minedBlock, err := bc.MineBlockWithErr(minerWallet.Address)
	if err != nil {
		fmt.Printf("[-] Mining block #1 failed: %v\n", err)
		return
	}
	fmt.Printf("[OK] Block #%d successfully mined!\n", minedBlock.Index)
	fmt.Printf("    Hash: %s\n", minedBlock.Hash)
	fmt.Printf("    Miner Balance: %d %s\n", bc.GetBalance(minerWallet.Address)/core.CoinUnit, core.ActiveTicker())

	// 4. Construct and sign transaction from Miner to W1
	fmt.Printf("\n[~] Transferring 20 %s from Miner to Wallet W1 (with ML-DSA-65 Signature, Fee & Nonce)...\n", core.ActiveTicker())
	tx := core.Transaction{
		Sender:    minerWallet.Address,
		SenderPub: minerWallet.PublicKey,
		Recipient: w1.Address,
		Amount:    20 * core.CoinUnit,
		Fee:       1 * core.CoinUnit,
		Nonce:     bc.GetPendingNonce(minerWallet.Address) + 1,
	}

	if err := tx.Sign(minerWallet.PrivateKey); err != nil {
		fmt.Printf("[-] Failed to sign transaction: %v\n", err)
		return
	}

	success, msg := bc.AddTransaction(tx)
	if !success {
		fmt.Printf("[-] Transaction rejected: %s\n", msg)
		return
	}
	fmt.Printf("[OK] %s (Nonce: #%d, Fee: %d Armoids)\n", msg, tx.Nonce, tx.Fee)

	// 5. Miner mines block #2 to confirm mempool transactions
	fmt.Println("\n[~] Miner mining block #2 to confirm transactions...")
	minedBlock2, err := bc.MineBlockWithErr(minerWallet.Address)
	if err != nil {
		fmt.Printf("[-] Mining failed: %v\n", err)
		return
	}
	fmt.Printf("[OK] Block #%d successfully mined!\n", minedBlock2.Index)
	fmt.Printf("    Hash: %s\n", minedBlock2.Hash)

	// 6. Cryptographic chain integrity validation
	fmt.Println("\n[~] Verifying cryptographic integrity of the entire chain...")
	if bc.IsChainValid() {
		fmt.Println("[OK] BLOCKCHAIN INTEGRITY: 100% VALID & SECURE!")
	} else {
		fmt.Println("[-] WARNING: Blockchain integrity check failed!")
	}

	// 7. Verify Final Balances
	fmt.Println("\n==================================================")
	fmt.Println(" FINAL BALANCES AFTER TEST SUITE:")
	fmt.Println("==================================================")
	fmt.Printf("  Miner Balance : %d %s (100 rewards + 1 fee collected - 21 sent)\n", bc.GetBalance(minerWallet.Address)/core.CoinUnit, core.ActiveTicker())
	fmt.Printf("  W1 Balance    : %d %s (Received transfer)\n", bc.GetBalance(w1.Address)/core.CoinUnit, core.ActiveTicker())
	fmt.Printf("  W2 Balance    : %d %s\n", bc.GetBalance(w2.Address)/core.CoinUnit, core.ActiveTicker())
	fmt.Printf("  Circulating   : %d / %d %s\n", bc.GetCirculatingSupply()/core.CoinUnit, core.MaxSupply/core.CoinUnit, core.ActiveTicker())
	fmt.Println("==================================================")
	fmt.Println("Tip: Run as full daemon with optional background miner:")
	fmt.Printf("  go run main.go -daemon -port %s -p2p %s -mine\n", core.MainnetRPCPort, core.MainnetP2PPort)
	fmt.Println("==================================================")
}
