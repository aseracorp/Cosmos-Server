package docker

import (
	"testing"
)

// VolumeNameFromSource must recover the durable volume name from the host
// path the docker daemon reports for a volume mount. The daemon's canonical
// layout is "<base>/<name>/_data"; the trailing "/_data" component (and any
// stray trailing slash) must be stripped. A bare name (already resolved) and
// an arbitrary source without the "/_data" marker pass through unchanged.
func TestVolumeNameFromSource(t *testing.T) {
	cases := []struct {
		name, source, want string
	}{
		{"canonical layout", "/var/lib/docker/volumes/abc123/_data", "abc123"},
		{"random anonymous hash", "/var/lib/docker/volumes/7a0b4a1dde91273e729ded989bd3bde1e4446ce0abbcdd1c322686e99a3c39e9/_data", "7a0b4a1dde91273e729ded989bd3bde1e4446ce0abbcdd1c322686e99a3c39e9"},
		{"bare name (already resolved)", "abc123", "abc123"},
		{"empty source", "", ""},
		{"nested driver path", "/mnt/custom/docker/volumes/myvol/_data", "myvol"},
		{"plain path without _data marker is a name", "/var/lib/docker/volumes/other", "other"},
		{"volume name ending in _data is untouched", "/var/lib/docker/volumes/foo_data", "foo_data"},
		{"stray trailing slash", "/var/lib/docker/volumes/abc123/", "abc123"},
	}
	for _, c := range cases {
		if got := VolumeNameFromSource(c.source); got != c.want {
			t.Errorf("%s: VolumeNameFromSource(%q) = %q, want %q", c.name, c.source, got, c.want)
		}
	}
}