package docker

import (
	"encoding/json"
	"testing"

	conttype "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func TestInitialConfigRoundTrip(t *testing.T) {
	svc := ContainerCreateRequestContainer{
		Name:        "web",
		Image:       "nginx:latest",
		Environment: []string{"FOO=bar", "TZ=auto"},
		Labels: map[string]string{
			"cosmos-network-name": "cosmos-web-default",
			"com.example.tag":     "keep-me",
		},
		Ports: []string{"8080:80/tcp"},
		Volumes: []mount.Mount{
			{Type: mount.TypeVolume, Source: "webdata", Target: "/data"},
		},
		RestartPolicy: "unless-stopped",
		MemLimit:      "256mb",
	}

	conf := &conttype.Config{Labels: map[string]string{}}
	if err := SetInitialConfigLabel(conf, svc); err != nil {
		t.Fatalf("SetInitialConfigLabel: %v", err)
	}

	got, ok := GetInitialConfig(conf)
	if !ok {
		t.Fatal("GetInitialConfig: expected stored snapshot, got none")
	}
	if got.Name != "web" || got.Image != "nginx:latest" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.Environment) != 2 || got.Environment[0] != "FOO=bar" {
		t.Errorf("environment mismatch: %+v", got.Environment)
	}
	if got.Labels["cosmos-network-name"] != "cosmos-web-default" {
		t.Errorf("labels mismatch: %+v", got.Labels)
	}

	// The internal label must be stripped from the user-visible labels.
	stripped := StripInitialConfigLabel(got.Labels)
	if _, present := stripped[initialConfigLabel]; present {
		t.Error("StripInitialConfigLabel: initial-config label still present")
	}
	if stripped["com.example.tag"] != "keep-me" {
		t.Error("StripInitialConfigLabel: unrelated label was removed")
	}
}

func TestInitialConfigNoSnapshot(t *testing.T) {
	conf := &conttype.Config{Labels: map[string]string{"something": "else"}}
	if _, ok := GetInitialConfig(conf); ok {
		t.Fatal("GetInitialConfig: expected no snapshot for container without the label")
	}
	if _, ok := GetInitialConfig(nil); ok {
		t.Fatal("GetInitialConfig(nil): expected no snapshot")
	}
}

func TestInitialConfigDeepCopyIndependence(t *testing.T) {
	original := ContainerCreateRequestContainer{
		Name:  "db",
		Image: "postgres:16",
		Labels: map[string]string{
			"a": "1",
		},
		Networks: map[string]ContainerCreateRequestServiceNetwork{
			"net1": {Aliases: []string{"db"}},
		},
	}

	copy_, err := deepCopyServiceRequest(original)
	if err != nil {
		t.Fatalf("deepCopyServiceRequest: %v", err)
	}

	// Mutate the copy; the original must not change.
	copy_.Labels["a"] = "999"
	copy_.Networks["net1"] = ContainerCreateRequestServiceNetwork{Aliases: []string{"mutated"}}
	copy_.Networks["net2"] = ContainerCreateRequestServiceNetwork{}

	if original.Labels["a"] != "1" {
		t.Errorf("original labels mutated: %v", original.Labels)
	}
	if _, ok := original.Networks["net2"]; ok {
		t.Errorf("original networks mutated: %+v", original.Networks)
	}
	if len(original.Networks["net1"].Aliases) != 1 || original.Networks["net1"].Aliases[0] != "db" {
		t.Errorf("original network aliases mutated: %+v", original.Networks["net1"])
	}
}

func TestInitialConfigLabelStoredAsJSON(t *testing.T) {
	svc := ContainerCreateRequestContainer{Name: "x", Image: "y"}
	conf := &conttype.Config{Labels: map[string]string{}}
	if err := SetInitialConfigLabel(conf, svc); err != nil {
		t.Fatalf("SetInitialConfigLabel: %v", err)
	}
	raw := conf.Labels[initialConfigLabel]
	if raw == "" {
		t.Fatal("label not written")
	}
	var probe map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		t.Fatalf("label is not valid JSON: %v", err)
	}
	if probe["image"] != "y" {
		t.Errorf("label JSON content wrong: %s", raw)
	}
}