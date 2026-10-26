//go:build !windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

var (
	rpcURL        = "http://127.0.0.1:" + core.TestnetRPCPort
	listenPort    = 18088
	activeWallet  *wallet.Wallet
	activeAddress string

	// Mining state
	miningActive       bool
	miningLock         sync.Mutex
	miningStopChan     chan struct{}
	miningStartTime    time.Time
	miningSessionBlks  int64
	miningSessionRwd   int64
	miningLiveHashrate uint64 // atomic bits of float64
	miningLogs         []string
	miningLogsLock     sync.Mutex
	benchmarkPerCore   float64
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

func calibrateBenchmark() float64 {
	testData := []byte("0:1700000000:0000000000000000:0000000000000000:armx1test:12345:0000ffff0000")
	start := time.Now()
	var count int64 = 0
	targetDuration := 30 * time.Millisecond
	for time.Since(start) < targetDuration {
		h1 := sha256.Sum256(testData)
		_ = sha256.Sum256(h1[:])
		count++
	}
	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		return float64(count) / elapsed
	}
	return 450_000.0
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}

func main() {
	portFlag := flag.Int("port", 18088, "HTTP GUI Wallet port")
	rpcFlag := flag.String("rpc", "http://127.0.0.1:"+core.TestnetRPCPort, "Armaxcoin node RPC URL")
	noBrowserFlag := flag.Bool("no-browser", false, "Do not automatically launch web browser")
	flag.Parse()

	listenPort = *portFlag
	rpcURL = strings.TrimRight(*rpcFlag, "/")

	benchmarkPerCore = calibrateBenchmark()

	// Initialize or load wallet
	loadOrInitWallet()

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", handleWebUI)
	serverMux.HandleFunc("/api/stats", handleStats)
	serverMux.HandleFunc("/api/peers", handlePeers)
	serverMux.HandleFunc("/api/chain", handleChain)
	serverMux.HandleFunc("/api/mempool", handleMempool)
	serverMux.HandleFunc("/api/cli", handleCLI)
	serverMux.HandleFunc("/api/balance", handleBalance)
	serverMux.HandleFunc("/api/history", handleHistory)
	serverMux.HandleFunc("/api/send", handleSend)
	serverMux.HandleFunc("/api/mine/start", handleMineStart)
	serverMux.HandleFunc("/api/mine/stop", handleMineStop)
	serverMux.HandleFunc("/api/mine/status", handleMineStatus)
	serverMux.HandleFunc("/api/addressbook", handleAddressBook)
	serverMux.HandleFunc("/api/wallet/info", handleWalletInfo)
	serverMux.HandleFunc("/api/wallet/restore", handleWalletRestore)
	serverMux.HandleFunc("/api/export-csv", handleExportCSV)

	addr := fmt.Sprintf("127.0.0.1:%d", listenPort)
	guiURL := fmt.Sprintf("http://127.0.0.1:%d", listenPort)

	fmt.Println("=================================================================")
	fmt.Printf("   ARMAXCOIN CORE - DESKTOP GUI WALLET (v%s - %s)\n", core.Version, strings.ToUpper(runtime.GOOS))
	fmt.Println("=================================================================")
	fmt.Printf("[*] Active Address: %s\n", activeAddress)
	fmt.Printf("[*] Connected Node RPC: %s\n", rpcURL)
	fmt.Printf("[*] GUI Wallet Interface: %s\n", guiURL)
	fmt.Println("=================================================================")

	// Wait for port listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to bind GUI port %d: %v\n", listenPort, err)
		os.Exit(1)
	}

	if !*noBrowserFlag {
		go func() {
			time.Sleep(300 * time.Millisecond)
			fmt.Println("[*] Launching system default browser...")
			openBrowser(guiURL)
		}()
	}

	server := &http.Server{Handler: serverMux}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "GUI Server Error: %v\n", err)
	}
}

func loadOrInitWallet() {
	walletFile := "wallet.json"
	if _, err := os.Stat(walletFile); err == nil {
		// Read address from file
		data, err := os.ReadFile(walletFile)
		if err == nil {
			var ks wallet.KeystoreFile
			if json.Unmarshal(data, &ks) == nil && ks.Address != "" {
				activeAddress = ks.Address
				return
			}
		}
	}

	// Generate default session wallet
	w, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("[-] Failed to create session wallet: %v\n", err)
		return
	}
	activeWallet = w
	activeAddress = w.Address
	// Encrypt and save with default empty passphrase if brand new
	_ = wallet.EncryptAndSaveKeystore(w, "", walletFile)
	w.ClearMnemonic()
}

func handleWalletInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"address": activeAddress,
		"version": core.Version,
		"network": core.ActiveNetwork.NetworkID,
		"crypto":  "ML-DSA-65 (FIPS 204)",
	})
}

func handleWalletRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
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

	restored, err := wallet.WalletFromMnemonic(mnemonic, "", 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to restore: %v", err), http.StatusBadRequest)
		return
	}

	if err := wallet.EncryptAndSaveKeystore(restored, req.Passphrase, "wallet.json"); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save wallet: %v", err), http.StatusInternalServerError)
		return
	}

	activeWallet = restored
	activeAddress = restored.Address
	restored.ClearMnemonic()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"address": activeAddress,
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
	case "getnetworkinfo", "getmempoolinfo", "getblockchaininfo":
		path = "/stats"
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/stats")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handlePeers(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/peers")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
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

func handleMempool(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(rpcURL + "/mempool")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handleBalance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		addr = activeAddress
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", rpcURL, addr))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		addr = activeAddress
	}
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/history?address=%s", rpcURL, addr))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
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
		http.Error(w, "Invalid ARMX amount", http.StatusBadRequest)
		return
	}

	var feeArmoids int64 = 0
	if req.Fee != "" {
		if f, err := core.ParseArmxDecimal(req.Fee); err == nil {
			feeArmoids = f
		}
	}

	// Load signing wallet
	var signingWallet *wallet.Wallet
	if activeWallet != nil && activeWallet.PrivateKey != nil {
		signingWallet = activeWallet
	} else {
		loadedW, err := wallet.DecryptAndLoadKeystore(req.Passphrase, "wallet.json")
		if err != nil {
			http.Error(w, fmt.Sprintf("Keystore passphrase decryption error: %v", err), http.StatusUnauthorized)
			return
		}
		signingWallet = loadedW
	}

	autoFee := wallet.EstimateMinMempoolFee(signingWallet, to, amtArmoids, 0)
	if feeArmoids < autoFee {
		feeArmoids = autoFee
	}

	// Fetch next nonce
	client := http.Client{Timeout: 5 * time.Second}
	bResp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", rpcURL, signingWallet.Address))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to query nonce: %v", err), http.StatusBadGateway)
		return
	}
	defer bResp.Body.Close()
	var bal struct {
		PendingNonce int64 `json:"pending_nonce"`
		NextNonce    int64 `json:"next_nonce"`
	}
	_ = json.NewDecoder(bResp.Body).Decode(&bal)

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
		http.Error(w, "Invalid miner address", http.StatusBadRequest)
		return
	}

	threads := req.Threads
	if threads < 1 {
		threads = runtime.NumCPU()
	}
	delay := req.Delay
	if delay < 100 {
		delay = 1000
	}

	miningLock.Lock()
	if miningActive {
		miningLock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}
	miningActive = true
	miningStopChan = make(chan struct{})
	miningStartTime = time.Now()
	miningLock.Unlock()

	appendMiningLog(fmt.Sprintf("[*] CPU Mining started! Threads: %d, Delay: %dms, Miner: %s", threads, delay, minerAddr))

	// Start hashrate poller
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		client := http.Client{Timeout: 800 * time.Millisecond}
		for {
			select {
			case <-miningStopChan:
				setMiningHps(0)
				return
			case <-ticker.C:
				var liveHps float64 = 0
				resp, err := client.Get(rpcURL + "/stats")
				if err == nil && resp.StatusCode == 200 {
					var stats struct {
						MiningHashrate float64 `json:"mining_hashrate"`
					}
					if json.NewDecoder(resp.Body).Decode(&stats) == nil {
						liveHps = stats.MiningHashrate
					}
					resp.Body.Close()
				}
				if liveHps <= 0 {
					liveHps = benchmarkPerCore * float64(threads)
				}
				setMiningHps(liveHps)
			}
		}
	}()

	// Start coordinated mining worker
	go func() {
		client := &http.Client{}
		mineURL := fmt.Sprintf("%s/mine", rpcURL)

		for {
			select {
			case <-miningStopChan:
				return
			default:
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"miner":   minerAddr,
				"threads": threads,
			})
			reqCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			req, err := http.NewRequestWithContext(reqCtx, "POST", mineURL, bytes.NewBuffer(payload))
			if err != nil {
				cancel()
				time.Sleep(1 * time.Second)
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			cancel()
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			var mResult struct {
				Status  string      `json:"status"`
				Message string      `json:"message"`
				Block   *core.Block `json:"block"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&mResult)
			resp.Body.Close()

			if resp.StatusCode == 200 && mResult.Status == "success" {
				atomic.AddInt64(&miningSessionBlks, 1)
				var rewardAmt int64 = 60 * core.CoinUnit
				blkIdx := 0
				if mResult.Block != nil {
					blkIdx = mResult.Block.Index
					if len(mResult.Block.Transactions) > 0 {
						rewardAmt = mResult.Block.Transactions[0].Amount
					}
				}
				atomic.AddInt64(&miningSessionRwd, rewardAmt)
				hashPrefix := ""
				if mResult.Block != nil && len(mResult.Block.Hash) > 16 {
					hashPrefix = " | hash: " + mResult.Block.Hash[:16] + "..."
				}
				appendMiningLog(fmt.Sprintf("[OK] BLOCK FOUND! #%d%s | Reward: %s credited",
					blkIdx, hashPrefix, core.FormatArmxDecimal(rewardAmt)))
			} else if mResult.Message != "" {
				appendMiningLog(fmt.Sprintf("[-] Mining notice (HTTP %d): %s", resp.StatusCode, mResult.Message))
			}

			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func handleMineStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	miningLock.Lock()
	if !miningActive {
		miningLock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_running"})
		return
	}
	miningActive = false
	if miningStopChan != nil {
		close(miningStopChan)
	}
	miningLock.Unlock()

	setMiningHps(0)
	appendMiningLog("[*] CPU Mining stopped by user.")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func handleMineStatus(w http.ResponseWriter, r *http.Request) {
	miningLock.Lock()
	active := miningActive
	st := miningStartTime
	miningLock.Unlock()

	elapsedSecs := 0
	if active {
		elapsedSecs = int(time.Since(st).Seconds())
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active":         active,
		"hashrate":       getMiningHps(),
		"elapsed_secs":   elapsedSecs,
		"session_blocks": atomic.LoadInt64(&miningSessionBlks),
		"session_reward": atomic.LoadInt64(&miningSessionRwd),
		"logs":           getMiningLogs(),
		"cpu_cores":      runtime.NumCPU(),
	})
}

func handleAddressBook(w http.ResponseWriter, r *http.Request) {
	bookFile := "addressbook.json"
	type Contact struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		DateAdded string `json:"date_added"`
	}

	if r.Method == http.MethodGet {
		var contacts []Contact
		data, err := os.ReadFile(bookFile)
		if err == nil {
			_ = json.Unmarshal(data, &contacts)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contacts)
		return
	}

	if r.Method == http.MethodPost {
		var contacts []Contact
		if err := json.NewDecoder(r.Body).Decode(&contacts); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		data, _ := json.MarshalIndent(contacts, "", "  ")
		_ = os.WriteFile(bookFile, data, 0600)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		addr = activeAddress
	}
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/history?address=%s", rpcURL, addr))
	if err != nil {
		http.Error(w, "Failed to query history", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var records []struct {
		BlockIndex int64            `json:"block_index"`
		Timestamp  int64            `json:"timestamp"`
		Tx         core.Transaction `json:"tx"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&records)

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment;filename=armax_transactions.csv")

	w.Write([]byte("Index,Type,Amount_ARMX,Height,TXID,Timestamp\n"))
	for i, r := range records {
		txType := "Received"
		if r.Tx.Sender == "COINBASE" {
			txType = "Mined (Coinbase)"
		} else if core.MatchAddress(r.Tx.Sender, addr) {
			txType = "Sent"
		}
		hStr := "Pending"
		if r.BlockIndex >= 0 {
			hStr = fmt.Sprintf("%d", r.BlockIndex)
		}
		tStr := time.Unix(r.Timestamp, 0).Format("2006-01-02 15:04:05")
		line := fmt.Sprintf("%d,%s,%s,%s,%s,%s\n",
			i+1, txType, core.FormatArmxDecimal(r.Tx.Amount), hStr, r.Tx.HashHex(), tStr)
		w.Write([]byte(line))
	}
}

// Embedded Web UI HTML/CSS/JavaScript
func handleWebUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webUIHTML))
}

const webUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Armaxcoin Core - Desktop GUI Wallet</title>
<style>
:root {
  --bg-dark: #0f172a;
  --bg-card: #1e293b;
  --bg-input: #090d16;
  --primary: #38bdf8;
  --primary-hover: #0284c7;
  --accent: #10b981;
  --danger: #ef4444;
  --text-main: #f8fafc;
  --text-muted: #94a3b8;
  --border: #334155;
  --radius: 8px;
}
* { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
body { background: var(--bg-dark); color: var(--text-main); min-height: 100vh; display: flex; flex-direction: column; }
header { background: #0b1120; border-bottom: 1px solid var(--border); padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; }
.logo-title { display: flex; align-items: center; gap: 12px; font-weight: 700; font-size: 18px; color: var(--primary); letter-spacing: 0.5px; }
.network-badge { background: #1e3a8a; color: #93c5fd; padding: 4px 10px; border-radius: 999px; font-size: 12px; font-weight: 600; }
.top-info { display: flex; align-items: center; gap: 20px; font-size: 13px; color: var(--text-muted); }
.top-info span b { color: var(--text-main); }

nav.tabs { display: flex; background: #131d31; border-bottom: 1px solid var(--border); padding: 0 16px; overflow-x: auto; }
.tab-btn { background: none; border: none; color: var(--text-muted); padding: 14px 18px; font-size: 14px; font-weight: 600; cursor: pointer; border-bottom: 3px solid transparent; display: flex; align-items: center; gap: 8px; transition: all 0.2s; white-space: nowrap; }
.tab-btn:hover { color: var(--text-main); background: rgba(255,255,255,0.02); }
.tab-btn.active { color: var(--primary); border-bottom-color: var(--primary); }

main.content { flex: 1; padding: 24px; max-width: 1200px; margin: 0 auto; width: 100%; }
.tab-pane { display: none; }
.tab-pane.active { display: block; animation: fadeIn 0.2s ease-in-out; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: translateY(0); } }

.card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; margin-bottom: 20px; }
.card-title { font-size: 15px; font-weight: 700; color: var(--primary); margin-bottom: 16px; display: flex; align-items: center; gap: 8px; }

.balance-big { font-size: 32px; font-weight: 800; color: var(--text-main); letter-spacing: -0.5px; }
.balance-sub { color: var(--text-muted); font-size: 13px; margin-top: 4px; }
.addr-box { display: flex; gap: 10px; margin-top: 14px; }
.addr-input { flex: 1; background: var(--bg-input); border: 1px solid var(--border); color: #38bdf8; font-family: "Consolas", monospace; font-size: 13px; padding: 10px 14px; border-radius: var(--radius); }

.grid-2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; }
.info-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid rgba(255,255,255,0.05); font-size: 13px; }
.info-row:last-child { border-bottom: none; }
.info-label { color: var(--text-muted); }
.info-val { font-weight: 600; color: var(--text-main); }

.form-group { margin-bottom: 16px; }
.form-label { display: block; font-size: 13px; font-weight: 600; color: var(--text-muted); margin-bottom: 6px; }
.form-control { width: 100%; background: var(--bg-input); border: 1px solid var(--border); color: var(--text-main); padding: 10px 14px; border-radius: var(--radius); font-size: 14px; outline: none; }
.form-control:focus { border-color: var(--primary); }

.btn { background: var(--primary); color: #02131e; border: none; padding: 10px 18px; border-radius: var(--radius); font-weight: 700; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; transition: background 0.2s; font-size: 13px; }
.btn:hover { background: var(--primary-hover); }
.btn-secondary { background: #334155; color: var(--text-main); }
.btn-secondary:hover { background: #475569; }
.btn-danger { background: var(--danger); color: #fff; }
.btn-danger:hover { background: #dc2626; }
.btn-outline { background: transparent; border: 1px solid var(--border); color: var(--text-main); }
.btn-outline:hover { background: rgba(255,255,255,0.05); border-color: var(--primary); }

.presets { display: flex; gap: 8px; margin-top: 8px; }
.preset-btn { background: #1e293b; border: 1px solid var(--border); color: var(--text-muted); padding: 6px 12px; border-radius: 6px; font-size: 12px; cursor: pointer; font-weight: 600; }
.preset-btn:hover { color: var(--primary); border-color: var(--primary); }

table.data-table { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
table.data-table th { background: #131d31; color: var(--text-muted); padding: 12px; font-weight: 600; border-bottom: 1px solid var(--border); }
table.data-table td { padding: 12px; border-bottom: 1px solid rgba(255,255,255,0.05); color: var(--text-main); }
table.data-table tbody tr { cursor: pointer; transition: background 0.15s; }
table.data-table tbody tr:hover { background: rgba(56, 189, 248, 0.08); }

.badge { display: inline-block; padding: 4px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; }
.badge-received { background: rgba(16, 185, 129, 0.15); color: #34d399; }
.badge-sent { background: rgba(239, 68, 68, 0.15); color: #f87171; }
.badge-mined { background: rgba(56, 189, 248, 0.15); color: #38bdf8; }

.modal-backdrop { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.7); display: none; align-items: center; justify-content: center; z-index: 1000; padding: 16px; }
.modal-backdrop.active { display: flex; }
.modal { background: var(--bg-card); border: 1px solid var(--border); border-radius: 10px; max-width: 600px; width: 100%; padding: 24px; box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.5); }
.modal-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; border-bottom: 1px solid var(--border); padding-bottom: 12px; }
.modal-title { font-size: 16px; font-weight: 700; color: var(--primary); }
.modal-close { background: none; border: none; color: var(--text-muted); font-size: 20px; cursor: pointer; }
.modal-body { font-size: 13px; line-height: 1.6; }

.log-box { background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius); padding: 12px; font-family: "Consolas", monospace; font-size: 12px; height: 180px; overflow-y: auto; color: #a5f3fc; line-height: 1.5; white-space: pre-wrap; }

footer.statusbar { background: #090d16; border-top: 1px solid var(--border); padding: 8px 24px; font-size: 12px; color: var(--text-muted); display: flex; justify-content: space-between; align-items: center; }
</style>
</head>
<body>

<header>
  <div class="logo-title">
    <span> ARMAXCOIN CORE</span>
    <span class="network-badge" id="lblNetworkBadge">TESTNET-1</span>
  </div>
  <div class="top-info">
    <span>Height: <b id="lblTopHeight">Syncing...</b></span>
    <span>Peers: <b id="lblTopPeers">0</b></span>
    <span>Diff: <b id="lblTopDiff">3</b></span>
    <span>Miner: <b id="lblTopMiner" style="color:#10b981">IDLE</b></span>
  </div>
</header>

<nav class="tabs">
  <button class="tab-btn active" onclick="switchTab('overview')"> Overview</button>
  <button class="tab-btn" onclick="switchTab('send')"> Send ARMX</button>
  <button class="tab-btn" onclick="switchTab('receive')"> Receive</button>
  <button class="tab-btn" onclick="switchTab('miner')"> CPU Miner</button>
  <button class="tab-btn" onclick="switchTab('transactions')"> Transactions</button>
  <button class="tab-btn" onclick="switchTab('addressbook')"> Address Book</button>
  <button class="tab-btn" onclick="switchTab('diagnostics')"> Diagnostics</button>
</nav>

<main class="content">

  <!-- TAB 1: OVERVIEW -->
  <section id="tab-overview" class="tab-pane active">
    <div class="card">
      <div class="card-title"> Wallet Balance & Primary Account</div>
      <div class="balance-big" id="lblBalanceMain">0.00000000 ARMX</div>
      <div class="balance-sub" id="lblBalanceSub">Spendable: 0.00000000 ARMX | Immature Rewards: 0.00000000 ARMX | Total: 0.00000000 ARMX</div>
      <div class="addr-box">
        <input type="text" class="addr-input" id="lblOverviewAddr" readonly value="Loading address...">
        <button class="btn btn-secondary" onclick="copyText(document.getElementById('lblOverviewAddr').value)"> Copy</button>
        <button class="btn btn-outline" onclick="openVisualCard()"> Visual Card</button>
      </div>
    </div>

    <div class="card">
      <div class="card-title"> Network & Consensus Profile</div>
      <div class="grid-2">
        <div>
          <div class="info-row"><span class="info-label">Blockchain Height:</span><span class="info-val" id="lblStatHeight">-</span></div>
          <div class="info-row"><span class="info-label">Active P2P Peers:</span><span class="info-val" id="lblStatPeers">-</span></div>
          <div class="info-row"><span class="info-label">LWMA-1 Target Difficulty:</span><span class="info-val" id="lblStatDiff">-</span></div>
        </div>
        <div>
          <div class="info-row"><span class="info-label">Unconfirmed Mempool:</span><span class="info-val" id="lblStatMempool">-</span></div>
          <div class="info-row"><span class="info-label">Post-Quantum Signature:</span><span class="info-val" style="color:#38bdf8">ML-DSA-65 (FIPS 204)</span></div>
          <div class="info-row"><span class="info-label">Block Time Target:</span><span class="info-val">120 seconds (N=60)</span></div>
        </div>
      </div>
    </div>
  </section>

  <!-- TAB 2: SEND -->
  <section id="tab-send" class="tab-pane">
    <div class="card">
      <div class="card-title"> Send ARMX (Post-Quantum Signed Transaction)</div>
      <div class="form-group">
        <label class="form-label">Recipient Address (Bech32 armx1...):</label>
        <div style="display:flex; gap:10px;">
          <input type="text" class="form-control" id="txtSendTo" placeholder="armx1...">
          <button class="btn btn-secondary" onclick="switchTab('addressbook')"> Contacts</button>
        </div>
      </div>

      <div class="grid-2">
        <div class="form-group">
          <label class="form-label">Amount (ARMX):</label>
          <input type="text" class="form-control" id="txtSendAmount" placeholder="0.00000000" oninput="updateSendTotal()">
        </div>
        <div class="form-group">
          <label class="form-label">Miner Fee (ARMX):</label>
          <input type="text" class="form-control" id="txtSendFee" value="0.00010000" oninput="updateSendTotal()">
          <div class="presets">
            <button class="preset-btn" onclick="setFee('0.00025000')"> Fast</button>
            <button class="preset-btn" onclick="setFee('0.00010000')"> Normal</button>
            <button class="preset-btn" onclick="setFee('0.00006000')"> Economy</button>
          </div>
        </div>
      </div>

      <div class="form-group">
        <label class="form-label">Keystore Passphrase (if encrypted):</label>
        <input type="password" class="form-control" id="txtSendPass" placeholder="Enter passphrase...">
      </div>

      <div style="display:flex; justify-content:space-between; align-items:center; margin-top:20px;">
        <span style="font-size:14px; color:var(--text-muted);">Estimated Total Outflow: <b id="lblSendTotal" style="color:var(--text-main)">0.00000000 ARMX</b></span>
        <button class="btn" onclick="executeSendTransaction()"> Sign & Broadcast Transaction</button>
      </div>

      <div id="sendResultBox" style="margin-top:16px; display:none; padding:12px; border-radius:6px; font-size:13px; font-family:monospace;"></div>
    </div>
  </section>

  <!-- TAB 3: RECEIVE -->
  <section id="tab-receive" class="tab-pane">
    <div class="card">
      <div class="card-title"> Receiving Address & QR URI</div>
      <p style="font-size:13px; color:var(--text-muted); margin-bottom:12px;">Share this Bech32 address to receive ARMX payments or mining payouts.</p>
      <div class="addr-box">
        <input type="text" class="addr-input" id="txtReceiveAddr" readonly value="">
        <button class="btn btn-secondary" onclick="copyText(document.getElementById('txtReceiveAddr').value)"> Copy</button>
        <button class="btn btn-outline" onclick="openVisualCard()"> Visual Card & URI</button>
      </div>
    </div>

    <div class="card">
      <div class="card-title"> Restore / Import Mnemonic Seed Phrase</div>
      <div class="form-group">
        <label class="form-label">BIP-39 Mnemonic (12 or 24 words):</label>
        <input type="text" class="form-control" id="txtRestoreMnemonic" placeholder="word1 word2 word3 ...">
      </div>
      <div class="form-group">
        <label class="form-label">New Keystore Password:</label>
        <input type="password" class="form-control" id="txtRestorePass" placeholder="Enter password to encrypt wallet...">
      </div>
      <button class="btn btn-secondary" onclick="executeRestoreWallet()"> Restore Wallet from Mnemonic</button>
    </div>
  </section>

  <!-- TAB 4: CPU MINER -->
  <section id="tab-miner" class="tab-pane">
    <div class="card">
      <div class="card-title"> Integrated CPU Proof-of-Work Miner</div>
      <div class="grid-2">
        <div class="form-group">
          <label class="form-label">Mining Reward Recipient:</label>
          <input type="text" class="form-control" id="txtMinerAddr" value="">
        </div>
        <div class="form-group">
          <label class="form-label">Threads (Cores):</label>
          <div style="display:flex; gap:8px;">
            <input type="number" class="form-control" id="txtMinerThreads" value="4" min="1" max="128">
            <button class="btn btn-secondary" id="btnMaxCores" onclick="setMaxCores()"> Max Cores</button>
          </div>
        </div>
      </div>

      <div style="display:flex; gap:12px; margin-top:8px;">
        <button class="btn" id="btnStartMine" onclick="startMining()">Start Mining</button>
        <button class="btn btn-danger" id="btnStopMine" onclick="stopMining()" disabled>Stop Mining</button>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Live PoW Hashrate & Mining Telemetry</div>
      <div class="grid-2">
        <div class="info-row"><span class="info-label">Current Mining Hashrate:</span><span class="info-val" id="lblLiveHashrate" style="color:#38bdf8; font-size:16px;">0.00 H/s (IDLE)</span></div>
        <div class="info-row"><span class="info-label">Active Mining Duration:</span><span class="info-val" id="lblLiveDuration">00:00:00</span></div>
        <div class="info-row"><span class="info-label">Blocks Mined This Session:</span><span class="info-val" id="lblSessionBlocks">0 blocks</span></div>
        <div class="info-row"><span class="info-label">Session Rewards Earned:</span><span class="info-val" id="lblSessionRewards">0.00000000 ARMX</span></div>
      </div>
    </div>

    <div class="card">
      <div class="card-title"> Miner Output Console</div>
      <div class="log-box" id="minerLogBox">[*] Ready to mine. Press Start Mining to begin SHA-256d PoW.</div>
    </div>
  </section>

  <!-- TAB 5: TRANSACTIONS -->
  <section id="tab-transactions" class="tab-pane">
    <div class="card">
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:16px;">
        <div class="card-title" style="margin-bottom:0"> Transaction Ledger & History</div>
        <div style="display:flex; gap:8px;">
          <button class="btn btn-secondary" onclick="exportCSV()"> Export CSV</button>
          <button class="btn btn-outline" onclick="loadHistory()"> Refresh</button>
        </div>
      </div>
      <p style="font-size:12px; color:var(--text-muted); margin-bottom:12px;"> <b>Tip:</b> Click or double-click any transaction row to inspect complete cryptographic details, nonce, fee, and copy raw TXID.</p>
      <div style="display:grid;grid-template-columns:minmax(180px,2fr) minmax(130px,1fr) minmax(130px,1fr) minmax(130px,1fr);gap:8px;margin-bottom:12px;">
        <input id="txSearch" class="form-control" placeholder="Search TXID or address" oninput="renderTransactionPage()">
        <select id="txTypeFilter" class="form-control" onchange="renderTransactionPage()"><option value="all">All types</option><option value="received">Received</option><option value="sent">Sent</option><option value="mined">Mined</option><option value="pending">Pending</option></select>
        <input id="txDateFrom" type="date" class="form-control" aria-label="From date" onchange="renderTransactionPage()">
        <input id="txDateTo" type="date" class="form-control" aria-label="To date" onchange="renderTransactionPage()">
      </div>

      <div style="overflow-x:auto;">
        <table class="data-table" id="txTable">
          <thead>
            <tr>
              <th>Type</th>
              <th>Amount (ARMX)</th>
              <th>Height</th>
              <th>TXID</th>
              <th>Timestamp</th>
            </tr>
          </thead>
          <tbody id="txTableBody">
            <tr><td colspan="5" style="text-align:center; color:var(--text-muted)">Loading transaction ledger...</td></tr>
          </tbody>
        </table>
      </div>
      <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:14px;flex-wrap:wrap;">
        <span id="txPageInfo" style="color:var(--text-muted);font-size:13px;">Page 1 / 1 -- 0 transactions</span>
        <div style="display:flex;gap:8px;">
          <button class="btn btn-outline" onclick="changeTxPage(-1)">Previous</button>
          <button class="btn btn-outline" onclick="changeTxPage(1)">Next</button>
          <select id="txPageSize" class="form-control" style="width:auto;" onchange="changeTxPageSize()">
            <option value="10">10 / page</option><option value="25" selected>25 / page</option><option value="50">50 / page</option><option value="100">100 / page</option>
          </select>
        </div>
      </div>
    </div>
  </section>

  <!-- TAB 6: ADDRESS BOOK -->
  <section id="tab-addressbook" class="tab-pane">
    <div class="card">
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:16px;">
        <div class="card-title" style="margin-bottom:0"> Contact Address Book</div>
        <button class="btn" onclick="openAddContactModal()"> Add Contact</button>
      </div>

      <div style="overflow-x:auto;">
        <table class="data-table">
          <thead>
            <tr>
              <th>Label / Contact Name</th>
              <th>Bech32 Address</th>
              <th>Date Added</th>
              <th style="text-align:right">Actions</th>
            </tr>
          </thead>
          <tbody id="contactsTableBody">
            <tr><td colspan="4" style="text-align:center; color:var(--text-muted)">No saved contacts yet.</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>

  <!-- TAB 7: DIAGNOSTICS -->
  <section id="tab-diagnostics" class="tab-pane">
    <div class="card">
      <div class="card-title"> Core Daemon Status & P2P Metrics</div>
      <div class="grid-2">
        <div>
          <div class="info-row"><span class="info-label">Daemon Connection:</span><span class="info-val" id="lblDiagStatus">Checking daemon connection...</span></div>
          <div class="info-row"><span class="info-label">Network Profile ID:</span><span class="info-val" id="lblDiagNetwork">TESTNET-1</span></div>
          <div class="info-row"><span class="info-label">Blockchain Tip Height:</span><span class="info-val" id="lblDiagHeight">-</span></div>
          <div class="info-row"><span class="info-label">Latest Block Hash:</span><span class="info-val" id="lblDiagHash" style="font-family:monospace;font-size:11px;">-</span></div>
          <div class="info-row"><span class="info-label">Connected P2P Mesh:</span><span class="info-val" id="lblDiagPeers">-</span></div>
        </div>
        <div>
          <div class="info-row"><span class="info-label">Mempool Pending Queue:</span><span class="info-val" id="lblDiagMempool">-</span></div>
          <div class="info-row"><span class="info-label">LWMA-1 Target Difficulty:</span><span class="info-val" id="lblDiagDiff">-</span></div>
          <div class="info-row"><span class="info-label">Circulating Supply:</span><span class="info-val" id="lblDiagSupply">-</span></div>
          <div class="info-row"><span class="info-label">P2P Security:</span><span class="info-val" style="color:#38bdf8">TLS 1.3 TOFU Pinning Enforced</span></div>
          <div class="info-row"><span class="info-label">Sybil / Eclipse Defense:</span><span class="info-val" style="color:#10b981">Subnet Limiter Active</span></div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-title"> Interactive RPC Command Console</div>
      <div style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap;">
        <button class="btn btn-secondary" onclick="loadDiagnostics('stats')">Full /stats</button>
        <button class="btn btn-secondary" onclick="loadDiagnostics('peers')">Active /peers</button>
        <button class="btn btn-secondary" onclick="loadDiagnostics('chain')">/chain Info</button>
        <button class="btn btn-secondary" onclick="loadDiagnostics('mempool')">/mempool Info</button>
        <button class="btn btn-outline" onclick="document.getElementById('diagConsole').innerText='Console output cleared.'">Clear Console</button>
      </div>
      <p style="color:var(--text-muted);font-size:13px;margin-bottom:12px;">Supported commands: help, getnetworkinfo, getblockchaininfo, getpeerinfo, getbalance [address], gethistory [address], getmempoolinfo, clear. Arbitrary shell commands are disabled.</p>
      <div style="display:flex;gap:8px;margin-bottom:12px;">
        <input id="cliCommand" class="form-control" placeholder="Enter command, e.g. getnetworkinfo" onkeydown="if(event.key==='Enter')runCLI()">
        <button class="btn" onclick="runCLI()">Run</button>
      </div>
      <pre class="log-box" id="diagConsole" style="height:320px; color:#f8fafc;">Click any action above or type a command to inspect node state.</pre>
    </div>
  </section>

</main>

<footer class="statusbar">
  <div>Armaxcoin Core Desktop Web GUI v1.0.4 | Post-Quantum ML-DSA-65</div>
  <div id="lblStatusbarSummary">Node: Connected | Ready</div>
</footer>

<!-- MODAL: TX DETAILS -->
<div class="modal-backdrop" id="txDetailModal">
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title"> Transaction Details</div>
      <button class="modal-close" onclick="closeModal('txDetailModal')">&times;</button>
    </div>
    <div class="modal-body" id="txDetailBody"></div>
    <div style="margin-top:20px; display:flex; justify-content:flex-end; gap:8px;">
      <button class="btn btn-secondary" onclick="copyTxDetailJSON()"> Copy Raw JSON</button>
      <button class="btn" onclick="closeModal('txDetailModal')">Close</button>
    </div>
  </div>
</div>

<!-- MODAL: VISUAL CARD -->
<div class="modal-backdrop" id="visualCardModal">
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title"> Visual Address Card & URI</div>
      <button class="modal-close" onclick="closeModal('visualCardModal')">&times;</button>
    </div>
    <div class="modal-body">
      <p style="color:var(--text-muted); font-size:12px; margin-bottom:12px;">Easy-to-read chunked format prevents transcription errors:</p>
      <div id="visualAddressChunks" style="font-family:monospace; font-size:18px; font-weight:700; background:#090d16; padding:16px; border-radius:8px; text-align:center; letter-spacing:2px; color:#38bdf8; word-break:break-all;"></div>
      <div style="margin-top:16px;">
        <label class="form-label">BIP-21 Payment URI:</label>
        <input type="text" class="form-control" id="txtBip21URI" readonly>
      </div>
    </div>
    <div style="margin-top:20px; display:flex; justify-content:flex-end; gap:8px;">
      <button class="btn btn-secondary" onclick="copyText(document.getElementById('txtBip21URI').value)"> Copy URI</button>
      <button class="btn" onclick="closeModal('visualCardModal')">Close</button>
    </div>
  </div>
</div>

<!-- MODAL: ADD CONTACT -->
<div class="modal-backdrop" id="addContactModal">
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title"> Add Contact to Address Book</div>
      <button class="modal-close" onclick="closeModal('addContactModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div class="form-group">
        <label class="form-label">Contact Name / Label:</label>
        <input type="text" class="form-control" id="txtNewContactName" placeholder="e.g. Alice Mining Rig">
      </div>
      <div class="form-group">
        <label class="form-label">Bech32 Address (armx1...):</label>
        <input type="text" class="form-control" id="txtNewContactAddress" placeholder="armx1...">
      </div>
    </div>
    <div style="margin-top:20px; display:flex; justify-content:flex-end; gap:8px;">
      <button class="btn btn-secondary" onclick="closeModal('addContactModal')">Cancel</button>
      <button class="btn" onclick="saveNewContact()"> Save Contact</button>
    </div>
  </div>
</div>

<script>
let currentAddress = "";
let currentTxRecords = [];
let filteredTxRecords = [];
let currentTxPage = 0;
let currentTxPageSize = 25;
let selectedTxRecord = null;
let contactsList = [];
let currentMaturity = 10;

function switchTab(name) {
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
  event.target.classList.add('active');
  const target = document.getElementById('tab-' + name);
  if (target) target.classList.add('active');
  if (name === 'transactions') loadHistory();
  if (name === 'addressbook') loadContacts();
}

function openModal(id) { document.getElementById(id).classList.add('active'); }
function closeModal(id) { document.getElementById(id).classList.remove('active'); }

function copyText(val) {
  navigator.clipboard.writeText(val);
  alert("Copied to clipboard: " + val);
}

function formatArmx(armoids) {
  const d = (armoids / 100000000).toFixed(8);
  return d + " ARMX";
}

function formatHashrate(hps) {
  if (hps >= 1000000000) return (hps/1000000000).toFixed(2) + " GH/s";
  if (hps >= 1000000) return (hps/1000000).toFixed(2) + " MH/s";
  if (hps >= 1000) return (hps/1000).toFixed(2) + " kH/s";
  return Math.round(hps) + " H/s";
}

let currentTopHeight = 0;

async function fetchStats() {
  try {
    const res = await fetch('/api/stats');
    if (!res.ok) return;
    const s = await res.json();
    currentTopHeight = s.height || 0;
    document.getElementById('lblTopHeight').innerText = s.height || 0;
    document.getElementById('lblStatHeight').innerText = (s.height || 0) + " (" + (s.blocks || 0) + " blocks)";
    const activePeers = Number(s.peers_active);
    const pCount = Number.isFinite(activePeers) ? Math.max(0, activePeers) : 0;
    document.getElementById('lblTopPeers').innerText = pCount;
    document.getElementById('lblStatPeers').innerText = pCount + " connected";
    document.getElementById('lblTopDiff').innerText = s.difficulty || 3;
    document.getElementById('lblStatDiff').innerText = "Difficulty " + (s.difficulty || 3) + " (LWMA-1)";
    document.getElementById('lblStatMempool').innerText = (s.mempool_size || s.mempool || 0) + " pending txs";
    if (s.network_id) document.getElementById('lblNetworkBadge').innerText = s.network_id;

    const diagStatus = document.getElementById('lblDiagStatus');
    if (diagStatus) diagStatus.innerText = "Online & Synced (" + (s.network_id || "TESTNET-1") + ")";
    const diagNet = document.getElementById('lblDiagNetwork');
    if (diagNet && s.network_id) diagNet.innerText = s.network_id;
    const diagH = document.getElementById('lblDiagHeight');
    if (diagH) diagH.innerText = (s.height || 0) + " (" + (s.blocks || 0) + " blocks)";
    const diagHash = document.getElementById('lblDiagHash');
    if (diagHash) diagHash.innerText = s.latest_hash || "-";
    const diagP = document.getElementById('lblDiagPeers');
    if (diagP) diagP.innerText = pCount + " active peers (known: " + (s.peers_known || 0) + ")";
    const diagM = document.getElementById('lblDiagMempool');
    if (diagM) diagM.innerText = (s.mempool_size || s.mempool || 0) + " unconfirmed txs";
    const diagD = document.getElementById('lblDiagDiff');
    if (diagD) diagD.innerText = "Difficulty " + (s.difficulty || 3) + " (LWMA-1 N=60)";
    const diagS = document.getElementById('lblDiagSupply');
    if (diagS && s.circulating_supply !== undefined) diagS.innerText = Number(s.circulating_supply).toFixed(8) + " ARMX";
  } catch(e) {
    const diagStatus = document.getElementById('lblDiagStatus');
    if (diagStatus) diagStatus.innerText = "Disconnected / Unreachable";
  }
}

async function fetchBalance() {
  if (!currentAddress) return;
  try {
    const res = await fetch('/api/balance?address=' + currentAddress);
    if (!res.ok) return;
    const b = await res.json();
    const confirmed = b.confirmed_balance || b.balance || 0;
    const pending = b.pending_balance || confirmed;
    const immature = b.immature_balance || 0;
    const spendable = b.spendable_balance !== undefined ? b.spendable_balance : pending;

    document.getElementById('lblBalanceMain').innerText = formatArmx(spendable);
    if (b.coinbase_maturity) {
      currentMaturity = b.coinbase_maturity;
    }
    const mat = currentMaturity;
    if (immature > 0) {
      document.getElementById('lblBalanceSub').innerText = 'Spendable: ' + formatArmx(spendable) + ' | Immature Rewards: ' + formatArmx(immature) + ' (<' + mat + ' blocks) | Total: ' + formatArmx(confirmed);
    } else if (pending === confirmed) {
      document.getElementById('lblBalanceSub').innerText = 'Confirmed: ' + formatArmx(confirmed) + ' (100% Settled) | Available: ' + formatArmx(pending);
    } else if (pending < confirmed) {
      const diff = confirmed - pending;
      document.getElementById('lblBalanceSub').innerText = 'Confirmed: ' + formatArmx(confirmed) + ' | Available: ' + formatArmx(pending) + ' (-' + formatArmx(diff) + ' in Mempool)';
    } else {
      const diff = pending - confirmed;
      document.getElementById('lblBalanceSub').innerText = 'Confirmed: ' + formatArmx(confirmed) + ' | Available: ' + formatArmx(pending) + ' (+' + formatArmx(diff) + ' Pending)';
    }
  } catch(e) {}
}

async function loadHistory() {
  if (!currentAddress) return;
  try {
    const res = await fetch('/api/history?address=' + encodeURIComponent(currentAddress));
    if (!res.ok) throw new Error('History request failed: HTTP ' + res.status);
    const records = await res.json();
    currentTxRecords = Array.isArray(records) ? records : [];
    renderTransactionPage();
  } catch(e) {
    document.getElementById('txTableBody').innerHTML = '<tr><td colspan="5" style="text-align:center;color:#f87171">Unable to load transaction history: ' + String(e.message).replace(/[<>&]/g, '') + '</td></tr>';
  }
}

function changeTxPage(delta) {
  const pages = Math.max(1, Math.ceil(filteredTxRecords.length / currentTxPageSize));
  currentTxPage = Math.max(0, Math.min(pages - 1, currentTxPage + delta));
  renderTransactionPage();
}

function changeTxPageSize() {
  currentTxPageSize = Math.max(1, parseInt(document.getElementById('txPageSize').value, 10) || 25);
  currentTxPage = 0;
  renderTransactionPage();
}

function renderTransactionPage() {
  const tbody = document.getElementById('txTableBody');
  const query = (document.getElementById('txSearch')?.value || '').trim().toLowerCase();
  const typeFilter = document.getElementById('txTypeFilter')?.value || 'all';
  const fromValue = document.getElementById('txDateFrom')?.value || '';
  const toValue = document.getElementById('txDateTo')?.value || '';
  const fromTime = fromValue ? new Date(fromValue + 'T00:00:00').getTime() / 1000 : null;
  const toTime = toValue ? new Date(toValue + 'T23:59:59').getTime() / 1000 : null;

  filteredTxRecords = currentTxRecords.filter(r => {
    const tx = r.tx || {};
    const sender = String(tx.sender || '');
    const recipient = String(tx.recipient || '');
    const txid = String(tx.hash || tx.txid || '');
    const isCoinbase = sender === 'COINBASE' || sender === 'SYSTEM';
    const isSent = sender.toLowerCase() === currentAddress.toLowerCase();
    const isPending = Number(r.block_index) < 0;
    const searchable = (txid + ' ' + sender + ' ' + recipient).toLowerCase();
    if (query && !searchable.includes(query)) return false;
    if (typeFilter === 'received' && (isCoinbase || isSent || recipient.toLowerCase() !== currentAddress.toLowerCase())) return false;
    if (typeFilter === 'sent' && !isSent) return false;
    if (typeFilter === 'mined' && !isCoinbase) return false;
    if (typeFilter === 'pending' && !isPending) return false;
    const ts = Number(r.timestamp || 0);
    if (fromTime !== null && ts < fromTime) return false;
    if (toTime !== null && ts > toTime) return false;
    return true;
  });

  if (filteredTxRecords.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:var(--text-muted)">No transactions match these filters.</td></tr>';
    document.getElementById('txPageInfo').innerText = 'Page 1 / 1 -- 0 matching transactions (' + currentTxRecords.length + ' total loaded)';
    currentTxPage = 0;
    return;
  }
  const pageCount = Math.max(1, Math.ceil(filteredTxRecords.length / currentTxPageSize));
  currentTxPage = Math.min(currentTxPage, pageCount - 1);
  const start = currentTxPage * currentTxPageSize;
  const rows = filteredTxRecords.slice(start, start + currentTxPageSize);
  let html = '';
  rows.forEach((r, idx) => {
    const tx = r.tx || {};
    const sender = String(tx.sender || '');
    const recipient = String(tx.recipient || '');
    const isCoinbase = sender === 'COINBASE' || sender === 'SYSTEM';
    let badge = '<span class="badge badge-received">Received</span>';
    if (isCoinbase) badge = '<span class="badge badge-mined">Mined (Coinbase)</span>';
    else if (sender.toLowerCase() === currentAddress.toLowerCase()) badge = '<span class="badge badge-sent">Sent</span>';
    const confs = r.block_index >= 0 && currentTopHeight >= r.block_index ? currentTopHeight - r.block_index + 1 : 0;
    let height = 'Pending';
    if (r.block_index >= 0) height = isCoinbase ? (confs < currentMaturity ? '#' + r.block_index + ' (' + confs + '/' + currentMaturity + ' blks)' : '#' + r.block_index + ' (Mature)') : (confs < 6 ? '#' + r.block_index + ' (' + confs + '/6 confs)' : confs < 12 ? '#' + r.block_index + ' (' + confs + ' confs)' : '#' + r.block_index + ' (Finalized)');
    const time = new Date(r.timestamp * 1000).toLocaleString();
    const txid = String(tx.hash || tx.txid || 'unknown');
    const short = txid.length > 20 ? txid.substring(0, 10) + '...' + txid.substring(txid.length - 8) : txid;
    html += '<tr onclick="showTxDetailModal(' + currentTxRecords.indexOf(r) + ')"><td>' + badge + '</td><td style="font-weight:700">' + formatArmx(tx.amount) + '</td><td>' + height + '</td><td style="font-family:monospace;color:#38bdf8">' + short + '</td><td style="color:var(--text-muted)">' + time + '</td></tr>';
  });
  tbody.innerHTML = html;
  document.getElementById('txPageInfo').innerText = 'Page ' + (currentTxPage + 1) + ' / ' + pageCount + ' -- ' + filteredTxRecords.length + ' matching / ' + currentTxRecords.length + ' total loaded';
}

async function runCLI() {
  const raw = document.getElementById('cliCommand').value.trim();
  if (!raw) return;
  const parts = raw.split(/\s+/);
  const command = (parts[0] || '').toLowerCase();
  const arg = parts.slice(1).join(' ');
  const out = document.getElementById('cliOutput');
  if (command === 'clear') { out.innerText = ''; return; }
  if (command === 'help') {
    out.innerText = 'Supported commands:\nhelp\ngetnetworkinfo\ngetblockchaininfo\ngetpeerinfo\ngetbalance [address]\ngethistory [address]\ngetmempoolinfo\nclear';
    return;
  }
  out.innerText = '$ ' + raw + '\nQuerying node...';
  try {
    const res = await fetch('/api/cli?command=' + encodeURIComponent(command) + '&arg=' + encodeURIComponent(arg));
    const body = await res.text();
    out.innerText = '$ ' + raw + '\nHTTP ' + res.status + '\n' + body;
  } catch (e) { out.innerText = 'CLI error: ' + e.message; }
}

function showTxDetailModal(idx) {
  const r = currentTxRecords[idx];
  if (!r) return;
  selectedTxRecord = r;
  const isPending = (r.block_index < 0);
  const isCoinbase = (r.tx.sender === 'COINBASE' || r.tx.sender === 'SYSTEM');
  let confs = 0;
  if (!isPending && currentTopHeight >= r.block_index) {
    confs = currentTopHeight - r.block_index + 1;
  }

  let statusStr = '[Pending] Unconfirmed in Mempool';
  if (!isPending) {
    if (isCoinbase) {
      statusStr = confs < currentMaturity ? '[Immature] Mining Reward -- ' + confs + '/' + currentMaturity + ' Confirmations (Locked until ' + currentMaturity + ' blocks)' : '[OK] Mined & Mature -- ' + confs + ' Confirmations (Fully Spendable)';
    } else {
      statusStr = confs < 6 ? '[Confirming] -- ' + confs + '/6 Confirmations (Wait 6 blocks)' : confs < 12 ? '[OK] Confirmed -- ' + confs + ' Confirmations (Safe for Standard Payments)' : '[Finalized] Securely Finalized -- ' + confs + ' Confirmations (Safe for High-Value Payments)';
    }
  }

  const txid = r.tx.hash || r.tx.txid || '';
  const tStr = new Date(r.timestamp * 1000).toLocaleString();

  let html = '<div class="info-row"><span class="info-label">Confirmation Status:</span><span class="info-val">' + statusStr + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Confirmations Count:</span><span class="info-val" style="font-weight:700">' + confs + ' confirmations</span></div>';
  html += '<div class="info-row"><span class="info-label">Transaction ID:</span><span class="info-val" style="font-family:monospace; word-break:break-all; color:#38bdf8">' + txid + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Sender:</span><span class="info-val" style="font-family:monospace; word-break:break-all;">' + r.tx.sender + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Recipient:</span><span class="info-val" style="font-family:monospace; word-break:break-all;">' + r.tx.recipient + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Amount:</span><span class="info-val" style="font-size:15px; font-weight:700">' + formatArmx(r.tx.amount) + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Miner Fee:</span><span class="info-val">' + formatArmx(r.tx.fee) + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Account Nonce:</span><span class="info-val">#' + (r.tx.nonce || 1) + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Timestamp:</span><span class="info-val">' + tStr + '</span></div>';
  html += '<div class="info-row"><span class="info-label">Post-Quantum Cryptography:</span><span class="info-val" style="color:#38bdf8">ML-DSA-65 (FIPS 204)</span></div>';

  document.getElementById('txDetailBody').innerHTML = html;
  openModal('txDetailModal');
}

function copyTxDetailJSON() {
  if (selectedTxRecord) {
    copyText(JSON.stringify(selectedTxRecord, null, 2));
  }
}

function openVisualCard() {
  if (!currentAddress) return;
  const chunks = [];
  for (let i = 0; i < currentAddress.length; i += 4) {
    chunks.push(currentAddress.substring(i, i + 4));
  }
  document.getElementById('visualAddressChunks').innerText = chunks.join('  ');
  document.getElementById('txtBip21URI').value = 'armax:' + currentAddress;
  openModal('visualCardModal');
}

function setFee(val) {
  document.getElementById('txtSendFee').value = val;
  updateSendTotal();
}

function updateSendTotal() {
  const amt = parseFloat(document.getElementById('txtSendAmount').value) || 0;
  const fee = parseFloat(document.getElementById('txtSendFee').value) || 0;
  document.getElementById('lblSendTotal').innerText = (amt + fee).toFixed(8) + " ARMX";
}

async function executeSendTransaction() {
  const to = document.getElementById('txtSendTo').value.trim();
  const amt = document.getElementById('txtSendAmount').value.trim();
  const fee = document.getElementById('txtSendFee').value.trim();
  const pass = document.getElementById('txtSendPass').value;
  const box = document.getElementById('sendResultBox');

  if (!to || !amt) {
    alert("Please enter recipient address and amount.");
    return;
  }
  if (!confirm("Confirm sending " + amt + " ARMX (Fee: " + fee + " ARMX) to " + to + "?")) return;

  box.style.display = 'block';
  box.style.background = '#1e293b';
  box.style.color = '#38bdf8';
  box.innerText = "Signing with post-quantum ML-DSA-65 and broadcasting...";

  try {
    const res = await fetch('/api/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ to, amount: amt, fee, passphrase: pass })
    });
    const data = await res.json();
    if (res.ok) {
      box.style.background = 'rgba(16, 185, 129, 0.15)';
      box.style.color = '#34d399';
      box.innerText = "OK Transaction Broadcasted Successfully!\nTXID: " + data.txid;
      document.getElementById('txtSendTo').value = '';
      document.getElementById('txtSendAmount').value = '';
      fetchBalance();
      loadHistory();
    } else {
      box.style.background = 'rgba(239, 68, 68, 0.15)';
      box.style.color = '#f87171';
      box.innerText = "[-] Broadcast Rejected: " + (data.error || JSON.stringify(data));
    }
  } catch(e) {
    box.style.background = 'rgba(239, 68, 68, 0.15)';
    box.style.color = '#f87171';
    box.innerText = "[-] Error: " + e.message;
  }
}

async function pollMiner() {
  try {
    const res = await fetch('/api/mine/status');
    if (!res.ok) return;
    const m = await res.json();
    document.getElementById('btnStartMine').disabled = m.active;
    document.getElementById('btnStopMine').disabled = !m.active;
    document.getElementById('lblTopMiner').innerText = m.active ? formatHashrate(m.hashrate) : 'IDLE';
    document.getElementById('lblTopMiner').style.color = m.active ? '#38bdf8' : '#10b981';

    document.getElementById('lblLiveHashrate').innerText = m.active ? formatHashrate(m.hashrate) : '0.00 H/s (IDLE)';
    const s = m.elapsed_secs || 0;
    const hrs = Math.floor(s / 3600).toString().padStart(2, '0');
    const mins = Math.floor((s % 3600) / 60).toString().padStart(2, '0');
    const secs = (s % 60).toString().padStart(2, '0');
    document.getElementById('lblLiveDuration').innerText = hrs + ':' + mins + ':' + secs;
    document.getElementById('lblSessionBlocks').innerText = (m.session_blocks || 0) + ' blocks';
    document.getElementById('lblSessionRewards').innerText = formatArmx(m.session_reward || 0);

    if (m.logs && m.logs.length > 0) {
      const logBox = document.getElementById('minerLogBox');
      logBox.innerText = m.logs.join('\n');
      logBox.scrollTop = logBox.scrollHeight;
    }
  } catch(e) {}
}

async function startMining() {
  const addr = document.getElementById('txtMinerAddr').value.trim() || currentAddress;
  const threads = parseInt(document.getElementById('txtMinerThreads').value) || 4;
  await fetch('/api/mine/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ miner: addr, threads: threads, delay: 1000 })
  });
  pollMiner();
}

async function stopMining() {
  await fetch('/api/mine/stop', { method: 'POST' });
  pollMiner();
}

function setMaxCores() {
  fetch('/api/mine/status').then(r => r.json()).then(d => {
    if (d.cpu_cores) document.getElementById('txtMinerThreads').value = d.cpu_cores;
  });
}

async function loadContacts() {
  try {
    const res = await fetch('/api/addressbook');
    contactsList = await res.json();
    const tbody = document.getElementById('contactsTableBody');
    if (!contactsList || contactsList.length === 0) {
      tbody.innerHTML = '<tr><td colspan="4" style="text-align:center; color:var(--text-muted)">No saved contacts yet. Click "Add Contact" to save one.</td></tr>';
      return;
    }
    let html = '';
    contactsList.forEach((c, idx) => {
      html += '<tr>';
      html += '<td style="font-weight:600">' + c.name + '</td>';
      html += '<td style="font-family:monospace; color:#38bdf8">' + c.address + '</td>';
      html += '<td style="color:var(--text-muted)">' + (c.date_added || '-') + '</td>';
      html += '<td style="text-align:right">';
      html += '<button class="btn btn-secondary" style="padding:4px 8px; margin-right:6px;" onclick="sendToContact(\'' + c.address + '\')"> Send</button>';
      html += '<button class="btn btn-outline" style="padding:4px 8px; margin-right:6px;" onclick="copyText(\'' + c.address + '\')">Copy</button>';
      html += '<button class="btn btn-danger" style="padding:4px 8px;" onclick="deleteContact(' + idx + ')">Delete</button>';
      html += '</td>';
      html += '</tr>';
    });
    tbody.innerHTML = html;
  } catch(e) {}
}

function openAddContactModal() {
  document.getElementById('txtNewContactName').value = '';
  document.getElementById('txtNewContactAddress').value = '';
  openModal('addContactModal');
}

async function saveNewContact() {
  const name = document.getElementById('txtNewContactName').value.trim();
  const address = document.getElementById('txtNewContactAddress').value.trim();
  if (!name || !address) {
    alert("Please fill name and address.");
    return;
  }
  const dateStr = new Date().toISOString().split('T')[0];
  contactsList.push({ name, address, date_added: dateStr });
  await fetch('/api/addressbook', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(contactsList)
  });
  closeModal('addContactModal');
  loadContacts();
}

async function deleteContact(idx) {
  if (!confirm("Delete contact " + contactsList[idx].name + "?")) return;
  contactsList.splice(idx, 1);
  await fetch('/api/addressbook', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(contactsList)
  });
  loadContacts();
}

function sendToContact(addr) {
  document.getElementById('txtSendTo').value = addr;
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
  document.querySelectorAll('.tab-btn')[1].classList.add('active');
  document.getElementById('tab-send').classList.add('active');
}

function exportCSV() {
  window.location.href = '/api/export-csv?address=' + currentAddress;
}

async function loadDiagnostics(action) {
  const c = document.getElementById('diagConsole');
  action = action || 'stats';
  c.innerText = "Querying node /" + action + "...";
  try {
    let url = '/api/stats';
    if (action === 'peers') url = '/api/peers';
    else if (action === 'chain') url = '/api/chain';
    else if (action === 'mempool') url = '/api/mempool';

    const res = await fetch(url);
    const data = await res.json();
    c.innerText = "=== NODE /" + action.toUpperCase() + " (" + new Date().toLocaleTimeString() + ") ===\n" + JSON.stringify(data, null, 2);
  } catch(e) {
    c.innerText = "Error querying /" + action + ": " + e.message;
  }
}

async function runCLI() {
  const input = document.getElementById('cliCommand');
  const consoleBox = document.getElementById('diagConsole');
  if (!input || !consoleBox) return;
  const raw = (input.value || '').trim();
  if (!raw) return;

  if (raw.toLowerCase() === 'clear') {
    consoleBox.innerText = 'Console output cleared.';
    input.value = '';
    return;
  }

  if (raw.toLowerCase() === 'help') {
    consoleBox.innerText = "Armaxcoin Core CLI Commands:\n" +
      "  help                     - List supported CLI commands\n" +
      "  getnetworkinfo           - P2P network, protocol, active/known peer metrics\n" +
      "  getblockchaininfo        - Height, block count, difficulty, tip hash\n" +
      "  getpeerinfo              - Detailed connected peer mesh list & TLS status\n" +
      "  getmempoolinfo           - Unconfirmed transactions pending in queue\n" +
      "  getbalance [address]     - Query spendable & immature balance for address\n" +
      "  gethistory [address]     - Retrieve on-chain transaction history\n" +
      "  clear                    - Clear console screen";
    input.value = '';
    return;
  }

  const parts = raw.split(/\s+/);
  const cmd = parts[0].toLowerCase();
  const arg = parts.slice(1).join(' ');

  consoleBox.innerText = "$ " + raw + "\nExecuting query...";
  try {
    const res = await fetch('/api/cli?command=' + encodeURIComponent(cmd) + '&arg=' + encodeURIComponent(arg));
    const text = await res.text();
    let formatted = text;
    try {
      formatted = JSON.stringify(JSON.parse(text), null, 2);
    } catch (_) {}
    consoleBox.innerText = "$ " + raw + "\nHTTP " + res.status + "\n" + formatted;
  } catch (err) {
    consoleBox.innerText = "$ " + raw + "\nRequest failed: " + err.message;
  }
}

// Initial initialization
(async function init() {
  try {
    const res = await fetch('/api/wallet/info');
    const info = await res.json();
    currentAddress = info.address;
    document.getElementById('lblOverviewAddr').value = currentAddress;
    document.getElementById('txtReceiveAddr').value = currentAddress;
    document.getElementById('txtMinerAddr').value = currentAddress;
  } catch(e) {}

  fetchStats();
  fetchBalance();
  loadHistory();
  loadContacts();
  pollMiner();

  setInterval(() => {
    fetchStats();
    fetchBalance();
    pollMiner();
  }, 2500);
})();
</script>
</body>
</html>
`
