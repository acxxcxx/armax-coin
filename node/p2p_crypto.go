package node

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	"strings"
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

// GetNodeTLSCertPath returns the persistent PEM file path for P2P TLS in data dir [F1-BLOCKER, T11]
func GetNodeTLSCertPath(dataDir ...string) string {
	subDir := ""
	if netName := core.GetActiveNetwork().Name; netName == "testnet" || netName == "regtest" || netName == "soak" {
		subDir = netName
	}

	if len(dataDir) > 0 && dataDir[0] != "" {
		targetDir := dataDir[0]
		if subDir != "" && filepath.Base(dataDir[0]) != subDir {
			targetDir = filepath.Join(dataDir[0], subDir)
		}
		_ = os.MkdirAll(targetDir, 0700)
		return filepath.Join(targetDir, "p2ptls.pem")
	}

	if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
		targetDir := envDir
		if subDir != "" && filepath.Base(envDir) != subDir {
			targetDir = filepath.Join(envDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0700)
		return filepath.Join(targetDir, "p2ptls.pem")
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		targetDir := filepath.Join(homeDir, ".armax")
		if subDir != "" {
			targetDir = filepath.Join(targetDir, subDir)
		}
		_ = os.MkdirAll(targetDir, 0700)
		return filepath.Join(targetDir, "p2ptls.pem")
	}

	baseDir := "."
	if subDir != "" {
		baseDir = subDir
		_ = os.MkdirAll(baseDir, 0700)
	}
	return filepath.Join(baseDir, "p2ptls.pem")
}

// loadOrGenerateNodeTLSCertificate loads persistent P2P TLS keypair from p2ptls.pem,
// or generates a new one with 10-year validity if none exists, persisting it with 0600 permissions [F1-BLOCKER, T11].
func loadOrGenerateNodeTLSCertificate(dataDir ...string) (tls.Certificate, error) {
	tlsPath := GetNodeTLSCertPath(dataDir...)
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

// GetNodeTLSConfig returns a thread-safe cloned copy of the active node TLS config.
func GetNodeTLSConfig() *tls.Config {
	nodeTLSMu.RLock()
	defer nodeTLSMu.RUnlock()
	if nodeTLSConfig == nil {
		return nil
	}
	return nodeTLSConfig.Clone()
}

// InitNodeIdentity initializes or loads the persistent P2P TLS identity and returns its TLS config and SHA-256 fingerprint [T11]
func InitNodeIdentity(dataDir ...string) (*tls.Config, [32]byte, error) {
	nodeTLSMu.Lock()
	defer nodeTLSMu.Unlock()

	cert, err := loadOrGenerateNodeTLSCertificate(dataDir...)
	if err != nil {
		return nil, [32]byte{}, err
	}
	nodeTLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	var fp [32]byte
	if len(cert.Certificate) > 0 {
		fp = sha256.Sum256(cert.Certificate[0])
	}
	return nodeTLSConfig.Clone(), fp, nil
}

// EnsureNodeTLSConfig initializes or reloads nodeTLSConfig for the active network profile [F1-BLOCKER, T11]
func EnsureNodeTLSConfig(dataDir ...string) (*tls.Config, error) {
	conf, _, err := InitNodeIdentity(dataDir...)
	return conf, err
}

func init() {
	clientTLSConf = &tls.Config{
		// The handshake is deliberately not chain-verified: nodes use self-signed identities.
		// Peer authentication is performed after the handshake by TOFU fingerprint pinning
		// (see PeerServer P2PTOFU); without TOFU the transport is encryption-only [N-08].
		InsecureSkipVerify: true,
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
	return UpgradeToEncryptedTransportWithConfig(rawConn, nil)
}

// UpgradeToEncryptedTransportWithConfig wraps rawConn with TLS if incoming data is TLS 1.3,
// using the provided tls.Config or falling back to the global node identity TLS config.
func UpgradeToEncryptedTransportWithConfig(rawConn net.Conn, conf *tls.Config) (net.Conn, bool) {
	var activeConf *tls.Config
	if conf != nil {
		activeConf = conf.Clone()
	} else {
		activeConf = GetNodeTLSConfig()
		if activeConf == nil {
			var err error
			activeConf, err = EnsureNodeTLSConfig()
			if err != nil || activeConf == nil {
				if IsStrictTLS() {
					_ = rawConn.Close()
					return nil, false
				}
				return rawConn, false
			}
		}
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
		tlsConn := tls.Server(wrapped, activeConf)
		return tlsConn, true
	}

	// Plaintext fallback (rejected if strict TLS is enforced)
	if IsStrictTLS() {
		_ = rawConn.Close()
		return nil, false
	}
	return &bufferedConn{Conn: rawConn, r: br}, false
}

// ResolveAndValidateLoopback verifies that addr resolves exclusively to valid loopback destinations.
// In local mainnet mode:
// 1. Literal loopback IPs (IPv4 127.0.0.0/8, IPv6 ::1, IPv4-mapped IPv6) are accepted directly.
// 2. Only "localhost" is permitted as a hostname; arbitrary external hostnames are strictly rejected without external DNS lookups.
// 3. For "localhost", resolution is performed exactly once and every returned IP must be loopback.
// 4. Returns targetAddrs as concrete "verified_ip:port" candidates (e.g. ::1 and 127.0.0.1) so dialers
//    connect directly to verified IPs without triggering secondary DNS lookups (eliminating DNS rebinding and TOCTOU vulnerabilities).
// 5. Returns serverName to preserve TLS SNI and certificate validation.
func ResolveAndValidateLoopback(addr string) (targetAddrs []string, serverName string, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, "", fmt.Errorf("local mainnet mode: invalid host:port format %q: %w", addr, err)
	}
	if host == "" || portStr == "" {
		return nil, "", fmt.Errorf("local mainnet mode: empty host or port in address %q", addr)
	}
	hostClean := strings.TrimPrefix(host, "[")
	hostClean = strings.TrimSuffix(hostClean, "]")

	// 1. Literal IP check (IPv4, IPv6, IPv4-mapped IPv6)
	ip := net.ParseIP(hostClean)
	if ip != nil {
		if !isLoopbackIP(ip) {
			return nil, "", fmt.Errorf("local mainnet mode strictly prohibits dialing non-loopback peer %q", addr)
		}
		return []string{net.JoinHostPort(ip.String(), portStr)}, hostClean, nil
	}

	// 2. Hostname restriction: strictly limit to "localhost" to avoid arbitrary external DNS resolution
	if !strings.EqualFold(hostClean, "localhost") {
		return nil, "", fmt.Errorf("local mainnet mode rejects arbitrary hostname %q: only loopback IPs and 'localhost' are permitted", hostClean)
	}

	// 3. Resolve "localhost" once and verify all returned addresses are loopback
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resolvedIPs, err := net.DefaultResolver.LookupHost(ctx, hostClean)
	if err != nil || len(resolvedIPs) == 0 {
		return nil, "", fmt.Errorf("local mainnet mode: failed to resolve host %q: %w", hostClean, err)
	}
	var targets []string
	for _, rip := range resolvedIPs {
		parsed := net.ParseIP(rip)
		if parsed == nil || !isLoopbackIP(parsed) {
			return nil, "", fmt.Errorf("local mainnet mode: host %q resolved to non-loopback IP %s", hostClean, rip)
		}
		targets = append(targets, net.JoinHostPort(parsed.String(), portStr))
	}

	return targets, hostClean, nil
}

// ValidateLoopbackAddress checks that addr is a valid loopback destination.
// Returns an error if parsing fails, resolution fails, or any resolved destination is non-loopback.
func ValidateLoopbackAddress(addr string) error {
	_, _, err := ResolveAndValidateLoopback(addr)
	return err
}

func isLoopbackIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Handles IPv4 (127.0.0.0/8) and IPv4-mapped IPv6 (::ffff:127.0.0.0/8)
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 127
	}
	// Handles pure IPv6 loopback (::1)
	return ip.IsLoopback()
}

// DialEncrypted attempts to establish a TLS 1.3 encrypted P2P connection first;
// if the remote peer is an unencrypted node and strict TLS is disabled, it falls back to standard TCP.
func DialEncrypted(addr string, timeout time.Duration) (net.Conn, bool, error) {
	return DialEncryptedWithFallback(addr, timeout, !IsStrictTLS())
}

// DialEncryptedWithFallbackForNetwork attempts to establish an encrypted P2P connection for a specific network ID.
// When netID indicates Mainnet and local mainnet mode is active, it strictly validates that addr resolves exclusively
// to loopback before establishing any TCP connection. Dialing non-loopback or malformed addresses returns an immediate error (fail-closed).
// The dial target is resolved to concrete loopback IP:port candidates to eliminate DNS rebinding and TOCTOU vulnerabilities.
// For Testnet or other networks, local mainnet loopback restrictions are bypassed.
func DialEncryptedWithFallbackForNetwork(netID string, addr string, timeout time.Duration, allowPlaintext bool) (net.Conn, bool, error) {
	isMainnet := false
	if netID != "" {
		canon, _, err := parseNetworkID(netID)
		if err == nil && canon == "ARMX-MAINNET-1" {
			isMainnet = true
		}
	} else if strings.EqualFold(core.GetActiveNetwork().Name, "mainnet") {
		isMainnet = true
	}

	var targets []string
	var tlsConf *tls.Config
	if clientTLSConf != nil {
		tlsConf = clientTLSConf.Clone()
	}

	if isMainnet && core.IsLocalMainnet() {
		resolvedTargets, serverName, err := ResolveAndValidateLoopback(addr)
		if err != nil {
			return nil, false, err
		}
		targets = resolvedTargets
		if tlsConf != nil && tlsConf.ServerName == "" {
			tlsConf.ServerName = serverName
		}
	} else {
		targets = []string{addr}
	}

	dialer := &net.Dialer{Timeout: timeout}

	// 1. Try TLS on verified target IPs
	if tlsConf != nil {
		var lastTLSErr error
		for _, target := range targets {
			tlsConn, err := tls.DialWithDialer(dialer, "tcp", target, tlsConf)
			if err == nil {
				return tlsConn, true, nil
			}
			lastTLSErr = err
		}
		if !allowPlaintext || IsStrictTLS() {
			return nil, false, lastTLSErr
		}
	}

	if !allowPlaintext || IsStrictTLS() {
		return nil, false, errors.New("strict TLS or TOFU enforced: unencrypted TCP fallback disabled")
	}

	// 2. Fallback to standard TCP on verified target IPs
	var lastTCPErr error
	for _, target := range targets {
		tcpConn, err := dialer.Dial("tcp", target)
		if err == nil {
			return tcpConn, false, nil
		}
		lastTCPErr = err
	}
	return nil, false, lastTCPErr
}

// DialEncryptedWithFallback preserves backward compatibility by routing through the active network profile.
func DialEncryptedWithFallback(addr string, timeout time.Duration, allowPlaintext bool) (net.Conn, bool, error) {
	return DialEncryptedWithFallbackForNetwork(core.GetActiveNetwork().NetworkID, addr, timeout, allowPlaintext)
}
