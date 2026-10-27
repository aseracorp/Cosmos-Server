package proxy

import (
	"net"
	"net/http"
	"strings"

	"github.com/azukaar/cosmos-server/src/utils"
)

// Indexing policies. Tri-state: a route — or the node-wide default — can
// allow indexing, block it, or inherit. The empty value is "inherit", so an
// existing config, which carries only the legacy HTTPConfig.AllowSearchEngine
// boolean, keeps working untouched.
const (
	RobotsInherit = ""
	RobotsAllow   = "allow"
	RobotsBlock   = "block"
)

// robotsTag is what makes "block" real. robots.txt is a suggestion a crawler
// may ignore, and a Disallow in robots.txt does not stop a page linked from
// elsewhere from being indexed; X-Robots-Tag is a response header, and the
// crawlers that matter (Google, Bing) honour it per URL.
const robotsTag = "noindex, nofollow, noimageindex"

// robotsPath is the path a crawler reads a site's indexing policy from.
// Cosmos answers it at the host root only, never under a route's path prefix:
// an app behind a prefix route (Immich, Nextcloud, ...) has its own
// /prefix/robots.txt and Cosmos must not put a file in front of it.
const robotsPath = "/robots.txt"

// NormalizeRobotsPolicy maps user input onto one of the three policies.
// Unknown values collapse to RobotsInherit, so a typo can never silently
// widen the exposure of a route the operator meant to block.
func NormalizeRobotsPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RobotsAllow:
		return RobotsAllow
	case RobotsBlock:
		return RobotsBlock
	default:
		return RobotsInherit
	}
}

// IsValidRobotsPolicy reports whether a user-supplied value names one of the
// three policies. Empty (inherit) is valid; a typo is not, so a value that
// would silently collapse to "inherit" is rejected where it is written rather
// than quietly dropped.
func IsValidRobotsPolicy(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	return NormalizeRobotsPolicy(value) != RobotsInherit
}

// RobotsAllowed derives the answer from the tri-state policy, falling back to
// the legacy AllowSearchEngine flag when no policy is set.
func RobotsAllowed(policy string, legacyAllowSearchEngine bool) bool {
	switch NormalizeRobotsPolicy(policy) {
	case RobotsAllow:
		return true
	case RobotsBlock:
		return false
	default:
		return legacyAllowSearchEngine
	}
}

// NodeRobotsAllowed is the node-wide indexing default, read live from the main
// config so a settings change is picked up without a restart.
func NodeRobotsAllowed() bool {
	cfg := utils.GetMainConfig()
	return RobotsAllowed(cfg.HTTPConfig.RobotsPolicy, cfg.HTTPConfig.AllowSearchEngine)
}

// IsHTTPRoute reports whether a route is served over the HTTP(S) router on a
// hostname a crawler can fetch /robots.txt from — the same set of routes
// BuildFromConfig registers. A route forwarding a raw TCP/UDP port is reached
// on that port, not on a hostname, so an indexing policy has nothing to say
// there and the UI hides the setting for it.
func IsHTTPRoute(route utils.ProxyRouteConfig) bool {
	if route.Mode == "STATIC" || route.Mode == "SPA" {
		return true
	}
	if route.Target == "" {
		return false
	}
	return strings.HasPrefix(route.Target, "http://") || strings.HasPrefix(route.Target, "https://")
}

// RouteRobotsBlocked resolves a route's policy against the node-wide default.
// The second return value says whether the route declares a policy at all: a
// route that inherits states nothing and is left byte-for-byte as it is today.
// That is the upgrade-safety guarantee — node-wide indexing has always been
// blocked by default, so emitting X-Robots-Tag for inheriting routes would
// silently tell search engines to drop pages that are indexed now.
func RouteRobotsBlocked(route utils.ProxyRouteConfig) (blocked bool, declares bool) {
	switch NormalizeRobotsPolicy(route.RobotsPolicy) {
	case RobotsAllow:
		return false, true
	case RobotsBlock:
		return true, true
	default:
		return !NodeRobotsAllowed(), false
	}
}

// writeRobots serves the Disallow file. Only the blocking case ever writes a
// robots.txt: when indexing is allowed Cosmos says nothing, which keeps
// today's behaviour (the request falls through, to the app behind the route or
// to a 404) and stops Cosmos from advertising a file it has no opinion about.
func writeRobots(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// HostnameOnly strips the port from a Host header value, keeping bracketed
// and bare IPv6 addresses intact.
func HostnameOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// hostMatchesHostname mirrors what the proxy router accepts for a route's
// host: the exact name, and the ":port" / "0.0.0.0:port" wildcards that match
// any hostname on that port.
func hostMatchesHostname(pattern, hostname string) bool {
	if pattern == "" {
		return false
	}
	name, port, hasPort := strings.Cut(pattern, ":")
	if hasPort && (name == "" || name == "0.0.0.0") {
		return port != ""
	}
	return name == hostname
}

// RobotsAllowedForHost answers: may search engines index this hostname?
//
// A hostname publishes one robots.txt, so every route that serves it has to
// agree. The rule is the strictest declared policy wins: an intranet app
// pinned to "allow" opens that hostname, and the moment any route serving the
// same hostname asks to be hidden, the file hides it — a route can never be
// exposed by a neighbour's looser setting. Fine-grained per-URL control stays
// with X-Robots-Tag, which Google honours per URL.
//
// The second return value says whether any route declared a policy for this
// host; when none did, the caller falls back to the node-wide setting.
func RobotsAllowedForHost(config utils.Config, host string) (allow bool, declared bool) {
	hostname := HostnameOnly(host)
	cosmosHostname := HostnameOnly(config.HTTPConfig.Hostname)

	allow = true
	for _, route := range config.HTTPConfig.ProxyConfig.Routes {
		if route.Disabled || !IsHTTPRoute(route) {
			continue
		}
		policy := NormalizeRobotsPolicy(route.RobotsPolicy)
		if policy == RobotsInherit {
			continue
		}
		servesHost := (route.UseHost && hostMatchesHostname(route.Host, hostname)) ||
			(!route.UseHost && cosmosHostname != "" && hostname == cosmosHostname)
		if !servesHost {
			continue
		}
		if policy == RobotsBlock {
			allow = false
		}
		declared = true
	}
	return allow, declared
}

// RobotsMiddleware applies a route's indexing policy to its responses:
// X-Robots-Tag on everything the route serves.
//
// It is registered in the route's chain ahead of the auth gate, the bot
// filter, the throttles and SmartShield, so the header lands on the app's
// answers and on Cosmos's own 403 / 429 / login-redirect responses alike — the
// login page of an app is exactly the page that must never end up in an index.
func RobotsMiddleware(route utils.ProxyRouteConfig) func(http.Handler) http.Handler {
	blocked, declares := RouteRobotsBlocked(route)
	tag := ""
	if declares && blocked {
		tag = robotsTag
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tag != "" {
				w.Header().Set("X-Robots-Tag", tag)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RobotsTxtMiddleware answers /robots.txt for the hostname a request arrived
// on. It runs as global middleware, before route matching, because a hostname
// publishes a single robots.txt while the routes sharing it are scattered
// across the proxy router, the Cosmos UI and the API.
//
// Serving Disallow when indexing is blocked is what a crawler acts on. Falling
// through when indexing is allowed keeps today's behaviour (the request
// reaches the app behind the route, or 404s) and keeps Cosmos from publishing
// a file it has no opinion about.
func RobotsTxtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != robotsPath {
			next.ServeHTTP(w, r)
			return
		}
		if allow, declared := RobotsAllowedForHost(utils.GetMainConfig(), r.Host); declared {
			if allow {
				next.ServeHTTP(w, r)
				return
			}
			writeRobots(w)
			return
		}
		if NodeRobotsAllowed() {
			next.ServeHTTP(w, r)
			return
		}
		writeRobots(w)
	})
}

// cosmosSurface is Cosmos's own HTTP surface — the admin UI, the API, the
// login page and the OAuth/OIDC endpoints. None of it is content, all of it
// is machinery, and a login page in Google's index is the usual outcome of
// turning on "allow indexing" on a home server.
//
// Prefix-routed apps live under the Cosmos hostname too (https://cosmos/x/...),
// and none of these prefixes collide with a normal app path, so this list is
// what separates "Cosmos" from "the app behind it" for indexing purposes.
var cosmosSurface = []string{
	"/cosmos-ui",
	"/cosmos/",
	"/oauth2",
	"/.well-known",
	"/logo",
}

// NoIndexCosmosSurface keeps Cosmos's own surface out of search engines,
// whatever the routes' indexing policies say: an admin UI reachable by
// crawlers is a security problem, not a publishing choice.
func NoIndexCosmosSurface(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range cosmosSurface {
			if strings.HasPrefix(r.URL.Path, prefix) {
				w.Header().Set("X-Robots-Tag", robotsTag)
				break
			}
		}
		next.ServeHTTP(w, r)
	})
}

// IsCosmosSurfacePath reports whether a path belongs to Cosmos's own surface
// (admin UI, API, login, OAuth/OIDC) rather than to an app behind a route.
func IsCosmosSurfacePath(path string) bool {
	for _, prefix := range cosmosSurface {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// NoIndexSurfaceUnmatched stamps Cosmos surface paths that matched no route at
// all (a 404 on /oauth2/authorize with no params, a 405 on the token
// endpoint...).
//
// gorilla mux runs the NotFound/MethodNotAllowed handlers WITHOUT the
// router.Use chain, so the surface guarantee leaks on exactly the malformed
// crawler hits it exists to cover. Wrapping the fallback handlers closes that
// gap. App paths are left alone: an app's own 404 page is the app's business.
func NoIndexSurfaceUnmatched(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsCosmosSurfacePath(r.URL.Path) {
			w.Header().Set("X-Robots-Tag", robotsTag)
		}
		next.ServeHTTP(w, r)
	})
}
