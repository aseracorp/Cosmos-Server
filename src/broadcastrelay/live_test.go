package broadcastrelay

import (
	"net"
	"os"
	"testing"
)

// TestOpenInterfaceLive validates the low-level socket path against a real
// interface when CAP_NET_RAW is available. It is skipped if the environment
// cannot open an AF_PACKET socket (e.g. no privilege).
func TestOpenInterfaceLive(t *testing.T) {
	iface := os.Getenv("RELAY_TEST_IFACE")
	if iface == "" {
		iface = "eth0"
	}

	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		t.Skipf("interface %s not present: %v", iface, err)
	}

	ni, err := openInterface(iface)
	if err != nil {
		// If we lack CAP_NET_RAW, skip rather than fail — this is an
		// environment limitation, not a code bug.
		t.Skipf("cannot open AF_PACKET socket on %s: %v (no CAP_NET_RAW?)", iface, err)
	}
	defer closeSocket(ni.sock)

	if ni.idx != ifi.Index {
		t.Fatalf("index mismatch: got %d want %d", ni.idx, ifi.Index)
	}
	t.Logf("opened AF_PACKET socket on %s (idx %d, MAC %x)", ni.iface, ni.idx, ni.hw)

	// Verify SO_BINDTODEVICE didn't error and the socket is functional by
	// doing a non-blocking read (should return EAGAIN, not a hard error).
	n, _, err := readPacket(ni.sock, make([]byte, 65536))
	if err != nil {
		t.Fatalf("readPacket returned unexpected error: %v", err)
	}
	if n != 0 {
		t.Logf("read %d bytes (traffic present)", n)
	}
}

// TestRewriteFrameLive validates that rewriteFrame produces a well-formed frame
// with correct checksums using the live interface's MAC.
func TestRewriteFrameLive(t *testing.T) {
	iface := os.Getenv("RELAY_TEST_IFACE")
	if iface == "" {
		iface = "eth0"
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		t.Skipf("interface %s not present: %v", iface, err)
	}
	hw := [6]byte{}
	if len(ifi.HardwareAddr) >= 6 {
		copy(hw[:], ifi.HardwareAddr[:6])
	}

	src := &netIf{iface: "src", hw: [6]byte{0x02, 0, 0, 0, 0, 1}}
	dst := &netIf{iface: "dst", hw: hw}

	// Build a UDP broadcast frame: mDNS query to 224.0.0.251:5353.
	frame := buildMDNSFrame()

	out := rewriteFrame(frame, src, dst)
	if out == nil {
		t.Fatal("rewriteFrame returned nil for a valid mDNS broadcast")
	}
	if len(out) != len(frame) {
		t.Fatalf("length changed: got %d want %d", len(out), len(frame))
	}
	// Source MAC must be rewritten to dst's MAC.
	for i := 0; i < 6; i++ {
		if out[6+i] != hw[i] {
			t.Fatalf("source MAC not rewritten: %x", out[6:12])
		}
	}
	// TTL must be decremented from 64 to 63.
	if out[14+8] != 63 {
		t.Fatalf("TTL not decremented: got %d", out[14+8])
	}
	// Verify IP header checksum is valid.
	ip := out[14:]
	if checksum(ip[:20]) != 0 {
		t.Fatalf("IP header checksum invalid: %x", checksum(ip[:20]))
	}
	t.Logf("rewritten frame OK (dst MAC %x, TTL %d)", out[6:12], out[14+8])
}

// buildMDNSFrame constructs an IPv4 UDP multicast frame destined to mDNS.
func buildMDNSFrame() []byte {
	// Ethernet header: dst 01:00:5e:00:00:fb (224.0.0.251 mcast), src arbitrary,
	// ethertype IPv4.
	frame := make([]byte, 14+20+8+24)
	dst := []byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0xfb}
	src := []byte{0x02, 0, 0, 0, 0, 0x01}
	copy(frame[0:6], dst)
	copy(frame[6:12], src)
	frame[12], frame[13] = 0x08, 0x00

	ip := frame[14:34]
	ip[0] = 0x45 // IPv4, IHL 5
	// total length = 20+8+24 = 52
	ip[2], ip[3] = 0x00, 0x34
	// id
	ip[4], ip[5] = 0x12, 0x34
	// flags/frag
	ip[6], ip[7] = 0x40, 0x00
	ip[8] = 64 // TTL
	ip[9] = 17 // UDP
	// src 192.168.1.50
	copy(ip[12:16], []byte{192, 168, 1, 50})
	// dst 224.0.0.251
	copy(ip[16:20], []byte{224, 0, 0, 251})
	// IP checksum
	ip[10], ip[11] = 0, 0
	s := checksum(ip[:20])
	ip[10], ip[11] = byte(s>>8), byte(s&0xff)

	udp := frame[34:42]
	// src port 5353
	udp[0], udp[1] = 0x14, 0xe9
	// dst port 5353
	udp[2], udp[3] = 0x14, 0xe9
	// len = 8+24 = 32
	udp[4], udp[5] = 0x00, 0x20
	// payload: DNS-ish
	copy(frame[42:], []byte("mDNS query test payload"))

	return frame
}
