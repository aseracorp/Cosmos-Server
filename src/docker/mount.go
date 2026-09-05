package docker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

// CosmosMount mirrors mount.Mount but uses lowercase JSON tags to match
// the Docker Compose specification and the rest of Cosmos's API conventions.
// It also supports reading legacy uppercase fields for backward compatibility.
type CosmosMount struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
	// SubPath mounts a subdirectory (or a single file) inside a named
	// volume instead of the whole volume, like docker-compose's `subpath`.
	// It is wired to mount.VolumeOptions.Subpath for volume mounts.
	SubPath string `json:"subpath,omitempty"`
	// ReadOnly mounts the source read-only (compose short-syntax `:ro`).
	ReadOnly bool `json:"readOnly,omitempty"`
}

// UnmarshalJSON implements a compatibility layer that accepts both lowercase
// (new canonical) and uppercase (legacy Docker SDK) field names.
func (c *CosmosMount) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = CosmosMount{}

	// Helper: try lowercase key first, then uppercase fallback
	getStr := func(keys ...string) string {
		for _, low := range keys {
			if v, ok := raw[low]; ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
		return ""
	}

	c.Type = getStr("type", "Type")
	c.Source = getStr("source", "Source")
	c.Target = getStr("target", "Target")
	// Accept the docker-compose spelling ("subpath") as well as the Docker
	// SDK JSON spelling ("Subpath") and the legacy "SubPath" casing.
	c.SubPath = getStr("subpath", "Subpath", "SubPath")
	if v, ok := raw["readOnly"]; ok {
		if b, ok := v.(bool); ok {
			c.ReadOnly = b
		}
	}
	if v, ok := raw["read_only"]; ok {
		if b, ok := v.(bool); ok {
			c.ReadOnly = b
		}
	}
	if v, ok := raw["mode"]; ok {
		if mode, ok := v.(string); ok {
			for _, seg := range strings.Split(mode, ",") {
				if seg == "ro" {
					c.ReadOnly = true
				}
			}
		}
	}
	return nil
}

// ToDockerMount converts a CosmosMount into the Docker SDK mount.Mount type.
func (c CosmosMount) ToDockerMount() mount.Mount {
	m := mount.Mount{
		Type:     mount.Type(c.Type),
		Source:   c.Source,
		Target:   c.Target,
		ReadOnly: c.ReadOnly,
	}

	// Docker engine 26.0+ supports mounting a subpath of a named volume
	// (volume-subpath). docker-compose exposes this as the `subpath` option.
	if c.SubPath != "" {
		m.VolumeOptions = &mount.VolumeOptions{
			Subpath: c.SubPath,
		}
	}

	return m
}

// ToDockerMountSlice converts a slice of CosmosMount into a slice of mount.Mount.
func ToDockerMountSlice(c []CosmosMount) []mount.Mount {
	m := make([]mount.Mount, len(c))
	for i, mount := range c {
		m[i] = mount.ToDockerMount()
	}
	return m
}

// IsBindMount returns true if the CosmosMount type is a bind mount.
func (c CosmosMount) IsBindMount() bool {
	return c.Type == string(mount.TypeBind)
}

// IsVolumeMount returns true if the CosmosMount type is a volume mount.
func (c CosmosMount) IsVolumeMount() bool {
	return c.Type == string(mount.TypeVolume)
}

// FromDockerMount converts a Docker SDK mount.Mount into a CosmosMount.
func FromDockerMount(m mount.Mount) CosmosMount {
	cm := CosmosMount{
		Type:     string(m.Type),
		Source:   m.Source,
		Target:   m.Target,
		ReadOnly: m.ReadOnly,
	}
	if m.VolumeOptions != nil {
		cm.SubPath = m.VolumeOptions.Subpath
	}
	return cm
}

// FromDockerMountSlice converts a slice of mount.Mount into a slice of CosmosMount.
func FromDockerMountSlice(mounts []mount.Mount) []CosmosMount {
	c := make([]CosmosMount, len(mounts))
	for i, m := range mounts {
		c[i] = FromDockerMount(m)
	}
	return c
}

// FriendlySubpathError inspects a container-create error and, when it is a
// volume-subpath failure, returns a clear, actionable message (in English, to
// match the rest of the server-side streamed logs) instead of the raw daemon
// error. The raw error is returned unchanged for unrelated failures.
//
// Two failure modes are translated:
//  1. Engine too old: the daemon rejects `VolumeOptions.Subpath` unless the
//     negotiated API version is >= 1.45 (Docker Engine 26.0+).
//  2. Missing subpath: the named volume exists but the requested subpath does
//     not (the daemon refuses to create it, resolving under the volume root).
func FriendlySubpathError(err error, mounts []CosmosMount) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// (1) Daemon API too old for volume subpaths.
	if strings.Contains(msg, "VolumeOptions.Subpath needs API v1.45") ||
		strings.Contains(msg, "VolumeOptions.Subpath") && strings.Contains(msg, "v1.45") {
		return fmt.Errorf("Volume subpaths require Docker Engine 26.0 or newer. "+
			"Your Docker engine version does not support the 'subpath' mount option. "+
			"Please upgrade Docker Engine to 26.0+ to use subpath mounts, or remove the 'subpath' from the volume. (%s)", msg)
	}

	// (2) Subpath does not exist inside the volume.
	if strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "ErrNotAccessible") ||
		strings.Contains(msg, "ENOENT") {
		if sub := subpathFromMounts(mounts); sub != "" {
			return fmt.Errorf("The subpath '%s' does not exist inside its volume. "+
				"Docker mounts a subpath only if it already exists inside the volume — "+
				"docker doesn't create it. Add the %s directory/file to the volume "+
				"(e.g. via a temporary helper container or an init that populates it), "+
				"then retry. (%s)", sub, sub, msg)
		}
	}

	return err
}

// subpathFromMounts returns the first non-empty subpath among the given mounts.
func subpathFromMounts(mounts []CosmosMount) string {
	for _, m := range mounts {
		if m.SubPath != "" {
			return m.SubPath
		}
	}
	return ""
}

// CosmosMountsFromDocker converts a slice of Docker SDK mount.Mount into the
// CosmosMount form. It's the same as FromDockerMountSlice; the alias exists so
// call sites read naturally when they need the subpath-aware CosmosMount list
// just to build a friendly error message.
func CosmosMountsFromDocker(mounts []mount.Mount) []CosmosMount {
	return FromDockerMountSlice(mounts)
}
