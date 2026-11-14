package utils

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The route editor talks to the API through encoding/json, and route fields are
// marshalled under their Go names (LBMode, AdditionalTargets, WhitelistInboundIPs)
// because most of them carry no json tag. Go's decoder matches a JSON key to a
// field case-insensitively, but it does NOT ignore underscores: "socks_auth_mode"
// does not land in SocksAuthMode.
//
// That is the failure these tests pin. The symptom of getting it wrong is
// silent -- the payload decodes, the field stays empty, and the relay runs with
// scheme defaults while the user believes they configured it.

// The exact payload shape client/src/utils/socksRoute.js emits on save. Kept in
// sync with SOCKS_JSON_KEYS there by hand: the client is not compiled here, so
// this is the contract boundary between the two.
const uiSocksPayload = `{
  "name": "socks-out",
  "mode": "PROXY",
  "target": "socks5t://",
  "host": "0.0.0.0:1080",
  "useHost": true,
  "SocksAuthMode": "password",
  "SocksDenyDestinations": ["127.0.0.0/8", "10.0.0.0/8"],
  "SocksUDP": "false"
}`

func TestRouteDecodesUICamelCaseSocksPayload(t *testing.T) {
	var r ProxyRouteConfig
	if err := json.Unmarshal([]byte(uiSocksPayload), &r); err != nil {
		t.Fatalf("the payload the UI sends must decode, got: %v", err)
	}
	if r.SocksAuthMode != "password" {
		t.Errorf("SocksAuthMode = %q, want \"password\" -- the relay would run anonymously", r.SocksAuthMode)
	}
	if len(r.SocksDenyDestinations) != 2 {
		t.Errorf("SocksDenyDestinations = %v, want 2 entries", r.SocksDenyDestinations)
	}
	if r.SocksUDP != "false" {
		t.Errorf("SocksUDP = %q, want \"false\"", r.SocksUDP)
	}
	if r.Target != "socks5t://" {
		t.Errorf("Target = %q, want socks5t://", r.Target)
	}
}

func TestSocksUDPStaysTriStateOverTheWire(t *testing.T) {
	// The whole point of the string tri-state: an omitted override and an
	// explicit false are different instructions (inherit the scheme, versus
	// refuse UDP on a socks5t route). A bool field cannot represent that.
	var absent, off, on ProxyRouteConfig

	if err := json.Unmarshal([]byte(`{"target":"socks5t://"}`), &absent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"target":"socks5t://","SocksUDP":"false"}`), &off); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"target":"socks5t://","SocksUDP":"true"}`), &on); err != nil {
		t.Fatal(err)
	}

	if absent.SocksUDP != "" {
		t.Errorf("unset SocksUDP = %q, want \"\" (inherit the scheme default)", absent.SocksUDP)
	}
	if off.SocksUDP != "false" {
		t.Errorf("explicit SocksUDP = %q, want \"false\"", off.SocksUDP)
	}
	if absent.SocksUDP == off.SocksUDP {
		t.Error("unset and explicit-false decode to the same value: rebinds cannot tell them apart")
	}
	if on.SocksUDP != "true" {
		t.Errorf("SocksUDP \"true\" = %q, want \"true\"", on.SocksUDP)
	}

	// A JSON boolean is what a sloppy client sends; the decoder must reject it
	// loudly rather than coerce it into a "false"-looking string.
	var bad ProxyRouteConfig
	if err := json.Unmarshal([]byte(`{"SocksUDP":false}`), &bad); err == nil {
		t.Error("a JSON boolean decoded into the SocksUDP string field; want a rejection")
	}
}

func TestSocksFieldsMarshalUnderGoFieldNames(t *testing.T) {
	// The API also answers with these routes, so the encoder side matters for
	// any client that reads the config back (SDK, CLI, tests).
	r := ProxyRouteConfig{
		Name:                  "socks-out",
		Target:                "socks5h://",
		SocksAuthMode:         "password",
		SocksDenyDestinations: []string{"127.0.0.0/8"},
		SocksUDP:              "true",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"SocksAuthMode", "SocksDenyDestinations", "SocksUDP"} {
		if !slices.Contains(keysOf(back), key) {
			t.Errorf("marshalled route is missing %q; keys = %v", key, keysOf(back))
		}
	}

	// Empty overrides are omitted, so the saved config stays readable: an
	// explicit "" for every unset relay option is noise.
	empty := ProxyRouteConfig{Name: "n", Target: "socks5://"}
	b, err = json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Contains(got, "SocksAuthMode") || strings.Contains(got, "SocksUDP") {
		t.Errorf("unset socks fields were marshalled: %s", got)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
