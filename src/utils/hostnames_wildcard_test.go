package utils

import (
	"reflect"
	"testing"
)

// withHostnamesConfig installs a config for GetAllHostnames and restores the
// previous one (and the tunnel-route hook) when the test ends.
func withHostnamesConfig(t *testing.T, hostname string, wildcard bool, override string, routes []ProxyRouteConfig, tunnels []ProxyRouteConfig) {
	t.Helper()
	previous := GetMainConfig()
	previousTunnels := GetConstellationTunnelRoutes
	t.Cleanup(func() {
		LoadBaseMainConfig(previous)
		GetConstellationTunnelRoutes = previousTunnels
	})

	config := previous
	config.NewInstall = false
	config.HTTPConfig.Hostname = hostname
	config.HTTPConfig.UseWildcardCertificate = wildcard
	config.HTTPConfig.OverrideWildcardDomains = override
	config.HTTPConfig.ProxyConfig.Routes = routes
	LoadBaseMainConfig(config)
	GetConstellationTunnelRoutes = func() []ProxyRouteConfig { return tunnels }
}

func host(h string) ProxyRouteConfig {
	return ProxyRouteConfig{Name: h, UseHost: true, Host: h}
}

func TestGetAllHostnamesCollectsRoutes(t *testing.T) {
	withHostnamesConfig(t, "cluster.example.com:8443", false, "",
		[]ProxyRouteConfig{
			host("app.example.com:8080"),
			host("App.example.com:8080"), // not deduped: hostnames are kept as written
			{Name: "path-only", UseHost: false, Host: "ignored.example.com"},
			{Name: "empty", UseHost: true, Host: ""},
			{Name: "list", UseHost: true, Host: "a.example.com,b.example.com"},
			{Name: "spaced", UseHost: true, Host: "a.example.com b.example.com"},
			host("app.example.com:8080"), // exact duplicate
			host("cluster.example.com:8443"),
		},
		[]ProxyRouteConfig{host("tunnel.example.com")},
	)

	for name, tc := range map[string]struct {
		removePorts bool
		want        []string
	}{
		"with ports":    {false, []string{"cluster.example.com:8443", "app.example.com:8080", "App.example.com:8080", "tunnel.example.com"}},
		"without ports": {true, []string{"cluster.example.com", "app.example.com", "App.example.com", "tunnel.example.com"}},
	} {
		if got := GetAllHostnames(false, tc.removePorts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// a route on ":<port>" or "0.0.0.0:<port>" answers any Host on that port. It
// has no name to certify or publish (Let's Encrypt refuses an empty
// identifier), but EnsureHostname needs the raw entry to let requests through.
func TestGetAllHostnamesPortOnlyRoutes(t *testing.T) {
	withHostnamesConfig(t, "cluster.example.com", false, "",
		[]ProxyRouteConfig{host(":8603"), host("0.0.0.0:9000"), host("app.example.com:8080")}, nil)

	want := []string{"cluster.example.com", "app.example.com"}
	if got := GetAllHostnames(false, true); !reflect.DeepEqual(got, want) {
		t.Errorf("without ports: got %v, want %v", got, want)
	}
	want = []string{"cluster.example.com", ":8603", "0.0.0.0:9000", "app.example.com:8080"}
	if got := GetAllHostnames(false, false); !reflect.DeepEqual(got, want) {
		t.Errorf("with ports: got %v, want %v", got, want)
	}
	// the certificate path must never see an empty name
	for _, h := range GetAllHostnames(true, true) {
		if h == "" || h == "0.0.0.0" {
			t.Errorf("certificate hostnames contain %q", h)
		}
	}
}

func TestGetAllHostnamesWildcard(t *testing.T) {
	routes := []ProxyRouteConfig{
		host("app.example.com"),
		host("deep.app.example.com"), // one wildcard level does not cover it
		host("other.org"),
		host("example.com"),
	}

	for name, tc := range map[string]struct {
		hostname      string
		wildcard      bool
		applyWildcard bool
		override      string
		want          []string
	}{
		"wildcard off in config": {
			"cluster.example.com", false, true, "",
			[]string{"cluster.example.com", "app.example.com", "deep.app.example.com", "other.org", "example.com"},
		},
		"wildcard not requested": {
			"cluster.example.com", true, false, "",
			[]string{"cluster.example.com", "app.example.com", "deep.app.example.com", "other.org", "example.com"},
		},
		"wildcard on": {
			"cluster.example.com", true, true, "",
			[]string{"*.example.com", "deep.app.example.com", "other.org", "example.com"},
		},
		"wildcard on, hostname with port": {
			"cluster.example.com:8443", true, true, "",
			[]string{"*.example.com", "deep.app.example.com", "other.org", "example.com"},
		},
		"override domains": {
			"cluster.example.com", true, true, "*.other.org,*.app.example.com",
			[]string{"*.other.org", "*.app.example.com", "cluster.example.com", "app.example.com", "other.org", "example.com"},
		},
		"override domains, apex listed": {
			"cluster.example.com", true, true, "other.org,*.other.org",
			[]string{"*.other.org", "cluster.example.com", "app.example.com", "deep.app.example.com", "other.org", "example.com"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withHostnamesConfig(t, tc.hostname, tc.wildcard, tc.override, routes, nil)
			if got := GetAllHostnames(tc.applyWildcard, true); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// a main hostname that cannot carry a wildcard certificate leaves the list as it is
func TestGetAllHostnamesWildcardRefused(t *testing.T) {
	for _, hostname := range []string{"192.168.1.10", "localhost", "nas.local", "nas", "cosmos.test"} {
		t.Run(hostname, func(t *testing.T) {
			withHostnamesConfig(t, hostname, true, "", []ProxyRouteConfig{host("app." + hostname)}, nil)
			want := []string{hostname, "app." + hostname}
			if got := GetAllHostnames(true, true); !reflect.DeepEqual(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}

	// the override makes a wildcard possible even for such a hostname
	withHostnamesConfig(t, "nas.local", true, "*.nas.local", []ProxyRouteConfig{host("app.nas.local")}, nil)
	want := []string{"*.nas.local", "nas.local"}
	if got := GetAllHostnames(true, true); !reflect.DeepEqual(got, want) {
		t.Errorf("override on .local: got %v, want %v", got, want)
	}
}
