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

func socksRoute(target, authMode string, allow, deny []string) utils.ProxyRouteConfig {
	return utils.ProxyRouteConfig{
		Name:                   "socks-test",
		Target:                 target,
		SocksAuthMode:          authMode,
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
	s := startTestRelay(t, socksRoute("socks5://", "", nil, nil))

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
	s := startTestRelay(t, socksRoute("socks5h://", "password", nil, nil))
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
	s := startTestRelay(t, socksRoute("socks5h://", "password", nil, nil))
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

	s := startTestRelay(t, socksRoute("socks5h://", "password", nil, nil))
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

// UDP ASSOCIATE is refused, not implemented: the relay is TCP-only. A client
// probing it must get "command not supported" so it falls back to CONNECT --
// replying success and then dropping every datagram is the failure that reads
// to a user as "the proxy is broken".
func TestUDPAssociateIsRefused(t *testing.T) {
	for _, target := range []string{"socks5://", "socks5h://"} {
		s := startTestRelay(t, socksRoute(target, "", nil, nil))
		conn := dialRelay(t, s)
		greet(t, conn, []byte{socksMethodNoAuth})

		// sendRequest reads the full 10-byte reply: a client that gets a
		// truncated reply hangs. RFC 1928 closes the connection after a refused
		// request (one request per connection), so the assertion here is that the
		// answer arrived and the relay goes on accepting clients.
		reply := sendRequest(t, conn, socksCmdUDPAssociate, socksATYPIPv4, net.IPv4zero, 0)
		if reply[1] != socksRepCmdNotSupport {
			t.Fatalf("%s: rep = %d, want command-not-supported", target, reply[1])
		}
		conn.Close()

		again := dialRelay(t, s)
		if m := greet(t, again, []byte{socksMethodNoAuth}); m != socksMethodNoAuth {
			t.Errorf("%s: relay stopped accepting clients after a refusal (method %d)", target, m)
		}
	}
}

func TestBindCommandIsRefused(t *testing.T) {
	// BIND is the reverse-connect command: it makes the relay open a listening
	// socket and invite a third party to connect to it.
	s := startTestRelay(t, socksRoute("socks5://", "", nil, nil))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdBind, socksATYPIPv4, net.ParseIP("127.0.0.1").To4(), 8080)
	if reply[1] != socksRepCmdNotSupport {
		t.Fatalf("bind rep = %d, want command-not-supported (%d)", reply[1], socksRepCmdNotSupport)
	}
}

func TestDenyListBlocksConnect(t *testing.T) {
	echo := startEchoTCP(t)
	s := startTestRelay(t, socksRoute("socks5://", "", nil, []string{"127.0.0.0/8"}))
	conn := dialRelay(t, s)
	greet(t, conn, []byte{socksMethodNoAuth})

	reply := sendRequest(t, conn, socksCmdConnect, socksATYPIPv4, net.ParseIP("127.0.0.1").To4(), echoPort(t, echo))
	// "not allowed by ruleset" is the permanent code; a client stops retrying on
	// it, which is the correct response to a policy refusal.
	if reply[1] != socksRepNotAllowed {
		t.Fatalf("rep = %d, want not-allowed-by-ruleset (%d)", reply[1], socksRepNotAllowed)
	}
}

// The scheme list is exactly what clients speak. socks5t is the scheme no
// client can send: no URL parser, curl, browser or library accepts it, so
// accepting it here would offer a route target nothing can use.
func TestSchemesAccepted(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		if _, err := newSocksPolicy(scheme, "", nil, nil); err != nil {
			t.Errorf("%s: %v", scheme, err)
		}
	}
	for _, scheme := range []string{"socks5t", "socks4", "socks5s", ""} {
		if _, err := newSocksPolicy(scheme, "", nil, nil); err == nil {
			t.Errorf("%s: accepted, want rejection", scheme)
		}
	}
}

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
		p, err := newSocksPolicy(Socks5hScheme, "", tt.allow, tt.deny)
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
	if _, err := newSocksPolicy(Socks5hScheme, "", nil, []string{"not an address"}); err == nil {
		t.Fatal("expected an error for a malformed deny entry")
	}
	if _, err := newSocksPolicy(Socks5hScheme, "", []string{"10.0.0.0/33"}, nil); err == nil {
		t.Fatal("expected an error for an invalid CIDR")
	}
}

func TestDenyListAppliesToResolvedAddress(t *testing.T) {
	// A name that resolves to loopback must not bypass "deny 127.0.0.0/8". This
	// is the DNS-rebinding-shaped hole in destination filters.
	p, err := newSocksPolicy(Socks5hScheme, "", nil, []string{"127.0.0.0/8"})
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
	p, err := newSocksPolicy(Socks5Scheme, "", nil, nil)
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
		socksRoute("socks5://", "", nil, nil),
		socksRoute("socks5h://", "password", []string{"*.example.com"}, nil),
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
		{"bad auth mode", socksRoute("socks5h://", "basic", nil, nil), "auth mode"},
		{"socks5t is not a scheme", socksRoute("socks5t://", "", nil, nil), "unsupported SOCKS scheme"},
		{"socks4a is not a scheme", socksRoute("socks4a://", "", nil, nil), "unsupported SOCKS scheme"},
		{"socks5t with options", socksRoute("socks5t://", "password", nil, nil), "unsupported SOCKS scheme"},
		{"relay with pool targets", func() utils.ProxyRouteConfig {
			r := socksRoute("socks5h://", "", nil, nil)
			r.AdditionalTargets = []string{"socks5h://"}
			return r
		}(), "additional targets"},
		{"socks options on non-relay", func() utils.ProxyRouteConfig {
			r := utils.ProxyRouteConfig{Name: "http", Target: "http://localhost:8080", SocksAuthMode: "password"}
			return r
		}(), "sets SOCKS relay options"},
		{"malformed deny", socksRoute("socks5h://", "", nil, []string{"@@@"}), "socks_deny_destinations"},
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
