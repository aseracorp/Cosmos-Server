// Community build stub of the Cosmos Pro feature set: types exist so the shared
// code and the SDK generator compile; handlers answer PRO001 and hooks do nothing.

package pro

import (
	"github.com/nats-io/nats.go"
)

// RegisterCIResponder subscribes this node to its own op subject.
func RegisterCIResponder(nc *nats.Conn, self string) (*nats.Subscription, error) {
	// Pro feature stub.
	return nil, nil
}

// StartCIScheduler starts the leader loop, once per process.
func StartCIScheduler() {
	// Pro feature stub.
}

// SetCIMetricPusher wires the metrics sink for CI counters.
func SetCIMetricPusher(f func(key string, value int, label, unit, object, setOperation, agglo string)) {
	// Pro feature stub.
}

// CIImagePullAuth is wired as docker.ImagePullAuthProvider.
func CIImagePullAuth(host string) (string, string) {
	// Pro feature stub.
	return "", ""
}
