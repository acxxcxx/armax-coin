package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
)

// TestV1011_BackwardCompatibilityAtBlock15967 verifies full backward compatibility between
// v1.0.10 and v1.0.11 nodes around active ARMX-TESTNET-1 height #15,967 through #19,999
// (legacy JSON block sizing), and unified activation of C6 (+600s) and TxBinaryV1 at #20,000.
func TestV1011_BackwardCompatibilityAtBlock15967(t *testing.T) {
	unlock := core.AcquireNetworkTestLock()
	defer unlock()

	prevNet := core.GetActiveNetwork().Name
	if prevNet != "testnet" {
		if err := core.SetNetwork("testnet"); err != nil {
			t.Fatalf("SetNetwork(testnet) failed: %v", err)
		}
		defer func() {
			_ = core.SetNetwork(prevNet)
		}()
	}

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := core.MustNewBlockchain(1)
	defer bc.Close()

	// Ensure binary block sizing stays inactive across #15,967..#19,999 and activates cleanly at #20,000.
	if core.IsBlockBinarySizeActive(15967) {
		t.Fatalf("expected IsBlockBinarySizeActive(15967) == false on ARMX-TESTNET-1")
	}
	if core.IsBlockBinarySizeActive(19999) {
		t.Fatalf("expected IsBlockBinarySizeActive(19999) == false on ARMX-TESTNET-1")
	}
	if !core.IsBlockBinarySizeActive(20000) {
		t.Fatalf("expected IsBlockBinarySizeActive(20000) == true on ARMX-TESTNET-1")
	}

	now := time.Now().Unix()
	target1 := core.DifficultyToTarget(1)
	targetHex1 := fmt.Sprintf("%064x", target1)
	minerAddr := bc.Chain[0].Miner

	// Build a lightweight contiguous chain up to height #15,966 (nextHeight = #15,967)
	// with realistic 60-second block spacing near current wall-clock time.
	baseTs := now - int64(21000)*60
	if baseTs <= bc.Chain[0].Timestamp {
		baseTs = bc.Chain[0].Timestamp + 60
	}

	bc.Lock.Lock()
	prevHash := bc.Chain[0].Hash
	for h := 1; h <= 15966; h++ {
		blkHash := fmt.Sprintf("%064x", h)
		bc.Chain = append(bc.Chain, core.Block{
			Index:      h,
			Timestamp:  baseTs + int64(h)*60,
			PrevHash:   prevHash,
			Hash:       blkHash,
			Miner:      minerAddr,
			Difficulty: 1,
			Target:     targetHex1,
		})
		prevHash = blkHash
	}

	// Fund sender account and populate mempool with 200 signed ML-DSA-65 transactions
	// whose combined JSON size exceeds MaxBlockSize (~2.15 MB > 2.0 MB) while binary size is ~1.05 MB.
	senderAcc := newTestAccount(t, "v1011_sender")
	recipAcc := newTestAccount(t, "v1011_recip")
	bc.State.Lock.Lock()
	bc.State.Accounts[senderAcc.AddressHex] = core.AccountState{
		Balance: 100_000_000 * core.CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	signedTxs := make([]core.Transaction, 0, 200)
	for i := 1; i <= 200; i++ {
		tx := core.Transaction{
			Sender:    senderAcc.Address,
			SenderPub: senderAcc.PubHex,
			Recipient: recipAcc.Address,
			Amount:    core.CoinUnit,
			Fee:       core.MinRelayFee,
			Nonce:     int64(i),
			Type:      core.TxTypeStandard,
		}
		if err := tx.Sign(senderAcc.PrivKey); err != nil {
			bc.Lock.Unlock()
			t.Fatalf("sign tx %d failed: %v", i, err)
		}
		signedTxs = append(signedTxs, tx)
		bc.Mempool = append(bc.Mempool, tx)
	}
	bc.Lock.Unlock()

	srv := httptest.NewServer(NewRPCHandler(bc, nil, nil, nil))
	defer srv.Close()

	// 1. Verify active daemon version (core.Version) on /status and /stats telemetry at #15,966.
	respStatus, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status failed: %v", err)
	}
	var statusPayload map[string]interface{}
	if err := json.NewDecoder(respStatus.Body).Decode(&statusPayload); err != nil {
		respStatus.Body.Close()
		t.Fatalf("decode /status failed: %v", err)
	}
	respStatus.Body.Close()
	if statusPayload["version"] != core.Version {
		t.Fatalf("expected /status version %s, got %v", core.Version, statusPayload["version"])
	}
	if statusPayload["network_id"] != core.TestnetProfile.NetworkID {
		t.Fatalf("expected /status network_id %s, got %v", core.TestnetProfile.NetworkID, statusPayload["network_id"])
	}

	respStats, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats failed: %v", err)
	}
	var statsPayload map[string]interface{}
	if err := json.NewDecoder(respStats.Body).Decode(&statsPayload); err != nil {
		respStats.Body.Close()
		t.Fatalf("decode /stats failed: %v", err)
	}
	respStats.Body.Close()
	if statsPayload["network_id"] != core.TestnetProfile.NetworkID {
		t.Fatalf("expected /stats network_id %s, got %v", core.TestnetProfile.NetworkID, statsPayload["network_id"])
	}
	if statsPayload["daa_engine"] != "Armax-ASERT-AW" {
		t.Fatalf("expected /stats daa_engine Armax-ASERT-AW at #15,966, got %v", statsPayload["daa_engine"])
	}

	// 2. Verify /getblocktemplate at nextHeight #15,967 uses legacy JSON block sizing (~181 tx cap < 200)
	// so blocks remain 100% acceptable to v1.0.10 nodes.
	respTpl15967, err := http.Get(srv.URL + "/getblocktemplate")
	if err != nil {
		t.Fatalf("GET /getblocktemplate at #15967 failed: %v", err)
	}
	var tpl15967 struct {
		Height       int                `json:"height"`
		CurTime      int64              `json:"curtime"`
		MinTime      int64              `json:"mintime"`
		MaxTime      int64              `json:"maxtime"`
		DaaEngine    string             `json:"daa_engine"`
		PowAlgo      string             `json:"pow_algo"`
		Transactions []core.Transaction `json:"transactions"`
	}
	if err := json.NewDecoder(respTpl15967.Body).Decode(&tpl15967); err != nil {
		respTpl15967.Body.Close()
		t.Fatalf("decode /getblocktemplate #15967 failed: %v", err)
	}
	respTpl15967.Body.Close()

	if tpl15967.Height != 15967 {
		t.Fatalf("expected /getblocktemplate height 15967, got %d", tpl15967.Height)
	}
	if tpl15967.DaaEngine != "Armax-ASERT-AW" {
		t.Fatalf("expected daa_engine Armax-ASERT-AW, got %q", tpl15967.DaaEngine)
	}
	if tpl15967.PowAlgo != "armaxhash-v2" {
		t.Fatalf("expected pow_algo armaxhash-v2, got %q", tpl15967.PowAlgo)
	}
	if len(tpl15967.Transactions) >= 200 || len(tpl15967.Transactions) == 0 {
		t.Fatalf("expected legacy JSON block sizing cap (<200 txs) at #15967, got %d", len(tpl15967.Transactions))
	}

	// 3. Verify PreCheckBlock rejects a 200-tx (~2.15 MB JSON) block at #15,967 and #19,999 under legacy sizing,
	// whereas at #20,000 EffectiveBlockSize switches to BinarySize (< 2 MB) and passes step-0 size checks.
	coinbase := core.Transaction{
		Sender:    "SYSTEM",
		Recipient: senderAcc.Address,
		Amount:    core.InitialReward,
		Nonce:     15967,
		Signature: "REWARD_SIG",
	}
	largeTxs := make([]core.Transaction, 0, len(signedTxs)+1)
	largeTxs = append(largeTxs, coinbase)
	largeTxs = append(largeTxs, signedTxs...)

	blk15967 := core.Block{
		Index:        15967,
		Timestamp:    now,
		Transactions: largeTxs,
		PrevHash:     prevHash,
		Miner:        senderAcc.Address,
		Difficulty:   1,
		Target:       targetHex1,
	}
	if blk15967.Size() <= core.MaxBlockSize {
		t.Fatalf("expected raw JSON block size > MaxBlockSize (%d), got %d", core.MaxBlockSize, blk15967.Size())
	}
	if blk15967.BinarySize() >= core.MaxBlockSize {
		t.Fatalf("expected BinarySize < MaxBlockSize (%d), got %d", core.MaxBlockSize, blk15967.BinarySize())
	}
	if err := bc.PreCheckBlock(&blk15967); err == nil || !strings.Contains(err.Error(), "MaxBlockSize") {
		t.Fatalf("expected PreCheckBlock to reject oversized JSON block at #15967 with MaxBlockSize error, got: %v", err)
	}

	blk19999 := blk15967
	blk19999.Index = 19999
	if gotEff := core.EffectiveBlockSize(&blk19999); gotEff != blk19999.Size() {
		t.Fatalf("expected EffectiveBlockSize at #19999 to equal legacy JSON Size (%d), got %d", blk19999.Size(), gotEff)
	}
	if err := bc.PreCheckBlock(&blk19999); err == nil || !strings.Contains(err.Error(), "MaxBlockSize") {
		t.Fatalf("expected PreCheckBlock to reject oversized JSON block at #19999 with MaxBlockSize error, got: %v", err)
	}

	blk20000 := blk15967
	blk20000.Index = 20000
	if gotEff := core.EffectiveBlockSize(&blk20000); gotEff != blk20000.BinarySize() {
		t.Fatalf("expected EffectiveBlockSize at #20000 to equal BinarySize (%d), got %d", blk20000.BinarySize(), gotEff)
	}
	if err := bc.PreCheckBlock(&blk20000); err != nil && strings.Contains(err.Error(), "MaxBlockSize") {
		t.Fatalf("unexpected MaxBlockSize error at #20000 under TxBinaryV1 sizing: %v", err)
	}

	// 4. Verify P2P JSON wire compatibility (Message wrapping NEW_BLOCK and BLOCK_RANGE_RESPONSE).
	newBlkMsg := Message{
		NetworkID: core.NetworkID(),
		Type:      "NEW_BLOCK",
		Payload:   blk15967,
	}
	wireNewBlk, err := json.Marshal(newBlkMsg)
	if err != nil {
		t.Fatalf("marshal NEW_BLOCK failed: %v", err)
	}
	var decodedNewBlk Message
	if err := json.Unmarshal(wireNewBlk, &decodedNewBlk); err != nil {
		t.Fatalf("unmarshal NEW_BLOCK failed: %v", err)
	}
	if decodedNewBlk.NetworkID != core.TestnetProfile.NetworkID || decodedNewBlk.Type != "NEW_BLOCK" {
		t.Fatalf("unexpected NEW_BLOCK envelope: %+v", decodedNewBlk)
	}
	newBlkBytes, err := decodedNewBlk.PayloadBytes()
	if err != nil {
		t.Fatalf("NEW_BLOCK PayloadBytes failed: %v", err)
	}
	var roundTripBlk core.Block
	if err := json.Unmarshal(newBlkBytes, &roundTripBlk); err != nil {
		t.Fatalf("unmarshal NEW_BLOCK payload failed: %v", err)
	}
	if roundTripBlk.Index != 15967 || len(roundTripBlk.Transactions) != len(largeTxs) {
		t.Fatalf("NEW_BLOCK round-trip mismatch: index=%d txs=%d", roundTripBlk.Index, len(roundTripBlk.Transactions))
	}

	rangeMsg := Message{
		NetworkID: core.NetworkID(),
		Type:      "BLOCK_RANGE_RESPONSE",
		Payload: BlockRangeResponse{
			FromIndex: 15967,
			ToIndex:   15967,
			Blocks:    []core.Block{blk15967},
		},
	}
	wireRange, err := json.Marshal(rangeMsg)
	if err != nil {
		t.Fatalf("marshal BLOCK_RANGE_RESPONSE failed: %v", err)
	}
	var decodedRange Message
	if err := json.Unmarshal(wireRange, &decodedRange); err != nil {
		t.Fatalf("unmarshal BLOCK_RANGE_RESPONSE failed: %v", err)
	}
	rangeBytes, err := decodedRange.PayloadBytes()
	if err != nil {
		t.Fatalf("BLOCK_RANGE_RESPONSE PayloadBytes failed: %v", err)
	}
	var roundTripRange BlockRangeResponse
	if err := json.Unmarshal(rangeBytes, &roundTripRange); err != nil {
		t.Fatalf("unmarshal BlockRangeResponse failed: %v", err)
	}
	if len(roundTripRange.Blocks) != 1 || roundTripRange.Blocks[0].Index != 15967 {
		t.Fatalf("BLOCK_RANGE_RESPONSE round-trip mismatch: %+v", roundTripRange)
	}

	// 5. Advance synthetic tip through #19,998 -> verify /getblocktemplate at #19,999 still caps < 200 txs
	// under legacy JSON sizing, then advance to #19,999 with a forward-stamped (+590s) parent timestamp
	// and verify /getblocktemplate at #20,000 simultaneously activates C6 (+600s) and TxBinaryV1 (all 200 txs fit).
	type tplResp struct {
		Height       int                `json:"height"`
		CurTime      int64              `json:"curtime"`
		MinTime      int64              `json:"mintime"`
		MaxTime      int64              `json:"maxtime"`
		Transactions []core.Transaction `json:"transactions"`
	}
	fetchTemplate := func(wantHeight int) tplResp {
		resp, err := http.Get(srv.URL + "/getblocktemplate")
		if err != nil {
			t.Fatalf("GET /getblocktemplate at #%d failed: %v", wantHeight, err)
		}
		defer resp.Body.Close()
		var tpl tplResp
		if err := json.NewDecoder(resp.Body).Decode(&tpl); err != nil {
			t.Fatalf("decode /getblocktemplate #%d failed: %v", wantHeight, err)
		}
		if tpl.Height != wantHeight {
			t.Fatalf("expected height %d, got %d", wantHeight, tpl.Height)
		}
		return tpl
	}
	appendSynthetic := func(h int, ts int64) {
		blkHash := fmt.Sprintf("%064x", h)
		bc.Chain = append(bc.Chain, core.Block{
			Index:      h,
			Timestamp:  ts,
			PrevHash:   prevHash,
			Hash:       blkHash,
			Miner:      minerAddr,
			Difficulty: 1,
			Target:     targetHex1,
		})
		prevHash = blkHash
	}

	bc.Lock.Lock()
	for h := 15967; h <= 19998; h++ {
		appendSynthetic(h, baseTs+int64(h)*60)
	}
	bc.Lock.Unlock()

	tpl19999 := fetchTemplate(19999)
	if len(tpl19999.Transactions) == 0 || len(tpl19999.Transactions) >= 200 {
		t.Fatalf("expected /getblocktemplate at #19,999 to cap < 200 txs under legacy JSON Size(), got %d", len(tpl19999.Transactions))
	}

	bc.Lock.Lock()
	appendSynthetic(19999, time.Now().Unix()+590)
	parent19999Ts := bc.Chain[19999].Timestamp
	bc.Lock.Unlock()

	tpl20k := fetchTemplate(20000)
	if len(tpl20k.Transactions) != 200 {
		t.Fatalf("expected /getblocktemplate at #20,000 to fit all 200 mempool ML-DSA-65 txs under TxBinaryV1, got %d", len(tpl20k.Transactions))
	}
	if tpl20k.MinTime != parent19999Ts {
		t.Fatalf("expected C2 strict mintime == parent #19999 timestamp (%d), got %d", parent19999Ts, tpl20k.MinTime)
	}
	if tpl20k.CurTime < tpl20k.MinTime {
		t.Fatalf("expected curtime (%d) >= mintime (%d)", tpl20k.CurTime, tpl20k.MinTime)
	}
	if tpl20k.MaxTime < tpl20k.CurTime || tpl20k.MaxTime > tpl20k.CurTime+15 {
		t.Fatalf("expected maxtime (%d) within [curtime=%d, curtime+15=%d]", tpl20k.MaxTime, tpl20k.CurTime, tpl20k.CurTime+15)
	}
}
