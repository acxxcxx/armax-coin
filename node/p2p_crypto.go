package node

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

var (
	nodeTLSConfig *tls.Config
	clientTLSConf *tls.Config
	strictTLS     int32 // 1 if strict TLS 1.3 is enforced (no plaintext fallback) [N-08]
	nodeTLSMu     sync.RWMutex
)

// SetStrictTLS enables or disables enforcement of strict TLS 1.3 transport without plaintext fallback [N-08]
func SetStrictTLS(enforce bool) {
	if enforce {
		atomic.StoreInt32(&strictTLS, 1)
	} else {
		atomic.StoreInt32(&strictTLS, 0)
	}
}

// IsStrictTLS returns whether strict TLS 1.3 transport is enforced
func IsStrictTLS() bool {
	return atomic.LoadInt32(&strictTLS) == 1
}

// GetNodeTLSCertPath returns the persistent PEM file path for P2P TLS in data dir [F1-BLOCKER]
func GetNodeTLSCertPath() string {
	subDir := ""
	if netName := core.GetActiveNetwork().Name; netName == "testnet" || netName == "regtest" || netName == "soak" {
		subDir = netName
	}

	if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
		targetDir := envDir
		if subDir != "" && filepath.Base(envDir) != subDir {
			targetDir = filepath.Join(envDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0755)
		return filepath.Join(targetDir, "p2ptls.pem")
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		targetDir := filepath.Join(homeDir, ".armax")
		if subDir != "" {
			targetDir = filepath.Join(targetDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0755)
		return filepath.Join(targetDir, "p2ptls.pem")
	}

	baseDir := "."
	if subDir != "" {
		baseDir = subDir
		_ = os.MkdirAll(baseDir, 0755)
	}
	return filepath.Join(baseDir, "p2ptls.pem")
}

// loadOrGenerateNodeTLSCertificate loads persistent P2P TLS keypair from p2ptls.pem,
// or generates a new one with 10-year validity if none exists, persisting it with 0600 permissions [F1-BLOCKER].
func loadOrGenerateNodeTLSCertificate() (tls.Certificate, error) {
	tlsPath := GetNodeTLSCertPath()
	if data, err := os.ReadFile(tlsPath); err == nil {
		if len(data) == 0 {
			return tls.Certificate{}, fmt.Errorf("existing P2P TLS identity %s is empty; refusing to replace it automatically", tlsPath)
		}
		cert, parseErr := tls.X509KeyPair(data, data)
		if parseErr != nil {
			return tls.Certificate{}, fmt.Errorf("existing P2P TLS identity %s is invalid; refusing to replace it automatically: %w", tlsPath, parseErr)
		}
		return cert, nil
	} else if !os.IsNotExist(err) {
		return tls.Certificate{}, fmt.Errorf("failed to read P2P TLS identity %s: %w", tlsPath, err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, err
	}

	// 10-year certificate lifetime (3650 days) for persistent peer TLS identity [F1-BLOCKER]
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Armaxcoin Decentralized P2P Network"},
			CommonName:   "armax-node-peer",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(3650 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	combined := append(certPEM, keyPEM...)
	tlsDir := filepath.Dir(tlsPath)
	if err := os.MkdirAll(tlsDir, 0700); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create TLS directory: %w", err)
	}

	// Atomic write with unique temporary file for concurrent safety [Follow-up Point 6]
	tmpFile, err := os.CreateTemp(tlsDir, "p2ptls-*.tmp")
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create temporary TLS cert file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	_ = os.Chmod(tmpPath, 0600)
	if _, err := tmpFile.Write(combined); err != nil {
		_ = tmpFile.Close()
		return tls.Certificate{}, fmt.Errorf("failed to write TLS cert data: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return tls.Certificate{}, fmt.Errorf("failed to sync TLS cert file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to close TLS cert file: %w", err)
	}
	if err := os.Rename(tmpPath, tlsPath); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to atomically rename TLS cert file: %w", err)
	}
	tmpPath = "" // rename succeeded
	_ = os.Chmod(tlsPath, 0600)

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}, nil
}

// EnsureNodeTLSConfig initializes or reloads nodeTLSConfig for the active network profile [F1-BLOCKER]
func EnsureNodeTLSConfig() (*tls.Config, error) {
	nodeTLSMu.Lock()
	defer nodeTLSMu.Unlock()

	cert, err := loadOrGenerateNodeTLSCertificate()
	if err != nil {
		return nil, err
	}
	nodeTLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	return nodeTLSConfig, nil
}

func init() {
	_, _ = EnsureNodeTLSConfig()
	clientTLSConf = &tls.Config{
		InsecureSkipVerify: true, // Opportunistic P2P transport (self-signed, unauthenticated TLS 1.3) [N-08]
		MinVersion:         tls.VersionTLS13,
	}
}

// bufferedConn wraps net.Conn to replay a peeked byte
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

// UpgradeToEncryptedTransport detects whether an incoming connection is TLS 1.3 (starts with 0x16)
// or plaintext JSON, and seamlessly wraps it with TLS if encrypted.
// When strict TLS is enabled, plaintext inbound connections are rejected and nil is returned [N-08].
func UpgradeToEncryptedTransport(rawConn net.Conn) (net.Conn, bool) {
	if nodeTLSConfig == nil {
		_, _ = EnsureNodeTLSConfig()
	}
	if nodeTLSConfig == nil {
		if IsStrictTLS() {
			_ = rawConn.Close()
			return nil, false
		}
		return rawConn, false
	}

	br := bufio.NewReader(rawConn)
	firstByte, err := br.Peek(1)
	if err != nil {
		if IsStrictTLS() {
			_ = rawConn.Close()
			return nil, false
		}
		return rawConn, false
	}

	// 0x16 is the standard TLS Handshake record type
	if firstByte[0] == 0x16 {
		wrapped := &bufferedConn{Conn: rawConn, r: br}
		tlsConn := tls.Server(wrapped, nodeTLSConfig)
		return tlsConn, true
	}

	// Plaintext fallback (rejected if strict TLS is enforced)
	if IsStrictTLS() {
		_ = rawConn.Close()
		return nil, false
	}
	return &bufferedConn{Conn: rawConn, r: br}, false
}

// DialEncrypted attempts to establish a TLS 1.3 encrypted P2P connection first;
// if the remote peer is an unencrypted node and strict TLS is disabled, it falls back to standard TCP.
func DialEncrypted(addr string, timeout time.Duration) (net.Conn, bool, error) {
	return DialEncryptedWithFallback(addr, timeout, !IsStrictTLS())
}

// DialEncryptedWithFallback attempts to establish a TLS 1.3 encrypted P2P connection first.
// If allowPlaintext is false, it refuses to dial or fall back to unencrypted TCP [N-08, F1].
func DialEncryptedWithFallback(addr string, timeout time.Duration, allowPlaintext bool) (net.Conn, bool, error) {
	dialer := &net.Dialer{Timeout: timeout}

	if clientTLSConf != nil {
		tlsConn, err := tls.DialWithDialer(dialer, "tcp", addr, clientTLSConf)
		if err == nil {
			return tlsConn, true, nil
		}
		if !allowPlaintext || IsStrictTLS() {
			return nil, false, err
		}
	}

	if !allowPlaintext || IsStrictTLS() {
		return nil, false, errors.New("strict TLS or TOFU enforced: unencrypted TCP fallback disabled")
	}

	// Fallback to standard TCP
	tcpConn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, false, err
	}
	return tcpConn, false, nil
}
