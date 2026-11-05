package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// BansFile is stored next to the peer store so bans are isolated per network and data directory.
const BansFile = "bans.json"

// banStorePath returns the ban file path, or "" when no peer store directory is known.
func (p *PeerServer) banStorePath() string {
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	if p.peerStorePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p.peerStorePath), BansFile)
}

// banIPLocked records a ban (caller holds banLock) and persists only when the IP was not already
// banned, so a flood from one banned peer does not cause repeated disk writes.
func (p *PeerServer) banIPLocked(ip string, expiry time.Time) {
	if p.bannedIPs == nil {
		p.bannedIPs = make(map[string]time.Time)
	}
	wasBanned := time.Now().Before(p.bannedIPs[ip])
	p.bannedIPs[ip] = expiry
	if !wasBanned {
		go p.persistBans()
	}
}

// persistBans writes active bans (ip -> unix expiry) atomically. It never creates directories,
// so a missing data directory simply skips persistence.
func (p *PeerServer) persistBans() {
	path := p.banStorePath()
	if path == "" {
		return
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return
	}

	now := time.Now()
	p.banLock.RLock()
	snapshot := make(map[string]int64, len(p.bannedIPs))
	for ip, exp := range p.bannedIPs {
		if exp.After(now) {
			snapshot[ip] = exp.Unix()
		}
	}
	p.banLock.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}

	p.banFileMu.Lock()
	defer p.banFileMu.Unlock()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// loadBans restores unexpired bans from disk. A missing or corrupt file is ignored.
func (p *PeerServer) loadBans() {
	path := p.banStorePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored map[string]int64
	if json.Unmarshal(data, &stored) != nil {
		return
	}

	now := time.Now()
	p.banLock.Lock()
	defer p.banLock.Unlock()
	if p.bannedIPs == nil {
		p.bannedIPs = make(map[string]time.Time)
	}
	for ip, unix := range stored {
		exp := time.Unix(unix, 0)
		if exp.After(now) && exp.After(p.bannedIPs[ip]) {
			p.bannedIPs[ip] = exp
		}
	}
}
