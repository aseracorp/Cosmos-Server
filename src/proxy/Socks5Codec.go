package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// Wire codec for the SOCKS5 relay, kept out of Socks5.go so the protocol bytes
// and the connection lifecycle can be read apart, and so the codec can be
// exercised without sockets.

// socksRequest is a decoded client request. Exactly one of host/ip is populated,
// depending on the address type the client used.
type socksRequest struct {
	command byte
	host    string
	ip      net.IP
	port    string
}

func (r socksRequest) String() string {
	switch {
	case r.host != "":
		return r.host + ":" + r.port
	case r.ip != nil:
		return r.ip.String() + ":" + r.port
	default:
		return "?"
	}
}

type socksAddr struct {
	ip   net.IP
	host string
	port string
}

func zeroAddr() socksAddr { return socksAddr{ip: net.IPv4zero, port: "0"} }

func socksAddrFrom(req socksRequest) socksAddr {
	if req.host != "" {
		return socksAddr{host: req.host, port: req.port}
	}
	ip := req.ip
	if ip == nil {
		ip = net.IPv4zero
	}
	return socksAddr{ip: ip, port: req.port}
}

func readByte(r io.Reader) (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

// readLengthPrefixed reads one RFC 1929 length-prefixed field (username or
// password). A zero-length field is rejected: an empty username is not a
// credential, and accepting it would make "no username" a valid login.
func readLengthPrefixed(r io.Reader) ([]byte, error) {
	n, err := readByte(r)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("empty length-prefixed field")
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// readGreeting parses the version byte and the offered method list.
func readGreeting(r io.Reader) ([]byte, error) {
	ver, err := readByte(r)
	if err != nil {
		return nil, err
	}
	if ver != socksVersion {
		return nil, fmt.Errorf("unsupported socks version %d (this relay speaks SOCKS5 only)", ver)
	}
	n, err := readByte(r)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("client offered no auth methods")
	}
	methods := make([]byte, n)
	if _, err := io.ReadFull(r, methods); err != nil {
		return nil, err
	}
	return methods, nil
}

// readRequest parses a CONNECT / UDP ASSOCIATE / BIND request.
//
// RFC 1928's request is VER, CMD, RESV(0x00), ATYP, ADDR, PORT. The version byte
// is read first and checked: a client that sends 0x05 (every real one does) is
// accepted, and a client that sends anything else is refused. Reading CMD first
// and tolerating any leading byte is how a server ends up accepting a SOCKS4
// request as if it were SOCKS5.
func readRequest(r io.Reader) (socksRequest, error) {
	var req socksRequest

	ver, err := readByte(r)
	if err != nil {
		return req, err
	}
	if ver != socksVersion {
		return req, fmt.Errorf("unsupported socks version %d in request", ver)
	}
	cmd, err := readByte(r)
	if err != nil {
		return req, err
	}
	reserved, err := readByte(r)
	if err != nil {
		return req, err
	}
	if reserved != 0x00 {
		return req, fmt.Errorf("invalid socks reserved byte %d", reserved)
	}
	atyp, err := readByte(r)
	if err != nil {
		return req, err
	}

	switch atyp {
	case socksATYPIPv4:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return req, err
		}
		req.ip = net.IP(ip)
	case socksATYPIPv6:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(r, ip); err != nil {
			return req, err
		}
		req.ip = net.IP(ip)
	case socksATYPDomain:
		n, err := readByte(r)
		if err != nil {
			return req, err
		}
		if n == 0 {
			return req, fmt.Errorf("empty domain in request")
		}
		host := make([]byte, n)
		if _, err := io.ReadFull(r, host); err != nil {
			return req, err
		}
		req.host = string(host)
	default:
		return req, fmt.Errorf("unsupported address type %d", atyp)
	}

	var pb [2]byte
	if _, err := io.ReadFull(r, pb[:]); err != nil {
		return req, err
	}
	req.command = cmd
	req.port = strconv.Itoa(int(binary.BigEndian.Uint16(pb[:])))
	return req, nil
}

func writeMethodReply(w io.Writer, method byte) error {
	_, err := w.Write([]byte{socksVersion, method})
	return err
}

func writeAuthReply(w io.Writer, status byte) error {
	_, err := w.Write([]byte{0x01, status})
	return err
}

// encodeAddr appends an address in the form the client used. A domain too long
// to encode is an error rather than a truncated write: a short reply makes a
// client hang waiting for bytes that never arrive.
func encodeAddr(buf []byte, addr socksAddr) ([]byte, error) {
	switch {
	case addr.host != "":
		if len(addr.host) > 255 {
			return nil, fmt.Errorf("domain too long for a socks address: %s", addr.host)
		}
		buf = append(buf, socksATYPDomain, byte(len(addr.host)))
		buf = append(buf, addr.host...)
	case addr.ip != nil && len(addr.ip) == 16 && addr.ip.To4() == nil:
		buf = append(buf, socksATYPIPv6)
		buf = append(buf, addr.ip...)
	default:
		ip := addr.ip
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		} else {
			ip = net.IPv4zero
		}
		buf = append(buf, socksATYPIPv4)
		buf = append(buf, ip...)
	}

	port, err := strconv.Atoi(addr.port)
	if err != nil || port < 0 || port > 65535 {
		port = 0
	}
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], uint16(port))
	return append(buf, pb[:]...), nil
}

// writeReply writes a SOCKS5 response. Reply length is the wire contract.
func writeReply(w io.Writer, rep byte, addr socksAddr) error {
	buf := make([]byte, 0, 22)
	buf = append(buf, socksVersion, rep, 0x00)

	encoded, err := encodeAddr(buf, addr)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

// socksRepForError maps a dial failure onto the RFC's reply codes. The
// distinction matters to clients: "host unreachable" is treated as retryable,
// "not allowed by ruleset" as permanent, so a wrong code makes a client hammer a
// relay that will never answer.
func socksRepForError(err error) byte {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not allowed"):
		return socksRepNotAllowed
	case strings.Contains(msg, "no addresses"), strings.Contains(msg, "no such host"):
		return socksRepHostUnreach
	case strings.Contains(msg, "resolve"):
		return socksRepHostUnreach
	case strings.Contains(msg, "refused"):
		return socksRepConnRefused
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return socksRepTTLExpired
	default:
		return socksRepGeneralError
	}
}

// socksFrame encodes the address header of a SOCKS UDP datagram payload. The
// RSVD/FRAG prefix is prepended by the caller.
func socksFrame(req socksRequest) []byte {
	out, err := encodeAddr(make([]byte, 0, 20), socksAddr{ip: req.ip, host: req.host, port: req.port})
	if err != nil {
		return nil
	}
	return out
}

// socksFrameLen is the byte length of an encoded address header. It must match
// what socksFrame produces, which is why both go through encodeAddr.
func socksFrameLen(req socksRequest) int { return len(socksFrame(req)) }

// socksFrameFromAddr encodes the address header for a datagram arriving from a
// peer, which is always numeric.
func socksFrameFromAddr(addr net.Addr) []byte {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return nil
	}
	out, err := encodeAddr(make([]byte, 0, 20), socksAddr{ip: udpAddr.IP, port: strconv.Itoa(udpAddr.Port)})
	if err != nil {
		return nil
	}
	return out
}

// socksUDPFrameHeader is the fixed prefix of a SOCKS UDP datagram: the RSVD byte
// and the FRAG byte. RFC 1928 puts ATYP at offset 2, so anything wider makes a
// compliant client read the destination out of the wrong byte.
const socksUDPFrameHeader = 2

// parseUDPFrame decodes a client datagram: it skips the RSVD/FRAG prefix and
// reads the destination the datagram is addressed to.
//
// A datagram with FRAG set is refused. Cosmos does not reassemble fragments: a
// client handed half a datagram treats it as whole, so forwarding a fragment
// silently corrupts a stream, while dropping it is a clean failure.
func parseUDPFrame(data []byte) (socksRequest, error) {
	var req socksRequest
	if len(data) < socksUDPFrameHeader+4 {
		return req, fmt.Errorf("short udp frame (%d bytes)", len(data))
	}
	req.command = socksCmdUDPAssociate

	if data[1] != 0x00 {
		return req, fmt.Errorf("fragmented udp datagram (FRAG=%d) is not relayed", data[1])
	}

	atyp := data[socksUDPFrameHeader]
	i := socksUDPFrameHeader + 1
	switch atyp {
	case socksATYPIPv4:
		if len(data) < i+4+2 {
			return req, fmt.Errorf("short udp frame (ipv4)")
		}
		req.ip = net.IP(data[i : i+4])
		i += 4
	case socksATYPIPv6:
		if len(data) < i+16+2 {
			return req, fmt.Errorf("short udp frame (ipv6)")
		}
		req.ip = net.IP(data[i : i+16])
		i += 16
	case socksATYPDomain:
		n := int(data[i])
		i++
		if n == 0 || len(data) < i+n+2 {
			return req, fmt.Errorf("short udp frame (domain)")
		}
		req.host = string(data[i : i+n])
		i += n
	default:
		return req, fmt.Errorf("unsupported udp address type %d", atyp)
	}
	req.port = strconv.Itoa(int(binary.BigEndian.Uint16(data[i : i+2])))
	return req, nil
}

// pump copies one direction and half-closes on EOF, so a relayed tunnel
// propagates a clean close instead of hanging until one side times out.
func pump(dst, src net.Conn) {
	if tcp, ok := src.(*net.TCPConn); ok {
		defer tcp.CloseWrite()
	}
	if tcp, ok := dst.(*net.TCPConn); ok {
		defer tcp.CloseWrite()
	}
	buf := make([]byte, 32*1024)
	io.CopyBuffer(dst, src, buf)
}
