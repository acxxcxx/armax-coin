//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/node"
	"github.com/acxxcxx/armax-coin/wallet"
	"github.com/lxn/walk"
	"github.com/skip2/go-qrcode"

	//lint:ignore ST1001 Walk declarative GUI DSL requires dot import per library design
	. "github.com/lxn/walk/declarative"
)

// TxItem represents a row in the transaction history TableView
type TxItem struct {
	Index     int
	Type      string
	Amount    string
	Height    string
	TxID      string
	Timestamp string
	Record    *TxHistoryRecord
}

type TxModel struct {
	walk.TableModelBase
	items         []*TxItem // currently displayed page
	allItems      []*TxItem // complete history loaded from the node
	filteredItems []*TxItem // result of the local search filter
	page          int
	pageSize      int
}

func (m *TxModel) RowCount() int {
	return len(m.items)
}

func (m *TxModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	item := m.items[row]
	switch col {
	case 0:
		return item.Type
	case 1:
		return item.Amount
	case 2:
		return item.Height
	case 3:
		return item.TxID
	case 4:
		return item.Timestamp
	}
	return ""
}

// Contact represents an entry in the persistent Address Book
type Contact struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	DateAdded string `json:"date_added"`
}

// AddressBookModel provides data backing for the contacts TableView
type AddressBookModel struct {
	walk.TableModelBase
	items []*Contact
}

func (m *AddressBookModel) RowCount() int {
	return len(m.items)
}

func (m *AddressBookModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	c := m.items[row]
	switch col {
	case 0:
		return c.Name
	case 1:
		return c.Address
	case 2:
		return c.DateAdded
	}
	return ""
}

type StatsResponse struct {
	Height            int     `json:"height"`
	Blocks            int     `json:"blocks"`
	PeersActive       int     `json:"peers_active"`
	PeersKnown        int     `json:"peers_known"`
	PeersCount        int     `json:"peers_count"`
	Peers             int     `json:"peers"`
	Difficulty        int     `json:"difficulty"`
	LatestHash        string  `json:"latest_hash"`
	MempoolSize       int     `json:"mempool_size"`
	Mempool           int     `json:"mempool"`
	CirculatingSupply float64 `json:"circulating_supply"`
	NetworkID         string  `json:"network_id"`
	MiningHashrate    float64 `json:"mining_hashrate"`
}

type BalanceResponse struct {
	Balance          int64 `json:"balance"`
	ConfirmedBalance int64 `json:"confirmed_balance"`
	PendingBalance   int64 `json:"pending_balance"`
	ImmatureBalance  int64 `json:"immature_balance"`
	SpendableBalance int64 `json:"spendable_balance"`
	CoinbaseMaturity int   `json:"coinbase_maturity"`
	PendingNonce     int64 `json:"pending_nonce"`
	NextNonce        int64 `json:"next_nonce"`
}

func (b BalanceResponse) GetConfirmed() int64 {
	if b.ConfirmedBalance != 0 {
		return b.ConfirmedBalance
	}
	return b.Balance
}

func (b BalanceResponse) GetSpendable() int64 {
	if b.ImmatureBalance > 0 {
		return b.SpendableBalance
	}
	if b.SpendableBalance > 0 {
		return b.SpendableBalance
	}
	if b.PendingBalance > 0 {
		return b.PendingBalance
	}
	if b.PendingBalance == 0 && b.SpendableBalance == 0 {
		return 0
	}
	return b.GetConfirmed()
}

type TxHistoryRecord struct {
	BlockIndex int              `json:"block_index"`
	Timestamp  int64            `json:"timestamp"`
	Status     string           `json:"status"`
	Tx         core.Transaction `json:"tx"`
}

var (
	activeWallet           *wallet.Wallet
	activeAddress          string
	currentBalance         int64
	rpcURL                 string
	activeNetwork          core.NetworkProfile
	txModel                = &TxModel{}
	addressBookModel       = &AddressBookModel{}
	addressBookLock        sync.Mutex
	miningActive           bool
	miningLock             sync.Mutex
	miningStopChan         chan struct{}
	sessionBlocks          int
	sessionReward          int64
	lastMinedHeight        int
	benchmarkHashesPerCore float64
	refreshBusy            int32
	txPageLabel            *walk.Label
	leTxSearch             *walk.LineEdit
	tvTransactions         *walk.TableView
	ivReceiveQR            *walk.ImageView
	leReceiveURI           *walk.LineEdit
	lastMempoolCount       int
	lastBalanceAmount      int64

	// Node Diagnostics Labels
	lblDiagDaemonStatus *walk.Label
	lblDiagHeight       *walk.Label
	lblDiagBestHash     *walk.Label
	lblDiagPeers        *walk.Label
	lblDiagDifficulty   *walk.Label
	lblDiagMempool      *walk.Label
	lblDiagSupply       *walk.Label
)

func formatHashrate(hps float64) string {
	if hps >= 1_000_000_000 {
		return fmt.Sprintf("%.2f GH/s", hps/1_000_000_000.0)
	} else if hps >= 1_000_000 {
		return fmt.Sprintf("%.2f MH/s", hps/1_000_000.0)
	} else if hps >= 1_000 {
		return fmt.Sprintf("%.2f kH/s", hps/1_000.0)
	}
	return fmt.Sprintf("%.0f H/s", hps)
}

func measureCPUMiningThroughput(threads int) float64 {
	if threads < 1 {
		threads = 1
	}
	if benchmarkHashesPerCore > 0 {
		return benchmarkHashesPerCore * float64(threads)
	}

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
		benchmarkHashesPerCore = float64(count) / elapsed
		return benchmarkHashesPerCore * float64(threads)
	}
	return float64(threads) * 450_000.0 // Fallback estimate: 450 kH/s per core
}

func findAddressBookPath() string {
	// Keep user data alongside the network-scoped RPC cookie instead of an
	// arbitrary working directory or the executable installation directory.
	return filepath.Join(filepath.Dir(node.GetRPCAuthCookiePath()), "addressbook.json")
}

func loadAddressBook() {
	addressBookLock.Lock()
	defer addressBookLock.Unlock()

	p := findAddressBookPath()
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var contacts []*Contact
	if err := json.Unmarshal(data, &contacts); err == nil {
		addressBookModel.items = contacts
		addressBookModel.PublishRowsReset()
	}
}

func saveAddressBook() error {
	addressBookLock.Lock()
	defer addressBookLock.Unlock()

	data, err := json.MarshalIndent(addressBookModel.items, "", "  ")
	if err != nil {
		return err
	}
	path := findAddressBookPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "addressbook-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func isNodeOnline(url string) bool {
	client := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(url + "/stats")
	if err == nil && resp.StatusCode == 200 {
		_ = resp.Body.Close()
		return true
	}
	return false
}

func findWalletPath() string {
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		cand := filepath.Join(exeDir, "wallet.json")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
		if _, err := os.Stat("wallet.json"); err == nil {
			return "wallet.json"
		}
		return cand
	}
	return "wallet.json"
}

func promptCreateWalletDialog(owner walk.Form) (*wallet.Wallet, error) {
	var dlg *walk.Dialog
	var lePass, leConfirm *walk.LineEdit
	var acceptPB, cancelPB *walk.PushButton
	var passphrase string

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Create Armaxcoin Wallet",
		MinSize:       Size{Width: 420, Height: 240},
		Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
		DefaultButton: &acceptPB,
		CancelButton:  &cancelPB,
		Children: []Widget{
			Label{Text: "Choose a strong passphrase to encrypt your wallet (min 12 characters):", Font: Font{Bold: true}},
			VSpacer{Size: 6},
			Label{Text: "Wallet Passphrase:"},
			LineEdit{AssignTo: &lePass, PasswordMode: true},
			Label{Text: "Confirm Passphrase:"},
			LineEdit{AssignTo: &leConfirm, PasswordMode: true},
			VSpacer{Size: 10},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &acceptPB,
						Text:     "Create & Backup Mnemonic",
						OnClicked: func() {
							pass := lePass.Text()
							conf := leConfirm.Text()
							if len(pass) < 12 {
								walk.MsgBox(dlg, "Error", "Passphrase must be at least 12 characters.", walk.MsgBoxIconError)
								return
							}
							if pass != conf {
								walk.MsgBox(dlg, "Error", "Passphrases do not match. Please re-enter.", walk.MsgBoxIconError)
								return
							}
							passphrase = pass
							dlg.Accept()
						},
					},
					PushButton{
						AssignTo:  &cancelPB,
						Text:      "Cancel",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}.Create(owner)

	if err != nil {
		return nil, err
	}
	if dlg.Run() != walk.DlgCmdOK {
		return nil, errors.New("wallet creation cancelled")
	}

	// Generate wallet and display 24-word recovery phrase ONCE
	newW, err := wallet.NewWallet()
	if err != nil {
		walk.MsgBox(owner, "Error", fmt.Sprintf("Failed to generate wallet: %v", err), walk.MsgBoxIconError)
		return nil, err
	}
	mnemonic := newW.Mnemonic

	var backupDlg *walk.Dialog
	var okBackupPB, cancelBackupPB *walk.PushButton
	var teMnemonic *walk.TextEdit

	err = Dialog{
		AssignTo:      &backupDlg,
		Title:         "24-Word Recovery Phrase Backup",
		MinSize:       Size{Width: 500, Height: 320},
		Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
		DefaultButton: &okBackupPB,
		CancelButton:  &cancelBackupPB,
		Children: []Widget{
			Label{Text: "IMPORTANT: Write down these 24 words in exact order on paper.", Font: Font{Bold: true}},
			Label{Text: "This recovery phrase is displayed ONCE and will NOT be stored in plaintext."},
			VSpacer{Size: 6},
			TextEdit{AssignTo: &teMnemonic, Text: mnemonic, ReadOnly: true, Font: Font{Family: "Consolas", PointSize: 10}},
			VSpacer{Size: 10},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo:  &okBackupPB,
						Text:      "I Have Written It Down -> Next",
						OnClicked: func() { backupDlg.Accept() },
					},
					PushButton{
						AssignTo:  &cancelBackupPB,
						Text:      "Cancel",
						OnClicked: func() { backupDlg.Cancel() },
					},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		return nil, err
	}
	if backupDlg.Run() != walk.DlgCmdOK {
		return nil, errors.New("wallet backup cancelled")
	}

	// Confirmation step: Re-type 3 random words before saving
	for {
		indices := wallet.PickRandomWordIndices()
		var verifyDlg *walk.Dialog
		var leWord1, leWord2, leWord3 *walk.LineEdit
		var verifyPB, cancelVerifyPB *walk.PushButton

		err = Dialog{
			AssignTo:      &verifyDlg,
			Title:         "Confirm Mnemonic Backup",
			MinSize:       Size{Width: 420, Height: 260},
			Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
			DefaultButton: &verifyPB,
			CancelButton:  &cancelVerifyPB,
			Children: []Widget{
				Label{Text: "Confirm your backup by re-typing the 3 requested words:", Font: Font{Bold: true}},
				VSpacer{Size: 6},
				Label{Text: fmt.Sprintf("Word #%d:", indices[0])},
				LineEdit{AssignTo: &leWord1},
				Label{Text: fmt.Sprintf("Word #%d:", indices[1])},
				LineEdit{AssignTo: &leWord2},
				Label{Text: fmt.Sprintf("Word #%d:", indices[2])},
				LineEdit{AssignTo: &leWord3},
				VSpacer{Size: 10},
				Composite{
					Layout: HBox{MarginsZero: true},
					Children: []Widget{
						HSpacer{},
						PushButton{
							AssignTo: &verifyPB,
							Text:     "Verify & Save Wallet",
							OnClicked: func() {
								w1 := strings.TrimSpace(leWord1.Text())
								w2 := strings.TrimSpace(leWord2.Text())
								w3 := strings.TrimSpace(leWord3.Text())
								if w1 == "" || w2 == "" || w3 == "" {
									walk.MsgBox(verifyDlg, "Incomplete Input", "Please fill in all 3 requested words from your mnemonic backup.", walk.MsgBoxIconWarning)
									return
								}
								answers := [3]string{w1, w2, w3}
								if !wallet.VerifyMnemonicWords(mnemonic, indices, answers) {
									walk.MsgBox(verifyDlg, "Verification Failed", "One or more words do not match your backup. Please try again.", walk.MsgBoxIconError)
									return
								}
								verifyDlg.Accept()
							},
						},
						PushButton{
							AssignTo:  &cancelVerifyPB,
							Text:      "Cancel",
							OnClicked: func() { verifyDlg.Cancel() },
						},
					},
				},
			},
		}.Create(owner)
		if err != nil {
			return nil, err
		}

		if verifyDlg.Run() == walk.DlgCmdOK {
			break
		}
		if walk.MsgBox(owner, "Cancel Wallet Creation?", "Are you sure you want to cancel wallet creation and exit?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
			return nil, errors.New("wallet creation cancelled")
		}
	}

	// Encrypt keystore to disk and zeroize mnemonic from memory
	walletPath := findWalletPath()
	if err := wallet.EncryptAndSaveKeystore(newW, passphrase, walletPath); err != nil {
		walk.MsgBox(owner, "Wallet Save Error", fmt.Sprintf("Failed to save encrypted keystore to '%s': %v", walletPath, err), walk.MsgBoxIconError)
		return nil, fmt.Errorf("failed to save keystore: %w", err)
	}
	newW.ClearMnemonic()
	walk.MsgBox(owner, "Wallet Saved", fmt.Sprintf("Your wallet has been safely encrypted and saved!\n\nAddress:\n%s\n\nFile:\n%s", newW.Address, walletPath), walk.MsgBoxIconInformation)
	return newW, nil
}

func promptMigrateWalletDialog(owner walk.Form, walletPath, oldPass string) (*wallet.Wallet, error) {
	for {
		var dlg *walk.Dialog
		var leNewPass, leConfirm *walk.LineEdit
		var okPB, cancelPB *walk.PushButton
		var newPass string

		err := Dialog{
			AssignTo:      &dlg,
			Title:         "Mandatory Wallet Passphrase Migration",
			MinSize:       Size{Width: 420, Height: 220},
			Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
			DefaultButton: &okPB,
			CancelButton:  &cancelPB,
			Children: []Widget{
				Label{Text: "Enter a new secure passphrase (minimum 12 characters):", Font: Font{Bold: true}},
				VSpacer{Size: 6},
				Label{Text: "New Passphrase:"},
				LineEdit{AssignTo: &leNewPass, PasswordMode: true},
				Label{Text: "Confirm New Passphrase:"},
				LineEdit{AssignTo: &leConfirm, PasswordMode: true},
				VSpacer{Size: 10},
				Composite{
					Layout: HBox{MarginsZero: true},
					Children: []Widget{
						HSpacer{},
						PushButton{
							AssignTo: &okPB,
							Text:     "Migrate & Save Keystore",
							OnClicked: func() {
								p1 := leNewPass.Text()
								p2 := leConfirm.Text()
								if len(p1) < 12 {
									walk.MsgBox(dlg, "Error", "New passphrase must be at least 12 characters.", walk.MsgBoxIconError)
									return
								}
								if p1 != p2 {
									walk.MsgBox(dlg, "Error", "Passphrases do not match. Please re-enter.", walk.MsgBoxIconError)
									return
								}
								if wallet.IsLegacyPassphrase(p1) {
									walk.MsgBox(dlg, "Error", "Cannot migrate to the legacy default passphrase.", walk.MsgBoxIconError)
									return
								}
								newPass = p1
								dlg.Accept()
							},
						},
						PushButton{
							AssignTo:  &cancelPB,
							Text:      "Cancel",
							OnClicked: func() { dlg.Cancel() },
						},
					},
				},
			},
		}.Create(owner)

		if err != nil {
			return nil, err
		}
		if dlg.Run() != walk.DlgCmdOK {
			return nil, errors.New("passphrase migration cancelled")
		}

		migratedW, err := wallet.MigrateKeystorePassphrase(walletPath, oldPass, newPass)
		if err != nil {
			walk.MsgBox(owner, "Migration Error", fmt.Sprintf("Failed to migrate keystore: %v", err), walk.MsgBoxIconError)
			continue
		}
		walk.MsgBox(owner, "Migration Complete", "Wallet successfully migrated to your new passphrase!", walk.MsgBoxIconInformation)
		return migratedW, nil
	}
}

func promptUnlockWalletDialog(owner walk.Form, walletPath string) (*wallet.Wallet, error) {
	for {
		var dlg *walk.Dialog
		var lePass *walk.LineEdit
		var okPB, cancelPB *walk.PushButton
		var enteredPass string

		err := Dialog{
			AssignTo:      &dlg,
			Title:         "Unlock Armaxcoin Wallet",
			MinSize:       Size{Width: 400, Height: 180},
			Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
			DefaultButton: &okPB,
			CancelButton:  &cancelPB,
			Children: []Widget{
				Label{Text: "Enter passphrase to unlock your wallet keystore:", Font: Font{Bold: true}},
				VSpacer{Size: 6},
				LineEdit{AssignTo: &lePass, PasswordMode: true},
				VSpacer{Size: 10},
				Composite{
					Layout: HBox{MarginsZero: true},
					Children: []Widget{
						HSpacer{},
						PushButton{
							AssignTo: &okPB,
							Text:     "Unlock Wallet",
							OnClicked: func() {
								enteredPass = lePass.Text()
								dlg.Accept()
							},
						},
						PushButton{
							AssignTo:  &cancelPB,
							Text:      "Cancel / Exit",
							OnClicked: func() { dlg.Cancel() },
						},
					},
				},
			},
		}.Create(owner)

		if err != nil {
			return nil, err
		}
		if dlg.Run() != walk.DlgCmdOK {
			return nil, errors.New("wallet unlock cancelled")
		}

		w, err := wallet.DecryptAndLoadKeystore(enteredPass, walletPath)
		if err != nil {
			// Delay 1.5 seconds after failed passphrase attempt to mitigate brute force [L3-1]
			time.Sleep(1500 * time.Millisecond)
			walk.MsgBox(owner, "Unlock Failed", "Invalid passphrase or corrupted keystore. Please try again.", walk.MsgBoxIconError)
			continue
		}

		// Check if wallet is using legacy default passphrase [L3-1]
		if wallet.IsLegacyPassphrase(enteredPass) || wallet.IsKeystoreUsingLegacyPassphrase(walletPath) {
			walk.MsgBox(owner, "Security Warning",
				"WARNING: Your wallet is protected by the insecure legacy default passphrase!\n\nYou must migrate to a new secure passphrase (min 12 characters) now.",
				walk.MsgBoxIconWarning)
			migratedW, err := promptMigrateWalletDialog(owner, walletPath, enteredPass)
			if err == nil && migratedW != nil {
				return migratedW, nil
			}
		}

		return w, nil
	}
}

func loadOrInitWallet(owner walk.Form) {
	walletPath := findWalletPath()
	if _, err := os.Stat(walletPath); err == nil {
		w, err := promptUnlockWalletDialog(owner, walletPath)
		if err == nil && w != nil {
			activeWallet = w
			activeAddress = w.Address
			return
		}
		if err != nil && err.Error() != "wallet unlock cancelled" {
			walk.MsgBox(owner, "Wallet Error", fmt.Sprintf("Error opening wallet: %v", err), walk.MsgBoxIconError)
			fmt.Fprintf(os.Stderr, "[-] Error unlocking wallet: %v\n", err)
		}
		// If user cancelled unlock dialog, exit application safely
		os.Exit(0)
	}

	w, err := promptCreateWalletDialog(owner)
	if err == nil && w != nil {
		activeWallet = w
		activeAddress = w.Address
		return
	}
	if err != nil && err.Error() != "wallet creation cancelled" && err.Error() != "wallet backup cancelled" && err.Error() != "mnemonic verification cancelled" {
		walk.MsgBox(owner, "Wallet Error", fmt.Sprintf("Error creating wallet: %v", err), walk.MsgBoxIconError)
		fmt.Fprintf(os.Stderr, "[-] Error creating wallet: %v\n", err)
	}
	os.Exit(0)
}

func showTxDetailsDialog(owner walk.Form, item *TxItem) {
	if item == nil {
		return
	}

	var dlg *walk.Dialog
	var closePB *walk.PushButton
	rec := item.Record

	txid := item.TxID
	shortTxID := txid
	if len(shortTxID) > 16 {
		shortTxID = shortTxID[:8] + "..." + shortTxID[len(shortTxID)-8:]
	}

	sender := "SYSTEM (Coinbase Block Reward)"
	recip := ""
	var nonce int64 = 0
	var fee int64 = 0
	if rec != nil {
		if rec.Tx.Sender != "" && rec.Tx.Sender != "SYSTEM" && rec.Tx.Sender != "COINBASE" {
			sender = rec.Tx.Sender
		}
		recip = rec.Tx.Recipient
		nonce = rec.Tx.Nonce
		fee = rec.Tx.Fee
	}

	confs := 0
	if rec != nil && rec.BlockIndex >= 0 && lastMinedHeight >= rec.BlockIndex {
		confs = lastMinedHeight - rec.BlockIndex + 1
	}

	statusDesc := "[Pending] Unconfirmed (in Mempool)"
	mat := activeNetwork.CoinbaseMaturity
	if mat <= 0 {
		mat = 100
	}
	if item.Height != "Pending" && item.Height != "" {
		if item.Type == "Mined (Coinbase)" {
			if confs < mat {
				statusDesc = fmt.Sprintf("[Immature] Mining Reward -- %d/%d Confirmations (Locked until %d blocks)", confs, mat, mat)
			} else {
				statusDesc = fmt.Sprintf("[OK] Mined & Mature -- %d Confirmations (Fully Spendable)", confs)
			}
		} else {
			if confs < 6 {
				statusDesc = fmt.Sprintf("[Confirming] -- %d/6 Confirmations (Wait 6 blocks for safety)", confs)
			} else if confs < 12 {
				statusDesc = fmt.Sprintf("OK Confirmed -- %d Confirmations (Safe for Standard Payments)", confs)
			} else {
				statusDesc = fmt.Sprintf("[Finalized] Securely Finalized -- %d Confirmations (Safe for High-Value Payments)", confs)
			}
		}
	}

	rawJSON := ""
	if rec != nil {
		if jBytes, err := json.MarshalIndent(rec.Tx, "", "  "); err == nil {
			rawJSON = string(jBytes)
		}
	}

	var teRaw *walk.TextEdit

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Transaction Details -- " + shortTxID,
		MinSize:       Size{Width: 580, Height: 520},
		Size:          Size{Width: 620, Height: 560},
		Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
		DefaultButton: &closePB,
		CancelButton:  &closePB,
		Children: []Widget{
			GroupBox{
				Title:  "Status & Amount",
				Layout: Grid{Columns: 2, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
				Children: []Widget{
					Label{Text: "Status:"},
					Label{Text: statusDesc, Font: Font{Bold: true}},

					Label{Text: "Transaction Type:"},
					Label{Text: item.Type, Font: Font{Bold: true}},

					Label{Text: "Total Amount:"},
					Label{Text: item.Amount, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},

					Label{Text: "Miner Fee:"},
					Label{Text: formatArmx(fee)},

					Label{Text: "Account Nonce:"},
					Label{Text: fmt.Sprintf("#%d", nonce)},

					Label{Text: "Block Height:"},
					Label{Text: item.Height},

					Label{Text: "Confirmations:"},
					Label{Text: fmt.Sprintf("%d confirmations", confs), Font: Font{Bold: true}},

					Label{Text: "Date & Time:"},
					Label{Text: item.Timestamp},
				},
			},

			GroupBox{
				Title:  "Addresses & Cryptography (NIST FIPS 204 ML-DSA-65)",
				Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
				Children: []Widget{
					Label{Text: "Transaction Hash (TXID):"},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							LineEdit{Text: txid, ReadOnly: true, Font: Font{Family: "Consolas", PointSize: 9}},
							PushButton{
								Text: " Copy",
								OnClicked: func() {
									_ = walk.Clipboard().SetText(txid)
									walk.MsgBox(dlg, "Copied", "TXID copied to clipboard!", walk.MsgBoxIconInformation)
								},
							},
						},
					},
					VSpacer{Size: 4},

					Label{Text: "From (Sender):"},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							LineEdit{Text: sender, ReadOnly: true, Font: Font{Family: "Consolas", PointSize: 9}},
							PushButton{
								Text: " Copy",
								OnClicked: func() {
									_ = walk.Clipboard().SetText(sender)
									walk.MsgBox(dlg, "Copied", "Sender address copied to clipboard!", walk.MsgBoxIconInformation)
								},
							},
						},
					},
					VSpacer{Size: 4},

					Label{Text: "To (Recipient):"},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							LineEdit{Text: recip, ReadOnly: true, Font: Font{Family: "Consolas", PointSize: 9}},
							PushButton{
								Text: " Copy",
								OnClicked: func() {
									_ = walk.Clipboard().SetText(recip)
									walk.MsgBox(dlg, "Copied", "Recipient address copied to clipboard!", walk.MsgBoxIconInformation)
								},
							},
						},
					},
				},
			},

			GroupBox{
				Title:  "Raw Transaction Payload (JSON)",
				Layout: VBox{Margins: Margins{Left: 12, Top: 8, Right: 12, Bottom: 8}},
				Children: []Widget{
					TextEdit{
						AssignTo: &teRaw,
						Text:     rawJSON,
						ReadOnly: true,
						Font:     Font{Family: "Consolas", PointSize: 8},
						VScroll:  true,
					},
				},
			},

			VSpacer{Size: 8},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{
						Text: " Copy Full JSON",
						OnClicked: func() {
							_ = walk.Clipboard().SetText(rawJSON)
							walk.MsgBox(dlg, "Copied", "Raw JSON transaction copied to clipboard!", walk.MsgBoxIconInformation)
						},
					},
					HSpacer{},
					PushButton{
						AssignTo:  &closePB,
						Text:      "Close",
						OnClicked: func() { dlg.Accept() },
					},
				},
			},
		},
	}.Create(owner)

	if err == nil {
		dlg.Run()
	}
}

func exportTransactionsCSV(mw *walk.MainWindow) {
	if len(txModel.allItems) == 0 {
		walk.MsgBox(mw, "Export Notice", "No transactions found in history to export.", walk.MsgBoxIconInformation)
		return
	}

	dlg := new(walk.FileDialog)
	dlg.Title = "Export Transaction History (CSV)"
	dlg.Filter = "CSV Spreadsheet (*.csv)|*.csv|All Files (*.*)|*.*"
	dlg.FilePath = fmt.Sprintf("armax_transactions_%s.csv", time.Now().Format("20060102_150405"))

	if ok, err := dlg.ShowSave(mw); !ok || err != nil {
		return
	}

	file, err := os.Create(dlg.FilePath)
	if err != nil {
		walk.MsgBox(mw, "Export Error", fmt.Sprintf("Failed to create file: %v", err), walk.MsgBoxIconError)
		return
	}
	defer file.Close()

	var buf bytes.Buffer
	buf.WriteString("Index,Type,Amount_ARMX,Block_Height,TXID,Date_Time\r\n")
	for _, it := range txModel.allItems {
		cleanType := strings.ReplaceAll(it.Type, ",", " ")
		cleanAmt := strings.TrimSuffix(strings.ReplaceAll(it.Amount, ",", ""), " ARMX")
		buf.WriteString(fmt.Sprintf("%d,%s,%s,%s,%s,%s\r\n",
			it.Index, cleanType, cleanAmt, it.Height, it.TxID, it.Timestamp))
	}

	if _, err := file.Write(buf.Bytes()); err != nil {
		walk.MsgBox(mw, "Export Error", fmt.Sprintf("Failed to write CSV: %v", err), walk.MsgBoxIconError)
		return
	}

	walk.MsgBox(mw, "Export Success", fmt.Sprintf("Successfully exported %d transactions to:\n%s", len(txModel.allItems), dlg.FilePath), walk.MsgBoxIconInformation)
}

func promptAddContactDialog(owner walk.Form) {
	var dlg *walk.Dialog
	var leName, leAddr *walk.LineEdit
	var okPB, cancelPB *walk.PushButton

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Add New Contact to Address Book",
		MinSize:       Size{Width: 460, Height: 200},
		Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
		DefaultButton: &okPB,
		CancelButton:  &cancelPB,
		Children: []Widget{
			Label{Text: "Contact Name / Label (e.g. Cold Storage, Mining Pool, Friend):", Font: Font{Bold: true}},
			LineEdit{AssignTo: &leName, CueBanner: "Enter contact label"},
			VSpacer{Size: 6},
			Label{Text: "Armaxcoin Bech32 Address (armx1...):", Font: Font{Bold: true}},
			LineEdit{AssignTo: &leAddr, CueBanner: "armx1... address", Font: Font{Family: "Consolas", PointSize: 9}},
			VSpacer{Size: 10},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &okPB,
						Text:     "Save Contact",
						OnClicked: func() {
							name := strings.TrimSpace(leName.Text())
							addr := strings.TrimSpace(leAddr.Text())
							if name == "" {
								walk.MsgBox(dlg, "Error", "Contact name cannot be empty.", walk.MsgBoxIconError)
								return
							}
							if !core.ValidateAddress(addr) {
								walk.MsgBox(dlg, "Invalid Address", "Must be a valid Armaxcoin Bech32 address (armx1...).", walk.MsgBoxIconError)
								return
							}

							addressBookLock.Lock()
							addressBookModel.items = append(addressBookModel.items, &Contact{
								Name:      name,
								Address:   addr,
								DateAdded: time.Now().Format("2006-01-02 15:04"),
							})
							addressBookLock.Unlock()
							_ = saveAddressBook()
							addressBookModel.PublishRowsReset()
							dlg.Accept()
						},
					},
					PushButton{
						AssignTo:  &cancelPB,
						Text:      "Cancel",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}.Create(owner)

	if err == nil {
		dlg.Run()
	}
}

func promptVisualAddressCard(owner walk.Form, address string) {
	var dlg *walk.Dialog
	var closePB *walk.PushButton

	// Group address into 4-character blocks for visual checking
	var chunked []string
	cleanAddr := strings.TrimSpace(address)
	for i := 0; i < len(cleanAddr); i += 4 {
		end := i + 4
		if end > len(cleanAddr) {
			end = len(cleanAddr)
		}
		chunked = append(chunked, cleanAddr[i:end])
	}
	formattedAddr := strings.Join(chunked, "  ")

	paymentURI := fmt.Sprintf("armax:%s?label=DesktopWallet", cleanAddr)

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Visual Payment Address & Cryptographic Identity",
		MinSize:       Size{Width: 540, Height: 320},
		Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
		DefaultButton: &closePB,
		CancelButton:  &closePB,
		Children: []Widget{
			Label{
				Text: "Your Armaxcoin Post-Quantum Receiving Address:",
				Font: Font{Bold: true, PointSize: 10},
			},
			VSpacer{Size: 6},
			GroupBox{
				Title:  "Visual Verification Blocks (Grouped by 4 Characters)",
				Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
				Children: []Widget{
					Label{
						Text: formattedAddr,
						Font: Font{Family: "Consolas", PointSize: 11, Bold: true},
					},
				},
			},
			VSpacer{Size: 6},
			GroupBox{
				Title:  "Payment URI (BIP-21 Compatible)",
				Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
				Children: []Widget{
					LineEdit{
						Text:     paymentURI,
						ReadOnly: true,
						Font:     Font{Family: "Consolas", PointSize: 9},
					},
				},
			},
			VSpacer{Size: 8},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{
						Text: " Copy Address",
						OnClicked: func() {
							_ = walk.Clipboard().SetText(cleanAddr)
							walk.MsgBox(dlg, "Copied", "Address copied to clipboard!", walk.MsgBoxIconInformation)
						},
					},
					PushButton{
						Text: " Copy Payment URI",
						OnClicked: func() {
							_ = walk.Clipboard().SetText(paymentURI)
							walk.MsgBox(dlg, "Copied", "Payment URI copied to clipboard!", walk.MsgBoxIconInformation)
						},
					},
					HSpacer{},
					PushButton{
						AssignTo:  &closePB,
						Text:      "Close",
						OnClicked: func() { dlg.Accept() },
					},
				},
			},
		},
	}.Create(owner)

	if err == nil {
		dlg.Run()
	}
}

func generateQRBitmap(content string, size int) (*walk.Bitmap, error) {
	if content == "" {
		return nil, errors.New("empty content")
	}
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	img := qr.Image(size)
	return walk.NewBitmapFromImageForDPI(img, 96)
}

func updateReceiveQRCode(addr string) {
	if ivReceiveQR == nil || addr == "" {
		return
	}
	bmp, err := generateQRBitmap(addr, 168)
	if err == nil && bmp != nil {
		_ = ivReceiveQR.SetImage(bmp)
	}
	if leReceiveURI != nil {
		leReceiveURI.SetText("armax:" + addr + "?label=DesktopWallet")
	}
}

func formatArmx(armoids int64) string {
	return core.FormatArmxDecimal(armoids) + " ARMX"
}

func main() {
	fmt.Println("==================================================")
	fmt.Println("WARNING  ARMAX ALPHA TESTNET - no economic value, unaudited")
	fmt.Println("  ARMAXCOIN (ARMX) - DESKTOP GUI WALLET")
	fmt.Println("==================================================")

	networkPtr := flag.String("network", "testnet", "Network profile (testnet, mainnet) [L4-1]")
	portPtr := flag.String("port", "", "RPC Daemon port (deprecated: use -rpcport)")
	rpcPortPtr := flag.String("rpcport", "", "RPC Daemon port (overrides default)")
	p2pPortPtr := flag.String("p2p", "", "P2P port (deprecated: use -p2pport)")
	p2pPortNewPtr := flag.String("p2pport", "", "P2P port (overrides default)")
	dataDirPtr := flag.String("datadir", "", "Custom data directory")
	infoPtr := flag.Bool("info", false, "Display active network profile configuration and exit [L3-8]")
	versionPtr := flag.Bool("version", false, "Display software release version and exit [L4-4]")
	flag.Parse()

	if *versionPtr {
		fmt.Printf("armgui v%s\n", core.Version)
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
	activeNetwork = core.GetActiveNetwork()

	if *infoPtr {
		fmt.Printf("[+] Target Network   : %s (%s, RPC: %s)\n", strings.ToUpper(activeNetwork.Name), activeNetwork.NetworkID, cliNet.RPCPort)
		fmt.Printf("[+] Network Profile  : %s (%s)\n", strings.ToUpper(activeNetwork.Name), activeNetwork.NetworkID)
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

	if exe, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(exe))
	}

	rpcPort := cliNet.RPCPort
	if effRPCPort != "" {
		rpcPort = effRPCPort
	}
	p2pPort := cliNet.P2PPort
	if effP2PPort != "" {
		p2pPort = effP2PPort
	}
	rpcURL = "http://127.0.0.1:" + rpcPort

	// 1. Start embedded node if not already running on port
	if !isNodeOnline(rpcURL) {
		bc, openErr := core.OpenBlockchain(3)
		if openErr != nil {
			fmt.Printf("[-] %v\n", openErr)
			os.Exit(1)
		}
		bc.CoinbaseMaturity = activeNetwork.CoinbaseMaturity
		cfg, err := node.LoadConfig("armax.conf")
		if err != nil {
			cfg = node.DefaultConfig()
			cfg.RPCPort = rpcPort
			cfg.P2PPort = p2pPort
		} else if activeNetwork.Name == "testnet" || activeNetwork.Name == "regtest" || activeNetwork.Name == "soak" {
			cfg.NetworkID = activeNetwork.NetworkID
			if cfg.RPCPort == core.MainnetRPCPort || effRPCPort != "" {
				cfg.RPCPort = rpcPort
			}
			if cfg.P2PPort == core.MainnetP2PPort || effP2PPort != "" {
				cfg.P2PPort = p2pPort
			}
			if len(cfg.SeedPeers) == 0 || (len(core.MainnetProfile.DefaultSeeds) > 0 && len(cfg.SeedPeers) > 0 && cfg.SeedPeers[0] == core.MainnetProfile.DefaultSeeds[0]) {
				cfg.SeedPeers = activeNetwork.DefaultSeeds
			}
		}
		if cfg.NetworkID == "" {
			cfg.NetworkID = activeNetwork.NetworkID
		}

		go node.StartServerWithConfig(bc, cfg)

		for i := 0; i < 40; i++ {
			time.Sleep(100 * time.Millisecond)
			if isNodeOnline(rpcURL) {
				break
			}
		}
	}

	loadOrInitWallet(nil)
	loadAddressBook()
	benchmarkHashesPerCore = measureCPUMiningThroughput(1)

	// UI Controls Assignment
	var mw *walk.MainWindow
	var tabWidget *walk.TabWidget
	var sbNet, sbHeight, sbPeers, sbMining *walk.StatusBarItem

	// Overview Controls
	var lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblNetworkID *walk.Label
	var leOverviewAddress *walk.LineEdit

	// Send Controls
	var leSendRecipient, leSendAmount, leSendFee, leSendPassphrase *walk.LineEdit
	var lblSendTotalPreview *walk.Label
	var teSendResult *walk.TextEdit

	// Receive Controls
	var leReceiveAddress, leRestoreMnemonic *walk.LineEdit

	// Mining Controls
	var leMiningAddress, leMiningThreads, leMiningDelay *walk.LineEdit
	var lblMiningStatus, lblMiningHashrate, lblMiningElapsed, lblMiningDiff, lblMiningBlocks, lblMiningRewards *walk.Label
	var teMiningLog *walk.TextEdit
	var btnStartMining, btnStopMining *walk.PushButton

	// Transactions Controls
	// (tvTransactions declared at package level for automatic redraw invalidation)

	// Address Book Controls
	var tvAddressBook *walk.TableView

	// Node Console Controls
	var teNodeConsole *walk.TextEdit
	var leNodeCommand *walk.LineEdit

	// Safe Icon Loading
	var appIcon *walk.Icon
	iconCandidates := []string{
		"armax_logo.ico",
		filepath.Join("gui", "armax_logo.ico"),
	}
	if exe, err := os.Executable(); err == nil {
		iconCandidates = append(iconCandidates,
			filepath.Join(filepath.Dir(exe), "armax_logo.ico"),
			filepath.Join(filepath.Dir(exe), "gui", "armax_logo.ico"),
		)
	}
	for _, icPath := range iconCandidates {
		if ic, err := walk.NewIconFromFile(icPath); err == nil {
			appIcon = ic
			break
		}
	}

	updateSendTotalPreview := func() {
		if lblSendTotalPreview == nil || leSendAmount == nil || leSendFee == nil {
			return
		}
		amtStr := strings.TrimSpace(leSendAmount.Text())
		feeStr := strings.TrimSpace(leSendFee.Text())
		var amtArmoids, feeArmoids int64
		if amtStr != "" {
			if a, err := core.ParseArmxDecimal(amtStr); err == nil && a > 0 {
				amtArmoids = a
			}
		}
		if feeStr != "" {
			if f, err := core.ParseArmxDecimal(feeStr); err == nil && f >= 0 {
				feeArmoids = f
			}
		}
		total := amtArmoids + feeArmoids
		lblSendTotalPreview.SetText(fmt.Sprintf("Total deduction: %s (Amount: %s + Fee: %s)",
			formatArmx(total), formatArmx(amtArmoids), formatArmx(feeArmoids)))
	}

	mwDecl := MainWindow{
		AssignTo: &mw,
		Title:    fmt.Sprintf("Armaxcoin Core - Desktop Wallet v%s [%s]", core.Version, strings.ToUpper(activeNetwork.Name)),
		MinSize:  Size{Width: 920, Height: 680},
		Size:     Size{Width: 960, Height: 720},
		Layout:   VBox{Margins: Margins{Left: 8, Top: 8, Right: 8, Bottom: 4}},
		StatusBarItems: []StatusBarItem{
			{
				AssignTo: &sbNet,
				Text:     "Network: " + strings.ToUpper(activeNetwork.Name),
				Width:    130,
			},
			{
				AssignTo: &sbHeight,
				Text:     "Height: Syncing...",
				Width:    120,
			},
			{
				AssignTo: &sbPeers,
				Text:     "Peers: 0",
				Width:    90,
			},
			{
				AssignTo: &sbMining,
				Text:     "Miner: IDLE",
				Width:    180,
			},
			{
				Text: "ML-DSA-65 (FIPS 204)",
			},
		},
		Children: []Widget{
			TabWidget{
				AssignTo: &tabWidget,
				OnCurrentIndexChanged: func() {
					if tabWidget != nil && tabWidget.CurrentIndex() == 4 { // Tab 5: Transactions
						loadTransactionsHistory(mw)
						if tvTransactions != nil {
							_ = tvTransactions.Invalidate()
						}
					}
				},
				Pages: []TabPage{
					// ==========================================
					// TAB 1: OVERVIEW
					// ==========================================
					{
						Title:  " Overview",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							GroupBox{
								Title:  "Wallet Balance & Address",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{
										AssignTo: &lblBalanceMain,
										Text:     "0.00000000 ARMX",
										Font:     Font{Family: "Segoe UI", PointSize: 18, Bold: true},
									},
									Label{
										AssignTo: &lblBalanceArmoids,
										Text:     "Spendable: 0.00000000 ARMX | Immature Rewards: 0.00000000 ARMX | Total: 0.00000000 ARMX",
										Font:     Font{Family: "Segoe UI", PointSize: 9},
									},
									VSpacer{Size: 6},
									Label{
										Text: "Active Wallet Receiving Address (Bech32):",
									},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											LineEdit{
												AssignTo: &leOverviewAddress,
												Text:     activeAddress,
												ReadOnly: true,
												Font:     Font{Family: "Consolas", PointSize: 10},
											},
											PushButton{
												Text: " Copy Address",
												OnClicked: func() {
													_ = walk.Clipboard().SetText(activeAddress)
													walk.MsgBox(mw, "Copied", "Address copied to clipboard!", walk.MsgBoxIconInformation)
												},
											},
											PushButton{
												Text: " Visual Address Card & URI",
												Font: Font{Bold: true},
												OnClicked: func() {
													promptVisualAddressCard(mw, activeAddress)
												},
											},
										},
									},
								},
							},

							GroupBox{
								Title:  "Network & Security Profile",
								Layout: Grid{Columns: 2, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Network Profile:"},
									Label{Text: strings.ToUpper(activeNetwork.Name) + " (" + activeNetwork.NetworkID + ")", Font: Font{Bold: true}},

									Label{Text: "Blockchain Height:"},
									Label{AssignTo: &lblHeight, Text: "Loading blocks..."},

									Label{Text: "Connected P2P Peers:"},
									Label{AssignTo: &lblPeers, Text: "0 peers"},

									Label{Text: "Current Difficulty Target:"},
									Label{AssignTo: &lblDifficulty, Text: "3"},

									Label{Text: "Mempool Pending Transactions:"},
									Label{AssignTo: &lblMempool, Text: "0 unconfirmed txs"},

									Label{Text: "Post-Quantum Cryptography:"},
									Label{Text: "ML-DSA-65 (FIPS 204)", Font: Font{Bold: true}},

									Label{Text: "Network ID Protocol:"},
									Label{AssignTo: &lblNetworkID, Text: activeNetwork.NetworkID},
								},
							},

							VSpacer{},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									PushButton{
										Text: " Refresh Status Now",
										OnClicked: func() {
											refreshNodeAndWallet(mw, lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblMiningDiff, sbHeight, sbPeers)
										},
									},
								},
							},
						},
					},

					// ==========================================
					// TAB 2: SEND
					// ==========================================
					{
						Title:  "Send ARMX",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							GroupBox{
								Title:  "Create & Sign Transaction",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Pay To (Bech32 Recipient Address, armx1...):"},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											LineEdit{
												AssignTo:  &leSendRecipient,
												CueBanner: "Paste recipient armx1... address here",
												Font:      Font{Family: "Consolas", PointSize: 9},
											},
											PushButton{
												Text: " Contacts",
												OnClicked: func() {
													if tabWidget != nil {
														_ = tabWidget.SetCurrentIndex(5)
													}
												},
											},
										},
									},
									VSpacer{Size: 6},
									Label{Text: "Amount (ARMX):"},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											LineEdit{
												AssignTo:  &leSendAmount,
												CueBanner: "e.g. 10.5",
												OnTextChanged: func() {
													updateSendTotalPreview()
												},
											},
											PushButton{
												Text: "Max Balance",
												OnClicked: func() {
													autoFee := wallet.EstimateMinMempoolFee(activeWallet, "", 0, 0)
													if currentBalance > autoFee {
														sendMax := currentBalance - autoFee
														leSendAmount.SetText(core.FormatArmxDecimal(sendMax))
														leSendFee.SetText(core.FormatArmxDecimal(autoFee))
														updateSendTotalPreview()
													}
												},
											},
										},
									},
									VSpacer{Size: 6},
									Label{Text: "Miner Transaction Fee (ARMX):"},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											LineEdit{
												AssignTo: &leSendFee,
												Text:     "0.00010000",
												OnTextChanged: func() {
													updateSendTotalPreview()
												},
											},
											PushButton{
												Text: " Fast",
												OnClicked: func() {
													leSendFee.SetText("0.00025000")
													updateSendTotalPreview()
												},
											},
											PushButton{
												Text: " Normal",
												OnClicked: func() {
													leSendFee.SetText("0.00010000")
													updateSendTotalPreview()
												},
											},
											PushButton{
												Text: " Economy",
												OnClicked: func() {
													leSendFee.SetText("0.00006000")
													updateSendTotalPreview()
												},
											},
										},
									},
									VSpacer{Size: 4},
									Label{
										AssignTo: &lblSendTotalPreview,
										Text:     "Total deduction: 0.00010000 ARMX",
										Font:     Font{Family: "Segoe UI", PointSize: 9, Bold: true},
									},
									VSpacer{Size: 6},
									Label{Text: "Wallet Passphrase (Optional, leave blank if plain keystore):"},
									LineEdit{
										AssignTo:     &leSendPassphrase,
										PasswordMode: true,
										CueBanner:    "Enter passphrase if wallet is encrypted",
									},
									VSpacer{Size: 10},
									PushButton{
										Text: " Send Transaction",
										Font: Font{Bold: true},
										OnClicked: func() {
											sendTransaction(mw, leSendRecipient, leSendAmount, leSendFee, leSendPassphrase, teSendResult)
										},
									},
								},
							},
							GroupBox{
								Title:  "Transaction Broadcast Status",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									TextEdit{
										AssignTo: &teSendResult,
										ReadOnly: true,
										Font:     Font{Family: "Consolas", PointSize: 9},
										VScroll:  true,
									},
								},
							},
						},
					},

					// ==========================================
					// TAB 3: RECEIVE
					// ==========================================
					{
						Title:  "Receive ARMX",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							GroupBox{
								Title:  "Payment Address & QR Barcode",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{
										Text: "Share this address or scan the QR barcode to receive ARMX:",
									},
									LineEdit{
										AssignTo: &leReceiveAddress,
										Text:     activeAddress,
										ReadOnly: true,
										Font:     Font{Family: "Consolas", PointSize: 11, Bold: true},
									},
									VSpacer{Size: 8},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											ImageView{
												AssignTo: &ivReceiveQR,
												Mode:     ImageViewModeCenter,
												MinSize:  Size{Width: 168, Height: 168},
												MaxSize:  Size{Width: 168, Height: 168},
											},
											HSpacer{Size: 12},
											Composite{
												Layout: VBox{MarginsZero: true},
												Children: []Widget{
													Label{
														Text: "Scan this QR code with any mobile scanner or Armax camera.",
														Font: Font{Bold: true},
													},
													VSpacer{Size: 4},
													Label{
														Text: "Payment URI (BIP-21 Compatible):",
													},
													LineEdit{
														AssignTo: &leReceiveURI,
														Text:     "armax:" + activeAddress + "?label=DesktopWallet",
														ReadOnly: true,
														Font:     Font{Family: "Consolas", PointSize: 9},
													},
													VSpacer{Size: 8},
													Composite{
														Layout: HBox{MarginsZero: true},
														Children: []Widget{
															PushButton{
																Text: " Copy Address",
																OnClicked: func() {
																	_ = walk.Clipboard().SetText(activeAddress)
																	walk.MsgBox(mw, "Copied", "Address copied to clipboard!", walk.MsgBoxIconInformation)
																},
															},
															PushButton{
																Text: " Copy Payment URI",
																OnClicked: func() {
																	uri := fmt.Sprintf("armax:%s?label=DesktopWallet", activeAddress)
																	_ = walk.Clipboard().SetText(uri)
																	walk.MsgBox(mw, "Copied", "Payment URI copied to clipboard!", walk.MsgBoxIconInformation)
																},
															},
															PushButton{
																Text: " Visual Address Card",
																Font: Font{Bold: true},
																OnClicked: func() {
																	promptVisualAddressCard(mw, activeAddress)
																},
															},
														},
													},
													VSpacer{Size: 6},
													Composite{
														Layout: HBox{MarginsZero: true},
														Children: []Widget{
															PushButton{
																Text: " Backup Keystore (JSON)",
																OnClicked: func() {
																	dlg := new(walk.FileDialog)
																	dlg.Title = "Save Wallet Keystore Backup"
																	dlg.Filter = "JSON Keystore (*.json)|*.json|All Files (*.*)|*.*"
																	if ok, err := dlg.ShowSave(mw); err != nil || !ok {
																		return
																	}
																	src := findWalletPath()
																	data, err := os.ReadFile(src)
																	if err != nil {
																		walk.MsgBox(mw, "Backup Error", fmt.Sprintf("Failed to read wallet keystore: %v", err), walk.MsgBoxIconError)
																		return
																	}
																	if err := os.WriteFile(dlg.FilePath, data, 0600); err != nil {
																		walk.MsgBox(mw, "Backup Error", fmt.Sprintf("Failed to save backup: %v", err), walk.MsgBoxIconError)
																		return
																	}
																	walk.MsgBox(mw, "Backup Success", "Wallet keystore successfully backed up to:\n"+dlg.FilePath, walk.MsgBoxIconInformation)
																},
															},
															PushButton{
																Text: " Create New Wallet",
																OnClicked: func() {
																	res := walk.MsgBox(mw, "Confirm", "Create a new wallet? Ensure your current wallet is backed up!", walk.MsgBoxOKCancel|walk.MsgBoxIconWarning)
																	if res == walk.DlgCmdOK {
																		newW, err := promptCreateWalletDialog(mw)
																		if err == nil && newW != nil {
																			activeWallet = newW
																			activeAddress = newW.Address
																			leReceiveAddress.SetText(activeAddress)
																			leOverviewAddress.SetText(activeAddress)
																			leMiningAddress.SetText(activeAddress)
																			if leReceiveURI != nil {
																				leReceiveURI.SetText("armax:" + activeAddress + "?label=DesktopWallet")
																			}
																			updateReceiveQRCode(activeAddress)
																			refreshNodeAndWallet(mw, lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblMiningDiff, sbHeight, sbPeers)
																		}
																	}
																},
															},
															HSpacer{},
														},
													},
												},
											},
										},
									},
								},
							},

							GroupBox{
								Title:  "Restore Wallet From 24-Word Seed Phrase",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Enter your 24 BIP-39 mnemonic words (separated by spaces):"},
									LineEdit{
										AssignTo:  &leRestoreMnemonic,
										CueBanner: "word1 word2 word3 ... word24",
										Font:      Font{Family: "Consolas", PointSize: 9},
									},
									VSpacer{Size: 6},
									PushButton{
										Text: " Restore Wallet",
										OnClicked: func() {
											words := strings.TrimSpace(leRestoreMnemonic.Text())
											if words == "" {
												walk.MsgBox(mw, "Error", "Please enter mnemonic seed words.", walk.MsgBoxIconError)
												return
											}
											restoredW, err := wallet.WalletFromMnemonic(words, "", 0)
											if err != nil {
												walk.MsgBox(mw, "Restore Error", fmt.Sprintf("Failed to restore: %v", err), walk.MsgBoxIconError)
												return
											}
											// Prompt for secure passphrase (min 12 characters) [L3-1]
											var passDlg *walk.Dialog
											var lePass1, lePass2 *walk.LineEdit
											var okPB, cancelPB *walk.PushButton
											var passphrase string
											err = Dialog{
												AssignTo:      &passDlg,
												Title:         "Encrypt Restored Wallet",
												MinSize:       Size{Width: 400, Height: 200},
												Layout:        VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}},
												DefaultButton: &okPB,
												CancelButton:  &cancelPB,
												Children: []Widget{
													Label{Text: "Choose a passphrase to encrypt your wallet (min 12 characters):", Font: Font{Bold: true}},
													VSpacer{Size: 6},
													Label{Text: "Passphrase:"},
													LineEdit{AssignTo: &lePass1, PasswordMode: true},
													Label{Text: "Confirm Passphrase:"},
													LineEdit{AssignTo: &lePass2, PasswordMode: true},
													VSpacer{Size: 10},
													Composite{
														Layout: HBox{MarginsZero: true},
														Children: []Widget{
															HSpacer{},
															PushButton{
																AssignTo: &okPB,
																Text:     "Encrypt & Save",
																OnClicked: func() {
																	p1 := lePass1.Text()
																	p2 := lePass2.Text()
																	if len(p1) < 12 {
																		walk.MsgBox(passDlg, "Error", "Passphrase must be at least 12 characters.", walk.MsgBoxIconError)
																		return
																	}
																	if p1 != p2 {
																		walk.MsgBox(passDlg, "Error", "Passphrases do not match.", walk.MsgBoxIconError)
																		return
																	}
																	passphrase = p1
																	passDlg.Accept()
																},
															},
															PushButton{AssignTo: &cancelPB, Text: "Cancel", OnClicked: func() { passDlg.Cancel() }},
														},
													},
												},
											}.Create(mw)
											if err != nil || passDlg.Run() != walk.DlgCmdOK {
												return
											}
											walletPath := findWalletPath()
											if err := wallet.EncryptAndSaveKeystore(restoredW, passphrase, walletPath); err != nil {
												walk.MsgBox(mw, "Save Error", fmt.Sprintf("Failed to save keystore to '%s': %v", walletPath, err), walk.MsgBoxIconError)
												return
											}
											restoredW.ClearMnemonic()
											activeWallet = restoredW
											activeAddress = restoredW.Address
											leReceiveAddress.SetText(activeAddress)
											leOverviewAddress.SetText(activeAddress)
											leMiningAddress.SetText(activeAddress)
											leRestoreMnemonic.SetText("")
											if leReceiveURI != nil {
												leReceiveURI.SetText("armax:" + activeAddress + "?label=DesktopWallet")
											}
											updateReceiveQRCode(activeAddress)
											walk.MsgBox(mw, "Wallet Restored", "Wallet successfully restored and encrypted!\nAddress: "+activeAddress, walk.MsgBoxIconInformation)
											refreshNodeAndWallet(mw, lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblMiningDiff, sbHeight, sbPeers)
										},
									},
								},
							},
						},
					},

					// ==========================================
					// TAB 4: MINING
					// ==========================================
					{
						Title:  "CPU Miner",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							GroupBox{
								Title:  "Mining Configuration",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Mining Reward Recipient Address:"},
									LineEdit{
										AssignTo: &leMiningAddress,
										Text:     activeAddress,
										Font:     Font{Family: "Consolas", PointSize: 9},
									},
									VSpacer{Size: 6},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											Label{Text: "CPU Threads:"},
											LineEdit{
												AssignTo: &leMiningThreads,
												Text:     strconv.Itoa(runtime.NumCPU()),
											},
											PushButton{
												Text: " Max Cores (" + strconv.Itoa(runtime.NumCPU()) + ")",
												OnClicked: func() {
													leMiningThreads.SetText(strconv.Itoa(runtime.NumCPU()))
												},
											},
											HSpacer{Size: 20},
											Label{Text: "Round Delay (ms):"},
											LineEdit{
												AssignTo: &leMiningDelay,
												Text:     "1000",
											},
											HSpacer{},
										},
									},
									VSpacer{Size: 8},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											PushButton{
												AssignTo: &btnStartMining,
												Text:     "Start Mining",
												Font:     Font{Bold: true},
												OnClicked: func() {
													startMiningLoop(mw, leMiningAddress, leMiningThreads, leMiningDelay,
														lblMiningStatus, lblMiningHashrate, lblMiningElapsed, lblMiningBlocks, lblMiningRewards, teMiningLog,
														btnStartMining, btnStopMining, sbMining)
												},
											},
											PushButton{
												AssignTo: &btnStopMining,
												Text:     "Stop Mining",
												Enabled:  false,
												OnClicked: func() {
													stopMiningLoop(lblMiningStatus, lblMiningHashrate, btnStartMining, btnStopMining, sbMining, teMiningLog)
												},
											},
										},
									},
								},
							},

							GroupBox{
								Title:  "Live Performance & Mining Statistics",
								Layout: Grid{Columns: 2, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Miner State:"},
									Label{AssignTo: &lblMiningStatus, Text: "IDLE (Stopped)", Font: Font{Bold: true}},

									Label{Text: "Current PoW Hashrate:"},
									Label{AssignTo: &lblMiningHashrate, Text: "0.00 H/s (IDLE)", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},

									Label{Text: "Active Mining Duration:"},
									Label{AssignTo: &lblMiningElapsed, Text: "00:00:00"},

									Label{Text: "Blocks Mined This Session:"},
									Label{AssignTo: &lblMiningBlocks, Text: "0 blocks"},

									Label{Text: "Session Rewards Earned:"},
									Label{AssignTo: &lblMiningRewards, Text: "0.00000000 ARMX", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},

									Label{Text: "Network Target Difficulty:"},
									Label{AssignTo: &lblMiningDiff, Text: "Difficulty 3 (LWMA-1 N=60)"},
								},
							},

							GroupBox{
								Title:  "Live Mining Log Output",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									TextEdit{
										AssignTo: &teMiningLog,
										ReadOnly: true,
										Font:     Font{Family: "Consolas", PointSize: 9},
										VScroll:  true,
									},
								},
							},
						},
					},

					// ==========================================
					// TAB 5: TRANSACTIONS
					// ==========================================
					{
						Title:  "Transactions",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									LineEdit{AssignTo: &leTxSearch, CueBanner: "Search TXID, sender, recipient or type", OnTextChanged: func() { applyTransactionFilter(mw, leTxSearch.Text()) }},
									PushButton{Text: "Clear", OnClicked: func() { leTxSearch.SetText(""); applyTransactionFilter(mw, "") }},
								},
							},
							TableView{
								AssignTo:      &tvTransactions,
								Model:         txModel,
								StretchFactor: 1,
								Font:          Font{Family: "Segoe UI", PointSize: 9},
								OnItemActivated: func() {
									idx := tvTransactions.CurrentIndex()
									if idx >= 0 && idx < len(txModel.items) {
										showTxDetailsDialog(mw, txModel.items[idx])
									}
								},
								Columns: []TableViewColumn{
									{Title: "Type", Width: 120},
									{Title: "Amount (ARMX)", Width: 150},
									{Title: "Block Height", Width: 95},
									{Title: "TXID / Hash", Width: 280},
									{Title: "Date & Time", Width: 160},
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									PushButton{Text: "First", OnClicked: func() { updateTransactionPage(mw, 0) }},
									PushButton{Text: "Previous", OnClicked: func() { updateTransactionPage(mw, txModel.page-1) }},
									PushButton{Text: "Next", OnClicked: func() { updateTransactionPage(mw, txModel.page+1) }},
									PushButton{Text: "Last", OnClicked: func() { updateTransactionPage(mw, 999999) }},
									HSpacer{Size: 10},
									Label{AssignTo: &txPageLabel, Text: "Page 1 / 1 -- 0 transactions total"},
									HSpacer{},
								},
							},
							VSpacer{Size: 6},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									PushButton{
										Text: " View Details (Double-Click)",
										Font: Font{Bold: true},
										OnClicked: func() {
											idx := tvTransactions.CurrentIndex()
											if idx >= 0 && idx < len(txModel.items) {
												showTxDetailsDialog(mw, txModel.items[idx])
											} else {
												walk.MsgBox(mw, "Select Transaction", "Please select a transaction row from the table first.", walk.MsgBoxIconInformation)
											}
										},
									},
									PushButton{
										Text: " Copy TXID",
										OnClicked: func() {
											idx := tvTransactions.CurrentIndex()
											if idx >= 0 && idx < len(txModel.items) {
												txid := txModel.items[idx].TxID
												_ = walk.Clipboard().SetText(txid)
												walk.MsgBox(mw, "Copied", "TXID copied: "+txid, walk.MsgBoxIconInformation)
											}
										},
									},
									PushButton{
										Text: " Export CSV",
										OnClicked: func() {
											exportTransactionsCSV(mw)
										},
									},
									HSpacer{},
									PushButton{
										Text: " Refresh History",
										OnClicked: func() {
											loadTransactionsHistory(mw)
										},
									},
								},
							},
						},
					},

					// ==========================================
					// TAB 6: ADDRESS BOOK
					// ==========================================
					{
						Title:  "Address Book",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							Label{
								Text: "Saved Armaxcoin Addresses (Contacts & Frequently Used Addresses):",
								Font: Font{Bold: true},
							},
							VSpacer{Size: 4},
							TableView{
								AssignTo:      &tvAddressBook,
								Model:         addressBookModel,
								StretchFactor: 1,
								Font:          Font{Family: "Segoe UI", PointSize: 9},
								OnItemActivated: func() {
									idx := tvAddressBook.CurrentIndex()
									if idx >= 0 && idx < len(addressBookModel.items) {
										c := addressBookModel.items[idx]
										leSendRecipient.SetText(c.Address)
										if tabWidget != nil {
											_ = tabWidget.SetCurrentIndex(1)
										}
									}
								},
								Columns: []TableViewColumn{
									{Title: "Label / Name", Width: 180},
									{Title: "Bech32 Address (armx1...)", Width: 460},
									{Title: "Date Added", Width: 150},
								},
							},
							VSpacer{Size: 6},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									PushButton{
										Text: " Add Contact",
										Font: Font{Bold: true},
										OnClicked: func() {
											promptAddContactDialog(mw)
										},
									},
									PushButton{
										Text: " Send to Contact",
										Font: Font{Bold: true},
										OnClicked: func() {
											idx := tvAddressBook.CurrentIndex()
											if idx >= 0 && idx < len(addressBookModel.items) {
												c := addressBookModel.items[idx]
												leSendRecipient.SetText(c.Address)
												if tabWidget != nil {
													_ = tabWidget.SetCurrentIndex(1)
												}
											} else {
												walk.MsgBox(mw, "Select Contact", "Please select a contact from the list first.", walk.MsgBoxIconInformation)
											}
										},
									},
									PushButton{
										Text: " Copy Address",
										OnClicked: func() {
											idx := tvAddressBook.CurrentIndex()
											if idx >= 0 && idx < len(addressBookModel.items) {
												c := addressBookModel.items[idx]
												_ = walk.Clipboard().SetText(c.Address)
												walk.MsgBox(mw, "Copied", "Address copied: "+c.Address, walk.MsgBoxIconInformation)
											}
										},
									},
									PushButton{
										Text: " Delete",
										OnClicked: func() {
											idx := tvAddressBook.CurrentIndex()
											if idx >= 0 && idx < len(addressBookModel.items) {
												c := addressBookModel.items[idx]
												if walk.MsgBox(mw, "Confirm Delete", fmt.Sprintf("Delete contact '%s'?", c.Name), walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
													addressBookLock.Lock()
													addressBookModel.items = append(addressBookModel.items[:idx], addressBookModel.items[idx+1:]...)
													addressBookLock.Unlock()
													_ = saveAddressBook()
													addressBookModel.PublishRowsReset()
												}
											}
										},
									},
									HSpacer{},
								},
							},
						},
					},

					// ==========================================
					// TAB 7: NODE CONSOLE & DIAGNOSTICS
					// ==========================================
					{
						Title:  "Node Diagnostics",
						Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}},
						Children: []Widget{
							GroupBox{
								Title:  "Core Daemon Status & P2P Metrics",
								Layout: Grid{Columns: 2, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Label{Text: "Daemon RPC Connection:"},
									Label{AssignTo: &lblDiagDaemonStatus, Text: "Checking daemon connection...", Font: Font{Bold: true}},

									Label{Text: "Network Profile ID:"},
									Label{Text: strings.ToUpper(activeNetwork.Name) + " (" + activeNetwork.NetworkID + ")", Font: Font{Bold: true}},

									Label{Text: "Blockchain Tip Height:"},
									Label{AssignTo: &lblDiagHeight, Text: "Loading height..."},

									Label{Text: "Latest Block Hash:"},
									Label{AssignTo: &lblDiagBestHash, Text: "Loading tip hash...", Font: Font{Family: "Consolas", PointSize: 9}},

									Label{Text: "Connected P2P Mesh:"},
									Label{AssignTo: &lblDiagPeers, Text: "0 active peers"},

									Label{Text: "Mempool Pending Queue:"},
									Label{AssignTo: &lblDiagMempool, Text: "0 unconfirmed txs"},

									Label{Text: "Target Difficulty:"},
									Label{AssignTo: &lblDiagDifficulty, Text: "Difficulty 3 (LWMA-1 N=60)"},

									Label{Text: "Circulating Supply:"},
									Label{AssignTo: &lblDiagSupply, Text: "0.00000000 ARMX"},

									Label{Text: "P2P Transport Security:"},
									Label{Text: "TLS 1.3 Active (TOFU Certificate Pinning Enforced)", Font: Font{Bold: true}},

									Label{Text: "Sybil / Eclipse Defense:"},
									Label{Text: "Subnet Limiter Active (Max 3 peers per /24 subnet)", Font: Font{Bold: true}},
								},
							},
							GroupBox{
								Title:  "Interactive RPC Command Console",
								Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
								Children: []Widget{
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											PushButton{Text: "Full /stats", OnClicked: func() { queryNodeDiagnostics(teNodeConsole) }},
											PushButton{Text: "Active /peers", OnClicked: func() { queryNodePeers(teNodeConsole) }},
											PushButton{Text: "/chain Info", OnClicked: func() { queryNodeChain(teNodeConsole) }},
											PushButton{Text: "/mempool Info", OnClicked: func() { queryNodeMempool(teNodeConsole) }},
											HSpacer{},
											PushButton{Text: "Clear Output", OnClicked: func() { teNodeConsole.SetText("") }},
										},
									},
									VSpacer{Size: 6},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											LineEdit{
												AssignTo:  &leNodeCommand,
												CueBanner: "Commands: help, getnetworkinfo, getblockchaininfo, getpeerinfo, getmempoolinfo, getbalance [addr], gethistory [addr]",
											},
											PushButton{
												Text:      "Run Command",
												Font:      Font{Bold: true},
												OnClicked: func() { runGUICommand(mw, leNodeCommand, teNodeConsole) },
											},
										},
									},
									VSpacer{Size: 6},
									TextEdit{
										AssignTo: &teNodeConsole,
										ReadOnly: true,
										Font:     Font{Family: "Consolas", PointSize: 9},
										VScroll:  true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if appIcon != nil {
		mwDecl.Icon = appIcon
	}
	fmt.Println("[GUI] Initializing MainWindow...")
	if err := mwDecl.Create(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL GUI Creation Error: %v\n", err)
		walk.MsgBox(nil, "FATAL GUI Creation Error", err.Error(), walk.MsgBoxIconError)
		os.Exit(1)
	}
	fmt.Println("[GUI] MainWindow created successfully. Entering event loop...")

	// Initial data sync
	updateReceiveQRCode(activeAddress)
	refreshNodeAndWallet(mw, lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblMiningDiff, sbHeight, sbPeers)
	loadTransactionsHistory(mw)
	queryNodeDiagnostics(teNodeConsole)

	// Automated background periodic poller every 3 seconds
	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			if mw != nil {
				refreshNodeAndWallet(mw, lblBalanceMain, lblBalanceArmoids, lblHeight, lblPeers, lblDifficulty, lblMempool, lblMiningDiff, sbHeight, sbPeers)
			}
		}
	}()

	mw.Run()
}

func refreshNodeAndWallet(mw *walk.MainWindow, lblBal, lblArmoids, lblH, lblP, lblDiff, lblMem, lblMinDiff *walk.Label, sbH, sbP *walk.StatusBarItem) {
	if !atomic.CompareAndSwapInt32(&refreshBusy, 0, 1) {
		return
	}

	go func() {
		defer atomic.StoreInt32(&refreshBusy, 0)

		// 1. Fetch Node /stats
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(rpcURL + "/stats")
		var stats StatsResponse
		hasStats := false
		if err == nil && resp.StatusCode == 200 {
			if json.NewDecoder(resp.Body).Decode(&stats) == nil {
				hasStats = true
			}
			_ = resp.Body.Close()
		}

		peerCount := 0
		if hasStats {
			peerCount = stats.PeersActive
			if peerCount < 0 {
				peerCount = 0
			}
		}

		mempoolCount := 0
		if hasStats {
			mempoolCount = stats.MempoolSize
			if mempoolCount == 0 && stats.Mempool > 0 {
				mempoolCount = stats.Mempool
			}
		}

		// 2. Fetch Wallet /balance
		var bal BalanceResponse
		hasBal := false
		if activeAddress != "" {
			bResp, bErr := client.Get(rpcURL + "/balance?address=" + activeAddress)
			if bErr == nil && bResp.StatusCode == 200 {
				if json.NewDecoder(bResp.Body).Decode(&bal) == nil {
					hasBal = true
					currentBalance = bal.GetSpendable()
				}
				_ = bResp.Body.Close()
			}
		}

		mw.Synchronize(func() {
			if hasBal {
				if lblBal != nil {
					lblBal.SetText(formatArmx(bal.GetSpendable()))
				}
				if lblArmoids != nil {
					confirmed := bal.GetConfirmed()
					spendable := bal.GetSpendable()
					immature := bal.ImmatureBalance
					pending := bal.PendingBalance

					if immature > 0 {
						mat := bal.CoinbaseMaturity
						if mat <= 0 {
							mat = activeNetwork.CoinbaseMaturity
							if mat <= 0 {
								mat = 100
							}
						}
						lblArmoids.SetText(fmt.Sprintf("Spendable: %s | Immature Rewards: %s (<%d blocks) | Total: %s",
							formatArmx(spendable), formatArmx(immature), mat, formatArmx(confirmed)))
					} else if pending == confirmed {
						lblArmoids.SetText(fmt.Sprintf("Confirmed: %s (100%% Settled) | Available: %s",
							formatArmx(confirmed), formatArmx(pending)))
					} else if pending < confirmed {
						diff := confirmed - pending
						lblArmoids.SetText(fmt.Sprintf("Confirmed: %s | Available: %s (-%s in Mempool)",
							formatArmx(confirmed), formatArmx(pending), formatArmx(diff)))
					} else {
						diff := pending - confirmed
						lblArmoids.SetText(fmt.Sprintf("Confirmed: %s | Available: %s (+%s Pending)",
							formatArmx(confirmed), formatArmx(pending), formatArmx(diff)))
					}
				}
			}

			if hasStats {
				if lblH != nil {
					if stats.Blocks > 0 {
						lblH.SetText(fmt.Sprintf("%d (%d blocks)", stats.Height, stats.Blocks))
					} else {
						lblH.SetText(fmt.Sprintf("%d blocks", stats.Height))
					}
				}
				if lblP != nil {
					lblP.SetText(fmt.Sprintf("%d active peers", peerCount))
				}
				if lblDiff != nil {
					lblDiff.SetText(fmt.Sprintf("Difficulty %d (LWMA-1)", stats.Difficulty))
				}
				if lblMinDiff != nil {
					lblMinDiff.SetText(fmt.Sprintf("Difficulty %d (LWMA-1 N=60)", stats.Difficulty))
				}
				if lblMem != nil {
					lblMem.SetText(fmt.Sprintf("%d unconfirmed txs", mempoolCount))
				}
				if sbH != nil {
					sbH.SetText(fmt.Sprintf("Height: %d", stats.Height))
				}
				if sbP != nil {
					sbP.SetText(fmt.Sprintf("Peers: %d", peerCount))
				}

				// Update Tab 7 Node Diagnostics labels
				if lblDiagDaemonStatus != nil {
					lblDiagDaemonStatus.SetText(fmt.Sprintf("Connected & Healthy (%s)", rpcURL))
				}
				if lblDiagHeight != nil {
					if stats.Blocks > 0 {
						lblDiagHeight.SetText(fmt.Sprintf("%d (%d blocks)", stats.Height, stats.Blocks))
					} else {
						lblDiagHeight.SetText(fmt.Sprintf("%d blocks", stats.Height))
					}
				}
				if lblDiagBestHash != nil {
					h := stats.LatestHash
					if h == "" {
						h = "0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b (Genesis)"
					}
					lblDiagBestHash.SetText(h)
				}
				if lblDiagPeers != nil {
					lblDiagPeers.SetText(fmt.Sprintf("%d active / %d known peers", stats.PeersActive, stats.PeersKnown))
				}
				if lblDiagDifficulty != nil {
					lblDiagDifficulty.SetText(fmt.Sprintf("Target Difficulty %d (LWMA-1 N=60)", stats.Difficulty))
				}
				if lblDiagMempool != nil {
					lblDiagMempool.SetText(fmt.Sprintf("%d unconfirmed transactions", mempoolCount))
				}
				if lblDiagSupply != nil {
					lblDiagSupply.SetText(fmt.Sprintf("%.8f ARMX", stats.CirculatingSupply))
				}

				// If block height changed, mempool changed, or confirmed balance changed, refresh transaction table
				shouldReloadTx := false
				if stats.Height != lastMinedHeight {
					lastMinedHeight = stats.Height
					shouldReloadTx = true
				}
				if mempoolCount != lastMempoolCount {
					lastMempoolCount = mempoolCount
					shouldReloadTx = true
				}
				if hasBal && bal.GetConfirmed() != lastBalanceAmount {
					lastBalanceAmount = bal.GetConfirmed()
					shouldReloadTx = true
				}
				if shouldReloadTx {
					loadTransactionsHistory(mw)
				}
			} else {
				if lblDiagDaemonStatus != nil {
					lblDiagDaemonStatus.SetText("Daemon Unreachable / Offline (" + rpcURL + ")")
				}
			}
		})
	}()
}

func applyTransactionFilter(mw *walk.MainWindow, query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]*TxItem, 0, len(txModel.allItems))
	for _, item := range txModel.allItems {
		if item == nil {
			continue
		}
		if query == "" {
			filtered = append(filtered, item)
			continue
		}
		sender, recipient := "", ""
		if item.Record != nil {
			sender = item.Record.Tx.Sender
			recipient = item.Record.Tx.Recipient
		}
		searchable := strings.ToLower(item.TxID + " " + item.Type + " " + sender + " " + recipient + " " + item.Timestamp)
		if strings.Contains(searchable, query) {
			filtered = append(filtered, item)
		}
	}
	txModel.filteredItems = filtered
	// Retain the current page if it is still within the valid range
	updateTransactionPage(mw, txModel.page)
}

func updateTransactionPage(mw *walk.MainWindow, page int) {
	if txModel.pageSize <= 0 {
		txModel.pageSize = 25
	}
	if txModel.filteredItems == nil {
		txModel.filteredItems = append([]*TxItem(nil), txModel.allItems...)
	}
	total := len(txModel.filteredItems)
	pages := (total + txModel.pageSize - 1) / txModel.pageSize
	if pages < 1 {
		pages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	txModel.page = page
	start := page * txModel.pageSize
	end := start + txModel.pageSize
	if end > total {
		end = total
	}
	if start > total {
		start = total
	}
	txModel.items = append([]*TxItem(nil), txModel.filteredItems[start:end]...)
	txModel.PublishRowsReset()
	if tvTransactions != nil {
		_ = tvTransactions.Invalidate()
	}
	if txPageLabel != nil {
		txPageLabel.SetText(fmt.Sprintf("Page %d / %d -- %d matching / %d total loaded", page+1, pages, total, len(txModel.allItems)))
	}
}

func loadTransactionsHistory(mw *walk.MainWindow) {
	if activeAddress == "" {
		return
	}
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(rpcURL + "/history?address=" + activeAddress)
		if err != nil || resp.StatusCode != 200 {
			return
		}
		defer resp.Body.Close()

		var records []TxHistoryRecord
		if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
			return
		}

		var items []*TxItem
		for i, r := range records {
			txType := "Received"
			if r.Tx.Sender == "COINBASE" || r.Tx.Sender == "SYSTEM" {
				txType = "Mined (Coinbase)"
			} else if core.MatchAddress(r.Tx.Sender, activeAddress) {
				txType = "Sent"
			}

			hStr := "Pending"
			if r.BlockIndex >= 0 {
				confs := 0
				if lastMinedHeight >= r.BlockIndex {
					confs = lastMinedHeight - r.BlockIndex + 1
				}
				if txType == "Mined (Coinbase)" {
					mat := activeNetwork.CoinbaseMaturity
					if mat <= 0 {
						mat = 100
					}
					if confs < mat {
						hStr = fmt.Sprintf("#%d (%d/%d blks)", r.BlockIndex, confs, mat)
					} else {
						hStr = fmt.Sprintf("#%d (Mature)", r.BlockIndex)
					}
				} else {
					if confs < 6 {
						hStr = fmt.Sprintf("#%d (%d/6 confs)", r.BlockIndex, confs)
					} else if confs < 12 {
						hStr = fmt.Sprintf("#%d (%d confs)", r.BlockIndex, confs)
					} else {
						hStr = fmt.Sprintf("#%d (Finalized)", r.BlockIndex)
					}
				}
			}

			tStr := time.Unix(r.Timestamp, 0).Format("2006-01-02 15:04:05")
			txid := r.Tx.HashHex()

			recCopy := r
			items = append(items, &TxItem{
				Index:     i,
				Type:      txType,
				Amount:    formatArmx(r.Tx.Amount),
				Height:    hStr,
				TxID:      txid,
				Timestamp: tStr,
				Record:    &recCopy,
			})
		}

		mw.Synchronize(func() {
			txModel.allItems = items
			if txModel.pageSize <= 0 {
				txModel.pageSize = 25
			}
			query := ""
			if leTxSearch != nil {
				query = leTxSearch.Text()
			}
			applyTransactionFilter(mw, query)
			if tvTransactions != nil {
				_ = tvTransactions.Invalidate()
			}
		})
	}()
}

func sendTransaction(mw *walk.MainWindow, leTo, leAmt, leFee, lePass *walk.LineEdit, teResult *walk.TextEdit) {
	to := strings.TrimSpace(leTo.Text())
	amtStr := strings.TrimSpace(leAmt.Text())
	feeStr := strings.TrimSpace(leFee.Text())
	pass := strings.TrimSpace(lePass.Text())

	if to == "" {
		walk.MsgBox(mw, "Error", "Recipient address cannot be empty.", walk.MsgBoxIconError)
		return
	}
	if !core.ValidateAddress(to) {
		walk.MsgBox(mw, "Invalid Address", "Recipient address must be a valid Bech32 address (armx1...)", walk.MsgBoxIconError)
		return
	}

	amtArmoids, err := core.ParseArmxDecimal(amtStr)
	if err != nil || amtArmoids <= 0 {
		walk.MsgBox(mw, "Invalid Amount", fmt.Sprintf("Invalid ARMX amount: %v", err), walk.MsgBoxIconError)
		return
	}

	var feeArmoids int64 = 0
	if feeStr != "" {
		if parsedFee, err := core.ParseArmxDecimal(feeStr); err == nil {
			feeArmoids = parsedFee
		}
	}

	// Obtain sender signing wallet
	var signingWallet *wallet.Wallet
	if activeWallet != nil && activeWallet.PrivateKey != nil {
		signingWallet = activeWallet
	} else {
		w, err := wallet.DecryptAndLoadKeystore(pass, findWalletPath())
		if err != nil {
			walk.MsgBox(mw, "Keystore Error", fmt.Sprintf("Failed to decrypt keystore with passphrase: %v", err), walk.MsgBoxIconError)
			return
		}
		signingWallet = w
	}

	// Automatic fee estimation based on signed ML-DSA-65 transaction byte size
	autoFee := wallet.EstimateMinMempoolFee(signingWallet, to, amtArmoids, 0)
	if feeArmoids < autoFee {
		feeArmoids = autoFee
		leFee.SetText(core.FormatArmxDecimal(feeArmoids))
	}

	totalRequired := amtArmoids + feeArmoids
	if totalRequired > currentBalance {
		walk.MsgBox(mw, "Insufficient Balance",
			fmt.Sprintf("Total required: %s\nAvailable balance: %s", formatArmx(totalRequired), formatArmx(currentBalance)),
			walk.MsgBoxIconError)
		return
	}

	confirmMsg := fmt.Sprintf("Confirm sending:\n\nAmount: %s\nFee: %s (automatic min fee)\nTotal: %s\nTo: %s",
		formatArmx(amtArmoids), formatArmx(feeArmoids), formatArmx(totalRequired), to)
	if walk.MsgBox(mw, "Confirm Transaction", confirmMsg, walk.MsgBoxOKCancel|walk.MsgBoxIconQuestion) != walk.DlgCmdOK {
		return
	}

	go func() {
		// Query next nonce
		client := http.Client{Timeout: 5 * time.Second}
		bResp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", rpcURL, signingWallet.Address))
		if err != nil {
			mw.Synchronize(func() {
				walk.MsgBox(mw, "RPC Error", fmt.Sprintf("Failed to query nonce: %v", err), walk.MsgBoxIconError)
			})
			return
		}
		defer bResp.Body.Close()
		var bal BalanceResponse
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
			mw.Synchronize(func() {
				walk.MsgBox(mw, "Sign Error", fmt.Sprintf("Post-quantum signature creation failed: %v", err), walk.MsgBoxIconError)
			})
			return
		}

		txData, _ := json.Marshal(tx)
		postResp, err := client.Post(rpcURL+"/tx/new", "application/json", bytes.NewBuffer(txData))
		if err != nil {
			mw.Synchronize(func() {
				walk.MsgBox(mw, "Broadcast Error", fmt.Sprintf("Failed to broadcast transaction: %v", err), walk.MsgBoxIconError)
			})
			return
		}
		defer postResp.Body.Close()
		bodyBytes, _ := io.ReadAll(postResp.Body)

		mw.Synchronize(func() {
			if postResp.StatusCode == 200 {
				txid := tx.HashHex()
				msg := fmt.Sprintf("[OK] Transaction Successfully Broadcast to Mempool!\nTXID: %s\nNonce: #%d\nAmount: %s\nResponse: %s",
					txid, txNonce, formatArmx(amtArmoids), string(bodyBytes))
				teResult.SetText(msg)
				leTo.SetText("")
				leAmt.SetText("")
				walk.MsgBox(mw, "Success", "Transaction broadcasted successfully!\nTXID: "+txid, walk.MsgBoxIconInformation)
				loadTransactionsHistory(mw)
			} else {
				teResult.SetText(fmt.Sprintf("[-] Broadcast Rejected (HTTP %d): %s", postResp.StatusCode, string(bodyBytes)))
				walk.MsgBox(mw, "Rejected", string(bodyBytes), walk.MsgBoxIconError)
			}
		})
	}()
}

func startMiningLoop(mw *walk.MainWindow, leAddr, leThreads, leDelay *walk.LineEdit,
	lblStat, lblHash, lblElapsed, lblBlks, lblRwd *walk.Label, teLog *walk.TextEdit,
	btnStart, btnStop *walk.PushButton, sbMining *walk.StatusBarItem) {

	miningLock.Lock()
	if miningActive {
		miningLock.Unlock()
		return
	}
	miningActive = true
	miningStopChan = make(chan struct{})
	miningLock.Unlock()

	minerAddr := strings.TrimSpace(leAddr.Text())
	if minerAddr == "" {
		minerAddr = activeAddress
	}
	if !core.ValidateAddress(minerAddr) {
		walk.MsgBox(mw, "Invalid Address", "Mining recipient address must be a valid Bech32 address.", walk.MsgBoxIconError)
		miningLock.Lock()
		miningActive = false
		miningLock.Unlock()
		return
	}

	delayMs, _ := strconv.Atoi(strings.TrimSpace(leDelay.Text()))
	if delayMs < 100 {
		delayMs = 1000
	}

	threads, _ := strconv.Atoi(strings.TrimSpace(leThreads.Text()))
	if threads < 1 {
		threads = 1
	}

	btnStart.SetEnabled(false)
	btnStop.SetEnabled(true)
	lblStat.SetText(fmt.Sprintf("MINING ACTIVE (%d threads)", threads))
	sbMining.SetText("Miner: MINING...")

	appendLog := func(msg string) {
		ts := time.Now().Format("15:04:05")
		line := fmt.Sprintf("[%s] %s\r\n", ts, msg)
		current := teLog.Text()
		if len(current) > 30000 {
			current = current[len(current)-20000:]
			teLog.SetText(current + line)
		} else {
			teLog.AppendText(line)
		}
		teLog.ScrollToCaret()
	}

	appendLog(fmt.Sprintf("[*] CPU Mining started! Threads: %d, Delay: %dms, Address: %s", threads, delayMs, minerAddr))

	// Load auth cookie if available
	var authUser, authPass string
	cookieCandidates := []string{node.GetRPCAuthCookiePath()}
	for _, cand := range cookieCandidates {
		if data, err := os.ReadFile(cand); err == nil {
			parts := strings.Split(strings.TrimSpace(string(data)), ":")
			if len(parts) == 2 {
				authUser = parts[0]
				authPass = parts[1]
				break
			}
		}
	}

	// 1-second UI updater ticker for live Hashrate and Mining Duration
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		client := http.Client{Timeout: 800 * time.Millisecond}

		for {
			select {
			case <-miningStopChan:
				return
			case t := <-ticker.C:
				elapsed := t.Sub(startTime)
				h := int(elapsed.Hours())
				m := int(elapsed.Minutes()) % 60
				s := int(elapsed.Seconds()) % 60
				elapsedStr := fmt.Sprintf("%02d:%02d:%02d", h, m, s)

				// Query live node hashrate from /stats
				var liveHps float64 = 0
				resp, err := client.Get(rpcURL + "/stats")
				if err == nil && resp.StatusCode == 200 {
					var stats StatsResponse
					if json.NewDecoder(resp.Body).Decode(&stats) == nil {
						liveHps = stats.MiningHashrate
					}
					resp.Body.Close()
				}

				// If node reports hashrate, display it; otherwise calibrate local throughput
				hashrateStr := ""
				if liveHps > 0 {
					hashrateStr = formatHashrate(liveHps)
				} else {
					estHps := measureCPUMiningThroughput(threads)
					hashrateStr = fmt.Sprintf("~%s (local est.)", formatHashrate(estHps))
				}

				mw.Synchronize(func() {
					if lblElapsed != nil {
						lblElapsed.SetText(elapsedStr)
					}
					if lblHash != nil {
						lblHash.SetText(hashrateStr)
					}
					if sbMining != nil {
						sbMining.SetText(fmt.Sprintf("Miner: %s", hashrateStr))
					}
				})
			}
		}
	}()

	// Coordinated mining worker: sends single request with requested threads to daemon
	go func() {
		client := &http.Client{}
		mineURL := fmt.Sprintf("%s/mine", rpcURL)

		for {
			select {
			case <-miningStopChan:
				return
			default:
			}

			payload := map[string]interface{}{
				"miner":   minerAddr,
				"threads": threads,
			}
			postPayload, _ := json.Marshal(payload)

			reqCtx, cancelReq := context.WithTimeout(context.Background(), 15*time.Minute)
			go func() {
				select {
				case <-miningStopChan:
					cancelReq()
				case <-reqCtx.Done():
				}
			}()

			req, err := http.NewRequestWithContext(reqCtx, "POST", mineURL, bytes.NewBuffer(postPayload))
			if err != nil {
				cancelReq()
				time.Sleep(1 * time.Second)
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			if authUser != "" && authPass != "" {
				req.SetBasicAuth(authUser, authPass)
			}

			resp, err := client.Do(req)
			cancelReq()
			if err != nil {
				select {
				case <-miningStopChan:
					return
				default:
					time.Sleep(2 * time.Second)
					continue
				}
			}

			type MResp struct {
				Status  string      `json:"status"`
				Message string      `json:"message"`
				Block   *core.Block `json:"block"`
			}
			var mResult MResp
			_ = json.NewDecoder(resp.Body).Decode(&mResult)
			resp.Body.Close()

			if resp.StatusCode == 200 && mResult.Status == "success" {
				sessionBlocks++
				rewardAmt := int64(60 * core.CoinUnit)
				if mResult.Block != nil && len(mResult.Block.Transactions) > 0 {
					rewardAmt = mResult.Block.Transactions[0].Amount
				}
				sessionReward += rewardAmt

				blkIdx := 0
				if mResult.Block != nil {
					blkIdx = mResult.Block.Index
				}

				hashPrefix := ""
				if mResult.Block != nil && len(mResult.Block.Hash) > 16 {
					hashPrefix = " | hash: " + mResult.Block.Hash[:16] + "..."
				}

				mw.Synchronize(func() {
					appendLog(fmt.Sprintf("[OK] BLOCK FOUND! #%d%s | Reward: %s credited", blkIdx, hashPrefix, formatArmx(rewardAmt)))
					lblBlks.SetText(fmt.Sprintf("%d blocks", sessionBlocks))
					lblRwd.SetText(formatArmx(sessionReward))
					sbMining.SetText(fmt.Sprintf("Miner: %d BLOCKS", sessionBlocks))
					loadTransactionsHistory(mw)
				})
			} else {
				if mResult.Message != "" {
					mw.Synchronize(func() {
						appendLog(fmt.Sprintf("[-] Mining notice (HTTP %d): %s", resp.StatusCode, mResult.Message))
					})
				}
			}

			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}()
}

func stopMiningLoop(lblStat, lblHash *walk.Label, btnStart, btnStop *walk.PushButton, sbMining *walk.StatusBarItem, teLog *walk.TextEdit) {
	miningLock.Lock()
	if !miningActive {
		miningLock.Unlock()
		return
	}
	miningActive = false
	if miningStopChan != nil {
		close(miningStopChan)
	}
	miningLock.Unlock()

	btnStart.SetEnabled(true)
	btnStop.SetEnabled(false)
	lblStat.SetText("IDLE (Stopped)")
	if lblHash != nil {
		lblHash.SetText("0.00 H/s (IDLE)")
	}
	sbMining.SetText("Miner: IDLE")

	ts := time.Now().Format("15:04:05")
	teLog.AppendText(fmt.Sprintf("[%s] [*] CPU Mining stopped by user.\r\n", ts))
	teLog.ScrollToCaret()
}

func runGUICommand(owner walk.Form, input *walk.LineEdit, output *walk.TextEdit) {
	fields := strings.Fields(strings.TrimSpace(input.Text()))
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	if cmd == "clear" {
		output.SetText("")
		return
	}
	if cmd == "help" {
		output.SetText("ARMAX GUI CLI (read-only commands)\nhelp\ngetnetworkinfo\ngetblockchaininfo\ngetpeerinfo\ngetbalance [address]\ngethistory [address]\ngetmempoolinfo\nclear\n\nCommands query the configured node; they do not execute OS shell commands.")
		return
	}
	path := ""
	switch cmd {
	case "getnetworkinfo", "getmempoolinfo", "getblockchaininfo":
		path = "/stats"
	case "getpeerinfo":
		path = "/peers"
	case "getbalance":
		addr := activeAddress
		if len(fields) > 1 {
			addr = fields[1]
		}
		if !core.ValidateAddress(addr) {
			output.SetText("Invalid address. Usage: getbalance [armx1... address]")
			return
		}
		path = "/balance?address=" + url.QueryEscape(addr)
	case "gethistory":
		addr := activeAddress
		if len(fields) > 1 {
			addr = fields[1]
		}
		if !core.ValidateAddress(addr) {
			output.SetText("Invalid address. Usage: gethistory [armx1... address]")
			return
		}
		path = "/history?address=" + url.QueryEscape(addr)
	default:
		output.SetText("Unknown command. Type help for supported commands.")
		return
	}
	go func() {
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(strings.TrimRight(rpcURL, "/") + path)
		if err != nil {
			output.Synchronize(func() { output.SetText("CLI error: " + err.Error()) })
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		result := fmt.Sprintf("$ %s\nHTTP %d\n%s", strings.TrimSpace(input.Text()), resp.StatusCode, string(data))
		output.Synchronize(func() { output.SetText(result) })
	}()
}

func queryNodeDiagnostics(teConsole *walk.TextEdit) {
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(rpcURL + "/stats")
		if err != nil {
			teConsole.Synchronize(func() {
				teConsole.SetText("[-] Error querying /stats: " + err.Error())
			})
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)

		var pretty bytes.Buffer
		_ = json.Indent(&pretty, data, "", "  ")

		pResp, pErr := client.Get(rpcURL + "/peers")
		peersStr := "[]"
		if pErr == nil {
			pData, _ := io.ReadAll(pResp.Body)
			pResp.Body.Close()
			var pPretty bytes.Buffer
			if json.Indent(&pPretty, pData, "", "  ") == nil {
				peersStr = pPretty.String()
			}
		}

		output := fmt.Sprintf("=== NODE /stats ===\r\n%s\r\n\r\n=== CONNECTED /peers ===\r\n%s\r\n", pretty.String(), peersStr)
		teConsole.Synchronize(func() {
			teConsole.SetText(output)
		})
	}()
}

func queryNodePeers(teConsole *walk.TextEdit) {
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		pResp, pErr := client.Get(rpcURL + "/peers")
		if pErr != nil {
			teConsole.Synchronize(func() {
				teConsole.SetText("[-] Error querying /peers: " + pErr.Error())
			})
			return
		}
		defer pResp.Body.Close()
		pData, _ := io.ReadAll(pResp.Body)
		var pPretty bytes.Buffer
		_ = json.Indent(&pPretty, pData, "", "  ")
		output := fmt.Sprintf("=== CONNECTED PEERS (%s) ===\r\n%s\r\n", time.Now().Format("15:04:05"), pPretty.String())
		teConsole.Synchronize(func() {
			teConsole.SetText(output)
		})
	}()
}

func queryNodeChain(teConsole *walk.TextEdit) {
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		cResp, cErr := client.Get(rpcURL + "/chain")
		if cErr != nil {
			teConsole.Synchronize(func() {
				teConsole.SetText("[-] Error querying /chain: " + cErr.Error())
			})
			return
		}
		defer cResp.Body.Close()
		cData, _ := io.ReadAll(io.LimitReader(cResp.Body, 1<<20))
		var cPretty bytes.Buffer
		_ = json.Indent(&cPretty, cData, "", "  ")
		output := fmt.Sprintf("=== BLOCKCHAIN RECENT HEADERS (%s) ===\r\n%s\r\n", time.Now().Format("15:04:05"), cPretty.String())
		teConsole.Synchronize(func() {
			teConsole.SetText(output)
		})
	}()
}

func queryNodeMempool(teConsole *walk.TextEdit) {
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		mResp, mErr := client.Get(rpcURL + "/mempool")
		if mErr != nil {
			teConsole.Synchronize(func() {
				teConsole.SetText("[-] Error querying /mempool: " + mErr.Error())
			})
			return
		}
		defer mResp.Body.Close()
		mData, _ := io.ReadAll(io.LimitReader(mResp.Body, 1<<20))
		var mPretty bytes.Buffer
		_ = json.Indent(&mPretty, mData, "", "  ")
		output := fmt.Sprintf("=== UNCONFIRMED MEMPOOL TRANSACTIONS (%s) ===\r\n%s\r\n", time.Now().Format("15:04:05"), mPretty.String())
		teConsole.Synchronize(func() {
			teConsole.SetText(output)
		})
	}()
}
