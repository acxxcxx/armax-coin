package node

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

type RPCAuthManager struct {
	User     string
	Password string
	Cookie   string
	lock     sync.RWMutex
}

var (
	GlobalAuth             = &RPCAuthManager{}
	AllowInsecureLocalhost = false // [N-19] Opt-in flag for unauthenticated localhost requests
)

type authFailureRecord struct {
	count    int
	lastFail time.Time
}

type authLockoutTracker struct {
	sync.Mutex
	fails    map[string]authFailureRecord
	lockouts map[string]time.Time
}

var globalAuthLockout = &authLockoutTracker{
	fails:    make(map[string]authFailureRecord),
	lockouts: make(map[string]time.Time),
}

func (alt *authLockoutTracker) isLocked(ip string) bool {
	alt.Lock()
	defer alt.Unlock()
	if exp, ok := alt.lockouts[ip]; ok {
		if time.Now().Before(exp) {
			return true
		}
		delete(alt.lockouts, ip)
		delete(alt.fails, ip)
	}
	return false
}

func (alt *authLockoutTracker) recordFailure(ip string) {
	alt.Lock()
	defer alt.Unlock()
	now := time.Now()

	// Prune stale failure entries (> 15 minutes) periodically if tracking more than 1,000 IPs [v1.0.6]
	if len(alt.fails)%500 == 0 && len(alt.fails) > 1000 {
		cutoff := now.Add(-15 * time.Minute)
		for k, rec := range alt.fails {
			if rec.lastFail.Before(cutoff) {
				delete(alt.fails, k)
				delete(alt.lockouts, k)
			}
		}
	}
	// Bound total memory: enforce hard cap at 5,000 entries [v1.0.6, FIX-06]
	// Eviction Strategy:
	// 1. Pass 1: Evict expired lockouts and stale failures older than 5 minutes.
	// 2. Pass 2: If still at or above cap (5,000), evict non-locked failure records
	//    (IPs with 1-4 failures) first, strictly protecting active, unexpired lockouts
	//    from being evicted by high-volume IP flood attacks.
	// 3. Pass 3: Emergency fallback: If all entries in tracker are locked, evict excess
	//    down to 4,000 entries to prevent node memory exhaustion / OOM.
	if len(alt.fails) >= 5000 {
		cutoff := now.Add(-5 * time.Minute)
		for k, rec := range alt.fails {
			exp, isLocked := alt.lockouts[k]
			if isLocked {
				if now.After(exp) {
					delete(alt.lockouts, k)
					delete(alt.fails, k)
				}
			} else if rec.lastFail.Before(cutoff) {
				delete(alt.fails, k)
			}
		}

		// Pass 2: Prioritize evicting non-locked failures to protect active lockouts
		if len(alt.fails) >= 5000 {
			for k := range alt.fails {
				if k == ip {
					continue
				}
				if _, isLocked := alt.lockouts[k]; !isLocked {
					delete(alt.fails, k)
					if len(alt.fails) < 4000 {
						break
					}
				}
			}
		}

		// Pass 3: Emergency fallback under flood of uniquely locked IPs
		if len(alt.fails) >= 5000 {
			toEvict := len(alt.fails) - 4000
			for k := range alt.fails {
				if k == ip {
					continue
				}
				if toEvict <= 0 {
					break
				}
				delete(alt.fails, k)
				delete(alt.lockouts, k)
				toEvict--
			}
		}
	}

	rec := alt.fails[ip]
	rec.count++
	rec.lastFail = now
	alt.fails[ip] = rec

	if rec.count >= 10 {
		alt.lockouts[ip] = now.Add(10 * time.Minute)
	} else if rec.count >= 5 {
		alt.lockouts[ip] = now.Add(1 * time.Minute)
	}
}

func (alt *authLockoutTracker) recordSuccess(ip string) {
	alt.Lock()
	defer alt.Unlock()
	delete(alt.fails, ip)
	delete(alt.lockouts, ip)
}

// GetRPCAuthCookiePath returns the network-scoped cookie path shared by the
// daemon and local clients. It prevents clients from guessing based on their
// working directory when ARMX_DATA_DIR is configured.
func GetRPCAuthCookiePath() string {
	netName := core.GetActiveNetwork().Name
	subDir := netName
	if subDir == "" {
		subDir = "mainnet"
	}
	targetDir := subDir
	if envDir := os.Getenv("ARMX_DATA_DIR"); envDir != "" {
		targetDir = envDir
		if filepath.Base(envDir) != subDir {
			targetDir = filepath.Join(envDir, subDir)
		}
	}
	_ = os.MkdirAll(targetDir, 0700)
	return filepath.Join(targetDir, "armax.cookie")
}

// constantTimeCompare performs a secure, timing-attack-resistant string equality comparison
// without leaking string length via early termination or comparison length
func constantTimeCompare(a, b string) bool {
	hA := sha256.Sum256([]byte(a))
	hB := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(hA[:], hB[:]) == 1
}

// InitRPCAuth initializes authentication credentials and writes armax.cookie file.
// Propagates all errors from rand.Read, directory creation, file write, sync, and atomic rename.
// Cookie auth is NOT marked active unless the cookie file has been successfully persisted with 0600 permissions [Phase 7].
func InitRPCAuth(confUser, confPass string) error {
	GlobalAuth.lock.Lock()
	defer GlobalAuth.lock.Unlock()

	GlobalAuth.User = confUser
	GlobalAuth.Password = confPass
	GlobalAuth.Cookie = "" // Invalidate existing cookie until new one is successfully persisted

	// Generate random 16-byte hex token for cookie authentication
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("failed to generate random bytes for RPC cookie: %w", err)
	}
	cookieToken := hex.EncodeToString(tokenBytes)

	// Persist cookie to the same network-scoped path exposed to local clients.
	cookieFile := GetRPCAuthCookiePath()
	targetDir := filepath.Dir(cookieFile)

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for RPC cookie at %s: %w", targetDir, err)
	}

	cookieContent := fmt.Sprintf("__cookie__:%s\n", cookieToken)

	// Atomic write with 0600 permissions: unique temp file + sync + rename [Follow-up Point 6]
	tmpFile, err := os.CreateTemp(targetDir, "cookie-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary cookie file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	_ = os.Chmod(tmpPath, 0600)
	if _, err := tmpFile.Write([]byte(cookieContent)); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write cookie data: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync cookie file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close cookie file: %w", err)
	}
	if err := os.Rename(tmpPath, cookieFile); err != nil {
		// F-02: Do NOT speculatively delete the existing cookie file on rename failure.
		return fmt.Errorf("failed to atomically rename cookie file: %w", err)
	}
	tmpPath = "" // rename succeeded
	_ = os.Chmod(cookieFile, 0600)

	// Cookie is securely written; activate cookie auth
	GlobalAuth.Cookie = cookieToken
	return nil
}

// Authenticate verifies HTTP Basic Auth credentials from incoming RPC requests using constant-time checks
func Authenticate(r *http.Request) bool {
	GlobalAuth.lock.RLock()
	defer GlobalAuth.lock.RUnlock()

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if globalAuthLockout.isLocked(clientIP) {
		return false
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	isDirectLocal := host == "127.0.0.1" || host == "::1" || host == "localhost"

	// If request passed through a reverse proxy (X-Forwarded-For or X-Real-IP present),
	// it cannot be treated as a direct trusted localhost request.
	isProxied := r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" || r.Header.Get("Forwarded") != ""
	isTrustedLocal := isDirectLocal && !isProxied

	// [N-19] Insecure localhost bypass mode is allowed ONLY if explicitly opted in via AllowInsecureLocalhost
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		if AllowInsecureLocalhost && isTrustedLocal {
			return true
		}
		globalAuthLockout.recordFailure(clientIP)
		return false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		globalAuthLockout.recordFailure(clientIP)
		return false
	}

	payload, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		globalAuthLockout.recordFailure(clientIP)
		return false
	}

	cred := strings.SplitN(string(payload), ":", 2)
	if len(cred) != 2 {
		globalAuthLockout.recordFailure(clientIP)
		return false
	}
	user, pass := cred[0], cred[1]

	// 1. Check against explicit armax.conf credentials with constant-time comparison
	if GlobalAuth.User != "" && GlobalAuth.Password != "" {
		if constantTimeCompare(user, GlobalAuth.User) && constantTimeCompare(pass, GlobalAuth.Password) {
			globalAuthLockout.recordSuccess(clientIP)
			return true
		}
	}

	// 2. Check against .cookie token with constant-time comparison (only when non-empty)
	if GlobalAuth.Cookie != "" {
		if constantTimeCompare(user, "__cookie__") && constantTimeCompare(pass, GlobalAuth.Cookie) {
			globalAuthLockout.recordSuccess(clientIP)
			return true
		}
	}

	globalAuthLockout.recordFailure(clientIP)
	return false
}

// RequireAuth wraps an http.HandlerFunc with authentication enforcement
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !Authenticate(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Armaxcoin RPC"`)
			http.Error(w, `{"error":"401 Unauthorized: Valid RPC credentials required"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ResetRPCAuthForTesting resets auth state and clears IP lockouts (used in unit tests)
func ResetRPCAuthForTesting() {
	GlobalAuth.lock.Lock()
	defer GlobalAuth.lock.Unlock()
	GlobalAuth.User = ""
	GlobalAuth.Password = ""
	GlobalAuth.Cookie = ""

	globalAuthLockout.Lock()
	globalAuthLockout.fails = make(map[string]authFailureRecord)
	globalAuthLockout.lockouts = make(map[string]time.Time)
	globalAuthLockout.Unlock()
}
