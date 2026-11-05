package node

import (
	"fmt"
	"net"
	"strings"
)

// SetSeedFingerprints installs operator-supplied certificate fingerprints for seed nodes so
// the very first connection to a seed is authenticated instead of blindly trusted (TOFU).
// Keys are "host:port" (IP or DNS name); a DNS name is resolved now and every resulting
// address receives the same pin, so all addresses behind that name must present one identity.
// A pinned peer that presents a different certificate is rejected, even if it was TOFU-pinned
// to something else before. Peers without an entry keep the normal TOFU behavior.
func (p *PeerServer) SetSeedFingerprints(pins map[string]string) error {
	resolved := make(map[string]string, len(pins))
	for addr, fp := range pins {
		fp = strings.ToLower(strings.TrimSpace(fp))
		if len(fp) != 64 || strings.Trim(fp, "0123456789abcdef") != "" {
			return fmt.Errorf("seed fingerprint for %q must be 64 hex characters (SHA-256 of the SPKI)", addr)
		}
		addr = strings.TrimSpace(addr)
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return fmt.Errorf("seed fingerprint key %q must be host:port", addr)
		}
		resolved[normalizePeerEndpoint(addr)] = fp
		for _, ra := range resolveDNSSeed(addr, false) {
			resolved[normalizePeerEndpoint(ra)] = fp
		}
	}
	p.seedPinMu.Lock()
	p.seedPins = resolved
	p.seedPinMu.Unlock()
	return nil
}

// checkSeedPin rejects a connection whose certificate differs from the operator-pinned one.
func (p *PeerServer) checkSeedPin(peerAddr, fp string) error {
	p.seedPinMu.RLock()
	want, ok := p.seedPins[normalizePeerEndpoint(peerAddr)]
	p.seedPinMu.RUnlock()
	if !ok || strings.EqualFold(want, fp) {
		return nil
	}
	return fmt.Errorf("P2P TOFU verification failed for peer %s: certificate does not match the pinned seed fingerprint (pinned %s, got %s)", peerAddr, want, fp)
}
