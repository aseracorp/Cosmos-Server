package docker

import "testing"

func TestDockerVersionAtLeast(t *testing.T) {
	cases := []struct {
		ver, min string
		want     bool
	}{
		{"29.6.1", "29.5.0", true},
		{"29.5.0", "29.5.0", true},
		{"29.5.1", "29.5.0", true},
		{"29.4.3", "29.5.0", false},
		{"26.0.0", "29.5.0", false},
		{"29.5.0-rc.1", "29.5.0", true}, // pre-release stripped; treat >= (conservative: no NoCopy on it)
		{"28.0.0", "29.5.0", false},
		{"30.0.0", "29.5.0", true},
		{"29.10.0", "29.5.0", true},
	}
	for _, c := range cases {
		if got := dockerVersionAtLeast(c.ver, c.min); got != c.want {
			t.Errorf("dockerVersionAtLeast(%q,%q)=%v want %v", c.ver, c.min, got, c.want)
		}
	}
}
