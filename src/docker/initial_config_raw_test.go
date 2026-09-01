package docker

import (
	"testing"

	conttype "github.com/docker/docker/api/types/container"
)

func TestInitialConfigRawRoundTrip(t *testing.T) {
	const rawDoc = `{
  // my service
  name: web,
  image: nginx:latest, # pinned
  ports: [
    "8080:80" /* http */
  ]
}`

	conf := &conttype.Config{Labels: map[string]string{}}
	SetInitialConfigRawLabel(conf, rawDoc)
	if got := GetInitialConfigRaw(conf); got != rawDoc {
		t.Errorf("raw round-trip mismatch:\n got: %q\nwant: %q", got, rawDoc)
	}

	// Absent raw label -> empty string, no error.
	if got := GetInitialConfigRaw(&conttype.Config{Labels: map[string]string{"a": "b"}}); got != "" {
		t.Errorf("expected empty raw for missing label, got %q", got)
	}
	if got := GetInitialConfigRaw(nil); got != "" {
		t.Errorf("expected empty raw for nil config, got %q", got)
	}
}

func TestStripInitialConfigRemovesRawLabel(t *testing.T) {
	labels := map[string]string{
		"cosmos.initial-config":      `{"image":"nginx"}`,
		"cosmos.initial-config-raw":  "{\n  // comment\n  image: nginx\n}",
		"com.example.user-label":     "keep",
	}
	stripped := StripInitialConfigLabel(labels)
	if _, ok := stripped["cosmos.initial-config"]; ok {
		t.Error("structured label still present after strip")
	}
	if _, ok := stripped["cosmos.initial-config-raw"]; ok {
		t.Error("raw label still present after strip")
	}
	if stripped["com.example.user-label"] != "keep" {
		t.Error("unrelated label was removed")
	}
	// Input must not be mutated.
	if _, ok := labels["cosmos.initial-config"]; !ok {
		t.Error("StripInitialConfigLabel mutated its input")
	}
}

func TestCreateServiceExtractsRawFromBody(t *testing.T) {
	// Simulate the client payload: parsed object + top-level "$$raw" with
	// comments (mirrors API.docker.createService's body construction).
	body := `{"services":{"web":{"image":"nginx"}},"$$raw":"{\n  // comment\n  image: nginx\n}"}`

	var req DockerServiceCreateRequest
	if err := jsonDecode([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw := extractRawConfig([]byte(body))
	if raw == "" {
		t.Fatal("expected $$raw extraction to succeed")
	}
	if raw != "{\n  // comment\n  image: nginx\n}" {
		t.Errorf("raw mismatch: %q", raw)
	}
	// Regular API clients (no $$raw) must still decode fine and yield "".
	plain := `{"services":{"web":{"image":"nginx"}}}`
	var req2 DockerServiceCreateRequest
	if err := jsonDecode([]byte(plain), &req2); err != nil {
		t.Fatalf("plain decode: %v", err)
	}
	if raw := extractRawConfig([]byte(plain)); raw != "" {
		t.Errorf("expected empty raw for plain body, got %q", raw)
	}
}

func TestRawConfigForService(t *testing.T) {
	// Single-service stack: raw document is exactly this service, keep it.
	got := RawConfigForService("web", "{\n  // comment\n  web: { image: nginx }\n}", 1)
	if got == "" {
		t.Fatal("single-service stack should keep the raw config")
	}
	if got != "{\n  // comment\n  web: { image: nginx }\n}" {
		t.Errorf("single-service raw should be unchanged, got %q", got)
	}

	// Multi-service stack: must NOT store the whole document per container.
	multi := "{\n  web: { image: nginx },\n  db: { image: postgres }\n}"
	if got := RawConfigForService("web", multi, 2); got != "" {
		t.Errorf("multi-service stack should not persist raw per container, got %q", got)
	}

	// Empty / missing inputs.
	if got := RawConfigForService("", "x", 1); got != "" {
		t.Errorf("empty serviceName should yield empty, got %q", got)
	}
	if got := RawConfigForService("web", "", 1); got != "" {
		t.Errorf("empty raw should yield empty, got %q", got)
	}
	if got := RawConfigForService("web", "   ", 1); got != "" {
		t.Errorf("whitespace raw should yield empty, got %q", got)
	}
}