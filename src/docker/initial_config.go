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

// StripInitialConfigLabel removes the internal initial-config label from a
// labels map without mutating the input. The label is an internal detail:
// the compose editor shows the decoded service definition, not the label.
func StripInitialConfigLabel(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	if _, ok := labels[initialConfigLabel]; !ok {
		return labels
	}
	out := make(map[string]string, len(labels)-1)
	for k, v := range labels {
		if k == initialConfigLabel {
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
