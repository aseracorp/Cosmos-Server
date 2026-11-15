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

// socksRequest is a decoded client request. ip is the destination: a socks5
// request carries an IPv4/IPv6 address, and a request carrying a domain is
// refused in readRequest, so there is no name to carry.
type socksRequest struct {
	command byte
	ip      net.IP
	port    string
}

func (r socksRequest) String() string {
	if r.ip == nil {
		return "?"
	}
	return r.ip.String() + ":" + r.port
}

// socksAddr is the bound-address field of a reply. It echoes the address the
// client asked for, which is always numeric here.
type socksAddr struct {
	ip   net.IP
	port string
}

func socksAddrFrom(req socksRequest) socksAddr {
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
		// RFC 1928 allows a client to send a domain and asks the server to
		// resolve it. That is the remote-resolution mode (socks5h), and this
		// relay does not do it: Cosmos is a reverse proxy, and resolving a
		// client's outbound names is a forward-proxy feature. Refused with the
		// address-type code, so a client that sends ATYP=0x03 gets a clean
		// answer and resolves the name itself.
		return req, fmt.Errorf("domain destinations are not supported: this relay dials addresses, a socks5 client must resolve the name itself")
	default:
		return req, fmt.Errorf("address type %d is not supported", atyp)
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
	case strings.Contains(msg, "not supported"):
		// A domain destination: the client must resolve it and retry with an
		// address. This is permanent for this relay, so it is not the retryable
		// "host unreachable" code.
		return socksRepATYPNotSupport
	case strings.Contains(msg, "refused"):
		return socksRepConnRefused
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return socksRepTTLExpired
	default:
		return socksRepGeneralError
	}
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
