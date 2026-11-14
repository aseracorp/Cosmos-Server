package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/azukaar/cosmos-server/src/utils"
)

// The schemes a SOCKS relay route can be declared with. They are not three
// implementations: they are the three things a SOCKS client is described as
// wanting, and each one decides who resolves the hostname and whether UDP is
// offered.
const (
	// Socks5Scheme: the client resolved the name and sends an address. Cosmos
	// dials exactly what it was given.
	Socks5Scheme = "socks5"
	// Socks5hScheme: the client sends a hostname and Cosmos resolves it, which
	// is what makes the relay useful to a client with no DNS of its own.
	Socks5hScheme = "socks5h"
	// Socks5tScheme: resolve locally (as socks5h) and offer UDP ASSOCIATE too,
	// so DNS / QUIC clients can use the relay.
	Socks5tScheme = "socks5t"
)

// IsSocksScheme reports whether a target scheme is served by the SOCKS relay
// rather than by the plain tcp/udp port forwarders.
func IsSocksScheme(scheme string) bool {
	s := strings.ToLower(scheme)
	return s == Socks5Scheme || s == Socks5hScheme || s == Socks5tScheme
}

// IsValidSocksAuthMode is the configapi-side check for socks_auth_mode. Empty
// means "none"; anything else is a typo that would lock every client out.
func IsValidSocksAuthMode(mode string) bool {
	return mode == "" || strings.EqualFold(mode, "none") || strings.EqualFold(mode, "password")
}

// socksSchemeOf pulls the relay scheme out of a route target. A relay route is
// written as "socks5://", "socks5h://" or "socks5t://" — the scheme is the whole
// target, because a relay has no upstream to name.
func socksSchemeOf(target string) string {
	parts := strings.Split(target, "://")
	if len(parts) == 1 {
		return ""
	}
	return strings.ToLower(parts[0])
}

// socksDestination is one entry of an allow/deny list: a CIDR, a bare IP, a
// hostname, or a "*.example.com" wildcard.
type socksDestination struct {
	net      *net.IPNet
	ip       net.IP
	host     string
	wildcard string // for "*.example.com": the suffix after the dot
}

func (d socksDestination) matchesIP(candidate net.IP) bool {
	if candidate == nil {
		return false
	}
	if d.net != nil {
		return d.net.Contains(candidate)
	}
	if d.ip != nil {
		return d.ip.Equal(candidate)
	}
	return false
}

func (d socksDestination) matchesHost(candidate string) bool {
	if candidate == "" {
		return false
	}
	c := strings.ToLower(candidate)
	if d.host != "" {
		return c == d.host
	}
	if d.wildcard != "" {
		// "*.example.com" matches example.com itself as well as any label
		// under it, which is what operators mean by a domain wildcard.
		return c == d.wildcard || strings.HasSuffix(c, "."+d.wildcard)
	}
	return false
}

// parseSocksDestinations compiles a destination list. An entry that is neither
// an address nor a plausible hostname is an error rather than a silent
// no-match: a typo in a deny list is a hole, so failing loudly is the safe
// direction.
func parseSocksDestinations(specs []string) ([]socksDestination, error) {
	out := make([]socksDestination, 0, len(specs))
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}

		if strings.Contains(spec, "/") {
			_, netw, err := net.ParseCIDR(spec)
			if err != nil {
				return nil, fmt.Errorf("invalid socks destination %q: %w", spec, err)
			}
			out = append(out, socksDestination{net: netw})
			continue
		}

		if ip := net.ParseIP(spec); ip != nil {
			out = append(out, socksDestination{ip: ip})
			continue
		}

		lower := strings.ToLower(spec)
		if strings.HasPrefix(lower, "*.") {
			suffix := lower[2:]
			if suffix == "" || !plausibleHostname(suffix) {
				return nil, fmt.Errorf("invalid socks destination %q", spec)
			}
			out = append(out, socksDestination{wildcard: suffix})
			continue
		}

		if !plausibleHostname(lower) {
			return nil, fmt.Errorf("invalid socks destination %q (not an address, host or *.domain)", spec)
		}
		out = append(out, socksDestination{host: lower})
	}
	return out, nil
}

func plausibleHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// socksPolicy is the compiled route policy behind one relay: authentication,
// destination filtering, and the dial/UDP mode implied by the scheme.
//
// It is compiled once when the listener binds, because a route config is a
// policy document and not a per-request config: a malformed deny entry must
// fail the bind loudly rather than be quietly ignored when a client happens to
// hit it.
type socksPolicy struct {
	mode         string // one of Socks5Scheme / Socks5hScheme / Socks5tScheme
	needPassword bool
	allowDest    []socksDestination
	denyDest     []socksDestination
	udpEnabled   bool
}

func newSocksPolicy(mode, authMode string, allow, deny []string, udpOverride *bool) (*socksPolicy, error) {
	p := &socksPolicy{mode: strings.ToLower(mode)}
	switch p.mode {
	case Socks5Scheme, Socks5hScheme, Socks5tScheme:
	default:
		return nil, fmt.Errorf("unsupported socks scheme %q (use socks5, socks5h, socks5t)", mode)
	}

	// "password" is the only auth mode that does anything; "none"/"" are
	// anonymous. Anything else is a typo that would silently disable
	// authentication, so it is refused at bind time.
	p.needPassword = strings.EqualFold(authMode, "password")

	var err error
	if p.allowDest, err = parseSocksDestinations(allow); err != nil {
		return nil, fmt.Errorf("socks_allow_destinations: %w", err)
	}
	if p.denyDest, err = parseSocksDestinations(deny); err != nil {
		return nil, fmt.Errorf("socks_deny_destinations: %w", err)
	}

	// UDP ASSOCIATE is a capability of the scheme (socks5t offers it); socks_udp
	// lets a route turn it off, or turn it on for a scheme that does not offer
	// it by default.
	p.udpEnabled = p.mode == Socks5tScheme
	if udpOverride != nil {
		p.udpEnabled = *udpOverride
	}
	return p, nil
}

// policyFromRoute compiles the policy for a SOCKS relay route.
func policyFromRoute(rtr *utils.ProxyRouteConfig) (*socksPolicy, error) {
	var udpOverride *bool
	if rtr.SocksUDP != "" {
		v := strings.EqualFold(rtr.SocksUDP, "true")
		udpOverride = &v
	}
	scheme := socksSchemeOf(rtr.Target)
	if scheme == "" {
		return nil, fmt.Errorf("socks route %s has no scheme in target %q", rtr.Name, rtr.Target)
	}
	return newSocksPolicy(scheme, rtr.SocksAuthMode, rtr.SocksAllowDestinations, rtr.SocksDenyDestinations, udpOverride)
}

// allowDestination decides whether a destination may be relayed.
//
// Order matters: explicit deny beats explicit allow; a hostname is judged on
// the name alone (no DNS here, so policy cannot be bypassed by a name that
// resolves to a filtered address); an IP is judged on the address; a hostname
// that is not an address is allowed when the allow list is empty, i.e. "allow
// everything".
func (p *socksPolicy) allowDestination(host string, ip net.IP) bool {
	for _, d := range p.denyDest {
		if d.matchesHost(host) || d.matchesIP(ip) {
			return false
		}
	}
	if len(p.allowDest) == 0 {
		return true
	}
	for _, d := range p.allowDest {
		if d.matchesHost(host) || d.matchesIP(ip) {
			return true
		}
	}
	return false
}

// dialTarget resolves a destination into a host to dial. Numeric addresses dial
// directly; names go through the box's resolver, which is the whole point of
// socks5h/socks5t.
func (p *socksPolicy) dialTarget(host string, port string, ip net.IP) (string, error) {
	if ip != nil {
		return ip.String(), nil
	}
	if host == "" {
		return "", fmt.Errorf("no destination")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid destination port %q", port)
	}
	if p.mode == Socks5Scheme {
		// socks5 means the client resolved already; a name in a socks5 request
		// is a client bug, and resolving it is the forgiving reading of the
		// protocol (curl and Chrome effectively do this).
		return host, nil
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("resolve %s: no addresses", host)
	}
	// The policy was checked against the name; check the resolved address too,
	// so "deny 127.0.0.0/8" cannot be bypassed by a name pointing at loopback.
	for _, s := range ips {
		if !p.allowDestination("", net.ParseIP(s)) {
			return "", fmt.Errorf("destination not allowed (resolved %s -> %s)", host, s)
		}
	}
	return ips[0], nil
}

// resolveAndDial applies the destination policy before dialing anything.
func (p *socksPolicy) resolveAndDial(host string, port string, ip net.IP) (net.Conn, error) {
	if !p.allowDestination(host, ip) {
		return nil, fmt.Errorf("destination not allowed")
	}
	target, err := p.dialTarget(host, port, ip)
	if err != nil {
		return nil, err
	}
	return net.Dial("tcp", net.JoinHostPort(target, port))
}

// resolveUDP applies the same policy as resolveAndDial and returns the address
// to send datagrams to, for UDP ASSOCIATE.
func (p *socksPolicy) resolveUDP(host string, port string, ip net.IP) (*net.UDPAddr, error) {
	if !p.allowDestination(host, ip) {
		return nil, fmt.Errorf("destination not allowed")
	}
	target, err := p.dialTarget(host, port, ip)
	if err != nil {
		return nil, err
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(target, port))
}

// checkCredentials verifies a client against Cosmos's own user store.
//
// This is deliberately the same primitive the dashboard login uses
// (utils.CheckPassword: bcrypt against the stored hash, with a dummy compare
// for unknown users so a failed login does not leak which nicknames exist).
// The relay has no per-route credential field, so there is no second auth
// system with semantics that differ from the rest of the proxy: a SOCKS client
// logs in with the same account as a dashboard user.
//
// A route that asks for password auth requires a real user account. There is no
// fallback to anonymous — an operator typo must not turn an authenticated relay
// into an open one.
// socksCheckPassword is the credential check, held as a variable so tests can
// drive authentication without a user store. Same pattern as netDial and
// tcpShieldMiddleware in SocketProxy.go.
var socksCheckPassword = utils.CheckPassword

func (p *socksPolicy) checkCredentials(user, password string) error {
	if !p.needPassword {
		return nil
	}
	if err := socksCheckPassword(user, password); err != nil {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}

// ValidateSocksRoute is the configapi-side check for a SOCKS relay route.
// Returns "" for a valid route (including non-socks routes, which have nothing
// to check) and an operator-readable message otherwise.
//
// Validation belongs at the API boundary and not only at bind time: a route
// saved with a typo in a deny list would otherwise bind successfully and then
// refuse every destination it was meant to allow, which reads to the operator as
// "the relay is broken" rather than "my config is wrong".
//
// Two combinations are rejected outright:
//   - socks_udp with a value that is not true/false/"": read as a plain bool,
//     "0" silently becomes false and "no" becomes true, so an operator gets UDP
//     behaviour they did not ask for.
//   - a relay with additional targets: pool members are dial targets for a
//     forwarder, and a relay's destinations come from client requests, so the
//     load balancer has nothing to choose between.
func ValidateSocksRoute(route utils.ProxyRouteConfig) string {
	if !IsSocksScheme(socksSchemeOf(route.Target)) {
		if route.SocksAuthMode != "" || route.SocksUDP != "" || len(route.SocksAllowDestinations) > 0 || len(route.SocksDenyDestinations) > 0 {
			return "Route \"" + route.Name + "\" sets SOCKS relay options but its target is not socks5/socks5h/socks5t"
		}
		return ""
	}

	if !IsValidSocksAuthMode(route.SocksAuthMode) {
		return "Unsupported SOCKS auth mode \"" + route.SocksAuthMode + "\" on route \"" + route.Name +
			"\". Supported values are \"password\" (require a Cosmos account) and \"\"/\"none\" (anonymous)"
	}

	switch strings.ToLower(route.SocksUDP) {
	case "", "true", "false":
	default:
		return "Unsupported SOCKS udp value \"" + route.SocksUDP + "\" on route \"" + route.Name +
			"\". Supported values are \"true\", \"false\" and \"\" (empty: use the scheme's default)"
	}

	if len(route.AdditionalTargets) > 0 {
		return "Route \"" + route.Name + "\" is a SOCKS relay and cannot have additional targets: a relay's destinations come from client requests, so there is nothing to load balance"
	}

	if _, err := parseSocksDestinations(route.SocksAllowDestinations); err != nil {
		return "Route \"" + route.Name + "\": socks_allow_destinations " + err.Error()
	}
	if _, err := parseSocksDestinations(route.SocksDenyDestinations); err != nil {
		return "Route \"" + route.Name + "\": socks_deny_destinations " + err.Error()
	}
	return ""
}
