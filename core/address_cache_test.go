package core

import (
	"testing"
)

func TestAddressToHexCached_BoundedCapacity(t *testing.T) {
	// Verify cache bounds and eviction behavior
	addrHexCache.Lock()
	addrHexCache.entries = make(map[string]string)
	addrHexCache.maxSize = 50
	addrHexCache.Unlock()

	defer func() {
		addrHexCache.Lock()
		addrHexCache.entries = make(map[string]string)
		addrHexCache.maxSize = 10000
		addrHexCache.Unlock()
	}()

	payload := make([]byte, AddressPayloadLen)
	// Insert 100 distinct valid addresses
	for i := 0; i < 100; i++ {
		payload[0] = byte(i)
		payload[1] = byte(i >> 8)
		addr, err := Bech32Encode(Bech32HRP, payload)
		if err != nil {
			t.Fatalf("failed to encode address: %v", err)
		}
		hexStr, err := AddressToHexCached(addr)
		if err != nil || hexStr == "" {
			t.Fatalf("failed to get cached hex: %v", err)
		}
	}

	addrHexCache.RLock()
	cachedCount := len(addrHexCache.entries)
	addrHexCache.RUnlock()

	if cachedCount > 50 {
		t.Fatalf("cache exceeded maximum bound of 50: got %d", cachedCount)
	}
	if cachedCount < 40 {
		t.Fatalf("cache evicted too aggressively: got %d", cachedCount)
	}
}

