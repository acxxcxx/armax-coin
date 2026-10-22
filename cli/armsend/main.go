package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/wallet"
)

type BalanceResponse struct {
	Address          string `json:"address"`
	Balance          int64  `json:"balance"`
	PendingBalance   int64  `json:"pending_balance"`
	ImmatureBalance  int64  `json:"immature_balance"`
	SpendableBalance int64  `json:"spendable_balance"`
	Nonce            int64  `json:"nonce"`
	PendingNonce     int64  `json:"pending_nonce"`
	NextNonce        int64  `json:"next_nonce"` // Unambiguous next transaction nonce
}

func main() {
	networkPtr := flag.String("network", "testnet", "Network profile (testnet, mainnet) [L1-3, L4-1]")
	toPtr := flag.String("to", "", "Recipient wallet address (required, Bech32 armx1...)")
	amountPtr := flag.String("amount", "", "Amount to send in ARMX decimal string (e.g. '10.5' ARMX)")
	amountArmxPtr := flag.String("amount-armx", "", "Amount to send in ARMX decimal string (alias for -amount)")
	amountArmoidsPtr := flag.Int64("amount-armoids", 0, "Amount to send in ARMX base units (Armoids, 1 ARMX = 10^8)")
	feePtr := flag.String("fee", "", "Transaction miner fee in ARMX decimal string (e.g. '0.0001', default: auto)")
	feeArmoidsPtr := flag.Int64("fee-armoids", 0, "Transaction miner fee in Armoids (default: 0 = automatic minimum fee)")
	walletFilePtr := flag.String("wallet", "wallet.json", "Encrypted wallet keystore file path")
	filePtr := flag.String("file", "", "Encrypted wallet keystore file path (alias for -wallet)")
	daemonPtr := flag.String("daemon", "", "RPC daemon node URL (default: profile port)")
	rpcUserPtr := flag.String("rpcuser", "", "Username for RPC authentication")
	rpcPasswordPtr := flag.String("rpcpassword", "", "Password for RPC authentication")
	dryRunPtr := flag.Bool("dry-run", false, "Validate transaction parameters and balance without broadcasting to network")
	estimateFeePtr := flag.Bool("estimatefee", false, "Calculate and display estimated minimum transaction fee")
	mempoolPtr := flag.Bool("mempool", false, "Display unconfirmed transactions waiting in daemon mempool")
	getMempoolInfoPtr := flag.Bool("getmempoolinfo", false, "Display unconfirmed transactions waiting in daemon mempool (alias for -mempool)")
	decodePtr := flag.String("decode", "", "Query and decode details of a transaction by TXID")
	confirmationsPtr := flag.String("confirmations", "", "Query confirmation count for a given transaction TXID")
	waitPtr := flag.Bool("wait", false, "Wait until transaction is included in a block with at least 1 confirmation")
	jsonPtr := flag.Bool("json", false, "Output results in JSON format for scripting and automation")
	portPtr := flag.String("port", "", "RPC Daemon port (deprecated: use -rpcport)")
	rpcPortPtr := flag.String("rpcport", "", "RPC Daemon port (overrides default)")
	p2pPortPtr := flag.String("p2p", "", "P2P port (deprecated: use -p2pport)")
	p2pPortNewPtr := flag.String("p2pport", "", "P2P port (overrides default)")
	dataDirPtr := flag.String("datadir", "", "Custom data directory")
	infoPtr := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionPtr := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	flag.Parse()

	if *versionPtr {
		fmt.Printf("armsend v%s\n", core.Version)
		return
	}

	effRPCPort := *rpcPortPtr
	effP2PPort := *p2pPortNewPtr

	// Check deprecated flags with explicit warnings [L4-8]
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			if !*jsonPtr {
				fmt.Println("[!] Warning: -port is deprecated, please use -rpcport instead")
			}
			if effRPCPort == "" {
				effRPCPort = *portPtr
			}
		}
		if f.Name == "p2p" {
			if !*jsonPtr {
				fmt.Println("[!] Warning: -p2p is deprecated, please use -p2pport instead")
			}
			if effP2PPort == "" {
				effP2PPort = *p2pPortPtr
			}
		}
	})

	if *dataDirPtr != "" {
		_ = os.Setenv("ARMX_DATA_DIR", *dataDirPtr)
	}

	targetWalletFile := *walletFilePtr
	if *filePtr != "" {
		targetWalletFile = *filePtr
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
				if !*jsonPtr {
					fmt.Println("[!] Note: regtest profile cannot be selected via environment variable. Please specify '-network regtest' explicitly.")
				}
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
	fmt.Println("WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited")
	fmt.Println("==================================================")

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

	daemonURL := *daemonPtr
	if daemonURL == "" {
		if effRPCPort != "" {
			daemonURL = "http://127.0.0.1:" + effRPCPort
		} else {
			daemonURL = cliNet.DaemonURL
		}
	}

	// Helper function for authenticated HTTP GET requests
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

	// Helper function for authenticated HTTP POST requests
	httpPostAuth := func(endpoint string, contentType string, body io.Reader) (*http.Response, error) {
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequest("POST", endpoint, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		if *rpcUserPtr != "" || *rpcPasswordPtr != "" {
			req.SetBasicAuth(*rpcUserPtr, *rpcPasswordPtr)
		}
		return client.Do(req)
	}

	// 1. Transaction Decoding Mode
	if *decodePtr != "" {
		txid := strings.TrimSpace(*decodePtr)
		resp, err := httpGetAuth(fmt.Sprintf("%s/tx?hash=%s", daemonURL, txid))
		if err != nil {
			fmt.Printf("[-] Failed to query transaction from daemon: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("[-] Transaction '%s' not found (HTTP %d)\n", txid, resp.StatusCode)
			os.Exit(1)
		}
		if *jsonPtr {
			fmt.Println(string(data))
			return
		}
		var txInfo struct {
			Status        string           `json:"status"`
			BlockIndex    int              `json:"block_index"`
			BlockHash     string           `json:"block_hash"`
			Confirmations int              `json:"confirmations"`
			Timestamp     int64            `json:"timestamp"`
			Tx            core.Transaction `json:"tx"`
		}
		if err := json.Unmarshal(data, &txInfo); err != nil {
			fmt.Printf("[OK] Raw Transaction Data:\n%s\n", string(data))
			return
		}
		fmt.Println("==================================================")
		fmt.Println(" ARMAX TRANSACTION DETAILS:")
		fmt.Println("==================================================")
		fmt.Printf("  TXID / Hash    : %s\n", txid)
		fmt.Printf("  Status         : %s (%d confirmations)\n", strings.ToUpper(txInfo.Status), txInfo.Confirmations)
		fmt.Printf("  Block Index    : #%d (Hash: %s)\n", txInfo.BlockIndex, txInfo.BlockHash)
		fmt.Printf("  Sender         : %s\n", txInfo.Tx.Sender)
		fmt.Printf("  Recipient      : %s\n", txInfo.Tx.Recipient)
		fmt.Printf("  Amount         : %.8f ARMX (%d Armoids)\n", float64(txInfo.Tx.Amount)/1e8, txInfo.Tx.Amount)
		fmt.Printf("  Miner Fee      : %.8f ARMX (%d Armoids)\n", float64(txInfo.Tx.Fee)/1e8, txInfo.Tx.Fee)
		fmt.Printf("  Nonce          : #%d\n", txInfo.Tx.Nonce)
		fmt.Printf("  Size           : %d bytes\n", txInfo.Tx.Size())
		fmt.Println("==================================================")
		return
	}

	// 2. Query Confirmations Mode
	if *confirmationsPtr != "" {
		txid := strings.TrimSpace(*confirmationsPtr)
		resp, err := httpGetAuth(fmt.Sprintf("%s/tx?hash=%s", daemonURL, txid))
		if err != nil {
			fmt.Printf("[-] Failed to query transaction: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("[-] Transaction '%s' not found in mempool or chain\n", txid)
			os.Exit(1)
		}
		var txInfo struct {
			Status        string `json:"status"`
			Confirmations int    `json:"confirmations"`
		}
		_ = json.Unmarshal(data, &txInfo)
		if *jsonPtr {
			out, _ := json.Marshal(map[string]interface{}{
				"txid":          txid,
				"status":        txInfo.Status,
				"confirmations": txInfo.Confirmations,
			})
			fmt.Println(string(out))
			return
		}
		fmt.Printf("Transaction %s: %d confirmations (status: %s)\n", txid, txInfo.Confirmations, txInfo.Status)
		return
	}

	// 3. Estimate Fee Mode
	if *estimateFeePtr {
		typicalTxSize := 3500 // Typical post-quantum ML-DSA-65 transaction size
		estFee := core.CalculateMinMempoolFee(typicalTxSize)
		if *jsonPtr {
			out, _ := json.Marshal(map[string]interface{}{
				"typical_size_bytes": typicalTxSize,
				"fee_armx":           float64(estFee) / 1e8,
				"fee_armoids":        estFee,
			})
			fmt.Println(string(out))
			return
		}
		fmt.Println("==================================================")
		fmt.Println(" TRANSACTION FEE ESTIMATE:")
		fmt.Println("==================================================")
		fmt.Printf("  Estimated Payload Size : ~%d bytes (Post-Quantum ML-DSA-65)\n", typicalTxSize)
		fmt.Printf("  Estimated Minimum Fee  : %.8f ARMX (%d Armoids)\n", float64(estFee)/1e8, estFee)
		fmt.Println("==================================================")
		return
	}

	// 4. Mempool Inspection Mode
	if *mempoolPtr || *getMempoolInfoPtr {
		resp, err := httpGetAuth(daemonURL + "/mempool")
		if err != nil {
			fmt.Printf("[-] Failed to query mempool from daemon (%s): %v\n", daemonURL, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("[-] Failed to retrieve mempool from daemon (HTTP %d)\n", resp.StatusCode)
			os.Exit(1)
		}
		if *jsonPtr {
			fmt.Println(string(body))
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
		fmt.Println(" ARMAX MEMPOOL QUEUE (SENDER INSPECTION):")
		fmt.Println("==================================================")
		fmt.Printf("  Mempool Size    : %d unconfirmed transactions\n", len(txs))
		fmt.Printf("  Total Size      : %d bytes (%.2f KB)\n", totalBytes, float64(totalBytes)/1024.0)
		fmt.Println("--------------------------------------------------")
		for i, tx := range txs {
			fmt.Printf("  [%d] TXID: %s\n", i+1, tx.HashHex())
			fmt.Printf("      From : %s\n", tx.Sender)
			fmt.Printf("      To   : %s\n", tx.Recipient)
			fmt.Printf("      Amt  : %s | Fee: %s | Nonce #%d | Size: %d bytes\n",
				core.FormatArmxDecimal(tx.Amount), core.FormatArmxDecimal(tx.Fee), tx.Nonce, tx.Size())
		}
		if len(txs) == 0 {
			fmt.Println("  (no unconfirmed transactions in mempool)")
		}
		fmt.Println("==================================================")
		return
	}

	if !*jsonPtr {
		fmt.Println("==================================================")
		fmt.Println("WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited")
		fmt.Println(" ARMAXCOIN (ARMX) - POST-QUANTUM TRANSACTION SENDER")
		fmt.Println("==================================================")
		fmt.Printf("[+] Target Network   : %s (%s, RPC: %s)\n", strings.ToUpper(activeNet.Name), activeNet.NetworkID, activeNet.RPCPort)
	}

	// Resolve send amount (priority: -amount-armoids, then -amount, then -amount-armx)
	var finalAmount int64 = 0
	if *amountArmoidsPtr > 0 {
		finalAmount = *amountArmoidsPtr
	} else if *amountPtr != "" {
		parsed, err := core.ParseArmxDecimal(*amountPtr)
		if err != nil {
			fmt.Printf("[-] Invalid -amount '%s': %v\n", *amountPtr, err)
			os.Exit(1)
		}
		finalAmount = parsed
	} else if *amountArmxPtr != "" {
		parsed, err := core.ParseArmxDecimal(*amountArmxPtr)
		if err != nil {
			fmt.Printf("[-] Invalid -amount-armx '%s': %v\n", *amountArmxPtr, err)
			os.Exit(1)
		}
		finalAmount = parsed
	}

	// Resolve transaction fee
	var userFee int64 = 0
	if *feeArmoidsPtr > 0 {
		userFee = *feeArmoidsPtr
	} else if *feePtr != "" {
		parsedFee, err := core.ParseArmxDecimal(*feePtr)
		if err != nil {
			fmt.Printf("[-] Invalid -fee '%s': %v\n", *feePtr, err)
			os.Exit(1)
		}
		userFee = parsedFee
	}

	if *toPtr == "" || finalAmount <= 0 {
		fmt.Println("Usage:")
		fmt.Println("  ./armsend -to <RECIPIENT_ADDR> -amount <ARMX_DECIMAL> [-fee <ARMX_FEE>] [-wallet <FILE>]")
		fmt.Println("  ./armsend -to <RECIPIENT_ADDR> -amount-armoids <ARMOIDS> [-fee-armoids <FEE>] [-wallet <FILE>]")
		fmt.Println("  ./armsend -mempool [-daemon <URL>]")
		fmt.Println("\nExamples:")
		fmt.Println("  ./armsend -to armx1... -amount 10.5")
		fmt.Println("  ./armsend -to armx1... -amount 10.5 -fee 0.0001 -dry-run")
		fmt.Println("  ./armsend -mempool")
		fmt.Println("  ./armsend -decode <TXID>")
		fmt.Println("  ./armsend -confirmations <TXID>")
		os.Exit(1)
	}

	if !core.ValidateAddress(*toPtr) {
		fmt.Printf("[-] Invalid recipient address '%s': must be canonical Bech32 (armx1...)\n", *toPtr)
		os.Exit(1)
	}

	passphrase, err := wallet.PromptPassphrase("ARMX_WALLET_PASSPHRASE", "Enter keystore passphrase: ")
	if err != nil {
		fmt.Println("[-] Passphrase error:", err)
		os.Exit(1)
	}

	w, err := wallet.DecryptAndLoadKeystore(passphrase, targetWalletFile)
	if err != nil {
		fmt.Printf("[-] Failed to unlock wallet keystore '%s': %v\n", targetWalletFile, err)
		os.Exit(1)
	}

	if !*jsonPtr {
		fmt.Printf("[~] Querying account nonce and balance from %s...\n", daemonURL)
	}
	resp, err := httpGetAuth(fmt.Sprintf("%s/balance?address=%s", daemonURL, w.Address))
	if err != nil {
		fmt.Println("[-] Failed to query daemon:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var balResp BalanceResponse
	if err := json.Unmarshal(body, &balResp); err != nil {
		fmt.Println("[-] Invalid response from daemon:", string(body))
		os.Exit(1)
	}

	if !*jsonPtr {
		fmt.Printf("[+] Sender Address     : %s\n", w.Address)
		fmt.Printf("[+] Available Balance  : %.8f ARMX (%d Armoids)\n", float64(balResp.PendingBalance)/1e8, balResp.PendingBalance)
		fmt.Printf("[+] Current Nonce      : #%d\n", balResp.PendingNonce)
	}

	txNonce := balResp.NextNonce
	if txNonce <= 0 {
		txNonce = balResp.PendingNonce + 1
	}
	if !*jsonPtr {
		fmt.Printf("[+] Next TX Nonce      : #%d\n", txNonce)
	}

	// Construct transaction and calculate minimum fee
	tx := core.Transaction{
		Sender:    w.Address,
		SenderPub: w.PublicKey,
		Recipient: *toPtr,
		Amount:    finalAmount,
		Fee:       userFee,
		Nonce:     txNonce,
	}

	if err := tx.Sign(w.PrivateKey); err != nil {
		fmt.Println("[-] Post-quantum signature creation failed:", err)
		os.Exit(1)
	}

	minRequiredFee := core.CalculateMinMempoolFee(tx.Size())
	if userFee < minRequiredFee {
		if userFee == 0 {
			if !*jsonPtr {
				fmt.Printf("[+] Automatic fee applied: %.8f ARMX (%d Armoids for %d bytes)\n",
					float64(minRequiredFee)/1e8, minRequiredFee, tx.Size())
			}
		} else {
			if !*jsonPtr {
				fmt.Printf("[!] User fee (%.8f ARMX) below minimum relay fee. Automatically upgraded to: %.8f ARMX (%d Armoids for %d bytes)\n",
					float64(userFee)/1e8, float64(minRequiredFee)/1e8, minRequiredFee, tx.Size())
			}
		}
		tx.Fee = minRequiredFee
		if err := tx.Sign(w.PrivateKey); err != nil {
			fmt.Println("[-] Re-signing failed:", err)
			os.Exit(1)
		}
	}

	totalCost := finalAmount + tx.Fee
	availBal := balResp.PendingBalance
	if balResp.ImmatureBalance > 0 || balResp.SpendableBalance > 0 {
		availBal = balResp.SpendableBalance
	}
	if availBal < totalCost {
		if balResp.ImmatureBalance > 0 {
			fmt.Printf("[-] Insufficient spendable balance: has %.8f ARMX spendable (%.8f ARMX immature < %d blocks), requires %.8f ARMX (amount %.8f + fee %.8f)\n",
				float64(availBal)/1e8, float64(balResp.ImmatureBalance)/1e8, activeNet.CoinbaseMaturity,
				float64(totalCost)/1e8, float64(finalAmount)/1e8, float64(tx.Fee)/1e8)
		} else {
			fmt.Printf("[-] Insufficient balance: has %.8f ARMX, requires %.8f ARMX (amount %.8f + fee %.8f)\n",
				float64(availBal)/1e8, float64(totalCost)/1e8, float64(finalAmount)/1e8, float64(tx.Fee)/1e8)
		}
		os.Exit(1)
	}

	// Handle Dry-Run Mode
	if *dryRunPtr {
		if *jsonPtr {
			out, _ := json.Marshal(map[string]interface{}{
				"status":    "valid",
				"dry_run":   true,
				"txid":      tx.HashHex(),
				"sender":    tx.Sender,
				"recipient": tx.Recipient,
				"amount":    float64(tx.Amount) / 1e8,
				"fee":       float64(tx.Fee) / 1e8,
				"nonce":     tx.Nonce,
				"size":      tx.Size(),
			})
			fmt.Println(string(out))
			return
		}
		fmt.Println("==================================================")
		fmt.Println(" DRY-RUN TRANSACTION VALIDATION SUCCEEDED:")
		fmt.Println("==================================================")
		fmt.Printf("  TXID / Hash    : %s\n", tx.HashHex())
		fmt.Printf("  Sender         : %s\n", tx.Sender)
		fmt.Printf("  Recipient      : %s\n", tx.Recipient)
		fmt.Printf("  Amount         : %.8f ARMX (%d Armoids)\n", float64(tx.Amount)/1e8, tx.Amount)
		fmt.Printf("  Miner Fee      : %.8f ARMX (%d Armoids)\n", float64(tx.Fee)/1e8, tx.Fee)
		fmt.Printf("  Nonce          : #%d\n", tx.Nonce)
		fmt.Printf("  Payload Size   : %d bytes (ML-DSA-65 quantum-safe)\n", tx.Size())
		fmt.Println("  Broadcast      : SKIPPED (dry-run mode: no transaction broadcast)")
		fmt.Println("==================================================")
		return
	}

	txData, _ := json.Marshal(tx)
	if !*jsonPtr {
		fmt.Printf("[~] Broadcasting transaction to mempool: %s/tx/new...\n", daemonURL)
	}

	postResp, err := httpPostAuth(daemonURL+"/tx/new", "application/json", bytes.NewBuffer(txData))
	if err != nil {
		fmt.Println("[-] Broadcast failed:", err)
		os.Exit(1)
	}
	defer postResp.Body.Close()

	respBytes, _ := io.ReadAll(postResp.Body)
	if postResp.StatusCode != http.StatusOK {
		if *jsonPtr {
			out, _ := json.Marshal(map[string]interface{}{
				"status":  "error",
				"code":    postResp.StatusCode,
				"message": string(respBytes),
			})
			fmt.Println(string(out))
		} else {
			fmt.Println("==================================================")
			fmt.Printf("[-] Transaction rejected by node (HTTP %d):\n", postResp.StatusCode)
			fmt.Printf("    %s\n", string(respBytes))
			fmt.Println("==================================================")
		}
		os.Exit(1)
	}

	txHash := tx.HashHex()

	if *jsonPtr {
		out, _ := json.Marshal(map[string]interface{}{
			"status":    "broadcast_success",
			"txid":      txHash,
			"sender":    tx.Sender,
			"recipient": tx.Recipient,
			"amount":    float64(tx.Amount) / 1e8,
			"fee":       float64(tx.Fee) / 1e8,
			"nonce":     tx.Nonce,
			"size":      tx.Size(),
		})
		fmt.Println(string(out))
	} else {
		fmt.Println("==================================================")
		fmt.Println(" POST-QUANTUM TRANSACTION BROADCAST SUCCESSFUL!")
		fmt.Printf("   TXID / Hash : %s\n", txHash)
		fmt.Printf("   Sender      : %s\n", tx.Sender)
		fmt.Printf("   Recipient   : %s\n", tx.Recipient)
		fmt.Printf("   Amount      : %.8f ARMX (%d Armoids)\n", float64(tx.Amount)/1e8, tx.Amount)
		fmt.Printf("   Miner Fee   : %.8f ARMX (%d Armoids)\n", float64(tx.Fee)/1e8, tx.Fee)
		fmt.Printf("   Nonce       : #%d\n", tx.Nonce)
		fmt.Printf("   Size        : %d bytes (ML-DSA-65 NIST FIPS 204)\n", tx.Size())
		fmt.Printf("   Timestamp   : %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("   Status      : Broadcast to P2P mempool. Awaiting block inclusion.")
		fmt.Println("==================================================")
	}

	// Handle Wait Confirmation Mode
	if *waitPtr {
		waitStart := time.Now()
		if !*jsonPtr {
			fmt.Printf("[%s] [~] Waiting for transaction %s to be included in block...\n",
				time.Now().Format("15:04:05"), txHash)
		}
		for {
			time.Sleep(3 * time.Second)
			checkResp, err := httpGetAuth(fmt.Sprintf("%s/tx?hash=%s", daemonURL, txHash))
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(checkResp.Body)
			checkResp.Body.Close()
			if checkResp.StatusCode == http.StatusOK {
				var res struct {
					Status        string `json:"status"`
					BlockIndex    int    `json:"block_index"`
					Confirmations int    `json:"confirmations"`
				}
				if json.Unmarshal(data, &res) == nil && res.Status == "confirmed" && res.Confirmations >= 1 {
					if !*jsonPtr {
						elapsed := int(time.Since(waitStart).Seconds())
						fmt.Printf("[%s] [OK] Transaction mined into Block #%d (%d confirmation(s), %ds)!\n",
							time.Now().Format("15:04:05"), res.BlockIndex, res.Confirmations, elapsed)
					}
					break
				}
			}
		}
	}
}
