package docker

import (
	"encoding/json"
	"strings"

	"github.com/azukaar/cosmos-server/src/utils"

	conttype "github.com/docker/docker/api/types/container"
)

// initialConfigLabel is the label under which Cosmos stores the service
// definition exactly as the user submitted it (or as it was imported from a
// compose file) at the moment the container is created/recreated through the
// Cosmos API. It is the source of truth for what the user *configured*,
// as opposed to what the Docker daemon resolved at runtime (defaults, added
// env vars, inserted labels, normalized values, etc).
//
// The label approach mirrors com.docker.compose.depends_on: it is a durable,
// per-container record that survives re-creates, restarts and moves, and it
// does not require any central database. It is transparent to docker-compose
// (which ignores unknown labels) and to the docker CLI (docker inspect).
//
// The value is the JSON encoding of ContainerCreateRequestContainer, the same
// shape the compose editor (HJSON view) reads and writes.
const initialConfigLabel = "cosmos.initial-config"

// initialConfigRawLabel holds the literal HJSON/JSON text the user had in
// the compose editor at save time (comments included). It is optional: only
// set when the client sent "$$raw" alongside the parsed config. When present,
// the compose editor shows this exact text instead of re-rendering the
// parsed object, so // # and /* */ comments survive round-trips.
const initialConfigRawLabel = "cosmos.initial-config-raw"

// SetInitialConfigRawLabel stores the original editor text (HJSON with
// comments) on the container config being created.
func SetInitialConfigRawLabel(conf *conttype.Config, raw string) {
	if conf == nil || raw == "" {
		return
	}
	if conf.Labels == nil {
		conf.Labels = make(map[string]string)
	}
	conf.Labels[initialConfigRawLabel] = raw
}

// GetInitialConfigRaw returns the stored original editor text, or "" when
// the container has none (created before this feature, or saved from a client
// that did not send raw text).
func GetInitialConfigRaw(conf *conttype.Config) string {
	if conf == nil || conf.Labels == nil {
		return ""
	}
	return conf.Labels[initialConfigRawLabel]
}

// jsonDecode is a thin wrapper over json.Unmarshal kept for tests.
func jsonDecode(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// extractRawConfig pulls the optional "$$raw" member (literal HJSON text the
// user typed, comments included) out of a CreateService request body. The
// member is a client-side convention of the compose editor; regular API
// clients that POST plain JSON simply have no "$$raw" and get "".
func extractRawConfig(rawBody []byte) string {
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		return ""
	}
	if raw, ok := bodyMap["$$raw"].(string); ok {
		return raw
	}
	return ""
}

// SetInitialConfigLabel stores the user-submitted service definition on the
// container config being created. The stored payload must be the *original*
// request, before Cosmos rewrites labels (cosmos.stack, depends_on,
// cosmos-force-network-mode, resolved cosmos-network-name), normalizes
// network_mode or injects defaults — otherwise the "initial settings" shown
// in the compose editor would drift towards the runtime state on every edit.
//
// serialized is a cheap way to avoid re-marshaling when the caller already
// produced the JSON (e.g. from the create request).
func SetInitialConfigLabel(conf *conttype.Config, svc ContainerCreateRequestContainer) error {
	if conf == nil {
		return nil
	}
	raw, err := json.Marshal(svc)
	if err != nil {
		return err
	}
	if conf.Labels == nil {
		conf.Labels = make(map[string]string)
	}
	conf.Labels[initialConfigLabel] = string(raw)
	return nil
}

// GetInitialConfig decodes the stored initial config from a container's
// labels. Returns ok=false when the container has no stored snapshot (e.g. it
// was created before this feature, or created outside Cosmos). The labels map
// is read-only; the caller must not mutate it.
func GetInitialConfig(conf *conttype.Config) (ContainerCreateRequestContainer, bool) {
	var svc ContainerCreateRequestContainer
	if conf == nil || conf.Labels == nil {
		return svc, false
	}
	raw := conf.Labels[initialConfigLabel]
	if strings.TrimSpace(raw) == "" {
		return svc, false
	}
	if err := json.Unmarshal([]byte(raw), &svc); err != nil {
		utils.Error("GetInitialConfig: cannot decode stored initial config", err)
		return svc, false
	}
	return svc, true
}

// StripInitialConfigLabel removes the internal initial-config labels (both
// the structured snapshot and the raw editor text) from a labels map without
// mutating the input. They are internal details: the compose editor shows the
// decoded service definition (or the raw text), not the labels themselves.
func StripInitialConfigLabel(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	_, hasSnapshot := labels[initialConfigLabel]
	_, hasRaw := labels[initialConfigRawLabel]
	if !hasSnapshot && !hasRaw {
		return labels
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if k == initialConfigLabel || k == initialConfigRawLabel {
			continue
		}
		out[k] = v
	}
	return out
}

// deepCopyServiceRequest returns a deep copy of a ContainerCreateRequestContainer
// via JSON round-trip, so later mutations of shared maps/slices (labels,
// networks, env...) by CreateService cannot corrupt the stored initial-config
// snapshot. A JSON round-trip is used because every field is JSON-serializable
// and this guarantees a fully independent copy without hand-cloning every
// nested type.
func deepCopyServiceRequest(in ContainerCreateRequestContainer) (ContainerCreateRequestContainer, error) {
	var out ContainerCreateRequestContainer
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
