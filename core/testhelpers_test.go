package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core/pqsig"
)

func init() {
	if os.Getenv("ARMX_DATA_DIR") == "" {
		_ = os.Setenv("ARMX_DATA_DIR", os.TempDir())
	}
}

type testAccount struct {
	PrivKey    *pqsig.PrivateKey
	PubKey     []byte
	PubHex     string
	Address    string
	AddressHex string
}

var testAccountCounters sync.Map

func newTestAccount(t *testing.T, id ...string) *testAccount {
	name := "benchmark"
	if t != nil {
		t.Helper()
		name = t.Name()
	}
	var subId string
	if len(id) > 0 {
		subId = id[0]
	} else {
		val, _ := testAccountCounters.LoadOrStore(name, new(uint64))
		counter := val.(*uint64)
		curr := atomic.AddUint64(counter, 1)
		subId = fmt.Sprintf("acc_%d", curr)
	}
	seed := sha256.Sum256([]byte(fmt.Sprintf("armax-test-seed:%s:%s", name, subId)))
	sk, err := pqsig.GenerateFromSeed(seed)
	if err != nil {
		if t != nil {
			t.Fatalf("newTestAccount failed: %v", err)
		}
		panic(fmt.Sprintf("newTestAccount failed: %v", err))
	}
	pub := sk.PublicKeyBytes()
	pubHex := hex.EncodeToString(pub)
	addr, err := PubToAddress(pqsig.SigAlgMLDSA65, pub)
	if err != nil {
		if t != nil {
			t.Fatalf("PubToAddress failed: %v", err)
		}
		panic(fmt.Sprintf("PubToAddress failed: %v", err))
	}
	addrHex, err := AddressToHex(addr)
	if err != nil {
		if t != nil {
			t.Fatalf("AddressToHex failed: %v", err)
		}
		panic(fmt.Sprintf("AddressToHex failed: %v", err))
	}
	return &testAccount{
		PrivKey:    sk,
		PubKey:     pub,
		PubHex:     pubHex,
		Address:    addr,
		AddressHex: addrHex,
	}
}
