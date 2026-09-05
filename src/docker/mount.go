package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/azukaar/cosmos-server/src/utils"
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
	// NoCopy is the volume-nocopy mount option. It is auto-enabled for
	// subpath mounts (moby#52546: Docker's seed-from-image copy step fails
	// when the subpath resolves to a file) and preserved through export.
	NoCopy bool `json:"noCopy,omitempty"`
}

// UnmarshalJSON implements a compatibility layer that accepts both lowercase
// (new canonical) and uppercase (legacy Docker SDK) field names.
func (c *CosmosMount) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = CosmosMount{}

	// Case-insensitive lookup: the compose/HJSON editor and the Docker SDK
	// have historically spelled these keys differently (subpath / subPath /
	// SubPath / Subpath, readOnly / read_only / ReadOnly, type / Type, ...).
	// Match any spelling so a subpath is never silently dropped.
	find := func(name string) (interface{}, bool) {
		for k, v := range raw {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
		return nil, false
	}
	getStr := func(name string) string {
		if v, ok := find(name); ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getBool := func(names ...string) bool {
		for _, n := range names {
			if v, ok := find(n); ok {
				if b, ok := v.(bool); ok {
					return b
				}
			}
		}
		return false
	}

	c.Type = getStr("type")
	c.Source = getStr("source")
	c.Target = getStr("target")
	// Accept docker-compose "subpath", the Docker SDK "Subpath", and the
	// legacy "SubPath" / mixed "subPath" spellings case-insensitively.
	c.SubPath = getStr("subpath")
	c.ReadOnly = getBool("readOnly", "read_only", "readonly")
	c.NoCopy = getBool("noCopy", "nocopy", "no_copy")
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
		// moby#52546: when volume-subpath resolves to a FILE, Docker's
		// seed-from-image copy step tries to treat the resolved path as a
		// directory and fails with "not a directory". Setting NoCopy (the
		// `volume-nocopy` mount option) skips that seeding step entirely,
		// which moby's maintainers confirmed as the correct workaround and
		// fix. It's also semantically right: the image's target-path contents
		// must never be copied into a subpath mount anyway, so subpath mounts
		// always enable NoCopy. The flag is kept on the CosmosMount so the
		// compose/HJSON export round-trips it (it matches what the daemon
		// reports back).
		m.VolumeOptions = &mount.VolumeOptions{
			Subpath: c.SubPath,
			NoCopy:  true,
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
		cm.NoCopy = m.VolumeOptions.NoCopy
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

	// (0) Pre-emptive engine check: if the requested subpath would be silently
	// ignored by the daemon (engine < 26 / API < 1.45), say so clearly instead
	// of letting it mount the whole volume without the subpath.
	if hasSubpath(mounts) && !SupportsVolumeSubpath() {
		return fmt.Errorf("Volume subpaths require Docker Engine 26.0 or newer (API 1.45+). "+
			"Your engine (API %s) does not support mounting a subpath of a volume, so the "+
			"subpath '%s' would be silently ignored and the whole volume mounted instead. "+
			"Upgrade Docker Engine to 26.0+, or remove the 'subpath' from the volume. (%s)",
			EngineAPIVersionOrUnknown(), firstSubpath(mounts), msg)
	}

	// (1) Daemon API too old for volume subpaths.
	if strings.Contains(msg, "VolumeOptions.Subpath needs API v1.45") ||
		strings.Contains(msg, "VolumeOptions.Subpath") && strings.Contains(msg, "v1.45") {
		return fmt.Errorf("Volume subpaths require Docker Engine 26.0 or newer. "+
			"Your Docker engine version does not support the 'subpath' mount option. "+
			"Please upgrade Docker Engine to 26.0+ to use subpath mounts, or remove the 'subpath' from the volume. (%s)", msg)
	}

	// (1.5) runc start error: mounting the safe-mount onto a file target fails
	// with ENOTDIR when the subpath inside the volume is actually a directory
	// (or the destination type mismatches). Explain what's wrong.
	if strings.Contains(msg, "not a directory") && strings.Contains(msg, "mount") {
		if sub := subpathFromMounts(mounts); sub != "" {
			return fmt.Errorf("Could not mount volume subpath '%s': Docker refuses to mount a directory subpath onto a file target "+
				"(ENOTDIR). Check that the subpath inside the volume is a regular file, not a directory — if it was created as a "+
				"directory, empty it (or delete it) and retry, and Cosmos will recreate it as a file. (%s)", sub, msg)
		}
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

// hasSubpath reports whether any mount carries a subpath.
func hasSubpath(mounts []CosmosMount) bool {
	for _, m := range mounts {
		if m.SubPath != "" {
			return true
		}
	}
	return false
}

// firstSubpath returns the first non-empty subpath among the given mounts.
func firstSubpath(mounts []CosmosMount) string {
	return subpathFromMounts(mounts)
}

// EngineAPIVersionOrUnknown returns the negotiated API version or "unknown".
func EngineAPIVersionOrUnknown() string {
	if dockerAPIVersion == "" {
		return "unknown"
	}
	return dockerAPIVersion
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

// EngineAPIVersion returns the negotiated Docker Engine API version (e.g.
// "1.45") captured at Connect time, or "" if not yet known.
func EngineAPIVersion() string {
	return dockerAPIVersion
}

// SupportsVolumeSubpath reports whether the connected Docker engine supports
// mounting a subpath of a named volume. That feature landed in API v1.45
// (Docker Engine 26.0+); on older engines the daemon silently ignores
// VolumeOptions.Subpath and mounts the whole volume. When dockerAPIVersion is
// unknown (not yet connected) we optimistically assume support, matching the
// pre-existing behavior, and the daemon's inspect output will tell us the
// truth at export time.
func SupportsVolumeSubpath() bool {
	if dockerAPIVersion == "" {
		return true
	}
	return dockerAPIVersion >= "1.45"
}

// ToDockerMountSliceConvertible converts CosmosMounts to Docker mount.Mounts.
// On engines that support VolumeOptions.Subpath (API 1.45+, Docker Engine
// 26.0+) it behaves exactly like ToDockerMountSlice. On older engines, where
// the daemon would silently ignore the subpath and mount the whole volume, it
// emulates the subpath by converting each volume+subpath mount into a bind
// mount of the volume's data directory + subpath (which the caller ensures
// exists). The result is identical to what Docker 26 does internally.
func ToDockerMountSliceConvertible(mounts []CosmosMount) []mount.Mount {
	// Always use the native VolumeOptions.Subpath for subpath mounts. Emulating
	// a file subpath as a raw bind of .../_data/<subpath> was tried and fails:
	// runc resolves the raw host path inside the container's mount namespace
	// and the kernel returns ENOTDIR ("mount a directory onto a file"). The
	// native subpath path uses Docker's safe proc-fd mechanism and works.
	//
	// NoCopy is set (moby#52546) so Docker skips the seed-from-image copy step
	// that breaks for file subpaths when the target exists in the image.
	for _, m := range mounts {
		utils.Debug(fmt.Sprintf("ToDockerMountSliceConvertible native: type=%s source=%s target=%s subpath=%q readOnly=%v",
			m.Type, m.Source, m.Target, m.SubPath, m.ReadOnly))
	}
	return ToDockerMountSlice(mounts)
}

// volumeDataDirPattern matches a bind mount source that points inside a
// Docker named volume's data directory:
//
//	<data-root>/volumes/<name>/_data[/<subpath>...]
//
// On engines < 26 ToDockerMountSliceConvertible emulates a volume subpath by
// bind-mounting exactly this path. FromDockerMountSmart reverses that so the
// export (and therefore the compose/HJSON editor) shows volume+subpath again.
var volumeDataDirPattern = regexp.MustCompile(`^/var/lib/docker/volumes/([^/]+)/_data(/.*)?$`)

// FromDockerMountSmart converts a daemon-reported mount.Mount into a
// CosmosMount, reconstructing a volume+subpath from the bind-mount emulation
// used on engines < 26. On engines that natively support subpaths
// (HostConfig.Mounts[].VolumeOptions.Subpath) it behaves like FromDockerMount.
func FromDockerMountSmart(m mount.Mount) CosmosMount {
	cm := FromDockerMount(m)

	// Native subpath already carried in VolumeOptions → nothing to do.
	if m.Type == mount.TypeVolume && cm.SubPath != "" {
		return cm
	}

	// Emulated subpath: a bind mount into a volume's _data directory.
	if m.Type == mount.TypeBind {
		if groups := volumeDataDirPattern.FindStringSubmatch(m.Source); groups != nil {
			volName := groups[1]
			sub := strings.TrimPrefix(groups[2], "/")
			if len(groups) > 2 && groups[2] != "" && sub != "" {
				cm = CosmosMount{
					Type:     "volume",
					Source:   volName,
					Target:   m.Target,
					SubPath:  sub,
					ReadOnly: m.ReadOnly,
				}
			}
		}
	}

	return cm
}

// EnsureSubpathExists creates the missing parent directories and, when the
// final subpath component looks like a file (basename contains a dot), the
// file itself, inside the given mount root (a named volume's _data dir).
//
// Docker's volume-subpath mount requires the subpath to already exist inside
// the volume: safepath.Join refuses nonexistent paths with ErrNotAccessible,
// and if a file subpath was wrongly created as a directory the daemon fails
// with "mount a directory onto a file". This helper mirrors what compose users
// expect from Cosmos's bind-mount auto-creation, extended to also create the
// target FILE (empty) so mounting a file subpath works out of the box.
//
// Returns the paths that were created.
func EnsureSubpathExists(mountRoot, subpath string) ([]string, error) {
	created := []string{}
	if subpath == "" {
		return created, nil
	}
	clean := strings.TrimPrefix(filepath.Clean(subpath), string(filepath.Separator))
	if clean == "." || clean == "" {
		return created, nil
	}

	// Walk each component so we create parents before the final component.
	dirs := filepath.Dir(clean)
	if dirs != "." && dirs != "" {
		p := filepath.Join(mountRoot, dirs)
		if err := os.MkdirAll(p, 0o750); err != nil {
			return created, err
		}
		created = append(created, p)
	}

	full := filepath.Join(mountRoot, clean)
	fi, statErr := os.Stat(full)
	if statErr == nil {
		// Already exists. If it's a file subpath but exists as a DIRECTORY
		// (left over from earlier auto-create bugs that MkdirAll'd the whole
		// file path), replace it: Docker would otherwise resolve a directory
		// subpath and fail mounting it onto the file target at container start
		// (ENOTDIR: "mount source that is a directory, destination is a file").
		if looksLikeFile(clean) && fi.IsDir() {
			// The subpath is a directory but the mount intends a file (its
			// basename has a dot). If empty, we can safely replace it with a
			// file; if not, we must not delete user data — fail with a clear
			// message instead of the cryptic runc ENOTDIR.
			entries, derr := os.ReadDir(full)
			if derr == nil && len(entries) == 0 {
				if rerr := replaceDirWithEmptyFile(full, 0o640); rerr != nil {
					return created, rerr
				}
				created = append(created, full)
			} else if derr == nil {
				return created, fmt.Errorf(
					"subpath '%s' exists as a non-empty directory inside the volume, but it is mounted as a FILE. "+
						"empty it (or remove it) and retry; Docker cannot mount a directory onto a file target. "+
						"(%d entries present)", subpath, len(entries))
			} else {
				return created, derr
			}
		}
		return created, nil
	} else if !os.IsNotExist(statErr) {
		return created, statErr
	}

	base := filepath.Base(clean)
	if looksLikeFile(clean) {
		// Looks like a file (e.g. config.yaml, app.conf) → create it empty.
		dir := filepath.Dir(full)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return created, err
		}
		f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			return created, err
		}
		f.Close()
		created = append(created, full)
	} else {
		// Looks like a directory → create it.
		if err := os.MkdirAll(full, 0o750); err != nil {
			return created, err
		}
		created = append(created, full)
	}
	_ = base

	return created, nil
}

// looksLikeFile reports whether a subpath's basename looks like a file
// (contains a dot), matching how bind-mount auto-creation detects files.
func looksLikeFile(subpath string) bool {
	base := filepath.Base(subpath)
	return strings.Contains(base, ".")
}

// replaceDirWithEmptyFile replaces a (potentially non-empty) directory at path
// with an empty regular file, so a file-subpath resolves to the right type.
// Only safe because fixing the type mismatch is exactly what's needed to let
// Docker mount the subpath onto its (file) target.
func replaceDirWithEmptyFile(path string, mode os.FileMode) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	return f.Close()
}

// ValidateMountConflicts checks a service's volume list for the most common
// Docker mount configuration error: a mount whose target is nested inside
// another mount's target directory.
//
// Example (invalid):
//
//	volumes:
//	  - source: somevol, target: /volumes        # folder mount
//	  - source: cfg, target: /volumes/config.yaml, subpath: app.yaml
//	                                             # file mount INTO that folder
//
// Docker cannot mount a path inside a directory that is itself a mount point:
// runc fails at container start with a cryptic ENOTDIR ("mount a directory
// onto a file (or vice-versa)"). This helper rejects the combination up front
// with a clear message instead.
//
// It returns an error describing the first offending pair, or nil if the
// mounts don't nest.
func ValidateMountConflicts(mounts []CosmosMount) error {
	for i := range mounts {
		targetI := filepath.Clean(mounts[i].Target)
		if targetI == "" || targetI == "." || targetI == "/" {
			continue
		}
		for j := range mounts {
			if i == j {
				continue
			}
			targetJ := filepath.Clean(mounts[j].Target)
			if targetJ == "" || targetJ == "." || targetJ == "/" {
				continue
			}
			// targetJ is a strict, component-wise parent of targetI -> the
			// two mounts would nest (targetI sits inside the mount at targetJ).
			if isStrictPathInside(targetI, targetJ) {
				return fmt.Errorf(
					"Invalid volume configuration: mount %d target '%s' (source '%s'%s) is inside another mount's target '%s' (source '%s'). "+
						"Docker cannot mount a path inside a directory that is already a mount point; this fails at container start. "+
						"Choose distinct, non-nested target directories.",
					i+1, mounts[i].Target, mounts[i].Source, subpathDesc(mounts[i]),
					mounts[j].Target, mounts[j].Source)
			}
		}
	}
	return nil
}

func isStrictPathInside(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func subpathDesc(m CosmosMount) string {
	if m.SubPath != "" {
		return " (subpath '" + m.SubPath + "')"
	}
	return ""
}
