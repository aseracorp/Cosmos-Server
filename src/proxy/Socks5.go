package proxy

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// Socks5.go implements a SOCKS5 relay (RFC 1928) with username/password
// authentication (RFC 1929) and UDP ASSOCIATE, exposed as a route target scheme
// ("socks5", "socks5h", "socks5t").
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
//  5. socks5 vs socks5h decides who resolves the name; socks5t additionally
//     offers UDP ASSOCIATE. The scheme is capability signalling, not three
//     implementations.
//
// Deliberate limits, each enforced in code rather than only documented:
//   - No TLS, no GSSAPI. Methods offered are "no auth" and "password" only.
//   - UDP ASSOCIATE relays un-fragmented datagrams. Fragments are dropped rather
//     than reassembled: a half-reassembled datagram is silently corrupt, a
//     refused one is not.
//   - Command 0x02 (BIND) is refused. It is the server-side reverse-connect
//     command, which turns a relay into a listener on someone else's schedule.
//   - Only CONNECT and UDP ASSOCIATE are handled.

const (
	socksVersion      = 0x05
	socksMethodNoAuth = 0x00
	// socksMethodPassword is 0x02 per RFC 1929. 0x01 is GSSAPI, which this relay
	// does not speak.
	socksMethodPassword = 0x02
	socksMethodNoAccept = 0xFF

	socksCmdConnect      = 0x01
	socksCmdBind         = 0x02
	socksCmdUDPAssociate = 0x03

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

	// socksUDPRelayTTL is how long an idle UDP association keeps its relay
	// socket: long enough for a DNS exchange and a QUIC handshake, short enough
	// that abandoned associations do not accumulate sockets.
	socksUDPRelayTTL = 2 * time.Minute

	// socksHandshakeTimeout bounds how long a client may spend on greeting, auth
	// and request. Without it a slow client pins a goroutine and a connection
	// forever.
	socksHandshakeTimeout = 15 * time.Second

	// socksMaxDatagram caps a relayed UDP payload. A compliant client must handle
	// 512 bytes; 2048 covers normal DNS and QUIC while refusing frames that look
	// like amplification attempts.
	socksMaxDatagram = 2048
)

// socksServer is a bound SOCKS5 relay: a TCP listener for greetings and
// CONNECT, plus a UDP socket for UDP ASSOCIATE.
type socksServer struct {
	tcp    net.Listener
	udp    *net.UDPConn
	policy *socksPolicy

	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	relayLock sync.Mutex
	relays    map[string]*socksRelay

	// conns tracks accepted client connections so Close() can interrupt them.
	// Without it, Close() closes only the listeners: a live CONNECT tunnel or a
	// UDP association keeps pumping, handleTCP never returns, and the wg.Wait()
	// in Close() hangs — which in production means StopAllProxies spends its
	// 30s timeout on every config change that touches a relay route.
	connsLock sync.Mutex
	conns     map[net.Conn]struct{}
}

// socksRelay is per-association state: the socket Cosmos uses to talk to
// destinations on behalf of one client, the peer addresses that socket has been
// asked to talk to, and the client's current UDP address.
//
// Associations are keyed by client IP, not client address. A SOCKS client sends
// its UDP ASSOCIATE datagrams from a socket it picked itself — a different port
// from the TCP control connection, one the server is never told about. Keying by
// the TCP address, which looks tidy, drops every datagram a real client sends.
// Keying by IP and remembering the most recent datagram sender is what curl, ssh
// and Dante actually interoperate with.
//
// Responses go only to peers this association sent to, so a third party cannot
// inject datagrams into someone else's association and the relay socket cannot be
// turned into a reflector.
type socksRelay struct {
	conn      *net.UDPConn
	mu        sync.Mutex
	client    net.Addr // current UDP peer; nil until the client sends a datagram
	peers     map[string]bool
	expiresAt time.Time
}

const socksMaxPeersPerRelay = 256

func (r *socksRelay) touch() {
	r.mu.Lock()
	r.expiresAt = time.Now().Add(socksUDPRelayTTL)
	r.mu.Unlock()
}

func (r *socksRelay) setClient(addr net.Addr) {
	r.mu.Lock()
	r.client = addr
	r.expiresAt = time.Now().Add(socksUDPRelayTTL)
	r.mu.Unlock()
}

func (r *socksRelay) getClient() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

func (r *socksRelay) addPeer(addr string) {
	r.mu.Lock()
	if len(r.peers) < socksMaxPeersPerRelay {
		r.peers[addr] = true
	}
	r.mu.Unlock()
}

func (r *socksRelay) isPeer(addr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peers[addr]
}

func (r *socksRelay) expired() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Now().After(r.expiresAt)
}

// hostKey strips the port from an address: associations are keyed by client IP.
func hostKey(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// bindSocksServer compiles the policy and binds the TCP listener plus the UDP
// socket. Both are bound eagerly: a route that advertises UDP ASSOCIATE must not
// discover at first use that it cannot provide it, and a relay route occupies the
// UDP port of its number the same way a udp route does.
func bindSocksServer(host string, port int, rtr *utils.ProxyRouteConfig) (*socksServer, error) {
	policy, err := policyFromRoute(rtr)
	if err != nil {
		return nil, err
	}

	tcp, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("failed to listen socks %s:%d: %w", host, port, err)
	}

	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(host), Port: port})
	if err != nil {
		tcp.Close()
		return nil, fmt.Errorf("failed to listen socks udp %s:%d: %w", host, port, err)
	}

	s := &socksServer{
		tcp:     tcp,
		udp:     udp,
		policy:  policy,
		closeCh: make(chan struct{}),
		relays:  make(map[string]*socksRelay),
		conns:   make(map[net.Conn]struct{}),
	}

	s.wg.Add(2)
	go s.acceptTCP()
	go s.acceptUDP()

	return s, nil
}

func (s *socksServer) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.tcp.Close()
		s.udp.Close()

		// Interrupt live relays: pumps and association reads end on conn close,
		// which lets the per-connection goroutines exit and wg.Wait() return.
		s.connsLock.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.connsLock.Unlock()
		s.relayLock.Lock()
		for _, r := range s.relays {
			r.conn.Close()
		}
		s.relays = map[string]*socksRelay{}
		s.relayLock.Unlock()
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
// tunnel or a UDP association.
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
	case socksCmdUDPAssociate:
		s.handleUDPAssociate(client, req)
	case socksCmdBind:
		// Reverse-connect BIND: refused on purpose, see the header comment.
		writeReply(client, socksRepCmdNotSupport, zeroAddr())
	default:
		writeReply(client, socksRepCmdNotSupport, zeroAddr())
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

// handleUDPAssociate answers a UDP ASSOCIATE request.
//
// The TCP control connection stays open for the life of the association:
// datagrams travel on the UDP socket, and closing the control connection is the
// client's signal that the association is over. Idle associations are swept by
// the TTL; server Close() ends every one of them.
func (s *socksServer) handleUDPAssociate(client net.Conn, req socksRequest) {
	if !s.policy.udpEnabled {
		// UDP is a capability of the scheme (socks5t) or of socks_udp=true.
		// Refusing is protocol-correct: a client probing UDP gets a clean
		// "not supported" and falls back to TCP.
		writeReply(client, socksRepCmdNotSupport, socksAddrFrom(req))
		return
	}

	relay, err := net.ListenUDP("udp", nil)
	if err != nil {
		writeReply(client, socksRepGeneralError, socksAddrFrom(req))
		utils.Error("[Socks5] udp relay bind failed", err)
		return
	}

	key := hostKey(client.RemoteAddr())
	r := &socksRelay{
		conn:      relay,
		peers:     make(map[string]bool),
		expiresAt: time.Now().Add(socksUDPRelayTTL),
	}

	s.relayLock.Lock()
	if existing, ok := s.relays[key]; ok {
		// One association per client IP: a client that re-negotiates without
		// closing the old control connection replaces it, so sockets cannot
		// accumulate behind a buggy or abusive client.
		existing.conn.Close()
	}
	s.relays[key] = r
	s.relayLock.Unlock()

	// The reply advertises the address the client should send datagrams to: this
	// server's own UDP socket, not the relay socket.
	bnd, ok := s.udp.LocalAddr().(*net.UDPAddr)
	if !ok {
		s.closeRelay(key, r)
		return
	}
	if err := writeReply(client, socksRepSuccess, socksAddr{ip: bnd.IP, port: strconv.Itoa(bnd.Port)}); err != nil {
		utils.Debug("[Socks5] udp reply write failed: " + err.Error())
		s.closeRelay(key, r)
		return
	}
	utils.Debug("[Socks5] udp association " + key + " (relay " + relay.LocalAddr().String() + ")")

	// The response pump lives as long as the relay socket: closeRelay/Close()
	// closes it, which unblocks the pump's read.
	go s.relayResponses(r)

	// Block for the life of the association. RFC 1928 keeps the control
	// connection open until the client is done with UDP, and handleTCP closes it
	// only when this returns: returning early would tear the association down
	// before the first datagram arrives, which is exactly what a client sees as
	// "UDPAssociate answered, then every datagram vanished".
	//
	// The read is unbounded on purpose — this is the client's teardown signal,
	// not a handshake. Idle associations are bounded by the TTL sweep.
	io.Copy(io.Discard, client)
	s.closeRelay(key, r)
}

// closeRelay closes a relay only if it is still the current one for that key, so
// a teardown from a replaced association cannot close the replacement.
func (s *socksServer) closeRelay(key string, r *socksRelay) {
	s.relayLock.Lock()
	if current, ok := s.relays[key]; ok && current == r {
		r.conn.Close()
		delete(s.relays, key)
	}
	s.relayLock.Unlock()
}

// acceptUDP demuxes inbound datagrams to the association belonging to their
// sender's IP and forwards each one to the destination named in its own frame.
//
// Per-datagram destinations are the protocol: an association is a channel, not a
// pinned tunnel, and clients use one association for many destinations (a
// resolver talking to several upstream servers is the common case).
//
// A datagram whose sender IP has no association is dropped: an unauthenticated
// host cannot push traffic through a relay it did not negotiate.
func (s *socksServer) acceptUDP() {
	defer s.wg.Done()
	buf := make([]byte, socksMaxDatagram+1)

	for {
		// The socket is shared by every association, so the read deadline is set
		// per read; it exists to let the TTL sweep run while the relay is idle.
		s.udp.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.closeCh:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				s.expireRelays()
				continue
			}
			utils.Debug("[Socks5] udp read failed: " + err.Error())
			return
		}

		s.relayLock.Lock()
		r := s.relays[hostKey(from)]
		s.relayLock.Unlock()
		if r == nil {
			continue
		}

		// The sender of the most recent datagram is the client's current UDP
		// address: responses are sent back to it.
		r.setClient(from)

		s.forwardClientDatagram(r, from, buf[:n])
	}
}

// forwardClientDatagram decodes one client datagram and sends its payload to the
// destination carried in the frame, after applying the destination policy.
func (s *socksServer) forwardClientDatagram(r *socksRelay, from net.Addr, data []byte) {
	if len(data) > socksMaxDatagram {
		utils.Debug("[Socks5] udp datagram too large (" + strconv.Itoa(len(data)) + " bytes) from " + from.String())
		return
	}

	req, err := parseUDPFrame(data)
	if err != nil {
		utils.Debug("[Socks5] malformed udp frame from " + from.String() + ": " + err.Error())
		return
	}

	headerLen := socksUDPFrameHeader + socksFrameLen(req)
	if headerLen >= len(data) {
		return
	}
	payload := data[headerLen:]

	// RFC 1928: the E bit of the last byte of DATA means more fragments follow.
	// Dropped, not reassembled — see parseUDPFrame.
	if payload[len(payload)-1]&0x80 != 0 {
		utils.Debug("[Socks5] udp fragment from " + from.String() + " dropped (fragmented datagrams are not relayed)")
		return
	}

	if !s.policy.allowDestination(req.host, req.ip) {
		utils.Debug("[Socks5] udp destination " + req.String() + " refused by policy")
		return
	}
	addr, err := s.policy.resolveUDP(req.host, req.port, req.ip)
	if err != nil {
		utils.Debug("[Socks5] udp destination refused: " + err.Error())
		return
	}

	// Register the peer before writing: the echo of a fast destination can beat
	// the registration, and the pump would drop the first response as "unknown
	// peer". A peer entry left behind by a failed write is harmless — the
	// destination never received the datagram, so it has nothing to answer with.
	r.addPeer(addr.String())

	if _, err := r.conn.WriteToUDP(payload, addr); err != nil {
		utils.Debug("[Socks5] udp write to " + addr.String() + " failed: " + err.Error())
		return
	}
}

// relayResponses is the per-association response pump: it runs for the life of
// the association and forwards replies from known peers back to the client,
// re-framed with the address of the peer that sent them.
//
// It is a goroutine per association rather than a drain triggered by the next
// inbound datagram. Draining on inbound traffic means a reply from a slow
// upstream sits unforwarded until the client happens to send something else —
// invisible for a chatty client, fatal for a one-shot exchange like a DNS query.
//
// Replies from addresses this association never sent to are dropped, which is
// what stops the relay socket from being turned into a reflector.
func (s *socksServer) relayResponses(r *socksRelay) {
	buf := make([]byte, socksMaxDatagram+1)
	for {
		// Block until a reply arrives, the relay socket closes (Close() or
		// closeRelay), or the association goes idle past the TTL.
		r.conn.SetReadDeadline(time.Now().Add(socksUDPRelayTTL))
		n, from, err := r.conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if r.expired() {
					return
				}
				continue
			}
			return
		}
		if n > socksMaxDatagram {
			utils.Debug("[Socks5] udp response too large (" + strconv.Itoa(n) + " bytes) from " + from.String())
			continue
		}
		if !r.isPeer(from.String()) {
			utils.Debug("[Socks5] udp response from unknown peer " + from.String() + " dropped")
			continue
		}
		client := r.getClient()
		if client == nil {
			// No datagram has arrived from the client yet, so there is no
			// verified address to reply to. Dropping is the safe reading: a
			// relay must not send unsolicited traffic to an unverified address.
			continue
		}

		frame := socksFrameFromAddr(from)
		if len(frame) == 0 {
			return
		}
		out := make([]byte, 0, n+socksUDPFrameHeader+len(frame))
		out = append(out, 0x00, 0x00) // RSVD, FRAG=0
		out = append(out, frame...)
		out = append(out, buf[:n]...)

		if _, err := s.udp.WriteTo(out, client); err != nil {
			utils.Debug("[Socks5] udp write to client failed: " + err.Error())
			return
		}
	}
}

// expireRelays closes associations that have gone idle past the TTL.
func (s *socksServer) expireRelays() {
	s.relayLock.Lock()
	defer s.relayLock.Unlock()
	for key, r := range s.relays {
		if r.expired() {
			r.conn.Close()
			delete(s.relays, key)
			utils.Debug("[Socks5] udp association " + key + " expired (idle)")
		}
	}
}
