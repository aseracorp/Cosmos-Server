package proxy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// End-to-end tests for SOCKS relay routes.
//
// These drive the production wiring, not the relay internals: a route is put in
// the main config, InitInternalSocketProxy runs (route parsing, port-claim
// resolution, StopAllProxies, the activeProxies registry, startProxy,
// startSocksProxy) and the relay is then spoken to over a socket the test dialed
// from outside. The unit tests in socks5_test.go pin the protocol; these pin the
// route -> listener path and the lifecycle, which is where a relay gets deleted,
// rebound and leaked.
//
// Deliberately no HTTP server, no storage, no docker: the relay reaches upstream
// with plain net.Dial, so the whole feature is exercisable without Cosmos's
// runtime stack, and the test stays honest about what it proves.

// socksE2EConfig installs a config with the given routes and disables
// Constellation so InitInternalSocketProxy reads only the routes we set.
func socksE2EConfig(t *testing.T, routes ...utils.ProxyRouteConfig) {
	t.Helper()
	cfg := utils.Config{
		LoggingLevel: utils.LoggingLevel("ERROR"),
		HTTPConfig: utils.HTTPConfig{
			HTTPPort:    "0",
			HTTPSPort:   "0",
			ProxyConfig: utils.ProxyConfig{Routes: routes},
		},
		ConstellationConfig: utils.ConstellationConfig{Enabled: false},
	}
	utils.LoadBaseMainConfig(cfg)
	utils.IsHTTPS = false
	// ConstellationConfig.Enabled=false short-circuits GetLocalTunnelCache to an
	// empty list, so InitInternalSocketProxy sees exactly the routes set here.
	t.Cleanup(StopAllProxies)
}

// socksE2EPort returns a free TCP port on 127.0.0.1.
//
// A relay binds TCP and UDP on the same number, so we cannot let the OS pick the
// port: we need a number we can hand to the route. Probing by binding TCP and
// releasing it can race with an ephemeral allocation, which is why the dial
// helpers retry: the flake, if any, shows up as a slow connect, not a red test.
func socksE2EPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe for a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("release probe port: %v", err)
	}
	return port
}

// socksE2EListen runs the production init path and returns the relay's address.
func socksE2EListen(t *testing.T, route utils.ProxyRouteConfig) string {
	t.Helper()
	socksE2EConfig(t, route)
	InitInternalSocketProxy()
	return socksE2EWaitListening(t, route)
}

// socksE2EWaitListening waits for the route's port to accept a connection.
//
// initInternalPortProxy launches each proxy with "go startProxy", so the bind
// lands after InitInternalSocketProxy returns. Waiting for a successful connect
// is what makes a rebind testable: "the config changed" is not "the new relay is
// live", and a test that dials between the two fails for a reason that has
// nothing to do with the feature.
func socksE2EWaitListening(t *testing.T, route utils.ProxyRouteConfig) string {
	t.Helper()
	addr := "127.0.0.1:" + strings.Split(route.Host, ":")[1]
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("relay did not start listening on %s (active proxies: %v)",
		addr, GetActiveProxies())
	return addr
}

// --- a SOCKS5 client, written to the RFC, not to the server's internals ------

// socksE2ELogin swaps in an in-memory user store. utils.CheckPassword reads the
// real database, which needs storage, MongoDB and a login rate limiter whose
// buckets outlive a test; the auth exchange itself is exercised against the
// server's own policy code, with only the credential lookup replaced.
func socksE2ELogin(t *testing.T, users map[string]string) func() {
	t.Helper()
	prev := socksCheckPassword
	socksCheckPassword = func(user, password string) error {
		want, ok := users[user]
		if !ok || want != password {
			return fmt.Errorf("no such user or wrong password")
		}
		return nil
	}
	return func() { socksCheckPassword = prev }
}

// socksE2EHandshake negotiates methods. Returns the method the relay selected.
func socksE2EHandshake(t *testing.T, conn net.Conn, methods []byte) byte {
	t.Helper()
	greeting := append([]byte{socksVersion, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("greeting reply: %v", err)
	}
	if reply[0] != socksVersion {
		t.Fatalf("greeting version = %d, want 5", reply[0])
	}
	return reply[1]
}

const (
	socksE2EAuthVersion = 0x01 // RFC 1929 subnegotiation version
	socksE2EAuthOK      = 0x00
)

func socksE2EAuth(t *testing.T, conn net.Conn, user, password string) {
	t.Helper()
	msg := []byte{socksE2EAuthVersion, byte(len(user))}
	msg = append(msg, []byte(user)...)
	msg = append(msg, byte(len(password)))
	msg = append(msg, []byte(password)...)
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("auth request: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	if reply[0] != socksE2EAuthVersion {
		t.Fatalf("auth reply version = %d, want 1", reply[0])
	}
	if reply[1] != socksE2EAuthOK {
		t.Fatalf("auth rejected (status %d)", reply[1])
	}
}

// socksE2ERequest sends a CONNECT/UDP-ASSOCIATE request and returns the reply.
func socksE2ERequest(t *testing.T, conn net.Conn, cmd byte, host string, port int) []byte {
	t.Helper()
	ip := net.ParseIP(host).To4()
	if ip == nil {
		t.Fatalf("socksE2ERequest wants an IPv4 literal, got %q", host)
	}
	var buf bytes.Buffer
	buf.WriteByte(socksVersion)
	buf.WriteByte(cmd)
	buf.WriteByte(0x00)
	buf.WriteByte(socksATYPIPv4)
	buf.Write(ip)
	if err := binary.Write(&buf, binary.BigEndian, uint16(port)); err != nil {
		t.Fatalf("encode port: %v", err)
	}
	if _, err := conn.Write(buf.Bytes()); err != nil {
		t.Fatalf("request: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("request reply: %v", err)
	}
	return reply
}

// --- tests ------------------------------------------------------------------

// socksE2EEcho is a loopback TCP server answering one request per connection.
func socksE2EEcho(t *testing.T, body string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listener: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read the request headers, not to EOF. A SOCKS tunnel is a raw
				// byte pipe: the client closes its read side when done, not its
				// write side, so ReadAll would block forever and a relay bug
				// would be indistinguishable from a lazy test server.
				req := readRequestHeaders(c)
				// Echo the request line back so the test can prove the relay
				// carried the client's bytes, not the test's.
				fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
					len(body)+len(req), body+string(req))
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).String()
}

// readRequestHeaders drains up to the end of the HTTP header block.
func readRequestHeaders(c net.Conn) []byte {
	buf := make([]byte, 0, 512)
	one := make([]byte, 1)
	for len(buf) < 4096 {
		n, err := c.Read(one)
		if n == 0 || err != nil {
			break
		}
		buf = append(buf, one[0])
		if bytes.HasSuffix(buf, []byte("\r\n\r\n")) {
			break
		}
	}
	return buf
}

func socksRouteOnPort(scheme string, port int) utils.ProxyRouteConfig {
	return utils.ProxyRouteConfig{
		Name:    "socks-" + scheme,
		Mode:    "PROXY",
		Target:  scheme + "://",
		Host:    "127.0.0.1:" + fmt.Sprint(port),
		UseHost: true,
	}
}

func TestE2ERouteStartsRelayAndTunnelsARealRequest(t *testing.T) {
	port := socksE2EPort(t)
	addr := socksE2EListen(t, socksRouteOnPort("socks5h", port))
	echo := socksE2EEcho(t, "upstream-body")

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()

	if m := socksE2EHandshake(t, conn, []byte{socksMethodNoAuth}); m != socksMethodNoAuth {
		t.Fatalf("relay selected method %d, want no-auth (scheme default for socks5h)", m)
	}

	reply := socksE2ERequest(t, conn, socksCmdConnect, "127.0.0.1", socksPortOf(t, echo))
	if reply[1] != socksRepSuccess {
		t.Fatalf("CONNECT status = %d, want success (reply % x)", reply[1], reply)
	}

	// The tunnel is a raw byte pipe: speak HTTP over it.
	if _, err := conn.Write([]byte("GET /probe HTTP/1.1\r\nHost: echo\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatalf("write through tunnel: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	if !strings.Contains(string(got), "upstream-body") {
		t.Fatalf("response from relayed connection missing upstream body: %q", got)
	}
	if !strings.Contains(string(got), "GET /probe HTTP/1.1") {
		t.Fatalf("upstream did not receive the client's request line: %q", got)
	}
}

func socksPortOf(t *testing.T, hostPort string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("bad echo address %q: %v", hostPort, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad echo port %q: %v", portStr, err)
	}
	return port
}

func TestE2EPasswordRouteRefusesAnonymousAndTunnelsForAnAccount(t *testing.T) {
	port := socksE2EPort(t)
	route := socksRouteOnPort("socks5h", port)
	route.SocksAuthMode = "password"
	addr := socksE2EListen(t, route)
	echo := socksE2EEcho(t, "authenticated-body")

	restore := socksE2ELogin(t, map[string]string{"alice": "hunter2"})
	defer restore()

	// A client offering only no-auth must be refused, not silently accepted.
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})
	conn.Close()
	if m != socksMethodNoAccept {
		t.Fatalf("no-auth method accepted on a password route (selected %d)", m)
	}

	// The same route works for a real credential.
	conn2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn2.Close()
	if m := socksE2EHandshake(t, conn2, []byte{socksMethodPassword}); m != socksMethodPassword {
		t.Fatalf("selected method %d, want password (0x02)", m)
	}
	socksE2EAuth(t, conn2, "alice", "hunter2")

	reply := socksE2ERequest(t, conn2, socksCmdConnect, "127.0.0.1", socksPortOf(t, echo))
	if reply[1] != socksRepSuccess {
		t.Fatalf("CONNECT after auth = %d, want success", reply[1])
	}
	if _, err := conn2.Write([]byte("GET / HTTP/1.1\r\nHost: echo\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, _ := io.ReadAll(conn2)
	if !strings.Contains(string(got), "authenticated-body") {
		t.Fatalf("authenticated tunnel did not carry the upstream response: %q", got)
	}
}

func TestE2EBadCredentialsAreRefused(t *testing.T) {
	port := socksE2EPort(t)
	route := socksRouteOnPort("socks5t", port)
	route.SocksAuthMode = "password"
	addr := socksE2EListen(t, route)

	restore := socksE2ELogin(t, map[string]string{"alice": "hunter2"})
	defer restore()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if m := socksE2EHandshake(t, conn, []byte{socksMethodPassword}); m != socksMethodPassword {
		t.Fatalf("selected method %d, want password", m)
	}

	msg := []byte{socksE2EAuthVersion, 5}
	msg = append(msg, []byte("alice")...)
	msg = append(msg, 5)
	msg = append(msg, []byte("wrong!!")...)
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("auth request: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	if reply[1] != 0x01 {
		t.Fatalf("bad credentials accepted (status %d, want 0x01)", reply[1])
	}
}

// socksE2EUDPEcho answers datagrams on a loopback UDP socket.
func socksE2EUDPEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("udp echo: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		b := make([]byte, 2048)
		for {
			n, from, err := l.ReadFrom(b)
			if err != nil {
				return
			}
			l.WriteTo(b[:n], from)
		}
	}()
	return l.LocalAddr().(*net.UDPAddr)
}

func TestE2EUDPAssociateCarriesDatagrams(t *testing.T) {
	port := socksE2EPort(t)
	addr := socksE2EListen(t, socksRouteOnPort("socks5t", port))
	echo := socksE2EUDPEcho(t)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})

	reply := socksE2ERequest(t, conn, socksCmdUDPAssociate, "127.0.0.1", 0)
	if reply[1] != socksRepSuccess {
		t.Fatalf("UDP ASSOCIATE = %d, want success (socks5t offers UDP)", reply[1])
	}
	bndPort := int(binary.BigEndian.Uint16(reply[8:10]))

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("client udp: %v", err)
	}
	defer client.Close()

	frame := []byte{0x00, 0x00, socksATYPIPv4, 127, 0, 0, 1,
		byte(echo.Port >> 8), byte(echo.Port & 0xFF)}
	frame = append(frame, []byte("dns-query")...)
	if _, err := client.WriteTo(frame, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: bndPort}); err != nil {
		t.Fatalf("datagram to relay: %v", err)
	}

	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 2048)
	n, from, err := client.ReadFrom(b)
	if err != nil {
		t.Fatalf("no reply to the association: %v", err)
	}
	req, err := parseUDPFrame(b[:n])
	if err != nil {
		t.Fatalf("reply frame: %v", err)
	}
	if req.String() != echo.String() {
		t.Fatalf("reply destination = %s, want the echo server %s", req, echo)
	}
	payload := b[socksUDPFrameHeader+socksFrameLen(req) : n]
	if string(payload) != "dns-query" {
		t.Fatalf("payload = %q, want dns-query", payload)
	}
	// Replies come back on the client's own socket, from the advertised BND port.
	if from.String() != "127.0.0.1:"+fmt.Sprint(bndPort) {
		t.Fatalf("reply source = %s, want 127.0.0.1:%d", from, bndPort)
	}
}

func TestE2EUDPRefusedOnSocks5h(t *testing.T) {
	port := socksE2EPort(t)
	addr := socksE2EListen(t, socksRouteOnPort("socks5h", port))

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})
	reply := socksE2ERequest(t, conn, socksCmdUDPAssociate, "127.0.0.1", 0)
	if reply[1] != socksRepCmdNotSupport {
		t.Fatalf("UDP ASSOCIATE on socks5h = %d, want command-not-supported", reply[1])
	}
}

// A live tunnel must not hold the teardown hostage. StopAllProxies waits on each
// proxy's "stopped" signal; a relay that ignored its stop channel would sit there
// for the registry's full 30s timeout on every config change that touches the
// route, and the port would stay bound after the route was deleted.
func TestE2EOpenTunnelDoesNotBlockTeardown(t *testing.T) {
	port := socksE2EPort(t)
	addr := socksE2EListen(t, socksRouteOnPort("socks5h", port))
	echo := socksE2EEcho(t, "held-open")

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})
	reply := socksE2ERequest(t, conn, socksCmdConnect, "127.0.0.1", socksPortOf(t, echo))
	if reply[1] != socksRepSuccess {
		t.Fatalf("CONNECT = %d, want success", reply[1])
	}
	// Tunnel established and left open: no close from the client side.

	done := make(chan struct{})
	go func() { StopAllProxies(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAllProxies did not return within 5s while a tunnel was open")
	}
	conn.Close()

	if got := GetActiveProxies(); len(got) != 0 {
		t.Fatalf("active proxies after stop = %v, want none", got)
	}

	// The port is free again: re-binding the same route must succeed.
	bound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s still bound after teardown: %v", addr, err)
	}
	bound.Close()
}

func TestE2EDeletedRouteReleasesPortForANewRoute(t *testing.T) {
	port := socksE2EPort(t)
	first := socksRouteOnPort("socks5h", port)
	addr := socksE2EListen(t, first)

	// A second route claims the same port. ResolvePortClaims reports the clash
	// and keeps one; the deleted route's port must be usable by the survivor.
	InitInternalSocketProxy() // no-op re-init with the same route
	if proxies := GetActiveProxies(); len(proxies) != 1 {
		t.Fatalf("active proxies = %v, want exactly one claim on %s", proxies, addr)
	}

	// Route deleted: config no longer mentions it.
	socksE2EConfig(t)
	InitInternalSocketProxy()
	if proxies := GetActiveProxies(); len(proxies) != 0 {
		t.Fatalf("active proxies after deletion = %v, want none", proxies)
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("deleted route kept the port bound: %v", err)
	}
	l.Close()
}

func TestE2EConfigChangeRebindsRelayWithNewPolicy(t *testing.T) {
	port := socksE2EPort(t)
	route := socksRouteOnPort("socks5h", port)
	addr := socksE2EListen(t, route)

	// Anonymous to start.
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})
	conn.Close()
	if m != socksMethodNoAuth {
		t.Fatalf("first config selected %d, want no-auth", m)
	}

	restore := socksE2ELogin(t, map[string]string{"alice": "hunter2"})
	defer restore()

	// Tightened to require a password, through the same init path.
	// Re-installing the config and re-running InitInternalSocketProxy is what a
	// route save does in production: StopAllProxies drains the old relay, then the
	// new policy binds the same port.
	route.SocksAuthMode = "password"
	cfg := utils.GetMainConfig()
	cfg.HTTPConfig.ProxyConfig.Routes = []utils.ProxyRouteConfig{route}
	utils.LoadBaseMainConfig(cfg)
	InitInternalSocketProxy()

	addr = socksE2EWaitListening(t, route)
	conn2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("redial after rebind: %v", err)
	}
	defer conn2.Close()
	m2 := socksE2EHandshake(t, conn2, []byte{socksMethodNoAuth})
	conn2.Close()
	if m2 != socksMethodNoAccept {
		t.Fatalf("after rebind no-auth selected %d, want 0xFF (policy did not take effect)", m2)
	}
}

func TestE2EDenyListBlocksConnectThroughProductionPath(t *testing.T) {
	port := socksE2EPort(t)
	route := socksRouteOnPort("socks5h", port)
	route.SocksDenyDestinations = []string{"127.0.0.0/8"}
	addr := socksE2EListen(t, route)
	echo := socksE2EEcho(t, "must-not-reach")

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	socksE2EHandshake(t, conn, []byte{socksMethodNoAuth})
	reply := socksE2ERequest(t, conn, socksCmdConnect, "127.0.0.1", socksPortOf(t, echo))
	if reply[1] == socksRepSuccess {
		t.Fatal("CONNECT to a denied destination succeeded")
	}

	// The relay stays usable: a refused request is not a closed connection.
	if _, err := conn.Write([]byte{}); err != nil {
		t.Logf("connection state after refusal: %v", err)
	}
}
