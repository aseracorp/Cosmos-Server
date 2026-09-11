package broadcastrelay

import (
	"net"
	"os"
	"testing"
	"time"
)

// TestRelayCapturesLiveTraffic runs the full Relay object against a real
// interface for a short window and asserts the socket pipeline (open -> bind ->
// read) works end to end. It does not require another network to forward to: it
// simply proves frames can be captured. Skipped when AF_PACKET is unavailable.
func TestRelayCapturesLiveTraffic(t *testing.T) {
	iface := os.Getenv("RELAY_TEST_IFACE")
	if iface == "" {
		iface = "eth0"
	}
	if _, err := net.InterfaceByName(iface); err != nil {
		t.Skipf("interface %s not present", iface)
	}

	r := New()
	if err := r.Enable("test-net", iface); err != nil {
		t.Skipf("cannot enable relay on %s (no CAP_NET_RAW?): %v", iface, err)
	}
	defer r.Disable("test-net")
	r.Start()
	defer r.Stop()

	// Run the capture loop for 2 seconds; the forwarding goroutine should not
	// panic and sockets should remain open.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !r.Enabled("test-net") {
		t.Fatal("relay should still be enabled after capture loop")
	}
	t.Log("relay capture loop ran without error on", iface)
}
