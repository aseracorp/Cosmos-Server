package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/azukaar/cosmos-server/src/utils"
)

// The only scheme a SOCKS relay route can be declared with.
//
// socks5 means the client resolves the name and hands Cosmos an address, and
// Cosmos dials exactly that. Remote resolution (socks5h / --socks5-hostname) is
// deliberately absent: it is a forward-proxy feature. Its purpose is to serve a
// client that has no DNS of its own -- an anonymizer, a tunnel, a device behind
// a captive network -- which is the opposite of what Cosmos does. Cosmos is a
// reverse proxy: routes expose listeners and forward inbound traffic to
// services, and every client that reaches one has already resolved the name.
// Accepting socks5h would make Cosmos a recursive resolver for outbound
// connections, publish a capability a reverse proxy has no reason to have, and
// give clients a way to reach hosts by name through a deployment whose policy
// model is addresses.
const (
	// Socks5Scheme: the client resolved the name and sends an address.
	Socks5Scheme = "socks5"
)

// IsSocksScheme reports whether a target scheme is served by the SOCKS relay
// rather than by the plain tcp/udp port forwarders.
func IsSocksScheme(scheme string) bool {
	return strings.ToLower(scheme) == Socks5Scheme
}

// IsValidSocksAuthMode is the configapi-side check for socks_auth_mode. Empty
// means "none"; anything else is a typo that would lock every client out.
func IsValidSocksAuthMode(mode string) bool {
	return mode == "" || strings.EqualFold(mode, "none") || strings.EqualFold(mode, "password")
}

// socksSchemeOf pulls the relay scheme out of a route target. A relay route is
// written as "socks5://" — the scheme is the whole target, because a relay has
// no upstream to name.
func socksSchemeOf(target string) string {
	parts := strings.Split(target, "://")
	if len(parts) == 1 {
		return ""
	}
	return strings.ToLower(parts[0])
}

// socksDestination is one entry of an allow/deny list: a CIDR or a bare IP.
//
// Names are not entries. Matching a destination against a hostname needs the
// relay to resolve it, and this relay does not resolve anything (see
// Socks5Scheme). A hostname entry would look like protection to the operator
// and match nothing at runtime -- the dangerous shape of a policy bug, so it is
// rejected at the API and at bind time rather than silently ignored.
type socksDestination struct {
	net *net.IPNet
	ip  net.IP
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

// parseSocksDestinations compiles a destination list. Anything that is not an
// address is an error rather than a silent no-match: a typo in a deny list is a
// hole, so failing loudly is the safe direction. Names, including "*.domain"
// wildcards, are rejected with a message that says why -- they cannot be
// enforced by a relay that does not resolve.
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

		if strings.Contains(spec, ".") || strings.Contains(spec, ":") {
			return nil, fmt.Errorf("invalid socks destination %q: hostnames are not supported, use an IP or a CIDR (this relay dials addresses, it does not resolve names)", spec)
		}
		return nil, fmt.Errorf("invalid socks destination %q (not an IP or a CIDR)", spec)
	}
	return out, nil
}

// socksPolicy is the compiled route policy behind one relay: authentication,
// destination filtering, and the resolution mode implied by the scheme.
//
// It is compiled once when the listener binds, because a route config is a
// policy document and not a per-request config: a malformed deny entry must
// fail the bind loudly rather than be quietly ignored when a client happens to
// hit it.
type socksPolicy struct {
	mode         string // one of Socks5Scheme / Socks5hScheme
	needPassword bool
	allowDest    []socksDestination
	denyDest     []socksDestination
}

func newSocksPolicy(mode, authMode string, allow, deny []string) (*socksPolicy, error) {
	p := &socksPolicy{mode: strings.ToLower(mode)}
	switch p.mode {
	case Socks5Scheme:
	default:
		return nil, fmt.Errorf("unsupported socks scheme %q (the only supported target scheme is socks5)", mode)
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
	return p, nil
}

// policyFromRoute compiles the policy for a SOCKS relay route.
func policyFromRoute(rtr *utils.ProxyRouteConfig) (*socksPolicy, error) {
	scheme := socksSchemeOf(rtr.Target)
	if scheme == "" {
		return nil, fmt.Errorf("socks route %s has no scheme in target %q", rtr.Name, rtr.Target)
	}
	return newSocksPolicy(scheme, rtr.SocksAuthMode, rtr.SocksAllowDestinations, rtr.SocksDenyDestinations)
}

// allowDestination decides whether a destination may be relayed.
//
// Order matters: explicit deny beats explicit allow; an empty allow list means
// "allow everything" (minus the deny list). The address the client sent is the
// whole decision -- there is no name to judge and nothing to re-check after a
// resolution, because this relay does not resolve.
func (p *socksPolicy) allowDestination(ip net.IP) bool {
	if ip == nil {
		// Fail closed: an address the policy cannot judge is not a destination
		// the relay may dial, even when the allow list is empty. "Allow
		// everything" means every address the client identified, not "dial
		// whatever turns up".
		return false
	}
	for _, d := range p.denyDest {
		if d.matchesIP(ip) {
			return false
		}
	}
	if len(p.allowDest) == 0 {
		return true
	}
	for _, d := range p.allowDest {
		if d.matchesIP(ip) {
			return true
		}
	}
	return false
}

// dialAddress validates the destination port and returns the address to dial.
//
// The address is the one the client put in its request, and it is dialled as
// given. There is no name to resolve: a socks5 request carries an address, and
// a request carrying a domain is refused in readRequest before it gets here.
// That is what keeps this relay a dialer rather than a resolver.
func (p *socksPolicy) dialAddress(port string, ip net.IP) (string, error) {
	if ip == nil {
		return "", fmt.Errorf("no destination address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid destination port %q", port)
	}
	return ip.String(), nil
}

// dialDestination applies the destination policy before dialing anything.
func (p *socksPolicy) dialDestination(port string, ip net.IP) (net.Conn, error) {
	if ip == nil {
		// Checked before the policy so the operator sees "no destination
		// address" rather than a filter refusal. allowDestination rejects a nil
		// address as well, so this is not the only line of defence.
		return nil, fmt.Errorf("no destination address")
	}
	if !p.allowDestination(ip) {
		return nil, fmt.Errorf("destination not allowed")
	}
	target, err := p.dialAddress(port, ip)
	if err != nil {
		return nil, err
	}
	return net.Dial("tcp", net.JoinHostPort(target, port))
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
//   - socks options on a route whose target is not a socks scheme: the operator
//     thinks they configured a relay and configured nothing.
//   - a relay with additional targets: pool members are dial targets for a
//     forwarder, and a relay's destinations come from client requests, so the
//     load balancer has nothing to choose between.
func ValidateSocksRoute(route utils.ProxyRouteConfig) string {
	// A target naming a SOCKS scheme we do not serve is refused here, not only
	// at bind time. Without this the route saves cleanly, startProxy logs
	// "Unsupported protocol" in a log nobody reads, and the operator is left
	// with a route that does nothing. socks5t is the usual case: the scheme
	// every SOCKS client rejects.
	if scheme := socksSchemeOf(route.Target); scheme != "" &&
		strings.HasPrefix(scheme, "socks") && !IsSocksScheme(scheme) {
		return "Route \"" + route.Name + "\" uses unsupported SOCKS scheme \"" + scheme +
			"\": the only supported target is socks5://"
	}

	// socks5h is refused by name, not folded into the generic scheme error: an
	// operator who wrote it means "resolve for me", and the useful answer is
	// that Cosmos does not do outbound resolution, not a list of schemes.
	if scheme := socksSchemeOf(route.Target); scheme == "socks5h" {
		return "Route \"" + route.Name + "\" uses socks5h, which asks Cosmos to resolve destination hostnames. Cosmos is a reverse proxy and does not resolve outbound names: use socks5://, where the client sends the address"
	}

	if !IsSocksScheme(socksSchemeOf(route.Target)) {
		if route.SocksAuthMode != "" || len(route.SocksAllowDestinations) > 0 || len(route.SocksDenyDestinations) > 0 {
			return "Route \"" + route.Name + "\" sets SOCKS relay options but its target is not socks5"
		}
		return ""
	}

	if !IsValidSocksAuthMode(route.SocksAuthMode) {
		return "Unsupported SOCKS auth mode \"" + route.SocksAuthMode + "\" on route \"" + route.Name +
			"\". Supported values are \"password\" (require a Cosmos account) and \"\"/\"none\" (anonymous)"
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
