package docker

import (
	"net"
	"strings"
	"sync"

	"github.com/azukaar/cosmos-server/src/broadcastrelay"
	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/docker/docker/api/types"
)

// RelayLabelKey is the Docker network label that marks a network as
// relay-enabled when it was created via Cosmos. Existing networks cannot have
// their labels mutated in place by Docker, so the persistent source of truth is
// the main config (utils.DockerConfig.RelayNetworks); the label is a
// machine-readable marker for networks created with the toggle on.
const RelayLabelKey = "cosmos.broadcastrelay"

// relayManager owns the single in-process broadcast/multicast relay and keeps it
// in sync with the set of Docker networks that are relay-enabled.
type relayManager struct {
	mu    sync.Mutex
	relay *broadcastrelay.Relay
}

var relayMgr = &relayManager{}

// InitBroadcastRelay starts the relay manager and reconciles it with the current
// set of Docker networks. Must be called after Docker is connected.
func InitBroadcastRelay() {
	relayMgr.mu.Lock()
	defer relayMgr.mu.Unlock()

	relayMgr.relay = broadcastrelay.New()
	relayMgr.relay.Start()
	relayMgr.reconcileLocked()
}

// ReconcileBroadcastRelay re-scans Docker networks and enables/disables relay on
// them according to the config list and labels. Safe to call repeatedly (e.g. on
// Docker events).
func ReconcileBroadcastRelay() {
	relayMgr.mu.Lock()
	defer relayMgr.mu.Unlock()
	relayMgr.reconcileLocked()
}

// StopBroadcastRelay shuts down the relay (used on server shutdown).
func StopBroadcastRelay() {
	relayMgr.mu.Lock()
	defer relayMgr.mu.Unlock()
	if relayMgr.relay != nil {
		relayMgr.relay.Stop()
	}
}

// ToggleNetworkRelay enables or disables broadcast/multicast relaying on a
// network, persisting the choice in the main config and re-reconciling.
func ToggleNetworkRelay(networkName string, enabled bool) {
	config := utils.GetMainConfig()

	if enabled {
		for _, n := range config.DockerConfig.RelayNetworks {
			if n == networkName {
				// already present
				utils.SaveConfigTofile(config)
				ReconcileBroadcastRelay()
				return
			}
		}
		config.DockerConfig.RelayNetworks = append(config.DockerConfig.RelayNetworks, networkName)
	} else {
		out := config.DockerConfig.RelayNetworks[:0]
		for _, n := range config.DockerConfig.RelayNetworks {
			if n != networkName {
				out = append(out, n)
			}
		}
		config.DockerConfig.RelayNetworks = out
	}

	utils.SaveConfigTofile(config)
	utils.Log("[BroadcastRelay] Toggled relay on network " + networkName + " -> " + strings.ToUpper(boolStr(enabled)))
	ReconcileBroadcastRelay()
}

// IsNetworkRelayEnabled reports whether a network has relay enabled, either via
// the main config list or its Docker label.
func IsNetworkRelayEnabled(networkName string) bool {
	config := utils.GetMainConfig()
	for _, n := range config.DockerConfig.RelayNetworks {
		if n == networkName {
			return true
		}
	}
	// Fall back to the label for networks created via Cosmos with the toggle on.
	if net, err := DockerClient.NetworkInspect(DockerContext, networkName, types.NetworkInspectOptions{}); err == nil {
		return isRelayEnabled(net)
	}
	return false
}

// reconcileLocked must be called with relayMgr.mu held.
func (m *relayManager) reconcileLocked() {
	if m.relay == nil {
		return
	}
	if err := Connect(); err != nil {
		utils.Error("[BroadcastRelay] Cannot connect to Docker for reconciliation", err)
		return
	}

	networks, err := DockerClient.NetworkList(DockerContext, types.NetworkListOptions{})
	if err != nil {
		utils.Error("[BroadcastRelay] Failed to list networks", err)
		return
	}

	// Build the set of relay-enabled networks (config list OR label).
	config := utils.GetMainConfig()
	enabledSet := map[string]bool{}
	for _, n := range config.DockerConfig.RelayNetworks {
		enabledSet[n] = true
	}
	for _, n := range networks {
		if isRelayEnabled(n) {
			enabledSet[n.Name] = true
		}
	}

	// Map network name -> linux bridge interface name.
	wanted := map[string]string{}
	for _, n := range networks {
		if enabledSet[n.Name] {
			iface := bridgeInterfaceFor(n)
			if iface != "" {
				wanted[n.Name] = iface
			} else {
				utils.Warn("[BroadcastRelay] Network " + n.Name + " has relay enabled but no bridge interface found")
			}
		}
	}

	// Disable networks that are no longer wanted.
	for name := range m.relayEnabledSnapshot() {
		if _, ok := wanted[name]; !ok {
			m.relay.Disable(name)
			utils.Log("[BroadcastRelay] Disabled relay on network " + name)
		}
	}

	// Enable wanted networks.
	for name, iface := range wanted {
		if err := m.relay.Enable(name, iface); err != nil {
			utils.Error("[BroadcastRelay] Failed to enable relay on network "+name, err)
		} else {
			utils.Log("[BroadcastRelay] Enabled relay on network " + name + " (" + iface + ")")
		}
	}
}

// relayEnabledSnapshot returns the set of currently-enabled network names.
func (m *relayManager) relayEnabledSnapshot() map[string]bool {
	if m.relay == nil {
		return map[string]bool{}
	}
	// The relay tracks enabled networks by name; reconstruct the snapshot.
	config := utils.GetMainConfig()
	out := map[string]bool{}
	for _, n := range config.DockerConfig.RelayNetworks {
		if m.relay.Enabled(n) {
			out[n] = true
		}
	}
	return out
}

// bridgeInterfaceFor returns the Linux interface Cosmos should capture on for a
// Docker network. Resolution depends on where Cosmos itself runs:
//
//   - Host network mode: the Docker bridge is visible as "br-<id>" and we can
//     capture on it directly.
//   - Inside a container: Cosmos is attached to the network through a local
//     veth (eth0, eth1, ...) whose IP belongs to the network's subnet; we find
//     that interface by matching its addresses against the network IPAM subnet.
//
// Returns "" when the interface cannot be determined (e.g. overlay networks to
// which Cosmos has no local attachment).
func bridgeInterfaceFor(n types.NetworkResource) string {
	if n.Driver != "bridge" && n.Driver != "default" {
		return ""
	}

	// Host mode: bridge device name = br-<first 12 hex of network ID>.
	if utils.IsHostNetwork || !utils.IsInsideContainer {
		return bridgeInterfaceForHost(n)
	}

	// Container mode: find the local interface whose IP is in the subnet.
	return localIfaceForSubnet(n)
}

// bridgeInterfaceForHost returns the Linux bridge device name ("br-<id>") for a
// bridge-driver network when Cosmos runs on the host network namespace.
func bridgeInterfaceForHost(n types.NetworkResource) string {
	if n.Driver != "bridge" && n.Driver != "default" {
		return ""
	}
	if len(n.ID) >= 12 {
		return "br-" + n.ID[:12]
	}
	return ""
}

// localIfaceForSubnet finds the local interface with an IPv4 address inside the
// network's first IPAM subnet. Returns "" if none matches (Cosmos is not
// attached to the network, or it's a subnetless/default network).
func localIfaceForSubnet(n types.NetworkResource) string {
	subnet := ""
	if len(n.IPAM.Config) > 0 {
		subnet = n.IPAM.Config[0].Subnet
	}
	if subnet == "" {
		return ""
	}
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return ""
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet2, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet2.IP.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			if ipnet.Contains(ip4) {
				return iface.Name
			}
		}
	}
	return ""
}

// isRelayEnabled reports whether a network has the relay label set.
func isRelayEnabled(n types.NetworkResource) bool {
	return strings.EqualFold(n.Labels[RelayLabelKey], "true")
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
