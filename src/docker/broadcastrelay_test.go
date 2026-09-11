package docker

import (
	"testing"

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
