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

func TestResourceFormAbsencePreservesLimits(t *testing.T) {
	// A partial update that touches nothing resource-related must leave the
	// existing limits alone: the helper receives nil and changes nothing.
	res := containerType.Resources{
		Memory: 512 << 20, MemorySwap: 1 << 30, MemoryReservation: 256 << 20, NanoCPUs: 2e9,
	}
	form := decodeResourceForm(t, `{"envVars":["A=b"]}`)
	if form.MemLimit != nil || form.MemSwapLimit != nil || form.MemReservation != nil || form.CPUs != nil {
		t.Fatalf("omitted fields must decode as nil, got %+v", form)
	}
	if err := applyResourceConstraints(&res, nil, nil, nil, nil); err != nil {
		t.Fatalf("apply with no limits: %s", err)
	}
	if res.Memory != 512<<20 || res.MemorySwap != 1<<30 || res.MemoryReservation != 256<<20 || res.NanoCPUs != 2e9 {
		t.Fatalf("limits must survive an unrelated update, got %+v", res)
	}
}

func TestResourceFormExplicitClear(t *testing.T) {
	res := containerType.Resources{Memory: 512 << 20, MemorySwap: 1 << 30, NanoCPUs: 2e9}
	form := decodeResourceForm(t, `{"memLimit":"","cpus":0}`)
	mem, swap, resv, cpus, err := parseResourceForm(form)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if mem == nil || cpus == nil {
		t.Fatalf("explicit empty values must be present, not absent")
	}
	if swap != nil || resv != nil {
		t.Fatalf("unmentioned swap/reservation must stay absent, got %v %v", swap, resv)
	}
	if err := applyResourceConstraints(&res, mem, swap, resv, cpus); err != nil {
		t.Fatalf("apply: %s", err)
	}
	if res.Memory != 0 {
		t.Errorf("explicit clear must set Memory=0, got %d", res.Memory)
	}
	if res.NanoCPUs != 0 {
		t.Errorf("explicit clear must set NanoCPUs=0, got %d", res.NanoCPUs)
	}
	// Clearing the memory limit leaves an existing swap limit in place, which
	// is what the daemon wants: swap accounting without a memory limit is not
	// something Cosmos should decide silently.
	if res.MemorySwap != 1<<30 {
		t.Errorf("MemorySwap must survive an unrelated clear, got %d", res.MemorySwap)
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
		form := decodeResourceForm(t, `{"memLimit":"`+tc.in+`","memSwapLimit":"`+tc.in+`","memReservation":"`+tc.in+`"}`)
		mem, swap, resv, _, err := parseResourceForm(form)
		if err != nil {
			t.Fatalf("parse %q: %s", tc.in, err)
		}
		for _, p := range []*int64{mem, swap, resv} {
			if p == nil || *p != tc.want {
				t.Errorf("parse %q = %v want %d", tc.in, p, tc.want)
			}
		}
	}
	form := decodeResourceForm(t, `{"memLimit":"nonsense"}`)
	if _, _, _, _, err := parseResourceForm(form); err == nil {
		t.Errorf("unparseable limit must be reported, not swallowed")
	}
}

// The daemon rejects a swap limit below the memory limit and a reservation
// above it, so Cosmos validates the resulting state itself: a bad update fails
// cleanly here instead of failing the container recreate later.
func TestResourceFormPairValidation(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		base    containerType.Resources
		wantErr bool
	}{
		{"swap below memory", `{"memLimit":"1gb"}`, containerType.Resources{MemorySwap: 512 << 20}, true},
		{"swap equal to memory", `{"memLimit":"512mb","memSwapLimit":"512mb"}`, containerType.Resources{}, false},
		{"unlimited swap accepts any memory", `{"memLimit":"1gb"}`, containerType.Resources{MemorySwap: -1}, false},
		{"explicit unlimited swap", `{"memLimit":"512mb","memSwapLimit":"-1"}`, containerType.Resources{}, false},
		{"reservation above memory", `{"memLimit":"512mb","memReservation":"1gb"}`, containerType.Resources{}, true},
		{"reservation below memory", `{"memLimit":"512mb","memReservation":"256mb"}`, containerType.Resources{}, false},
		{"clearing memory keeps a stale swap", `{"memLimit":""}`, containerType.Resources{Memory: 512 << 20, MemorySwap: 1 << 30}, false},
	}
	for _, c := range cases {
		res := c.base
		mem, swap, resv, cpus, err := parseResourceForm(decodeResourceForm(t, c.body))
		if err != nil {
			t.Fatalf("%s: parse: %s", c.name, err)
		}
		err = applyResourceConstraints(&res, mem, swap, resv, cpus)
		if c.wantErr && err == nil {
			t.Errorf("%s: must be rejected", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: must be accepted, got %s", c.name, err)
		}
	}

	// A rejected update must not mutate the state it was checked against.
	res := containerType.Resources{Memory: 512 << 20, MemorySwap: 512 << 20}
	mem, swap, resv, cpus, _ := parseResourceForm(decodeResourceForm(t, `{"memLimit":"1gb"}`))
	if applyResourceConstraints(&res, mem, swap, resv, cpus) == nil {
		t.Fatalf("swap floor must be enforced")
	}
	if res.Memory != 512<<20 {
		t.Errorf("rejected update must not mutate Memory, got %d", res.Memory)
	}
}

func TestResourceFormFieldsAreIndependent(t *testing.T) {
	// A CPU-only update must not touch any of the three memory limits.
	res := containerType.Resources{
		Memory: 512 << 20, MemorySwap: 1 << 30, MemoryReservation: 256 << 20,
	}
	mem, swap, resv, cpus, err := parseResourceForm(decodeResourceForm(t, `{"cpus":1.5}`))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if err := applyResourceConstraints(&res, mem, swap, resv, cpus); err != nil {
		t.Fatalf("apply: %s", err)
	}
	if res.NanoCPUs != 1.5e9 {
		t.Errorf("NanoCPUs=%v want 1.5e9", res.NanoCPUs)
	}
	if res.Memory != 512<<20 || res.MemorySwap != 1<<30 || res.MemoryReservation != 256<<20 {
		t.Errorf("memory limits must survive a CPU-only update, got %+v", res)
	}

	// A swap-only update must leave memory and reservation alone.
	res = containerType.Resources{Memory: 512 << 20, MemoryReservation: 256 << 20}
	mem, swap, resv, cpus, err = parseResourceForm(decodeResourceForm(t, `{"memSwapLimit":"2gb"}`))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if err := applyResourceConstraints(&res, mem, swap, resv, cpus); err != nil {
		t.Fatalf("apply: %s", err)
	}
	if res.MemorySwap != 2<<30 {
		t.Errorf("MemorySwap=%d want 2gb", res.MemorySwap)
	}
	if res.Memory != 512<<20 || res.MemoryReservation != 256<<20 {
		t.Errorf("swap-only update must not touch memory/reservation, got %+v", res)
	}
}

// The wizard builds compose payloads as JS objects and serializes them with
// Hjson, so the wire format is a contract between the two: keys the client
// does not send must be absent (never the literal token "undefined", which
// parses back as a string and fails to unmarshal into []string), and byte
// limits must arrive as strings. This locks that shape against the real
// request struct.
func TestWizardComposePayloadShape(t *testing.T) {
	var req DockerServiceCreateRequest
	err := json.Unmarshal([]byte(`{"services":{"x":{"image":"nginx","dns":"undefined"}}}`), &req)
	if err == nil {
		t.Fatalf("dns as a string must not unmarshal into []string")
	}

	req = DockerServiceCreateRequest{}
	err = json.Unmarshal([]byte(`{"services":{"x":{"image":"nginx","mem_limit":"536870912","memswap_limit":"1gb","cpus":2}}}`), &req)
	if err != nil {
		t.Fatalf("wizard payload with limits must unmarshal: %s", err)
	}
	c := req.Services["x"]
	if c.MemLimit != "536870912" {
		t.Errorf("mem_limit=%q want the byte count as a string", c.MemLimit)
	}
	if c.MemSwapLimit != "1gb" {
		t.Errorf("memswap_limit=%q want the compose byte size", c.MemSwapLimit)
	}
	if c.CPUs != 2 {
		t.Errorf("cpus=%v want 2", c.CPUs)
	}
	if bytes, perr := units.RAMInBytes(string(c.MemLimit)); perr != nil || bytes != 512<<20 {
		t.Errorf("mem_limit %q parsed to %d, %v", c.MemLimit, bytes, perr)
	}
}

// The export is what the UI reads back, so a swap limit must survive the
// round-trip. FormatByteSize returns "" for -1, which is why export.go
// special-cases unlimited swap: rendering it as unset would lose the setting.
//
// units.RAMInBytes rejects negative sizes ("Backward compatibility: reject
// negative sizes"), so "-1" cannot be parsed by RAMInBytes. Every consumer has
// to special-case it — CreateService does, parseResourceForm does, and this
// test pins that down so a future refactor does not "simplify" it into a
// RAMInBytes call that silently turns unlimited swap into a parse error.
func TestExportSwapRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		bytes     int64
		exported  string
		wantParse int64
	}{
		{1 << 30, "1gb", 1 << 30},
		{512 << 20, "512mb", 512 << 20},
		{1500, "1500b", 1500},
	} {
		got := FormatByteSize(tc.bytes)
		if got != tc.exported {
			t.Errorf("FormatByteSize(%d)=%q want %q", tc.bytes, got, tc.exported)
		}
		if parsed, err := units.RAMInBytes(got); err != nil || parsed != tc.wantParse {
			t.Errorf("round-trip of %d via %q gave %d, %v", tc.bytes, got, parsed, err)
		}
	}

	if FormatByteSize(-1) != "" {
		t.Errorf("FormatByteSize(-1) is expected to be empty, which is why export handles -1 itself")
	}
	if _, err := units.RAMInBytes("-1"); err == nil {
		t.Errorf(`RAMInBytes("-1") must fail — "-1" has to be special-cased, not parsed`)
	}

	// The update route handles it, as CreateService does.
	form := decodeResourceForm(t, `{"memLimit":"512mb","memSwapLimit":"-1"}`)
	_, swap, _, _, err := parseResourceForm(form)
	if err != nil {
		t.Fatalf(`parseResourceForm must accept "-1": %s`, err)
	}
	if swap == nil || *swap != -1 {
		t.Errorf(`memswap_limit "-1" must parse to -1 (unlimited), got %v`, swap)
	}
}
