package node

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armaxcoincore/armax-coin/core"
)

func startNegativeHeightMockPeer(t *testing.T, cert tls.Certificate, reportedHeight int) (net.Listener, string, *atomic.Int32) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatalf("tls.Listen failed: %v", err)
	}

	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func(conn net.Conn) {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				dec := json.NewDecoder(conn)
				enc := json.NewEncoder(conn)
				var msg Message
				if err := dec.Decode(&msg); err != nil {
					return
				}
				if msg.Type == "GET_HEIGHT" {
					_ = enc.Encode(Message{
						NetworkID: msg.NetworkID,
						Type:      "HEIGHT_RESPONSE",
						Payload: HeightResponse{
							Height:     reportedHeight,
							Chainwork:  "0",
							LatestHash: core.GenesisHash,
						},
					})
				}
			}(c)
		}
	}()
	return ln, ln.Addr().String(), &conns
}

func TestCodeQL_G1_RequestChainSyncRejectsNegativeHeight(t *testing.T) {
	ClearPeerServers()
	defer ClearPeerServers()

	t.Setenv("ARMX_DATA_DIR", t.TempDir())
	t.Setenv("ARMX_ALLOW_PRIVATE_PEERS", "1")

	certConf, _, err := InitNodeIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("InitNodeIdentity failed: %v", err)
	}

	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "bc.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath: %v", err)
	}
	defer bc.Close()

	for _, badHeight := range []int{-1, -10, math.MinInt} {
		ps := NewPeerServer(getFreePort(t), bc, t.TempDir())
		ps.AllowPrivatePeers = true
		ps.RequireTLS = false
		RegisterPeerServer(ps)

		ln, peerAddr, dialCount := startNegativeHeightMockPeer(t, certConf.Certificates[0], badHeight)
		ps.RequestChainSync(peerAddr)

		_ = ln.Close()
		UnregisterPeerServer(ps)
		ps.Stop()

		if dialCount.Load() > 1 {
			t.Fatalf("negative peer height %d triggered unexpected outbound block-stream dial (conns=%d)", badHeight, dialCount.Load())
		}
	}
}

func TestCodeQL_G2_POSTMineRejectsExcessiveOrNegativeThreads(t *testing.T) {
	t.Setenv("ARMX_DATA_DIR", t.TempDir())

	bc, err := core.NewBlockchainWithStoragePath(1, filepath.Join(t.TempDir(), "bc.db"))
	if err != nil {
		t.Fatalf("NewBlockchainWithStoragePath: %v", err)
	}
	defer bc.Close()

	acc := newTestAccount(t, "miner")
	limiter := newIPRateLimiter()
	defer limiter.Close()

	handler := NewRPCHandler(bc, nil, limiter, &ServerConfig{
		RPCBind: "127.0.0.1",
		RPCPort: core.TestnetRPCPort,
		MineKey: "security-test-key",
	})

	overLimit := core.MaxAllowedMiningThreads() + 1
	cases := []struct {
		name string
		url  string
		body []byte
	}{
		{
			name: "json_excessive_threads",
			url:  "/mine",
			body: func() []byte {
				b, _ := json.Marshal(map[string]interface{}{"miner": acc.Address, "threads": overLimit})
				return b
			}(),
		},
		{
			name: "json_negative_threads",
			url:  "/mine",
			body: func() []byte {
				b, _ := json.Marshal(map[string]interface{}{"miner": acc.Address, "threads": -1})
				return b
			}(),
		},
		{
			name: "query_excessive_threads",
			url:  "/mine?miner=" + acc.Address + "&threads=9999999",
		},
		{
			name: "query_non_integer_threads",
			url:  "/mine?miner=" + acc.Address + "&threads=abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.url, bytes.NewReader(tc.body))
			req.Host = "127.0.0.1"
			req.RemoteAddr = "127.0.0.1:41234"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Mine-Key", "security-test-key")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400 Bad Request for %s, got %d (%s)", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCodeQL_G4_ConstantTimeCompareKeyedHMAC(t *testing.T) {
	if !constantTimeCompare("", "") {
		t.Fatalf("expected empty strings to match")
	}
	if !constantTimeCompare("operator:secret-pass-2026", "operator:secret-pass-2026") {
		t.Fatalf("expected identical ASCII strings to match")
	}
	if !constantTimeCompare("kunci-αβγ-🔑", "kunci-αβγ-🔑") {
		t.Fatalf("expected identical Unicode strings to match")
	}
	if constantTimeCompare("operator:secret-pass-2026", "operator:secret-pass-2027") {
		t.Fatalf("expected same-length differing strings to mismatch")
	}
	if constantTimeCompare("short", "much-longer-secret-string") {
		t.Fatalf("expected length-differing strings to mismatch")
	}
	if constantTimeCompare("", "nonempty") {
		t.Fatalf("expected empty vs non-empty string to mismatch")
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !constantTimeCompare("concurrent-secret", "concurrent-secret") {
				t.Errorf("concurrent constantTimeCompare failed to match identical strings")
			}
			if constantTimeCompare("concurrent-secret", "concurrent-secreX") {
				t.Errorf("concurrent constantTimeCompare falsely matched different strings")
			}
		}()
	}
	wg.Wait()
}

func TestCodeQL_G5_ClientTLSConfigVerifyConnection(t *testing.T) {
	if clientTLSConf == nil {
		t.Fatalf("expected clientTLSConf to be initialized")
	}
	if clientTLSConf.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 minimum version, got %#x", clientTLSConf.MinVersion)
	}
	if !clientTLSConf.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify=true for self-signed P2P TOFU/SPKI identity model")
	}
	if clientTLSConf.VerifyConnection == nil {
		t.Fatalf("expected VerifyConnection callback to enforce peer SPKI presence")
	}

	if err := clientTLSConf.VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Fatalf("VerifyConnection must reject empty PeerCertificates")
	}
	if err := clientTLSConf.VerifyConnection(tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}); err == nil {
		t.Fatalf("VerifyConnection must reject certificate without RawSubjectPublicKeyInfo")
	}

	conf, _, err := InitNodeIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("InitNodeIdentity failed: %v", err)
	}
	parsed, err := x509.ParseCertificate(conf.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("x509.ParseCertificate failed: %v", err)
	}
	if err := clientTLSConf.VerifyConnection(tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{parsed},
	}); err != nil {
		t.Fatalf("VerifyConnection rejected valid self-signed SPKI certificate: %v", err)
	}
}
