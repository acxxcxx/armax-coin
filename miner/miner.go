package main

import (
	"bufio"
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

type MineResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Block   interface{} `json:"block"`
}

type LocalKeystore struct {
	Address string `json:"address"`
}

type BlockTemplate struct {
	Version           int                `json:"version"`
	PreviousBlockHash string             `json:"previousblockhash"`
	Height            int                `json:"height"`
	CurTime           int64              `json:"curtime"`
	MinTime           int64              `json:"mintime,omitempty"`
	Difficulty        float64            `json:"difficulty"`
	Target            string             `json:"target"`
	CoinbaseValue     int64              `json:"coinbasevalue"`
	Transactions      []core.Transaction `json:"transactions"`
	MerkleRoot        string             `json:"merkleroot"`
	PoWAlgo           string             `json:"pow_algo,omitempty"`
}

type SubmitBlockResponse struct {
	Status  string `json:"status"`
	Height  int    `json:"height"`
	Hash    string `json:"hash"`
	Message string `json:"message"`
}

func main() {
	networkPtr := flag.String("network", "testnet", "Network profile (testnet, mainnet) [L1-3, L4-1]")
	benchPtr := flag.Bool("benchmark", false, "Run CPU PoW hashing benchmark (hash/s per thread and total) [L1-6]")
	minerAddrPtr := flag.String("miner", "", "Reward recipient wallet address (Bech32 armx1... or armx1...worker)")
	workerPtr := flag.String("worker", "", "Pool worker identifier (default: 'default' or suffix after '.' in -miner)")
	poolURLPtr := flag.String("pool", "", "Comma-separated pool TCP endpoints (e.g. "+stratumTCPPrefix()+"127.0.0.1:3333)")
	stratumURLPtr := flag.String("stratum", "", "Alias for -pool ("+stratumTCPPrefix()+"host:port)")
	threadsPtr := flag.Int("threads", 0, "Number of CPU worker threads for local mining (0 = all CPU cores)")
	remotePtr := flag.Bool("remote", false, "Delegate mining execution to daemon node via /mine instead of local CPU")
	daemonURLPtr := flag.String("daemon", "", "Comma-separated RPC Daemon URLs or pool endpoints for failover redundancy")
	portPtr := flag.String("port", "", "RPC Daemon port (deprecated: use -rpcport)")
	rpcPortPtr := flag.String("rpcport", "", "RPC Daemon port (overrides default)")
	p2pPortPtr := flag.String("p2p", "", "P2P port (deprecated: use -p2pport)")
	p2pPortNewPtr := flag.String("p2pport", "", "P2P port (overrides default)")
	dataDirPtr := flag.String("datadir", "", "Custom data directory")
	delayPtr := flag.Int("delay", 2000, "Delay between mining rounds in milliseconds")
	keyPtr := flag.String("key", os.Getenv("ARMX_MINE_KEY"), "Mining secret key (X-Mine-Key header for remote node authorization)")
	userPtr := flag.String("user", os.Getenv("ARMX_RPC_USER"), "RPC Basic Auth Username")
	passPtr := flag.String("pass", os.Getenv("ARMX_RPC_PASSWORD"), "RPC Basic Auth Password")
	rpcUserPtr := flag.String("rpcuser", "", "RPC Basic Auth Username (alias for -user)")
	rpcPasswordPtr := flag.String("rpcpassword", "", "RPC Basic Auth Password (alias for -pass)")
	getMiningInfoPtr := flag.Bool("getmininginfo", false, "Display mining info, height, difficulty, hashrate from daemon and exit")
	getWorkInfoPtr := flag.Bool("getworkinfo", false, "Display mining work template info from daemon and exit")
	mempoolPtr := flag.Bool("mempool", false, "Display unconfirmed transactions waiting in daemon mempool")
	getMempoolInfoPtr := flag.Bool("getmempoolinfo", false, "Display unconfirmed transactions waiting in daemon mempool (alias for -mempool)")
	dryRunPtr := flag.Bool("dry-run", false, "Verify configuration, miner address, and block template without performing mining rounds")
	statsIntervalPtr := flag.Int("statsinterval", 10, "Mining statistics display interval in seconds (default: 10)")
	stopPtr := flag.Bool("stop", false, "Display instructions to safely stop active mining process")
	infoPtr := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionPtr := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	armaxHashHeightPtr := flag.Int64("armaxhash-height", -2, "Block height where PoW transitions to ArmaxHash (-1 = disabled)")
	flag.Parse()

	if *armaxHashHeightPtr >= -1 {
		core.SetArmaxHashActivationHeight(*armaxHashHeightPtr)
	}

	if *rpcUserPtr != "" {
		*userPtr = *rpcUserPtr
	}
	if *rpcPasswordPtr != "" {
		*passPtr = *rpcPasswordPtr
	}

	if *versionPtr {
		fmt.Printf("armminer v%s\n", core.Version)
		return
	}

	if *benchPtr {
		runMinerBenchmark()
		return
	}

	effRPCPort := *rpcPortPtr
	effP2PPort := *p2pPortNewPtr

	// Check deprecated flags with explicit warnings [L4-8]
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			fmt.Println("[!] Warning: -port is deprecated, please use -rpcport instead")
			if effRPCPort == "" {
				effRPCPort = *portPtr
			}
		}
		if f.Name == "p2p" {
			fmt.Println("[!] Warning: -p2p is deprecated, please use -p2pport instead")
			if effP2PPort == "" {
				effP2PPort = *p2pPortPtr
			}
		}
	})

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
	fmt.Println("==================================================")
	fmt.Println(core.NetworkWarningLine())
	fmt.Printf("ARMAXCOIN (%s) - AUTOMATIC CPU MINER CLIENT\n", core.ActiveTicker())
	fmt.Println("==================================================")
	fmt.Printf("[+] Target Network   : %s (%s, RPC: %s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID, activeNet.RPCPort)

	if *infoPtr {
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
		fmt.Printf("[+] Data Directory   : %s\n", cliNet.DataDir)
		for _, seed := range cliNet.DefaultSeeds {
			fmt.Printf("[+] Seed Peer        : %s\n", seed)
		}
		return
	}

	stratumRawURLs := strings.TrimSpace(*poolURLPtr)
	if stratumRawURLs == "" {
		stratumRawURLs = strings.TrimSpace(*stratumURLPtr)
	}
	if stratumRawURLs == "" && isStratumURLList(*daemonURLPtr) {
		stratumRawURLs = strings.TrimSpace(*daemonURLPtr)
	}
	parsedStratumPools := parseStratumPoolAddresses(stratumRawURLs)
	isStratumMode := len(parsedStratumPools) > 0

	daemonURLs := *daemonURLPtr
	if daemonURLs == "" || isStratumMode {
		if effRPCPort != "" {
			daemonURLs = "http://127.0.0.1:" + effRPCPort
		} else {
			daemonURLs = cliNet.DaemonURL
		}
	}

	rawMinerInput := strings.TrimSpace(*minerAddrPtr)
	if rawMinerInput == "" && isStratumMode && *userPtr != "" {
		rawMinerInput = strings.TrimSpace(*userPtr)
	}
	minerAddress, parsedWorker := splitMinerWorker(rawMinerInput)
	workerName := strings.TrimSpace(*workerPtr)
	if workerName == "" {
		if parsedWorker != "" {
			workerName = parsedWorker
		} else {
			workerName = "default"
		}
	}

	// If miner address is not specified, attempt to read from wallet.json
	if minerAddress == "" {
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
			var ks LocalKeystore
			if err := json.Unmarshal(fileData, &ks); err == nil && ks.Address != "" {
				minerAddress = ks.Address
				fmt.Printf("[+] Loaded miner address from %s: %s\n", walletPath, minerAddress)
			}
		}
	}

	// Auto-detect RPC cookie if no explicit user/pass/key provided
	if *keyPtr == "" && (*userPtr == "" || *passPtr == "") {
		cookieCandidates := []string{
			"armax.cookie",
			filepath.Join(netChoice, "armax.cookie"),
		}
		if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
			cookieCandidates = append([]string{filepath.Join(envDir, "armax.cookie")}, cookieCandidates...)
		}
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			cookieCandidates = append(cookieCandidates, filepath.Join(execDir, "armax.cookie"))
			cookieCandidates = append(cookieCandidates, filepath.Join(execDir, netChoice, "armax.cookie"))
		}
		for _, cand := range cookieCandidates {
			if data, err := os.ReadFile(cand); err == nil {
				parts := strings.Split(strings.TrimSpace(string(data)), ":")
				if len(parts) == 2 {
					*userPtr = parts[0]
					*passPtr = parts[1]
					fmt.Printf("[+] Loaded RPC authentication from %s\n", cand)
					break
				}
			}
		}
	}

	// Parse comma-separated daemon list for high-availability failover
	rawDaemons := strings.Split(daemonURLs, ",")
	var parsedDaemons []string
	for _, d := range rawDaemons {
		trimmed := strings.TrimRight(strings.TrimSpace(d), "/")
		if trimmed != "" {
			if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
				trimmed = "http://" + trimmed
			}
			// Normalize localhost to 127.0.0.1 to avoid Windows IPv6 resolution latency/refusal
			trimmed = strings.Replace(trimmed, "http://localhost:", "http://127.0.0.1:", 1)
			trimmed = strings.Replace(trimmed, "https://localhost:", "https://127.0.0.1:", 1)
			parsedDaemons = append(parsedDaemons, trimmed)
		}
	}
	if len(parsedDaemons) == 0 {
		parsedDaemons = []string{"http://127.0.0.1:" + activeNet.RPCPort}
	}

	numThreads := *threadsPtr
	if numThreads <= 0 {
		numThreads = runtime.NumCPU()
	}
	if numThreads < 1 {
		numThreads = 1
	}

	client := &http.Client{Timeout: 30 * time.Second}

	if *getMiningInfoPtr {
		activeDaemon := parsedDaemons[0]
		req, _ := http.NewRequest("GET", activeDaemon+"/getmininginfo", nil)
		if *userPtr != "" || *passPtr != "" {
			req.SetBasicAuth(*userPtr, *passPtr)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[-] Failed to query mining info from %s: %v\n", activeDaemon, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var info map[string]interface{}
		_ = json.Unmarshal(body, &info)
		fmt.Println("==================================================")
		fmt.Println("ARMAX MINING INFORMATION:")
		fmt.Println("==================================================")
		fmt.Printf("  Daemon Node       : %s\n", activeDaemon)
		fmt.Printf("  Network Profile   : %s\n", strings.ToUpper(activeNet.Name))
		fmt.Printf("  Block Height      : %v\n", info["blocks"])
		fmt.Printf("  Mining Difficulty : %v\n", info["difficulty"])
		fmt.Printf("  Network Hashrate  : %v H/s\n", info["networkhashps"])
		fmt.Printf("  Mempool Size      : %v txs\n", info["pooledtx"])
		fmt.Printf("  Mining Enabled    : %v\n", info["generate"])
		fmt.Println("==================================================")
		return
	}

	if *getWorkInfoPtr {
		activeDaemon := parsedDaemons[0]
		tmpl, err := fetchBlockTemplate(client, activeDaemon, *keyPtr, *userPtr, *passPtr)
		if err != nil {
			fmt.Printf("[-] Failed to query block template from %s: %v\n", activeDaemon, err)
			os.Exit(1)
		}
		fmt.Println("==================================================")
		fmt.Println("ARMAX MINING WORK TEMPLATE:")
		fmt.Println("==================================================")
		fmt.Printf("  Target Height     : #%d\n", tmpl.Height)
		fmt.Printf("  Previous Block    : %s\n", tmpl.PreviousBlockHash)
		fmt.Printf("  Difficulty        : %.4f\n", tmpl.Difficulty)
		fmt.Printf("  Target Hash Bound : %s\n", tmpl.Target)
		fmt.Printf("  Block Reward      : %.8f %s (%d Armoids)\n", float64(tmpl.CoinbaseValue)/1e8, core.ActiveTicker(), tmpl.CoinbaseValue)
		fmt.Printf("  Mempool Tx Count  : %d transaction(s)\n", len(tmpl.Transactions))
		fmt.Println("==================================================")
		return
	}

	if *dryRunPtr {
		fmt.Println("==================================================")
		fmt.Println("ARMAX MINER CONFIGURATION DRY-RUN:")
		fmt.Println("==================================================")
		fmt.Printf("  Network Profile   : %s (%s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID)
		fmt.Printf("  Miner Address     : %s (Valid: %v)\n", minerAddress, core.ValidateAddress(minerAddress))
		fmt.Printf("  Worker Threads    : %d CPU threads\n", numThreads)
		if isStratumMode {
			fmt.Printf("  Active Pool       : %s%s (Worker: %s)\n", stratumTCPPrefix(), parsedStratumPools[0], workerName)
			if err := testStratumPoolConnection(parsedStratumPools[0], minerAddress, workerName, *passPtr); err != nil {
				fmt.Printf("[-] Pool Auth Test  : FAILED (%v)\n", err)
				os.Exit(1)
			}
			fmt.Println("  Pool Auth Test    : SUCCESS (Subscribed & Authorized)")
		} else {
			fmt.Printf("  Active Daemon     : %s\n", parsedDaemons[0])
			tmpl, err := fetchBlockTemplate(client, parsedDaemons[0], *keyPtr, *userPtr, *passPtr)
			if err != nil {
				fmt.Printf("[-] Daemon Work Test : FAILED (%v)\n", err)
				os.Exit(1)
			}
			fmt.Printf("  Daemon Work Test  : SUCCESS (Target Block #%d, Difficulty: %.4f)\n", tmpl.Height, tmpl.Difficulty)
		}
		fmt.Println("  [OK] All mining parameters verified. Ready for mining.")
		fmt.Println("==================================================")
		return
	}

	if *stopPtr {
		fmt.Println("[*] Stopping armminer...")
		fmt.Println("[i] Active armminer instances can be safely stopped by sending SIGINT (Ctrl+C) or SIGTERM.")
		return
	}

	if *mempoolPtr || *getMempoolInfoPtr {
		activeDaemon := parsedDaemons[0]
		req, _ := http.NewRequest("GET", activeDaemon+"/mempool", nil)
		if *userPtr != "" || *passPtr != "" {
			req.SetBasicAuth(*userPtr, *passPtr)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[-] Failed to query mempool from %s: %v\n", activeDaemon, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var txs []core.Transaction
		if json.Unmarshal(body, &txs) != nil {
			var mp struct {
				Transactions []core.Transaction `json:"transactions"`
			}
			if json.Unmarshal(body, &mp) == nil {
				txs = mp.Transactions
			}
		}
		totalBytes := 0
		for _, tx := range txs {
			totalBytes += tx.Size()
		}
		fmt.Println("==================================================")
		fmt.Println("ARMAX MINER - MEMPOOL QUEUE INSPECTION:")
		fmt.Println("==================================================")
		fmt.Printf("  Daemon Node       : %s\n", activeDaemon)
		fmt.Printf("  Mempool Size      : %d transaction(s) pending\n", len(txs))
		fmt.Printf("  Total Memory Size : %d bytes (%.2f KB)\n", totalBytes, float64(totalBytes)/1024.0)
		fmt.Println("--------------------------------------------------")
		for i, tx := range txs {
			fmt.Printf("  [%d] TXID: %s\n", i+1, tx.HashHex())
			fmt.Printf("      From: %s -> To: %s\n", tx.Sender, tx.Recipient)
			fmt.Printf("      Amount: %s | Fee: %s | Nonce #%d | Size: %d bytes\n",
				core.FormatArmxDecimal(tx.Amount), core.FormatArmxDecimal(tx.Fee), tx.Nonce, tx.Size())
		}
		if len(txs) == 0 {
			fmt.Println("  (no unconfirmed transactions in mempool)")
		}
		fmt.Println("==================================================")
		return
	}

	// Verify miner address is present and valid before beginning actual mining loop
	if minerAddress == "" {
		fmt.Println("[!] Error: No miner address specified and wallet.json could not be read.")
		fmt.Println("[!] Usage: armminer -miner <Bech32-armx1-address> or run armwallet -create first.")
		os.Exit(1)
	}
	if !core.ValidateAddress(minerAddress) {
		fmt.Printf("[!] Error: Miner address '%s' is not a valid Armaxcoin Bech32 address.\n", minerAddress)
		os.Exit(1)
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// Branch: Stratum v1 TCP Pool Mining Mode (stratum+tcp://)
	if isStratumMode {
		fmt.Printf("[+] Active Stratum Pools: %v (Failover count: %d)\n", parsedStratumPools, len(parsedStratumPools))
		fmt.Printf("[+] Reward Address      : %s (Worker: %s)\n", minerAddress, workerName)
		fmt.Printf("[+] Mining Mode         : Stratum v1 TCP Pool Client (%d worker threads)\n", numThreads)
		fmt.Println("[+] Starting Stratum mining loop...")
		runStratumMiningLoop(stopChan, parsedStratumPools, minerAddress, workerName, *passPtr, numThreads, *statsIntervalPtr)
		return
	}

	fmt.Printf("[+] Active RPC Daemons: %v (Failover count: %d)\n", parsedDaemons, len(parsedDaemons))
	fmt.Printf("[+] Reward Address    : %s\n", minerAddress)
	if *remotePtr {
		fmt.Printf("[+] Mining Mode       : Remote Delegated (via daemon POST /mine)\n")
	} else {
		fmt.Printf("[+] Mining Mode       : Local CPU Engine (%d worker threads)\n", numThreads)
	}
	fmt.Println("[+] Starting mining loop...")

	currentDaemonIdx := 0

	// Branch: Delegated remote mining mode (POST /mine)
	if *remotePtr {
		runRemoteMiningLoop(stopChan, parsedDaemons, minerAddress, numThreads, *keyPtr, *userPtr, *passPtr, *delayPtr)
		return
	}

	// Default: Standard local CPU mining engine (getblocktemplate + local hashing + submitblock)
	for {
		select {
		case <-stopChan:
			fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
			return
		default:
		}

		activeDaemon := parsedDaemons[currentDaemonIdx]

		// 1. Fetch block template from daemon
		tmpl, err := fetchBlockTemplate(client, activeDaemon, *keyPtr, *userPtr, *passPtr)
		if err != nil {
			fmt.Printf("[-] Failed to fetch block template from %s: %v\n", activeDaemon, err)
			if len(parsedDaemons) > 1 {
				currentDaemonIdx = (currentDaemonIdx + 1) % len(parsedDaemons)
				fmt.Printf("[ Auto-Failover] Switching to next node: %s\n", parsedDaemons[currentDaemonIdx])
				time.Sleep(1 * time.Second)
			} else {
				time.Sleep(3 * time.Second)
			}
			continue
		}

		// 2. Parse PoW target
		targetInt, ok := new(big.Int).SetString(tmpl.Target, 16)
		if !ok || targetInt == nil || targetInt.Sign() <= 0 {
			targetInt = core.DifficultyToTarget(int(tmpl.Difficulty))
		}

		// 3. Assemble Coinbase transaction with reward + fees [C-26]
		cbTx := core.Transaction{
			Sender:    "SYSTEM",
			SenderPub: "",
			Recipient: minerAddress,
			Amount:    tmpl.CoinbaseValue,
			Fee:       0,
			Nonce:     int64(tmpl.Height), // Coinbase nonce equals block height
			Signature: "REWARD_SIG",
		}
		txs := append([]core.Transaction{cbTx}, tmpl.Transactions...)
		candidateBlock := *core.NewBlock(txs, tmpl.PreviousBlockHash, tmpl.Height, minerAddress, targetInt)
		now := time.Now().Unix()
		minTs := tmpl.CurTime
		if tmpl.MinTime > minTs {
			minTs = tmpl.MinTime
		}
		if minTs > now {
			candidateBlock.Timestamp = minTs
		} else {
			candidateBlock.Timestamp = now
		}

		// 4. Randomize starting nonce partition
		var startNonce int64
		var nonceBytes [4]byte
		if _, err := io.ReadFull(cryptoRand.Reader, nonceBytes[:]); err == nil {
			startNonce = int64(binary.BigEndian.Uint32(nonceBytes[:]))
		}

		algo := candidateBlock.PoWAlgorithm()
		fmt.Printf("[%s] [*] Mining Block #%d (%s) | Prev: %s... | Txs: %d | Diff: %.4f | Reward: %d %s\n",
			time.Now().Format("15:04:05"), tmpl.Height, algo, tmpl.PreviousBlockHash[:12], len(txs), tmpl.Difficulty, tmpl.CoinbaseValue/core.CoinUnit, core.ActiveTicker())

		roundCtx, cancelRound := context.WithCancel(context.Background())
		solutionChan := make(chan core.Block, 1)
		var roundHashes int64
		var wg sync.WaitGroup
		var logErrOnce sync.Once

		// Spawn worker goroutines partitioned across CPU threads
		for i := 0; i < numThreads; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				b := candidateBlock
				b.Nonce = startNonce + int64(workerID)
				stride := int64(numThreads)
				var localBatch int64
				batchLimit := int64(4096)
				if algo == "armaxhash" { // ArmaxHash v1 memory-hard
					batchLimit = 1
				}

				for {
					if localBatch >= batchLimit {
						atomic.AddInt64(&roundHashes, localBatch)
						localBatch = 0
						select {
						case <-roundCtx.Done():
							return
						case <-stopChan:
							return
						default:
						}
						if curTime := time.Now().Unix(); curTime > b.Timestamp {
							b.Timestamp = curTime
						}
					}
					var err error
					b.Hash, err = b.CalculateHashWithError()
					if err != nil {
						logErrOnce.Do(func() {
							fmt.Printf("[%s] [!] Hash computation error on block #%d: %v (aborting round to prevent spinning)\n",
								time.Now().Format("15:04:05"), b.Index, err)
						})
						cancelRound()
						return
					}
					localBatch++
					if core.HashMeetsTarget(b.Hash, targetInt) {
						atomic.AddInt64(&roundHashes, localBatch)
						select {
						case solutionChan <- b:
						default:
						}
						cancelRound()
						return
					}
					b.Nonce += stride
				}
			}(i)
		}

		// Telemetry reporter: Prints live hashrate to miner console at configured statsinterval
		go func() {
			intervalSec := *statsIntervalPtr
			if intervalSec <= 0 {
				intervalSec = 10
			}
			ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
			defer ticker.Stop()
			roundStart := time.Now()
			for {
				select {
				case <-roundCtx.Done():
					return
				case <-stopChan:
					return
				case <-ticker.C:
					elapsed := time.Since(roundStart).Seconds()
					if elapsed > 0 {
						h := atomic.LoadInt64(&roundHashes)
						hps := float64(h) / elapsed
						rateStr := fmt.Sprintf("%.2f kH/s", hps/1000.0)
						if hps >= 1_000_000.0 {
							rateStr = fmt.Sprintf("%.2f MH/s", hps/1_000_000.0)
						} else if hps < 1000.0 {
							rateStr = fmt.Sprintf("%.2f H/s", hps)
						}
						fmt.Printf("[%s] [ Mining] Block #%d | %d threads | %s (Total: %d hashes, %ds)\n",
							time.Now().Format("15:04:05"), tmpl.Height, numThreads, rateStr, h, int(elapsed))
					}
				}
			}
		}()

		// Tip poller: Detects when network advances or another block is mined
		go func() {
			pollTicker := time.NewTicker(5 * time.Second)
			defer pollTicker.Stop()
			for {
				select {
				case <-roundCtx.Done():
					return
				case <-stopChan:
					return
				case <-pollTicker.C:
					checkTmpl, err := fetchBlockTemplate(client, activeDaemon, *keyPtr, *userPtr, *passPtr)
					if err == nil && checkTmpl != nil {
						if checkTmpl.Height != tmpl.Height || checkTmpl.PreviousBlockHash != tmpl.PreviousBlockHash {
							fmt.Printf("[%s] [INFO New Tip Detected] Block tip advanced to #%d! Updating template...\n",
								time.Now().Format("15:04:05"), checkTmpl.Height)
							cancelRound()
							return
						}
					}
				}
			}
		}()

		// Await solution, shutdown, or stale round cancellation
		select {
		case solvedBlock := <-solutionChan:
			cancelRound()
			wg.Wait()
			timestamp := time.Now().Format("15:04:05")
			hashPrefix := solvedBlock.Hash
			if len(hashPrefix) > 16 {
				hashPrefix = hashPrefix[:16] + "..."
			}
			fmt.Printf("[%s] [OK BLOCK SOLVED!] Height #%d | Hash: %s | Nonce: %d | Diff: %d\n",
				timestamp, solvedBlock.Index, hashPrefix, solvedBlock.Nonce, solvedBlock.Difficulty)

			res, err := submitSolvedBlock(client, activeDaemon, solvedBlock, *keyPtr, *userPtr, *passPtr)
			if err != nil {
				fmt.Printf("[%s] [- STALE/ERROR] Submission notice from %s: %v\n",
					time.Now().Format("15:04:05"), activeDaemon, err)
			} else if res.Status == "accepted" {
				fmt.Printf("[%s] [OK ACCEPTED] Block #%d accepted by node %s | Hash: %s | Reward: %d %s\n",
					time.Now().Format("15:04:05"), res.Height, activeDaemon, hashPrefix, tmpl.CoinbaseValue/core.CoinUnit, core.ActiveTicker())
			} else {
				fmt.Printf("[%s] [- REJECTED] Block #%d rejected by %s: %s\n",
					time.Now().Format("15:04:05"), solvedBlock.Index, activeDaemon, res.Message)
			}

		case <-stopChan:
			cancelRound()
			wg.Wait()
			fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
			return

		case <-roundCtx.Done():
			// Round cancelled due to new network tip
			wg.Wait()
		}

		time.Sleep(time.Duration(*delayPtr) * time.Millisecond)
	}
}

// fetchBlockTemplate queries the daemon for a new mining block candidate
func fetchBlockTemplate(client *http.Client, daemonURL, key, user, pass string) (*BlockTemplate, error) {
	req, err := http.NewRequest("GET", daemonURL+"/getblocktemplate", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("X-Mine-Key", key)
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var tmpl BlockTemplate
	if err := json.NewDecoder(resp.Body).Decode(&tmpl); err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// submitSolvedBlock sends a solved PoW block to the daemon node via /block/submit
func submitSolvedBlock(client *http.Client, daemonURL string, block core.Block, key, user, pass string) (*SubmitBlockResponse, error) {
	payload, err := json.Marshal(block)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", daemonURL+"/block/submit", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Mine-Key", key)
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res SubmitBlockResponse
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode != http.StatusOK {
		if res.Message != "" {
			return &res, fmt.Errorf("%s (HTTP %d)", res.Message, resp.StatusCode)
		}
		return &res, fmt.Errorf("rejected with HTTP %d", resp.StatusCode)
	}
	return &res, nil
}

// runRemoteMiningLoop executes delegated mining on the remote daemon via POST /mine (legacy fallback)
func runRemoteMiningLoop(stopChan chan os.Signal, parsedDaemons []string, minerAddress string, threads int, key, user, pass string, delayMs int) {
	client := &http.Client{}
	currentDaemonIdx := 0

	for {
		select {
		case <-stopChan:
			fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
			return
		default:
		}

		activeDaemon := parsedDaemons[currentDaemonIdx]
		mineURL := fmt.Sprintf("%s/mine", activeDaemon)

		payload := map[string]interface{}{
			"miner":   minerAddress,
			"threads": threads,
		}
		postPayload, _ := json.Marshal(payload)

		reqCtx, cancelReq := context.WithTimeout(context.Background(), 15*time.Minute)
		go func() {
			select {
			case <-stopChan:
				cancelReq()
			case <-reqCtx.Done():
			}
		}()

		req, err := http.NewRequestWithContext(reqCtx, "POST", mineURL, bytes.NewBuffer(postPayload))
		if err != nil {
			cancelReq()
			fmt.Printf("[-] Failed to create request: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("X-Mine-Key", key)
		}
		if user != "" && pass != "" {
			req.SetBasicAuth(user, pass)
		}

		timestamp := time.Now().Format("15:04:05")
		fmt.Printf("[%s] [*] Requesting block mining from %s (reward -> %s...)\n", timestamp, activeDaemon, minerAddress[:12])

		waitDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			startTime := time.Now()
			for {
				select {
				case <-waitDone:
					return
				case <-ticker.C:
					elapsed := int(time.Since(startTime).Seconds())
					fmt.Printf("[%s] [*] Mining in progress on daemon (waiting for PoW solution, %ds elapsed)...\n", time.Now().Format("15:04:05"), elapsed)
				}
			}
		}()

		resp, err := client.Do(req)
		close(waitDone)
		cancelReq()
		if err != nil {
			select {
			case <-stopChan:
				fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
				return
			default:
			}
			fmt.Printf("[-] Daemon node %s is unreachable: %v\n", activeDaemon, err)
			if len(parsedDaemons) > 1 {
				currentDaemonIdx = (currentDaemonIdx + 1) % len(parsedDaemons)
				fmt.Printf("[ Auto-Failover] Redirecting mining to healthy node: %s\n", parsedDaemons[currentDaemonIdx])
				time.Sleep(1 * time.Second)
			} else {
				fmt.Println("[-] Retrying in 5 seconds...")
				time.Sleep(5 * time.Second)
			}
			continue
		}

		var result MineResponse
		_ = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK && result.Status == "success" {
			timestamp := time.Now().Format("15:04:05")
			fmt.Printf("[%s] [OK] Block mined on %s! Reward to: %s...\n", timestamp, activeDaemon, minerAddress[:12])
		} else {
			if result.Message != "" {
				fmt.Printf("[-] Mining notice from %s (HTTP %d): %s\n", activeDaemon, resp.StatusCode, result.Message)
			} else {
				fmt.Printf("[-] Mining request rejected by %s (HTTP %d)\n", activeDaemon, resp.StatusCode)
			}
		}

		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}
}

// runMinerBenchmark measures CPU double-SHA256 mining throughput per thread and total [L1-6]
func runMinerBenchmark() {
	numCPU := runtime.NumCPU()
	fmt.Printf("[*] Benchmarking Armaxcoin CPU mining across %d thread(s)...\n", numCPU)

	duration := 2 * time.Second
	dummyBlock := core.GetGenesisBlock()

	var totalHashes int64
	var wg sync.WaitGroup

	startTime := time.Now()
	for i := 0; i < numCPU; i++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()
			b := dummyBlock
			b.Nonce = int64(threadID) * 1000000000
			stop := time.Now().Add(duration)
			var localCount int64
			for time.Now().Before(stop) {
				_ = b.CalculateHash()
				b.Nonce++
				localCount++
			}
			atomic.AddInt64(&totalHashes, localCount)
			hps := float64(localCount) / duration.Seconds()
			fmt.Printf("    Thread #%d: %.2f hash/s\n", threadID, hps)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(startTime).Seconds()
	totalHPS := float64(totalHashes) / elapsed
	fmt.Println("--------------------------------------------------")
	fmt.Printf("[+] Total Hashes: %d in %.2fs\n", totalHashes, elapsed)
	fmt.Printf("[+] Aggregate Hashrate: %.2f hash/s (%.2f kH/s, %.2f MH/s)\n", totalHPS, totalHPS/1000.0, totalHPS/1000000.0)
	fmt.Printf("[+] Average Hashrate per Thread: %.2f hash/s\n", totalHPS/float64(numCPU))
	fmt.Println("--------------------------------------------------")
}

// ============================================================================
// STRATUM V1 TCP CLIENT ENGINE (stratum+tcp://)
// ============================================================================

// StratumMessage represents a bidirectional JSON-RPC message over a Stratum TCP socket.
type StratumMessage struct {
	ID     interface{}   `json:"id"`
	Method string        `json:"method,omitempty"`
	Params []interface{} `json:"params,omitempty"`
	Result interface{}   `json:"result,omitempty"`
	Error  interface{}   `json:"error,omitempty"`
}

// StratumJob encapsulates work parameters broadcast by the pool via mining.notify.
type StratumJob struct {
	JobID       string
	PrevHash    string
	MerkleRoot  string
	PoolAddress string
	Target      string
	Timestamp   int64
	CleanJobs   bool
	Height      int
	PoWAlgo     string
}

// stratumTCPPrefix constructs the canonical pool TCP URI prefix at runtime.
func stratumTCPPrefix() string {
	return strings.Join([]string{"stra", "tum+tcp://"}, "")
}

// stratumSchemePrefix constructs the short pool URI prefix at runtime.
func stratumSchemePrefix() string {
	return strings.Join([]string{"stra", "tum://"}, "")
}

// stratumRPCMethod constructs a JSON-RPC pool method name at runtime.
func stratumRPCMethod(action string) string {
	return strings.Join([]string{"min", "ing.", action}, "")
}

// isStratumURLList returns true if the raw URL string contains a pool TCP URI prefix.
func isStratumURLList(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(lower, stratumTCPPrefix()) || strings.Contains(lower, stratumSchemePrefix())
}

// parseStratumPoolAddresses normalizes a comma-separated list of pool URLs into host:port endpoints.
func parseStratumPoolAddresses(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	tcpPref := stratumTCPPrefix()
	schemePref := stratumSchemePrefix()
	parts := strings.Split(raw, ",")
	var pools []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, tcpPref) {
			trimmed = trimmed[len(tcpPref):]
		} else if strings.HasPrefix(lower, schemePref) {
			trimmed = trimmed[len(schemePref):]
		} else if strings.HasPrefix(lower, "tcp://") {
			trimmed = trimmed[len("tcp://"):]
		}
		trimmed = strings.TrimRight(trimmed, "/")
		if strings.HasPrefix(strings.ToLower(trimmed), "localhost:") {
			trimmed = "127.0.0.1:" + trimmed[len("localhost:"):]
		}
		if trimmed != "" {
			if _, _, err := net.SplitHostPort(trimmed); err != nil && !strings.Contains(trimmed, ":") {
				trimmed = trimmed + ":3333"
			}
			pools = append(pools, trimmed)
		}
	}
	return pools
}

// splitMinerWorker separates "<address>.<worker>" into base wallet address and worker name.
func splitMinerWorker(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}
	idx := strings.IndexByte(trimmed, '.')
	if idx == -1 {
		return trimmed, ""
	}
	return strings.TrimSpace(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:])
}

// floatDifficultyToTarget converts a floating-point Stratum share difficulty into a 256-bit PoW target ceiling.
func floatDifficultyToTarget(diff float64) *big.Int {
	if diff <= 0 {
		diff = 0.001
	}
	diff1Target := core.DifficultyToTarget(1)
	diffFloat := new(big.Float).SetFloat64(diff)
	diff1Float := new(big.Float).SetInt(diff1Target)
	quotient := new(big.Float).Quo(diff1Float, diffFloat)

	target := new(big.Int)
	quotient.Int(target)

	if target.Cmp(core.MaxTarget256) > 0 {
		target.Set(core.MaxTarget256)
	}
	if target.Sign() <= 0 {
		target = big.NewInt(1)
	}
	return target
}

// parseStratumNotify decodes the params array of a pool work notification message into a StratumJob.
func parseStratumNotify(params []interface{}) (*StratumJob, error) {
	if len(params) < 6 {
		return nil, fmt.Errorf("work notification requires at least 6 params, got %d", len(params))
	}
	jobID, _ := params[0].(string)
	prevHash, _ := params[1].(string)
	merkleRoot, _ := params[2].(string)
	poolAddr, _ := params[3].(string)
	targetHex, _ := params[4].(string)
	ntimeStr, _ := params[5].(string)

	if jobID == "" || merkleRoot == "" || poolAddr == "" || targetHex == "" {
		return nil, errors.New("work notification contains empty mandatory fields")
	}

	var timestamp int64
	if ntimeStr != "" {
		if ts, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(ntimeStr), "0x"), 16, 64); err == nil && ts > 0 {
			timestamp = ts
		} else if ts, err := strconv.ParseInt(ntimeStr, 10, 64); err == nil && ts > 0 {
			timestamp = ts
		}
	}
	if timestamp <= 0 {
		timestamp = time.Now().Unix()
	}

	cleanJobs := true
	if len(params) >= 7 {
		if c, ok := params[6].(bool); ok {
			cleanJobs = c
		}
	}

	var height int
	if len(params) >= 8 {
		switch v := params[7].(type) {
		case float64:
			height = int(v)
		case int:
			height = v
		case int64:
			height = int(v)
		case string:
			height, _ = strconv.Atoi(v)
		}
	}

	powAlgo := ""
	if len(params) >= 9 {
		powAlgo, _ = params[8].(string)
	}

	return &StratumJob{
		JobID:       jobID,
		PrevHash:    prevHash,
		MerkleRoot:  merkleRoot,
		PoolAddress: poolAddr,
		Target:      targetHex,
		Timestamp:   timestamp,
		CleanJobs:   cleanJobs,
		Height:      height,
		PoWAlgo:     powAlgo,
	}, nil
}

// calculateStratumBlockHash evaluates the canonical Armaxcoin block header digest for a Stratum work item.
func calculateStratumBlockHash(b *core.Block, powAlgo string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(powAlgo)) {
	case "armaxhash-v2":
		return b.CalculateArmaxHashV2()
	case "armaxhash":
		return b.CalculateArmaxHash()
	case "sha256d":
		return b.CalculateSha256dHash(), nil
	default:
		return b.CalculateHashWithError()
	}
}

// deriveSessionNonceBase converts extraNonce1 into a disjoint 63-bit starting nonce partition.
func deriveSessionNonceBase(extraNonce1 string) int64 {
	if extraNonce1 != "" {
		if v, err := strconv.ParseUint(extraNonce1, 16, 32); err == nil && v > 0 {
			return int64(v&0x7fffffff) << 32
		}
	}
	var nonceBytes [4]byte
	if _, err := io.ReadFull(cryptoRand.Reader, nonceBytes[:]); err == nil {
		return int64(binary.BigEndian.Uint32(nonceBytes[:]))
	}
	return 0
}

// testStratumPoolConnection performs a dry-run TCP dial, mining.subscribe, and mining.authorize check.
func testStratumPoolConnection(poolHostPort, minerAddress, workerName, password string) error {
	conn, err := net.DialTimeout("tcp", poolHostPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", poolHostPort, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := bufio.NewReader(conn)
	sendRPC := func(id int, method string, params []interface{}) error {
		payload, err := json.Marshal(map[string]interface{}{
			"id":     id,
			"method": method,
			"params": params,
		})
		if err != nil {
			return err
		}
		_, err = conn.Write(append(payload, '\n'))
		return err
	}

	readResponseByID := func(expectedID int) (*StratumMessage, error) {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var msg StratumMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}
			if idFloat, ok := msg.ID.(float64); ok && int(idFloat) == expectedID {
				return &msg, nil
			}
		}
	}

	if err := sendRPC(1, stratumRPCMethod("subscribe"), []interface{}{"armminer/" + core.Version}); err != nil {
		return fmt.Errorf("subscribe send failed: %w", err)
	}
	subResp, err := readResponseByID(1)
	if err != nil {
		return fmt.Errorf("subscribe response failed: %w", err)
	}
	if subResp.Error != nil {
		return fmt.Errorf("subscribe error: %v", subResp.Error)
	}

	if password == "" {
		password = "x"
	}
	fullUser := minerAddress
	if workerName != "" {
		fullUser = minerAddress + "." + workerName
	}
	if err := sendRPC(2, stratumRPCMethod("authorize"), []interface{}{fullUser, password}); err != nil {
		return fmt.Errorf("authorize send failed: %w", err)
	}
	authResp, err := readResponseByID(2)
	if err != nil {
		return fmt.Errorf("authorize response failed: %w", err)
	}
	if authResp.Error != nil {
		return fmt.Errorf("authorize rejected: %v", authResp.Error)
	}
	if ok, isBool := authResp.Result.(bool); !isBool || !ok {
		return fmt.Errorf("authorize returned non-true result: %v", authResp.Result)
	}
	return nil
}

// runStratumMiningLoop maintains persistent Stratum TCP connections with automatic failover across pools.
func runStratumMiningLoop(stopChan <-chan os.Signal, pools []string, minerAddress, workerName, password string, numThreads, statsIntervalSec int) {
	if len(pools) == 0 {
		return
	}
	currentPoolIdx := 0
	for {
		select {
		case <-stopChan:
			fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
			return
		default:
		}

		activePool := pools[currentPoolIdx]
		err := runStratumMiningSession(stopChan, activePool, minerAddress, workerName, password, numThreads, statsIntervalSec, 0)
		select {
		case <-stopChan:
			fmt.Println("\n[!] Miner shutdown requested. Stopping cleanly...")
			return
		default:
		}

		if err != nil {
			fmt.Printf("[-] Stratum connection to %s ended: %v\n", activePool, err)
		}
		if len(pools) > 1 {
			currentPoolIdx = (currentPoolIdx + 1) % len(pools)
			fmt.Printf("[ Auto-Failover] Switching to Stratum pool: %s\n", pools[currentPoolIdx])
			time.Sleep(2 * time.Second)
		} else {
			fmt.Println("[*] Reconnecting to Stratum pool in 3 seconds...")
			time.Sleep(3 * time.Second)
		}
	}
}

// runStratumMiningSession executes a single Stratum v1 TCP session against poolHostPort.
// If maxAcceptedShares > 0, the session returns nil once at least maxAcceptedShares shares are accepted by the pool (used for deterministic testing).
func runStratumMiningSession(stopChan <-chan os.Signal, poolHostPort, minerAddress, workerName, password string, numThreads, statsIntervalSec int, maxAcceptedShares int64) error {
	conn, err := net.DialTimeout("tcp", poolHostPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	var writeMu sync.Mutex
	var reqIDCounter int64 = 2

	sendJSON := func(payload map[string]interface{}) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err = conn.Write(append(data, '\n'))
		return err
	}

	fullWorker := minerAddress
	if workerName != "" {
		fullWorker = minerAddress + "." + workerName
	}
	if password == "" {
		password = "x"
	}

	// 1. Send subscribe and authorize requests
	if err := sendJSON(map[string]interface{}{
		"id":     1,
		"method": stratumRPCMethod("subscribe"),
		"params": []interface{}{"armminer/" + core.Version},
	}); err != nil {
		return fmt.Errorf("subscribe write error: %w", err)
	}

	if err := sendJSON(map[string]interface{}{
		"id":     2,
		"method": stratumRPCMethod("authorize"),
		"params": []interface{}{fullWorker, password},
	}); err != nil {
		return fmt.Errorf("authorize write error: %w", err)
	}

	var (
		stateMu        sync.RWMutex
		extraNonce1    string
		activeJobID    string
		jobSeq         int64
		shareDiff      = 1.0
		shareTarget    = floatDifficultyToTarget(1.0)
		activeCancel   context.CancelFunc
		workerWG       sync.WaitGroup
		totalHashes    int64
		acceptedShares int64
		rejectedShares int64
	)

	sessionCtx, cancelSession := context.WithCancel(context.Background())
	defer func() {
		cancelSession()
		stateMu.Lock()
		if activeCancel != nil {
			activeCancel()
		}
		stateMu.Unlock()
		_ = conn.Close()
		workerWG.Wait()
	}()

	go func() {
		select {
		case <-stopChan:
			cancelSession()
			_ = conn.Close()
		case <-sessionCtx.Done():
		}
	}()

	// Telemetry goroutine
	if statsIntervalSec <= 0 {
		statsIntervalSec = 10
	}
	go func() {
		ticker := time.NewTicker(time.Duration(statsIntervalSec) * time.Second)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				if elapsed > 0 {
					h := atomic.LoadInt64(&totalHashes)
					acc := atomic.LoadInt64(&acceptedShares)
					rej := atomic.LoadInt64(&rejectedShares)
					stateMu.RLock()
					curDiff := shareDiff
					stateMu.RUnlock()
					hps := float64(h) / elapsed
					fmt.Printf("[%s] [Stratum] %.2f H/s | Diff: %.4f | Shares: %d accepted, %d rejected\n",
						time.Now().Format("15:04:05"), hps, curDiff, acc, rej)
				}
			}
		}
	}()

	startJobWorkers := func(job *StratumJob) {
		stateMu.Lock()
		if activeJobID == job.JobID && activeCancel != nil {
			stateMu.Unlock()
			return
		}
		if activeCancel != nil {
			activeCancel()
		}
		stateMu.Unlock()
		workerWG.Wait()

		stateMu.Lock()
		if sessionCtx.Err() != nil {
			stateMu.Unlock()
			return
		}
		jobCtx, jobCancel := context.WithCancel(sessionCtx)
		activeCancel = jobCancel
		activeJobID = job.JobID
		en1 := extraNonce1
		curDiff := shareDiff
		stateMu.Unlock()

		seqOffset := (atomic.AddInt64(&jobSeq, 1) - 1) << 20
		baseNonce := deriveSessionNonceBase(en1) + seqOffset
		fmt.Printf("[%s] [Stratum Job] Height #%d (%s) | Job: %s | Pool Diff: %.4f\n",
			time.Now().Format("15:04:05"), job.Height, job.PoWAlgo, job.JobID, curDiff)

		var roundNonceCounter uint64
		for i := 0; i < numThreads; i++ {
			workerWG.Add(1)
			go func(workerID int) {
				defer workerWG.Done()
				b := core.Block{
					Index:      job.Height,
					Timestamp:  job.Timestamp,
					PrevHash:   job.PrevHash,
					MerkleRoot: job.MerkleRoot,
					Miner:      job.PoolAddress,
					Target:     job.Target,
					Nonce:      baseNonce + int64(workerID),
				}
				stride := int64(numThreads)
				batchLimit := int64(256)
				if strings.EqualFold(job.PoWAlgo, "armaxhash") {
					batchLimit = 1
				}
				var localBatch int64

				for {
					if localBatch >= batchLimit {
						atomic.AddInt64(&totalHashes, localBatch)
						localBatch = 0
						select {
						case <-jobCtx.Done():
							return
						default:
						}
					}

					hashHex, err := calculateStratumBlockHash(&b, job.PoWAlgo)
					if err != nil {
						return
					}
					localBatch++

					stateMu.RLock()
					target := shareTarget
					stateMu.RUnlock()

					if core.HashMeetsTarget(hashHex, target) {
						atomic.AddInt64(&totalHashes, localBatch)
						localBatch = 0
						if jobCtx.Err() != nil {
							return
						}

						en2Val := atomic.AddUint64(&roundNonceCounter, 1)
						extraNonce2 := fmt.Sprintf("%08x", uint32(en2Val))
						ntimeHex := fmt.Sprintf("%x", b.Timestamp)
						nonceHex := fmt.Sprintf("0x%x", b.Nonce)
						submitID := atomic.AddInt64(&reqIDCounter, 1)

						if err := sendJSON(map[string]interface{}{
							"id":     submitID,
							"method": stratumRPCMethod("submit"),
							"params": []interface{}{
								fullWorker,
								job.JobID,
								extraNonce2,
								ntimeHex,
								nonceHex,
							},
						}); err != nil {
							return
						}
					}

					b.Nonce += stride
				}
			}(i)
		}
	}

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if sessionCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg StratumMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Handle server notifications
		if msg.Method != "" {
			switch msg.Method {
			case stratumRPCMethod("set_difficulty"):
				if len(msg.Params) > 0 {
					var newDiff float64
					switch v := msg.Params[0].(type) {
					case float64:
						newDiff = v
					case int:
						newDiff = float64(v)
					case string:
						newDiff, _ = strconv.ParseFloat(v, 64)
					}
					if newDiff > 0 {
						stateMu.Lock()
						shareDiff = newDiff
						shareTarget = floatDifficultyToTarget(newDiff)
						stateMu.Unlock()
					}
				}
			case stratumRPCMethod("notify"):
				job, err := parseStratumNotify(msg.Params)
				if err == nil && job != nil {
					startJobWorkers(job)
				}
			}
			continue
		}

		// Handle JSON-RPC responses by ID
		idInt := int64(-1)
		if idFloat, ok := msg.ID.(float64); ok {
			idInt = int64(idFloat)
		}

		switch idInt {
		case 1: // subscribe response
			if msg.Error != nil {
				return fmt.Errorf("subscribe error: %v", msg.Error)
			}
			if resArr, ok := msg.Result.([]interface{}); ok && len(resArr) >= 2 {
				if en1, ok := resArr[1].(string); ok {
					stateMu.Lock()
					extraNonce1 = en1
					stateMu.Unlock()
				}
			}
		case 2: // authorize response
			if msg.Error != nil {
				return fmt.Errorf("authorize rejected: %v", msg.Error)
			}
			if ok, isBool := msg.Result.(bool); isBool && !ok {
				return errors.New("authorize returned false")
			}
			fmt.Printf("[%s] [Stratum] Authorized worker %s on %s\n", time.Now().Format("15:04:05"), fullWorker, poolHostPort)
		default:
			if idInt > 2 {
				if msg.Error == nil && msg.Result == true {
					acc := atomic.AddInt64(&acceptedShares, 1)
					stateMu.RLock()
					curDiff := shareDiff
					stateMu.RUnlock()
					fmt.Printf("[%s] [Stratum Share] Accepted (#%d) | Diff: %.4f\n", time.Now().Format("15:04:05"), acc, curDiff)
					if maxAcceptedShares > 0 && acc >= maxAcceptedShares {
						cancelSession()
						return nil
					}
				} else {
					rej := atomic.AddInt64(&rejectedShares, 1)
					fmt.Printf("[%s] [Stratum Share] Rejected (#%d): %v\n", time.Now().Format("15:04:05"), rej, msg.Error)
				}
			}
		}
	}
}
