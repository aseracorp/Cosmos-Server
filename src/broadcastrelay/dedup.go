package broadcastrelay

import (
	"sync"
	"time"
)

// dedupCache is a small time-bucketed cache of recently-seen frame hashes used
// to prevent relay loops in a mesh of N networks. If the same frame (identified
// by hash of src MAC, dst MAC, and payload) is seen again within dedupWindow, it
// is dropped.
type dedupCache struct {
	mu    sync.Mutex
	seens map[uint64]time.Time
}

func newDedupCache() *dedupCache {
	return &dedupCache{
		seens: make(map[uint64]time.Time),
	}
}

// see returns true if the frame is NEW (should be forwarded), false if it is a
// repeat within the dedup window (should be dropped).
func (c *dedupCache) see(frame []byte) bool {
	h := hashFrame(frame)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if seen, ok := c.seens[h]; ok && now.Before(seen.Add(dedupWindow)) {
		return false
	}
	c.seens[h] = now

	// Opportunistic cleanup of very old entries to bound memory.
	if len(c.seens) > 4096 {
		for k, t := range c.seens {
			if now.After(t.Add(2*dedupWindow)) {
				delete(c.seens, k)
			}
		}
	}
	return true
}

// hashFrame computes a 64-bit FNV-1a hash identifying a datagram for loop
// prevention. It intentionally hashes only the IP-level identity — source IP,
// destination IP, protocol, and transport ports + payload — and NOT the L2 MAC
// addresses, because the relay rewrites the source MAC at every hop. Hashing
// the IP identity means the same logical datagram hashes identically no matter
// how many networks it has been relayed through, so the dedup cache reliably
// breaks relay loops in a mesh of N networks.
func hashFrame(frame []byte) uint64 {
	const prime = 0x100000001b3
	hash := uint64(0xcbf29ce484222325)

	// Need at least eth header + full IP header to identify a datagram.
	if len(frame) < 14+20 {
		return hashByte(hash, prime, frame)
	}

	ip := frame[14:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl {
		return hashByte(hash, prime, frame)
	}

	// Hash source IP + destination IP (invariant across hops).
	hash = hashByte(hash, prime, ip[12:16])
	hash = hashByte(hash, prime, ip[16:20])
	// Hash protocol.
	hash = hashByte(hash, prime, ip[9:10])

	// Hash transport ports (invariant across hops) when present.
	if ihl+4 <= len(ip) {
		hash = hashByte(hash, prime, ip[ihl:ihl+4])
	}

	// Hash the payload (bounded to keep it cheap and stable).
	start := 14 + ihl + 8 // eth + IP + transport header
	if start > len(frame) {
		start = 14 + ihl
	}
	limit := len(frame)
	if limit > start+1440 {
		limit = start + 1440
	}
	hash = hashByte(hash, prime, frame[start:limit])

	return hash
}

func hashByte(h uint64, prime uint64, data []byte) uint64 {
	for _, b := range data {
		h = h ^ uint64(b)
		h = h * prime
	}
	return h
}