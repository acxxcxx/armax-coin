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
	"github.com/acxxcxx/armax-coin/wallet"
)

const (
	DefaultWalletFile = "wallet.json"
	PassphraseEnvVar  = "ARMX_WALLET_PASSPHRASE"
)

func main() {
	networkPtr := flag.String("network", "testnet", "Network profile (testnet, mainnet) [L1-3, L4-1]")
	createPtr := flag.Bool("create", false, "Create a new quantum-safe wallet with 24-word recovery mnemonic")
	newAddressPtr := flag.Bool("newaddress", false, "Create a new quantum-safe address/wallet (alias for -create)")
	restorePtr := flag.String("restore", "", "Restore wallet from BIP-39 mnemonic phrase (24 or 12 words)")
	balancePtr := flag.String("balance", "", "Check balance of a specific wallet address")
	getBalancePtr := flag.String("getbalance", "", "Check balance of a specific wallet address (alias for -balance)")
	addressPtr := flag.Bool("address", false, "Display address from encrypted keystore file")
	listAddressesPtr := flag.Bool("listaddresses", false, "List all addresses available in keystore file")
	backupPtr := flag.String("backup", "", "Safely backup wallet keystore file to specified destination path")
	walletCheckPtr := flag.Bool("walletcheck", false, "Verify that wallet keystore can be opened, decrypted, and is cryptographically consistent")
	rescanPtr := flag.Bool("rescan", false, "Rescan blockchain transactions for active wallet address")
	getTxPtr := flag.String("gettransaction", "", "Display detailed transaction information for a given TXID")
	mempoolPtr := flag.Bool("mempool", false, "Display unconfirmed transactions waiting in the daemon mempool")
	getMempoolPtr := flag.Bool("getmempool", false, "Display unconfirmed transactions waiting in the daemon mempool (alias for -mempool)")
	getMempoolInfoPtr := flag.Bool("getmempoolinfo", false, "Display unconfirmed transactions waiting in the daemon mempool (alias for -mempool)")
	filePtr := flag.String("file", DefaultWalletFile, "Wallet keystore file path (default: wallet.json)")
	walletPtr := flag.String("wallet", "", "Wallet keystore file path (alias for -file)")
	daemonPtr := flag.String("daemon", "", "RPC daemon URL for balance queries (default: profile port)")
	rpcUserPtr := flag.String("rpcuser", "", "Username for RPC authentication")
	rpcPasswordPtr := flag.String("rpcpassword", "", "Password for RPC authentication")
	portPtr := flag.String("port", "", "RPC Daemon port (deprecated: use -rpcport)")
	rpcPortPtr := flag.String("rpcport", "", "RPC Daemon port (overrides default)")
	p2pPortPtr := flag.String("p2p", "", "P2P port (deprecated: use -p2pport)")
	p2pPortNewPtr := flag.String("p2pport", "", "P2P port (overrides default)")
	dataDirPtr := flag.String("datadir", "", "Custom data directory")
	infoPtr := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionPtr := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	flag.Parse()

	if *versionPtr {
		fmt.Printf("armwallet v%s\n", core.Version)
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

	targetWalletPath := *filePtr
	if *walletPtr != "" {
		targetWalletPath = *walletPtr
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

	daemonURL := *daemonPtr
	if daemonURL == "" {
		if effRPCPort != "" {
			daemonURL = "http://127.0.0.1:" + effRPCPort
		} else {
			daemonURL = cliNet.DaemonURL
		}
	}

	fmt.Println("==================================================")
	fmt.Println("WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited")
	fmt.Println(" ARMAXCOIN (ARMX) - POST-QUANTUM CLI WALLET (ML-DSA-65)")
	fmt.Println("==================================================")
	fmt.Printf("[+] Target Network   : %s (%s, RPC: %s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID, activeNet.RPCPort)

	if *infoPtr {
		fmt.Printf("[+] Network Profile  : %s (%s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID)
		fmt.Printf("[+] RPC Port         : %s\n", cliNet.RPCPort)
		fmt.Printf("[+] P2P Port         : %s\n", cliNet.P2PPort)
		fmt.Printf("[+] Data Directory   : %s\n", cliNet.DataDir)
		for _, seed := range cliNet.DefaultSeeds {
			fmt.Printf("[+] Seed Peer        : %s\n", seed)
		}
		return
	}

	// Helper for authenticated HTTP GET requests
	httpGetAuth := func(endpoint string) (*http.Response, error) {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		if *rpcUserPtr != "" || *rpcPasswordPtr != "" {
			req.SetBasicAuth(*rpcUserPtr, *rpcPasswordPtr)
		}
		return client.Do(req)
	}

	if *createPtr || *newAddressPtr {
		createWallet(targetWalletPath)
	} else if *restorePtr != "" {
		restoreWallet(*restorePtr, targetWalletPath)
	} else if *balancePtr != "" || *getBalancePtr != "" {
		addr := *balancePtr
		if addr == "" {
			addr = *getBalancePtr
		}
		checkBalance(addr, daemonURL, httpGetAuth)
	} else if *addressPtr || *listAddressesPtr {
		showAddresses(targetWalletPath)
	} else if *backupPtr != "" {
		backupWallet(targetWalletPath, *backupPtr)
	} else if *walletCheckPtr {
		walletCheck(targetWalletPath)
	} else if *getTxPtr != "" {
		showTransaction(*getTxPtr, daemonURL, httpGetAuth)
	} else if *rescanPtr {
		rescanWallet(targetWalletPath, daemonURL, httpGetAuth)
	} else if *mempoolPtr || *getMempoolPtr || *getMempoolInfoPtr {
		showMempool(daemonURL, httpGetAuth)
	} else {
		fmt.Println("Usage:")
		fmt.Println("  ./armwallet -create [-wallet <FILE>]                            (Create new wallet with 24-word mnemonic)")
		fmt.Println("  ./armwallet -newaddress [-wallet <FILE>]                        (Create new wallet address)")
		fmt.Println("  ./armwallet -restore \"WORDS\" [-wallet <FILE>]                   (Restore wallet from mnemonic)")
		fmt.Println("  ./armwallet -address [-wallet <FILE>]                           (Display address from keystore)")
		fmt.Println("  ./armwallet -listaddresses [-wallet <FILE>]                     (List addresses in keystore)")
		fmt.Println("  ./armwallet -balance <ADDRESS> [-daemon <URL>]                  (Check wallet balance via daemon)")
		fmt.Println("  ./armwallet -getbalance <ADDRESS> [-daemon <URL>]               (Check wallet balance via daemon)")
		fmt.Println("  ./armwallet -mempool [-daemon <URL>]                            (Display unconfirmed transactions in mempool)")
		fmt.Println("  ./armwallet -gettransaction <TXID> [-daemon <URL>]              (View transaction details)")
		fmt.Println("  ./armwallet -backup <DEST_FILE> [-wallet <FILE>]                (Safely backup wallet keystore)")
		fmt.Println("  ./armwallet -walletcheck [-wallet <FILE>]                       (Verify keystore decryption and integrity)")
		fmt.Println("  ./armwallet -rescan [-wallet <FILE>]                            (Rescan transaction history)")
	}
}

func getPassphrase(confirm bool) (string, error) {
	pass, err := wallet.PromptPassphrase(PassphraseEnvVar, "Enter keystore passphrase: ")
	if err != nil {
		return "", err
	}
	if confirm && os.Getenv(PassphraseEnvVar) == "" {
		pass2, err := wallet.PromptPassphrase("", "Confirm keystore passphrase: ")
		if err != nil {
			return "", err
		}
		if pass != pass2 {
			return "", fmt.Errorf("passphrases do not match")
		}
	}
	return pass, nil
}

func createWallet(filePath string) {
	passphrase, err := getPassphrase(true)
	if err != nil {
		fmt.Println("[-] Error:", err)
		return
	}

	w, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("[-] Failed to create wallet: %v\n", err)
		return
	}

	if err := wallet.EncryptAndSaveKeystore(w, passphrase, filePath); err != nil {
		fmt.Println("[-] Failed to save encrypted keystore:", err)
		return
	}

	fmt.Println("\n[OK] Post-quantum wallet successfully created and secured with Argon2id + AES-256-GCM!")
	fmt.Printf("    Wallet Address     : %s\n", w.Address)
	fmt.Printf("    Public Key (Hex)   : %s...\n", w.PublicKey[:40])
	fmt.Println("--------------------------------------------------")
	fmt.Println(" 24-WORD BIP-39 RECOVERY PHRASE (DISPLAYED ONCE - SAVE SECURELY):")
	fmt.Printf("   \"%s\"\n", w.Mnemonic)
	fmt.Println("   [!] Write down these 24 words on paper and store offline! Never share them!")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("    Saved to           : %s (mode: 0600)\n", filePath)
}

func restoreWallet(mnemonic, filePath string) {
	passphrase, err := getPassphrase(true)
	if err != nil {
		fmt.Println("[-] Error:", err)
		return
	}

	w, err := wallet.WalletFromMnemonic(mnemonic, "", 0)
	if err != nil {
		fmt.Printf("[-] Failed to restore wallet from mnemonic: %v\n", err)
		return
	}

	if err := wallet.EncryptAndSaveKeystore(w, passphrase, filePath); err != nil {
		fmt.Println("[-] Failed to save encrypted keystore:", err)
		return
	}

	fmt.Println("\n[OK] Wallet successfully restored and encrypted with Argon2id!")
	fmt.Printf("    Restored Address   : %s\n", w.Address)
	fmt.Printf("    Saved to           : %s\n", filePath)
}

func showAddress(filePath string) {
	showAddresses(filePath)
}

func showAddresses(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("[-] Cannot read keystore file '%s': %v\n", filePath, err)
		return
	}

	var ks wallet.KeystoreFile
	if err := json.Unmarshal(data, &ks); err != nil {
		fmt.Println("[-] Invalid keystore file format:", err)
		return
	}

	fmt.Println("==================================================")
	fmt.Println(" WALLET KEYSTORE ADDRESSES:")
	fmt.Println("==================================================")
	fmt.Printf("  Address #1 (Active) : %s\n", ks.Address)
	if len(ks.PublicKey) >= 48 {
		fmt.Printf("  Public Key (ML-DSA) : %s...\n", ks.PublicKey[:48])
	}
	fmt.Printf("  Keystore File       : %s\n", filePath)
	fmt.Println("==================================================")
}

func backupWallet(sourcePath, destPath string) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		fmt.Printf("[-] Failed to read wallet keystore '%s': %v\n", sourcePath, err)
		os.Exit(1)
	}

	// Validate source is valid keystore JSON without exposing secrets
	var ks wallet.KeystoreFile
	if err := json.Unmarshal(data, &ks); err != nil || ks.Address == "" {
		fmt.Printf("[-] Refusing to backup corrupted keystore file '%s'\n", sourcePath)
		os.Exit(1)
	}

	destDir := filepath.Dir(destPath)
	if destDir != "" && destDir != "." {
		if err := os.MkdirAll(destDir, 0700); err != nil {
			fmt.Printf("[-] Cannot create target backup directory '%s': %v\n", destDir, err)
			os.Exit(1)
		}
	}

	// Commit via a synced temporary file: a backup must never replace an
	// existing good copy with partial ciphertext after an interruption.
	tmp, err := os.CreateTemp(destDir, ".wallet-backup-*.tmp")
	if err != nil {
		fmt.Printf("[-] Backup failed: could not create temporary file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		fmt.Printf("[-] Backup failed: could not secure temporary file: %v\n", err)
		os.Exit(1)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		fmt.Printf("[-] Backup failed: could not write temporary file: %v\n", err)
		os.Exit(1)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		fmt.Printf("[-] Backup failed: could not sync temporary file: %v\n", err)
		os.Exit(1)
	}
	if err := tmp.Close(); err != nil {
		fmt.Printf("[-] Backup failed: could not close temporary file: %v\n", err)
		os.Exit(1)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		fmt.Printf("[-] Backup failed: could not write to destination file '%s': %v\n", destPath, err)
		os.Exit(1)
	}
	if err := os.Chmod(destPath, 0600); err != nil {
		fmt.Printf("[-] Backup failed: could not secure destination file '%s': %v\n", destPath, err)
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Println(" WALLET BACKUP SUCCESSFUL:")
	fmt.Println("==================================================")
	fmt.Printf("  Source Keystore     : %s\n", sourcePath)
	fmt.Printf("  Target Destination  : %s (mode: 0600)\n", destPath)
	fmt.Printf("  Wallet Address      : %s\n", ks.Address)
	fmt.Println("  [OK] Encrypted wallet backup completed securely.")
	fmt.Println("==================================================")
}

func walletCheck(filePath string) {
	passphrase, err := wallet.PromptPassphrase(PassphraseEnvVar, "Enter keystore passphrase to verify integrity: ")
	if err != nil {
		fmt.Println("[-] Passphrase prompt error:", err)
		os.Exit(1)
	}

	w, err := wallet.DecryptAndLoadKeystore(passphrase, filePath)
	if err != nil {
		fmt.Println("==================================================")
		fmt.Printf("[-] WALLET INTEGRITY CHECK FAILED: %v\n", err)
		fmt.Println("==================================================")
		os.Exit(1)
	}

	if !core.ValidateAddress(w.Address) {
		fmt.Println("[-] Wallet integrity check failed: stored address is not valid Bech32")
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Println(" WALLET INTEGRITY CHECK PASSED:")
	fmt.Println("==================================================")
	fmt.Printf("  Wallet File         : %s\n", filePath)
	fmt.Printf("  Wallet Address      : %s\n", w.Address)
	fmt.Println("  Key Scheme          : ML-DSA-65 (NIST FIPS 204 Post-Quantum)")
	fmt.Println("  Encryption          : Argon2id + AES-256-GCM (Authenticated)")
	fmt.Println("  Status              : Healthy and consistent. Ready for transactions.")
	fmt.Println("==================================================")
}

func showTransaction(txid string, daemonURL string, httpGet func(string) (*http.Response, error)) {
	cleanTxid := strings.TrimSpace(txid)
	fmt.Printf("[~] Querying transaction %s from daemon %s...\n", cleanTxid, daemonURL)
	resp, err := httpGet(fmt.Sprintf("%s/tx?hash=%s", daemonURL, cleanTxid))
	if err != nil {
		fmt.Printf("[-] Failed to connect to daemon: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("[-] Transaction '%s' not found (HTTP %d)\n", cleanTxid, resp.StatusCode)
		return
	}

	var res struct {
		Status        string           `json:"status"`
		BlockIndex    int              `json:"block_index"`
		BlockHash     string           `json:"block_hash"`
		Confirmations int              `json:"confirmations"`
		Timestamp     int64            `json:"timestamp"`
		Tx            core.Transaction `json:"tx"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		fmt.Printf("[OK] Raw Transaction Data:\n%s\n", string(body))
		return
	}

	fmt.Println("==================================================")
	fmt.Println(" ARMAX TRANSACTION DETAILS:")
	fmt.Println("==================================================")
	fmt.Printf("  TXID / Hash    : %s\n", cleanTxid)
	fmt.Printf("  Status         : %s (%d confirmations)\n", strings.ToUpper(res.Status), res.Confirmations)
	fmt.Printf("  Block Index    : #%d (Hash: %s)\n", res.BlockIndex, res.BlockHash)
	fmt.Printf("  Sender         : %s\n", res.Tx.Sender)
	fmt.Printf("  Recipient      : %s\n", res.Tx.Recipient)
	fmt.Printf("  Amount         : %.8f ARMX (%d Armoids)\n", float64(res.Tx.Amount)/1e8, res.Tx.Amount)
	fmt.Printf("  Miner Fee      : %.8f ARMX (%d Armoids)\n", float64(res.Tx.Fee)/1e8, res.Tx.Fee)
	fmt.Printf("  Nonce          : #%d\n", res.Tx.Nonce)
	fmt.Printf("  Size           : %d bytes (ML-DSA-65)\n", res.Tx.Size())
	fmt.Println("==================================================")
}

func rescanWallet(filePath string, daemonURL string, httpGet func(string) (*http.Response, error)) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("[-] Cannot read keystore file '%s': %v\n", filePath, err)
		return
	}
	var ks wallet.KeystoreFile
	if err := json.Unmarshal(data, &ks); err != nil {
		fmt.Println("[-] Invalid keystore file format:", err)
		return
	}

	fmt.Printf("[*] Rescanning transactions for wallet address: %s...\n", ks.Address)
	resp, err := httpGet(fmt.Sprintf("%s/history?address=%s", daemonURL, ks.Address))
	if err != nil {
		fmt.Printf("[-] Failed to query transaction history: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var historyResp struct {
		Address      string             `json:"address"`
		Transactions []core.Transaction `json:"transactions"`
	}
	if err := json.Unmarshal(body, &historyResp); err != nil {
		fmt.Printf("[-] Failed to parse history response: %v\n", err)
		return
	}

	fmt.Println("==================================================")
	fmt.Printf(" RESCAN RESULTS FOR %s (%d txs found):\n", ks.Address, len(historyResp.Transactions))
	fmt.Println("==================================================")
	for i, tx := range historyResp.Transactions {
		role := "Received"
		if tx.Sender == ks.Address {
			role = "Sent"
		}
		fmt.Printf("  [%d] %s: %.8f ARMX | Nonce #%d | Fee: %d Armoids | TXID: %s\n",
			i+1, role, float64(tx.Amount)/1e8, tx.Nonce, tx.Fee, tx.HashHex())
	}
	if len(historyResp.Transactions) == 0 {
		fmt.Println("  (no transactions recorded yet for this address)")
	}
	fmt.Println("==================================================")
}

func checkBalance(address string, daemonURL string, httpGet func(string) (*http.Response, error)) {
	fmt.Printf("[~] Connecting to daemon %s for address: %s...\n", daemonURL, address)
	resp, err := httpGet(daemonURL + "/balance?address=" + address)
	if err != nil {
		fmt.Println("[-] Failed to connect to daemon (ensure armcoind is running):", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("[-] Failed to read daemon response:", err)
		return
	}

	var result struct {
		Address          string `json:"address"`
		Balance          int64  `json:"balance"`
		PendingBalance   int64  `json:"pending_balance"`
		ImmatureBalance  int64  `json:"immature_balance"`
		SpendableBalance int64  `json:"spendable_balance"`
		CoinbaseMaturity int    `json:"coinbase_maturity"`
		Nonce            int64  `json:"nonce"`
		PendingNonce     int64  `json:"pending_nonce"`
		NextNonce        int64  `json:"next_nonce"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("[OK] Raw response:\n%s\n", string(body))
		return
	}

	nextNonce := result.NextNonce
	if nextNonce <= 0 {
		nextNonce = result.PendingNonce + 1
	}

	fmt.Println("\n==================================================")
	fmt.Println(" ARMAX ACCOUNT DETAILS:")
	fmt.Println("==================================================")
	fmt.Printf("  Wallet Address    : %s\n", result.Address)
	if result.ImmatureBalance > 0 {
		fmt.Printf("  Spendable Balance : %.8f ARMX (%d Armoids)\n", float64(result.SpendableBalance)/1e8, result.SpendableBalance)
		fmt.Printf("  Immature Rewards  : %.8f ARMX (%d Armoids) [< %d block confirmations]\n", float64(result.ImmatureBalance)/1e8, result.ImmatureBalance, result.CoinbaseMaturity)
		fmt.Printf("  Total Confirmed   : %.8f ARMX (%d Armoids)\n", float64(result.Balance)/1e8, result.Balance)
	} else {
		fmt.Printf("  Confirmed Balance : %.8f ARMX (%d Armoids)\n", float64(result.Balance)/1e8, result.Balance)
		fmt.Printf("  Available Balance : %.8f ARMX (%d Armoids) [after pending mempool txs]\n", float64(result.PendingBalance)/1e8, result.PendingBalance)
	}
	fmt.Printf("  Account Nonce     : #%d (Pending Nonce: #%d, Next TX Nonce: #%d)\n", result.Nonce, result.PendingNonce, nextNonce)
	fmt.Printf("  Query Timestamp   : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("==================================================")
}

func showMempool(daemonURL string, httpGet func(string) (*http.Response, error)) {
	fmt.Printf("[~] Querying mempool queue from daemon %s...\n", daemonURL)
	resp, err := httpGet(daemonURL + "/mempool")
	if err != nil {
		fmt.Println("[-] Failed to connect to daemon (ensure armcoind is running):", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("[-] Failed to retrieve mempool from daemon (HTTP %d)\n", resp.StatusCode)
		return
	}

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
	fmt.Println(" ARMAX MEMPOOL TRANSACTION QUEUE:")
	fmt.Println("==================================================")
	fmt.Printf("  Mempool Size    : %d unconfirmed transactions\n", len(txs))
	fmt.Printf("  Total Memory    : %d bytes (%.2f KB)\n", totalBytes, float64(totalBytes)/1024.0)
	fmt.Println("--------------------------------------------------")
	for i, tx := range txs {
		fmt.Printf("  [%d] TXID : %s\n", i+1, tx.HashHex())
		fmt.Printf("      From : %s\n", tx.Sender)
		fmt.Printf("      To   : %s\n", tx.Recipient)
		fmt.Printf("      Amt  : %s | Fee: %s | Nonce #%d | Size: %d bytes\n",
			core.FormatArmxDecimal(tx.Amount), core.FormatArmxDecimal(tx.Fee), tx.Nonce, tx.Size())
	}
	if len(txs) == 0 {
		fmt.Println("  (mempool queue is currently empty - 0 pending transactions)")
	}
	fmt.Println("==================================================")
}
