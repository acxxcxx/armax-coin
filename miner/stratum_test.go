package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

func TestParseStratumPoolAddresses(t *testing.T) {
	if !isStratumURLList("stratum+tcp://127.0.0.1:3333") {
		t.Fatal("expected stratum+tcp:// to be detected as Stratum URL")
	}
	if isStratumURLList("http://127.0.0.1:19333") {
		t.Fatal("did not expect http:// daemon URL to be detected as Stratum URL")
	}

	pools := parseStratumPoolAddresses("stratum+tcp://localhost:3333, stratum://10.0.0.5:4444, pool.armax-coin.cloud")
	if len(pools) != 3 {
		t.Fatalf("expected 3 parsed pools, got %d (%v)", len(pools), pools)
	}
	if pools[0] != "127.0.0.1:3333" {
		t.Errorf("expected pools[0] = 127.0.0.1:3333, got %s", pools[0])
	}
	if pools[1] != "10.0.0.5:4444" {
		t.Errorf("expected pools[1] = 10.0.0.5:4444, got %s", pools[1])
	}
	if pools[2] != "pool.armax-coin.cloud:3333" {
		t.Errorf("expected pools[2] = pool.armax-coin.cloud:3333, got %s", pools[2])
	}
}

func TestSplitMinerWorker(t *testing.T) {
	addr, worker := splitMinerWorker("armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd.rig01")
	if addr != "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd" {
		t.Errorf("unexpected address: %s", addr)
	}
	if worker != "rig01" {
		t.Errorf("expected worker rig01, got %s", worker)
	}

	addrOnly, emptyWorker := splitMinerWorker("armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd")
	if addrOnly != "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd" || emptyWorker != "" {
		t.Errorf("unexpected split for bare address: addr=%s worker=%s", addrOnly, emptyWorker)
	}
}

func TestFloatDifficultyToTarget(t *testing.T) {
	t1 := floatDifficultyToTarget(1.0)
	if t1.Cmp(core.DifficultyToTarget(1)) != 0 {
		t.Fatalf("expected diff 1.0 target to match core.DifficultyToTarget(1)")
	}

	tHalf := floatDifficultyToTarget(0.5)
	if tHalf.Cmp(t1) <= 0 {
		t.Fatalf("expected diff 0.5 target to be greater than diff 1.0 target")
	}

	tDouble := floatDifficultyToTarget(2.0)
	if tDouble.Cmp(t1) >= 0 {
		t.Fatalf("expected diff 2.0 target to be less than diff 1.0 target")
	}
}

func TestParseStratumNotify(t *testing.T) {
	params := []interface{}{
		"job_2900_1",
		"0000000000000000000000000000000000000000000000000000000000000123",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd",
		"0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"671f8c00",
		true,
		float64(2900),
		"armaxhash-v2",
	}

	job, err := parseStratumNotify(params)
	if err != nil {
		t.Fatalf("parseStratumNotify failed: %v", err)
	}
	if job.JobID != "job_2900_1" || job.Height != 2900 || job.PoWAlgo != "armaxhash-v2" {
		t.Fatalf("unexpected parsed job: %+v", job)
	}
	if job.Timestamp != 0x671f8c00 {
		t.Fatalf("expected timestamp 0x671f8c00, got 0x%x", job.Timestamp)
	}
}

func TestStratumDryRunAndEndToEndMiningSession(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local port: %v", err)
	}
	defer listener.Close()

	poolAddr := "armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd"
	prevHash := "0000000000000000000000000000000000000000000000000000000000000001"
	merkleRoot := "1111111111111111111111111111111111111111111111111111111111111111"
	netTargetHex := "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	jobTimestamp := int64(1730000000)
	jobHeight := 2900
	jobAlgo := "armaxhash-v2"
	shareDiff := 0.25 // Easy difficulty for fast unit test execution

	shareVerified := make(chan string, 1)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					var req StratumMessage
					if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
						continue
					}

					switch req.Method {
					case "mining.subscribe":
						resp := map[string]interface{}{
							"id": req.ID,
							"result": []interface{}{
								[]interface{}{
									[]string{"mining.set_difficulty", "01020304"},
									[]string{"mining.notify", "01020304"},
								},
								"01020304",
								4,
							},
							"error": nil,
						}
						b, _ := json.Marshal(resp)
						_, _ = c.Write(append(b, '\n'))

						// Send set_difficulty and notify
						diffNotif := map[string]interface{}{
							"id":     nil,
							"method": "mining.set_difficulty",
							"params": []interface{}{shareDiff},
						}
						db, _ := json.Marshal(diffNotif)
						_, _ = c.Write(append(db, '\n'))

						jobNotif := map[string]interface{}{
							"id":     nil,
							"method": "mining.notify",
							"params": []interface{}{
								"job_test_1",
								prevHash,
								merkleRoot,
								poolAddr,
								netTargetHex,
								fmt.Sprintf("%x", jobTimestamp),
								true,
								jobHeight,
								jobAlgo,
							},
						}
						jb, _ := json.Marshal(jobNotif)
						_, _ = c.Write(append(jb, '\n'))

					case "mining.authorize":
						resp := map[string]interface{}{
							"id":     req.ID,
							"result": true,
							"error":  nil,
						}
						b, _ := json.Marshal(resp)
						_, _ = c.Write(append(b, '\n'))

					case "mining.submit":
						if len(req.Params) < 5 {
							continue
						}
						ntimeStr, _ := req.Params[3].(string)
						nonceStr, _ := req.Params[4].(string)
						ts, _ := strconv.ParseInt(ntimeStr, 16, 64)
						nonceInt, _ := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(nonceStr), "0x"), 16, 64)

						candidate := core.Block{
							Index:      jobHeight,
							Timestamp:  ts,
							PrevHash:   prevHash,
							MerkleRoot: merkleRoot,
							Miner:      poolAddr,
							Nonce:      nonceInt,
							Target:     netTargetHex,
						}
						hashHex, err := calculateStratumBlockHash(&candidate, jobAlgo)
						if err != nil || !core.HashMeetsTarget(hashHex, floatDifficultyToTarget(shareDiff)) {
							resp := map[string]interface{}{
								"id":     req.ID,
								"result": false,
								"error":  []interface{}{23, "Invalid share", nil},
							}
							b, _ := json.Marshal(resp)
							_, _ = c.Write(append(b, '\n'))
							continue
						}

						resp := map[string]interface{}{
							"id":     req.ID,
							"result": true,
							"error":  nil,
						}
						b, _ := json.Marshal(resp)
						_, _ = c.Write(append(b, '\n'))

						select {
						case shareVerified <- hashHex:
						default:
						}
					}
				}
			}(conn)
		}
	}()

	hostPort := listener.Addr().String()

	// 1. Test dry-run Stratum connection helper
	if err := testStratumPoolConnection(hostPort, poolAddr, "worker1", "x"); err != nil {
		t.Fatalf("testStratumPoolConnection failed: %v", err)
	}

	// 2. Test full mining session until 1 share is solved and accepted
	stopChan := make(chan os.Signal, 1)
	if err := runStratumMiningSession(stopChan, hostPort, poolAddr, "worker1", "x", 2, 60, 1); err != nil {
		t.Fatalf("runStratumMiningSession failed: %v", err)
	}

	select {
	case solvedHash := <-shareVerified:
		if len(solvedHash) != 64 {
			t.Fatalf("expected 64-char hex hash, got %s", solvedHash)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mock Stratum server to verify submitted share")
	}
}

