package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

func TestTransaction_BinaryRoundTrip_Standard(t *testing.T) {
	senderPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(sender) failed: %v", err)
	}
	recipPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(recipient) failed: %v", err)
	}

	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, senderPriv.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress(sender) failed: %v", err)
	}
	recipAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, recipPriv.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress(recipient) failed: %v", err)
	}

	tx := Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    125 * CoinUnit,
		Fee:       10_000,
		Nonce:     42,
		Type:      TxTypeStandard,
	}
	if err := tx.Sign(senderPriv); err != nil {
		t.Fatalf("tx.Sign failed: %v", err)
	}

	if tx.IsCoinbase() {
		t.Fatalf("expected standard tx.IsCoinbase() == false")
	}

	bin, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	const expectedStandardBinarySize = 5320
	if len(bin) != expectedStandardBinarySize {
		t.Fatalf("expected binary size %d bytes, got %d", expectedStandardBinarySize, len(bin))
	}
	if tx.BinarySize() != expectedStandardBinarySize {
		t.Fatalf("expected tx.BinarySize() == %d, got %d", expectedStandardBinarySize, tx.BinarySize())
	}

	var decoded Transaction
	if err := decoded.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if decoded.Sender != tx.Sender ||
		decoded.SenderPub != tx.SenderPub ||
		decoded.Recipient != tx.Recipient ||
		decoded.Amount != tx.Amount ||
		decoded.Fee != tx.Fee ||
		decoded.Nonce != tx.Nonce ||
		decoded.Signature != tx.Signature ||
		decoded.SigAlg != tx.SigAlg ||
		decoded.Type != tx.Type {
		t.Fatalf("decoded standard transaction field mismatch")
	}
	if decoded.HashHex() != tx.HashHex() {
		t.Fatalf("hash mismatch: got %s, want %s", decoded.HashHex(), tx.HashHex())
	}
	if decoded.WitnessHashHex() != tx.WitnessHashHex() {
		t.Fatalf("witness hash mismatch: got %s, want %s", decoded.WitnessHashHex(), tx.WitnessHashHex())
	}
	if err := decoded.VerifyWithError(); err != nil {
		t.Fatalf("decoded standard transaction failed ML-DSA-65 signature verification: %v", err)
	}
}

func TestTransaction_BinaryRoundTrip_Coinbase(t *testing.T) {
	minerPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(miner) failed: %v", err)
	}
	minerAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, minerPriv.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress(miner) failed: %v", err)
	}

	cb := Transaction{
		Sender:    "SYSTEM",
		SenderPub: "",
		Recipient: minerAddr,
		Amount:    InitialReward + 500_000,
		Fee:       0,
		Nonce:     13335,
		Signature: "REWARD_SIG",
	}

	if !cb.IsCoinbase() {
		t.Fatalf("expected cb.IsCoinbase() == true")
	}

	bin, err := cb.MarshalBinary()
	if err != nil {
		t.Fatalf("coinbase MarshalBinary failed: %v", err)
	}

	const expectedCoinbaseBinarySize = 60
	if len(bin) != expectedCoinbaseBinarySize {
		t.Fatalf("expected coinbase binary size %d bytes, got %d", expectedCoinbaseBinarySize, len(bin))
	}
	if cb.BinarySize() != expectedCoinbaseBinarySize {
		t.Fatalf("expected cb.BinarySize() == %d, got %d", expectedCoinbaseBinarySize, cb.BinarySize())
	}

	var decoded Transaction
	if err := decoded.UnmarshalBinary(bin); err != nil {
		t.Fatalf("coinbase UnmarshalBinary failed: %v", err)
	}

	if !decoded.IsCoinbase() ||
		decoded.Sender != "SYSTEM" ||
		decoded.SenderPub != "" ||
		decoded.Recipient != cb.Recipient ||
		decoded.Amount != cb.Amount ||
		decoded.Fee != 0 ||
		decoded.Nonce != cb.Nonce ||
		decoded.Signature != "REWARD_SIG" {
		t.Fatalf("decoded coinbase transaction mismatch: %+v", decoded)
	}
	if decoded.HashHex() != cb.HashHex() {
		t.Fatalf("coinbase hash mismatch: got %s, want %s", decoded.HashHex(), cb.HashHex())
	}
}

func TestTransaction_BinaryRoundTrip_HTLC(t *testing.T) {
	senderPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(sender) failed: %v", err)
	}
	recipPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(recipient) failed: %v", err)
	}
	senderAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, senderPriv.PublicKeyBytes())
	recipAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, recipPriv.PublicKeyBytes())

	secret := []byte("armax-atomic-swap-preimage-secret-32b")
	hlDigest := sha256.Sum256(secret)
	hashLockHex := hex.EncodeToString(hlDigest[:])
	preimageHex := hex.EncodeToString(secret)

	cases := []struct {
		name string
		tx   Transaction
	}{
		{
			name: "HTLC_LOCK",
			tx: Transaction{
				Sender:    senderAddr,
				Recipient: recipAddr,
				Amount:    50 * CoinUnit,
				Fee:       5000,
				Nonce:     1,
				Type:      "HTLC_LOCK",
				HashLock:  hashLockHex,
				TimeLock:  1765000000,
			},
		},
		{
			name: "HTLC_CLAIM",
			tx: Transaction{
				Sender:    senderAddr,
				Recipient: recipAddr,
				Amount:    50 * CoinUnit,
				Fee:       5000,
				Nonce:     2,
				Type:      "HTLC_CLAIM",
				HashLock:  hashLockHex,
				TimeLock:  1765000000,
				Preimage:  preimageHex,
			},
		},
		{
			name: "HTLC_REFUND",
			tx: Transaction{
				Sender:    senderAddr,
				Recipient: recipAddr,
				Amount:    50 * CoinUnit,
				Fee:       5000,
				Nonce:     3,
				Type:      "HTLC_REFUND",
				HashLock:  hashLockHex,
				TimeLock:  1765000000,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := tc.tx
			if err := tx.Sign(senderPriv); err != nil {
				t.Fatalf("Sign failed: %v", err)
			}

			bin, err := tx.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}
			if tx.BinarySize() != len(bin) {
				t.Fatalf("BinarySize() %d != len(MarshalBinary()) %d", tx.BinarySize(), len(bin))
			}

			var decoded Transaction
			if err := decoded.UnmarshalBinary(bin); err != nil {
				t.Fatalf("UnmarshalBinary failed: %v", err)
			}

			if decoded.Type != tx.Type ||
				decoded.HashLock != tx.HashLock ||
				decoded.TimeLock != tx.TimeLock ||
				decoded.Preimage != tx.Preimage ||
				decoded.Sender != tx.Sender ||
				decoded.Recipient != tx.Recipient ||
				decoded.Amount != tx.Amount ||
				decoded.Fee != tx.Fee ||
				decoded.Nonce != tx.Nonce ||
				decoded.Signature != tx.Signature {
				t.Fatalf("HTLC round-trip field mismatch for %s", tc.name)
			}
			if decoded.HashHex() != tx.HashHex() {
				t.Fatalf("HTLC hash mismatch: got %s, want %s", decoded.HashHex(), tx.HashHex())
			}
			if !pqsig.Verify(senderPriv.PublicKeyBytes(), decoded.Hash(), func() []byte {
				b, _ := hex.DecodeString(decoded.Signature)
				return b
			}()) {
				t.Fatalf("HTLC ML-DSA-65 signature failed after UnmarshalBinary")
			}
		})
	}
}

func TestBlock_BinarySize_2MBCapacity(t *testing.T) {
	minerPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(miner) failed: %v", err)
	}
	senderPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(sender) failed: %v", err)
	}
	recipPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(recipient) failed: %v", err)
	}

	minerAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, minerPriv.PublicKeyBytes())
	senderAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, senderPriv.PublicKeyBytes())
	recipAddr, _ := PubToAddress(pqsig.SigAlgMLDSA65, recipPriv.PublicKeyBytes())

	// Sign one canonical ML-DSA-65 template transaction and derive distinct nonces (or clone)
	// For realistic ML-DSA-65 payloads without spending test CPU signing 375 dilithium keys sequentially,
	// reuse a valid signed 5,320-byte ML-DSA-65 transaction with unique Nonce and verify the first & last.
	baseTx := Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    1 * CoinUnit,
		Fee:       1000,
		Nonce:     1,
	}
	if err := baseTx.Sign(senderPriv); err != nil {
		t.Fatalf("Sign baseTx failed: %v", err)
	}

	cbTx := Transaction{
		Sender:    "SYSTEM",
		Recipient: minerAddr,
		Amount:    InitialReward,
		Fee:       0,
		Nonce:     20001,
		Signature: "REWARD_SIG",
	}

	prevHash := fmt.Sprintf("%064x", 0xabc123)
	target := DifficultyToTarget(7)
	blk := Block{
		Index:        20001,
		Timestamp:    1765000000,
		Transactions: []Transaction{cbTx},
		PrevHash:     prevHash,
		Hash:         fmt.Sprintf("%064x", 0xdef456),
		Nonce:        987654321,
		Miner:        minerAddr,
		Difficulty:   7,
		Target:       fmt.Sprintf("%064x", target),
	}

	// Pack as many ML-DSA-65 standard transactions as fit within MaxBlockSize (2,000,000 bytes).
	regularTxCount := 0
	for i := 1; i <= MaxBlockTransactions-1; i++ {
		tx := baseTx
		tx.Nonce = int64(i)
		tx.InvalidateCache()
		blk.Transactions = append(blk.Transactions, tx)
		blk.InvalidateCache()
		if blk.BinarySize() > MaxBlockSize {
			blk.Transactions = blk.Transactions[:len(blk.Transactions)-1]
			blk.InvalidateCache()
			break
		}
		regularTxCount++
	}

	blk.MerkleRoot = CalculateMerkleRoot(blk.Transactions)
	blk.InvalidateCache()

	binSize := blk.BinarySize()
	if binSize <= 0 || binSize > MaxBlockSize {
		t.Fatalf("packed binary block size %d out of bounds (1..%d)", binSize, MaxBlockSize)
	}

	// Verify 2 MB block accommodates ~375 standard ML-DSA-65 txs (between 350 and 400).
	if regularTxCount < 350 || regularTxCount > 400 {
		t.Fatalf("expected 2 MB binary block capacity within 350..400 regular ML-DSA-65 txs, got %d (total with coinbase: %d, size: %d bytes)",
			regularTxCount, len(blk.Transactions), binSize)
	}

	// Confirm same transaction count under JSON encoding far exceeds MaxBlockSize (previously capped ~185 txs).
	jsonSize := blk.Size()
	if jsonSize <= MaxBlockSize {
		t.Fatalf("expected JSON size for %d txs to exceed MaxBlockSize (%d), got %d", regularTxCount, MaxBlockSize, jsonSize)
	}

	// Round-trip whole 2 MB binary block and verify exact size and field equality.
	blkBytes, err := blk.MarshalBinary()
	if err != nil {
		t.Fatalf("Block.MarshalBinary failed: %v", err)
	}
	if len(blkBytes) != binSize {
		t.Fatalf("Block.BinarySize() (%d) != len(Block.MarshalBinary()) (%d)", binSize, len(blkBytes))
	}

	var decodedBlk Block
	if err := decodedBlk.UnmarshalBinary(blkBytes); err != nil {
		t.Fatalf("Block.UnmarshalBinary failed: %v", err)
	}
	if decodedBlk.Index != blk.Index ||
		decodedBlk.Timestamp != blk.Timestamp ||
		decodedBlk.PrevHash != blk.PrevHash ||
		decodedBlk.MerkleRoot != blk.MerkleRoot ||
		decodedBlk.Hash != blk.Hash ||
		decodedBlk.Nonce != blk.Nonce ||
		decodedBlk.Miner != blk.Miner ||
		decodedBlk.Difficulty != blk.Difficulty ||
		decodedBlk.Target != blk.Target ||
		len(decodedBlk.Transactions) != len(blk.Transactions) {
		t.Fatalf("decoded binary block header or tx count mismatch")
	}
	if !decodedBlk.Transactions[0].IsCoinbase() {
		t.Fatalf("decoded tx[0] lost Coinbase flag")
	}
	if decodedBlk.Transactions[1].HashHex() != blk.Transactions[1].HashHex() ||
		decodedBlk.Transactions[len(decodedBlk.Transactions)-1].Nonce != int64(regularTxCount) {
		t.Fatalf("decoded block boundary transactions mismatch")
	}

	t.Logf("2 MB Binary Block Capacity: %d regular ML-DSA-65 txs + 1 Coinbase = %d total txs (%d binary bytes vs %d JSON bytes)",
		regularTxCount, len(blk.Transactions), binSize, jsonSize)
}

func TestBlockBinarySizeActivation_BackwardCompatibility(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()

	savedNet := ActiveNetwork
	ActiveNetwork = TestnetProfile
	defer func() { ActiveNetwork = savedNet }()
	ResetBlockBinarySizeActivationHeightOverride()
	defer ResetBlockBinarySizeActivationHeightOverride()

	if TestnetProfile.BlockBinarySizeActivationHeight != 20000 {
		t.Fatalf("expected TestnetProfile.BlockBinarySizeActivationHeight == 20000, got %d", TestnetProfile.BlockBinarySizeActivationHeight)
	}

	if IsBlockBinarySizeActive(15967) {
		t.Fatalf("Block #15,967 must stay on legacy JSON sizing for v1.0.10 compatibility")
	}
	if IsBlockBinarySizeActive(19999) {
		t.Fatalf("Block #19,999 must stay on legacy JSON sizing prior to #20,000 activation")
	}
	if !IsBlockBinarySizeActive(20000) || !IsBlockBinarySizeActive(20001) {
		t.Fatalf("Block >= 20,000 must activate binary size evaluation")
	}

	senderPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(sender) failed: %v", err)
	}
	recipPriv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(recipient) failed: %v", err)
	}
	senderAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, senderPriv.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress(sender) failed: %v", err)
	}
	recipAddr, err := PubToAddress(pqsig.SigAlgMLDSA65, recipPriv.PublicKeyBytes())
	if err != nil {
		t.Fatalf("PubToAddress(recipient) failed: %v", err)
	}

	baseTx := Transaction{
		Sender:    senderAddr,
		Recipient: recipAddr,
		Amount:    5 * CoinUnit,
		Fee:       10_000,
		Nonce:     1,
		Type:      TxTypeStandard,
	}
	if err := baseTx.Sign(senderPriv); err != nil {
		t.Fatalf("baseTx.Sign failed: %v", err)
	}

	buildSignedBlock := func(height int, numRegular int) Block {
		cb := Transaction{
			Sender:    "SYSTEM",
			Recipient: recipAddr,
			Amount:    InitialReward + int64(numRegular)*baseTx.Fee,
			Fee:       0,
			Nonce:     int64(height),
			Signature: "REWARD_SIG",
			SigAlg:    pqsig.SigAlgMLDSA65,
		}
		txs := make([]Transaction, 0, numRegular+1)
		txs = append(txs, cb)
		for i := 1; i <= numRegular; i++ {
			tx := baseTx
			tx.Nonce = int64(i)
			tx.InvalidateCache()
			txs = append(txs, tx)
		}
		target := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		blk := *NewBlock(txs, strings.Repeat("aa", 32), height, recipAddr, target)
		blk.Timestamp = 1710000000 + int64(height)*120
		blk.Hash = blk.CalculateHash()
		return blk
	}

	// Build a 200-tx block (~2.16 MB JSON > MaxBlockSize, ~1.06 MB Binary < MaxBlockSize)
	blk200At15967 := buildSignedBlock(15967, 200)
	if blk200At15967.Size() <= MaxBlockSize || blk200At15967.BinarySize() >= MaxBlockSize {
		t.Fatalf("unexpected fixture sizes: JSON=%d, Binary=%d", blk200At15967.Size(), blk200At15967.BinarySize())
	}
	if got := EffectiveBlockSize(&blk200At15967); got != blk200At15967.Size() {
		t.Fatalf("Block #15,967 EffectiveBlockSize got %d, want JSON Size() %d", got, blk200At15967.Size())
	}

	blk200At19999 := buildSignedBlock(19999, 200)
	if got := EffectiveBlockSize(&blk200At19999); got != blk200At19999.Size() {
		t.Fatalf("Block #19,999 EffectiveBlockSize got %d, want JSON Size() %d", got, blk200At19999.Size())
	}

	genTest := GetTestnetGenesisBlock()
	prev15966 := Block{Index: 15966, Hash: blk200At15967.PrevHash, Timestamp: blk200At15967.Timestamp - 120}
	if err := validateBlockContext(&blk200At15967, &prev15966, []Block{genTest, prev15966}, true); err == nil || !strings.Contains(err.Error(), "exceeds MaxBlockSize") {
		t.Fatalf("expected Block #15,967 with 200 JSON txs to be rejected with MaxBlockSize error (v1.0.10 parity), got: %v", err)
	}
	prev19998 := Block{Index: 19998, Hash: blk200At19999.PrevHash, Timestamp: blk200At19999.Timestamp - 120}
	if err := validateBlockContext(&blk200At19999, &prev19998, []Block{genTest, prev19998}, true); err == nil || !strings.Contains(err.Error(), "exceeds MaxBlockSize") {
		t.Fatalf("expected Block #19,999 with 200 JSON txs to be rejected with MaxBlockSize error, got: %v", err)
	}

	// At Block #20,000, EffectiveBlockSize switches to BinarySize() -> fits up to 375 regular + 1 coinbase = 376 txs!
	blk375At20000 := buildSignedBlock(20000, 375)
	if got := EffectiveBlockSize(&blk375At20000); got != blk375At20000.BinarySize() {
		t.Fatalf("Block #20,000 EffectiveBlockSize got %d, want BinarySize() %d", got, blk375At20000.BinarySize())
	}
	if EffectiveBlockSize(&blk375At20000) > MaxBlockSize {
		t.Fatalf("Block #20,000 with 376 total txs should fit inside MaxBlockSize: got %d", EffectiveBlockSize(&blk375At20000))
	}
	// Verify validateBlockContext accepts size at #20,000 (easy target override so target check passes cleanly)
	easyTarget := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	oldHook := targetOverrideHook
	targetOverrideHook = func() *big.Int { return easyTarget }
	defer func() { targetOverrideHook = oldHook }()

	prev19999 := Block{Index: 19999, Hash: blk375At20000.PrevHash, Timestamp: blk375At20000.Timestamp - 120}
	if err := validateBlockContext(&blk375At20000, &prev19999, []Block{genTest, prev19999}, true); err != nil {
		t.Fatalf("expected Block #20,000 with 376 ML-DSA-65 txs (%d binary bytes) to pass validateBlockContext, got: %v",
			blk375At20000.BinarySize(), err)
	}

	// 390 regular txs exceed MaxBlockSize even in binary encoding and must still be rejected at #20,000.
	blk390At20000 := buildSignedBlock(20000, 390)
	if blk390At20000.BinarySize() <= MaxBlockSize {
		t.Fatalf("expected 390 regular txs to exceed MaxBlockSize even in binary, got %d", blk390At20000.BinarySize())
	}
	if err := validateBlockContext(&blk390At20000, &prev19999, []Block{genTest, prev19999}, true); err == nil || !strings.Contains(err.Error(), "exceeds MaxBlockSize") {
		t.Fatalf("expected oversized 390-tx binary block at #20,000 to be rejected with MaxBlockSize, got: %v", err)
	}
}

func TestMainnetRemainLocked_WithGenesisASERTAndBinaryReady(t *testing.T) {
	unlock := AcquireNetworkTestLock()
	defer unlock()

	if MainnetLaunched {
		t.Fatalf("CRITICAL: core.MainnetLaunched must remain false prior to official Mainnet release")
	}

	oldAllowMainnetHook := allowMainnetInTestsHook
	allowMainnetInTestsHook = nil
	if !IsLocalMainnet() {
		if _, err := ResolveCLINetwork("mainnet"); err == nil || !strings.Contains(err.Error(), "Mainnet has not been launched") {
			allowMainnetInTestsHook = oldAllowMainnetHook
			t.Fatalf("expected ResolveCLINetwork(\"mainnet\") to reject when MainnetLaunched=false, got err=%v", err)
		}
	}
	allowMainnetInTestsHook = oldAllowMainnetHook

	if MainnetProfile.ArmaxHashActivationHeight != -1 {
		t.Fatalf("expected MainnetProfile.ArmaxHashActivationHeight == -1, got %d", MainnetProfile.ArmaxHashActivationHeight)
	}
	if MainnetProfile.ArmaxHashV2ActivationHeight != 1 {
		t.Fatalf("expected MainnetProfile.ArmaxHashV2ActivationHeight == 1, got %d", MainnetProfile.ArmaxHashV2ActivationHeight)
	}
	if MainnetProfile.ASERTActivationHeight != 0 {
		t.Fatalf("expected MainnetProfile.ASERTActivationHeight == 0, got %d", MainnetProfile.ASERTActivationHeight)
	}
	if MainnetProfile.FutureTimestampLimitActivationHeight != 0 {
		t.Fatalf("expected MainnetProfile.FutureTimestampLimitActivationHeight == 0, got %d", MainnetProfile.FutureTimestampLimitActivationHeight)
	}
	if MainnetProfile.BlockBinarySizeActivationHeight != 0 {
		t.Fatalf("expected MainnetProfile.BlockBinarySizeActivationHeight == 0, got %d", MainnetProfile.BlockBinarySizeActivationHeight)
	}
	if MainnetProfile.TimestampRulesActivationHeight != 0 {
		t.Fatalf("expected MainnetProfile.TimestampRulesActivationHeight == 0, got %d", MainnetProfile.TimestampRulesActivationHeight)
	}
	if MainnetProfile.AntiStallActivationHeight != -1 {
		t.Fatalf("expected MainnetProfile.AntiStallActivationHeight == -1 on Mainnet, got %d", MainnetProfile.AntiStallActivationHeight)
	}

	savedNet := ActiveNetwork
	ActiveNetwork = MainnetProfile
	defer func() { ActiveNetwork = savedNet }()

	EnableLocalMainnet()
	defer DisableLocalMainnet()

	ResetArmaxHashActivationHeightOverride()
	ResetArmaxHashV2ActivationHeightOverride()
	ResetASERTActivationHeightOverride()
	ResetFutureTimestampLimitActivationHeightOverride()
	ResetTimestampRulesActivationHeightOverride()
	ResetBlockBinarySizeActivationHeightOverride()
	ResetASERTDriftCache()
	defer ResetASERTDriftCache()

	gen := GetMainnetGenesisBlock()
	if gen.PoWAlgorithm() != "sha256d" {
		t.Fatalf("expected Mainnet Genesis #0 PoWAlgorithm == sha256d, got %s", gen.PoWAlgorithm())
	}
	if err := ValidateGenesisBlock(&gen); err != nil {
		t.Fatalf("expected ValidateGenesisBlock(&gen) == nil, got %v", err)
	}

	if IsArmaxHashActive(1) {
		t.Fatalf("expected Mainnet IsArmaxHashActive(1) == false (v1 skipped)")
	}
	if !IsArmaxHashV2Active(1) || !IsAnyArmaxHashActive(1) {
		t.Fatalf("expected Mainnet ArmaxHash v2 active at Block #1 without overrides")
	}
	if (&Block{Index: 1}).PoWAlgorithm() != "armaxhash-v2" {
		t.Fatalf("expected Mainnet Block #1 PoWAlgorithm == armaxhash-v2")
	}
	if !IsASERTActive(1) || GetCurrentDAAEngineName(1) != "Armax-ASERT-AW" {
		t.Fatalf("expected Mainnet ASERT to be natively active from block #1")
	}
	if MaxFutureTimestampSeconds(1) != FutureTimestampLimitSeconds {
		t.Fatalf("expected Mainnet C6 600s limit from block #1, got %d", MaxFutureTimestampSeconds(1))
	}
	if !IsTimestampRulesActive(1) {
		t.Fatalf("expected Mainnet C1/C2 timestamp rules active from block #1")
	}
	if IsAntiStallActive(1) || IsAntiStallActive(100000) {
		t.Fatalf("Mainnet must permanently disable TEDR Anti-Stall even with ASERT active")
	}
	if !IsBlockBinarySizeActive(0) || !IsBlockBinarySizeActive(1) {
		t.Fatalf("expected Mainnet BlockBinarySize to be ready from Genesis #0")
	}

	anchorBlk, anchorTarget, err := ResolveASERTAnchor([]Block{gen}, 0)
	if err != nil {
		t.Fatalf("ResolveASERTAnchor on Mainnet Genesis #0 failed: %v", err)
	}
	armaxLimit := GetArmaxHashPowLimit()
	if anchorBlk.Index != 0 || anchorBlk.Hash != gen.Hash || anchorTarget.Cmp(armaxLimit) != 0 {
		t.Fatalf("expected Mainnet ASERT anchor to be Genesis #0 (%s) with ArmaxHashPowLimit (%064x), got index=%d hash=%s target=%064x",
			gen.Hash, armaxLimit, anchorBlk.Index, anchorBlk.Hash, anchorTarget)
	}

	// Verify CalculateArmaxASERTTarget operates cleanly at ArmaxHashPowLimit scale for Block #1
	tgt1 := CalculateArmaxASERTTarget([]Block{gen}, 1, gen.Timestamp+120)
	if tgt1 == nil || tgt1.Cmp(armaxLimit) != 0 {
		t.Fatalf("expected nominal Block #1 target to equal ArmaxHashPowLimit (%064x), got %064x", armaxLimit, tgt1)
	}

	// Verify validateBlockContext rejects a Block #1 whose hash is forged with legacy SHA-256d
	cb := Transaction{Sender: "SYSTEM", Recipient: GenesisMiner, Amount: InitialReward, Nonce: 1, Signature: "REWARD_SIG"}
	forged := NewBlock([]Transaction{cb}, gen.Hash, 1, GenesisMiner, tgt1)
	forged.Timestamp = gen.Timestamp + 120
	forged.Hash = forged.CalculateSha256dHash()
	if err := validateBlockContext(forged, &gen, []Block{gen}, true); err == nil || !strings.Contains(err.Error(), "calculated hash mismatch") {
		t.Fatalf("expected validateBlockContext to reject SHA-256d Block #1 on Mainnet, got: %v", err)
	}

	// Verify EffectiveTxSize & EffectiveBlockSize use BinarySize() from Genesis #0 on Mainnet
	priv, _, err := pqsig.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	addr, _ := PubToAddress(pqsig.SigAlgMLDSA65, priv.PublicKeyBytes())
	tx := Transaction{Sender: addr, Recipient: addr, Amount: 1000, Fee: 1000, Nonce: 1}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if got := EffectiveTxSize(&tx, 1); got != 5320 || got != tx.BinarySize() {
		t.Fatalf("expected Mainnet EffectiveTxSize == BinarySize (5320), got %d", got)
	}
	blk := NewBlock([]Transaction{tx}, gen.Hash, 1, addr, anchorTarget)
	if got := EffectiveBlockSize(blk); got != blk.BinarySize() {
		t.Fatalf("expected Mainnet EffectiveBlockSize == BinarySize (%d), got %d", blk.BinarySize(), got)
	}
}
