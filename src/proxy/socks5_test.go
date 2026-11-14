package proxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// Codec tests exercise the wire bytes directly: a SOCKS client parses fixed
// offsets, so a reply that is one byte short hangs the client rather than
// failing it. These are the tests that catch that class of bug.

func TestReadGreetingRejectsSocks4(t *testing.T) {
	_, err := readGreeting(bytes.NewReader([]byte{0x04, 0x01, 0x00}))
	if err == nil || !strings.Contains(err.Error(), "SOCKS5") {
		t.Fatalf("expected a socks5-only refusal, got %v", err)
	}
}

func TestReadGreetingRejectsEmptyMethodList(t *testing.T) {
	// A client offering no methods cannot be served, and reading a zero-length
	// method list as "no methods" is how a server ends up choosing none.
	if _, err := readGreeting(bytes.NewReader([]byte{0x05, 0x00})); err == nil {
		t.Fatal("expected an error for an empty method list")
	}
}

func TestReadRequestAddressTypes(t *testing.T) {
	tests := []struct {
		name     string
		in       []byte
		wantHost string
		wantIP   string
		wantPort string
	}{
		{"ipv4", []byte{socksVersion, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0x31, 0xD1}, "", "127.0.0.1", "12753"},
		{"ipv6", append(append([]byte{socksVersion, 0x01, 0x00, 0x04}, net.ParseIP("::1").To16()...), 0x00, 0x50), "", "::1", "80"},
		{"domain", []byte{socksVersion, 0x01, 0x00, 0x03, 6, 'e', 'x', '.', 'c', 'o', 'm', 0x01, 0xBB}, "ex.com", "", "443"},
	}
	for _, tt := range tests {
		req, err := readRequest(bytes.NewReader(tt.in))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if req.host != tt.wantHost {
			t.Errorf("%s: host = %q, want %q", tt.name, req.host, tt.wantHost)
		}
		if tt.wantIP != "" && req.ip.String() != tt.wantIP {
			t.Errorf("%s: ip = %s, want %s", tt.name, req.ip, tt.wantIP)
		}
		if req.port != tt.wantPort {
			t.Errorf("%s: port = %s, want %s", tt.name, req.port, tt.wantPort)
		}
	}
}

func TestReadRequestRejectsUnknownAddressType(t *testing.T) {
	if _, err := readRequest(bytes.NewReader([]byte{socksVersion, 0x01, 0x00, 0x09, 1, 2, 3, 4, 0, 80})); err == nil {
		t.Fatal("expected an error for address type 9")
	}
}

func TestWriteReplyLengths(t *testing.T) {
	// Reply framing is fixed-offset parsing on the client side: v4 is 10 bytes,
	// v6 is 22, domain is 7+len(name).
	tests := []struct {
		name string
		addr socksAddr
		want int
	}{
		{"ipv4", socksAddr{ip: net.IPv4(10, 0, 0, 1), port: "8080"}, 10},
		{"ipv6", socksAddr{ip: net.ParseIP("2001:db8::1"), port: "80"}, 22},
		{"domain", socksAddr{host: "example.com", port: "443"}, 7 + 11},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		if err := writeReply(&buf, socksRepSuccess, tt.addr); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if buf.Len() != tt.want {
			t.Errorf("%s: reply is %d bytes, want %d", tt.name, buf.Len(), tt.want)
		}
		if buf.Bytes()[0] != socksVersion || buf.Bytes()[1] != socksRepSuccess {
			t.Errorf("%s: bad version/rep header %x", tt.name, buf.Bytes()[:2])
		}
	}
}

func TestWriteReplyRejectsUnencodableDomain(t *testing.T) {
	// A truncated reply makes a client wait forever, so an over-long domain must
	// surface as an error.
	long := strings.Repeat("a", 256)
	var buf bytes.Buffer
	if err := writeReply(&buf, socksRepSuccess, socksAddr{host: long, port: "80"}); err == nil {
		t.Fatal("expected an error for a domain longer than 255 bytes")
	}
}

func TestParseUDPFrameDestinations(t *testing.T) {
	// RSVD, FRAG, RSVD, then the address header.
	v4 := []byte{0x00, 0x00, 0x01, 8, 8, 8, 8, 0x00, 0x35, 'h', 'i'}
	req, err := parseUDPFrame(v4)
	if err != nil {
		t.Fatal(err)
	}
	if req.ip.String() != "8.8.8.8" || req.port != "53" {
		t.Errorf("got %s", req)
	}

	domain := append([]byte{0x00, 0x00, 0x03, 8, 'd', 'n', 's', '.', 'o', 'p', 'e', 'n'}, 0x00, 0x35)
	req, err = parseUDPFrame(domain)
	if err != nil {
		t.Fatal(err)
	}
	if req.host != "dns.open" || req.port != "53" {
		t.Errorf("got %s", req)
	}
}

func TestParseUDPFrameRejectsFragments(t *testing.T) {
	// FRAG != 0 means a fragmented datagram. Relay one and a client treats a
	// fragment as a whole datagram: silent corruption. Dropping is a clean fail.
	frag := []byte{0x00, 0x01, 0x01, 8, 8, 8, 8, 0x00, 0x35, 'p'}
	if _, err := parseUDPFrame(frag); err == nil || !strings.Contains(err.Error(), "fragment") {
		t.Fatalf("expected a fragment refusal, got %v", err)
	}
}

func TestParseUDPFrameRejectsTruncated(t *testing.T) {
	for _, in := range [][]byte{{0x00, 0x00}, {0x00, 0x00, 0x01, 8, 8}, nil} {
		if _, err := parseUDPFrame(in); err == nil {
			t.Errorf("expected an error for %x", in)
		}
	}
}

func TestSocksRepForErrorDistinguishesPermanent(t *testing.T) {
	// A client retries "host unreachable" and stops on "not allowed by ruleset".
	// Reporting a policy refusal as a transient error makes a client hammer a
	// relay that will never answer.
	tests := []struct {
		msg  string
		want byte
	}{
		{"destination not allowed", socksRepNotAllowed},
		{"resolve example.com: no addresses", socksRepHostUnreach},
		{"dial tcp: connection refused", socksRepConnRefused},
		{"i/o timeout", socksRepTTLExpired},
		{"something else", socksRepGeneralError},
	}
	for _, tt := range tests {
		if got := socksRepForError(errors.New(tt.msg)); got != tt.want {
			t.Errorf("socksRepForError(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func socksRoute(target, authMode, udp string, allow, deny []string) utils.ProxyRouteConfig {
	return utils.ProxyRouteConfig{
		Name:                   "socks-test",
		Target:                 target,
		SocksAuthMode:          authMode,
		SocksUDP:               udp,
		SocksAllowDestinations: allow,
		SocksDenyDestinations:  deny,
	}
}

// startTestRelay binds a relay on an OS-chosen port, mirroring how
// startSocksProxy drives it, and tears it down with the registry's stop signal.
func startTestRelay(t *testing.T, route utils.ProxyRouteConfig) *socksServer {
	t.Helper()
	s, err := bindSocksServer("127.0.0.1", 0, &route)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func relayTCPAddr(t *testing.T, s *socksServer) string {
	t.Helper()
	return s.tcp.Addr().String()
}

func dialRelay(t *testing.T, s *socksServer) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", relayTCPAddr(t, s))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// greet performs the method negotiation and returns the chosen method.
func greet(t *testing.T, conn net.Conn, methods []byte) byte {
	t.Helper()
	greeting := append([]byte{socksVersion, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		t.Fatalf("greeting write: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("greeting reply: %v", err)
	}
	if reply[0] != socksVersion {
		t.Fatalf("greeting reply version = %d, want 5", reply[0])
	}
	return reply[1]
}

// sendRequest writes a CONNECT/UDP request and reads the reply.
func sendRequest(t *testing.T, conn net.Conn, cmd byte, atyp byte, addr []byte, port uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteByte(socksVersion)
	buf.WriteByte(cmd)
	buf.WriteByte(0x00)
	buf.WriteByte(atyp)
	buf.Write(addr)
	binary.Write(&buf, binary.BigEndian, port)
	if _, err := conn.Write(buf.Bytes()); err != nil {
		t.Fatalf("request write: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("request reply: %v", err)
	}
	return reply
}

func startEchoTCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(c, c)
				c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).String()
}

func echoPort(t *testing.T, hostport string) uint16 {
	t.Helper()
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(n)
}

// --- relay lifecycle ---------------------------------------------------------

func TestAnonymousConnectRelaysBytes(t *testing.T) {
	echo := startEchoTCP(t)
	s := startTestRelay(t, socksRoute("socks5://", "", "", nil, nil))

	conn := dialRelay(t, s)
	if m := greet(t, conn, []byte{socksMethodNoAuth}); m != socksMethodNoAuth {
		t.Fatalf("method = %d, want no-auth (%d)", m, socksMethodNoAuth)
	}

	ip := net.ParseIP("127.0.0.1").To4()
	reply := sendRequest(t, conn, socksCmdConnect, socksATYPIPv4, ip, echoPort(t, echo))
	if reply[1] != socksRepSuccess {
		t.Fatalf("connect rep = %d, want success", reply[1])
	}

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("echo read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("echoed %q, want %q", buf, "hello")
	}
}

// A relay configured for password auth must never hand out anonymous access.
// This is the security property the auth mode exists to guarantee.
func TestPasswordRequiredRefusesNoAuthClient(t *testing.T) {
	s := startTestRelay(t, socksRoute("socks5h://", "password", "", nil, nil))
	conn := dialRelay(t, s)

	if m := greet(t, conn, []byte{socksMethodNoAuth}); m != socksMethodNoAccept {
		t.Fatalf("method = %d, want no-acceptable-methods (%d)", m, socksMethodNoAccept)
	}
}

func TestPasswordAuthAcceptsValidCredentials(t *testing.T) {
	orig := socksCheckPassword
	socksCheckPassword = func(user, pass string) error {
		if user == "martin" && pass == "right" {
			return nil
		}
		return errors.New("nope")
	}
	t.Cleanup(func() { socksCheckPassword = orig })

	echo := startEchoTCP(t)
	s := startTestRelay(t, socksRoute("socks5h://", "password", "", nil, nil))
	conn := dialRelay(t, s)

	if m := greet(t, conn, []byte{socksMethodPassword}); m != socksMethodPassword {
		t.Fatalf("method = %d, want password (%d)", m, socksMethodPassword)
	}

	// RFC 1929: version byte, ULEN, UNAME, PLEN, PASSWD; reply is VER, STATUS.
	var creds bytes.Buffer
	creds.WriteByte(0x01)
	creds.WriteByte(byte(len("martin")))
	creds.WriteString("martin")
	creds.WriteByte(byte(len("right")))
	creds.WriteString("right")
	if _, err := conn.Write(creds.Bytes()); err != nil {
		t.Fatal(err)
	}
	status := make([]byte, 2)
	if _, err := io.ReadFull(conn, status); err != nil {
		t.Fatal(err)
	}
	if status[1] != 0x00 {
		t.Fatalf("auth status = %d, want success", status[1])
	}

	reply := sendRequest(t, conn, socksCmdConnect, socksATYPIPv4, net.ParseIP("127.0.0.1").To4(), echoPort(t, echo))
	if reply[1] != socksRepSuccess {
		t.Fatalf("connect rep = %d, want success", reply[1])
	}
}

func TestPasswordAuthRejectsBadCredentials(t *testing.T) {
	orig := socksCheckPassword
	socksCheckPassword = func(user, pass string) error { return errors.New("nope") }
	t.Cleanup(func() { socksCheckPassword = orig })

	s := startTestRelay(t, socksRoute("socks5h://", "password", "", nil, nil))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodPassword})

	var creds bytes.Buffer
	creds.WriteByte(0x01)
	creds.WriteByte(4)
	creds.WriteString("mart")
	creds.WriteByte(3)
	creds.WriteString("bad")
	if _, err := conn.Write(creds.Bytes()); err != nil {
		t.Fatal(err)
	}
	status := make([]byte, 2)
	if _, err := io.ReadFull(conn, status); err != nil {
		t.Fatal(err)
	}
	if status[1] != 0x01 {
		t.Fatalf("auth status = %d, want failure (1)", status[1])
	}
}

func TestBindCommandIsRefused(t *testing.T) {
	// BIND is the reverse-connect command: it makes the relay open a listening
	// socket and invite a third party to connect to it.
	s := startTestRelay(t, socksRoute("socks5://", "", "", nil, nil))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdBind, socksATYPIPv4, net.ParseIP("127.0.0.1").To4(), 8080)
	if reply[1] != socksRepCmdNotSupport {
		t.Fatalf("bind rep = %d, want command-not-supported (%d)", reply[1], socksRepCmdNotSupport)
	}
}

func TestDenyListBlocksConnect(t *testing.T) {
	echo := startEchoTCP(t)
	s := startTestRelay(t, socksRoute("socks5://", "", "", nil, []string{"127.0.0.0/8"}))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdConnect, socksATYPIPv4, net.ParseIP("127.0.0.1").To4(), echoPort(t, echo))
	// "not allowed by ruleset" is the permanent code; a client stops retrying on
	// it, which is the correct response to a policy refusal.
	if reply[1] != socksRepNotAllowed {
		t.Fatalf("rep = %d, want not-allowed-by-ruleset (%d)", reply[1], socksRepNotAllowed)
	}
}

func TestUDPAssociateRefusedWhenUDPDisabled(t *testing.T) {
	s := startTestRelay(t, socksRoute("socks5h://", "", "false", nil, nil))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdUDPAssociate, socksATYPIPv4, net.IPv4zero, 0)
	if reply[1] != socksRepCmdNotSupport {
		t.Fatalf("rep = %d, want command-not-supported", reply[1])
	}
}

func TestUDPAssociateRelaysDatagramRoundTrip(t *testing.T) {
	// A UDP echo server stands in for the destination.
	echoUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echoUDP.Close() })
	go func() {
		buf := make([]byte, 1024)
		for {
			n, from, err := echoUDP.ReadFrom(buf)
			if err != nil {
				return
			}
			echoUDP.WriteTo(buf[:n], from)
		}
	}()
	echoAddr := echoUDP.LocalAddr().(*net.UDPAddr)

	s := startTestRelay(t, socksRoute("socks5t://", "", "", nil, nil))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdUDPAssociate, socksATYPIPv4, net.IPv4zero, 0)
	if reply[1] != socksRepSuccess {
		t.Fatalf("associate rep = %d, want success", reply[1])
	}
	// The reply advertises the server's UDP address: that is where datagrams go.
	bndPort := int(binary.BigEndian.Uint16(reply[8:10]))
	if bndPort != s.udp.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("advertised bnd port %d, relay listens on %d", bndPort, s.udp.LocalAddr().(*net.UDPAddr).Port)
	}

	// A real client sends datagrams from its own socket, on a port the server is
	// never told about. This is the path that a TCP-address-keyed association
	// drops.
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clientUDP.Close() })

	frame := []byte{0x00, 0x00, socksATYPIPv4, 127, 0, 0, 1, byte(echoAddr.Port >> 8), byte(echoAddr.Port & 0xFF)}
	frame = append(frame, []byte("ping")...)
	if _, err := clientUDP.WriteTo(frame, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: bndPort}); err != nil {
		t.Fatal(err)
	}

	clientUDP.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp := make([]byte, 1024)
	n, from, err := clientUDP.ReadFrom(resp)
	if err != nil {
		t.Fatalf("no response datagram: %v", err)
	}
	// Replies arrive on the client's own UDP socket (the association is keyed by
	// client IP, not by the TCP control connection) and carry the relay's
	// advertised BND.ADDR/BND.PORT as their source.
	if from.String() != "127.0.0.1:"+strconv.Itoa(bndPort) {
		t.Fatalf("reply source = %s, want the advertised %s:%d", from, "127.0.0.1", bndPort)
	}
	// Parse the reply the way a client does: RSVD + FRAG + ATYP + ADDR + PORT,
	// then the payload. Hand-counting offsets here would hide exactly the kind
	// of framing bug this suite exists to catch.
	parsed, err := parseUDPFrame(resp[:n])
	if err != nil {
		t.Fatalf("reply is not a valid socks udp frame: %v", err)
	}
	if resp[1] != 0x00 {
		t.Fatalf("reply FRAG = %d, want 0 (the relay never fragments)", resp[1])
	}
	if parsed.String() != echoAddr.String() {
		t.Fatalf("reply destination = %s, want the echo server %s", parsed, echoAddr)
	}

	payload := resp[socksUDPFrameHeader+socksFrameLen(parsed) : n]
	if string(payload) != "ping" {
		t.Fatalf("payload = %q, want %q", payload, "ping")
	}
}

// --- policy & validation -----------------------------------------------------

func TestAllowDestinationPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		allow []string
		deny  []string
		host  string
		ip    string
		want  bool
	}{
		{"empty allow means all", nil, nil, "example.com", "", true},
		{"deny beats allow", []string{"example.com"}, []string{"example.com"}, "example.com", "", false},
		{"allow list restricts", []string{"example.com"}, nil, "other.com", "", false},
		{"wildcard covers apex and labels", []string{"*.example.com"}, nil, "deep.example.com", "", true},
		{"wildcard covers apex", []string{"*.example.com"}, nil, "example.com", "", true},
		{"wildcard does not cross boundaries", []string{"*.example.com"}, nil, "evil.example.com.attacker.net", "", false},
		{"cidr matches", []string{"10.0.0.0/8"}, nil, "", "10.1.2.3", true},
		{"cidr excludes", []string{"10.0.0.0/8"}, nil, "", "11.1.2.3", false},
		{"deny cidr blocks ip", nil, []string{"127.0.0.0/8"}, "", "127.0.0.1", false},
		{"bare ip allow", []string{"8.8.8.8"}, nil, "", "8.8.8.8", true},
	}
	for _, tt := range tests {
		p, err := newSocksPolicy(Socks5hScheme, "", tt.allow, tt.deny, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var ip net.IP
		if tt.ip != "" {
			ip = net.ParseIP(tt.ip)
		}
		if got := p.allowDestination(tt.host, ip); got != tt.want {
			t.Errorf("%s: allowDestination(%q,%q) = %v, want %v", tt.name, tt.host, tt.ip, got, tt.want)
		}
	}
}

func TestPolicyRejectsMalformedDenyList(t *testing.T) {
	// A typo in a deny list is a hole, not a cosmetic problem: bind must fail
	// loudly rather than accept a rule set that matches nothing.
	if _, err := newSocksPolicy(Socks5hScheme, "", nil, []string{"not an address"}, nil); err == nil {
		t.Fatal("expected an error for a malformed deny entry")
	}
	if _, err := newSocksPolicy(Socks5hScheme, "", []string{"10.0.0.0/33"}, nil, nil); err == nil {
		t.Fatal("expected an error for an invalid CIDR")
	}
}

func TestUDPEnablementFollowsScheme(t *testing.T) {
	trueVal, falseVal := true, false
	tests := []struct {
		scheme   string
		override *bool
		want     bool
	}{
		{Socks5Scheme, nil, false},
		{Socks5hScheme, nil, false},
		{Socks5tScheme, nil, true},
		{Socks5tScheme, &falseVal, false},
		{Socks5Scheme, &trueVal, true},
	}
	for _, tt := range tests {
		p, err := newSocksPolicy(tt.scheme, "", nil, nil, tt.override)
		if err != nil {
			t.Fatalf("%s: %v", tt.scheme, err)
		}
		if p.udpEnabled != tt.want {
			t.Errorf("%s override=%v: udpEnabled = %v, want %v", tt.scheme, tt.override, p.udpEnabled, tt.want)
		}
	}
}

func TestDenyListAppliesToResolvedAddress(t *testing.T) {
	// A name that resolves to loopback must not bypass "deny 127.0.0.0/8". This
	// is the DNS-rebinding-shaped hole in destination filters.
	p, err := newSocksPolicy(Socks5hScheme, "", nil, []string{"127.0.0.0/8"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.resolveAndDial("localhost", "9", nil); err == nil {
		t.Fatal("expected the resolved loopback address to be refused")
	} else if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected a policy refusal, got %v", err)
	}
}

func TestRejectsInvalidDestinationPort(t *testing.T) {
	p, err := newSocksPolicy(Socks5Scheme, "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"0", "65536", "abc", ""} {
		if _, err := p.resolveAndDial("127.0.0.1", port, nil); err == nil {
			t.Errorf("port %q accepted, want rejection", port)
		}
	}
}

func TestValidateSocksRouteAcceptsValidConfigs(t *testing.T) {
	valid := []utils.ProxyRouteConfig{
		socksRoute("socks5://", "", "", nil, nil),
		socksRoute("socks5h://", "password", "", []string{"*.example.com"}, nil),
		socksRoute("socks5t://", "none", "false", nil, []string{"127.0.0.0/8"}),
		{Name: "http", Target: "http://localhost:8080"},
	}
	for _, r := range valid {
		if msg := ValidateSocksRoute(r); msg != "" {
			t.Errorf("valid route %s/%s rejected: %s", r.Target, r.Name, msg)
		}
	}
}

func TestValidateSocksRouteRejectsMeaninglessConfigs(t *testing.T) {
	bad := []struct {
		name  string
		route utils.ProxyRouteConfig
		want  string
	}{
		{"bad auth mode", socksRoute("socks5h://", "basic", "", nil, nil), "auth mode"},
		{"bad udp value", socksRoute("socks5t://", "", "yes", nil, nil), "udp value"},
		{"relay with pool targets", func() utils.ProxyRouteConfig {
			r := socksRoute("socks5h://", "", "", nil, nil)
			r.AdditionalTargets = []string{"socks5h://"}
			return r
		}(), "additional targets"},
		{"socks options on non-relay", func() utils.ProxyRouteConfig {
			r := utils.ProxyRouteConfig{Name: "http", Target: "http://localhost:8080", SocksAuthMode: "password"}
			return r
		}(), "sets SOCKS relay options"},
		{"malformed deny", socksRoute("socks5h://", "", "", nil, []string{"@@@"}), "socks_deny_destinations"},
	}
	for _, tt := range bad {
		msg := ValidateSocksRoute(tt.route)
		if msg == "" {
			t.Errorf("%s: accepted, want a rejection mentioning %q", tt.name, tt.want)
			continue
		}
		if !strings.Contains(msg, tt.want) {
			t.Errorf("%s: message %q should mention %q", tt.name, msg, tt.want)
		}
	}
}

func TestReadRequestRejectsNonSocks5Version(t *testing.T) {
	// The request's first byte is VER. A SOCKS4 request (version 4, then a port
	// and a user) must be refused rather than parsed as SOCKS5 -- that is how a
	// relay ends up accepting a protocol it never advertised.
	for _, ver := range []byte{0x00, 0x04, 0x06} {
		if _, err := readRequest(bytes.NewReader([]byte{ver, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0, 80})); err == nil {
			t.Errorf("request with version %d accepted, want rejection", ver)
		}
	}
}

func TestReadRequestRejectsNonZeroReserved(t *testing.T) {
	if _, err := readRequest(bytes.NewReader([]byte{socksVersion, 0x01, 0x01, 0x01, 1, 2, 3, 4, 0, 80})); err == nil {
		t.Fatal("RESV byte 1 accepted, RFC 1928 requires 0x00")
	}
}
