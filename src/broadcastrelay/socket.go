package broadcastrelay

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// openInterface opens an AF_PACKET socket bound to the given interface (e.g.
// "br-1a2b3c4d5e6f"), capturing only IPv4 frames. Requires CAP_NET_RAW.
func openInterface(iface string) (*netIf, error) {
	// Resolve interface index.
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("broadcastrelay: cannot find interface %s: %w", iface, err)
	}

	// Create AF_PACKET raw socket capturing all L2 frames (ETH_P_ALL); we filter
	// down to IPv4 broadcast/multicast in isRelayableFrame so the socket is also
	// future-proof for other EtherTypes.
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		return nil, fmt.Errorf("broadcastrelay: cannot open raw socket on %s: %w (requires CAP_NET_RAW)", iface, err)
	}

	// Bind to the specific device.
	err = unix.Bind(fd, &unix.SockaddrLinklayer{
		Protocol: htons(unix.ETH_P_ALL),
		Ifindex:  ifi.Index,
		Pkttype:  unix.PACKET_HOST,
	})
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("broadcastrelay: cannot bind socket to %s: %w", iface, err)
	}

	// Also bind at the socket level to the device, so outgoing frames use the
	// right interface regardless of routing table (SO_BINDTODEVICE).
	_ = unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)

	hw := [6]byte{}
	if len(ifi.HardwareAddr) >= 6 {
		copy(hw[:], ifi.HardwareAddr[:6])
	}

	return &netIf{
		iface: iface,
		idx:   ifi.Index,
		hw:    hw,
		sock:  fd,
	}, nil
}

func closeSocket(fd int) {
	if fd > 0 {
		unix.Close(fd)
	}
}

// readPacket does a non-blocking read from the packet socket. Returns the number
// of bytes read and the source linklayer address.
func readPacket(fd int, buf []byte) (int, *unix.SockaddrLinklayer, error) {
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	return n, nil, nil
}

// sendPacket writes a frame out through the interface-bound socket.
func sendPacket(fd int, frame []byte) {
	_ = unix.Sendto(fd, frame, 0, &unix.SockaddrLinklayer{})
}

// isRelayableFrame checks whether an Ethernet frame should be relayed: it must
// be IPv4 and destined to a broadcast or multicast L2 address.
func isRelayableFrame(frame []byte) bool {
	if len(frame) < 14 {
		return false
	}
	// EtherType = IPv4?
	if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIP {
		return false
	}
	dst := frame[0:6]
	if isBroadcast(dst) {
		return true
	}
	if isMulticast(dst) {
		return true
	}
	return false
}

func isBroadcast(mac []byte) bool {
	if len(mac) < 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		if mac[i] != broadcastMAC[i] {
			return false
		}
	}
	return true
}

func isMulticast(mac []byte) bool {
	return len(mac) >= 1 && mac[0]&multicastMACBit == 1
}

// rewriteFrame produces the outgoing frame for dst, rewriting the source MAC to
// the outgoing interface's MAC, decrementing the IP TTL and recomputing IP and
// transport checksums. Returns nil if the frame cannot be rewritten (e.g. not
// UDP with a relaying policy, or too short).
func rewriteFrame(frame []byte, src, dst *netIf) []byte {
	if len(frame) < 14+20 { // eth + min IP header
		return nil
	}

	// Copy the frame.
	out := make([]byte, len(frame))
	copy(out, frame)

	// Rewrite source MAC to the outgoing interface's MAC.
	copy(out[6:12], dst.hw[:])

	ip := out[14:]
	ipVerIHL := ip[0]
	ihl := int(ipVerIHL&0x0f) * 4
	if ihl < 20 || len(ip) < ihl {
		return nil
	}

	proto := ip[9]

	// Decrement TTL; drop if it would expire.
	ttl := ip[8]
	if ttl <= 1 {
		return nil
	}
	ip[8] = ttl - 1

	// Source IP: keep as-is (the original sender). This is what makes the relay
	// "transparent" for discovery protocols. Recompute checksums below.
	ip[10], ip[11] = 0, 0 // clear header checksum
	sum := checksum(ip[:ihl])
	ip[10], ip[11] = byte(sum>>8), byte(sum&0xff)

	// Recompute transport checksum for TCP/UDP since TTL changed the pseudo-header
	// (and the header checksum change invalidates it).
	srcIP := net.IP(ip[12:16])
	dstIP := net.IP(ip[16:20])

	switch proto {
	case 17: // UDP
		if len(ip) < ihl+8 {
			return nil
		}
		dstPort := binary.BigEndian.Uint16(ip[ihl+2 : ihl+4])
		if !shouldRelayUDP(dstPort, dstIP) {
			return nil
		}
		// Clear checksum, compute UDP checksum incl. pseudo-header.
		udpLen := int(binary.BigEndian.Uint16(ip[ihl+4 : ihl+6]))
		if udpLen < 8 || ihl+udpLen > len(ip) {
			return nil
		}
		udp := ip[ihl : ihl+udpLen]
		udp[6], udp[7] = 0, 0
		// UDP checksum with pseudo-header.
		sum := pseudoChecksum(srcIP, dstIP, proto, udp)
		if sum == 0 {
			sum = 0xffff
		}
		udp[6], udp[7] = byte(sum>>8), byte(sum&0xff)
	case 6: // TCP
		tcpOff := ihl
		if len(ip) < tcpOff+20 {
			return nil
		}
		// Recompute over whole TCP segment with pseudo-header.
		tcp := ip[tcpOff:]
		tcp[16], tcp[17] = 0, 0
		sum := pseudoChecksum(srcIP, dstIP, proto, tcp)
		tcp[16], tcp[17] = byte(sum>>8), byte(sum&0xff)
	}

	return out
}

// checksum computes the standard Internet checksum over the given bytes.
func checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// pseudoChecksum computes a TCP/UDP checksum including the IPv4 pseudo-header.
func pseudoChecksum(src, dst net.IP, proto byte, segment []byte) uint16 {
	var sum uint32
	// Pseudo-header: src(4) + dst(4) + zero(1) + proto(1) + length(2)
	sum += uint32(binary.BigEndian.Uint16(src[0:2]))
	sum += uint32(binary.BigEndian.Uint16(src[2:4]))
	sum += uint32(binary.BigEndian.Uint16(dst[0:2]))
	sum += uint32(binary.BigEndian.Uint16(dst[2:4]))
	sum += uint32(proto)
	sum += uint32(len(segment))
	return checksum16(sum, segment)
}

func checksum16(sum uint32, data []byte) uint16 {
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | (v>>8)&0x00ff
}