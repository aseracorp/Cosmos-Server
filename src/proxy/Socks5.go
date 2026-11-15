package proxy

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// Socks5.go implements a TCP-only SOCKS5 relay (RFC 1928 CONNECT) with
// username/password authentication (RFC 1929), exposed as a route target scheme
// ("socks5", "socks5h").
//
// Design notes, in the order the constraints matter:
//
//  1. A relay route is a listener, so it lives in the SocketProxy registry: same
//     Bind/Close lifecycle, same port-collision semantics, same "rebind on
//     config change" path as the tcp/udp port forwarders.
//  2. A relay is not a forwarder. It has no upstream target in the route: the
//     destination comes from the client's request. Consequences: a relay route
//     cannot be a pool member (pool members are dial targets), and the relay must
//     not inherit the target-port collision checks.
//  3. Credentials come from Cosmos's own user store (utils.CheckPassword), the
//     same account a dashboard user logs in with, gated by socks_auth_mode. There
//     is no per-route credential field, so there is no second auth system with
//     semantics that differ from the rest of the proxy.
//  4. Destination filtering is applied before dialing and re-checked against the
//     resolved address. Deny always beats allow.
//  5. socks5 vs socks5h decides who resolves the name. That is the whole scheme
//     split, and it is the only one clients speak.
//
// Deliberate limits, each enforced in code rather than only documented:
//   - No TLS, no GSSAPI. Methods offered are "no auth" and "password" only.
//   - TCP only. UDP ASSOCIATE (0x03) is refused, not implemented. Cosmos is a
//     reverse proxy: routes expose listeners and forward to services, and every
//     UDP thing a deployment needs (a gameserver, DNS, a UDP health check) is
//     already served by a udp socket route. A SOCKS client reaching arbitrary
//     UDP destinations through Cosmos is a capability this deployment has no
//     reason to add, and it needs its own UDP socket per association plus a
//     client-address tracker to be done safely.
//   - Command 0x02 (BIND) is refused. It is the server-side reverse-connect
//     command, which turns a relay into a listener on someone else's schedule.
//   - CONNECT is the only command handled.

const (
	socksVersion      = 0x05
	socksMethodNoAuth = 0x00
	// socksMethodPassword is 0x02 per RFC 1929. 0x01 is GSSAPI, which this relay
	// does not speak.
	socksMethodPassword = 0x02
	socksMethodNoAccept = 0xFF

	socksCmdConnect      = 0x01
	socksCmdBind         = 0x02
	socksCmdUDPAssociate = 0x03 // refused: see the header comment

	socksATYPIPv4   = 0x01
	socksATYPDomain = 0x03
	socksATYPIPv6   = 0x04

	socksRepSuccess        = 0x00
	socksRepGeneralError   = 0x01
	socksRepNotAllowed     = 0x02
	socksRepNetworkUnreach = 0x03
	socksRepHostUnreach    = 0x04
	socksRepConnRefused    = 0x05
	socksRepTTLExpired     = 0x06
	socksRepCmdNotSupport  = 0x07
	socksRepATYPNotSupport = 0x08

	// socksHandshakeTimeout bounds how long a client may spend on greeting, auth
	// and request. Without it a slow client pins a goroutine and a connection
	// forever.
	socksHandshakeTimeout = 15 * time.Second
)

// socksServer is a bound SOCKS5 relay: a TCP listener for greetings and
// CONNECT, and nothing else.
type socksServer struct {
	tcp    net.Listener
	policy *socksPolicy

	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	// conns tracks accepted client connections so Close() can interrupt them.
	// Without it, Close() closes only the listener: a live CONNECT tunnel keeps
	// pumping, handleTCP never returns, and the wg.Wait() in Close() hangs —
	// which in production means StopAllProxies spends its 30s timeout on every
	// config change that touches a relay route.
	connsLock sync.Mutex
	conns     map[net.Conn]struct{}
}

// bindSocksServer compiles the policy and binds the TCP listener.
func bindSocksServer(host string, port int, rtr *utils.ProxyRouteConfig) (*socksServer, error) {
	policy, err := policyFromRoute(rtr)
	if err != nil {
		return nil, err
	}

	tcp, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("failed to listen socks %s:%d: %w", host, port, err)
	}

	s := &socksServer{
		tcp:     tcp,
		policy:  policy,
		closeCh: make(chan struct{}),
		conns:   make(map[net.Conn]struct{}),
	}

	s.wg.Add(1)
	go s.acceptTCP()

	return s, nil
}

func (s *socksServer) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.tcp.Close()

		// Interrupt live tunnels: the pumps end on conn close, which lets the
		// per-connection goroutines exit so wg.Wait() returns.
		s.connsLock.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.connsLock.Unlock()
	})
	s.wg.Wait()
	return nil
}

func (s *socksServer) acceptTCP() {
	defer s.wg.Done()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			select {
			case <-s.closeCh:
			default:
				utils.Debug("[Socks5] accept failed: " + err.Error())
			}
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleTCP(conn)
		}()
	}
}

// handleTCP runs one client connection: greeting, auth, request, then a byte
// tunnel.
//
// The handshake is deadline-bounded; the relay after it is not. A relayed tunnel
// has no meaningful idle bound: a long-lived SSH session through the relay is
// legitimate traffic, and killing it would break the feature.
func (s *socksServer) handleTCP(client net.Conn) {
	defer func() {
		s.connsLock.Lock()
		delete(s.conns, client)
		s.connsLock.Unlock()
		client.Close()
	}()
	s.connsLock.Lock()
	s.conns[client] = struct{}{}
	s.connsLock.Unlock()

	_ = client.SetReadDeadline(time.Now().Add(socksHandshakeTimeout))

	method, err := s.negotiate(client)
	if err != nil {
		utils.Debug("[Socks5] handshake failed from " + client.RemoteAddr().String() + ": " + err.Error())
		return
	}
	if method == socksMethodNoAccept {
		return
	}
	if method == socksMethodPassword {
		if err := s.authenticate(client); err != nil {
			utils.Warn("[Socks5] authentication failed from " + client.RemoteAddr().String() + ": " + err.Error())
			return
		}
	}

	req, err := readRequest(client)
	if err != nil {
		utils.Debug("[Socks5] bad request from " + client.RemoteAddr().String() + ": " + err.Error())
		return
	}

	// Handshake done; a relayed tunnel may idle as long as it needs.
	client.SetReadDeadline(time.Time{})

	switch req.command {
	case socksCmdConnect:
		s.handleConnect(client, req)
	case socksCmdBind, socksCmdUDPAssociate:
		// BIND is the reverse-connect command; UDP ASSOCIATE needs a UDP socket
		// per association. Both are refused on purpose, see the header comment:
		// a client probing them gets a clean "not supported" and falls back to
		// CONNECT.
		writeReply(client, socksRepCmdNotSupport, socksAddrFrom(req))
	default:
		writeReply(client, socksRepCmdNotSupport, socksAddrFrom(req))
	}
}

// negotiate reads the greeting and picks exactly one method.
//
// RFC 1928 requires the server to choose one method. Offering several and letting
// the client pick is how a server ends up handing out "no auth" to a client that
// wanted a password, so the reply always carries a single byte.
func (s *socksServer) negotiate(client net.Conn) (byte, error) {
	methods, err := readGreeting(client)
	if err != nil {
		return 0, err
	}

	if !s.policy.needPassword {
		if err := writeMethodReply(client, socksMethodNoAuth); err != nil {
			return 0, err
		}
		return socksMethodNoAuth, nil
	}

	for _, m := range methods {
		if m == socksMethodPassword {
			if err := writeMethodReply(client, socksMethodPassword); err != nil {
				return 0, err
			}
			return socksMethodPassword, nil
		}
	}

	// The relay requires a password and the client did not offer one. Refuse with
	// the RFC's "no acceptable methods" byte; downgrading to anonymous is the
	// failure mode this function exists to prevent.
	_ = writeMethodReply(client, socksMethodNoAccept)
	return socksMethodNoAccept, fmt.Errorf("client did not offer password auth, which this relay requires")
}

// authenticate runs the RFC 1929 username/password subnegotiation.
//
// Credentials are verified per connection against the live user store, so a
// password change takes effect on the next connection without rebinding and a
// deleted user stops working immediately.
func (s *socksServer) authenticate(client net.Conn) error {
	// RFC 1929 opens the subnegotiation with a version byte. Reading ULEN
	// without consuming it makes VER (0x01) the username length: the server then
	// reads one byte of name, treats the next name byte as the password length,
	// and blocks on a password that will never arrive.
	ver, err := readByte(client)
	if err != nil {
		return err
	}
	if ver != 0x01 {
		return fmt.Errorf("unsupported socks auth version %d", ver)
	}

	userBytes, err := readLengthPrefixed(client)
	if err != nil {
		return err
	}
	passBytes, err := readLengthPrefixed(client)
	if err != nil {
		return err
	}

	if err := s.policy.checkCredentials(string(userBytes), string(passBytes)); err != nil {
		_ = writeAuthReply(client, 0x01)
		return err
	}
	return writeAuthReply(client, 0x00)
}

// handleConnect dials the destination and pumps bytes both ways.
//
// The reply echoes the destination the client asked for. RFC 1928 wants the
// address the server bound to for CONNECT, which for a relay is the destination
// address itself; echoing the request is the same value and avoids a second name
// resolution.
func (s *socksServer) handleConnect(client net.Conn, req socksRequest) {
	upstream, err := s.policy.resolveAndDial(req.host, req.port, req.ip)
	if err != nil {
		writeReply(client, socksRepForError(err), socksAddrFrom(req))
		utils.Debug("[Socks5] connect from " + client.RemoteAddr().String() + " to " + req.String() + " refused: " + err.Error())
		return
	}
	defer upstream.Close()

	if err := writeReply(client, socksRepSuccess, socksAddrFrom(req)); err != nil {
		utils.Debug("[Socks5] connect reply write failed: " + err.Error())
		return
	}
	utils.Debug("[Socks5] relayed " + client.RemoteAddr().String() + " -> " + req.String())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pump(client, upstream)
	}()
	go func() {
		defer wg.Done()
		pump(upstream, client)
	}()
	wg.Wait()
}
