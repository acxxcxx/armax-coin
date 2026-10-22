package node

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acxxcxx/armax-coin/core"
	"github.com/acxxcxx/armax-coin/core/pqsig"
)

func init() {
	if os.Getenv("ARMX_DATA_DIR") == "" {
		_ = os.Setenv("ARMX_DATA_DIR", os.TempDir())
	}
}

func getFreePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind ephemeral port: %v", err)
	}
	defer l.Close()
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
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
	t.Helper()
	name := t.Name()
	var subId string
	if len(id) > 0 {
		subId = id[0]
	} else {
		val, _ := testAccountCounters.LoadOrStore(name, new(uint64))
		counter := val.(*uint64)
		curr := atomic.AddUint64(counter, 1)
		subId = fmt.Sprintf("acc_%d", curr)
	}
	seed := sha256.Sum256([]byte(fmt.Sprintf("armax-node-test-seed:%s:%s", name, subId)))
	sk, err := pqsig.GenerateFromSeed(seed)
	if err != nil {
		t.Fatalf("newTestAccount failed: %v", err)
	}
	pub := sk.PublicKeyBytes()
	pubHex := hex.EncodeToString(pub)
	addr, err := core.PubToAddress(pqsig.SigAlgMLDSA65, pub)
	if err != nil {
		t.Fatalf("PubToAddress failed: %v", err)
	}
	addrHex, err := core.AddressToHex(addr)
	if err != nil {
		t.Fatalf("AddressToHex failed: %v", err)
	}
	return &testAccount{
		PrivKey:    sk,
		PubKey:     pub,
		PubHex:     pubHex,
		Address:    addr,
		AddressHex: addrHex,
	}
}

//lint:ignore U1000 used in tofu_production_traffic_test.go under testhooks build tag
func setPeerStoreMutationHook(fn func(op string, addr string)) {
	peerStoreMutationHookMu.Lock()
	peerStoreMutationHook = fn
	peerStoreMutationHookMu.Unlock()
}
