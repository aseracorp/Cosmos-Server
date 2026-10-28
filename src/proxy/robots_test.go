package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/azukaar/cosmos-server/src/utils"
)

// setIndexingConfig pins the node-wide indexing setting and the routes for one
// test. The node-wide setting is given as a tri-state policy plus the legacy
// boolean it falls back to.
func setIndexingConfig(nodePolicy string, legacyAllow bool, hostname string, routes ...utils.ProxyRouteConfig) {
	cfg := utils.Config{}
	cfg.HTTPConfig.RobotsPolicy = nodePolicy
	cfg.HTTPConfig.AllowSearchEngine = legacyAllow
	cfg.HTTPConfig.Hostname = hostname
	cfg.HTTPConfig.ProxyConfig.Routes = routes
	utils.LoadBaseMainConfig(cfg)
}

// app answers as the app behind a route and records the path it was reached
// with, which is how the tests assert nothing was rewritten.
type appSpy struct{ path string }

func (a *appSpy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("app"))
	})
}

// doRoute runs one request through a route's robots middleware.
func doRoute(middleware func(http.Handler) http.Handler, target string) (header http.Header, body, forwarded string) {
	app := &appSpy{}
	rec := httptest.NewRecorder()
	middleware(app.handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	res := rec.Result()
	raw, _ := io.ReadAll(res.Body)
	return res.Header, string(raw), app.path
}

// doRobotsTxt runs a /robots.txt request through the global middleware. The
// next handler stands in for "Cosmos has no opinion": whatever it writes is
// what the crawler sees (the app behind the route, or a 404).
func doRobotsTxt(t *testing.T, host string) (fellThrough bool, body string, header http.Header) {
	t.Helper()
	app := &appSpy{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://"+host+robotsPath, nil)
	req.Host = host
	RobotsTxtMiddleware(app.handler()).ServeHTTP(rec, req)
	res := rec.Result()
	raw, _ := io.ReadAll(res.Body)
	return app.path == robotsPath, string(raw), res.Header
}

func TestNormalizeRobotsPolicy(t *testing.T) {
	cases := map[string]string{
		"":             RobotsInherit,
		"inherit":      RobotsInherit,
		"allow":        RobotsAllow,
		"BLOCK":        RobotsBlock,
		"  Block  ":    RobotsBlock,
		"noindex":      RobotsInherit,
		"yes":          RobotsInherit,
		"allow, block": RobotsInherit,
	}
	for in, want := range cases {
		if got := NormalizeRobotsPolicy(in); got != want {
			t.Errorf("NormalizeRobotsPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsValidRobotsPolicy(t *testing.T) {
	for _, ok := range []string{"", "allow", "block", "ALLOW", " Block "} {
		if !IsValidRobotsPolicy(ok) {
			t.Errorf("IsValidRobotsPolicy(%q) = false, want true", ok)
		}
	}
	// A typo must be rejected: collapsing it to "inherit" would silently hand a
	// route the operator meant to block back to the node-wide default.
	for _, bad := range []string{"noindex", "yes", "no", "allow, block", "bolck"} {
		if IsValidRobotsPolicy(bad) {
			t.Errorf("IsValidRobotsPolicy(%q) = true, want false", bad)
		}
	}
}

func TestRobotsAllowedFallsBackToLegacyFlag(t *testing.T) {
	for _, tc := range []struct {
		policy string
		legacy bool
		want   bool
	}{
		{RobotsAllow, false, true},
		{RobotsAllow, true, true},
		{RobotsBlock, true, false},
		{RobotsBlock, false, false},
		{RobotsInherit, true, true},
		{RobotsInherit, false, false},
		{"bogus", true, true},
	} {
		if got := RobotsAllowed(tc.policy, tc.legacy); got != tc.want {
			t.Errorf("RobotsAllowed(%q, legacy=%v) = %v, want %v", tc.policy, tc.legacy, got, tc.want)
		}
	}
}

// An existing configuration carries only the legacy boolean. Nothing may
// change for it until a policy is set explicitly.
func TestLegacyConfigKeepsItsBehaviour(t *testing.T) {
	setIndexingConfig(RobotsInherit, false, "cosmos.example.com")
	if NodeRobotsAllowed() {
		t.Error("legacy AllowSearchEngine=false must keep indexing blocked")
	}
	setIndexingConfig(RobotsInherit, true, "cosmos.example.com")
	if !NodeRobotsAllowed() {
		t.Error("legacy AllowSearchEngine=true must keep indexing allowed")
	}
}

// The scenario behind this feature: an intranet app on a node that blocks
// indexing. The route pins "allow", so its hostname stops publishing a
// Disallow file.
func TestRouteAllowOverridesNodeWideBlock(t *testing.T) {
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "blog", UseHost: true, Host: "blog.example.com",
			Mode: "SERVAPP", Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsAllow})

	fell, body, _ := doRobotsTxt(t, "blog.example.com")
	if !fell {
		t.Errorf("a route pinned to allow must not publish a Disallow file, got %q", body)
	}
	if strings.Contains(body, "Disallow") {
		t.Errorf("body = %q, want the request to reach the app", body)
	}
}

// The inverse: indexing allowed node-wide, one route wants to stay out.
func TestRouteBlockOverridesNodeWideAllow(t *testing.T) {
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "admin", UseHost: true, Host: "admin.example.com",
			Mode: "SERVAPP", Target: "http://127.0.0.1:9000", RobotsPolicy: RobotsBlock})

	fell, body, header := doRobotsTxt(t, "admin.example.com")
	if fell {
		t.Error("a route pinned to block must publish a robots.txt itself")
	}
	if !strings.Contains(body, "User-agent: *") || !strings.Contains(body, "Disallow: /") {
		t.Errorf("body = %q, want a Disallow: / robots.txt", body)
	}
	if !strings.HasPrefix(header.Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", header.Get("Content-Type"))
	}

	// A hostname no route claims keeps the node-wide answer.
	if fell, _, _ := doRobotsTxt(t, "cosmos.example.com"); !fell {
		t.Error("node-wide allow must leave other hostnames indexable")
	}
}

// A hostname publishes one robots.txt. Two routes on it may not outvote each
// other: the strictest policy wins, so a route can never be exposed by a
// neighbour's looser setting.
func TestStrictestPolicyWinsOnASharedHost(t *testing.T) {
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "app", UseHost: true, Host: "app.example.com",
			Mode: "SERVAPP", Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsAllow},
		utils.ProxyRouteConfig{Name: "app-admin", UseHost: true, Host: "app.example.com",
			UsePathPrefix: true, PathPrefix: "/admin", Mode: "SERVAPP",
			Target: "http://127.0.0.1:9000", RobotsPolicy: RobotsBlock})

	if fell, body, _ := doRobotsTxt(t, "app.example.com"); fell {
		t.Errorf("a blocking route on the host must win, request reached the app: %q", body)
	}

	// Flipping the prefix route to allow opens the host again.
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "app", UseHost: true, Host: "app.example.com",
			Mode: "SERVAPP", Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsAllow},
		utils.ProxyRouteConfig{Name: "app-admin", UseHost: true, Host: "app.example.com",
			UsePathPrefix: true, PathPrefix: "/admin", Mode: "SERVAPP",
			Target: "http://127.0.0.1:9000", RobotsPolicy: RobotsAllow})
	if fell, _, _ := doRobotsTxt(t, "app.example.com"); !fell {
		t.Error("two agreeing allow routes must leave the host indexable")
	}
}

// A route with no hostname is served under the Cosmos domain and shares that
// domain's robots.txt.
func TestPrefixRoutesOnTheCosmosDomainPublishTheirPolicy(t *testing.T) {
	route := utils.ProxyRouteConfig{Name: "web", Mode: "SERVAPP",
		Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsAllow}

	// Indexing blocked node-wide, one prefix route asks to be indexed.
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com", route)
	if fell, _, _ := doRobotsTxt(t, "cosmos.example.com"); !fell {
		t.Error("an allowed prefix route must open the Cosmos domain's robots.txt")
	}

	// Indexing allowed node-wide, one prefix route asks to be hidden.
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "web", Mode: "SERVAPP",
			Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsBlock})
	if fell, body, _ := doRobotsTxt(t, "cosmos.example.com"); fell || !strings.Contains(body, "Disallow: /") {
		t.Errorf("a blocked prefix route must publish Disallow, got fell=%v body=%q", fell, body)
	}

	// A different hostname is not governed by a prefix route.
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com", route)
	if fell, body, _ := doRobotsTxt(t, "elsewhere.example.com"); fell {
		t.Errorf("an unrelated host must keep the node-wide answer, got %q", body)
	}
}

// A disabled route has no router entry at all, so its policy must not leak
// into the robots.txt of its hostname.
func TestDisabledRoutesAreIgnored(t *testing.T) {
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "blog", Disabled: true, UseHost: true, Host: "blog.example.com",
			Mode: "SERVAPP", Target: "http://127.0.0.1:8080", RobotsPolicy: RobotsAllow})

	if fell, _, _ := doRobotsTxt(t, "blog.example.com"); fell {
		t.Error("a disabled route must not open its hostname")
	}
}

// A hostname with no declared policy keeps the node-wide answer, both ways.
func TestUndeclaredHostFollowsNodeWideSetting(t *testing.T) {
	setIndexingConfig(RobotsInherit, false, "cosmos.example.com")
	if fell, body, _ := doRobotsTxt(t, "app.example.com"); fell || !strings.Contains(body, "Disallow: /") {
		t.Errorf("node-wide block must publish Disallow, got fell=%v body=%q", fell, body)
	}

	setIndexingConfig(RobotsInherit, true, "cosmos.example.com")
	if fell, _, _ := doRobotsTxt(t, "app.example.com"); !fell {
		t.Error("legacy AllowSearchEngine=true must leave robots.txt to the app")
	}
}

// The host-wildcard routes accept any hostname on a port; a policy on one
// applies to hostnames reached on that port.
func TestWildcardHostRoutes(t *testing.T) {
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "any", UseHost: true, Host: "0.0.0.0:443",
			Mode: "PROXY", Target: "https://127.0.0.1:8443", RobotsPolicy: RobotsAllow})

	// The wildcard route serves every hostname on its port, so its policy is
	// the policy of all of them.
	if fell, _, _ := doRobotsTxt(t, "anything.example.com"); !fell {
		t.Error("a wildcard host route pinned to allow must open the hostnames it serves")
	}
	if fell, _, _ := doRobotsTxt(t, "cosmos.example.com"); !fell {
		t.Error("a 0.0.0.0:443 route also serves the Cosmos hostname on 443")
	}

	// A wildcard pinned to block closes them all, overriding node-wide allow.
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com",
		utils.ProxyRouteConfig{Name: "any", UseHost: true, Host: ":443",
			Mode: "PROXY", Target: "https://127.0.0.1:8443", RobotsPolicy: RobotsBlock})
	if fell, body, _ := doRobotsTxt(t, "anything.example.com"); fell || !strings.Contains(body, "Disallow: /") {
		t.Errorf("a wildcard route pinned to block must publish Disallow, got fell=%v body=%q", fell, body)
	}
}

func TestHostnameOnly(t *testing.T) {
	for in, want := range map[string]string{
		"app.example.com":      "app.example.com",
		"app.example.com:8443": "app.example.com",
		"":                     "",
		"::1":                  "::1",
		"app.example.com:80":   "app.example.com",
		"[::1]:8443":           "::1",
	} {
		if got := HostnameOnly(in); got != want {
			t.Errorf("HostnameOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostMatchesHostname(t *testing.T) {
	cases := []struct {
		pattern, hostname string
		want              bool
	}{
		{"app.example.com", "app.example.com", true},
		{"app.example.com", "other.example.com", false},
		{"app.example.com", "sub.app.example.com", false},
		{":443", "anything.example.com", true},
		{"0.0.0.0:443", "anything.example.com", true},
		{"", "app.example.com", false},
	}
	for _, c := range cases {
		if got := hostMatchesHostname(c.pattern, c.hostname); got != c.want {
			t.Errorf("hostMatchesHostname(%q, %q) = %v, want %v", c.pattern, c.hostname, got, c.want)
		}
	}
}

// A route pinned to "block" has to say so on every response: a Disallow in
// robots.txt does not stop a page linked from elsewhere from being indexed,
// and a crawler that ignores robots.txt honours the header.
func TestRobotsMiddlewareTagsBlockedRoute(t *testing.T) {
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com")
	h := RobotsMiddleware(utils.ProxyRouteConfig{Name: "r", RobotsPolicy: RobotsBlock})

	_, body, forwarded := doRoute(h, "/deep/path")
	if body != "app" {
		t.Errorf("body = %q, want the app answer", body)
	}
	if forwarded != "/deep/path" {
		t.Errorf("forwarded URL = %q, want it passed through untouched", forwarded)
	}
}

func TestRobotsMiddlewareXRobotsTagValues(t *testing.T) {
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com")

	blocked := RobotsMiddleware(utils.ProxyRouteConfig{Name: "r", RobotsPolicy: RobotsBlock})
	header, _, _ := doRoute(blocked, "/")
	if tag := header.Get("X-Robots-Tag"); !strings.Contains(tag, "noindex") || !strings.Contains(tag, "nofollow") {
		t.Errorf("blocked route X-Robots-Tag = %q, want noindex and nofollow", tag)
	}

	allowed := RobotsMiddleware(utils.ProxyRouteConfig{Name: "r", RobotsPolicy: RobotsAllow})
	header, _, _ = doRoute(allowed, "/")
	if tag := header.Get("X-Robots-Tag"); tag != "" {
		t.Errorf("allowed route X-Robots-Tag = %q, want unset", tag)
	}
}

// The upgrade-safety guarantee: a route that declares nothing is untouched
// whatever the node-wide setting is. Node-wide indexing has always defaulted
// to blocked, so tagging inheriting routes would tell search engines to drop
// pages that are indexed today.
func TestInheritingRouteIsTransparent(t *testing.T) {
	for _, policy := range []string{RobotsBlock, RobotsAllow} {
		setIndexingConfig(policy, policy == RobotsAllow, "cosmos.example.com")
		header, body, forwarded := doRoute(RobotsMiddleware(utils.ProxyRouteConfig{Name: "r"}), "/some/path")
		if body != "app" {
			t.Errorf("node policy %q: body = %q, want the app answer", policy, body)
		}
		if forwarded != "/some/path" {
			t.Errorf("node policy %q: forwarded URL = %q, want it untouched", policy, forwarded)
		}
		if tag := header.Get("X-Robots-Tag"); tag != "" {
			t.Errorf("node policy %q: inheriting route set X-Robots-Tag = %q, want unset", policy, tag)
		}
	}
}

// A route forwarding a raw port is reached on that port, not through a
// hostname a crawler fetches /robots.txt from, so the policy has no surface
// there.
func TestSocketRoutesAreNotHTTPRoutes(t *testing.T) {
	for _, r := range []utils.ProxyRouteConfig{
		{Name: "tcp", Mode: "SERVAPP", Target: "127.0.0.1:3128"},
		{Name: "proxy-tcp", Mode: "PROXY", Target: "tcp://127.0.0.1:3128"},
		{Name: "empty", Mode: "SERVAPP", Target: ""},
	} {
		if IsHTTPRoute(r) {
			t.Errorf("%s: mode=%s target=%s, want not an HTTP route", r.Name, r.Mode, r.Target)
		}
	}
	for _, r := range []utils.ProxyRouteConfig{
		{Name: "http", Mode: "SERVAPP", Target: "http://127.0.0.1:3128"},
		{Name: "https", Mode: "PROXY", Target: "https://example.com"},
		{Name: "redirect", Mode: "REDIRECT", Target: "https://example.com"},
		{Name: "static", Mode: "STATIC", Target: "/srv/www"},
		{Name: "spa", Mode: "SPA", Target: "/srv/www"},
	} {
		if !IsHTTPRoute(r) {
			t.Errorf("%s: mode=%s target=%s, want an HTTP route", r.Name, r.Mode, r.Target)
		}
	}
}

// Cosmos's own surface stays out of the index whatever the routes say: an
// exposed admin UI is a security problem, not a publishing choice.
func TestNoIndexCosmosSurfaceCoversCosmosSurface(t *testing.T) {
	setIndexingConfig(RobotsAllow, true, "cosmos.example.com")
	rec := httptest.NewRecorder()
	NoIndexCosmosSurface((&appSpy{}).handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://cosmos.example.com/cosmos/api/config", nil))
	if tag := rec.Result().Header.Get("X-Robots-Tag"); !strings.Contains(tag, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex on the Cosmos API", tag)
	}
}

// robots.txt is answered for the bare path only: an app behind a prefix route
// has its own robots.txt and Cosmos must not put a file in front of it.
func TestRobotsTxtMiddlewareLeavesOtherPathsAlone(t *testing.T) {
	setIndexingConfig(RobotsBlock, false, "cosmos.example.com")
	app := &appSpy{}
	rec := httptest.NewRecorder()
	RobotsTxtMiddleware(app.handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://app.example.com/app/robots.txt", nil))
	if app.path != "/app/robots.txt" {
		t.Errorf("nested robots.txt was intercepted: forwarded %q", app.path)
	}
	if strings.Contains(rec.Body.String(), "Disallow") {
		t.Errorf("nested robots.txt got Cosmos's file: %q", rec.Body.String())
	}
}

// A 404 on a Cosmos surface path is produced by gorilla's NotFoundHandler,
// which runs WITHOUT the router.Use chain — so the surface guarantee has to be
// applied to the fallback handlers explicitly. These paths are exactly what a
// crawler probing an "indexable" Cosmos hostname will hit.
func TestNoIndexSurfaceUnmatchedCoversUnmatchedSurfacePaths(t *testing.T) {
	rec := httptest.NewRecorder()
	handler := NoIndexSurfaceUnmatched(http.NotFoundHandler())
	for _, path := range []string{
		"/oauth2/authorize",
		"/oauth2/token",
		"/cosmos-ui/some/deep/thing",
		"/.well-known/oauth-authorization-server",
		"/logo/png",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noimageindex" {
			t.Errorf("unmatched surface path %q: X-Robots-Tag = %q, want the noindex tag",
				path, got)
		}
		rec = httptest.NewRecorder()
	}
	// An app's own 404 page is the app's business — no tag stamped.
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some-app/missing", nil))
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("app path 404: X-Robots-Tag = %q, want nothing stamped", got)
	}
}
