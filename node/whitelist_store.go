package node

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WhitelistFile is stored next to the peer store and bans.json so policies are isolated per network.
const WhitelistFile = "whitelist.json"

// whitelistStorePath returns the path to whitelist.json, with optional ARMX_WHITELIST_PATH env override.
func (p *PeerServer) whitelistStorePath() string {
	if custom := os.Getenv("ARMX_WHITELIST_PATH"); custom != "" {
		return custom
	}
	p.Lock.RLock()
	defer p.Lock.RUnlock()
	if p.peerStorePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p.peerStorePath), WhitelistFile)
}

// loadWhitelist reads and parses whitelist.json and env var ARMX_WHITELIST_IPS.
// Any active bans or accumulated ban scores matching a whitelisted IP are automatically purged.
func (p *PeerServer) loadWhitelist() {
	path := p.whitelistStorePath()
	newWhitelist := make(map[string]struct{})
	var modTime time.Time

	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			modTime = fi.ModTime()
			data, readErr := os.ReadFile(path)
			if readErr == nil && len(data) > 0 {
				// Support JSON array: ["1.2.3.4", "5.6.7.8"]
				var arr []string
				if err := json.Unmarshal(data, &arr); err == nil {
					for _, raw := range arr {
						ip := normalizeIP(raw)
						if ip != "" {
							newWhitelist[ip] = struct{}{}
						}
					}
				} else {
					// Fallback support for JSON object: {"1.2.3.4": true}
					var obj map[string]interface{}
					if err := json.Unmarshal(data, &obj); err == nil {
						for raw, val := range obj {
							if b, ok := val.(bool); !ok || b {
								ip := normalizeIP(raw)
								if ip != "" {
									newWhitelist[ip] = struct{}{}
								}
							}
						}
					}
				}
			}
		}
	}

	// Also support comma-separated env var: ARMX_WHITELIST_IPS=1.2.3.4,5.6.7.8
	if envList := os.Getenv("ARMX_WHITELIST_IPS"); envList != "" {
		for _, raw := range strings.Split(envList, ",") {
			ip := normalizeIP(raw)
			if ip != "" {
				newWhitelist[ip] = struct{}{}
			}
		}
	}

	p.whitelistLock.Lock()
	p.whitelistIPs = newWhitelist
	p.whitelistModTime = modTime
	p.whitelistLock.Unlock()

	// Automatically purge any active bans or ban scores for whitelisted IPs
	if len(newWhitelist) > 0 {
		p.banLock.Lock()
		purgedAny := false
		for ip := range newWhitelist {
			if p.banScores != nil {
				delete(p.banScores, ip)
			}
			if p.bannedIPs != nil {
				if _, exists := p.bannedIPs[ip]; exists {
					delete(p.bannedIPs, ip)
					purgedAny = true
				}
			}
		}
		p.banLock.Unlock()

		if purgedAny {
			go p.persistBans()
		}
	}
}

// isWhitelisted checks whether the given IP is immune to ban scoring and rate limits.
// Periodically checks if whitelist.json on disk has been updated (hot reload).
func (p *PeerServer) isWhitelisted(ip string) bool {
	cleanIP := normalizeIP(ip)
	if cleanIP == "" {
		return false
	}

	// Hot-reload check if whitelist.json file modification time changed
	path := p.whitelistStorePath()
	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			p.whitelistLock.RLock()
			lastMod := p.whitelistModTime
			p.whitelistLock.RUnlock()
			if fi.ModTime().After(lastMod) {
				p.loadWhitelist()
			}
		}
	}

	p.whitelistLock.RLock()
	defer p.whitelistLock.RUnlock()
	if p.whitelistIPs == nil {
		return false
	}
	_, ok := p.whitelistIPs[cleanIP]
	return ok
}

// AddWhitelistIP dynamically registers an IP into the active whitelist and purges any bans.
func (p *PeerServer) AddWhitelistIP(ip string) {
	cleanIP := normalizeIP(ip)
	if cleanIP == "" {
		return
	}
	p.whitelistLock.Lock()
	if p.whitelistIPs == nil {
		p.whitelistIPs = make(map[string]struct{})
	}
	p.whitelistIPs[cleanIP] = struct{}{}
	p.whitelistLock.Unlock()

	p.banLock.Lock()
	if p.banScores != nil {
		delete(p.banScores, cleanIP)
	}
	purged := false
	if p.bannedIPs != nil {
		if _, exists := p.bannedIPs[cleanIP]; exists {
			delete(p.bannedIPs, cleanIP)
			purged = true
		}
	}
	p.banLock.Unlock()

	if purged {
		go p.persistBans()
	}
}

// normalizeIP extracts the host portion and trims whitespace from IP or host:port strings.
func normalizeIP(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		trimmed = host
	}
	parsed := net.ParseIP(trimmed)
	if parsed != nil {
		return parsed.String()
	}
	return trimmed
}
