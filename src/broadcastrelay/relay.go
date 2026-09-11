// Package broadcastrelay implements in-process relaying of IPv4 broadcast and
// multicast (L2) frames between Docker bridge networks.
//
// Motivation: by default, Docker bridge networks are isolated from one another —
// broadcast and multicast traffic (mDNS, SSDP/UPnP, NetBIOS, Chromecast, printer
// discovery, etc.) does not cross network boundaries. This package sniffs such
// frames on the bridge interface of every *enabled* network and re-injects them
// onto the other enabled networks, so that services on different networks can
// discover each other without loosening network isolation for unicast traffic.
//
// This is the functional equivalent of tools such as udp-broadcast-relay(-redux),
// but implemented in-process in Go and covering ALL multicast/broadcast IPv4
// traffic (not a single UDP port) between any pair of enabled networks.
package broadcastrelay

import (
	"net"
	"sync"
	"time"
)

// etherType we care about: only IPv4 frames are relayed.
const ethTypeIP = 0x0800

// broadcastMAC is the L2 broadcast destination address.
var broadcastMAC = [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// multicastMACBit is the LSB of the first MAC octet; set for multicast frames.
const multicastMACBit = 0x01

// dedupWindow is how long a seen frame is remembered to prevent relay loops
// between N networks (mesh). Slightly above the max TTL of an mDNS/SSDP retry.
const dedupWindow = 3 * time.Second

// shouldRelayUDP decides whether a UDP datagram destined to a broadcast or
// multicast address should be relayed.
//
// Policy: relay ALL broadcast/multicast IPv4 UDP between enabled networks,
// EXCEPT DHCP (ports 67/68). Relaying DHCP across networks is dangerous — it
// can hand out addresses, cause IP conflicts, or accept rogue offers — so it is
// deliberately never forwarded. Everything else (mDNS, SSDP/UPnP, NetBIOS,
// WS-Discovery, Chromecast/DIAL, ARP-less discovery, games, ...) is relayed,
// matching the behaviour of udp-broadcast-relay(-redux) when enabled.
func shouldRelayUDP(dstPort uint16, dstIP net.IP) bool {
	// Never relay DHCP — could hand out addresses on the wrong network.
	if dstPort == 67 || dstPort == 68 {
		return false
	}
	// Relay all other broadcast/multicast UDP traffic between enabled networks.
	_ = dstIP
	return true
}

// Relay manages the set of network bridge interfaces between which broadcast and
// multicast frames are forwarded.
type Relay struct {
	mu       sync.Mutex
	enabled  map[string]*netIf         // network name -> interface state
	running  bool
	stop     chan struct{}
	dedup    *dedupCache
}

// netIf describes one enabled network's bridge interface.
type netIf struct {
	network string // Docker network name
	iface   string // Linux bridge interface name (br-<id>)
	idx     int    // interface index
	hw      [6]byte // interface hardware (MAC) address
	sock    int    // AF_PACKET socket bound to the interface
}

// New creates a relay with the given networks enabled. Each entry must be the
// Linux bridge interface name for a Docker network.
func New() *Relay {
	return &Relay{
		enabled: make(map[string]*netIf),
		dedup:   newDedupCache(),
	}
}

// Enable adds a network to be relayed and (re)starts the capture loop if needed.
// iface must be the bridge interface name, e.g. "br-1a2b3c4d5e6f".
func (r *Relay) Enable(network, iface string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If already enabled on the same interface, nothing to do.
	if cur, ok := r.enabled[network]; ok && cur.iface == iface {
		return nil
	}

	ni, err := openInterface(iface)
	if err != nil {
		return err
	}
	ni.network = network

	// Replace existing.
	if old, ok := r.enabled[network]; ok {
		closeSocket(old.sock)
	}
	r.enabled[network] = ni

	return nil
}

// Disable removes a network from the relay and stops capturing on it.
func (r *Relay) Disable(network string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ni, ok := r.enabled[network]; ok {
		closeSocket(ni.sock)
		delete(r.enabled, network)
	}
}

// Enabled returns true if the given network is currently being relayed.
func (r *Relay) Enabled(network string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.enabled[network]
	return ok
}

// Start launches the forwarding goroutine for all currently-enabled networks.
// It is safe to call multiple times.
func (r *Relay) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	r.running = true
	r.stop = make(chan struct{})
	stop := r.stop
	r.mu.Unlock()

	go r.run(stop)
	r.mu.Lock()
}

// Stop terminates all capture loops.
func (r *Relay) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stop)
	for _, ni := range r.enabled {
		closeSocket(ni.sock)
	}
	r.enabled = make(map[string]*netIf)
}

func (r *Relay) run(stop chan struct{}) {
	// Polling loop: on each tick, rebuild the set of sockets to read from and
	// forward frames. This keeps the implementation simple and robust to
	// interface churn, and matches the low-traffic nature of discovery traffic.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.forwardOnce()
		}
	}
}

// forwardOnce reads one pending frame from each enabled socket and forwards it
// to all other enabled sockets.
func (r *Relay) forwardOnce() {
	r.mu.Lock()
	// Snapshot the enabled set under lock, then release before blocking reads.
	items := make([]*netIf, 0, len(r.enabled))
	for _, ni := range r.enabled {
		items = append(items, ni)
	}
	r.mu.Unlock()

	for _, src := range items {
		// Non-blocking read of a single frame from each socket.
		buf := make([]byte, 65536)
		n, from, err := readPacket(src.sock, buf)
		if err != nil || n <= 0 {
			continue
		}
		_ = from
		if !isRelayableFrame(buf[:n]) {
			continue
		}
		r.forwardFrame(src, buf[:n])
	}
}

// forwardFrame re-injects the frame on every OTHER enabled interface, rewriting
// source MAC and decrementing TTL / recomputing checksums to avoid confusing the
// receiving network, and dedups to prevent loops.
func (r *Relay) forwardFrame(src *netIf, frame []byte) {
	// Loop protection: hash the relevant header bytes.
	if !r.dedup.see(frame) {
		return
	}

	r.mu.Lock()
	targets := make([]*netIf, 0, len(r.enabled))
	for name, ni := range r.enabled {
		if name != src.network {
			targets = append(targets, ni)
		}
	}
	r.mu.Unlock()

	for _, dst := range targets {
		out := rewriteFrame(frame, src, dst)
		if out == nil {
			continue
		}
		sendPacket(dst.sock, out)
	}
}
