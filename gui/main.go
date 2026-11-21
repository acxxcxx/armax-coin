//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
	"github.com/armaxcoincore/armax-coin/wallet"
)

var activeNetwork core.NetworkProfile

func main() {
	activeNetwork = core.GetActiveNetwork()
	runGUIServerMain()
}

// =========================================================================
// Shared Data Models, Formatters & Pre-Sign Helpers (Covered by gui_test.go)
// =========================================================================

// TxItem represents a formatted transaction row for UI models.
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
	items []*TxItem
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

// Contact represents a saved address book contact.
type Contact struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	DateAdded string `json:"date_added"`
}

type AddressBookModel struct {
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

// SessionMinedModel exposes tracked mined block confirmations and orphan reconciliations.
type SessionMinedModel struct {
	items []SessionMinedBlock
}

func (m *SessionMinedModel) RowCount() int {
	return len(m.items)
}

func (m *SessionMinedModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	e := m.items[row]
	switch col {
	case 0:
		return fmt.Sprintf("#%d", e.Height)
	case 1:
		if len(e.Hash) > 16 {
			return e.Hash[:16] + "..."
		}
		return e.Hash
	case 2:
		return formatArmx(e.Reward)
	case 3:
		return fmt.Sprintf("%d", e.PeersAtMine)
	case 4:
		mat := e.Maturity
		if mat <= 0 {
			mat = 100
		}
		return fmt.Sprintf("%d/%d", e.Confirmations, mat)
	case 5:
		return string(e.Status)
	}
	return ""
}

// PeerInspectorRow represents a single privacy-filtered P2P peer row.
type PeerInspectorRow struct {
	Address string
	Role    string
	Status  string
}

type PeerInspectorModel struct {
	items []PeerInspectorRow
}

func (m *PeerInspectorModel) RowCount() int {
	return len(m.items)
}

func (m *PeerInspectorModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	p := m.items[row]
	switch col {
	case 0:
		return p.Address
	case 1:
		return p.Role
	case 2:
		return p.Status
	}
	return ""
}

func isSeedEndpoint(endpoint string, seeds []string) bool {
	ep := strings.TrimSpace(endpoint)
	for _, s := range seeds {
		sc := strings.TrimSpace(s)
		if strings.EqualFold(sc, ep) {
			return true
		}
		if host, _, err := net.SplitHostPort(sc); err == nil && host != "" && strings.EqualFold(host, ep) {
			return true
		}
	}
	return false
}

// parseSafePeerInspectorRows filters raw /peers JSON via filterSafePeersJSON so ephemeral home nodes are never exposed.
func parseSafePeerInspectorRows(raw []byte, netProfile core.NetworkProfile) []PeerInspectorRow {
	safeRaw := filterSafePeersJSON(raw, netProfile)

	var strPeers []string
	if json.Unmarshal(safeRaw, &strPeers) == nil {
		rows := make([]PeerInspectorRow, 0, len(strPeers))
		for _, ep := range strPeers {
			role := "Public Node"
			if isSeedEndpoint(ep, netProfile.DefaultSeeds) {
				role = "DNS Seed"
			}
			rows = append(rows, PeerInspectorRow{Address: ep, Role: role, Status: "Active"})
		}
		return rows
	}

	var objPeers []map[string]interface{}
	if json.Unmarshal(safeRaw, &objPeers) == nil {
		rows := make([]PeerInspectorRow, 0, len(objPeers))
		for _, obj := range objPeers {
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
			if addr == "" {
				continue
			}
			role := "Public Node"
			if isSeed, ok := obj["is_seed"].(bool); (ok && isSeed) || isSeedEndpoint(addr, netProfile.DefaultSeeds) {
				role = "DNS Seed"
			}
			status := "Active"
			if active, ok := obj["is_active"].(bool); ok && !active {
				status = "Known"
			}
			rows = append(rows, PeerInspectorRow{Address: addr, Role: role, Status: status})
		}
		return rows
	}
	return nil
}

// filterTxItems applies text search, type selection, and optional YYYY-MM-DD date bounds.
func filterTxItems(all []*TxItem, query, txType, dateFrom, dateTo string) []*TxItem {
	q := strings.ToLower(strings.TrimSpace(query))
	tFilter := strings.TrimSpace(txType)
	dFrom := strings.TrimSpace(dateFrom)
	dTo := strings.TrimSpace(dateTo)

	filtered := make([]*TxItem, 0, len(all))
	for _, item := range all {
		if item == nil {
			continue
		}
		if tFilter != "" && !strings.EqualFold(tFilter, "All") {
			if strings.EqualFold(tFilter, "Pending") {
				if item.Record != nil {
					if item.Record.BlockIndex >= 0 {
						continue
					}
				} else if !strings.Contains(strings.ToLower(item.Height), "pending") {
					continue
				}
			} else if !strings.EqualFold(item.Type, tFilter) {
				continue
			}
		}
		if len(item.Timestamp) >= 10 {
			day := item.Timestamp[:10]
			if len(dFrom) == 10 && day < dFrom {
				continue
			}
			if len(dTo) == 10 && day > dTo {
				continue
			}
		}
		if q != "" {
			sender, recipient := "", ""
			if item.Record != nil {
				sender = item.Record.Tx.Sender
				recipient = item.Record.Tx.Recipient
			}
			searchable := strings.ToLower(item.TxID + " " + item.Type + " " + sender + " " + recipient + " " + item.Timestamp)
			if !strings.Contains(searchable, q) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func formatAddress4CharBlocks(addr string) string {
	cleanAddr := strings.TrimSpace(addr)
	if cleanAddr == "" {
		return ""
	}
	var chunked []string
	for i := 0; i < len(cleanAddr); i += 4 {
		end := i + 4
		if end > len(cleanAddr) {
			end = len(cleanAddr)
		}
		chunked = append(chunked, cleanAddr[i:end])
	}
	return strings.Join(chunked, "  ")
}

func buildBIP21PaymentURI(addr, amountStr, label string) string {
	cleanAddr := strings.TrimSpace(addr)
	if cleanAddr == "" {
		return ""
	}
	cleanLabel := strings.TrimSpace(label)
	if cleanLabel == "" {
		cleanLabel = "DesktopWallet"
	}
	params := make([]string, 0, 2)
	if amtStr := strings.TrimSpace(amountStr); amtStr != "" {
		if amt, err := core.ParseArmxDecimal(amtStr); err == nil && amt > 0 {
			formatted := strings.TrimRight(strings.TrimRight(core.FormatArmxDecimal(amt), "0"), ".")
			if formatted != "" {
				params = append(params, "amount="+url.QueryEscape(formatted))
			}
		}
	}
	params = append(params, "label="+url.QueryEscape(cleanLabel))
	return "armax:" + cleanAddr + "?" + strings.Join(params, "&")
}

type StatsResponse struct {
	Height              int     `json:"height"`
	Blocks              int     `json:"blocks"`
	PeersActive         int     `json:"peers_active"`
	PeersKnown          int     `json:"peers_known"`
	PeersSeeds          int     `json:"peers_seeds"`
	PeersCandidates     int     `json:"peers_candidates"`
	PeersCount          int     `json:"peers_count"`
	Peers               int     `json:"peers"`
	Difficulty          int     `json:"difficulty"`
	DifficultyRatio     float64 `json:"difficulty_ratio"`
	DifficultyHashes    string  `json:"difficulty_hashes"`
	DifficultyFormatted string  `json:"difficulty_formatted"`
	DAAEngine           string  `json:"daa_engine"`
	ASERTScheduleDrift  int64   `json:"asert_drift_seconds"`
	ASERTDriftFormatted string  `json:"asert_drift_formatted"`
	LatestHash          string  `json:"latest_hash"`
	MempoolSize         int     `json:"mempool_size"`
	Mempool             int     `json:"mempool"`
	CirculatingSupply   float64 `json:"circulating_supply"`
	CoinbaseMaturity    int     `json:"coinbase_maturity"`
	NetworkID           string  `json:"network_id"`
	Ticker              string  `json:"ticker"`
	MiningHashrate      float64 `json:"mining_hashrate"`
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

type TxHistoryRecord struct {
	BlockIndex int              `json:"block_index"`
	Timestamp  int64            `json:"timestamp"`
	Status     string           `json:"status"`
	Tx         core.Transaction `json:"tx"`
}

func armxTicker() string {
	netID := activeNetwork.NetworkID
	if netID == "" {
		netID = core.ActiveNetwork.NetworkID
	}
	return core.TickerForNetworkID(netID)
}

func formatArmx(armoids int64) string {
	return fmt.Sprintf("%s %s", core.FormatArmxDecimal(armoids), armxTicker())
}

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

type NodeNetworkStatus struct {
	Online    bool
	Match     bool
	NetworkID string
	Err       error
}

func verifyNodeNetwork(targetURL string, expectedNetID string) NodeNetworkStatus {
	client := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(targetURL + "/stats")
	if err != nil {
		return NodeNetworkStatus{Online: false, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return NodeNetworkStatus{Online: false, Err: fmt.Errorf("node returned HTTP %d", resp.StatusCode)}
	}

	hdrID := strings.TrimSpace(resp.Header.Get("X-Network-ID"))
	if hdrID == "" {
		return NodeNetworkStatus{Online: true, Match: false, Err: errors.New("missing X-Network-ID response header")}
	}
	if hdrID != expectedNetID {
		return NodeNetworkStatus{Online: true, Match: false, NetworkID: hdrID, Err: fmt.Errorf("network mismatch on response header: expected '%s', got '%s'", expectedNetID, hdrID)}
	}

	var stats struct {
		NetworkID string `json:"network_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return NodeNetworkStatus{Online: true, Match: false, Err: fmt.Errorf("malformed /stats response: %w", err)}
	}
	if stats.NetworkID == "" {
		return NodeNetworkStatus{Online: true, Match: false, Err: errors.New("node returned empty network_id")}
	}
	if stats.NetworkID != expectedNetID {
		return NodeNetworkStatus{Online: true, Match: false, NetworkID: stats.NetworkID, Err: fmt.Errorf("network mismatch: expected '%s', got '%s'", expectedNetID, stats.NetworkID)}
	}
	return NodeNetworkStatus{Online: true, Match: true, NetworkID: stats.NetworkID}
}

func isNodeOnline(targetURL string) bool {
	status := verifyNodeNetwork(targetURL, activeNetwork.NetworkID)
	return status.Online && status.Match
}

func validateGUISendAmounts(amtStr, feeStr string) (amtArmoids, feeArmoids int64, errTitle, errMsg string, ok bool) {
	amt, err := core.ParseArmxDecimal(amtStr)
	if err != nil || amt <= 0 {
		return 0, 0, "Invalid Amount", fmt.Sprintf("Invalid %s amount: %v", armxTicker(), err), false
	}
	var fee int64
	if strings.TrimSpace(feeStr) != "" {
		parsedFee, err := core.ParseArmxDecimal(feeStr)
		if err != nil {
			return 0, 0, "Invalid Fee", fmt.Sprintf("Invalid %s fee: %v", armxTicker(), err), false
		}
		fee = parsedFee
	}
	if amt > core.MaxSupply-fee {
		return 0, 0, "Invalid Amount", fmt.Sprintf("Total amount + fee exceeds maximum supply (%s)", formatArmx(core.MaxSupply)), false
	}
	return amt, fee, "", "", true
}

func queryNonceAndSignGUITransaction(
	daemonURL string,
	expectedNetID string,
	signingWallet *wallet.Wallet,
	to string,
	amtArmoids int64,
	feeArmoids int64,
) (*core.Transaction, int64, string, string, error) {
	if amtArmoids <= 0 || amtArmoids > core.MaxSupply || feeArmoids < 0 || feeArmoids > core.MaxSupply || amtArmoids > core.MaxSupply-feeArmoids {
		msg := "Transaction amount or fee exceeds permitted bounds"
		return nil, 0, "Invalid Amount", msg, errors.New("transaction amount or fee exceeds permitted bounds")
	}

	nodeNet := verifyNodeNetwork(daemonURL, expectedNetID)
	if !nodeNet.Match {
		msg := fmt.Sprintf("Cannot send transaction: daemon validation failed (%v)", nodeNet.Err)
		return nil, 0, "Network Mismatch", msg, errors.New(msg)
	}

	client := http.Client{Timeout: 5 * time.Second}
	bResp, err := client.Get(fmt.Sprintf("%s/balance?address=%s", daemonURL, signingWallet.Address))
	if err != nil {
		msg := fmt.Sprintf("Failed to query nonce: %v", err)
		return nil, 0, "RPC Error", msg, errors.New(msg)
	}
	defer bResp.Body.Close()
	if bResp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("Failed to query nonce (HTTP %d)", bResp.StatusCode)
		return nil, 0, "RPC Error", msg, errors.New(msg)
	}
	if netHdr := strings.TrimSpace(bResp.Header.Get("X-Network-ID")); netHdr != "" && !strings.EqualFold(netHdr, expectedNetID) {
		msg := fmt.Sprintf("Network mismatch on /balance: daemon reported %s, expected %s", netHdr, expectedNetID)
		return nil, 0, "Network Mismatch", msg, errors.New(msg)
	}

	var bal BalanceResponse
	if err := json.NewDecoder(bResp.Body).Decode(&bal); err != nil {
		msg := fmt.Sprintf("Invalid balance response from daemon: %v", err)
		return nil, 0, "RPC Error", msg, errors.New(msg)
	}

	avail := bal.PendingBalance
	if avail == 0 {
		avail = bal.GetConfirmed()
	}
	if avail < amtArmoids+feeArmoids {
		msg := fmt.Sprintf("Total required: %s\nAvailable balance: %s", formatArmx(amtArmoids+feeArmoids), formatArmx(avail))
		return nil, 0, "Insufficient Balance", msg, errors.New(msg)
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
		msg := fmt.Sprintf("Post-quantum signature creation failed: %v", err)
		return nil, 0, "Sign Error", msg, errors.New(msg)
	}

	return &tx, txNonce, "", "", nil
}

func queryValidatedNodeEndpoint(targetRPCURL string, endpointPath string, expectedNetID string, timeout time.Duration) ([]byte, int, error) {
	client := http.Client{Timeout: timeout}
	reqURL := strings.TrimRight(targetRPCURL, "/") + endpointPath
	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, 0, fmt.Errorf("daemon offline or unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("node returned HTTP %d", resp.StatusCode)
	}

	hdrNetID := strings.TrimSpace(resp.Header.Get("X-Network-ID"))
	if hdrNetID == "" {
		return nil, resp.StatusCode, errors.New("missing X-Network-ID response header")
	}
	if hdrNetID != expectedNetID {
		return nil, resp.StatusCode, fmt.Errorf("network mismatch on response header: expected '%s', got '%s'", expectedNetID, hdrNetID)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	if strings.HasPrefix(endpointPath, "/stats") {
		var stats struct {
			NetworkID string `json:"network_id"`
		}
		if err := json.Unmarshal(data, &stats); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("malformed /stats response: %w", err)
		}
		if stats.NetworkID == "" {
			return nil, resp.StatusCode, errors.New("node returned empty network_id")
		}
		if stats.NetworkID != expectedNetID {
			return nil, resp.StatusCode, fmt.Errorf("network mismatch on response body: expected '%s', got '%s'", expectedNetID, stats.NetworkID)
		}
	}

	return data, resp.StatusCode, nil
}

func executeConsoleCommand(rawCmd string, targetRPCURL string, expectedNetID string) string {
	fields := strings.Fields(strings.TrimSpace(rawCmd))
	if len(fields) == 0 {
		return ""
	}
	cmd := strings.ToLower(fields[0])
	if cmd == "clear" {
		return ""
	}
	if cmd == "help" {
		return "ARMAX GUI CLI (read-only commands)\nhelp\ngetnetworkinfo\ngetblockchaininfo\ngetpeerinfo\ngetbalance [address]\ngethistory [address]\ngetmempoolinfo\nclear\n\nCommands query the configured node; they do not execute OS shell commands."
	}
	path := ""
	switch cmd {
	case "getnetworkinfo", "getblockchaininfo":
		path = "/stats"
	case "getmempoolinfo", "mempool":
		path = "/mempool"
	case "getpeerinfo":
		path = "/peers"
	case "getbalance":
		addr := activeAddress
		if len(fields) > 1 {
			addr = fields[1]
		}
		if !core.ValidateAddress(addr) {
			return "Invalid address. Usage: getbalance [armx1... address]"
		}
		path = "/balance?address=" + url.QueryEscape(addr)
	case "gethistory":
		addr := activeAddress
		if len(fields) > 1 {
			addr = fields[1]
		}
		if !core.ValidateAddress(addr) {
			return "Invalid address. Usage: gethistory [armx1... address]"
		}
		path = "/history?address=" + url.QueryEscape(addr)
	default:
		return "Unknown command. Type help for supported commands."
	}

	data, statusCode, err := queryValidatedNodeEndpoint(targetRPCURL, path, expectedNetID, 5*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "offline") || strings.Contains(err.Error(), "unreachable") {
			return fmt.Sprintf("CLI error: daemon offline or unreachable: %v", err)
		}
		return fmt.Sprintf("CLI error: network validation failed: %v", err)
	}
	if cmd == "getpeerinfo" {
		data = filterSafePeersJSON(data, activeNetwork)
	} else if cmd == "getmempoolinfo" || cmd == "mempool" {
		data = formatMempoolInfoJSON(data, expectedNetID)
	}
	return fmt.Sprintf("$ %s\nHTTP %d\n%s", strings.TrimSpace(rawCmd), statusCode, string(data))
}
