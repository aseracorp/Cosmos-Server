package docker

import (
	"encoding/json"
	"testing"

	containerType "github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
)

// decodeResourceForm exercises the real JSON shape of the update payload, so
// presence/absence is checked the way the route sees it.
func decodeResourceForm(t *testing.T, body string) ContainerForm {
	t.Helper()
	var form ContainerForm
	if err := json.Unmarshal([]byte(body), &form); err != nil {
		t.Fatalf("decode %q: %s", body, err)
	}
	return form
}

func strptr(s string) *string { return &s }
func fptr(f float64) *float64 { return &f }

func TestResourceFormAbsencePreservesLimits(t *testing.T) {
	// A partial update that touches nothing resource-related must leave the
	// existing limits alone: the helper receives nil and changes nothing.
	res := containerType.Resources{Memory: 512 << 20, MemorySwap: 1 << 30, NanoCPUs: 2e9}
	form := decodeResourceForm(t, `{"envVars":["A=b"]}`)
	if form.MemLimit != nil || form.CPUs != nil {
		t.Fatalf("omitted fields must decode as nil, got MemLimit=%v CPUs=%v", form.MemLimit, form.CPUs)
	}
	if err := applyResourceConstraints(&res, nil, nil); err != nil {
		t.Fatalf("apply with no limits: %s", err)
	}
	if res.Memory != 512<<20 || res.MemorySwap != 1<<30 || res.NanoCPUs != 2e9 {
		t.Fatalf("limits must survive an unrelated update, got %+v", res)
	}
}

func TestResourceFormExplicitClear(t *testing.T) {
	res := containerType.Resources{Memory: 512 << 20, NanoCPUs: 2e9}
	form := decodeResourceForm(t, `{"memLimit":"","cpus":0}`)
	memBytes, nanoCPUs, err := parseResourceForm(form)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if memBytes == nil || nanoCPUs == nil {
		t.Fatalf("explicit empty values must be present, not absent")
	}
	if err := applyResourceConstraints(&res, memBytes, nanoCPUs); err != nil {
		t.Fatalf("apply: %s", err)
	}
	if res.Memory != 0 {
		t.Errorf("explicit clear must set Memory=0, got %d", res.Memory)
	}
	if res.NanoCPUs != 0 {
		t.Errorf("explicit clear must set NanoCPUs=0, got %d", res.NanoCPUs)
	}
}

func TestResourceFormParseUnits(t *testing.T) {
	// The UI sends the daemon's byte count back verbatim; compose-style units
	// must parse the same way CreateService parses them.
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"512mb", 512 * 1024 * 1024},
		{"1gb", 1024 * 1024 * 1024},
		{"512MiB", 512 * 1024 * 1024},
		{"1073741824", 1073741824},
	} {
		form := decodeResourceForm(t, `{"memLimit":"`+tc.in+`"}`)
		memBytes, _, err := parseResourceForm(form)
		if err != nil {
			t.Fatalf("parse %q: %s", tc.in, err)
		}
		if memBytes == nil || *memBytes != tc.want {
			t.Errorf("parse %q = %v want %d", tc.in, memBytes, tc.want)
		}
	}
	form := decodeResourceForm(t, `{"memLimit":"nonsense"}`)
	if _, _, err := parseResourceForm(form); err == nil {
		t.Errorf("unparseable limit must be reported, not swallowed")
	}
}

func TestResourceFormSwapFloor(t *testing.T) {
	// The daemon rejects mem_limit above an existing memswap_limit. Catching
	// it here means a bad update fails cleanly instead of the recreate failing.
	res := containerType.Resources{Memory: 512 << 20, MemorySwap: 512 << 20}
	form := decodeResourceForm(t, `{"memLimit":"1gb"}`)
	memBytes, _, err := parseResourceForm(form)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if err := applyResourceConstraints(&res, memBytes, nil); err == nil {
		t.Fatalf("mem_limit above memswap_limit must be rejected")
	}
	if res.Memory != 512<<20 {
		t.Errorf("rejected update must not mutate Memory, got %d", res.Memory)
	}

	// Swap unlimited (-1) is not a floor.
	res = containerType.Resources{Memory: 512 << 20, MemorySwap: -1}
	if err := applyResourceConstraints(&res, memBytes, nil); err != nil {
		t.Fatalf("unlimited swap accepts any memory limit, got %s", err)
	}
	if res.Memory != 1<<30 {
		t.Errorf("Memory=%d want 1GB", res.Memory)
	}
}

// The client builds compose payloads as JS objects and serializes them with
// Hjson, so the wire format is a contract between the two: keys the client
// does not send must be absent (never the literal token "undefined", which
// parses back as a string and fails to unmarshal into []string), and byte
// limits must arrive as strings. This locks that shape against the real
// request struct.
func TestWizardComposePayloadShape(t *testing.T) {
	// What the wizard sends for a fresh container: dns has no value, so the
	// key is omitted. The old code wrote `dns: containerInfo.HostConfig.Dns`
	// (undefined), which Hjson stringified as `dns: undefined` and the server
	// read as the string "undefined".
	var req DockerServiceCreateRequest
	err := json.Unmarshal([]byte(`{"services":{"x":{"image":"nginx","dns":"undefined"}}}`), &req)
	if err == nil {
		t.Fatalf("dns as a string must not unmarshal into []string")
	}

	req = DockerServiceCreateRequest{}
	err = json.Unmarshal([]byte(`{"services":{"x":{"image":"nginx","mem_limit":"536870912","cpus":2}}}`), &req)
	if err != nil {
		t.Fatalf("wizard payload with limits must unmarshal: %s", err)
	}
	c := req.Services["x"]
	if c.MemLimit != "536870912" {
		t.Errorf("mem_limit=%q want the byte count as a string", c.MemLimit)
	}
	if c.CPUs != 2 {
		t.Errorf("cpus=%v want 2", c.CPUs)
	}
	// Round-trip: the wizard's byte count parses to the same limit CreateService
	// would apply from a hand-written compose file.
	if bytes, perr := units.RAMInBytes(string(c.MemLimit)); perr != nil || bytes != 512<<20 {
		t.Errorf("mem_limit %q parsed to %d, %v", c.MemLimit, bytes, perr)
	}
}

func TestResourceFormCPUOnlyUpdate(t *testing.T) {
	res := containerType.Resources{Memory: 512 << 20, MemorySwap: 1 << 30}
	form := decodeResourceForm(t, `{"cpus":1.5}`)
	memBytes, nanoCPUs, err := parseResourceForm(form)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if memBytes != nil {
		t.Fatalf("memLimit absent must stay absent")
	}
	if err := applyResourceConstraints(&res, memBytes, nanoCPUs); err != nil {
		t.Fatalf("apply: %s", err)
	}
	if res.NanoCPUs != 1.5e9 {
		t.Errorf("NanoCPUs=%v want 1.5e9", res.NanoCPUs)
	}
	if res.Memory != 512<<20 || res.MemorySwap != 1<<30 {
		t.Errorf("memory limits must survive a CPU-only update, got %+v", res)
	}
}
