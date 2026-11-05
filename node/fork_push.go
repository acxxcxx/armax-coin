package node

import (
	"encoding/json"
	"flag"
	"strings"
	"sync"
	"time"

	"github.com/acxxcxx/armax-coin/core"
)

const (
	// MsgPushBlocks carries a contiguous run of blocks pushed by a peer that holds a heavier chain
	// than the receiver. It lets a node without an inbound-reachable port propagate its chain over
	// the connection it opened itself. Older nodes ignore unknown message types.
	MsgPushBlocks = "PUSH_BLOCKS"

	// MaxPushBlocks bounds the blocks sent to one peer per push round.
	MaxPushBlocks = 1000

	pushCooldown = 30 * time.Second
)

// headerPoWHash recomputes the block hash and checks the claimed target is within the network limit
// and met by the hash. It does not validate the difficulty schedule or the block body.
func headerPoWHash(b core.Block) (string, bool) {
	calcHash, err := b.CalculateHashWithError()
	if err != nil || b.Hash == "" || b.Hash != calcHash {
		return "", false
	}

	// Maximum allowable target ceiling (minimum network difficulty) [T3]
	maxAllowedTarget := core.GetPowLimit()
	if core.IsArmaxHashActive(b.Index) {
		maxAllowedTarget = core.GetArmaxHashPowLimit()
	}
	if flag.Lookup("test.v") != nil && !core.GetActiveNetwork().IsMainnet {
		if maxAllowedTarget.Cmp(core.DifficultyToTarget(1)) < 0 {
			maxAllowedTarget = core.DifficultyToTarget(1)
		}
	}

	target := b.GetTarget()
	if target == nil || target.Sign() <= 0 || target.Cmp(maxAllowedTarget) > 0 || !core.HashMeetsTarget(calcHash, target) {
		return "", false
	}
	return calcHash, true
}

type pushBufferSession struct {
	ancestor int
	blocks   []core.Block
	lastSeen time.Time
}

var (
	pushSessionsMu sync.Mutex
	pushSessions   = make(map[string]*pushBufferSession)
)

func cleanupExpiredPushSessionsLocked() {
	now := time.Now()
	for addr, sess := range pushSessions {
		if now.Sub(sess.lastSeen) > 30*time.Second {
			delete(pushSessions, addr)
		}
	}
}

// handlePushedBlocks applies blocks pushed by a peer. A run that extends the local tip is appended;
// a run that branches off the active chain is adopted only through ResolveFork, which enforces the
// strictly-greater chainwork rule, reorg depth limit and full block validation. Multi-chunk reorgs
// are staged in a peer buffer until cumulative chainwork is sufficient to reorganize [HIGH-07].
func (p *PeerServer) handlePushedBlocks(blocks []core.Block, remoteIP, remoteAddr string) {
	if p.Blockchain == nil || len(blocks) == 0 {
		return
	}
	if len(blocks) > SyncChunkSize {
		p.AddBanScore(remoteIP, 20)
		return
	}
	for i := range blocks {
		if i > 0 && (blocks[i].Index != blocks[i-1].Index+1 || blocks[i].PrevHash != blocks[i-1].Hash) {
			p.AddBanScore(remoteIP, 20)
			return
		}
		if _, ok := headerPoWHash(blocks[i]); !ok {
			p.AddBanScore(remoteIP, 20)
			return
		}
	}

	p.pushMu.Lock()
	defer p.pushMu.Unlock()

	pushSessionsMu.Lock()
	cleanupExpiredPushSessionsLocked()
	session, hasSession := pushSessions[remoteAddr]
	pushSessionsMu.Unlock()

	p.Blockchain.Lock.RLock()
	chain := p.Blockchain.Chain
	currentLen := len(chain)
	localTipWork := core.CalculateChainwork(chain)
	p.Blockchain.Lock.RUnlock()

	var candidateBranch []core.Block
	var effectiveAncestor int

	if hasSession && len(session.blocks) > 0 {
		lastSessionBlock := session.blocks[len(session.blocks)-1]
		if blocks[0].Index == lastSessionBlock.Index+1 && blocks[0].PrevHash == lastSessionBlock.Hash {
			// Contiguous continuation of an active session
			session.blocks = append(session.blocks, blocks...)
			session.lastSeen = time.Now()
			if len(session.blocks) > MaxPushBlocks {
				pushSessionsMu.Lock()
				delete(pushSessions, remoteAddr)
				pushSessionsMu.Unlock()
				p.AddBanScore(remoteIP, 20)
				return
			}
			candidateBranch = session.blocks
			effectiveAncestor = session.ancestor
		}
	}

	if candidateBranch == nil {
		// New or standalone push: check if it connects to local chain
		p.Blockchain.Lock.RLock()
		chain = p.Blockchain.Chain
		currentLen = len(chain)
		localTipWork = core.CalculateChainwork(chain)
		for len(blocks) > 0 && blocks[0].Index < currentLen && chain[blocks[0].Index].Hash == blocks[0].Hash {
			blocks = blocks[1:]
		}
		p.Blockchain.Lock.RUnlock()

		if len(blocks) == 0 {
			return
		}
		ancestor := blocks[0].Index - 1
		if ancestor < 0 || ancestor >= currentLen || chain[ancestor].Hash != blocks[0].PrevHash {
			return
		}
		effectiveAncestor = ancestor
		candidateBranch = blocks
	}

	// 1. If candidate branch directly extends the current active chain tip
	if effectiveAncestor == currentLen-1 {
		pushSessionsMu.Lock()
		delete(pushSessions, remoteAddr)
		pushSessionsMu.Unlock()

		for _, blk := range blocks {
			if !p.Blockchain.ValidateAndAddBlock(blk) {
				p.AddBanScore(remoteIP, 20)
				p.markBlockNegative(blk.Hash)
				return
			}
			p.markBlockSeen(blk.Hash)
		}
		tip := blocks[len(blocks)-1]
		core.LogInfo(core.LogCategorySync, "Applied %d pushed blocks from %s, new height #%d", len(blocks), remoteAddr, tip.Index)
		go p.RelayBlock(tip, remoteAddr)
		p.processOrphans(tip.Hash)
		p.processOrphanTxs()
		return
	}

	// 2. Candidate branch is a reorganization
	fullSimulated := append([]core.Block(nil), chain[:effectiveAncestor+1]...)
	fullSimulated = append(fullSimulated, candidateBranch...)
	candidateWork := core.CalculateChainwork(fullSimulated)

	if candidateWork.Cmp(localTipWork) <= 0 {
		// Heavier chain not yet reached; stage in session buffer awaiting subsequent chunks [HIGH-07]
		pushSessionsMu.Lock()
		pushSessions[remoteAddr] = &pushBufferSession{
			ancestor: effectiveAncestor,
			blocks:   candidateBranch,
			lastSeen: time.Now(),
		}
		pushSessionsMu.Unlock()
		debugLogf("[P2P] PUSH_BLOCKS from %s staged (%d blocks, height #%d..#%d), awaiting remaining chunks",
			remoteAddr, len(candidateBranch), candidateBranch[0].Index, candidateBranch[len(candidateBranch)-1].Index)
		return
	}

	// Candidate branch has greater cumulative chainwork! Execute reorganization.
	pushSessionsMu.Lock()
	delete(pushSessions, remoteAddr)
	pushSessionsMu.Unlock()

	if err := p.Blockchain.ResolveFork(effectiveAncestor, candidateBranch); err != nil {
		if !strings.Contains(err.Error(), "chainwork") {
			p.AddBanScore(remoteIP, 20)
		}
		debugLogf("[P2P] PUSH_BLOCKS from %s not applied: %v", remoteAddr, err)
		return
	}

	tip := candidateBranch[len(candidateBranch)-1]
	for _, blk := range candidateBranch {
		p.markBlockSeen(blk.Hash)
	}
	core.LogInfo(core.LogCategorySync, "Applied %d pushed blocks from %s across reorg, new height #%d", len(candidateBranch), remoteAddr, tip.Index)
	go p.RelayBlock(tip, remoteAddr)
	p.processOrphans(tip.Hash)
	p.processOrphanTxs()
}

// pushCooldownElapsed reports whether a push to peerAddr is allowed and records the attempt.
func (p *PeerServer) pushCooldownElapsed(peerAddr string) bool {
	p.pushCooldownMu.Lock()
	defer p.pushCooldownMu.Unlock()
	if p.pushCooldowns == nil {
		p.pushCooldowns = make(map[string]time.Time)
	}
	now := time.Now()
	if last, ok := p.pushCooldowns[peerAddr]; ok && now.Sub(last) < pushCooldown {
		return false
	}
	p.pushCooldowns[peerAddr] = now
	return true
}

// shouldPushTo decides whether this node holds a chain a peer is missing. A peer one block behind is
// ordinary propagation lag and is left alone.
func shouldPushTo(localHeight int, peerHeight int, localWorkGreater bool) bool {
	if !localWorkGreater {
		return false
	}
	return peerHeight >= localHeight || localHeight-peerHeight >= 2
}

// pushChainToPeer finds the common ancestor with a peer and sends our branch as PUSH_BLOCKS over a
// connection we dial ourselves, so it works when the peer cannot dial us back.
func (p *PeerServer) pushChainToPeer(peerAddress string, peerHeight int) {
	if p.Blockchain == nil || p.isShuttingDown() || !p.pushCooldownElapsed(peerAddress) {
		return
	}

	forkResp, err := p.QueryPeer(peerAddress, Message{
		NetworkID: p.GetNetworkID(),
		Type:      "GET_FORKPOINT",
		Payload:   ForkpointRequest{Locators: p.GenerateBlockLocators()},
	}, 5*time.Second)
	if err != nil || forkResp == nil || forkResp.Type != "FORKPOINT_RESPONSE" {
		return
	}
	fb, err := forkResp.PayloadBytes()
	if err != nil {
		return
	}
	var fp ForkpointResponse
	if json.Unmarshal(fb, &fp) != nil || fp.AncestorHeight < 0 {
		return
	}

	p.Blockchain.Lock.RLock()
	tip := p.Blockchain.GetLatestBlockUnsafe().Index
	if fp.AncestorHeight >= tip || tip-fp.AncestorHeight > core.MaxReorganizationDepth {
		p.Blockchain.Lock.RUnlock()
		return
	}
	end := tip
	if end-fp.AncestorHeight > MaxPushBlocks {
		end = fp.AncestorHeight + MaxPushBlocks
	}
	pending := make([]core.Block, end-fp.AncestorHeight)
	copy(pending, p.Blockchain.Chain[fp.AncestorHeight+1:end+1])
	p.Blockchain.Lock.RUnlock()

	// A branch that replaces blocks on the peer can be pushed in multiple chunks up to MaxPushBlocks [HIGH-07].
	if peerHeight > fp.AncestorHeight && len(pending) > MaxPushBlocks {
		core.LogWarn(core.LogCategorySync, "Peer %s forks %d blocks below our tip; exceeds MaxPushBlocks (%d)", peerAddress, len(pending), MaxPushBlocks)
		return
	}

	conn, release, err := p.DialPeer(peerAddress, 15*time.Second)
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close()
		release()
	}()

	core.LogInfo(core.LogCategorySync, "Pushing %d blocks (#%d..#%d) to peer %s", len(pending), pending[0].Index, end, peerAddress)
	for len(pending) > 0 {
		n, size := 0, 0
		for n < len(pending) && n < SyncChunkSize {
			bs := pending[n].Size()
			if n > 0 && size+bs > MaxBlockRangeResponseBytes {
				break
			}
			size += bs
			n++
		}
		data, err := json.Marshal(Message{
			NetworkID: p.GetNetworkID(),
			Type:      MsgPushBlocks,
			Payload:   BlockRangeResponse{FromIndex: pending[0].Index, ToIndex: pending[n-1].Index, Blocks: pending[:n]},
		})
		if err != nil {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err := conn.Write(append(data, '\n')); err != nil {
			return
		}
		pending = pending[n:]
		time.Sleep(50 * time.Millisecond)
	}
}
