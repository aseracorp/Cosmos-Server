package broadcastrelay

import (
	"net"
	"testing"
)

func TestIsRelayableFrame(t *testing.T) {
	// Broadcast IPv4 frame.
	f := make([]byte, 64)
	for i := 0; i < 6; i++ {
		f[i] = 0xff
	}
	f[12], f[13] = 0x08, 0x00 // ETH_P_IP
	if !isRelayableFrame(f) {
		t.Fatal("broadcast frame should be relayable")
	}

	// Unicast IPv4 frame (dst MAC LSB clear, not all ff).
	g := make([]byte, 64)
	g[0] = 0x00
	g[12], g[13] = 0x08, 0x00
	if isRelayableFrame(g) {
		t.Fatal("unicast frame should not be relayable")
	}

	// Multicast IPv4 frame.
	h := make([]byte, 64)
	h[0] = 0x01
	h[1] = 0x00
	h[12], h[13] = 0x08, 0x00
	if !isRelayableFrame(h) {
		t.Fatal("multicast frame should be relayable")
	}

	// Non-IP frame.
	k := make([]byte, 64)
	k[0] = 0xff
	k[12], k[13] = 0x86, 0xdd // IPv6
	if isRelayableFrame(k) {
		t.Fatal("IPv6 frame should not be relayed (IPv4 only)")
	}
}

func TestShouldRelayUDP(t *testing.T) {
	mdns := net.ParseIP("224.0.0.251")
	if !shouldRelayUDP(5353, mdns) {
		t.Fatal("mDNS should be relayed")
	}
	if shouldRelayUDP(67, mdns) {
		t.Fatal("DHCP server should not be relayed")
	}
	if shouldRelayUDP(68, mdns) {
		t.Fatal("DHCP client should not be relayed")
	}
	// Policy: all other broadcast/multicast UDP is relayed (arbitrary ports).
	if !shouldRelayUDP(1900, net.ParseIP("239.255.255.250")) {
		t.Fatal("SSDP should be relayed")
	}
	if !shouldRelayUDP(32123, net.ParseIP("224.0.0.9")) {
		t.Fatal("game multicast on arbitrary port should be relayed")
	}
	if !shouldRelayUDP(9999, net.ParseIP("255.255.255.255")) {
		t.Fatal("broadcast on arbitrary port should be relayed")
	}
}

func TestChecksum(t *testing.T) {
	// Internet checksum of zero bytes should be 0xffff (all ones).
	data := []byte{0x00, 0x00}
	if checksum(data) != 0xffff {
		t.Fatalf("expected 0xffff, got %x", checksum(data))
	}
}

func TestDedup(t *testing.T) {
	c := newDedupCache()
	f := make([]byte, 64)
	f[0] = 0xff
	f[12], f[13] = 0x08, 0x00
	if !c.see(f) {
		t.Fatal("first see should be true")
	}
	if c.see(f) {
		t.Fatal("second see within window should be false (dedup)")
	}
}

// TestDedupAcrossMACRewrite validates that the same datagram, seen after its
// source MAC has been rewritten by a relay hop, is still recognized as a
// duplicate — this is what breaks relay loops in a mesh of networks.
func TestDedupAcrossMACRewrite(t *testing.T) {
	c := newDedupCache()

	// Original frame from a real device on net A.
	orig := buildMDNSFrame()
	orig[6] = 0xde // device MAC
	orig[7] = 0xad

	if !c.see(orig) {
		t.Fatal("first occurrence should be seen as new")
	}

	// The same datagram after relay A->B rewrote the source MAC.
	rewritten := make([]byte, len(orig))
	copy(rewritten, orig)
	rewritten[6] = 0xbb // bridge B MAC
	rewritten[7] = 0xbb

	if c.see(rewritten) {
		t.Fatal("same datagram with rewritten src MAC should be deduped (loop prevention)")
	}
}
