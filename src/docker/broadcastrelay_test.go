package docker

import (
	"strings"
	"testing"

	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/docker/docker/api/types"
)

func TestIsRelayEnabled(t *testing.T) {
	n := types.NetworkResource{Labels: map[string]string{RelayLabelKey: "true"}}
	if !isRelayEnabled(n) {
		t.Fatal("expected relay enabled for label true")
	}
	n2 := types.NetworkResource{Labels: map[string]string{}}
	if isRelayEnabled(n2) {
		t.Fatal("expected relay disabled for no label")
	}
	n3 := types.NetworkResource{Labels: map[string]string{RelayLabelKey: "false"}}
	if isRelayEnabled(n3) {
		t.Fatal("expected relay disabled for label false")
	}
}

func TestBridgeInterfaceForHostMode(t *testing.T) {
	// Host mode: br-<first 12 of ID>.
	n := types.NetworkResource{
		ID:     "abcdef0123456789deadbeef",
		Driver: "bridge",
	}
	if got := bridgeInterfaceForHost(n); got != "br-abcdef012345" {
		t.Fatalf("host-mode bridge name wrong: got %q", got)
	}
}

func TestBridgeInterfaceForNonBridge(t *testing.T) {
	n := types.NetworkResource{ID: "abcdef0123456789deadbeef", Driver: "overlay"}
	if got := bridgeInterfaceForHost(n); got != "" {
		t.Fatalf("overlay should return empty, got %q", got)
	}
}

func TestToggleNetworkRelayPublishesToMemoryStore(t *testing.T) {
	prevFolder := utils.CONFIGFOLDER
	utils.CONFIGFOLDER = t.TempDir() + "/"
	t.Cleanup(func() {
		utils.CONFIGFOLDER = prevFolder
	})

	// Give the in-memory store a known starting point. relayMgr.relay is nil in
	// this test, so ReconcileBroadcastRelay() returns immediately without
	// touching Docker; we only assert on the config store, which is what the
	// UI's ListNetworksRoute reads to render the toggle.
	utils.LoadBaseMainConfig(utils.DefaultConfig)
	cfg := utils.GetMainConfig()
	if len(cfg.DockerConfig.RelayNetworks) != 0 {
		t.Fatalf("expected empty RelayNetworks at start, got %#v", cfg.DockerConfig.RelayNetworks)
	}

	// Enabling must be visible to GetMainConfig immediately (regression test:
	// previously only the file was updated, so the toggle appeared to fall back
	// off until restart).
	ToggleNetworkRelay("test-net", true)
	cfg = utils.GetMainConfig()
	if !strings.Contains(strings.Join(cfg.DockerConfig.RelayNetworks, ","), "test-net") {
		t.Fatalf("relay network not published to in-memory config after enable: %#v", cfg.DockerConfig.RelayNetworks)
	}

	// Disabling must remove it from the in-memory config immediately.
	ToggleNetworkRelay("test-net", false)
	cfg = utils.GetMainConfig()
	for _, n := range cfg.DockerConfig.RelayNetworks {
		if n == "test-net" {
			t.Fatal("relay network still present in in-memory config after disable")
		}
	}
}
