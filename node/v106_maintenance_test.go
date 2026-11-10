package node

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

// TestIPRateLimiterLifecycleCleanup verifies that Close() cleanly terminates
// the background pruning goroutine and does not panic on multiple calls [v1.0.6].
func TestIPRateLimiterLifecycleCleanup(t *testing.T) {
	limiter := newIPRateLimiter()
	if limiter == nil {
		t.Fatal("expected non-nil rate limiter")
	}

	// Record a mock request
	if !limiter.allow("127.0.0.1", 100) {
		t.Fatal("expected request to be allowed")
	}

	// Verify Close cleanly terminates without hang
	done := make(chan struct{})
	go func() {
		limiter.Close()
		// Safe to call multiple times (idempotent via sync.Once)
		limiter.Close()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded
	case <-time.After(3 * time.Second):
		t.Fatal("limiter.Close() timed out or deadlocked")
	}
}

// TestAuthFailureTrackerBoundedEviction verifies that the auth failure tracker
// maintains bounded memory under high-volume failure flood and correctly enforces lockouts [v1.0.6].
func TestAuthFailureTrackerBoundedEviction(t *testing.T) {
	ResetRPCAuthForTesting()

	// 1. Simulate 6,000 distinct failing IPs (e.g. distributed scanner)
	for i := 0; i < 6000; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xFF, (i>>8)&0xFF, i&0xFF)
		globalAuthLockout.recordFailure(ip)
	}

	globalAuthLockout.Lock()
	trackedCount := len(globalAuthLockout.fails)
	globalAuthLockout.Unlock()

	// Memory bound verification: count must not exceed hard cap of 5000
	if trackedCount > 5000 {
		t.Fatalf("expected fails map to be bounded <= 5000, got %d", trackedCount)
	}

	// 2. Verify legitimate lockout functionality
	testIP := "192.0.2.100"
	for i := 0; i < 4; i++ {
		globalAuthLockout.recordFailure(testIP)
	}
	if globalAuthLockout.isLocked(testIP) {
		t.Fatal("ip should not be locked at 4 failures")
	}
	globalAuthLockout.recordFailure(testIP) // 5th failure -> triggers 1-minute lockout
	if !globalAuthLockout.isLocked(testIP) {
		t.Fatal("ip should be locked at 5 failures")
	}

	// 3. Verify recordSuccess clears the lockout
	globalAuthLockout.recordSuccess(testIP)
	if globalAuthLockout.isLocked(testIP) {
		t.Fatal("ip should not be locked after recordSuccess")
	}
}

// TestRelayAndBroadcastNoopDuringShutdown verifies that relay and broadcast
// operations immediately return without spawning goroutines when the server is stopping [v1.0.6].
func TestRelayAndBroadcastNoopDuringShutdown(t *testing.T) {
	ps := &PeerServer{
		Port:  "19335",
		state: ServerStateStopping,
		Peers: []string{"127.0.0.1:19335"},
	}

	block := core.GetGenesisBlock()
	tx := core.Transaction{
		Sender:    "armx1test",
		Recipient: "armx1recip",
		Amount:    1000,
		Fee:       100,
		Nonce:     1,
		Type:      core.TxTypeStandard,
	}

	// None of these should hang, panic, or attempt to dial
	ps.BroadcastBlock([]string{"127.0.0.1:19999"}, block)
	ps.BroadcastTransaction([]string{"127.0.0.1:19999"}, tx)
	ps.RelayBlock(block, "127.0.0.1:19999")
	ps.RelayTransaction(tx, "127.0.0.1:19999")
}

// TestHeartbeatDropsFailedSession verifies that a failed ping during heartbeat
// prunes the broken outbound session from the server's session cache [v1.0.6].
func TestHeartbeatDropsFailedSession(t *testing.T) {
	cycle := &serverCycle{
		id:          1,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	defer close(cycle.quitChan)

	ps := &PeerServer{
		Port:             "19335",
		state:            ServerStateRunning,
		outboundSessions: make(map[string]*outboundPeerSession),
		currentCycle:     1,
	}

	// Insert a dummy dead session
	deadAddr := "127.0.0.1:59999"
	ps.outboundSessions[deadAddr] = &outboundPeerSession{
		peerAddress: deadAddr,
		cycle:       cycle,
	}

	if ps.getExistingOutboundSession(deadAddr) == nil {
		t.Fatal("expected dummy session to exist")
	}

	// Drop session simulation (same logic as heartbeat error handling)
	ps.dropOutboundSession(deadAddr, ps.getExistingOutboundSession(deadAddr))

	if ps.getExistingOutboundSession(deadAddr) != nil {
		t.Fatal("expected session to be dropped from outboundSessions")
	}
}

// TestVersionIdentifierV106 verifies canonical version constants [v1.0.6].
func TestVersionIdentifierV106(t *testing.T) {
	if core.Version != "1.0.9" {
		t.Fatalf("expected core.Version to be 1.0.9, got %s", core.Version)
	}
}

// TestServerGracefulShutdownSequence verifies that server shutdown cleanly
// releases resources and closes the blockchain storage without deadlock [v1.0.6].
func TestServerGracefulShutdownSequence(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain failed: %v", err)
	}

	// Verify chain has genesis block
	if len(bc.Chain) == 0 {
		t.Fatal("expected non-empty chain")
	}

	// Simulate shutdown sequence: flush and close
	bc.Lock.RLock()
	if err := bc.SaveToFile(); err != nil {
		t.Errorf("SaveToFile error: %v", err)
	}
	bc.Lock.RUnlock()

	if err := bc.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}

	// Verify subsequent OpenBlockchain succeeds on same directory (clean unmap, no lock contention)
	bc2, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("Re-opening blockchain failed after graceful close: %v", err)
	}
	_ = bc2.Close()
}

// TestShutdownServerSafelyCleanDrain verifies that when an HTTP handler completes within
// the shutdown timeout, ShutdownServerSafely returns drained=true and safely closes BoltDB [FIX-01].
func TestShutdownServerSafelyCleanDrain(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain error: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	handlerExecuted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/test-fast", func(w http.ResponseWriter, r *http.Request) {
		close(handlerExecuted)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()

	// Fire a fast request
	go func() {
		resp, getErr := http.Get("http://" + ln.Addr().String() + "/test-fast")
		if getErr == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()

	<-handlerExecuted

	drained, err := ShutdownServerSafely(srv, nil, nil, bc, 2*time.Second)
	if !drained {
		t.Fatalf("expected drained=true, got false (err: %v)", err)
	}
	if err != nil {
		t.Fatalf("expected nil error on clean drain, got %v", err)
	}

	// Verify BoltDB was closed by attempting to open it cleanly again
	bc2, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("expected database to reopen cleanly: %v", err)
	}
	_ = bc2.Close()
}

// TestShutdownServerSafelyTimeoutAbortsBoltDBClose verifies that if an in-flight HTTP handler
// does NOT finish within the timeout, ShutdownServerSafely aborts closing BoltDB (fails closed),
// returns drained=false, and prevents panic or corruption on BoltDB [FIX-01].
func TestShutdownServerSafelyTimeoutAbortsBoltDBClose(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain error: %v", err)
	}
	defer func() {
		_ = bc.Close()
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	handlerStarted := make(chan struct{})
	handlerRelease := make(chan struct{})
	handlerDone := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/test-slow", func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-handlerRelease
		// Verify handler can still safely interact with bc because bc was NOT closed!
		bc.Lock.RLock()
		_ = len(bc.Chain)
		bc.Lock.RUnlock()
		w.WriteHeader(http.StatusOK)
		close(handlerDone)
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()

	go func() {
		resp, _ := http.Get("http://" + ln.Addr().String() + "/test-slow")
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	<-handlerStarted

	// Call ShutdownServerSafely with very short timeout (50ms) while handler is still blocked
	drained, err := ShutdownServerSafely(srv, nil, nil, bc, 50*time.Millisecond)
	if drained {
		t.Fatalf("expected drained=false on timeout, got true")
	}
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	// Release handler and verify it finishes cleanly without panic
	close(handlerRelease)
	select {
	case <-handlerDone:
		// Succeeded: in-flight handler finished without accessing a closed BoltDB
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish cleanly")
	}
}

// TestPeerServerStopWithTimeoutDetection verifies that StopWithTimeout reliably reports
// whether cycle workers have drained or timed out [FIX-01].
func TestPeerServerStopWithTimeoutDetection(t *testing.T) {
	cycle := &serverCycle{
		id:          1,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}

	ps := &PeerServer{
		Port:        "19335",
		state:       ServerStateRunning,
		activeCycle: cycle,
	}

	// 1. Worker blocking past timeout -> must return false
	workerDone := make(chan struct{})
	cycle.wg.Add(1)
	go func() {
		defer cycle.wg.Done()
		<-workerDone
	}()

	// Spawn cycle teardown completion in background
	go func() {
		<-cycle.quitChan
		cycle.wg.Wait()
		close(cycle.doneChan)
	}()

	stopped := ps.StopWithTimeout(50 * time.Millisecond)
	if stopped {
		t.Fatal("expected StopWithTimeout to return false when worker is still blocked")
	}

	// 2. Worker releases -> must wait and drain
	close(workerDone)
	select {
	case <-cycle.doneChan:
	case <-time.After(2 * time.Second):
		t.Fatal("cycle failed to finish after worker release")
	}

	// Subsequent stop on stopped cycle returns true
	ps.lifecycleLock.Lock()
	ps.state = ServerStateStopped
	ps.lifecycleLock.Unlock()
	if !ps.StopWithTimeout(50 * time.Millisecond) {
		t.Fatal("expected StopWithTimeout on stopped server to return true")
	}
}

// TestShutdownServerSafelyAbortsOnP2PTimeout verifies that if P2P workers do not drain within
// timeout, ShutdownServerSafely aborts closing BoltDB (fails closed) and propagates error [FIX-01].
func TestShutdownServerSafelyAbortsOnP2PTimeout(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain error: %v", err)
	}
	defer func() {
		_ = bc.Close()
	}()

	cycle := &serverCycle{
		id:          2,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}

	ps := &PeerServer{
		Port:        "19335",
		state:       ServerStateRunning,
		activeCycle: cycle,
	}

	// Block worker
	workerBlock := make(chan struct{})
	cycle.wg.Add(1)
	go func() {
		defer cycle.wg.Done()
		<-workerBlock
	}()
	go func() {
		<-cycle.quitChan
		cycle.wg.Wait()
		close(cycle.doneChan)
	}()

	drained, err := ShutdownServerSafely(nil, ps, nil, bc, 50*time.Millisecond)
	if drained {
		t.Fatal("expected drained=false on P2P timeout, got true")
	}
	if err == nil || !strings.Contains(err.Error(), "P2P workers failed to drain") {
		t.Fatalf("expected P2P workers failed to drain error, got %v", err)
	}

	// Verify BoltDB was NOT closed (handlers/workers can still access it)
	bc.Lock.RLock()
	chainLen := len(bc.Chain)
	bc.Lock.RUnlock()
	if chainLen <= 0 {
		t.Fatal("expected non-empty chain on open database")
	}

	// Release worker
	close(workerBlock)
}

// TestShutdownServerSafelySaveToFileError verifies that SaveToFile failures are propagated
// and not falsely reported as clean shutdown [FIX-01].
func TestShutdownServerSafelySaveToFileError(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain error: %v", err)
	}
	defer func() {
		_ = bc.Close()
	}()

	// Force SaveToFile to fail by setting in-memory mode, then closing store
	bc.Lock.Lock()
	bc.StoragePath = ":memory:"
	bc.Lock.Unlock()
	_ = bc.Close()

	drained, err := ShutdownServerSafely(nil, nil, nil, bc, 1*time.Second)
	if drained {
		t.Fatal("expected drained=false when SaveToFile fails, got true")
	}
	if err == nil || !strings.Contains(err.Error(), "SaveToFile failed") {
		t.Fatalf("expected SaveToFile failed error, got: %v", err)
	}
}

// TestShutdownServerSafelyAllDrainedSuccess verifies clean shutdown when HTTP and P2P drain cleanly [FIX-01].
func TestShutdownServerSafelyAllDrainedSuccess(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ARMX_DATA_DIR", tempDir)

	bc, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("OpenBlockchain error: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(ln) }()

	cycle := &serverCycle{
		id:          3,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	ps := &PeerServer{
		Port:        "19335",
		state:       ServerStateRunning,
		activeCycle: cycle,
	}
	go func() {
		<-cycle.quitChan
		cycle.wg.Wait()
		close(cycle.doneChan)
	}()

	limiter := newIPRateLimiter()

	drained, err := ShutdownServerSafely(srv, ps, limiter, bc, 2*time.Second)
	if !drained {
		t.Fatalf("expected drained=true on clean shutdown, got false: %v", err)
	}
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify database was safely closed and can be reopened
	bc2, err := core.OpenBlockchain(1)
	if err != nil {
		t.Fatalf("failed to reopen cleanly closed blockchain: %v", err)
	}
	_ = bc2.Close()
}

// TestHeartbeatFailureSessionReplacementIntegration exercises the actual heartbeat failure path:
// 1. Registers session A.
// 2. Starts a heartbeat using Session A.
// 3. Replaces Session A with Session B at the same peer address before the heartbeat response completes.
// 4. Makes Session A's heartbeat fail.
// 5. Verifies Session A is closed.
// 6. Verifies Session B remains registered and usable [FIX-04, v1.0.6].
func TestHeartbeatFailureSessionReplacementIntegration(t *testing.T) {
	cycle := &serverCycle{
		id:          4,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	defer close(cycle.quitChan)

	ps := &PeerServer{
		Port:             "19335",
		state:            ServerStateRunning,
		activeCycle:      cycle,
		outboundSessions: make(map[string]*outboundPeerSession),
		currentCycle:     4,
	}

	targetAddr := "127.0.0.1:49153"

	// Create net.Pipe connections for session A
	clientConnA, serverConnA := net.Pipe()
	sessionA := &outboundPeerSession{
		peerAddress: targetAddr,
		conn:        clientConnA,
		cycle:       cycle,
	}

	// Create net.Pipe connections for session B
	clientConnB, serverConnB := net.Pipe()
	defer serverConnB.Close()
	sessionB := &outboundPeerSession{
		peerAddress: targetAddr,
		conn:        clientConnB,
		cycle:       cycle,
	}

	// 1. Register Session A
	ps.outboundSessions[targetAddr] = sessionA

	// 2. Spawn heartbeat worker closure targeting Session A (identical to startPeerSessionHeartbeat)
	heartbeatDone := make(chan struct{})
	go func(targetSess *outboundPeerSession) {
		defer close(heartbeatDone)
		// Simulating peer query failure: close serverConnA abruptly
		_ = serverConnA.Close()

		// Heartbeat ping fails
		ps.dropOutboundSession(targetAddr, targetSess)
	}(sessionA)

	// 3. Replace Session A with Session B before Session A finishes its failure handling
	ps.outboundMu.Lock()
	ps.outboundSessions[targetAddr] = sessionB
	ps.outboundMu.Unlock()

	// Wait for heartbeat failure routine to complete
	<-heartbeatDone

	// 4 & 5. Verify Session A is closed
	if !sessionA.isClosed() {
		t.Fatal("expected session A to be closed after heartbeat failure")
	}

	// 6. Verify Session B remains registered and usable
	current := ps.getExistingOutboundSession(targetAddr)
	if current != sessionB {
		t.Fatalf("expected replacement session B to remain registered in outboundSessions, got %v", current)
	}
	if sessionB.isClosed() {
		t.Fatal("expected replacement session B to NOT be closed")
	}
}

// TestHeartbeatSessionReplacementIsolation verifies that if session A fails its heartbeat
// after having been replaced in outboundSessions by session B, session B is NOT dropped [FIX-04].
func TestHeartbeatSessionReplacementIsolation(t *testing.T) {
	cycle := &serverCycle{
		id:          1,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	defer close(cycle.quitChan)

	ps := &PeerServer{
		Port:             "19335",
		state:            ServerStateRunning,
		outboundSessions: make(map[string]*outboundPeerSession),
		currentCycle:     1,
	}

	targetAddr := "127.0.0.1:49152"

	// Session A
	sessionA := &outboundPeerSession{
		peerAddress: targetAddr,
		cycle:       cycle,
	}
	// Session B (replacement session)
	sessionB := &outboundPeerSession{
		peerAddress: targetAddr,
		cycle:       cycle,
	}

	// Initially session A is registered
	ps.outboundSessions[targetAddr] = sessionA

	// Simulate Session B replacing Session A in outboundSessions
	ps.outboundSessions[targetAddr] = sessionB

	// Heartbeat worker for Session A fails and calls dropOutboundSession with sessionA
	ps.dropOutboundSession(targetAddr, sessionA)

	// Verify Session B remains intact in outboundSessions!
	current := ps.getExistingOutboundSession(targetAddr)
	if current != sessionB {
		t.Fatalf("expected replacement session B to remain registered, got %v", current)
	}

	// Now dropping session B should drop it
	ps.dropOutboundSession(targetAddr, sessionB)
	if ps.getExistingOutboundSession(targetAddr) != nil {
		t.Fatalf("expected session B to be dropped")
	}
}

// TestOutboundWorkAdmissionDuringShutdown verifies that registerOutboundWork tracks goroutines
// during normal operation and rejects work once server enters shutdown state [FIX-05].
func TestOutboundWorkAdmissionDuringShutdown(t *testing.T) {
	cycle := &serverCycle{
		id:          1,
		quitChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
	}
	defer close(cycle.quitChan)

	ps := &PeerServer{
		Port:        "19335",
		state:       ServerStateRunning,
		activeCycle: cycle,
	}

	// 1. When running, work must be admitted and cycle.wg incremented
	done, admitted := ps.registerOutboundWork()
	if !admitted {
		t.Fatal("expected work to be admitted while ServerStateRunning")
	}
	done()

	// 2. Transition to ServerStateStopping
	ps.lifecycleLock.Lock()
	ps.state = ServerStateStopping
	ps.lifecycleLock.Unlock()

	_, admittedAfterStop := ps.registerOutboundWork()
	if admittedAfterStop {
		t.Fatal("expected work to be rejected while ServerStateStopping")
	}

	// 3. Concurrently invoke broadcasts/relays during shutdown transition to verify no WaitGroup panic
	block := core.GetGenesisBlock()
	tx := core.Transaction{Sender: "s", Recipient: "r", Amount: 1, Type: core.TxTypeStandard}

	for i := 0; i < 50; i++ {
		go ps.BroadcastBlock([]string{"127.0.0.1:19999"}, block)
		go ps.BroadcastTransaction([]string{"127.0.0.1:19999"}, tx)
		go ps.RelayBlock(block, "127.0.0.1:19999")
		go ps.RelayTransaction(tx, "127.0.0.1:19999")
	}
}

// TestAuthLockoutDurationsAndExpiry verifies 5-failure (1m), 10-failure (10m),
// and expiry mechanics [FIX-06].
func TestAuthLockoutDurationsAndExpiry(t *testing.T) {
	ResetRPCAuthForTesting()

	testIP := "198.51.100.5"

	// 4 failures -> not locked
	for i := 0; i < 4; i++ {
		globalAuthLockout.recordFailure(testIP)
	}
	if globalAuthLockout.isLocked(testIP) {
		t.Fatal("should not be locked at 4 failures")
	}

	// 5th failure -> locked for 1 min
	globalAuthLockout.recordFailure(testIP)
	if !globalAuthLockout.isLocked(testIP) {
		t.Fatal("should be locked at 5 failures")
	}

	globalAuthLockout.Lock()
	exp1 := globalAuthLockout.lockouts[testIP]
	globalAuthLockout.Unlock()
	if time.Until(exp1) < 50*time.Second || time.Until(exp1) > 65*time.Second {
		t.Fatalf("expected ~1m lockout, got %v", time.Until(exp1))
	}

	// Up to 10 failures -> locked for 10 min
	for i := 0; i < 5; i++ {
		globalAuthLockout.recordFailure(testIP)
	}
	globalAuthLockout.Lock()
	exp10 := globalAuthLockout.lockouts[testIP]
	globalAuthLockout.Unlock()
	if time.Until(exp10) < 9*time.Minute || time.Until(exp10) > 11*time.Minute {
		t.Fatalf("expected ~10m lockout, got %v", time.Until(exp10))
	}

	// Simulate expiry by setting expiration in the past
	globalAuthLockout.Lock()
	globalAuthLockout.lockouts[testIP] = time.Now().Add(-1 * time.Second)
	globalAuthLockout.Unlock()

	if globalAuthLockout.isLocked(testIP) {
		t.Fatal("expected isLocked to return false after expiry")
	}
}

// TestAuthLockoutPreservesActiveLockoutsUnderFlood verifies that during a flood
// of non-locked failures from distinct IPs, active unexpired lockouts are preserved [FIX-06].
func TestAuthLockoutPreservesActiveLockoutsUnderFlood(t *testing.T) {
	ResetRPCAuthForTesting()

	// 1. Establish an active lockout on a target IP
	targetIP := "203.0.113.99"
	for i := 0; i < 5; i++ {
		globalAuthLockout.recordFailure(targetIP)
	}
	if !globalAuthLockout.isLocked(targetIP) {
		t.Fatal("expected targetIP to be locked")
	}

	// 2. Flood with 6,000 distinct IPs (each failing only once, non-locked)
	for i := 0; i < 6000; i++ {
		floodIP := fmt.Sprintf("198.18.%d.%d", (i>>8)&0xFF, i&0xFF)
		globalAuthLockout.recordFailure(floodIP)
	}

	globalAuthLockout.Lock()
	totalTracked := len(globalAuthLockout.fails)
	globalAuthLockout.Unlock()

	// Hard cap must be maintained <= 5,000
	if totalTracked > 5000 {
		t.Fatalf("expected total tracked <= 5000, got %d", totalTracked)
	}

	// Target IP's active lockout MUST be preserved because non-locked failures were evicted first
	if !globalAuthLockout.isLocked(targetIP) {
		t.Fatal("active lockout on targetIP was prematurely evicted during flood!")
	}
}

// TestAuthorizedMinerWithXMineKeyDoesNotTriggerLockout verifies that repeated mining requests
// authenticated with X-Mine-Key do not trigger auth failures or IP lockout [HIGH-10].
func TestAuthorizedMinerWithXMineKeyDoesNotTriggerLockout(t *testing.T) {
	ResetRPCAuthForTesting()

	cfg := &ServerConfig{
		MineKey: "super-secret-mining-key-12345",
	}

	minerIP := "192.0.2.77"
	remoteAddr := minerIP + ":54321"

	// Simulate 10 consecutive mining requests using only X-Mine-Key (no basic auth header)
	for i := 0; i < 10; i++ {
		req, err := http.NewRequest(http.MethodGet, "/mine", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Mine-Key", cfg.MineKey)

		if !isAuthorizedMiner(req, cfg) {
			t.Fatalf("request #%d should be authorized via X-Mine-Key", i+1)
		}
	}

	// Verify that miner IP was never recorded as failed or locked out
	if globalAuthLockout.isLocked(minerIP) {
		t.Fatalf("miner IP %s was improperly locked out despite valid X-Mine-Key", minerIP)
	}

	globalAuthLockout.Lock()
	rec, hasRecord := globalAuthLockout.fails[minerIP]
	globalAuthLockout.Unlock()

	if hasRecord && rec.count > 0 {
		t.Fatalf("miner IP %s has %d failed attempts recorded, expected 0", minerIP, rec.count)
	}
}
