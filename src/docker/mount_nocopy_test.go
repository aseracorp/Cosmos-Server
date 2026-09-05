package docker

import (
	"encoding/json"
	"testing"
)

func TestNoCopyGating(t *testing.T) {
	// Simulate: parse a subpath mount, then check what NoCopy ToDockerMount produces
	// under different engine versions.
	cases := []struct {
		name            string
		serverVersion   string
		explicitNoCopy  bool
		wantNoCopy      bool
	}{
		{"engine 29.6.1 no explicit", "29.6.1", false, false},
		{"engine 29.5.0 no explicit", "29.5.0", false, false},
		{"engine 29.4.3 no explicit (workaround on)", "29.4.3", false, true},
		{"engine 26.0.0 no explicit (workaround on)", "26.0.0", false, true},
		{"engine 29.6.1 explicit true", "29.6.1", true, true},
		{"engine 29.4.3 explicit true", "29.4.3", true, true},
	}
	for _, c := range cases {
		old := dockerServerVersion
		dockerServerVersion = c.serverVersion
		cm := CosmosMount{Type: "volume", Source: "v", Target: "/f", SubPath: "a/b.conf"}
		if c.explicitNoCopy {
			cm.NoCopy = true
		}
		dm := cm.ToDockerMount()
		got := dm.VolumeOptions != nil && dm.VolumeOptions.NoCopy
		dockerServerVersion = old
		if got != c.wantNoCopy {
			t.Errorf("%s: NoCopy=%v want %v", c.name, got, c.wantNoCopy)
		}
	}
}

func TestNoCopyJSONRoundTrip(t *testing.T) {
	cm := CosmosMount{Type: "volume", Source: "v", Target: "/f", SubPath: "a/b.conf", NoCopy: true}
	dm := cm.ToDockerMount()
	b, _ := json.Marshal(dm)
	want := `"NoCopy":true`
	if !contains(string(b), want) {
		t.Errorf("ToDockerMount JSON missing NoCopy: %s", b)
	}
	// export side
	back := FromDockerMount(dm)
	if !back.NoCopy {
		t.Errorf("NoCopy lost in FromDockerMount round-trip")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
