package core

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

func setupP25EasyTarget(t *testing.T) {
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int {
		return easyTarget
	}
	t.Cleanup(func() {
		targetOverrideHook = oldHook
	})
}

// TestP2_5_TransactionTamperingRejections verifies corruption and tampering rejections
func TestP2_5_TransactionTamperingRejections(t *testing.T) {
	acc1 := newTestAccount(t, "user1")
	acc2 := newTestAccount(t, "user2")

	baseTx := Transaction{
		Sender:    acc1.Address,
		SenderPub: acc1.PubHex,
		Recipient: acc2.Address,
		Amount:    1000 * CoinUnit,
		Fee:       CalculateMinMempoolFee(5500),
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := baseTx.Sign(acc1.PrivKey); err != nil {
		t.Fatalf("baseTx.Sign failed: %v", err)
	}
	if !baseTx.Verify() {
		t.Fatalf("baseTx.Verify failed on valid transaction")
	}

	// 1. Corrupted message (Amount altered after signing)
	txCorruptMsg := baseTx
	txCorruptMsg.Amount += 100
	if txCorruptMsg.Verify() {
		t.Errorf("Expected Verify() to reject transaction with altered amount")
	}

	// 2. Corrupted signature bit flipped
	txCorruptSig := baseTx
	sigBytes, _ := hex.DecodeString(txCorruptSig.Signature)
	sigBytes[50] ^= 0x01
	txCorruptSig.Signature = hex.EncodeToString(sigBytes)
	if txCorruptSig.Verify() {
		t.Errorf("Expected Verify() to reject transaction with flipped signature bit")
	}

	// 3. Corrupted public key bit flipped
	txCorruptPub := baseTx
	pubBytes, _ := hex.DecodeString(txCorruptPub.SenderPub)
	pubBytes[20] ^= 0x01
	txCorruptPub.SenderPub = hex.EncodeToString(pubBytes)
	if txCorruptPub.Verify() {
		t.Errorf("Expected Verify() to reject transaction with corrupted public key")
	}

	// 4. Invalid SigAlg
	txWrongAlg := baseTx
	txWrongAlg.SigAlg = 0x99
	if txWrongAlg.Verify() {
		t.Errorf("Expected Verify() to reject transaction with invalid SigAlg 0x99")
	}

	// 5. Truncated public key length
	txTruncPub := baseTx
	txTruncPub.SenderPub = txTruncPub.SenderPub[:len(txTruncPub.SenderPub)-4]
	if txTruncPub.Verify() {
		t.Errorf("Expected Verify() to reject transaction with truncated public key")
	}

	// 6. Truncated signature length
	txTruncSig := baseTx
	txTruncSig.Signature = txTruncSig.Signature[:len(txTruncSig.Signature)-4]
	if txTruncSig.Verify() {
		t.Errorf("Expected Verify() to reject transaction with truncated signature")
	}

	// 7. Sender address mismatch (Sender address belongs to acc2, but pubkey belongs to acc1)
	txMismatchSender := baseTx
	txMismatchSender.Sender = acc2.Address
	if txMismatchSender.Verify() {
		t.Errorf("Expected Verify() to reject transaction where Sender does not match SenderPub")
	}
}

// TestP2_5_BlockWith150PQCTransactions tests that a block containing 150 ML-DSA-65 transactions
// is fully valid and strictly within MaxBlockSize (2 MB).
func TestP2_5_BlockWith150PQCTransactions(t *testing.T) {
	setupP25EasyTarget(t)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	minerAcc := newTestAccount(t, "miner")
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()

	const numTxs = 150
	txs := make([]Transaction, 0, numTxs+1)

	// Coinbase tx
	coinbase := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAcc.Address,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     1,
		Signature: "REWARD_SIG",
	}
	txs = append(txs, coinbase)

	senderAcc := newTestAccount(t, "heavy_sender")
	senderHex, _ := AddressToHex(senderAcc.Address)
	recipAcc := newTestAccount(t, "heavy_recip")

	bc.State.Lock.Lock()
	bc.State.Accounts[senderHex] = AccountState{
		Balance: 100_000 * CoinUnit,
		Nonce:   0,
	}
	bc.State.Lock.Unlock()

	var totalFees int64 = 0
	for i := 1; i <= numTxs; i++ {
		tx := Transaction{
			Sender:    senderAcc.Address,
			SenderPub: senderAcc.PubHex,
			Recipient: recipAcc.Address,
			Amount:    1 * CoinUnit,
			Fee:       MinRelayFee * 10,
			Nonce:     int64(i),
			Type:      TxTypeStandard,
		}
		if err := tx.Sign(senderAcc.PrivKey); err != nil {
			t.Fatalf("Signing tx #%d failed: %v", i, err)
		}
		txs = append(txs, tx)
		totalFees += tx.Fee
	}

	txs[0].Amount += totalFees // Include fees into coinbase reward

	block := *NewBlock(txs, prev.Hash, 1, minerAcc.Address, target)
	block.Timestamp = prev.Timestamp + 1
	for {
		block.Hash = block.CalculateHash()
		if HashMeetsTarget(block.Hash, target) {
			break
		}
		block.Nonce++
	}

	blockSize := block.Size()
	t.Logf("[P2-5] Block with 150 PQC transactions size: %d bytes (MaxBlockSize: %d)", blockSize, MaxBlockSize)
	if blockSize > MaxBlockSize {
		t.Fatalf("Block size %d exceeds MaxBlockSize %d", blockSize, MaxBlockSize)
	}

	if err := bc.ValidateBlock(block); err != nil {
		t.Fatalf("ValidateBlock failed on valid 150 PQC tx block: %v", err)
	}
}

// TestP2_5_BlockExceedingMaxBlockSizeRejected tests that a block exceeding MaxBlockSize is rejected
// with a specific error message.
func TestP2_5_BlockExceedingMaxBlockSizeRejected(t *testing.T) {
	setupP25EasyTarget(t)
	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	minerAcc := newTestAccount(t, "miner")
	prev := bc.GetLatestBlock()
	target := bc.CalculateLWMA1()

	// Create oversized block with transactions exceeding 2,000,000 bytes
	senderAcc := newTestAccount(t, "sender")
	recipAcc := newTestAccount(t, "recip")

	txs := []Transaction{
		{
			Sender:    "SYSTEM",
			Recipient: minerAcc.Address,
			Amount:    InitialReward,
			Nonce:     1,
			Signature: "REWARD_SIG",
		},
	}

	// 400 transactions * ~5.5 KB = ~2.2 MB > 2.0 MB MaxBlockSize
	for i := 1; i <= 390; i++ {
		tx := Transaction{
			Sender:    senderAcc.Address,
			SenderPub: senderAcc.PubHex,
			Recipient: recipAcc.Address,
			Amount:    1 * CoinUnit,
			Fee:       MinRelayFee * 10,
			Nonce:     int64(i),
			Type:      TxTypeStandard,
		}
		if err := tx.Sign(senderAcc.PrivKey); err != nil {
			t.Fatalf("Failed to sign tx: %v", err)
		}
		txs = append(txs, tx)
	}

	block := *NewBlock(txs, prev.Hash, 1, minerAcc.Address, target)
	block.Timestamp = prev.Timestamp + 1
	for {
		block.Hash = block.CalculateHash()
		if HashMeetsTarget(block.Hash, target) {
			break
		}
		block.Nonce++
	}

	if block.Size() <= MaxBlockSize {
		t.Fatalf("Block size %d should exceed MaxBlockSize (%d) for negative test", block.Size(), MaxBlockSize)
	}

	err := bc.ValidateBlock(block)
	if err == nil {
		t.Fatalf("Expected ValidateBlock to reject oversized block (> MaxBlockSize), got nil")
	}
	if !strings.Contains(err.Error(), "exceeds MaxBlockSize") {
		t.Fatalf("Expected 'exceeds MaxBlockSize' in rejection error, got: %v", err)
	}
}

// BenchmarkTxVerify benchmarks single transaction ML-DSA-65 verification speed
func BenchmarkTxVerify(b *testing.B) {
	acc1 := newTestAccount(nil, "bench_user1")
	acc2 := newTestAccount(nil, "bench_user2")

	tx := Transaction{
		Sender:    acc1.Address,
		SenderPub: acc1.PubHex,
		Recipient: acc2.Address,
		Amount:    1000 * CoinUnit,
		Fee:       MinRelayFee * 10,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(acc1.PrivKey); err != nil {
		b.Fatalf("tx.Sign failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !tx.Verify() {
			b.Fatalf("tx.Verify failed in benchmark")
		}
	}
}

// BenchmarkBlockValidation benchmarks full block validation with 10 ML-DSA-65 transactions
func BenchmarkBlockValidation(b *testing.B) {
	b.Setenv("ARMX_DATA_DIR", b.TempDir())
	bc := NewBlockchain(1)
	defer bc.Close()

	minerAcc := newTestAccount(nil, "bench_miner")
	senderAcc := newTestAccount(nil, "bench_sender")
	recipAcc := newTestAccount(nil, "bench_recip")

	senderHex, _ := AddressToHex(senderAcc.Address)
	baseAccount := AccountState{
		Balance: 10_000_000 * CoinUnit,
		Nonce:   0,
	}

	prev := bc.GetLatestBlock()
	blockIdx := prev.Index + 1
	target := bc.CalculateLWMA1()

	txs := []Transaction{
		{
			Sender:    "SYSTEM",
			Recipient: minerAcc.Address,
			Amount:    InitialReward,
			Nonce:     int64(blockIdx),
			Signature: "REWARD_SIG",
		},
	}

	for i := 1; i <= 10; i++ {
		tx := Transaction{
			Sender:    senderAcc.Address,
			SenderPub: senderAcc.PubHex,
			Recipient: recipAcc.Address,
			Amount:    1 * CoinUnit,
			Fee:       MinRelayFee * 10,
			Nonce:     int64(i),
			Type:      TxTypeStandard,
		}
		_ = tx.Sign(senderAcc.PrivKey)
		txs = append(txs, tx)
	}

	block := *NewBlock(txs, prev.Hash, blockIdx, minerAcc.Address, target)
	block.Timestamp = prev.Timestamp + 1
	for {
		block.Hash = block.CalculateHash()
		if HashMeetsTarget(block.Hash, target) {
			break
		}
		block.Nonce++
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		bc.State.Lock.Lock()
		bc.State.Accounts[senderHex] = baseAccount
		bc.State.Lock.Unlock()
		b.StartTimer()

		if err := bc.ValidateBlock(block); err != nil {
			b.Fatalf("ValidateBlock failed in benchmark: %v", err)
		}
	}
}
